package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"kaspersky-go-test/internal/config"
)

type runningServer struct {
	cancel  context.CancelFunc
	done    chan struct{}
	address string
	err     error
}

func startServer(t *testing.T, handler http.Handler, timeout time.Duration) *runningServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	running := &runningServer{cancel: cancel, done: make(chan struct{}), address: listener.Addr().String()}
	cfg := config.HTTPConfig{ReadHeaderTimeout: time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: time.Second, ShutdownTimeout: timeout}
	go func() {
		running.err = serveHTTP(ctx, listener, handler, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		close(running.done)
	}()
	t.Cleanup(func() {
		running.cancel()
		select {
		case <-running.done:
		case <-time.After(4 * time.Second):
			t.Error("server did not terminate")
		}
	})
	return running
}

func waitStopped(t *testing.T, running *runningServer) error {
	t.Helper()
	select {
	case <-running.done:
		return running.err
	case <-time.After(4 * time.Second):
		t.Fatal("server did not stop")
		return nil
	}
}

func TestServerNormalShutdown(t *testing.T) {
	running := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), time.Second)
	running.cancel()
	if err := waitStopped(t, running); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", running.address)
	if err != nil {
		t.Fatalf("listener not released: %v", err)
	}
	listener.Close()
}

func TestServerDrainsInFlightRequest(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	running := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.Context()
		<-release
		_, _ = w.Write([]byte("finished"))
	}), 2*time.Second)
	client := &http.Client{Timeout: 3 * time.Second}
	result := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + running.address)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil {
				err = readErr
			} else if string(body) != "finished" {
				err = errors.New("request did not finish")
			}
		}
		result <- err
	}()
	var requestCtx context.Context
	select {
	case requestCtx = <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not accepted")
	}
	running.cancel()
	// Wait until Shutdown has closed the listener, without assuming scheduling delays.
	deadline := time.After(time.Second)
	for {
		connection, err := net.DialTimeout("tcp", running.address, 50*time.Millisecond)
		if err != nil {
			break
		}
		connection.Close()
		select {
		case <-deadline:
			t.Fatal("listener still accepting")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if requestCtx.Err() != nil {
		t.Error("signal canceled active request before drain")
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := waitStopped(t, running); err != nil {
		t.Fatal(err)
	}
}

func TestServerForcesCloseAfterShutdownTimeout(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	running := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(canceled) }), 40*time.Millisecond)
	result := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + running.address)
		if response != nil {
			response.Body.Close()
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request not accepted")
	}
	running.cancel()
	if err := waitStopped(t, running); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("request context was not canceled")
	}
	if err := <-result; err == nil {
		t.Fatal("forced-close request unexpectedly completed")
	}
}

func TestServerReportsListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	err = serveHTTP(context.Background(), listener, http.NotFoundHandler(), config.HTTPConfig{ShutdownTimeout: time.Second}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("listener failure not preserved: %v", err)
	}
}
