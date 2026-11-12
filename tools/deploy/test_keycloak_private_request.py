"""Проверяет фактические argv, окружение и удаление приватного состояния kcadm."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class PrivateRequestTest(unittest.TestCase):
    def run_request(self, auth_kind, input_kind, payload, fail=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            capture = root / "capture.jsonl"
            stub = root / "kcadm"
            stub.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
config = pathlib.Path(args[args.index("--config") + 1])
record = {"args": args, "client_secret": os.getenv("KC_CLI_CLIENT_SECRET"),
          "password": os.getenv("KC_CLI_PASSWORD"), "private": str(config.parent),
          "directory_mode": config.parent.stat().st_mode & 0o777}
if args[:2] == ["config", "credentials"]:
    config.write_text("synthetic-auth-cache")
else:
    record["config_mode"] = config.stat().st_mode & 0o777
    if "-f" in args:
        body = pathlib.Path(args[args.index("-f") + 1])
        record["body"] = json.loads(body.read_text())
        record["body_mode"] = body.stat().st_mode & 0o777
with open(os.environ["CAPTURE"], "a") as stream:
    stream.write(json.dumps(record) + "\\n")
if args[:2] != ["config", "credentials"] and os.getenv("FAIL_REQUEST") == "1":
    sys.exit(7)
''')
            stub.chmod(0o700)
            source = Path(__file__).with_name("keycloak-private-request.sh").read_text()
            source = source.replace("command=/opt/keycloak/bin/kcadm.sh", f"command={stub}")
            result = subprocess.run(
                ["sh", "-ec", source, "sh", auth_kind, input_kind, "update", "clients/synthetic"],
                input="synthetic-client\nsynthetic-auth-secret\n" + payload,
                text=True, capture_output=True,
                env={**os.environ, "CAPTURE": str(capture), "FAIL_REQUEST": str(int(fail))},
                timeout=5,
            )
            self.assertEqual(result.returncode, 7 if fail else 0, result.stderr)
            records = [json.loads(line) for line in capture.read_text().splitlines()]
            self.assertEqual(len(records), 2)
            for record in records:
                self.assertNotIn("synthetic-auth-secret", " ".join(record["args"]))
                self.assertNotIn("synthetic-new-password", " ".join(record["args"]))
                self.assertEqual(record["directory_mode"], 0o700)
                self.assertFalse(Path(record["private"]).exists())
            self.assertEqual(records[1]["config_mode"], 0o600)
            self.assertEqual(result.stdout, "")
            return records

    def test_client_auth_and_json_never_enter_argv(self):
        body = {"secret": 'synthetic-body-"\\\nsecret'}
        records = self.run_request("client", "json", json.dumps(body))
        self.assertEqual(records[0]["client_secret"], "synthetic-auth-secret")
        self.assertIsNone(records[1]["client_secret"])
        self.assertEqual(records[1]["body"], body)
        self.assertEqual(records[1]["body_mode"], 0o600)

    def test_bootstrap_password_and_new_password_use_scoped_environment(self):
        records = self.run_request("user", "password", "synthetic-new-password\n")
        self.assertEqual(records[0]["password"], "synthetic-auth-secret")
        self.assertEqual(records[1]["password"], "synthetic-new-password")

    def test_failure_also_removes_auth_cache_and_input(self):
        self.run_request("client", "json", '{"secret":"synthetic-body-secret"}', fail=True)


if __name__ == "__main__":
    unittest.main()
