package order

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
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

type fakeNumbers struct {
	value string
	err   error
	now   time.Time
}

func (n fakeNumbers) New(now time.Time) (string, error) {
	if n.err != nil {
		return "", n.err
	}
	if n.value != "" {
		return n.value, nil
	}
	return "ORD-20260714-TEST", nil
}

type fakeReader struct {
	byIDOrder *Order
	byIDErr   error
	byIDCalls int

	listFilter ListFilter
	listPage   Page
	listErr    error
	listCalls  int
}

func (r *fakeReader) ByID(_ context.Context, id uint64) (*Order, error) {
	r.byIDCalls++
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	if r.byIDOrder == nil {
		return nil, nil
	}
	if r.byIDOrder.ID != 0 && r.byIDOrder.ID != id {
		return nil, ErrNotFound
	}
	return cloneOrder(r.byIDOrder), nil
}

func (r *fakeReader) List(_ context.Context, filter ListFilter) (Page, error) {
	r.listCalls++
	r.listFilter = filter
	return clonePage(r.listPage), r.listErr
}

type fakeTransactor struct {
	tx  *fakeTx
	err error
}

func (t fakeTransactor) WithinTransaction(ctx context.Context, fn func(Tx) error) error {
	if t.err != nil {
		return t.err
	}
	if t.tx == nil {
		return errors.New("missing tx")
	}
	return fn(t.tx)
}

type fakeTx struct {
	claim      IdempotencyResult
	claimErr   error
	claimInput IdempotencyClaim

	products  map[uint64]SellableProduct
	lockErr   error
	lockedIDs []uint64

	decreased   []StockChange
	decreaseErr error

	increased   []StockChange
	increaseErr error

	createOrder *Order
	createErr   error

	complete    IdempotencyCompletion
	completeErr error

	lockOrder     *Order
	lockOrderErr  error
	lockOrderID   uint64
	updatedOrder  *Order
	updateVersion uint64
	updateErr     error
}

func (tx *fakeTx) ClaimIdempotency(_ context.Context, claim IdempotencyClaim) (IdempotencyResult, error) {
	tx.claimInput = claim
	return tx.claim, tx.claimErr
}

func (tx *fakeTx) LockProducts(_ context.Context, ids []uint64) ([]SellableProduct, error) {
	tx.lockedIDs = append([]uint64(nil), ids...)
	if tx.lockErr != nil {
		return nil, tx.lockErr
	}
	products := make([]SellableProduct, 0, len(ids))
	for _, id := range ids {
		product, ok := tx.products[id]
		if !ok {
			return nil, ErrNotFound
		}
		products = append(products, product)
	}
	return products, nil
}

func (tx *fakeTx) DecreaseStock(_ context.Context, change StockChange) error {
	tx.decreased = append(tx.decreased, change)
	return tx.decreaseErr
}

func (tx *fakeTx) IncreaseStock(_ context.Context, change StockChange) error {
	tx.increased = append(tx.increased, change)
	return tx.increaseErr
}

func (tx *fakeTx) CreateOrder(_ context.Context, order *Order) error {
	tx.createOrder = cloneOrder(order)
	if tx.createErr == nil {
		order.ID = 42
	}
	return tx.createErr
}

func (tx *fakeTx) CompleteIdempotency(_ context.Context, completion IdempotencyCompletion) error {
	tx.complete = completion
	return tx.completeErr
}

func (tx *fakeTx) LockOrder(_ context.Context, id uint64) (*Order, error) {
	tx.lockOrderID = id
	if tx.lockOrderErr != nil {
		return nil, tx.lockOrderErr
	}
	return cloneOrder(tx.lockOrder), nil
}

func (tx *fakeTx) UpdateStatus(_ context.Context, order *Order, expectedVersion uint64) error {
	tx.updatedOrder = cloneOrder(order)
	tx.updateVersion = expectedVersion
	return tx.updateErr
}

