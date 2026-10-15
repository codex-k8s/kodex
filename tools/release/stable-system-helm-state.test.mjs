import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { matchSystemObjects, readSystemObjects, systemDesiredValues, systemExpectedObjects, systemObjectKey,
  systemObjectState, systemRecoveryManifest, systemRecoveryValues } from "./stable-system-helm-state.mjs";

const profile = JSON.parse(await readFile(new URL("../../infra/stable-system/helm-profiles.json", import.meta.url), "utf8")).profiles[0];
const object = (kind = "Deployment", name = "cert-manager") => ({ apiVersion: kind === "Deployment" ? "apps/v1" : "v1", kind,
  metadata: { name, namespace: profile.namespace, uid: name + "-uid", resourceVersion: "1", labels: { app: name } },
  spec: { replicas: 1, template: { spec: { containers: [{ name: "app", image: "example.invalid/app:v1", resources: {},
    env: [{ name: "EXAMPLE", value: "PRIVATE_STATE_SENTINEL" }] }] } } }, status: { readyReplicas: 1 } });
const bounds = { requests: { cpu: "50m", memory: "64Mi" }, limits: { cpu: "500m", memory: "256Mi" } };

test("object commitments bind configuration/UID, but not status and managedFields", () => {
  const a = object(), b = structuredClone(a); b.status.readyReplicas = 2;
  b.metadata.resourceVersion = "2"; b.metadata.managedFields = [{ manager: "controller" }];
  assert.deepEqual(systemObjectState(a), systemObjectState(b));
  b.spec.replicas = 2; assert.notDeepEqual(systemObjectState(a), systemObjectState(b));
  assert.equal(JSON.stringify(systemObjectState(a)).includes("PRIVATE_STATE_SENTINEL"), false);
  for (const kind of ["Secret", "ConfigMap"]) {
    const r = object(kind); r.data = { value: "PRIVATE_SECRET_SENTINEL" };
    const state = systemObjectState(r);
    assert.deepEqual(Object.keys(state).sort(), ["apiVersion", "kind", "name", "namespace", "resourceVersion", "uid"]);
  }
});

test("resource snapshot never requests Secret or ConfigMap data", async () => {
  const a = object(), secret = object("Secret", "credential"), cm = object("ConfigMap", "config");
  const result = await readSystemObjects(profile, [a, secret, cm], async (argv, input) => {
    const items = JSON.parse(input).items;
    assert.equal(JSON.stringify(items).includes("PRIVATE_STATE_SENTINEL"), false);
    if (items[0].kind === "Deployment") { assert.equal(argv.at(-1), "json"); return JSON.stringify({ items: [a] }); }
    assert.match(argv.at(-1), /^jsonpath=/); assert.equal(argv.at(-1).includes(".data"), false);
    return [secret, cm].map(r => [r.apiVersion, r.kind, r.metadata.namespace, r.metadata.name, r.metadata.uid, r.metadata.resourceVersion].join("|")).join("\n");
  });
  assert.equal(result.size, 3); assert.equal(JSON.stringify(result.get(systemObjectKey(secret))).includes(".data"), false);
});

test("forward recovery retains only PDB overlays, original configuration and secrets stay intact", () => {
  const before = { replicaCount: 1, cainjector: { resources: { limits: { memory: "200Mi" } } },
    private: "PRIVATE_SECRET_SENTINEL" }, snapshot = structuredClone(before);
  const next = systemDesiredValues(before, profile.values), recovery = systemRecoveryValues(profile, before);
  assert.equal(next.replicaCount, 2); assert.equal(recovery.replicaCount, 1);
  assert.deepEqual(recovery.cainjector.resources, before.cainjector.resources);
  assert.equal(recovery.podDisruptionBudget.minAvailable, 1); assert.equal(recovery.webhook.podDisruptionBudget.minAvailable, 1);
  assert.equal(recovery.private, before.private); assert.deepEqual(before, snapshot);
  const original = [object()], desired = [structuredClone(original[0]), { apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { name: "exact" } }];
  desired[0].spec.replicas = 2;
  const restored = systemRecoveryManifest(original, desired);
  assert.deepEqual(restored[0], original[0]); assert.deepEqual(restored[1], desired[1]);
});

test("expected object states bind forward/recovery and reject foreign new PDB", async () => {
  const a = object(), after = structuredClone(a); after.spec.replicas = 2; after.spec.template.spec.containers[0].resources = bounds;
  const pdb = { apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { name: "cert-manager", namespace: profile.namespace },
    spec: { minAvailable: 1, selector: { matchLabels: { app: "cert-manager" } } } };
  const current = new Map([[systemObjectKey(a), a]]);
  const expected = await systemExpectedObjects(profile, [a], [after, pdb], current, async (argv, input) => {
    assert.ok(argv.includes("--dry-run=server")); return input;
  });
  assert.equal(expected.length, 2); const deployment = expected.find(e => e.kind === "Deployment"), support = expected.find(e => e.kind === "PodDisruptionBudget");
  assert.equal(deployment.uid, a.metadata.uid); assert.equal(deployment.recovery, deployment.before);
  assert.notEqual(deployment.before, deployment.after); assert.equal(support.uid, null); assert.equal(support.recovery, support.after);
  current.set(systemObjectKey(pdb), { ...pdb, metadata: { ...pdb.metadata, uid: "foreign", resourceVersion: "1" } });
  await assert.rejects(systemExpectedObjects(profile, [a], [after, pdb], current, async () => ""), /SYSTEM_HELM_SUPPORT_COLLISION/);
});

test("phase matching rejects credential drift, foreign UID and unplanned live changes", async () => {
  const a = object(), secret = object("Secret", "credential"), after = structuredClone(a);
  after.spec.replicas = 2; after.spec.template.spec.containers[0].resources = bounds;
  const original = new Map([a, secret].map(r => [systemObjectKey(r), r]));
  const expected = await systemExpectedObjects(profile, [a, secret], [after, secret], original, async () => "");
  assert.equal(matchSystemObjects(expected, original, ["before"]).length, 2);
  const serving = new Map([after, secret].map(r => [systemObjectKey(r), r]));
  assert.equal(matchSystemObjects(expected, serving, ["after"]).length, 2);
  for (const mutate of [r => { r.get(systemObjectKey(a)).metadata.uid = "foreign"; },
    r => { r.get(systemObjectKey(secret)).metadata.resourceVersion = "2"; },
    r => { r.get(systemObjectKey(a)).spec.template.spec.containers[0].env = []; },
    r => { r.delete(systemObjectKey(a)); }]) {
    const broken = structuredClone(serving); mutate(broken);
    assert.throws(() => matchSystemObjects(expected, broken, ["before", "after", "recovery"]), /SYSTEM_HELM_/);
  }
});
