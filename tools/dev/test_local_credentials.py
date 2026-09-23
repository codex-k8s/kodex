"""Проверки bootstrap SSO на синтетических credentials без живого окружения."""

import os
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
            'source "$1"; [[ "$KODEX_LOCAL_OWNER_USERNAME" == "$expected_username" && '
            '"$KODEX_LOCAL_OWNER_EMAIL" == "$expected_email" && '
            '"$KODEX_LOCAL_OWNER_PASSWORD" == "$expected_password" ]]', "check", str(self.path)],
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


if __name__ == "__main__":
    unittest.main()
