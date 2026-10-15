import assert from "node:assert/strict";
import test from "node:test";
import { prepareFrontendCutover } from "./stable-frontend-model.mjs";

const release = { component: "staff-control-center", sourceSHA: "a".repeat(40), provenanceSHA256: "b".repeat(64),
  image: "example.invalid/kodex/staff-control-center@sha256:" + "c".repeat(64) };
const runtimeSnapshot = { name: "staff-control-center-runtime-stable-" + "d".repeat(16), revision: "e".repeat(64) };
function fixtures() {
  const deployment = { kind: "Deployment", metadata: { name: "staff-control-center", namespace: "kodex-system", uid: "uid", resourceVersion: "1" },
    spec: { replicas: 1, template: { metadata: {}, spec: { serviceAccountName: "staff-control-center",
      containers: [{ name: "staff-control-center", image: "source-image", command: ["/workspace/tools/dev/run-frontend.sh"],
        args: [], workingDir: "/workspace/services/staff/control-center", env: [
          { name: "KODEX_DEV_NODE_IMAGE", value: "source-image" }, { name: "KODEX_DEV_CACHE_DIR", value: "/tmp/vite-cache" },
          { name: "KODEX_DEV_PUBLIC_HOST", value: "platform.example.test" },
          { name: "KODEX_DEV_API_TARGET", value: "https://control-api-gateway.kodex-system.svc:8443" },
        ] }],
      volumes: [{ name: "runtime-config", configMap: { name: "version-pinned-current" } }, { name: "source", hostPath: { path: "/private" } }] } } } };
  const template = { metadata: {}, spec: { serviceAccountName: "staff-control-center", automountServiceAccountToken: false,
    containers: [{ name: "staff-control-center", ports: [{ name: "https", containerPort: 8443 }],
      readinessProbe: { httpGet: { scheme: "HTTPS" } }, securityContext: { runAsUser: 101 },
      volumeMounts: [{ name: "runtime-config", mountPath: "/var/run/config/kodex/staff-control-center/runtime", readOnly: true }] }],
    volumes: [{ name: "runtime-config", configMap: { name: "canonical" } }] } };
  return { deployment, template };
}

test("compiled frontend pins nginx and immutable runtime snapshot without mutating source", () => {
  const { deployment, template } = fixtures(), original = structuredClone(deployment);
  const plan = prepareFrontendCutover(deployment, template, release, runtimeSnapshot), after = plan.patch.at(-1).value;
  assert.deepEqual(deployment, original);
  assert.equal(after.replicas, 2); assert.equal(plan.requiresServiceTLSCutover, true);
  assert.equal(after.template.spec.containers[0].image, release.image);
  assert.equal(after.template.spec.volumes[0].configMap.name, runtimeSnapshot.name);
  assert.equal(after.template.metadata.annotations["kodex.dev/runtime-config-revision"], runtimeSnapshot.revision);
  assert.equal(after.template.spec.volumes.some(v => v.hostPath), false);
});

test("source drift unknown image template mounts and service identity are rejected", () => {
  for (const change of [v => { v.deployment.spec.template.spec.containers[0].command = ["other"]; },
    v => { v.template.spec.serviceAccountName = "foreign"; },
    v => { v.template.spec.volumes.push({ name: "source", hostPath: { path: "/workspace" } }); },
    v => { v.template.spec.containers[0].command = ["vite"]; }]) {
    const value = fixtures(); change(value);
    assert.throws(() => prepareFrontendCutover(value.deployment, value.template, release, runtimeSnapshot));
  }
});

test("unknown frontend env is rejected instead of copying secrets into recovery material", () => {
  const value = fixtures();
  value.deployment.spec.template.spec.containers[0].env.push({ name: "PRIVATE", value: "PRIVATE_SENTINEL" });
  assert.throws(() => prepareFrontendCutover(value.deployment, value.template, release, runtimeSnapshot), /SOURCE_CONFIGURATION_UNSUPPORTED/);
});
