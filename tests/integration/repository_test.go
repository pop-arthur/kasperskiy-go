//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"kasperski-go-test/internal/domain"
	"kasperski-go-test/internal/repository/postgres"
	"kasperski-go-test/internal/usecase"
)

var _ usecase.ThreatRepository = (*postgres.ThreatRepository)(nil)

func newRepository(t *testing.T) (context.Context, *pgxpool.Pool, *postgres.ThreatRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required; use make test-repository for an isolated database")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse TEST_DATABASE_URL: invalid connection configuration")
	}
	config.MaxConns = 4
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	config.ConnConfig.RuntimeParams["application_name"] = "repository-integration"
	config.ConnConfig.RuntimeParams["timezone"] = "Europe/Moscow"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return ctx, pool, postgres.NewThreatRepository(pool)
}

func key(t *testing.T) domain.MD5 {
	t.Helper()
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		t.Fatal(err)
	}
	md5, err := domain.ParseMD5(hex.EncodeToString(bytes[:]))
	if err != nil {
		t.Fatal(err)
	}
	return md5
}

func requireThreat(t *testing.T, got domain.Threat, md5 domain.MD5, data domain.ThreatData) {
	t.Helper()
	if got.MD5 != md5 || got.Name != data.Name || !reflect.DeepEqual(got.Description, data.Description) {
		t.Fatalf("unexpected threat fields: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("database timestamps are missing")
	}
	if got.CreatedAt.Location() != time.UTC || got.UpdatedAt.Location() != time.UTC {
		t.Fatal("timestamps must be normalized to UTC")
	}
}

func TestRepositoryCreateAndGet(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	for _, description := range []*string{nil, ptr(""), ptr(" описание\nугрозы ")} {
		md5 := key(t)
		data := domain.ThreatData{Name: "Trojan'); DROP TABLE public.threats; --", Description: description}
		created, err := repo.Create(ctx, md5, data)
		if err != nil {
			t.Fatal(err)
		}
		requireThreat(t, created, md5, data)
		if !created.CreatedAt.Equal(created.UpdatedAt) {
			t.Fatal("initial timestamps must be equal")
		}
		got, err := repo.GetByMD5(ctx, md5)
		if err != nil || !reflect.DeepEqual(got, created) {
			t.Fatalf("GetByMD5 = %+v, %v; want %+v", got, err, created)
		}
		var storedCreated, storedUpdated time.Time
		if err := pool.QueryRow(ctx, "SELECT created_at, updated_at FROM public.threats WHERE md5=$1", string(md5)).Scan(&storedCreated, &storedUpdated); err != nil {
			t.Fatal(err)
		}
		if !created.CreatedAt.Equal(storedCreated) || !created.UpdatedAt.Equal(storedUpdated) {
			t.Fatal("returned timestamps differ from the stored timestamps")
		}
	}
	for _, name := range []string{"", " \n ", "Угроза"} {
		md5 := key(t)
		created, err := repo.Create(ctx, md5, domain.ThreatData{Name: name})
		if err != nil || created.Name != name {
			t.Fatalf("name was not preserved: %q, %v", created.Name, err)
		}
	}
}

func TestRepositoryNotFound(t *testing.T) {
	ctx, _, repo := newRepository(t)
	md5 := key(t)
	if _, err := repo.GetByMD5(ctx, md5); !errors.Is(err, domain.ErrThreatNotFound) {
		t.Fatalf("GetByMD5 = %v; want not found", err)
	}
	if _, err := repo.Update(ctx, md5, domain.ThreatData{Name: "missing"}); !errors.Is(err, domain.ErrThreatNotFound) {
		t.Fatalf("Update = %v; want not found", err)
	}
	if _, err := repo.GetByMD5(ctx, md5); !errors.Is(err, domain.ErrThreatNotFound) {
		t.Fatalf("Update created an absent record: %v", err)
	}
}

func TestRepositoryDuplicateDoesNotChangeRecord(t *testing.T) {
	ctx, _, repo := newRepository(t)
	md5 := key(t)
	created, err := repo.Create(ctx, md5, domain.ThreatData{Name: "original", Description: ptr("keep")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, md5, domain.ThreatData{Name: "overwrite"}); !errors.Is(err, domain.ErrThreatAlreadyExists) {
		t.Fatalf("Create = %v; want already exists", err)
	}
	got, err := repo.GetByMD5(ctx, md5)
	if err != nil || !reflect.DeepEqual(got, created) {
		t.Fatalf("duplicate changed record: %+v, %v", got, err)
	}
}

func TestRepositoryConcurrentCreate(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	md5 := key(t)
	type result struct {
		threat domain.Threat
		err    error
	}
	start, results := make(chan struct{}), make(chan result, 2)
	for _, name := range []string{"first", "second"} {
		go func() {
			<-start
			threat, err := repo.Create(ctx, md5, domain.ThreatData{Name: name})
			results <- result{threat, err}
		}()
	}
	close(start)
	var winner domain.Threat
	var successes, conflicts int
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			winner = result.threat
		case errors.Is(result.err, domain.ErrThreatAlreadyExists):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent Create error: %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d, conflicts=%d", successes, conflicts)
	}
	got, err := repo.GetByMD5(ctx, md5)
	if err != nil || !reflect.DeepEqual(got, winner) {
		t.Fatalf("stored row is not the winning insert: %+v, %v", got, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public.threats WHERE md5=$1", string(md5)).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count = %d, %v", count, err)
	}
}

func TestRepositoryUpdate(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	md5 := key(t)
	created, err := repo.Create(ctx, md5, domain.ThreatData{Name: "original", Description: ptr("old")})
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []domain.ThreatData{
		{Name: "renamed", Description: ptr("new")},
		{Name: "", Description: ptr("")},
		{Name: " "},
		{Name: " "}, // Same values must still assign updated_at.
	} {
		// A known old timestamp verifies assignment without timing sleeps.
		if _, err := pool.Exec(ctx, "UPDATE public.threats SET updated_at='2000-01-01T00:00:00Z' WHERE md5=$1", string(md5)); err != nil {
			t.Fatal(err)
		}
		updated, err := repo.Update(ctx, md5, data)
		if err != nil {
			t.Fatal(err)
		}
		requireThreat(t, updated, md5, data)
		if !updated.CreatedAt.Equal(created.CreatedAt) || updated.UpdatedAt.Year() == 2000 {
			t.Fatal("Update changed created_at or did not assign updated_at")
		}
		got, err := repo.GetByMD5(ctx, md5)
		if err != nil || !reflect.DeepEqual(got, updated) {
			t.Fatalf("persisted update differs: %+v, %v", got, err)
		}
	}
}

func TestRepositoryPreservesTechnicalErrors(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	// This deliberately violates the repository's validated-input precondition.
	_, err := repo.Create(ctx, domain.MD5("bad"), domain.ThreatData{Name: "invalid"})
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23514" || errors.Is(err, domain.ErrThreatAlreadyExists) {
		t.Fatalf("expected wrapped CHECK violation, got %v", err)
	}

	// A different unique constraint must not be reported as a duplicate MD5.
	_, err = pool.Exec(ctx, `CREATE UNIQUE INDEX threats_description_test_key
		ON public.threats (description) WHERE description LIKE 'unique-test-marker-%'`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, "DROP INDEX public.threats_description_test_key"); err != nil {
			t.Errorf("drop test index: %v", err)
		}
	})
	data := domain.ThreatData{Name: "same", Description: ptr("unique-test-marker-" + string(key(t)))}
	if _, err := repo.Create(ctx, key(t), data); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Create(ctx, key(t), data)
	if !errors.As(err, &pgError) || pgError.Code != "23505" || pgError.ConstraintName != "threats_description_test_key" || errors.Is(err, domain.ErrThreatAlreadyExists) {
		t.Fatalf("non-MD5 unique violation was misclassified: %v", err)
	}
}

