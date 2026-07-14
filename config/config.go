// Package config loads and validates the application's external configuration.
package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	mysqlconfig "github.com/go-sql-driver/mysql"
)

const (
	minimumJWTKeyBytes = 32
	maximumJWTLeeway   = 5 * time.Minute
	minimumArgonMemory = 19 * 1024
	maximumArgonMemory = 1024 * 1024
)

// Config is the validated application configuration passed to runtime components.
type Config struct {
	Environment string
	HTTP        HTTP
	DB          Database
	JWT         JWT
	Password    Password
	CORS        CORS
	RateLimit   RateLimit
}

// HTTP contains server limits and lifecycle timeouts.
type HTTP struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
}

// Database contains MySQL connection and database/sql pool settings.
type Database struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	MaxIdle         int
	MaxOpen         int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// JWT contains token signing identity and expiry settings.
type JWT struct {
	Key        string
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Leeway     time.Duration
}

// Password contains bounded Argon2id work and output parameters. Signed wide
// fields keep weak decoding from wrapping hostile negative or oversized input.
type Password struct {
	Memory      int64
	Iterations  int64
	Parallelism int64
	SaltLength  int64
	KeyLength   int64
}

// CORS contains the browser origins explicitly allowed to call the API.
type CORS struct {
	AllowedOrigins   []string
	AllowCredentials bool
}

// RateLimit contains login throttling parameters consumed by the HTTP middleware.
type RateLimit struct {
	LoginRequestsPerSecond float64
	LoginBurst             int
}

// defaultConfig returns an isolated, typed configuration suitable for one application load.
// Every consumed key has a typed default so absence is explicit and independent of process state.
func defaultConfig() Config {
	return Config{
		Environment: "development",
		HTTP: HTTP{
			Address:           ":8080",
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       2 * time.Minute,
			ShutdownTimeout:   10 * time.Second,
			MaxBodyBytes:      1 << 20,
		},
		DB: Database{
			Host: "127.0.0.1",
			Port: 3306,
			User: "app",
			// An empty database password is safe as a development default and is rejected in production.
			Password:        "",
			Name:            "clean_arch",
			MaxIdle:         10,
			MaxOpen:         25,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 5 * time.Minute,
		},
		JWT: JWT{
			// JWT keys intentionally have no usable default so startup cannot silently use a shared secret.
			Key:        "",
			Issuer:     "clean-arch-gin",
			Audience:   "clean-arch-api",
			AccessTTL:  15 * time.Minute,
			RefreshTTL: 7 * 24 * time.Hour,
			Leeway:     0,
		},
		Password: Password{
			Memory:      64 * 1024,
			Iterations:  3,
			Parallelism: 2,
			SaltLength:  16,
			KeyLength:   32,
		},
		CORS: CORS{
			AllowedOrigins:   []string{},
			AllowCredentials: false,
		},
		RateLimit: RateLimit{
			LoginRequestsPerSecond: 1,
			LoginBurst:             5,
		},
	}
}

// Load reads the current process environment through the standard library and validates it.
func Load() (Config, error) {
	return loadFrom(os.LookupEnv)
}

