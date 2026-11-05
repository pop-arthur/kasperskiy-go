// Package usecase описывает операции над угрозами и контракт хранилища.
package usecase

import (
	"context"

	"kaspersky-go-test/internal/domain"
)

// ThreatRepository принимает проверенные данные; отсутствие записи и дубликат возвращаются как ошибки domain, технические причины сохраняются для errors.Is/As.
type ThreatRepository interface {
	// GetByMD5 возвращает запись или domain.ErrThreatNotFound.
	GetByMD5(ctx context.Context, md5 domain.MD5) (domain.Threat, error)
	// Create добавляет угрозу с датами хранилища; существующий MD5 даёт ErrThreatAlreadyExists без перезаписи.
	Create(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error)
	// Update заменяет Name и Description, сохраняя MD5 и CreatedAt; nil удаляет описание, отсутствующая запись не создаётся.
	Update(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error)
}
