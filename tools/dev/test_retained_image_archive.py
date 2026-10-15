"""Сохранение exact служебного OCI не подменяется новым build receipt."""

import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class RetainedArchive(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-retained-archive-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.component = "image-admission"
        self.key = "b" * 64
        self.output = self.root / "verified.tar"

    def prepare(self, user="10001:10001", orphan=False, corrupt=False):
        self.members = {"oci-layout": b'{"imageLayoutVersion":"1.0.0"}'}
        def blob(value, media):
            encoded = value if isinstance(value, bytes) else json.dumps(value).encode()
            digest = hashlib.sha256(encoded).hexdigest()
            self.members["blobs/sha256/" + digest] = encoded
            return {"digest": "sha256:" + digest, "size": len(encoded), "mediaType": media}
        config = blob({"architecture": "amd64", "os": "linux", "config": {"User": user, "Cmd": ["/bin/sh"],
            "Labels": {"org.opencontainers.image.revision": "a" * 40}}}, "application/vnd.oci.image.config.v1+json")
        layer = blob(b"synthetic retained layer", "application/vnd.oci.image.layer.v1.tar")
        manifest = blob({"schemaVersion": 2, "config": config, "layers": [layer]}, "application/vnd.oci.image.manifest.v1+json")
        self.digest = manifest["digest"]
        manifest["annotations"] = {"org.opencontainers.image.ref.name": "local-" + self.key,
            "io.containerd.image.name": "registry.local.kodex/kodex/" + self.component + ":local-" + self.key}
        self.members["index.json"] = json.dumps({"schemaVersion": 2, "manifests": [manifest]}).encode()
        if orphan:
            blob(b"SENTINEL_UNREFERENCED_PRIVATE_BLOB", "application/vnd.oci.image.layer.v1.tar")
        if corrupt:
            self.members["blobs/sha256/" + layer["digest"][7:]] = b"SENTINEL_CORRUPT_PRIVATE_BLOB"
        self.archive = self.root / (self.component + "-" + self.key + ".oci.tar")
        with tarfile.open(self.archive, "w") as archive:
            for name, value in self.members.items():
                info = tarfile.TarInfo(name)
                info.size = len(value)
                archive.addfile(info, io.BytesIO(value))
        self.archive.chmod(0o664)

    def verify(self, component=None, digest=None):
        return subprocess.run(["python3", str(ROOT / "tools/dev/verify-platform-image-archive.py"), "--retained",
            component or self.component, str(self.archive), digest or self.digest, str(self.output)],
            capture_output=True, text=True, timeout=5)

    def test_both_existing_bundles_are_preserved_without_repacking(self):
        for component in ["image-admission", "image-admission-tools"]:
            self.component = component
            self.prepare()
            result = self.verify()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(self.output.read_bytes(), self.archive.read_bytes())
            self.assertEqual(self.output.stat().st_mode & 0o777, 0o400)
            self.output.unlink()

    def test_wrong_component_digest_and_parent_are_closed(self):
        self.prepare()
        self.assertNotEqual(self.verify("control-plane").returncode, 0)
        self.assertNotEqual(self.verify(digest="sha256:" + "c" * 64).returncode, 0)
        self.output.unlink()
        self.root.chmod(0o770)
        self.assertNotEqual(self.verify().returncode, 0)

    def test_corrupt_or_unreferenced_blob_never_becomes_verified(self):
        for options in [dict(corrupt=True), dict(orphan=True), dict(user="0:0")]:
            self.prepare(**options)
            result = self.verify()
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("SENTINEL_", result.stdout + result.stderr)
            self.output.unlink()


if __name__ == "__main__":
    unittest.main()
