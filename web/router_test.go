package web

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"testing"

	"clean-arch-gin/config"
	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/reporting"
	"clean-arch-gin/user"
)

type fakeRouterAuthService struct{}

func (fakeRouterAuthService) Register(context.Context, user.RegisterInput) (*user.User, error) {
	return &user.User{ID: 1, Email: "user@example.com", Name: "User", Status: user.StatusActive}, nil
}
func (fakeRouterAuthService) Login(context.Context, user.LoginInput) (user.Tokens, error) {
	return user.Tokens{}, nil
}
func (fakeRouterAuthService) Refresh(context.Context, string) (user.Tokens, error) {
	return user.Tokens{}, nil
}
func (fakeRouterAuthService) Logout(context.Context, user.Identity) error { return nil }
func (fakeRouterAuthService) Authenticate(context.Context, string) (user.Identity, error) {
	return user.Identity{UserID: 7, SessionID: "session-1", TokenID: "token-1"}, nil
}
func (fakeRouterAuthService) Authorize(_ context.Context, identity user.Identity, permission string) error {
	if identity.UserID == 0 || permission == "" {
		return user.ErrForbidden
	}
	return nil
}

type fakeRouterUserService struct{}

func (fakeRouterUserService) Me(context.Context, uint64) (*user.User, error) {
	return &user.User{ID: 7, Email: "me@example.com", Name: "Me", Status: user.StatusActive}, nil
}
func (fakeRouterUserService) UpdateMe(context.Context, uint64, user.UpdateMeInput) (*user.User, error) {
	return &user.User{ID: 7, Email: "me@example.com", Name: "Me", Status: user.StatusActive}, nil
}
func (fakeRouterUserService) List(context.Context, user.Actor, user.ListFilter) (user.Page, error) {
	return user.Page{}, nil
}
func (fakeRouterUserService) AdminByID(context.Context, user.Actor, uint64) (*user.User, error) {
	return &user.User{ID: 8, Email: "admin@example.com", Name: "Admin", Status: user.StatusActive}, nil
}
func (fakeRouterUserService) SetStatus(context.Context, user.Actor, user.SetStatusInput) error {
	return nil
}
func (fakeRouterUserService) ReplaceRoles(context.Context, user.Actor, user.ReplaceRolesInput) error {
	return nil
}
func (fakeRouterUserService) Permissions(context.Context, uint64) ([]string, error) {
	return []string{user.PermissionUsersRead}, nil
}

type fakeRouterProductService struct{}

func (fakeRouterProductService) Create(context.Context, product.CreateInput) (*product.Product, error) {
	return &product.Product{ID: 1, SKU: "SKU-1", Name: "Widget", Price: product.Money{Amount: 100, Currency: "CNY"}, Status: product.StatusDraft}, nil
}
func (fakeRouterProductService) Update(context.Context, product.UpdateInput) (*product.Product, error) {
	return &product.Product{ID: 1, SKU: "SKU-1", Name: "Widget", Price: product.Money{Amount: 100, Currency: "CNY"}, Status: product.StatusDraft}, nil
}
func (fakeRouterProductService) Publish(context.Context, uint64, uint64) (*product.Product, error) {
	return nil, nil
}
func (fakeRouterProductService) Unpublish(context.Context, uint64, uint64) (*product.Product, error) {
	return nil, nil
}
func (fakeRouterProductService) PublicByID(context.Context, uint64) (*product.Product, error) {
	return &product.Product{ID: 1, SKU: "SKU-1", Name: "Widget", Price: product.Money{Amount: 100, Currency: "CNY"}, Status: product.StatusActive}, nil
}
func (fakeRouterProductService) AdminByID(context.Context, uint64) (*product.Product, error) {
	return &product.Product{ID: 1, SKU: "SKU-1", Name: "Widget", Price: product.Money{Amount: 100, Currency: "CNY"}, Status: product.StatusDraft}, nil
}
func (fakeRouterProductService) ListPublic(context.Context, product.ListFilter) (product.Page, error) {
	return product.Page{}, nil
}
func (fakeRouterProductService) ListAdmin(context.Context, product.ListFilter) (product.Page, error) {
	return product.Page{}, nil
}
func (fakeRouterProductService) AdjustStock(context.Context, product.Actor, product.AdjustStockInput) (*product.Product, error) {
	return &product.Product{ID: 1, SKU: "SKU-1", Name: "Widget", Price: product.Money{Amount: 100, Currency: "CNY"}, Status: product.StatusActive}, nil
}

