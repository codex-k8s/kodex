import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createApplicationSource, inspectSource, planSourceChange, prepareApplicationSource, validSource } from "./application-source.mjs";

const oldRoot = "/srv/kodex-dev/old";
const newRoot = "/srv/kodex-dev/new";
const oldSHA = "a".repeat(40), newSHA = "b".repeat(40);
const source = { path: newRoot, revision: newSHA };
const inspect = (path) => ({ revision: path === oldRoot ? oldSHA : newSHA, dependenciesSHA256: "d".repeat(64), mountpointsReady: true });
function fixture() {
  return { metadata: { name: "control-api-gateway", labels: { "kodex.dev/local-profile": "hot-reload" } },
    spec: { template: { spec: { containers: [
      { name: "control-api-gateway", volumeMounts: [{ name: "dev-source", mountPath: "/workspace", readOnly: true }] },
      { name: "internal-rpc-authority-issuer", volumeMounts: [{ name: "dev-source", mountPath: "/workspace", readOnly: true }] },
    ], initContainers: [{ name: "authority-init", volumeMounts: [{ name: "dev-source", mountPath: "/workspace", readOnly: true }] }],
    volumes: [{ name: "dev-source", hostPath: { path: oldRoot, type: "Directory" } }, { name: "trust", secret: { secretName: "existing-trust" } }] } } } };
}

test("first source update detaches only application from shared sidecar checkout", () => {
  const current = fixture(), after = structuredClone(current.spec);
  const result = planSourceChange(current, after, 0, source, inspect);
  assert.deepEqual(after.template.spec.containers[1], current.spec.template.spec.containers[1]);
  assert.deepEqual(after.template.spec.initContainers, current.spec.template.spec.initContainers);
  assert.deepEqual(after.template.spec.volumes.slice(0, 2), current.spec.template.spec.volumes);
  assert.equal(after.template.spec.containers[0].volumeMounts[0].name, "dev-application-source");
  assert.equal(after.template.spec.volumes[2].hostPath.path, newRoot);
  assert.deepEqual(result.before, { path: oldRoot, revision: oldSHA });
  assert.equal(result.patch.length, 2);
});

test("subsequent update and rollback keep dedicated source isolated", () => {
  const current = fixture(), after = structuredClone(current.spec);
  planSourceChange(current, after, 0, source, inspect);
  current.spec = after;
  const rolledBack = structuredClone(after);
  const result = planSourceChange(current, rolledBack, 0, { path: oldRoot, revision: oldSHA }, inspect);
  assert.equal(result.patch.length, 1);
  assert.equal(rolledBack.template.spec.volumes[2].hostPath.path, oldRoot);
  assert.deepEqual(rolledBack.template.spec.containers[1], after.template.spec.containers[1]);
});

test("source update rejects wrong revision, writable mount and sidecar sharing", () => {
  for (const mutate of [
    (d) => { d.metadata.labels = {}; },
    (d) => { d.spec.template.spec.containers[0].volumeMounts[0].readOnly = false; },
    (d) => { d.spec.template.spec.containers[1].volumeMounts[0].name = "dev-application-source"; },
  ]) { const current = fixture(); mutate(current); assert.throws(() => planSourceChange(current, structuredClone(current.spec), 0, source, inspect)); }
  const current = fixture();
  assert.throws(() => planSourceChange(current, structuredClone(current.spec), 0, { ...source, revision: oldSHA }, inspect), /SOURCE_REVISION_MISMATCH/);
  assert.equal(validSource({ path: "/srv/../private", revision: newSHA }), false);
});

