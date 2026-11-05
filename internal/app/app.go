// Package app собирает зависимости API и управляет HTTP-сервером и пулом PostgreSQL.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"kaspersky-go-test/internal/config"
	"kaspersky-go-test/internal/repository/postgres"
	httptransport "kaspersky-go-test/internal/transport/http"
	"kaspersky-go-test/internal/usecase"
)

// Run проверяет соединение с БД и запускает API до отмены ctx; принимает проверенный cfg и ненулевой logger, закрывает пул после остановки HTTP.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	poolConfig, err := pgxpool.ParseConfig(cfg.Postgres.URL())
	if err != nil {
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
	cancelStartup()
	if err := ctx.Err(); err != nil {
		return err
	}
	repository := postgres.NewThreatRepository(pool, cfg.Postgres.OperationTimeout)
	service := usecase.NewThreatService(repository)
	handler := httptransport.NewHandler(service, logger, cfg.HTTP.MaxBodyBytes, httptransport.SecurityOptions{
		APIKey: cfg.APIKey, RateLimitRPS: cfg.HTTP.RateLimitRPS, RateLimitBurst: cfg.HTTP.RateLimitBurst, RateLimitMaxClients: cfg.HTTP.RateLimitMaxClients,
		ReadinessTimeout: min(cfg.Postgres.OperationTimeout, time.Second), Ping: pool.Ping,
	})
	listener, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	return serveHTTP(ctx, listener, handler, cfg.HTTP, logger)
}
