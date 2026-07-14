# Domain-First Clean Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将仓库重构为 Go 1.25、Gin、GORM Gen 和 MySQL 驱动的领域优先模块化单体，完整提供用户/RBAC/会话、商品库存、订单事务、统一 HTTP API、管理命令和生产级生命周期。

**Architecture:** `user`、`product`、`order` 与 `reporting` 包拥有业务规则和消费方端口；`mysqlstore`、`security` 与 `web` 是外层适配器；`cmd/server` 和 `cmd/admin` 使用 Wire 显式组装。版本化 SQL 是数据库结构的唯一事实来源，跨库存与订单的写操作始终位于同一 MySQL 事务内。

**Tech Stack:** Go 1.25.12、Gin 1.12、GORM 1.31、GORM Gen 0.3、MySQL 8.4、Wire 0.7、urfave/cli v3.10、golang-jwt/jwt v5、Argon2id、golang-migrate v4、Testcontainers-Go 0.43、标准库 `os`/`testing`/`httptest`/`log/slog`。

---

## User override: standard-library configuration

The user explicitly removed Viper after Task 4. This section supersedes the Viper-specific parts of Tasks 1 and 13:

- `config` reads `APP_*` variables through `os.LookupEnv` and strict standard-library parsers.
- There is no implicit YAML/JSON configuration-file merge and no global configuration state.
- An explicitly present empty environment value overrides defaults and then fails validation when the field is required.
- `cmd/admin` uses `github.com/urfave/cli/v3`; neither Cobra nor direct standard-library `flag` parsing is used for the command tree.
- Task 4A removes Cobra and Viper from source/module graph and pins urfave/cli before Task 5 starts.

## File map

```text
cmd/server/main.go, run.go, wire.go, wire_gen.go
cmd/admin/main.go, root.go, migrate.go, bootstrap.go, wire.go, wire_gen.go
config/config.go, config_test.go
user/{errors,types,service,auth,rbac}.go + *_test.go
product/{errors,types,service}.go + *_test.go
order/{errors,types,service,query}.go + *_test.go
reporting/{types,service}.go + *_test.go
security/{password,jwt,refresh}.go + *_test.go
mysqlstore/db.go
mysqlstore/model/{user,product,order}.go
mysqlstore/query/*_gen.go
mysqlstore/generate/main.go
mysqlstore/{user,session,product,order,reporting}_store.go + *_test.go
migrations/{migrations.go,000001_initial.up.sql,000001_initial.down.sql}
web/{response,error,decode,pagination,middleware,auth_handler,user_handler,product_handler,order_handler,stats_handler,router}.go + *_test.go
README.md, docs/api.md, docs/architecture.md, docs/error-codes.md
Dockerfile, docker-compose.yaml, env.example, justfile
```

Existing `internal/` code is removed only in Task 14, after the replacement application builds and its tests pass. Existing comments that remain semantically valid move with their logic; comments whose semantics change are rewritten to describe the new constraints.

### Task 1: Go 1.25 toolchain, dependencies, and strict configuration

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `config/config.go`
- Create: `config/config_test.go`

- [ ] **Step 1: Write failing configuration tests**

Create table-driven tests for environment-only configuration. The tests must construct a fresh `viper.Viper`, register every key before `Unmarshal`, and cover valid configuration, missing JWT key, short JWT key, invalid duration, invalid CORS URL, invalid pool bounds, and production use of an example secret.

```go
func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "valid", env: validEnv()},
		{name: "missing JWT key", env: without(validEnv(), "APP_JWT_KEY"), wantErr: "jwt key is required"},
		{name: "pool bounds", env: with(validEnv(), "APP_DB_MAX_IDLE", "11", "APP_DB_MAX_OPEN", "10"), wantErr: "max idle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range allConfigEnvKeys() { t.Setenv(key, "") }
			for key, value := range tt.env { t.Setenv(key, value) }
			_, err := Load()
			if tt.wantErr == "" && err != nil { t.Fatalf("Load() error = %v", err) }
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./config -run TestLoad -v`

Expected: FAIL because package `config` and `Load` do not exist.

- [ ] **Step 3: Upgrade the module and pin runtime/tool dependencies**

Set `go 1.25.0`. Add direct runtime dependencies at the verified stable versions and tools through Go's `tool` directive:

```bash
go get github.com/gin-gonic/gin@v1.12.0 \
  github.com/golang-jwt/jwt/v5@v5.3.1 \
  github.com/golang-migrate/migrate/v4@v4.19.1 \
  github.com/google/go-cmp@v0.7.0 \
  github.com/spf13/cobra@v1.10.2 \
  github.com/spf13/viper@v1.21.0 \
  github.com/testcontainers/testcontainers-go/modules/mysql@v0.43.0 \
  golang.org/x/crypto@v0.54.0 \
  golang.org/x/time@v0.15.0 \
  gorm.io/driver/mysql@v1.6.0 \
  gorm.io/gen@v0.3.28 \
  gorm.io/gorm@v1.31.2
go get -tool github.com/google/wire/cmd/wire@v0.7.0
go mod tidy
```

If a selected module declares a Go version newer than 1.25, choose the newest release from the same module that supports Go 1.25 and record the compatibility reason in the commit body.

- [ ] **Step 4: Implement typed configuration and validation**

Define focused nested structs and duration types. `Load` creates its own Viper instance, registers all defaults/keys, maps `APP_*` environment variables, unmarshals once, and calls `Validate`.

```go
type Config struct {
	Environment string     `mapstructure:"environment"`
	HTTP        HTTP       `mapstructure:"http"`
	DB          Database   `mapstructure:"db"`
	JWT         JWT        `mapstructure:"jwt"`
	Password    Password   `mapstructure:"password"`
	CORS        CORS       `mapstructure:"cors"`
	RateLimit   RateLimit  `mapstructure:"rate_limit"`
}

type HTTP struct {
	Address           string        `mapstructure:"address"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	MaxBodyBytes      int64         `mapstructure:"max_body_bytes"`
}

func NewViper() *viper.Viper
func Load() (Config, error)
func LoadWith(*viper.Viper) (Config, error)
func (c Config) Validate() error
func (d Database) DSN() string
```

`Load` calls `LoadWith(NewViper())`. Use explicit `BindEnv` for every key so Viper `Unmarshal` sees environment-only values. `cmd/admin` receives a fresh `NewViper` instance, binds command flags during `PersistentPreRunE`, then calls `LoadWith`, which keeps flags, environment and an optional config file in one typed source. Return wrapped errors with the failing configuration area; never log inside `config`.

- [ ] **Step 5: Run tests and static checks**

Run: `gofmt -w config && go test ./config -v && go vet ./config`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum config
git commit -m "feat(config): 升级 Go 1.25 并实现严格配置"
```

### Task 2: Versioned schema, migration runner, and GORM Gen models

