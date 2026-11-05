// Package httptransport обслуживает REST API по контракту OpenAPI.
package httptransport

import (
	"context"
	"log/slog"
	"net/http"

	"kaspersky-go-test/internal/domain"
)

// ThreatService задаёт операции для HTTP-обработчиков; предметные ошибки преобразует в HTTP-ответы транспорт.
type ThreatService interface {
	// GetByMD5 находит угрозу по MD5, принятому в HTTP-пути.
	GetByMD5(context.Context, string) (domain.Threat, error)
	// Create сохраняет угрозу после предметной валидации.
	Create(context.Context, string, domain.ThreatData) (domain.Threat, error)
	// Update заменяет изменяемые поля существующей угрозы.
	Update(context.Context, string, domain.ThreatData) (domain.Threat, error)
}

type handler struct {
	service      ThreatService
	maxBodyBytes int64
}

// NewHandler собирает маршруты и защиту API; вызывает panic при nil-зависимостях, пустом ключе или неположительных лимитах и таймауте readiness.
func NewHandler(service ThreatService, logger *slog.Logger, maxBodyBytes int64, security SecurityOptions) http.Handler {
	if service == nil || logger == nil || maxBodyBytes <= 0 {
		panic("HTTP handler requires service, logger, and a positive body limit")
	}
	h := &handler{service: service, maxBodyBytes: maxBodyBytes}
	return requestLogging(logger, recoverPanics(protect(h.router(), security)))
}

func (h *handler) ready(w http.ResponseWriter, r *http.Request, operation string) bool {
	if err := r.Context().Err(); err != nil {
		h.serviceError(w, r, operation, err)
		return false
	}
	return true
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w, r, "get") {
		return
	}
	threat, err := h.service.GetByMD5(r.Context(), r.PathValue("md5"))
	if err != nil {
		h.serviceError(w, r, "get", err)
		return
	}
	respond(w, http.StatusOK, responseFrom(threat))
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w, r, "create") {
		return
	}
	object, err := decodeObject(w, r, h.maxBodyBytes)
	if err != nil {
		h.invalidRequest(w, r, "create", err)
		return
	}
	input, err := decodeCreate(object)
	if err != nil {
		h.invalidRequest(w, r, "create", err)
		return
	}
	threat, err := h.service.Create(r.Context(), input.MD5, domain.ThreatData{Name: input.Name, Description: input.Description})
	if err != nil {
		h.serviceError(w, r, "create", err)
		return
	}
	respond(w, http.StatusCreated, responseFrom(threat))
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w, r, "update") {
		return
	}
	object, err := decodeObject(w, r, h.maxBodyBytes)
	if err != nil {
		h.invalidRequest(w, r, "update", err)
		return
	}
	input, err := decodeUpdate(object)
	if err != nil {
		h.invalidRequest(w, r, "update", err)
		return
	}
	threat, err := h.service.Update(r.Context(), r.PathValue("md5"), domain.ThreatData{Name: input.Name, Description: input.Description})
	if err != nil {
		h.serviceError(w, r, "update", err)
		return
	}
	respond(w, http.StatusOK, responseFrom(threat))
}
