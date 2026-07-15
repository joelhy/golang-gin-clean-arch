//go:build integration

package mysqlstore

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clean-arch-gin/mysqlstore/model"
	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/user"

	"gorm.io/gorm"
)

func TestOrderStore(t *testing.T) {
	t.Run("CheckoutPersistsServerSnapshotAndReplay", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-T9-0001"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-checkout@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)

		first := createStoredProduct(t, store, product.Product{
			SKU:         "ORD-001",
			Name:        "Alpha",
			Description: "first",
			Price:       product.Money{Amount: 300, Currency: "CNY"},
			Stock:       5,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		second := createStoredProduct(t, store, product.Product{
			SKU:         "ORD-002",
			Name:        "Beta",
			Description: "second",
			Price:       product.Money{Amount: 500, Currency: "CNY"},
			Stock:       3,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now.Add(time.Second),
			UpdatedAt:   now.Add(time.Second),
		})

		created, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "checkout-snapshot",
			Items: []order.RequestedItem{
				{ProductID: second.ID, Quantity: 1},
				{ProductID: first.ID, Quantity: 2},
			},
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if created.Number != "ORD-T9-0001" || created.Total.Amount != 1100 || created.Status != order.StatusPending {
			t.Fatalf("Create() order = %+v", created)
		}
		if got := []uint64{created.Items[0].ProductID, created.Items[1].ProductID}; !slices.Equal(got, []uint64{first.ID, second.ID}) {
			t.Fatalf("Create() item product order = %v", got)
		}

		// A later catalog price change must not alter the already accepted order snapshot.
		reloadedProduct, err := store.ByID(t.Context(), first.ID, false)
		if err != nil {
			t.Fatalf("ByID(product after checkout) error = %v", err)
		}
		updated := *reloadedProduct
		updated.Price.Amount = 999
		updated.Version++
		updated.UpdatedAt = now.Add(5 * time.Minute)
		if err := store.Update(t.Context(), &updated, reloadedProduct.Version); err != nil {
			t.Fatalf("Update(product) error = %v", err)
		}

		replayed, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "checkout-snapshot",
			Items: []order.RequestedItem{
				{ProductID: first.ID, Quantity: 2},
				{ProductID: second.ID, Quantity: 1},
			},
		})
		if err != nil {
			t.Fatalf("Create(replay) error = %v", err)
		}
		if replayed.ID != created.ID || replayed.Items[0].UnitPrice.Amount != 300 || replayed.Total.Amount != 1100 {
			t.Fatalf("Create(replay) order = %+v", replayed)
		}

		assertOrderRows(t, db, created.ID, []int64{600, 500})
		assertStocks(t, db, map[uint64]uint32{first.ID: 3, second.ID: 2})
		assertIdempotencyRow(t, db, customer.ID, order.OperationCheckout, "checkout-snapshot", created.ID)
	})

	t.Run("UniqueOrderNumberConflict", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-DUP-1", "ORD-DUP-1"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-dup@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 8, 30, 0, 0, time.UTC)

		item := createStoredProduct(t, store, product.Product{
			SKU:         "ORD-DUP",
			Name:        "Unique Number",
			Description: "conflict",
			Price:       product.Money{Amount: 200, Currency: "CNY"},
			Stock:       5,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		firstOrder, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "unique-number-1",
			Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("Create(first) error = %v", err)
		}
		if _, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "unique-number-2",
			Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 1}},
		}); !errors.Is(err, order.ErrConflict) {
			t.Fatalf("Create(second) error = %v, want ErrConflict", err)
		}

		assertOrderCount(t, db, 1)
		assertMissingIdempotencyRow(t, db, customer.ID, order.OperationCheckout, "unique-number-2")
		assertStocks(t, db, map[uint64]uint32{item.ID: 4})
		if firstOrder.Number != "ORD-DUP-1" {
			t.Fatalf("Create(first) number = %q", firstOrder.Number)
		}
	})

	t.Run("InsufficientStockRollsBack", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-ROLLBACK-1"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-rollback@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)

		item := createStoredProduct(t, store, product.Product{
			SKU:         "ROLLBACK-1",
			Name:        "Limited",
			Description: "single unit",
			Price:       product.Money{Amount: 100, Currency: "CNY"},
			Stock:       1,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		if _, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "rollback",
			Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 2}},
		}); !errors.Is(err, order.ErrInsufficientStock) {
			t.Fatalf("Create() error = %v, want ErrInsufficientStock", err)
		}

		assertOrderCount(t, db, 0)
		assertStocks(t, db, map[uint64]uint32{item.ID: 1})
		assertStockAdjustmentCount(t, db, item.ID, 0)
		assertMissingIdempotencyRow(t, db, customer.ID, order.OperationCheckout, "rollback")
	})

	t.Run("DifferentPayloadConflict", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-CONFLICT-1"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-conflict@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 9, 15, 0, 0, time.UTC)

		item := createStoredProduct(t, store, product.Product{
			SKU:         "CONFLICT-1",
			Name:        "Conflict",
			Description: "conflict",
			Price:       product.Money{Amount: 150, Currency: "CNY"},
			Stock:       5,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		if _, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "same-key",
			Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 1}},
		}); err != nil {
			t.Fatalf("Create(first) error = %v", err)
		}
		if _, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "same-key",
			Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 2}},
		}); !errors.Is(err, order.ErrConflict) {
			t.Fatalf("Create(second) error = %v, want ErrConflict", err)
		}

		assertOrderCount(t, db, 1)
		assertStocks(t, db, map[uint64]uint32{item.ID: 4})
	})

	t.Run("ConcurrentSameKeyCreatesOneOrder", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-SAMEKEY-1", "ORD-SAMEKEY-2"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-same-key@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC)

		item := createStoredProduct(t, store, product.Product{
			SKU:         "SAMEKEY-1",
			Name:        "Shared",
			Description: "same key",
			Price:       product.Money{Amount: 400, Currency: "CNY"},
			Stock:       2,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		start := make(chan struct{})
		ready := sync.WaitGroup{}
		ready.Add(2)
		var workers sync.WaitGroup
		workers.Add(2)
		results := make([]*order.Order, 2)
		errs := make([]error, 2)
		for i := range 2 {
			go func(index int) {
				defer workers.Done()
				ready.Done()
				<-start
				results[index], errs[index] = svc.Create(t.Context(), customer.ID, order.CreateInput{
					IdempotencyKey: "concurrent-key",
					Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 1}},
				})
			}(i)
		}
		ready.Wait()
		close(start)
		workers.Wait()

		for _, err := range errs {
			if err != nil {
				t.Fatalf("Create(concurrent same key) errs = %v", errs)
			}
		}
		if results[0].ID == 0 || results[0].ID != results[1].ID {
			t.Fatalf("Create(concurrent same key) ids = %d/%d", results[0].ID, results[1].ID)
		}
		assertOrderCount(t, db, 1)
		assertStocks(t, db, map[uint64]uint32{item.ID: 1})
		assertStockAdjustmentCount(t, db, item.ID, 1)
	})

	t.Run("ConcurrentLastStockOneWinner", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-LAST-1", "ORD-LAST-2"}})
		users := mustUserStore(t, db)
		firstUser := createStoredUser(t, users, "order-last-1@example.com", user.RoleCustomer)
		secondUser := createStoredUser(t, users, "order-last-2@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

		item := createStoredProduct(t, store, product.Product{
			SKU:         "LAST-1",
			Name:        "Last Unit",
			Description: "last stock",
			Price:       product.Money{Amount: 500, Currency: "CNY"},
			Stock:       1,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		start := make(chan struct{})
		ready := sync.WaitGroup{}
		ready.Add(2)
		var workers sync.WaitGroup
		workers.Add(2)
		var successes atomic.Int64
		errs := make([]error, 2)
		userIDs := []uint64{firstUser.ID, secondUser.ID}
		for i := range 2 {
			go func(index int) {
				defer workers.Done()
				ready.Done()
				<-start
				_, errs[index] = svc.Create(t.Context(), userIDs[index], order.CreateInput{
					IdempotencyKey: fmt.Sprintf("last-stock-%d", index+1),
					Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 1}},
				})
				if errs[index] == nil {
					successes.Add(1)
				}
			}(i)
		}
		ready.Wait()
		close(start)
		workers.Wait()

		if successes.Load() != 1 {
			t.Fatalf("successes = %d, errs = %v", successes.Load(), errs)
		}
		failures := 0
		for _, err := range errs {
			if err == nil {
				continue
			}
			if !errors.Is(err, order.ErrInsufficientStock) && !errors.Is(err, order.ErrConflict) {
				t.Fatalf("Create(concurrent last stock) error = %v", err)
			}
			failures++
		}
		if failures != 1 {
			t.Fatalf("failures = %d, errs = %v", failures, errs)
		}

		assertOrderCount(t, db, 1)
		assertStocks(t, db, map[uint64]uint32{item.ID: 0})
		assertStockAdjustmentCount(t, db, item.ID, 1)
	})

	t.Run("CancellationRestoresStock", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-CANCEL-1"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-cancel@example.com", user.RoleCustomer)
		admin := createStoredUser(t, mustUserStore(t, db), "order-cancel-admin@example.com", user.RoleAdmin)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC)

		first := createStoredProduct(t, store, product.Product{
			SKU:         "CANCEL-1",
			Name:        "First",
			Description: "cancel one",
			Price:       product.Money{Amount: 300, Currency: "CNY"},
			Stock:       3,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		second := createStoredProduct(t, store, product.Product{
			SKU:         "CANCEL-2",
			Name:        "Second",
			Description: "cancel two",
			Price:       product.Money{Amount: 200, Currency: "CNY"},
			Stock:       2,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now.Add(time.Second),
			UpdatedAt:   now.Add(time.Second),
		})

		created, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "cancel-order",
			Items: []order.RequestedItem{
				{ProductID: second.ID, Quantity: 1},
				{ProductID: first.ID, Quantity: 2},
			},
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		for _, productID := range []uint64{first.ID, second.ID} {
			reloaded, err := store.ByID(t.Context(), productID, false)
			if err != nil {
				t.Fatalf("ByID(product %d before deactivate) error = %v", productID, err)
			}
			reloaded.Status = product.StatusInactive
			reloaded.Version++
			reloaded.UpdatedAt = now.Add(5 * time.Minute)
			if err := store.Update(t.Context(), reloaded, reloaded.Version-1); err != nil {
				t.Fatalf("Update(product %d to inactive) error = %v", productID, err)
			}
		}

		cancelled, err := svc.Cancel(t.Context(), order.Actor{UserID: admin.ID, Admin: true}, created.ID, created.Version)
		if err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		if cancelled.Status != order.StatusCancelled || cancelled.Version != created.Version+1 {
			t.Fatalf("Cancel() order = %+v", cancelled)
		}

		assertStocks(t, db, map[uint64]uint32{first.ID: 3, second.ID: 2})
		assertStockAdjustmentCount(t, db, first.ID, 2)
		assertStockAdjustmentCount(t, db, second.ID, 2)
		assertLatestStockAdjustmentActor(t, db, first.ID, admin.ID)
		assertLatestStockAdjustmentActor(t, db, second.ID, admin.ID)
		assertOrderStatusVersion(t, db, created.ID, order.StatusCancelled, created.Version+1)
	})

	t.Run("TransitionVersionConflict", func(t *testing.T) {
		db := newTestDB(t)
		svc := newOrderService(t, db, fixedOrderClock{}, &sequenceNumbers{values: []string{"ORD-VERSION-1"}})
		customer := createStoredUser(t, mustUserStore(t, db), "order-version@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		now := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)

		item := createStoredProduct(t, store, product.Product{
			SKU:         "VERSION-1",
			Name:        "Versioned",
			Description: "version conflict",
			Price:       product.Money{Amount: 120, Currency: "CNY"},
			Stock:       3,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		created, err := svc.Create(t.Context(), customer.ID, order.CreateInput{
			IdempotencyKey: "version-conflict",
			Items:          []order.RequestedItem{{ProductID: item.ID, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if _, err := svc.Confirm(t.Context(), created.ID, created.Version+1); !errors.Is(err, order.ErrConflict) {
			t.Fatalf("Confirm() error = %v, want ErrConflict", err)
		}
		assertOrderStatusVersion(t, db, created.ID, order.StatusPending, created.Version)
		assertStocks(t, db, map[uint64]uint32{item.ID: 2})
	})

	t.Run("ListSupportsCompoundFiltersAndDeterministicOrdering", func(t *testing.T) {
		db := newTestDB(t)
		users := mustUserStore(t, db)
		customer := createStoredUser(t, users, "order-list@example.com", user.RoleCustomer)
		store := mustProductStore(t, db)
		reader := mustOrderStore(t, db)
		now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

		first := createStoredProduct(t, store, product.Product{
			SKU:         "LIST-1",
			Name:        "First",
			Description: "list one",
			Price:       product.Money{Amount: 100, Currency: "CNY"},
			Stock:       10,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		second := createStoredProduct(t, store, product.Product{
			SKU:         "LIST-2",
			Name:        "Second",
			Description: "list two",
			Price:       product.Money{Amount: 200, Currency: "CNY"},
			Stock:       10,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now.Add(time.Second),
			UpdatedAt:   now.Add(time.Second),
		})

		seedOrderRow(t, db, seededOrder{
			ID: 100, Number: "ORD-LIST-100", UserID: customer.ID, Status: order.StatusPending, TotalAmount: 300, Currency: "CNY", Version: 1,
			CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute),
			Items: []seededOrderItem{
				{ID: 1002, ProductID: second.ID, SKU: second.SKU, Name: second.Name, UnitPriceAmount: 200, Currency: "CNY", Quantity: 1, SubtotalAmount: 200},
				{ID: 1001, ProductID: first.ID, SKU: first.SKU, Name: first.Name, UnitPriceAmount: 100, Currency: "CNY", Quantity: 1, SubtotalAmount: 100},
			},
		})
		seedOrderRow(t, db, seededOrder{
			ID: 101, Number: "ORD-LIST-101", UserID: customer.ID, Status: order.StatusConfirmed, TotalAmount: 300, Currency: "CNY", Version: 2,
			CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(3 * time.Minute),
			Items: []seededOrderItem{
				{ID: 1011, ProductID: first.ID, SKU: first.SKU, Name: first.Name, UnitPriceAmount: 100, Currency: "CNY", Quantity: 3, SubtotalAmount: 300},
			},
		})
		seedOrderRow(t, db, seededOrder{
			ID: 102, Number: "ORD-LIST-102", UserID: customer.ID, Status: order.StatusCancelled, TotalAmount: 900, Currency: "CNY", Version: 3,
			CreatedAt: now.Add(4 * time.Minute), UpdatedAt: now.Add(4 * time.Minute),
			Items: []seededOrderItem{
				{ID: 1021, ProductID: second.ID, SKU: second.SKU, Name: second.Name, UnitPriceAmount: 200, Currency: "CNY", Quantity: 2, SubtotalAmount: 400},
				{ID: 1022, ProductID: first.ID, SKU: first.SKU, Name: first.Name, UnitPriceAmount: 100, Currency: "CNY", Quantity: 5, SubtotalAmount: 500},
			},
		})

		from := now.Add(time.Minute)
		to := now.Add(5 * time.Minute)
		minTotal := int64(250)
		maxTotal := int64(900)
		page, err := reader.List(t.Context(), order.ListFilter{
			UserID:         customer.ID,
			Statuses:       []order.Status{order.StatusPending, order.StatusConfirmed, order.StatusCancelled},
			CreatedFrom:    &from,
			CreatedTo:      &to,
			MinTotalAmount: &minTotal,
			MaxTotalAmount: &maxTotal,
			Sort:           "total",
			Descending:     true,
			Limit:          10,
		})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if page.Total != 3 || len(page.Items) != 3 {
			t.Fatalf("List() page = %+v", page)
		}
		gotIDs := []uint64{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID}
		if !slices.Equal(gotIDs, []uint64{102, 100, 101}) {
			t.Fatalf("List() ids = %v", gotIDs)
		}
		if gotItemIDs := []uint64{page.Items[1].Items[0].ID, page.Items[1].Items[1].ID}; !slices.Equal(gotItemIDs, []uint64{1001, 1002}) {
			t.Fatalf("List() item ids = %v", gotItemIDs)
		}

		found, err := reader.ByID(t.Context(), 100)
		if err != nil {
			t.Fatalf("ByID() error = %v", err)
		}
		if gotItemIDs := []uint64{found.Items[0].ID, found.Items[1].ID}; !slices.Equal(gotItemIDs, []uint64{1001, 1002}) {
			t.Fatalf("ByID() item ids = %v", gotItemIDs)
		}
		found.Items[0].Name = "mutated"
		reloaded, err := reader.ByID(t.Context(), 100)
		if err != nil {
			t.Fatalf("ByID(reloaded) error = %v", err)
		}
		if reloaded.Items[0].Name == "mutated" {
			t.Fatal("ByID() exposed mutable persistence-backed item data")
		}
	})
}

type fixedOrderClock struct{}

func (fixedOrderClock) Now() time.Time {
	return time.Date(2026, 7, 15, 7, 0, 0, 0, time.UTC)
}

type sequenceNumbers struct {
	mu     sync.Mutex
	values []string
	index  int
}

func (n *sequenceNumbers) New(_ time.Time) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.index >= len(n.values) {
		return "", io.EOF
	}
	value := n.values[n.index]
	n.index++
	return value, nil
}

func newOrderService(t *testing.T, db *gorm.DB, clock order.Clock, numbers order.NumberGenerator) *order.Service {
	t.Helper()
	store := mustOrderStore(t, db)
	svc, err := order.NewService(store, store, clock, numbers)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return svc
}

func mustOrderStore(t *testing.T, db *gorm.DB) *OrderStore {
	t.Helper()
	store, err := NewOrderStore(db)
	if err != nil {
		t.Fatalf("NewOrderStore() error = %v", err)
	}
	return store
}

func assertOrderRows(t *testing.T, db *gorm.DB, orderID uint64, subtotals []int64) {
	t.Helper()
	q := query.Use(db)
	rows, err := q.OrderItem.WithContext(t.Context()).
		Where(q.OrderItem.OrderID.Eq(orderID)).
		Order(q.OrderItem.ID.Asc()).
		Find()
	if err != nil {
		t.Fatalf("load order items: %v", err)
	}
	if len(rows) != len(subtotals) {
		t.Fatalf("order items = %d, want %d", len(rows), len(subtotals))
	}
	for i, want := range subtotals {
		if rows[i].SubtotalAmount != want {
			t.Fatalf("order item %d subtotal = %d, want %d", i, rows[i].SubtotalAmount, want)
		}
	}
}

func assertStocks(t *testing.T, db *gorm.DB, wants map[uint64]uint32) {
	t.Helper()
	q := query.Use(db)
	for productID, want := range wants {
		row, err := q.Product.WithContext(t.Context()).Where(q.Product.ID.Eq(productID)).First()
		if err != nil {
			t.Fatalf("load product %d: %v", productID, err)
		}
		if row.Stock != want {
			t.Fatalf("product %d stock = %d, want %d", productID, row.Stock, want)
		}
	}
}

func assertIdempotencyRow(t *testing.T, db *gorm.DB, userID uint64, op, key string, orderID uint64) {
	t.Helper()
	q := query.Use(db)
	row, err := q.IdempotencyKey.WithContext(t.Context()).
		Where(q.IdempotencyKey.UserID.Eq(userID), q.IdempotencyKey.Operation.Eq(op), q.IdempotencyKey.IdempotencyKey.Eq(key)).
		First()
	if err != nil {
		t.Fatalf("load idempotency row: %v", err)
	}
	if row.OrderID == nil || *row.OrderID != orderID {
		t.Fatalf("idempotency row = %+v, want order_id %d", row, orderID)
	}
	if len(row.RequestHash) != 32 {
		t.Fatalf("idempotency request hash length = %d, want 32", len(row.RequestHash))
	}
}

func assertMissingIdempotencyRow(t *testing.T, db *gorm.DB, userID uint64, op, key string) {
	t.Helper()
	q := query.Use(db)
	count, err := q.IdempotencyKey.WithContext(t.Context()).
		Where(q.IdempotencyKey.UserID.Eq(userID), q.IdempotencyKey.Operation.Eq(op), q.IdempotencyKey.IdempotencyKey.Eq(key)).
		Count()
	if err != nil {
		t.Fatalf("count idempotency rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("idempotency rows = %d, want 0", count)
	}
}

func assertOrderCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	q := query.Use(db)
	count, err := q.Order.WithContext(t.Context()).Count()
	if err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != want {
		t.Fatalf("orders = %d, want %d", count, want)
	}
}

func assertStockAdjustmentCount(t *testing.T, db *gorm.DB, productID uint64, want int64) {
	t.Helper()
	q := query.Use(db)
	count, err := q.StockAdjustment.WithContext(t.Context()).
		Where(q.StockAdjustment.ProductID.Eq(productID)).
		Count()
	if err != nil {
		t.Fatalf("count stock adjustments: %v", err)
	}
	if count != want {
		t.Fatalf("product %d stock adjustments = %d, want %d", productID, count, want)
	}
}

func assertLatestStockAdjustmentActor(t *testing.T, db *gorm.DB, productID uint64, want uint64) {
	t.Helper()
	q := query.Use(db)
	row, err := q.StockAdjustment.WithContext(t.Context()).
		Where(q.StockAdjustment.ProductID.Eq(productID)).
		Order(q.StockAdjustment.ID.Desc()).
		First()
	if err != nil {
		t.Fatalf("load latest stock adjustment for product %d: %v", productID, err)
	}
	if row.ActorUserID != want {
		t.Fatalf("product %d latest stock adjustment actor_user_id = %d, want %d", productID, row.ActorUserID, want)
	}
}

func assertOrderStatusVersion(t *testing.T, db *gorm.DB, orderID uint64, wantStatus order.Status, wantVersion uint64) {
	t.Helper()
	q := query.Use(db)
	row, err := q.Order.WithContext(t.Context()).Where(q.Order.ID.Eq(orderID)).First()
	if err != nil {
		t.Fatalf("load order %d: %v", orderID, err)
	}
	if row.Status != string(wantStatus) || row.Version != wantVersion {
		t.Fatalf("order row = %+v, want status %q version %d", row, wantStatus, wantVersion)
	}
}

type seededOrder struct {
	ID          uint64
	Number      string
	UserID      uint64
	Status      order.Status
	TotalAmount int64
	Currency    string
	Version     uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Items       []seededOrderItem
}

type seededOrderItem struct {
	ID              uint64
	ProductID       uint64
	SKU             string
	Name            string
	UnitPriceAmount int64
	Currency        string
	Quantity        uint32
	SubtotalAmount  int64
}

func seedOrderRow(t *testing.T, db *gorm.DB, seed seededOrder) {
	t.Helper()
	q := query.Use(db)
	orderRow := &model.Order{
		ID:          seed.ID,
		Number:      seed.Number,
		UserID:      seed.UserID,
		Status:      string(seed.Status),
		TotalAmount: seed.TotalAmount,
		Currency:    seed.Currency,
		Version:     seed.Version,
		CreatedAt:   seed.CreatedAt,
		UpdatedAt:   seed.UpdatedAt,
	}
	if err := q.Order.WithContext(t.Context()).Create(orderRow); err != nil {
		t.Fatalf("seed order %d: %v", seed.ID, err)
	}
	for _, item := range seed.Items {
		row := &model.OrderItem{
			ID:              item.ID,
			OrderID:         seed.ID,
			ProductID:       item.ProductID,
			ProductSKU:      item.SKU,
			ProductName:     item.Name,
			UnitPriceAmount: item.UnitPriceAmount,
			Currency:        item.Currency,
			Quantity:        item.Quantity,
			SubtotalAmount:  item.SubtotalAmount,
			CreatedAt:       seed.CreatedAt,
		}
		if err := q.OrderItem.WithContext(t.Context()).Create(row); err != nil {
			t.Fatalf("seed order item %d: %v", item.ID, err)
		}
	}
}
