package main

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"clean-arch-gin/config"
	"clean-arch-gin/mysqlstore"
	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/reporting"
	"clean-arch-gin/security"
	"clean-arch-gin/user"
	"clean-arch-gin/web"
	"gorm.io/gorm"
)

type serverRuntime struct {
	Config   config.Config
	Handler  http.Handler
	Listener net.Listener
	Closer   func() error
}

func run(ctx context.Context, app serverRuntime) error {
	if ctx == nil {
		return errors.New("run server: context is required")
	}
	if app.Handler == nil {
		return errors.New("run server: handler is required")
	}
	if app.Listener == nil {
		return errors.New("run server: listener is required")
	}
	server := newHTTPServer(app.Config.HTTP, app.Handler)
	serveErr := make(chan error, 1)
	go func() {
		err := server.Serve(app.Listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	var err error
	select {
	case err = <-serveErr:
	case <-ctx.Done():
		// The parent context is already canceled here; graceful shutdown needs a
		// fresh bounded context so in-flight requests get the configured drain window.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), app.Config.HTTP.ShutdownTimeout)
		shutdownErr := server.Shutdown(shutdownCtx)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
		}
		err = errors.Join(shutdownErr, <-serveErr)
	}
	return errors.Join(err, closeRuntime(app.Closer))
}

func newHTTPServer(cfg config.HTTP, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
}

func closeRuntime(closeFn func() error) error {
	if closeFn == nil {
		return nil
	}
	return closeFn()
}

type databaseResource struct {
	DB    *gorm.DB
	Close func() error
}

func openDatabase(ctx context.Context, cfg config.Config, logger *slog.Logger) (databaseResource, error) {
	db, closeDB, err := mysqlstore.Open(ctx, cfg.DB, logger)
	if err != nil {
		return databaseResource{}, err
	}
	return databaseResource{DB: db, Close: closeDB}, nil
}

func gormDB(resource databaseResource) *gorm.DB {
	return resource.DB
}

func listen(cfg config.Config) (net.Listener, error) {
	return net.Listen("tcp", cfg.HTTP.Address)
}

func provideServerRuntime(cfg config.Config, handler http.Handler, listener net.Listener, db databaseResource) serverRuntime {
	return serverRuntime{
		Config:   cfg,
		Handler:  handler,
		Listener: listener,
		Closer:   db.Close,
	}
}

func provideRouter(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	db databaseResource,
	auth *user.AuthService,
	users *user.Service,
	products *product.Service,
	orders *order.Service,
	stats *reporting.Service,
) (http.Handler, error) {
	return web.NewRouter(ctx, web.Dependencies{
		Auth:     authDependency{registration: users, auth: auth},
		Users:    users,
		Products: products,
		Orders:   orders,
		Stats:    stats,
		Readiness: func(ctx context.Context) error {
			sqlDB, err := db.DB.DB()
			if err != nil {
				return err
			}
			return sqlDB.PingContext(ctx)
		},
		Logger: logger,
		Config: cfg,
	})
}

type authDependency struct {
	registration *user.Service
	auth         *user.AuthService
}

func (d authDependency) Register(ctx context.Context, input user.RegisterInput) (*user.User, error) {
	return d.registration.Register(ctx, input)
}

func (d authDependency) Login(ctx context.Context, input user.LoginInput) (user.Tokens, error) {
	return d.auth.Login(ctx, input)
}

func (d authDependency) Refresh(ctx context.Context, rawRefresh string) (user.Tokens, error) {
	return d.auth.Refresh(ctx, rawRefresh)
}

func (d authDependency) Logout(ctx context.Context, identity user.Identity) error {
	return d.auth.Logout(ctx, identity)
}

func (d authDependency) Authenticate(ctx context.Context, rawAccess string) (user.Identity, error) {
	return d.auth.Authenticate(ctx, rawAccess)
}

func (d authDependency) Authorize(ctx context.Context, identity user.Identity, permission string) error {
	return d.auth.Authorize(ctx, identity, permission)
}

func providePasswordManager(cfg config.Config) (*security.PasswordManager, error) {
	return security.NewPasswordManager(security.PasswordConfig{
		Memory:      cfg.Password.Memory,
		Iterations:  cfg.Password.Iterations,
		Parallelism: cfg.Password.Parallelism,
		SaltLength:  cfg.Password.SaltLength,
		KeyLength:   cfg.Password.KeyLength,
	}, rand.Reader)
}

func provideJWT(cfg config.Config) (*security.JWT, error) {
	return security.NewJWT(security.JWTConfig{
		Key:      []byte(cfg.JWT.Key),
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
		TTL:      cfg.JWT.AccessTTL,
		Leeway:   cfg.JWT.Leeway,
	})
}

func provideRefreshGenerator() (*security.RefreshGenerator, error) {
	return security.NewRefreshGenerator(rand.Reader)
}

func provideIDGenerator() (*security.RandomIDGenerator, error) {
	return security.NewRandomIDGenerator(rand.Reader)
}

func provideRefreshTTL(cfg config.Config) time.Duration {
	return cfg.JWT.RefreshTTL
}

func provideUserClock() user.Clock {
	return user.SystemClock{}
}

func provideProductClock() product.Clock {
	return product.SystemClock{}
}

func provideOrderClock() order.Clock {
	return order.SystemClock{}
}

func provideNumberGenerator() order.NumberGenerator {
	return order.NewNumberGenerator(rand.Reader)
}
