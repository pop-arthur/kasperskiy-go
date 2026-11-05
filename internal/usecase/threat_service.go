package usecase

import (
	"context"

	"kasperski-go-test/internal/domain"
)

// ThreatService validates input and coordinates the three threat operations.
// It holds no request state and can be shared by concurrent callers when its
// repository is safe for concurrent use.
type ThreatService struct {
	repository ThreatRepository
}

// NewThreatService requires a non-nil repository and performs no I/O.
func NewThreatService(repository ThreatRepository) *ThreatService {
	return &ThreatService{repository: repository}
}

func (service *ThreatService) GetByMD5(ctx context.Context, md5 string) (domain.Threat, error) {
	if err := ctx.Err(); err != nil {
		return domain.Threat{}, err
	}
	canonical, err := domain.ParseMD5(md5)
	if err != nil {
		return domain.Threat{}, err
	}
	threat, err := service.repository.GetByMD5(ctx, canonical)
	if err != nil {
		return domain.Threat{}, err
	}
	return threat, nil
}

func (service *ThreatService) Create(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error) {
	if err := ctx.Err(); err != nil {
		return domain.Threat{}, err
	}
	canonical, err := domain.ParseMD5(md5)
	if err != nil {
		return domain.Threat{}, err
	}
	if err := data.Validate(); err != nil {
		return domain.Threat{}, err
	}
	threat, err := service.repository.Create(ctx, canonical, data)
	if err != nil {
		return domain.Threat{}, err
	}
	return threat, nil
}

func (service *ThreatService) Update(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error) {
	if err := ctx.Err(); err != nil {
		return domain.Threat{}, err
	}
	canonical, err := domain.ParseMD5(md5)
	if err != nil {
		return domain.Threat{}, err
	}
	if err := data.Validate(); err != nil {
		return domain.Threat{}, err
	}
	threat, err := service.repository.Update(ctx, canonical, data)
	if err != nil {
		return domain.Threat{}, err
	}
	return threat, nil
}
