package user

import (
	"clean-arch-gin/internal/adapters/controllers"
	"clean-arch-gin/internal/adapters/middleware"

	"github.com/gin-gonic/gin"
)

// UserRouteConfig holds dependencies for user routes
type UserRouteConfig struct {
	UserController *controllers.UserController
	AuthMiddleware *middleware.AuthMiddleware
}

// RegisterRoutes registers all user-related routes with proper organization
func RegisterRoutes(rg *gin.RouterGroup, config UserRouteConfig) {
	// Public user routes (no authentication required)
	registerPublicRoutes(rg, config)

	// Protected user routes (authentication required)
	registerProtectedRoutes(rg, config)

	// Admin user routes (admin role required)
	registerAdminRoutes(rg, config)
}

// registerPublicRoutes sets up public user routes
func registerPublicRoutes(rg *gin.RouterGroup, config UserRouteConfig) {
	public := rg.Group("/users")
	{
		// Authentication routes
		auth := public.Group("/auth")
		{
			auth.POST("/register", config.UserController.CreateUser)
			auth.POST("/login", handleLogin)                    // Placeholder
			auth.POST("/forgot-password", handleForgotPassword) // Placeholder
			auth.POST("/reset-password", handleResetPassword)   // Placeholder
		}

		// Public user information
		public.GET("/:id/public", handleGetPublicProfile) // Placeholder
	}
}

// registerProtectedRoutes sets up authenticated user routes
func registerProtectedRoutes(rg *gin.RouterGroup, config UserRouteConfig) {
	protected := rg.Group("/users")
	// Apply authentication middleware
	if config.AuthMiddleware != nil {
		protected.Use(config.AuthMiddleware.RequireAuth())
	}
	{
		// Current user routes
		me := protected.Group("/me")
		{
			me.GET("", handleGetCurrentUser) // Placeholder
			me.PUT("", config.UserController.UpdateUser)
			me.DELETE("", config.UserController.DeleteUser)
			me.GET("/profile", handleGetProfile)    // Placeholder
			me.PUT("/profile", handleUpdateProfile) // Placeholder
		}

		// User preferences
		preferences := protected.Group("/me/preferences")
		{
			preferences.GET("", handleGetPreferences)    // Placeholder
			preferences.PUT("", handleUpdatePreferences) // Placeholder
		}

		// User notifications
		notifications := protected.Group("/me/notifications")
		{
			notifications.GET("", handleGetNotifications)          // Placeholder
			notifications.PUT("/:id/read", handleMarkAsRead)       // Placeholder
			notifications.DELETE("/:id", handleDeleteNotification) // Placeholder
		}
	}
}

// registerAdminRoutes sets up admin-only user routes
func registerAdminRoutes(rg *gin.RouterGroup, config UserRouteConfig) {
	admin := rg.Group("/admin/users")
	// Apply authentication and admin role middleware
	if config.AuthMiddleware != nil {
		admin.Use(config.AuthMiddleware.RequireAuth())
		admin.Use(config.AuthMiddleware.RequireRole("admin"))
	}
	{
		// User management
		admin.GET("", config.UserController.GetUsers)
		admin.GET("/:id", config.UserController.GetUser)
		admin.PUT("/:id", handleAdminUpdateUser)     // Placeholder
		admin.DELETE("/:id", handleAdminDeleteUser)  // Placeholder
		admin.PUT("/:id/status", handleUpdateStatus) // Placeholder
		admin.PUT("/:id/role", handleUpdateRole)     // Placeholder

		// Bulk operations
		bulk := admin.Group("/bulk")
		{
			bulk.POST("/export", handleBulkExport)   // Placeholder
			bulk.POST("/import", handleBulkImport)   // Placeholder
			bulk.DELETE("/delete", handleBulkDelete) // Placeholder
		}

		// User analytics
		analytics := admin.Group("/analytics")
		{
			analytics.GET("/stats", handleUserStats)       // Placeholder
			analytics.GET("/activity", handleUserActivity) // Placeholder
			analytics.GET("/reports", handleUserReports)   // Placeholder
		}
	}
}

// RegisterV2Routes demonstrates how to handle API versioning
func RegisterV2Routes(rg *gin.RouterGroup, config UserRouteConfig) {
	v2Users := rg.Group("/users")
	{
		// V2 might have different structure or new features
		v2Users.POST("", handleCreateUserV2) // Placeholder
		v2Users.GET("/:id", handleGetUserV2) // Placeholder
		v2Users.GET("", handleGetUsersV2)    // Placeholder

		// New V2 features
		v2Users.GET("/:id/timeline", handleUserTimeline)       // Placeholder
		v2Users.GET("/:id/connections", handleUserConnections) // Placeholder
	}
}

// Placeholder handlers (would be implemented in actual controllers)
func handleLogin(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Login endpoint"})
}

func handleForgotPassword(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Forgot password endpoint"})
}

func handleResetPassword(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Reset password endpoint"})
}

func handleGetPublicProfile(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get public profile endpoint"})
}

func handleGetCurrentUser(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get current user endpoint"})
}

func handleGetProfile(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get profile endpoint"})
}

func handleUpdateProfile(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Update profile endpoint"})
}

func handleGetPreferences(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get preferences endpoint"})
}

func handleUpdatePreferences(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Update preferences endpoint"})
}

func handleGetNotifications(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get notifications endpoint"})
}

func handleMarkAsRead(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Mark as read endpoint"})
}

func handleDeleteNotification(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Delete notification endpoint"})
}

func handleAdminUpdateUser(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Admin update user endpoint"})
}

func handleAdminDeleteUser(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Admin delete user endpoint"})
}

func handleUpdateStatus(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Update status endpoint"})
}

func handleUpdateRole(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Update role endpoint"})
}

func handleBulkExport(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Bulk export endpoint"})
}

func handleBulkImport(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Bulk import endpoint"})
}

func handleBulkDelete(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Bulk delete endpoint"})
}

func handleUserStats(c *gin.Context) {
	c.JSON(200, gin.H{"message": "User stats endpoint"})
}

func handleUserActivity(c *gin.Context) {
	c.JSON(200, gin.H{"message": "User activity endpoint"})
}

func handleUserReports(c *gin.Context) {
	c.JSON(200, gin.H{"message": "User reports endpoint"})
}

func handleCreateUserV2(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Create user V2 endpoint"})
}

func handleGetUserV2(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get user V2 endpoint"})
}

func handleGetUsersV2(c *gin.Context) {
	c.JSON(200, gin.H{"message": "Get users V2 endpoint"})
}

func handleUserTimeline(c *gin.Context) {
	c.JSON(200, gin.H{"message": "User timeline endpoint"})
}

func handleUserConnections(c *gin.Context) {
	c.JSON(200, gin.H{"message": "User connections endpoint"})
}
