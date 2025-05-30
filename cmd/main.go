package main

import (
	"log"
	"os"

	"clean-arch-gin/internal/adapters/shared/models"
	"clean-arch-gin/internal/infrastructure/config"
	"clean-arch-gin/internal/infrastructure/database"
	"clean-arch-gin/internal/modules"
	orderModule "clean-arch-gin/internal/modules/order"
	userModule "clean-arch-gin/internal/modules/user"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize database
	db, err := database.NewConnection(cfg)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Create module registry for large-scale organization
	registry := modules.NewModuleRegistry()

	// Register feature modules
	registry.Register(userModule.NewUserModule(db))
	registry.Register(orderModule.NewOrderModule(db))
	// registry.Register(productModule.NewProductModule(db))
	// registry.Register(paymentModule.NewPaymentModule(db))
	// registry.Register(inventoryModule.NewInventoryModule(db))

	// Initialize all modules
	if err := registry.InitializeAll(); err != nil {
		log.Fatal("Failed to initialize modules:", err)
	}

	// Run database migrations for all modules
	if err := registry.MigrateAll(db); err != nil {
		log.Fatal("Failed to migrate modules:", err)
	}

	// Migrate shared models (used across multiple domains)
	if err := database.AutoMigrate(db, &models.UserModel{}); err != nil {
		log.Fatal("Failed to migrate shared models:", err)
	}

	// Setup router with modular architecture
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// Health check endpoint with module status
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":      "healthy",
			"modules":     getModuleStatuses(registry),
			"description": "Domain-specific adapter architecture",
		})
	})

	// API versioning with modular routes
	v1 := r.Group("/api/v1")
	{
		// Register all module routes automatically
		registry.RegisterAllRoutes(v1)
	}

	// Future API versions can be added here
	// v2 := r.Group("/api/v2")
	// {
	//     // Register v2 routes with different module configurations
	//     // Each domain can evolve independently
	// }

	// Start server
	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Starting large-scale modular server on port %s", port)
	log.Printf("📦 Registered modules: %v", getModuleNames(registry))
	log.Printf("🏗️ Architecture: Domain-specific adapters with GORM Gen")
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// getModuleNames returns a list of registered module names
func getModuleNames(registry *modules.ModuleRegistry) []string {
	var names []string
	for _, module := range registry.GetModules() {
		names = append(names, module.Name())
	}
	return names
}

// getModuleStatuses returns the status of all modules
func getModuleStatuses(registry *modules.ModuleRegistry) map[string]string {
	statuses := make(map[string]string)
	for _, module := range registry.GetModules() {
		statuses[module.Name()] = "active"
	}
	return statuses
}
