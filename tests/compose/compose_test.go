//go:build e2e

package compose_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kaspersky-go-test/tests/testutil"
)

type deployment struct {
	t       *testing.T
	project string
	env     map[string]string
	envfile string
}

func newDeployment(t *testing.T) *deployment {
	t.Helper()
	d := &deployment{t: t, project: "threats-go-" + testutil.RandomID(), envfile: filepath.Join(t.TempDir(), "empty.env"), env: map[string]string{
		"API_KEY": testutil.RandomID() + testutil.RandomID(), "POSTGRES_DB": "threats_test", "POSTGRES_USER": "threats_test", "POSTGRES_PASSWORD": testutil.RandomID() + "@:/?#",
		"API_PORT": "0", "POSTGRES_PORT": "0", "HTTP_RATE_LIMIT_RPS": "10000", "HTTP_RATE_LIMIT_BURST": "10000", "POSTGRES_OPERATION_TIMEOUT": "5s",
	}}
	if err := os.WriteFile(d.envfile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Cleanup не использует контекст завершившегося теста и удаляет только его проект.
		if _, err := d.compose("down", "--volumes", "--remove-orphans"); err != nil {
			t.Error(err)
		}
		if _, err := d.command(nil, "docker", []string{"image", "rm", d.project + "-api"}, nil); err != nil {
			t.Log("test image cleanup:", err)
		}
	})
	if _, err := d.compose("config", "--quiet"); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d *deployment) command(changes map[string]string, name string, args []string, input *string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = testutil.Root()
	cmd.Env = testutil.CleanEnv(testutil.Override(d.env, changes))
	if input != nil {
		cmd.Stdin = strings.NewReader(*input)
	}
	out, err := cmd.CombinedOutput()
	text := testutil.Redact(string(out), d.env["API_KEY"], d.env["POSTGRES_PASSWORD"])
	if err != nil {
		return text, fmt.Errorf("%s command failed: %v\n%s", name, err, text)
	}
	return strings.TrimSpace(text), nil
}
func (d *deployment) args() []string {
	return []string{"compose", "--project-name", d.project, "--env-file", d.envfile, "--file", filepath.Join(testutil.Root(), "compose.yaml")}
}
func (d *deployment) compose(args ...string) (string, error) {
	return d.command(nil, "docker", append(d.args(), args...), nil)
}
func (d *deployment) must(args ...string) string {
	d.t.Helper()
	out, err := d.compose(args...)
	if err != nil {
		d.t.Fatal(err)
	}
	return out
}
func (d *deployment) sql(query string) string {
	d.t.Helper()
	args := append(d.args(), "exec", "-T", "postgres", "psql", "-X", "-At", "-v", "ON_ERROR_STOP=1", "-U", d.env["POSTGRES_USER"], "-d", d.env["POSTGRES_DB"])
	out, err := d.command(nil, "docker", args, &query)
	if err != nil {
		d.t.Fatal(err)
	}
	return out
}
func (d *deployment) make(target string, changes map[string]string, success bool) {
	d.t.Helper()
	// Аргумент COMPOSE передаётся make напрямую, без shell и интерполяции секретов.
	var quoted []string
	for _, arg := range append([]string{"docker"}, d.args()...) {
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "'\"'\"'")+"'")
	}
	_, err := d.command(changes, "make", []string{"COMPOSE=" + strings.Join(quoted, " "), target}, nil)
	if success && err != nil {
		d.t.Fatal(err)
	}
	if !success && err == nil {
		d.t.Fatal("make unexpectedly succeeded")
	}
}
func (d *deployment) base() string { return "http://" + d.must("port", "api", "8080") }
func (d *deployment) inspect(format string) string {
	d.t.Helper()
	id := d.must("ps", "-a", "-q", "api")
	out, err := d.command(nil, "docker", []string{"inspect", "--format", format, id}, nil)
	if err != nil {
		d.t.Fatal(err)
	}
	return out
}

func TestComposeDeployment(t *testing.T) {
	d := newDeployment(t)
	d.make("up", map[string]string{"HTTP_MAX_BODY_BYTES": "0"}, false)
	if d.inspect("{{.State.ExitCode}}") != "1" {
		t.Fatal("configuration failure did not exit 1")
	}
	if d.sql("SHOW server_version;") == "" {
		t.Fatal("database unavailable")
	}
	if state := d.sql("SELECT version,dirty FROM public.schema_migrations;"); state != "1|f" {
		t.Fatal("migration state:", state)
	}
	// Ошибка мигратора должна прервать make up до запуска API.
	d.sql("UPDATE public.schema_migrations SET dirty=true;")
	d.make("up", nil, false)
	d.sql("UPDATE public.schema_migrations SET dirty=false;")
	d.sql("ALTER TABLE public.threats RENAME TO threats_hidden;")
	d.make("up", nil, true)
	if d.inspect("{{.State.Health.Status}}") != "healthy" {
		t.Fatal("make up returned before readiness")
	}
	base := d.base()
	key := d.env["API_KEY"]
	body := testutil.JSONRequest(t, base, "GET", "/info/"+strings.Repeat("f", 32), "", key, 500)
	if body["details"] != "" {
		t.Fatal("SQL details leaked")
	}
	d.sql("ALTER TABLE public.threats_hidden RENAME TO threats;")
	testutil.JSONRequest(t, base, "GET", "/readyz", "", "", 404)
	testutil.JSONRequest(t, base, "GET", "/missing", "", "", 401)
	md5 := testutil.RandomID()
	testutil.JSONRequest(t, base, "POST", "/info", `{"md5":"`+md5+`","name":"compose-persistent"}`, key, 201)
	testutil.JSONRequest(t, base, "PUT", "/info/"+md5, `{"name":"compose-updated"}`, key, 200)
	d.make("down", nil, true)
	d.make("up", nil, true)
	base = d.base()
	if d.inspect("{{.State.Health.Status}}") != "healthy" {
		t.Fatal("recreated API unhealthy")
	}
	if body := testutil.JSONRequest(t, base, "GET", "/info/"+md5, "", key, 200); body["name"] != "compose-updated" {
		t.Fatal("volume lost data")
	}
	if state := d.sql("SELECT version,dirty FROM public.schema_migrations;"); state != "1|f" {
		t.Fatal("volume lost migration state")
	}
	d.must("stop", "api")
	if d.inspect("{{.State.ExitCode}}") != "0" {
		t.Fatal("Compose stop did not finish gracefully")
	}
}
