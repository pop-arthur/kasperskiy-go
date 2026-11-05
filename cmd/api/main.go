// Command api runs the threat information REST service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"kasperski-go-test/internal/config"
	"kasperski-go-test/internal/repository/postgres"
	httptransport "kasperski-go-test/internal/transport/http"
	"kasperski-go-test/internal/usecase"
)

func main() { os.Exit(run()) }

func run() int {
	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("API startup failed", "error", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runServer(ctx, cfg, logger); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return 0
		}
		bootstrapLogger.Error("API stopped with error", "error", err)
		return 1
	}
	return 0
}

func runServer(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	poolConfig, err := pgxpool.ParseConfig(cfg.Postgres.URL())
	if err != nil {
		// Parse errors may include the original connection string.
		return errors.New("invalid PostgreSQL connection configuration")
	}
	poolConfig.MaxConns = cfg.Postgres.MaxConns
	poolConfig.ConnConfig.ConnectTimeout = cfg.Postgres.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "threats-api"
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.Postgres.ConnectTimeout)
	defer cancelStartup()
	pool, err := pgxpool.NewWithConfig(startupCtx, poolConfig)
	if err != nil {
		return fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	if err := checkSchema(startupCtx, pool); err != nil {
		return fmt.Errorf("verify database schema; run make migrate-up: %w", err)
	}
	cancelStartup()
	if err := ctx.Err(); err != nil {
		return err
	}
	repository := postgres.NewThreatRepository(pool)
	service := usecase.NewThreatService(repository)
	handler := httptransport.NewHandler(service, logger, cfg.HTTP.MaxBodyBytes)
	listener, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	return serveHTTP(ctx, listener, handler, cfg.HTTP, logger)
}

func checkSchema(ctx context.Context, pool *pgxpool.Pool) error {
	// One clean version is required; application startup never executes DDL.
	const versionQuery = `SELECT count(*) = 1 AND COALESCE(bool_and(version = 1 AND NOT dirty), false)
		FROM public.schema_migrations`
	var valid bool
	if err := pool.QueryRow(ctx, versionQuery).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errors.New("expected migration version 1 with dirty=false")
	}
	rows, err := pool.Query(ctx, `SELECT md5, name, description, created_at, updated_at FROM public.threats LIMIT 0`)
	if err != nil {
		return err
	}
	rows.Close()
	return rows.Err()
}
