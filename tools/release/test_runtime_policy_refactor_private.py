"""Проверки приватных артефактов и отказа без разрушительного повтора."""

import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


private = load("policy_private", "runtime-policy-refactor-private.py")


class RuntimePolicyPrivateTest(unittest.TestCase):
    def test_roundtrip_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            target = root / "proof.json"
            private.create_private(target, {"status": "INTENT", "revision": 1})
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            self.assertEqual(private.read_private(target), {"status": "INTENT", "revision": 1})
            with self.assertRaises(FileExistsError):
                private.create_private(target, {"status": "OTHER"})
            self.assertEqual(private.read_private(target)["status"], "INTENT")

    def test_duplicate_keys_links_and_fifo_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            target = root / "proof.json"
            target.write_text('{"x":1,"x":2}')
            target.chmod(0o600)
            with self.assertRaises(private.PrivateArtifactError):
                private.read_private(target)
            target.write_text('{"x":NaN}')
            with self.assertRaises(private.PrivateArtifactError):
                private.read_private(target)
            target.write_text(json.dumps({"x": 1}))
            link = root / "hardlink.json"
            os.link(target, link)
            with self.assertRaises(private.PrivateArtifactError):
                private.read_private(target)
            link.unlink()
            symlink = root / "symlink.json"
            symlink.symlink_to(target)
            with self.assertRaises(OSError):
                private.read_private(symlink)
            fifo = root / "fifo.json"
            os.mkfifo(fifo, 0o600)
            with self.assertRaises(private.PrivateArtifactError):
                private.read_private(fifo)

    def test_unsafe_parent_and_symlink_parent_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o755)
            with self.assertRaises(private.PrivateArtifactError):
                private.create_private(root / "proof.json", {})
            root.chmod(0o700)
            with self.assertRaises(ValueError):
                private.create_private(root / "nan.json", {"value": float("nan")})
            self.assertFalse((root / "nan.json").exists())
            alias = root.parent / (root.name + "-alias")
            alias.symlink_to(root, target_is_directory=True)
            try:
                with self.assertRaises(private.PrivateArtifactError):
                    private.create_private(alias / "proof.json", {})
            finally:
                alias.unlink()

    def test_directory_fsync_failure_preserves_uncertain_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            target = root / "intent.json"
            real_fsync = os.fsync
            calls = []

            def injected(fd):
                calls.append(fd)
                if len(calls) == 2:
                    raise OSError("synthetic directory fsync failure")
                return real_fsync(fd)

            with mock.patch.object(private.os, "fsync", side_effect=injected):
                with self.assertRaises(OSError):
                    private.create_private(target, {"status": "INTENT"})
            self.assertTrue(target.exists())
            self.assertEqual(private.read_private(target)["status"], "INTENT")
            with self.assertRaises(FileExistsError):
                private.create_private(target, {"status": "INTENT"})

    def test_content_change_after_file_fsync_is_rejected_and_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            target = root / "intent.json"
            real_fsync = os.fsync
            calls = []

            def injected(fd):
                calls.append(fd)
                real_fsync(fd)
                if len(calls) == 1:
                    target.write_text('{"status":"OTHER"}\n')

            with mock.patch.object(private.os, "fsync", side_effect=injected):
                with self.assertRaises(private.PrivateArtifactError):
                    private.create_private(target, {"status": "INTENT"})
            self.assertTrue(target.exists())
            self.assertEqual(private.read_private(target)["status"], "OTHER")


if __name__ == "__main__":
    unittest.main()
