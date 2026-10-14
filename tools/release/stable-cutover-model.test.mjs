import assert from "node:assert/strict";
import test from "node:test";
import { prepareGoCutover, validateStorageProtocol } from "./stable-cutover-model.mjs";

function fixture(name = "session-archive", module = "services/jobs/session-archive", args = ["controller"]) {
  return { kind: "Deployment", metadata: { name, namespace: "kodex-system", uid: "uid", resourceVersion: "2" },
    spec: { replicas: 1, template: { metadata: { annotations: {} }, spec: { securityContext: { fsGroup: 1001 },
      containers: [{ name, image: "dev", command: ["/workspace/tools/dev/run-go-hot-reload.sh"],
        args: [module, `./cmd/${name}`, name, ...args],
        env: [{ name: "GOCACHE", value: "/go" }, { name: "TOKEN", valueFrom: { secretKeyRef: { name: "exact", key: "key" } } }],
        volumeMounts: [{ name: "dev-source", mountPath: "/workspace" }, { name: "secret", mountPath: "/var/run/secret", readOnly: true }] }],
      volumes: [{ name: "dev-source", hostPath: { path: "/private" } }, { name: "secret", secret: { secretName: "exact" } }] } } } };
}
const release = { component: "session-archive", image: "example.invalid/kodex/session-archive@sha256:" + "a".repeat(64),
  sourceSHA: "b".repeat(40), provenanceSHA256: "c".repeat(64) };

test("normal cutover preserves mode secrets and source input with exact CAS", () => {
  const value = fixture(), original = structuredClone(value);
  const result = prepareGoCutover(value, release), after = result.patch.at(-1).value;
  assert.deepEqual(value, original);
  assert.equal(after.replicas, 2); assert.equal(after.strategy.rollingUpdate.maxUnavailable, 0);
  assert.deepEqual(after.template.spec.containers[0].command, ["/usr/local/bin/session-archive"]);
  assert.deepEqual(after.template.spec.containers[0].args, ["controller"]);
  assert.deepEqual(after.template.spec.containers[0].env, original.spec.template.spec.containers[0].env.slice(1));
  assert.deepEqual(after.template.spec.volumes, [original.spec.template.spec.volumes[1]]);
  assert.deepEqual(after.template.spec.securityContext, original.spec.template.spec.securityContext);
  assert.deepEqual(result.patch.slice(0, 3).map(p => p.op), ["test", "test", "test"]);
  assert.equal(after.template.metadata.labels["kodex.dev/local-profile"], "stable");
});

test("unregistered component wrong command and placeholder images fail closed", () => {
  assert.throws(() => prepareGoCutover(fixture("foreign"), release), /UNSUPPORTED/);
  const wrong = fixture(); wrong.spec.template.spec.containers[0].args[1] = "./cmd/foreign";
  assert.throws(() => prepareGoCutover(wrong, release), /MISMATCH/);
  assert.throws(() => prepareGoCutover(fixture(), { ...release, image: release.image.replace(/a{64}$/, "0".repeat(64)) }), /RELEASE_INVALID/);
  assert.throws(() => prepareGoCutover(fixture(), { ...release, component: "control-plane" }), /RELEASE_INVALID/);
});

test("sidecar shared dev volumes and unrecognized host mounts block conversion", () => {
  const shared = fixture(); shared.spec.template.spec.containers.push({ name: "sidecar", volumeMounts: [{ name: "dev-source" }] });
  assert.throws(() => prepareGoCutover(shared, release), /DEV_VOLUME_SHARED/);
  const unknown = fixture(); unknown.spec.template.spec.volumes.push({ name: "foreign", hostPath: { path: "/data" } });
  assert.throws(() => prepareGoCutover(unknown, release), /HOST_MOUNT_REMAINS/);
  const changed = fixture(); changed.spec.template.spec.volumes[0] = { name: "dev-source", secret: { secretName: "must-preserve" } };
  assert.throws(() => prepareGoCutover(changed, release), /DEV_VOLUME_TYPE_CHANGED/);
  const extra = fixture(); extra.spec.template.spec.volumes.push({ name: "dev-important", secret: { secretName: "retained" } });
  assert.ok(prepareGoCutover(extra, release).patch.at(-1).value.template.spec.volumes.some(v => v.secret?.secretName === "retained"));
});

test("RWO needs exact node affinity and registries need a common upload secret", () => {
  const value = fixture("kodex-image-registry-push");
  value.spec.template.spec.volumes = [{ name: "registry", persistentVolumeClaim: { claimName: "data" } }];
  const claims = [{ name: "data", bound: true, accessModes: ["ReadWriteOnce"], nodeName: "node-a" }];
  assert.throws(() => validateStorageProtocol(value, { claims, sharedHTTPSecret: true }), /NODE_PIN_REQUIRED/);
  value.spec.template.spec.nodeSelector = { "kubernetes.io/hostname": "node-a" };
  assert.throws(() => validateStorageProtocol(value, { claims }), /HTTP_SECRET_REQUIRED/);
  assert.equal(validateStorageProtocol(value, { claims, sharedHTTPSecret: true }), true);
  claims[0].accessModes = ["ReadWriteOncePod"];
  assert.throws(() => validateStorageProtocol(value, { claims, sharedHTTPSecret: true }), /SINGLE_POD_ONLY/);
});

test("artifact spool init switches to the same compiled controller without losing its storage", () => {
  const value = fixture("runtime-controller", "services/internal/runtime-controller", []);
  value.spec.template.spec.volumes.push({ name: "dev-build-artifact-spool-init", hostPath: { path: "/fixture/cache" } },
    { name: "artifact-spool", emptyDir: { sizeLimit: "1Gi" } });
  value.spec.template.spec.initContainers = [{ name: "artifact-spool-init", image: "dev", command: ["/bin/sh", "-ec"],
    args: ["go build ./cmd/runtime-controller; exec binary --prepare-artifact-spool /var/lib/kodex/runtime-controller/artifact-spool"],
    volumeMounts: [{ name: "dev-source", mountPath: "/workspace" }, { name: "dev-build-artifact-spool-init", mountPath: "/go/build-cache" },
      { name: "artifact-spool", mountPath: "/var/lib/kodex/runtime-controller/artifact-spool" }] }];
  const after = prepareGoCutover(value, { ...release, component: "runtime-controller" }).patch.at(-1).value;
  const init = after.template.spec.initContainers[0];
  assert.equal(init.image, release.image);
  assert.deepEqual(init.command, ["/usr/local/bin/runtime-controller"]);
  assert.deepEqual(init.args, ["--prepare-artifact-spool", "/var/lib/kodex/runtime-controller/artifact-spool"]);
  assert.equal(init.volumeMounts.length, 1);
  assert.ok(after.template.spec.volumes.some(v => v.name === "artifact-spool"));
});

test("actual Go cutover cannot bypass the PVC protocol gate", () => {
  const value = fixture(); value.spec.template.spec.volumes.push({ name: "data", persistentVolumeClaim: { claimName: "scanner" } });
  assert.throws(() => prepareGoCutover(value, release), /STORAGE_CLAIM_NOT_BOUND/);
  const storageClaims = [{ name: "scanner", bound: true, accessModes: ["ReadWriteOnce"], nodeName: "node-a" }];
  const after = prepareGoCutover(value, release, { storageClaims }).patch.at(-1).value;
  assert.equal(after.template.spec.nodeSelector["kubernetes.io/hostname"], "node-a");
  storageClaims[0].accessModes = ["ReadWriteOncePod"];
  assert.throws(() => prepareGoCutover(value, release, { storageClaims }), /SINGLE_POD_ONLY/);
});
