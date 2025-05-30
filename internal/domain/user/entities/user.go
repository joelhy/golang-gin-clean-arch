package entities

import (
	"time"

	sharedEntities "clean-arch-gin/internal/domain/shared/entities"
)

// User represents the pure domain entity
// No external dependencies - follows Clean Architecture principles
type User struct {
	ID        uint
	Email     string
	Name      string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time // Pure time pointer, no GORM dependency
}

// NewUser creates a new user with validation
func NewUser(email, name, password string) (*User, error) {
	if email == "" {
		return nil, ErrInvalidEmail
	}
	if name == "" {
		return nil, ErrInvalidName
	}
	if password == "" {
		return nil, ErrInvalidPassword
	}

	return &User{
		Email:     email,
		Name:      name,
		Password:  password,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// IsDeleted checks if the user is soft deleted
func (u *User) IsDeleted() bool {
	return u.DeletedAt != nil
}

// MarkAsDeleted soft deletes the user
func (u *User) MarkAsDeleted() {
	now := time.Now()
	u.DeletedAt = &now
	u.UpdatedAt = now
}

// UpdateInfo updates user information
func (u *User) UpdateInfo(name, email string) {
	if name != "" {
		u.Name = name
	}
	if email != "" {
		u.Email = email
	}
	u.UpdatedAt = time.Now()
}

// ChangePassword updates the user's password with validation
func (u *User) ChangePassword(newPassword string) error {
	if newPassword == "" {
		return ErrInvalidPassword
	}

	u.Password = newPassword
	u.UpdatedAt = time.Now()
	return nil
}

// Activate activates a soft-deleted user
func (u *User) Activate() {
	u.DeletedAt = nil
	u.UpdatedAt = time.Now()
}

// Domain errors for user
var (
	ErrInvalidEmail    = sharedEntities.DomainError{Message: "email is required"}
	ErrInvalidName     = sharedEntities.DomainError{Message: "name is required"}
	ErrInvalidPassword = sharedEntities.DomainError{Message: "password is required"}
	ErrUserNotFound    = sharedEntities.DomainError{Message: "user not found"}
	ErrEmailExists     = sharedEntities.DomainError{Message: "user with this email already exists"}
)
