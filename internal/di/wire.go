//go:build wireinject
// +build wireinject

package di

import (
	"clean-arch-gin/internal/adapters/controllers"
	"clean-arch-gin/internal/adapters/repositories"
	"clean-arch-gin/internal/adapters/usecases"
	"clean-arch-gin/internal/infrastructure/config"

	"github.com/google/wire"
	"gorm.io/gorm"
)

// InitializeUserController initializes a user controller with all dependencies
func InitializeUserController(db *gorm.DB, cfg *config.Config) *controllers.UserController {
	wire.Build(
		repositories.NewUserRepository,
		usecases.NewUserUseCase,
		controllers.NewUserController,
	)
	return &controllers.UserController{}
}

// Application represents the entire application with all dependencies
type Application struct {
	UserController *controllers.UserController
	Config         *config.Config
}

// InitializeApplication initializes the entire application
func InitializeApplication(db *gorm.DB, cfg *config.Config) *Application {
	wire.Build(
		repositories.NewUserRepository,
		usecases.NewUserUseCase,
		controllers.NewUserController,
		wire.Struct(new(Application), "*"),
	)
	return &Application{}
}
