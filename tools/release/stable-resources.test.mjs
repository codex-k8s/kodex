import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { boundedResources, podResourceBudget, resourceQuantity } from "./stable-resources.mjs";
import { goResourceComponent } from "./stable-cutover-model.mjs";

const resources = { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "1", memory: "512Mi" } };

test("closed resource quantities and bounds reject ambiguous/unbounded profiles", () => {
  assert.equal(resourceQuantity("1.5", "cpu"), 1500);
  assert.equal(resourceQuantity("1Gi", "memory"), 1024 ** 3);
  for (const [value, name] of [["0.1m", "cpu"], ["0", "memory"], ["4GB", "memory"], ["100Mi", "cpu"], [1, "cpu"]])
    assert.throws(() => resourceQuantity(value, name), /QUANTITY_INVALID/);
  assert.deepEqual(boundedResources(resources), resources);
  for (const value of [undefined, {}, { ...resources, claims: [] }, { requests: resources.requests, limits: {} },
    { ...resources, requests: { cpu: "2", memory: "128Mi" } },
    { ...resources, limits: { ...resources.limits, "nvidia.com/gpu": "1" } }])
    assert.throws(() => boundedResources(value), /RESOURCE_/);
  assert.throws(() => boundedResources(resources, { limits: { "ephemeral-storage": "2Gi" } }), /STORAGE_BOUND_LOST/);
});

test("pod budget uses app sum versus sequential init maximum and reports missing limits", () => {
  const app = name => ({ name, resources });
  const pod = { containers: [app("one"), app("two")], initContainers: [{ name: "init", resources: {
    requests: { cpu: "1", memory: "1Gi" }, limits: { cpu: "2", memory: "2Gi" } } }] };
  assert.deepEqual(podResourceBudget(pod), { cpuMilli: { requests: 1000, limits: 2000 },
    memoryBytes: { requests: 1024 ** 3, limits: 2 * 1024 ** 3 } });
  pod.containers[0].resources = { requests: resources.requests };
  assert.equal(podResourceBudget(pod).cpuMilli.limits, null);
  assert.equal(podResourceBudget(pod).memoryBytes.limits, null);
  pod.resources = resources;
  assert.throws(() => podResourceBudget(pod), /POD_RESOURCE_PROFILE_INVALID/);
});

test("restartable init contributes to later init stages and steady state without double counting", () => {
  const pod = { overhead: { cpu: "10m", memory: "1Mi" }, containers: [{ name: "app", resources }],
    initContainers: [{ name: "sidecar", restartPolicy: "Always", resources },
      { name: "setup", resources: { requests: { cpu: "500m", memory: "512Mi" }, limits: { cpu: "2", memory: "1Gi" } } }] };
  assert.deepEqual(podResourceBudget(pod), { cpuMilli: { requests: 610, limits: 3010 },
    memoryBytes: { requests: 641 * 1024 ** 2, limits: 1537 * 1024 ** 2 } });
});

test("all registered Go deployables have bounded canonical resource profiles including spool", () => {
  for (const name of ["control-plane", "secret-broker", "stt-tts-service", "email-bridge", "runtime-controller",
    "integration-gateway", "integration-synthetic", "interaction-gateway", "control-api-gateway", "egress-gateway",
    "clamav-egress-gateway", "automation-scheduler", "artifact-retention", "session-archive", "backup-controller"]) {
    const component = goResourceComponent(name);
    const rows = JSON.parse(execFileSync("yq", ["eval", "-o=json", "-I=0",
      '[.spec.template.spec.containers[], .spec.template.spec.initContainers[]?] | map({"name": .name, "resources": .resources})',
      fileURLToPath(new URL(`../../deploy/k8s/base/${component}/deployment.yaml`, import.meta.url))], { encoding: "utf8" }));
    assert.ok(rows.find(c => c.name === component), name);
    for (const c of rows.filter(c => [component, "artifact-spool-init"].includes(c.name)))
      assert.deepEqual(boundedResources(c.resources), c.resources, name);
  }
  assert.throws(() => goResourceComponent("foreign"), /COMPONENT_UNSUPPORTED/);
});

test("two BuildKit pods have independent scratch and a bounded shared-host budget", () => {
  const deployment = JSON.parse(execFileSync("yq", ["eval", "-o=json", "-I=0", 'select(.kind == "Deployment")',
    fileURLToPath(new URL("../../deploy/k8s/base/image-supply-chain/buildkit.yaml", import.meta.url))], { encoding: "utf8" }));
  assert.equal(deployment.spec.replicas, 2);
  const pod = deployment.spec.template.spec;
  assert.equal(pod.hostUsers, false);
  assert.equal(pod.volumes.filter(v => v.emptyDir).length, 1);
  assert.equal(pod.volumes.some(v => v.persistentVolumeClaim || v.hostPath), false);
  assert.equal(deployment.spec.strategy.rollingUpdate.maxUnavailable, 0);
  const app = pod.containers.find(c => c.name === "buildkitd");
  boundedResources(app.resources);
  const budget = podResourceBudget(pod);
  assert.deepEqual(budget.cpuMilli, { requests: 2000, limits: 8000 });
  assert.deepEqual(budget.memoryBytes, { requests: 2 * 1024 ** 3, limits: 16 * 1024 ** 3 });
});
