// Command api currently verifies bootstrap configuration and exits.
package main

import (
	"log/slog"
	"os"
)

func main() {
	level := slog.LevelInfo
	if value, ok := os.LookupEnv("LOG_LEVEL"); ok {
		if err := level.UnmarshalText([]byte(value)); err != nil {
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("invalid configuration", "variable", "LOG_LEVEL", "error", err)
			os.Exit(1)
		}
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	logger.Info("bootstrap complete; HTTP server is not implemented")
}
