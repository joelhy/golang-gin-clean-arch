// Package mysqlstore provides MySQL-backed persistence adapters.
package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"clean-arch-gin/config"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type poolConfigurer interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxLifetime(time.Duration)
	SetConnMaxIdleTime(time.Duration)
}

type gormOpener func(string) (*gorm.DB, error)
type sqlOpener func(string, string) (*sql.DB, error)

var (
	// ErrInvalidConfig identifies caller-controlled configuration rejected before opening a connection.
	ErrInvalidConfig = errors.New("invalid database configuration")
	// ErrInitialize identifies a sanitized driver or GORM initialization failure.
	ErrInitialize = errors.New("database initialization failed")
	// ErrSQLPool identifies failure to obtain GORM's underlying database/sql pool.
	ErrSQLPool = errors.New("database SQL pool unavailable")
	// ErrPing identifies failure to establish a usable connection.
	ErrPing = errors.New("database ping failed")
	// ErrClose identifies a sanitized driver shutdown failure.
	ErrClose = errors.New("database close failed")
)

// Open establishes a context-checked GORM connection and configures its SQL pool.
func Open(ctx context.Context, cfg config.Database, logger *slog.Logger) (*gorm.DB, func() error, error) {
	return open(ctx, cfg, logger, openGORM)
}

// OpenMigrations opens a dedicated SQL pool capable of executing trusted embedded migration batches.
func OpenMigrations(ctx context.Context, cfg config.Database) (*sql.DB, func() error, error) {
	return openMigrations(ctx, cfg, sql.Open)
}

func open(ctx context.Context, cfg config.Database, logger *slog.Logger, opener gormOpener) (*gorm.DB, func() error, error) {
	if ctx == nil {
		return nil, nil, errors.New("open mysql application database: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("open mysql database: %w", err)
	}
	if opener == nil {
		return nil, nil, fmt.Errorf("open mysql application database: opener is required: %w", ErrInitialize)
	}

	dsn, err := databaseDSN(cfg, false)
	if err != nil {
		return nil, nil, fmt.Errorf("open mysql application database: %w", ErrInvalidConfig)
	}
	gormDB, err := opener(dsn)
	if err != nil {
		closeGORM(gormDB)
		// Preserve a stable errors.Is category without wrapping the unsafe driver
		// error, which may embed the full password-bearing DSN in its text.
		return nil, nil, fmt.Errorf("open mysql application database: %w", ErrInitialize)
	}
	if gormDB == nil {
		return nil, nil, fmt.Errorf("open mysql application database: no database returned: %w", ErrInitialize)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("open mysql application database: %w", ErrSQLPool)
	}
	// Pool limits are applied before the first ping so even startup and recovery
	// traffic cannot briefly exceed the database's configured connection budget.
	configurePool(sqlDB, cfg)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("open mysql application database: %w", ErrPing)
	}

	// GORM initializes with a silent logger so connection errors are returned once,
	// at this boundary. Only a successfully opened DB receives the injected logger.
	gormDB.Config.Logger = newGORMLogger(logger)

	return gormDB, idempotentClose(sqlDB, "application"), nil
}

func openMigrations(ctx context.Context, cfg config.Database, opener sqlOpener) (*sql.DB, func() error, error) {
	if ctx == nil {
		return nil, nil, errors.New("open mysql migration database: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("open mysql migration database: %w", err)
	}
	if opener == nil {
		return nil, nil, fmt.Errorf("open mysql migration database: opener is required: %w", ErrInitialize)
	}

	dsn, err := databaseDSN(cfg, true)
	if err != nil {
		return nil, nil, fmt.Errorf("open mysql migration database: %w", ErrInvalidConfig)
	}
	db, err := opener("mysql", dsn)
	if err != nil {
		if db != nil {
			_ = db.Close()
		}
		// Migration driver errors receive the same category-preserving redaction;
		// callers can branch with errors.Is without exposing connection details.
		return nil, nil, fmt.Errorf("open mysql migration database: %w", ErrInitialize)
	}
	if db == nil {
		return nil, nil, fmt.Errorf("open mysql migration database: no database returned: %w", ErrInitialize)
	}

	configurePool(db, cfg)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("open mysql migration database: %w", ErrPing)
	}
	return db, idempotentClose(db, "migration"), nil
}

func openGORM(dsn string) (*gorm.DB, error) {
	return gorm.Open(mysql.New(mysql.Config{
		DSN:                       dsn,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               gormlogger.Discard,
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
}

func databaseDSN(cfg config.Database, multiStatements bool) (string, error) {
	if err := validateDatabaseConfig(cfg); err != nil {
		return "", err
	}
	driverConfig, err := mysqlconfig.ParseDSN(cfg.DSN())
	if err != nil {
		return "", err
	}
	// Multi-statements are enabled only by OpenMigrations for trusted embedded SQL.
	// The application pool always disables both features to reduce injection impact.
	driverConfig.MultiStatements = multiStatements
	driverConfig.InterpolateParams = false
	return driverConfig.FormatDSN(), nil
}

func validateDatabaseConfig(cfg config.Database) error {
	// Open can be called without config.Load, so the database boundary repeats only
	// the invariants needed to prevent unsafe/unbounded pool construction.
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(cfg.User) == "" || strings.TrimSpace(cfg.Name) == "" {
		return ErrInvalidConfig
	}
	if cfg.MaxOpen <= 0 || cfg.MaxIdle < 0 || cfg.MaxIdle > cfg.MaxOpen {
		return ErrInvalidConfig
	}
	if cfg.ConnMaxLifetime <= 0 || cfg.ConnMaxIdleTime <= 0 {
		return ErrInvalidConfig
	}
	return nil
}

func configurePool(pool poolConfigurer, cfg config.Database) {
	pool.SetMaxOpenConns(cfg.MaxOpen)
	pool.SetMaxIdleConns(cfg.MaxIdle)
	pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
}

func idempotentClose(db *sql.DB, poolName string) func() error {
	var closeOnce sync.Once
	var closeErr error
	return func() error {
		closeOnce.Do(func() {
			if err := db.Close(); err != nil {
				// The category remains inspectable while the unsafe third-party close
				// text is discarded because it may contain connection details.
				closeErr = fmt.Errorf("close mysql %s database: %w", poolName, ErrClose)
			}
		})
		return closeErr
	}
}

func newGORMLogger(logger *slog.Logger) gormlogger.Interface {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	// ParameterizedQueries keeps hashes, credentials, and other bound values out of
	// SQL diagnostics while preserving slow-query visibility through injected slog.
	return gormlogger.NewSlogLogger(logger, gormlogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		LogLevel:                  gormlogger.Warn,
		Colorful:                  false,
	})
}

func closeGORM(db *gorm.DB) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
