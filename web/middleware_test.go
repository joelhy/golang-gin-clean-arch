package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareRequestIDPropagation(t *testing.T) {
	engine := gin.New()
	engine.Use(RequestID())
	engine.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(headerRequestID, "req-123")
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if got := rec.Header().Get(headerRequestID); got != "req-123" {
		t.Fatalf("response request id = %q, want %q", got, "req-123")
	}
}

func TestMiddlewareRecoveryReturnsInternalEnvelopeAndLogsStack(t *testing.T) {
	logger, logBuffer := testLogger()

	engine := gin.New()
	engine.Use(RequestID(), Recovery(logger))
	engine.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got["code"] != float64(CodeInternal) {
		t.Fatalf("body = %#v", got)
	}
	if _, ok := got["stack"]; ok {
		t.Fatalf("response leaked stack: %#v", got)
	}
	if !strings.Contains(logBuffer.String(), "panic recovered") || !strings.Contains(logBuffer.String(), "goroutine") {
		t.Fatalf("recovery log = %q, want panic message and stack", logBuffer.String())
	}
	if rec.Header().Get(headerRequestID) == "" {
		t.Fatal("recovery response must include request id header")
	}
}

func TestMiddlewareSecurityHeaders(t *testing.T) {
	engine := gin.New()
	engine.Use(SecurityHeaders())
	engine.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options = %q", rec.Header().Get("X-Frame-Options"))
	}
}

func TestMiddlewareAccessLogRedactsCredentials(t *testing.T) {
	logger, logBuffer := testLogger()

	engine := gin.New()
	engine.Use(RequestID(), AccessLog(logger))
	engine.GET("/users/:id", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "http://user:pass@example.com/users/42", nil)
	req.Header.Set("Authorization", "Bearer top-secret")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	logs := logBuffer.String()
	if !strings.Contains(logs, "/users/:id") {
		t.Fatalf("access log = %q, want normalized path", logs)
	}
	if strings.Contains(logs, "top-secret") || strings.Contains(logs, "user:pass") {
		t.Fatalf("access log leaked credentials: %q", logs)
	}
}

func TestCORSAllowsConfiguredOrigin(t *testing.T) {
	engine := gin.New()
	engine.Use(CORS(CORSOptions{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowCredentials: true,
	}))
	engine.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q", got)
	}
}

func TestCORSRejectsDisallowedPreflight(t *testing.T) {
	engine := gin.New()
	engine.Use(CORS(CORSOptions{AllowedOrigins: []string{"https://app.example.com"}}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	assertEnvelopeFailure(t, rec.Body.Bytes())
}

func TestMiddlewareTrustedProxiesSupport(t *testing.T) {
	engine := gin.New()
	if err := UseStandard(engine, MiddlewareOptions{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TrustedProxies: []string{"10.0.0.0/8"},
	}); err != nil {
		t.Fatalf("UseStandard() error = %v", err)
	}
	engine.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if body := strings.TrimSpace(rec.Body.String()); body != "198.51.100.20" {
		t.Fatalf("client ip = %q, want %q", body, "198.51.100.20")
	}
}

func TestMiddlewareMaxBodyBytes(t *testing.T) {
	engine := gin.New()
	engine.Use(MaxBodyBytes(8))
	engine.POST("/", func(c *gin.Context) {
		_, _ = io.Copy(io.Discard, c.Request.Body)
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"alice"}`))
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	assertEnvelopeFailure(t, rec.Body.Bytes())
}

func TestRateLimitByIPAndAccount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	limiter := NewLoginRateLimiter(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), RateLimitOptions{
		RequestsPerSecond: 1,
		Burst:             1,
		MaxEntries:        32,
		CleanupInterval:   10 * time.Millisecond,
	})

	engine := gin.New()
	engine.SetTrustedProxies([]string{"10.0.0.0/8"})
	engine.Use(limiter.Middleware())
	engine.POST("/login", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		c.String(http.StatusOK, string(body))
	})

	req1 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"user@example.com"}`))
	req1.RemoteAddr = "10.1.2.3:1234"
	req1.Header.Set("X-Forwarded-For", "198.51.100.20")
	rec1 := httptest.NewRecorder()
	engine.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK || strings.TrimSpace(rec1.Body.String()) != `{"email":"user@example.com"}` {
		t.Fatalf("first response = %d %q", rec1.Code, rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"user@example.com"}`))
	req2.RemoteAddr = "10.1.2.3:1234"
	req2.Header.Set("X-Forwarded-For", "198.51.100.20")
	rec2 := httptest.NewRecorder()
	engine.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}
	assertEnvelopeFailure(t, rec2.Body.Bytes())
}

func TestRateLimitCleanupStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	limiter := NewLoginRateLimiter(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), RateLimitOptions{
		RequestsPerSecond: 1,
		Burst:             1,
		MaxEntries:        8,
		CleanupInterval:   10 * time.Millisecond,
	})

	cancel()

	select {
	case <-limiter.Done():
	case <-time.After(time.Second):
		t.Fatal("cleanup goroutine did not stop after context cancellation")
	}
}

func TestMiddlewareCanceledContextReturns499(t *testing.T) {
	engine := gin.New()
	engine.Use(ContextCancellation())
	engine.GET("/", func(c *gin.Context) {
		<-c.Request.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != 499 {
		t.Fatalf("status = %d, want 499", rec.Code)
	}
	assertEnvelopeFailure(t, rec.Body.Bytes())
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func assertEnvelopeFailure(t *testing.T, body []byte) {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v; body=%q", err, string(body))
	}
	if got["code"] == nil {
		t.Fatalf("body missing code field: %#v", got)
	}
	if code, ok := got["code"].(float64); !ok || code == 0 {
		t.Fatalf("code = %#v, want nonzero integer", got["code"])
	}
	if message, ok := got["message"].(string); !ok || strings.TrimSpace(message) == "" {
		t.Fatalf("message = %#v, want nonempty string", got["message"])
	}
	for _, forbidden := range []string{"success", "meta", "details", "request_id"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("body contains forbidden key %q: %#v", forbidden, got)
		}
	}
}