func newTestService(t *testing.T, transactor Transactor, reader Reader, clock Clock, numbers NumberGenerator) *Service {
	t.Helper()
	service, err := NewService(transactor, reader, clock, numbers)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func TestNewServiceRejectsNilDependencies(t *testing.T) {
	transactor := fakeTransactor{tx: &fakeTx{}}
	reader := &fakeReader{}
	clock := fakeClock{now: fixedTime}
	numbers := fakeNumbers{}

	tests := []struct {
		name       string
		transactor Transactor
		reader     Reader
		clock      Clock
		numbers    NumberGenerator
		wantErr    string
	}{
		{name: "valid", transactor: transactor, reader: reader, clock: clock, numbers: numbers},
		{name: "nil transactor", reader: reader, clock: clock, numbers: numbers, wantErr: "transactor is required"},
		{name: "nil reader", transactor: transactor, clock: clock, numbers: numbers, wantErr: "reader is required"},
		{name: "nil clock", transactor: transactor, reader: reader, numbers: numbers, wantErr: "clock is required"},
		{name: "nil numbers", transactor: transactor, reader: reader, clock: clock, wantErr: "number generator is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewService(tt.transactor, tt.reader, tt.clock, tt.numbers)
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

func TestCheckoutSortsLocksAndUsesServerPrices(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		2: {ID: 2, SKU: "B", Name: "B", UnitPrice: Money{Amount: 500, Currency: "CNY"}, Stock: 2},
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: 300, Currency: "CNY"}, Stock: 2},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{value: "ORD-20260714-LOCKS"})

	got, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-1",
		Items: []RequestedItem{
			{ProductID: 2, Quantity: 1},
			{ProductID: 1, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff([]uint64{1, 2}, tx.lockedIDs); diff != "" {
		t.Fatalf("LockProducts() ids mismatch (-want +got):\n%s", diff)
	}
	if got.Total.Amount != 1100 {
		t.Fatalf("Create() total = %d, want 1100", got.Total.Amount)
	}
	if got.Number != "ORD-20260714-LOCKS" {
		t.Fatalf("Create() number = %q, want %q", got.Number, "ORD-20260714-LOCKS")
	}
	if diff := cmp.Diff([]StockChange{
		{ProductID: 1, Delta: -2},
		{ProductID: 2, Delta: -1},
	}, tx.decreased); diff != "" {
		t.Fatalf("DecreaseStock() mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateRejectsEmptyOrder(t *testing.T) {
	svc := newTestService(t, fakeTransactor{tx: &fakeTx{}}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{IdempotencyKey: "checkout-1"})
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInvalidOrder)
	}
}

func TestCreateConsolidatesDuplicateProducts(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		9: {ID: 9, SKU: "SKU-9", Name: "Widget", UnitPrice: Money{Amount: 250, Currency: "USD"}, Stock: 10},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	got, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-duplicates",
		Items: []RequestedItem{
			{ProductID: 9, Quantity: 1},
			{ProductID: 9, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("Create() item count = %d, want 1", len(got.Items))
	}
	if got.Items[0].Quantity != 3 {
		t.Fatalf("Create() quantity = %d, want 3", got.Items[0].Quantity)
	}
	if got.Items[0].Subtotal.Amount != 750 {
		t.Fatalf("Create() subtotal = %d, want 750", got.Items[0].Subtotal.Amount)
	}
}

func TestCreateRejectsZeroQuantity(t *testing.T) {
	svc := newTestService(t, fakeTransactor{tx: &fakeTx{}}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-zero",
		Items:          []RequestedItem{{ProductID: 9, Quantity: 0}},
	})
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInvalidOrder)
	}
}

func TestCreateRejectsMixedCurrencies(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: 100, Currency: "USD"}, Stock: 1},
		2: {ID: 2, SKU: "B", Name: "B", UnitPrice: Money{Amount: 100, Currency: "CNY"}, Stock: 1},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-currency",
		Items: []RequestedItem{
			{ProductID: 1, Quantity: 1},
			{ProductID: 2, Quantity: 1},
		},
	})
	if !errors.Is(err, ErrInvalidCurrency) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInvalidCurrency)
	}
}

func TestCreateRejectsSubtotalOverflow(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: math.MaxInt64, Currency: "USD"}, Stock: 2},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-overflow-subtotal",
		Items:          []RequestedItem{{ProductID: 1, Quantity: 2}},
	})
	if !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInvalidMoney)
	}
}

