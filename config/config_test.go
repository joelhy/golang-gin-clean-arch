package config

import (
	"math"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadFromUsesSafeDefaults(t *testing.T) {
	env := map[string]string{
		"APP_JWT_KEY": strings.Repeat("k", minimumJWTKeyBytes),
	}

	cfg, err := loadFrom(lookupMap(env))
	if err != nil {
		t.Fatalf("loadFrom() error = %v", err)
	}

	want := Config{
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
			Host:            "127.0.0.1",
			Port:            3306,
			User:            "app",
			Name:            "clean_arch",
			MaxIdle:         10,
			MaxOpen:         25,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 5 * time.Minute,
		},
		JWT: JWT{
			Key:        env["APP_JWT_KEY"],
			Issuer:     "clean-arch-gin",
			Audience:   "clean-arch-api",
			AccessTTL:  15 * time.Minute,
			RefreshTTL: 7 * 24 * time.Hour,
		},
		Password: Password{
			Memory:      64 * 1024,
			Iterations:  3,
			Parallelism: 2,
			SaltLength:  16,
			KeyLength:   32,
		},
		CORS: CORS{AllowedOrigins: []string{}},
		RateLimit: RateLimit{
			LoginRequestsPerSecond: 1,
			LoginBurst:             5,
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("loadFrom() = %#v, want %#v", cfg, want)
	}
}

func TestLoadFromOverridesEverySupportedEnvironmentKey(t *testing.T) {
	env := map[string]string{
		"APP_ENVIRONMENT":                          " TEST ",
		"APP_HTTP_ADDRESS":                         " 127.0.0.1:9090 ",
		"APP_HTTP_READ_HEADER_TIMEOUT":             "6s",
		"APP_HTTP_READ_TIMEOUT":                    "16s",
		"APP_HTTP_WRITE_TIMEOUT":                   "31s",
		"APP_HTTP_IDLE_TIMEOUT":                    "3m",
		"APP_HTTP_SHUTDOWN_TIMEOUT":                "11s",
		"APP_HTTP_MAX_BODY_BYTES":                  "2097152",
		"APP_DB_HOST":                              " db.internal ",
		"APP_DB_PORT":                              "3307",
		"APP_DB_USER":                              " service ",
		"APP_DB_PASSWORD":                          " db-secret ",
		"APP_DB_NAME":                              "app_db",
		"APP_DB_MAX_IDLE":                          "12",
		"APP_DB_MAX_OPEN":                          "30",
		"APP_DB_CONN_MAX_LIFETIME":                 "45m",
		"APP_DB_CONN_MAX_IDLE_TIME":                "7m",
		"APP_JWT_KEY":                              " jwt-key-material-0123456789abcdef ",
		"APP_JWT_ISSUER":                           "issuer-override",
		"APP_JWT_AUDIENCE":                         "audience-override",
		"APP_JWT_ACCESS_TTL":                       "20m",
		"APP_JWT_REFRESH_TTL":                      "240h",
		"APP_JWT_LEEWAY":                           "45s",
		"APP_PASSWORD_MEMORY":                      "131072",
		"APP_PASSWORD_ITERATIONS":                  "4",
		"APP_PASSWORD_PARALLELISM":                 "3",
		"APP_PASSWORD_SALT_LENGTH":                 "24",
		"APP_PASSWORD_KEY_LENGTH":                  "48",
		"APP_CORS_ALLOWED_ORIGINS":                 " https://app.example.com , http://localhost:3000 ",
		"APP_CORS_ALLOW_CREDENTIALS":               "true",
		"APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND": "2.5",
		"APP_RATE_LIMIT_LOGIN_BURST":               "8",
	}

	cfg, err := loadFrom(lookupMap(env))
	if err != nil {
		t.Fatalf("loadFrom() error = %v", err)
	}

	want := Config{
		Environment: "test",
		HTTP: HTTP{
			Address:           "127.0.0.1:9090",
			ReadHeaderTimeout: 6 * time.Second,
			ReadTimeout:       16 * time.Second,
			WriteTimeout:      31 * time.Second,
			IdleTimeout:       3 * time.Minute,
			ShutdownTimeout:   11 * time.Second,
			MaxBodyBytes:      2097152,
		},
		DB: Database{
			Host:            "db.internal",
			Port:            3307,
			User:            " service ",
			Password:        " db-secret ",
			Name:            "app_db",
			MaxIdle:         12,
			MaxOpen:         30,
			ConnMaxLifetime: 45 * time.Minute,
			ConnMaxIdleTime: 7 * time.Minute,
		},
		JWT: JWT{
			Key:        " jwt-key-material-0123456789abcdef ",
			Issuer:     "issuer-override",
			Audience:   "audience-override",
			AccessTTL:  20 * time.Minute,
			RefreshTTL: 240 * time.Hour,
			Leeway:     45 * time.Second,
		},
		Password: Password{
			Memory:      131072,
			Iterations:  4,
			Parallelism: 3,
			SaltLength:  24,
			KeyLength:   48,
		},
		CORS: CORS{
			AllowedOrigins:   []string{"https://app.example.com", "http://localhost:3000"},
			AllowCredentials: true,
		},
		RateLimit: RateLimit{
			LoginRequestsPerSecond: 2.5,
			LoginBurst:             8,
		},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("loadFrom() = %#v, want %#v", cfg, want)
	}
}

func TestLoadFromTreatsPresentEmptyStringsAsOverrides(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "environment", env: with(validEnv(), "APP_ENVIRONMENT", ""), wantErr: "environment"},
		{name: "HTTP address", env: with(validEnv(), "APP_HTTP_ADDRESS", ""), wantErr: "address is required"},
		{name: "database host", env: with(validEnv(), "APP_DB_HOST", ""), wantErr: "host is required"},
		{name: "database user", env: with(validEnv(), "APP_DB_USER", ""), wantErr: "user is required"},
		{name: "database name", env: with(validEnv(), "APP_DB_NAME", ""), wantErr: "name is required"},
		{name: "JWT key", env: with(validEnv(), "APP_JWT_KEY", ""), wantErr: "jwt key is required"},
		{name: "JWT issuer", env: with(validEnv(), "APP_JWT_ISSUER", ""), wantErr: "issuer is required"},
		{name: "JWT audience", env: with(validEnv(), "APP_JWT_AUDIENCE", ""), wantErr: "audience is required"},
		{
			name: "production database password",
			env: with(validEnv(),
				"APP_ENVIRONMENT", "production",
				"APP_DB_PASSWORD", "",
			),
			wantErr: "password is required in production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFrom(lookupMap(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadFrom() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadFromRejectsPresentEmptyTypedValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "integer", key: "APP_DB_MAX_OPEN"},
		{name: "int64", key: "APP_HTTP_MAX_BODY_BYTES"},
		{name: "boolean", key: "APP_CORS_ALLOW_CREDENTIALS"},
		{name: "float", key: "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND"},
		{name: "duration", key: "APP_HTTP_READ_TIMEOUT"},
		{name: "list", key: "APP_CORS_ALLOWED_ORIGINS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFrom(lookupMap(with(validEnv(), tt.key, "")))
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("loadFrom() error = %v, want key %q", err, tt.key)
			}
		})
	}
}

