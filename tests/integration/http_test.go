//go:build integration

package integration_test

import (
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
	server := httptest.NewServer(httptransport.NewHandler(service, slog.New(slog.NewJSONHandler(io.Discard, nil)), 4096))
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
