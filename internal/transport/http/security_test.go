package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func securityHandler(options SecurityOptions, log io.Writer, next http.Handler) http.Handler {
	return requestLogging(logger(log), recoverPanics(protect(next, options)))
}

func TestBearerAuthentication(t *testing.T) {
	for _, tt := range []struct {
		name    string
		headers []string
		status  int
	}{
		{"valid", []string{"Bearer " + testAPIKey}, 204},
		{"case insensitive scheme", []string{"bEaReR " + testAPIKey}, 204},
		{"missing", nil, 401}, {"empty", []string{""}, 401},
		{"wrong key", []string{"Bearer " + strings.Repeat("b", 32)}, 401},
		{"wrong case token", []string{"Bearer " + strings.ToUpper(testAPIKey)}, 401},
		{"wrong scheme", []string{"Basic " + testAPIKey}, 401},
		{"two values", []string{"Bearer " + testAPIKey, "Bearer " + testAPIKey}, 401},
		{"comma joined", []string{"Bearer " + testAPIKey + ", Bearer " + testAPIKey}, 401},
		{"extra spaces", []string{"Bearer  " + testAPIKey}, 401},
		{"tab", []string{"Bearer\t" + testAPIKey}, 401},
		{"empty token", []string{"Bearer "}, 401},
		{"trailing space", []string{"Bearer " + testAPIKey + " "}, 401},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			called := false
			h := securityHandler(testSecurity(), &logs, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
			r := request("DELETE", "/missing", "")
			r.Header.Del("Authorization")
			for _, value := range tt.headers {
				r.Header.Add("Authorization", value)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.status || called != (tt.status == 204) {
				t.Fatalf("status %d, called %v", w.Code, called)
			}
			if tt.status == 401 {
				requireErrorResponse(t, w, 401)
				if w.Header().Get("WWW-Authenticate") != "Bearer" || !equalError(w, "Unauthorized") {
					t.Fatal("invalid authentication response")
				}
			}
			if strings.Contains(logs.String(), testAPIKey) || strings.Contains(logs.String(), "Authorization") {
				t.Fatal("credentials leaked")
			}
		})
	}
}

