//go:build integration

package database_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kaspersky-go-test/tests/testutil"
)

func TestMigrationsAndConstraints(t *testing.T) {
	db := testutil.StartDatabase(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for range 2 {
		if err := db.Migrate(ctx, "up"); err != nil {
			t.Fatal(err)
		}
	}
	pool := db.Pool(t)
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, "SELECT version,dirty FROM public.schema_migrations").Scan(&version, &dirty); err != nil || version != 1 || dirty {
		t.Fatalf("unexpected migration state: version=%d dirty=%v err=%v", version, dirty, err)
	}
	var server string
	if err := pool.QueryRow(ctx, "SHOW server_version").Scan(&server); err != nil || !strings.HasPrefix(server, "18.6") {
		t.Fatal("wrong PostgreSQL version")
	}
	script, err := os.ReadFile(filepath.Join(testutil.Root(), "tests/integration/schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(script)); err != nil {
		t.Fatal("schema constraints:", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO public.threats (md5,name) VALUES ('d41d8cd98f00b204e9800998ecf8427e','before rollback')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE public.schema_migrations SET dirty=true"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, "up"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "dirty") {
		t.Fatal("dirty migration state accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE public.schema_migrations SET dirty=false"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, "down", "1"); err != nil {
		t.Fatal(err)
	}
	var missing bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.threats') IS NULL").Scan(&missing); err != nil || !missing {
		t.Fatal("rollback retained table")
	}
	if err := db.Migrate(ctx, "up"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM public.threats").Scan(&count); err != nil || count != 0 {
		t.Fatal("reapply did not create an empty table")
	}
}
