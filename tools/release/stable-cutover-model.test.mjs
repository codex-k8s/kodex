import assert from "node:assert/strict";
import test from "node:test";
import { prepareGoCutover as prepareModel, validateStorageProtocol } from "./stable-cutover-model.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";

const resources = { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "1", memory: "512Mi" } };
const prepareGoCutover = (deployment, release, options = {}) => prepareModel(deployment, release,
  { resourceProfile: { main: resources, spoolInit: resources }, ...options });

function fixture(name = "session-archive", module = "services/jobs/session-archive", args = ["controller"]) {
  return { kind: "Deployment", metadata: { name, namespace: "kodex-system", uid: "uid", resourceVersion: "2" },
    spec: { replicas: 1, template: { metadata: { annotations: {} }, spec: { securityContext: { fsGroup: 1001 },
      containers: [{ name, image: "dev", command: ["/workspace/tools/dev/run-go-hot-reload.sh"],
        args: [module, `./cmd/${name}`, name, ...args],
        env: [{ name: "GOCACHE", value: "/go" }, { name: "TOKEN", valueFrom: { secretKeyRef: { name: "exact", key: "key" } } },
          ...(name === "session-archive" ? [{ name: "SESSION_ARCHIVE_WORKER_IMAGE", value: "example.invalid/kodex/session-archive@sha256:" + "d".repeat(64) }] : [])],
        volumeMounts: [{ name: "dev-source", mountPath: "/workspace" }, { name: "secret", mountPath: "/var/run/secret", readOnly: true }] }],
      volumes: [{ name: "dev-source", hostPath: { path: "/private" } }, { name: "secret", secret: { secretName: "exact" } }] } } } };
}
const release = { component: "session-archive", image: "example.invalid/kodex/session-archive@sha256:" + "a".repeat(64),
  sourceSHA: "b".repeat(40), provenanceSHA256: "c".repeat(64) };

test("normal cutover preserves mode secrets and source input with exact CAS", () => {
  const value = fixture();
  value.spec.template.spec.containers[0].workingDir = "/workspace/services/jobs/session-archive";
  value.spec.template.spec.containers[0].env.push({ name: "HOME", value: "/go/build-cache/home" });
  const original = structuredClone(value);
  const result = prepareGoCutover(value, release), after = result.patch.at(-1).value;
  assert.deepEqual(value, original);
  assert.equal(after.replicas, 2); assert.equal(after.strategy.rollingUpdate.maxUnavailable, 0);
  assert.deepEqual(after.template.spec.containers[0].command, ["/usr/local/bin/session-archive"]);
  assert.deepEqual(after.template.spec.containers[0].args, ["controller"]);
  const expectedEnv = structuredClone(original.spec.template.spec.containers[0].env.slice(1).filter(e => e.name !== "HOME"));
  expectedEnv.find(e => e.name === "SESSION_ARCHIVE_WORKER_IMAGE").value = release.image;
  assert.deepEqual(after.template.spec.containers[0].env, expectedEnv);
  assert.deepEqual(after.template.spec.volumes, [original.spec.template.spec.volumes[1]]);
  assert.deepEqual(after.template.spec.securityContext, original.spec.template.spec.securityContext);
  assert.deepEqual(result.patch.slice(0, 3).map(p => p.op), ["test", "test", "test"]);
  assert.equal(after.template.metadata.labels["kodex.dev/local-profile"], "stable");
  assert.equal(Object.hasOwn(after.template.spec.containers[0], "workingDir"), false);
  assert.deepEqual(after.template.spec.containers[0].resources, resources);
  assert.deepEqual(applyReverseSpec(after, result.recovery.inverse), original.spec);
});

