package reporting

import (
	"context"
	"errors"
	"time"
)

const maxSnapshotRange = 366 * 24 * time.Hour

type Service struct {
	store Store
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("new reporting service: store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) Snapshot(ctx context.Context, window Range) (Snapshot, error) {
	if err := validateRange(window); err != nil {
		return Snapshot{}, err
	}
	return s.store.Snapshot(ctx, window)
}

func validateRange(window Range) error {
	switch {
	case window.From.IsZero():
		return &ValidationError{Field: "from", Message: "is required", Err: ErrInvalidRange}
	case window.To.IsZero():
		return &ValidationError{Field: "to", Message: "is required", Err: ErrInvalidRange}
	case window.From.Location() != time.UTC:
		return &ValidationError{Field: "from", Message: "must be UTC", Err: ErrInvalidRange}
	case window.To.Location() != time.UTC:
		return &ValidationError{Field: "to", Message: "must be UTC", Err: ErrInvalidRange}
	case !window.From.Before(window.To):
		return &ValidationError{Field: "range", Message: "must satisfy from < to", Err: ErrInvalidRange}
	case window.To.Sub(window.From) > maxSnapshotRange:
		return &ValidationError{Field: "range", Message: "must not exceed 366 days", Err: ErrInvalidRange}
	}
	return nil
}