type fakeRouterOrderService struct{}

func (fakeRouterOrderService) Create(context.Context, uint64, order.CreateInput) (*order.Order, error) {
	return &order.Order{ID: 1, UserID: 7, Number: "ORD-1", Status: order.StatusPending, Total: order.Money{Amount: 100, Currency: "CNY"}}, nil
}
func (fakeRouterOrderService) ByID(context.Context, order.Actor, uint64) (*order.Order, error) {
	return &order.Order{ID: 1, UserID: 7, Number: "ORD-1", Status: order.StatusPending, Total: order.Money{Amount: 100, Currency: "CNY"}}, nil
}
func (fakeRouterOrderService) List(context.Context, order.Actor, order.ListFilter) (order.Page, error) {
	return order.Page{}, nil
}
func (fakeRouterOrderService) Confirm(context.Context, uint64, uint64) (*order.Order, error) {
	return &order.Order{ID: 1, Status: order.StatusConfirmed, Total: order.Money{Amount: 100, Currency: "CNY"}}, nil
}
func (fakeRouterOrderService) Ship(context.Context, uint64, uint64) (*order.Order, error) {
	return &order.Order{ID: 1, Status: order.StatusShipped, Total: order.Money{Amount: 100, Currency: "CNY"}}, nil
}
func (fakeRouterOrderService) Deliver(context.Context, uint64, uint64) (*order.Order, error) {
	return &order.Order{ID: 1, Status: order.StatusDelivered, Total: order.Money{Amount: 100, Currency: "CNY"}}, nil
}
func (fakeRouterOrderService) Cancel(context.Context, order.Actor, uint64, uint64) (*order.Order, error) {
	return &order.Order{ID: 1, Status: order.StatusCancelled, Total: order.Money{Amount: 100, Currency: "CNY"}}, nil
}

type fakeRouterStatsService struct{}

func (fakeRouterStatsService) Snapshot(context.Context, reporting.Range) (reporting.Snapshot, error) {
	return reporting.Snapshot{
		Users:     reporting.Users{Total: 1, Active: 1, New: 1},
		Inventory: reporting.Inventory{ActiveProducts: 1, UnitsInStock: 2, StockValue: 200, Currency: "CNY"},
		Orders:    reporting.Orders{Total: 1, Pending: 1, GrossAmount: 100, Currency: "CNY"},
	}, nil
}

func TestRouterRegistersApprovedRoutesOnly(t *testing.T) {
	router, err := NewRouter(context.Background(), Dependencies{
		Auth:      fakeRouterAuthService{},
		Users:     fakeRouterUserService{},
		Products:  fakeRouterProductService{},
		Orders:    fakeRouterOrderService{},
		Stats:     fakeRouterStatsService{},
		Readiness: func(context.Context) error { return nil },
		Logger:    slog.Default(),
		Config:    config.Config{CORS: config.CORS{}, RateLimit: config.RateLimit{LoginRequestsPerSecond: 10, LoginBurst: 10}},
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	got := make([]string, 0, len(router.Routes()))
	for _, route := range router.Routes() {
		got = append(got, route.Method+" "+route.Path)
	}
	sort.Strings(got)

	want := []string{
		"GET /api/v1/admin/orders",
		"GET /api/v1/admin/stats",
		"GET /api/v1/admin/users",
		"GET /api/v1/admin/users/:id",
		"GET /api/v1/orders",
		"GET /api/v1/orders/:id",
		"GET /api/v1/products",
		"GET /api/v1/products/:id",
		"GET /api/v1/users/me",
		"GET /health/live",
		"GET /health/ready",
		"PATCH /api/v1/admin/products/:id",
		"PATCH /api/v1/admin/users/:id/status",
		"PATCH /api/v1/users/me",
		"POST /api/v1/admin/orders/:id/cancel",
		"POST /api/v1/admin/orders/:id/confirm",
		"POST /api/v1/admin/orders/:id/deliver",
		"POST /api/v1/admin/orders/:id/ship",
		"POST /api/v1/admin/products",
		"POST /api/v1/admin/products/:id/stock-adjustments",
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/logout",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/register",
		"POST /api/v1/orders",
		"POST /api/v1/orders/:id/cancel",
		"PUT /api/v1/admin/users/:id/roles",
	}
	if len(got) != len(want) {
		t.Fatalf("Routes() count = %d, want %d\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Routes()[%d] = %q, want %q\nall=%v", i, got[i], want[i], got)
		}
	}

	for _, forbidden := range []string{
		"GET /api/v1/admin/products",
		"GET /api/v1/admin/products/:id",
		"GET /api/v1/admin/users/:id/permissions",
		"POST /api/v1/admin/products/:id/publish",
		"POST /api/v1/admin/products/:id/unpublish",
	} {
		for _, route := range got {
			if route == forbidden {
				t.Fatalf("forbidden route registered: %s", forbidden)
			}
		}
	}
}

func TestRouterAppliesConfiguredMaxBodyBytes(t *testing.T) {
	router, err := NewRouter(context.Background(), Dependencies{
		Auth:     fakeRouterAuthService{},
		Users:    fakeRouterUserService{},
		Products: fakeRouterProductService{},
		Orders:   fakeRouterOrderService{},
		Stats:    fakeRouterStatsService{},
		Logger:   slog.Default(),
		Config: config.Config{
			HTTP:      config.HTTP{MaxBodyBytes: 8},
			CORS:      config.CORS{},
			RateLimit: config.RateLimit{LoginRequestsPerSecond: 10, LoginBurst: 10},
		},
	})
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	rec := performRequest(t, router, http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(`{"email":"user@example.com"}`), map[string][]string{
		"Content-Type": {"application/json"},
	})

	assertStatusCode(t, rec, http.StatusRequestEntityTooLarge)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeMalformedJSON))
}

