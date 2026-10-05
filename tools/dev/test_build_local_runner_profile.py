"""Герметичные профили runner: OCI fixtures без Docker, downloads и кластера."""

import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "tools/dev/build-local-runner.sh"
MOCK = r'''#!/usr/bin/env python3
import gzip, hashlib, io, json, os, pathlib, sys, tarfile
root = pathlib.Path(os.environ["RUNNER_FIXTURE"])
name, args = pathlib.Path(sys.argv[0]).name, sys.argv[1:]
with (root / "calls.jsonl").open("a") as stream:
    stream.write(json.dumps({"name": name, "args": args}) + "\n")
if name == "docker" and args == ["buildx", "version"]:
    sys.exit(0)
if name == "ensure-local-buildx-builder.sh":
    sys.exit(0)
if name == "import-local-image.sh":
    sys.exit(42 if os.environ.get("FAIL_IMPORT") else 0)
assert name == "docker" and args[:2] == ["buildx", "build"]
profile = args[args.index("--label") + 1].split("=", 1)[1]
assert args[args.index("--target") + 1] == profile + "-runtime"
profile = os.environ.get("PROFILE_LABEL_OVERRIDE", profile)
tag = args[args.index("--tag") + 1]
output = args[args.index("--output") + 1].split("dest=", 1)[1]
def archive(entries):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w") as tar:
        for name, body in entries:
            member = tarfile.TarInfo(name)
            member.mode, member.size = 0o555, len(body)
            tar.addfile(member, io.BytesIO(body))
    return stream.getvalue()
def digest(body): return "sha256:" + hashlib.sha256(body).hexdigest()
blobs = []
def blob(body, media):
    key = digest(body)
    blobs.append(("blobs/sha256/" + key[7:], body))
    return {"mediaType": media, "digest": key, "size": len(body)}
oci = "application/vnd.oci.image."
layer = archive([("usr/local/bin/kodex-agent-runner", b"synthetic protected runner")])
config = {"architecture": "amd64", "os": "linux", "rootfs": {"type": "layers", "diff_ids": [digest(layer)]},
          "config": {"Labels": {"kodex.dev/runner-image-profile": profile}}}
manifest = {"schemaVersion": 2, "mediaType": oci + "manifest.v1+json",
            "config": blob(json.dumps(config).encode(), oci + "config.v1+json"),
            "layers": [blob(gzip.compress(layer, mtime=0), oci + "layer.v1.tar+gzip")]}
descriptor = blob(json.dumps(manifest).encode(), oci + "manifest.v1+json")
descriptor["annotations"] = {"io.containerd.image.name": tag, "org.opencontainers.image.ref.name": tag.rsplit(":", 1)[1]}
index = {"schemaVersion": 2, "mediaType": oci + "index.v1+json", "manifests": [descriptor]}
pathlib.Path(output).write_bytes(archive([("oci-layout", b'{"imageLayoutVersion":"1.0.0"}'),
                                      ("index.json", json.dumps(index).encode()), *blobs]))
'''


