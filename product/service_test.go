package product

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

var fixedTime = time.Date(2026, 7, 14, 9, 30, 0, 123, time.FixedZone("test", 8*60*60))

type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time { return c.now }

type fakeStore struct {
	createProduct  *Product
	createErr      error
	byIDProduct    *Product
	byIDErr        error
	byIDVisible    bool
	byIDCalls      int
	updateProduct  *Product
	updateExpected uint64
	updateResult   *Product
	updateErr      error
	updateCalls    int
	adjustment     AdjustmentInput
	adjustResult   *Product
	adjustErr      error
	adjustCalls    int
	listFilter     ListFilter
	listVisible    bool
	listPage       Page
	listErr        error
	listCalls      int
}

func (s *fakeStore) Create(_ context.Context, product *Product) error {
	if s.createErr == nil {
		product.ID = 42
	}
	s.createProduct = cloneProduct(product)
	return s.createErr
}

func (s *fakeStore) ByID(_ context.Context, id uint64, activeOnly bool) (*Product, error) {
	s.byIDCalls++
	s.byIDVisible = activeOnly
	if s.byIDProduct == nil {
		return nil, s.byIDErr
	}
	if s.byIDProduct.ID != 0 && s.byIDProduct.ID != id {
		return nil, ErrNotFound
	}
	if activeOnly && s.byIDProduct.Status != StatusActive {
		return nil, ErrNotFound
	}
	return cloneProduct(s.byIDProduct), s.byIDErr
}

func (s *fakeStore) Update(_ context.Context, product *Product, expectedVersion uint64) error {
	s.updateCalls++
	s.updateProduct = cloneProduct(product)
	s.updateExpected = expectedVersion
	if s.updateErr != nil {
		return s.updateErr
	}
	s.updateResult = cloneProduct(product)
	return nil
}

func (s *fakeStore) AdjustStock(_ context.Context, input AdjustmentInput) (*Product, error) {
	s.adjustCalls++
	s.adjustment = input
	if s.adjustErr != nil {
		return nil, s.adjustErr
	}
	return cloneProduct(s.adjustResult), nil
}

func (s *fakeStore) List(_ context.Context, filter ListFilter, activeOnly bool) (Page, error) {
	s.listCalls++
	s.listFilter = filter
	s.listVisible = activeOnly
	return clonePage(s.listPage), s.listErr
}

