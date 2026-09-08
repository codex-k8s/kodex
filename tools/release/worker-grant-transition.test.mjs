import { test } from "node:test";
import assert from "node:assert/strict";
import { planWorkerTransition, requireInstanceWriters, verifyOverlap, requireUnchangedPlan } from "./worker-grant-transition.mjs";
import { fingerprint } from "./scoped-release.mjs";

const uid = "11111111-1111-4111-8111-111111111111";
const otherUID = "22222222-2222-4222-8222-222222222222";
function fixture(native = false) {
  const grant = { name: "runtime-controller-platform-worker-grant-agent", image: "unchanged-agent",
    env: [{ name: "SIGNER_KEY_PATH", value: "/private/key" }], volumeMounts: [{ name: "signer", mountPath: "/private" }] };
  return { apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "runtime-controller", namespace: "kodex-system", uid,
    resourceVersion: "12", generation: 3, labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/local-profile": "hot-reload" } },
  spec: { replicas: 1, strategy: { type: "Recreate" }, template: { metadata: { annotations: { unchanged: "yes" } }, spec: {
    containers: [{ name: "runtime-controller", image: "unchanged-application", env: [{ name: "APP_SETTING", value: "same" }] }, ...(native ? [] : [grant])],
    initContainers: native ? [{ ...grant, restartPolicy: "Always" }] : [], volumes: [{ name: "signer", secret: { secretName: "reference" } }],
  } } }, status: { observedGeneration: 3, replicas: 1, updatedReplicas: 1, availableReplicas: 1, readyReplicas: 1 } };
}
function apply(deployment, plan) {
  const next = structuredClone(deployment);
  for (const patch of plan.patch.filter((item) => item.op !== "test")) {
    const parts = patch.path.split("/").slice(1); const key = parts.pop();
    let destination = next; for (const part of parts) destination = destination[part];
    destination[key] = structuredClone(patch.value);
  }
  next.status = { ...next.status, replicas: next.spec.replicas, updatedReplicas: next.spec.replicas,
    availableReplicas: next.spec.replicas, readyReplicas: next.spec.replicas };
  assert.equal(fingerprint(next.spec), plan.afterSpecSHA256);
  return next;
}

test("ordered transition covers regular/native writers without changing images, sources or signer", () => {
  for (const native of [false, true]) {
    const original = fixture(native); const copy = structuredClone(original);
    const activation = planWorkerTransition(original, "activate");
    assert.deepEqual(original, copy);
    assert.deepEqual(activation.patch.slice(0, 2).map((item) => item.path), ["/metadata/uid", "/metadata/resourceVersion"]);
    let state = apply(original, activation);
    requireInstanceWriters(state.spec.template);
    assert.deepEqual(state.spec.template.spec.containers[0], original.spec.template.spec.containers[0]);
    assert.deepEqual(state.spec.template.spec.volumes, original.spec.template.spec.volumes);
    const template = structuredClone(state.spec.template);
    for (const phase of ["overlap", "rolling", "settle"]) {
      state = apply(state, planWorkerTransition(state, phase));
      assert.deepEqual(state.spec.template, template);
    }
    assert.equal(state.spec.replicas, 1);
    assert.deepEqual(state.spec.strategy, { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } });
    assert.throws(() => planWorkerTransition(state, "activate"), /SINGLE_RECREATE_REQUIRED/);
  }
});

test("activation rejects foreign profiles, identity drift, partial rollout and caller instance", () => {
  for (const mutate of [
    (d) => { d.metadata.namespace = "production"; },
    (d) => { d.metadata.name = "foreign"; },
    (d) => { delete d.metadata.labels["kodex.dev/local-profile"]; },
    (d) => { d.metadata.deletionTimestamp = "2026-09-08T10:00:00Z"; },
    (d) => { d.status.readyReplicas = 0; },
    (d) => { d.status.observedGeneration = 2; },
    (d) => { d.status.replicas = 2; },
    (d) => { d.spec.paused = true; },
    (d) => { d.spec.strategy = { type: "RollingUpdate" }; },
    (d) => { d.spec.template.spec.containers[1].env.push({ name: "PLATFORM_WORKER_GRANT_INSTANCE_ID", value: uid }); },
  ]) {
    const state = fixture(); mutate(state); assert.throws(() => planWorkerTransition(state, "activate"));
  }
  for (const phase of ["overlap", "rolling", "settle", "unknown"]) assert.throws(() => planWorkerTransition(fixture(), phase));
});

