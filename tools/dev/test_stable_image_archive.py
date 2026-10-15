"""Проверка штатных platform OCI на одноразовых синтетических архивах."""

import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class StableArchive(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-stable-archive-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive = self.root / "image.oci.tar"
        self.receipt_path = self.root / "receipt.json"
        self.output = self.root / "verified.tar"
        self.members = {"oci-layout": b'{"imageLayoutVersion":"1.0.0"}'}
        self.component = "control-plane"
        self.source = "a" * 40
        self.user = "10001:10001"

    def blob(self, value, media):
        digest = hashlib.sha256(value).hexdigest()
        self.members["blobs/sha256/" + digest] = value
        return {"digest": "sha256:" + digest, "mediaType": media, "size": len(value)}

    def prepare(self):
        entrypoint = self.component + "-entrypoint" if self.component == "staff-control-center" else self.component
        config = self.blob(json.dumps({"architecture": "amd64", "os": "linux", "config": {
            "Entrypoint": ["/usr/local/bin/" + entrypoint], "User": self.user,
            "Labels": {"org.opencontainers.image.revision": self.source}}}).encode(),
            "application/vnd.oci.image.config.v1+json")
        self.layer = self.blob(b"synthetic platform layer", "application/vnd.oci.image.layer.v1.tar")
        manifest = self.blob(json.dumps({"schemaVersion": 2, "config": config, "layers": [self.layer]}).encode(),
            "application/vnd.oci.image.manifest.v1+json")
        manifest["annotations"] = {"org.opencontainers.image.ref.name": self.source,
            "io.containerd.image.name": "registry.local.kodex/kodex/" + self.component + ":" + self.source}
        self.members["index.json"] = json.dumps({"schemaVersion": 2, "manifests": [manifest]}).encode()
        recipes = json.loads((ROOT / "tools/release/images.json").read_text())["images"]
        recipe = next(image for image in recipes if image["component"] == self.component)
        self.receipt = {"schema": "kodex.stable-local-build/v1", "component": self.component,
            "sourceSHA": self.source, "contextSHA256": "b" * 64, "imageDigest": manifest["digest"],
            "dockerfile": recipe["dockerfile"], "target": recipe.get("target", ""),
            "admission": "NOT_RUN", "publication": "NOT_RUN", "nodePull": "NOT_RUN"}
        self.save()

    def save(self):
        with tarfile.open(self.archive, "w") as archive:
            for name, value in self.members.items():
                info = tarfile.TarInfo(name)
                info.size = len(value)
                archive.addfile(info, io.BytesIO(value))
        self.archive.chmod(0o600)
        self.receipt["archiveSHA256"] = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        self.save_receipt()

    def save_receipt(self):
        self.receipt_path.write_text(json.dumps(self.receipt))
        self.receipt_path.chmod(0o600)

    def verify(self):
        return subprocess.run(["python3", str(ROOT / "tools/dev/verify-platform-image-archive.py"), "--stable",
            str(self.receipt_path), str(self.archive), str(self.output)], capture_output=True, text=True, timeout=5)

    def test_compiled_platform_archive_preserves_bytes(self):
        self.prepare()
        result = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.output.read_bytes(), self.archive.read_bytes())
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o400)

    def test_frontend_entrypoint_and_user_are_supported(self):
        self.component, self.user = "staff-control-center", "101:101"
        self.prepare()
        self.assertEqual(self.verify().returncode, 0)

    def test_standalone_registry_authorizer_recipe_is_supported(self):
        self.component = "registry-pull-authorizer"
        self.prepare()
        self.assertEqual(self.receipt["target"], "registry-pull-runtime")
        self.assertEqual(self.verify().returncode, 0)

    def test_corrupt_layer_rejected_even_with_matching_archive_receipt(self):
        self.prepare()
        self.members["blobs/sha256/" + self.layer["digest"][7:]] = b"PRIVATE_CORRUPT_LAYER_SENTINEL"
        self.save()
        result = self.verify()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("PRIVATE_CORRUPT_LAYER_SENTINEL", result.stdout + result.stderr)

    def test_unknown_recipe_component_and_claimed_results_rejected(self):
        self.prepare()
        for key, value in [("component", "agent-runner"), ("target", "foreign"), ("publication", "PASS"),
                           ("dockerfile", "foreign/Dockerfile")]:
            original = self.receipt[key]
            self.receipt[key] = value
            self.save_receipt()
            self.assertNotEqual(self.verify().returncode, 0)
            self.receipt[key] = original

    def test_receipt_and_archive_symlinks_and_broad_modes_rejected(self):
        self.prepare()
        self.receipt_path.chmod(0o644)
        self.assertNotEqual(self.verify().returncode, 0)
        self.receipt_path.chmod(0o600)
        original = self.root / "original.tar"
        self.archive.rename(original)
        self.archive.symlink_to(original)
        self.assertNotEqual(self.verify().returncode, 0)
        self.archive.unlink()
        original.rename(self.archive)
        os.link(self.archive, self.root / "hardlink.tar")
        self.assertNotEqual(self.verify().returncode, 0)

    def test_hidden_second_archive_and_root_user_rejected(self):
        self.prepare()
        with self.archive.open("ab") as archive:
            archive.write(b"PRIVATE_TRAILING_ARCHIVE_SENTINEL")
        self.receipt["archiveSHA256"] = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        self.save_receipt()
        self.assertNotEqual(self.verify().returncode, 0)
        self.output.unlink()
        self.user = "0:0"
        self.prepare()
        self.assertNotEqual(self.verify().returncode, 0)

    def test_tar_path_traversal_is_not_an_oci_blob(self):
        self.prepare()
        self.members["../foreign"] = b"not allowed"
        self.save()
        self.assertNotEqual(self.verify().returncode, 0)


if __name__ == "__main__":
    unittest.main()
