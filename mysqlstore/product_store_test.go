//go:build integration

package mysqlstore

import (
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clean-arch-gin/mysqlstore/model"
	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/product"
	"gorm.io/gorm"
)

func TestProductStore(t *testing.T) {
	store := mustProductStore(t, newTestDB(t))
	t.Run("CreateLookupListAndUpdate", func(t *testing.T) {
		testProductStoreCreateLookupListAndUpdate(t, store)
	})
	t.Run("ListStableSorting", func(t *testing.T) {
		testProductStoreListStableSorting(t, store)
	})
	t.Run("UpdateAndAdjustStockErrorMapping", func(t *testing.T) {
		testProductStoreUpdateAndAdjustStockErrorMapping(t, store)
	})
	t.Run("ConcurrentStockAdjustment", func(t *testing.T) {
		testProductStoreConcurrentStockAdjustment(t, store)
	})
}

func testProductStoreCreateLookupListAndUpdate(t *testing.T, store *ProductStore) {
	now := time.Date(2026, 7, 14, 1, 2, 3, 456000000, time.UTC)

	active := &product.Product{
		SKU:         "SKU-ACTIVE",
		Name:        "Alpha",
		Description: "Public product",
		Price:       product.Money{Amount: 1999, Currency: "CNY"},
		Stock:       7,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Create(t.Context(), active); err != nil {
		t.Fatalf("Create(active) error = %v", err)
	}
	if active.ID == 0 {
		t.Fatal("Create(active) did not assign ID")
	}

	draft := &product.Product{
		SKU:         "SKU-DRAFT",
		Name:        "Beta",
		Description: "",
		Price:       product.Money{Amount: 2999, Currency: "USD"},
		Stock:       3,
		Status:      product.StatusDraft,
		Version:     1,
		CreatedAt:   now.Add(time.Second),
		UpdatedAt:   now.Add(time.Second),
	}
	if err := store.Create(t.Context(), draft); err != nil {
		t.Fatalf("Create(draft) error = %v", err)
	}

	inactive := &product.Product{
		SKU:         "SKU-INACTIVE",
		Name:        "Gamma",
		Description: "Hidden",
		Price:       product.Money{Amount: 4999, Currency: "EUR"},
		Stock:       1,
		Status:      product.StatusInactive,
		Version:     1,
		CreatedAt:   now.Add(2 * time.Second),
		UpdatedAt:   now.Add(2 * time.Second),
	}
	if err := store.Create(t.Context(), inactive); err != nil {
		t.Fatalf("Create(inactive) error = %v", err)
	}

	duplicate := &product.Product{
		SKU:         active.SKU,
		Name:        "Duplicate",
		Description: "should fail",
		Price:       product.Money{Amount: 1000, Currency: "CNY"},
		Stock:       0,
		Status:      product.StatusDraft,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Create(t.Context(), duplicate); !errors.Is(err, product.ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want ErrConflict", err)
	} else if strings.Contains(err.Error(), active.SKU) {
		t.Fatalf("duplicate Create() leaked SKU in error: %v", err)
	}

	gotActive, err := store.ByID(t.Context(), active.ID, true)
	if err != nil {
		t.Fatalf("ByID(active, public) error = %v", err)
	}
	if gotActive.Status != product.StatusActive || gotActive.Stock != active.Stock {
		t.Fatalf("ByID(active, public) = %+v", gotActive)
	}
	if _, err := store.ByID(t.Context(), draft.ID, true); !errors.Is(err, product.ErrNotFound) {
		t.Fatalf("ByID(draft, public) error = %v, want ErrNotFound", err)
	}

	gotDraft, err := store.ByID(t.Context(), draft.ID, false)
	if err != nil {
		t.Fatalf("ByID(draft, admin) error = %v", err)
	}
	if gotDraft.Status != product.StatusDraft {
		t.Fatalf("ByID(draft, admin) = %+v", gotDraft)
	}

	publicPage, err := store.List(t.Context(), product.ListFilter{Sort: "created_at", Limit: 10}, true)
	if err != nil {
		t.Fatalf("List(public) error = %v", err)
	}
	if publicPage.Total != 1 || len(publicPage.Items) != 1 || publicPage.Items[0].ID != active.ID {
		t.Fatalf("List(public) = %+v", publicPage)
	}

	adminPage, err := store.List(t.Context(), product.ListFilter{Sort: "created_at", Limit: 10}, false)
	if err != nil {
		t.Fatalf("List(admin) error = %v", err)
	}
	if adminPage.Total != 3 || len(adminPage.Items) != 3 {
		t.Fatalf("List(admin) = %+v", adminPage)
	}

	draftPage, err := store.List(t.Context(), product.ListFilter{
		Sort:   "created_at",
		Limit:  10,
		Status: product.StatusDraft,
	}, false)
	if err != nil {
		t.Fatalf("List(draft filter) error = %v", err)
	}
	if draftPage.Total != 1 || len(draftPage.Items) != 1 || draftPage.Items[0].ID != draft.ID {
		t.Fatalf("List(draft filter) = %+v", draftPage)
	}

	publicDraftPage, err := store.List(t.Context(), product.ListFilter{
		Sort:   "created_at",
		Limit:  10,
		Status: product.StatusDraft,
	}, true)
	if err != nil {
		t.Fatalf("List(public draft filter) error = %v", err)
	}
	if publicDraftPage.Total != 1 || len(publicDraftPage.Items) != 1 || publicDraftPage.Items[0].ID != active.ID {
		t.Fatalf("List(public draft filter) = %+v", publicDraftPage)
	}

	publicInactivePage, err := store.List(t.Context(), product.ListFilter{
		Sort:   "created_at",
		Limit:  10,
		Status: product.StatusInactive,
	}, true)
	if err != nil {
		t.Fatalf("List(public inactive filter) error = %v", err)
	}
	if publicInactivePage.Total != 1 || len(publicInactivePage.Items) != 1 || publicInactivePage.Items[0].ID != active.ID {
		t.Fatalf("List(public inactive filter) = %+v", publicInactivePage)
	}

	update := *gotActive
	update.Name = "Alpha Updated"
	update.Description = "New description"
	update.Price = product.Money{Amount: 2099, Currency: "CNY"}
	update.Stock = 999
	update.Version++
	update.UpdatedAt = now.Add(10 * time.Minute)
	if err := store.Update(t.Context(), &update, gotActive.Version); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded, err := store.ByID(t.Context(), active.ID, false)
	if err != nil {
		t.Fatalf("ByID(reloaded) error = %v", err)
	}
	if reloaded.Name != "Alpha Updated" || reloaded.Description != "New description" || reloaded.Price.Amount != 2099 || reloaded.Version != 2 {
		t.Fatalf("reloaded product = %+v", reloaded)
	}
	if reloaded.Stock != active.Stock {
		t.Fatalf("reloaded stock = %d, want persisted %d", reloaded.Stock, active.Stock)
	}
}

func testProductStoreListStableSorting(t *testing.T, store *ProductStore) {
	now := time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC)

	first := createStoredProduct(t, store, product.Product{
		SKU:         "SORT-001",
		Name:        "Shared",
		Description: "first",
		Price:       product.Money{Amount: 1500, Currency: "CNY"},
		Stock:       1,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	second := createStoredProduct(t, store, product.Product{
		SKU:         "SORT-002",
		Name:        "Shared",
		Description: "second",
		Price:       product.Money{Amount: 1500, Currency: "CNY"},
		Stock:       1,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   now.Add(time.Second),
		UpdatedAt:   now.Add(time.Second),
	})
	third := createStoredProduct(t, store, product.Product{
		SKU:         "SORT-003",
		Name:        "Shared",
		Description: "third",
		Price:       product.Money{Amount: 2500, Currency: "CNY"},
		Stock:       1,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   now.Add(2 * time.Second),
		UpdatedAt:   now.Add(2 * time.Second),
	})

	page, err := store.List(t.Context(), product.ListFilter{Sort: "price", Descending: true, Limit: 10}, true)
	if err != nil {
		t.Fatalf("List(price desc) error = %v", err)
	}
	if len(page.Items) < 4 {
		t.Fatalf("List(price desc) returned %d items, want at least 4", len(page.Items))
	}
	positions := map[uint64]int{}
	for index, item := range page.Items {
		positions[item.ID] = index
	}
	if positions[third.ID] > positions[first.ID] || positions[third.ID] > positions[second.ID] {
		t.Fatalf("higher price product order = %v, want %d before %d and %d", productIDs(page.Items), third.ID, first.ID, second.ID)
	}
	if positions[first.ID] > positions[second.ID] {
		t.Fatalf("same-price tie break order = %v, want %d before %d", productIDs(page.Items), first.ID, second.ID)
	}
	for _, item := range page.Items {
		if item.Status != product.StatusActive {
			t.Fatalf("List(price desc) included non-active product: %+v", item)
		}
	}
}

func testProductStoreUpdateAndAdjustStockErrorMapping(t *testing.T, store *ProductStore) {
	actorID := createActorUser(t, store.db, "stock-actor@example.com")
	base := createStoredProduct(t, store, product.Product{
		SKU:         "STOCK-BASE",
		Name:        "Stock Base",
		Description: "base",
		Price:       product.Money{Amount: 5000, Currency: "USD"},
		Stock:       2,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC),
	})

	stale := *base
	stale.Name = "Stale"
	stale.Version++
	stale.UpdatedAt = stale.UpdatedAt.Add(time.Minute)
	if err := store.Update(t.Context(), &stale, 99); !errors.Is(err, product.ErrConflict) {
		t.Fatalf("stale Update() error = %v, want ErrConflict", err)
	}

	missing := *base
	missing.ID = math.MaxUint64
	missing.Version = 2
	if err := store.Update(t.Context(), &missing, 1); !errors.Is(err, product.ErrNotFound) {
		t.Fatalf("missing Update() error = %v, want ErrNotFound", err)
	}

	adjusted, err := store.AdjustStock(t.Context(), product.AdjustmentInput{
		ProductID: base.ID,
		Delta:     -1,
		Reason:    "sale",
		Version:   1,
		Actor:     product.Actor{UserID: actorID},
		UpdatedAt: time.Date(2026, 7, 14, 3, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AdjustStock() error = %v", err)
	}
	if adjusted.Stock != 1 || adjusted.Version != 2 {
		t.Fatalf("AdjustStock() = %+v", adjusted)
	}
	assertStockAdjustmentLedger(t, store.db, base.ID, actorID, -1, 1, "sale")

	ledgerBeforeConflict := countStockAdjustments(t, store.db, base.ID)
	if _, err := store.AdjustStock(t.Context(), product.AdjustmentInput{
		ProductID: base.ID,
		Delta:     -1,
		Reason:    "stale-version",
		Version:   1,
		Actor:     product.Actor{UserID: actorID},
		UpdatedAt: time.Date(2026, 7, 14, 3, 1, 30, 0, time.UTC),
	}); !errors.Is(err, product.ErrConflict) {
		t.Fatalf("stale version AdjustStock() error = %v, want ErrConflict", err)
	}
	ledgerAfterConflict := countStockAdjustments(t, store.db, base.ID)
	if ledgerAfterConflict != ledgerBeforeConflict {
		t.Fatalf("stale version AdjustStock() inserted ledger row: before=%d after=%d", ledgerBeforeConflict, ledgerAfterConflict)
	}

	if _, err := store.AdjustStock(t.Context(), product.AdjustmentInput{
		ProductID: base.ID,
		Delta:     -2,
		Reason:    "oversell",
		Version:   adjusted.Version,
		Actor:     product.Actor{UserID: actorID},
		UpdatedAt: time.Date(2026, 7, 14, 3, 2, 0, 0, time.UTC),
	}); !errors.Is(err, product.ErrInsufficientStock) {
		t.Fatalf("oversell AdjustStock() error = %v, want ErrInsufficientStock", err)
	}

	if _, err := store.AdjustStock(t.Context(), product.AdjustmentInput{
		ProductID: base.ID,
		Delta:     int64(math.MaxInt32) + 1,
		Reason:    "overflow-delta",
		Version:   adjusted.Version,
		Actor:     product.Actor{UserID: actorID},
		UpdatedAt: time.Date(2026, 7, 14, 3, 3, 0, 0, time.UTC),
	}); !errors.Is(err, product.ErrInvalidStockAdjustment) {
		t.Fatalf("overflow delta AdjustStock() error = %v, want ErrInvalidStockAdjustment", err)
	}

	overflow := createStoredProduct(t, store, product.Product{
		SKU:         "STOCK-MAX",
		Name:        "Maxed",
		Description: "max",
		Price:       product.Money{Amount: 100, Currency: "USD"},
		Stock:       math.MaxUint32,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   time.Date(2026, 7, 14, 3, 4, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 7, 14, 3, 4, 0, 0, time.UTC),
	})
	if _, err := store.AdjustStock(t.Context(), product.AdjustmentInput{
		ProductID: overflow.ID,
		Delta:     1,
		Reason:    "restock",
		Version:   1,
		Actor:     product.Actor{UserID: actorID},
		UpdatedAt: time.Date(2026, 7, 14, 3, 5, 0, 0, time.UTC),
	}); !errors.Is(err, product.ErrInvalidStock) {
		t.Fatalf("overflow stock AdjustStock() error = %v, want ErrInvalidStock", err)
	}
}

func testProductStoreConcurrentStockAdjustment(t *testing.T, store *ProductStore) {
	actorID := createActorUser(t, store.db, "concurrent-actor@example.com")
	item := createStoredProduct(t, store, product.Product{
		SKU:         "CONCURRENT-001",
		Name:        "Concurrent",
		Description: "single unit",
		Price:       product.Money{Amount: 1200, Currency: "CNY"},
		Stock:       1,
		Status:      product.StatusActive,
		Version:     1,
		CreatedAt:   time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 7, 14, 4, 0, 0, 0, time.UTC),
	})

	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(2)
	var workers sync.WaitGroup
	workers.Add(2)
	var successes atomic.Int64
	errs := make([]error, 2)
	for index := range 2 {
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			_, errs[index] = store.AdjustStock(t.Context(), product.AdjustmentInput{
				ProductID: item.ID,
				Delta:     -1,
				Reason:    "sale",
				Version:   1,
				Actor:     product.Actor{UserID: actorID},
				UpdatedAt: time.Date(2026, 7, 14, 4, 1, 0, 0, time.UTC),
			})
			if errs[index] == nil {
				successes.Add(1)
			}
		}()
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
		if !errors.Is(err, product.ErrInsufficientStock) && !errors.Is(err, product.ErrConflict) {
			t.Fatalf("concurrent AdjustStock() error = %v", err)
		}
		failures++
	}
	if failures != 1 {
		t.Fatalf("failures = %d, errs = %v", failures, errs)
	}

	got, err := store.ByID(t.Context(), item.ID, false)
	if err != nil {
		t.Fatalf("ByID() after concurrent adjustment error = %v", err)
	}
	if got.Stock != 0 || got.Version != 2 {
		t.Fatalf("post-concurrency product = %+v", got)
	}

	q := query.Use(store.db)
	count, err := q.StockAdjustment.WithContext(t.Context()).Where(q.StockAdjustment.ProductID.Eq(item.ID)).Count()
	if err != nil {
		t.Fatalf("count stock adjustments: %v", err)
	}
	if count != 1 {
		t.Fatalf("stock adjustment rows = %d, want 1", count)
	}
}

func mustProductStore(t *testing.T, db *gorm.DB) *ProductStore {
	t.Helper()
	store, err := NewProductStore(db)
	if err != nil {
		t.Fatalf("NewProductStore() error = %v", err)
	}
	return store
}

func createStoredProduct(t *testing.T, store *ProductStore, item product.Product) *product.Product {
	t.Helper()
	created := item
	if err := store.Create(t.Context(), &created); err != nil {
		t.Fatalf("Create(%q) error = %v", item.SKU, err)
	}
	return &created
}

func createActorUser(t *testing.T, db *gorm.DB, email string) uint64 {
	t.Helper()
	now := time.Now().UTC()
	row := &model.User{
		Email:        email,
		DisplayName:  "Stock Actor",
		PasswordHash: "encoded",
		Status:       "active",
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := query.Use(db).User.WithContext(t.Context()).Create(row); err != nil {
		t.Fatalf("create actor user: %v", err)
	}
	return row.ID
}

func assertStockAdjustmentLedger(t *testing.T, db *gorm.DB, productID, actorID uint64, delta int32, stockAfter uint32, reason string) {
	t.Helper()
	q := query.Use(db)
	rows, err := q.StockAdjustment.WithContext(t.Context()).
		Where(q.StockAdjustment.ProductID.Eq(productID)).
		Order(q.StockAdjustment.ID.Asc()).
		Find()
	if err != nil {
		t.Fatalf("load stock adjustments: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("stock adjustments = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.ActorUserID != actorID || got.Delta != delta || got.StockAfter != stockAfter || got.Reason != reason {
		t.Fatalf("stock adjustment = %+v", got)
	}
}

func countStockAdjustments(t *testing.T, db *gorm.DB, productID uint64) int64 {
	t.Helper()
	q := query.Use(db)
	count, err := q.StockAdjustment.WithContext(t.Context()).
		Where(q.StockAdjustment.ProductID.Eq(productID)).
		Count()
	if err != nil {
		t.Fatalf("count stock adjustments: %v", err)
	}
	return count
}

func productIDs(items []product.Product) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
