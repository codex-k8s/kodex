"""Контракт Docker-контекста и cache invalidation backup unit без Docker и кластера."""

import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/dev/build-local-backup-controller.sh"
DOCKERFILE = ROOT / "services/jobs/backup-controller/Dockerfile"
MODULE = ROOT / "services/jobs/backup-controller/go.mod"
STABLE_SCRIPT = ROOT / "tools/release/build-stable-local.sh"
MOCK = r'''#!/usr/bin/env python3
import hashlib, io, json, os, pathlib, sys, tarfile
root = pathlib.Path(os.environ["BACKUP_BUILD_FIXTURE"])
name, args = pathlib.Path(sys.argv[0]).name, sys.argv[1:]
with (root / "calls.jsonl").open("a") as stream:
    stream.write(json.dumps({"name": name, "args": args}) + "\n")
if name in ("ensure-local-buildx-builder.sh", "import-local-image.sh"):
    sys.exit(0)
if name == "docker" and (args == ["buildx", "version"] or args[:2] == ["buildx", "inspect"]):
    sys.exit(0)
assert name == "docker" and args[:2] == ["buildx", "build"], "unexpected fixture command"
output = args[args.index("--output") + 1].split("dest=", 1)[1]
tag = args[args.index("--tag") + 1]
if os.environ.get("BACKUP_EXPECTED_SOURCE"):
    source = os.environ["BACKUP_EXPECTED_SOURCE"]
    assert "SOURCE_SHA=" + source in args and "VERSION=" + source in args
    assert tag == "registry.local.kodex/kodex/backup-controller:" + source
    context = pathlib.Path(args[-1])
    assert not (context / ".env").exists() and not (context / ".git").exists()
    assert (context / "services/jobs/backup-controller/fixture.txt").is_file()
manifest = "sha256:" + hashlib.sha256(tag.encode()).hexdigest()
body = json.dumps({"manifests": [{"digest": manifest}]}).encode()
with tarfile.open(output, "w") as archive:
    entry = tarfile.TarInfo("index.json")
    entry.size = len(body)
    archive.addfile(entry, io.BytesIO(body))
'''


