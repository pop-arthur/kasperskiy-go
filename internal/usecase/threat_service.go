package usecase

import (
	"context"

	"kaspersky-go-test/internal/domain"
)

// ThreatService проверяет входные данные и вызывает хранилище; поздняя отмена контекста не меняет успешный результат.
type ThreatService struct {
	repository ThreatRepository
}

// NewThreatService создаёт сервис; repository должен быть ненулевым.
func NewThreatService(repository ThreatRepository) *ThreatService {
	return &ThreatService{repository: repository}
}

// GetByMD5 проверяет MD5, приводит его к нижнему регистру и запрашивает угрозу в хранилище.
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

// Create проверяет MD5 и поля угрозы перед записью; существующий хеш даёт ErrThreatAlreadyExists.
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

// Update проверяет данные и заменяет поля существующей угрозы; nil в Description удаляет описание.
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
