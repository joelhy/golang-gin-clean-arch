package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"clean-arch-gin/mysqlstore/model"
	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/order"
	"clean-arch-gin/product"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	mysqlDeadlockCode        uint16 = 1213
	mysqlLockWaitTimeoutCode uint16 = 1205
	orderCheckoutReason             = "order_checkout"
	orderCancelReason               = "order_cancel"
	maxOrderTxRetries               = 3
)

type OrderStore struct {
	db *gorm.DB
	q  *query.Query
}

type orderStoreTx struct {
	db *gorm.DB
	q  *query.Query

	lockedProducts  map[uint64]*lockedProductState
	lockedOrder     *order.Order
	pendingLedger   []pendingStockAdjustment
	ledgerFlushed   bool
	cancellationSet bool
}

type lockedProductState struct {
	id       uint64
	sku      string
	name     string
	price    int64
	currency string
	stock    uint32
	version  uint64
}

type pendingStockAdjustment struct {
	productID   uint64
	delta       int32
	stockAfter  uint32
	reason      string
	actorUserID uint64
}

var _ order.Transactor = (*OrderStore)(nil)
var _ order.Reader = (*OrderStore)(nil)
var _ order.Tx = (*orderStoreTx)(nil)

func NewOrderStore(db *gorm.DB) (*OrderStore, error) {
	if db == nil {
		return nil, errors.New("new MySQL order store: database is required")
	}
	return &OrderStore{db: db, q: query.Use(db)}, nil
}

func (s *OrderStore) WithinTransaction(ctx context.Context, fn func(order.Tx) error) error {
	if ctx == nil {
		return errors.New("order transaction: context is required")
	}
	if fn == nil {
		return errors.New("order transaction: callback is required")
	}

	var lastErr error
	for attempt := 0; attempt <= maxOrderTxRetries; attempt++ {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return fn(newOrderStoreTx(tx))
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == maxOrderTxRetries || !isRetryableOrderTxError(err) {
			return err
		}
		// Deadlock and lock-wait retries must rerun the whole callback on a brand-new
		// transaction so no partially-mutated in-memory state leaks across attempts.
		if err := waitOrderRetry(ctx, attempt); err != nil {
			return fmt.Errorf("order transaction: %w", err)
		}
	}
	return lastErr
}

func (s *OrderStore) ByID(ctx context.Context, id uint64) (*order.Order, error) {
	if ctx == nil {
		return nil, errors.New("find order by ID: context is required")
	}
	if id == 0 {
		return nil, nil
	}

	row, err := s.q.Order.WithContext(ctx).Where(s.q.Order.ID.Eq(id)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find order by ID: %w", err)
	}

	itemsByOrderID, err := s.loadOrderItems(ctx, []uint64{id})
	if err != nil {
		return nil, fmt.Errorf("find order by ID: %w", err)
	}
	mapped := modelToOrder(row, itemsByOrderID[row.ID])
	return &mapped, nil
}

