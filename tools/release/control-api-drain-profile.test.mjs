import test from "node:test";
import assert from "node:assert/strict";
import { planDrainProfile } from "./control-api-drain-profile.mjs";

function fixture() {
  return { apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "control-api-gateway", namespace: "kodex-system",
    uid: "11111111-1111-4111-8111-111111111111", resourceVersion: "1", generation: 1,
    labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/local-profile": "hot-reload" } },
  spec: { replicas: 1, strategy: { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } }, template: { metadata: { annotations: { existing: "kept" } }, spec: { terminationGracePeriodSeconds: 35, containers: [
    { name: "control-api-gateway", image: "unchanged", env: [{ name: "CONTROL_API_GATEWAY_SHUTDOWN_TIMEOUT", value: "20s" }] },
    { name: "internal-rpc-authority-issuer", image: "unchanged-security", volumeMounts: [{ name: "trust", mountPath: "/trust" }] },
  ], volumes: [{ name: "trust", secret: { secretName: "unchanged" } }] } } },
  status: { observedGeneration: 1, replicas: 1, updatedReplicas: 1, availableReplicas: 1 } };
}

test("drain migration changes only own hooks, grace and profile annotation", () => {
  const deployment = fixture(), before = structuredClone(deployment);
  const plan = planDrainProfile(deployment);
  assert.deepEqual(deployment, before);
  assert.equal(plan.changed, true);
  assert.deepEqual(plan.patch.filter((entry) => entry.path.endsWith("/lifecycle")).map((entry) => entry.value.preStop.sleep.seconds), [10, 40]);
  assert.equal(plan.patch.find((entry) => entry.path.endsWith("terminationGracePeriodSeconds")).value, 120);
  assert.ok(plan.patch.every((entry) => !/image|volume|\/env/.test(entry.path)));
});

test("drain migration refuses foreign ownership, incomplete rollout and custom hooks", () => {
  for (const mutate of [
    (d) => { d.metadata.labels = {}; },
    (d) => { d.spec.strategy.type = "Recreate"; },
    (d) => { d.status.availableReplicas = 0; },
    (d) => { d.spec.template.spec.containers[0].env[0].value = "60s"; },
    (d) => { d.spec.template.spec.containers[1].lifecycle = { preStop: { exec: { command: ["custom"] } } }; },
  ]) { const deployment = fixture(); mutate(deployment); assert.throws(() => planDrainProfile(deployment)); }
});