func TestRepositoryClosedPool(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	pool.Close()
	md5 := key(t)
	for name, call := range operations(repo, md5) {
		t.Run(name, func(t *testing.T) {
			_, err := call(ctx)
			if err == nil || errors.Unwrap(err) == nil || errors.Is(err, domain.ErrThreatNotFound) || errors.Is(err, domain.ErrThreatAlreadyExists) {
				t.Fatalf("expected wrapped technical error, got %v", err)
			}
		})
	}
}

func TestRepositoryCanceledContext(t *testing.T) {
	ctx, _, repo := newRepository(t)
	md5 := key(t)
	for name, call := range operations(repo, md5) {
		t.Run(name, func(t *testing.T) {
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := call(canceled); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled operation = %v", err)
			}
			expired, cancel := context.WithDeadline(ctx, time.Unix(1, 0))
			defer cancel()
			if _, err := call(expired); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expired operation = %v", err)
			}
		})
	}
}

func TestRepositoryCancellationDuringUpdate(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	md5 := key(t)
	_, err := repo.Create(ctx, md5, domain.ThreatData{Name: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT md5 FROM public.threats WHERE md5=$1 FOR UPDATE", string(md5)); err != nil {
		t.Fatal(err)
	}
	pending, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := repo.Update(pending, md5, domain.ThreatData{Name: "canceled"})
		result <- err
	}()
	waitForBlockedUpdate(t, ctx, pool)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("in-flight cancellation = %v", err)
		}
	case <-ctx.Done():
		t.Fatal("canceled Update did not terminate")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Cancellation can return before the server observes the disconnected client.
	// Do not infer rollback from a context error after releasing the row lock.
	if _, err := repo.GetByMD5(ctx, md5); err != nil {
		t.Fatalf("pool did not recover after cancellation: %v", err)
	}
}

