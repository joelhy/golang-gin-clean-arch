package migrations

import (
	"context"
	"database/sql"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var requiredTables = []string{
	"users",
	"roles",
	"permissions",
	"user_roles",
	"role_permissions",
	"sessions",
	"refresh_tokens",
	"products",
	"orders",
	"order_items",
	"stock_adjustments",
	"idempotency_keys",
}

func TestFilesContainOnlyVersionedMigrationDirections(t *testing.T) {
	entries, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	want := []string{"000001_initial.down.sql", "000001_initial.up.sql"}
	if !slices.Equal(entries, want) {
		t.Fatalf("embedded SQL files = %v, want %v", entries, want)
	}
}

func TestFilesContainRequiredTablesInReversibleOrder(t *testing.T) {
	up := readMigration(t, "000001_initial.up.sql")
	down := readMigration(t, "000001_initial.down.sql")

	created := tableNames(up, `(?im)^\s*CREATE\s+TABLE\s+([a-z_]+)\s*\(`)
	dropped := tableNames(down, `(?im)^\s*DROP\s+TABLE\s+([a-z_]+)\s*;`)
	if !slices.Equal(created, requiredTables) {
		t.Fatalf("created tables = %v, want %v", created, requiredTables)
	}

	wantDropped := slices.Clone(requiredTables)
	slices.Reverse(wantDropped)
	if !slices.Equal(dropped, wantDropped) {
		t.Fatalf("dropped tables = %v, want strict reverse order %v", dropped, wantDropped)
	}
}

func TestInitialUpMigrationDefinesSecurityAndDataConstraints(t *testing.T) {
	up := readMigration(t, "000001_initial.up.sql")
	requiredFragments := []string{
		"-- Create users table",
		"email VARCHAR(320)",
		"password_hash VARCHAR(255)",
		"status VARCHAR(16)",
		"price_amount BIGINT",
		"stock INT UNSIGNED",
		"delta INT",
		"total_amount BIGINT",
		"quantity INT UNSIGNED",
		"request_hash BINARY(32)",
		"digest BINARY(32)",
		"DATETIME(6)",
		"CONSTRAINT chk_products_price_amount_nonnegative CHECK (price_amount >= 0)",
		"CONSTRAINT chk_products_stock_nonnegative CHECK (stock >= 0)",
		"CONSTRAINT chk_orders_total_amount_nonnegative CHECK (total_amount >= 0)",
		"CONSTRAINT chk_order_items_quantity_positive CHECK (quantity > 0)",
		"CONSTRAINT uk_idempotency_keys_scope UNIQUE (user_id, operation, idempotency_key)",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToLower(up), "automigrate") {
		t.Fatal("versioned migration must not depend on AutoMigrate")
	}
}

func TestInitialUpMigrationSeedsStableRolesAndPermissions(t *testing.T) {
	up := readMigration(t, "000001_initial.up.sql")
	for _, value := range []string{
		"'customer'",
		"'admin'",
		"'users:read'",
		"'users:write'",
		"'users:roles'",
		"'products:write'",
		"'products:stock'",
		"'orders:read_all'",
		"'orders:manage'",
		"'stats:read'",
	} {
		if !strings.Contains(up, value) {
			t.Errorf("up migration missing seed value %s", value)
		}
	}
	if !strings.Contains(up, "WHERE r.name = 'admin'") {
		t.Fatal("up migration must grant every stable permission to the admin role")
	}
	if strings.Contains(strings.ToLower(up), "password_hash) values") {
		t.Fatal("initial migration must not seed user credentials")
	}
}

func TestRunRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		db        *sql.DB
		direction Direction
		steps     uint
		want      string
	}{
		{name: "nil context", direction: Up, want: "context"},
		{name: "nil database", ctx: t.Context(), direction: Up, want: "database"},
		{name: "unknown direction", ctx: t.Context(), db: &sql.DB{}, direction: Direction("sideways"), want: "direction"},
		{name: "zero down steps", ctx: t.Context(), db: &sql.DB{}, direction: Down, want: "steps"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(tt.ctx, tt.db, tt.direction, tt.steps)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Fatalf("Run() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestRunHonorsAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Run(ctx, &sql.DB{}, Up, 0)
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Run() error = %v, want context cancellation", err)
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	content, err := fs.ReadFile(Files, name)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", name, err)
	}
	return string(content)
}

func tableNames(content, expression string) []string {
	matches := regexp.MustCompile(expression).FindAllStringSubmatch(content, -1)
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match[1])
	}
	return names
}
