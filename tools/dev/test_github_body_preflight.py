import importlib.util
import os
from pathlib import Path
import sys
import tempfile
import unittest


spec = importlib.util.spec_from_file_location(
    "github_body_preflight", Path(__file__).with_name("github-body-preflight.py")
)
guard = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = guard
spec.loader.exec_module(guard)


class PublicationBodyBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.root.chmod(0o700)
        self.body = self.write("description.md", "Refs #1823. Проверены изменения и unit-тесты.\n")

    def write(self, name, text):
        path = self.root / name
        path.write_text(text)
        path.chmod(0o600)
        return path

    def test_valid_body_is_immutable_and_contains_exact_checked_bytes(self):
        result = guard.verify_body(self.body, self.root)
        self.assertEqual(result.content, self.body.read_bytes())
        self.assertEqual(len(result.sha256), 64)
        self.body.write_text("Changed after validation")
        self.assertNotEqual(result.content, self.body.read_bytes())

    def test_original_wrong_body_file_failure_is_rejected(self):
        wrong_path = self.write(".env", "GIT_BOT_PAT=synthetic-not-a-real-token\n")
        with self.assertRaisesRegex(guard.BodyRejected, "APPROVED_MARKDOWN"):
            guard.verify_body(wrong_path, self.root)

    def test_renamed_dotenv_is_rejected(self):
        self.body.write_text("OPENAI_API_KEY=synthetic-not-a-real-key\n")
        with self.assertRaisesRegex(guard.BodyRejected, "DOTENV_ASSIGNMENTS"):
            guard.verify_body(self.body, self.root)

    def test_symlink_and_hardlink_do_not_hide_the_secret_source(self):
        secret = self.write(".env", "SYNTHETIC_PASSWORD=local-fixture\n")
        self.body.unlink()
        self.body.symlink_to(secret)
        with self.assertRaises(guard.BodyRejected):
            guard.verify_body(self.body, self.root)
        self.body.unlink()
        os.link(secret, self.body)
        with self.assertRaises(guard.BodyRejected):
            guard.verify_body(self.body, self.root)

    def test_known_secret_is_rejected_even_without_assignment(self):
        secret = self.write(".env", "SERVICE_PASSWORD='synthetic unique credential'\n")
        self.body.write_text("Text including synthetic unique credential in prose")
        with self.assertRaisesRegex(guard.BodyRejected, "KNOWN_SECRET_VALUE"):
            guard.verify_body(self.body, self.root, secret)

    def test_raw_credential_pattern_is_rejected(self):
        self.body.write_text("Example: " + "ghp_" + "A" * 30)
        with self.assertRaisesRegex(guard.BodyRejected, "CREDENTIAL_PATTERN"):
            guard.verify_body(self.body, self.root)

    def test_wrong_directory_and_public_file_are_rejected(self):
        other = self.root / "other"
        other.mkdir(mode=0o700)
        with self.assertRaises(guard.BodyRejected):
            guard.verify_body(self.body, other)
        self.body.chmod(0o644)
        with self.assertRaises(guard.BodyRejected):
            guard.verify_body(self.body, self.root)

    def test_empty_binary_and_oversize_bodies_are_rejected(self):
        for data in (b"", b"\xff", b"a\0b", b"x" * 65537):
            self.body.write_bytes(data)
            with self.assertRaises(guard.BodyRejected):
                guard.verify_body(self.body, self.root)


if __name__ == "__main__":
    unittest.main()