**Files:**
- Delete: `migrations/001_create_users_table.sql`
- Create: `migrations/000001_initial.up.sql`
- Create: `migrations/000001_initial.down.sql`
- Create: `migrations/migrations.go`
- Create: `migrations/migrations_test.go`
- Create: `mysqlstore/db.go`
- Create: `mysqlstore/db_test.go`
- Create: `mysqlstore/model/user.go`
- Create: `mysqlstore/model/product.go`
- Create: `mysqlstore/model/order.go`
- Create: `mysqlstore/generate/main.go`
- Create: `mysqlstore/query/*_gen.go` via generation

- [ ] **Step 1: Write failing schema and DB configuration tests**

Test the embedded migration source, DSN sanitization in errors, SQL pool settings, and exact table/model names. The schema test must assert both migration directions are embedded and the up migration includes every required table and constraint.

```go
func TestFilesContainRequiredTables(t *testing.T) {
	up, err := fs.ReadFile(Files, "000001_initial.up.sql")
	if err != nil { t.Fatal(err) }
	for _, table := range []string{
		"users", "roles", "permissions", "user_roles", "role_permissions",
		"sessions", "refresh_tokens", "products", "stock_adjustments", "orders", "order_items", "idempotency_keys",
	} {
		if !bytes.Contains(up, []byte("CREATE TABLE "+table)) {
			t.Errorf("up migration does not create %s", table)
		}
	}
}
```

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./migrations ./mysqlstore -run 'TestFiles|TestOpen' -v`

Expected: FAIL because the new migration package and store do not exist.

- [ ] **Step 3: Write the reversible initial migration**

Use MySQL 8 syntax, `BIGINT UNSIGNED` identifiers, `BIGINT` money, `INT UNSIGNED` quantities, UTC-capable `DATETIME(6)`, foreign keys, check constraints, and deterministic index names. Seed `customer` and `admin` roles plus the permissions listed in the design by stable names, not hard-coded user credentials.

`idempotency_keys` must include `user_id`, `operation`, `idempotency_key`, a 32-byte request hash, nullable `order_id`, timestamps, and a unique key over `(user_id, operation, idempotency_key)`. `sessions` stores the token family lifecycle; `refresh_tokens` stores one SHA-256 digest per issued token plus consumed/revoked/replaced metadata. Keeping consumed rows is required to detect reuse of any older token, not only the immediately previous token.

- [ ] **Step 4: Implement embedded migration runner**

```go
//go:embed *.sql
var Files embed.FS

type Direction string
const (
	Up Direction = "up"
	Down Direction = "down"
)

func Run(ctx context.Context, db *sql.DB, direction Direction, steps uint) error
```

Use `source/iofs` and the MySQL database driver from `golang-migrate`. Treat `migrate.ErrNoChange` as success. For down migrations reject `steps == 0`; `up` with zero steps means migrate to the latest version. Preserve Context by checking it before and after the migration library call and wrap errors with direction and step count.

- [ ] **Step 5: Implement database opening and pool configuration**

```go
func Open(ctx context.Context, cfg config.Database, logger *slog.Logger) (*gorm.DB, func() error, error)
```

Open the GORM MySQL dialector with parameterized logger configuration, obtain `*sql.DB`, set pool limits, and `PingContext` with the caller Context. Return a close function; do not log and return the same error. Error text must never contain the password-bearing DSN.

- [ ] **Step 6: Define persistence models and generate typed queries**

Persistence models mirror SQL columns exactly and use `TableName` methods. Keep password hashes and refresh digests out of JSON by not adding transport tags. The generator applies all models and writes to `mysqlstore/query`:

```go
func main() {
	g := gen.NewGenerator(gen.Config{
		OutPath:      "../query",
		ModelPkgPath: "../model",
		Mode:         gen.WithDefaultQuery | gen.WithQueryInterface,
	})
	g.ApplyBasic(
		model.User{}, model.Role{}, model.Permission{}, model.UserRole{}, model.RolePermission{}, model.Session{}, model.RefreshToken{},
		model.Product{}, model.StockAdjustment{}, model.Order{}, model.OrderItem{}, model.IdempotencyKey{},
	)
	g.Execute()
}
```

Run from `mysqlstore/generate`: `go run .`. Add a `//go:generate` directive in `mysqlstore/model/doc.go` whose working directory produces the same output.

- [ ] **Step 7: Run generation and tests**

Run:

```bash
gofmt -w migrations mysqlstore
go generate ./mysqlstore/model
go test ./migrations ./mysqlstore/... -v
go vet ./migrations ./mysqlstore/...
```

Expected: PASS and a clean second `go generate ./mysqlstore/model` diff.

- [ ] **Step 8: Commit**

```bash
git add migrations mysqlstore go.mod go.sum
git commit -m "feat(database): 建立版本化结构与 GORM Gen 查询"
```

### Task 3: User, RBAC, and account domain

**Files:**
- Create: `user/errors.go`
- Create: `user/types.go`
- Create: `user/service.go`
- Create: `user/rbac.go`
- Create: `user/service_test.go`
- Create: `user/rbac_test.go`

- [ ] **Step 1: Write failing entity and service tests**

Use manual fakes for the consumer-owned ports. Cover email normalization, invalid email/name/password, default customer role, duplicate email, self profile update, admin listing filters, status transitions, role replacement, unknown roles, and last-admin protection.

```go
func TestServiceRegister(t *testing.T) {
	store := newFakeStore()
	svc := NewService(store, fakePasswords{}, fakeClock{now: fixedTime})
	got, err := svc.Register(t.Context(), RegisterInput{
		Email: "  USER@Example.COM ", Name: "Joel", Password: "correct horse battery staple",
	})
	if err != nil { t.Fatal(err) }
	if got.Email != "user@example.com" { t.Fatalf("Email = %q", got.Email) }
	if diff := cmp.Diff([]string{"customer"}, roleNames(got.Roles)); diff != "" { t.Fatal(diff) }
}

func TestServiceCannotDisableLastAdmin(t *testing.T) {
	store := fakeStore{lastAdminID: 7}
	svc := NewService(&store, fakePasswords{}, fakeClock{now: fixedTime})
	err := svc.SetStatus(t.Context(), Actor{UserID: 7}, 7, StatusDisabled)
	if !errors.Is(err, ErrLastAdmin) { t.Fatalf("error = %v", err) }
}
```

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./user -run 'TestService|TestNormalize|TestPermission' -v`

Expected: FAIL because package `user` is not implemented.

- [ ] **Step 3: Define domain values, errors, and ports**

```go
type Status string
const (
	StatusActive Status = "active"
	StatusDisabled Status = "disabled"
)

