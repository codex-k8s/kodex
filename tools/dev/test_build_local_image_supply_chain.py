"""Герметичные проверки параллельных сборок без Docker и доступа к кластеру."""

import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time
import unittest


SCRIPT = Path(__file__).with_name("build-local-image-supply-chain.sh")
REVISION = "a" * 40
IMAGE_NAMES = (
    "image-admission-tools", "image-admission", "role-image-builder",
    "internal-rpc-authority",
)
MOCK = r'''#!/usr/bin/env python3
import fcntl, io, json, os, pathlib, signal, subprocess, sys, tarfile, time
root = pathlib.Path(os.environ["BUILD_FIXTURE"])
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
def event(kind, target="", delta=0, child=0):
    with (root / "events.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        path = root / "active"
        active = int(path.read_text()) if path.exists() else 0
        active += delta
        path.write_text(str(active))
        with (root / "events.jsonl").open("a") as output:
            output.write(json.dumps(dict(kind=kind, target=target, active=active,
                                         pid=os.getpid(), child=child)) + "\n")
        return active
if name == "git":
    print(os.environ.get("BUILD_REVISION", "a" * 40))
elif name == "kubectl":
    if args == ["config", "current-context"]:
        print("k3d-import-fixture" if os.environ.get("IMPORT_K3D") else "synthetic-staging")
    elif "nodes" in args:
        print(json.dumps({"items": [{"metadata": {"name": "k3d-import-fixture-" + node},
              "status": {"nodeInfo": {"operatingSystem": "linux", "architecture": os.environ.get("IMPORT_ARCH", "amd64")}}}
              for node in ("agent-0", "server-0")]}))
    else:
        print(json.dumps({"metadata": {"labels": {
            "app.kubernetes.io/part-of": "kodex", "kodex.dev/environment": "staging"}}}))
elif name == "ensure-local-buildx-builder.sh":
    pass
elif name == "import-local-image.sh":
    repository = args[args.index("--repository") + 1]
    target = repository.rsplit("/", 1)[-1]
    if event("import", target) != 0:
        sys.exit(91)
    if target == os.environ.get("FAIL_IMPORT"):
        sys.exit(92)
    assert args[args.index("--exact-reference") + 1] == repository + "@sha256:" + "b" * 64
elif name == "docker" and args[:2] == ["buildx", "version"]:
    pass
elif name == "k3d":
    if args[:2] == ["image", "import"]:
        event("archive_import")
    elif args == ["node", "list", "-o", "json"]:
        print(json.dumps([{"name": "k3d-import-fixture-server-0", "role": "server", "runtimeLabels": {"k3d.cluster": "import-fixture"}},
                          {"name": "k3d-import-fixture-agent-0", "role": "agent", "runtimeLabels": {"k3d.cluster": os.environ.get("IMPORT_CLUSTER", "import-fixture")}},
                          {"name": "k3d-import-fixture-shadow-server-0", "role": "server", "runtimeLabels": {"k3d.cluster": "import-fixture-shadow"}}]))
    else:
        sys.exit(94)
elif name == "docker" and args[0] == "exec":
    offset = 2 if args[1] == "-i" else 1
    node, command = args[offset], args[offset + 1:]
    if command[0] == "crictl":
        assert command[1:5] == ["--runtime-endpoint", "unix:///run/k3s/containerd/containerd.sock", "--image-endpoint", "unix:///run/k3s/containerd/containerd.sock"]
        pinned = os.environ.get("IMPORT_CRI_PINNED", "true") == "true"
        reference = os.environ["IMPORT_REFERENCE"] if not os.environ.get("IMPORT_CRI_WRONG") else "foreign@sha256:" + "a" * 64
        print(json.dumps({"status": {"pinned": pinned, "repoDigests": [reference], "id": "sha256:" + "d" * 64},
                          "info": {"imageSpec": {"os": "linux", "architecture": "amd64"}}}))
        sys.exit(0)
    assert command[:5] == ["ctr", "--address", "/run/k3s/containerd/containerd.sock", "-n", "k8s.io"]
    command = command[5:]
    if command[:2] == ["images", "import"]:
        assert "--digests" in command and "--platform" in command and "linux/amd64" in command
        assert "io.cri-containerd.image=managed" in command and "io.cri-containerd.pinned=pinned" in command
        assert command[-1] == "-"
        sys.stdin.buffer.read()
        event("node_import", node)
        (root / ("alias-" + node)).unlink(missing_ok=True)
    elif command[:4] == ["images", "tag", "--local", "--force"]:
        assert command[4] == os.environ["IMPORT_REFERENCE"].split("@", 1)[0] + ":cached"
        assert command[5] == os.environ["IMPORT_REFERENCE"]
        event("node_alias", node)
        (root / ("alias-" + node)).write_text("copied-pinned-labels")
    elif command[:2] == ["images", "list"]:
        if node != os.environ.get("MISSING_IMPORT_NODE"):
            digest = os.environ["IMPORT_REFERENCE"].split("@", 1)[1]
            if os.environ.get("IMPORT_WRONG_TARGET"): digest = "sha256:" + "a" * 64
            pin = "pinned" if not os.environ.get("IMPORT_UNPINNED") else "false"
            platform = os.environ.get("IMPORT_PLATFORM", "linux/amd64")
            reference = command[2].removeprefix("name==")
            if os.environ.get("IMPORT_NAMED_ONLY") and "@" in reference:
                if not (root / ("alias-" + node)).exists():
                    sys.exit(0)
            print(reference, "application/vnd.oci.image.manifest.v1+json", digest,
                  "10.0 MiB", platform, "io.cri-containerd.image=managed,io.cri-containerd.pinned=" + pin)
    elif command[:3] == ["images", "check", "--quiet"]:
        if not os.environ.get("IMPORT_INCOMPLETE"):
            print(os.environ["IMPORT_REFERENCE"])
    elif command[:2] == ["content", "get"]:
        event("node_digest_readback", node)
        sys.stdout.write(os.environ["IMPORT_MANIFEST"])
    else:
        sys.exit(95)
elif name == "docker" and args[:2] == ["buildx", "build"]:
    tag = args[args.index("--tag") + 1]
    target = tag.split(":")[0].rsplit("/", 1)[-1]
    if "--load" in args:
        target += "-load"
    child = subprocess.Popen(["sleep", "60"])
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    event("start", target, 1, child.pid)
    try:
        if os.environ.get("BLOCK_BUILD"):
            time.sleep(60)
        if target == os.environ.get("FAIL_BUILD"):
            time.sleep(0.1)
            sys.exit(42)
        if target == os.environ.get("MUTATE_BUILD"):
            with (root / "source/libs/go/fixture.txt").open("a") as source:
                source.write("changed during build\n")
        time.sleep({"image-admission-tools": 0.05,
                    "image-admission": 0.2}.get(target, 0.12))
        if "--output" in args:
            output = args[args.index("--output") + 1].split("dest=", 1)[1]
            data = json.dumps({"manifests": [{"digest": "sha256:" + "b" * 64}]}).encode()
            with tarfile.open(output, "w") as archive:
                info = tarfile.TarInfo("index.json")
                info.size = len(data)
                archive.addfile(info, io.BytesIO(data))
    finally:
        child.terminate()
        child.wait()
        event("finish", target, -1)
else:
    sys.exit(93)
'''


class BuildLocalImageSupplyChainTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="kodex-build-parallel-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / "source"
        self.state = self.root / "state"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.state.mkdir()
        for path in (
            "tools/dev/Dockerfile.local-image-supply-chain",
            "tools/dev/Dockerfile.local-image-supply-chain.dockerignore",
            "infra/dockerfile-frontend/Dockerfile",
            "infra/admission-tools/Dockerfile",
            "tools/render-image-admission-job.sh",
            "services/jobs/role-image-builder/Dockerfile",
            "services/internal/internal-rpc-authority/Dockerfile",
            "libs/go/fixture.txt",
        ):
            target = self.source / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text("synthetic build input\n")
        release = self.source / "tools/release"
        release.mkdir()
        (release / "application-source.mjs").write_text("export function inspectSource() {}\n")
        mock = self.root / "mock.py"
        mock.write_text(MOCK)
        mock.chmod(0o700)
        for name in ("git", "kubectl", "docker", "k3d"):
            (self.bin / name).symlink_to(mock)
        for name in ("ensure-local-buildx-builder.sh", "import-local-image.sh"):
            (self.source / "tools/dev" / name).symlink_to(mock)
        cache = self.state / "cache/image-supply-chain"
        cache.mkdir(parents=True)
        (cache / f"role-input-{REVISION}.oci.tar").write_text("cached synthetic role input")
        (self.state / "role-image-input.json").write_text("{}")
        for name in IMAGE_NAMES:
            (self.state / f"{name}-image").write_text("previous image\n")
        self.environment = dict(os.environ, BUILD_FIXTURE=str(self.root))
        self.environment["PATH"] = str(self.bin) + os.pathsep + os.environ["PATH"]

    def arguments(self, jobs=None, component="all"):
        args = ["bash", str(SCRIPT), "--source-root", str(self.source),
                "--state-directory", str(self.state), "--context", "synthetic-staging",
                "--component", component]
        if jobs is not None:
            args.extend(["--build-jobs", str(jobs)])
        return args

    def run_build(self, jobs=None, component="all", **environment):
        return subprocess.run(self.arguments(jobs, component),
                              env=dict(self.environment, **environment),
                              capture_output=True, text=True, timeout=12)

    def events(self):
        path = self.root / "events.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def assert_no_new_pointers(self):
        for name in IMAGE_NAMES:
            self.assertEqual((self.state / f"{name}-image").read_text(), "previous image\n")
        self.assertFalse([path for path in (self.state / "cache/image-supply-chain").glob("build.*")
                          if path.is_dir()])

    def test_parallel_builds_are_bounded_and_import_after_barrier(self):
        result = self.run_build(2)
        self.assertEqual(result.returncode, 0, result.stderr)
        events = self.events()
        self.assertEqual(max(item["active"] for item in events), 2)
        self.assertEqual(sum(item["kind"] == "start" for item in events), 5)
        last_finish = max(index for index, item in enumerate(events) if item["kind"] == "finish")
        first_import = min(index for index, item in enumerate(events) if item["kind"] == "import")
        self.assertLess(last_finish, first_import)
        for name in IMAGE_NAMES:
            path = self.state / f"{name}-image"
            self.assertTrue(path.read_text().endswith("@sha256:" + "b" * 64 + "\n"))
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
        before = len(events)
        repeated = self.run_build(4)
        self.assertEqual(repeated.returncode, 0, repeated.stderr)
        self.assertEqual([item["target"] for item in self.events()[before:] if item["kind"] == "start"],
                         ["image-admission-tools-load"])
        self.assertEqual([item["target"] for item in self.events()[before:] if item["kind"] == "import"],
                         list(IMAGE_NAMES), "cache hit must restore every exact image, not trust previous pointers")

    def test_commit_only_change_invalidates_versioned_recipe_in_all_profiles(self):
        for component in ("all", "image-admission", "authority-security"):
            with self.subTest(component=component):
                first = self.run_build(4, component=component)
                self.assertEqual(first.returncode, 0, first.stderr)
                before = len(self.events())
                revision = {"all": "c", "image-admission": "d", "authority-security": "e"}[component] * 40
                (self.state / "cache/image-supply-chain" / f"role-input-{revision}.oci.tar").write_text("cached synthetic role input")
                second = self.run_build(4, component=component, BUILD_REVISION=revision)
                self.assertEqual(second.returncode, 0, second.stderr)
                starts = [item["target"] for item in self.events()[before:] if item["kind"] == "start"]
                expected = list(IMAGE_NAMES) + ["image-admission-tools-load"] if component == "all" else (
                    ["image-admission"] if component == "image-admission" else ["internal-rpc-authority", "image-admission"])
                self.assertCountEqual(starts, expected, "new SOURCE_SHA/VERSION must not reuse the old OCI archive")

    def test_default_remains_sequential(self):
        result = self.run_build()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(max(item["active"] for item in self.events()), 1)

    def test_context_allowlist_change_invalidates_oci_cache(self):
        first = self.run_build(1, component="image-admission")
        self.assertEqual(first.returncode, 0, first.stderr)
        before = len(self.events())
        (self.source / "tools/dev/Dockerfile.local-image-supply-chain.dockerignore").write_text("changed fixture rules\n")
        second = self.run_build(1, component="image-admission")
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual([event["target"] for event in self.events()[before:] if event["kind"] == "start"], ["image-admission"])

    def test_cached_import_requires_exact_reference_and_digest_on_every_node(self):
        import hashlib
        manifest = '{"schemaVersion":2,"layers":[]}'
        digest = "sha256:" + hashlib.sha256(manifest.encode()).hexdigest()
        repository = "registry.local.kodex/kodex/role-image-builder"
        reference = repository + "@" + digest
        archive = self.root / "cached.oci.tar"
        import io, tarfile
        index = json.dumps({"manifests": [{"digest": digest, "annotations": {"io.containerd.image.name": repository + ":cached"}}]}).encode()
        with tarfile.open(archive, "w") as output:
            info = tarfile.TarInfo("index.json")
            info.size = len(index)
            output.addfile(info, io.BytesIO(index))
        args = ["bash", str(SCRIPT.with_name("import-local-image.sh")),
                "--context", "k3d-import-fixture", "--archive", str(archive),
                "--repository", repository, "--tag", repository + ":cached", "--exact-reference", reference]
        environment = dict(self.environment, IMPORT_K3D="1", IMPORT_MANIFEST=manifest, IMPORT_REFERENCE=reference)
        result = subprocess.run(args, env=environment, capture_output=True, text=True, timeout=12)
        self.assertEqual(result.returncode, 0, result.stderr)
        nodes = {"k3d-import-fixture-server-0", "k3d-import-fixture-agent-0"}
        self.assertEqual({item["target"] for item in self.events() if item["kind"] == "node_import"}, nodes)
        self.assertEqual({item["target"] for item in self.events() if item["kind"] == "node_digest_readback"}, nodes)
        result = subprocess.run(args, env=dict(environment, MISSING_IMPORT_NODE="k3d-import-fixture-server-0"),
                                capture_output=True, text=True, timeout=12)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("durable pin mismatch", result.stderr)
        readback_args = args + ["--mode", "readback"]
        before = sum(item['kind'] == 'node_import' for item in self.events())
        result = subprocess.run(readback_args, env=environment, capture_output=True, text=True, timeout=12)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum(item['kind'] == 'node_import' for item in self.events()), before)
        for key, value, error in (("IMPORT_WRONG_TARGET", "1", "durable pin mismatch"),
                                 ("IMPORT_UNPINNED", "1", "durable pin mismatch"),
                                 ("IMPORT_PLATFORM", "linux/arm64", "durable pin mismatch"),
                                 ("IMPORT_INCOMPLETE", "1", "native unpack is incomplete"),
                                 ("IMPORT_ARCH", "arm64", "architecture readback failed"),
                                 ("IMPORT_CLUSTER", "foreign", "registries mismatch"),
                                 ("IMPORT_MANIFEST", "corrupted", "manifest digest mismatch")):
            with self.subTest(key=key):
                result = subprocess.run(readback_args, env=dict(environment, **{key: value}),
                                        capture_output=True, text=True, timeout=12)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
        for key in ("IMPORT_CRI_PINNED", "IMPORT_CRI_WRONG"):
            with self.subTest(key=key):
                result = subprocess.run(readback_args, env=dict(environment, **{key: "false" if key == "IMPORT_CRI_PINNED" else "1"}),
                                        capture_output=True, text=True, timeout=15)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("CRI exact immutable image or durable pin readback failed", result.stderr)
        outside = args.copy()
        outside[outside.index('--repository') + 1] = 'registry.local.kodex/kodex/foreign'
        result = subprocess.run(outside, env=environment, capture_output=True, text=True, timeout=12)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('closed local platform profile', result.stderr)
        result = subprocess.run(args, env=dict(environment, IMPORT_NAMED_ONLY="1"),
                                capture_output=True, text=True, timeout=12)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual({item["target"] for item in self.events() if item["kind"] == "node_alias"}, nodes)

    def test_archive_mixed_names_rejected_before_import(self):
        import io, tarfile
        repository = "registry.local.kodex/kodex/role-image-builder"
        digest = "sha256:" + "a" * 64
        archive = self.root / "mixed-names.oci.tar"
        for annotations in ({"io.containerd.image.name": "foreign.invalid/image:cached", "org.opencontainers.image.ref.name": repository + ":cached"},
                            {"io.containerd.image.name": repository + ":cached", "org.opencontainers.image.ref.name": "foreign.invalid/image:cached"}):
            with self.subTest(annotations=annotations):
                index = json.dumps({"manifests": [{"digest": digest, "annotations": annotations}]}).encode()
                with tarfile.open(archive, "w") as output:
                    info = tarfile.TarInfo("index.json")
                    info.size = len(index)
                    output.addfile(info, io.BytesIO(index))
                result = subprocess.run(["bash", str(SCRIPT.with_name("import-local-image.sh")),
                    "--context", "k3d-import-fixture", "--archive", str(archive), "--repository", repository,
                    "--tag", repository + ":cached", "--exact-reference", repository + "@" + digest],
                    env=dict(self.environment, IMPORT_K3D="1"), capture_output=True, text=True, timeout=5)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("OCI archive exact descriptor or name mismatch", result.stderr)
                self.assertFalse(any(item["kind"] == "node_import" for item in self.events()))

    def test_failed_build_cancels_siblings_without_import_or_pointer(self):
        result = self.run_build(4, FAIL_BUILD="image-admission")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("OCI build failed: image-admission", result.stderr)
        self.assertFalse(any(item["kind"] == "import" for item in self.events()))
        self.assert_no_new_pointers()

    def test_failed_load_is_also_before_import_barrier(self):
        result = self.run_build(2, FAIL_BUILD="image-admission-tools-load")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(item["kind"] == "import" for item in self.events()))
        self.assert_no_new_pointers()

    def test_failed_digest_readback_preserves_all_previous_pointers(self):
        result = self.run_build(2, FAIL_IMPORT="role-image-builder")
        self.assertNotEqual(result.returncode, 0)
        self.assert_no_new_pointers()

    def test_source_change_during_build_rejects_outputs_before_import(self):
        result = self.run_build(2, MUTATE_BUILD="image-admission-tools")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("source changed during supply-chain build", result.stderr)
        self.assertFalse(any(item["kind"] == "import" for item in self.events()))
        self.assertFalse(list((self.state / "cache/image-supply-chain").glob("image-*.oci.tar")))
        self.assert_no_new_pointers()

    def test_single_component_and_authority_security_keep_exact_recipe(self):
        result = self.run_build(4, component="authority-security")
        self.assertEqual(result.returncode, 0, result.stderr)
        metadata = json.loads((self.state / "authority-security-images.json").read_text())
        self.assertEqual(metadata["revision"], REVISION)
        self.assertTrue(metadata["digestReadback"])
        self.assertEqual(sum(item["kind"] == "start" for item in self.events()), 2)

    def test_invalid_concurrency_fails_before_external_commands(self):
        for jobs in (0, 5, "auto", "2;echo"):
            with self.subTest(jobs=jobs):
                result = self.run_build(jobs)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("build jobs must be between 1 and 4", result.stderr)
        self.assertFalse(self.events())

    def test_termination_joins_descendants_and_releases_writer_lock(self):
        process = subprocess.Popen(self.arguments(2),
                                   env=dict(self.environment, BLOCK_BUILD="1"),
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                starts = [item for item in self.events() if item["kind"] == "start"]
                if len(starts) >= 2:
                    break
                time.sleep(0.02)
            self.assertEqual(len(starts), 2)
            duplicate = self.run_build(2)
            self.assertNotEqual(duplicate.returncode, 0)
            self.assertIn("another supply-chain build owns", duplicate.stderr)
            process.send_signal(signal.SIGTERM)
            self.assertEqual(process.wait(timeout=8), 143)
            for item in starts:
                for pid in (item["pid"], item["child"]):
                    with self.assertRaises(ProcessLookupError):
                        os.kill(pid, 0)
            self.assert_no_new_pointers()
            resumed = self.run_build(2)
            self.assertEqual(resumed.returncode, 0, resumed.stderr)
        finally:
            if process.poll() is None:
                process.send_signal(signal.SIGTERM)
                process.wait(timeout=8)


class AdmissionDatabaseLayerOrderTest(unittest.TestCase):
    def test_immutable_database_layer_precedes_frequently_changed_validators(self):
        repository = SCRIPT.parents[2]
        for name in ("tools/dev/Dockerfile.local-image-supply-chain",
                     "infra/admission-tools/Dockerfile"):
            with self.subTest(dockerfile=name):
                source = (repository / name).read_text()
                database = source.index("ADD --checksum=sha256:")
                imported = source.index("&& grype db import /tmp/grype-db.tar.zst")
                verified = source.index("&& grype db status >/dev/null")
                for validator in ("image-tool-inventory-validator",
                                  "image-vulnerability-report-validator"):
                    materialized = source.index(
                        f"COPY --from=build --chmod=0555 /out/{validator} ")
                    self.assertLess(database, imported)
                    self.assertLess(imported, verified)
                    self.assertLess(verified, materialized)
                    self.assertLess(materialized, source.index("RUN for tool in "))
                self.assertIn("GRYPE_DB_AUTO_UPDATE=false", source)
                self.assertIn("GRYPE_DB_VALIDATE_AGE=true", source)


if __name__ == "__main__":
    unittest.main()
