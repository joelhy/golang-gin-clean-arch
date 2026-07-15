package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"clean-arch-gin/reporting"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type fakeStatsHandlerService struct {
	snapshotFn func(context.Context, reporting.Range) (reporting.Snapshot, error)
}

func (f fakeStatsHandlerService) Snapshot(ctx context.Context, window reporting.Range) (reporting.Snapshot, error) {
	return f.snapshotFn(ctx, window)
}

func TestStatsHandlerSnapshot(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewStatsHandler(fakeStatsHandlerService{
		snapshotFn: func(_ context.Context, window reporting.Range) (reporting.Snapshot, error) {
			wantFrom := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			wantTo := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
			if window != (reporting.Range{From: wantFrom, To: wantTo}) {
				t.Fatalf("window = %+v", window)
			}
			return reporting.Snapshot{
				Users:     reporting.Users{Total: 10, Active: 8, New: 3},
				Inventory: reporting.Inventory{ActiveProducts: 4, UnitsInStock: 22, StockValue: 8800, Currency: "CNY"},
				Orders:    reporting.Orders{Total: 7, Pending: 1, Confirmed: 1, Shipped: 2, Delivered: 2, Cancelled: 1, GrossAmount: 15200, Currency: "CNY"},
			}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/stats", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionStatsRead), handler.Get)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/stats?from=2026-07-01T00:00:00Z&to=2026-07-15T00:00:00Z", nil, nil)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.users.total", float64(10))
	assertJSONPath(t, rec.Body.Bytes(), "data.inventory.currency", "CNY")
	assertJSONPath(t, rec.Body.Bytes(), "data.orders.gross_amount.amount", float64(15200))
}

func TestStatsHandlerFailsClosedWithoutPermissionEvidence(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	called := false
	handler := NewStatsHandler(fakeStatsHandlerService{
		snapshotFn: func(_ context.Context, _ reporting.Range) (reporting.Snapshot, error) {
			called = true
			return reporting.Snapshot{}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/stats", injectIdentity(user.Identity{UserID: 77}), handler.Get)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/stats?from=2026-07-01T00:00:00Z&to=2026-07-15T00:00:00Z", nil, nil)

	assertStatusCode(t, rec, http.StatusForbidden)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
	if called {
		t.Fatal("Get must not run without permission evidence")
	}
}
