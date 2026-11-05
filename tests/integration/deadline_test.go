//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"kaspersky-go-test/internal/domain"
	"kaspersky-go-test/internal/repository/postgres"
	httptransport "kaspersky-go-test/internal/transport/http"
	"kaspersky-go-test/internal/usecase"
)

const operationTimeout = 200 * time.Millisecond

func occupyPool(t *testing.T, ctx context.Context, pool *pgxpool.Pool) func() {
	t.Helper()
	var connections []*pgxpool.Conn
	release := func() {
		for _, connection := range connections {
			connection.Release()
		}
		connections = nil
	}
	t.Cleanup(release)
	for range pool.Config().MaxConns {
		connection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}
	return release
}

func lockThreat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, md5 domain.MD5) func() {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("release row lock: %v", err)
		}
	}
	t.Cleanup(release)
	if _, err := tx.Exec(ctx, "SELECT md5 FROM public.threats WHERE md5=$1 FOR UPDATE", string(md5)); err != nil {
		t.Fatal(err)
	}
	return release
}

func requireOperationDeadline(t *testing.T, parent context.Context, got domain.Threat, err error) {
	t.Helper()
	if !errors.Is(err, context.DeadlineExceeded) || errors.Unwrap(err) == nil {
		t.Fatalf("operation error = %v; want wrapped deadline", err)
	}
	if parent.Err() != nil {
		t.Fatalf("parent expired before repository's own budget: %v", parent.Err())
	}
	if !reflect.DeepEqual(got, domain.Threat{}) {
		t.Fatalf("result with error must be zero: %+v", got)
	}
}

func TestRepositoryOperationTimeoutWhileWaitingForConnection(t *testing.T) {
	ctx, pool, recovered := newRepository(t)
	repo := postgres.NewThreatRepository(pool, operationTimeout)
	release := occupyPool(t, ctx, pool)
	md5 := key(t)
	for name, call := range operations(repo, md5) {
		t.Run(name, func(t *testing.T) {
			parent, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			got, err := call(parent)
			requireOperationDeadline(t, parent, got, err)
		})
	}
	release()
	calls := operations(recovered, md5)
	for _, name := range []string{"create", "get", "update"} {
		if _, err := calls[name](ctx); err != nil {
			t.Fatalf("pool did not recover for %s: %v", name, err)
		}
	}
}

func TestRepositoryOperationTimeoutDuringUpdate(t *testing.T) {
	ctx, pool, recovered := newRepository(t)
	md5 := key(t)
	if _, err := recovered.Create(ctx, md5, domain.ThreatData{Name: "original"}); err != nil {
		t.Fatal(err)
	}
	release := lockThreat(t, ctx, pool, md5)
	repo := postgres.NewThreatRepository(pool, operationTimeout)
	parent, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	got, err := repo.Update(parent, md5, domain.ThreatData{Name: "timed out"})
	requireOperationDeadline(t, parent, got, err)
	release()
	if _, err := recovered.GetByMD5(ctx, md5); err != nil {
		t.Fatalf("pool did not recover after SQL timeout: %v", err)
	}
}

type deadlineLogs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (logs *deadlineLogs) Write(data []byte) (int, error) {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.buffer.Write(data)
}

func (logs *deadlineLogs) String() string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.buffer.String()
}

func TestHTTPOperationTimeoutWithPostgres(t *testing.T) {
	ctx, pool, recovered := newRepository(t)
	md5 := key(t)
	if _, err := recovered.Create(ctx, md5, domain.ThreatData{Name: "original"}); err != nil {
		t.Fatal(err)
	}
	release := lockThreat(t, ctx, pool, md5)
	repo := postgres.NewThreatRepository(pool, operationTimeout)
	var logs deadlineLogs
	handler := httptransport.NewHandler(usecase.NewThreatService(repo), slog.New(slog.NewJSONHandler(&logs, nil)), 4096, integrationSecurity(pool.Ping))
	server := httptest.NewUnstartedServer(handler)
	server.Config.ReadHeaderTimeout = time.Second
	server.Config.ReadTimeout = 3 * time.Second
	server.Config.WriteTimeout = 3 * time.Second
	server.Start()
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 3 * time.Second
	r, err := http.NewRequestWithContext(ctx, http.MethodPut, server.URL+"/info/"+string(md5), strings.NewReader(`{"name":"timed out"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+integrationAPIKey)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 500 || response.Header.Get("Content-Type") != "application/json" || len(body) != 2 || body["message"] != "Internal server error" || body["details"] != "" {
		t.Fatalf("timeout response = %d, %v", response.StatusCode, body)
	}
	if strings.Count(logs.String(), `"level":"ERROR"`) != 1 {
		t.Fatalf("timeout must be logged once: %s", logs.String())
	}
	release()
	r, err = http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/info/"+string(md5), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+integrationAPIKey)
	response, err = client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("HTTP did not recover after timeout: %d", response.StatusCode)
	}
}