func TestRepositoryDeadlineWhileWaitingForConnection(t *testing.T) {
	ctx, pool, repo := newRepository(t)
	for range pool.Config().MaxConns {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
	}
	deadline, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if _, err := repo.GetByMD5(deadline, key(t)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool wait deadline = %v", err)
	}
}

func waitForBlockedUpdate(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	waiting, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := pool.QueryRow(waiting, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name='repository-integration' AND wait_event_type='Lock'
			AND query LIKE '%UPDATE public.threats%')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ticker.C:
		case <-waiting.Done():
			t.Fatal("Update did not reach the database lock")
		}
	}
}

func operations(repo *postgres.ThreatRepository, md5 domain.MD5) map[string]func(context.Context) (domain.Threat, error) {
	return map[string]func(context.Context) (domain.Threat, error){
		"get": func(ctx context.Context) (domain.Threat, error) { return repo.GetByMD5(ctx, md5) },
		"create": func(ctx context.Context) (domain.Threat, error) {
			return repo.Create(ctx, md5, domain.ThreatData{Name: "test"})
		},
		"update": func(ctx context.Context) (domain.Threat, error) {
			return repo.Update(ctx, md5, domain.ThreatData{Name: "test"})
		},
	}
}

func ptr(value string) *string { return &value }

func TestRepositoryConcurrentUpdate(t *testing.T) {
	ctx, _, repo := newRepository(t)
	md5 := key(t)
	created, err := repo.Create(ctx, md5, domain.ThreatData{Name: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		data   domain.ThreatData
		threat domain.Threat
		err    error
	}
	start, results := make(chan struct{}), make(chan result, 2)
	for _, data := range []domain.ThreatData{
		{Name: "first", Description: ptr("first description")},
		{Name: "second"},
	} {
		go func() {
			<-start
			updated, err := repo.Update(ctx, md5, data)
			results <- result{data, updated, err}
		}()
	}
	close(start)
	var versions []domain.Threat
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		requireThreat(t, result.threat, md5, result.data)
		if !result.threat.CreatedAt.Equal(created.CreatedAt) {
			t.Fatal("concurrent update changed created_at")
		}
		versions = append(versions, result.threat)
	}
	got, err := repo.GetByMD5(ctx, md5)
	if err != nil || !(reflect.DeepEqual(got, versions[0]) || reflect.DeepEqual(got, versions[1])) {
		t.Fatalf("stored row must match one complete update: %+v, %v", got, err)
	}
}
