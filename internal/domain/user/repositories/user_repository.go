package repositories

import (
	"clean-arch-gin/internal/domain/user/entities"
)

// UserRepository defines the contract for user data persistence
// This interface belongs to the domain layer and is implemented by the infrastructure layer
type UserRepository interface {
	// Basic CRUD operations
	Create(user *entities.User) error
	GetByID(id uint) (*entities.User, error)
	GetByEmail(email string) (*entities.User, error)
	GetAll(limit, offset int) ([]*entities.User, error)
	Update(user *entities.User) error
	Delete(id uint) error
	Count() (int64, error)

	// Advanced query methods (enabled by GORM Gen)
	GetUsersByEmailDomain(domain string) ([]*entities.User, error)
	GetActiveUsers() ([]*entities.User, error)
	GetUsersWithFilters(limit, offset int, email, name string) ([]*entities.User, error)
}
