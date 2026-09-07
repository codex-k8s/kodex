#!/usr/bin/env python3
"""Проверяет code-first bootstrap/rotation только на disposable local fixture."""

import base64
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BROKER = ROOT / "services/internal/secret-broker"
SCRIPT = ROOT / "tools/install/bootstrap-secret-drafts.sh"


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def main():
    with tempfile.TemporaryDirectory(prefix="kodex-secret-drafts-") as raw:
        temporary = Path(raw)
        temporary.chmod(0o700)
        binary = temporary / "bin"
        binary.mkdir(mode=0o700)
        (binary / "kubectl").symlink_to(ROOT / "scripts/tests/fixtures/secret-drafts-kubectl.py")
        first, second, third = (temporary / name for name in ("first.json", "second.json", "third.json"))
        for args in (
            ["generate", "--output-file", str(first)],
            ["rotate", "--input-file", str(first), "--output-file", str(second), "--expected-revision", "1"],
            ["rotate", "--input-file", str(second), "--output-file", str(third), "--expected-revision", "2"],
        ):
            completed = subprocess.run(["go", "run", "./cmd/secret-draft-keys", *args], cwd=BROKER, capture_output=True, timeout=60)
            require(completed.returncode == 0 and not completed.stdout, "fixture key generation failed")
        state_path = temporary / "state.json"
        environment = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ["PATH"], KODEX_DRAFT_FIXTURE_STATE=str(state_path))
        forbidden = [key["material"].encode() for path in (first, second, third) for key in json.loads(path.read_text())["keys"]]

        def state():
            return json.loads(state_path.read_text())

        def write_state(value):
            state_path.write_text(json.dumps(value))

        def run(mode, keyring, expected=None, succeeds=True, extra=None):
            args = ["bash", str(SCRIPT), mode, "--context", "fixture", "--keyring-file", str(keyring), "--readback-timeout-seconds", "1"]
            if expected is not None:
                args += ["--expected-revision", str(expected)]
            completed = subprocess.run(args, cwd=ROOT, env=dict(environment, **(extra or {})), capture_output=True, timeout=30)
            require((completed.returncode == 0) == succeeds, "bootstrap outcome mismatch")
            require(not any(item in completed.stdout + completed.stderr for item in forbidden), "key material escaped bootstrap diagnostics")

        run("ensure", first, succeeds=False, extra={"KODEX_DRAFT_FIXTURE_LOST_WRITE": "1"})
        require("keyring" not in state(), "unknown backup write allowed serving effect")
        run("ensure", first)
        initial = state()
        run("ensure", first)
        require(state() == initial, "repeated bootstrap changed existing guard or keyring")
        run("ensure", second, succeeds=False)
        require(state() == initial, "bootstrap silently rotated key material")
        run("rotate", second, 1, succeeds=False, extra={"KODEX_DRAFT_FIXTURE_CONFLICT": "1"})
        require(state()["keyring"] == initial["keyring"] and state()["guard"] == initial["guard"], "failed CAS changed existing keyring or guard")
        run("rotate", second, 1, succeeds=False, extra={"KODEX_DRAFT_FIXTURE_LOST_WRITE": "1"})
        rotated = state()
        require(rotated["keyring"]["metadata"]["resourceVersion"] == "2", "lost response did not preserve committed rotation")
        run("rotate", second, 1)
        require(state() == rotated, "rotation retry repeated effect")
        old_uses = json.loads(initial["guard"]["data"]["state.json"])["uses"]
        new_uses = json.loads(rotated["guard"]["data"]["state.json"])["uses"]
        require(new_uses[:1] == old_uses, "rotation reset durable encryption count")
        run("rotate", third, 1, succeeds=False)
        require(state() == rotated, "stale expected revision changed serving keyring")
        damaged = dict(rotated)
        damaged.pop("guard")
        write_state(damaged)
        run("ensure", second, succeeds=False)
        require("guard" not in state(), "missing high watermark was reinitialized")
        write_state(rotated)
        run("rotate", third, 2, succeeds=False, extra={"KODEX_DRAFT_FIXTURE_NO_ACK": "1"})
        require(state()["keyring"]["metadata"]["resourceVersion"] == "3", "rotation fixture did not publish candidate")
        require(json.loads(state()["guard"]["data"]["state.json"])["manifest"]["revision"] == 2, "unacknowledged rotation was accepted")
        print("Secret draft bootstrap, CAS rotation, rollback and exact readback fixtures passed")


if __name__ == "__main__":
    main()
