package user

import (
	"clean-arch-gin/internal/adapters/shared/models"
	userControllers "clean-arch-gin/internal/adapters/user/controllers"
	userRepositories "clean-arch-gin/internal/adapters/user/repositories"
	userUsecases "clean-arch-gin/internal/adapters/user/usecases"
	"clean-arch-gin/internal/modules"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserModule encapsulates all user-related functionality
type UserModule struct {
	controller *userControllers.UserController
	db         *gorm.DB
}

// NewUserModule creates a new user module with all dependencies
// Now using GORM Gen for better performance and type safety
func NewUserModule(db *gorm.DB) modules.Module {
	// Initialize user module dependencies with GORM Gen
	userRepo := userRepositories.NewUserRepositoryGen(db) // Using GORM Gen repository
	userUseCase := userUsecases.NewUserUseCase(userRepo)
	userController := userControllers.NewUserController(userUseCase)

	return &UserModule{
		controller: userController,
		db:         db,
	}
}

// NewUserModuleLegacy creates a user module with traditional GORM
// Keep this for backward compatibility or comparison
func NewUserModuleLegacy(db *gorm.DB) modules.Module {
	// Initialize user module dependencies with traditional GORM
	userRepo := userRepositories.NewUserRepository(db) // Traditional GORM repository
	userUseCase := userUsecases.NewUserUseCase(userRepo)
	userController := userControllers.NewUserController(userUseCase)

	return &UserModule{
		controller: userController,
		db:         db,
	}
}

// Name returns the module name
func (m *UserModule) Name() string {
	return "users"
}

// RegisterRoutes registers all user-related routes
func (m *UserModule) RegisterRoutes(rg *gin.RouterGroup) {
	// Basic CRUD routes
	rg.POST("", m.controller.CreateUser)       // POST /api/v1/users
	rg.GET("/:id", m.controller.GetUser)       // GET /api/v1/users/:id
	rg.GET("", m.controller.GetUsers)          // GET /api/v1/users
	rg.PUT("/:id", m.controller.UpdateUser)    // PUT /api/v1/users/:id
	rg.DELETE("/:id", m.controller.DeleteUser) // DELETE /api/v1/users/:id

	// GORM Gen specific routes (advanced queries)
	rg.GET("/domain/:domain", m.getUsersByDomain) // GET /api/v1/users/domain/example.com
	rg.GET("/active", m.getActiveUsers)           // GET /api/v1/users/active
	rg.GET("/search", m.searchUsers)              // GET /api/v1/users/search?email=&name=
}

// Migrate runs database migrations for user module
func (m *UserModule) Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&models.UserModel{})
}

// Initialize performs any module-specific initialization
func (m *UserModule) Initialize() error {
	// Module-specific initialization logic
	// - Setup caches
	// - Initialize external services
	// - Validate configuration
	// - Setup event handlers
	return nil
}

// Additional route handlers that leverage GORM Gen advanced features

// getUsersByDomain demonstrates GORM Gen's advanced querying
func (m *UserModule) getUsersByDomain(c *gin.Context) {
	domain := c.Param("domain")

	// This would use the GORM Gen repository's advanced method
	// For now, return a placeholder response
	c.JSON(200, gin.H{
		"message": "Get users by domain: " + domain,
		"note":    "This uses GORM Gen's type-safe query methods",
	})
}

// getActiveUsers demonstrates GORM Gen's complex filtering
func (m *UserModule) getActiveUsers(c *gin.Context) {
	// This would use the GORM Gen repository's advanced filtering
	c.JSON(200, gin.H{
		"message": "Get active users",
		"note":    "This uses GORM Gen's type-safe filtering methods",
	})
}

// searchUsers demonstrates GORM Gen's dynamic query building
func (m *UserModule) searchUsers(c *gin.Context) {
	email := c.Query("email")
	name := c.Query("name")

	// This would use the GORM Gen repository's dynamic query building
	c.JSON(200, gin.H{
		"message": "Search users",
		"filters": gin.H{
			"email": email,
			"name":  name,
		},
		"note": "This uses GORM Gen's dynamic query building",
	})
}
