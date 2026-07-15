package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type testClock struct {
	now time.Time
}

func (c testClock) Now() time.Time { return c.now }

type fakeAuthRegistrationService struct {
	registerFn func(context.Context, user.RegisterInput) (*user.User, error)
}

func (f fakeAuthRegistrationService) Register(ctx context.Context, input user.RegisterInput) (*user.User, error) {
	return f.registerFn(ctx, input)
}

type fakeAuthService struct {
	loginFn        func(context.Context, user.LoginInput) (user.Tokens, error)
	refreshFn      func(context.Context, string) (user.Tokens, error)
	logoutFn       func(context.Context, user.Identity) error
	authenticateFn func(context.Context, string) (user.Identity, error)
	authorizeFn    func(context.Context, user.Identity, string) error
}

func (f fakeAuthService) Login(ctx context.Context, input user.LoginInput) (user.Tokens, error) {
	return f.loginFn(ctx, input)
}

func (f fakeAuthService) Refresh(ctx context.Context, rawRefresh string) (user.Tokens, error) {
	return f.refreshFn(ctx, rawRefresh)
}

func (f fakeAuthService) Logout(ctx context.Context, identity user.Identity) error {
	return f.logoutFn(ctx, identity)
}

func (f fakeAuthService) Authenticate(ctx context.Context, rawAccess string) (user.Identity, error) {
	return f.authenticateFn(ctx, rawAccess)
}

func (f fakeAuthService) Authorize(ctx context.Context, identity user.Identity, permission string) error {
	return f.authorizeFn(ctx, identity, permission)
}

func TestAuthHandlerRegisterCreated(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	now := time.Date(2026, 7, 15, 10, 0, 0, 123, time.FixedZone("CST", 8*3600))
	handler := NewAuthHandler(
		fakeAuthRegistrationService{
			registerFn: func(_ context.Context, input user.RegisterInput) (*user.User, error) {
				if input.Email != "user@example.com" || input.Name != "Alice" || input.Password != "correct horse battery staple" {
					t.Fatalf("register input = %+v", input)
				}
				return &user.User{
					ID:        7,
					Email:     input.Email,
					Name:      input.Name,
					Status:    user.StatusActive,
					Version:   1,
					CreatedAt: now,
					UpdatedAt: now,
					Roles:     []user.Role{{Name: user.RoleCustomer}},
				}, nil
			},
		},
		fakeAuthService{},
		testClock{now: now.UTC()},
	)

	router := gin.New()
	router.POST("/api/v1/auth/register", handler.Register)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"user@example.com","name":"Alice","password":"correct horse battery staple"}`)

	assertStatusCode(t, rec, http.StatusCreated)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeOK))
	assertJSONPath(t, rec.Body.Bytes(), "data.id", float64(7))
	assertJSONPath(t, rec.Body.Bytes(), "data.email", "user@example.com")
	assertJSONPath(t, rec.Body.Bytes(), "data.created_at", now.UTC().Format(time.RFC3339Nano))
	assertJSONMissing(t, rec.Body.Bytes(), "data.password_hash")
}

func TestAuthHandlerRegisterDuplicateEmail(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewAuthHandler(
		fakeAuthRegistrationService{
			registerFn: func(_ context.Context, _ user.RegisterInput) (*user.User, error) {
				return nil, user.ErrEmailExists
			},
		},
		fakeAuthService{},
		testClock{},
	)

	router := gin.New()
	router.POST("/api/v1/auth/register", handler.Register)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"user@example.com","name":"Alice","password":"correct horse battery staple"}`)

	assertStatusCode(t, rec, http.StatusConflict)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeEmailExists))
}

func TestAuthHandlerRegisterMalformedJSON(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewAuthHandler(fakeAuthRegistrationService{}, fakeAuthService{}, testClock{})

	router := gin.New()
	router.POST("/api/v1/auth/register", handler.Register)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/auth/register", `{"email":"user@example.com"`)

	assertStatusCode(t, rec, http.StatusBadRequest)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeMalformedJSON))
}