class BuildLocalRunnerProfile(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="kodex-runner-profile-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.source, self.state, self.bin = [self.root / name for name in ("source", "state", "bin")]
        self.source.mkdir()
        self.state.mkdir(mode=0o700)
        self.bin.mkdir()
        for name in ("services/jobs/agent-runner/Dockerfile", "services/jobs/agent-runner/build-go-tool.sh",
                     "tools/release/runner-binary-provenance.py"):
            destination = self.source / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, destination)
        (self.source / "libs/go").mkdir(parents=True)
        (self.source / "libs/go/fixture.txt").write_text("synthetic source\n")
        mock = self.root / "mock.py"
        mock.write_text(MOCK)
        mock.chmod(0o700)
        (self.bin / "docker").symlink_to(mock)
        (self.source / "tools/dev").mkdir()
        for name in ("ensure-local-buildx-builder.sh", "import-local-image.sh"):
            (self.source / "tools/dev" / name).symlink_to(mock)
        self.env = {"PATH": str(self.bin) + os.pathsep + os.defpath, "RUNNER_FIXTURE": str(self.root), "LANG": "C"}
        for args in (("init", "-q"), ("add", "."), ("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")):
            subprocess.run(["git", *args], cwd=self.source, check=True, capture_output=True, env=self.env)

    def run_builder(self, *args, overrides=None):
        return subprocess.run(["bash", str(SCRIPT), "--source-root", str(self.source), "--state-directory", str(self.state),
                               "--context", "synthetic-kodex", *args], capture_output=True, text=True, timeout=30,
                              env={**self.env, **(overrides or {})})

    def calls(self):
        path = self.root / "calls.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_local_default_and_full_do_not_share_archive_or_provenance(self):
        references = []
        for arguments in ((), ("--image-profile", "full"), (), ("--image-profile", "full")):
            result = self.run_builder(*arguments)
            self.assertEqual(result.returncode, 0, result.stderr)
            references.append((self.state / "agent-runner-image").read_text())
        self.assertNotEqual(references[0], references[1])
        self.assertEqual(references[:2], references[2:])
        builds = [call for call in self.calls() if call["name"] == "docker" and call["args"][:2] == ["buildx", "build"]]
        self.assertEqual(len(builds), 2)
        self.assertEqual([call["args"][call["args"].index("--target") + 1] for call in builds], ["local-runtime", "full-runtime"])
        proofs = [json.loads(path.read_text()) for path in (self.state / "cache").glob("*.provenance.json")]
        self.assertEqual({proof["imageProfile"] for proof in proofs}, {"local", "full"})
        self.assertEqual(len({proof["buildInputSHA256"] for proof in proofs}), 2)
        self.assertEqual(len({proof["sourceInputSHA256"] for proof in proofs}), 1)
        self.assertEqual(len(list((self.state / "cache").glob("agent-runner-*.oci.tar"))), 2)

    def test_invalid_and_duplicate_profile_stop_before_tools(self):
        for args in (("--image-profile", "future"), ("--image-profile", ""), ("--image-profile",),
                     ("--image-profile", "local", "--image-profile", "full")):
            self.assertNotEqual(self.run_builder(*args).returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_mismatched_archive_profile_stops_before_import_and_pin(self):
        result = self.run_builder("--image-profile", "full", overrides={"PROFILE_LABEL_OVERRIDE": "local"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("IMAGE_PROFILE_BINDING_MISMATCH", result.stderr)
        self.assertFalse((self.state / "agent-runner-image").exists())
        self.assertFalse(any(call["name"] == "import-local-image.sh" for call in self.calls()))

    def test_failed_import_keeps_previous_output_pin(self):
        pin = self.state / "agent-runner-image"
        previous = "registry.fixture.invalid/previous@sha256:" + "a" * 64 + "\n"
        pin.write_text(previous)
        self.assertNotEqual(self.run_builder("--image-profile", "full", overrides={"FAIL_IMPORT": "1"}).returncode, 0)
        self.assertEqual(pin.read_text(), previous)

    def test_seed_and_render_use_same_exact_manifest_without_profile_fallback(self):
        seed = (ROOT / "tools/dev/seed-local-image-supply-chain.sh").read_text()
        render = (ROOT / "tools/dev/render-local.sh").read_text()
        self.assertIn('runner_reference=$(<"$state_directory/agent-runner-image")', seed)
        self.assertIn('"$candidate_digest" == "$runner_digest"', seed)
        self.assertIn('-name \'agent-runner-*.oci.tar\'', seed)
        self.assertIn('(.environments[] | select(.key == "standard") | .baseImageDigest) = strenv(RUNNER_DIGEST)', render)
        self.assertIn('.data.trustedRoleBaseDigest = strenv(RUNNER_DIGEST)', render)

    def test_full_final_stage_contains_all_38_declared_tool_installations(self):
        dockerfile = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text()
        self.assertTrue(re.search(r'^FROM .* AS full-runtime$', dockerfile, re.MULTILINE))
        full = dockerfile.split(" AS full-runtime\n", 1)[1]
        self.assertNotIn('playwright install --with-deps chromium', full)
        manifest = (ROOT / "services/jobs/agent-runner/npm-toolchain/package.json").read_text()
        self.assertIn('"@playwright/mcp": "0.0.77"', manifest)
        self.assertIn('npm ci --prefix /opt/kodex/npm-toolchain --ignore-scripts', full)
        self.assertIn('PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1', full)
        self.assertIn('PLAYWRIGHT_MCP_EXECUTABLE_PATH=/usr/lib/chromium/chromium', full)
        self.assertIn('COPY --from=toolchain-build /out/kodex-protected/ /usr/local/bin/', full)
        self.assertIn('RUN ["/kodex-go-toolchain-guard", "install", "services", "/usr/local/go/bin/go"]', full)
        required = ("bash", "curl", "git", "gh", "jq", "yq", "ripgrep", "make", "just", "go", "goimports", "gofumpt",
                           "golangci-lint", "staticcheck", "goose", "sqlc", "buf", "protoc", "protoc-gen-go", "protoc-gen-go-grpc",
                           "grpcurl", "mockgen", "oapi-codegen", "node", "npm", "pnpm", "yarn", "typescript", "eslint", "prettier",
                           "vite", "vue-tsc", "vitest", "playwright", "chromium", "playwright-mcp", "wscat", "codex")
        probes = (ROOT / "libs/go/runtimecontract/image_inventory.go").read_text().split('return []ImageToolProbe{', 1)[1].split('optional(probe(', 1)[0]
        observed = re.findall(r'probe\("([^"]+)"|Name: "([^"]+)"', probes)
        self.assertEqual(tuple(left or right for left, right in observed), required)
        self.assertEqual(len(required), 38)
        for executable in required:
            self.assertIn(executable, dockerfile + manifest)

    def test_local_codex_default_remains_in_its_own_stage(self):
        dockerfile = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text()
        local = dockerfile.split(" AS local-codex-cli\n", 1)[1].split("\nFROM ", 1)[0]
        self.assertIn("ARG KODEX_CODEX_PACKAGE=@openai/codex@0.160.0", local)
        self.assertLess(local.index("ARG KODEX_CODEX_PACKAGE="), local.index("RUN "))

    def test_security_refresh_and_native_browser_are_source_pinned_before_runner(self):
        full = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text().split(" AS full-runtime\n", 1)[1]
        self.assertIn('ARG KODEX_TOOLCHAIN_SECURITY_REVISION=20261005', full)
        self.assertIn('LABEL kodex.dev/toolchain-security-revision=${KODEX_TOOLCHAIN_SECURITY_REVISION}', full)
        self.assertLess(full.index('APT::Update::Error-Mode=any update'), full.index('apt-get upgrade -y --no-install-recommends'))
        self.assertLess(full.index('apt-get upgrade -y --no-install-recommends'), full.index('apt-get install -y --no-install-recommends'))
        self.assertNotIn('/ms-playwright', full)
        self.assertIn("chromium.launch({executablePath:process.env.PLAYWRIGHT_MCP_EXECUTABLE_PATH,headless:true})", full)
        self.assertIn('runuser -u kodex -- node -e', full)
        self.assertLess(full.index('runuser -u kodex -- node -e'), full.index('COPY --from=runner-build '))

    def test_actual_dockerfile_security_gate_rejects_old_or_missing_packages(self):
        full = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text().split(" AS full-runtime\n", 1)[1]
        start = full.index('\trequire_security_version()')
        end = full.index('\tln -sf /usr/bin/tini', start)
        gate = full[start:end].replace('\\\n', '\n')
        query = self.bin / "dpkg-query"
        query.write_text('#!/bin/sh\ncase "$3" in\nlibexpat1|libexpat1-dev) printf "%s" "${EXPAT_VERSION}" ;;\nlibaprutil1) printf "%s" "${APR_VERSION}" ;;\nchromium) printf "%s" "${CHROMIUM_VERSION}" ;;\n*) exit 1 ;;\nesac\n')
        query.chmod(0o700)
        versions = {"EXPAT_VERSION": "2.5.0-1+deb12u4", "APR_VERSION": "1.6.3-1+deb12u1", "CHROMIUM_VERSION": "154.0.8037.92-1~deb12u1"}
        for overrides, expected in (({}, 0), ({"EXPAT_VERSION": "2.5.0-1+deb12u2"}, 1),
                                    ({"APR_VERSION": "1.6.3-1"}, 1), ({"CHROMIUM_VERSION": "149.0.7827.55"}, 1),
                                    ({"EXPAT_VERSION": ""}, 1), ({"CHROMIUM_VERSION": "invalid"}, 1)):
            result = subprocess.run(['sh', '-ec', gate], env={**self.env, **versions, **overrides}, capture_output=True, timeout=5)
            self.assertEqual(result.returncode == 0, expected == 0, result.stderr.decode())

    def test_security_revision_changes_real_source_input_and_cache_identity(self):
        self.assertEqual(self.run_builder('--image-profile', 'full').returncode, 0)
        dockerfile = self.source / 'services/jobs/agent-runner/Dockerfile'
        dockerfile.write_text(dockerfile.read_text().replace('KODEX_TOOLCHAIN_SECURITY_REVISION=20261005', 'KODEX_TOOLCHAIN_SECURITY_REVISION=20261006'))
        for args in (('add', '.'), ('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'security fixture')):
            subprocess.run(['git', *args], cwd=self.source, check=True, capture_output=True, env=self.env)
        result = self.run_builder('--image-profile', 'full')
        self.assertEqual(result.returncode, 0, result.stderr)
        proofs = [json.loads(path.read_text()) for path in (self.state / 'cache').glob('*.provenance.json')]
        self.assertEqual(len(proofs), 2)
        self.assertEqual(len({proof['sourceInputSHA256'] for proof in proofs}), 2)
        self.assertEqual(len({proof['buildInputSHA256'] for proof in proofs}), 2)

    def test_go_security_pin_changes_real_source_input_and_cache_identity(self):
        self.assertEqual(self.run_builder('--image-profile', 'full').returncode, 0)
        helper = self.source / 'services/jobs/agent-runner/build-go-tool.sh'
        helper.write_text(helper.read_text().replace('golang.org/x/crypto v0.56.0', 'golang.org/x/crypto v0.57.0'))
        for args in (('add', '.'), ('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'tool fixture')):
            subprocess.run(['git', *args], cwd=self.source, check=True, capture_output=True, env=self.env)
        result = self.run_builder('--image-profile', 'full')
        self.assertEqual(result.returncode, 0, result.stderr)
        proofs = [json.loads(path.read_text()) for path in (self.state / 'cache').glob('*.provenance.json')]
        self.assertEqual(len(proofs), 2)
        self.assertEqual(len({proof['sourceInputSHA256'] for proof in proofs}), 2)
        self.assertEqual(len({proof['buildInputSHA256'] for proof in proofs}), 2)

    def test_actual_browser_probe_uses_native_path_and_closes_without_raw_errors(self):
        full = (ROOT / 'services/jobs/agent-runner/Dockerfile').read_text().split(' AS full-runtime\n', 1)[1]
        probe = re.search(r"RUN runuser -u kodex -- node -e '([^'\n]+)'", full).group(1)
        module = self.root / 'playwright.cjs'
        module.write_text('''exports.chromium={launch:async(options)=>{
if(options.executablePath!=="/usr/lib/chromium/chromium"||options.headless!==true)throw new Error("wrong native browser");
if(process.env.PROBE_FAIL)throw new Error("SENTINEL_PRIVATE_ERROR");
return {newPage:async()=>({setContent:async(value)=>{if(value!=="<title>kodex</title>")throw new Error("wrong content");},title:async()=>"kodex"}),close:async()=>{process.stdout.write("CLOSED");}};
}};''')
        probe = probe.replace('/opt/kodex/npm-toolchain/node_modules/playwright', str(module))
        for failed in (False, True):
            result = subprocess.run([shutil.which('node'), '-e', probe], env={**self.env, 'PLAYWRIGHT_MCP_EXECUTABLE_PATH': '/usr/lib/chromium/chromium',
                                    **({'PROBE_FAIL': '1'} if failed else {})}, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 1 if failed else 0)
            self.assertEqual(result.stdout, '' if failed else 'CLOSED')
            self.assertEqual(result.stderr, 'System Chromium probe failed\n' if failed else '')
            self.assertNotIn('SENTINEL_PRIVATE_ERROR', result.stderr)

    def test_npm_toolchain_publishes_relative_links_from_closed_source(self):
        dockerfile = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text()
        full = dockerfile.split(" AS full-runtime\n", 1)[1]
        helper = (ROOT / "services/jobs/agent-runner/npm-toolchain/install.mjs").read_text()
        self.assertIn('symlinkSync(relative(dirname(destination), target), destination)', helper)
        self.assertLess(full.index('install.mjs verify'), full.index('install.mjs publish-links'))
        self.assertLess(full.index('uninstall --global npm --prefix /usr/local'), full.index('install.mjs publish-links'))
        self.assertLess(full.index('install.mjs publish-links'), full.index('COPY --from=runner-build '))
        self.assertIn('require("/opt/kodex/npm-toolchain/node_modules/playwright")', full)
        self.assertIn('test "$(yarn --version)" = "$(node -p', full)
        probe = (ROOT / "services/jobs/agent-runner/internal/imageinventory/probe.go").read_text()
        self.assertIn('root.Open(strings.TrimPrefix(path, "/"))', probe)
        self.assertNotIn('os.Open(path)', probe)

    def test_toolchain_and_full_download_layers_do_not_depend_on_runner_source(self):
        dockerfile = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text()
        stages = {}
        headers = list(re.finditer(r'^FROM ([^\n]+) AS ([a-z0-9-]+)$', dockerfile, re.MULTILINE))
        for index, header in enumerate(headers):
            end = headers[index + 1].start() if index + 1 < len(headers) else len(dockerfile)
            stages[header.group(2)] = (header.group(1), dockerfile[header.end():end])
        parent, tools = stages["toolchain-build"]
        self.assertEqual(parent, "build")
        self.assertNotRegex(stages["build"][1], r'(?m)^(?:COPY|ADD) ')
        self.assertEqual(re.findall(r'(?m)^(?:COPY|ADD) (.+)$', tools),
                         ["services/jobs/agent-runner/build-go-tool.sh /opt/kodex/build-go-tool.sh"])
        self.assertNotIn("runner-build", tools)
        self.assertEqual(tools.count("sh /opt/kodex/build-go-tool.sh "), 13)
        self.assertNotIn("go install", tools)
        full = stages["full-runtime"][1]
        installation_end = full.index('rm -rf /var/lib/apt/lists/* /root/.cache')
        source_copy = full.index('COPY --from=runner-build ')
        self.assertGreater(source_copy, installation_end)
        early_copies = re.findall(r'(?m)^COPY .*?--from=([^ ]+)', full[:installation_end])
        self.assertEqual(early_copies, ["toolchain-build", "platform-cli-build", "build"])

    def test_full_guard_stages_exact_runner_after_prepare_before_install(self):
        dockerfile = (ROOT / "services/jobs/agent-runner/Dockerfile").read_text()
        full = dockerfile.split(" AS full-runtime\n", 1)[1]
        expected = (
            'RUN ["/kodex-go-toolchain-guard", "prepare", "services"]',
            'COPY --from=toolchain-build /out/kodex-protected/ /opt/kodex/protected-artifacts/',
            'COPY --from=runner-build /out/kodex-protected/kodex-agent-runner /opt/kodex/protected-artifacts/kodex-agent-runner',
            'COPY --from=build /out/kodex-go-toolchain-guard /opt/kodex/protected-artifacts/kodex-init',
            'RUN ["/kodex-go-toolchain-guard", "install", "services", "/usr/local/go/bin/go"]',
            'USER 10001:10001',
            'ENTRYPOINT ["/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"]',
        )
        positions = [full.index(instruction) for instruction in expected]
        self.assertEqual(positions, sorted(positions))
        self.assertEqual(full.count("COPY --from=runner-build "), 1)
        self.assertIn('COPY --from=build /usr/local/go/ /usr/local/go/', full)


if __name__ == "__main__":
    unittest.main()
