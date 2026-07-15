package mysqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"clean-arch-gin/config"
	"clean-arch-gin/mysqlstore/model"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpenConfiguresPoolPingsAndClosesOnce(t *testing.T) {
	connector := &stubConnector{}
	sqlDB := sql.OpenDB(connector)
	gormDB := newTestGORMDB(t, sqlDB)
	cfg := testDatabaseConfig()
	var logs bytes.Buffer

	got, closeDB, err := open(t.Context(), cfg, slog.New(slog.NewTextHandler(&logs, nil)), func(dsn string) (*gorm.DB, error) {
		parsed, err := mysqlconfig.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("ParseDSN() error = %v", err)
		}
		if parsed.MultiStatements {
			t.Fatal("application opener DSN must disable multi-statements")
		}
		if parsed.InterpolateParams {
			t.Fatal("opener DSN must keep application parameter interpolation disabled")
		}
		if parsed.User != cfg.User || parsed.Passwd != cfg.Password || parsed.DBName != cfg.Name {
			t.Fatalf("opener DSN identity = %q/%q, want %q/%q", parsed.User, parsed.DBName, cfg.User, cfg.Name)
		}
		return gormDB, nil
	})
	if err != nil {
		t.Fatalf("open() error = %v", err)
	}
	if got != gormDB {
		t.Fatal("open() returned a different GORM database")
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != cfg.MaxOpen {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, cfg.MaxOpen)
	}
	if got := connector.pingCount(); got != 1 {
		t.Fatalf("Ping count = %d, want 1", got)
	}
	got.Config.Logger.Warn(t.Context(), "injected logger probe")
	if !strings.Contains(logs.String(), "injected logger probe") {
		t.Fatalf("GORM logger did not use injected slog logger: %q", logs.String())
	}

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() { errs <- closeDB() })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("closeDB() error = %v", err)
		}
	}
	if got := connector.closeCount(); got != 1 {
		t.Fatalf("driver Close count = %d, want 1", got)
	}
}

func TestOpenMigrationsUsesDedicatedMultiStatementPool(t *testing.T) {
	connector := &stubConnector{}
	sqlDB := sql.OpenDB(connector)
	cfg := testDatabaseConfig()

	got, closeDB, err := openMigrations(t.Context(), cfg, func(driverName, dsn string) (*sql.DB, error) {
		if driverName != "mysql" {
			t.Fatalf("driver name = %q, want mysql", driverName)
		}
		parsed, err := mysqlconfig.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("ParseDSN() error = %v", err)
		}
		if !parsed.MultiStatements {
			t.Fatal("migration opener DSN must enable multi-statements")
		}
		if parsed.InterpolateParams {
			t.Fatal("migration opener DSN must keep parameter interpolation disabled")
		}
		return sqlDB, nil
	})
	if err != nil {
		t.Fatalf("openMigrations() error = %v", err)
	}
	if got != sqlDB {
		t.Fatal("openMigrations() returned a different SQL database")
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != cfg.MaxOpen {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, cfg.MaxOpen)
	}
	if got := connector.pingCount(); got != 1 {
		t.Fatalf("Ping count = %d, want 1", got)
	}
	if err := closeDB(); err != nil {
		t.Fatalf("closeDB() error = %v", err)
	}
}

func TestConfigurePoolAppliesEveryLimit(t *testing.T) {
	cfg := testDatabaseConfig()
	pool := &recordingPool{}

	configurePool(pool, cfg)

	if pool.maxOpen != cfg.MaxOpen || pool.maxIdle != cfg.MaxIdle {
		t.Fatalf("connection counts = open %d idle %d, want open %d idle %d", pool.maxOpen, pool.maxIdle, cfg.MaxOpen, cfg.MaxIdle)
	}
	if pool.maxLifetime != cfg.ConnMaxLifetime || pool.maxIdleTime != cfg.ConnMaxIdleTime {
		t.Fatalf("connection durations = lifetime %s idle %s, want lifetime %s idle %s", pool.maxLifetime, pool.maxIdleTime, cfg.ConnMaxLifetime, cfg.ConnMaxIdleTime)
	}
}

