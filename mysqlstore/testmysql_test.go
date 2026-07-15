//go:build integration

package mysqlstore

import (
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"clean-arch-gin/config"
	"clean-arch-gin/migrations"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	mysqlcontainer "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	// SQL readiness probes intentionally connect while MySQL is still initializing.
	// The returned errors drive the wait loop, so driver-level EOF logs add noise without useful signal.
	if err := mysqlconfig.SetLogger(&mysqlconfig.NopLogger{}); err != nil {
		t.Fatalf("mute MySQL readiness probe logs: %v", err)
	}

	container, err := mysqlcontainer.Run(
		t.Context(),
		"mysql:8.4",
		mysqlcontainer.WithDatabase("clean_arch"),
		testcontainers.WithWaitStrategyAndDeadline(
			3*time.Minute,
			wait.ForSQL("3306/tcp", "mysql", func(host string, port network.Port) string {
				// MySQL 8.4 logs both temporary and final server startup lines, and
				// log phrasing can vary. SQL readiness proves the final server, test
				// user, and database are actually usable before migrations run.
				return fmt.Sprintf("test:test@tcp(%s:%s)/clean_arch?timeout=5s", host, port.Port())
			}).WithStartupTimeout(3*time.Minute).WithPollInterval(time.Second),
		),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start disposable MySQL: %v", err)
	}

	dsn, err := container.ConnectionString(t.Context(), "parseTime=true", "loc=UTC")
	if err != nil {
		t.Fatalf("read disposable MySQL connection settings: %v", err)
	}
	driverConfig, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse disposable MySQL connection settings: %v", err)
	}
	host, rawPort, err := net.SplitHostPort(driverConfig.Addr)
	if err != nil {
		t.Fatalf("parse disposable MySQL address: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse disposable MySQL port: %v", err)
	}
	cfg := config.Database{
		Host: host, Port: port, User: driverConfig.User, Password: driverConfig.Passwd, Name: driverConfig.DBName,
		MaxIdle: 4, MaxOpen: 16, ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute,
	}

	migrationDB, closeMigrations, err := OpenMigrations(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open disposable migration database: %v", err)
	}
	if err := migrations.Run(t.Context(), migrationDB, migrations.Up, 0); err != nil {
		_ = closeMigrations()
		t.Fatalf("migrate disposable MySQL: %v", err)
	}
	if err := closeMigrations(); err != nil {
		t.Fatalf("close disposable migration database: %v", err)
	}

	db, closeDB, err := Open(t.Context(), cfg, nil)
	if err != nil {
		t.Fatalf("open disposable application database: %v", err)
	}
	t.Cleanup(func() {
		if err := closeDB(); err != nil {
			t.Errorf("close disposable application database: %v", err)
		}
	})
	return db
}