// loadFrom loads configuration through an injected lookup without global state or process mutation.
func loadFrom(lookup func(string) (string, bool)) (Config, error) {
	cfg := defaultConfig()

	// Explicit empty values must outrank defaults so required settings fail closed.
	loadEnvironment(lookup, "APP_ENVIRONMENT", &cfg.Environment)
	loadTrimmedString(lookup, "APP_HTTP_ADDRESS", &cfg.HTTP.Address)
	loadTrimmedString(lookup, "APP_DB_HOST", &cfg.DB.Host)
	loadString(lookup, "APP_DB_USER", &cfg.DB.User)
	loadString(lookup, "APP_DB_PASSWORD", &cfg.DB.Password)
	loadString(lookup, "APP_DB_NAME", &cfg.DB.Name)
	loadString(lookup, "APP_JWT_KEY", &cfg.JWT.Key)
	loadString(lookup, "APP_JWT_ISSUER", &cfg.JWT.Issuer)
	loadString(lookup, "APP_JWT_AUDIENCE", &cfg.JWT.Audience)

	// Explicit field assignments keep the accepted environment surface auditable and fail on weak coercion.
	if err := loadDuration(lookup, "APP_HTTP_READ_HEADER_TIMEOUT", &cfg.HTTP.ReadHeaderTimeout); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_HTTP_READ_TIMEOUT", &cfg.HTTP.ReadTimeout); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_HTTP_WRITE_TIMEOUT", &cfg.HTTP.WriteTimeout); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_HTTP_IDLE_TIMEOUT", &cfg.HTTP.IdleTimeout); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_HTTP_SHUTDOWN_TIMEOUT", &cfg.HTTP.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if err := loadInt64(lookup, "APP_HTTP_MAX_BODY_BYTES", &cfg.HTTP.MaxBodyBytes); err != nil {
		return Config{}, err
	}
	if err := loadInt(lookup, "APP_DB_PORT", &cfg.DB.Port); err != nil {
		return Config{}, err
	}
	if err := loadInt(lookup, "APP_DB_MAX_IDLE", &cfg.DB.MaxIdle); err != nil {
		return Config{}, err
	}
	if err := loadInt(lookup, "APP_DB_MAX_OPEN", &cfg.DB.MaxOpen); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_DB_CONN_MAX_LIFETIME", &cfg.DB.ConnMaxLifetime); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_DB_CONN_MAX_IDLE_TIME", &cfg.DB.ConnMaxIdleTime); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_JWT_ACCESS_TTL", &cfg.JWT.AccessTTL); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_JWT_REFRESH_TTL", &cfg.JWT.RefreshTTL); err != nil {
		return Config{}, err
	}
	if err := loadDuration(lookup, "APP_JWT_LEEWAY", &cfg.JWT.Leeway); err != nil {
		return Config{}, err
	}
	if err := loadInt64(lookup, "APP_PASSWORD_MEMORY", &cfg.Password.Memory); err != nil {
		return Config{}, err
	}
	if err := loadInt64(lookup, "APP_PASSWORD_ITERATIONS", &cfg.Password.Iterations); err != nil {
		return Config{}, err
	}
	if err := loadInt64(lookup, "APP_PASSWORD_PARALLELISM", &cfg.Password.Parallelism); err != nil {
		return Config{}, err
	}
	if err := loadInt64(lookup, "APP_PASSWORD_SALT_LENGTH", &cfg.Password.SaltLength); err != nil {
		return Config{}, err
	}
	if err := loadInt64(lookup, "APP_PASSWORD_KEY_LENGTH", &cfg.Password.KeyLength); err != nil {
		return Config{}, err
	}
	if err := loadStringList(lookup, "APP_CORS_ALLOWED_ORIGINS", &cfg.CORS.AllowedOrigins); err != nil {
		return Config{}, err
	}
	if err := loadBool(lookup, "APP_CORS_ALLOW_CREDENTIALS", &cfg.CORS.AllowCredentials); err != nil {
		return Config{}, err
	}
	if err := loadFloat64(lookup, "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND", &cfg.RateLimit.LoginRequestsPerSecond); err != nil {
		return Config{}, err
	}
	if err := loadInt(lookup, "APP_RATE_LIMIT_LOGIN_BURST", &cfg.RateLimit.LoginBurst); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}
	return cfg, nil
}

// Validate rejects unsafe, incomplete, or internally inconsistent configuration.
func (c Config) Validate() error {
	switch c.Environment {
	case "development", "test", "production":
	default:
		return fmt.Errorf("environment must be development, test, or production")
	}
	if err := c.HTTP.validate(); err != nil {
		return fmt.Errorf("http: %w", err)
	}
	if err := c.DB.validate(); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	if c.Environment == "production" && strings.TrimSpace(c.DB.Password) == "" {
		return fmt.Errorf("db: password is required in production")
	}
	if err := c.JWT.validate(c.Environment); err != nil {
		return fmt.Errorf("jwt: %w", err)
	}
	if err := c.Password.validate(); err != nil {
		return fmt.Errorf("password: %w", err)
	}
	if err := c.CORS.validate(); err != nil {
		return fmt.Errorf("cors: %w", err)
	}
	if err := c.RateLimit.validate(); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}
	return nil
}

