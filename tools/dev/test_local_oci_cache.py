"""Безопасное удаление только exact obsolete OCI на одноразовых fixtures."""

import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("local-oci-cache.py")
SPEC = importlib.util.spec_from_file_location("runner_oci_cache", SCRIPT)
CACHE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = CACHE
SPEC.loader.exec_module(CACHE)


class RunnerOciCache(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="kodex-oci-cache-")
        self.addCleanup(temporary.cleanup)
        self.state = Path(temporary.name)
        self.directory = self.state / "cache"
        self.directory.mkdir(mode=0o700)
        (self.directory / "image-supply-chain").mkdir(mode=0o700)
        self.cache = CACHE.Cache(self.state)
        self.kept_digest = "sha256:" + "a" * 64
        self.old_digest = "sha256:" + "b" * 64
        self.kept = self.archive("a", self.kept_digest)
        self.old = self.archive("b", self.old_digest)
        self.pins()
        for index, component in enumerate(CACHE.COMPONENTS, 1):
            if component != "agent-runner":
                pin = self.state / (component + "-image")
                pin.write_text("registry.local.kodex/kodex/" + component + "@sha256:" + f"{index:064x}" + "\n")
                pin.chmod(0o600)

    def pins(self, current=None, restore=None):
        current = current or self.kept_digest
        restore = restore or self.kept_digest
        pin = self.state / "agent-runner-image"
        pin.write_text(CACHE.RUNNER + current + "\n")
        pin.chmod(0o600)
        archive = self.kept if restore == self.kept_digest else self.old
        value = [{"name": "agent-runner", "archive": str(archive),
                  "repo": CACHE.RUNNER[:-1], "ref": CACHE.RUNNER + restore,
                  "tag": CACHE.RUNNER[:-1] + ":local-fixture"}]
        path = self.state / CACHE.RESTORE
        path.write_text(json.dumps(value))
        path.chmod(0o600)

    def archive(self, key, digest, *, extra=None, index=None, component="agent-runner"):
        path = self.directory / CACHE.COMPONENTS[component] / (component + "-" + key * 64 + ".oci.tar")
        value = index or {"schemaVersion": 2, "manifests": [{"digest": digest}]}
        with tarfile.open(path, "w", format=tarfile.USTAR_FORMAT) as archive:
            body = json.dumps(value).encode()
            info = tarfile.TarInfo("index.json")
            info.size = len(body)
            archive.addfile(info, io.BytesIO(body))
            if extra:
                archive.addfile(extra)
        path.chmod(0o664)
        return path

    def target(self, path=None, digest=None):
        return (path or self.old).relative_to(self.directory).as_posix() + "@" + (digest or self.old_digest)

    def test_audit_is_read_only_and_prune_only_explicit_obsolete(self):
        pending = self.directory / (self.old.name + ".next")
        pending.write_bytes(b"partial build")
        metadata = self.directory / "keep.json"
        metadata.write_bytes(b"metadata")
        audit = self.cache.audit()
        self.assertEqual(audit["archiveCount"], 2)
        self.assertEqual(audit["obsoleteBytes"], self.old.stat().st_size)
        self.assertTrue(self.old.exists())
        result = self.cache.prune([self.target()])
        self.assertEqual(len(result["removed"]), 1)
        self.assertFalse(self.old.exists())
        self.assertTrue(self.kept.exists())
        self.assertEqual(pending.read_bytes(), b"partial build")
        self.assertEqual(metadata.read_bytes(), b"metadata")
        self.assertTrue((self.state / CACHE.RESTORE).exists())

    def test_current_and_restore_pins_protected_including_duplicate_digest(self):
        duplicate = self.archive("c", self.kept_digest)
        for target in (self.target(self.kept, self.kept_digest), self.target(duplicate, self.kept_digest)):
            with self.assertRaisesRegex(CACHE.Failure, "ARCHIVE_PROTECTED"):
                self.cache.prune([target])
        self.pins(restore=self.old_digest)
        with self.assertRaisesRegex(CACHE.Failure, "ARCHIVE_PROTECTED"):
            self.cache.prune([self.target()])

    def test_exact_all_nine_components_and_parents_keep_current_and_prune_only_selected(self):
        targets = []
        for index, component in enumerate(CACHE.COMPONENTS, 1):
            if component == "agent-runner":
                targets.append(self.target())
                continue
            current = "sha256:" + f"{index:064x}"
            self.archive("a", current, component=component)
            old = self.archive("b", self.old_digest, component=component)
            targets.append(self.target(old))
        audit = self.cache.audit()
        self.assertEqual(audit["archiveCount"], 18)
        self.assertEqual(sum(entry["state"] == "KEEP" for entry in audit["archives"]), 9)
        self.assertEqual(set(entry["component"] for entry in audit["archives"]), set(CACHE.COMPONENTS))
        result = self.cache.prune(targets)
        self.assertEqual(len(result["removed"]), 9)
        self.assertEqual(self.cache.audit()["archiveCount"], 9)

    def test_new_runner_and_previous_restore_manifest_both_survive(self):
        digest = "sha256:72b27d82bc3583995e870ab7a134153404b856d89a26b06716eaf748f2588104"
        fresh = self.archive("c", digest)
        self.pins(current=digest, restore=self.kept_digest)
        audit = self.cache.audit()
        self.assertEqual(sum(entry["state"] == "KEEP" for entry in audit["archives"]), 2)
        self.cache.prune([self.target()])
        self.assertTrue(fresh.exists())
        self.assertTrue(self.kept.exists())

    def test_unknown_component_wrong_parent_and_public_scope_override_closed(self):
        for name in ("other-service-" + "b" * 64 + ".oci.tar",
                     "role-input-" + "b" * 40 + ".oci.tar",
                     "image-supply-chain/session-archive-" + "b" * 64 + ".oci.tar",
                     "image-admission-" + "b" * 64 + ".oci.tar",
                     "image-supply-chain/../" + self.old.name):
            with self.assertRaisesRegex(CACHE.Failure, "TARGET_INVALID"):
                self.cache.prune([name + "@" + self.old_digest])
        self.assertTrue(self.old.exists())

    def test_other_component_current_pin_change_blocks_runner_unlink(self):
        original = self.cache.pins
        count = 0
        def changed():
            nonlocal count
            count += 1
            if count == 2:
                pin = self.state / "image-admission-image"
                pin.write_text("registry.local.kodex/kodex/image-admission@" + self.old_digest + "\n")
            return original()
        with patch.object(self.cache, "pins", side_effect=changed):
            with self.assertRaisesRegex(CACHE.Failure, "PIN_CHANGED"):
                self.cache.prune([self.target()])
        self.assertTrue(self.old.exists())

    def test_nested_parent_symlink_and_readable_private_pin_closed(self):
        directory = self.directory / "image-supply-chain"
        directory.rmdir()
        directory.symlink_to(self.directory, target_is_directory=True)
        with self.assertRaisesRegex(CACHE.Failure, "DIRECTORY_UNSAFE"):
            self.cache.audit()
        directory.unlink()
        directory.mkdir(mode=0o700)
        (self.state / "image-admission-image").chmod(0o644)
        with self.assertRaisesRegex(CACHE.Failure, "PIN_FILE_UNSAFE"):
            self.cache.prune([self.target()])

    def test_missing_targets_traversal_suffix_and_wrong_digest_fail_before_unlink(self):
        for targets in ([], [self.target(), self.target()], ["../" + self.target()],
                        [str(self.old) + "@" + self.old_digest], [self.old.name + ".next@" + self.old_digest],
                        [self.target(digest="sha256:" + "f" * 64)]):
            with self.assertRaises(CACHE.Failure):
                self.cache.prune(targets)
            self.assertTrue(self.old.exists())

    def test_symlink_hardlink_and_foreign_owner_rejected(self):
        self.old.unlink()
        self.old.symlink_to(self.kept)
        with self.assertRaisesRegex(CACHE.Failure, "FILE_UNSAFE"):
            self.cache.prune([self.target()])
        self.old.unlink()
        os.link(self.kept, self.old)
        with self.assertRaisesRegex(CACHE.Failure, "FILE_UNSAFE"):
            self.cache.prune([self.target()])
        self.old.unlink()
        self.archive("b", self.old_digest)
        with patch.object(CACHE.os, "getuid", return_value=os.getuid() + 1):
            with self.assertRaises(CACHE.Failure):
                self.cache.prune([self.target()])

    def test_writable_parent_and_symlink_parent_rejected(self):
        self.directory.chmod(0o770)
        with self.assertRaisesRegex(CACHE.Failure, "DIRECTORY_UNSAFE"):
            self.cache.prune([self.target()])
        self.directory.chmod(0o700)
        moved = self.state / "moved"
        self.directory.rename(moved)
        self.directory.symlink_to(moved, target_is_directory=True)
        with self.assertRaisesRegex(CACHE.Failure, "DIRECTORY_UNSAFE"):
            self.cache.prune([self.target()])

    def test_pin_change_between_preflight_and_unlink_closes(self):
        original = self.cache.pins
        count = 0
        def changed():
            nonlocal count
            count += 1
            if count == 2:
                self.pins(current=self.old_digest)
            return original()
        with patch.object(self.cache, "pins", side_effect=changed):
            with self.assertRaisesRegex(CACHE.Failure, "PIN_CHANGED"):
                self.cache.prune([self.target()])
        self.assertTrue(self.old.exists())

    def test_inode_size_and_manifest_changes_before_unlink_close(self):
        for change in ("inode", "size", "manifest"):
            self.archive("b", self.old_digest)
            original = self.cache.archive
            count = 0
            def changed(fd, name):
                nonlocal count
                count += 1
                if count == 2:
                    if change == "inode":
                        self.old.unlink()
                        self.archive("b", self.old_digest)
                    elif change == "size":
                        with self.old.open("ab") as stream: stream.write(b"x")
                    else:
                        self.archive("b", "sha256:" + "c" * 64)
                return original(fd, name)
            with patch.object(self.cache, "archive", side_effect=changed):
                expected = "TAR_END_INVALID" if change == "size" else "ARCHIVE_CHANGED"
                with self.assertRaisesRegex(CACHE.Failure, expected):
                    self.cache.prune([self.target()])
            self.assertTrue(self.old.exists())

    def test_partial_failure_reports_already_removed_archive_without_continuing(self):
        second = self.archive("c", "sha256:" + "c" * 64)
        original = self.cache.pins
        count = 0
        def changed():
            nonlocal count
            count += 1
            if count == 4:
                self.pins(current="sha256:" + "c" * 64)
            return original()
        with patch.object(self.cache, "pins", side_effect=changed), patch.object(CACHE, "Cache", return_value=self.cache), patch("builtins.print") as output:
            self.assertEqual(CACHE.main(["prune", "--target", self.target(), "--target", self.target(second, "sha256:" + "c" * 64)]), 1)
        report = json.loads(output.call_args.args[0])
        self.assertEqual(report["code"], "PIN_CHANGED")
        self.assertEqual(len(report["removed"]), 1)
        self.assertGreater(report["removedBytes"], 0)
        self.assertFalse(self.old.exists())
        self.assertTrue(second.exists())

    def test_tar_link_duplicate_index_and_oversized_index_rejected(self):
        linked = tarfile.TarInfo("blobs/sha256/" + "c" * 64)
        linked.type = tarfile.SYMTYPE
        linked.linkname = "index.json"
        self.archive("b", self.old_digest, extra=linked)
        with self.assertRaisesRegex(CACHE.Failure, "TAR_MEMBER_UNSAFE"):
            self.cache.prune([self.target()])
        duplicate = tarfile.TarInfo("index.json")
        self.archive("b", self.old_digest, extra=duplicate)
        with self.assertRaisesRegex(CACHE.Failure, "TAR_MEMBER_UNSAFE"):
            self.cache.prune([self.target()])
        self.archive("b", self.old_digest, index={"schemaVersion": 2, "manifests": [{"digest": self.old_digest}], "padding": "x" * CACHE.JSON_LIMIT})
        with self.assertRaisesRegex(CACHE.Failure, "INDEX_SIZE_INVALID"):
            self.cache.prune([self.target()])

    def test_cli_scope_is_fixed_and_default_is_audit(self):
        with patch.object(CACHE, "Cache", return_value=self.cache), patch("builtins.print") as output:
            self.assertEqual(CACHE.main([]), 0)
            self.assertIn('"mode": "AUDIT"', output.call_args.args[0])
            self.assertTrue(self.old.exists())
        result = subprocess.run([sys.executable, str(SCRIPT), "--state-directory", str(self.state)],
                                capture_output=True, text=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
