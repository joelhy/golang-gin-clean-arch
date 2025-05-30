# Large-Scale Clean Architecture: Domain Organization & Route Layering

## **🎯 The Challenge: Scaling Beyond Simple CRUD**

When your application grows beyond a few entities, you need strategic organization:

- **100+ domain objects** → Need clear boundaries
- **Multiple teams** → Need independent modules  
- **Complex business logic** → Need domain aggregates
- **Various contexts** → Need bounded contexts

## **🏗️ Domain Organization Strategies**

### **1. Feature-Based Organization (Recommended)**

```
internal/
├── domain/
│   ├── user/           # User bounded context
│   │   ├── entities/
│   │   │   ├── user.go
│   │   │   ├── profile.go
│   │   │   └── preferences.go
│   │   ├── repositories/
│   │   │   ├── user_repository.go
│   │   │   └── profile_repository.go
│   │   ├── usecases/
│   │   │   ├── user_usecase.go
│   │   │   └── profile_usecase.go
│   │   └── errors/
│   │       └── user_errors.go
│   │
│   ├── order/          # Order bounded context
│   │   ├── entities/
│   │   │   ├── order.go
│   │   │   ├── order_item.go
│   │   │   └── payment.go
│   │   ├── aggregates/
│   │   │   └── order_aggregate.go
│   │   ├── repositories/
│   │   │   └── order_repository.go
│   │   └── usecases/
│   │       ├── place_order_usecase.go
│   │       └── cancel_order_usecase.go
│   │
│   ├── product/        # Product bounded context
│   │   ├── entities/
│   │   │   ├── product.go
│   │   │   ├── category.go
│   │   │   └── inventory.go
│   │   ├── repositories/
│   │   └── usecases/
│   │
│   └── shared/         # Shared domain concepts
│       ├── value_objects/
│       │   ├── money.go
│       │   ├── address.go
│       │   └── email.go
│       └── events/
│           ├── domain_event.go
│           └── event_publisher.go
│
├── adapters/
│   ├── user/           # User adapters
│   │   ├── controllers/
│   │   ├── repositories/
│   │   ├── models/
│   │   └── dtos/
│   ├── order/          # Order adapters
│   │   ├── controllers/
│   │   ├── repositories/
│   │   ├── models/
│   │   └── dtos/
│   └── product/        # Product adapters
│       ├── controllers/
│       ├── repositories/
│       ├── models/
│       └── dtos/
│
└── infrastructure/
    ├── config/
    ├── database/
    ├── router/
    │   ├── user_routes.go
    │   ├── order_routes.go
    │   ├── product_routes.go
    │   └── router.go
    └── middleware/
```

### **2. Aggregate-Based Organization (DDD Approach)**

```
internal/
├── domain/
│   ├── aggregates/
│   │   ├── user_aggregate/
│   │   │   ├── user.go           # Aggregate root
│   │   │   ├── profile.go        # Entity
│   │   │   ├── preferences.go    # Value object
│   │   │   └── user_repository.go
│   │   │
│   │   ├── order_aggregate/
│   │   │   ├── order.go          # Aggregate root
│   │   │   ├── order_item.go     # Entity
│   │   │   ├── shipping.go       # Value object
│   │   │   └── order_repository.go
│   │   │
│   │   └── product_aggregate/
│   │       ├── product.go        # Aggregate root
│   │       ├── category.go       # Entity
│   │       ├── pricing.go        # Value object
│   │       └── product_repository.go
│   │
│   ├── services/          # Domain services
│   │   ├── pricing_service.go
│   │   ├── inventory_service.go
│   │   └── notification_service.go
│   │
│   └── shared/
│       ├── value_objects/
│       └── specifications/
│
└── application/
    ├── user/
    │   ├── commands/
    │   ├── queries/
    │   └── handlers/
    ├── order/
    │   ├── commands/
    │   ├── queries/
    │   └── handlers/
    └── product/
        ├── commands/
        ├── queries/
        └── handlers/
```

