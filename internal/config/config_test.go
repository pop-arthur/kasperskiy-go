package config

import (
	"log/slog"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func loadValues(values map[string]string) (Config, error) {
	return load(func(name string) (string, bool) { value, exists := values[name]; return value, exists })
}

func TestDefaults(t *testing.T) {
	cfg, err := loadValues(map[string]string{"POSTGRES_PASSWORD": "password"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelInfo || !reflect.DeepEqual(cfg.HTTP, HTTPConfig{
		Addr: "127.0.0.1:8080", ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, ShutdownTimeout: 10 * time.Second, MaxBodyBytes: 1048576,
	}) {
		t.Fatalf("unexpected HTTP defaults: %+v", cfg.HTTP)
	}
	if cfg.Postgres.Host != "127.0.0.1" || cfg.Postgres.Port != 5432 || cfg.Postgres.Database != "threats" || cfg.Postgres.User != "threats" || cfg.Postgres.SSLMode != "disable" || cfg.Postgres.MaxConns != 10 || cfg.Postgres.ConnectTimeout != 5*time.Second {
		t.Fatal("unexpected PostgreSQL defaults")
	}
}

func TestConfigurationOverridesAndEscapedURL(t *testing.T) {
	password := " p@ss:/?#% '& "
	cfg, err := loadValues(map[string]string{
		"LOG_LEVEL": "DEBUG+2", "HTTP_ADDR": "[::1]:0", "HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_READ_TIMEOUT": "3s", "HTTP_WRITE_TIMEOUT": "4s", "HTTP_IDLE_TIMEOUT": "5s", "HTTP_SHUTDOWN_TIMEOUT": "6s",
		"HTTP_MAX_BODY_BYTES": "1", "POSTGRES_HOST": "::1", "POSTGRES_PORT": "65535",
		"POSTGRES_DB": "db /?#%", "POSTGRES_USER": "u@:/?#", "POSTGRES_PASSWORD": password,
		"POSTGRES_SSLMODE": "verify-full", "POSTGRES_MAX_CONNS": "1", "POSTGRES_CONNECT_TIMEOUT": "7s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelDebug+2 || cfg.HTTP.MaxBodyBytes != 1 || cfg.Postgres.Port != 65535 || cfg.Postgres.MaxConns != 1 || cfg.HTTP.ShutdownTimeout != 6*time.Second || cfg.Postgres.ConnectTimeout != 7*time.Second {
		t.Fatal("overrides not applied")
	}
	parsed, err := url.Parse(cfg.Postgres.URL())
	if err != nil {
		t.Fatal("invalid connection URL")
	}
	gotPassword, _ := parsed.User.Password()
	if parsed.User.Username() != cfg.Postgres.User || gotPassword != password || parsed.Path != "/"+cfg.Postgres.Database || parsed.Host != "[::1]:65535" || parsed.Query().Get("sslmode") != "verify-full" {
		t.Fatal("connection URL changed credentials/address")
	}
}

func TestInvalidConfigurationDoesNotExposeValues(t *testing.T) {
	cases := map[string][]string{
		"LOG_LEVEL": {"", "secret-invalid"}, "HTTP_ADDR": {"", "missing-port", "localhost:-1", "localhost:65536", "bad host:80", "127.0.0.1:http"},
		"HTTP_READ_HEADER_TIMEOUT": {"", "0s", "-1s", "secret-invalid"}, "HTTP_READ_TIMEOUT": {"0"}, "HTTP_WRITE_TIMEOUT": {"-1s"}, "HTTP_IDLE_TIMEOUT": {"invalid"}, "HTTP_SHUTDOWN_TIMEOUT": {"0s"},
		"HTTP_MAX_BODY_BYTES": {"", "0", "-1", "9223372036854775808"},
		"POSTGRES_HOST":       {"", "bad host", "host:5432", "/tmp/postgres", "[::1]"}, "POSTGRES_PORT": {"", "0", "65536", "-1", "secret-invalid"},
		"POSTGRES_DB": {""}, "POSTGRES_USER": {""}, "POSTGRES_PASSWORD": {"", "secret\x00value"}, "POSTGRES_SSLMODE": {"", "secret-invalid"},
		"POSTGRES_MAX_CONNS": {"0", "-1", "2147483648", "secret-invalid"}, "POSTGRES_CONNECT_TIMEOUT": {"0s", "-1s", "secret-invalid"},
	}
	for name, values := range cases {
		for _, value := range values {
			t.Run(name+"/"+value, func(t *testing.T) {
				cfg, err := loadValues(map[string]string{"POSTGRES_PASSWORD": "do-not-log-this", name: value})
				if err == nil || !strings.Contains(err.Error(), name) || !reflect.DeepEqual(cfg, Config{}) {
					t.Fatalf("expected configuration error for %s", name)
				}
				if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "do-not-log-this") {
					t.Fatal("configuration value leaked")
				}
			})
		}
	}
	if _, err := loadValues(nil); err == nil || !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatal("missing password accepted")
	}
}

func TestValidBoundariesAndSSLModes(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"} {
		_, err := loadValues(map[string]string{"POSTGRES_PASSWORD": "x", "POSTGRES_SSLMODE": mode, "HTTP_ADDR": ":65535", "POSTGRES_PORT": "1", "POSTGRES_MAX_CONNS": "2147483647", "HTTP_MAX_BODY_BYTES": "9223372036854775807"})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadUsesEnvironment(t *testing.T) {
	// Explicit values isolate this test from the developer's shell configuration.
	values := map[string]string{
		"LOG_LEVEL": "INFO", "HTTP_ADDR": "127.0.0.1:0", "HTTP_READ_HEADER_TIMEOUT": "5s", "HTTP_READ_TIMEOUT": "10s", "HTTP_WRITE_TIMEOUT": "15s", "HTTP_IDLE_TIMEOUT": "60s", "HTTP_SHUTDOWN_TIMEOUT": "10s", "HTTP_MAX_BODY_BYTES": "1048576",
		"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": "5432", "POSTGRES_DB": "threats", "POSTGRES_USER": "threats", "POSTGRES_PASSWORD": "from-environment", "POSTGRES_SSLMODE": "disable", "POSTGRES_MAX_CONNS": "10", "POSTGRES_CONNECT_TIMEOUT": "5s",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil || cfg.Postgres.Password != "from-environment" || cfg.HTTP.Addr != "127.0.0.1:0" {
		t.Fatal("environment configuration not applied")
	}
}
