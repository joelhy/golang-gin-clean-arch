package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestLoadWith(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "valid configuration", env: validEnv()},
		{name: "missing JWT key", env: without(validEnv(), "APP_JWT_KEY"), wantErr: "jwt key is required"},
		{name: "short JWT key", env: with(validEnv(), "APP_JWT_KEY", "too-short"), wantErr: "at least 32 bytes"},
		{name: "invalid duration", env: with(validEnv(), "APP_HTTP_READ_TIMEOUT", "eventually"), wantErr: "http.read_timeout"},
		{name: "invalid CORS URL", env: with(validEnv(), "APP_CORS_ALLOWED_ORIGINS", "ftp://api.example.com"), wantErr: "absolute http/https URL"},
		{name: "invalid database pool bounds", env: with(validEnv(), "APP_DB_MAX_IDLE", "11", "APP_DB_MAX_OPEN", "10"), wantErr: "max idle"},
		{
			name: "production example secret",
			env: with(
				validEnv(),
				"APP_ENVIRONMENT", "production",
				"APP_JWT_KEY", "change-me-in-production-1234567890",
			),
			wantErr: "example secret",
		},
		{
			name: "production example-labelled secret",
			env: with(
				validEnv(),
				"APP_ENVIRONMENT", "production",
				"APP_JWT_KEY", "example-jwt-key-0123456789abcdef0123456789",
			),
			wantErr: "example secret",
		},
		{
			name:    "non-finite rate limit",
			env:     with(validEnv(), "APP_RATE_LIMIT_LOGIN_REQUESTS_PER_SECOND", "NaN"),
			wantErr: "login requests per second must be positive and finite",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setConfigEnv(t, tt.env)

			cfg, err := LoadWith(NewViper())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadWith() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadWith() error = %v", err)
			}
			if cfg.HTTP.ReadTimeout != 11*time.Second {
				t.Errorf("HTTP.ReadTimeout = %v, want 11s", cfg.HTTP.ReadTimeout)
			}
			if len(cfg.CORS.AllowedOrigins) != 2 {
				t.Errorf("CORS.AllowedOrigins = %v, want two origins", cfg.CORS.AllowedOrigins)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	setConfigEnv(t, validEnv())

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadWithPreservesBoundFlag(t *testing.T) {
	setConfigEnv(t, validEnv())
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("http-address", "", "HTTP listen address")
	if err := flags.Parse([]string{"--http-address=127.0.0.1:9090"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	v := NewViper()
	if err := v.BindPFlag("http.address", flags.Lookup("http-address")); err != nil {
		t.Fatalf("BindPFlag() error = %v", err)
	}
	cfg, err := LoadWith(v)
	if err != nil {
		t.Fatalf("LoadWith() error = %v", err)
	}
	if cfg.HTTP.Address != "127.0.0.1:9090" {
		t.Fatalf("HTTP.Address = %q, want flag value", cfg.HTTP.Address)
	}
}

func TestLoadWithReadsConfiguredFile(t *testing.T) {
	setConfigEnv(t, map[string]string{})
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
environment: test
http:
  address: 127.0.0.1:9091
jwt:
  key: 0123456789abcdef0123456789abcdef
  issuer: file-issuer
  audience: file-audience
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	v := NewViper()
	v.SetConfigFile(path)
	cfg, err := LoadWith(v)
	if err != nil {
		t.Fatalf("LoadWith() error = %v", err)
	}
	if cfg.HTTP.Address != "127.0.0.1:9091" {
		t.Fatalf("HTTP.Address = %q, want config file value", cfg.HTTP.Address)
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

func setConfigEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range allConfigEnvKeys() {
		t.Setenv(key, "")
	}
	for key, value := range env {
		t.Setenv(key, value)
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