### **3. Layer-First Organization (Traditional)**

```
internal/
├── domain/
│   ├── entities/
│   │   ├── user/
│   │   ├── order/
│   │   └── product/
│   ├── repositories/
│   │   ├── user/
│   │   ├── order/
│   │   └── product/
│   └── usecases/
│       ├── user/
│       ├── order/
│       └── product/
│
├── adapters/
│   ├── controllers/
│   ├── repositories/
│   └── models/
│
└── infrastructure/
    ├── config/
    ├── database/
    └── router/
```

## **🚦 Route Organization Patterns**

### **1. Modular Route Organization**

```go
// internal/infrastructure/router/router.go
package router

import (
    userRoutes "clean-arch-gin/internal/infrastructure/router/user"
    orderRoutes "clean-arch-gin/internal/infrastructure/router/order"
    productRoutes "clean-arch-gin/internal/infrastructure/router/product"
    "github.com/gin-gonic/gin"
)

func NewRouter(app *di.Application) *gin.Engine {
    r := gin.New()
    
    // Add global middleware
    r.Use(gin.Logger())
    r.Use(gin.Recovery())
    r.Use(middleware.CORS())
    
    // API versioning
    v1 := r.Group("/api/v1")
    {
        // Register feature routes
        userRoutes.RegisterRoutes(v1, app.UserController, app.AuthMiddleware)
        orderRoutes.RegisterRoutes(v1, app.OrderController, app.AuthMiddleware)
        productRoutes.RegisterRoutes(v1, app.ProductController)
    }
    
    v2 := r.Group("/api/v2")
    {
        // New API version with different structure
        userRoutes.RegisterV2Routes(v2, app.UserControllerV2)
    }
    
    return r
}
```

### **2. Feature-Specific Route Files**

```go
// internal/infrastructure/router/user/user_routes.go
package user

import (
    "clean-arch-gin/internal/adapters/controllers"
    "clean-arch-gin/internal/adapters/middleware"
    "github.com/gin-gonic/gin"
)

func RegisterRoutes(rg *gin.RouterGroup, 
                   userCtrl *controllers.UserController,
                   authMiddleware *middleware.AuthMiddleware) {
    
    users := rg.Group("/users")
    {
        // Public routes
        users.POST("/register", userCtrl.Register)
        users.POST("/login", userCtrl.Login)
        users.POST("/forgot-password", userCtrl.ForgotPassword)
        
        // Protected routes
        protected := users.Group("")
        protected.Use(authMiddleware.RequireAuth())
        {
            protected.GET("/profile", userCtrl.GetProfile)
            protected.PUT("/profile", userCtrl.UpdateProfile)
            protected.DELETE("/account", userCtrl.DeleteAccount)
            
            // Admin routes
            admin := protected.Group("")
            admin.Use(authMiddleware.RequireRole("admin"))
            {
                admin.GET("", userCtrl.ListUsers)
                admin.GET("/:id", userCtrl.GetUser)
                admin.PUT("/:id/status", userCtrl.UpdateUserStatus)
            }
        }
    }
    
    // User-related sub-resources
    profiles := rg.Group("/profiles")
    profiles.Use(authMiddleware.RequireAuth())
    {
        profiles.GET("/:userId", userCtrl.GetUserProfile)
        profiles.PUT("/:userId", userCtrl.UpdateUserProfile)
    }
}
```

### **3. Resource-Based Route Organization**