type User struct {
	ID uint64
	Email string
	Name string
	PasswordHash string
	Status Status
	Version uint64
	Roles []Role
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Identity struct { UserID uint64; SessionID, TokenID string }

type Store interface {
	CreateWithRole(context.Context, *User, string) error
	ByID(context.Context, uint64) (*User, error)
	ByEmail(context.Context, string) (*User, error)
	UpdateProfile(context.Context, *User, uint64) error
	List(context.Context, ListFilter) (Page, error)
	SetStatus(context.Context, uint64, Status, uint64) error
	ReplaceRoles(context.Context, uint64, []string) error
	RoleNamesExist(context.Context, []string) (bool, error)
	IsLastActiveAdmin(context.Context, uint64) (bool, error)
	Permissions(context.Context, uint64) ([]string, error)
}

type Passwords interface {
	Hash(string) (string, error)
	Verify(encoded, password string) (bool, error)
	NeedsRehash(string) bool
}

type Clock interface { Now() time.Time }
type SystemClock struct{}
func (SystemClock) Now() time.Time { return time.Now() }
```

Errors are stable sentinel values wrapped with context where needed: `ErrInvalidEmail`, `ErrInvalidName`, `ErrWeakPassword`, `ErrEmailExists`, `ErrNotFound`, `ErrDisabled`, `ErrForbidden`, `ErrRoleNotFound`, `ErrLastAdmin`, and `ErrConflict`.

- [ ] **Step 4: Implement account and RBAC rules**

`Register` normalizes email with `strings.TrimSpace` and lowercase, validates it with `net/mail`, enforces 12–128 UTF-8 bytes for passwords, hashes before persistence, and assigns `customer`. `BootstrapAdmin` performs the same validation and hashing but atomically assigns `admin`; duplicate email remains an error. `UpdateMe` only accepts display-name changes. Admin methods require an `Actor` carrying already-authenticated identity and explicit permission strings.

List filters expose only whitelisted fields:

```go
type ListFilter struct {
	Email, Name string
	Status Status
	Role string
	Sort string
	Descending bool
	Limit, Offset int
}

func (f ListFilter) Validate() error
```

Clamp no values silently: invalid status, sort, limit outside 1–100, or negative offset returns a domain validation error.

- [ ] **Step 5: Run domain tests**

Run: `gofmt -w user && go test ./user -v && go vet ./user`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add user
git commit -m "feat(user): 实现用户与 RBAC 领域规则"
```

### Task 4: Argon2id, JWT access tokens, and refresh token primitives

**Files:**
- Create: `security/password.go`
- Create: `security/password_test.go`
- Create: `security/jwt.go`
- Create: `security/jwt_test.go`
- Create: `security/refresh.go`
- Create: `security/refresh_test.go`
- Create: `security/random.go`
- Create: `security/random_test.go`

- [ ] **Step 1: Write failing security tests**

Cover Argon2id round-trip, wrong password, malformed encoded hash, rehash detection, JWT round-trip, rejected algorithm, bad issuer/audience, expired/not-yet-valid tokens, and 10,000 refresh tokens with no duplicates. Tests use an injected `io.Reader` and fixed time.

```go
func TestJWTRejectsUnexpectedAlgorithm(t *testing.T) {
	m := NewJWT(JWTConfig{Key: bytes.Repeat([]byte("k"), 32), Issuer: "test", Audience: "api", TTL: time.Minute})
	raw := signWithNoneAlgorithm(t, validClaims())
	_, err := m.Parse(raw, fixedTime)
	if !errors.Is(err, ErrInvalidToken) { t.Fatalf("Parse() error = %v", err) }
}

func TestRefreshGeneratorStoresOnlyDigest(t *testing.T) {
	g := NewRefreshGenerator(bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	raw, digest, err := g.Generate()
	if err != nil { t.Fatal(err) }
	if raw == "" || digest == "" || strings.Contains(digest, raw) { t.Fatal("invalid raw/digest separation") }
}
```

- [ ] **Step 2: Verify the tests fail**

Run: `go test ./security -v`

Expected: FAIL because package `security` does not exist.

- [ ] **Step 3: Implement Argon2id encoding**

```go
type PasswordConfig struct { Memory uint32; Iterations uint32; Parallelism uint8; SaltLength uint32; KeyLength uint32 }
type PasswordManager struct { cfg PasswordConfig; random io.Reader }

func (m *PasswordManager) Hash(password string) (string, error)
func (m *PasswordManager) Verify(encoded, password string) (bool, error)
func (m *PasswordManager) NeedsRehash(encoded string) bool
```

Use the standard `$argon2id$v=19$m=...,t=...,p=...$salt$hash` form, strict parser bounds, `subtle.ConstantTimeCompare`, and errors that do not include passwords or hash bytes.

- [ ] **Step 4: Implement JWT access token manager**

```go
type JWTConfig struct { Key []byte; Issuer, Audience string; TTL time.Duration }
type JWT struct { cfg JWTConfig }

func (m *JWT) Issue(identity user.Identity, now time.Time) (token string, expiresAt time.Time, err error)
func (m *JWT) Parse(raw string, now time.Time) (user.Identity, error)
```

Use `jwt.NewParser` with `WithValidMethods([]string{"HS256"})`, issuer, audience, leeway, issued-at, expiration and not-before validation. Parse numeric user IDs without float conversion.

- [ ] **Step 5: Implement refresh token generation**

Generate 32 random bytes, return base64url raw token, and return a lowercase hex SHA-256 digest for persistence. Expose a `Digest(raw string) string` helper for presented tokens. Random-reader failures must propagate.

Also implement a separate random identifier generator that returns 128-bit base64url identifiers for session IDs, refresh-token row IDs and JWT IDs. The authentication service consumes it through its own narrow `IDGenerator` interface.

- [ ] **Step 6: Run security tests**

Run: `gofmt -w security && go test ./security -v && go vet ./security`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add security
git commit -m "feat(security): 实现密码与令牌安全组件"
```

### Task 4A: Replace Viper with standard-library environment loading

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write failing standard-library loader tests**

Test a pure lookup-function loader without mutating process environment. Cover defaults, every `APP_*` override, present-but-empty required values, invalid bool/int/float/duration/list values, comma-separated CORS origins, and the existing validation matrix. Add an architecture assertion that `config` imports neither Viper nor mapstructure.

```go
func TestLoadFromEnvironment(t *testing.T) {
	env := map[string]string{"APP_JWT_KEY": strings.Repeat("k", 32)}
	cfg, err := loadFrom(func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil { t.Fatal(err) }
	if cfg.JWT.Key != env["APP_JWT_KEY"] { t.Fatalf("JWT key was not loaded") }
}
```

- [ ] **Step 2: Verify RED**

Run: `go test -count=1 ./config -v`

Expected: FAIL because the Viper-based API does not provide the pure standard-library loader/import boundary.

- [ ] **Step 3: Implement explicit environment decoding**

`Load` calls an unexported `loadFrom(os.LookupEnv)`. Start from typed safe development defaults, then use small field-specific helpers around `strconv.Atoi`, `strconv.ParseInt`, `strconv.ParseFloat`, `strconv.ParseBool`, `time.ParseDuration`, and comma-separated string parsing. If an environment key is present, parse its exact value; never treat an empty value as absent. Call the existing `Validate` once after decoding.

Remove `NewViper`, `LoadWith`, mapstructure tags and configuration-file behavior. Retain `Database.DSN`, validation, safe error categories and existing public configuration structs so database/security consumers do not change.

- [ ] **Step 4: Remove Viper from the module graph**

Remove the direct Cobra and Viper requirements. Pin `github.com/urfave/cli/v3@v3.10.1` for Task 13. Run `go mod tidy`, then restore the implementation plan's still-unused pinned future dependencies as explicit indirect requirements, excluding Cobra and Viper. Verify `go list -deps ./...` and `go mod why` show no Cobra/Viper source dependency.

- [ ] **Step 5: Verify and commit**

Run:

```bash
gofmt -w config
go test -count=1 ./config -v
go test -race -count=1 ./config
go vet ./config
go test -count=1 ./...
git diff --check
```

Commit:

```bash
git add config go.mod go.sum
git commit -m "refactor(config): 使用标准库加载环境配置"
```

### Task 5: Session service and MySQL user/RBAC/session adapters

**Files:**
- Create: `user/auth.go`
- Create: `user/auth_test.go`
- Create: `mysqlstore/user_store.go`
- Create: `mysqlstore/session_store.go`
- Create: `mysqlstore/user_store_test.go`
- Create: `mysqlstore/session_store_test.go`
- Create: `mysqlstore/testmysql_test.go`

- [ ] **Step 1: Write failing authentication service tests**

Use fake user/session stores and token primitives to cover login success, generic invalid credentials, disabled account, transparent password rehash, access authentication, permission authorization, refresh rotation, expired refresh, logout, and previous-token reuse revoking the session.

```go
func TestAuthRefreshReuseRevokesSession(t *testing.T) {
	sessions := newFakeSessions()
	svc := newAuthService(sessions)
	first, err := svc.Login(t.Context(), LoginInput{Email: "user@example.com", Password: validPassword})
	if err != nil { t.Fatal(err) }
	if _, err := svc.Refresh(t.Context(), first.RefreshToken); err != nil { t.Fatal(err) }
	if _, err := svc.Refresh(t.Context(), first.RefreshToken); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("second Refresh() error = %v", err)
	}
	if !sessions.revoked { t.Fatal("session was not revoked after refresh reuse") }
}
```

- [ ] **Step 2: Verify authentication tests fail**

Run: `go test ./user -run 'TestAuth' -v`

Expected: FAIL because `AuthService` and session ports are absent.

- [ ] **Step 3: Define and implement session application service**

```go
type Session struct {
	ID string
	UserID uint64
	Rotation uint64
	ExpiresAt time.Time
	RevokedAt, ReuseDetectedAt *time.Time
	CreatedAt, UpdatedAt time.Time
}

type RefreshToken struct {
	ID, SessionID, Digest string
	ExpiresAt time.Time
	ConsumedAt, RevokedAt *time.Time
	ReplacedByID string
}

type SessionStore interface {
	Create(context.Context, *Session, *RefreshToken) error
	Active(context.Context, string, uint64, time.Time) (*Session, error)
	Rotate(context.Context, RotateSessionInput) (RotateSessionResult, error)
	Revoke(context.Context, string, uint64, time.Time) error
}

type AccessTokens interface {
	Issue(Identity, time.Time) (string, time.Time, error)
	Parse(string, time.Time) (Identity, error)
}

type RefreshTokens interface {
	Generate() (raw, digest string, err error)
	Digest(string) string
}

type IDGenerator interface { NewID() (string, error) }

type AuthService struct { /* explicit stores, password manager, token managers, clock and TTL */ }
func (s *AuthService) Login(context.Context, LoginInput) (Tokens, error)
func (s *AuthService) Refresh(context.Context, string) (Tokens, error)
func (s *AuthService) Logout(context.Context, Identity) error
func (s *AuthService) Authenticate(context.Context, string) (Identity, error)
func (s *AuthService) Authorize(context.Context, Identity, string) error
```

Refresh rotation must delegate compare-and-swap and reuse detection to `SessionStore.Rotate`; it must not perform a read followed by an unprotected write.

- [ ] **Step 4: Write the MySQL integration harness**

Under `//go:build integration`, start `mysql:8.4`, obtain its connection string, open through `mysqlstore.Open`, run migrations up, and register cleanup that closes the DB then terminates the container. Expose `newTestDB(t)` to other `mysqlstore` integration tests. Do not use fixed ports or sleeps; use Testcontainers wait strategies.

- [ ] **Step 5: Implement user and RBAC store with GORM Gen**

Map unique email errors to `user.ErrEmailExists`, missing rows to `user.ErrNotFound`, and version mismatches to `user.ErrConflict` using `errors.Is`-compatible wrapping. `CreateWithRole` is transactional. `List` builds only whitelisted query expressions and always appends ID ordering. `IsLastActiveAdmin` performs a count inside the same transaction used by status/role changes so two concurrent revocations cannot remove the last administrator.

- [ ] **Step 6: Implement atomic session rotation**

Find the refresh-token row by its unique digest, then lock both it and its session with `clause.Locking{Strength: "UPDATE"}`. Compare SHA-256 digests in constant time. A current token is marked consumed, linked to the newly inserted token, and increments the session rotation counter. Any consumed token reuse sets `reuse_detected_at`, revokes the session and every unconsumed family token, then returns `user.ErrRefreshReuse`. Expired, revoked, or unknown tokens return stable errors without leaking which lookup failed.

- [ ] **Step 7: Run unit and integration tests**

Run:

```bash
gofmt -w user mysqlstore
go test ./user -run 'TestAuth' -v
go test -tags=integration ./mysqlstore -run 'TestUserStore|TestSessionStore' -v
go vet ./user ./mysqlstore/...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add user mysqlstore
git commit -m "feat(auth): 实现会话轮换与 RBAC 持久化"
```

### Task 6: Product and inventory domain

**Files:**
- Create: `product/errors.go`
- Create: `product/types.go`
- Create: `product/service.go`
- Create: `product/service_test.go`

- [ ] **Step 1: Write failing product tests**

Cover SKU normalization, required name, positive integer price, ISO 4217 currency validation, non-negative initial stock, create/update, publish/unpublish, stock increase/decrease, insufficient stock, zero adjustment, reason requirement, version conflicts, filters, sorting and pagination.

```go
func TestServiceAdjustStock(t *testing.T) {
	store := fakeStore{product: Product{ID: 9, SKU: "SKU-9", Stock: 5, Version: 2}}
	svc := NewService(&store, fakeClock{now: fixedTime})
	got, err := svc.AdjustStock(t.Context(), Actor{UserID: 1}, AdjustStockInput{
		ProductID: 9, Delta: -3, Reason: "damaged", Version: 2,
	})
	if err != nil { t.Fatal(err) }
	if got.Stock != 2 { t.Fatalf("Stock = %d", got.Stock) }
	if store.adjustment.Delta != -3 || store.adjustment.StockAfter != 2 { t.Fatal("incorrect stock ledger") }
}
```

- [ ] **Step 2: Verify tests fail**

Run: `go test ./product -v`

Expected: FAIL because package `product` does not exist.

- [ ] **Step 3: Define product types and consumer ports**

```go
type Status string
const (
	StatusDraft Status = "draft"
	StatusActive Status = "active"
	StatusInactive Status = "inactive"
)

type Money struct { Amount int64; Currency string }
type Product struct {
	ID uint64
	SKU, Name, Description string
	Price Money
	Stock uint32
	Status Status
	Version uint64
	CreatedAt, UpdatedAt time.Time
}

type Store interface {
	Create(context.Context, *Product) error
	ByID(context.Context, uint64, bool) (*Product, error)
	Update(context.Context, *Product, uint64) error
	AdjustStock(context.Context, AdjustmentInput) (*Product, error)
	List(context.Context, ListFilter, bool) (Page, error)
}

type Clock interface { Now() time.Time }
type SystemClock struct{}
func (SystemClock) Now() time.Time { return time.Now() }
```

- [ ] **Step 4: Implement business rules**

Only administrative service methods accept draft/inactive products. Public lookup/list force active-only behavior. Stock adjustments are explicit domain operations with actor, delta, reason and expected version; ordinary update cannot change stock. Currency is uppercased and restricted to a documented set containing `CNY`, `USD`, `EUR`, `JPY`, and `GBP`.

- [ ] **Step 5: Run product tests**

Run: `gofmt -w product && go test ./product -v && go vet ./product`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add product
git commit -m "feat(product): 实现商品目录与库存规则"
```

### Task 7: MySQL product and stock ledger adapter

**Files:**
- Create: `mysqlstore/product_store.go`
- Create: `mysqlstore/product_store_test.go`

- [ ] **Step 1: Write failing integration tests**

Use the shared MySQL container to test unique SKU mapping, active-only lookup, compound filtering, stable sorting, optimistic version conflict, atomic stock adjustment, insufficient stock, and concurrent adjustments that never make stock negative.

```go
func TestProductStoreConcurrentStockAdjustment(t *testing.T) {
	store := seedProductStore(t, 1)
	var successes atomic.Int64
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := store.AdjustStock(t.Context(), product.AdjustmentInput{ProductID: 1, Delta: -1, Reason: "sale"})
			if err == nil { successes.Add(1) }
		})
	}
	wg.Wait()
	if successes.Load() != 1 { t.Fatalf("successes = %d", successes.Load()) }
}
```

- [ ] **Step 2: Verify integration tests fail**

Run: `go test -tags=integration ./mysqlstore -run TestProductStore -v`

Expected: FAIL because `ProductStore` is absent.

- [ ] **Step 3: Implement GORM Gen product adapter**

Use generated fields for filters and sorts. For stock adjustment, begin a transaction, lock the product row, compute the new stock using checked integer arithmetic, reject negative results, update with a version predicate, insert `stock_adjustments`, and commit. Add comments explaining why stock cannot be changed by ordinary updates and why the row lock is required.

- [ ] **Step 4: Run adapter tests**

Run: `gofmt -w mysqlstore && go test -tags=integration ./mysqlstore -run TestProductStore -v && go vet ./mysqlstore/...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mysqlstore
git commit -m "feat(product): 实现商品与库存 MySQL 适配器"
```

### Task 8: Order aggregate, state machine, queries, and checkout service

**Files:**
- Create: `order/errors.go`
- Create: `order/types.go`
- Create: `order/service.go`
- Create: `order/query.go`
- Create: `order/number.go`
- Create: `order/service_test.go`
- Create: `order/query_test.go`

- [ ] **Step 1: Write failing order tests**

Cover empty order, duplicate product consolidation, zero quantity, mixed currency, checked subtotal/total overflow, server price snapshots, insufficient stock, deterministic lock ordering, idempotent replay, idempotency payload conflict, customer/admin cancellation permissions, inventory restoration, every legal transition, every illegal transition, query filters and stable sorting.

```go
func TestCheckoutSortsLocksAndUsesServerPrices(t *testing.T) {
	tx := fakeTx{products: map[uint64]SellableProduct{
		2: {ID: 2, SKU: "B", Name: "B", UnitPrice: Money{Amount: 500, Currency: "CNY"}, Stock: 2},
		1: {ID: 1, SKU: "A", Name: "A", UnitPrice: Money{Amount: 300, Currency: "CNY"}, Stock: 2},
	}}
	svc := NewService(fakeTransactor{tx: &tx}, fakeReader{}, fakeClock{now: fixedTime})
	got, err := svc.Create(t.Context(), 7, CreateInput{IdempotencyKey: "checkout-1", Items: []RequestedItem{{ProductID: 2, Quantity: 1}, {ProductID: 1, Quantity: 2}}})
	if err != nil { t.Fatal(err) }
	if diff := cmp.Diff([]uint64{1, 2}, tx.lockedIDs); diff != "" { t.Fatal(diff) }
	if got.Total.Amount != 1100 { t.Fatalf("total = %d", got.Total.Amount) }
}
```

- [ ] **Step 2: Verify tests fail**

Run: `go test ./order -v`

Expected: FAIL because package `order` is not implemented.

- [ ] **Step 3: Define aggregate values and ports**

```go
type Status string
const (
	StatusPending Status = "pending"
	StatusConfirmed Status = "confirmed"
	StatusShipped Status = "shipped"
	StatusDelivered Status = "delivered"
	StatusCancelled Status = "cancelled"
)