func TestOpenRedactsDSNAndDoesNotLogReturnedError(t *testing.T) {
	cfg := testDatabaseConfig()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	db, closeDB, err := open(t.Context(), cfg, logger, func(dsn string) (*gorm.DB, error) {
		return nil, fmt.Errorf("driver rejected %s", dsn)
	})
	if err == nil {
		t.Fatal("open() error = nil, want failure")
	}
	if !errors.Is(err, ErrInitialize) {
		t.Fatalf("open() error = %v, want ErrInitialize", err)
	}
	if db != nil {
		t.Fatalf("open() returned database %v on error", db)
	}
	if closeDB != nil {
		t.Fatal("open() returned a close function on error")
	}
	for _, secret := range []string{cfg.Password, cfg.DSN()} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("open() error exposed secret %q: %v", secret, err)
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("open() logged an error it also returned: %q", logs.String())
	}
}

func TestOpenRedactsPingFailure(t *testing.T) {
	cfg := testDatabaseConfig()
	connector := &stubConnector{pingErr: fmt.Errorf("cannot connect using %s", cfg.DSN())}
	sqlDB := sql.OpenDB(connector)
	gormDB := newTestGORMDB(t, sqlDB)

	_, _, err := open(t.Context(), cfg, slog.Default(), func(string) (*gorm.DB, error) { return gormDB, nil })
	if err == nil {
		t.Fatal("open() error = nil, want ping failure")
	}
	if !errors.Is(err, ErrPing) {
		t.Fatalf("open() error = %v, want ErrPing", err)
	}
	for _, secret := range []string{cfg.Password, cfg.DSN()} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("open() ping error exposed secret %q: %v", secret, err)
		}
	}
}

func TestCloseRedactsDriverFailure(t *testing.T) {
	cfg := testDatabaseConfig()
	connector := &stubConnector{closeErr: fmt.Errorf("close failed for %s", cfg.DSN())}
	sqlDB := sql.OpenDB(connector)
	gormDB := newTestGORMDB(t, sqlDB)

	_, closeDB, err := open(t.Context(), cfg, slog.Default(), func(string) (*gorm.DB, error) { return gormDB, nil })
	if err != nil {
		t.Fatalf("open() error = %v", err)
	}
	err = closeDB()
	if err == nil {
		t.Fatal("closeDB() error = nil, want driver close failure")
	}
	if !errors.Is(err, ErrClose) {
		t.Fatalf("closeDB() error = %v, want ErrClose", err)
	}
	for _, secret := range []string{cfg.Password, cfg.DSN()} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("closeDB() error exposed secret %q: %v", secret, err)
		}
	}
}

func TestOpenRejectsNilContext(t *testing.T) {
	_, _, err := Open(nil, testDatabaseConfig(), slog.Default())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "context") {
		t.Fatalf("Open() error = %v, want context validation error", err)
	}
}

func TestOpenCategorizesInvalidConfig(t *testing.T) {
	cfg := testDatabaseConfig()
	cfg.Host = ""

	_, _, err := open(t.Context(), cfg, slog.Default(), func(string) (*gorm.DB, error) {
		t.Fatal("opener must not run for invalid config")
		return nil, nil
	})
	if err == nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("open() error = %v, want ErrInvalidConfig", err)
	}
}

func TestOpenCategorizesSQLPoolFailure(t *testing.T) {
	cfg := testDatabaseConfig()

	_, _, err := open(t.Context(), cfg, slog.Default(), func(string) (*gorm.DB, error) {
		return &gorm.DB{Config: &gorm.Config{}}, nil
	})
	if err == nil || !errors.Is(err, ErrSQLPool) {
		t.Fatalf("open() error = %v, want ErrSQLPool", err)
	}
	assertErrorRedacted(t, err, cfg)
}