```go
// internal/infrastructure/router/order/order_routes.go
package order

func RegisterRoutes(rg *gin.RouterGroup, 
                   orderCtrl *controllers.OrderController,
                   paymentCtrl *controllers.PaymentController,
                   authMiddleware *middleware.AuthMiddleware) {
    
    // Orders
    orders := rg.Group("/orders")
    orders.Use(authMiddleware.RequireAuth())
    {
        orders.POST("", orderCtrl.CreateOrder)
        orders.GET("", orderCtrl.GetUserOrders)
        orders.GET("/:id", orderCtrl.GetOrder)
        orders.PUT("/:id/cancel", orderCtrl.CancelOrder)
        
        // Order items (nested resource)
        orders.GET("/:id/items", orderCtrl.GetOrderItems)
        orders.POST("/:id/items", orderCtrl.AddOrderItem)
        orders.DELETE("/:id/items/:itemId", orderCtrl.RemoveOrderItem)
        
        // Order payments (nested resource)
        orders.GET("/:id/payments", paymentCtrl.GetOrderPayments)
        orders.POST("/:id/payments", paymentCtrl.ProcessPayment)
        orders.PUT("/:id/payments/:paymentId/refund", paymentCtrl.RefundPayment)
    }
    
    // Separate payment endpoints
    payments := rg.Group("/payments")
    payments.Use(authMiddleware.RequireAuth())
    {
        payments.GET("", paymentCtrl.GetUserPayments)
        payments.GET("/:id", paymentCtrl.GetPayment)
        payments.POST("/:id/webhook", paymentCtrl.HandleWebhook)
    }
}
```

## **📦 Modular Architecture Implementation**

### **1. Module Registration Pattern**

```go
// internal/modules/module.go
package modules

import (
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

type Module interface {
    Name() string
    RegisterRoutes(rg *gin.RouterGroup)
    Migrate(db *gorm.DB) error
    Initialize() error
}

type ModuleRegistry struct {
    modules []Module
}

func NewModuleRegistry() *ModuleRegistry {
    return &ModuleRegistry{
        modules: make([]Module, 0),
    }
}

func (r *ModuleRegistry) Register(module Module) {
    r.modules = append(r.modules, module)
}

func (r *ModuleRegistry) InitializeAll() error {
    for _, module := range r.modules {
        if err := module.Initialize(); err != nil {
            return fmt.Errorf("failed to initialize module %s: %w", module.Name(), err)
        }
    }
    return nil
}

func (r *ModuleRegistry) RegisterAllRoutes(rg *gin.RouterGroup) {
    for _, module := range r.modules {
        moduleGroup := rg.Group("/" + strings.ToLower(module.Name()))
        module.RegisterRoutes(moduleGroup)
    }
}

func (r *ModuleRegistry) MigrateAll(db *gorm.DB) error {
    for _, module := range r.modules {
        if err := module.Migrate(db); err != nil {
            return fmt.Errorf("failed to migrate module %s: %w", module.Name(), err)
        }
    }
    return nil
}
```

### **2. User Module Implementation**

```go
// internal/modules/user/user_module.go
package user

import (
    "clean-arch-gin/internal/adapters/controllers"
    "clean-arch-gin/internal/adapters/models"
    "clean-arch-gin/internal/adapters/repositories"
    "clean-arch-gin/internal/adapters/usecases"
    "clean-arch-gin/internal/modules"
    
    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

type UserModule struct {
    controller *controllers.UserController
    db         *gorm.DB
}

func NewUserModule(db *gorm.DB) modules.Module {
    // Initialize dependencies
    userRepo := repositories.NewUserRepository(db)
    userUseCase := usecases.NewUserUseCase(userRepo)
    userController := controllers.NewUserController(userUseCase)
    
    return &UserModule{
        controller: userController,
        db:         db,
    }
}

func (m *UserModule) Name() string {
    return "User"
}

func (m *UserModule) RegisterRoutes(rg *gin.RouterGroup) {
    users := rg.Group("/users")
    {
        users.POST("", m.controller.CreateUser)
        users.GET("/:id", m.controller.GetUser)
        users.GET("", m.controller.GetUsers)
        users.PUT("/:id", m.controller.UpdateUser)
        users.DELETE("/:id", m.controller.DeleteUser)
    }
}

func (m *UserModule) Migrate(db *gorm.DB) error {
    return db.AutoMigrate(&models.UserModel{})
}

func (m *UserModule) Initialize() error {
    // Module-specific initialization logic
    return nil
}
```