func TestLoadFromRejectsInvalidTypedValuesWithKeyContext(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "signed integer", key: "APP_DB_MAX_OPEN", value: "many"},
		{name: "port", key: "APP_DB_PORT", value: "3306.0"},
		{name: "int64", key: "APP_HTTP_MAX_BODY_BYTES", value: "9223372036854775808"},
		{name: "password int64", key: "APP_PASSWORD_MEMORY", value: "65536.0"},
		{name: "boolean", key: "APP_CORS_ALLOW_CREDENTIALS", value: "yes"},
		{name: "float", key: "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND", value: "fast"},
		{name: "NaN", key: "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND", value: "NaN"},
		{name: "positive infinity", key: "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND", value: "+Inf"},
		{name: "negative infinity", key: "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND", value: "-Inf"},
		{name: "duration", key: "APP_HTTP_READ_TIMEOUT", value: "eventually"},
		{name: "leading empty list element", key: "APP_CORS_ALLOWED_ORIGINS", value: ",https://app.example.com"},
		{name: "middle empty list element", key: "APP_CORS_ALLOWED_ORIGINS", value: "https://app.example.com,,http://localhost:3000"},
		{name: "trailing empty list element", key: "APP_CORS_ALLOWED_ORIGINS", value: "https://app.example.com,"},
		{name: "whitespace list element", key: "APP_CORS_ALLOWED_ORIGINS", value: "https://app.example.com,   ,http://localhost:3000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFrom(lookupMap(with(validEnv(), tt.key, tt.value)))
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("loadFrom() error = %v, want key %q", err, tt.key)
			}
		})
	}
}

