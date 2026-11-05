// Command api запускает сервис угроз или проверяет его готовность.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"kaspersky-go-test/internal/app"
	"kaspersky-go-test/internal/config"
)

func main() { os.Exit(run()) }

func run() int {
	if len(os.Args) > 1 {
		return runHealthcheck(os.Args[1:])
	}
	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("API startup failed", "error", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, logger); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return 0
		}
		bootstrapLogger.Error("API stopped with error", "error", err)
		return 1
	}
	return 0
}
