import { fingerprint } from "./stable-inventory.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";
import { boundedResources } from "./stable-resources.mjs";

export const admissionName = "image-admission-controller";
export const admissionLease = admissionName + "-leader";
export const admissionIntent = "kodex.dev/stable-admission-plan";
const ns = "kodex-system";
const policyName = /^kodex-image-admission-policy(?:-[a-f0-9]{32})?$/;
function requireValue(value, code) { if (!value) throw Error(code); }

export function admissionRole(role, binding, leaseRule) {
  requireValue(role?.kind === "Role" && role.metadata.namespace === ns && role.metadata.name === admissionName &&
    role.metadata.uid && role.metadata.resourceVersion && binding?.kind === "RoleBinding" && binding.metadata.namespace === ns &&
    binding.metadata.name === admissionName && binding.metadata.uid && !role.metadata.deletionTimestamp && !binding.metadata.deletionTimestamp,
    "ADMISSION_ROLE_IDENTITY_INVALID");
  requireValue(fingerprint(binding.roleRef) === fingerprint({ apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: admissionName }) &&
    fingerprint(binding.subjects) === fingerprint([{ kind: "ServiceAccount", name: admissionName, namespace: ns }]), "ADMISSION_BINDING_INVALID");
  requireValue(fingerprint(leaseRule) === fingerprint({ apiGroups: ["coordination.k8s.io"], resources: ["leases"],
    resourceNames: [admissionLease], verbs: ["get", "update"] }), "ADMISSION_LEASE_RULE_INVALID");
  requireValue(Array.isArray(role.rules) && role.rules.length > 0 && role.rules.length <= 16 &&
    role.rules.every(r => !r.apiGroups?.includes("*") && !r.apiGroups?.includes("coordination.k8s.io")), "ADMISSION_SOURCE_ROLE_UNSUPPORTED");
  const read = role.rules.find(r => fingerprint(r.apiGroups) === fingerprint([""]) && fingerprint(r.resources) === fingerprint(["configmaps"]));
  requireValue(read?.resourceNames?.length === 1 && policyName.test(read.resourceNames[0]) &&
    fingerprint(role.rules) === fingerprint([
      { apiGroups: [""], resourceNames: read.resourceNames, resources: ["configmaps"], verbs: ["get"] },
      { apiGroups: ["supplychain.kodex.dev"], resourceNames: read.resourceNames, resources: ["imageadmissionpolicyparameters"], verbs: ["get"] },
      { apiGroups: [""], resources: ["persistentvolumeclaims"], verbs: ["get", "list", "create", "update", "delete"] },
      { apiGroups: ["batch"], resources: ["jobs"], verbs: ["get", "list", "create", "delete"] },
    ]), "ADMISSION_SOURCE_ROLE_UNSUPPORTED");
  return { uid: role.metadata.uid, policyName: read.resourceNames[0], bindingUID: binding.metadata.uid,
    bindingSHA256: fingerprint({ roleRef: binding.roleRef, subjects: binding.subjects }),
    beforeRulesSHA256: fingerprint(role.rules), afterRulesSHA256: fingerprint([...role.rules, leaseRule]), leaseRule };
}