type Money struct { Amount int64; Currency string }
type Item struct { ID, ProductID uint64; SKU, Name string; UnitPrice, Subtotal Money; Quantity uint32 }
type Order struct { ID, UserID uint64; Number string; Status Status; Total Money; Items []Item; Version uint64; CreatedAt, UpdatedAt time.Time }

type Transactor interface { WithinTransaction(context.Context, func(Tx) error) error }
type Tx interface {
	ClaimIdempotency(context.Context, IdempotencyClaim) (IdempotencyResult, error)
	LockProducts(context.Context, []uint64) ([]SellableProduct, error)
	DecreaseStock(context.Context, StockChange) error
	IncreaseStock(context.Context, StockChange) error
	CreateOrder(context.Context, *Order) error
	CompleteIdempotency(context.Context, IdempotencyCompletion) error
	LockOrder(context.Context, uint64) (*Order, error)
	UpdateStatus(context.Context, *Order, uint64) error
}
type Reader interface { ByID(context.Context, uint64) (*Order, error); List(context.Context, ListFilter) (Page, error) }
type Clock interface { Now() time.Time }
type SystemClock struct{}
func (SystemClock) Now() time.Time { return time.Now() }
```

- [ ] **Step 4: Implement checkout and state machine**

Canonicalize the request before hashing: combine repeated product IDs, sort by product ID, encode product ID and quantity in fixed binary form, and hash with SHA-256. Create an order number from UTC date plus a cryptographically random suffix injected through a `NumberGenerator` port; do not derive uniqueness from an in-memory counter.

Use checked multiplication and addition for money. Every state transition is a method on `Order` that validates the current state. The service enforces ownership and admin permissions before entering persistence operations.

Implement `NumberGenerator` in `order/number.go` with an injected `io.Reader`. It emits `ORD-YYYYMMDD-<base32 random suffix>` from UTC time and 80 random bits, so uniqueness does not depend on process-local counters and tests can be deterministic.

- [ ] **Step 5: Implement validated list filters**

`ListFilter` includes user ID, statuses, created-from/to, minimum/maximum amount, sort, direction, limit and offset. Reject inverted ranges, unknown states, unknown sort fields, limit outside 1–100, and negative offset.

- [ ] **Step 6: Run order tests**

Run: `gofmt -w order && go test ./order -v && go vet ./order`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add order
git commit -m "feat(order): 实现订单聚合与结算规则"
```

