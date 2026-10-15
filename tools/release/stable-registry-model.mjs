import { fingerprint } from "./stable-inventory.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";

export const registryRoles = {
  "kodex-image-registry-pull": { claim: "kodex-image-registry-promoted", readOnly: true },
  "kodex-image-registry-staging-read": { claim: "kodex-image-registry-staging", readOnly: true },
  "kodex-image-registry-push": { claim: "kodex-image-registry-staging", readOnly: false },
  "kodex-image-registry-promotion": { claim: "kodex-image-registry-promoted", readOnly: false },
  "kodex-image-registry-evidence": { claim: "kodex-image-registry-evidence", readOnly: false },
};
export const registryIntent = "kodex.dev/stable-registry-plan";
export const httpStateName = name => name + "-http-state-stable";
function requireValue(value, code) { if (!value) throw Error(code); }

export function prepareRegistry(deployment, storage, helperImage, executorSHA) {
  const meta = deployment?.metadata, name = meta?.name, role = registryRoles[name], before = deployment?.spec;
  requireValue(deployment?.kind === "Deployment" && meta.namespace === "kodex-system" && role && meta.uid && meta.resourceVersion &&
    !meta.deletionTimestamp && before?.replicas === 1 && before.paused !== true && before.template?.spec, "REGISTRY_SOURCE_IDENTITY_INVALID");
  const after = structuredClone(before), pod = after.template.spec, main = pod.containers?.find(c => c.name === "registry");
  requireValue(main && /^registry:2\.8\.3@sha256:[a-f0-9]{64}$/.test(main.image ?? "") &&
    !(main.command ?? []).length && !(main.args ?? []).length &&
    pod.containers.every(c => /@sha256:[a-f0-9]{64}$/.test(c.image ?? "")) && !pod.volumes?.some(v => v.hostPath) &&
    new Set(pod.containers.map(c => c.name)).size === pod.containers.length &&
    pod.containers.every(c => new Set((c.env ?? []).map(e => e.name)).size === (c.env ?? []).length), "REGISTRY_SOURCE_PROFILE_INVALID");
  const claims = (pod.volumes ?? []).filter(v => v.persistentVolumeClaim);
  requireValue(claims.length === 1 && claims[0].name === "data" && claims[0].persistentVolumeClaim.claimName === role.claim &&
    storage?.claim === role.claim && storage.bound === true && storage.accessModes?.length === 1 && storage.accessModes[0] === "ReadWriteOnce" &&
    typeof storage.node === "string" && storage.node && /^[a-f0-9]{64}$/.test(storage.commitment ?? ""), "REGISTRY_EXACT_STORAGE_REQUIRED");
  requireValue((!pod.nodeSelector?.["kubernetes.io/hostname"] || pod.nodeSelector["kubernetes.io/hostname"] === storage.node) &&
    (!pod.nodeName || pod.nodeName === storage.node) && !pod.affinity?.podAntiAffinity?.requiredDuringSchedulingIgnoredDuringExecution?.length &&
    !(pod.topologySpreadConstraints ?? []).some(c => c.whenUnsatisfiable === "DoNotSchedule"), "REGISTRY_STORAGE_SCHEDULING_CONFLICT");
  const dataMount = main.volumeMounts?.find(v => v.name === "data" && v.mountPath === "/var/lib/registry");
  requireValue(dataMount && (dataMount.readOnly === true) === role.readOnly &&
    (claims[0].persistentVolumeClaim.readOnly === true) === role.readOnly && !(main.envFrom ?? []).length &&
    !(main.env ?? []).some(e => e.name === "REGISTRY_HTTP_SECRET"), "REGISTRY_STORAGE_PROFILE_INVALID");
  const maintenance = main.env?.find(e => e.name === "REGISTRY_STORAGE_MAINTENANCE");
  if (role.readOnly) {
    requireValue(maintenance && !maintenance.valueFrom && maintenance.value === '{"readonly":{"enabled":true}}' &&
      main.env.some(e => e.name === "REGISTRY_STORAGE_DELETE_ENABLED" && e.value === "false"), "REGISTRY_READONLY_BOUNDARY_INVALID");
  } else requireValue(!maintenance, "REGISTRY_WRITER_PROFILE_INVALID");
  requireValue(/^[a-f0-9]{40}$/.test(executorSHA), "REGISTRY_EXECUTOR_INVALID");
  pod.nodeSelector = { ...pod.nodeSelector, "kubernetes.io/hostname": storage.node };
  if (!role.readOnly) main.env.push({ name: "REGISTRY_HTTP_SECRET", valueFrom: { secretKeyRef: { name: httpStateName(name), key: "secret" } } });
  if (name === "kodex-image-registry-pull") {
    const helper = pod.containers.find(c => c.name === "pull-authorizer");
    requireValue(helper?.command?.join(" ") === "/usr/local/bin/registry-pull-authorizer" && !(helper.args ?? []).length &&
      /^registry\.local\.kodex\/kodex\/registry-pull-authorizer@sha256:[a-f0-9]{64}$/.test(helperImage ?? "") &&
      !helperImage.endsWith("0".repeat(64)), "REGISTRY_HELPER_IMAGE_INVALID");
    helper.image = helperImage;
    helper.imagePullPolicy = "IfNotPresent";
  }
  after.replicas = 2;
  after.strategy = { type: "RollingUpdate", rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } };
  after.template.metadata ??= {};
  after.template.metadata.annotations = { ...after.template.metadata.annotations, "kodex.dev/stable-registry-source-sha": executorSHA };
  return { name, uid: meta.uid, labelsSHA256: fingerprint(meta.labels ?? {}), storage, secretName: role.readOnly ? null : httpStateName(name),
    beforeSpecSHA256: fingerprint(before), afterSpecSHA256: fingerprint(after),
    quiescedBeforeSHA256: fingerprint({ ...before, replicas: 0 }), quiescedAfterSHA256: fingerprint({ ...after, replicas: 0 }),
    inverse: reverseSpecPatch(before, after), patch: [
      { op: "test", path: "/metadata/uid", value: meta.uid }, { op: "test", path: "/metadata/resourceVersion", value: meta.resourceVersion },
      { op: "test", path: "/spec", value: before }, { op: "replace", path: "/spec", value: after },
    ] };
}
