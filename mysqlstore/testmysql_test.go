//go:build integration

package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
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

const testRootPassword = "test"

var (
	sharedMySQL      *mysqlcontainer.MySQLContainer
	sharedMySQLOnce  sync.Once
	sharedMySQLError error
	testDatabaseSeq  atomic.Uint64
)

func TestMain(m *testing.M) {
	code := m.Run()
	if sharedMySQL != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		_ = sharedMySQL.Terminate(ctx)
		cancel()
	}
	os.Exit(code)
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	container := sharedMySQLContainer(t)
	databaseName := fmt.Sprintf("clean_arch_test_%d", testDatabaseSeq.Add(1))
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelSetup()

	endpoint, err := container.PortEndpoint(setupCtx, "3306/tcp", "")
	if err != nil {
		t.Fatalf("read disposable MySQL endpoint: %v", err)
	}
	host, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatalf("parse disposable MySQL address: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse disposable MySQL port: %v", err)
	}

	adminDB := openAdminDB(t, setupCtx, host, port)
	if _, err := adminDB.ExecContext(setupCtx, "CREATE DATABASE "+databaseName+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create disposable database: %v", err)
	}
	if err := adminDB.Close(); err != nil {
		t.Fatalf("close disposable admin database: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		adminDB := openAdminDB(t, ctx, host, port)
		defer func() {
			if err := adminDB.Close(); err != nil {
				t.Errorf("close disposable admin database: %v", err)
			}
		}()
		if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+databaseName); err != nil {
			t.Errorf("drop disposable database %s: %v", databaseName, err)
		}
	})

	cfg := config.Database{
		Host: host, Port: port, User: "root", Password: testRootPassword, Name: databaseName,
		MaxIdle: 4, MaxOpen: 16, ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute,
	}

	migrationDB, closeMigrations, err := OpenMigrations(setupCtx, cfg)
	if err != nil {
		t.Fatalf("open disposable migration database: %v", err)
	}
	if err := migrations.Run(setupCtx, migrationDB, migrations.Up, 0); err != nil {
		_ = closeMigrations()
		t.Fatalf("migrate disposable MySQL: %v", err)
	}
	if err := closeMigrations(); err != nil {
		t.Fatalf("close disposable migration database: %v", err)
	}

	db, closeDB, err := Open(setupCtx, cfg, nil)
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

func sharedMySQLContainer(t *testing.T) *mysqlcontainer.MySQLContainer {
	t.Helper()

	// SQL readiness probes intentionally connect while MySQL is still initializing.
	// The returned errors drive the wait loop, so driver-level EOF logs add noise without useful signal.
	if err := mysqlconfig.SetLogger(&mysqlconfig.NopLogger{}); err != nil {
		t.Fatalf("mute MySQL readiness probe logs: %v", err)
	}

	sharedMySQLOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		sharedMySQL, sharedMySQLError = mysqlcontainer.Run(
			ctx,
			"mysql:8.4",
			mysqlcontainer.WithUsername("root"),
			mysqlcontainer.WithPassword(testRootPassword),
			mysqlcontainer.WithDatabase("clean_arch"),
			testcontainers.WithWaitStrategyAndDeadline(
				3*time.Minute,
				wait.ForSQL("3306/tcp", "mysql", func(host string, port network.Port) string {
					// MySQL 8.4 logs both temporary and final server startup lines, and
					// log phrasing can vary. SQL readiness proves the final server and root
					// credentials are usable before per-test databases and migrations run.
					return fmt.Sprintf("root:%s@tcp(%s:%s)/clean_arch?timeout=5s", testRootPassword, host, port.Port())
				}).WithStartupTimeout(3*time.Minute).WithPollInterval(time.Second),
			),
		)
	})
	if sharedMySQLError != nil {
		t.Fatalf("start shared MySQL: %v", sharedMySQLError)
	}
	if sharedMySQL == nil {
		t.Fatal("start shared MySQL: container is nil")
	}
	return sharedMySQL
}

func openAdminDB(t *testing.T, ctx context.Context, host string, port int) *sql.DB {
	t.Helper()

	adminConfig := mysqlconfig.Config{
		User:      "root",
		Passwd:    testRootPassword,
		Net:       "tcp",
		Addr:      net.JoinHostPort(host, strconv.Itoa(port)),
		Collation: "utf8mb4_unicode_ci",
		ParseTime: true,
		Loc:       time.UTC,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}
	dsn := adminConfig.FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open disposable admin database: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping disposable admin database: %v", err)
	}
	return db
}
