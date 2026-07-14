// Package mysqlstore provides MySQL-backed persistence adapters.
package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

// Open establishes a context-checked GORM connection and configures its SQL pool.
func Open(ctx context.Context, cfg config.Database, logger *slog.Logger) (*gorm.DB, func() error, error) {
	return open(ctx, cfg, logger, openGORM)
}

func open(ctx context.Context, cfg config.Database, logger *slog.Logger, opener gormOpener) (*gorm.DB, func() error, error) {
	if ctx == nil {
		return nil, nil, errors.New("open mysql database: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("open mysql database: %w", err)
	}
	if opener == nil {
		return nil, nil, errors.New("open mysql database: opener is required")
	}

	dsn, err := migrationCompatibleDSN(cfg)
	if err != nil {
		// A malformed DSN may contain credentials, so this boundary deliberately does
		// not wrap the driver error or echo the generated connection string.
		return nil, nil, errors.New("open mysql database: invalid connection configuration")
	}
	gormDB, err := opener(dsn)
	if err != nil {
		closeGORM(gormDB)
		// Driver errors are intentionally redacted because some implementations embed
		// the full password-bearing DSN in their Error string.
		return nil, nil, errors.New("open mysql database: connection initialization failed")
	}
	if gormDB == nil {
		return nil, nil, errors.New("open mysql database: connection initialization returned no database")
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, nil, errors.New("open mysql database: SQL pool is unavailable")
	}
	// Pool limits are applied before the first ping so even startup and recovery
	// traffic cannot briefly exceed the database's configured connection budget.
	configurePool(sqlDB, cfg)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, errors.New("open mysql database: ping failed")
	}

	// GORM initializes with a silent logger so connection errors are returned once,
	// at this boundary. Only a successfully opened DB receives the injected logger.
	gormDB.Config.Logger = newGORMLogger(logger)

	var closeOnce sync.Once
	var closeErr error
	closeDB := func() error {
		closeOnce.Do(func() {
			if err := sqlDB.Close(); err != nil {
				// Apply the same redaction policy to shutdown because third-party
				// driver errors are not guaranteed to omit connection details.
				closeErr = errors.New("close mysql database: driver close failed")
			}
		})
		return closeErr
	}
	return gormDB, closeDB, nil
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

func migrationCompatibleDSN(cfg config.Database) (string, error) {
	driverConfig, err := mysqlconfig.ParseDSN(cfg.DSN())
	if err != nil {
		return "", err
	}
	// golang-migrate sends each trusted embedded version as one SQL batch. This is
	// the only reason multi-statements are enabled; business values remain bound
	// parameters and interpolation stays disabled to avoid widening injection risk.
	driverConfig.MultiStatements = true
	driverConfig.InterpolateParams = false
	return driverConfig.FormatDSN(), nil
}

func configurePool(pool poolConfigurer, cfg config.Database) {
	pool.SetMaxOpenConns(cfg.MaxOpen)
	pool.SetMaxIdleConns(cfg.MaxIdle)
	pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
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