export function prepareAdmission(deployment, canonical, image) {
  const meta = deployment?.metadata, before = deployment?.spec;
  requireValue(deployment?.kind === "Deployment" && meta.namespace === ns && meta.name === admissionName && meta.uid && meta.resourceVersion &&
    !meta.deletionTimestamp && meta.labels?.["kodex.dev/local-profile"] === "hot-reload" && before?.replicas === 1 && before.paused !== true,
    "ADMISSION_SOURCE_IDENTITY_INVALID");
  const after = structuredClone(before), pod = after.template?.spec, app = pod?.containers?.[0];
  requireValue(pod?.serviceAccountName === admissionName && pod.automountServiceAccountToken === false && pod.containers?.length === 1 &&
    !(pod.initContainers ?? []).length && !pod.volumes?.some(v => v.hostPath || v.persistentVolumeClaim) &&
    app?.name === admissionName && app.command?.join(" ") === "/usr/local/bin/" + admissionName && !(app.args ?? []).length &&
    /^registry\.local\.kodex\/kodex\/image-admission@sha256:[a-f0-9]{64}$/.test(app.image ?? "") &&
    Array.isArray(app.env) && new Set(app.env.map(e => e.name)).size === app.env.length && !app.env.some(e => e.name === "POD_UID"),
    "ADMISSION_SOURCE_PROFILE_INVALID");
  requireValue(!(app.envFrom ?? []).length && app.env.some(e => e.name === "KODEX_RPC_PROFILE" && e.value === "trusted-cluster" && !e.valueFrom) &&
    app.env.some(e => e.name === "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP" && !e.valueFrom && policyName.test(e.value ?? "")),
    "ADMISSION_SOURCE_CONFIGURATION_UNSUPPORTED");
  requireValue(["IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS", "IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS"].every(key =>
    app.env.some(e => e.name === key && e.value === "false" && !e.valueFrom)), "ADMISSION_SOURCE_MAINTENANCE_UNSUPPORTED");
  requireValue(!Object.hasOwn(before.selector?.matchLabels ?? {}, "kodex.dev/local-profile") &&
    !(before.selector?.matchExpressions ?? []).some(e => e.key === "kodex.dev/local-profile"), "ADMISSION_PROFILE_SELECTOR_BOUND");
  requireValue(image?.component === admissionName && /^[a-f0-9]{40}$/.test(image.sourceSHA ?? "") &&
    /^sha256:[a-f0-9]{64}$/.test(image.digest ?? "") && !image.digest.endsWith("0".repeat(64)), "ADMISSION_RELEASE_INVALID");
  const main = canonical?.spec?.template?.spec?.containers?.find(c => c.name === admissionName);
  requireValue(canonical?.kind === "Deployment" && canonical.metadata?.name === admissionName && main, "ADMISSION_CANONICAL_PROFILE_INVALID");
  app.resources = boundedResources(main.resources, app.resources);
  app.image = `registry.local.kodex/kodex/${admissionName}@${image.digest}`;
  app.imagePullPolicy = "IfNotPresent";
  app.env.push({ name: "POD_UID", valueFrom: { fieldRef: { apiVersion: "v1", fieldPath: "metadata.uid" } } });
  after.replicas = 2;
  after.strategy = { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } };
  after.template.metadata.labels = { ...after.template.metadata.labels, "kodex.dev/local-profile": "stable" };
  after.template.metadata.annotations = { ...after.template.metadata.annotations,
    "kodex.dev/source-sha": image.sourceSHA, "kodex.dev/deployment-profile": "trusted-cluster" };
  const labels = { ...meta.labels, "kodex.dev/local-profile": "stable" };
  return { uid: meta.uid, beforeSpecSHA256: fingerprint(before), afterSpecSHA256: fingerprint(after),
    beforeLabelsSHA256: fingerprint(meta.labels), afterLabelsSHA256: fingerprint(labels),
    quiescedBeforeSHA256: fingerprint({ ...before, replicas: 0 }), quiescedAfterSHA256: fingerprint({ ...after, replicas: 0 }),
    inverse: reverseSpecPatch(before, after),
    patch: [{ op: "test", path: "/metadata/uid", value: meta.uid }, { op: "test", path: "/metadata/resourceVersion", value: meta.resourceVersion },
      { op: "test", path: "/metadata/labels", value: meta.labels }, { op: "test", path: "/spec", value: before },
      { op: "replace", path: "/spec", value: after }, { op: "replace", path: "/metadata/labels", value: labels }] };
}

export function admissionSupportMatches(resource, template) {
  if (resource?.kind !== template.kind || resource.metadata?.namespace !== ns || resource.metadata.name !== template.metadata.name ||
    !resource.metadata.uid || !/^[0-9]+$/.test(resource.metadata.resourceVersion ?? "") || resource.metadata.deletionTimestamp ||
    resource.metadata.ownerReferences?.length || resource.metadata.finalizers?.length ||
    !Object.entries(template.metadata.labels ?? {}).every(([k, v]) => resource.metadata.labels?.[k] === v)) return false;
  if (template.kind === "PodDisruptionBudget") return fingerprint(resource.spec) === fingerprint(template.spec);
  if (template.kind !== "Lease") return false;
  const s = resource.spec ?? {}, holder = s.holderIdentity;
  return !s.strategy && !s.preferredHolder && (s.leaseTransitions === undefined || Number.isInteger(s.leaseTransitions) && s.leaseTransitions >= 0) &&
    (s.leaseDurationSeconds === undefined || Number.isInteger(s.leaseDurationSeconds) && s.leaseDurationSeconds > 0 && s.leaseDurationSeconds <= 60) &&
    (!holder || /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(holder) && s.leaseDurationSeconds &&
      Number.isFinite(Date.parse(s.acquireTime)) && Number.isFinite(Date.parse(s.renewTime)));
}