func (s *OrderStore) List(ctx context.Context, filter order.ListFilter) (order.Page, error) {
	if ctx == nil {
		return order.Page{}, errors.New("list orders: context is required")
	}
	if err := filter.Validate(); err != nil {
		return order.Page{}, fmt.Errorf("list orders: %w", err)
	}

	dao := s.q.Order.WithContext(ctx)
	dao = applyOrderFilters(dao, s.q, filter)

	total, err := dao.Count()
	if err != nil {
		return order.Page{}, fmt.Errorf("list orders: count: %w", err)
	}

	orderExpr, err := orderSortExpr(s.q, filter.Sort, filter.Descending)
	if err != nil {
		return order.Page{}, err
	}
	dao = dao.Order(orderExpr)
	if filter.Sort != "id" {
		// Stable secondary ordering keeps list pagination deterministic when many
		// rows share the same business sort key such as total or created_at.
		dao = dao.Order(s.q.Order.ID.Asc())
	}

	rows, err := dao.Offset(filter.Offset).Limit(filter.Limit).Find()
	if err != nil {
		return order.Page{}, fmt.Errorf("list orders: query page: %w", err)
	}
	orderIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		orderIDs = append(orderIDs, row.ID)
	}
	itemsByOrderID, err := s.loadOrderItems(ctx, orderIDs)
	if err != nil {
		return order.Page{}, fmt.Errorf("list orders: %w", err)
	}

	items := make([]order.Order, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelToOrder(row, itemsByOrderID[row.ID]))
	}
	return order.Page{Items: items, Total: uint64(total), Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (s *OrderStore) loadOrderItems(ctx context.Context, orderIDs []uint64) (map[uint64][]order.Item, error) {
	if len(orderIDs) == 0 {
		return map[uint64][]order.Item{}, nil
	}
	rows, err := s.q.OrderItem.WithContext(ctx).
		Where(s.q.OrderItem.OrderID.In(orderIDs...)).
		Order(s.q.OrderItem.ID.Asc()).
		Find()
	if err != nil {
		return nil, fmt.Errorf("load order items: %w", err)
	}
	itemsByOrderID := make(map[uint64][]order.Item, len(orderIDs))
	for _, row := range rows {
		itemsByOrderID[row.OrderID] = append(itemsByOrderID[row.OrderID], modelToOrderItem(row))
	}
	return itemsByOrderID, nil
}

func newOrderStoreTx(db *gorm.DB) *orderStoreTx {
	return &orderStoreTx{
		db:             db,
		q:              query.Use(db),
		lockedProducts: make(map[uint64]*lockedProductState),
	}
}

func (tx *orderStoreTx) ClaimIdempotency(ctx context.Context, claim order.IdempotencyClaim) (order.IdempotencyResult, error) {
	row := &model.IdempotencyKey{
		UserID:         claim.UserID,
		Operation:      claim.Operation,
		IdempotencyKey: claim.Key,
		RequestHash:    append([]byte(nil), claim.RequestHash[:]...),
		CreatedAt:      claim.CreatedAt.UTC(),
		UpdatedAt:      claim.CreatedAt.UTC(),
	}
	if err := tx.q.IdempotencyKey.WithContext(ctx).Create(row); err == nil {
		return order.IdempotencyResult{Kind: order.IdempotencyClaimed}, nil
	} else if !isDuplicateKeyError(err) {
		return order.IdempotencyResult{}, fmt.Errorf("claim idempotency: insert claim: %w", err)
	}

	// The duplicate path takes an UPDATE lock on the existing scope row so every
	// same-key caller observes one serialized request hash and completion outcome.
	existing, err := tx.q.IdempotencyKey.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			tx.q.IdempotencyKey.UserID.Eq(claim.UserID),
			tx.q.IdempotencyKey.Operation.Eq(claim.Operation),
			tx.q.IdempotencyKey.IdempotencyKey.Eq(claim.Key),
		).
		First()
	if err != nil {
		return order.IdempotencyResult{}, fmt.Errorf("claim idempotency: lock existing claim: %w", err)
	}
	if len(existing.RequestHash) != len(claim.RequestHash) || !slices.Equal(existing.RequestHash, claim.RequestHash[:]) {
		return order.IdempotencyResult{Kind: order.IdempotencyConflict}, nil
	}
	if existing.OrderID != nil {
		return order.IdempotencyResult{Kind: order.IdempotencyReplay, OrderID: *existing.OrderID}, nil
	}
	return order.IdempotencyResult{Kind: order.IdempotencyClaimed}, nil
}

func (tx *orderStoreTx) LockProducts(ctx context.Context, ids []uint64) ([]order.SellableProduct, error) {
	sortedIDs := normalizedSortedIDs(ids)
	if len(sortedIDs) == 0 {
		return nil, nil
	}

	rows, err := tx.q.Product.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			tx.q.Product.ID.In(sortedIDs...),
			tx.q.Product.Status.Eq(string(product.StatusActive)),
		).
		Order(tx.q.Product.ID.Asc()).
		Find()
	if err != nil {
		return nil, fmt.Errorf("lock products: %w", err)
	}
	if len(rows) != len(sortedIDs) {
		return nil, order.ErrNotFound
	}

	products := make([]order.SellableProduct, 0, len(rows))
	for _, row := range rows {
		tx.lockedProducts[row.ID] = &lockedProductState{
			id:       row.ID,
			sku:      row.SKU,
			name:     row.Name,
			price:    row.PriceAmount,
			currency: row.Currency,
			stock:    row.Stock,
			version:  row.Version,
		}
		products = append(products, order.SellableProduct{
			ID:        row.ID,
			SKU:       row.SKU,
			Name:      row.Name,
			UnitPrice: order.Money{Amount: row.PriceAmount, Currency: row.Currency},
			Stock:     row.Stock,
		})
	}
	return products, nil
}

