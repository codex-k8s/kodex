import test from "node:test";
import assert from "node:assert/strict";
import { planSourceChange, validSource } from "./application-source.mjs";

const oldRoot = "/srv/kodex-dev/old";
const newRoot = "/srv/kodex-dev/new";
const oldSHA = "a".repeat(40), newSHA = "b".repeat(40);
const source = { path: newRoot, revision: newSHA };
const inspect = (path) => ({ revision: path === oldRoot ? oldSHA : newSHA, dependenciesSHA256: "d".repeat(64) });
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
});
