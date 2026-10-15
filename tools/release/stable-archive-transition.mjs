import { fingerprint } from "./stable-inventory.mjs";

const namespace = "kodex-runtime", name = "session-archive-controller";
function requireValue(value, code) { if (!value) throw Error(code); }

// Desired правила читаются вызывающим адаптером из единственного repo-owned
// Role manifest. Ни caller payload, ни label не назначают новую authority.
export function archiveTransition(role, binding, desiredRole) {
  for (const resource of [role, binding, desiredRole])
    requireValue(resource?.metadata?.namespace === namespace && resource.metadata.name === name &&
      resource.apiVersion === "rbac.authorization.k8s.io/v1", "STABLE_ARCHIVE_ROLE_SCOPE_INVALID");
  requireValue(role.kind === "Role" && binding.kind === "RoleBinding" && desiredRole.kind === "Role" &&
    role.metadata.uid && role.metadata.resourceVersion && binding.metadata.uid, "STABLE_ARCHIVE_ROLE_IDENTITY_INVALID");
  requireValue(binding.roleRef?.apiGroup === "rbac.authorization.k8s.io" && binding.roleRef.kind === "Role" &&
    binding.roleRef.name === name && binding.subjects?.length === 1 &&
    binding.subjects[0].kind === "ServiceAccount" && binding.subjects[0].name === "session-archive" &&
    binding.subjects[0].namespace === "kodex-system" && !(binding.subjects[0].apiGroup ?? ""),
    "STABLE_ARCHIVE_BINDING_INVALID");
  requireValue(Array.isArray(desiredRole.rules) && desiredRole.rules.length === 4 &&
    desiredRole.rules.every(r => r.apiGroups?.length === 1 && r.resources?.length === 1 &&
      ["/pods", "/configmaps", "/persistentvolumeclaims", "batch/jobs"].includes(`${r.apiGroups[0]}/${r.resources[0]}`) &&
      Array.isArray(r.verbs) && r.verbs.length > 0 && r.verbs.every(v => ["get", "list", "create", "delete"].includes(v)) &&
      Object.keys(r).every(k => ["apiGroups", "resources", "verbs"].includes(k))), "STABLE_ARCHIVE_DESIRED_RULES_INVALID");
  requireValue(new Set(desiredRole.rules.map(r => `${r.apiGroups[0]}/${r.resources[0]}`)).size === 4,
    "STABLE_ARCHIVE_DESIRED_RULES_INVALID");
  requireValue(Array.isArray(role.rules) && role.rules.length === 4 && role.rules.every(r => {
    const target = desiredRole.rules.find(t => fingerprint(t.apiGroups) === fingerprint(r.apiGroups) &&
      fingerprint(t.resources) === fingerprint(r.resources));
    return target && Array.isArray(r.verbs) && r.verbs.length > 0 &&
      r.verbs.every(v => target.verbs.includes(v) ||
        (v === "deletecollection" && ["configmaps", "jobs"].includes(r.resources[0]))) &&
      Object.keys(r).every(k => ["apiGroups", "resources", "verbs"].includes(k));
  }), "STABLE_ARCHIVE_SOURCE_RULES_INVALID");
  requireValue(new Set(role.rules.map(r => `${r.apiGroups[0]}/${r.resources[0]}`)).size === 4,
    "STABLE_ARCHIVE_SOURCE_RULES_INVALID");
  const proof = { roleUID: role.metadata.uid, beforeRulesSHA256: fingerprint(role.rules),
    afterRulesSHA256: fingerprint(desiredRole.rules), bindingUID: binding.metadata.uid,
    bindingSHA256: fingerprint({ roleRef: binding.roleRef, subjects: binding.subjects }) };
  return { proof, patch: [
    { op: "test", path: "/metadata/uid", value: role.metadata.uid },
    { op: "test", path: "/metadata/resourceVersion", value: role.metadata.resourceVersion },
    { op: "test", path: "/rules", value: role.rules },
    { op: "replace", path: "/rules", value: desiredRole.rules },
  ] };
}

export function sameArchiveBoundary(actual, expected, phase = "before") {
  return actual?.roleUID === expected?.roleUID && actual.bindingUID === expected.bindingUID &&
    actual.bindingSHA256 === expected.bindingSHA256 && actual.afterRulesSHA256 === expected.afterRulesSHA256 &&
    actual.beforeRulesSHA256 === (phase === "after" ? expected.afterRulesSHA256 : expected.beforeRulesSHA256);
}

export function ownedDeploymentPods(list, deploymentUID) {
  requireValue(Array.isArray(list?.items), "STABLE_POD_LIST_INVALID");
  const owners = new Set(list.items.filter(r => r.kind === "ReplicaSet" && r.metadata?.ownerReferences?.some(o =>
    o.uid === deploymentUID && o.kind === "Deployment" && o.controller === true)).map(r => r.metadata.uid));
  const pods = list.items.filter(r => r.kind === "Pod");
  requireValue(pods.every(r => r.metadata?.uid && r.metadata.ownerReferences?.some(o =>
    o.kind === "ReplicaSet" && owners.has(o.uid) && o.controller === true)), "STABLE_FOREIGN_SERVING_POD");
  return pods;
}

// Terminal history сохраняется в API до отдельной GC, но не является
// действующей replica. Отсекается только доказанно завершённый Pod: неизвестные
// статусы, продолжающийся init/debug container и чужой lineage не скрываются.
export function activeDeploymentPods(list, deploymentUID) {
  return ownedDeploymentPods(list, deploymentUID).filter(pod => !isProvenStoppedPod(pod));
}

export function isProvenStoppedPod(pod) {
  if (!["Succeeded", "Failed"].includes(pod.status?.phase)) return false;
  const expected = [...(pod.spec?.containers ?? []), ...(pod.spec?.initContainers ?? []), ...(pod.spec?.ephemeralContainers ?? [])].map(c => c.name);
  const states = [...(pod.status.containerStatuses ?? []), ...(pod.status.initContainerStatuses ?? []), ...(pod.status.ephemeralContainerStatuses ?? [])];
  return expected.length > 0 && new Set(expected).size === expected.length && states.length === expected.length &&
    new Set(states.map(s => s.name)).size === states.length && states.every(s => expected.includes(s.name) &&
      s.state?.terminated && Number.isInteger(s.state.terminated.exitCode) && !s.state.running && !s.state.waiting);
}
