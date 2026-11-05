// Package config читает и проверяет настройки окружения через cleanenv.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"reflect"

	"github.com/ilyakaznacheev/cleanenv"
	"strconv"
	"strings"
	"time"
)

// Config содержит настройки процесса и секреты, поэтому целиком логировать её нельзя.
type Config struct {
	// APIKey — обязательный секрет для Bearer-аутентификации всех бизнес-маршрутов.
	APIKey string `env:"API_KEY" env-required:"true"`
	// LogLevel задаёт порог структурированного логирования в синтаксисе slog.
	LogLevel slog.Level `env:"LOG_LEVEL" env-default:"INFO"`
	// HTTP содержит настройки listener и HTTP-адаптера.
	HTTP HTTPConfig
	// Postgres содержит реквизиты и ограничения работы с БД.
	Postgres PostgresConfig
}

// HTTPConfig задаёт адрес сервера, таймауты и лимиты HTTP-запросов.
type HTTPConfig struct {
	// Addr — TCP-адрес host:port; порт 0 выбирает свободный порт при запуске.
	Addr string `env:"HTTP_ADDR" env-default:"127.0.0.1:8080"`
	// ReadHeaderTimeout ограничивает чтение HTTP-заголовков.
	ReadHeaderTimeout time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" env-default:"5s"`
	// ReadTimeout ограничивает чтение запроса, включая тело.
	ReadTimeout time.Duration `env:"HTTP_READ_TIMEOUT" env-default:"10s"`
	// WriteTimeout ограничивает запись HTTP-ответа независимо от deadline операции БД.
	WriteTimeout time.Duration `env:"HTTP_WRITE_TIMEOUT" env-default:"15s"`
	// IdleTimeout ограничивает ожидание следующего запроса на keep-alive соединении.
	IdleTimeout time.Duration `env:"HTTP_IDLE_TIMEOUT" env-default:"60s"`
	// ShutdownTimeout задаёт время на завершение текущих запросов перед принудительным закрытием.
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" env-default:"10s"`
	// MaxBodyBytes — максимальный размер тела POST/PUT в байтах.
	MaxBodyBytes int64 `env:"HTTP_MAX_BODY_BYTES" env-default:"1048576"`
	// RateLimitRPS задаёт скорость восстановления токенов в секунду для каждого IP.
	RateLimitRPS int `env:"HTTP_RATE_LIMIT_RPS" env-default:"10"`
	// RateLimitBurst задаёт ёмкость token bucket, доступную новому IP целиком.
	RateLimitBurst int `env:"HTTP_RATE_LIMIT_BURST" env-default:"20"`
	// RateLimitMaxClients ограничивает число одновременно хранимых IP в limiter.
	RateLimitMaxClients int `env:"HTTP_RATE_LIMIT_MAX_CLIENTS" env-default:"10000"`
}

// PostgresConfig содержит реквизиты БД, размер пула и таймауты операций.
type PostgresConfig struct {
	// Host — TCP-хост PostgreSQL; IPv6 задаётся без квадратных скобок.
	Host string `env:"POSTGRES_HOST" env-default:"127.0.0.1"`
	// Port — TCP-порт PostgreSQL в диапазоне 1–65535.
	Port uint16 `env:"POSTGRES_PORT" env-default:"5432"`
	// Database — имя базы данных.
	Database string `env:"POSTGRES_DB" env-default:"threats"`
	// User — роль подключения PostgreSQL.
	User string `env:"POSTGRES_USER" env-default:"threats"`
	// Password — обязательный пароль; пробелы не обрезаются.
	Password string `env:"POSTGRES_PASSWORD" env-required:"true"`
	// SSLMode задаёт режим TLS, принимаемый pgx.
	SSLMode string `env:"POSTGRES_SSLMODE" env-default:"disable"`
	// MaxConns ограничивает число соединений в общем пуле.
	MaxConns int32 `env:"POSTGRES_MAX_CONNS" env-default:"10"`
	// ConnectTimeout ограничивает подключение и общий startup-контекст приложения.
	ConnectTimeout time.Duration `env:"POSTGRES_CONNECT_TIMEOUT" env-default:"5s"`
	// OperationTimeout — единый бюджет ожидания пула, выполнения SQL и чтения результата операции.
	OperationTimeout time.Duration `env:"POSTGRES_OPERATION_TIMEOUT" env-default:"5s"`
}

