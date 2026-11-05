package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"kasperski-go-test/internal/config"
)

// serveHTTP owns the listener and request contexts, but not the database pool.
func serveHTTP(ctx context.Context, listener net.Listener, handler http.Handler, cfg config.HTTPConfig, logger *slog.Logger) error {
	defer listener.Close()
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	server := &http.Server{
		Handler: handler, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError), ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout,
		BaseContext: func(net.Listener) context.Context { return requestCtx },
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	logger.Info("HTTP server listening", "address", listener.Addr().String())
	select {
	case err := <-finished:
		cancelRequests()
		closeErr := server.Close()
		return errors.Join(fmt.Errorf("serve HTTP: %w", err), closeErr)
	case <-ctx.Done():
	}

	logger.Info("HTTP server stopping")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		cancelRequests()
		closeErr := server.Close()
		<-finished
		return errors.Join(fmt.Errorf("shutdown HTTP server: %w", err), closeErr)
	}
	<-finished
	logger.Info("HTTP server stopped")
	return nil
}
