package models

import (
	"time"

	userEntities "clean-arch-gin/internal/domain/user/entities"

	"gorm.io/gorm"
)

// UserModel represents the GORM model for users
// This is infrastructure layer concern - contains GORM tags and database-specific logic
type UserModel struct {
	ID        uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	Email     string         `gorm:"uniqueIndex;not null;size:255" json:"email"`
	Name      string         `gorm:"not null;size:255" json:"name"`
	Password  string         `gorm:"not null;size:255" json:"-"` // Excluded from JSON
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName sets the table name for GORM
func (UserModel) TableName() string {
	return "users"
}

// ToDomainEntity converts GORM model to domain entity
// This maintains clean architecture boundaries
func (u *UserModel) ToDomainEntity() *userEntities.User {
	var deletedAt *time.Time
	if u.DeletedAt.Valid {
		deletedAt = &u.DeletedAt.Time
	}

	return &userEntities.User{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		Password:  u.Password,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
		DeletedAt: deletedAt,
	}
}

// NewUserModelFromEntity creates GORM model from domain entity
// This maintains clean architecture boundaries
func NewUserModelFromEntity(user *userEntities.User) *UserModel {
	userModel := &UserModel{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		Password:  user.Password,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}

	if user.DeletedAt != nil {
		userModel.DeletedAt = gorm.DeletedAt{
			Time:  *user.DeletedAt,
			Valid: true,
		}
	}

	return userModel
}
