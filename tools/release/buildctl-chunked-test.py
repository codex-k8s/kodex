"""Synthetic subprocess contract для opt-in publish adapter, без registry/credentials среды."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import shutil
import sys
import tempfile
import unittest

ADAPTER = Path(__file__).with_name("buildctl-chunked.py")
DIGEST = "sha256:" + "1" * 64


class AdapterTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="kodex-chunked-contract-")
        self.root = Path(self.directory.name)
        self.root.chmod(0o700)
        self.buildctl = self.root / "buildctl"
        self.buildctl.write_text("""#!/usr/bin/env python3
import json,pathlib,sys
args=sys.argv[1:]
output=dict(p.split('=',1)for p in args[args.index('--output')+1].split(','))
assert output['type']=='oci' and output['oci-mediatypes']=='true'
assert output['attestation-inline']=='true'
pathlib.Path(output['dest']).write_bytes(b'synthetic-oci')
metadata={'containerimage.digest':'sha256:'+'1'*64,'preserved-field':{'proof':'synthetic'}}
pathlib.Path(args[args.index('--metadata-file')+1]).write_text(json.dumps(metadata))
print('synthetic https://registry.example.invalid/v2/upload?_state=SYNTHETIC_UPLOAD_STATE')
""")
        self.publisher = self.root / "publisher"
        self.publisher.write_text("""#!/usr/bin/env python3
import json,pathlib,sys
args=sys.argv[1:]
pathlib.Path(__file__).with_name('publisher-call.json').write_text(json.dumps(args))
print(json.dumps({'digest':args[args.index('--expected-digest')+1],'reused':False}))
""")
        for path in (self.buildctl, self.publisher):
            path.chmod(0o700)
        self.auth = self.root / "docker"
        self.auth.mkdir(mode=0o700)
        secret = self.auth / "config.json"
        secret.write_text(json.dumps({"auths": {"registry.example.invalid": {"auth": "c3ludGhldGljOnN5bnRoZXRpYw=="}}}))
        secret.chmod(0o600)
        self.config = self.root / "config.json"
        self.target = self.root / "result.json"
        self.refresh_config()
        self.env = {"PATH": os.environ["PATH"], "HOME": str(self.root), "DOCKER_CONFIG": str(self.auth),
                    "KODEX_RELEASE_CHUNKED_CONFIG": str(self.config)}

    def tearDown(self):
        self.directory.cleanup()

    def refresh_config(self):
        value = {"schema_version": 1, "chunk_bytes": 8 << 20, "max_archive_bytes": 1 << 20,
                 "timeout_seconds": 10, "ca_file": ""}
        for key, path in (("buildctl", self.buildctl), ("publisher", self.publisher)):
            value[key] = {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
        self.config.write_text(json.dumps(value))
        self.config.chmod(0o600)

    def invoke(self, extra=()):
        args = [sys.executable, "-B", str(ADAPTER), "--addr", "unix:///synthetic/buildkit.sock", "build",
                "--frontend", "dockerfile.v0", "--output", "type=image,name=registry.example.invalid/synthetic/image:candidate,push=true",
                "--metadata-file", str(self.target), *extra]
        return subprocess.run(args, env=self.env, capture_output=True, text=True, timeout=15)

    def test_confirmed_digest_preserves_all_original_metadata(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(self.target.read_text()), {"containerimage.digest": DIGEST, "preserved-field": {"proof": "synthetic"}})
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o600)
        args = json.loads((self.root / "publisher-call.json").read_text())
        self.assertEqual(args[args.index("--docker-config") + 1], str(self.auth / "config.json"))
        self.assertNotIn("c3ludGhldGljOnN5bnRoZXRpYw==", " ".join(args))
        self.assertNotIn("SYNTHETIC_UPLOAD_STATE", result.stdout + result.stderr)
        self.assertFalse(list(self.root.glob(".chunked-oci-*")))

    def test_failed_publish_never_exposes_metadata_or_upstream_payload(self):
        self.publisher.write_text("#!/usr/bin/env python3\nimport sys\nprint('SYNTHETIC_PRIVATE_PAYLOAD',file=sys.stderr)\nsys.exit(1)\n")
        self.refresh_config()
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.target.exists())
        self.assertNotIn("SYNTHETIC_PRIVATE_PAYLOAD", result.stdout + result.stderr)
        evidence = list(self.root.glob(".chunked-oci-*"))
        self.assertEqual(len(evidence), 1)
        self.assertTrue((evidence[0] / "image.tar").is_file())
        self.assertEqual(json.loads((evidence[0] / "failure.json").read_text())["stage"], "publication")
        self.assertEqual((evidence[0] / "failure.json").stat().st_mode & 0o777, 0o600)

    def test_buildctl_explicit_metadata_permissions_are_restricted(self):
        self.buildctl.write_text(self.buildctl.read_text() + "\npathlib.Path(args[args.index('--metadata-file')+1]).chmod(0o666)\n")
        self.refresh_config()
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.target.stat().st_mode & 0o777, 0o600)

    def test_unconfirmed_digest_never_becomes_build_metadata(self):
        self.publisher.write_text("#!/usr/bin/env python3\nimport json\nprint(json.dumps({'digest':'sha256:'+'2'*64}))\n")
        self.refresh_config()
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.target.exists())

    def test_failed_evidence_survives_caller_metadata_cleanup(self):
        metadata_root = self.root / "caller-temporary-metadata"
        metadata_root.mkdir(mode=0o700)
        self.target = metadata_root / "result.json"
        self.publisher.write_text("#!/usr/bin/env python3\nimport sys\nsys.exit(1)\n")
        self.refresh_config()
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        shutil.rmtree(metadata_root)
        evidence = list(self.root.glob(".chunked-oci-*"))
        self.assertEqual(len(evidence), 1)
        self.assertTrue((evidence[0] / "image.tar").is_file())
        self.assertTrue((evidence[0] / "failure.json").is_file())

    def test_changed_tool_binary_is_rejected_before_execution(self):
        self.buildctl.write_text(self.buildctl.read_text() + "\n# changed source\n")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.target.exists())
        self.assertFalse((self.root / "publisher-call.json").exists())

    def test_existing_metadata_is_not_overwritten(self):
        self.target.write_text("synthetic-previous-result")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.target.read_text(), "synthetic-previous-result")

    def test_additional_secret_mount_is_rejected(self):
        result = self.invoke(("--secret=synthetic-forbidden",))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "publisher-call.json").exists())

    def test_alternate_exporter_argument_is_rejected(self):
        result = self.invoke(("--output=type=image,name=other.example.invalid/image:tag,push=true",))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "publisher-call.json").exists())

    def test_private_config_cannot_be_a_symlink(self):
        original = self.root / "original.json"
        self.config.rename(original)
        self.config.symlink_to(original)
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "publisher-call.json").exists())


if __name__ == "__main__":
    unittest.main()
