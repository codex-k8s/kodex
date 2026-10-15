import { fingerprint } from "./stable-inventory.mjs";
import { boundedResources } from "./stable-resources.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
const identity = r => `${r.apiVersion}/${r.kind}/${r.metadata?.namespace ?? ""}/${r.metadata?.name}`;
function index(resources) {
  requireValue(Array.isArray(resources) && resources.length > 0 && resources.length < 512, "SYSTEM_HELM_RENDER_INVALID");
  const result = new Map();
  for (const r of resources) {
    requireValue(r?.apiVersion && r.kind && r.metadata?.name && !result.has(identity(r)), "SYSTEM_HELM_RENDER_INVALID");
    result.set(identity(r), r);
  }
  return result;
}

export function systemHelmTransition(profile, before, after) {
  requireValue(profile && ["cert-manager", "trust-manager", "kube-prometheus-stack"].includes(profile.name) &&
    Array.isArray(profile.deployments) && profile.deployments.length > 0, "SYSTEM_HELM_PROFILE_INVALID");
  const old = index(before), desired = index(after), changes = [], targets = new Set();
  for (const [key, b] of old) {
    const a = desired.get(key);
    requireValue(a, "SYSTEM_HELM_RESOURCE_REMOVAL_FORBIDDEN");
    const expected = structuredClone(b);
    if (b.metadata.namespace === profile.namespace && b.kind === "Deployment" && profile.deployments.includes(b.metadata.name)) {
      requireValue([1, 2].includes(b.spec?.replicas ?? 1) && a.spec?.replicas === 2 &&
        a.spec.template?.spec?.containers?.length === b.spec.template?.spec?.containers?.length,
      "SYSTEM_HELM_REPLICA_PROFILE_INVALID");
      expected.spec.replicas = 2;
      for (const group of ["containers", "initContainers"]) {
        const source = b.spec.template.spec[group] ?? [], next = a.spec.template.spec[group] ?? [];
        requireValue(source.length === next.length, "SYSTEM_HELM_CONTAINER_CHANGE_FORBIDDEN");
        for (let i = 0; i < source.length; i++) {
          requireValue(source[i].name === next[i].name, "SYSTEM_HELM_CONTAINER_CHANGE_FORBIDDEN");
          expected.spec.template.spec[group][i].resources = boundedResources(next[i].resources, source[i].resources);
        }
      }
      targets.add(b.metadata.name);
    } else if (b.metadata.namespace === profile.namespace && b.kind === "Alertmanager" && b.metadata.name === profile.alertmanager) {
      requireValue([1, 2].includes(b.spec?.replicas ?? 1) && a.spec?.replicas === 2 &&
        !b.spec.paused && !a.spec.paused, "SYSTEM_HELM_ALERTMANAGER_INVALID");
      expected.spec.replicas = 2;
      expected.spec.resources = boundedResources(a.spec.resources, b.spec.resources);
      targets.add("Alertmanager/" + b.metadata.name);
    }
    requireValue(fingerprint(expected) === fingerprint(a), "SYSTEM_HELM_UNRELATED_CHANGE_FORBIDDEN");
    if (fingerprint(b) !== fingerprint(a)) changes.push({ kind: b.kind, name: b.metadata.name,
      beforeSHA256: fingerprint(b), afterSHA256: fingerprint(a) });
  }
  for (const [key, a] of desired) {
    if (old.has(key)) continue;
    const allowed = profile.deployments.includes(a.metadata?.name) || a.metadata?.name === profile.alertmanager;
    requireValue(allowed && a.kind === "PodDisruptionBudget" && a.apiVersion === "policy/v1" &&
      a.metadata.namespace === profile.namespace && a.spec?.minAvailable === 1 && a.spec.maxUnavailable === undefined &&
      Object.keys(a.spec).every(k => ["minAvailable", "selector"].includes(k)) &&
      Object.keys(a.spec.selector?.matchLabels ?? {}).length > 0 && !a.spec.selector?.matchExpressions?.length,
    "SYSTEM_HELM_RESOURCE_ADDITION_FORBIDDEN");
    const target = after.find(r => r.metadata?.namespace === profile.namespace && r.metadata?.name === a.metadata.name &&
      (r.kind === "Deployment" || r.kind === "Alertmanager"));
    const labels = target?.kind === "Deployment" ? target.spec.template.metadata.labels :
      { alertmanager: profile.alertmanager, "app.kubernetes.io/name": "alertmanager" };
    requireValue(labels && Object.entries(a.spec.selector.matchLabels).every(([k, v]) => labels[k] === v),
      "SYSTEM_HELM_PDB_SELECTOR_INVALID");
    changes.push({ kind: a.kind, name: a.metadata.name, beforeSHA256: null, afterSHA256: fingerprint(a) });
  }
  requireValue(profile.deployments.every(name => targets.has(name)) &&
    (!profile.alertmanager || targets.has("Alertmanager/" + profile.alertmanager)), "SYSTEM_HELM_TARGET_MISSING");
  return { beforeSHA256: fingerprint(before), afterSHA256: fingerprint(after), changes };
}
