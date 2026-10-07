"""Проверки очистки на одноразовых Git worktree внутри локальной .cache."""

import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location(
    "cleanup_completed_worktrees", Path(__file__).with_name("cleanup-completed-worktrees.py"))
CLEANUP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CLEANUP)


class CleanupCompletedWorktrees(unittest.TestCase):
    def setUp(self):
        cache = Path.home() / ".cache"
        cache.mkdir(exist_ok=True)
        temporary = tempfile.TemporaryDirectory(prefix="cleanup-fixture-", dir=cache)
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.git("init", "--quiet")
        self.git("config", "user.name", "Synthetic Operator")
        self.git("config", "user.email", "fixture@example.invalid")
        (self.repository / "fixture.txt").write_text("synthetic\n")
        (self.repository / ".gitignore").write_text("ignored-cache/\n")
        self.git("add", "fixture.txt", ".gitignore")
        self.git("commit", "--quiet", "-m", "Одноразовая проверка")
        self.head = self.git("rev-parse", "HEAD").strip()
        self.target = self.root / "worktree"
        self.git("worktree", "add", "--quiet", "--detach", str(self.target), self.head)
        self.ref = "refs/kodex/cleanup-preserved/" + self.head

    def git(self, *arguments):
        result = subprocess.run(["git", "-C", str(self.repository), *arguments],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
                                env={"PATH": os.environ.get("PATH", "/usr/bin:/bin"),
                                     "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"})
        return result.stdout.decode()

    def run_cleanup(self, *, target=None, head=None, apply=False):
        return CLEANUP._cleanup(self.repository, str(target or self.target), head or self.head,
                                apply=apply, permitted_root=self.root)

    def test_dry_run_changes_neither_tree_nor_ref(self):
        result = self.run_cleanup()
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["refStatus"], "ABSENT")
        self.assertTrue(self.target.is_dir())
        self.assertNotIn(self.ref, self.git("show-ref"))

    def test_apply_preserves_ref_removes_and_allows_restore(self):
        result = self.run_cleanup(apply=True)
        self.assertEqual(result["refStatus"], "PRESERVED")
        self.assertFalse(self.target.exists())
        self.assertNotIn(str(self.target), self.git("worktree", "list", "--porcelain"))
        self.assertEqual(self.git("show-ref", "--verify", "--hash", self.ref).strip(), self.head)
        self.git("worktree", "add", "--quiet", "--detach", str(self.target), self.head)
        self.assertEqual((self.target / "fixture.txt").read_text(), "synthetic\n")

    def test_mismatched_head_fails_closed(self):
        with self.assertRaises(CLEANUP.Failure):
            self.run_cleanup(head="1" * 40, apply=True)
        self.assertTrue(self.target.exists())

    def test_dirty_untracked_and_ignored_fail_closed(self):
        for name in ("fixture.txt", "untracked.txt", "ignored-cache/item"):
            with self.subTest(name=name):
                path = self.target / name
                path.parent.mkdir(exist_ok=True)
                path.write_text("changed\n")
                with self.assertRaisesRegex(CLEANUP.Failure, "WORKTREE_NOT_CLEAN"):
                    self.run_cleanup(apply=True)
                self.assertNotIn(self.ref, self.git("show-ref"))
                if name == "fixture.txt":
                    path.write_text("synthetic\n")
                else:
                    path.unlink()
                    if name.startswith("ignored-cache/"):
                        path.parent.rmdir()

    def test_nonregistered_and_symlink_fail_closed(self):
        foreign = self.root / "foreign"
        foreign.mkdir()
        (foreign / ".git").write_text("not a worktree\n")
        with self.assertRaisesRegex(CLEANUP.Failure, "WORKTREE_NOT_REGISTERED"):
            self.run_cleanup(target=foreign, apply=True)
        alias = self.root / "alias"
        alias.symlink_to(self.target)
        with self.assertRaisesRegex(CLEANUP.Failure, "SYMLINK_PATH_FORBIDDEN"):
            self.run_cleanup(target=alias, apply=True)
        self.assertTrue(self.target.exists())

    def test_credentials_are_rejected_without_reading(self):
        secret = self.target / ".env"
        secret.touch(mode=0o000)
        with self.assertRaisesRegex(CLEANUP.Failure, "CREDENTIAL_PATH_FORBIDDEN"):
            self.run_cleanup(apply=True)

    def test_conflicting_preserved_ref_fails_closed(self):
        (self.repository / "fixture.txt").write_text("another commit\n")
        self.git("commit", "--quiet", "-am", "Другая одноразовая ревизия")
        other = self.git("rev-parse", "HEAD").strip()
        self.git("update-ref", self.ref, other)
        with self.assertRaisesRegex(CLEANUP.Failure, "PRESERVED_REF_MISMATCH"):
            self.run_cleanup(apply=True)
        self.assertTrue(self.target.exists())

    def test_production_policy_rejects_cache_fixture(self):
        with self.assertRaisesRegex(CLEANUP.Failure, "TARGET_OUTSIDE_TRUSTED_ROOT"):
            CLEANUP._cleanup(self.repository, str(self.target), self.head, apply=True)

    def test_symbolic_preserved_ref_fails_closed(self):
        branch = self.git("symbolic-ref", "HEAD").strip()
        self.git("symbolic-ref", self.ref, branch)
        with self.assertRaisesRegex(CLEANUP.Failure, "PRESERVED_REF_NOT_DIRECT"):
            self.run_cleanup(apply=True)
        self.assertTrue(self.target.exists())


if __name__ == "__main__":
    unittest.main()