test("frontend changes only source directory and requires prepared unchanged dependencies", () => {
  const current = fixture(); current.metadata.name = "staff-control-center";
  current.spec.template.spec.containers = [{ name: "staff-control-center", volumeMounts: [{ name: "dev-frontend-source", mountPath: "/app", readOnly: true }] }];
  current.spec.template.spec.initContainers = [];
  current.spec.template.spec.volumes = [
    { name: "dev-frontend-source", hostPath: { path: `${oldRoot}/services/staff/control-center` } },
    { name: "dev-frontend-runner", hostPath: { path: `${oldRoot}/tools/dev/run-frontend.sh` } },
  ];
  const after = structuredClone(current.spec);
  planSourceChange(current, after, 0, source, inspect);
  assert.equal(after.template.spec.volumes[0].hostPath.path, `${newRoot}/services/staff/control-center`);
  assert.deepEqual(after.template.spec.volumes[1], current.spec.template.spec.volumes[1]);
  assert.throws(() => planSourceChange(current, structuredClone(current.spec), 0, source,
    (path) => ({ ...inspect(path), dependenciesSHA256: (path === oldRoot ? "a" : "b").repeat(64) })), /FRONTEND_DEPENDENCY_PREPARATION_REQUIRED/);
  assert.throws(() => planSourceChange(current, structuredClone(current.spec), 0, source,
    (path) => ({ ...inspect(path), mountpointsReady: path === oldRoot })), /FRONTEND_MOUNTPOINT_PREPARATION_REQUIRED/);
  current.spec.template.spec.containers[0].volumeMounts.push({ name: "unknown", mountPath: "/app/unprepared" });
  assert.throws(() => planSourceChange(current, structuredClone(current.spec), 0, source, inspect), /FRONTEND_NESTED_MOUNT_UNSUPPORTED/);
});

