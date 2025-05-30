//go:build ignore

package main

import (
	"clean-arch-gin/internal/adapters/shared/models"

	"gorm.io/driver/mysql"
	"gorm.io/gen"
	"gorm.io/gorm"
)

// generateCode generates type-safe database code using GORM Gen
func main() {
	// Initialize GORM Gen
	g := gen.NewGenerator(gen.Config{
		OutPath:           "./internal/infrastructure/database/query", // Output directory
		Mode:              gen.WithoutContext | gen.WithDefaultQuery | gen.WithQueryInterface,
		FieldNullable:     true,
		FieldCoverable:    false,
		FieldSignable:     false,
		FieldWithIndexTag: false,
		FieldWithTypeTag:  true,
	})

	// Connect to database for schema introspection
	// This can use a test database or the actual database
	db, err := gorm.Open(mysql.Open("user:password@tcp(localhost:3306)/clean_arch_db?charset=utf8mb4&parseTime=True&loc=Local"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database for code generation")
	}

	// Set the database connection
	g.UseDB(db)

	// Generate basic CRUD operations for models
	// This creates type-safe query methods
	g.ApplyBasic(
		models.UserModel{},
		// Add other models here as they're created
		// models.OrderModel{},
		// models.ProductModel{},
	)

	// Generate custom methods for complex queries
	user := g.GenerateModel("users")

	// Apply custom interface methods for User
	g.ApplyInterface(func(UserQueryInterface) {}, user)

	// Execute code generation
	g.Execute()
}

// UserQueryInterface defines custom query methods for User
// These will be implemented by GORM Gen
type UserQueryInterface interface {
	// Custom query: Find users by email domain
	FindByEmailDomain(domain string) ([]*models.UserModel, error)
	// Custom query: Find active users (non-deleted)
	FindActiveUsers() ([]*models.UserModel, error)
	// Custom query: Count users by status
	CountByCreatedDate(date string) (int64, error)
	// Custom query: Find users with pagination and filters
	FindWithFilters(limit, offset int, email, name string) ([]*models.UserModel, error)
}