// DSN formats a MySQL connection string using the driver implementation so credentials
// and database names follow its escaping rules instead of hand-rolled concatenation.
func (d Database) DSN() string {
	cfg := mysqlconfig.Config{
		User:      d.User,
		Passwd:    d.Password,
		Net:       "tcp",
		Addr:      net.JoinHostPort(d.Host, strconv.Itoa(d.Port)),
		DBName:    d.Name,
		Collation: "utf8mb4_unicode_ci",
		ParseTime: true,
		Loc:       time.UTC,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}
	return cfg.FormatDSN()
}

func loadEnvironment(lookup func(string) (string, bool), key string, target *string) {
	if value, ok := lookup(key); ok {
		*target = strings.ToLower(strings.TrimSpace(value))
	}
}

func loadString(lookup func(string) (string, bool), key string, target *string) {
	if value, ok := lookup(key); ok {
		*target = value
	}
}

func loadTrimmedString(lookup func(string) (string, bool), key string, target *string) {
	if value, ok := lookup(key); ok {
		*target = strings.TrimSpace(value)
	}
}

func loadInt(lookup func(string) (string, bool), key string, target *int) error {
	value, ok := lookup(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid base-10 integer", key)
	}
	*target = parsed
	return nil
}

func loadInt64(lookup func(string) (string, bool), key string, target *int64) error {
	value, ok := lookup(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("%s must be a valid signed 64-bit integer", key)
	}
	*target = parsed
	return nil
}

func loadBool(lookup func(string) (string, bool), key string, target *bool) error {
	value, ok := lookup(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid boolean", key)
	}
	*target = parsed
	return nil
}

func loadFloat64(lookup func(string) (string, bool), key string, target *float64) error {
	value, ok := lookup(key)
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return fmt.Errorf("%s must be a valid finite number", key)
	}
	*target = parsed
	return nil
}

func loadDuration(lookup func(string) (string, bool), key string, target *time.Duration) error {
	value, ok := lookup(key)
	if !ok {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid duration", key)
	}
	*target = parsed
	return nil
}

func loadStringList(lookup func(string) (string, bool), key string, target *[]string) error {
	value, ok := lookup(key)
	if !ok {
		return nil
	}

	parts := strings.Split(value, ",")
	parsed := make([]string, len(parts))
	for index, part := range parts {
		parsed[index] = strings.TrimSpace(part)
		if parsed[index] == "" {
			return fmt.Errorf("%s must not contain an empty element", key)
		}
	}
	*target = parsed
	return nil
}

