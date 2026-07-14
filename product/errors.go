package product

import "errors"

var (
	ErrInvalidSKU             = errors.New("invalid sku")
	ErrInvalidName            = errors.New("invalid name")
	ErrInvalidPrice           = errors.New("invalid price")
	ErrInvalidCurrency        = errors.New("invalid currency")
	ErrInvalidStock           = errors.New("invalid stock")
	ErrInvalidStatus          = errors.New("invalid status")
	ErrInvalidFilter          = errors.New("invalid product list filter")
	ErrInvalidStockAdjustment = errors.New("invalid stock adjustment")
	ErrNotFound               = errors.New("not found")
	ErrForbidden              = errors.New("forbidden")
	ErrConflict               = errors.New("conflict")
	ErrInsufficientStock      = errors.New("insufficient stock")
)

// ValidationError keeps a stable sentinel while exposing the failing field and a
// human-readable message.
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