### Task 9: Transactional MySQL order adapter

**Files:**
- Create: `mysqlstore/order_store.go`
- Create: `mysqlstore/order_store_test.go`

- [ ] **Step 1: Write failing integration tests**

Test real MySQL transactions for successful checkout, service-side price snapshot, unique order number, insufficient stock rollback, idempotent replay, different payload conflict, concurrent same-key checkout creating one order, concurrent last-stock checkout producing one winner, cancellation restoration, transition version conflict, compound filters, and deterministic ordering.

```go
func TestOrderStoreConcurrentLastStock(t *testing.T) {
	db := newTestDB(t)
	seedActiveProduct(t, db, 1, 1, 500, "CNY")
	svc := newOrderService(db)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			_, err := svc.Create(t.Context(), uint64(i+1), order.CreateInput{
				IdempotencyKey: fmt.Sprintf("key-%d", i), Items: []order.RequestedItem{{ProductID: 1, Quantity: 1}},
			})
			results <- err
		})
	}
	wg.Wait(); close(results)
	assertOneSuccessAndOne(t, results, order.ErrInsufficientStock)
}
```

- [ ] **Step 2: Verify integration tests fail**

Run: `go test -tags=integration ./mysqlstore -run TestOrderStore -v`

Expected: FAIL because the transaction adapter is absent.

