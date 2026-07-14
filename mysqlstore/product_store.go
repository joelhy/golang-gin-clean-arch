package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"math"

	"clean-arch-gin/mysqlstore/model"
	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/product"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProductStore struct {
	db *gorm.DB
	q  *query.Query
}

var _ product.Store = (*ProductStore)(nil)

func NewProductStore(db *gorm.DB) (*ProductStore, error) {
	if db == nil {
		return nil, errors.New("new MySQL product store: database is required")
	}
	return &ProductStore{db: db, q: query.Use(db)}, nil
}

func (s *ProductStore) Create(ctx context.Context, item *product.Product) error {
	if ctx == nil {
		return errors.New("create product: context is required")
	}
	if item == nil {
		return errors.New("create product: product is required")
	}

	row := productToModel(item)
	if err := s.q.Product.WithContext(ctx).Create(row); err != nil {
		return mapProductWriteError("create product", err)
	}
	*item = modelToProduct(row)
	return nil
}

func (s *ProductStore) ByID(ctx context.Context, id uint64, activeOnly bool) (*product.Product, error) {
	if ctx == nil {
		return nil, errors.New("find product by ID: context is required")
	}

	dao := s.q.Product.WithContext(ctx).Where(s.q.Product.ID.Eq(id))
	if activeOnly {
		dao = dao.Where(s.q.Product.Status.Eq(string(product.StatusActive)))
	}
	row, err := dao.First()
	if err != nil {
		return nil, mapProductLookupError("find product by ID", err)
	}
	got := modelToProduct(row)
	return &got, nil
}

func (s *ProductStore) Update(ctx context.Context, item *product.Product, expectedVersion uint64) error {
	if ctx == nil {
		return errors.New("update product: context is required")
	}
	if item == nil {
		return errors.New("update product: product is required")
	}

	return query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		// Ordinary catalog edits must not move stock. Inventory changes require the
		// dedicated ledger path so product stock and stock_adjustments stay aligned.
		result, err := tx.Product.WithContext(ctx).
			Where(tx.Product.ID.Eq(item.ID), tx.Product.Version.Eq(expectedVersion)).
			UpdateSimple(
				tx.Product.SKU.Value(item.SKU),
				tx.Product.Name.Value(item.Name),
				tx.Product.Description.Value(item.Description),
				tx.Product.PriceAmount.Value(item.Price.Amount),
				tx.Product.Currency.Value(item.Price.Currency),
				tx.Product.Status.Value(string(item.Status)),
				tx.Product.Version.Value(item.Version),
				tx.Product.UpdatedAt.Value(item.UpdatedAt.UTC()),
			)
		if err != nil {
			return mapProductWriteError("update product", err)
		}
		if result.RowsAffected != 0 {
			return nil
		}
		exists, err := productExists(ctx, tx, item.ID)
		if err != nil {
			return fmt.Errorf("update product: inspect update miss: %w", err)
		}
		if exists {
			return product.ErrConflict
		}
		return product.ErrNotFound
	})
}

