"""Verify actual migrations and persistence in an isolated Compose project."""

import argparse
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import uuid
from urllib.parse import quote


ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--runtime", action="store_true", help="check the real API process and Compose deployment")
    group.add_argument("--integration", "--repository", dest="integration", action="store_true", help="run Go integration tests instead of migration smoke tests")
    args = parser.parse_args()
    project = "threats-check-" + uuid.uuid4().hex[:12]
    env = {
        key: value for key, value in os.environ.items()
        if not key.startswith(("POSTGRES_", "HTTP_", "PG"))
        and key not in {"API_PORT", "LOG_LEVEL", "COMPOSE_PROFILES", "COMPOSE_FILE", "COMPOSE_PROJECT_NAME", "COMPOSE_ENV_FILES"}
    }
    env.update(
        POSTGRES_DB="threats_test",
        POSTGRES_USER="threats_test",
        # Special characters verify that credentials are not interpolated into a URL.
        POSTGRES_PASSWORD=secrets.token_hex(16) + "@:/?#",
        API_PORT="0",
        POSTGRES_PORT="0",  # Docker assigns an available loopback port.
    )
    with tempfile.TemporaryDirectory(prefix="threats-check-") as directory:
        envfile = Path(directory) / "empty.env"
        envfile.touch()
        compose = [
            "docker", "compose", "--project-name", project,
            "--env-file", str(envfile), "--file", str(ROOT / "compose.yaml"),
        ]

        def run(*args, input=None):
            result = subprocess.run(
                compose + list(args), cwd=ROOT, env=env,
                input=input, text=True, capture_output=True,
            )
            if result.returncode:
                raise RuntimeError(
                    "Compose command failed: " + " ".join(args)
                    + "\n" + result.stdout + result.stderr
                )
            return result.stdout.strip()

        def migrate(*args):
            return run(
                "run", "--rm", "migrate", "-path", "/migrations", "-database",
                "postgres://postgres:5432/?sslmode=disable", *args,
            )

        def sql(query):
            return run(
                "exec", "-T", "postgres", "psql", "-X", "-At",
                "-v", "ON_ERROR_STOP=1", "-U", env["POSTGRES_USER"],
                "-d", env["POSTGRES_DB"], input=query,
            )

        try:
            run("config", "--quiet")
            run("up", "-d", "--wait", "postgres")
            assert sql("SHOW server_version;").startswith("18.6"), "Wrong PostgreSQL version"
            if args.runtime:
                from runtime_checks import verify_runtime
                verify_runtime(ROOT, directory, env, compose, run, migrate, sql)
                return
            migrate("up")
            assert sql("SELECT version, dirty FROM public.schema_migrations;") == "1|f"
            if args.integration:
                address = run("port", "postgres", "5432")
                port = address.rsplit(":", 1)[1]
                test_env = dict(env)
                test_env["TEST_DATABASE_URL"] = (
                    "postgres://" + quote(env["POSTGRES_USER"], safe="") + ":"
                    + quote(env["POSTGRES_PASSWORD"], safe="")
                    + "@127.0.0.1:" + port + "/" + quote(env["POSTGRES_DB"], safe="")
                    + "?sslmode=disable"
                )
                subprocess.run(
                    ["go", "test", "-race", "-tags=integration", "-count=1", "-v", "./tests/integration"],
                    cwd=ROOT, env=test_env, check=True,
                )
                return
            migrate("up")  # Repeated up must leave the existing schema unchanged.
            sql((ROOT / "tests/integration/schema.sql").read_text())
            print("PASS: migration up, repeated up, constraints and defaults", flush=True)

            sql("INSERT INTO public.threats (md5, name) VALUES "
                "('d41d8cd98f00b204e9800998ecf8427e', 'persistent');")
            run("down")  # Remove containers/network, retain the named volume.
            run("up", "-d", "--wait", "postgres")
            assert sql("SELECT name FROM public.threats;") == "persistent"
            assert sql("SELECT version, dirty FROM public.schema_migrations;") == "1|f"
            print("PASS: data and migration version survive container recreation", flush=True)

            migrate("down", "1")
            assert sql("SELECT to_regclass('public.threats') IS NULL;") == "t"
            migrate("up")
            assert sql("SELECT version, dirty FROM public.schema_migrations;") == "1|f"
            assert sql("SELECT count(*) FROM public.threats;") == "0"
            print("PASS: rollback and reapply", flush=True)
        finally:
            # Only this randomly named test project's volume is removed.
            run("down", "--volumes", "--remove-orphans")


if __name__ == "__main__":
    main()
