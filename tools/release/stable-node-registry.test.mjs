import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { logicalRegistryHost, nativeRegistryMirror, nodeRegistryTLSFiles, validateNodeContainer } from "./stable-node-registry-model.mjs";

const physicalHost = "pull.example.invalid";
function source() {
  return { mirrors: { [physicalHost]: { endpoint: [`https://${physicalHost}`] }, "other.example.invalid": { endpoint: ["https://other.example.invalid"] } },
    configs: { [physicalHost]: { auth: { username: "SENTINEL_NODE_USER", password: "SENTINEL_NODE_PASSWORD_".repeat(4) },
      tls: { ca_file: nodeRegistryTLSFiles[0], cert_file: nodeRegistryTLSFiles[1], key_file: nodeRegistryTLSFiles[2] } } } };
}
test("native logical mirror preserves one exact auth TLS source and unrelated settings", () => {
  const registry = source(), config = { "node-label": ["fixture=true"] }, original = structuredClone({ registry, config });
  const after = nativeRegistryMirror(registry, config, physicalHost);
  assert.deepEqual({ registry, config }, original);
  assert.deepEqual(after.registry.configs, registry.configs);
  assert.equal(Object.hasOwn(after.registry.configs, logicalRegistryHost), false);
  assert.deepEqual(after.registry.mirrors[logicalRegistryHost], registry.mirrors[physicalHost]);
  assert.deepEqual(after.registry.mirrors["other.example.invalid"], registry.mirrors["other.example.invalid"]);
  assert.deepEqual(after.config, { ...config, "disable-default-registry-endpoint": true });
});
test("mirror rejects fallback rewrites second credential source TLS skip and wildcard routes", () => {
  for (const mutate of [
    r => { r.configs[logicalRegistryHost] = structuredClone(r.configs[physicalHost]); },
    r => { r.mirrors[logicalRegistryHost] = { endpoint: ["https://foreign.example.invalid"] }; },
    r => { r.mirrors[physicalHost].rewrite = { ".*": "foreign" }; },
    r => { r.configs[physicalHost].tls.insecure_skip_verify = true; },
    r => { r.mirrors["*"] = r.mirrors[physicalHost]; },
    r => { r.configs[physicalHost].auth.auth = "SENTINEL_OTHER_CREDENTIAL"; },
  ]) {
    const registry = source(); mutate(registry);
    assert.throws(() => nativeRegistryMirror(registry, {}, physicalHost), /NODE_MIRROR_/);
  }
  assert.throws(() => nativeRegistryMirror(source(), { "private-registry": "/foreign" }, physicalHost), /OVERRIDE/);
  assert.throws(() => nativeRegistryMirror(source(), {}, "https://pull.example.invalid"), /HOST_INVALID/);
});
test("node identity requires exact Kubernetes Docker cluster role and no alternate config", () => {
  const node = { metadata: { name: "k3d-fixture-agent-0", uid: "node-uid" }, status: { conditions: [{ type: "Ready", status: "True" }] } };
  const container = { Id: "a".repeat(64), Image: "sha256:" + "b".repeat(64), Name: "/" + node.metadata.name,
    Config: { Labels: { "k3d.cluster": "fixture", "k3d.role": "agent" }, Cmd: ["agent"], Env: ["K3S_TOKEN=SENTINEL_PRIVATE"] }, State: { Running: true } };
  const proof = validateNodeContainer(node, container, "fixture");
  assert.equal(JSON.stringify(proof).includes("SENTINEL"), false);
  for (const mutate of [
    c => { c.Config.Labels["k3d.cluster"] = "fixture-foreign"; },
    c => { c.Config.Cmd.push("--config=/foreign"); },
    c => { c.Config.Env.push("K3S_PRIVATE_REGISTRY=/foreign"); },
    c => { c.State.Running = false; },
  ]) {
    const changed = structuredClone(container); mutate(changed);
    assert.throws(() => validateNodeContainer(node, changed, "fixture"), /NODE_MIRROR_/);
  }
});

const fixtureImage = process.env.KODEX_NODE_REGISTRY_FIXTURE_IMAGE;
const worker = readFileSync(fileURLToPath(new URL("./stable-node-registry-worker.sh", import.meta.url)), "utf8");
const hash = text => createHash("sha256").update(text).digest("hex");
const workerOptions = { skip: !fixtureImage };