func TestOpenMigrationsRedactsInitializeFailure(t *testing.T) {
	cfg := testDatabaseConfig()

	_, _, err := openMigrations(t.Context(), cfg, func(string, string) (*sql.DB, error) {
		return nil, fmt.Errorf("driver rejected %s", cfg.DSN())
	})
	if err == nil || !errors.Is(err, ErrInitialize) {
		t.Fatalf("openMigrations() error = %v, want ErrInitialize", err)
	}
	assertErrorRedacted(t, err, cfg)
}

func TestModelTableNamesAndTransportIsolation(t *testing.T) {
	tests := []struct {
		name  string
		model interface{ TableName() string }
		want  string
	}{
		{name: "user", model: model.User{}, want: "users"},
		{name: "role", model: model.Role{}, want: "roles"},
		{name: "permission", model: model.Permission{}, want: "permissions"},
		{name: "user role", model: model.UserRole{}, want: "user_roles"},
		{name: "role permission", model: model.RolePermission{}, want: "role_permissions"},
		{name: "session", model: model.Session{}, want: "sessions"},
		{name: "refresh token", model: model.RefreshToken{}, want: "refresh_tokens"},
		{name: "product", model: model.Product{}, want: "products"},
		{name: "stock adjustment", model: model.StockAdjustment{}, want: "stock_adjustments"},
		{name: "order", model: model.Order{}, want: "orders"},
		{name: "order item", model: model.OrderItem{}, want: "order_items"},
		{name: "idempotency key", model: model.IdempotencyKey{}, want: "idempotency_keys"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.TableName(); got != tt.want {
				t.Fatalf("TableName() = %q, want %q", got, tt.want)
			}
			typ := reflect.TypeOf(tt.model)
			for fieldIndex := range typ.NumField() {
				field := typ.Field(fieldIndex)
				if _, ok := field.Tag.Lookup("json"); ok {
					t.Errorf("field %s.%s has transport-only json tag", typ.Name(), field.Name)
				}
			}
		})
	}
}

func testDatabaseConfig() config.Database {
	return config.Database{
		Host:            "db.internal",
		Port:            3306,
		User:            "service",
		Password:        "highly-secret-password",
		Name:            "clean_arch",
		MaxIdle:         3,
		MaxOpen:         7,
		ConnMaxLifetime: 15 * time.Minute,
		ConnMaxIdleTime: 3 * time.Minute,
	}
}

func assertErrorRedacted(t *testing.T, err error, cfg config.Database) {
	t.Helper()
	for _, secret := range []string{cfg.Password, cfg.DSN()} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed secret %q: %v", secret, err)
		}
	}
}

func newTestGORMDB(t *testing.T, sqlDB *sql.DB) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

type recordingPool struct {
	maxOpen     int
	maxIdle     int
	maxLifetime time.Duration
	maxIdleTime time.Duration
}

func (p *recordingPool) SetMaxOpenConns(value int)              { p.maxOpen = value }
func (p *recordingPool) SetMaxIdleConns(value int)              { p.maxIdle = value }
func (p *recordingPool) SetConnMaxLifetime(value time.Duration) { p.maxLifetime = value }
func (p *recordingPool) SetConnMaxIdleTime(value time.Duration) { p.maxIdleTime = value }

type stubConnector struct {
	mu       sync.Mutex
	pingErr  error
	closeErr error
	pings    int
	closes   int
}

func (c *stubConnector) Connect(context.Context) (driver.Conn, error) {
	return &stubConn{connector: c}, nil
}

func (c *stubConnector) Driver() driver.Driver { return stubDriver{} }

func (c *stubConnector) pingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pings
}

func (c *stubConnector) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes
}

type stubDriver struct{}

func (stubDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type stubConn struct {
	connector *stubConnector
}

func (*stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (*stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("not implemented") }

func (c *stubConn) Ping(context.Context) error {
	c.connector.mu.Lock()
	defer c.connector.mu.Unlock()
	c.connector.pings++
	return c.connector.pingErr
}

func (c *stubConn) Close() error {
	c.connector.mu.Lock()
	defer c.connector.mu.Unlock()
	c.connector.closes++
	return c.connector.closeErr
}
