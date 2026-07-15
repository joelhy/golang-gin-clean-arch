package web

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"clean-arch-gin/order"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type fakeOrderHandlerService struct {
	createFn  func(context.Context, uint64, order.CreateInput) (*order.Order, error)
	byIDFn    func(context.Context, order.Actor, uint64) (*order.Order, error)
	listFn    func(context.Context, order.Actor, order.ListFilter) (order.Page, error)
	cancelFn  func(context.Context, order.Actor, uint64, uint64) (*order.Order, error)
	confirmFn func(context.Context, uint64, uint64) (*order.Order, error)
	shipFn    func(context.Context, uint64, uint64) (*order.Order, error)
	deliverFn func(context.Context, uint64, uint64) (*order.Order, error)
}

func (f fakeOrderHandlerService) Create(ctx context.Context, userID uint64, input order.CreateInput) (*order.Order, error) {
	return f.createFn(ctx, userID, input)
}

func (f fakeOrderHandlerService) ByID(ctx context.Context, actor order.Actor, orderID uint64) (*order.Order, error) {
	return f.byIDFn(ctx, actor, orderID)
}

func (f fakeOrderHandlerService) List(ctx context.Context, actor order.Actor, filter order.ListFilter) (order.Page, error) {
	return f.listFn(ctx, actor, filter)
}

func (f fakeOrderHandlerService) Cancel(ctx context.Context, actor order.Actor, orderID uint64, expectedVersion uint64) (*order.Order, error) {
	return f.cancelFn(ctx, actor, orderID, expectedVersion)
}

func (f fakeOrderHandlerService) Confirm(ctx context.Context, orderID uint64, expectedVersion uint64) (*order.Order, error) {
	return f.confirmFn(ctx, orderID, expectedVersion)
}

func (f fakeOrderHandlerService) Ship(ctx context.Context, orderID uint64, expectedVersion uint64) (*order.Order, error) {
	return f.shipFn(ctx, orderID, expectedVersion)
}

func (f fakeOrderHandlerService) Deliver(ctx context.Context, orderID uint64, expectedVersion uint64) (*order.Order, error) {
	return f.deliverFn(ctx, orderID, expectedVersion)
}

func TestOrderHandlerCreateRequiresIdempotencyKey(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	called := false
	handler := NewOrderHandler(fakeOrderHandlerService{
		createFn: func(_ context.Context, _ uint64, _ order.CreateInput) (*order.Order, error) {
			called = true
			return nil, nil
		},
	})

	router := gin.New()
	router.POST("/api/v1/orders", injectIdentity(user.Identity{UserID: 7}), handler.Create)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/orders", `{"items":[{"product_id":9,"quantity":2}]}`)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeValidation))
	assertJSONPath(t, rec.Body.Bytes(), "data.0.field", "idempotency_key")
	if called {
		t.Fatal("Create must not run without a valid Idempotency-Key header")
	}
}

func TestOrderHandlerCreateSupportsReplayAndConflict(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	var calls int
	handler := NewOrderHandler(fakeOrderHandlerService{
		createFn: func(_ context.Context, userID uint64, input order.CreateInput) (*order.Order, error) {
			calls++
			if userID != 7 || input.IdempotencyKey != "checkout-1" {
				t.Fatalf("userID=%d input=%+v", userID, input)
			}
			if input.Items[0].Quantity == 3 {
				return nil, order.ErrConflict
			}
			return &order.Order{
				ID:     41,
				UserID: 7,
				Number: "ORD-41",
				Status: order.StatusPending,
				Total:  order.Money{Amount: 2400, Currency: "CNY"},
				Items: []order.Item{{
					ID: 401, ProductID: 9, SKU: "SKU-9", Name: "Widget",
					UnitPrice: order.Money{Amount: 1200, Currency: "CNY"},
					Subtotal:  order.Money{Amount: 2400, Currency: "CNY"},
					Quantity:  2,
				}},
				Version:   1,
				CreatedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
			}, nil
		},
	})

	router := gin.New()
	router.POST("/api/v1/orders", injectIdentity(user.Identity{UserID: 7}), handler.Create)

	first := performRequest(t, router, http.MethodPost, "/api/v1/orders", bytes.NewBufferString(`{"items":[{"product_id":9,"quantity":2}]}`), map[string][]string{
		"Content-Type":    {"application/json"},
		"Idempotency-Key": {"checkout-1"},
	})
	replay := performRequest(t, router, http.MethodPost, "/api/v1/orders", bytes.NewBufferString(`{"items":[{"product_id":9,"quantity":2}]}`), map[string][]string{
		"Content-Type":    {"application/json"},
		"Idempotency-Key": {"checkout-1"},
	})
	conflict := performRequest(t, router, http.MethodPost, "/api/v1/orders", bytes.NewBufferString(`{"items":[{"product_id":9,"quantity":3}]}`), map[string][]string{
		"Content-Type":    {"application/json"},
		"Idempotency-Key": {"checkout-1"},
	})

	assertStatusCode(t, first, http.StatusCreated)
	assertStatusCode(t, replay, http.StatusCreated)
	assertStatusCode(t, conflict, http.StatusConflict)
	assertJSONPath(t, replay.Body.Bytes(), "data.id", float64(41))
	assertJSONPath(t, conflict.Body.Bytes(), "code", float64(CodeIdempotencyConflict))
}