test("archive controller and worker share one OCI and ambiguous source pins fail closed", () => {
  for (const mutation of [
    app => { app.env = app.env.filter(e => e.name !== "SESSION_ARCHIVE_WORKER_IMAGE"); },
    app => { app.env.push(structuredClone(app.env.at(-1))); },
    app => { app.env.at(-1).value = "image:latest"; },
    app => { app.env.at(-1).valueFrom = { configMapKeyRef: { name: "foreign", key: "image" } }; },
  ]) {
    const value = fixture(); mutation(value.spec.template.spec.containers[0]);
    assert.throws(() => prepareGoCutover(value, release), /ARCHIVE_WORKER_PIN_INVALID|DEV_ENV_UNSUPPORTED/);
  }
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

test("cutover removes isolated application source and still rejects a sidecar sharing it", () => {
  const value = fixture();
  value.spec.template.spec.containers[0].volumeMounts[0].name = "dev-application-source";
  value.spec.template.spec.volumes.push({ name: "dev-application-source", hostPath: { path: "/exact/application" } });
  const after = prepareGoCutover(value, release).patch.at(-1).value;
  assert.equal(after.template.spec.volumes.some(v => v.hostPath), false);
  assert.equal(after.template.spec.containers[0].volumeMounts.some(m => m.mountPath === "/workspace"), false);
  value.spec.template.spec.containers.push({ name: "sidecar", volumeMounts: [{ name: "dev-application-source", mountPath: "/workspace", readOnly: true }] });
  assert.throws(() => prepareGoCutover(value, release), /DEV_VOLUME_SHARED/);
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
  const after = prepareGoCutover(value, { ...release, component: "runtime-controller" },
    { sourceSpoolScript: value.spec.template.spec.initContainers[0].args[0] }).patch.at(-1).value;
  const init = after.template.spec.initContainers[0];
  assert.equal(init.image, release.image);
  assert.deepEqual(init.resources, resources);
  assert.deepEqual(init.command, ["/usr/local/bin/runtime-controller"]);
  assert.deepEqual(init.args, ["--prepare-artifact-spool", "/var/lib/kodex/runtime-controller/artifact-spool"]);
  assert.equal(init.volumeMounts.length, 1);
  assert.ok(after.template.spec.volumes.some(v => v.name === "artifact-spool"));
});

test("Go recovery material roundtrips source without copying unchanged credentials", () => {
  const value = fixture();
  value.spec.template.spec.containers[0].env.push({ name: "PRIVATE", value: "DO_NOT_COPY_PRIVATE_1810" });
  const result = prepareGoCutover(value, release);
  assert.deepEqual(applyReverseSpec(result.patch.at(-1).value, result.recovery.inverse), value.spec);
  assert.equal(JSON.stringify(result.recovery).includes("DO_NOT_COPY_PRIVATE_1810"), false);
  assert.equal(result.recovery.mode, "ARCHIVE_PAUSE_FORWARD_ONLY");
  const duplicate = structuredClone(value);
  duplicate.spec.template.spec.containers[0].env.push({ name: "PRIVATE", value: "OTHER_PRIVATE_1810" });
  assert.throws(() => prepareGoCutover(duplicate, release), /DEV_ENV_UNSUPPORTED/);
  for (const env of [{ name: "GOPROXY", value: "https://user:private@example.test" },
    { name: "GOCACHE", valueFrom: { secretKeyRef: { name: "private", key: "value" } } },
    { name: "GOFLAGS", value: "-ldflags=private" }]) {
    const source = fixture(); source.spec.template.spec.containers[0].env.push(env);
    assert.throws(() => prepareGoCutover(source, release), /DEV_ENV_UNSUPPORTED/);
  }
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

test("Go conversion requires bounded canonical resources and preserves unrelated sidecar limits", () => {
  const value = fixture();
  assert.throws(() => prepareModel(value, release), /RESOURCE_PROFILE_INVALID/);
  value.spec.template.spec.containers[0].resources = { requests: { cpu: "50m", memory: "64Mi" } };
  value.spec.template.spec.containers.push({ name: "scanner", resources: { requests: { cpu: "100m", memory: "512Mi" },
    limits: { cpu: "2", memory: "2Gi" } } });
  const result = prepareGoCutover(value, release), after = result.patch.at(-1).value;
  assert.deepEqual(after.template.spec.containers[1], value.spec.template.spec.containers[1]);
  assert.deepEqual(applyReverseSpec(after, result.recovery.inverse), value.spec);
});
