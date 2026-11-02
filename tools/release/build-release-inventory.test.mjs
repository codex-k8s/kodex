import assert from "node:assert/strict";
import { spawnSync, execFileSync } from "node:child_process";
import { chmodSync, copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../../", import.meta.url));
const inventory = JSON.parse(readFileSync(join(root, "tools/release/images.json"), "utf8"));
const digest = `sha256:${"1".repeat(64)}`;

// Синтетический repository проверяет публичный entrypoint целиком; BuildKit и
// publisher заменены только на границах, без Docker, registry и credentials.
function runRelease(mutate = () => {}) {
  const directory = mkdtempSync(join(tmpdir(), "kodex-release-inventory-"));
  const fixture = join(directory, "repository");
  const bin = join(directory, "bin");
  const trace = join(directory, "builds.jsonl");
  const output = join(directory, "release-lock.json");
  function write(path, content, executable = false) {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, content);
    if (executable) chmodSync(path, 0o755);
  }
  try {
    mkdirSync(fixture);
    mkdirSync(bin);
    for (const path of ["tools/release/build-release.sh", "tools/release/validate-image-dockerfile-path.sh",
      "tools/release/shims/docker", "scripts/build-agent-runner-image.sh"]) {
      mkdirSync(dirname(join(fixture, path)), { recursive: true });
      copyFileSync(join(root, path), join(fixture, path));
      chmodSync(join(fixture, path), 0o755);
    }
    const manifest = structuredClone(inventory);
    mutate(manifest);
    write(join(fixture, "tools/release/images.json"), JSON.stringify(manifest));
    for (const image of manifest.images) write(join(fixture, image.dockerfile), "FROM scratch\n");
    write(join(fixture, "infra/admission-tools/Dockerfile"), "FROM scratch\n");
    write(join(fixture, "scripts/tests/go-toolchain-contract-test.sh"), "#!/bin/sh\nexit 0\n", true);
    write(join(fixture, "tools/release/schema-heads.py"), 'print(\'{"control-plane":"1","internal-rpc-authority":"2","email-bridge":"3"}\')\n');
    mkdirSync(join(fixture, "tools/release/role-image-input-publisher"), { recursive: true });
    write(join(bin, "go"), `#!/bin/sh\nprintf '%s\\n' '{"manifestDigest":"${digest}","payloadSha256":"${"2".repeat(64)}","sourceSha256":"${"3".repeat(64)}"}'\n`, true);
    write(join(bin, "buildctl"), `#!/usr/bin/python3
import json, os, pathlib, sys
args = sys.argv[1:]
with open(os.environ["TRACE"], "a") as stream:
    stream.write(json.dumps(args) + "\\n")
pathlib.Path(args[args.index("--metadata-file") + 1]).write_text(json.dumps({"containerimage.digest":"${digest}"}))
`, true);
    const git = (args) => execFileSync("git", ["-C", fixture, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    git(["init", "--quiet"]);
    git(["add", "."]);
    git(["-c", "user.name=Synthetic", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "fixture"]);
    const sha = git(["rev-parse", "HEAD"]).trim();
    const result = spawnSync(join(fixture, "tools/release/build-release.sh"), ["--source-sha", sha,
      "--output", output, "--registry-push", "registry.example.invalid", "--node-pull", "registry.example.invalid",
      "--parallelism", "2"], { cwd: fixture, encoding: "utf8", timeout: 20000,
      env: { PATH: `${bin}:/usr/bin:/bin`, HOME: directory, TRACE: trace,
        BUILDCTL_PATH: join(bin, "buildctl"), BUILDKIT_HOST: "unix:///synthetic/buildkit.sock" } });
    const builds = readFileSync(trace, "utf8").trim().split("\n").map(JSON.parse);
    let lock;
    if (result.status === 0) lock = JSON.parse(readFileSync(output, "utf8"));
    return { result, builds, lock, sha };
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

test("complete release inventory reaches BuildKit with exact targets and runner/base dependency", () => {
  const { result, builds, lock, sha } = runRelease();
  assert.equal(result.status, 0, result.stderr);
  assert.equal(builds.length, inventory.images.length + 1);
  for (const image of inventory.images) {
    const matches = builds.filter(args => args.includes(`type=image,name=registry.example.invalid/kodex/${image.component}:${sha},push=true`));
    assert.equal(matches.length, 1, image.component);
    const args = matches[0];
    const target = args.filter(value => value.startsWith("target="));
    assert.deepEqual(target, image.target ? [`target=${image.target}`] : [], image.component);
    if (image.component === "role-base-documents") {
      assert.ok(args.includes(`build-arg:AGENT_RUNNER_IMAGE=registry.example.invalid/kodex/agent-runner@${digest}`));
    }
  }
  assert.equal(lock.source_sha, sha);
  assert.equal(lock.images.length, inventory.images.length);
});

test("unsupported target fails closed and never reaches BuildKit", () => {
  const { result, builds, lock } = runRelease(manifest => {
    manifest.images[0].target = "unexpected-target";
  });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /unexpected Dockerfile target/);
  assert.equal(lock, undefined);
  assert.equal(builds.some(args => args.includes("target=unexpected-target")), false);
});
