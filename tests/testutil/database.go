//go:build integration || e2e

// Package testutil содержит fixtures только для изолированных Docker-тестов.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresImage закрепляет образ тестовой БД по версии и digest, как в Compose.
const PostgresImage = "postgres:18.6-bookworm@sha256:f1ea2a1a4fb7b68d4836be1fdcb8a6466a89b072158bf9d97663600538c294c5"

// MigrateImage закрепляет одноразовый мигратор, используемый для реальных SQL-миграций.
const MigrateImage = "migrate/migrate:v4.20.1@sha256:76cc2074cb6642631f34a898ced71e6aeaa6b1a4d78c4daa743275a22e0c5be7"

// Root возвращает абсолютный путь к корню проекта по расположению исходника helper-пакета.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

// RandomID возвращает 32 случайных hex-символа и вызывает panic при сбое источника случайности.
func RandomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Redact заменяет непустые секреты маркером [REDACTED] перед выводом диагностики.
func Redact(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	return text
}

// Database содержит временную PostgreSQL, её Docker-сеть и секретные реквизиты.
type Database struct {
	// Container — PostgreSQL-контейнер, принадлежащий fixture.
	Container *postgres.PostgresContainer
	// Network — отдельная Docker-сеть для БД и одноразового мигратора.
	Network *testcontainers.DockerNetwork
	// User — роль для подключения к тестовой БД.
	User string
	// Password — временный секрет, включающий зарезервированные URL-символы.
	Password string
	// Name — имя временной базы данных.
	Name string
	// DSN — экранированный URL подключения с опубликованным портом; содержит пароль.
	DSN string
}

// NewDatabase запускает БД на случайном порту без миграций; при успехе ресурсы нужно освободить через Close.
func NewDatabase(ctx context.Context) (_ *Database, resultErr error) {
	db := &Database{User: "threats_test", Name: "threats_test", Password: RandomID() + "@:/?#"}
	defer func() {
		if resultErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			resultErr = errors.Join(resultErr, db.Close(cleanup))
		}
	}()
	var err error
	db.Network, err = network.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create test network: %s", db.sanitize(err.Error()))
	}
	db.Container, err = postgres.Run(ctx, PostgresImage,
		postgres.WithDatabase(db.Name), postgres.WithUsername(db.User), postgres.WithPassword(db.Password),
		postgres.BasicWaitStrategies(), network.WithNetwork([]string{"postgres"}, db.Network),
		testcontainers.WithLogger(log.New(io.Discard, "", 0)),
	)
	if err != nil {
		return nil, fmt.Errorf("start test PostgreSQL: %s", db.sanitize(err.Error()))
	}
	host, err := db.Container.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve test PostgreSQL host: %s", db.sanitize(err.Error()))
	}
	port, err := db.Container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, fmt.Errorf("resolve test PostgreSQL port: %s", db.sanitize(err.Error()))
	}
	// Реквизиты содержат зарезервированные URL-символы: не собираем DSN через fmt.
	connection := url.URL{Scheme: "postgres", User: url.UserPassword(db.User, db.Password), Host: net.JoinHostPort(host, port.Port()), Path: "/" + db.Name, RawQuery: "sslmode=disable"}
	db.DSN = connection.String()
	return db, nil
}

func (db *Database) sanitize(text string) string { return Redact(text, db.Password, db.DSN) }

// Close удаляет тестовый контейнер, его тома и сеть; пулы нужно закрыть заранее, ctx должен оставаться активным.
func (db *Database) Close(ctx context.Context) error {
	var errs []error
	if db.Container != nil {
		if err := db.Container.Terminate(ctx); err != nil {
			errs = append(errs, fmt.Errorf("remove test PostgreSQL: %s", db.sanitize(err.Error())))
		}
	}
	if db.Network != nil {
		if err := db.Network.Remove(ctx); err != nil {
			errs = append(errs, fmt.Errorf("remove test network: %s", db.sanitize(err.Error())))
		}
	}
	return errors.Join(errs...)
}

// StartDatabase запускает БД, при migrate=true применяет миграции и регистрирует очистку через t.Cleanup.
func StartDatabase(t *testing.T, migrate bool) *Database {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := NewDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if migrate {
		if err := db.Migrate(ctx, "up"); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// Pool открывает пул, проверяет Ping и регистрирует закрытие через t.Cleanup; ошибка завершает тест.
func (db *Database) Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, db.DSN)
	if err != nil {
		t.Fatal(db.sanitize(err.Error()))
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(db.sanitize(err.Error()))
	}
	return pool
}

// Migrate запускает golang-migrate с args в одноразовом контейнере и убирает реквизиты из сообщений об ошибках.
func (db *Database) Migrate(ctx context.Context, args ...string) (resultErr error) {
	files, err := filepath.Glob(filepath.Join(Root(), "migrations", "*.sql"))
	if err != nil {
		return err
	}
	request := testcontainers.ContainerRequest{
		Image: MigrateImage, Env: map[string]string{"PGUSER": db.User, "PGDATABASE": db.Name, "PGPASSWORD": db.Password},
		Networks:   []string{db.Network.Name},
		Cmd:        append([]string{"-path", "/migrations", "-database", "postgres://postgres:5432/?sslmode=disable"}, args...),
		WaitingFor: wait.ForExit().WithExitTimeout(30 * time.Second),
	}
	for _, file := range files {
		request.Files = append(request.Files, testcontainers.ContainerFile{HostFilePath: file, ContainerFilePath: "/migrations/" + filepath.Base(file), FileMode: 0o444})
	}
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: true, Logger: log.New(io.Discard, "", 0)})
	if ctr != nil {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := ctr.Terminate(cleanup); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove test migrator: %s", db.sanitize(err.Error())))
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("run test migrations: %s", db.sanitize(err.Error()))
	}
	state, err := ctr.State(ctx)
	if err != nil {
		return fmt.Errorf("inspect test migrator: %s", db.sanitize(err.Error()))
	}
	if state.ExitCode != 0 {
		text := ""
		if logs, err := ctr.Logs(ctx); err == nil {
			defer logs.Close()
			data, _ := io.ReadAll(logs)
			text = string(data)
		}
		return fmt.Errorf("test migrator exited %d: %s", state.ExitCode, db.sanitize(text))
	}
	return nil
}

// CleanEnv исключает настройки приложения и Compose из окружения разработчика.
func CleanEnv(overrides map[string]string) []string {
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "POSTGRES_") || strings.HasPrefix(name, "HTTP_") || strings.HasPrefix(name, "PG") || strings.HasPrefix(name, "COMPOSE_") || name == "API_KEY" || name == "API_PORT" || name == "LOG_LEVEL" {
			continue
		}
		if _, ok := overrides[name]; !ok {
			env = append(env, entry)
		}
	}
	for name, value := range overrides {
		env = append(env, name+"="+value)
	}
	return env
}
