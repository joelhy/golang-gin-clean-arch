package reporting

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidRange  = errors.New("invalid reporting range")
	ErrMixedCurrency = errors.New("mixed currencies")
)

// ValidationError preserves the stable sentinel while still pointing callers at
// the exact query field or range invariant that failed.
type ValidationError struct {
	Field   string
	Message string
	Err     error
}

func (e *ValidationError) Error() string {
	if e.Message == "" {
		return e.Field + " is invalid"
	}
	return e.Field + " " + e.Message
}

func (e *ValidationError) Unwrap() error { return e.Err }

type Range struct {
	From time.Time
	To   time.Time
}

type Snapshot struct {
	Users     Users
	Inventory Inventory
	Orders    Orders
}

type Users struct {
	Total  uint64
	Active uint64
	New    uint64
}

type Inventory struct {
	ActiveProducts uint64
	UnitsInStock   uint64
	StockValue     int64
	Currency       string
}

type Orders struct {
	Total       uint64
	Pending     uint64
	Confirmed   uint64
	Shipped     uint64
	Delivered   uint64
	Cancelled   uint64
	GrossAmount int64
	Currency    string
}

type Store interface {
	Snapshot(context.Context, Range) (Snapshot, error)
}
