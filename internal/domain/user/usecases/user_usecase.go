package usecases

import (
	"clean-arch-gin/internal/domain/user/entities"
)

// UserUseCase defines the business logic operations for users
// This interface belongs to the domain layer
type UserUseCase interface {
	CreateUser(email, name, password string) (*entities.User, error)
	GetUser(id uint) (*entities.User, error)
	GetUsers(limit, offset int) ([]*entities.User, error)
	UpdateUser(id uint, email, name string) (*entities.User, error)
	DeleteUser(id uint) error
}
