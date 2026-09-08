import test from "node:test";
import assert from "node:assert/strict";
import { planFrontendDrainProfile, supportsFrontendDrain } from "./frontend-drain-profile.mjs";
import { fingerprint } from "./scoped-release.mjs";

function fixture() {
  return { apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "staff-control-center", namespace: "kodex-system",
    uid: "fixture-uid", resourceVersion: "1", generation: 1, labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/local-profile": "hot-reload" } },
  spec: { replicas: 1, strategy: { type: "RollingUpdate", rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } },
    template: { metadata: { annotations: { "kubectl.kubernetes.io/default-container": "staff-control-center" } },
      spec: { containers: [{ name: "staff-control-center", image: "unchanged-fixture-image", env: [{ name: "FIXTURE", value: "unchanged" }],
        lifecycle: { postStart: { exec: { command: ["fixture"] } } } }], volumes: [{ name: "unchanged", emptyDir: {} }], terminationGracePeriodSeconds: 30 } } },
  status: { observedGeneration: 1, replicas: 1, updatedReplicas: 1, availableReplicas: 1 } };
}

test("frontend drain changes only its preStop, grace and profile annotation", () => {
  const before = fixture(), plan = planFrontendDrainProfile(before), after = structuredClone(before.spec);
  after.template.spec.containers[0].lifecycle.preStop = { sleep: { seconds: 10 } };
  after.template.spec.terminationGracePeriodSeconds = 45;
  after.template.metadata.annotations["kodex.dev/frontend-drain-profile"] = "1";
  assert.equal(plan.afterSpecSHA256, fingerprint(after));
  assert.equal(plan.beforeSpecSHA256, fingerprint(before.spec));
  const repeated = planFrontendDrainProfile({ ...before, spec: after });
  assert.equal(repeated.beforeSpecSHA256, repeated.afterSpecSHA256);
  assert.deepEqual(plan.patch.slice(0, 3).map((p) => p.op), ["test", "test", "test"]);
});

test("frontend migration refuses foreign targets, unready rollout and custom hooks", () => {
  for (const change of [
    (d) => { d.metadata.name = "control-api-gateway"; },
    (d) => { d.metadata.labels["kodex.dev/local-profile"] = "production"; },
    (d) => { d.status.availableReplicas = 0; },
    (d) => { d.spec.template.spec.containers.push({ name: "foreign" }); },
    (d) => { d.spec.template.spec.containers[0].lifecycle.preStop = { exec: { command: ["custom"] } }; },
  ]) { const d = fixture(); change(d); assert.throws(() => planFrontendDrainProfile(d)); }
  const longer = fixture(); longer.spec.template.spec.terminationGracePeriodSeconds = 120;
  assert.equal(planFrontendDrainProfile(longer).patch.find((p) => p.path.endsWith("terminationGracePeriodSeconds")).value, 120);
});

test("sleep action requires stable API server and kubelet support", () => {
  assert.equal(supportsFrontendDrain("v1.36.3+k3s1"), true);
  assert.equal(supportsFrontendDrain("v1.34.0"), true);
  assert.equal(supportsFrontendDrain("v1.33.9"), false);
  assert.equal(supportsFrontendDrain("unknown"), false);
});
