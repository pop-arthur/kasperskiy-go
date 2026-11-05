//go:build e2e

package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write синхронизирует запись stdout/stderr с чтением буфера тестом.
func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}
func (b *lockedBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

// Process управляет настоящим API-процессом и безопасным сбором его логов.
type Process struct {
	// Cmd предоставляет запущенный процесс для отправки сигналов; его Wait выполняет helper.
	Cmd     *exec.Cmd
	logs    lockedBuffer
	done    chan struct{}
	secrets []string
	err     error
}

// BuildAPI собирает бинарник во временную директорию; ошибка или превышение двух минут завершает тест.
func BuildAPI(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "api")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/api")
	cmd.Dir = Root()
	cmd.Env = CleanEnv(nil)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build API: %v\n%s", err, output)
	}
	return binary
}

// StartAPI запускает бинарник с заданным окружением; cleanup посылает SIGINT и при необходимости завершает процесс принудительно.
func StartAPI(t *testing.T, binary string, env map[string]string) *Process {
	t.Helper()
	p := &Process{Cmd: exec.Command(binary), done: make(chan struct{}), secrets: []string{env["POSTGRES_PASSWORD"], env["API_KEY"]}}
	p.Cmd.Env = CleanEnv(env)
	p.Cmd.Stdout = &p.logs
	p.Cmd.Stderr = &p.logs
	if err := p.Cmd.Start(); err != nil {
		t.Fatal("start API:", err)
	}
	go func() { p.err = p.Cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = p.Cmd.Process.Signal(os.Interrupt)
		select {
		case <-p.done:
			return
		case <-time.After(8 * time.Second):
		}
		_ = p.Cmd.Process.Kill()
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			t.Error("API process did not exit after kill")
		}
	})
	return p
}

// Logs возвращает потокобезопасный снимок логов без API-ключа и пароля БД из env.
func (p *Process) Logs() string { return Redact(p.logs.text(), p.secrets...) }

// RawLogsContain проверяет утечку непустого секрета без вывода исходных логов.
func (p *Process) RawLogsContain(secret string) bool {
	return secret != "" && strings.Contains(p.logs.text(), secret)
}

// Listening ждёт адрес сервера до десяти секунд; ранний выход процесса или таймаут завершает тест.
func (p *Process) Listening(t *testing.T) string {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, line := range strings.Split(p.logs.text(), "\n") {
			var event struct {
				Message string `json:"msg"`
				Address string `json:"address"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Message == "HTTP server listening" {
				return "http://" + event.Address
			}
		}
		select {
		case <-p.done:
			t.Fatalf("API exited before listening: %s", p.Logs())
		case <-deadline.C:
			t.Fatalf("API startup timed out: %s", p.Logs())
		case <-ticker.C:
		}
	}
}

// Wait возвращает exit code процесса; ошибка ожидания или превышение восьми секунд завершает тест.
func (p *Process) Wait(t *testing.T) int {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(8 * time.Second):
		t.Fatalf("API did not exit: %s", p.Logs())
	}
	if p.err != nil {
		if exit, ok := p.err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		t.Fatalf("wait API: %v", p.err)
	}
	return 0
}

// AppEnv создаёт окружение для тестовой БД со случайным ключом, свободным HTTP-портом и высокими лимитами запросов.
func AppEnv(db *Database) map[string]string {
	parsed, err := url.Parse(db.DSN)
	if err != nil {
		panic("invalid test database URL")
	}
	return map[string]string{
		"API_KEY": RandomID() + RandomID(), "LOG_LEVEL": "INFO", "HTTP_ADDR": "127.0.0.1:0",
		"HTTP_RATE_LIMIT_RPS": "10000", "HTTP_RATE_LIMIT_BURST": "10000", "HTTP_RATE_LIMIT_MAX_CLIENTS": "10000",
		"HTTP_READ_HEADER_TIMEOUT": "5s", "HTTP_READ_TIMEOUT": "10s", "HTTP_WRITE_TIMEOUT": "15s", "HTTP_IDLE_TIMEOUT": "60s", "HTTP_SHUTDOWN_TIMEOUT": "2s", "HTTP_MAX_BODY_BYTES": "1048576",
		"POSTGRES_HOST": parsed.Hostname(), "POSTGRES_PORT": parsed.Port(), "POSTGRES_DB": db.Name, "POSTGRES_USER": db.User, "POSTGRES_PASSWORD": db.Password,
		"POSTGRES_SSLMODE": "disable", "POSTGRES_MAX_CONNS": "4", "POSTGRES_CONNECT_TIMEOUT": "1s", "POSTGRES_OPERATION_TIMEOUT": "5s",
	}
}

// Override возвращает копию env с наложенными changes, не изменяя исходные карты.
func Override(env map[string]string, changes map[string]string) map[string]string {
	result := make(map[string]string, len(env)+len(changes))
	for k, v := range env {
		result[k] = v
	}
	for k, v := range changes {
		result[k] = v
	}
	return result
}

// JSONRequest отправляет запрос с необязательным Bearer-ключом и проверяет статус и JSON-объект ответа; для HEAD не подходит.
func JSONRequest(t *testing.T, base, method, path, body, key string, status int) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal("HTTP request:", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if response.StatusCode != status || response.Header.Get("Content-Type") != "application/json" || json.Unmarshal(data, &result) != nil {
		t.Fatalf("%s %s: status %d, body %s; want %d JSON", method, path, response.StatusCode, data, status)
	}
	if status >= 400 {
		if _, ok := result["message"].(string); !ok {
			t.Fatal("missing error message")
		}
		if _, ok := result["details"].(string); !ok {
			t.Fatal("missing error details")
		}
	}
	return result
}

// Probe запускает healthcheck для base URL; ошибка или превышение четырёх секунд завершает тест.
func Probe(t *testing.T, binary, base string, env map[string]string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "healthcheck")
	cmd.Env = CleanEnv(Override(env, map[string]string{"HTTP_ADDR": strings.TrimPrefix(base, "http://")}))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("healthcheck: %v %s", err, Redact(string(output), env["API_KEY"], env["POSTGRES_PASSWORD"]))
	}
}

// ExpectStartupFailure проверяет exit 1, диагностику, единственный error-log и отсутствие утечек ключа и пароля.
func ExpectStartupFailure(t *testing.T, binary string, env map[string]string, diagnosis string) {
	t.Helper()
	p := StartAPI(t, binary, env)
	if code := p.Wait(t); code != 1 {
		t.Fatalf("startup exit %d; want 1", code)
	}
	logs := p.Logs()
	if !strings.Contains(logs, diagnosis) || strings.Count(logs, `"level":"ERROR"`) != 1 || strings.Contains(logs, "HTTP server listening") {
		t.Fatalf("invalid startup diagnostics: %s", logs)
	}
	for _, secret := range []string{env["API_KEY"], env["POSTGRES_PASSWORD"]} {
		if p.RawLogsContain(secret) {
			t.Fatal("startup logs leaked credentials")
		}
	}
}
