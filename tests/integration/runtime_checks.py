"""Checks the actual executable and Compose deployment in the runner's private DB."""

import json
from pathlib import Path
import queue
import secrets
import shlex
import signal
import socket
import subprocess
import threading
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class APIProcess:
    def __init__(self, binary, env):
        self.process = subprocess.Popen(
            [str(binary)], env=env, stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT, text=True,
        )
        self.lines = []
        self.events = queue.Queue()
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        for line in self.process.stdout:
            self.lines.append(line)
            try:
                self.events.put(json.loads(line))
            except ValueError:
                self.events.put({"invalid_log": True})

    def listening(self):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            try:
                event = self.events.get(timeout=0.1)
            except queue.Empty:
                if self.process.poll() is not None:
                    raise AssertionError("API exited before listening")
                continue
            if event.get("msg") == "HTTP server listening":
                return event["address"]
        raise AssertionError("API startup timed out")

    def wait(self):
        result = self.process.wait(timeout=8)
        self.reader.join(timeout=2)
        assert not self.reader.is_alive(), "API stdout reader did not stop"
        return result

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.wait()
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.wait()
        else:
            self.reader.join(timeout=2)
        self.process.stdout.close()


def request(base, method, path, body=None, expected=200):
    encoded = None if body is None else json.dumps(body).encode()
    headers = {} if body is None else {"Content-Type": "application/json"}
    req = Request(base + path, data=encoded, headers=headers, method=method)
    try:
        response = urlopen(req, timeout=3)
    except HTTPError as error:
        response = error
    with response:
        result = json.load(response)
        assert response.status == expected, f"{method} {path}: {response.status}, expected {expected}"
        return result


def application_env(env, port):
    result = {key: value for key, value in env.items() if not key.startswith(("HTTP_", "POSTGRES_", "PG"))}
    result.update(
        LOG_LEVEL="INFO", HTTP_ADDR="127.0.0.1:0",
        HTTP_READ_HEADER_TIMEOUT="5s", HTTP_READ_TIMEOUT="10s",
        HTTP_WRITE_TIMEOUT="15s", HTTP_IDLE_TIMEOUT="60s",
        HTTP_SHUTDOWN_TIMEOUT="2s", HTTP_MAX_BODY_BYTES="1048576",
        POSTGRES_HOST="127.0.0.1", POSTGRES_PORT=port,
        POSTGRES_DB=env["POSTGRES_DB"], POSTGRES_USER=env["POSTGRES_USER"],
        POSTGRES_PASSWORD=env["POSTGRES_PASSWORD"], POSTGRES_SSLMODE="disable",
        POSTGRES_MAX_CONNS="4", POSTGRES_CONNECT_TIMEOUT="1s",
    )
    return result


def check_failure(binary, env, expected_text):
    api = APIProcess(binary, env)
    try:
        assert api.wait() == 1, "startup failure must exit 1"
        logs = "".join(api.lines)
        assert expected_text in logs, "startup diagnosis missing"
        assert env["POSTGRES_PASSWORD"] not in logs, "password leaked in logs"
        assert logs.count('"level":"ERROR"') == 1, "startup error must be logged once"
        assert "HTTP server listening" not in logs, "failed startup opened HTTP listener"
    finally:
        api.close()


def verify_runtime(root, directory, env, compose, run, migrate, sql):
    binary = Path(directory) / "api"
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/api"], cwd=root, check=True)
    port = run("port", "postgres", "5432").rsplit(":", 1)[1]
    app_env = application_env(env, port)

    # The runner has deliberately not applied migrations yet.
    check_failure(binary, app_env, "make migrate-up")
    migrate("up")
    for change, restore in [
        ("UPDATE public.schema_migrations SET dirty=true", "UPDATE public.schema_migrations SET dirty=false"),
        ("UPDATE public.schema_migrations SET version=2", "UPDATE public.schema_migrations SET version=1"),
        ("ALTER TABLE public.threats RENAME TO threats_hidden", "ALTER TABLE public.threats_hidden RENAME TO threats"),
    ]:
        sql(change)
        try:
            check_failure(binary, app_env, "make migrate-up")
        finally:
            sql(restore)
    with socket.socket() as occupied:
        occupied.bind(("127.0.0.1", 0))
        occupied.listen()
        check_failure(binary, dict(app_env, HTTP_ADDR=f"127.0.0.1:{occupied.getsockname()[1]}"), "listen HTTP")
        check_failure(binary, dict(app_env, POSTGRES_PORT=str(occupied.getsockname()[1]), POSTGRES_CONNECT_TIMEOUT="150ms"), "connect to PostgreSQL")
        check_failure(binary, dict(app_env, POSTGRES_PORT=str(occupied.getsockname()[1]), POSTGRES_CONNECT_TIMEOUT="150ms", LOG_LEVEL="ERROR+1"), "connect to PostgreSQL")
    check_failure(binary, dict(app_env, HTTP_ADDR="invalid"), "HTTP_ADDR")
    print("PASS: binary rejects invalid config, unavailable DB, absent/dirty/wrong schema and occupied port", flush=True)

    api = APIProcess(binary, app_env)
    try:
        address = api.listening()
        base = "http://" + address
        md5 = secrets.token_hex(16)
        created = request(base, "POST", "/info", {"md5": md5.upper(), "name": "binary", "description": "old"}, 201)
        assert created["md5"] == md5
        assert request(base, "GET", "/info/" + md5)["name"] == "binary"
        assert "description" not in request(base, "PUT", "/info/" + md5, {"name": "updated"})
        api.process.send_signal(signal.SIGTERM)
        assert api.wait() == 0, "SIGTERM must shut down normally"
        assert app_env["POSTGRES_PASSWORD"] not in "".join(api.lines)
        host, port_number = address.rsplit(":", 1)
        try:
            connection = socket.create_connection((host, int(port_number)), timeout=0.2)
        except OSError:
            pass
        else:
            connection.close()
            raise AssertionError("HTTP port was not released")
    finally:
        api.close()
    api = APIProcess(binary, app_env)
    try:
        api.listening()
        api.process.send_signal(signal.SIGINT)
        assert api.wait() == 0, "SIGINT must shut down normally"
    finally:
        api.close()
    print("PASS: real binary POST/GET/PUT, SIGTERM/SIGINT and listener release", flush=True)

    def make(target):
        subprocess.run(["make", "COMPOSE=" + shlex.join(compose), target], cwd=root, env=env, check=True)

    def compose_address():
        address = run("port", "api", "8080")
        base = "http://" + address
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            try:
                request(base, "GET", "/info/" + "f" * 32, expected=404)
                return base
            except (URLError, ConnectionError, TimeoutError):
                time.sleep(0.05)
        raise AssertionError("Compose API did not become available")

    make("up")
    base = compose_address()
    persistent = secrets.token_hex(16)
    request(base, "POST", "/info", {"md5": persistent, "name": "compose-persistent"}, 201)
    request(base, "PUT", "/info/" + persistent, {"name": "compose-updated"})
    make("down")
    make("up")
    base = compose_address()
    assert request(base, "GET", "/info/" + persistent)["name"] == "compose-updated"
    identifier = run("ps", "-q", "api")
    run("stop", "api")
    exit_code = subprocess.check_output(["docker", "inspect", "--format", "{{.State.ExitCode}}", identifier], text=True).strip()
    assert exit_code == "0", "Compose stop must finish API gracefully"
    print("PASS: make up/down, Compose HTTP operations, persistent volume and graceful container stop", flush=True)