func (s *ProductStore) AdjustStock(ctx context.Context, input product.AdjustmentInput) (*product.Product, error) {
	if ctx == nil {
		return nil, errors.New("adjust product stock: context is required")
	}
	delta, err := stockDeltaToInt32(input.Delta)
	if err != nil {
		return nil, err
	}

	var updated product.Product
	err = query.Use(s.db.WithContext(ctx)).Transaction(func(tx *query.Query) error {
		// Every stock adjustment locks the product row before reading stock/version so
		// concurrent decrements serialize against the same database state and cannot
		// both spend the last unit or write a mismatched ledger entry.
		row, err := tx.Product.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(tx.Product.ID.Eq(input.ProductID)).
			First()
		if err != nil {
			return mapProductLookupError("adjust product stock: lock product", err)
		}

		stockAfter, err := checkedStockAfter(row.Stock, input.Delta)
		if err != nil {
			return err
		}

		result, err := tx.Product.WithContext(ctx).
			Where(tx.Product.ID.Eq(input.ProductID), tx.Product.Version.Eq(input.Version)).
			UpdateSimple(
				tx.Product.Stock.Value(stockAfter),
				tx.Product.Version.Value(row.Version+1),
				tx.Product.UpdatedAt.Value(input.UpdatedAt.UTC()),
			)
		if err != nil {
			return mapProductWriteError("adjust product stock: update product", err)
		}
		if result.RowsAffected == 0 {
			return product.ErrConflict
		}

		ledger := &model.StockAdjustment{
			ProductID:   input.ProductID,
			Delta:       delta,
			StockAfter:  stockAfter,
			Reason:      input.Reason,
			ActorUserID: input.Actor.UserID,
			CreatedAt:   input.UpdatedAt.UTC(),
		}
		if err := tx.StockAdjustment.WithContext(ctx).Create(ledger); err != nil {
			return fmt.Errorf("adjust product stock: insert ledger: %w", err)
		}

		row.Stock = stockAfter
		row.Version++
		row.UpdatedAt = input.UpdatedAt.UTC()
		updated = modelToProduct(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *ProductStore) List(ctx context.Context, filter product.ListFilter, activeOnly bool) (product.Page, error) {
	if ctx == nil {
		return product.Page{}, errors.New("list products: context is required")
	}
	if err := filter.Validate(); err != nil {
		return product.Page{}, fmt.Errorf("list products: %w", err)
	}

	dao := s.q.Product.WithContext(ctx)
	if activeOnly {
		dao = dao.Where(s.q.Product.Status.Eq(string(product.StatusActive)))
	} else if filter.Status != "" {
		dao = dao.Where(s.q.Product.Status.Eq(string(filter.Status)))
	}

	total, err := dao.Count()
	if err != nil {
		return product.Page{}, fmt.Errorf("list products: count: %w", err)
	}

	order, err := productSortExpr(s.q, filter.Sort, filter.Descending)
	if err != nil {
		return product.Page{}, err
	}
	dao = dao.Order(order)
	if filter.Sort != "id" {
		dao = dao.Order(s.q.Product.ID.Asc())
	}

	rows, err := dao.Offset(filter.Offset).Limit(filter.Limit).Find()
	if err != nil {
		return product.Page{}, fmt.Errorf("list products: query page: %w", err)
	}

	items := make([]product.Product, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelToProduct(row))
	}
	return product.Page{Items: items, Total: uint64(total), Limit: filter.Limit, Offset: filter.Offset}, nil
}

func productExists(ctx context.Context, tx *query.Query, id uint64) (bool, error) {
	count, err := tx.Product.WithContext(ctx).Where(tx.Product.ID.Eq(id)).Count()
	return count > 0, err
}

func checkedStockAfter(current uint32, delta int64) (uint32, error) {
	next := int64(current) + delta
	if next < 0 {
		return 0, product.ErrInsufficientStock
	}
	if next > math.MaxUint32 {
		return 0, product.ErrInvalidStock
	}
	return uint32(next), nil
}

func stockDeltaToInt32(delta int64) (int32, error) {
	if delta == 0 || delta < math.MinInt32 || delta > math.MaxInt32 {
		return 0, product.ErrInvalidStockAdjustment
	}
	return int32(delta), nil
}

func productSortExpr(q *query.Query, sort string, descending bool) (field.Expr, error) {
	var order field.Expr
	switch sort {
	case "id":
		order = q.Product.ID
	case "sku":
		order = q.Product.SKU
	case "name":
		order = q.Product.Name
	case "status":
		order = q.Product.Status
	case "price":
		order = q.Product.PriceAmount
	case "created_at":
		order = q.Product.CreatedAt
	case "updated_at":
		order = q.Product.UpdatedAt
	default:
		return nil, fmt.Errorf("list products: %w", product.ErrInvalidFilter)
	}
	if descending {
		return order.Desc(), nil
	}
	return order.Asc(), nil
}

func modelToProduct(row *model.Product) product.Product {
	if row == nil {
		return product.Product{}
	}
	description := ""
	if row.Description != nil {
		description = *row.Description
	}
	return product.Product{
		ID:          row.ID,
		SKU:         row.SKU,
		Name:        row.Name,
		Description: description,
		Price:       product.Money{Amount: row.PriceAmount, Currency: row.Currency},
		Stock:       row.Stock,
		Status:      product.Status(row.Status),
		Version:     row.Version,
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
}

func productToModel(item *product.Product) *model.Product {
	if item == nil {
		return nil
	}
	return &model.Product{
		ID:          item.ID,
		SKU:         item.SKU,
		Name:        item.Name,
		Description: nullableString(item.Description),
		PriceAmount: item.Price.Amount,
		Currency:    item.Price.Currency,
		Stock:       item.Stock,
		Status:      string(item.Status),
		Version:     item.Version,
		CreatedAt:   item.CreatedAt.UTC(),
		UpdatedAt:   item.UpdatedAt.UTC(),
	}
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func mapProductLookupError(operation string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%s: %w", operation, product.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func mapProductWriteError(operation string, err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		if mysqlDuplicateKeyName(mysqlErr.Message) == "uk_products_sku" {
			return fmt.Errorf("%s: %w", operation, product.ErrConflict)
		}
		return &redactedStorageError{operation: operation + ": duplicate database constraint", cause: err}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
