package web

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const headerRequestID = "X-Request-ID"

type contextKey string

const requestIDContextKey contextKey = "request_id"

type CORSOptions struct {
	AllowedOrigins   []string
	AllowCredentials bool
}

type MiddlewareOptions struct {
	Logger         *slog.Logger
	CORS           CORSOptions
	MaxBodyBytes   int64
	TrustedProxies []string
}

type RateLimitOptions struct {
	RequestsPerSecond float64
	Burst             int
	MaxEntries        int
	CleanupInterval   time.Duration
}

func UseStandard(engine *gin.Engine, opts MiddlewareOptions) error {
	if err := engine.SetTrustedProxies(opts.TrustedProxies); err != nil {
		return err
	}

	engine.Use(RequestID())
	engine.Use(AccessLog(opts.Logger))
	engine.Use(Recovery(opts.Logger))
	engine.Use(SecurityHeaders())
	engine.Use(ContextCancellation())
	if opts.MaxBodyBytes > 0 {
		engine.Use(MaxBodyBytes(opts.MaxBodyBytes))
	}
	engine.Use(CORS(opts.CORS))
	return nil
}

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(headerRequestID))
		if requestID == "" {
			requestID = newRequestID()
		}
		c.Set(string(requestIDContextKey), requestID)
		c.Writer.Header().Set(headerRequestID, requestID)
		c.Next()
	}
}

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				// Stack traces stay in logs only so failure bodies cannot leak internals to callers.
				logger.Error("panic recovered",
					slog.Any("panic", recovered),
					slog.String("request_id", requestIDFromContext(c)),
					slog.String("stack", string(debug.Stack())),
				)
				if !c.Writer.Written() {
					writeInternal(c)
				}
			}
		}()
		c.Next()
	}
}

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := c.Writer.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "no-referrer")
		headers.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Next()
	}
}

func CORS(opts CORSOptions) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(opts.AllowedOrigins))
	for _, origin := range opts.AllowedOrigins {
		allowed[origin] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		_, originAllowed := allowed[origin]
		isPreflight := c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != ""
		if !originAllowed {
			if isPreflight {
				// Preflight denial still needs the same JSON envelope as other middleware errors so clients can handle it uniformly.
				writeFailure(c, http.StatusForbidden, Problem{
					Code:    CodePermission,
					Message: "origin not allowed",
				})
				return
			}
			c.Next()
			return
		}

		headers := c.Writer.Header()
		headers.Set("Vary", "Origin")
		headers.Set("Access-Control-Allow-Origin", origin)
		if opts.AllowCredentials {
			headers.Set("Access-Control-Allow-Credentials", "true")
		}

		if isPreflight {
			headers.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			headers.Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-ID")
			headers.Set("Access-Control-Max-Age", "600")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func MaxBodyBytes(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil {
			c.Next()
			return
		}
		// Content-Length can fail fast, but chunked bodies still need MaxBytesReader to enforce the cap while streaming.
		if c.Request.ContentLength > limit && limit > 0 {
			writeFailure(c, http.StatusRequestEntityTooLarge, Problem{
				Code:    CodeMalformedJSON,
				Message: "request body too large",
			})
			return
		}

		originalWriter := c.Writer
		// Chunked or otherwise unknown-length bodies can trip the limit only after downstream starts reading.
		// Buffering that downstream response lets middleware replace an ignored read error with the standard 413 envelope.
		bufferedWriter := newBufferedResponseWriter(originalWriter)
		if c.Request.ContentLength < 0 {
			c.Writer = bufferedWriter
		}

		body := &maxBodyReadCloser{
			ReadCloser: http.MaxBytesReader(originalWriter, c.Request.Body, limit),
		}
		c.Request.Body = body
		c.Next()
		c.Writer = originalWriter

		if c.Request.ContentLength < 0 {
			// Drain after the handler returns so chunked or otherwise unknown-length bodies still trip the
			// max-bytes sentinel even when downstream code never read the payload at all.
			_, _ = io.Copy(io.Discard, body)
			if body.exceededLimit() && (!bufferedWriter.Written() || bufferedWriter.Status() < http.StatusBadRequest) {
				writeFailure(c, http.StatusRequestEntityTooLarge, Problem{
					Code:    CodeMalformedJSON,
					Message: "request body too large",
				})
				return
			}
			bufferedWriter.Commit()
		}

		if err := c.Request.Body.Close(); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			return
		}
	}
}

func ContextCancellation() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if c.Writer.Written() {
			return
		}
		if errors.Is(c.Request.Context().Err(), context.Canceled) {
			// A canceled request is a transport-level abort, but the client still needs the standard failure envelope.
			writeFailure(c, 499, Problem{
				Code:    CodeInternal,
				Message: "request canceled",
			})
		}
	}
}

func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		logger.Info("http request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int("bytes", c.Writer.Size()),
			slog.Duration("duration", time.Since(start)),
			slog.String("request_id", requestIDFromContext(c)),
		)
	}
}

type LoginRateLimiter struct {
	logger          *slog.Logger
	rateLimit       rate.Limit
	burst           int
	maxEntries      int
	cleanupInterval time.Duration

	mu      sync.Mutex
	entries map[string]*rateEntry
	done    chan struct{}
}

type rateEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewLoginRateLimiter(ctx context.Context, logger *slog.Logger, opts RateLimitOptions) *LoginRateLimiter {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 1024
	}
	if opts.CleanupInterval <= 0 {
		opts.CleanupInterval = time.Minute
	}

	limiter := &LoginRateLimiter{
		logger:          logger,
		rateLimit:       rate.Limit(opts.RequestsPerSecond),
		burst:           opts.Burst,
		maxEntries:      opts.MaxEntries,
		cleanupInterval: opts.CleanupInterval,
		entries:         make(map[string]*rateEntry),
		done:            make(chan struct{}),
	}

	go limiter.cleanupLoop(ctx)
	return limiter
}

func (l *LoginRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		account := loginAccount(c.Request)

		if !l.allow("ip:" + ip) {
			writeFailure(c, http.StatusTooManyRequests, Problem{
				Code:    CodeAuthentication,
				Message: "too many login attempts",
			})
			return
		}
		if account != "" && !l.allow("account:"+strings.ToLower(account)) {
			writeFailure(c, http.StatusTooManyRequests, Problem{
				Code:    CodeAuthentication,
				Message: "too many login attempts",
			})
			return
		}
		c.Next()
	}
}

func (l *LoginRateLimiter) Done() <-chan struct{} {
	return l.done
}

func (l *LoginRateLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= l.maxEntries {
		l.dropOldestLocked()
	}

	entry, ok := l.entries[key]
	if !ok {
		entry = &rateEntry{limiter: rate.NewLimiter(l.rateLimit, l.burst)}
		l.entries[key] = entry
	}
	entry.lastSeen = now
	return entry.limiter.Allow()
}

func (l *LoginRateLimiter) cleanupLoop(ctx context.Context) {
	defer close(l.done)

	ticker := time.NewTicker(l.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.cleanup(time.Now().Add(-5 * l.cleanupInterval))
		}
	}
}

func (l *LoginRateLimiter) cleanup(cutoff time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, entry := range l.entries {
		if entry.lastSeen.Before(cutoff) {
			delete(l.entries, key)
		}
	}
}

func (l *LoginRateLimiter) dropOldestLocked() {
	var (
		oldestKey string
		oldestAt  time.Time
	)
	for key, entry := range l.entries {
		if oldestKey == "" || entry.lastSeen.Before(oldestAt) {
			oldestKey = key
			oldestAt = entry.lastSeen
		}
	}
	if oldestKey != "" {
		delete(l.entries, oldestKey)
	}
}

func loginAccount(r *http.Request) string {
	if email := strings.TrimSpace(r.URL.Query().Get("email")); email != "" {
		return email
	}
	if account := strings.TrimSpace(r.URL.Query().Get("account")); account != "" {
		return account
	}

	if r.Body == nil {
		return ""
	}

	// The rate limiter must restore the body after peeking so downstream strict decoders still see the original payload.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Email   string `json:"email"`
		Account string `json:"account"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if payload.Email != "" {
		return payload.Email
	}
	return payload.Account
}

func requestIDFromContext(c *gin.Context) string {
	if value, ok := c.Get(string(requestIDContextKey)); ok {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return ""
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(raw[:])
}

type maxBodyReadCloser struct {
	io.ReadCloser
	exceeded bool
}

func (r *maxBodyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		r.exceeded = true
	}
	return n, err
}

func (r *maxBodyReadCloser) exceededLimit() bool {
	return r.exceeded
}

type bufferedResponseWriter struct {
	base   gin.ResponseWriter
	header http.Header
	body   bytes.Buffer
	status int
	size   int
}

func newBufferedResponseWriter(base gin.ResponseWriter) *bufferedResponseWriter {
	header := make(http.Header, len(base.Header()))
	for key, values := range base.Header() {
		header[key] = append([]string(nil), values...)
	}
	return &bufferedResponseWriter{
		base:   base,
		header: header,
		status: http.StatusOK,
		size:   -1,
	}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(code int) {
	if code > 0 {
		w.status = code
	}
}

func (w *bufferedResponseWriter) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
	}
}

func (w *bufferedResponseWriter) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	n, err := w.body.Write(data)
	w.size += n
	return n, err
}

func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	w.WriteHeaderNow()
	n, err := w.body.WriteString(s)
	w.size += n
	return n, err
}

func (w *bufferedResponseWriter) Status() int {
	return w.status
}

func (w *bufferedResponseWriter) Size() int {
	return w.size
}

func (w *bufferedResponseWriter) Written() bool {
	return w.size >= 0
}

func (w *bufferedResponseWriter) Flush() {}

func (w *bufferedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.base.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *bufferedResponseWriter) CloseNotify() <-chan bool {
	return w.base.CloseNotify()
}

func (w *bufferedResponseWriter) Pusher() http.Pusher {
	return w.base.Pusher()
}

func (w *bufferedResponseWriter) Commit() {
	dst := w.base.Header()
	for key := range dst {
		delete(dst, key)
	}
	for key, values := range w.header {
		dst[key] = append([]string(nil), values...)
	}

	if !w.Written() {
		if w.status != http.StatusOK {
			w.base.WriteHeader(w.status)
			w.base.WriteHeaderNow()
		}
		return
	}

	w.base.WriteHeader(w.status)
	w.base.WriteHeaderNow()
	if w.body.Len() == 0 {
		return
	}
	_, _ = w.base.Write(w.body.Bytes())
}
