//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httptransport "kaspersky-go-test/internal/transport/http"
	"kaspersky-go-test/internal/usecase"
)

func TestHTTPWithPostgres(t *testing.T) {
	ctx, pool, repository := newRepository(t)
	service := usecase.NewThreatService(repository)
	server := httptest.NewServer(httptransport.NewHandler(service, slog.New(slog.NewJSONHandler(io.Discard, nil)), 4096, integrationSecurity(pool.Ping)))
	defer server.Close()
	do := func(method, path, body string, status int) map[string]json.RawMessage {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if method == "POST" || method == "PUT" {
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		r.Header.Set("Authorization", "Bearer "+integrationAPIKey)
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		encoded, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != status || response.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("%s %s: %d, %s; want %d JSON", method, path, response.StatusCode, encoded, status)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if status >= 400 {
			for _, field := range []string{"message", "details"} {
				var value string
				if raw, ok := result[field]; !ok || string(raw) == "null" || json.Unmarshal(raw, &value) != nil {
					t.Fatalf("missing error string %s: %s", field, encoded)
				}
			}
		}
		return result
	}
	md5 := string(key(t))
	upper := strings.ToUpper(md5)
	created := do("POST", "/info", `{"md5":"`+upper+`","name":"Trojan","description":"old","created_at":false}`, 201)
	if string(created["md5"]) != `"`+md5+`"` {
		t.Fatalf("hash not canonical: %s", created["md5"])
	}
	for _, field := range []string{"created_at", "updated_at"} {
		var timestamp string
		if err := json.Unmarshal(created[field], &timestamp); err != nil || !strings.HasSuffix(timestamp, "Z") {
			t.Fatalf("timestamp missing/not UTC: %s", created[field])
		}
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	got := do("GET", "/info/"+upper, "", 200)
	if string(got["description"]) != `"old"` {
		t.Fatal("description not persisted")
	}
	do("POST", "/info", `{"md5":"`+md5+`","name":"duplicate"}`, 409)
	got = do("GET", "/info/"+md5, "", 200)
	if string(got["name"]) != `"Trojan"` {
		t.Fatal("duplicate overwrote record")
	}
	updated := do("PUT", "/info/"+upper, `{"name":"updated","md5":null,"updated_at":[]}`, 200)
	if _, ok := updated["description"]; ok {
		t.Fatal("nil description must be omitted")
	}
	if string(updated["created_at"]) != string(created["created_at"]) {
		t.Fatal("update changed creation time")
	}
	got = do("GET", "/info/"+md5, "", 200)
	if _, ok := got["description"]; ok {
		t.Fatal("description was not cleared in storage")
	}
	updated = do("PUT", "/info/"+md5, `{"name":"","description":""}`, 200)
	if string(updated["name"]) != `""` || string(updated["description"]) != `""` {
		t.Fatal("empty strings lost")
	}

	absent := string(key(t))
	do("GET", "/info/"+absent, "", 404)
	do("PUT", "/info/"+absent, `{"name":"missing"}`, 404)
	do("GET", "/info/"+absent, "", 404)
	do("GET", "/info/bad", "", 400)
	do("POST", "/info", `{"md5":"bad","name":""}`, 400)
	do("POST", "/info", `{"md5":"`+md5+`","name":null}`, 400)
	do("PUT", "/info/"+md5, `{"name":"bad\u0000name"}`, 400)
	do("PUT", "/info/"+md5, `{"name":"ok","description":null}`, 400)
	do("POST", "/info", `{`, 400)
	pool.Close()
	failure := do("GET", "/info/"+md5, "", 500)
	if string(failure["message"]) != `"Internal server error"` || string(failure["details"]) != `""` {
		t.Fatalf("internal details exposed: %v", failure)
	}
}

const integrationAPIKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func integrationSecurity(ping func(context.Context) error) httptransport.SecurityOptions {
	return httptransport.SecurityOptions{APIKey: integrationAPIKey, RateLimitRPS: 10000, RateLimitBurst: 10000, RateLimitMaxClients: 100, ReadinessTimeout: time.Second, Ping: ping}
}

func TestHTTPSecurityWithPostgres(t *testing.T) {
	_, pool, repository := newRepository(t)
	options := integrationSecurity(pool.Ping)
	options.RateLimitRPS = 1
	options.RateLimitBurst = 2
	server := httptest.NewServer(httptransport.NewHandler(usecase.NewThreatService(repository), slog.New(slog.NewJSONHandler(io.Discard, nil)), 4096, options))
	defer server.Close()
	md5 := string(key(t))
	send := func(method, path, body, token string) (int, http.Header, map[string]any) {
		t.Helper()
		r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header, result
	}
	if status, headers, body := send("GET", "/missing", "", ""); status != 401 || headers.Get("WWW-Authenticate") != "Bearer" || body["message"] != "Unauthorized" || body["details"] != "" {
		t.Fatal("missing authorization not rejected")
	}
	if status, _, _ := send("POST", "/info", `{"md5":"`+md5+`","name":"secured"}`, integrationAPIKey); status != 201 {
		t.Fatalf("create: %d", status)
	}
	if status, headers, body := send("GET", "/info/"+md5, "", integrationAPIKey); status != 429 || headers.Get("Retry-After") != "1" || body["message"] != "Too many requests" || body["details"] != "" {
		t.Fatal("rate limit contract violated")
	}
	if status, _, body := send("GET", "/readyz", "", ""); status != 200 || body["status"] != "ready" {
		t.Fatal("readiness affected by limiting")
	}
	// Ожидаем реальное восстановление токена, не предполагая точность планировщика.
	waitFor := func(method, path, body string) map[string]any {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			status, _, result := send(method, path, body, integrationAPIKey)
			if status == 200 {
				return result
			}
			if status != 429 || time.Now().After(deadline) {
				t.Fatalf("request did not recover: %d", status)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if result := waitFor("GET", "/info/"+md5, ""); result["name"] != "secured" {
		t.Fatal("data lost")
	}
	if result := waitFor("PUT", "/info/"+md5, `{"name":"updated"}`); result["name"] != "updated" {
		t.Fatal("update failed")
	}
	pool.Close()
	if status, _, body := send("GET", "/readyz", "", ""); status != 503 || body["message"] != "Service unavailable" || body["details"] != "" {
		t.Fatal("unavailable DB reported ready")
	}
}
