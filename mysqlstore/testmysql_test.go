//go:build integration

package mysqlstore

import (
	"net"
	"strconv"
	"testing"
	"time"

	"clean-arch-gin/config"
	"clean-arch-gin/migrations"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	mysqlcontainer "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	container, err := mysqlcontainer.Run(
		t.Context(),
		"mysql:8.4",
		mysqlcontainer.WithDatabase("clean_arch"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("port: 3306  MySQL Community Server").WithStartupTimeout(2*time.Minute),
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