func TestSecurityOrderingAndClientIP(t *testing.T) {
	options := testSecurity()
	options.RateLimitRPS = 1
	options.RateLimitBurst = 1
	h := securityHandler(options, io.Discard, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	run := func(remote, forwarded string, authorized bool) *httptest.ResponseRecorder {
		r := request("GET", "/missing", "")
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", forwarded)
		r.Header.Set("Forwarded", "for="+forwarded)
		if !authorized {
			r.Header.Del("Authorization")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := run("192.0.2.1:1000", "127.0.0.1", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	// Неавторизованный запрос расходует тот же bucket, что и IPv4-mapped адрес.
	if w := run("[::ffff:192.0.2.1]:2000", "203.0.113.1", true); w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("mapped IP bypass: %d", w.Code)
	}
	if w := run("[2001:db8::1]:1234", "192.0.2.1", true); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := run("192.0.2.2:1234", "192.0.2.1", true); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := run("invalid", "127.0.0.1", true); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestIPLimiterRefillAndCleanup(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newIPLimiter(2, 2, 2)
	l.now = func() time.Time { return now }
	a := netip.MustParseAddr("192.0.2.1")
	b := netip.MustParseAddr("192.0.2.2")
	c := netip.MustParseAddr("192.0.2.3")
	for _, want := range []int{0, 0, 1} {
		if got := l.allow(a); got != want {
			t.Fatalf("burst retry = %d, want %d", got, want)
		}
	}
	now = now.Add(250 * time.Millisecond)
	if l.allow(a) != 1 {
		t.Fatal("fractional token admitted")
	}
	now = now.Add(250 * time.Millisecond)
	if l.allow(a) != 0 || l.allow(a) != 1 {
		t.Fatal("token refill incorrect")
	}
	if l.allow(b) != 0 || l.allow(c) != 60 || len(l.clients) != 2 {
		t.Fatal("capacity not enforced")
	}
	now = now.Add(clientIdleTTL - time.Second)
	if l.allow(a) != 0 {
		t.Fatal("burst did not refill")
	}
	now = now.Add(time.Second)
	// На заполненной таблице принудительная очистка работает между плановыми проходами.
	if l.allow(c) != 0 || len(l.clients) != 2 {
		t.Fatal("idle entry not reclaimed at capacity")
	}
	if _, exists := l.clients[b]; exists {
		t.Fatal("idle client retained")
	}
	now = now.Add(clientIdleTTL)
	if l.allow(b) != 0 || len(l.clients) != 1 {
		t.Fatal("periodic cleanup failed")
	}
}

func TestIPLimiterConcurrent(t *testing.T) {
	l := newIPLimiter(10, 20, 10)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if l.allow(netip.MustParseAddr("192.0.2.1")) == 0 {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 20 || len(l.clients) != 1 {
		t.Fatalf("allowed %d", allowed.Load())
	}
}

func TestReadinessLocalOnlyAndIndependentOfLimiter(t *testing.T) {
	options := testSecurity()
	options.RateLimitBurst = 1
	calls := 0
	options.Ping = func(context.Context) error { calls++; return nil }
	h := securityHandler(options, io.Discard, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, remote := range []string{"127.0.0.1:1234", "[::1]:1234", "[::ffff:127.0.0.1]:1234"} {
		r := request("GET", "/missing", "")
		r.RemoteAddr = remote
		h.ServeHTTP(httptest.NewRecorder(), r)
		h.ServeHTTP(httptest.NewRecorder(), r)
		r = request("GET", "/readyz", "")
		r.RemoteAddr = remote
		r.Header.Del("Authorization")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != `{"status":"ready"}` {
			t.Fatalf("local readiness %d: %s", w.Code, w.Body)
		}
	}
	if calls != 3 {
		t.Fatal("ping not called")
	}
	for _, method := range []string{"GET", "POST", "HEAD"} {
		r := request(method, "/readyz", "")
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		requireErrorResponse(t, w, 404)
	}
	if calls != 3 {
		t.Fatal("external readiness queried DB")
	}
	r := request("POST", "/readyz", "")
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireErrorResponse(t, w, 405)
	if w.Header().Get("Allow") != "GET" {
		t.Fatal("missing Allow")
	}
}

func TestReadinessFailureAndDeadline(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		options := testSecurity()
		options.ReadinessTimeout = 20 * time.Millisecond
		options.Ping = func(ctx context.Context) error {
			if !timeout {
				return errors.New("private database error")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > options.ReadinessTimeout {
				t.Error("missing readiness deadline")
			}
			<-ctx.Done()
			return ctx.Err()
		}
		h := securityHandler(options, io.Discard, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected router call") }))
		r := request("GET", "/readyz", "")
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		requireErrorResponse(t, w, 503)
		if !equalError(w, "Service unavailable") {
			t.Fatal("internal error leaked")
		}
	}
}

func equalError(w *httptest.ResponseRecorder, message string) bool {
	var body struct {
		Message string `json:"message"`
		Details string `json:"details"`
	}
	return json.Unmarshal(w.Body.Bytes(), &body) == nil && body.Message == message && body.Details == ""
}

func TestSecurityConstructorPreconditions(t *testing.T) {
	for _, change := range []func(*SecurityOptions){
		func(o *SecurityOptions) { o.APIKey = "" },
		func(o *SecurityOptions) { o.RateLimitRPS = 0 },
		func(o *SecurityOptions) { o.RateLimitBurst = 0 },
		func(o *SecurityOptions) { o.RateLimitMaxClients = 0 },
		func(o *SecurityOptions) { o.ReadinessTimeout = 0 },
		func(o *SecurityOptions) { o.Ping = nil },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid security configuration accepted")
				}
			}()
			options := testSecurity()
			change(&options)
			_ = NewHandler(noService(t), logger(io.Discard), 4096, options)
		}()
	}
}
