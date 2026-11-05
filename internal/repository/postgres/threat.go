// Package postgres хранит угрозы в PostgreSQL через pgx и sqlc.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"kaspersky-go-test/internal/domain"
	"kaspersky-go-test/internal/repository/postgres/dbgen"
	"kaspersky-go-test/internal/usecase"
)

var _ usecase.ThreatRepository = (*ThreatRepository)(nil)

// ThreatRepository ограничивает каждый запрос общим таймаутом пула и SQL; отмена записи не гарантирует откат, повторов нет.
type ThreatRepository struct {
	queries          *dbgen.Queries
	operationTimeout time.Duration
}

// NewThreatRepository использует чужой пул и вызывает panic при nil-пуле или неположительном таймауте; пул закрывает вызывающий код.
func NewThreatRepository(pool *pgxpool.Pool, operationTimeout time.Duration) *ThreatRepository {
	if pool == nil || operationTimeout <= 0 {
		panic("PostgreSQL repository requires a pool and a positive operation timeout")
	}
	return &ThreatRepository{queries: dbgen.New(pool), operationTimeout: operationTimeout}
}

// GetByMD5 принимает проверенный MD5 и возвращает угрозу с датами в UTC или обёрнутую ErrThreatNotFound.
func (repo *ThreatRepository) GetByMD5(ctx context.Context, md5 domain.MD5) (domain.Threat, error) {
	ctx, cancel := context.WithTimeout(ctx, repo.operationTimeout)
	defer cancel()
	row, err := repo.queries.GetThreat(ctx, string(md5))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrThreatNotFound
		}
		return domain.Threat{}, fmt.Errorf("get threat: %w", err)
	}
	return threatFrom(row), nil
}

// Create вставляет угрозу с датами БД; только конфликт threats_pkey превращается в ErrThreatAlreadyExists.
func (repo *ThreatRepository) Create(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	ctx, cancel := context.WithTimeout(ctx, repo.operationTimeout)
	defer cancel()
	row, err := repo.queries.CreateThreat(ctx, dbgen.CreateThreatParams{
		MD5: string(md5), Name: data.Name, Description: data.Description,
	})
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "23505" && pgError.ConstraintName == "threats_pkey" {
			err = domain.ErrThreatAlreadyExists
		}
		return domain.Threat{}, fmt.Errorf("create threat: %w", err)
	}
	return threatFrom(row), nil
}

// Update заменяет поля и UpdatedAt, сохраняя CreatedAt; отсутствие строки даёт ErrThreatNotFound, отмена не гарантирует откат.
func (repo *ThreatRepository) Update(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	ctx, cancel := context.WithTimeout(ctx, repo.operationTimeout)
	defer cancel()
	row, err := repo.queries.UpdateThreat(ctx, dbgen.UpdateThreatParams{
		MD5: string(md5), Name: data.Name, Description: data.Description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrThreatNotFound
		}
		return domain.Threat{}, fmt.Errorf("update threat: %w", err)
	}
	return threatFrom(row), nil
}

func threatFrom(row dbgen.Threat) domain.Threat {
	return domain.Threat{
		MD5: domain.MD5(row.MD5), Name: row.Name, Description: row.Description,
		CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(),
	}
}
