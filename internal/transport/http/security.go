package httptransport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// SecurityOptions задаёт доступ, лимиты и проверку готовности HTTP-адаптера.
type SecurityOptions struct {
	// APIKey — секрет, сравниваемый с Bearer-токеном; не должен попадать в логи.
	APIKey string
	// RateLimitRPS задаёт число восстанавливаемых токенов в секунду на IP.
	RateLimitRPS int
	// RateLimitBurst задаёт ёмкость bucket и начальный запас токенов на IP.
	RateLimitBurst int
	// RateLimitMaxClients ограничивает число записей limiter, удаляемых после бездействия.
	RateLimitMaxClients int
	// ReadinessTimeout ограничивает вызов Ping для локального GET /readyz.
	ReadinessTimeout time.Duration
	// Ping проверяет доступность БД с переданным контекстом; функция должна быть ненулевой.
	Ping func(context.Context) error
}

func protect(next http.Handler, options SecurityOptions) http.Handler {
	if options.APIKey == "" || options.RateLimitRPS <= 0 || options.RateLimitBurst <= 0 || options.RateLimitMaxClients <= 0 || options.ReadinessTimeout <= 0 || options.Ping == nil {
		panic("HTTP security requires key, positive limits, readiness timeout and ping")
	}
	limiter := newIPLimiter(options.RateLimitRPS, options.RateLimitBurst, options.RateLimitMaxClients)
	expected := sha256.Sum256([]byte(options.APIKey))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil {
			respond(w, http.StatusBadRequest, ErrorResponse{Message: "Invalid request"})
			return
		}
		ip := address.Addr().Unmap()
		// Только прямое loopback-соединение получает доступ к readiness без ключа и лимита.
		// Proxy-заголовки не подтверждают локальность клиента.
		if r.URL.Path == "/readyz" {
			if !ip.IsLoopback() {
				respond(w, http.StatusNotFound, ErrorResponse{Message: "Route not found"})
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				respond(w, http.StatusMethodNotAllowed, ErrorResponse{Message: "Method not allowed"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), options.ReadinessTimeout)
			defer cancel()
			if err := options.Ping(ctx); err != nil {
				respond(w, http.StatusServiceUnavailable, ErrorResponse{Message: "Service unavailable"})
				return
			}
			respond(w, http.StatusOK, ReadinessResponse{Status: "ready"})
			return
		}
		if retry := limiter.allow(ip); retry > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			respond(w, http.StatusTooManyRequests, ErrorResponse{Message: "Too many requests"})
			return
		}
		values := r.Header.Values("Authorization")
		valid := false
		if len(values) == 1 {
			scheme, token, found := strings.Cut(values[0], " ")
			if found && strings.EqualFold(scheme, "Bearer") && token != "" && !strings.ContainsAny(token, " \t\r\n,") {
				actual := sha256.Sum256([]byte(token))
				valid = subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
			}
		}
		if !valid {
			w.Header().Set("WWW-Authenticate", "Bearer")
			respond(w, http.StatusUnauthorized, ErrorResponse{Message: "Unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
