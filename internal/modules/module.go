package modules

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module represents a feature module in the application
type Module interface {
	Name() string
	RegisterRoutes(rg *gin.RouterGroup)
	Migrate(db *gorm.DB) error
	Initialize() error
}

// ModuleRegistry manages all application modules
type ModuleRegistry struct {
	modules []Module
}

// NewModuleRegistry creates a new module registry
func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{
		modules: make([]Module, 0),
	}
}

// Register adds a module to the registry
func (r *ModuleRegistry) Register(module Module) {
	r.modules = append(r.modules, module)
}

// InitializeAll initializes all registered modules
func (r *ModuleRegistry) InitializeAll() error {
	for _, module := range r.modules {
		if err := module.Initialize(); err != nil {
			return fmt.Errorf("failed to initialize module %s: %w", module.Name(), err)
		}
	}
	return nil
}

// RegisterAllRoutes registers routes for all modules
func (r *ModuleRegistry) RegisterAllRoutes(rg *gin.RouterGroup) {
	for _, module := range r.modules {
		moduleGroup := rg.Group("/" + strings.ToLower(module.Name()))
		module.RegisterRoutes(moduleGroup)
	}
}

// MigrateAll runs database migrations for all modules
func (r *ModuleRegistry) MigrateAll(db *gorm.DB) error {
	for _, module := range r.modules {
		if err := module.Migrate(db); err != nil {
			return fmt.Errorf("failed to migrate module %s: %w", module.Name(), err)
		}
	}
	return nil
}

// GetModules returns all registered modules
func (r *ModuleRegistry) GetModules() []Module {
	return r.modules
}

// GetModuleByName returns a module by name
func (r *ModuleRegistry) GetModuleByName(name string) Module {
	for _, module := range r.modules {
		if strings.EqualFold(module.Name(), name) {
			return module
		}
	}
	return nil
}
