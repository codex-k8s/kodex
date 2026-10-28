"""Фактический yq render backup revision из exact release lock, без кластера."""

import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/release/pin-backup-controller-release.sh"
DIGEST = "sha256:" + "a" * 64
IMAGE = "registry.local.kodex/kodex/backup-controller@" + DIGEST


class BackupReleasePin(unittest.TestCase):
    def test_exact_image_revision_and_foreign_unit(self):
        self.run_case("")

    def test_invalid_locks_missing_or_duplicate_revision_and_image_mismatch(self):
        for defect in ("zero", "missing", "duplicate", "image", "duplicate-lock", "missing-unit"):
            with self.subTest(defect=defect):
                self.run_case(defect)

    def run_case(self, defect):
        with tempfile.TemporaryDirectory(prefix="backup-release-pin-") as directory:
            root = Path(directory)
            backup = json.loads(subprocess.check_output(["yq", "-o=json", ".",
                str(ROOT / "deploy/k8s/base/backup-controller/deployment.yaml")], text=True))
            container = backup["spec"]["template"]["spec"]["containers"][0]
            container["image"] = IMAGE
            sibling = copy.deepcopy(backup)
            sibling["metadata"]["name"] = "foreign-unit"
            images = [{"component": "backup-controller", "digest": DIGEST, "pull_ref": IMAGE}]
            if defect == "zero":
                images[0]["digest"] = "sha256:" + "0" * 64
            if defect == "missing":
                container["env"] = [v for v in container["env"] if v["name"] != "BACKUP_CONTROLLER_RELEASE_REVISION"]
            if defect == "duplicate":
                container["env"].append({"name": "BACKUP_CONTROLLER_RELEASE_REVISION", "value": "foreign"})
            if defect == "image":
                container["image"] = IMAGE[:-1] + "b"
            if defect == "duplicate-lock":
                images += copy.deepcopy(images)
            lock, render = root / "lock.json", root / "render.yaml"
            lock.write_text(json.dumps({"images": images}))
            render.write_text(("" if defect == "missing-unit" else json.dumps(backup) + "\n---\n") + json.dumps(sibling))
            before = render.read_bytes()
            result = subprocess.run(["bash", str(SCRIPT), str(lock), str(render)], capture_output=True, timeout=10)
            if defect:
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(render.read_bytes(), before)
            else:
                self.assertEqual(result.returncode, 0, result.stderr)
                documents = subprocess.check_output(["yq", "-o=json", "-I=0", ".", str(render)], text=True)
                values = [json.loads(line) for line in documents.splitlines()]
                self.assertEqual(values[1], sibling)
                env = values[0]["spec"]["template"]["spec"]["containers"][0]["env"]
                self.assertEqual(next(v["value"] for v in env if v["name"] == "BACKUP_CONTROLLER_RELEASE_REVISION"), DIGEST)


if __name__ == "__main__":
    unittest.main()
