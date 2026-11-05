//go:build e2e

package runtime_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"kaspersky-go-test/tests/testutil"
)

func TestAPIProcess(t *testing.T) {
	binary := testutil.BuildAPI(t)
	db := testutil.StartDatabase(t, false)
	env := testutil.AppEnv(db)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Отсутствующая схема больше не блокирует startup; readiness проверяет только Ping.
	t.Run("without migrations", func(t *testing.T) {
		p := testutil.StartAPI(t, binary, env)
		base := p.Listening(t)
		testutil.Probe(t, binary, base, env)
		body := testutil.JSONRequest(t, base, "GET", "/info/"+strings.Repeat("0", 32), "", env["API_KEY"], 500)
		if body["message"] != "Internal server error" || body["details"] != "" {
			t.Fatal("SQL details leaked")
		}
		_ = p.Cmd.Process.Signal(syscall.SIGTERM)
		if p.Wait(t) != 0 {
			t.Fatal("shutdown failed")
		}
	})
	if err := db.Migrate(ctx, "up"); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t)
	for _, tt := range []struct{ name, change, restore string }{
		{"dirty metadata", "UPDATE public.schema_migrations SET dirty=true", "UPDATE public.schema_migrations SET dirty=false"},
		{"other migration version", "UPDATE public.schema_migrations SET version=2", "UPDATE public.schema_migrations SET version=1"},
		{"missing table", "ALTER TABLE public.threats RENAME TO threats_hidden", "ALTER TABLE public.threats_hidden RENAME TO threats"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, tt.change); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := pool.Exec(ctx, tt.restore); err != nil {
					t.Error(err)
				}
			}()
			p := testutil.StartAPI(t, binary, env)
			base := p.Listening(t)
			testutil.Probe(t, binary, base, env)
			status := 404
			if tt.name == "missing table" {
				status = 500
			}
			testutil.JSONRequest(t, base, "GET", "/info/"+strings.Repeat("0", 32), "", env["API_KEY"], status)
			_ = p.Cmd.Process.Signal(syscall.SIGTERM)
			if p.Wait(t) != 0 {
				t.Fatal("shutdown failed")
			}
		})
	}
	t.Run("startup errors", func(t *testing.T) {
		occupied, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer occupied.Close()
		_, port, _ := net.SplitHostPort(occupied.Addr().String())
		testutil.ExpectStartupFailure(t, binary, testutil.Override(env, map[string]string{"HTTP_ADDR": occupied.Addr().String()}), "listen HTTP")
		for _, level := range []string{"INFO", "ERROR+1"} {
			testutil.ExpectStartupFailure(t, binary, testutil.Override(env, map[string]string{"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": port, "POSTGRES_CONNECT_TIMEOUT": "150ms", "LOG_LEVEL": level}), "connect to PostgreSQL")
		}
		for name, value := range map[string]string{"HTTP_ADDR": "invalid", "POSTGRES_OPERATION_TIMEOUT": "0s", "API_KEY": "", "HTTP_RATE_LIMIT_RPS": "0"} {
			testutil.ExpectStartupFailure(t, binary, testutil.Override(env, map[string]string{name: value}), name)
		}
	})
	t.Run("HTTP and signals", func(t *testing.T) {
		for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
			p := testutil.StartAPI(t, binary, env)
			base := p.Listening(t)
			testutil.Probe(t, binary, base, env)
			key := env["API_KEY"]
			md5 := testutil.RandomID()
			testutil.JSONRequest(t, base, "GET", "/missing", "", "", 401)
			testutil.JSONRequest(t, base, "GET", "/missing", "", "invalid", 401)
			created := testutil.JSONRequest(t, base, "POST", "/info", `{"md5":"`+strings.ToUpper(md5)+`","name":"binary","description":"old"}`, key, 201)
			if created["md5"] != md5 {
				t.Fatal("MD5 not normalized")
			}
			if got := testutil.JSONRequest(t, base, "GET", "/info/"+md5, "", key, 200); got["name"] != "binary" {
				t.Fatal("get failed")
			}
			if got := testutil.JSONRequest(t, base, "PUT", "/info/"+md5, `{"name":"updated"}`, key, 200); got["description"] != nil {
				t.Fatal("description not cleared")
			}
			if err := p.Cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if p.Wait(t) != 0 {
				t.Fatal("signal did not exit normally")
			}
			if p.RawLogsContain(key) || p.RawLogsContain(env["POSTGRES_PASSWORD"]) {
				t.Fatal("credentials leaked")
			}
			if conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), 200*time.Millisecond); err == nil {
				conn.Close()
				t.Fatal("port not released")
			}
		}
	})
	t.Run("incomplete bodies", func(t *testing.T) {
		p := testutil.StartAPI(t, binary, testutil.Override(env, map[string]string{"HTTP_READ_TIMEOUT": "200ms"}))
		base := p.Listening(t)
		md5 := testutil.RandomID()
		key := env["API_KEY"]
		created := testutil.JSONRequest(t, base, "POST", "/info", `{"md5":"`+md5+`","name":"unchanged"}`, key, 201)
		for _, method := range []string{"POST", "PUT"} {
			for _, halfClose := range []bool{false, true} {
				candidate := testutil.RandomID()
				body := `{"md5":"` + candidate + `","name":"must-not-be-written"}`
				path := "/info"
				if method == "PUT" {
					path += "/" + md5
				}
				conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					conn.Close()
					t.Fatal(err)
				}
				_, err = fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\nAuthorization: Bearer %s\r\nContent-Length: %d\r\n\r\n%s", method, path, key, len(body)+20, body)
				if err != nil {
					conn.Close()
					t.Fatal(err)
				}
				if halfClose {
					if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
						conn.Close()
						t.Fatal(err)
					}
				}
				data, err := io.ReadAll(conn)
				conn.Close()
				if len(data) != 0 {
					t.Fatal("incomplete body received response")
				}
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("server did not abort incomplete body")
				}
				if got := testutil.JSONRequest(t, base, "GET", "/info/"+md5, "", key, 200); !reflect.DeepEqual(got, created) {
					t.Fatal("incomplete PUT changed data")
				}
				testutil.JSONRequest(t, base, "GET", "/info/"+candidate, "", key, 404)
			}
		}
	})
	t.Run("rate limit and readiness", func(t *testing.T) {
		limited := testutil.Override(env, map[string]string{"HTTP_RATE_LIMIT_RPS": "1", "HTTP_RATE_LIMIT_BURST": "2"})
		p := testutil.StartAPI(t, binary, limited)
		base := p.Listening(t)
		testutil.JSONRequest(t, base, "GET", "/missing", "", "", 401)
		testutil.JSONRequest(t, base, "GET", "/info/"+strings.Repeat("f", 32), "", env["API_KEY"], 404)
		testutil.JSONRequest(t, base, "GET", "/missing", "", "", 429)
		testutil.Probe(t, binary, base, env)
		time.Sleep(1100 * time.Millisecond)
		testutil.JSONRequest(t, base, "GET", "/info/"+strings.Repeat("f", 32), "", env["API_KEY"], 404)
	})
}
