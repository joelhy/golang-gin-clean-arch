//go:build integration

package mysqlstore

import (
	"errors"
	"testing"
	"time"

	"clean-arch-gin/mysqlstore/query"
	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/reporting"
	"clean-arch-gin/user"
)

func TestReportingStore(t *testing.T) {
	t.Run("SnapshotAggregatesUsersInventoryAndOrders", func(t *testing.T) {
		db := newTestDB(t)
		userStore := mustUserStore(t, db)
		productStore := mustProductStore(t, db)
		store, err := NewReportingStore(db)
		if err != nil {
			t.Fatalf("NewReportingStore() error = %v", err)
		}

		now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
		from := now.Add(-24 * time.Hour)
		to := now.Add(24 * time.Hour)

		customer := createStoredUser(t, userStore, "report-customer@example.com", user.RoleCustomer)
		admin := createStoredUser(t, userStore, "report-admin@example.com", user.RoleAdmin)
		disabled := createStoredUser(t, userStore, "report-disabled@example.com", user.RoleCustomer)
		q := query.Use(db)
		for _, accountID := range []uint64{customer.ID, admin.ID, disabled.ID} {
			if _, err := q.User.WithContext(t.Context()).
				Where(q.User.ID.Eq(accountID)).
				UpdateSimple(
					q.User.CreatedAt.Value(now),
					q.User.UpdatedAt.Value(now),
				); err != nil {
				t.Fatalf("pin user %d timestamps: %v", accountID, err)
			}
		}
		if _, err := q.User.WithContext(t.Context()).
			Where(q.User.ID.Eq(disabled.ID)).
			UpdateSimple(q.User.Status.Value(string(user.StatusDisabled))); err != nil {
			t.Fatalf("disable user: %v", err)
		}

		first := createStoredProduct(t, productStore, product.Product{
			SKU:         "REP-001",
			Name:        "Alpha",
			Description: "alpha",
			Price:       product.Money{Amount: 500, Currency: "CNY"},
			Stock:       10,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		second := createStoredProduct(t, productStore, product.Product{
			SKU:         "REP-002",
			Name:        "Beta",
			Description: "beta",
			Price:       product.Money{Amount: 300, Currency: "CNY"},
			Stock:       4,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		_ = createStoredProduct(t, productStore, product.Product{
			SKU:         "REP-003",
			Name:        "Draft",
			Description: "draft",
			Price:       product.Money{Amount: 800, Currency: "CNY"},
			Stock:       20,
			Status:      product.StatusDraft,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		seedOrderRow(t, db, seededOrder{
			ID: 201, Number: "ORD-REP-201", UserID: customer.ID, Status: order.StatusPending, TotalAmount: 1000, Currency: "CNY", Version: 1,
			CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
			Items: []seededOrderItem{
				{ID: 2011, ProductID: first.ID, SKU: first.SKU, Name: first.Name, UnitPriceAmount: 500, Currency: "CNY", Quantity: 2, SubtotalAmount: 1000},
			},
		})
		seedOrderRow(t, db, seededOrder{
			ID: 202, Number: "ORD-REP-202", UserID: customer.ID, Status: order.StatusDelivered, TotalAmount: 1200, Currency: "CNY", Version: 2,
			CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
			Items: []seededOrderItem{
				{ID: 2021, ProductID: second.ID, SKU: second.SKU, Name: second.Name, UnitPriceAmount: 300, Currency: "CNY", Quantity: 4, SubtotalAmount: 1200},
			},
		})
		seedOrderRow(t, db, seededOrder{
			ID: 203, Number: "ORD-REP-203", UserID: admin.ID, Status: order.StatusCancelled, TotalAmount: 300, Currency: "CNY", Version: 3,
			CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now.Add(-30 * time.Minute),
			Items: []seededOrderItem{
				{ID: 2031, ProductID: second.ID, SKU: second.SKU, Name: second.Name, UnitPriceAmount: 300, Currency: "CNY", Quantity: 1, SubtotalAmount: 300},
			},
		})
		seedOrderRow(t, db, seededOrder{
			ID: 204, Number: "ORD-REP-204", UserID: admin.ID, Status: order.StatusConfirmed, TotalAmount: 500, Currency: "CNY", Version: 1,
			CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-72 * time.Hour),
			Items: []seededOrderItem{
				{ID: 2041, ProductID: first.ID, SKU: first.SKU, Name: first.Name, UnitPriceAmount: 500, Currency: "CNY", Quantity: 1, SubtotalAmount: 500},
			},
		})

		got, err := store.Snapshot(t.Context(), reporting.Range{From: from, To: to})
		if err != nil {
			t.Fatalf("Snapshot() error = %v", err)
		}
		want := reporting.Snapshot{
			Users:     reporting.Users{Total: 3, Active: 2, New: 3},
			Inventory: reporting.Inventory{ActiveProducts: 2, UnitsInStock: 14, StockValue: 6200, Currency: "CNY"},
			Orders:    reporting.Orders{Total: 3, Pending: 1, Confirmed: 0, Shipped: 0, Delivered: 1, Cancelled: 1, GrossAmount: 2500, Currency: "CNY"},
		}
		if got != want {
			t.Fatalf("Snapshot() = %+v, want %+v", got, want)
		}
	})

	t.Run("SnapshotRejectsMixedCurrencies", func(t *testing.T) {
		db := newTestDB(t)
		productStore := mustProductStore(t, db)
		store, err := NewReportingStore(db)
		if err != nil {
			t.Fatalf("NewReportingStore() error = %v", err)
		}

		now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
		_ = createStoredProduct(t, productStore, product.Product{
			SKU:         "REP-MIX-001",
			Name:        "CNY",
			Description: "cny",
			Price:       product.Money{Amount: 100, Currency: "CNY"},
			Stock:       2,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		_ = createStoredProduct(t, productStore, product.Product{
			SKU:         "REP-MIX-002",
			Name:        "USD",
			Description: "usd",
			Price:       product.Money{Amount: 100, Currency: "USD"},
			Stock:       2,
			Status:      product.StatusActive,
			Version:     1,
			CreatedAt:   now,
			UpdatedAt:   now,
		})

		_, err = store.Snapshot(t.Context(), reporting.Range{From: now.Add(-time.Hour), To: now.Add(time.Hour)})
		if !errors.Is(err, reporting.ErrMixedCurrency) {
			t.Fatalf("Snapshot() error = %v, want ErrMixedCurrency", err)
		}
	})
}