### **3. Main Application Assembly**

```go
// cmd/main.go
package main

import (
    "clean-arch-gin/internal/infrastructure/config"
    "clean-arch-gin/internal/infrastructure/database"
    "clean-arch-gin/internal/modules"
    userModule "clean-arch-gin/internal/modules/user"
    orderModule "clean-arch-gin/internal/modules/order"
    productModule "clean-arch-gin/internal/modules/product"
    
    "github.com/gin-gonic/gin"
)

func main() {
    // Initialize configuration and database
    cfg := config.NewConfig()
    db, err := database.NewConnection(cfg)
    if err != nil {
        log.Fatal("Failed to connect to database:", err)
    }
    
    // Create module registry
    registry := modules.NewModuleRegistry()
    
    // Register modules
    registry.Register(userModule.NewUserModule(db))
    registry.Register(orderModule.NewOrderModule(db))
    registry.Register(productModule.NewProductModule(db))
    
    // Initialize all modules
    if err := registry.InitializeAll(); err != nil {
        log.Fatal("Failed to initialize modules:", err)
    }
    
    // Run migrations
    if err := registry.MigrateAll(db); err != nil {
        log.Fatal("Failed to migrate modules:", err)
    }
    
    // Setup router
    r := gin.New()
    r.Use(gin.Logger())
    r.Use(gin.Recovery())
    
    // Register all module routes
    api := r.Group("/api/v1")
    registry.RegisterAllRoutes(api)
    
    // Start server
    r.Run(":8080")
}
```

## **🎛️ Advanced Patterns**

### **1. CQRS (Command Query Responsibility Segregation)**

```go
// internal/application/user/commands/
type CreateUserCommand struct {
    Email    string
    Name     string
    Password string
}

type CreateUserHandler struct {
    userRepo repositories.UserRepository
}

func (h *CreateUserHandler) Handle(cmd CreateUserCommand) (*entities.User, error) {
    // Command handling logic
}

// internal/application/user/queries/
type GetUserQuery struct {
    ID uint
}

type GetUserHandler struct {
    userRepo repositories.UserRepository
}

func (h *GetUserHandler) Handle(query GetUserQuery) (*entities.User, error) {
    // Query handling logic
}
```

### **2. Event-Driven Architecture**

```go
// internal/domain/shared/events/domain_event.go
type DomainEvent interface {
    EventName() string
    OccurredOn() time.Time
    EventData() interface{}
}

// internal/domain/user/events/user_created_event.go
type UserCreatedEvent struct {
    UserID    uint
    Email     string
    occurredOn time.Time
}

func (e UserCreatedEvent) EventName() string {
    return "user.created"
}
```

## **📊 Choosing the Right Organization**

### **Feature-Based (Recommended for most cases)**
✅ **Use when:**
- Medium to large applications
- Multiple development teams
- Clear business domains
- Need independent deployments

### **Aggregate-Based (DDD)**
✅ **Use when:**
- Complex business logic
- Rich domain models
- Event-driven architecture
- Domain experts involved

### **Layer-First**
✅ **Use when:**
- Small to medium applications
- Single development team
- Simple business logic
- Rapid development needed

## **🚀 Migration Strategy**

1. **Start Simple**: Begin with layer-first organization
2. **Identify Boundaries**: As you grow, identify natural feature boundaries
3. **Extract Modules**: Move related components into feature modules
4. **Add Abstractions**: Introduce module interfaces and registries
5. **Scale Gradually**: Add CQRS, events, etc. as needed

## **📝 Best Practices**

1. **Keep modules cohesive** - High cohesion within, low coupling between
2. **Define clear interfaces** - Modules should communicate through well-defined contracts
3. **Avoid circular dependencies** - Use dependency injection and interfaces
4. **Version your APIs** - Allow for independent module evolution
5. **Monitor boundaries** - Use tools to enforce architectural constraints 