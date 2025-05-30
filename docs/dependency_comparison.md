# Dependency Injection: Manual vs Framework Comparison

## Manual Dependency Injection (Before)

```go
// internal/infrastructure/router/router.go
func NewRouter(db *gorm.DB, cfg *config.Config) *gin.Engine {
    // Manual wiring - lots of boilerplate
    userRepo := repositories.NewUserRepository(db)
    userUseCase := usecases.NewUserUseCase(userRepo)
    userController := controllers.NewUserController(userUseCase)

    // If we add more features, this becomes unwieldy:
    // postRepo := repositories.NewPostRepository(db)
    // commentRepo := repositories.NewCommentRepository(db)
    // postUseCase := usecases.NewPostUseCase(postRepo, commentRepo, userRepo)
    // postController := controllers.NewPostController(postUseCase)
    // ... many more lines

    setupRoutes(r, userController)
    return r
}
```

**Problems:**
- ❌ **Boilerplate grows exponentially** with features
- ❌ **Constructor changes** require updates everywhere
- ❌ **Hard to test** the entire dependency tree
- ❌ **No lifecycle management** (singletons, etc.)
- ❌ **Dependencies mixed** with routing logic

## Wire Framework (After)

```go
// internal/di/wire.go
//go:build wireinject

func InitializeApplication(db *gorm.DB, cfg *config.Config) *Application {
    wire.Build(
        repositories.NewUserRepository,
        usecases.NewUserUseCase,
        controllers.NewUserController,
        // Adding new features is just one line:
        // repositories.NewPostRepository,
        // usecases.NewPostUseCase,
        // controllers.NewPostController,
        wire.Struct(new(Application), "*"),
    )
    return &Application{}
}
```

```go
// internal/infrastructure/router/router.go
func NewRouter(db *gorm.DB, cfg *config.Config) *gin.Engine {
    // Clean, simple initialization
    app := di.InitializeApplication(db, cfg)
    setupRoutes(r, app.UserController)
    return r
}
```

**Benefits:**
- ✅ **Compile-time safety** - catches wiring errors at build time
- ✅ **Minimal boilerplate** - one line per provider
- ✅ **Easy testing** - can generate test-specific dependency graphs
- ✅ **Clear separation** - DI logic separated from business logic
- ✅ **Automatic wiring** - Wire figures out the dependency graph

## Generated Code (wire_gen.go)

Wire automatically generates this efficient code:

```go
func InitializeApplication(db *gorm.DB, cfg *config.Config) *Application {
    userRepository := repositories.NewUserRepository(db)
    userUseCase := usecases.NewUserUseCase(userRepository)
    userController := controllers.NewUserController(userUseCase)
    application := &Application{
        UserController: userController,
        Config:         cfg,
    }
    return application
}
```

## Testing Benefits

### Manual DI Testing
```go
func TestController(t *testing.T) {
    // Have to manually create entire chain
    mockRepo := &MockUserRepository{}
    useCase := usecases.NewUserUseCase(mockRepo)
    controller := controllers.NewUserController(useCase)

    // Test controller...
}
```

### Wire DI Testing
```go
// Create test-specific injector
func InitializeTestApplication() *Application {
    wire.Build(
        NewMockUserRepository,  // Swap real repo for mock
        usecases.NewUserUseCase,
        controllers.NewUserController,
        wire.Struct(new(Application), "*"),
    )
    return &Application{}
}

func TestController(t *testing.T) {
    app := InitializeTestApplication()
    // Test with clean, isolated dependencies
}
```

## When to Use DI Frameworks

### ✅ **Use DI Framework When:**
- Complex dependency graphs (5+ services)
- Multiple environments (dev, test, prod with different implementations)
- Need lifecycle management (singletons, request-scoped)
- Team prefers declarative over imperative DI
- Want compile-time safety

### ❌ **Stick with Manual DI When:**
- Simple applications (1-3 services)
- Team prefers explicit, visible dependencies
- Minimal external dependencies preference
- Learning/teaching clean architecture

## Best Practices with Wire

1. **Keep injectors simple** - one per major component
2. **Use provider sets** for related dependencies
3. **Separate test injectors** from production ones
4. **Generate code in CI/CD** to catch wiring errors early
5. **Document provider responsibilities** clearly

## Conclusion

Wire provides **significant advantages** for medium to large applications while maintaining Go's philosophy of explicit dependencies and compile-time safety. The choice depends on your team's preferences and application complexity.
