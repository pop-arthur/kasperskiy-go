// Package postgres implements persistence using PostgreSQL and pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"kaspersky-go-test/internal/domain"
)

// ThreatRepository uses a caller-owned pool. It is safe for concurrent use.
type ThreatRepository struct {
	pool *pgxpool.Pool
}

// NewThreatRepository requires a non-nil pool. The caller owns its configuration,
// connectivity checks, and shutdown; constructing a repository performs no I/O.
func NewThreatRepository(pool *pgxpool.Pool) *ThreatRepository {
	return &ThreatRepository{pool: pool}
}

func (repo *ThreatRepository) GetByMD5(ctx context.Context, md5 domain.MD5) (domain.Threat, error) {
	const query = `
		SELECT md5, name, description, created_at, updated_at
		FROM public.threats
		WHERE md5 = $1`
	threat, err := scanThreat(repo.pool.QueryRow(ctx, query, string(md5)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrThreatNotFound
		}
		return domain.Threat{}, fmt.Errorf("get threat: %w", err)
	}
	return threat, nil
}

func (repo *ThreatRepository) Create(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	const query = `
		INSERT INTO public.threats (md5, name, description)
		VALUES ($1, $2, $3)
		RETURNING md5, name, description, created_at, updated_at`
	threat, err := scanThreat(repo.pool.QueryRow(ctx, query, string(md5), data.Name, data.Description))
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "threats_pkey" {
			err = domain.ErrThreatAlreadyExists
		}
		return domain.Threat{}, fmt.Errorf("create threat: %w", err)
	}
	return threat, nil
}

func (repo *ThreatRepository) Update(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	const query = `
		UPDATE public.threats
		SET name = $2, description = $3, updated_at = CURRENT_TIMESTAMP
		WHERE md5 = $1
		RETURNING md5, name, description, created_at, updated_at`
	threat, err := scanThreat(repo.pool.QueryRow(ctx, query, string(md5), data.Name, data.Description))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrThreatNotFound
		}
		return domain.Threat{}, fmt.Errorf("update threat: %w", err)
	}
	return threat, nil
}

func scanThreat(row pgx.Row) (domain.Threat, error) {
	var threat domain.Threat
	err := row.Scan(&threat.MD5, &threat.Name, &threat.Description, &threat.CreatedAt, &threat.UpdatedAt)
	if err != nil {
		return domain.Threat{}, err
	}
	threat.CreatedAt = threat.CreatedAt.UTC()
	threat.UpdatedAt = threat.UpdatedAt.UTC()
	return threat, nil
}
