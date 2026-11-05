package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthcheckResponses(t *testing.T) {
	for _, tt := range []struct {
		name        string
		status      int
		contentType string
		ready       bool
	}{
		{"existing threat", 200, "application/json", true},
		{"absent route", 404, "application/json", false},
		{"database error", 503, "application/json", false},
		{"wrong method", 405, "application/json", false},
		{"redirect", 307, "text/html", false},
		{"empty implicit success", 200, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/readyz" {
					t.Errorf("unexpected probe: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"status":"ready"}`))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := checkHTTP(ctx, strings.TrimPrefix(server.URL, "http://"))
			if (err == nil) != tt.ready {
				t.Fatalf("probe error = %v, ready = %v", err, tt.ready)
			}
		})
	}
}

func TestHealthcheckWildcardAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"0.0.0.0", ""} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := checkHTTP(ctx, net.JoinHostPort(host, port))
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthcheckDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := checkHTTP(ctx, strings.TrimPrefix(server.URL, "http://")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe error = %v; want deadline", err)
	}
}

func TestHealthcheckInvalidAddress(t *testing.T) {
	if err := checkHTTP(context.Background(), "missing-port"); err == nil {
		t.Fatal("invalid address accepted")
	}
}

func TestHealthcheckInvalidJSON(t *testing.T) {
	for _, body := range []string{`{}`, `{"status":"not ready"}`, `{"status":"ready"} {}`, `{"status":true}`, `{"status":"ready"`, `null`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			if checkHTTP(context.Background(), strings.TrimPrefix(server.URL, "http://")) == nil {
				t.Fatal("invalid probe accepted")
			}
		})
	}
}
