import assert from "node:assert/strict";
import test from "node:test";
import { fingerprint, summarizeInventory, summarizeWorkload } from "./stable-inventory.mjs";

function fixture(kind = "Deployment", name = "control-plane") {
  return { kind, metadata: { name, namespace: "kodex-system", uid: "fixture-uid", resourceVersion: "1" },
    spec: { replicas: 1, template: { spec: { containers: [{ name: "app", image: "example.invalid/app@sha256:" + "a".repeat(64),
      command: ["/workspace/tools/dev/run-go-hot-reload.sh"], env: [{ name: "PRIVATE", value: "PRIVATE_SENTINEL_1810" }],
      volumeMounts: [{ name: "source", mountPath: "/workspace", readOnly: true }] }],
    volumes: [{ name: "source", hostPath: { path: "/private/source-sentinel-1810" } }] } } }, status: { readyReplicas: 1 } };
}

test("service inventory plans two replicas without changing input", () => {
  const value = fixture(), before = structuredClone(value);
  const row = summarizeWorkload(value);
  assert.equal(row.category, "SERVICE"); assert.equal(row.targetReplicas, 2);
  assert.equal(row.hotContainerCount, 1); assert.equal(row.hostSourceMountCount, 1);
  assert.equal(row.images[0].immutable, true); assert.deepEqual(value, before);
  const encoded = JSON.stringify(row);
  assert.equal(encoded.includes("PRIVATE_SENTINEL_1810"), false);
  assert.equal(encoded.includes("source-sentinel-1810"), false);
  assert.equal(encoded.includes("command"), false);
});

test("stateful DB broker and object storage retain current replicas", () => {
  for (const name of ["kodex-postgresql", "kodex-nats", "seaweedfs", "proxy-session-store"]) {
    const row = summarizeWorkload(fixture("StatefulSet", name));
    assert.equal(row.category, "DATA"); assert.equal(row.targetReplicas, 1);
  }
  assert.equal(summarizeWorkload(fixture("Deployment", "kodex-nats")).targetReplicas, 1);
});

test("jobs cron and standalone executions are never doubled", () => {
  const job = fixture("Job", "synthetic-job");
  const cron = fixture("CronJob", "synthetic-cron"); cron.spec.jobTemplate = { spec: { template: cron.spec.template } };
  const pod = fixture("Pod", "runtime-turn-fixture"); pod.spec = pod.spec.template.spec;
  for (const value of [job, cron]) {
    const row = summarizeWorkload(value); assert.equal(row.category, "EXECUTION"); assert.equal(row.targetReplicas, null);
  }
  assert.equal(summarizeWorkload(pod).category, "INSTANCE");
  assert.equal(summarizeWorkload(pod).targetReplicas, null);
  assert.equal(summarizeWorkload(fixture("DaemonSet", "otel-agent")).category, "PER_NODE");
});

test("shared PVC service requires storage protocol review", () => {
  const value = fixture("Deployment", "kodex-image-registry-push");
  value.spec.template.spec.volumes.push({ name: "data", persistentVolumeClaim: { claimName: "exact-data" } });
  assert.equal(summarizeWorkload(value).requiresStorageReview, true);
  for (const name of ["kodex-monitoring-grafana", "alertmanager-kodex-monitoring-kube-prom-alertmanager"]) {
    const row = summarizeWorkload(fixture("StatefulSet", name));
    assert.equal(row.category, "SERVICE"); assert.equal(row.targetReplicas, 2);
    assert.equal(row.requiresStorageReview, true);
  }
  assert.equal(summarizeWorkload(fixture("StatefulSet", "prometheus-kodex-monitoring-kube-prom-prometheus")).category, "DATA");
});

test("native sidecar hot command and source mounts are included", () => {
  const value = fixture(); value.spec.template.spec.initContainers = structuredClone(value.spec.template.spec.containers);
  value.spec.template.spec.initContainers[0].restartPolicy = "Always";
  const row = summarizeWorkload(value); assert.equal(row.hotContainerCount, 2); assert.equal(row.hostSourceMountCount, 2);
});

test("inventory identity duplicates corrupt input and bounds fail closed", () => {
  assert.throws(() => summarizeInventory([]), /INVENTORY_BOUND_INVALID/);
  assert.throws(() => summarizeInventory([fixture(), fixture()]), /DUPLICATE_WORKLOAD/);
  for (const change of [v => { v.metadata.namespace = "foreign"; }, v => { v.metadata.uid = ""; },
    v => { v.kind = "Secret"; }, v => { v.spec.replicas = -1; }, v => { v.spec.template.spec.containers = []; }]) {
    const value = fixture(); change(value); assert.throws(() => summarizeWorkload(value));
  }
});

test("commitments are canonical but sensitive to changed source", () => {
  assert.equal(fingerprint({ a: 1, b: 2 }), fingerprint({ b: 2, a: 1 }));
  const a = summarizeInventory([fixture()]), changed = fixture(); changed.spec.replicas = 2;
  assert.notEqual(a.inventorySHA256, summarizeInventory([changed]).inventorySHA256);
});