- [ ] **Step 3: Implement transaction wrapper and bounded retry**

`WithinTransaction` uses `db.WithContext(ctx).Transaction`. Retry at most three times only for MySQL deadlock 1213 and lock wait timeout 1205, with Context-aware bounded jitter. Each retry receives a fresh transaction and reruns the entire callback. Non-retryable errors return immediately.

- [ ] **Step 4: Implement checkout persistence**

Claim idempotency by inserting the unique row. On duplicate key, lock and compare the stored request hash; return the stored order only when the hash matches and `order_id` is present. Lock products in sorted ID order with `FOR UPDATE`, update stock with explicit version/count checks, write stock ledger rows, insert order/items, and complete the idempotency row before commit.

- [ ] **Step 5: Implement cancellation and query persistence**

Lock the order before status change. For cancellation, lock associated products in sorted ID order, restore each quantity, write stock ledger rows, and update order status/version in the same transaction. Query mapping must preload items in deterministic item-ID order and never expose persistence models.

- [ ] **Step 6: Run integration tests and race-safe unit tests**

Run:

```bash
gofmt -w mysqlstore
go test ./order -race
go test -tags=integration ./mysqlstore -run TestOrderStore -v
go vet ./mysqlstore/...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add mysqlstore
git commit -m "feat(order): 实现订单与库存事务适配器"
```

### Task 10: Uniform HTTP envelope, strict decoding, and middleware

**Files:**
- Create: `web/response.go`
- Create: `web/response_test.go`
- Create: `web/error.go`
- Create: `web/error_test.go`
- Create: `web/decode.go`
- Create: `web/decode_test.go`
- Create: `web/pagination.go`
- Create: `web/pagination_test.go`
- Create: `web/middleware.go`
- Create: `web/middleware_test.go`

- [ ] **Step 1: Write failing response contract tests**

Assert exact JSON keys: success has `code: 0`, optional `data`, and no `message`; failure has nonzero integer `code`, nonempty `message`, optional `data`; no response contains `success`, `meta`, `details`, or `request_id`. Pagination must be nested under `data.pagination`.

```go
func TestWriteFailureContract(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeFailure(c, http.StatusBadRequest, Problem{Code: CodeValidation, Message: "request validation failed", Data: FieldErrors{{Field: "email", Reason: "invalid_email"}}})
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil { t.Fatal(err) }
	if got["code"] != float64(10001) || got["message"] == "" { t.Fatalf("body = %#v", got) }
	for _, forbidden := range []string{"success", "meta", "details", "request_id"} {
		if _, ok := got[forbidden]; ok { t.Errorf("unexpected key %q", forbidden) }
	}
}
```

- [ ] **Step 2: Write failing middleware tests**

Cover request ID header propagation, panic recovery, access logs without Authorization, CORS allow/deny/preflight, trusted proxies, security headers, max body size, login rate limiting, canceled Context, and middleware ordering.

- [ ] **Step 3: Verify tests fail**

Run: `go test ./web -run 'TestWrite|TestDecode|TestMiddleware|TestCORS|TestRate' -v`

Expected: FAIL because package `web` is absent.

- [ ] **Step 4: Implement response and error taxonomy**

```go
type envelope struct {
	Code int `json:"code"`
	Message string `json:"message,omitempty"`
	Data any `json:"data,omitempty"`
}

const (
	CodeOK = 0
	CodeMalformedJSON = 10000
	CodeValidation = 10001
	CodeAuthentication = 20000
	CodePermission = 20001
	CodeSessionExpired = 20002
	CodeUserNotFound = 30000
	CodeEmailExists = 30001
	CodeProductNotFound = 40000
	CodeInsufficientStock = 40001
	CodeOrderNotFound = 50000
	CodeInvalidTransition = 50001
	CodeIdempotencyConflict = 50002
	CodeInternal = 90000
)
```

Create one central `problemFromError(error) (status int, problem Problem)` using `errors.Is`. Unknown errors map to HTTP 500 and `CodeInternal` without exposing the original text.

- [ ] **Step 5: Implement strict JSON and pagination parsing**

Use `http.MaxBytesReader`, `json.Decoder.DisallowUnknownFields`, exactly one JSON value, and field-specific validation data. Query parsers return errors for repeated singleton parameters, invalid integers, limit outside 1–100, negative offset, unknown sort or direction, malformed UTC timestamps, and inverted ranges.

- [ ] **Step 6: Implement middleware**

Use injected `*slog.Logger`, configured CORS origins, trusted proxies, and `x/time/rate`. The limiter owns a bounded map of IP/account keys and a cleanup goroutine that stops on Context cancellation. Recovery records stack information only in logs. Access logs include method, normalized path, status, response bytes, duration and request ID, but never headers containing credentials.

- [ ] **Step 7: Run web foundation tests**

