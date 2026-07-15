package order

import "errors"

var (
	ErrInvalidOrder      = errors.New("invalid order")
	ErrInvalidMoney      = errors.New("invalid money")
	ErrInvalidCurrency   = errors.New("invalid currency")
	ErrInvalidStatus     = errors.New("invalid order status")
	ErrInvalidTransition = errors.New("invalid order transition")
	ErrInvalidFilter     = errors.New("invalid order list filter")
	ErrNotFound          = errors.New("not found")
	ErrForbidden         = errors.New("forbidden")
	ErrConflict          = errors.New("conflict")
	ErrInsufficientStock = errors.New("insufficient stock")
)

// ValidationError keeps a stable sentinel while still identifying which input
// field violated a domain rule.
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