function checkout() {
  const path = mkdtempSync(join(tmpdir(), "kodex-source-"));
  chmodSync(path, 0o755);
  const frontend = `${path}/services/staff/control-center`;
  mkdirSync(`${frontend}/public/config`, { recursive: true });
  writeFileSync(`${frontend}/package.json`, "{}\n");
  writeFileSync(`${frontend}/package-lock.json`, "{}\n");
  writeFileSync(`${frontend}/public/config/runtime-config.json`, "{}\n");
  writeFileSync(`${path}/.gitignore`, "node_modules/\nprivate-state\n");
  const git = (...args) => execFileSync("git", ["-C", path, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git("init"); git("remote", "add", "origin", "https://github.com/codex-k8s/kodex.git");
  git("add", "."); git("commit", "-m", "fixture");
  return { path, frontend, git, revision: git("rev-parse", "HEAD") };
}

test("source preparation creates only an ignored mountpoint and is repeatable", () => {
  const current = checkout();
  try {
    const before = inspectSource(current.path, true);
    assert.equal(before.mountpointsReady, false);
    const result = prepareApplicationSource({ path: current.path, revision: current.revision });
    assert.deepEqual(result.created, ["node_modules"]);
    assert.equal(inspectSource(current.path, true).mountpointsReady, true);
    assert.equal(current.git("status", "--porcelain", "--untracked-files=all"), "");
    assert.equal(inspectSource(current.path, true).dependenciesSHA256, before.dependenciesSHA256);
    assert.deepEqual(prepareApplicationSource({ path: current.path, revision: current.revision }).created, []);
  } finally { rmSync(current.path, { recursive: true, force: true }); }
});

test("source preparation rejects symlinks, wrong revision and dirty tracked input", () => {
  for (const change of [
    (f) => symlinkSync(tmpdir(), `${f.frontend}/node_modules`),
    (f) => { rmSync(`${f.frontend}/public`, { recursive: true }); symlinkSync(tmpdir(), `${f.frontend}/public`); },
    (f) => writeFileSync(`${f.frontend}/package.json`, "{\"changed\":true}\n"),
    (f) => { f.revision = "0".repeat(40); },
  ]) {
    const current = checkout();
    try { change(current); assert.throws(() => prepareApplicationSource({ path: current.path, revision: current.revision })); }
    finally { rmSync(current.path, { recursive: true, force: true }); }
  }
});

test("source preparation never overwrites an existing ignored directory", () => {
  const current = checkout();
  try {
    mkdirSync(`${current.frontend}/node_modules`);
    writeFileSync(`${current.frontend}/node_modules/sentinel`, "unchanged");
    assert.deepEqual(prepareApplicationSource({ path: current.path, revision: current.revision }).created, []);
    assert.equal(readFileSync(`${current.frontend}/node_modules/sentinel`, "utf8"), "unchanged");
  } finally { rmSync(current.path, { recursive: true, force: true }); }
});

test("runtime access rejects private tracked paths without changing permissions", () => {
  for (const relative of ["", "/services/staff", "/services/staff/control-center/package-lock.json"]) {
    const current = checkout();
    try {
      const path = current.path + relative;
      const mode = lstatSync(path).isDirectory() ? 0o700 : 0o600;
      chmodSync(path, mode);
      assert.throws(() => inspectSource(current.path, true), /SOURCE_RUNTIME_ACCESS_REQUIRED/);
      assert.equal(lstatSync(path).mode & 0o777, mode);
    } finally { rmSync(current.path, { recursive: true, force: true }); }
  }
});

test("source creation isolates a private caller mask and never copies ignored data", () => {
  const current = checkout(), parent = mkdtempSync(join(tmpdir(), "kodex-created-"));
  const source = { path: `${parent}/source`, revision: current.revision };
  writeFileSync(`${current.path}/private-state`, "private fixture", { mode: 0o600 });
  const previous = process.umask(0o077);
  try {
    const result = createApplicationSource(current.path, source);
    assert.equal(result.sourceCreated, true);
    assert.equal(inspectSource(source.path, true).mountpointsReady, true);
    assert.equal(lstatSync(source.path).mode & 0o777, 0o755);
    assert.equal(lstatSync(`${source.path}/services/staff/control-center/package-lock.json`).mode & 0o777, 0o644);
    assert.equal(lstatSync(`${source.path}/services/staff/control-center/node_modules`).mode & 0o777, 0o755);
    assert.equal(lstatSync(`${current.path}/private-state`).mode & 0o777, 0o600);
    assert.equal(readFileSync(`${current.path}/private-state`, "utf8"), "private fixture");
    assert.equal(existsSync(`${source.path}/private-state`), false);
    assert.throws(() => createApplicationSource(current.path, source), /SOURCE_DESTINATION_EXISTS/);
    writeFileSync(`${parent}/private-evidence`, "private fixture");
    assert.equal(lstatSync(`${parent}/private-evidence`).mode & 0o777, 0o600);
  } finally {
    process.umask(previous);
    current.git("worktree", "remove", "--force", source.path);
    rmSync(current.path, { recursive: true, force: true });
    rmSync(parent, { recursive: true, force: true });
  }
});

test("mountpoint creation restores a private mask and preserves executable files", () => {
  const current = checkout();
  const runner = `${current.path}/runner.sh`;
  writeFileSync(runner, "#!/bin/sh\nexit 0\n", { mode: 0o755 });
  current.git("add", "runner.sh"); current.git("commit", "-m", "executable fixture");
  current.revision = current.git("rev-parse", "HEAD");
  const previous = process.umask(0o077);
  try {
    prepareApplicationSource({ path: current.path, revision: current.revision });
    assert.equal(lstatSync(`${current.frontend}/node_modules`).mode & 0o777, 0o755);
    assert.equal(lstatSync(runner).mode & 0o777, 0o755);
    chmodSync(runner, 0o750);
    assert.throws(() => inspectSource(current.path), /SOURCE_RUNTIME_ACCESS_REQUIRED/);
    writeFileSync(`${current.path}/private-state`, "private fixture");
    assert.equal(lstatSync(`${current.path}/private-state`).mode & 0o777, 0o600);
  } finally { process.umask(previous); rmSync(current.path, { recursive: true, force: true }); }
});
