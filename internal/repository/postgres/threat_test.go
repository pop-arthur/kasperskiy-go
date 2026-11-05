package postgres_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"kaspersky-go-test/internal/repository/postgres"
)

func TestNewThreatRepositoryRejectsInvalidConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		pool    *pgxpool.Pool
		timeout time.Duration
	}{
		{"nil pool", nil, time.Second},
		{"zero timeout", &pgxpool.Pool{}, 0},
		{"negative timeout", &pgxpool.Pool{}, -time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("constructor accepted invalid configuration")
				}
			}()
			postgres.NewThreatRepository(tt.pool, tt.timeout)
		})
	}
}
