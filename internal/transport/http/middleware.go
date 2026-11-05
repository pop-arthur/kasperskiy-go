package httptransport

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type responseWriter struct {
	http.ResponseWriter
	status  int
	failure error
	stack   []byte
	aborted bool
}

// Unwrap предоставляет исходный writer для http.ResponseController.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// WriteHeader запоминает первый итоговый статус для единого access-log.
func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write учитывает неявный статус 200 при записи тела ответа.
func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) recordFailure(err error) {
	if w.failure == nil {
		w.failure = err
	}
}

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		state := &responseWriter{ResponseWriter: w}
		defer func() {
			if state.failure != nil {
				attrs := []any{"method", r.Method, "route", r.Pattern, "error", state.failure}
				if len(state.stack) > 0 {
					attrs = append(attrs, "stack", string(state.stack))
				}
				logger.ErrorContext(r.Context(), "HTTP request failed", attrs...)
			}
			attrs := []any{"method", r.Method, "route", r.Pattern, "duration", time.Since(start)}
			if state.status != 0 {
				attrs = append(attrs, "status", state.status)
			}
			if state.aborted {
				attrs = append(attrs, "aborted", true)
			}
			logger.InfoContext(r.Context(), "HTTP request completed", attrs...)
		}()
		next.ServeHTTP(state, r)
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					w.(*responseWriter).aborted = true
					panic(value)
				}
				state := w.(*responseWriter)
				state.recordFailure(fmt.Errorf("panic in HTTP handler (%T)", value))
				state.stack = debug.Stack()
				if state.status != 0 {
					state.aborted = true
					panic(http.ErrAbortHandler)
				}
				respond(w, http.StatusInternalServerError, ErrorResponse{Message: "Internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
