package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sort"
	"testing"

	"clean-arch-gin/config"
)

func TestAPIContractApprovedRoutes(t *testing.T) {
	router, err := NewRouter(context.Background(), contractDependencies())
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
}

func TestAPIContractEnvelopeExamples(t *testing.T) {
	router, err := NewRouter(context.Background(), contractDependencies())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	success := performRequest(t, router, http.MethodGet, "/health/live", nil, nil)
	assertStatusCode(t, success, http.StatusOK)
	assertJSONPath(t, success.Body.Bytes(), "code", float64(CodeOK))
	assertJSONPath(t, success.Body.Bytes(), "data.status", "live")

	created := performRequest(t, router, http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(`{"email":"user@example.com","name":"User","password":"correct horse battery staple"}`), map[string][]string{
		"Content-Type": {"application/json"},
	})
	assertStatusCode(t, created, http.StatusCreated)
	assertJSONPath(t, created.Body.Bytes(), "code", float64(CodeOK))
	assertJSONPath(t, created.Body.Bytes(), "data.email", "user@example.com")

	malformed := performRequest(t, router, http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(`{"email"`), map[string][]string{
		"Content-Type": {"application/json"},
	})
	assertStatusCode(t, malformed, http.StatusBadRequest)
	assertJSONPath(t, malformed.Body.Bytes(), "code", float64(CodeMalformedJSON))

	unauthorized := performRequest(t, router, http.MethodGet, "/api/v1/users/me", nil, nil)
	assertStatusCode(t, unauthorized, http.StatusUnauthorized)
	assertJSONPath(t, unauthorized.Body.Bytes(), "code", float64(CodeAuthentication))

	missing := performRequest(t, router, http.MethodGet, "/api/v1/missing", nil, nil)
	assertStatusCode(t, missing, http.StatusNotFound)
	assertJSONPath(t, missing.Body.Bytes(), "code", float64(CodeInternal))
}

func contractDependencies() Dependencies {
	return Dependencies{
		Auth:      fakeRouterAuthService{},
		Users:     fakeRouterUserService{},
		Products:  fakeRouterProductService{},
		Orders:    fakeRouterOrderService{},
		Stats:     fakeRouterStatsService{},
		Readiness: func(context.Context) error { return nil },
		Logger:    slog.Default(),
		Config: config.Config{
			CORS:      config.CORS{},
			RateLimit: config.RateLimit{LoginRequestsPerSecond: 10, LoginBurst: 10},
		},
	}
}