func (tx *orderStoreTx) DecreaseStock(ctx context.Context, change order.StockChange) error {
	return tx.applyStockChange(ctx, change, orderCheckoutReason)
}

func (tx *orderStoreTx) IncreaseStock(ctx context.Context, change order.StockChange) error {
	if err := tx.ensureCancellationProductsLocked(ctx); err != nil {
		return err
	}
	return tx.applyStockChange(ctx, change, orderCancelReason)
}

func (tx *orderStoreTx) applyStockChange(ctx context.Context, change order.StockChange, reason string) error {
	state, ok := tx.lockedProducts[change.ProductID]
	if !ok {
		return fmt.Errorf("adjust stock for product %d: %w", change.ProductID, order.ErrNotFound)
	}
	nextStock, err := checkedOrderStockAfter(state.stock, change.Delta)
	if err != nil {
		return err
	}
	delta, err := orderStockDeltaToInt32(change.Delta)
	if err != nil {
		return err
	}

	result, err := tx.q.Product.WithContext(ctx).
		Where(
			tx.q.Product.ID.Eq(change.ProductID),
			tx.q.Product.Version.Eq(state.version),
			tx.q.Product.Stock.Eq(state.stock),
		).
		UpdateSimple(
			tx.q.Product.Stock.Value(nextStock),
			tx.q.Product.Version.Value(state.version+1),
			tx.q.Product.UpdatedAt.Value(time.Now().UTC()),
		)
	if err != nil {
		return fmt.Errorf("adjust stock for product %d: %w", change.ProductID, err)
	}
	if result.RowsAffected != 1 {
		return order.ErrConflict
	}

	state.stock = nextStock
	state.version++
	tx.pendingLedger = append(tx.pendingLedger, pendingStockAdjustment{
		productID:   change.ProductID,
		delta:       delta,
		stockAfter:  nextStock,
		reason:      reason,
		actorUserID: change.ActorUserID,
	})
	return nil
}

func (tx *orderStoreTx) CreateOrder(ctx context.Context, item *order.Order) error {
	if item == nil {
		return errors.New("create order: order is required")
	}

	row := &model.Order{
		Number:      item.Number,
		UserID:      item.UserID,
		Status:      string(item.Status),
		TotalAmount: item.Total.Amount,
		Currency:    item.Total.Currency,
		Version:     item.Version,
		CreatedAt:   item.CreatedAt.UTC(),
		UpdatedAt:   item.UpdatedAt.UTC(),
	}
	if err := tx.q.Order.WithContext(ctx).Create(row); err != nil {
		return mapOrderWriteError("create order", err)
	}
	item.ID = row.ID

	for i := range item.Items {
		orderItem := &model.OrderItem{
			OrderID:         row.ID,
			ProductID:       item.Items[i].ProductID,
			ProductSKU:      item.Items[i].SKU,
			ProductName:     item.Items[i].Name,
			UnitPriceAmount: item.Items[i].UnitPrice.Amount,
			Currency:        item.Items[i].UnitPrice.Currency,
			Quantity:        item.Items[i].Quantity,
			SubtotalAmount:  item.Items[i].Subtotal.Amount,
			CreatedAt:       item.CreatedAt.UTC(),
		}
		if err := tx.q.OrderItem.WithContext(ctx).Create(orderItem); err != nil {
			return fmt.Errorf("create order: insert item: %w", err)
		}
		item.Items[i].ID = orderItem.ID
	}

	if err := tx.flushPendingLedger(ctx, row.ID, item.CreatedAt); err != nil {
		return err
	}
	return nil
}