func newTestService(t *testing.T, store Store, clock Clock) *Service {
	t.Helper()
	service, err := NewService(store, clock)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestNewServiceRejectsNilDependencies(t *testing.T) {
	store := &fakeStore{}
	clock := fakeClock{now: fixedTime}
	tests := []struct {
		name    string
		store   Store
		clock   Clock
		wantErr string
	}{
		{name: "valid", store: store, clock: clock},
		{name: "nil store", clock: clock, wantErr: "store is required"},
		{name: "nil clock", store: store, wantErr: "clock is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewService(tt.store, tt.clock)
			if tt.wantErr == "" {
				if err != nil || got == nil {
					t.Fatalf("NewService() = (%v, %v), want non-nil service and nil error", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewService() error = %v, want substring %q", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("NewService() service = %v, want nil", got)
			}
		})
	}
}

func TestNormalizeSKU(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim and uppercase", input: "  sku-9  ", want: "SKU-9"},
		{name: "required", input: " \t ", wantErr: ErrInvalidSKU},
		{name: "invalid utf8", input: string([]byte{0xff}), wantErr: ErrInvalidSKU},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSKU(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeSKU() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeSKU() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trim", input: "  Widget  ", want: "Widget"},
		{name: "required", input: " \t ", wantErr: ErrInvalidName},
		{name: "invalid utf8", input: string([]byte{0xff}), wantErr: ErrInvalidName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeName() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeMoney(t *testing.T) {
	tests := []struct {
		name    string
		input   Money
		want    Money
		wantErr error
	}{
		{name: "uppercase allowed currency", input: Money{Amount: 1999, Currency: "usd"}, want: Money{Amount: 1999, Currency: "USD"}},
		{name: "amount must be positive", input: Money{Amount: 0, Currency: "USD"}, wantErr: ErrInvalidPrice},
		{name: "currency required", input: Money{Amount: 100, Currency: " \t "}, wantErr: ErrInvalidCurrency},
		{name: "currency limited to documented set", input: Money{Amount: 100, Currency: "aud"}, wantErr: ErrInvalidCurrency},
		{name: "currency invalid utf8", input: Money{Amount: 100, Currency: string([]byte{0xff})}, wantErr: ErrInvalidCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMoney(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizeMoney() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("normalizeMoney() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidationErrorPreservesSentinel(t *testing.T) {
	err := &ValidationError{Field: "price", Message: "must be positive", Err: ErrInvalidPrice}
	if !errors.Is(err, ErrInvalidPrice) {
		t.Fatalf("errors.Is(%v, ErrInvalidPrice) = false", err)
	}
	if got, want := err.Error(), "price must be positive"; got != want {
		t.Fatalf("ValidationError.Error() = %q, want %q", got, want)
	}
}

func TestSystemClockNowUsesWallClock(t *testing.T) {
	before := time.Now()
	got := (SystemClock{}).Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatalf("SystemClock.Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestListFilterValidate(t *testing.T) {
	tests := []struct {
		name    string
		filter  ListFilter
		wantErr error
	}{
		{name: "valid", filter: ListFilter{Sort: "created_at", Limit: 20, Offset: 0}},
		{name: "invalid status", filter: ListFilter{Status: Status("retired"), Sort: "created_at", Limit: 20}, wantErr: ErrInvalidFilter},
		{name: "invalid sort", filter: ListFilter{Sort: "random", Limit: 20}, wantErr: ErrInvalidFilter},
		{name: "limit too small", filter: ListFilter{Sort: "created_at", Limit: 0}, wantErr: ErrInvalidFilter},
		{name: "limit too large", filter: ListFilter{Sort: "created_at", Limit: 101}, wantErr: ErrInvalidFilter},
		{name: "negative offset", filter: ListFilter{Sort: "created_at", Limit: 20, Offset: -1}, wantErr: ErrInvalidFilter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListFilter.Validate() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
		})
	}
}

func TestServiceCreate(t *testing.T) {
	store := &fakeStore{}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	got, err := service.Create(t.Context(), CreateInput{
		SKU:          " sku-9 ",
		Name:         "  Widget  ",
		Description:  " featured  ",
		Price:        Money{Amount: 2599, Currency: "usd"},
		InitialStock: 7,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	want := &Product{
		ID:          42,
		SKU:         "SKU-9",
		Name:        "Widget",
		Description: " featured  ",
		Price:       Money{Amount: 2599, Currency: "USD"},
		Stock:       7,
		Status:      StatusDraft,
		Version:     1,
		CreatedAt:   fixedTime,
		UpdatedAt:   fixedTime,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("Create() product mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want, store.createProduct); diff != "" {
		t.Fatalf("persisted product mismatch (-want +got):\n%s", diff)
	}
}

func TestServiceCreateRejectsNegativeInitialStock(t *testing.T) {
	service := newTestService(t, &fakeStore{}, fakeClock{now: fixedTime})

	_, err := service.Create(t.Context(), CreateInput{
		SKU:          "SKU-1",
		Name:         "Widget",
		Price:        Money{Amount: 100, Currency: "USD"},
		InitialStock: -1,
	})
	if !errors.Is(err, ErrInvalidStock) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInvalidStock)
	}
}

func TestServiceUpdatePreservesStock(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:          9,
			SKU:         "SKU-9",
			Name:        "Widget",
			Description: "old",
			Price:       Money{Amount: 100, Currency: "USD"},
			Stock:       5,
			Status:      StatusDraft,
			Version:     3,
			CreatedAt:   fixedTime.Add(-time.Hour),
			UpdatedAt:   fixedTime.Add(-time.Hour),
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	got, err := service.Update(t.Context(), UpdateInput{
		ID:              9,
		SKU:             "sku-10",
		Name:            "Updated",
		Description:     "new",
		Price:           Money{Amount: 120, Currency: "eur"},
		ExpectedVersion: 3,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got.Stock != 5 {
		t.Fatalf("Update() stock = %d, want 5", got.Stock)
	}
	if got.Status != StatusDraft {
		t.Fatalf("Update() status = %q, want %q", got.Status, StatusDraft)
	}
	if store.updateExpected != 3 {
		t.Fatalf("Update() expected version = %d, want 3", store.updateExpected)
	}
	if store.updateProduct.Stock != 5 {
		t.Fatalf("persisted stock = %d, want 5", store.updateProduct.Stock)
	}
	if store.byIDVisible {
		t.Fatal("Update() should fetch administrative view including draft products")
	}
}

func TestServicePublishAndUnpublish(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:        9,
			SKU:       "SKU-9",
			Name:      "Widget",
			Price:     Money{Amount: 100, Currency: "USD"},
			Stock:     5,
			Status:    StatusDraft,
			Version:   2,
			CreatedAt: fixedTime.Add(-time.Hour),
			UpdatedAt: fixedTime.Add(-time.Hour),
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	published, err := service.Publish(t.Context(), 9, 2)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Status != StatusActive {
		t.Fatalf("Publish() status = %q, want %q", published.Status, StatusActive)
	}

	store.byIDProduct = published
	unpublished, err := service.Unpublish(t.Context(), 9, published.Version)
	if err != nil {
		t.Fatalf("Unpublish() error = %v", err)
	}
	if unpublished.Status != StatusInactive {
		t.Fatalf("Unpublish() status = %q, want %q", unpublished.Status, StatusInactive)
	}
}

func TestServicePublicLookupForcesActiveOnly(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   1,
			Status:  StatusInactive,
			Version: 1,
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	_, err := service.PublicByID(t.Context(), 9)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("PublicByID() error = %v, want errors.Is(_, %v)", err, ErrNotFound)
	}
	if !store.byIDVisible {
		t.Fatal("PublicByID() should force active-only visibility")
	}
}

func TestServiceAdminLookupIncludesInactive(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   1,
			Status:  StatusInactive,
			Version: 1,
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	got, err := service.AdminByID(t.Context(), 9)
	if err != nil {
		t.Fatalf("AdminByID() error = %v", err)
	}
	if got.Status != StatusInactive {
		t.Fatalf("AdminByID() status = %q, want %q", got.Status, StatusInactive)
	}
	if store.byIDVisible {
		t.Fatal("AdminByID() should allow inactive records")
	}
}

func TestServiceListPublicForcesActiveStatusAndPagination(t *testing.T) {
	store := &fakeStore{
		listPage: Page{
			Items: []Product{{
				ID:      9,
				SKU:     "SKU-9",
				Name:    "Widget",
				Price:   Money{Amount: 100, Currency: "USD"},
				Status:  StatusActive,
				Version: 1,
			}},
			Total:  1,
			Limit:  10,
			Offset: 20,
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	got, err := service.ListPublic(t.Context(), ListFilter{
		Status:     StatusInactive,
		Sort:       "created_at",
		Descending: true,
		Limit:      10,
		Offset:     20,
	})
	if err != nil {
		t.Fatalf("ListPublic() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Status != StatusActive {
		t.Fatalf("ListPublic() items = %+v, want one active product", got.Items)
	}
	if !store.listVisible {
		t.Fatal("ListPublic() should force active-only visibility")
	}
	if store.listFilter.Status != StatusActive {
		t.Fatalf("ListPublic() status filter = %q, want %q", store.listFilter.Status, StatusActive)
	}
	if store.listFilter.Offset != 20 || store.listFilter.Limit != 10 || !store.listFilter.Descending {
		t.Fatalf("ListPublic() filter = %+v, want pagination and sort preserved", store.listFilter)
	}
}

func TestServiceListAdminAllowsDraftFilter(t *testing.T) {
	store := &fakeStore{
		listPage: Page{
			Items: []Product{{
				ID:      9,
				SKU:     "SKU-9",
				Name:    "Widget",
				Price:   Money{Amount: 100, Currency: "USD"},
				Status:  StatusDraft,
				Version: 1,
			}},
			Total:  1,
			Limit:  5,
			Offset: 0,
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	got, err := service.ListAdmin(t.Context(), ListFilter{
		Status: StatusDraft,
		Sort:   "sku",
		Limit:  5,
	})
	if err != nil {
		t.Fatalf("ListAdmin() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Status != StatusDraft {
		t.Fatalf("ListAdmin() items = %+v, want one draft product", got.Items)
	}
	if store.listVisible {
		t.Fatal("ListAdmin() should not force active-only visibility")
	}
	if store.listFilter.Status != StatusDraft {
		t.Fatalf("ListAdmin() status filter = %q, want %q", store.listFilter.Status, StatusDraft)
	}
}

func TestServiceAdjustStock(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   5,
			Status:  StatusActive,
			Version: 2,
		},
		adjustResult: &Product{
			ID:        9,
			SKU:       "SKU-9",
			Name:      "Widget",
			Price:     Money{Amount: 100, Currency: "USD"},
			Stock:     2,
			Status:    StatusActive,
			Version:   3,
			UpdatedAt: fixedTime,
		},
	}
	svc := newTestService(t, store, fakeClock{now: fixedTime})

	got, err := svc.AdjustStock(t.Context(), Actor{UserID: 1}, AdjustStockInput{
		ProductID: 9,
		Delta:     -3,
		Reason:    "damaged",
		Version:   2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Stock != 2 {
		t.Fatalf("Stock = %d", got.Stock)
	}
	if store.adjustment.Delta != -3 || store.adjustment.StockAfter != 2 {
		t.Fatal("incorrect stock ledger")
	}
	if store.adjustment.Actor.UserID != 1 {
		t.Fatalf("Actor = %+v, want user 1", store.adjustment.Actor)
	}
	if store.adjustment.Reason != "damaged" {
		t.Fatalf("Reason = %q, want damaged", store.adjustment.Reason)
	}
}

func TestServiceAdjustStockRejectsInvalidInput(t *testing.T) {
	service := newTestService(t, &fakeStore{}, fakeClock{now: fixedTime})
	tests := []struct {
		name    string
		actor   Actor
		input   AdjustStockInput
		wantErr error
	}{
		{
			name:    "actor required",
			input:   AdjustStockInput{ProductID: 9, Delta: 1, Reason: "restock", Version: 1},
			wantErr: ErrForbidden,
		},
		{
			name:    "zero delta",
			actor:   Actor{UserID: 1},
			input:   AdjustStockInput{ProductID: 9, Delta: 0, Reason: "restock", Version: 1},
			wantErr: ErrInvalidStockAdjustment,
		},
		{
			name:    "reason required",
			actor:   Actor{UserID: 1},
			input:   AdjustStockInput{ProductID: 9, Delta: 1, Reason: " \t ", Version: 1},
			wantErr: ErrInvalidStockAdjustment,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.AdjustStock(t.Context(), tt.actor, tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AdjustStock() error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
		})
	}
}

func TestServiceAdjustStockRejectsInsufficientStock(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   2,
			Status:  StatusActive,
			Version: 2,
		},
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	_, err := service.AdjustStock(t.Context(), Actor{UserID: 1}, AdjustStockInput{
		ProductID: 9,
		Delta:     -3,
		Reason:    "damaged",
		Version:   2,
	})
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("AdjustStock() error = %v, want errors.Is(_, %v)", err, ErrInsufficientStock)
	}
}

func TestServicePropagatesVersionConflict(t *testing.T) {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   2,
			Status:  StatusDraft,
			Version: 2,
		},
		updateErr: ErrConflict,
	}
	service := newTestService(t, store, fakeClock{now: fixedTime})

	_, err := service.Update(t.Context(), UpdateInput{
		ID:              9,
		SKU:             "SKU-9",
		Name:            "Widget",
		Price:           Money{Amount: 100, Currency: "USD"},
		ExpectedVersion: 2,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Update() error = %v, want errors.Is(_, %v)", err, ErrConflict)
	}
}

func TestClonePageReturnsOwnedItems(t *testing.T) {
	page := Page{
		Items: []Product{{
			ID:      1,
			SKU:     "SKU-1",
			Name:    "One",
			Price:   Money{Amount: 100, Currency: "USD"},
			Status:  StatusActive,
			Version: 1,
		}},
		Total:  1,
		Limit:  10,
		Offset: 0,
	}
	cloned := clonePage(page)
	cloned.Items[0].Name = "Changed"
	if page.Items[0].Name != "One" {
		t.Fatalf("clonePage() mutated source page: %+v", page.Items[0])
	}
}

func ExampleService_AdjustStock() {
	store := &fakeStore{
		byIDProduct: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   5,
			Status:  StatusActive,
			Version: 2,
		},
		adjustResult: &Product{
			ID:      9,
			SKU:     "SKU-9",
			Name:    "Widget",
			Price:   Money{Amount: 100, Currency: "USD"},
			Stock:   2,
			Status:  StatusActive,
			Version: 3,
		},
	}
	svc, _ := NewService(store, fakeClock{now: fixedTime})
	got, _ := svc.AdjustStock(context.Background(), Actor{UserID: 1}, AdjustStockInput{
		ProductID: 9,
		Delta:     -3,
		Reason:    "damaged",
		Version:   2,
	})
	fmt.Println(got.Stock)
	// Output: 2
}
