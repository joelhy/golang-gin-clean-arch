//go:build wireinject

package main

import (
	"log/slog"

	"github.com/google/wire"
)

func initializeAdminDependencies(logger *slog.Logger) (adminDependencies, error) {
	wire.Build(
		provideAdminDependencies,
		provideMigrationRunner,
		provideBootstrapper,
		providePasswordReader,
	)
	return adminDependencies{}, nil
}