func TestCreateRejectsTotalOverflow(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: math.MaxInt64 - 5, Currency: "USD"}, Stock: 1},
		2: {ID: 2, SKU: "B", Name: "B", UnitPrice: Money{Amount: 10, Currency: "USD"}, Stock: 1},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-overflow-total",
		Items: []RequestedItem{
			{ProductID: 1, Quantity: 1},
			{ProductID: 2, Quantity: 1},
		},
	})
	if !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInvalidMoney)
	}
}

func TestCreateRejectsInsufficientStock(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: 100, Currency: "USD"}, Stock: 1},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-stock",
		Items:          []RequestedItem{{ProductID: 1, Quantity: 2}},
	})
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrInsufficientStock)
	}
}

func TestCreateReplaysIdempotentRequest(t *testing.T) {
	existing := &Order{
		ID:        88,
		UserID:    7,
		Number:    "ORD-20260714-EXISTING",
		Status:    StatusPending,
		Total:     Money{Amount: 1100, Currency: "CNY"},
		CreatedAt: fixedTime,
		UpdatedAt: fixedTime,
	}
	tx := &fakeTx{
		claim: IdempotencyResult{Kind: IdempotencyReplay, OrderID: existing.ID},
	}
	reader := &fakeReader{byIDOrder: existing}
	svc := newTestService(t, fakeTransactor{tx: tx}, reader, fakeClock{now: fixedTime}, fakeNumbers{})

	got, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-1",
		Items: []RequestedItem{
			{ProductID: 2, Quantity: 1},
			{ProductID: 1, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if diff := cmp.Diff(existing, got); diff != "" {
		t.Fatalf("Create() replay mismatch (-want +got):\n%s", diff)
	}
	if len(tx.lockedIDs) != 0 {
		t.Fatalf("LockProducts() called for replay: %v", tx.lockedIDs)
	}
}

func TestCreateRejectsIdempotencyPayloadConflict(t *testing.T) {
	tx := &fakeTx{
		claim: IdempotencyResult{Kind: IdempotencyConflict},
	}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-1",
		Items:          []RequestedItem{{ProductID: 1, Quantity: 1}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrConflict)
	}
}

func TestServiceCancelEnforcesPermissionsAndRestoresInventory(t *testing.T) {
	order := &Order{
		ID:        55,
		UserID:    7,
		Status:    StatusPending,
		Total:     Money{Amount: 1100, Currency: "CNY"},
		Version:   3,
		CreatedAt: fixedTime.Add(-time.Hour),
		UpdatedAt: fixedTime.Add(-time.Hour),
		Items: []Item{
			{ProductID: 1, Quantity: 2},
			{ProductID: 2, Quantity: 1},
		},
	}

	t.Run("owner can cancel and restore stock", func(t *testing.T) {
		tx := &fakeTx{lockOrder: order}
		svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{byIDOrder: order}, fakeClock{now: fixedTime}, fakeNumbers{})

		got, err := svc.Cancel(t.Context(), Actor{UserID: 7}, 55, 3)
		if err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		if got.Status != StatusCancelled {
			t.Fatalf("Cancel() status = %q, want %q", got.Status, StatusCancelled)
		}
		if diff := cmp.Diff([]StockChange{
			{ProductID: 1, Delta: 2},
			{ProductID: 2, Delta: 1},
		}, tx.increased); diff != "" {
			t.Fatalf("IncreaseStock() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("non owner is forbidden", func(t *testing.T) {
		tx := &fakeTx{lockOrder: order}
		svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{byIDOrder: order}, fakeClock{now: fixedTime}, fakeNumbers{})

		_, err := svc.Cancel(t.Context(), Actor{UserID: 8}, 55, 3)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Cancel() error = %v, want errors.Is(_, %v)", err, ErrForbidden)
		}
		if tx.lockOrderID != 0 {
			t.Fatalf("LockOrder() should not be called for forbidden user, got %d", tx.lockOrderID)
		}
	})

	t.Run("admin can cancel any order", func(t *testing.T) {
		tx := &fakeTx{lockOrder: order}
		svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

		got, err := svc.Cancel(t.Context(), Actor{UserID: 99, Admin: true}, 55, 3)
		if err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		if got.Status != StatusCancelled {
			t.Fatalf("Cancel() status = %q, want %q", got.Status, StatusCancelled)
		}
	})
}

