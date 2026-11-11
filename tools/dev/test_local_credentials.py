"""Проверки bootstrap SSO на синтетических credentials без живого окружения."""

import os
import json
from pathlib import Path
import subprocess
import tempfile
import unittest


class LocalCredentialsTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="kodex-credentials-test-")
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "credentials.env"
        self.script = Path(__file__).parent / "prepare-local-credentials.sh"
        self.values = {
            "KODEX_LOCAL_OWNER_USERNAME": "test owner",
            "KODEX_LOCAL_OWNER_EMAIL": "owner@example.invalid",
            "KODEX_LOCAL_OWNER_PASSWORD": "test '$not_expanded; `not-run`\nsecond line",
        }
        self.environment = {"PATH": os.environ["PATH"], **self.values}

    def prepare(self, environment=None):
        return subprocess.run(["bash", str(self.script), str(self.path)],
                              env=environment or self.environment, capture_output=True, text=True, timeout=10)

    def test_exact_values_private_file_and_reuse(self):
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)
        check = subprocess.run([
            "bash", "-c",
            'source "$1"; kodex_load_local_credentials "$2" || exit; [[ "$KODEX_LOCAL_OWNER_USERNAME" == "$expected_username" && '
            '"$KODEX_LOCAL_OWNER_EMAIL" == "$expected_email" && '
            '"$KODEX_LOCAL_OWNER_PASSWORD" == "$expected_password" ]]', "check", str(self.script.parent / "lib/local-credentials.sh"), str(self.path)],
            env={"PATH": os.environ["PATH"],
                 "expected_username": self.values["KODEX_LOCAL_OWNER_USERNAME"],
                 "expected_email": self.values["KODEX_LOCAL_OWNER_EMAIL"],
                 "expected_password": self.values["KODEX_LOCAL_OWNER_PASSWORD"]},
            capture_output=True, timeout=10)
        self.assertEqual(check.returncode, 0)
        self.assertEqual(self.prepare().returncode, 0)

    def test_mismatch_preserves_original_and_does_not_expose_values(self):
        self.assertEqual(self.prepare().returncode, 0)
        before = self.path.read_bytes()
        result = self.prepare({**self.environment, "KODEX_LOCAL_OWNER_PASSWORD": "different-sensitive-test-value"})
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("different-sensitive-test-value", result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertEqual(self.path.read_bytes(), before)

    def test_partial_environment_and_symlink_fail_closed(self):
        result = self.prepare({"PATH": os.environ["PATH"], "KODEX_LOCAL_OWNER_USERNAME": "test"})
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.path.exists())
        self.path.symlink_to(Path(self.directory.name) / "absent")
        self.assertNotEqual(self.prepare().returncode, 0)
        self.assertFalse((Path(self.directory.name) / "absent").exists())


    def test_parser_rejects_unsafe_files_without_execution_or_values(self):
        self.assertEqual(self.prepare().returncode, 0)
        original = self.path.read_bytes()
        marker = Path(self.directory.name) / "must-not-exist"
        cases = [
            original + b"KODEX_LOCAL_OWNER_PASSWORD=\"duplicate-sensitive\"\n",
            original + b"UNKNOWN=\"sensitive\"\n",
            original.replace(b'KODEX_LOCAL_ADMIN_USERNAME="admin"', b'KODEX_LOCAL_ADMIN_USERNAME=admin'),
            f'KODEX_LOCAL_OWNER_PASSWORD=$(touch {marker})\n'.encode(),
            original.replace(b'KODEX_LOCAL_ADMIN_USERNAME="admin"', b'KODEX_LOCAL_ADMIN_USERNAME="bad\\u0000value"'),
            original + b"#" * 65536,
        ]
        for payload in cases:
            with self.subTest(case=cases.index(payload)):
                self.path.write_bytes(payload)
                result = self.prepare()
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")
                self.assertNotIn("sensitive", result.stderr)
                self.assertFalse(marker.exists())
        self.path.write_bytes(original)
        alias = self.path.with_suffix(".alias")
        os.link(self.path, alias)
        self.assertNotEqual(self.prepare().returncode, 0)
        alias.unlink()
        self.path.chmod(0o644)
        self.assertNotEqual(self.prepare().returncode, 0)
        self.path.unlink()
        os.mkfifo(self.path, 0o600)
        self.assertNotEqual(self.prepare().returncode, 0)

    def test_loader_does_not_partially_assign_on_invalid_tail(self):
        self.assertEqual(self.prepare().returncode, 0)
        with self.path.open("a") as stream:
            stream.write('UNKNOWN="sensitive"\n')
        result = subprocess.run([
            "bash", "-c", 'source "$1"; KODEX_LOCAL_ADMIN_USERNAME=unchanged; '
            'if kodex_load_local_credentials "$2"; then exit 5; fi; '
            '[[ "$KODEX_LOCAL_ADMIN_USERNAME" == unchanged ]]', "check",
            str(self.script.parent / "lib/local-credentials.sh"), str(self.path)],
            env={"PATH": os.environ["PATH"]}, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertNotIn("sensitive", result.stderr)

    def test_access_profile_is_separate_and_literal(self):
        helper = self.script.parent / "local_credentials.py"
        result = subprocess.run(["python3", str(helper), "prepare", "access", str(self.path)],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        values = dict(line.split("=", 1) for line in self.path.read_text().splitlines())
        self.assertEqual(len(values), 4)
        self.assertEqual(json.loads(values["KODEX_ACCESS_ADMIN_USERNAME"]), "kodex-debug-admin")
        self.assertNotEqual(self.prepare().returncode, 0)


if __name__ == "__main__":
    unittest.main()
