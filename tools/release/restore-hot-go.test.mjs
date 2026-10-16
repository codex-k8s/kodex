import assert from "node:assert/strict";
import test from "node:test";
import { hotSpec } from "./restore-hot-go.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";

const source = { path: "/srv/kodex-dev/application", revision: "a".repeat(40), contentSHA256: "b".repeat(64) };
function fixture() {
  const current = { metadata: { name: "control-plane", namespace: "kodex-system", uid: "exact-cp" }, spec: {
    replicas: 2, strategy: { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } },
    template: { metadata: { labels: { "kodex.dev/security-profile": "trusted-cluster", "kodex.dev/local-profile": "stable" }, annotations: {} },
      spec: { nodeSelector: { "kubernetes.io/hostname": "fixture-node" }, containers: [{ name: "control-plane", command: ["/usr/local/bin/control-plane"],
        image: "example.invalid/control-plane@sha256:" + "c".repeat(64), env: [{ name: "APPLICATION_VALUE", value: "synthetic" }], volumeMounts: [] },
      { name: "skill-scanner", image: "unchanged" }], volumes: [{ name: "database-ca", secret: { secretName: "database-ca" } }] } } } };
  const oldHot = structuredClone(current.spec);
  oldHot.replicas = 1; oldHot.strategy = { type: "Recreate" }; delete oldHot.template.spec.nodeSelector;
  const main = oldHot.template.spec.containers[0];
  main.command = ["/workspace/tools/dev/run-go-hot-reload.sh"];
  main.image = "docker.io/library/golang:1.26.6-alpine@sha256:" + "d".repeat(64);
  main.env.push({ name: "GOCACHE", value: "/go/build-cache/cache" });
  main.volumeMounts.push({ name: "dev-source", mountPath: "/workspace", readOnly: true });
  oldHot.template.spec.volumes.push({ name: "dev-source", hostPath: { path: "/old/source", type: "Directory" } });
  return { current, oldHot, recovery: hot => ({ name: "control-plane", uid: "exact-cp", recovery: { mode: "RESTORE_SOURCE", inverse: reverseSpecPatch(hot, current.spec) } }) };
}

test("hot restore preserves two replicas, rolling update, public config, sidecar and storage", () => {
  const { current, oldHot, recovery } = fixture(), after = hotSpec(current, recovery(oldHot), source);
  assert.equal(after.replicas, 2); assert.deepEqual(after.strategy, current.spec.strategy);
  assert.deepEqual(after.template.spec.nodeSelector, current.spec.template.spec.nodeSelector);
  assert.deepEqual(after.template.spec.containers[1], current.spec.template.spec.containers[1]);
  assert.deepEqual(after.template.spec.volumes[0], current.spec.template.spec.volumes[0]);
  assert.equal(after.template.spec.volumes[1].hostPath.path, source.path);
  assert.equal(after.template.metadata.annotations["kodex.dev/source-revision"], source.revision);
  assert.equal(after.template.spec.containers[0].env.find(e => e.name === "KODEX_DEV_POD_UID").valueFrom.fieldRef.fieldPath, "metadata.uid");
});

test("restore rejects foreign UID, writable source, changed app config, sidecar and persistent storage", () => {
  for (const mutate of [hot => hot.template.spec.containers[0].volumeMounts[0].readOnly = false,
    hot => hot.template.spec.containers[0].env[0].value = "changed",
    hot => hot.template.spec.containers[1].image = "changed",
    hot => hot.template.spec.volumes[0].secret.secretName = "foreign"]) {
    const { current, oldHot, recovery } = fixture(); mutate(oldHot);
    assert.throws(() => hotSpec(current, recovery(oldHot), source), /HOT_RESTORE_/);
  }
  const { current, oldHot, recovery } = fixture(), wrong = recovery(oldHot); wrong.uid = "foreign";
  assert.throws(() => hotSpec(current, wrong, source), /BOUNDARY/);
});

test("subsequent source checkpoint does not replay historical inverse or duplicate dev env", () => {
  const { current, oldHot, recovery } = fixture();
  const first = hotSpec(current, recovery(oldHot), source);
  const nextSource = { ...source, revision: "e".repeat(40), contentSHA256: "f".repeat(64) };
  const second = hotSpec({ ...current, spec: first }, recovery(oldHot), nextSource);
  assert.deepEqual(second.template.spec.containers, first.template.spec.containers);
  assert.deepEqual(second.template.spec.volumes, first.template.spec.volumes);
  assert.equal(second.template.metadata.annotations["kodex.dev/source-revision"], nextSource.revision);
});
