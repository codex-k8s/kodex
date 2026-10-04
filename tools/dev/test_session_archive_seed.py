"""Проверка preserved platform OCI и публикации без Docker, сети и node cache."""

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
SEED = ROOT / "tools/dev/seed-local-image-supply-chain.sh"


class PlatformArchiveSeed(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-archive-seed-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.cache = self.root / "cache"
        self.cache.mkdir(mode=0o700)
        self.output = self.root / "verified.oci.tar"
        self.members = {"oci-layout": b'{"imageLayoutVersion":"1.0.0"}'}
        self.layer = self.blob(b"synthetic preserved layer", "application/vnd.oci.image.layer.v1.tar")
        config = self.blob(json.dumps({"os": "linux", "architecture": "amd64", "config": {
            "Entrypoint": ["/usr/local/bin/session-archive"]}}).encode(),
            "application/vnd.oci.image.config.v1+json")
        manifest = self.blob(json.dumps({"schemaVersion": 2, "config": config,
            "layers": [self.layer]}).encode(), "application/vnd.oci.image.manifest.v1+json")
        self.digest = manifest["digest"]
        self.input_digest = "b" * 64
        manifest["annotations"] = {
            "org.opencontainers.image.ref.name": "local-" + self.input_digest,
            "io.containerd.image.name": "registry.local.kodex/kodex/session-archive:local-" + self.input_digest,
        }
        self.members["index.json"] = json.dumps({"schemaVersion": 2, "manifests": [manifest]}).encode()
        self.archive = self.cache / ("session-archive-" + self.input_digest + ".oci.tar")

    def blob(self, data, media_type):
        digest = "sha256:" + hashlib.sha256(data).hexdigest()
        self.members["blobs/sha256/" + digest[7:]] = data
        return {"mediaType": media_type, "digest": digest, "size": len(data)}

    def write_archive(self):
        with tarfile.open(self.archive, "w") as archive:
            for name, value in self.members.items():
                member = tarfile.TarInfo(name)
                member.size = len(value)
                archive.addfile(member, io.BytesIO(value))
        self.archive.chmod(0o600)

    def verify(self, expected=None):
        source = SEED.read_text()
        script = source.split("<<'PY_ARCHIVE'", 1)[1].split("\n", 1)[1].split("\nPY_ARCHIVE", 1)[0]
        return subprocess.run(["python3", "-", str(self.cache), expected or self.digest, str(self.output)],
            input=script, capture_output=True, text=True, timeout=5)

    def test_preserved_bytes_without_any_node_image_cache(self):
        self.write_archive()
        result = self.verify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.output.read_bytes(), self.archive.read_bytes())
        self.assertEqual(self.output.stat().st_mode & 0o777, 0o400)

    def test_corrupt_layer_never_becomes_verified(self):
        self.members["blobs/sha256/" + self.layer["digest"][7:]] = b"SENTINEL_PRIVATE_PAYLOAD"
        self.write_archive()
        result = self.verify()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("SENTINEL_PRIVATE_PAYLOAD", result.stdout + result.stderr)

    def test_wrong_digest_and_missing_archive_fail_closed(self):
        self.assertNotEqual(self.verify().returncode, 0)
        self.write_archive()
        self.assertNotEqual(self.verify("sha256:" + "a" * 64).returncode, 0)

    def test_foreign_repository_annotation_rejected(self):
        index = json.loads(self.members["index.json"])
        index["manifests"][0]["annotations"]["io.containerd.image.name"] = "foreign/image:latest"
        self.members["index.json"] = json.dumps(index).encode()
        self.write_archive()
        self.assertNotEqual(self.verify().returncode, 0)

    def test_symlink_and_hidden_second_archive_rejected(self):
        self.write_archive()
        original = self.root / "original.tar"
        self.archive.rename(original)
        self.archive.symlink_to(original)
        self.assertNotEqual(self.verify().returncode, 0)
        self.archive.unlink()
        original.rename(self.archive)
        with self.archive.open("ab") as archive:
            archive.write(b"SENTINEL_HIDDEN_ARCHIVE")
        result = self.verify()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("SENTINEL_HIDDEN_ARCHIVE", result.stdout + result.stderr)

    def test_promotion_and_render_use_one_exact_platform_digest(self):
        seed = SEED.read_text()
        renderer = (ROOT / "tools/dev/render-local.sh").read_text()
        self.assertIn('regctl image import "$target/kodex/session-archive:local-platform" /input/session-archive.oci.tar', seed)
        self.assertIn('regctl image digest "$target/kodex/session-archive@$KODEX_SESSION_ARCHIVE_DIGEST"', seed)
        self.assertIn('runtime_session_archive_image="$promoted_pull_host/kodex/session-archive@$session_archive_digest"', renderer)
        self.assertIn('SESSION_ARCHIVE_IMAGE="$runtime_session_archive_image"', renderer)
        self.assertNotIn('SESSION_ARCHIVE_IMAGE="$session_archive_image"', renderer)
        self.assertLess(seed.index("PY_ARCHIVE\n"), seed.index("get secret/kodex-image-promotion-writer"))

    def test_actual_renderer_sets_controller_and_worker_pin(self):
        source = (ROOT / "tools/dev/render-local.sh").read_text()
        fragment = source.split('SESSION_ARCHIVE_IMAGE="$runtime_session_archive_image"', 1)[1]
        expression = fragment.split("yq -i '", 1)[1].split("\n' \"$render\"", 1)[0]
        expected = "pull.fixture.invalid/kodex/session-archive@" + self.digest
        fixture = self.root / "render.json"
        fixture.write_text(json.dumps({"kind": "Deployment", "metadata": {"name": "session-archive"},
            "spec": {"template": {"spec": {"containers": [{"name": "session-archive", "image": "missing-node-cache",
                "env": [{"name": "SESSION_ARCHIVE_WORKER_IMAGE", "value": "missing-node-cache"}]}]}}}}))
        result = subprocess.run(["yq", "-i", expression, str(fixture)], capture_output=True, text=True,
            env={**os.environ, "SESSION_ARCHIVE_IMAGE": expected, "DEPLOYMENT_PROFILE": "web-only",
                "AUTHORITY_SOURCE_REVISION": "1"}, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        projected = subprocess.run(["yq", "-o=json", ".", str(fixture)], capture_output=True, text=True, timeout=5)
        self.assertEqual(projected.returncode, 0, projected.stderr)
        container = json.loads(projected.stdout)["spec"]["template"]["spec"]["containers"][0]
        self.assertEqual(container["image"], expected)
        self.assertEqual(container["env"][0]["value"], expected)
        self.assertEqual(container["imagePullPolicy"], "IfNotPresent")

    def test_private_cache_guard_and_group_writable_owned_file(self):
        self.write_archive()
        self.archive.chmod(0o664)
        self.assertEqual(self.verify().returncode, 0)
        self.output.unlink()
        self.cache.chmod(0o770)
        self.assertNotEqual(self.verify().returncode, 0)


if __name__ == "__main__":
    unittest.main()