func TestAuthHandlerLoginResponse(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	now := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			loginFn: func(_ context.Context, input user.LoginInput) (user.Tokens, error) {
				if input.Email != "user@example.com" || input.Password != "correct horse battery staple" {
					t.Fatalf("login input = %+v", input)
				}
				return user.Tokens{
					AccessToken:      "access",
					RefreshToken:     "refresh",
					AccessExpiresAt:  now.Add(15 * time.Minute),
					RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
				}, nil
			},
		},
		testClock{now: now},
	)

	router := gin.New()
	router.POST("/api/v1/auth/login", handler.Login)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"user@example.com","password":"correct horse battery staple"}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.token_type", "Bearer")
	assertJSONPath(t, rec.Body.Bytes(), "data.access_token", "access")
	assertJSONPath(t, rec.Body.Bytes(), "data.refresh_token", "refresh")
	assertJSONPath(t, rec.Body.Bytes(), "data.expires_in", float64(900))
	assertJSONPath(t, rec.Body.Bytes(), "data.refresh_expires_in", float64(2592000))
}

func TestAuthHandlerLoginInvalidCredentials(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			loginFn: func(_ context.Context, _ user.LoginInput) (user.Tokens, error) {
				return user.Tokens{}, user.ErrInvalidCredentials
			},
		},
		testClock{},
	)

	router := gin.New()
	router.POST("/api/v1/auth/login", handler.Login)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"user@example.com","password":"wrong password"}`)

	assertStatusCode(t, rec, http.StatusUnauthorized)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeAuthentication))
}

func TestAuthHandlerRefreshRotation(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	now := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			refreshFn: func(_ context.Context, rawRefresh string) (user.Tokens, error) {
				if rawRefresh != "refresh-1" {
					t.Fatalf("refresh token = %q", rawRefresh)
				}
				return user.Tokens{
					AccessToken:      "access-2",
					RefreshToken:     "refresh-2",
					AccessExpiresAt:  now.Add(5 * time.Minute),
					RefreshExpiresAt: now.Add(24 * time.Hour),
				}, nil
			},
		},
		testClock{now: now},
	)

	router := gin.New()
	router.POST("/api/v1/auth/refresh", handler.Refresh)

	rec := performJSON(t, router, http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token":"refresh-1"}`)

	assertStatusCode(t, rec, http.StatusOK)
	assertJSONPath(t, rec.Body.Bytes(), "data.refresh_token", "refresh-2")
	assertJSONPath(t, rec.Body.Bytes(), "data.expires_in", float64(300))
}

func TestAuthHandlerLogoutUsesAuthenticatedIdentity(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	var gotIdentity user.Identity
	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			logoutFn: func(_ context.Context, identity user.Identity) error {
				gotIdentity = identity
				return nil
			},
			authenticateFn: func(_ context.Context, rawAccess string) (user.Identity, error) {
				if rawAccess != "access-1" {
					t.Fatalf("raw access = %q", rawAccess)
				}
				return user.Identity{UserID: 8, SessionID: "session-1", TokenID: "token-1"}, nil
			},
		},
		testClock{},
	)

	router := gin.New()
	router.POST("/api/v1/auth/logout", handler.Authenticated(), handler.Logout)

	rec := performRequest(t, router, http.MethodPost, "/api/v1/auth/logout", nil, map[string][]string{
		"Authorization": {"Bearer access-1"},
	})

	assertStatusCode(t, rec, http.StatusOK)
	if gotIdentity.UserID != 8 || gotIdentity.SessionID != "session-1" {
		t.Fatalf("logout identity = %+v", gotIdentity)
	}
}

