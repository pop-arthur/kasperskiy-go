package usecase

import (
	"context"

	"kaspersky-go-test/internal/domain"
)

// ThreatRepository persists validated data. The caller must parse the MD5 and
// validate ThreatData before calling it. Each write returns the persisted row.
type ThreatRepository interface {
	GetByMD5(ctx context.Context, md5 domain.MD5) (domain.Threat, error)
	Create(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error)
	Update(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error)
}
