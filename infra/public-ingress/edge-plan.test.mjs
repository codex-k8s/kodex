import assert from "node:assert/strict";
import { chmod, mkdtemp, open, rm, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { protectedCommitment, readPrivateJSON } from "./edge-plan.mjs";

test("protected ingress baseline excludes only owned edge names and detects routing drift", () => {
  const ingress = { kind: "Deployment", metadata: { name: "traefik", uid: "uid", annotations: { exact: "fixture" } }, spec: { replicas: 1 } };
  const route = { kind: "Ingress", metadata: { name: "existing", uid: "route" }, spec: { rules: [{ host: "neighbor.example.test" }] } };
  const hash = protectedCommitment([ingress, route]);
  assert.equal(hash, protectedCommitment([route, ingress, { kind: "Service", metadata: { name: "kodex-public-edge",
    labels: { "app.kubernetes.io/managed-by": "kodex-public-edge" } }, spec: {} }]));
  assert.notEqual(hash, protectedCommitment([route, ingress, { kind: "Service", metadata: { name: "kodex-public-edge",
    uid: "unmanaged" }, spec: { ports: [] } }]));
  const changed = structuredClone(route); changed.spec.rules[0].host = "changed.example.test";
  assert.notEqual(hash, protectedCommitment([ingress, changed]));
  assert.throws(() => protectedCommitment([route]), /INGRESS_REQUIRED/);
});

test("private plans reject broad modes and links without reading their contents", async () => {
  const directory = await mkdtemp(join(tmpdir(), "kodex-edge-plan-test-"));
  try {
    const path = join(directory, "plan.json");
    const file = await open(path, "wx", 0o600);
    try { await file.writeFile('{"fixture":true}'); } finally { await file.close(); }
    assert.deepEqual(await readPrivateJSON(path), { fixture: true });
    await chmod(path, 0o644);
    await assert.rejects(readPrivateJSON(path), /PRIVATE_FILE_REQUIRED/);
    await chmod(path, 0o600);
    await symlink(path, join(directory, "link"));
    await assert.rejects(readPrivateJSON(join(directory, "link")));
  } finally { await rm(directory, { recursive: true, force: true }); }
});