func TestAuthHandlerAuthenticatedRejectsMissingMalformedAndDuplicateBearer(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			authenticateFn: func(_ context.Context, _ string) (user.Identity, error) {
				t.Fatal("Authenticate must not be called for invalid Authorization headers")
				return user.Identity{}, nil
			},
		},
		testClock{},
	)

	router := gin.New()
	router.GET("/protected", handler.Authenticated(), func(c *gin.Context) {
		writeSuccess(c, http.StatusOK, gin.H{"ok": true})
	})

	cases := []struct {
		name    string
		headers map[string][]string
	}{
		{name: "missing"},
		{name: "malformed", headers: map[string][]string{"Authorization": {"Token nope"}}},
		{name: "duplicate", headers: map[string][]string{"Authorization": {"Bearer one", "Bearer two"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performRequest(t, router, http.MethodGet, "/protected", nil, tc.headers)
			assertStatusCode(t, rec, http.StatusUnauthorized)
			assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeAuthentication))
		})
	}
}

func TestAuthHandlerAuthenticatedMapsRevokedSession(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			authenticateFn: func(_ context.Context, _ string) (user.Identity, error) {
				return user.Identity{}, user.ErrSessionRevoked
			},
		},
		testClock{},
	)

	router := gin.New()
	router.GET("/protected", handler.Authenticated(), func(c *gin.Context) {
		writeSuccess(c, http.StatusOK, gin.H{"ok": true})
	})

	rec := performRequest(t, router, http.MethodGet, "/protected", nil, map[string][]string{
		"Authorization": {"Bearer access-1"},
	})

	assertStatusCode(t, rec, http.StatusUnauthorized)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodeSessionExpired))
}

func TestAuthHandlerRequirePermissionDeniesBeforeHandler(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	called := false
	handler := NewAuthHandler(
		fakeAuthRegistrationService{},
		fakeAuthService{
			authenticateFn: func(_ context.Context, _ string) (user.Identity, error) {
				return user.Identity{UserID: 9, SessionID: "session-1", TokenID: "token-1"}, nil
			},
			authorizeFn: func(_ context.Context, identity user.Identity, permission string) error {
				if identity.UserID != 9 || permission != user.PermissionProductsWrite {
					t.Fatalf("authorize identity=%+v permission=%q", identity, permission)
				}
				return user.ErrForbidden
			},
		},
		testClock{},
	)

	router := gin.New()
	router.GET("/admin", handler.Authenticated(), handler.RequirePermission(user.PermissionProductsWrite), func(c *gin.Context) {
		called = true
		writeSuccess(c, http.StatusOK, gin.H{"ok": true})
	})

	rec := performRequest(t, router, http.MethodGet, "/admin", nil, map[string][]string{
		"Authorization": {"Bearer access-1"},
	})

	assertStatusCode(t, rec, http.StatusForbidden)
	assertJSONPath(t, rec.Body.Bytes(), "code", float64(CodePermission))
	if called {
		t.Fatal("final handler must not run after permission denial")
	}
}

func performJSON(t *testing.T, router http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return performRequest(t, router, method, path, bytes.NewBufferString(body), map[string][]string{
		"Content-Type": {"application/json"},
	})
}

func performRequest(t *testing.T, router http.Handler, method string, path string, body *bytes.Buffer, headers map[string][]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Buffer
	if body == nil {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = body
	}

	req := httptest.NewRequest(method, path, reader)
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertStatusCode(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, want, rec.Body.String())
	}
}

func assertJSONPath(t *testing.T, body []byte, path string, want any) {
	t.Helper()

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v; body=%q", err, string(body))
	}

	got, ok := jsonLookup(payload, path)
	if !ok {
		t.Fatalf("path %q missing in %s", path, string(body))
	}
	if got != want {
		t.Fatalf("path %q = %#v, want %#v", path, got, want)
	}
}

func assertJSONMissing(t *testing.T, body []byte, path string) {
	t.Helper()

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v; body=%q", err, string(body))
	}

	if _, ok := jsonLookup(payload, path); ok {
		t.Fatalf("path %q unexpectedly present in %s", path, string(body))
	}
}

func jsonLookup(payload any, path string) (any, bool) {
	current := payload
	for _, segment := range strings.Split(path, ".") {
		if index, err := strconv.Atoi(segment); err == nil {
			items, ok := current.([]any)
			if !ok || index < 0 || index >= len(items) {
				return nil, false
			}
			current = items[index]
			continue
		}

		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
