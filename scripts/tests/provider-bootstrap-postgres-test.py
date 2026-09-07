#!/usr/bin/env python3
"""Disposable PostgreSQL: настоящий operator SQL на полной схеме CP."""
import concurrent.futures
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
IMAGE = "docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7"
NAME = "kodex-provider-bootstrap-test-" + str(os.getpid())


def run(argv, *, data=None, env=None, cwd=None, ok=True, timeout=120):
    result = subprocess.run(argv, input=data, capture_output=True, env=env, cwd=cwd, timeout=timeout)
    if ok and result.returncode:
        raise AssertionError("disposable command failed")
    return result


def test():
    run(["docker", "run", "--rm", "-d", "--name", NAME, "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
         "-p", "127.0.0.1::5432", IMAGE])
    port = run(["docker", "inspect", "--format", '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}', NAME]).stdout.decode().strip()
    assert port.isdigit()
    for _ in range(30):
        if run(["pg_isready", "-h", "127.0.0.1", "-p", port, "-U", "postgres"], ok=False).returncode == 0:
            break
        time.sleep(1)
    else:
        raise AssertionError("disposable database readiness failed")
    cli = ["psql", "-XqAt", "-h", "127.0.0.1", "-p", port, "-U", "postgres", "-v", "ON_ERROR_STOP=1"]
    run(cli + ["-d", "postgres"], data=(ROOT / "deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql").read_bytes())
    with tempfile.TemporaryDirectory(prefix="kodex-provider-migration-") as directory:
        dsn = Path(directory) / "dsn"
        dsn.write_text("postgresql://control_plane_migrator@127.0.0.1:" + port + "/control_plane?sslmode=disable")
        dsn.chmod(0o600)
        env = {**os.environ, "CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE": str(dsn), "GOWORK": "off",
               "GOMAXPROCS": "2", "GOFLAGS": "-p=2"}
        run(["go", "run", "./cmd/cli", "up"], env=env, cwd=ROOT / "services/internal/control-plane", timeout=180)
    cli += ["-d", "control_plane"]
    run(cli, data=(ROOT / "scripts/tests/provider-bootstrap-fixture.sql").read_bytes())
    ensure = cli + ["-v", "account_ref=pacc_reserved_fixture", "-v", "stable_key=reserved-fixture",
                    "-v", "account_name=Synthetic reserved", "-v", "max_concurrent_executions=1"]
    for _ in range(2):
        run(ensure, data=(ROOT / "tools/dev/provider-account-ensure.sql").read_bytes())
    reserved = json.loads(run(cli + ["-v", "stable_key=reserved-fixture"],
                              data=(ROOT / "tools/dev/provider-account-snapshot.sql").read_bytes()).stdout)[0]
    assert reserved["version"] == 1 and reserved["state"] == "PENDING_AUTHORIZATION"
    assert reserved["credential"] is None

    def snapshot():
        return json.loads(run(cli + ["-v", "stable_key=default-openai-codex"],
                              data=(ROOT / "tools/dev/provider-account-snapshot.sql").read_bytes()).stdout)[0]

    def values(letter, version, prior):
        number = "abcd".index(letter) + 1
        return dict(account_ref="pacc_bootstrap_fixture", stable_key="default-openai-codex",
                    expected_version=str(version), expected_credential_ref=prior,
                    credential_ref="pcr_bootstrap_fixture_" + letter, secret_name="provider-bootstrap-" + letter,
                    secret_uid=f"10000000-0000-4000-8000-00000000000{number}",
                    secret_resource_version=str(number), content_sha256=letter * 64)

    def apply(params, succeeds=True):
        args = list(cli)
        for key, value in params.items():
            args += ["-v", key + "=" + value]
        result = run(args, data=(ROOT / "tools/dev/reconcile-provider-account.sql").read_bytes(), ok=False)
        if succeeds:
            assert result.returncode == 0 and json.loads(result.stdout) is True
        return result.returncode == 0

    a = values("a", 1, "")
    assert snapshot()["credential"] is None
    apply(a)
    first = snapshot()
    assert first["version"] == 2
    apply(a)
    assert snapshot() == first
    assert not apply(values("b", 1, ""), False)
    assert snapshot() == first
    bad = values("b", 2, first["credentialRef"])
    bad["content_sha256"] = "invalid"
    assert not apply(bad, False)
    assert snapshot() == first
    foreign = values("b", 2, first["credentialRef"])
    foreign["account_ref"] = "pacc_foreign_fixture"
    assert not apply(foreign, False)
    b = values("b", 2, first["credentialRef"])
    apply(b)
    second = snapshot()
    assert not apply(a, False)
    assert snapshot() == second
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(lambda letter: apply(values(letter, 3, second["credentialRef"]), False), ("c", "d")))
    assert sorted(results) == [False, True]
    terminal = snapshot()
    assert terminal["version"] == 4
    count = run(cli, data=b"SELECT count(*) FROM control_plane.provider_credential_revisions WHERE provider_account_id = (SELECT id FROM control_plane.provider_accounts WHERE ref='pacc_bootstrap_fixture');").stdout.strip()
    assert count == b"3"
    audits = run(cli, data=b"SELECT count(*) FROM control_plane.audit_events WHERE resource_ref='pacc_bootstrap_fixture' AND action='provider_account.bootstrap_credential_imported';").stdout.strip()
    assert audits == b"3"
    current = values("c" if results[0] else "d", 3, second["credentialRef"])
    current.update(account_ref="pacc_disabled_fixture", stable_key="disabled-fixture")
    assert not apply(current, False)
    print("Provider bootstrap PostgreSQL passed: initial/replay/stale/concurrent/foreign/rollback/retained/disabled")


if __name__ == "__main__":
    try:
        test()
    finally:
        run(["docker", "stop", "--time", "5", NAME], ok=False, timeout=20)
