import { test } from "node:test";
import assert from "node:assert/strict";
import { planWebSocketTransition, legacyEnv } from "./websocket-transition.mjs";
const now = Date.parse("2026-09-08T12:00:00Z");
function fixture() { return { apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "control-api-gateway", namespace: "kodex-system", uid: "11111111-1111-4111-8111-111111111111", resourceVersion: "4", generation: 3, labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/environment": "staging" } }, spec: { replicas: 2, strategy: { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } }, template: { spec: { containers: [{ name: "control-api-gateway", image: "unchanged", env: [{ name: "UNCHANGED", valueFrom: { secretKeyRef: { name: "reference-only", key: "key" } } }] }, { name: "sidecar", image: "unchanged" }] } } }, status: { observedGeneration: 3, updatedReplicas: 2, availableReplicas: 2, replicas: 2 } }; }
test("cutoff patch is restricted to API env and metadata with UID/resourceVersion CAS", () => {
  const original = fixture(); const before = structuredClone(original);
  const plan = planWebSocketTransition(original, "2026-09-08T13:00:00Z", now);
  assert.deepEqual(original, before);
  assert.deepEqual(plan.patch.filter((item) => item.op !== "test").map((item) => item.path), ["/spec/template/spec/containers/0/env", "/metadata/annotations"]);
  assert.deepEqual(plan.patch[3].value[0], before.spec.template.spec.containers[0].env[0]);
  assert.deepEqual(plan.patch.slice(0, 2).map((item) => item.path), ["/metadata/uid", "/metadata/resourceVersion"]);
});
test("rejects production, foreign identity, incomplete rollout and cutoff extension", () => {
  for (const mutate of [
    (d) => { d.metadata.namespace = "foreign"; },
    (d) => { d.metadata.labels["kodex.dev/environment"] = "production"; },
    (d) => { d.status.updatedReplicas = 1; },
    (d) => { d.spec.strategy.rollingUpdate.maxUnavailable = 1; },
    (d) => { d.spec.template.spec.containers[0].env.push({ name: legacyEnv, value: "2026-09-08T12:30:00Z" }); },
    (d) => { d.metadata.annotations = { "kodex.dev/ws-v1-retired": "true" }; },
  ]) { const d = fixture(); mutate(d); assert.throws(() => planWebSocketTransition(d, "2026-09-08T13:00:00Z", now)); }
  for (const until of ["bad", "2026-09-08T11:00:00Z", "2026-09-09T12:00:01Z"]) assert.throws(() => planWebSocketTransition(fixture(), until, now));
});
test("retirement removes only legacy env and prevents reopening", () => {
  const d = fixture(); d.spec.template.spec.containers[0].env.push({ name: legacyEnv, value: "2026-09-08T11:00:00Z" });
  const plan = planWebSocketTransition(d, null, now);
  assert.equal(plan.patch[3].value.length, 1);
  assert.equal(plan.patch[4].value["kodex.dev/ws-v1-retired"], "true");
});