func (tx *orderStoreTx) CompleteIdempotency(ctx context.Context, completion order.IdempotencyCompletion) error {
	result, err := tx.q.IdempotencyKey.WithContext(ctx).
		Where(
			tx.q.IdempotencyKey.UserID.Eq(completion.UserID),
			tx.q.IdempotencyKey.Operation.Eq(completion.Operation),
			tx.q.IdempotencyKey.IdempotencyKey.Eq(completion.Key),
			tx.q.IdempotencyKey.OrderID.IsNull(),
		).
		UpdateSimple(
			tx.q.IdempotencyKey.OrderID.Value(completion.OrderID),
			tx.q.IdempotencyKey.UpdatedAt.Value(completion.UpdatedAt.UTC()),
		)
	if err != nil {
		return fmt.Errorf("complete idempotency: %w", err)
	}
	if result.RowsAffected != 1 {
		return order.ErrConflict
	}
	return nil
}

func (tx *orderStoreTx) LockOrder(ctx context.Context, id uint64) (*order.Order, error) {
	row, err := tx.q.Order.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(tx.q.Order.ID.Eq(id)).
		First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock order: %w", err)
	}

	itemRows, err := tx.q.OrderItem.WithContext(ctx).
		Where(tx.q.OrderItem.OrderID.Eq(id)).
		Order(tx.q.OrderItem.ID.Asc()).
		Find()
	if err != nil {
		return nil, fmt.Errorf("lock order items: %w", err)
	}
	items := make([]order.Item, 0, len(itemRows))
	for _, itemRow := range itemRows {
		items = append(items, modelToOrderItem(itemRow))
	}

	locked := order.Order{
		ID:        row.ID,
		UserID:    row.UserID,
		Number:    row.Number,
		Status:    order.Status(row.Status),
		Total:     order.Money{Amount: row.TotalAmount, Currency: row.Currency},
		Items:     items,
		Version:   row.Version,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
	tx.lockedOrder = &locked
	return clonePersistedOrder(&locked), nil
}

func (tx *orderStoreTx) UpdateStatus(ctx context.Context, item *order.Order, expectedVersion uint64) error {
	if item == nil {
		return errors.New("update order status: order is required")
	}
	result, err := tx.q.Order.WithContext(ctx).
		Where(tx.q.Order.ID.Eq(item.ID), tx.q.Order.Version.Eq(expectedVersion)).
		UpdateSimple(
			tx.q.Order.Status.Value(string(item.Status)),
			tx.q.Order.Version.Value(item.Version),
			tx.q.Order.UpdatedAt.Value(item.UpdatedAt.UTC()),
		)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	if result.RowsAffected == 0 {
		exists, err := tx.orderExists(ctx, item.ID)
		if err != nil {
			return fmt.Errorf("update order status: inspect update miss: %w", err)
		}
		if exists {
			return order.ErrConflict
		}
		return order.ErrNotFound
	}
	return tx.flushPendingLedger(ctx, item.ID, item.UpdatedAt)
}

func (tx *orderStoreTx) orderExists(ctx context.Context, orderID uint64) (bool, error) {
	count, err := tx.q.Order.WithContext(ctx).Where(tx.q.Order.ID.Eq(orderID)).Count()
	return count > 0, err
}

func (tx *orderStoreTx) ensureCancellationProductsLocked(ctx context.Context) error {
	if tx.cancellationSet {
		return nil
	}
	tx.cancellationSet = true
	if tx.lockedOrder == nil || len(tx.lockedOrder.Items) == 0 {
		return nil
	}
	productIDs := make([]uint64, 0, len(tx.lockedOrder.Items))
	for _, item := range tx.lockedOrder.Items {
		productIDs = append(productIDs, item.ProductID)
	}
	_, err := tx.LockProducts(ctx, productIDs)
	return err
}

func (tx *orderStoreTx) flushPendingLedger(ctx context.Context, orderID uint64, when time.Time) error {
	if tx.ledgerFlushed || len(tx.pendingLedger) == 0 {
		return nil
	}
	// Ledger rows are emitted only after the order row exists so the optional
	// foreign key always points at the committed business record that caused stock
	// movement, rather than leaving checkout deductions orphaned from the order.
	for _, entry := range tx.pendingLedger {
		row := &model.StockAdjustment{
			ProductID:   entry.productID,
			Delta:       entry.delta,
			StockAfter:  entry.stockAfter,
			Reason:      entry.reason,
			ActorUserID: entry.actorUserID,
			OrderID:     &orderID,
			CreatedAt:   when.UTC(),
		}
		if err := tx.q.StockAdjustment.WithContext(ctx).Create(row); err != nil {
			return fmt.Errorf("persist stock ledger: %w", err)
		}
	}
	tx.ledgerFlushed = true
	return nil
}

