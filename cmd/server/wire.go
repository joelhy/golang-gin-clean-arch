//go:build wireinject

package main

import (
	"context"
	"log/slog"
	"net"

	"clean-arch-gin/config"
	"clean-arch-gin/mysqlstore"
	"clean-arch-gin/order"
	"clean-arch-gin/product"
	"clean-arch-gin/reporting"
	"clean-arch-gin/security"
	"clean-arch-gin/user"
	"github.com/google/wire"
)

func initializeRuntime(ctx context.Context, cfg config.Config, logger *slog.Logger, listener net.Listener) (serverRuntime, error) {
	wire.Build(
		openDatabase,
		gormDB,
		provideServerRuntime,
		provideRouter,
		providePasswordManager,
		provideJWT,
		provideRefreshGenerator,
		provideIDGenerator,
		provideRefreshTTL,
		provideUserClock,
		provideProductClock,
		provideOrderClock,
		provideNumberGenerator,
		mysqlstore.NewUserStore,
		mysqlstore.NewSessionStore,
		mysqlstore.NewProductStore,
		mysqlstore.NewOrderStore,
		mysqlstore.NewReportingStore,
		user.NewService,
		user.NewAuthService,
		product.NewService,
		order.NewService,
		reporting.NewService,
		wire.Bind(new(user.Store), new(*mysqlstore.UserStore)),
		wire.Bind(new(user.AuthUserStore), new(*mysqlstore.UserStore)),
		wire.Bind(new(user.SessionStore), new(*mysqlstore.SessionStore)),
		wire.Bind(new(user.Passwords), new(*security.PasswordManager)),
		wire.Bind(new(user.AccessTokens), new(*security.JWT)),
		wire.Bind(new(user.RefreshTokens), new(*security.RefreshGenerator)),
		wire.Bind(new(user.IDGenerator), new(*security.RandomIDGenerator)),
		wire.Bind(new(product.Store), new(*mysqlstore.ProductStore)),
		wire.Bind(new(order.Transactor), new(*mysqlstore.OrderStore)),
		wire.Bind(new(order.Reader), new(*mysqlstore.OrderStore)),
		wire.Bind(new(reporting.Store), new(*mysqlstore.ReportingStore)),
	)
	return serverRuntime{}, nil
}
