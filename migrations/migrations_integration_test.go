//go:build integration

package migrations

import (
	"bytes"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"clean-arch-gin/mysqlstore/model"
	"clean-arch-gin/mysqlstore/query"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	mysqlcontainer "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrationsUpDown(t *testing.T) {
	db := newIntegrationDatabase(t)

	if err := Run(t.Context(), db, Up, 0); err != nil {
		t.Fatalf("Run(up) error = %v", err)
	}
	assertBusinessTables(t, db, requiredTables)
	assertSeedData(t, db)
	assertGeneratedQueriesAndConstraints(t, db)

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

func assertGeneratedQueriesAndConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	gormDB, err := gorm.Open(gormmysql.New(gormmysql.Config{
		Conn:                      db,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		t.Fatalf("open GORM over migrated database: %v", err)
	}
	queries := query.Use(gormDB)
	now := time.Now().UTC().Truncate(time.Microsecond)
	user := &model.User{
		Email:        "mapping@example.com",
		DisplayName:  "Mapping User",
		PasswordHash: "integration-only-hash",
		Status:       "active",
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := queries.User.WithContext(t.Context()).Create(user); err != nil {
		t.Fatalf("create user through generated query: %v", err)
	}
	gotUser, err := queries.User.WithContext(t.Context()).Where(queries.User.ID.Eq(user.ID)).First()
	if err != nil {
		t.Fatalf("read user through generated query: %v", err)
	}
	if gotUser.Email != user.Email || gotUser.DisplayName != user.DisplayName || gotUser.PasswordHash != user.PasswordHash || gotUser.Status != user.Status || gotUser.Version != user.Version {
		t.Fatalf("generated user round trip = %+v, want persisted fields from %+v", gotUser, user)
	}

	description := "Generated query mapping product"
	product := &model.Product{
		SKU:         "MAP-SKU-1",
		Name:        "Mapped Product",
		Description: &description,
		PriceAmount: 1250,
		Currency:    "CNY",
		Stock:       5,
		Status:      "active",
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := queries.Product.WithContext(t.Context()).Create(product); err != nil {
		t.Fatalf("create product through generated query: %v", err)
	}
	gotProduct, err := queries.Product.WithContext(t.Context()).Where(queries.Product.ID.Eq(product.ID)).First()
	if err != nil {
		t.Fatalf("read product through generated query: %v", err)
	}
	if gotProduct.SKU != product.SKU || gotProduct.Name != product.Name || gotProduct.Description == nil || *gotProduct.Description != description || gotProduct.PriceAmount != product.PriceAmount || gotProduct.Currency != product.Currency || gotProduct.Stock != product.Stock || gotProduct.Status != product.Status || gotProduct.Version != product.Version {
		t.Fatalf("generated product round trip = %+v, want persisted fields from %+v", gotProduct, product)
	}

	assertExecFails(t, db, `
		INSERT INTO orders (number, user_id, status, total_amount, currency, version)
		VALUES (?, ?, ?, ?, ?, ?)`, "FK-MISSING-USER", uint64(999999), "pending", int64(0), "CNY", uint64(1))
	assertExecFails(t, db, `
		INSERT INTO products (sku, name, price_amount, currency, stock, status, version)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "NEGATIVE-PRICE", "Invalid Product", int64(-1), "CNY", uint32(0), "active", uint64(1))

	result, err := db.ExecContext(t.Context(), `
		INSERT INTO orders (number, user_id, status, total_amount, currency, version)
		VALUES (?, ?, ?, ?, ?, ?)`, "VALID-ORDER", user.ID, "pending", product.PriceAmount, product.Currency, uint64(1))
	if err != nil {
		t.Fatalf("insert valid order: %v", err)
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read valid order ID: %v", err)
	}
	assertExecFails(t, db, `
		INSERT INTO order_items (
			order_id, product_id, product_sku, product_name,
			unit_price_amount, currency, quantity, subtotal_amount
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		orderID, product.ID, product.SKU, product.Name, product.PriceAmount, product.Currency, uint32(0), int64(0))

	sessionID := strings.Repeat("s", 22)
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO sessions (id, user_id, rotation, expires_at)
		VALUES (?, ?, ?, ?)`, sessionID, user.ID, uint64(0), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert valid session: %v", err)
	}
	digest := bytes.Repeat([]byte{0x42}, 32)
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO refresh_tokens (id, session_id, digest, expires_at)
		VALUES (?, ?, ?, ?)`, strings.Repeat("a", 22), sessionID, digest, now.Add(time.Hour)); err != nil {
		t.Fatalf("insert valid refresh token: %v", err)
	}
	assertExecFails(t, db, `
		INSERT INTO refresh_tokens (id, session_id, digest, expires_at)
		VALUES (?, ?, ?, ?)`, strings.Repeat("b", 22), sessionID, digest, now.Add(time.Hour))
}

func assertExecFails(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), statement, args...); err == nil {
		t.Fatal("statement unexpectedly satisfied a database constraint")
	}
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