test("each writer must use Pod UID; annotation alone and mixed writers are insufficient", () => {
  const active = apply(fixture(true), planWorkerTransition(fixture(true), "activate"));
  for (const mutate of [
    (d) => { d.spec.template.spec.initContainers[0].env.pop(); },
    (d) => { d.spec.template.spec.initContainers[0].env.at(-1).valueFrom.fieldRef.fieldPath = "metadata.name"; },
    (d) => { d.spec.template.spec.initContainers[0].env.at(-1).valueFrom.fieldRef.apiVersion = "foreign"; },
    (d) => { d.spec.template.spec.initContainers[0].env.push(structuredClone(d.spec.template.spec.initContainers[0].env.at(-1))); },
    (d) => { d.spec.template.spec.containers.push({ name: "second-platform-worker-grant-agent", env: [] }); },
  ]) { const invalid = structuredClone(active); mutate(invalid); assert.throws(() => planWorkerTransition(invalid, "overlap")); }
  assert.throws(() => planWorkerTransition(active, "rolling"), /TRANSITION_ORDER_REQUIRED/);
});

function observations() {
  const before = { at: "2026-09-08T10:00:00Z", floors: [{ workload: "runtime-controller", generation: 7 }],
    instances: [uid, otherUID].map((instance) => ({ workload: "runtime-controller", instance, generation: 7,
      revision: 100, expiresAt: "2026-09-08T10:04:00Z", updatedAt: "2026-09-08T10:00:00Z" })) };
  const after = structuredClone(before); after.at = "2026-09-08T10:01:35Z";
  for (const row of after.instances) { row.revision = 190; row.updatedAt = "2026-09-08T10:01:30Z"; }
  return { before, after };
}
test("overlap requires both durable streams advancing at the unchanged generation", () => {
  const { before, after } = observations();
  verifyOverlap(before, after, "runtime-controller", [uid, otherUID]);
  for (const mutate of [
    (s) => { s.instances.pop(); },
    (s) => { s.instances[1].revision = 100; },
    (s) => { s.instances[1].generation = 8; },
    (s) => { s.instances[1].expiresAt = "2026-09-08T10:01:00Z"; },
    (s) => { s.instances[1].updatedAt = before.at; },
    (s) => { s.floors[0].generation = 8; },
    (s) => { s.at = "2026-09-08T10:00:05Z"; },
    (s) => { s.at = "2026-09-08T10:06:00Z"; },
  ]) { const invalid = structuredClone(after); mutate(invalid); assert.throws(() => verifyOverlap(before, invalid, "runtime-controller", [uid, otherUID])); }
  assert.throws(() => verifyOverlap(before, after, "runtime-controller", [uid, uid]));
});

test("persisted plan rejects target, reader, cluster and CAS drift before any patch", () => {
  const plan = { ...planWorkerTransition(fixture(), "activate"), version: 1, id: uid,
    context: "staging", clusterUID: uid, namespaceUID: otherUID,
    readers: { uid, specSHA256: "reader-spec", pods: [{ uid: otherUID, source: { revision: "exact-reader" } }] } };
  requireUnchangedPlan(plan, structuredClone(plan));
  for (const key of ["context", "target", "phase", "uid", "resourceVersion", "beforeSpecSHA256", "afterSpecSHA256", "clusterUID", "namespaceUID", "handoffProofSHA256"]) {
    const current = structuredClone(plan); current[key] = "changed";
    assert.throws(() => requireUnchangedPlan(plan, current), /PLAN_PRECONDITION_CHANGED/);
  }
  for (const mutate of [
    (p) => { p.readers.pods[0].uid = uid; },
    (p) => { p.readers.pods[0].source.revision = "old-reader"; },
    (p) => { p.readers.specSHA256 = "changed"; },
  ]) { const current = structuredClone(plan); mutate(current); assert.throws(() => requireUnchangedPlan(plan, current)); }
});
