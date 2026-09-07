#!/usr/bin/env python3
"""Фактическая мигрированная CP schema и изолированная SQL reference matrix."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

root = Path(__file__).resolve().parents[2]
name = f"kodex-orphan-pg-{os.getpid()}"
image = "docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7"
sql = (root / "services/internal/control-plane/internal/maintenance/runtimeorphan/orphan_references.sql").read_text()
uid = "11111111-1111-4111-8111-111111111111"
pins = ["-v", "secret_ref=sec_orphanfixture", "-v", "secret_name=runtime-secret-fixture-r1",
        "-v", "secret_uid=" + uid, "-v", "operation_ref=secop_fixture"]


def run(args, data=None, timeout=30, cwd=None):
    return subprocess.run(args, input=data, text=True, capture_output=True, timeout=timeout, cwd=cwd)


def psql(database, text, user="postgres", variables=()):
    return run(["docker", "exec", "-i", name, "psql", "-X", "-qAt", "-U", user,
                "-d", database, "-v", "ON_ERROR_STOP=1", *variables], text)


columns = {
    "runtime_secrets": ["ref"],
    "runtime_secret_revisions": ["secret_uid", "secret_name"],
    "runtime_secret_operations": ["ref", "terminal_secret_snapshot"],
    "runtime_secret_drafts": ["encrypted_descriptor"],
    "runtime_secret_draft_operations": ["ref", "terminal_snapshot", "encrypted_cleanup_descriptor", "materialization_cleanup_descriptor"],
    "provider_credential_revisions": ["secret_uid"],
    "provider_credential_cleanup_tasks": ["secret_uid"],
    "integration_credential_revisions": ["secret_uid"],
    "email_credential_descriptors": ["secret_uid"],
    "runtime_revisions": ["provider_secret_uid", "safe_snapshot"],
    "runtime_environment_versions": ["secret_descriptors"],
    "runtime_environment_drafts": ["specification"],
    "runtime_secret_draft_impact_items": ["snapshot"],
}

with tempfile.TemporaryDirectory() as tmp:
    binary = Path(tmp) / "cli"
    assert run(["go", "build", "-o", str(binary), "./cmd/cli"], timeout=120,
               cwd=root / "services/internal/control-plane").returncode == 0
    binary.chmod(0o555)
    dsn = Path(tmp) / "fixture-dsn"
    dsn.write_text("postgresql://control_plane_migrator@127.0.0.1/control_plane?sslmode=disable")
    volumes = []
    try:
        assert run(["docker", "run", "-d", "--pull=never", "--network=none", "--name", name,
                    "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "--mount", f"type=bind,src={binary},dst=/fixture-cli,readonly",
                    "--mount", f"type=bind,src={dsn},dst=/fixture-dsn,readonly", image]).returncode == 0
        volumes = [v["Name"] for v in json.loads(run(["docker", "inspect", name]).stdout)[0]["Mounts"] if v["Type"] == "volume"]
        deadline = time.monotonic() + 30
        while run(["docker", "exec", name, "pg_isready", "-U", "postgres"]).returncode:
            assert time.monotonic() < deadline
            time.sleep(1)
        bootstrap = (root / "deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql").read_text()
        assert psql("postgres", bootstrap).returncode == 0
        assert run(["docker", "exec", "-e", "CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=/fixture-dsn", name,
                    "/fixture-cli", "up"], timeout=90).returncode == 0, "canonical migration failed"
        actual = psql("control_plane", sql, variables=pins)
        assert actual.returncode == 0 and actual.stdout.strip() == "f", "actual owner schema query failed"
        assert psql("postgres", "CREATE DATABASE orphan_reference_fixture;").returncode == 0
        definitions = ["CREATE SCHEMA control_plane;"]
        types = {}
        for table, fields in columns.items():
            definitions.append(f"CREATE TABLE control_plane.{table} (")
            fields_sql = []
            for field in fields:
                result = psql("control_plane", f"SELECT format_type(a.atttypid,a.atttypmod) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='control_plane' AND c.relname='{table}' AND a.attname='{field}' AND NOT a.attisdropped;")
                value = result.stdout.strip()
                assert result.returncode == 0 and value in ("text", "uuid", "jsonb"), "unexpected canonical column type"
                types[table, field] = value
                fields_sql.append(field + " " + value)
            definitions.append(",".join(fields_sql) + ");")
        assert psql("orphan_reference_fixture", "\n".join(definitions)).returncode == 0
        for (table, field), kind in types.items():
            values = [json.dumps({"nested": [{"PascalCase": target}]}) for target in (uid, "sec_orphanfixture", "runtime-secret-fixture-r1")] if kind == "jsonb" else [uid if "uid" in field else "runtime-secret-fixture-r1" if field == "secret_name" else "sec_orphanfixture" if table == "runtime_secrets" else "secop_fixture"]
            for value in values:
                assert psql("orphan_reference_fixture", f"INSERT INTO control_plane.{table}({field}) VALUES ('{value}');").returncode == 0
                result = psql("orphan_reference_fixture", sql, variables=pins)
                assert result.returncode == 0 and result.stdout.strip() == "t", f"missing reference group {table}.{field}"
                assert psql("orphan_reference_fixture", f"TRUNCATE control_plane.{table};").returncode == 0
        assert psql("orphan_reference_fixture", "CREATE ROLE orphan_narrow LOGIN; GRANT USAGE ON SCHEMA control_plane TO orphan_narrow; GRANT SELECT ON ALL TABLES IN SCHEMA control_plane TO orphan_narrow;").returncode == 0
        assert psql("orphan_reference_fixture", sql, user="orphan_narrow", variables=pins).returncode != 0
        print("Runtime orphan PostgreSQL passed: canonical migrated schema, every historical/direct/cross-purpose group, global-reader refusal")
    finally:
        assert run(["docker", "rm", "-fv", name]).returncode == 0
        assert all(run(["docker", "volume", "inspect", v]).returncode != 0 for v in volumes)
