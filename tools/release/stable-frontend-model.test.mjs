import assert from "node:assert/strict";
import test from "node:test";
import { prepareFrontendCutover } from "./stable-frontend-model.mjs";

const release = { component: "staff-control-center", sourceSHA: "a".repeat(40), provenanceSHA256: "b".repeat(64),
  image: "example.invalid/kodex/staff-control-center@sha256:" + "c".repeat(64) };
function fixtures() {
  const deployment = { kind: "Deployment", metadata: { name: "staff-control-center", namespace: "kodex-system", uid: "uid", resourceVersion: "1" },
    spec: { replicas: 1, template: { metadata: {}, spec: { serviceAccountName: "staff-control-center",
      containers: [{ name: "staff-control-center", command: ["/workspace/tools/dev/run-frontend.sh"] }],
      volumes: [{ name: "runtime-config", configMap: { name: "version-pinned-current" } }, { name: "source", hostPath: { path: "/private" } }] } } } };
  const template = { metadata: {}, spec: { serviceAccountName: "staff-control-center", automountServiceAccountToken: false,
    containers: [{ name: "staff-control-center", ports: [{ name: "https", containerPort: 8443 }],
      readinessProbe: { httpGet: { scheme: "HTTPS" } }, securityContext: { runAsUser: 101 },
      volumeMounts: [{ name: "runtime-config", mountPath: "/var/run/config/kodex/staff-control-center/runtime", readOnly: true }] }],
    volumes: [{ name: "runtime-config", configMap: { name: "canonical" } }] } };
  return { deployment, template };
}

test("compiled frontend pins nginx runtime without dropping current public config", () => {
  const { deployment, template } = fixtures(), original = structuredClone(deployment);
  const plan = prepareFrontendCutover(deployment, template, release), after = plan.patch.at(-1).value;
  assert.deepEqual(deployment, original);
  assert.equal(after.replicas, 2); assert.equal(plan.requiresServiceTLSCutover, true);
  assert.equal(after.template.spec.containers[0].image, release.image);
  assert.equal(after.template.spec.volumes[0].configMap.name, "version-pinned-current");
  assert.equal(after.template.spec.volumes.some(v => v.hostPath), false);
});

test("source drift unknown image template mounts and service identity are rejected", () => {
  for (const change of [v => { v.deployment.spec.template.spec.containers[0].command = ["other"]; },
    v => { v.template.spec.serviceAccountName = "foreign"; },
    v => { v.template.spec.volumes.push({ name: "source", hostPath: { path: "/workspace" } }); },
    v => { v.template.spec.containers[0].command = ["vite"]; }]) {
    const value = fixtures(); change(value);
    assert.throws(() => prepareFrontendCutover(value.deployment, value.template, release));
  }
});