Run: `gofmt -w web && go test ./web -run 'TestWrite|TestDecode|TestMiddleware|TestCORS|TestRate' -v && go vet ./web`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web
git commit -m "feat(web): 建立统一响应与安全中间件"
```

### Task 11: Authentication, user, and product HTTP handlers

**Files:**
- Create: `web/auth_handler.go`
- Create: `web/auth_handler_test.go`
- Create: `web/user_handler.go`
- Create: `web/user_handler_test.go`
- Create: `web/product_handler.go`
- Create: `web/product_handler_test.go`

- [ ] **Step 1: Write failing authentication handler tests**

Test register 201, duplicate email 409, malformed JSON 400, login success with the exact token fields, generic invalid credentials 401, refresh rotation, logout, missing bearer token, malformed bearer token, revoked session, and permission denial.

```go
func TestLoginResponse(t *testing.T) {
	r := testRouterWithAuth(fakeAuth{tokens: user.Tokens{AccessToken: "access", RefreshToken: "refresh", AccessExpiresIn: 900, RefreshExpiresIn: 2592000}})
	w := performJSON(r, http.MethodPost, "/api/v1/auth/login", `{"email":"user@example.com","password":"correct horse battery staple"}`)
	assertEnvelope(t, w, http.StatusOK, 0)
	assertJSONPath(t, w.Body.Bytes(), "data.token_type", "Bearer")
	assertJSONPath(t, w.Body.Bytes(), "data.refresh_token", "refresh")
}
```

- [ ] **Step 2: Write failing user/product handler tests**

Cover current profile, profile update, admin user filters, status update, role replacement, public active-product list/get, admin product creation/update, stock adjustment, pagination in `data.pagination`, invalid filter codes, and sensitive field absence.

- [ ] **Step 3: Verify tests fail**

Run: `go test ./web -run 'TestAuthHandler|TestUserHandler|TestProductHandler' -v`

Expected: FAIL because handlers are absent.

- [ ] **Step 4: Implement auth and identity middleware adapters**

Handlers consume narrow interfaces defined in `web`, not concrete services. Authorization middleware extracts one `Bearer` token, calls `Authenticate`, stores a typed identity under a private context key, and permission middleware calls `Authorize` for the route permission. Missing or duplicate Authorization values fail closed.

Auth request/response DTOs:

```go
type registerRequest struct { Email string `json:"email"`; Name string `json:"name"`; Password string `json:"password"` }
type loginRequest struct { Email string `json:"email"`; Password string `json:"password"` }
type refreshRequest struct { RefreshToken string `json:"refresh_token"` }
type tokenDTO struct { TokenType, AccessToken, RefreshToken string; ExpiresIn, RefreshExpiresIn int64 }
```

- [ ] **Step 5: Implement user and product DTO mapping**

Response DTOs contain only public fields. Money is encoded as `{amount, currency}`. Times use UTC RFC3339Nano. IDs are JSON integers. Admin endpoints call services only after permission middleware; public product endpoints force active-only service methods rather than trusting query flags.

- [ ] **Step 6: Run handler tests**

Run: `gofmt -w web && go test ./web -run 'TestAuthHandler|TestUserHandler|TestProductHandler' -v && go vet ./web`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web
git commit -m "feat(api): 实现认证用户与商品接口"
```

### Task 12: Order, reporting, complete routing, and API contract tests

**Files:**
- Create: `reporting/types.go`
- Create: `reporting/service.go`
- Create: `reporting/service_test.go`
- Create: `mysqlstore/reporting_store.go`
- Create: `mysqlstore/reporting_store_test.go`
- Create: `web/order_handler.go`
- Create: `web/order_handler_test.go`
- Create: `web/stats_handler.go`
- Create: `web/stats_handler_test.go`
- Create: `web/router.go`
- Create: `web/router_test.go`

- [ ] **Step 1: Write failing reporting tests**

Define time-bounded user, inventory and order aggregates. Validate UTC `from < to`, enforce a maximum 366-day range, and test exact counts/sums against MySQL fixtures.

```go
type Snapshot struct {
	Users Users
	Inventory Inventory
	Orders Orders
}
type Users struct { Total, Active, New uint64 }
type Inventory struct { ActiveProducts uint64; UnitsInStock uint64; StockValue int64; Currency string }
type Orders struct { Total uint64; Pending, Confirmed, Shipped, Delivered, Cancelled uint64; GrossAmount int64; Currency string }
```

- [ ] **Step 2: Write failing order and routing contract tests**

Cover create with mandatory `Idempotency-Key`, replay, payload conflict, customer ownership, customer cancel, every admin transition, admin compound listing, missing/invalid permission, full route inventory, liveness, readiness success, readiness DB failure, 404 envelope, and 405 envelope.

- [ ] **Step 3: Verify tests fail**

Run: `go test ./reporting ./web -run 'TestReporting|TestOrderHandler|TestRouter|TestHealth' -v`

Expected: FAIL because reporting, order handlers, and final router are absent.

- [ ] **Step 4: Implement reporting service and MySQL read model**

`reporting.Service` validates the range then delegates to a read-only store. The MySQL adapter uses explicit aggregate queries and requires a single currency in the selected range; mixed currencies return a defined `reporting.ErrMixedCurrency` instead of adding unlike money.

- [ ] **Step 5: Implement order and stats handlers**

Create parses `Idempotency-Key` as 1–128 visible ASCII characters. Customer list forces the authenticated user ID. Admin list honors validated filters. State commands return the updated order. Order DTO includes immutable item snapshots and monetary objects.

- [ ] **Step 6: Build the complete router**

```go
type Dependencies struct {
	Auth AuthService
	Users UserService
	Products ProductService
	Orders OrderService
	Stats StatsService
	Readiness func(context.Context) error
	Logger *slog.Logger
	Config config.Config
}

func NewRouter(ctx context.Context, deps Dependencies) (*gin.Engine, error)
```

Register exactly the approved routes. Apply middleware in this order: request ID, recovery, security headers, CORS, access log, route-specific rate limits, authentication, authorization. Register `NoRoute` and `NoMethod` handlers that use the same envelope.

- [ ] **Step 7: Run contract and integration tests**

Run:

```bash
gofmt -w reporting mysqlstore web
go test ./reporting ./web -v
go test -tags=integration ./mysqlstore -run TestReportingStore -v
go vet ./reporting ./web ./mysqlstore/...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add reporting mysqlstore web
git commit -m "feat(api): 实现订单统计与完整路由"
```

### Task 13: urfave/cli admin CLI, Wire composition, and graceful server

**Files:**
- Delete: `cmd/main.go`
- Create: `cmd/server/main.go`
- Create: `cmd/server/run.go`
- Create: `cmd/server/run_test.go`
- Create: `cmd/server/wire.go`
- Create: `cmd/server/wire_gen.go`
- Create: `cmd/admin/main.go`
- Create: `cmd/admin/root.go`
- Create: `cmd/admin/root_test.go`
- Create: `cmd/admin/migrate.go`
- Create: `cmd/admin/bootstrap.go`
- Create: `cmd/admin/wire.go`
- Create: `cmd/admin/wire_gen.go`

- [ ] **Step 1: Write failing in-memory CLI tests**

