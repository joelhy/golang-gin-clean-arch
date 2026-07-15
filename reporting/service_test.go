package reporting

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	snapshotFn func(context.Context, Range) (Snapshot, error)
}

func (f fakeStore) Snapshot(ctx context.Context, window Range) (Snapshot, error) {
	return f.snapshotFn(ctx, window)
}

func TestServiceSnapshotValidatesRangeAndDelegates(t *testing.T) {
	t.Run("delegates validated UTC range", func(t *testing.T) {
		from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
		want := Snapshot{
			Users:     Users{Total: 4, Active: 3, New: 2},
			Inventory: Inventory{ActiveProducts: 2, UnitsInStock: 18, StockValue: 7200, Currency: "CNY"},
			Orders:    Orders{Total: 5, Pending: 1, Confirmed: 1, Shipped: 1, Delivered: 1, Cancelled: 1, GrossAmount: 5600, Currency: "CNY"},
		}
		called := false

		service, err := NewService(fakeStore{
			snapshotFn: func(_ context.Context, got Range) (Snapshot, error) {
				called = true
				if got != (Range{From: from, To: to}) {
					t.Fatalf("range = %+v", got)
				}
				return want, nil
			},
		})
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}

		got, err := service.Snapshot(t.Context(), Range{From: from, To: to})
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		if got != want {
			t.Fatalf("Snapshot() = %+v, want %+v", got, want)
		}
		if !called {
			t.Fatal("Snapshot() did not delegate to the store")
		}
	})

	t.Run("rejects non UTC boundaries", func(t *testing.T) {
		service, err := NewService(fakeStore{
			snapshotFn: func(_ context.Context, _ Range) (Snapshot, error) {
				t.Fatal("Snapshot() store must not run for invalid range")
				return Snapshot{}, nil
			},
		})
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}

		_, err = service.Snapshot(t.Context(), Range{
			From: time.Date(2026, 7, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*3600)),
			To:   time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "from" {
			t.Fatalf("Snapshot() error = %v, want ValidationError(field=from)", err)
		}
	})

	t.Run("rejects inverted and oversized ranges", func(t *testing.T) {
		service, err := NewService(fakeStore{
			snapshotFn: func(_ context.Context, _ Range) (Snapshot, error) {
				t.Fatal("Snapshot() store must not run for invalid range")
				return Snapshot{}, nil
			},
		})
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}

		_, err = service.Snapshot(t.Context(), Range{
			From: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "range" {
			t.Fatalf("Snapshot() error = %v, want ValidationError(field=range)", err)
		}

		_, err = service.Snapshot(t.Context(), Range{
			From: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		})
		if !errors.As(err, &validationErr) || validationErr.Field != "range" {
			t.Fatalf("Snapshot() error = %v, want ValidationError(field=range)", err)
		}
	})
}
