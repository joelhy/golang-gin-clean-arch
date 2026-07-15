package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clean-arch-gin/config"
)

func TestNewHTTPServerUsesConfiguredTimeouts(t *testing.T) {
	cfg := testConfig()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	server := newHTTPServer(cfg.HTTP, handler)

	if server.ReadHeaderTimeout != cfg.HTTP.ReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, cfg.HTTP.ReadHeaderTimeout)
	}
	if server.ReadTimeout != cfg.HTTP.ReadTimeout {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, cfg.HTTP.ReadTimeout)
	}
	if server.WriteTimeout != cfg.HTTP.WriteTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, cfg.HTTP.WriteTimeout)
	}
	if server.IdleTimeout != cfg.HTTP.IdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, cfg.HTTP.IdleTimeout)
	}
}

func TestRunStopsOnContextCancellationAndClosesOnce(t *testing.T) {
	listener := newSignalListener(t)
	ctx, cancel := context.WithCancel(t.Context())
	var closes atomic.Int32
	done := make(chan error, 1)

	go func() {
		done <- run(ctx, serverRuntime{
			Config:   testConfig(),
			Handler:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			Listener: listener,
			Closer: func() error {
				closes.Add(1)
				return nil
			},
		})
	}()

	<-listener.accepting
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run() did not stop after context cancellation")
	}
	if closes.Load() != 1 {
		t.Fatalf("closes = %d, want 1", closes.Load())
	}
}

func TestRunReturnsStartupErrorAndCloseErrors(t *testing.T) {
	startErr := errors.New("bind failed")
	closeErr := errors.New("close failed")
	err := run(t.Context(), serverRuntime{
		Config:   testConfig(),
		Handler:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		Listener: failingListener{err: startErr},
		Closer:   func() error { return closeErr },
	})

	if !errors.Is(err, startErr) {
		t.Fatalf("run() error = %v, want startup error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("run() error = %v, want close error", err)
	}
}

func TestRunReturnsShutdownAndCloseErrors(t *testing.T) {
	listener := newSignalListener(t)
	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	closeErr := errors.New("background close failed")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- run(ctx, serverRuntime{
			Config: config.Config{HTTP: config.HTTP{
				Address:           listener.Addr().String(),
				ReadHeaderTimeout: time.Second,
				ReadTimeout:       time.Second,
				WriteTimeout:      time.Second,
				IdleTimeout:       time.Second,
				ShutdownTimeout:   time.Nanosecond,
			}},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(handlerStarted)
				<-releaseHandler
			}),
			Listener: listener,
			Closer:   func() error { return closeErr },
		})
	}()

	<-listener.accepting
	clientErr := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String())
		if resp != nil {
			_ = resp.Body.Close()
		}
		clientErr <- err
	}()

	<-handlerStarted
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(time.Second):
		t.Fatal("run() did not return after shutdown timeout")
	}
	close(releaseHandler)
	<-clientErr

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run() error = %v, want shutdown deadline", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("run() error = %v, want close error", err)
	}
}

func TestInitializeRuntimeFromDatabaseClosesDBWhenProviderFails(t *testing.T) {
	closeErr := errors.New("close failed")
	var closes atomic.Int32

	_, err := initializeRuntimeFromDatabase(t.Context(), testConfig(), slog.Default(), failingListener{}, databaseResource{
		DB: nil,
		Close: func() error {
			closes.Add(1)
			return closeErr
		},
	})

	if err == nil {
		t.Fatal("initializeRuntimeFromDatabase() error = nil, want provider failure")
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("initializeRuntimeFromDatabase() error = %v, want close error", err)
	}
	if closes.Load() != 1 {
		t.Fatalf("closes = %d, want 1", closes.Load())
	}
}

func testConfig() config.Config {
	return config.Config{HTTP: config.HTTP{
		Address:           "127.0.0.1:0",
		ReadHeaderTimeout: 11 * time.Millisecond,
		ReadTimeout:       12 * time.Millisecond,
		WriteTimeout:      13 * time.Millisecond,
		IdleTimeout:       14 * time.Millisecond,
		ShutdownTimeout:   time.Second,
		MaxBodyBytes:      1024,
	}}
}

type signalListener struct {
	net.Listener
	accepting chan struct{}
	once      sync.Once
}

func newSignalListener(t *testing.T) *signalListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return &signalListener{Listener: ln, accepting: make(chan struct{})}
}

func (l *signalListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accepting) })
	return l.Listener.Accept()
}

type failingListener struct {
	err error
}

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (l failingListener) Close() error              { return nil }
func (l failingListener) Addr() net.Addr            { return dummyAddr("127.0.0.1:0") }

type dummyAddr string

func (a dummyAddr) Network() string { return "tcp" }
func (a dummyAddr) String() string  { return string(a) }