func TestLoadFromRunsValidation(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "valid configuration", env: validEnv()},
		{name: "missing JWT key", env: without(validEnv(), "APP_JWT_KEY"), wantErr: "jwt key is required"},
		{name: "short JWT key", env: with(validEnv(), "APP_JWT_KEY", "too-short"), wantErr: "at least 32 bytes"},
		{name: "invalid CORS URL", env: with(validEnv(), "APP_CORS_ALLOWED_ORIGINS", "ftp://api.example.com"), wantErr: "absolute http/https URL"},
		{name: "invalid database pool bounds", env: with(validEnv(), "APP_DB_MAX_IDLE", "11", "APP_DB_MAX_OPEN", "10"), wantErr: "max idle"},
		{
			name: "production example secret",
			env: with(validEnv(),
				"APP_ENVIRONMENT", "production",
				"APP_JWT_KEY", "change-me-in-production-1234567890",
			),
			wantErr: "example secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFrom(lookupMap(tt.env))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("loadFrom() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("loadFrom() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadReadsProcessEnvironment(t *testing.T) {
	setConfigEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.ReadTimeout != 11*time.Second {
		t.Fatalf("HTTP.ReadTimeout = %v, want 11s", cfg.HTTP.ReadTimeout)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "environment", mutate: func(c *Config) { c.Environment = "staging" }, wantErr: "environment"},
		{name: "HTTP address required", mutate: func(c *Config) { c.HTTP.Address = " " }, wantErr: "address is required"},
		{name: "HTTP address missing port", mutate: func(c *Config) { c.HTTP.Address = "localhost" }, wantErr: "valid host and port"},
		{name: "HTTP address port", mutate: func(c *Config) { c.HTTP.Address = ":0" }, wantErr: "invalid port"},
		{name: "HTTP read header timeout", mutate: func(c *Config) { c.HTTP.ReadHeaderTimeout = 0 }, wantErr: "read header timeout"},
		{name: "HTTP read timeout", mutate: func(c *Config) { c.HTTP.ReadTimeout = 0 }, wantErr: "read timeout"},
		{name: "HTTP write timeout", mutate: func(c *Config) { c.HTTP.WriteTimeout = 0 }, wantErr: "write timeout"},
		{name: "HTTP idle timeout", mutate: func(c *Config) { c.HTTP.IdleTimeout = 0 }, wantErr: "idle timeout"},
		{name: "HTTP shutdown timeout", mutate: func(c *Config) { c.HTTP.ShutdownTimeout = 0 }, wantErr: "shutdown timeout"},
		{name: "HTTP body limit", mutate: func(c *Config) { c.HTTP.MaxBodyBytes = 0 }, wantErr: "max body bytes"},
		{name: "database host", mutate: func(c *Config) { c.DB.Host = " " }, wantErr: "host is required"},
		{name: "database port", mutate: func(c *Config) { c.DB.Port = 65536 }, wantErr: "port must be between"},
		{name: "database user", mutate: func(c *Config) { c.DB.User = " " }, wantErr: "user is required"},
		{name: "database name", mutate: func(c *Config) { c.DB.Name = " " }, wantErr: "name is required"},
		{name: "database max open", mutate: func(c *Config) { c.DB.MaxOpen = 0 }, wantErr: "max open"},
		{name: "database negative max idle", mutate: func(c *Config) { c.DB.MaxIdle = -1 }, wantErr: "max idle cannot be negative"},
		{name: "database max idle exceeds max open", mutate: func(c *Config) { c.DB.MaxIdle = c.DB.MaxOpen + 1 }, wantErr: "max idle cannot exceed"},
		{name: "database connection max lifetime", mutate: func(c *Config) { c.DB.ConnMaxLifetime = 0 }, wantErr: "connection max lifetime"},
		{name: "database connection max idle time", mutate: func(c *Config) { c.DB.ConnMaxIdleTime = 0 }, wantErr: "connection max idle time"},
		{
			name: "production database password",
			mutate: func(c *Config) {
				c.Environment = "production"
				c.DB.Password = " "
			},
			wantErr: "password is required in production",
		},
		{name: "JWT key required", mutate: func(c *Config) { c.JWT.Key = "" }, wantErr: "jwt key is required"},
		{name: "JWT key length", mutate: func(c *Config) { c.JWT.Key = "short" }, wantErr: "at least 32 bytes"},
		{
			name: "JWT placeholder",
			mutate: func(c *Config) {
				c.Environment = "production"
				c.JWT.Key = "placeholder-jwt-key-that-is-long-enough-123"
			},
			wantErr: "example secret",
		},
		{name: "JWT issuer", mutate: func(c *Config) { c.JWT.Issuer = " " }, wantErr: "issuer is required"},
		{name: "JWT audience", mutate: func(c *Config) { c.JWT.Audience = " " }, wantErr: "audience is required"},
		{name: "JWT access TTL", mutate: func(c *Config) { c.JWT.AccessTTL = 0 }, wantErr: "access TTL"},
		{name: "JWT refresh TTL", mutate: func(c *Config) { c.JWT.RefreshTTL = 0 }, wantErr: "refresh TTL must be positive"},
		{name: "JWT refresh not greater", mutate: func(c *Config) { c.JWT.RefreshTTL = c.JWT.AccessTTL }, wantErr: "refresh TTL must exceed"},
		{name: "JWT negative leeway", mutate: func(c *Config) { c.JWT.Leeway = -time.Nanosecond }, wantErr: "leeway"},
		{name: "JWT excessive leeway", mutate: func(c *Config) { c.JWT.Leeway = 5*time.Minute + time.Nanosecond }, wantErr: "leeway"},
		{name: "JWT leeway exceeds access TTL", mutate: func(c *Config) { c.JWT.AccessTTL = time.Second; c.JWT.Leeway = time.Second + time.Nanosecond }, wantErr: "leeway"},
		{name: "password memory low", mutate: func(c *Config) { c.Password.Memory = minimumArgonMemory - 1 }, wantErr: "memory"},
		{name: "password memory high", mutate: func(c *Config) { c.Password.Memory = maximumArgonMemory + 1 }, wantErr: "memory"},
		{name: "password iterations low", mutate: func(c *Config) { c.Password.Iterations = 0 }, wantErr: "iterations"},
		{name: "password iterations high", mutate: func(c *Config) { c.Password.Iterations = 11 }, wantErr: "iterations"},
		{name: "password parallelism low", mutate: func(c *Config) { c.Password.Parallelism = 0 }, wantErr: "parallelism"},
		{name: "password parallelism high", mutate: func(c *Config) { c.Password.Parallelism = 33 }, wantErr: "parallelism"},
		{name: "password salt length low", mutate: func(c *Config) { c.Password.SaltLength = 15 }, wantErr: "salt length"},
		{name: "password salt length high", mutate: func(c *Config) { c.Password.SaltLength = 65 }, wantErr: "salt length"},
		{name: "password key length low", mutate: func(c *Config) { c.Password.KeyLength = 15 }, wantErr: "key length"},
		{name: "password key length high", mutate: func(c *Config) { c.Password.KeyLength = 65 }, wantErr: "key length"},
		{name: "CORS relative origin", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"app.example.com"} }, wantErr: "absolute http/https URL"},
		{name: "CORS scheme", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"ftp://app.example.com"} }, wantErr: "absolute http/https URL"},
		{name: "CORS credentials", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://user@app.example.com"} }, wantErr: "without credentials"},
		{name: "CORS path", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://app.example.com/path"} }, wantErr: "must not contain a path"},
		{name: "CORS query", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://app.example.com?x=1"} }, wantErr: "must not contain a path"},
		{name: "CORS fragment", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://app.example.com#x"} }, wantErr: "must not contain a path"},
		{name: "CORS hostname", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://:443"} }, wantErr: "hostname"},
		{name: "CORS zero port", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://app.example.com:0"} }, wantErr: "port"},
		{name: "CORS high port", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://app.example.com:65536"} }, wantErr: "port"},
		{name: "CORS malformed port", mutate: func(c *Config) { c.CORS.AllowedOrigins = []string{"https://app.example.com:http"} }, wantErr: "absolute http/https URL"},
		{name: "CORS duplicate", mutate: func(c *Config) {
			c.CORS.AllowedOrigins = []string{"https://app.example.com", "https://app.example.com"}
		}, wantErr: "duplicated"},
		{name: "rate limit zero", mutate: func(c *Config) { c.RateLimit.LoginRequestsPerSecond = 0 }, wantErr: "positive and finite"},
		{name: "rate limit NaN", mutate: func(c *Config) { c.RateLimit.LoginRequestsPerSecond = math.NaN() }, wantErr: "positive and finite"},
		{name: "rate limit infinity", mutate: func(c *Config) { c.RateLimit.LoginRequestsPerSecond = math.Inf(1) }, wantErr: "positive and finite"},
		{name: "rate limit burst", mutate: func(c *Config) { c.RateLimit.LoginBurst = 0 }, wantErr: "login burst"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfigForTest()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestPasswordValidationAcceptsInclusiveBounds(t *testing.T) {
	tests := []Password{
		{Memory: minimumArgonMemory, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16},
		{Memory: maximumArgonMemory, Iterations: 10, Parallelism: 32, SaltLength: 64, KeyLength: 64},
	}
	for _, password := range tests {
		if err := password.validate(); err != nil {
			t.Errorf("Password.validate() error = %v for %#v", err, password)
		}
	}
}

func TestCORSValidationAcceptsTCPPortBounds(t *testing.T) {
	cors := CORS{AllowedOrigins: []string{"https://app.example.com:1", "https://api.example.com:65535"}}
	if err := cors.validate(); err != nil {
		t.Fatalf("CORS.validate() error = %v", err)
	}
}

func TestPlaceholderSecretDetection(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		placeholder bool
	}{
		{name: "default marker", key: "default-jwt-key-that-is-long-enough-123", placeholder: true},
		{name: "example marker", key: "example-jwt-key-that-is-long-enough-123", placeholder: true},
		{name: "placeholder marker", key: "placeholder-jwt-key-that-is-long-enough-123", placeholder: true},
		{name: "change me marker", key: "change-me-in-production-1234567890", placeholder: true},
		{name: "change me compact marker", key: "changeme-jwt-key-that-is-long-enough-123", placeholder: true},
		{name: "your key marker", key: "your-key-must-be-replaced-0123456789", placeholder: true},
		{name: "your secret marker", key: "your-secret-must-be-replaced-012345", placeholder: true},
		{name: "embedded example substring", key: "securepreexamplesuffix-key-material-0123456789abcdef"},
		{name: "embedded default and placeholder substrings", key: "nodefaultplaceholderish-key-material-0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlaceholderSecret(tt.key); got != tt.placeholder {
				t.Fatalf("isPlaceholderSecret() = %t, want %t", got, tt.placeholder)
			}
		})
	}
}

func TestLoadFromErrorsDoNotExposeSecrets(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		env    func(string) map[string]string
	}{
		{
			name:   "short JWT key",
			secret: "private-short-key",
			env: func(secret string) map[string]string {
				return with(validEnv(), "APP_JWT_KEY", secret)
			},
		},
		{
			name:   "production placeholder JWT key",
			secret: "placeholder-private-key-material-0123456789",
			env: func(secret string) map[string]string {
				return with(validEnv(), "APP_ENVIRONMENT", "production", "APP_JWT_KEY", secret)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadFrom(lookupMap(tt.env(tt.secret)))
			if err == nil {
				t.Fatal("loadFrom() error = nil, want validation error")
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("loadFrom() error exposes secret: %v", err)
			}
		})
	}
}

func TestDatabaseDSN(t *testing.T) {
	database := Database{
		Host:     "db.internal",
		Port:     3307,
		User:     "service",
		Password: "s:ecret@value",
		Name:     "clean_arch",
	}

	for _, want := range []string{"service:s:ecret@value@tcp(db.internal:3307)/clean_arch", "parseTime=true"} {
		if got := database.DSN(); !strings.Contains(got, want) {
			t.Errorf("Database.DSN() = %q, want substring %q", got, want)
		}
	}
}

func TestCurrentSourceHasNoRemovedConfigurationOrCLIFramework(t *testing.T) {
	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("ReadFile(config.go) error = %v", err)
	}
	module, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("ReadFile(../go.mod) error = %v", err)
	}
	command := exec.Command("go", "list", "-deps", "./...")
	command.Dir = ".."
	dependencies, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./... error = %v\n%s", err, dependencies)
	}

	for _, forbidden := range []string{"github.com/spf13/viper", "github.com/spf13/cobra", "mapstructure"} {
		if strings.Contains(string(source), forbidden) {
			t.Errorf("config.go contains forbidden dependency %q", forbidden)
		}
		if strings.Contains(string(module), forbidden) {
			t.Errorf("go.mod contains forbidden dependency %q", forbidden)
		}
		if strings.Contains(string(dependencies), forbidden) {
			t.Errorf("config dependency graph contains forbidden dependency %q", forbidden)
		}
	}
}

func validConfigForTest() Config {
	cfg := defaultConfig()
	cfg.JWT.Key = strings.Repeat("k", minimumJWTKeyBytes)
	cfg.DB.Password = "database-secret"
	return cfg
}

func lookupMap(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func setConfigEnv(t *testing.T, env map[string]string) {
	t.Helper()
	original := make(map[string]struct {
		value string
		set   bool
	}, len(allConfigEnvKeys()))
	for _, key := range allConfigEnvKeys() {
		value, set := os.LookupEnv(key)
		original[key] = struct {
			value string
			set   bool
		}{value: value, set: set}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv(%q) error = %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range allConfigEnvKeys() {
			state := original[key]
			var err error
			if state.set {
				err = os.Setenv(key, state.value)
			} else {
				err = os.Unsetenv(key)
			}
			if err != nil {
				t.Errorf("restoring environment variable %q: %v", key, err)
			}
		}
	})
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Setenv(%q) error = %v", key, err)
		}
	}
}