func TestOrderTransitions(t *testing.T) {
	now := fixedTime
	tests := []struct {
		name        string
		initial     Status
		apply       func(*Order) error
		wantStatus  Status
		wantVersion uint64
		wantErr     error
	}{
		{name: "pending to confirmed", initial: StatusPending, apply: func(o *Order) error { return o.Confirm(now) }, wantStatus: StatusConfirmed, wantVersion: 2},
		{name: "pending to cancelled", initial: StatusPending, apply: func(o *Order) error { return o.Cancel(now) }, wantStatus: StatusCancelled, wantVersion: 2},
		{name: "confirmed to shipped", initial: StatusConfirmed, apply: func(o *Order) error { return o.Ship(now) }, wantStatus: StatusShipped, wantVersion: 2},
		{name: "confirmed to cancelled", initial: StatusConfirmed, apply: func(o *Order) error { return o.Cancel(now) }, wantStatus: StatusCancelled, wantVersion: 2},
		{name: "shipped to delivered", initial: StatusShipped, apply: func(o *Order) error { return o.Deliver(now) }, wantStatus: StatusDelivered, wantVersion: 2},
		{name: "confirmed cannot confirm again", initial: StatusConfirmed, apply: func(o *Order) error { return o.Confirm(now) }, wantErr: ErrInvalidTransition},
		{name: "pending cannot ship", initial: StatusPending, apply: func(o *Order) error { return o.Ship(now) }, wantErr: ErrInvalidTransition},
		{name: "pending cannot deliver", initial: StatusPending, apply: func(o *Order) error { return o.Deliver(now) }, wantErr: ErrInvalidTransition},
		{name: "shipped cannot cancel", initial: StatusShipped, apply: func(o *Order) error { return o.Cancel(now) }, wantErr: ErrInvalidTransition},
		{name: "delivered is terminal", initial: StatusDelivered, apply: func(o *Order) error { return o.Cancel(now) }, wantErr: ErrInvalidTransition},
		{name: "cancelled is terminal", initial: StatusCancelled, apply: func(o *Order) error { return o.Ship(now) }, wantErr: ErrInvalidTransition},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := &Order{
				ID:        9,
				UserID:    7,
				Status:    tt.initial,
				Total:     Money{Amount: 100, Currency: "USD"},
				Version:   1,
				CreatedAt: fixedTime.Add(-time.Hour),
				UpdatedAt: fixedTime.Add(-time.Hour),
			}
			err := tt.apply(order)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("transition error = %v, want errors.Is(_, %v)", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if order.Status != tt.wantStatus {
					t.Fatalf("status = %q, want %q", order.Status, tt.wantStatus)
				}
				if order.Version != tt.wantVersion {
					t.Fatalf("version = %d, want %d", order.Version, tt.wantVersion)
				}
				if order.UpdatedAt != now {
					t.Fatalf("updated at = %v, want %v", order.UpdatedAt, now)
				}
			}
		})
	}
}

