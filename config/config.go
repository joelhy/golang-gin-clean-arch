// Package config loads and validates the application's external configuration.
package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mysqlconfig "github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"
)

const (
	minimumJWTKeyBytes = 32
	minimumArgonMemory = 19 * 1024
	maximumArgonMemory = 1024 * 1024
)

type configDefault struct {
	key   string
	value any
}

// Every field has a registered key because Viper otherwise omits environment-only
// values while walking the configuration tree during Unmarshal.
var configDefaults = []configDefault{
	{key: "environment", value: "development"},
	{key: "http.address", value: ":8080"},
	{key: "http.read_header_timeout", value: 5 * time.Second},
	{key: "http.read_timeout", value: 15 * time.Second},
	{key: "http.write_timeout", value: 30 * time.Second},
	{key: "http.idle_timeout", value: 2 * time.Minute},
	{key: "http.shutdown_timeout", value: 10 * time.Second},
	{key: "http.max_body_bytes", value: int64(1 << 20)},
	{key: "db.host", value: "127.0.0.1"},
	{key: "db.port", value: 3306},
	{key: "db.user", value: "app"},
	// An empty database password is safe as a development default and is rejected in production.
	{key: "db.password", value: ""},
	{key: "db.name", value: "clean_arch"},
	{key: "db.max_idle", value: 10},
	{key: "db.max_open", value: 25},
	{key: "db.conn_max_lifetime", value: 30 * time.Minute},
	{key: "db.conn_max_idle_time", value: 5 * time.Minute},
	// JWT keys intentionally have no usable default so startup cannot silently use a shared secret.
	{key: "jwt.key", value: ""},
	{key: "jwt.issuer", value: "clean-arch-gin"},
	{key: "jwt.audience", value: "clean-arch-api"},
	{key: "jwt.access_ttl", value: 15 * time.Minute},
	{key: "jwt.refresh_ttl", value: 7 * 24 * time.Hour},
	{key: "password.memory", value: uint32(64 * 1024)},
	{key: "password.iterations", value: uint32(3)},
	{key: "password.parallelism", value: uint8(2)},
	{key: "password.salt_length", value: uint32(16)},
	{key: "password.key_length", value: uint32(32)},
	{key: "cors.allowed_origins", value: []string{}},
	{key: "cors.allow_credentials", value: false},
	{key: "rate_limit.login_requests_per_second", value: 1.0},
	{key: "rate_limit.login_burst", value: 5},
}

// Config is the validated application configuration passed to runtime components.
type Config struct {
	Environment string    `mapstructure:"environment"`
	HTTP        HTTP      `mapstructure:"http"`
	DB          Database  `mapstructure:"db"`
	JWT         JWT       `mapstructure:"jwt"`
	Password    Password  `mapstructure:"password"`
	CORS        CORS      `mapstructure:"cors"`
	RateLimit   RateLimit `mapstructure:"rate_limit"`
}

// HTTP contains server limits and lifecycle timeouts.
type HTTP struct {
	Address           string        `mapstructure:"address"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
	MaxBodyBytes      int64         `mapstructure:"max_body_bytes"`
}

// Database contains MySQL connection and database/sql pool settings.
type Database struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Name            string        `mapstructure:"name"`
	MaxIdle         int           `mapstructure:"max_idle"`
	MaxOpen         int           `mapstructure:"max_open"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
}

// JWT contains token signing identity and expiry settings.
type JWT struct {
	Key        string        `mapstructure:"key"`
	Issuer     string        `mapstructure:"issuer"`
	Audience   string        `mapstructure:"audience"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

// Password contains bounded Argon2id work and output parameters.
type Password struct {
	Memory      uint32 `mapstructure:"memory"`
	Iterations  uint32 `mapstructure:"iterations"`
	Parallelism uint8  `mapstructure:"parallelism"`
	SaltLength  uint32 `mapstructure:"salt_length"`
	KeyLength   uint32 `mapstructure:"key_length"`
}

// CORS contains the browser origins explicitly allowed to call the API.
type CORS struct {
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
}

// RateLimit contains login throttling parameters consumed by the HTTP middleware.
type RateLimit struct {
	LoginRequestsPerSecond float64 `mapstructure:"login_requests_per_second"`
	LoginBurst             int     `mapstructure:"login_burst"`
}

// NewViper returns an isolated Viper instance suitable for one command tree or load.
func NewViper() *viper.Viper {
	v := viper.New()
	configureViper(v)
	return v
}

// Load creates an isolated Viper instance, loads its sources, and validates the result.
func Load() (Config, error) {
	return LoadWith(NewViper())
}

// LoadWith loads configuration without replacing any values or flag bindings already on v.
func LoadWith(v *viper.Viper) (Config, error) {
	if v == nil {
		return Config{}, fmt.Errorf("load configuration: viper instance is nil")
	}
	configureViper(v)
	if err := bindEnvironment(v); err != nil {
		return Config{}, fmt.Errorf("load configuration environment: %w", err)
	}

	if path := strings.TrimSpace(v.GetString("config")); path != "" {
		v.SetConfigFile(path)
	}
	if path := v.ConfigFileUsed(); path != "" {
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read configuration file %q: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration: %w", err)
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

func configureViper(v *viper.Viper) {
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.AutomaticEnv()
	for _, item := range configDefaults {
		v.SetDefault(item.key, item.value)
	}
}

func bindEnvironment(v *viper.Viper) error {
	for _, item := range configDefaults {
		envName := "APP_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(item.key))
		// Explicit binding is required even with AutomaticEnv because Unmarshal only walks known keys.
		if err := v.BindEnv(item.key, envName); err != nil {
			return fmt.Errorf("bind %s to %s: %w", item.key, envName, err)
		}
	}
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
	// Example-like keys are allowed for local experimentation but must never reach production.
	if environment == "production" && isExampleSecret(j.Key) {
		return fmt.Errorf("jwt key must not use an example secret in production")
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
	if j.RefreshTTL <= 0 {
		return fmt.Errorf("refresh TTL must be positive")
	}
	if j.RefreshTTL <= j.AccessTTL {
		return fmt.Errorf("refresh TTL must exceed access TTL")
	}
	return nil
}

func isExampleSecret(secret string) bool {
	normalized := strings.ToLower(secret)
	for _, marker := range []string{"change-me", "changeme", "default-secret", "example", "your-secret"} {
		if strings.Contains(normalized, marker) {
			return true
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