func clonePersistedOrder(item *order.Order) *order.Order {
	if item == nil {
		return nil
	}
	cloned := *item
	cloned.Items = append([]order.Item(nil), item.Items...)
	return &cloned
}

func modelToOrder(row *model.Order, items []order.Item) order.Order {
	clonedItems := append([]order.Item(nil), items...)
	return order.Order{
		ID:        row.ID,
		UserID:    row.UserID,
		Number:    row.Number,
		Status:    order.Status(row.Status),
		Total:     order.Money{Amount: row.TotalAmount, Currency: row.Currency},
		Items:     clonedItems,
		Version:   row.Version,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	}
}

func modelToOrderItem(row *model.OrderItem) order.Item {
	return order.Item{
		ID:        row.ID,
		ProductID: row.ProductID,
		SKU:       row.ProductSKU,
		Name:      row.ProductName,
		UnitPrice: order.Money{Amount: row.UnitPriceAmount, Currency: row.Currency},
		Subtotal:  order.Money{Amount: row.SubtotalAmount, Currency: row.Currency},
		Quantity:  row.Quantity,
	}
}

func applyOrderFilters(dao query.IOrderDo, q *query.Query, filter order.ListFilter) query.IOrderDo {
	if filter.UserID != 0 {
		dao = dao.Where(q.Order.UserID.Eq(filter.UserID))
	}
	if len(filter.Statuses) != 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			statuses = append(statuses, string(status))
		}
		dao = dao.Where(q.Order.Status.In(statuses...))
	}
	if filter.CreatedFrom != nil {
		dao = dao.Where(q.Order.CreatedAt.Gte(filter.CreatedFrom.UTC()))
	}
	if filter.CreatedTo != nil {
		dao = dao.Where(q.Order.CreatedAt.Lte(filter.CreatedTo.UTC()))
	}
	if filter.MinTotalAmount != nil {
		dao = dao.Where(q.Order.TotalAmount.Gte(*filter.MinTotalAmount))
	}
	if filter.MaxTotalAmount != nil {
		dao = dao.Where(q.Order.TotalAmount.Lte(*filter.MaxTotalAmount))
	}
	return dao
}

func orderSortExpr(q *query.Query, sort string, descending bool) (field.Expr, error) {
	var expr field.Expr
	switch sort {
	case "id":
		expr = q.Order.ID
	case "number":
		expr = q.Order.Number
	case "status":
		expr = q.Order.Status
	case "total":
		expr = q.Order.TotalAmount
	case "updated_at":
		expr = q.Order.UpdatedAt
	case "", "created_at":
		expr = q.Order.CreatedAt
	default:
		return nil, fmt.Errorf("list orders: %w", order.ErrInvalidFilter)
	}
	if descending {
		return expr.Desc(), nil
	}
	return expr.Asc(), nil
}

func checkedOrderStockAfter(current uint32, delta int64) (uint32, error) {
	next := int64(current) + delta
	if next < 0 {
		return 0, order.ErrInsufficientStock
	}
	if next > math.MaxUint32 {
		return 0, order.ErrConflict
	}
	return uint32(next), nil
}

func orderStockDeltaToInt32(delta int64) (int32, error) {
	if delta == 0 || delta < math.MinInt32 || delta > math.MaxInt32 {
		return 0, order.ErrConflict
	}
	return int32(delta), nil
}

func normalizedSortedIDs(ids []uint64) []uint64 {
	if len(ids) == 0 {
		return nil
	}
	sorted := append([]uint64(nil), ids...)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

func waitOrderRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(15+attempt*15)*time.Millisecond + time.Duration(time.Now().UnixNano()%int64(10*time.Millisecond))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableOrderTxError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == mysqlDeadlockCode || mysqlErr.Number == mysqlLockWaitTimeoutCode
}

func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysqldriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func mapOrderWriteError(operation string, err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		switch mysqlDuplicateKeyName(mysqlErr.Message) {
		case "uk_orders_number":
			return fmt.Errorf("%s: %w", operation, order.ErrConflict)
		default:
			return &redactedStorageError{operation: operation + ": duplicate database constraint", cause: err}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
