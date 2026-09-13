import test from "node:test";
import assert from "node:assert/strict";
import { boundedBatch, fingerprint, planTarget, validateManifest } from "./scoped-release.mjs";

const image = (hex) => `registry.example/kodex/email-bridge@sha256:${hex.repeat(64)}`;
const releaseID = "11111111-1111-4111-8111-111111111111";
const target = { name: "email-bridge", image: image("b") };

function fixture() {
  return {
    apiVersion: "apps/v1", kind: "Deployment",
    metadata: { name: "email-bridge", namespace: "kodex-system",
      uid: "22222222-2222-4222-8222-222222222222", generation: 1, resourceVersion: "101",
      labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/environment": "staging" } },
    spec: { replicas: 2, strategy: { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } },
      template: { metadata: { annotations: { "kodex.dev/worker-grant-format": "2" } },
        spec: { containers: [
          { name: "email-bridge", image: image("a"), env: [{ name: "EXAMPLE_FILE", value: "/secrets/example" }] },
          { name: "platform-worker-grant-agent", image: image("c"),
            env: [{ name: "PLATFORM_WORKER_GRANT_INSTANCE_ID", valueFrom: { fieldRef: { fieldPath: "metadata.uid" } } }] },
          { name: "internal-rpc-authority-issuer", image: image("d") },
        ], volumes: [{ name: "trust", secret: { secretName: "example-trust" } }] } } },
    status: { observedGeneration: 1, replicas: 2, availableReplicas: 2, updatedReplicas: 2 },
  };
}

test("application patch preserves sidecars, trust, env, replicas and strategy", () => {
  const current = fixture();
  const before = structuredClone(current);
  const result = planTarget(current, target, releaseID);
  assert.deepEqual(current, before);
  assert.deepEqual(result.patch.map((entry) => entry.path), [
    "/metadata/uid", "/metadata/resourceVersion", "/spec/template/spec/containers/0/name",
    "/spec/template/spec/containers/0/image", "/spec/template/spec/containers/0/image",
    "/spec/template/metadata/annotations",
  ]);
  const expected = structuredClone(current.spec);
  expected.template.spec.containers[0].image = target.image;
  expected.template.metadata.annotations["kodex.dev/application-release"] = releaseID;
  assert.equal(result.beforeSpecSHA256, fingerprint(current.spec));
  assert.equal(result.afterSpecSHA256, fingerprint(expected));
  assert.deepEqual(result.rollback, { name: "email-bridge", image: image("a"), expectedImage: image("b"), rollbackOf: releaseID });
});

test("unsafe profiles, old grants and incomplete rollouts are rejected", () => {
  const mutations = [
    (d) => { d.spec.strategy = { type: "Recreate" }; },
    (d) => { d.spec.strategy.rollingUpdate.maxUnavailable = 1; },
    (d) => { d.spec.strategy.rollingUpdate.maxSurge = 0; },
    (d) => { d.metadata.labels["kodex.dev/environment"] = "production"; },
    (d) => { d.metadata.labels["app.kubernetes.io/part-of"] = "foreign"; },
    (d) => { d.metadata.namespace = "foreign"; },
    (d) => { d.status.availableReplicas = 1; },
    (d) => { d.status.observedGeneration = 0; },
    (d) => { d.status.updatedReplicas = 1; },
    (d) => { d.status.replicas = 3; },
    (d) => { d.spec.template.metadata.annotations["kodex.dev/worker-grant-format"] = "1"; },
    (d) => { d.spec.template.spec.containers[1].env[0].valueFrom.fieldRef.fieldPath = "metadata.name"; },
    (d) => { d.spec.template.spec.containers[1].env[0].value = "caller-instance"; },
    (d) => { d.spec.template.spec.containers[0].image = "registry.example/app:latest"; },
    (d) => { d.spec.template.metadata.annotations["kubectl.kubernetes.io/default-container"] = "internal-rpc-authority-issuer"; },
  ];
  for (const mutate of mutations) {
    const current = fixture(); mutate(current);
    assert.throws(() => planTarget(current, target, releaseID));
  }
});

test("readiness recovery accepts only one stable unready replica and keeps normal release guard", () => {
  const current = fixture();
  current.spec.replicas = 1;
  current.status = { observedGeneration: 1, replicas: 1, updatedReplicas: 1, readyReplicas: 0, availableReplicas: 0 };
  assert.throws(() => planTarget(current, target, releaseID), /AVAILABLE_REPLICAS_REQUIRED/);
  const recovered = planTarget(current, target, releaseID, undefined, "recovery");
  assert.equal(recovered.name, target.name);
  for (const mutate of [
    (deployment) => { deployment.status.readyReplicas = 1; deployment.status.availableReplicas = 1; },
    (deployment) => { deployment.status.updatedReplicas = 0; },
    (deployment) => { deployment.status.replicas = 2; },
    (deployment) => { deployment.spec.replicas = 2; },
  ]) {
    const invalid = structuredClone(current); mutate(invalid);
    assert.throws(() => planTarget(invalid, target, releaseID, undefined, "recovery"), /UNREADY_SINGLE_REPLICA_REQUIRED/);
  }
  assert.throws(() => planTarget(current,
    { ...target, rollbackOf: releaseID, expectedImage: image("a") }, releaseID, undefined, "recovery"),
  /UNREADY_SINGLE_REPLICA_REQUIRED/);
});

