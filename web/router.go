package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"clean-arch-gin/config"
	"clean-arch-gin/user"
	"github.com/gin-gonic/gin"
)

type AuthService interface {
	authRegistrationService
	authService
}

type UserService interface {
	userService
}

type ProductService interface {
	productService
}

type OrderService interface {
	orderService
}

type StatsService interface {
	statsService
}

type Dependencies struct {
	Auth      AuthService
	Users     UserService
	Products  ProductService
	Orders    OrderService
	Stats     StatsService
	Readiness func(context.Context) error
	Logger    *slog.Logger
	Config    config.Config
}

func NewRouter(ctx context.Context, deps Dependencies) (*gin.Engine, error) {
	switch {
	case deps.Auth == nil:
		return nil, errors.New("new router: auth service is required")
	case deps.Users == nil:
		return nil, errors.New("new router: user service is required")
	case deps.Products == nil:
		return nil, errors.New("new router: product service is required")
	case deps.Orders == nil:
		return nil, errors.New("new router: order service is required")
	case deps.Stats == nil:
		return nil, errors.New("new router: stats service is required")
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(RequestID())
	router.Use(Recovery(logger))
	router.Use(SecurityHeaders())
	router.Use(CORS(CORSOptions{
		AllowedOrigins:   deps.Config.CORS.AllowedOrigins,
		AllowCredentials: deps.Config.CORS.AllowCredentials,
	}))
	router.Use(AccessLog(logger))

	authHandler := NewAuthHandler(deps.Auth, deps.Auth, nil)
	userHandler := NewUserHandler(deps.Users)
	productHandler := NewProductHandler(deps.Products)
	orderHandler := NewOrderHandler(deps.Orders)
	statsHandler := NewStatsHandler(deps.Stats)

	loginLimiter := NewLoginRateLimiter(ctx, logger, RateLimitOptions{
		RequestsPerSecond: deps.Config.RateLimit.LoginRequestsPerSecond,
		Burst:             deps.Config.RateLimit.LoginBurst,
		MaxEntries:        1024,
		CleanupInterval:   time.Minute,
	})

	v1 := router.Group("/api/v1")
	authGroup := v1.Group("/auth")
	authGroup.POST("/register", authHandler.Register)
	authGroup.POST("/login", loginLimiter.Middleware(), authHandler.Login)
	authGroup.POST("/refresh", authHandler.Refresh)
	authGroup.POST("/logout", authHandler.Authenticated(), authHandler.Logout)

	v1.GET("/users/me", authHandler.Authenticated(), userHandler.Me)
	v1.PATCH("/users/me", authHandler.Authenticated(), userHandler.UpdateMe)

	v1.GET("/products", productHandler.ListPublic)
	v1.GET("/products/:id", productHandler.GetPublic)

	v1.POST("/orders", authHandler.Authenticated(), orderHandler.Create)
	v1.GET("/orders", authHandler.Authenticated(), orderHandler.ListCustomer)
	v1.GET("/orders/:id", authHandler.Authenticated(), orderHandler.GetCustomer)
	v1.POST("/orders/:id/cancel", authHandler.Authenticated(), orderHandler.CancelCustomer)

	admin := v1.Group("/admin", authHandler.Authenticated())
	admin.GET("/users", authHandler.RequirePermission(user.PermissionUsersRead), userHandler.List)
	admin.GET("/users/:id", authHandler.RequirePermission(user.PermissionUsersRead), userHandler.GetAdmin)
	admin.PATCH("/users/:id/status", authHandler.RequirePermission(user.PermissionUsersWrite), userHandler.SetStatus)
	admin.PUT("/users/:id/roles", authHandler.RequirePermission(user.PermissionUsersRoles), userHandler.ReplaceRoles)

	admin.POST("/products", authHandler.RequirePermission(user.PermissionProductsWrite), productHandler.Create)
	admin.PATCH("/products/:id", authHandler.RequirePermission(user.PermissionProductsWrite), productHandler.Update)
	admin.POST("/products/:id/stock-adjustments", authHandler.RequirePermission(user.PermissionProductsStock), productHandler.AdjustStock)

	admin.GET("/orders", authHandler.RequirePermission(user.PermissionOrdersReadAll), orderHandler.ListAdmin)
	admin.POST("/orders/:id/confirm", authHandler.RequirePermission(user.PermissionOrdersManage), orderHandler.Confirm)
	admin.POST("/orders/:id/ship", authHandler.RequirePermission(user.PermissionOrdersManage), orderHandler.Ship)
	admin.POST("/orders/:id/deliver", authHandler.RequirePermission(user.PermissionOrdersManage), orderHandler.Deliver)
	admin.POST("/orders/:id/cancel", authHandler.RequirePermission(user.PermissionOrdersManage), orderHandler.CancelAdmin)

	admin.GET("/stats", authHandler.RequirePermission(user.PermissionStatsRead), statsHandler.Get)

	router.GET("/health/live", func(c *gin.Context) {
		writeSuccess(c, http.StatusOK, gin.H{"status": "live"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		readiness := deps.Readiness
		if readiness == nil {
			writeSuccess(c, http.StatusOK, gin.H{"status": "ready"})
			return
		}
		// Readiness uses a short child context so a degraded database cannot pin
		// every probe goroutine until the client's own transport timeout expires.
		checkCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := readiness(checkCtx); err != nil {
			writeFailure(c, http.StatusServiceUnavailable, Problem{Code: CodeInternal, Message: "service unavailable"})
			return
		}
		writeSuccess(c, http.StatusOK, gin.H{"status": "ready"})
	})

	router.NoRoute(func(c *gin.Context) {
		writeFailure(c, http.StatusNotFound, Problem{Code: CodeInternal, Message: "route not found"})
	})
	router.NoMethod(func(c *gin.Context) {
		writeFailure(c, http.StatusMethodNotAllowed, Problem{Code: CodeInternal, Message: "method not allowed"})
	})

	return router, nil
}