class BackupBuildContract(unittest.TestCase):
    def test_docker_context_closes_all_local_module_replacements(self):
        copied = [(source.rstrip("/"), destination.rstrip("/")) for source, destination in re.findall(
            r"^COPY\s+(\S+)\s+(\S+)\s*$", DOCKERFILE.read_text(), re.M)]
        replacements = re.findall(r"^replace\s+\S+\s+=>\s+(\.\.?/\S+)\s*$",
                                  MODULE.read_text(), re.M)
        self.assertTrue(replacements, "local module fixture is absent")
        for replacement in replacements:
            dependency = (MODULE.parent / replacement).resolve().relative_to(ROOT).as_posix()
            with self.subTest(dependency=dependency):
                self.assertTrue(any((dependency == source and destination == dependency) or
                                    (dependency.startswith(source + "/") and
                                     destination + dependency[len(source):] == dependency)
                                    for source, destination in copied),
                                f"Docker COPY omits local module {dependency}")

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="kodex-backup-build-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.source, self.state, self.bin = [self.root / name for name in ("source", "state", "bin")]
        self.state.mkdir(mode=0o700)
        self.bin.mkdir()
        for name in ("services/jobs/backup-controller", "libs/go/grpcserver", "libs/go/httpserver",
                     "libs/go/objectstorage", "libs/go/observability", "libs/go/securefile",
                     "libs/go/serviceruntime"):
            target = self.source / name
            target.mkdir(parents=True)
            (target / "fixture.txt").write_text(name)
        shutil.copyfile(DOCKERFILE, self.source / "services/jobs/backup-controller/Dockerfile")
        for name in ("docker", "ensure-local-buildx-builder.sh", "import-local-image.sh"):
            target = self.bin / name if name == "docker" else self.source / "tools/dev" / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(MOCK)
            target.chmod(0o755)
        self.env = {"PATH": str(self.bin) + ":" + os.defpath,
                    "BACKUP_BUILD_FIXTURE": str(self.root), "LANG": "C"}

    def build(self):
        result = subprocess.run(["bash", str(SCRIPT), "--source-root", str(self.source),
                                 "--state-directory", str(self.state), "--context", "fixture-only"],
                                env=self.env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        return (self.state / "backup-controller-image").read_text()

    def builds(self):
        trace = self.root / "calls.jsonl"
        if not trace.exists():
            return []
        return [call for call in map(json.loads, trace.read_text().splitlines())
                if call["name"] == "docker" and call["args"][:2] == ["buildx", "build"]]

    def test_objectstorage_source_change_invalidates_cached_archive(self):
        first = self.build()
        dependency = self.source / "libs/go/objectstorage/fixture.txt"
        dependency.write_text(dependency.read_text() + "\nchanged dependency\n")
        second = self.build()
        self.assertEqual(len(self.builds()), 2, "dependency change reused stale cached OCI archive")
        self.assertNotEqual(first, second)
        self.assertEqual(len(list((self.state / "cache").glob("backup-controller-*.oci.tar"))), 2)

    def test_unchanged_source_reuses_exact_archive(self):
        first, second = self.build(), self.build()
        self.assertEqual(first, second)
        self.assertEqual(len(self.builds()), 1)

    def stable_fixture(self):
        for name in ("tools/release/images.json", "tools/release/validate-image-dockerfile-path.sh"):
            target = self.source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)
            target.chmod(0o755 if name.endswith(".sh") else 0o644)
        for args in (["init", "--quiet"], ["add", "."],
                     ["-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
                      "commit", "--quiet", "-m", "synthetic fixture"]):
            subprocess.run(["git", *args], cwd=self.source, env=self.env, check=True,
                           capture_output=True, timeout=5)
        return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=self.source,
                                       env=self.env, text=True).strip()

    def stable_build(self, source, component="backup-controller"):
        return subprocess.run(["bash", str(STABLE_SCRIPT), "--component", component,
                               "--source-sha", source, "--output-directory", str(self.root / "stable")],
                              cwd=self.source, env=dict(self.env, BACKUP_EXPECTED_SOURCE=source),
                              capture_output=True, text=True, timeout=15)

    def test_stable_backup_uses_exact_clean_archive_and_truthful_receipt(self):
        source = self.stable_fixture()
        (self.source / ".env").write_text("NONSECRET_UNTRACKED_FIXTURE")
        result = self.stable_build(source)
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads((self.root / "stable/build.json").read_text())
        self.assertEqual(receipt["component"], "backup-controller")
        self.assertEqual(receipt["sourceSHA"], source)
        context = subprocess.check_output(["git", "archive", source], cwd=self.source, env=self.env)
        self.assertEqual(receipt["contextSHA256"], hashlib.sha256(context).hexdigest())
        archive = self.root / "stable/backup-controller.oci.tar"
        self.assertEqual(receipt["archiveSHA256"], hashlib.sha256(archive.read_bytes()).hexdigest())
        for phase in ("admission", "publication", "nodePull"):
            self.assertEqual(receipt[phase], "NOT_RUN")
        self.assertEqual(len(self.builds()), 1)
        self.assertFalse(any(call["name"] == "import-local-image.sh" for call in
                             map(json.loads, (self.root / "calls.jsonl").read_text().splitlines())))

    def test_stable_backup_rejects_dirty_source_before_build(self):
        source = self.stable_fixture()
        (self.source / "services/jobs/backup-controller/fixture.txt").write_text("changed")
        result = self.stable_build(source)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("clean exact source is required", result.stderr)
        self.assertEqual(self.builds(), [])

    def test_stable_build_rejects_unknown_backup_sibling(self):
        source = self.stable_fixture()
        result = self.stable_build(source, "backup-controller-foreign")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("component is not in the stable platform registry", result.stderr)
        self.assertEqual(self.builds(), [])


if __name__ == "__main__":
    unittest.main()