Build a fresh command tree per test. Execute through the root with `SetArgs`. Cover help, unknown command, `migrate up`, `migrate down --steps 1 --confirm`, rejection without confirmation, `bootstrap-admin` required flags, standard-library environment loading, duplicate admin email, and output through `cmd.OutOrStdout()`.

```go
func TestMigrateDownRequiresConfirmation(t *testing.T) {
	root := NewRootCmd(fakeAdminDeps())
	root.SetArgs([]string{"migrate", "down", "--steps", "1"})
	err := root.ExecuteContext(t.Context())
	if err == nil || !strings.Contains(err.Error(), "--confirm") { t.Fatalf("error = %v", err) }
}
```

- [ ] **Step 2: Write failing server lifecycle tests**

Use a listener on `127.0.0.1:0`, cancel Context after readiness, assert the server stops within the configured shutdown timeout, and assert close functions run exactly once. Also test bind failure and shutdown timeout propagation without sleeps by synchronizing on channels.

- [ ] **Step 3: Verify tests fail**

Run: `go test ./cmd/admin ./cmd/server -v`

Expected: FAIL because command and server packages are absent.

- [ ] **Step 4: Implement urfave/cli command factories**

`NewApp` constructs a fresh `*cli.Command` tree, loads application settings once through the standard-library `config.Load`, and keeps command-specific flags local to each command. Actions accept `context.Context` and `*cli.Command`, return errors, and call injected functions; no package globals or process exits below `main`. `migrate down` requires `--steps > 0` and `--confirm`. `bootstrap-admin` calls `user.Service.BootstrapAdmin`; password comes from a flag only when explicitly supplied, otherwise from a no-echo terminal prompt. Never print the password.

- [ ] **Step 5: Implement server lifecycle**

`run(ctx, app)` creates an `http.Server` with all timeouts, starts it in one goroutine with a buffered result channel, waits for startup failure or Context cancellation, then calls `Shutdown` with a fresh timeout Context. Treat `http.ErrServerClosed` as success. Close DB and middleware background tasks with `errors.Join`.

- [ ] **Step 6: Add Wire injectors and generate code**

Provider sets bind only genuine consumer interfaces. `wire.go` uses only `//go:build wireinject`; do not add deprecated `// +build`. Generate through `go tool wire ./cmd/server ./cmd/admin`, commit `wire_gen.go`, and verify a second generation has no diff.

- [ ] **Step 7: Run CLI/server tests and builds**

Run:

```bash
gofmt -w cmd
go tool wire ./cmd/server ./cmd/admin
go test ./cmd/admin ./cmd/server -v
go build ./cmd/server ./cmd/admin
go vet ./cmd/...
```

Expected: PASS and clean Wire regeneration.

- [ ] **Step 8: Commit**

```bash
git add cmd go.mod go.sum
git commit -m "feat(runtime): 实现管理命令与优雅服务生命周期"
```

### Task 14: Remove legacy architecture, update deployment/docs, and verify end to end

**Files:**
- Delete: `internal/`
- Modify: `Dockerfile`
- Modify: `docker-compose.yaml`
- Modify: `env.example`
- Modify: `justfile`
- Replace: `README.md`
- Create: `docs/architecture.md`
- Create: `docs/api.md`
- Create: `docs/error-codes.md`
- Remove or rewrite: obsolete files under `docs/`

- [ ] **Step 1: Write failing repository-level acceptance checks**

Add `web/contract_test.go` to enumerate every approved route and verify the response envelope for representative success/failure paths. Add `architecture_test.go` at module root to run `go list -deps` and fail if `user`, `product`, `order`, or `reporting` imports Gin, GORM, MySQL driver, JWT, Cobra, or Viper.

```go
func TestDomainPackagesDoNotImportAdapters(t *testing.T) {
	for _, pkg := range []string{"./user", "./product", "./order", "./reporting"} {
		cmd := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, pkg)
		out, err := cmd.Output(); if err != nil { t.Fatal(err) }
		for _, forbidden := range []string{"gin-gonic", "gorm.io", "go-sql-driver", "golang-jwt", "urfave/cli", "cobra", "viper"} {
			if bytes.Contains(out, []byte(forbidden)) { t.Errorf("%s imports %s", pkg, forbidden) }
		}
	}
}

func TestLegacyArchitectureRemoved(t *testing.T) {
	if _, err := os.Stat("internal"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy internal directory still exists: %v", err)
	}
}
```

- [ ] **Step 2: Verify acceptance checks expose legacy paths**

Run: `go test ./...`

Expected: FAIL until the old `cmd/main.go` and `internal/` architecture are removed and route/build assumptions are updated.

- [ ] **Step 3: Remove obsolete implementation and preserve relevant rationale**

Delete the legacy `internal/` packages and old `cmd/main.go`. Before deletion, compare every existing explanatory comment with replacement code: migrate still-valid domain, security, transaction and conversion rationale to the owning new file; rewrite comments whose semantics changed; do not mechanically strip comments from code that survives.

- [ ] **Step 4: Update local and container operations**

Use a Go 1.25 build image, build `./cmd/server`, run as a non-root user, copy only the binary and CA/timezone data, add a health check, and stop copying `env.example` into the image. Compose uses MySQL 8.4, a named volume, health-based startup, migration as an explicit one-shot service, and no embedded default JWT key.

Update `justfile` recipes for `fmt`, `test`, `test-race`, `test-integration`, `generate`, `wire`, `migrate-up`, `migrate-down`, `bootstrap-admin`, `run`, `build`, and `docker-build`. Every recipe must call the new binaries/paths.

- [ ] **Step 5: Rewrite user-facing documentation**

README must include prerequisites, secure environment setup, migration, admin bootstrap, server startup, tests, generation, and Docker. `docs/architecture.md` documents dependency direction and transaction flow. `docs/api.md` documents every route, permission, request and response shape. `docs/error-codes.md` lists every nonzero integer code and HTTP mapping. Remove claims of infinite scalability and descriptions of unimplemented CQRS/event-driven behavior.

- [ ] **Step 6: Run generation consistency and complete verification**

Run:

```bash
gofmt -w $(git ls-files '*.go')
go generate ./...
git diff --exit-code -- mysqlstore/query cmd/server/wire_gen.go cmd/admin/wire_gen.go
go test ./...
go test -race ./...
go test -tags=integration ./...
go vet ./...
go build ./cmd/server ./cmd/admin
docker build -t clean-arch-gin:test .
git diff --check
```

Expected: every command exits 0. If Docker is unavailable, report the exact daemon error as a verification blocker; do not mark integration or image verification successful.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: 完成领域优先 Clean Architecture 重构"
```

## Plan self-review checklist

- Every requirement in the approved design maps to at least one task.
- Runtime interfaces are consumer-owned and concrete adapters remain outside domain packages.
- Authentication, refresh rotation, RBAC, inventory, idempotency and order state transitions have both unit and MySQL integration coverage.
- Uniform response rules exactly match the approved `code/message/data` contract.
- No legacy compatibility layer survives Task 14.
- Every implementation task starts from a failing test and ends with focused verification and a commit.