function fixture(t, testCase) {
  assert.match(fixtureImage, /^sha256:[a-f0-9]{64}$/);
  const directory = mkdtempSync(join(tmpdir(), "kodex-node-registry-fixture-"));
  chmodSync(directory, 0o700);
  let unknownRunning = false;
  const run = (script, args = [], input) => {
    const name = "kodex-node-registry-fixture-" + randomUUID();
    const argv = ["run", "--rm", "--name", name, "--label", "kodex.dev/fixture=node-registry-transition",
      "--network", "none", "--read-only", "--user", "0:0", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "FOWNER",
      "--cap-add", "DAC_OVERRIDE", "--security-opt", "no-new-privileges", "--pids-limit", "32", "--memory", "64m",
      "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=1m", "--mount", `type=bind,src=${directory},dst=/etc/rancher/k3s`,
      "--entrypoint", "/bin/sh", ...(input === undefined ? [] : ["-i"]), fixtureImage, "-c", script, "kodex-registry-fixture", ...args];
    try { return { status: 0, output: execFileSync("docker", argv, { input, encoding: "utf8", timeout: 60000,
      stdio: ["pipe", "pipe", "pipe"] }) }; }
    catch (error) {
      if (error.status === null) {
        try { unknownRunning = execFileSync("docker", ["inspect", "--format", "{{.State.Running}}", name], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim() === "true"; }
        catch { /* --rm уже мог завершить exact fixture. */ }
      }
      return { status: error.status ?? -1, output: String(error.stdout ?? "") + String(error.stderr ?? "") };
    }
  };
  try {
    const before = JSON.stringify(source()) + "\n", after = nativeRegistryMirror(source(), {}, physicalHost);
    const registryAfter = JSON.stringify(after.registry) + "\n", configAfter = JSON.stringify(after.config) + "\n";
    writeFileSync(join(directory, "registries.yaml"), before, { mode: 0o600 });
    const args = ["install", "d".repeat(64), hash(before), hash(registryAfter), "ABSENT", hash(configAfter), String(process.getuid()), "0"];
    const call = (mode, input) => run(worker, [mode, ...args.slice(1)], input);
    testCase({ directory, run, call, before, registryAfter, payload: registryAfter + configAfter });
  } finally {
    if (!unknownRunning) {
      // Только данный disposable bind mount; реальные node paths не доступны.
      const cleanup = run('chown -R "$1:$2" /etc/rancher/k3s', [String(process.getuid()), String(process.getgid())]);
      assert.equal(cleanup.status, 0, "fixture ownership cleanup failed");
      rmSync(directory, { recursive: true, force: true });
    } else t.diagnostic("Unknown running fixture preserved; no blind cleanup or restart");
  }
}
test("actual node worker installs and restores config without logging credential payload", workerOptions, t => fixture(t, ({ directory, call, run, before, registryAfter, payload }) => {
  const install = call("install", payload);
  assert.equal(install.status, 0, install.output);
  assert.equal(install.output.includes("SENTINEL"), false);
  assert.equal(readFileSync(join(directory, "registries.yaml"), "utf8"), registryAfter);
  assert.equal(call("verify-backup").status, 0);
  const duplicate = call("install", payload);
  assert.notEqual(duplicate.status, 0);
  assert.equal(readFileSync(join(directory, "registries.yaml"), "utf8"), registryAfter);
  assert.equal(call("rollback").status, 0);
  assert.equal(readFileSync(join(directory, "registries.yaml"), "utf8"), before);
  assert.equal(run("test ! -e /etc/rancher/k3s/config.yaml").status, 0);
}));
test("actual node worker refuses foreign drift and recovers a truncated install before effects", workerOptions, t => fixture(t, ({ directory, call, before, payload }) => {
  assert.notEqual(call("install", payload.split("\n")[0] + "\n").status, 0);
  assert.equal(readFileSync(join(directory, "registries.yaml"), "utf8"), before);
  assert.equal(call("rollback").status, 0);
  const foreign = before + "\n";
  writeFileSync(join(directory, "registries.yaml"), foreign);
  const rejected = call("rollback");
  assert.notEqual(rejected.status, 0);
  assert.equal(rejected.output.includes("SENTINEL"), false);
  assert.equal(readFileSync(join(directory, "registries.yaml"), "utf8"), foreign);
}));
