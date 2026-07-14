//go:build integration

package migrations

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	mysqlcontainer "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMigrationsUpDown(t *testing.T) {
	db := newIntegrationDatabase(t)

	if err := Run(t.Context(), db, Up, 0); err != nil {
		t.Fatalf("Run(up) error = %v", err)
	}
	assertBusinessTables(t, db, requiredTables)
	assertSeedData(t, db)

	// Re-running up verifies that the public boundary intentionally translates
	// golang-migrate's ErrNoChange into a successful idempotent operation.
	if err := Run(t.Context(), db, Up, 0); err != nil {
		t.Fatalf("second Run(up) error = %v", err)
	}

	if err := Run(t.Context(), db, Down, 1); err != nil {
		t.Fatalf("Run(down) error = %v", err)
	}
	assertBusinessTables(t, db, nil)
	assertCount(t, db, "SELECT COUNT(*) FROM schema_migrations", 0)
}

func newIntegrationDatabase(t *testing.T) *sql.DB {
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
		t.Fatalf("start disposable MySQL container: %v", err)
	}

	dsn, err := container.ConnectionString(t.Context(), "parseTime=true", "loc=UTC")
	if err != nil {
		t.Fatalf("get MySQL connection settings: %v", err)
	}
	driverConfig, err := mysqlconfig.ParseDSN(dsn)
	if err != nil {
		t.Fatal("parse generated MySQL connection settings")
	}
	// The migration driver executes each trusted embedded version as one batch;
	// application parameter interpolation remains disabled at this test boundary.
	driverConfig.MultiStatements = true
	driverConfig.InterpolateParams = false

	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		t.Fatalf("open disposable MySQL database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close disposable MySQL database: %v", err)
		}
	})
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("ping disposable MySQL database: %v", err)
	}
	return db
}

func assertBusinessTables(t *testing.T, db *sql.DB, want []string) {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_name <> 'schema_migrations'
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("query business tables: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan business table: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate business tables: %v", err)
	}

	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("business tables = %v, want %v", got, want)
	}
}

func assertSeedData(t *testing.T, db *sql.DB) {
	t.Helper()

	assertCount(t, db, "SELECT COUNT(*) FROM roles", 2)
	assertCount(t, db, "SELECT COUNT(*) FROM permissions", 8)
	assertCount(t, db, "SELECT COUNT(*) FROM role_permissions", 8)
	assertCount(t, db, "SELECT COUNT(*) FROM users", 0)

	wantRoles := []string{"admin", "customer"}
	if got := queryNames(t, db, "SELECT name FROM roles ORDER BY name"); !slices.Equal(got, wantRoles) {
		t.Fatalf("seeded roles = %v, want %v", got, wantRoles)
	}
	wantPermissions := []string{
		"orders:manage",
		"orders:read_all",
		"products:stock",
		"products:write",
		"stats:read",
		"users:read",
		"users:roles",
		"users:write",
	}
	if got := queryNames(t, db, "SELECT name FROM permissions ORDER BY name"); !slices.Equal(got, wantPermissions) {
		t.Fatalf("seeded permissions = %v, want %v", got, wantPermissions)
	}
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
}

func queryNames(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("query seed names: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan seed name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seed names: %v", err)
	}
	return names
}
