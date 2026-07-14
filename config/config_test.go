package config

import (
	"os"
	"path/filepath"
	"slices"
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

func TestLoadWithRejectsProductionPlaceholderJWTKeys(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{name: "generic default marker", key: "default-jwt-key-that-is-long-enough-123", wantErr: true},
		{name: "generic example marker", key: "example-jwt-key-that-is-long-enough-123", wantErr: true},
		{name: "generic placeholder marker", key: "placeholder-jwt-key-that-is-long-enough-123", wantErr: true},
		{name: "embedded example substring", key: "securepreexamplesuffix-key-material-0123456789abcdef"},
		{name: "embedded default and placeholder substrings", key: "nodefaultplaceholderish-key-material-0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setConfigEnv(t, with(
				validEnv(),
				"APP_ENVIRONMENT", "production",
				"APP_JWT_KEY", tt.key,
			))

			_, err := LoadWith(NewViper())
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "example secret") {
					t.Fatalf("LoadWith() error = %v, want example secret rejection", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadWith() error = %v, want valid non-placeholder key", err)
			}
		})
	}
}

func TestNewViperRegistersConfigKey(t *testing.T) {
	if keys := NewViper().AllKeys(); !slices.Contains(keys, "config") {
		t.Fatalf("NewViper().AllKeys() = %v, want config", keys)
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

func TestLoadWithRejectsInvalidArgon2Numbers(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		wantErr string
	}{
		{name: "negative memory", field: "memory", value: "-4294901760", wantErr: "memory"},
		{name: "overflow memory", field: "memory", value: "4295032832", wantErr: "memory"},
		{name: "negative iterations", field: "iterations", value: "-4294967293", wantErr: "iterations"},
		{name: "overflow iterations", field: "iterations", value: "4294967299", wantErr: "iterations"},
		{name: "negative parallelism", field: "parallelism", value: "-254", wantErr: "parallelism"},
		{name: "overflow parallelism", field: "parallelism", value: "258", wantErr: "parallelism"},
		{name: "negative salt length", field: "salt_length", value: "-4294967280", wantErr: "salt length"},
		{name: "overflow salt length", field: "salt_length", value: "4294967312", wantErr: "salt length"},
		{name: "negative key length", field: "key_length", value: "-4294967264", wantErr: "key length"},
		{name: "overflow key length", field: "key_length", value: "4294967328", wantErr: "key length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setConfigEnv(t, map[string]string{})
			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := []byte("jwt:\n  key: 0123456789abcdef0123456789abcdef\npassword:\n  " + tt.field + ": " + tt.value + "\n")
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			v := NewViper()
			v.SetConfigFile(path)
			_, err := LoadWith(v)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadWith() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadWithEmptyEnvironmentOverridesConfigFile(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{name: "empty HTTP address", key: "APP_HTTP_ADDRESS", wantErr: "address is required"},
		{name: "empty JWT key", key: "APP_JWT_KEY", wantErr: "jwt key is required"},
		{name: "empty JWT issuer", key: "APP_JWT_ISSUER", wantErr: "issuer is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setConfigEnv(t, with(validEnv(), tt.key, ""))
			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := []byte(`
http:
  address: 127.0.0.1:8090
jwt:
  key: abcdef0123456789abcdef0123456789
  issuer: file-issuer
`)
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			v := NewViper()
			v.SetConfigFile(path)
			_, err := LoadWith(v)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadWith() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadWithRejectsInvalidCORSOrigins(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		wantErr string
	}{
		{name: "missing hostname", origin: "https://:443", wantErr: "hostname"},
		{name: "zero port", origin: "https://example.com:0", wantErr: "port"},
		{name: "out of range port", origin: "https://example.com:65536", wantErr: "port"},
		{name: "non-numeric port", origin: "https://example.com:http", wantErr: "absolute http/https URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setConfigEnv(t, with(validEnv(), "APP_CORS_ALLOWED_ORIGINS", tt.origin))

			_, err := LoadWith(NewViper())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("LoadWith() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadWithUsesFlagEnvironmentFilePriority(t *testing.T) {
	setConfigEnv(t, with(validEnv(), "APP_HTTP_ADDRESS", "127.0.0.1:8082"))
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte(`
http:
  address: 127.0.0.1:8081
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("http-address", "127.0.0.1:8080", "HTTP listen address")
	if err := flags.Parse([]string{"--http-address=127.0.0.1:8083"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !flags.Changed("http-address") {
		t.Fatal("http-address flag was not marked changed")
	}

	v := NewViper()
	v.SetConfigFile(path)
	if err := v.BindPFlag("http.address", flags.Lookup("http-address")); err != nil {
		t.Fatalf("BindPFlag() error = %v", err)
	}
	cfg, err := LoadWith(v)
	if err != nil {
		t.Fatalf("LoadWith() error = %v", err)
	}
	if cfg.HTTP.Address != "127.0.0.1:8083" {
		t.Fatalf("HTTP.Address = %q, want changed flag value", cfg.HTTP.Address)
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
		"APP_CONFIG",
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
