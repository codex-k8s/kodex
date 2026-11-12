"""Исполняемая проверка erase shell на disposable деревьях: link, inode, mount и повтор после прерывания."""

from pathlib import Path
import subprocess
import tempfile
import unittest

from installation_storage import ERASE_SCRIPT


class StorageTests(unittest.TestCase):
    def test_internal_symlink_does_not_remove_neighbor_and_rejoin_is_idempotent(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target, neighbor, mounts = root / "owned", root / "neighbor", root / "mountinfo"
            target.mkdir()
            neighbor.mkdir()
            (neighbor / "preserved").write_text("synthetic neighbor")
            (target / "link").symlink_to(neighbor, target_is_directory=True)
            (target / "data").write_text("synthetic owned data")
            mounts.write_text("")
            identity = target.stat()
            command = ["sh", "-s", "--", str(target), str(identity.st_dev), str(identity.st_ino), str(mounts)]
            for _ in range(2):
                subprocess.run(command, input=ERASE_SCRIPT.encode(), capture_output=True, timeout=5, check=True)
                self.assertFalse(target.exists())
                self.assertEqual((neighbor / "preserved").read_text(), "synthetic neighbor")

    def test_replacement_root_and_nested_mount_fail_before_data_deletion(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target, mounts = root / "owned", root / "mountinfo"
            target.mkdir()
            (target / "data").write_text("preserve until identity proven")
            identity = target.stat()
            mounts.write_text("")
            for inode, mountpoint in ((identity.st_ino + 1, ""), (identity.st_ino, str(target / "child")), (identity.st_ino, str(target))):
                mounts.write_text("1 0 0:1 / " + mountpoint + " rw - tmpfs tmpfs rw\n" if mountpoint else "")
                result = subprocess.run(["sh", "-s", "--", str(target), str(identity.st_dev), str(inode), str(mounts)],
                                        input=ERASE_SCRIPT.encode(), capture_output=True, timeout=5)
                self.assertNotEqual(result.returncode, 0)
                self.assertTrue((target / "data").exists())
            link = root / "link"
            link.symlink_to(target, target_is_directory=True)
            result = subprocess.run(["sh", "-s", "--", str(link), str(identity.st_dev), str(identity.st_ino), str(mounts)],
                                    input=ERASE_SCRIPT.encode(), capture_output=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertTrue((target / "data").exists())


if __name__ == "__main__":
    unittest.main()