// URL возвращает DSN с экранированными реквизитами; строка содержит пароль и не подходит для логов.
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

// Load читает окружение без загрузки .env; пустые значения отклоняются, при ошибке возвращается нулевая Config без секретов в диагностике.
func Load() (Config, error) {
	fields := environmentFields(reflect.TypeFor[Config]())
	for _, field := range fields {
		value, exists := os.LookupEnv(field.name)
		if (!exists && field.required) || (exists && (value == "" || strings.ContainsRune(value, '\x00'))) {
			return Config{}, invalid(field.name)
		}
	}
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		// Ошибка cleanenv может содержать исходное значение. Извлекаем только известное
		// имя переменной из служебного префикса, не возвращая и не оборачивая причину.
		if strings.HasPrefix(err.Error(), "parsing field ") {
			_, suffix, ok := strings.Cut(err.Error(), ` env `)
			if ok {
				name, _, found := strings.Cut(suffix, `: `)
				if found {
					for _, field := range fields {
						if field.name == name {
							return Config{}, invalid(name)
						}
					}
				}
			}
		}
		return Config{}, fmt.Errorf("invalid configuration")
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

type environmentField struct {
	name     string
	required bool
}

func environmentFields(typ reflect.Type) []environmentField {
	var fields []environmentField
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if name := field.Tag.Get("env"); name != "" {
			_, required := field.Tag.Lookup("env-required")
			fields = append(fields, environmentField{name, required})
		} else if field.Type.Kind() == reflect.Struct {
			fields = append(fields, environmentFields(field.Type)...)
		}
	}
	return fields
}

func invalid(name string) error { return fmt.Errorf("invalid configuration: %s", name) }

func (cfg Config) validate() error {
	if !validAPIKey(cfg.APIKey) {
		return invalid("API_KEY")
	}
	host, port, err := net.SplitHostPort(cfg.HTTP.Addr)
	if err != nil || !validHost(host, true) {
		return invalid("HTTP_ADDR")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return invalid("HTTP_ADDR")
	}
	if !validHost(cfg.Postgres.Host, false) {
		return invalid("POSTGRES_HOST")
	}
	if cfg.Postgres.Port == 0 {
		return invalid("POSTGRES_PORT")
	}
	if cfg.Postgres.MaxConns <= 0 {
		return invalid("POSTGRES_MAX_CONNS")
	}
	if cfg.HTTP.MaxBodyBytes <= 0 {
		return invalid("HTTP_MAX_BODY_BYTES")
	}
	for _, setting := range []struct {
		name  string
		value int
	}{
		{"HTTP_RATE_LIMIT_RPS", cfg.HTTP.RateLimitRPS},
		{"HTTP_RATE_LIMIT_BURST", cfg.HTTP.RateLimitBurst},
		{"HTTP_RATE_LIMIT_MAX_CLIENTS", cfg.HTTP.RateLimitMaxClients},
	} {
		if setting.value <= 0 || int64(setting.value) > 1<<31-1 {
			return invalid(setting.name)
		}
	}
	for _, setting := range []struct {
		name  string
		value time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", cfg.HTTP.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", cfg.HTTP.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", cfg.HTTP.WriteTimeout},
		{"HTTP_IDLE_TIMEOUT", cfg.HTTP.IdleTimeout},
		{"HTTP_SHUTDOWN_TIMEOUT", cfg.HTTP.ShutdownTimeout},
		{"POSTGRES_CONNECT_TIMEOUT", cfg.Postgres.ConnectTimeout},
		{"POSTGRES_OPERATION_TIMEOUT", cfg.Postgres.OperationTimeout},
	} {
		if setting.value <= 0 {
			return invalid(setting.name)
		}
	}
	switch cfg.Postgres.SSLMode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return invalid("POSTGRES_SSLMODE")
	}
	return nil
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

func validAPIKey(key string) bool {
	if len(key) < 32 || len(key) > 256 {
		return false
	}
	for _, c := range key {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
