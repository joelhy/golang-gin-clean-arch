package order

import (
	"context"
	"fmt"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusShipped   Status = "shipped"
	StatusDelivered Status = "delivered"
	StatusCancelled Status = "cancelled"
)

type Money struct {
	Amount   int64
	Currency string
}

type Item struct {
	ID        uint64
	ProductID uint64
	SKU       string
	Name      string
	UnitPrice Money
	Subtotal  Money
	Quantity  uint32
}

type Order struct {
	ID        uint64
	UserID    uint64
	Number    string
	Status    Status
	Total     Money
	Items     []Item
	Version   uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Actor struct {
	UserID uint64
	Admin  bool
}

type RequestedItem struct {
	ProductID uint64
	Quantity  uint32
}

type CreateInput struct {
	IdempotencyKey string
	Items          []RequestedItem
}

type SellableProduct struct {
	ID        uint64
	SKU       string
	Name      string
	UnitPrice Money
	Stock     uint32
}

type StockChange struct {
	ProductID uint64
	Delta     int64
}

type Page struct {
	Items  []Order
	Total  uint64
	Limit  int
	Offset int
}

type ListFilter struct {
	UserID         uint64
	Statuses       []Status
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	MinTotalAmount *int64
	MaxTotalAmount *int64
	Sort           string
	Descending     bool
	Limit          int
	Offset         int
}

func (f ListFilter) Validate() error {
	for _, status := range f.Statuses {
		if !validStatus(status) {
			return &ValidationError{Field: "statuses", Message: fmt.Sprintf("%q is not supported", status), Err: ErrInvalidFilter}
		}
	}
	if f.CreatedFrom != nil && f.CreatedTo != nil && f.CreatedFrom.After(*f.CreatedTo) {
		return &ValidationError{Field: "created_range", Message: "must not be inverted", Err: ErrInvalidFilter}
	}
	if f.MinTotalAmount != nil && f.MaxTotalAmount != nil && *f.MinTotalAmount > *f.MaxTotalAmount {
		return &ValidationError{Field: "total_range", Message: "must not be inverted", Err: ErrInvalidFilter}
	}
	switch f.Sort {
	case "", "id", "number", "status", "total", "created_at", "updated_at":
	default:
		return &ValidationError{Field: "sort", Message: fmt.Sprintf("%q is not supported", f.Sort), Err: ErrInvalidFilter}
	}
	if f.Limit < 1 || f.Limit > 100 {
		return &ValidationError{Field: "limit", Message: "must be between 1 and 100", Err: ErrInvalidFilter}
	}
	if f.Offset < 0 {
		return &ValidationError{Field: "offset", Message: "must not be negative", Err: ErrInvalidFilter}
	}
	return nil
}

type IdempotencyClaim struct {
	UserID      uint64
	Key         string
	RequestHash [32]byte
	CreatedAt   time.Time
}

type IdempotencyKind string

const (
	IdempotencyClaimed  IdempotencyKind = "claimed"
	IdempotencyReplay   IdempotencyKind = "replay"
	IdempotencyConflict IdempotencyKind = "conflict"
)

type IdempotencyResult struct {
	Kind    IdempotencyKind
	OrderID uint64
}

type IdempotencyCompletion struct {
	UserID    uint64
	Key       string
	OrderID   uint64
	UpdatedAt time.Time
}

type Transactor interface {
	WithinTransaction(context.Context, func(Tx) error) error
}

type Tx interface {
	ClaimIdempotency(context.Context, IdempotencyClaim) (IdempotencyResult, error)
	LockProducts(context.Context, []uint64) ([]SellableProduct, error)
	DecreaseStock(context.Context, StockChange) error
	IncreaseStock(context.Context, StockChange) error
	CreateOrder(context.Context, *Order) error
	CompleteIdempotency(context.Context, IdempotencyCompletion) error
	LockOrder(context.Context, uint64) (*Order, error)
	UpdateStatus(context.Context, *Order, uint64) error
}

type Reader interface {
	ByID(context.Context, uint64) (*Order, error)
	List(context.Context, ListFilter) (Page, error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type NumberGenerator interface {
	New(time.Time) (string, error)
}

func validStatus(status Status) bool {
	return status == StatusPending ||
		status == StatusConfirmed ||
		status == StatusShipped ||
		status == StatusDelivered ||
		status == StatusCancelled
}