test("image-only rollback accepts stuck rollout but requires exact release", () => {
  const current = fixture();
  current.spec.template.spec.containers[0].image = image("b");
  current.spec.template.metadata.annotations["kodex.dev/application-release"] = releaseID;
  current.status.updatedReplicas = 1;
  current.status.replicas = 3;
  const rollback = { name: "email-bridge", image: image("a"), expectedImage: image("b"), rollbackOf: releaseID };
  assert.equal(planTarget(current, rollback, "33333333-3333-4333-8333-333333333333").image, image("a"));
  assert.throws(() => planTarget(current, { ...rollback, expectedImage: image("c") }, releaseID), /ROLLBACK_RELEASE_MISMATCH/);
  assert.throws(() => planTarget(current, { ...rollback, rollbackOf: "44444444-4444-4444-8444-444444444444" }, releaseID), /ROLLBACK_RELEASE_MISMATCH/);
});

test("native and renamed grant writers require exact Downward API instance grants", () => {
  for (const renamed of [false, true]) {
    const current = fixture();
    const [agent] = current.spec.template.spec.containers.splice(1, 1);
    agent.restartPolicy = "Always";
    if (renamed) {
      agent.name = "worker-credentials";
      agent.command = ["/usr/local/bin/internal-rpc-authority-platform-worker-grant-agent"];
    }
    current.spec.template.spec.initContainers = [agent];
    const before = structuredClone(current);
    planTarget(current, target, releaseID);
    assert.deepEqual(current, before);
    for (const mutate of [
      (d) => { d.spec.template.metadata.annotations["kodex.dev/worker-grant-format"] = "1"; },
      (d) => { d.spec.template.spec.initContainers[0].env = []; },
      (d) => { d.spec.template.spec.initContainers[0].env[0].valueFrom.fieldRef.fieldPath = "metadata.name"; },
      (d) => { d.spec.template.spec.initContainers[0].env[0].value = "caller-instance"; },
      (d) => { d.spec.template.spec.initContainers[0].env.push(structuredClone(agent.env[0])); },
      (d) => { d.spec.template.spec.containers.push({ name: "other-platform-worker-grant-agent", image: image("c"), env: [] }); },
    ]) {
      const invalid = structuredClone(current); mutate(invalid);
      assert.throws(() => planTarget(invalid, target, releaseID), /INSTANCE_GRANTS_REQUIRED/);
    }
  }
});

test("manifest rejects duplicate targets, security units and mutable images", () => {
  for (const targets of [
    [target, target], [], [{ ...target, name: "internal-rpc-authority-publisher" }],
    [{ ...target, image: "registry.example/app:latest" }], [{ ...target, image: image("0") }],
    [{ ...target, replicaCount: 0 }], [{ ...target, expectedImage: image("a") }],
  ]) assert.throws(() => validateManifest({ version: 1, targets }));
  assert.equal(validateManifest({ version: 1, targets: [target] }).targets.length, 1);
});

test("independent batch contains failure and limits concurrent work", async () => {
  const items = Array.from({ length: 7 }, (_, index) => ({ name: `target-${index}` }));
  let active = 0, maximum = 0, completed = 0;
  const result = await boundedBatch(items, 2, async (item) => {
    active++; maximum = Math.max(maximum, active);
    try {
      await new Promise((done) => setTimeout(done, 5));
      if (item.name === "target-1") throw new Error("synthetic failure");
      completed++;
      return { name: item.name, status: "PASS" };
    } finally { active--; }
  });
  assert.equal(maximum, 2);
  assert.equal(completed, 6);
  assert.equal(result[1].status, "FAIL");
  assert.equal(result.filter((item) => item.status === "PASS").length, 6);
  await assert.rejects(boundedBatch(items, 0, async () => {}), /INVALID_PARALLELISM/);
});

test("spec fingerprint is stable across object key ordering, not array order", () => {
  assert.equal(fingerprint({ b: 2, a: 1 }), fingerprint({ a: 1, b: 2 }));
  assert.notEqual(fingerprint([1, 2]), fingerprint([2, 1]));
});

test("source-only release and rollback preserve the security source and bind exact revision", () => {
  const oldRoot = "/srv/kodex-dev/old", newRoot = "/srv/kodex-dev/new";
  const oldSHA = "a".repeat(40), newSHA = "b".repeat(40);
  const inspect = (path) => ({ revision: path === oldRoot ? oldSHA : newSHA });
  const current = fixture();
  current.metadata.labels["kodex.dev/local-profile"] = "hot-reload";
  for (const container of current.spec.template.spec.containers)
    container.volumeMounts = [{ name: "dev-source", mountPath: "/workspace", readOnly: true }];
  current.spec.template.spec.volumes.push({ name: "dev-source", hostPath: { path: oldRoot, type: "Directory" } });
  const requested = { name: "email-bridge", source: { path: newRoot, revision: newSHA } };
  const planned = planTarget(current, requested, releaseID, inspect);
  assert.equal(planned.image, image("a"));
  assert.deepEqual(planned.rollback.source, { path: oldRoot, revision: oldSHA });
  assert.deepEqual(planned.rollback.expectedSource, requested.source);
  const after = structuredClone(current);
  for (const entry of planned.patch) {
    if (entry.op === "test") continue;
    const path = entry.path.slice(1).split("/"); const key = path.pop();
    let parent = after; for (const part of path) parent = parent[part];
    if (key === "-") parent.push(structuredClone(entry.value));
    else parent[key] = structuredClone(entry.value);
  }
  assert.equal(fingerprint(after.spec), planned.afterSpecSHA256);
  assert.deepEqual(after.spec.template.spec.containers.slice(1), current.spec.template.spec.containers.slice(1));
  const rollback = planTarget(after, planned.rollback, "55555555-5555-4555-8555-555555555555", inspect);
  assert.equal(rollback.image, image("a"));
  assert.throws(() => planTarget(after, { ...planned.rollback, expectedSource: { path: newRoot, revision: oldSHA } }, releaseID, inspect), /ROLLBACK_SOURCE_MISMATCH/);
});
