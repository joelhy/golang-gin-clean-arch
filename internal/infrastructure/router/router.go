package router

import (
	"clean-arch-gin/internal/adapters/controllers"
	"clean-arch-gin/internal/adapters/middleware"
	"clean-arch-gin/internal/di"
	"clean-arch-gin/internal/infrastructure/config"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NewRouter creates and configures the HTTP router using dependency injection
func NewRouter(db *gorm.DB, cfg *config.Config) *gin.Engine {
	// Set Gin mode
	gin.SetMode(cfg.Server.Mode)

	// Create Gin router
	r := gin.New()

	// Add middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())

	// Initialize dependencies using Wire
	app := di.InitializeApplication(db, cfg)

	// Setup routes
	setupRoutes(r, app.UserController)

	return r
}

// setupRoutes configures all application routes
func setupRoutes(r *gin.Engine, userController *controllers.UserController) {
	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "OK",
			"message": "Server is running",
		})
	})

	// API v1 routes
	v1 := r.Group("/api/v1")
	{
		// User routes
		users := v1.Group("/users")
		{
			users.POST("/", userController.CreateUser)
			users.GET("/:id", userController.GetUser)
			users.GET("/", userController.GetUsers)
			users.PUT("/:id", userController.UpdateUser)
			users.DELETE("/:id", userController.DeleteUser)
		}
	}
}
