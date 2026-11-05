// Package config reads and validates process configuration without loading .env files.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LogLevel slog.Level
	HTTP     HTTPConfig
	Postgres PostgresConfig
}

type HTTPConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
}

type PostgresConfig struct {
	Host           string
	Port           uint16
	Database       string
	User           string
	Password       string
	SSLMode        string
	MaxConns       int32
	ConnectTimeout time.Duration
}

// URL constructs a connection string with escaped credentials. Never log it.
func (cfg PostgresConfig) URL() string {
	connection := url.URL{
		Scheme: "postgres", User: url.UserPassword(cfg.User, cfg.Password),
		Host: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		Path: "/" + cfg.Database,
	}
	query := url.Values{"sslmode": {cfg.SSLMode}}
	connection.RawQuery = query.Encode()
	return connection.String()
}

func Load() (Config, error) { return load(os.LookupEnv) }

func load(lookup func(string) (string, bool)) (Config, error) {
	reader := envReader{lookup: lookup}
	cfg := Config{}
	if err := cfg.LogLevel.UnmarshalText([]byte(reader.text("LOG_LEVEL", "INFO"))); err != nil {
		reader.invalid("LOG_LEVEL")
	}
	cfg.HTTP = HTTPConfig{
		Addr:              reader.text("HTTP_ADDR", "127.0.0.1:8080"),
		ReadHeaderTimeout: reader.duration("HTTP_READ_HEADER_TIMEOUT", "5s"),
		ReadTimeout:       reader.duration("HTTP_READ_TIMEOUT", "10s"),
		WriteTimeout:      reader.duration("HTTP_WRITE_TIMEOUT", "15s"),
		IdleTimeout:       reader.duration("HTTP_IDLE_TIMEOUT", "60s"),
		ShutdownTimeout:   reader.duration("HTTP_SHUTDOWN_TIMEOUT", "10s"),
		MaxBodyBytes:      reader.integer("HTTP_MAX_BODY_BYTES", "1048576", 1, 1<<63-1),
	}
	host, port, err := net.SplitHostPort(cfg.HTTP.Addr)
	if err != nil || !validHost(host, true) {
		reader.invalid("HTTP_ADDR")
	} else if value, err := strconv.ParseUint(port, 10, 16); err != nil || value > 65535 {
		reader.invalid("HTTP_ADDR")
	}
	cfg.Postgres = PostgresConfig{
		Host:           reader.text("POSTGRES_HOST", "127.0.0.1"),
		Port:           uint16(reader.integer("POSTGRES_PORT", "5432", 1, 65535)),
		Database:       reader.text("POSTGRES_DB", "threats"),
		User:           reader.text("POSTGRES_USER", "threats"),
		Password:       reader.text("POSTGRES_PASSWORD", ""),
		SSLMode:        reader.text("POSTGRES_SSLMODE", "disable"),
		MaxConns:       int32(reader.integer("POSTGRES_MAX_CONNS", "10", 1, 1<<31-1)),
		ConnectTimeout: reader.duration("POSTGRES_CONNECT_TIMEOUT", "5s"),
	}
	if !validHost(cfg.Postgres.Host, false) {
		reader.invalid("POSTGRES_HOST")
	}
	switch cfg.Postgres.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		reader.invalid("POSTGRES_SSLMODE")
	}
	if reader.err != nil {
		return Config{}, reader.err
	}
	return cfg, nil
}

func validHost(host string, allowEmpty bool) bool {
	if host == "" {
		return allowEmpty
	}
	if strings.ContainsAny(host, " \t\r\n\x00/[]") {
		return false
	}
	return !strings.Contains(host, ":") || net.ParseIP(host) != nil
}

type envReader struct {
	lookup func(string) (string, bool)
	err    error
}

func (reader *envReader) invalid(name string) {
	if reader.err == nil {
		reader.err = fmt.Errorf("invalid configuration: %s", name)
	}
}

func (reader *envReader) text(name, fallback string) string {
	value, exists := reader.lookup(name)
	if !exists {
		value = fallback
	}
	if value == "" || strings.ContainsRune(value, '\x00') {
		reader.invalid(name)
	}
	return value
}

func (reader *envReader) duration(name, fallback string) time.Duration {
	value, err := time.ParseDuration(reader.text(name, fallback))
	if err != nil || value <= 0 {
		reader.invalid(name)
	}
	return value
}

func (reader *envReader) integer(name, fallback string, min, max int64) int64 {
	value, err := strconv.ParseInt(reader.text(name, fallback), 10, 64)
	if err != nil || value < min || value > max {
		reader.invalid(name)
	}
	return value
}
