"""Герметичная сборка CLI: точные pins, MVS и metadata без загрузок/OCI."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "services/jobs/agent-runner/build-go-tool.sh"
FAKE_GO = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
root = pathlib.Path(os.environ["GO_BUILD_FIXTURE"])
args = sys.argv[1:]
with (root / "calls").open("a") as stream:
    stream.write(json.dumps({"args":args, "sumdb":os.environ.get("GOSUMDB"),
        "proxy":os.environ.get("GOPROXY"), "toolchain":os.environ.get("GOTOOLCHAIN"),
        "private":os.environ.get("GOPRIVATE"), "insecure":os.environ.get("GOINSECURE"),
        "gomaxprocs":os.environ.get("GOMAXPROCS")}) + "\n")
if args == ["env", "GOVERSION"]:
    print(os.environ.get("FIXTURE_GO_VERSION", "go1.26.6")); sys.exit()
if args[:2] == ["mod", "init"]:
    for name in ("identity", "constraints", "dependencies"):
        (root / name).unlink(missing_ok=True)
    sys.exit()
if args[:2] == ["mod", "edit"]:
    module, version = args[-1].removeprefix("-require=").rsplit("@", 1)
    if not (root / "identity").exists():
        (root / "identity").write_text(json.dumps([module, version]))
    else:
        constraints = json.loads((root / "constraints").read_text()) if (root / "constraints").exists() else {}
        constraints[module] = version
        (root / "constraints").write_text(json.dumps(constraints))
    sys.exit()
if args[:2] == ["list", "-m"]:
    print("kodex.local/runner-tool")
    module, version = json.loads((root / "identity").read_text())
    print(module, version)
    dependencies = json.loads((root / "dependencies").read_text()) if (root / "dependencies").exists() else json.loads(os.environ.get("FIXTURE_DEPS", "{}"))
    for module, version in dependencies.items(): print(module, version)
    sys.exit()
if args[:1] == ["get"]:
    dependencies = json.loads(os.environ.get("FIXTURE_DEPS", "{}"))
    constraints = json.loads((root / "constraints").read_text()) if (root / "constraints").exists() else {}
    for module, version in constraints.items(): dependencies[module] = version
    for selection in args[2:]:
        module, version = selection.rsplit("@", 1); dependencies[module] = version
    if os.environ.get("FIXTURE_GRPC_NET_VERSION"):
        required = os.environ["FIXTURE_GRPC_NET_VERSION"]
        requested = next((value.rsplit("@", 1)[1] for value in args[2:] if value.startswith("golang.org/x/net@")), None)
        if requested and requested != required: sys.exit(1)
        dependencies["golang.org/x/net"] = required
    if os.environ.get("FIXTURE_INTRODUCED_CRYPTO"):
        dependencies["golang.org/x/crypto"] = constraints.get("golang.org/x/crypto", "v0.55.0")
    if os.environ.get("FIXTURE_NONCONVERGENT"):
        dependencies["golang.org/x/crypto"] = "v0.55.0"
    (root / "dependencies").write_text(json.dumps(dependencies))
    (root / "package").write_text(args[1].rsplit("@", 1)[0]); sys.exit()
if args == ["mod", "verify"]:
    sys.exit(1 if os.environ.get("FIXTURE_VERIFY_FAIL") else 0)
if args[:1] == ["build"]:
    pathlib.Path(args[args.index("-o") + 1]).write_text("synthetic binary"); sys.exit()
if args[:2] == ["version", "-m"]:
    print(args[-1], os.environ.get("FIXTURE_BINARY_GO", "go1.26.6"))
    module, version = json.loads((root / "identity").read_text())
    print("\tpath\t" + (root / "package").read_text())
    print("\tmod\t" + module + "\t" + os.environ.get("FIXTURE_BINARY_VERSION", version) + "\th1:fixture")
    deps = json.loads((root / "dependencies").read_text())
    if os.environ.get("FIXTURE_UNSAFE_DEP"): deps["golang.org/x/crypto"] = "v0.53.0"
    for module, version in deps.items(): print("\tdep\t" + module + "\t" + version + "\th1:fixture")
    if os.environ.get("FIXTURE_REPLACE"): print("\t=>\tlocal.invalid/fork\tv0.56.0\th1:fixture")
    sys.exit()
raise AssertionError(args)
'''


class RunnerGoToolBuild(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="kodex-go-tool-test-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.bin, self.output, self.temp = [self.root / name for name in ("bin", "output", "temporary")]
        for directory in (self.bin, self.output, self.temp):
            directory.mkdir()
        fake = self.bin / "go"
        fake.write_text(FAKE_GO)
        fake.chmod(0o700)
        self.env = {"PATH": str(self.bin) + os.pathsep + os.defpath,
                    "GO_BUILD_FIXTURE": str(self.root), "GOBIN": str(self.output),
                    "TMPDIR": str(self.temp), "LANG": "C", "GOSUMDB": "off",
                    "GOINSECURE": "*", "GOPRIVATE": "*", "GOTOOLCHAIN": "auto"}

    def run_build(self, selection="github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3", **env):
        return subprocess.run(["sh", str(SCRIPT), selection], env={**self.env, **env},
                              capture_output=True, text=True, timeout=10)

    def calls(self):
        path = self.root / "calls"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_all_eight_known_modules_upgrade_before_build_and_verified_metadata(self):
        floors = {"github.com/getkin/kin-openapi": "v0.149.0", "golang.org/x/crypto": "v0.56.0",
                  "golang.org/x/oauth2": "v0.27.0", "google.golang.org/grpc": "v1.83.2",
                  "oras.land/oras-go/v2": "v2.6.2", "golang.org/x/mod": "v0.40.0",
                  "golang.org/x/net": "v0.56.0", "golang.org/x/text": "v0.39.0"}
        result = self.run_build(FIXTURE_DEPS=json.dumps({key: "v0.1.0" for key in floors}))
        self.assertEqual(result.returncode, 0, result.stderr)
        get = next(call["args"] for call in self.calls() if call["args"][0] == "get")
        self.assertEqual(get, ["get", "github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3"])
        constraints = json.loads((self.root / "constraints").read_text())
        self.assertEqual(constraints, floors)
        self.assertEqual((self.output / "grpcurl").read_text(), "synthetic binary")
        for call in self.calls()[1:]:
            self.assertEqual(call["sumdb"], "sum.golang.org")
            self.assertEqual(call["proxy"], "https://proxy.golang.org")
            self.assertEqual(call["toolchain"], "local")
            self.assertEqual(call["private"], "")
            self.assertEqual(call["insecure"], "")
            self.assertEqual(call["gomaxprocs"], "4")
        build = next(call["args"] for call in self.calls() if call["args"][0] == "build")
        self.assertIn("-p=4", build)
        self.assertEqual(list(self.temp.iterdir()), [])

    def test_newer_versions_never_downgrade_and_absent_modules_never_added(self):
        result = self.run_build(FIXTURE_DEPS=json.dumps({"golang.org/x/crypto": "v0.57.0",
                                                       "golang.org/x/text": "v0.41.0"}))
        self.assertEqual(result.returncode, 0, result.stderr)
        get = next(call["args"] for call in self.calls() if call["args"][0] == "get")
        self.assertEqual(get, ["get", "github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3"])

    def test_grpc_required_net_above_floor_resolves_without_exact_get_conflict(self):
        result = self.run_build(FIXTURE_DEPS=json.dumps({"google.golang.org/grpc": "v1.61.0",
                                                       "golang.org/x/net": "v0.33.0"}),
                                FIXTURE_GRPC_NET_VERSION="v0.58.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads((self.root / "dependencies").read_text())["golang.org/x/net"], "v0.58.0")

    def test_nested_protoc_and_mock_modules_have_exact_main_identity(self):
        for selection, module in (("google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2", "google.golang.org/grpc/cmd/protoc-gen-go-grpc"),
                                  ("go.uber.org/mock/mockgen@v0.6.0", "go.uber.org/mock")):
            result = self.run_build(selection)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads((self.root / "identity").read_text())[0], module)

    def test_newly_introduced_unsafe_dependency_is_constrained_before_build(self):
        result = self.run_build(FIXTURE_DEPS=json.dumps({"google.golang.org/grpc": "v1.61.0"}),
                                FIXTURE_GRPC_NET_VERSION="v0.58.0", FIXTURE_INTRODUCED_CRYPTO="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        dependencies = json.loads((self.root / "dependencies").read_text())
        self.assertEqual(dependencies["golang.org/x/crypto"], "v0.56.0")
        self.assertEqual(dependencies["golang.org/x/net"], "v0.58.0")
        self.assertEqual(sum(call["args"][0] == "get" for call in self.calls()), 2)

    def test_nonconvergent_dependency_graph_is_bounded_and_never_built(self):
        result = self.run_build(FIXTURE_NONCONVERGENT="1")
        self.assertIn("DEPENDENCY_CLOSURE_UNSAFE", result.stderr)
        self.assertLessEqual(sum(call["args"][0] == "get" for call in self.calls()), 9)
        self.assertFalse(any(call["args"][0] == "build" for call in self.calls()))
        self.assertEqual(list(self.output.iterdir()), [])

    def test_unknown_package_and_unpinned_version_stop_before_go(self):
        for selection in ("example.invalid/tool@v1.0.0", "github.com/fullstorydev/grpcurl/cmd/grpcurl@latest",
                          "github.com/fullstorydev/grpcurl/cmd/grpcurl", "github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.3-rc1"):
            self.assertNotEqual(self.run_build(selection).returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_unsupported_compiler_stops_before_module_or_publish(self):
        result = self.run_build(FIXTURE_GO_VERSION="go1.26.4")
        self.assertIn("GO_TOOLCHAIN_MISMATCH", result.stderr)
        self.assertEqual(len(self.calls()), 1)
        self.assertEqual(list(self.output.iterdir()), [])

    def test_sumdb_integrity_failure_stops_before_build_and_preserves_previous_output(self):
        previous = self.output / "grpcurl"
        previous.write_text("previous exact binary")
        result = self.run_build(FIXTURE_VERIFY_FAIL="1")
        self.assertIn("MODULE_VERIFICATION_FAILED", result.stderr)
        self.assertFalse(any(call["args"][0] == "build" for call in self.calls()))
        self.assertEqual(previous.read_text(), "previous exact binary")

    def test_unsafe_binary_dependency_not_published(self):
        result = self.run_build(FIXTURE_UNSAFE_DEP="1")
        self.assertIn("DEPENDENCY_VERSION_UNSAFE", result.stderr)
        self.assertEqual(list(self.output.iterdir()), [])

    def test_wrong_binary_compiler_module_version_or_replacement_not_published(self):
        for env in ({"FIXTURE_BINARY_GO": "go1.26.4"}, {"FIXTURE_BINARY_VERSION": "v1.9.2"}, {"FIXTURE_REPLACE": "1"}):
            result = self.run_build(**env)
            self.assertIn("BUILD_IDENTITY_MISMATCH", result.stderr)
            self.assertEqual(list(self.output.iterdir()), [])

    def test_output_symlink_rejected(self):
        alias = self.root / "alias"
        alias.symlink_to(self.output, target_is_directory=True)
        self.assertIn("OUTPUT_DIRECTORY_INVALID", self.run_build(GOBIN=str(alias)).stderr)
        self.assertEqual(self.calls(), [])

    def test_helper_is_in_existing_runner_source_and_provenance_closure(self):
        verifier = (ROOT / "tools/release/runner-binary-provenance.py").read_text()
        self.assertIn("INPUTS = ('services/jobs/agent-runner', 'libs/go')", verifier)
        self.assertTrue(SCRIPT.is_relative_to(ROOT / "services/jobs/agent-runner"))


if __name__ == "__main__":
    unittest.main()
