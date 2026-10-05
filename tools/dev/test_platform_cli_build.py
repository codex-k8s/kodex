"""Offline source-build gh/kubectl/Helm: compiler, staging pins и версии без downloads."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "services/jobs/agent-runner/build-go-tool.sh"
DOCKERFILE = ROOT / "services/jobs/agent-runner/Dockerfile"
SELECTIONS = [
    ("github.com/cli/cli/v2/cmd/gh@v2.95.0", "github.com/cli/cli/v2", "gh", "2.95.0"),
    ("k8s.io/kubernetes/cmd/kubectl@v1.36.2", "k8s.io/kubernetes", "kubectl", "v1.36.2"),
    ("helm.sh/helm/v4/cmd/helm@v4.2.1", "helm.sh/helm/v4", "helm", "v4.2.1"),
]
STAGING = """api apiextensions-apiserver apimachinery apiserver cli-runtime client-go
cloud-provider cluster-bootstrap code-generator component-base component-helpers
controller-manager cri-api cri-client cri-streaming csi-translation-lib dynamic-resource-allocation
endpointslice externaljwt kms kube-aggregator kube-controller-manager kube-proxy kube-scheduler
kubectl kubelet metrics mount-utils pod-security-admission sample-apiserver streaming""".split()
MOCK = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
root = pathlib.Path(os.environ["PLATFORM_FIXTURE"])
args = sys.argv[1:]
with (root / "calls").open("a") as out: out.write(json.dumps(args) + "\n")
if pathlib.Path(sys.argv[0]).name == "apk":
    assert args == ["add", "--no-cache", "ca-certificates", "git"]; sys.exit()
if args == ["env", "GOVERSION"]:
    print(os.environ.get("FIXTURE_GO", "go1.26.6")); sys.exit()
if args[:2] == ["mod", "init"]:
    sys.exit()
if args[:2] == ["mod", "edit"]:
    requirements = [a.removeprefix("-require=").rsplit("@", 1) for a in args if a.startswith("-require=")]
    if "-go=1.26.6" in args:
        assert len(requirements) == 1
        (root / "identity").write_text(json.dumps(requirements[0]))
        (root / "staging").write_text("{}")
        (root / "floors").write_text("{}")
        (root / "dependencies").unlink(missing_ok=True)
    elif len(requirements) == 31:
        assert all(module.startswith("k8s.io/") and version == "v0.36.2" for module, version in requirements)
        assert len(requirements) == 31
        (root / "staging").write_text(json.dumps(dict(requirements)))
    else:
        assert len(requirements) == 1
        floors = json.loads((root / "floors").read_text())
        floors.update(requirements)
        (root / "floors").write_text(json.dumps(floors))
    sys.exit()
module, version = json.loads((root / "identity").read_text())
staging = json.loads((root / "staging").read_text())
if args[:2] == ["list", "-m"]:
    if module == "k8s.io/kubernetes": assert len(staging) == 31
    print("kodex.local/runner-tool")
    print(module, version)
    dependencies = (json.loads((root / "dependencies").read_text()) if (root / "dependencies").exists()
                    else {**staging, "golang.org/x/net": "v0.49.0", "google.golang.org/grpc": "v1.79.3"})
    for name, value in dependencies.items():
        print(name, value)
    sys.exit()
if args[:1] == ["get"]:
    assert len(args) == 2
    assert args[1].rsplit("@", 1)[1] == version
    (root / "package").write_text(args[1].rsplit("@", 1)[0])
    dependencies = {**staging, "golang.org/x/net": "v0.49.0", "google.golang.org/grpc": "v1.79.3"}
    dependencies.update(json.loads((root / "floors").read_text()))
    # grpc floor допускает более высокий transitive net, а не конфликтует.
    dependencies["golang.org/x/net"] = "v0.58.0"
    (root / "dependencies").write_text(json.dumps(dependencies)); sys.exit()
if args == ["mod", "verify"]:
    sys.exit(1 if os.environ.get("FIXTURE_VERIFY_FAIL") else 0)
if args[:1] == ["build"]:
    flags = next(a.removeprefix("-ldflags=") for a in args if a.startswith("-ldflags="))
    binary = pathlib.Path(args[args.index("-o") + 1]).name
    if binary == "gh":
        assert flags == "-X github.com/cli/cli/v2/internal/build.Version=2.95.0"
    elif binary == "helm":
        assert flags == "-X helm.sh/helm/v4/internal/version.version=v4.2.1"
    elif binary == "kubectl":
        assert "-X k8s.io/component-base/version.gitVersion=v1.36.2" in flags
        assert "-X k8s.io/client-go/pkg/version.gitVersion=v1.36.2" in flags
        assert ".gitCommit=" not in flags and ".gitTreeState=clean" not in flags
    else: raise AssertionError(binary)
    assert os.environ["CGO_ENABLED"] == "0"
    assert os.environ["GOTOOLCHAIN"] == "local"
    assert os.environ["GOSUMDB"] == "sum.golang.org"
    target = pathlib.Path(args[args.index("-o") + 1])
    runtime = {"gh": "gh version 2.95.0",
               "kubectl": '{\n  "clientVersion": {\n    "gitVersion": "v1.36.2",\n    "goVersion": "go1.26.6"\n  }\n}',
               "helm": "v4.2.1 go1.26.6"}[binary]
    target.write_text("#!" + sys.executable + "\nimport os, sys\n"
                      + "print(" + repr(runtime) + " if not os.environ.get('FIXTURE_RUNTIME_WRONG') else 'wrong')\n"
                      + "sys.exit(1 if os.environ.get('FIXTURE_RUNTIME_FAIL') else 0)\n")
    sys.exit()
if args[:2] == ["version", "-m"]:
    print(args[-1] + ": " + os.environ.get("FIXTURE_BINARY_GO", "go1.26.6"))
    print("\tpath\t" + (root / "package").read_text())
    print("\tmod\t" + module + "\t" + os.environ.get("FIXTURE_BINARY_VERSION", version) + "\th1:synthetic")
    deps = json.loads((root / "dependencies").read_text())
    if os.environ.get("FIXTURE_UNSAFE_NET"): deps["golang.org/x/net"] = "v0.49.0"
    for name, value in deps.items(): print("\tdep\t" + name + "\t" + value + "\th1:synthetic")
    if os.environ.get("FIXTURE_REPLACED"): print("\t=>\tlocal.invalid/fork\tv1.0.0\th1:synthetic")
    sys.exit()
raise AssertionError(args)
'''


class PlatformCLI(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="platform-cli-")
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name)
        self.bin, self.output, self.scratch = [self.directory / name for name in ("bin", "output", "scratch")]
        for directory in (self.bin, self.output, self.scratch):
            directory.mkdir()
        for name in ("go", "apk"):
            target = self.bin / name
            target.write_text(MOCK)
            target.chmod(0o755)
        self.env = {"PATH": str(self.bin) + ":" + os.defpath, "PLATFORM_FIXTURE": str(self.directory),
                    "GOBIN": str(self.output), "TMPDIR": str(self.scratch), "LANG": "C"}
        dockerfile = DOCKERFILE.read_text()
        self.stage = dockerfile.split("FROM build AS platform-cli-build\n", 1)[1].split("\nFROM ", 1)[0]
        self.run = self.stage.split("RUN ", 1)[1].replace("\\\n", "").replace(
            "/usr/local/bin/kodex-build-go-tool", str(SCRIPT))

    def build(self, selection, **extra):
        return subprocess.run(["sh", str(SCRIPT), selection], env=dict(self.env, **extra),
                              capture_output=True, text=True, timeout=10)

    def calls(self):
        return [json.loads(line) for line in (self.directory / "calls").read_text().splitlines()]

    def test_all_three_exact_packages_compiler_and_floors(self):
        for selection, module, binary, _ in SELECTIONS:
            with self.subTest(selection=selection):
                result = self.build(selection)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((self.output / binary).is_file())
                self.assertEqual(json.loads((self.directory / "identity").read_text())[0], module)
                get = [call for call in self.calls() if call[0] == "get"][-1]
                self.assertEqual(get, ["get", selection])
                constraints = [call for call in self.calls() if call[:2] == ["mod", "edit"]]
                self.assertIn(["mod", "edit", "-require=golang.org/x/net@v0.56.0"], constraints)
                self.assertIn(["mod", "edit", "-require=google.golang.org/grpc@v1.83.2"], constraints)
                dependencies = json.loads((self.directory / "dependencies").read_text())
                self.assertEqual(dependencies["golang.org/x/net"], "v0.58.0")

    def test_kubernetes_canonical_staging_map_before_graph(self):
        result = self.build(SELECTIONS[1][0])
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.calls()
        staging = next(call for call in calls if call[:2] == ["mod", "edit"] and len(call) > 10)
        self.assertEqual({a.removeprefix("-require=") for a in staging[2:]},
                         {"k8s.io/" + name + "@v0.36.2" for name in STAGING})
        self.assertLess(calls.index(staging), next(i for i, call in enumerate(calls) if call[:2] == ["list", "-m"]))
        result = self.build("k8s.io/kubernetes/cmd/kubectl@v1.37.0")
        self.assertIn("KUBERNETES_SOURCE_VERSION_UNSUPPORTED", result.stderr)

    def test_unsafe_compiler_dependency_module_or_checksum_never_publish(self):
        for key, value in (("FIXTURE_GO", "go1.26.4"), ("FIXTURE_BINARY_GO", "go1.26.4"),
                           ("FIXTURE_BINARY_VERSION", "v2.94.0"), ("FIXTURE_VERIFY_FAIL", "1"),
                           ("FIXTURE_UNSAFE_NET", "1"), ("FIXTURE_REPLACED", "1")):
            with self.subTest(key=key):
                target = self.output / "gh"
                target.write_text("previous exact output")
                result = self.build(SELECTIONS[0][0], **{key: value})
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(target.read_text(), "previous exact output")

    def test_full_platform_stage_runtime_versions_and_failed_valid_output(self):
        environment = dict(self.env, GH_CLI_VERSION="2.95.0", KUBECTL_VERSION="v1.36.2", HELM_VERSION="v4.2.1")
        for extra in ({}, {"FIXTURE_RUNTIME_WRONG": "1"}, {"FIXTURE_RUNTIME_FAIL": "1"}):
            result = subprocess.run(["sh", "-c", self.run], env=dict(environment, **extra),
                                    capture_output=True, text=True, timeout=10)
            if extra:
                self.assertNotEqual(result.returncode, 0)
            else:
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_source_profile_cache_and_no_prebuilt_fallback(self):
        dockerfile = DOCKERFILE.read_text()
        self.assertEqual(re.findall(r"(?m)^COPY [^\n]+", self.stage),
                         ["COPY --chmod=0555 services/jobs/agent-runner/build-go-tool.sh /usr/local/bin/kodex-build-go-tool"])
        self.assertNotIn("runner-build", self.stage)
        self.assertNotIn("toolchain-build", self.stage)
        self.assertIn("ENV GOTOOLCHAIN=local", dockerfile.split(" AS build\n", 1)[1].split("\nFROM ", 1)[0])
        self.assertIn("golang:1.26.6-alpine@sha256:", dockerfile.splitlines()[0])
        self.assertIn("COPY --from=platform-cli-build --chmod=0555 /out/kodex-platform/ /usr/local/bin/", dockerfile)
        for removed in ("cli/cli/releases/download/", "https://dl.k8s.io/release/",
                        "https://get.helm.sh/", "gh_arch", "kubectl_arch", "helm_arch"):
            self.assertNotIn(removed, dockerfile)
        self.assertNotIn("mod tidy", SCRIPT.read_text())
        self.assertNotIn("GOSUMDB=off", SCRIPT.read_text())


if __name__ == "__main__":
    unittest.main()