func TestNewNumberGeneratorUsesUTCDateAndRandomSuffix(t *testing.T) {
	now := time.Date(2026, 7, 14, 23, 30, 0, 0, time.FixedZone("cst", 8*60*60))
	// The fixed byte pattern makes the suffix deterministic while still exercising the base32 encoder.
	reader := io.LimitReader(strings.NewReader("abcdefghij"), 10)
	generator := NewNumberGenerator(reader)

	got, err := generator.New(now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got != "ORD-20260714-MFRGGZDFMZTWQ2LK" {
		t.Fatalf("New() = %q, want %q", got, "ORD-20260714-MFRGGZDFMZTWQ2LK")
	}
}

func TestCanonicalRequestHashIgnoresInputOrderAfterConsolidation(t *testing.T) {
	left, err := canonicalRequestHash([]RequestedItem{
		{ProductID: 2, Quantity: 1},
		{ProductID: 1, Quantity: 2},
		{ProductID: 2, Quantity: 3},
	})
	if err != nil {
		t.Fatalf("canonicalRequestHash() error = %v", err)
	}
	right, err := canonicalRequestHash([]RequestedItem{
		{ProductID: 1, Quantity: 2},
		{ProductID: 2, Quantity: 4},
	})
	if err != nil {
		t.Fatalf("canonicalRequestHash() error = %v", err)
	}
	if left != right {
		t.Fatalf("canonicalRequestHash() mismatch: %x != %x", left, right)
	}
	if left == sha256.Sum256(nil) {
		t.Fatal("canonicalRequestHash() should not match the empty digest")
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

func TestValidationErrorPreservesSentinel(t *testing.T) {
	err := &ValidationError{Field: "items", Message: "must not be empty", Err: ErrInvalidOrder}
	if !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("errors.Is(%v, ErrInvalidOrder) = false", err)
	}
	if got, want := err.Error(), "items must not be empty"; got != want {
		t.Fatalf("ValidationError.Error() = %q, want %q", got, want)
	}
}

func TestCreatePersistsIdempotencyHash(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: 100, Currency: "USD"}, Stock: 3},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-hash",
		Items:          []RequestedItem{{ProductID: 1, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantHash, err := canonicalRequestHash([]RequestedItem{{ProductID: 1, Quantity: 2}})
	if err != nil {
		t.Fatalf("canonicalRequestHash() error = %v", err)
	}
	if diff := cmp.Diff(wantHash[:], tx.claimInput.RequestHash[:]); diff != "" {
		t.Fatalf("ClaimIdempotency() request hash mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateUsesCheckoutOperationForIdempotencyClaimAndCompletion(t *testing.T) {
	tx := &fakeTx{products: map[uint64]SellableProduct{
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: 100, Currency: "USD"}, Stock: 3},
	}}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-operation",
		Items:          []RequestedItem{{ProductID: 1, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// The idempotency key is scoped by operation so checkout cannot collide with
	// other consumer actions that reuse the same key for the same user.
	if got := reflect.ValueOf(tx.claimInput).FieldByName("Operation"); !got.IsValid() {
		t.Fatal("ClaimIdempotency() claim is missing Operation")
	} else if got.String() != "checkout" {
		t.Fatalf("ClaimIdempotency() operation = %q, want %q", got.String(), "checkout")
	}
	if got := reflect.ValueOf(tx.complete).FieldByName("Operation"); !got.IsValid() {
		t.Fatal("CompleteIdempotency() completion is missing Operation")
	} else if got.String() != "checkout" {
		t.Fatalf("CompleteIdempotency() operation = %q, want %q", got.String(), "checkout")
	}
}

func TestCreateReturnsReplayNotFoundWhenReaderMissesOrder(t *testing.T) {
	tx := &fakeTx{
		claim: IdempotencyResult{Kind: IdempotencyReplay, OrderID: 99},
	}
	svc := newTestService(t, fakeTransactor{tx: tx}, &fakeReader{}, fakeClock{now: fixedTime}, fakeNumbers{})

	_, err := svc.Create(t.Context(), 7, CreateInput{
		IdempotencyKey: "checkout-1",
		Items:          []RequestedItem{{ProductID: 1, Quantity: 1}},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Create() error = %v, want errors.Is(_, %v)", err, ErrNotFound)
	}
}

func TestNumberGeneratorRejectsShortRandomReader(t *testing.T) {
	generator := NewNumberGenerator(strings.NewReader("short"))

	_, err := generator.New(fixedTime)
	if err == nil {
		t.Fatal("New() error = nil, want non-nil")
	}
}

func ExampleNewNumberGenerator() {
	reader := io.LimitReader(strings.NewReader("abcdefghij"), 10)
	generator := NewNumberGenerator(reader)
	number, _ := generator.New(time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	fmt.Println(number)
	// Output: ORD-20260714-MFRGGZDFMZTWQ2LK
}
