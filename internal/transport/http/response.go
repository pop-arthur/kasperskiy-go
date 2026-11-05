package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"kasperski-go-test/internal/domain"
)

// writeJSON encodes before committing headers, so encoding failures can become 500.
func writeJSON(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	written, err := w.Write(body)
	if err == nil && written != len(body) {
		return io.ErrShortWrite
	}
	return err
}

func respond(w http.ResponseWriter, status int, value any) {
	if err := writeJSON(w, status, value); err != nil {
		state := w.(*responseWriter)
		state.recordFailure(err)
		if state.status == 0 {
			// A fixed error DTO always encodes; never retry a partially written response.
			if err := writeJSON(w, http.StatusInternalServerError, errorResponse{Message: "Internal server error"}); err != nil {
				state.recordFailure(err)
			}
		}
	}
}

func (h *handler) invalidRequest(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if !h.ready(w, r, operation) {
		return
	}
	// Decoder errors contain only controlled rules/field names, never user values.
	respond(w, http.StatusBadRequest, errorResponse{Message: "Invalid request", Details: err.Error()})
}

func (h *handler) serviceError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	state := w.(*responseWriter)
	if errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled) {
		state.aborted = true
		return
	}
	status := http.StatusInternalServerError
	body := errorResponse{Message: "Internal server error"}
	switch {
	case errors.Is(err, domain.ErrInvalidMD5):
		status = http.StatusBadRequest
		body = errorResponse{Message: "Invalid MD5", Details: "MD5 must contain exactly 32 hexadecimal characters"}
	case errors.Is(err, domain.ErrInvalidThreatData):
		status = http.StatusBadRequest
		body = errorResponse{Message: "Invalid threat data", Details: "Invalid value in a known field"}
	case errors.Is(err, domain.ErrThreatNotFound) && (operation == "get" || operation == "update"):
		status = http.StatusNotFound
		body.Message = "Threat not found"
	case errors.Is(err, domain.ErrThreatAlreadyExists) && operation == "create":
		status = http.StatusConflict
		body.Message = "Threat already exists"
	}
	if status == http.StatusInternalServerError {
		state.recordFailure(err)
	}
	respond(w, status, body)
}
