package product

import (
	"context"
	"fmt"
	"time"
)

type Status string

const (
	StatusDraft    Status = "draft"
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
)

type Money struct {
	Amount   int64
	Currency string
}

type Product struct {
	ID          uint64
	SKU         string
	Name        string
	Description string
	Price       Money
	Stock       uint32
	Status      Status
	Version     uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Actor struct {
	UserID uint64
}

type CreateInput struct {
	SKU          string
	Name         string
	Description  string
	Price        Money
	InitialStock int64
}

type UpdateInput struct {
	ID              uint64
	SKU             string
	Name            string
	Description     string
	Price           Money
	ExpectedVersion uint64
}

type AdjustStockInput struct {
	ProductID uint64
	Delta     int64
	Reason    string
	Version   uint64
}

type AdjustmentInput struct {
	ProductID  uint64
	Delta      int64
	StockAfter uint32
	Reason     string
	Version    uint64
	Actor      Actor
	UpdatedAt  time.Time
}

type Page struct {
	Items  []Product
	Total  uint64
	Limit  int
	Offset int
}

type ListFilter struct {
	Status     Status
	Sort       string
	Descending bool
	Limit      int
	Offset     int
}

func (f ListFilter) Validate() error {
	if f.Status != "" && !validStatus(f.Status) {
		return &ValidationError{Field: "status", Message: fmt.Sprintf("%q is not supported", f.Status), Err: ErrInvalidFilter}
	}
	switch f.Sort {
	case "id", "sku", "name", "status", "price", "created_at", "updated_at":
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

type Store interface {
	Create(context.Context, *Product) error
	ByID(context.Context, uint64, bool) (*Product, error)
	Update(context.Context, *Product, uint64) error
	AdjustStock(context.Context, AdjustmentInput) (*Product, error)
	List(context.Context, ListFilter, bool) (Page, error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

func validStatus(status Status) bool {
	return status == StatusDraft || status == StatusActive || status == StatusInactive
}