func TestHealthEndpointsAndErrorEnvelopes(t *testing.T) {
	t.Run("liveness and readiness success", func(t *testing.T) {
		called := 0
		router, err := NewRouter(context.Background(), Dependencies{
			Auth:      fakeRouterAuthService{},
			Users:     fakeRouterUserService{},
			Products:  fakeRouterProductService{},
			Orders:    fakeRouterOrderService{},
			Stats:     fakeRouterStatsService{},
			Readiness: func(context.Context) error { called++; return nil },
			Logger:    slog.Default(),
			Config:    config.Config{CORS: config.CORS{}, RateLimit: config.RateLimit{LoginRequestsPerSecond: 10, LoginBurst: 10}},
		})
		if err != nil {
			t.Fatalf("NewRouter() error = %v", err)
		}

		live := performRequest(t, router, http.MethodGet, "/health/live", nil, nil)
		ready := performRequest(t, router, http.MethodGet, "/health/ready", nil, nil)

		assertStatusCode(t, live, http.StatusOK)
		assertStatusCode(t, ready, http.StatusOK)
		assertJSONPath(t, live.Body.Bytes(), "code", float64(CodeOK))
		assertJSONPath(t, ready.Body.Bytes(), "data.status", "ready")
		if called != 1 {
			t.Fatalf("readiness calls = %d, want 1", called)
		}
	})

	t.Run("readiness failure, no route, and no method use the envelope", func(t *testing.T) {
		router, err := NewRouter(context.Background(), Dependencies{
			Auth:      fakeRouterAuthService{},
			Users:     fakeRouterUserService{},
			Products:  fakeRouterProductService{},
			Orders:    fakeRouterOrderService{},
			Stats:     fakeRouterStatsService{},
			Readiness: func(context.Context) error { return errors.New("db unavailable") },
			Logger:    slog.Default(),
			Config:    config.Config{CORS: config.CORS{}, RateLimit: config.RateLimit{LoginRequestsPerSecond: 10, LoginBurst: 10}},
		})
		if err != nil {
			t.Fatalf("NewRouter() error = %v", err)
		}

		ready := performRequest(t, router, http.MethodGet, "/health/ready", nil, nil)
		missing := performRequest(t, router, http.MethodGet, "/missing", nil, nil)
		noMethod := performRequest(t, router, http.MethodGet, "/api/v1/auth/register", nil, nil)

		assertStatusCode(t, ready, http.StatusServiceUnavailable)
		assertStatusCode(t, missing, http.StatusNotFound)
		assertStatusCode(t, noMethod, http.StatusMethodNotAllowed)
		assertJSONPath(t, ready.Body.Bytes(), "code", float64(CodeInternal))
		assertJSONPath(t, missing.Body.Bytes(), "message", "route not found")
		assertJSONPath(t, noMethod.Body.Bytes(), "message", "method not allowed")
	})
}
