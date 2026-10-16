import assert from "node:assert/strict";
import test from "node:test";
import { nativeHelmJob } from "./stable-native-helm-job.mjs";
function fixture() {
  const job = { apiVersion: "batch/v1", kind: "Job", metadata: { name: "helm-install-traefik", namespace: "kube-system", uid: "job-uid",
    ownerReferences: [{ apiVersion: "helm.cattle.io/v1", kind: "HelmChart", name: "traefik", uid: "chart-uid" }] },
  spec: { template: { metadata: { annotations: { "helmcharts.helm.cattle.io/configHash": "SHA256=" + "A".repeat(64) } }, spec: {
    serviceAccountName: "helm-traefik", containers: [{ name: "helm", image: "rancher/klipper-helm:v0.10.0-build20260513",
      env: [{ name: "INSECURE_SKIP_TLS_VERIFY", value: "false" }, { name: "PLAIN_HTTP", value: "false" }, { name: "FAILURE_POLICY", value: "abort" }] }],
    volumes: [{ name: "values", projected: { sources: [{ secret: { name: "chart-values-traefik", items: [{ key: "HelmChartValuesContent", path: "values.yaml" }] } }] } }] } } },
  status: { succeeded: 1, conditions: [{ type: "Complete", status: "True" }] } };
  const pod = { kind: "Pod", metadata: { namespace: "kube-system", uid: "pod-uid", ownerReferences: [{ kind: "Job", name: "helm-install-traefik", uid: "job-uid", controller: true }] },
    spec: { containers: [{ name: "helm" }] }, status: { phase: "Succeeded", containerStatuses: [{ name: "helm", state: { terminated: { exitCode: 0 } } }] } };
  return { job, pods: { items: [pod] } };
}
test("native Helm terminal proof binds chart/job/pod lineage, config hash and both TLS flags", () => {
  const f = fixture(), proof = nativeHelmJob(f.job, f.pods, "chart-uid");
  assert.equal(proof.complete, true); assert.equal(proof.terminal, true); assert.deepEqual(proof.valuesKeys, ["HelmChartValuesContent"]);
  assert.equal(proof.podUIDs[0], "pod-uid");
});
test("complete Job with a running init/debug/regular container is not terminal", () => {
  for (const group of ["regular", "init", "debug"]) {
    const f = fixture(), p = f.pods.items[0], state = { name: group, state: { running: {} } };
    if (group === "regular") { p.spec.containers.push({ name: group }); p.status.containerStatuses.push(state); }
    if (group === "init") { p.spec.initContainers = [{ name: group }]; p.status.initContainerStatuses = [state]; }
    if (group === "debug") { p.spec.ephemeralContainers = [{ name: group }]; p.status.ephemeralContainerStatuses = [state]; }
    assert.equal(nativeHelmJob(f.job, f.pods, "chart-uid").terminal, false);
  }
});
test("foreign chart or pod owner and insecure Helm transport are rejected", () => {
  for (const mutate of [f => { f.job.metadata.ownerReferences[0].uid = "foreign"; }, f => { f.pods.items[0].metadata.ownerReferences[0].uid = "foreign"; },
    f => { f.job.spec.template.spec.containers[0].env[0].value = "true"; }, f => { f.job.spec.template.spec.serviceAccountName = "foreign"; }]) {
    const f = fixture(); mutate(f); assert.throws(() => nativeHelmJob(f.job, f.pods, "chart-uid"), /NATIVE_HELM_/);
  }
});
test("only recorded predecessor Job UIDs can explain old terminal pods", () => {
  const f = fixture(); f.pods.items[0].metadata.ownerReferences[0].uid = "old-job";
  assert.throws(() => nativeHelmJob(f.job, f.pods, "chart-uid"), /NATIVE_HELM_JOB_FOREIGN_POD/);
  assert.equal(nativeHelmJob(f.job, f.pods, "chart-uid", ["old-job"]).terminal, true);
});