func TestOrderHandlerCustomerRoutesUseAuthenticatedActor(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewOrderHandler(fakeOrderHandlerService{
		byIDFn: func(_ context.Context, actor order.Actor, orderID uint64) (*order.Order, error) {
			if actor.UserID != 7 || actor.Admin {
				t.Fatalf("actor = %+v", actor)
			}
			if orderID == 99 {
				return nil, order.ErrForbidden
			}
			return &order.Order{ID: orderID, UserID: 7, Number: "ORD-42", Status: order.StatusPending, Total: order.Money{Amount: 1200, Currency: "CNY"}}, nil
		},
		listFn: func(_ context.Context, actor order.Actor, filter order.ListFilter) (order.Page, error) {
			if actor.UserID != 7 || actor.Admin {
				t.Fatalf("actor = %+v", actor)
			}
			if filter.UserID != 7 || len(filter.Statuses) != 1 || filter.Statuses[0] != order.StatusPending {
				t.Fatalf("filter = %+v", filter)
			}
			return order.Page{Items: []order.Order{{ID: 42, UserID: 7, Number: "ORD-42", Status: order.StatusPending, Total: order.Money{Amount: 1200, Currency: "CNY"}}}, Total: 1, Limit: 20}, nil
		},
		cancelFn: func(_ context.Context, actor order.Actor, orderID uint64, expectedVersion uint64) (*order.Order, error) {
			if actor.UserID != 7 || actor.Admin || orderID != 42 || expectedVersion != 3 {
				t.Fatalf("actor=%+v orderID=%d version=%d", actor, orderID, expectedVersion)
			}
			return &order.Order{ID: 42, UserID: 7, Status: order.StatusCancelled, Version: 4, Total: order.Money{Amount: 1200, Currency: "CNY"}}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/orders", injectIdentity(user.Identity{UserID: 7}), handler.ListCustomer)
	router.GET("/api/v1/orders/:id", injectIdentity(user.Identity{UserID: 7}), handler.GetCustomer)
	router.POST("/api/v1/orders/:id/cancel", injectIdentity(user.Identity{UserID: 7}), handler.CancelCustomer)

	listRec := performRequest(t, router, http.MethodGet, "/api/v1/orders?status=pending", nil, nil)
	getRec := performRequest(t, router, http.MethodGet, "/api/v1/orders/42", nil, nil)
	forbiddenRec := performRequest(t, router, http.MethodGet, "/api/v1/orders/99", nil, nil)
	cancelRec := performJSON(t, router, http.MethodPost, "/api/v1/orders/42/cancel", `{"version":3}`)

	assertStatusCode(t, listRec, http.StatusOK)
	assertStatusCode(t, getRec, http.StatusOK)
	assertStatusCode(t, forbiddenRec, http.StatusForbidden)
	assertStatusCode(t, cancelRec, http.StatusOK)
	assertJSONPath(t, listRec.Body.Bytes(), "data.items.0.id", float64(42))
	assertJSONPath(t, cancelRec.Body.Bytes(), "data.status", "cancelled")
}

func TestOrderHandlerAdminListAndTransitionsRequirePermissionEvidence(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewOrderHandler(fakeOrderHandlerService{
		listFn: func(_ context.Context, actor order.Actor, filter order.ListFilter) (order.Page, error) {
			if actor.UserID != 77 || !actor.Admin {
				t.Fatalf("actor = %+v", actor)
			}
			if filter.UserID != 0 || len(filter.Statuses) != 2 || filter.Statuses[0] != order.StatusPending || filter.Statuses[1] != order.StatusConfirmed {
				t.Fatalf("filter = %+v", filter)
			}
			if filter.MinTotalAmount == nil || *filter.MinTotalAmount != 1000 || filter.MaxTotalAmount == nil || *filter.MaxTotalAmount != 5000 {
				t.Fatalf("filter = %+v", filter)
			}
			if filter.CreatedFrom == nil || filter.CreatedTo == nil || filter.Sort != "total" || !filter.Descending || filter.Limit != 5 || filter.Offset != 10 {
				t.Fatalf("filter = %+v", filter)
			}
			return order.Page{Items: []order.Order{{ID: 51, UserID: 9, Number: "ORD-51", Status: order.StatusPending, Total: order.Money{Amount: 1000, Currency: "CNY"}}}, Total: 1, Limit: 5, Offset: 10}, nil
		},
		confirmFn: func(_ context.Context, orderID uint64, expectedVersion uint64) (*order.Order, error) {
			if orderID != 51 || expectedVersion != 2 {
				t.Fatalf("confirm orderID=%d version=%d", orderID, expectedVersion)
			}
			return &order.Order{ID: 51, Status: order.StatusConfirmed, Version: 3, Total: order.Money{Amount: 1000, Currency: "CNY"}}, nil
		},
		shipFn: func(_ context.Context, orderID uint64, expectedVersion uint64) (*order.Order, error) {
			if orderID != 51 || expectedVersion != 3 {
				t.Fatalf("ship orderID=%d version=%d", orderID, expectedVersion)
			}
			return &order.Order{ID: 51, Status: order.StatusShipped, Version: 4, Total: order.Money{Amount: 1000, Currency: "CNY"}}, nil
		},
		deliverFn: func(_ context.Context, orderID uint64, expectedVersion uint64) (*order.Order, error) {
			if orderID != 51 || expectedVersion != 4 {
				t.Fatalf("deliver orderID=%d version=%d", orderID, expectedVersion)
			}
			return &order.Order{ID: 51, Status: order.StatusDelivered, Version: 5, Total: order.Money{Amount: 1000, Currency: "CNY"}}, nil
		},
		cancelFn: func(_ context.Context, actor order.Actor, orderID uint64, expectedVersion uint64) (*order.Order, error) {
			if actor.UserID != 77 || !actor.Admin || orderID != 51 || expectedVersion != 5 {
				t.Fatalf("cancel actor=%+v orderID=%d version=%d", actor, orderID, expectedVersion)
			}
			return &order.Order{ID: 51, Status: order.StatusCancelled, Version: 6, Total: order.Money{Amount: 1000, Currency: "CNY"}}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/orders", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionOrdersReadAll), handler.ListAdmin)
	router.POST("/api/v1/admin/orders/:id/confirm", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionOrdersManage), handler.Confirm)
	router.POST("/api/v1/admin/orders/:id/ship", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionOrdersManage), handler.Ship)
	router.POST("/api/v1/admin/orders/:id/deliver", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionOrdersManage), handler.Deliver)
	router.POST("/api/v1/admin/orders/:id/cancel", injectIdentity(user.Identity{UserID: 77}), injectPermissions(user.PermissionOrdersManage), handler.CancelAdmin)

	listRec := performRequest(t, router, http.MethodGet, "/api/v1/admin/orders?status=pending&status=confirmed&created_from=2026-07-15T00:00:00Z&created_to=2026-07-16T00:00:00Z&min_total_amount=1000&max_total_amount=5000&sort=total&direction=desc&limit=5&offset=10", nil, nil)
	confirmRec := performJSON(t, router, http.MethodPost, "/api/v1/admin/orders/51/confirm", `{"version":2}`)
	shipRec := performJSON(t, router, http.MethodPost, "/api/v1/admin/orders/51/ship", `{"version":3}`)
	deliverRec := performJSON(t, router, http.MethodPost, "/api/v1/admin/orders/51/deliver", `{"version":4}`)
	cancelRec := performJSON(t, router, http.MethodPost, "/api/v1/admin/orders/51/cancel", `{"version":5}`)

	assertStatusCode(t, listRec, http.StatusOK)
	assertStatusCode(t, confirmRec, http.StatusOK)
	assertStatusCode(t, shipRec, http.StatusOK)
	assertStatusCode(t, deliverRec, http.StatusOK)
	assertStatusCode(t, cancelRec, http.StatusOK)
	assertJSONPath(t, listRec.Body.Bytes(), "data.pagination.offset", float64(10))
	assertJSONPath(t, confirmRec.Body.Bytes(), "data.status", "confirmed")
	assertJSONPath(t, shipRec.Body.Bytes(), "data.status", "shipped")
	assertJSONPath(t, deliverRec.Body.Bytes(), "data.status", "delivered")
	assertJSONPath(t, cancelRec.Body.Bytes(), "data.status", "cancelled")
}

func TestOrderHandlerAdminRoutesFailClosedWithoutPermissionEvidence(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	called := false
	handler := NewOrderHandler(fakeOrderHandlerService{
		listFn: func(_ context.Context, _ order.Actor, _ order.ListFilter) (order.Page, error) {
			called = true
			return order.Page{}, nil
		},
	})

	router := gin.New()
	router.GET("/api/v1/admin/orders", injectIdentity(user.Identity{UserID: 77}), handler.ListAdmin)

	rec := performRequest(t, router, http.MethodGet, "/api/v1/admin/orders", nil, nil)

	assertStatusCode(t, rec, http.StatusForbidden)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
	if called {
		t.Fatal("ListAdmin must not run without permission evidence")
	}
}