func allConfigEnvKeys() []string {
	return []string{
		"APP_ENVIRONMENT",
		"APP_HTTP_ADDRESS",
		"APP_HTTP_READ_HEADER_TIMEOUT",
		"APP_HTTP_READ_TIMEOUT",
		"APP_HTTP_WRITE_TIMEOUT",
		"APP_HTTP_IDLE_TIMEOUT",
		"APP_HTTP_SHUTDOWN_TIMEOUT",
		"APP_HTTP_MAX_BODY_BYTES",
		"APP_DB_HOST",
		"APP_DB_PORT",
		"APP_DB_USER",
		"APP_DB_PASSWORD",
		"APP_DB_NAME",
		"APP_DB_MAX_IDLE",
		"APP_DB_MAX_OPEN",
		"APP_DB_CONN_MAX_LIFETIME",
		"APP_DB_CONN_MAX_IDLE_TIME",
		"APP_JWT_KEY",
		"APP_JWT_ISSUER",
		"APP_JWT_AUDIENCE",
		"APP_JWT_ACCESS_TTL",
		"APP_JWT_REFRESH_TTL",
		"APP_JWT_LEEWAY",
		"APP_PASSWORD_MEMORY",
		"APP_PASSWORD_ITERATIONS",
		"APP_PASSWORD_PARALLELISM",
		"APP_PASSWORD_SALT_LENGTH",
		"APP_PASSWORD_KEY_LENGTH",
		"APP_CORS_ALLOWED_ORIGINS",
		"APP_CORS_ALLOW_CREDENTIALS",
		"APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND",
		"APP_RATE_LIMIT_LOGIN_BURST",
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"APP_ENVIRONMENT":                          "development",
		"APP_HTTP_ADDRESS":                         "127.0.0.1:8080",
		"APP_HTTP_READ_HEADER_TIMEOUT":             "5s",
		"APP_HTTP_READ_TIMEOUT":                    "11s",
		"APP_HTTP_WRITE_TIMEOUT":                   "30s",
		"APP_HTTP_IDLE_TIMEOUT":                    "2m",
		"APP_HTTP_SHUTDOWN_TIMEOUT":                "10s",
		"APP_HTTP_MAX_BODY_BYTES":                  "1048576",
		"APP_DB_HOST":                              "127.0.0.1",
		"APP_DB_PORT":                              "3306",
		"APP_DB_USER":                              "app",
		"APP_DB_PASSWORD":                          "local-db-password",
		"APP_DB_NAME":                              "clean_arch",
		"APP_DB_MAX_IDLE":                          "10",
		"APP_DB_MAX_OPEN":                          "25",
		"APP_DB_CONN_MAX_LIFETIME":                 "30m",
		"APP_DB_CONN_MAX_IDLE_TIME":                "5m",
		"APP_JWT_KEY":                              "0123456789abcdef0123456789abcdef",
		"APP_JWT_ISSUER":                           "clean-arch-gin",
		"APP_JWT_AUDIENCE":                         "clean-arch-api",
		"APP_JWT_ACCESS_TTL":                       "15m",
		"APP_JWT_REFRESH_TTL":                      "168h",
		"APP_JWT_LEEWAY":                           "30s",
		"APP_PASSWORD_MEMORY":                      "65536",
		"APP_PASSWORD_ITERATIONS":                  "3",
		"APP_PASSWORD_PARALLELISM":                 "2",
		"APP_PASSWORD_SALT_LENGTH":                 "16",
		"APP_PASSWORD_KEY_LENGTH":                  "32",
		"APP_CORS_ALLOWED_ORIGINS":                 "https://app.example.com,http://localhost:3000",
		"APP_CORS_ALLOW_CREDENTIALS":               "true",
		"APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND": "1.5",
		"APP_RATE_LIMIT_LOGIN_BURST":               "5",
	}
}

func without(env map[string]string, key string) map[string]string {
	result := cloneEnv(env)
	delete(result, key)
	return result
}

func with(env map[string]string, values ...string) map[string]string {
	result := cloneEnv(env)
	for index := 0; index < len(values); index += 2 {
		result[values[index]] = values[index+1]
	}
	return result
}

func cloneEnv(env map[string]string) map[string]string {
	result := make(map[string]string, len(env))
	for key, value := range env {
		result[key] = value
	}
	return result
}
