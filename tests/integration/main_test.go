//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"kaspersky-go-test/tests/testutil"
)

var suiteDSN string

func TestMain(m *testing.M) { os.Exit(runSuite(m)) }

func runSuite(m *testing.M) (code int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := testutil.NewDatabase(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}()
	if err := db.Migrate(ctx, "up"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	suiteDSN = db.DSN
	return m.Run()
}