func (h HTTP) validate() error {
	address := strings.TrimSpace(h.Address)
	if address == "" {
		return fmt.Errorf("address is required")
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("address %q must contain a valid host and port: %w", h.Address, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("address %q has an invalid port", h.Address)
	}

	timeouts := []struct {
		name  string
		value time.Duration
	}{
		{name: "read header timeout", value: h.ReadHeaderTimeout},
		{name: "read timeout", value: h.ReadTimeout},
		{name: "write timeout", value: h.WriteTimeout},
		{name: "idle timeout", value: h.IdleTimeout},
		{name: "shutdown timeout", value: h.ShutdownTimeout},
	}
	for _, timeout := range timeouts {
		if timeout.value <= 0 {
			return fmt.Errorf("%s must be positive", timeout.name)
		}
	}
	if h.MaxBodyBytes <= 0 {
		return fmt.Errorf("max body bytes must be positive")
	}
	return nil
}

func (d Database) validate() error {
	if strings.TrimSpace(d.Host) == "" {
		return fmt.Errorf("host is required")
	}
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if strings.TrimSpace(d.User) == "" {
		return fmt.Errorf("user is required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if d.MaxOpen <= 0 {
		return fmt.Errorf("max open must be positive")
	}
	if d.MaxIdle < 0 {
		return fmt.Errorf("max idle cannot be negative")
	}
	if d.MaxIdle > d.MaxOpen {
		return fmt.Errorf("max idle cannot exceed max open")
	}
	if d.ConnMaxLifetime <= 0 {
		return fmt.Errorf("connection max lifetime must be positive")
	}
	if d.ConnMaxIdleTime <= 0 {
		return fmt.Errorf("connection max idle time must be positive")
	}
	return nil
}

func (j JWT) validate(environment string) error {
	if strings.TrimSpace(j.Key) == "" {
		return fmt.Errorf("jwt key is required")
	}
	if len([]byte(j.Key)) < minimumJWTKeyBytes {
		return fmt.Errorf("jwt key must be at least %d bytes", minimumJWTKeyBytes)
	}
	// Placeholder keys are allowed for local experimentation but must never reach production.
	if environment == "production" && isPlaceholderSecret(j.Key) {
		return fmt.Errorf("jwt key must not use a placeholder or example secret in production")
	}
	if strings.TrimSpace(j.Issuer) == "" {
		return fmt.Errorf("issuer is required")
	}
	if strings.TrimSpace(j.Audience) == "" {
		return fmt.Errorf("audience is required")
	}
	if j.AccessTTL <= 0 {
		return fmt.Errorf("access TTL must be positive")
	}
	if j.Leeway < 0 || j.Leeway > maximumJWTLeeway || j.Leeway > j.AccessTTL {
		return fmt.Errorf("leeway must be non-negative and no greater than %s or the access TTL", maximumJWTLeeway)
	}
	if j.RefreshTTL <= 0 {
		return fmt.Errorf("refresh TTL must be positive")
	}
	if j.RefreshTTL <= j.AccessTTL {
		return fmt.Errorf("refresh TTL must exceed access TTL")
	}
	return nil
}

func isPlaceholderSecret(secret string) bool {
	// Delimiter-aware tokens catch documented sample credentials after common
	// normalization while avoiding substring false positives in opaque real keys.
	tokens := strings.FieldsFunc(strings.ToLower(secret), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for index, token := range tokens {
		switch token {
		case "default", "example", "placeholder", "changeme":
			return true
		case "change":
			if index+1 < len(tokens) && tokens[index+1] == "me" {
				return true
			}
		case "your":
			if index+1 < len(tokens) && (tokens[index+1] == "key" || tokens[index+1] == "secret") {
				return true
			}
		}
	}
	return false
}

func (p Password) validate() error {
	// Both lower and upper bounds matter: weak parameters reduce password resistance,
	// while unbounded values can prevent the service from starting on its available memory.
	if p.Memory < minimumArgonMemory || p.Memory > maximumArgonMemory {
		return fmt.Errorf("memory must be between %d and %d KiB", minimumArgonMemory, maximumArgonMemory)
	}
	if p.Iterations < 1 || p.Iterations > 10 {
		return fmt.Errorf("iterations must be between 1 and 10")
	}
	if p.Parallelism < 1 || p.Parallelism > 32 {
		return fmt.Errorf("parallelism must be between 1 and 32")
	}
	if p.SaltLength < 16 || p.SaltLength > 64 {
		return fmt.Errorf("salt length must be between 16 and 64 bytes")
	}
	if p.KeyLength < 16 || p.KeyLength > 64 {
		return fmt.Errorf("key length must be between 16 and 64 bytes")
	}
	return nil
}

func (c CORS) validate() error {
	seen := make(map[string]struct{}, len(c.AllowedOrigins))
	for _, origin := range c.AllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || !parsed.IsAbs() || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return fmt.Errorf("allowed origin %q must be an absolute http/https URL without credentials", origin)
		}
		if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("allowed origin %q must not contain a path, query, or fragment", origin)
		}
		// url.Parse accepts an empty hostname and does not enforce the TCP port range.
		if parsed.Hostname() == "" {
			return fmt.Errorf("allowed origin %q must contain a hostname", origin)
		}
		if port := parsed.Port(); port != "" {
			portNumber, err := strconv.Atoi(port)
			if err != nil || portNumber < 1 || portNumber > 65535 {
				return fmt.Errorf("allowed origin %q port must be between 1 and 65535", origin)
			}
		}
		if _, exists := seen[origin]; exists {
			return fmt.Errorf("allowed origin %q is duplicated", origin)
		}
		seen[origin] = struct{}{}
	}
	return nil
}

func (r RateLimit) validate() error {
	if r.LoginRequestsPerSecond <= 0 || math.IsNaN(r.LoginRequestsPerSecond) || math.IsInf(r.LoginRequestsPerSecond, 0) {
		return fmt.Errorf("login requests per second must be positive and finite")
	}
	if r.LoginBurst <= 0 {
		return fmt.Errorf("login burst must be positive")
	}
	return nil
}
