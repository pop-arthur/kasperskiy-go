"""Verify actual migrations and persistence in an isolated Compose project."""

import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import uuid


ROOT = Path(__file__).resolve().parents[2]


def main():
    project = "threats-check-" + uuid.uuid4().hex[:12]
    env = dict(os.environ)
    env.update(
        POSTGRES_DB="threats_test",
        POSTGRES_USER="threats_test",
        # Special characters verify that credentials are not interpolated into a URL.
        POSTGRES_PASSWORD=secrets.token_hex(16) + "@:/?#",
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
            migrate("up")
            assert sql("SELECT version, dirty FROM public.schema_migrations;") == "1|f"
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
