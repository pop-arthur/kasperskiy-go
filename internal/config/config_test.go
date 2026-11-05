package config

import (
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func loadValues(t *testing.T, values map[string]string) (Config, error) {
	if values != nil {
		copy := make(map[string]string, len(values)+1)
		for k, v := range values {
			copy[k] = v
		}
		if _, ok := copy["API_KEY"]; !ok {
			copy["API_KEY"] = strings.Repeat("a", 32)
		}
		values = copy
	}
	t.Helper()
	for _, field := range environmentFields(reflect.TypeFor[Config]()) {
		t.Setenv(field.name, "")
		if err := os.Unsetenv(field.name); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	return Load()
}

func TestDefaults(t *testing.T) {
	cfg, err := loadValues(t, map[string]string{"POSTGRES_PASSWORD": "password"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelInfo || !reflect.DeepEqual(cfg.HTTP, HTTPConfig{
		Addr: "127.0.0.1:8080", ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, ShutdownTimeout: 10 * time.Second, MaxBodyBytes: 1048576, RateLimitRPS: 10, RateLimitBurst: 20, RateLimitMaxClients: 10000,
	}) {
		t.Fatalf("unexpected HTTP defaults: %+v", cfg.HTTP)
	}
	if cfg.Postgres.Host != "127.0.0.1" || cfg.Postgres.Port != 5432 || cfg.Postgres.Database != "threats" || cfg.Postgres.User != "threats" || cfg.Postgres.SSLMode != "disable" || cfg.Postgres.MaxConns != 10 || cfg.Postgres.ConnectTimeout != 5*time.Second || cfg.Postgres.OperationTimeout != 5*time.Second {
		t.Fatal("unexpected PostgreSQL defaults")
	}
}

func TestConfigurationOverridesAndEscapedURL(t *testing.T) {
	password := " p@ss:/?#% '& "
	cfg, err := loadValues(t, map[string]string{
		"LOG_LEVEL": "DEBUG+2", "HTTP_ADDR": "[::1]:0", "HTTP_READ_HEADER_TIMEOUT": "2s",
		"HTTP_READ_TIMEOUT": "3s", "HTTP_WRITE_TIMEOUT": "4s", "HTTP_IDLE_TIMEOUT": "5s", "HTTP_SHUTDOWN_TIMEOUT": "6s",
		"HTTP_MAX_BODY_BYTES": "1", "POSTGRES_HOST": "::1", "POSTGRES_PORT": "65535",
		"POSTGRES_DB": "db /?#%", "POSTGRES_USER": "u@:/?#", "POSTGRES_PASSWORD": password,
		"POSTGRES_SSLMODE": "verify-full", "POSTGRES_MAX_CONNS": "1", "POSTGRES_CONNECT_TIMEOUT": "7s", "POSTGRES_OPERATION_TIMEOUT": "250ms",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelDebug+2 || cfg.HTTP.MaxBodyBytes != 1 || cfg.Postgres.Port != 65535 || cfg.Postgres.MaxConns != 1 || cfg.HTTP.ShutdownTimeout != 6*time.Second || cfg.Postgres.ConnectTimeout != 7*time.Second || cfg.Postgres.OperationTimeout != 250*time.Millisecond {
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
		"API_KEY":                     {"", strings.Repeat("a", 31), strings.Repeat("a", 257), strings.Repeat("a", 31) + "!", strings.Repeat("a", 31) + "é"},
		"HTTP_RATE_LIMIT_RPS":         {"", "0", "-1", "1.5", "2147483648"},
		"HTTP_RATE_LIMIT_BURST":       {"", "0", "-1", "invalid", "2147483648"},
		"HTTP_RATE_LIMIT_MAX_CLIENTS": {"", "0", "-1", "invalid", "2147483648"},
		"LOG_LEVEL":                   {"", "secret-invalid"}, "HTTP_ADDR": {"", "missing-port", "localhost:-1", "localhost:65536", "bad host:80", "127.0.0.1:http"},
		"HTTP_READ_HEADER_TIMEOUT": {"", "0s", "-1s", "secret-invalid"}, "HTTP_READ_TIMEOUT": {"0"}, "HTTP_WRITE_TIMEOUT": {"-1s"}, "HTTP_IDLE_TIMEOUT": {"invalid"}, "HTTP_SHUTDOWN_TIMEOUT": {"0s"},
		"HTTP_MAX_BODY_BYTES": {"", "0", "-1", "9223372036854775808"},
		"POSTGRES_HOST":       {"", "bad host", "host:5432", "/tmp/postgres", "[::1]"}, "POSTGRES_PORT": {"", "0", "65536", "-1", "secret-invalid"},
		"POSTGRES_DB": {""}, "POSTGRES_USER": {""}, "POSTGRES_PASSWORD": {""}, "POSTGRES_SSLMODE": {"", "secret-invalid"},
		"POSTGRES_MAX_CONNS": {"0", "-1", "2147483648", "secret-invalid"}, "POSTGRES_CONNECT_TIMEOUT": {"0s", "-1s", "secret-invalid"},
		"POSTGRES_OPERATION_TIMEOUT": {"", "0s", "-1s", "secret-invalid", "9223372036854775808ns"},
	}
	for name, values := range cases {
		for _, value := range values {
			t.Run(name+"/"+value, func(t *testing.T) {
				cfg, err := loadValues(t, map[string]string{"POSTGRES_PASSWORD": "do-not-log-this", name: value})
				if err == nil || !strings.Contains(err.Error(), name) || !reflect.DeepEqual(cfg, Config{}) {
					t.Fatalf("expected configuration error for %s", name)
				}
				if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "do-not-log-this") {
					t.Fatal("configuration value leaked")
				}
			})
		}
	}
	if _, err := loadValues(t, nil); err == nil || !strings.Contains(err.Error(), "API_KEY") {
		t.Fatal("missing key accepted")
	}
}

func TestValidBoundariesAndSSLModes(t *testing.T) {
	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"} {
		_, err := loadValues(t, map[string]string{"POSTGRES_PASSWORD": "x", "POSTGRES_SSLMODE": mode, "HTTP_ADDR": ":65535", "POSTGRES_PORT": "1", "POSTGRES_MAX_CONNS": "2147483647", "HTTP_MAX_BODY_BYTES": "9223372036854775807"})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadUsesEnvironment(t *testing.T) {
	values := map[string]string{
		"API_KEY": strings.Repeat("a", 32), "HTTP_RATE_LIMIT_RPS": "10", "HTTP_RATE_LIMIT_BURST": "20", "HTTP_RATE_LIMIT_MAX_CLIENTS": "10000",
		"LOG_LEVEL": "INFO", "HTTP_ADDR": "127.0.0.1:0", "HTTP_READ_HEADER_TIMEOUT": "5s", "HTTP_READ_TIMEOUT": "10s", "HTTP_WRITE_TIMEOUT": "15s", "HTTP_IDLE_TIMEOUT": "60s", "HTTP_SHUTDOWN_TIMEOUT": "10s", "HTTP_MAX_BODY_BYTES": "1048576",
		"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": "5432", "POSTGRES_DB": "threats", "POSTGRES_USER": "threats", "POSTGRES_PASSWORD": "from-environment", "POSTGRES_SSLMODE": "disable", "POSTGRES_MAX_CONNS": "10", "POSTGRES_CONNECT_TIMEOUT": "5s", "POSTGRES_OPERATION_TIMEOUT": "250ms",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil || cfg.Postgres.Password != "from-environment" || cfg.HTTP.Addr != "127.0.0.1:0" || cfg.Postgres.OperationTimeout != 250*time.Millisecond {
		t.Fatal("environment configuration not applied")
	}
}

func TestOperationTimeoutValidBoundaries(t *testing.T) {
	for _, duration := range []time.Duration{time.Nanosecond, 1<<63 - 1} {
		cfg, err := loadValues(t, map[string]string{
			"POSTGRES_PASSWORD": "password", "POSTGRES_OPERATION_TIMEOUT": duration.String(),
		})
		if err != nil || cfg.Postgres.OperationTimeout != duration {
			t.Fatalf("operation timeout %s: got %s, %v", duration, cfg.Postgres.OperationTimeout, err)
		}
	}
}

func TestSecurityConfiguration(t *testing.T) {
	for _, length := range []int{32, 256} {
		cfg, err := loadValues(t, map[string]string{"API_KEY": strings.Repeat("-", length), "POSTGRES_PASSWORD": "x", "HTTP_RATE_LIMIT_RPS": "1", "HTTP_RATE_LIMIT_BURST": "2147483647", "HTTP_RATE_LIMIT_MAX_CLIENTS": "1"})
		if err != nil || len(cfg.APIKey) != length || cfg.HTTP.RateLimitRPS != 1 || cfg.HTTP.RateLimitBurst != 2147483647 || cfg.HTTP.RateLimitMaxClients != 1 {
			t.Fatal("security configuration not applied")
		}
	}
	_, err := loadValues(t, nil)
	if err == nil || !strings.Contains(err.Error(), "API_KEY") {
		t.Fatal("missing key accepted")
	}
	_, err = loadValues(t, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatal("missing password accepted")
	}
}

func TestLoadDoesNotReadDotenv(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("API_KEY="+strings.Repeat("a", 32)+"\nPOSTGRES_PASSWORD=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadValues(t, nil); err == nil || !strings.Contains(err.Error(), "API_KEY") {
		t.Fatal("Load unexpectedly read .env")
	}
}
