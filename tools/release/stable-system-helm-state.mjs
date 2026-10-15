import { fingerprint } from "./stable-inventory.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
const namePattern = /^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$/;
export const systemObjectKey = r => `${r.apiVersion}/${r.kind}/${r.metadata?.namespace ?? ""}/${r.metadata?.name}`;
const metadataOnly = r => ["Secret", "ConfigMap"].includes(r.kind);

// Status, managedFields и timestamps не являются конфигурацией. Значения
// Secret/ConfigMap для проверки drift вообще не запрашиваются: достаточно UID/RV.
export function systemObjectState(r) {
  requireValue(r?.metadata?.uid && r.metadata.resourceVersion && r.kind && r.apiVersion, "SYSTEM_HELM_OBJECT_IDENTITY_INVALID");
  const identity = { apiVersion: r.apiVersion, kind: r.kind, namespace: r.metadata.namespace ?? "", name: r.metadata.name,
    uid: r.metadata.uid };
  if (metadataOnly(r)) return { ...identity, resourceVersion: r.metadata.resourceVersion };
  const content = structuredClone(r); delete content.status;
  content.metadata = { name: r.metadata.name, ...(r.metadata.namespace ? { namespace: r.metadata.namespace } : {}),
    labels: r.metadata.labels ?? {}, annotations: r.metadata.annotations ?? {},
    ...(r.metadata.ownerReferences ? { ownerReferences: r.metadata.ownerReferences } : {}),
    ...(r.metadata.finalizers ? { finalizers: r.metadata.finalizers } : {}) };
  return { ...identity, contentSHA256: fingerprint(content) };
}

export async function readSystemObjects(profile, rendered, kube) {
  requireValue(rendered.length > 0 && rendered.length < 512 && new Set(rendered.map(systemObjectKey)).size === rendered.length,
    "SYSTEM_HELM_OBJECT_SET_INVALID");
  const definitions = rendered.map(r => {
    requireValue(/^[A-Za-z][A-Za-z0-9]{0,100}$/.test(r.kind ?? "") && /^[a-z0-9./-]{1,150}$/.test(r.apiVersion ?? "") &&
      namePattern.test(r.metadata?.name ?? "") && (!r.metadata.namespace || r.metadata.namespace === profile.namespace),
    "SYSTEM_HELM_OBJECT_SCOPE_INVALID");
    return { apiVersion: r.apiVersion, kind: r.kind, metadata: { name: r.metadata.name,
      ...(r.metadata.namespace ? { namespace: r.metadata.namespace } : {}) } };
  });
  const result = new Map();
  for (const onlyMetadata of [false, true]) {
    const items = definitions.filter(r => metadataOnly(r) === onlyMetadata);
    if (!items.length) continue;
    const format = onlyMetadata ? 'jsonpath={range .items[*]}{.apiVersion}{"|"}{.kind}{"|"}{.metadata.namespace}{"|"}{.metadata.name}{"|"}{.metadata.uid}{"|"}{.metadata.resourceVersion}{"\\n"}{end}' : "json";
    const raw = await kube(["-n", profile.namespace, "get", "-f", "/dev/stdin", "--ignore-not-found", "-o", format],
      JSON.stringify({ apiVersion: "v1", kind: "List", items }));
    const resources = onlyMetadata ? raw.trim().split("\n").filter(Boolean).map(line => {
      const values = line.split("|");
      requireValue(values.length === 6 && values.every((v, i) => i === 2 || v.length > 0), "SYSTEM_HELM_METADATA_INVALID");
      return { apiVersion: values[0], kind: values[1], metadata: { namespace: values[2] || undefined,
        name: values[3], uid: values[4], resourceVersion: values[5] } };
    }) : JSON.parse(raw).items;
    requireValue(Array.isArray(resources) && resources.length <= items.length, "SYSTEM_HELM_OBJECT_READ_INVALID");
    const expected = new Set(items.map(systemObjectKey));
    for (const r of resources) {
      const key = systemObjectKey(r);
      requireValue(expected.has(key) && !result.has(key) && !r.metadata.deletionTimestamp, "SYSTEM_HELM_OBJECT_READ_INVALID");
      systemObjectState(r); result.set(key, r);
    }
  }
  return result;
}

export function systemRecoveryValues(profile, original) {
  const result = structuredClone(original ?? {});
  const locations = profile.name === "cert-manager" ? [[], ["cainjector"], ["webhook"]] :
    profile.name === "trust-manager" ? [[]] : profile.name === "kube-prometheus-stack" ? [["kube-state-metrics"], ["alertmanager"]] : null;
  requireValue(locations, "SYSTEM_HELM_PROFILE_INVALID");
  for (const path of locations) {
    let target = result, source = profile.values;
    for (const part of path) { target[part] ??= {}; target = target[part]; source = source[part]; }
    // Только уже созданные exact PDB сохраняются при восстановлении. Их UID
    // не пересоздаётся, а replicas/resources возвращаются из исходной revision.
    target.podDisruptionBudget = structuredClone(source.podDisruptionBudget);
  }
  return result;
}

export function systemDesiredValues(original, overrides) {
  const result = structuredClone(original ?? {});
  for (const [key, value] of Object.entries(overrides)) {
    requireValue(!["__proto__", "constructor", "prototype"].includes(key), "SYSTEM_HELM_VALUES_KEY_INVALID");
    result[key] = value && typeof value === "object" && !Array.isArray(value) ?
      systemDesiredValues(result[key] && typeof result[key] === "object" && !Array.isArray(result[key]) ? result[key] : {}, value) : structuredClone(value);
  }
  return result;
}

export function systemRecoveryManifest(before, desired) {
  const previous = new Map(before.map(r => [systemObjectKey(r), r]));
  requireValue(previous.size === before.length, "SYSTEM_HELM_RENDER_INVALID");
  return desired.map(r => structuredClone(previous.get(systemObjectKey(r)) ?? r));
}

export async function systemExpectedObjects(profile, before, desired, live, kube) {
  const old = new Map(before.map(r => [systemObjectKey(r), r])), result = [];
  for (const r of desired) {
    const key = systemObjectKey(r), current = live.get(key);
    if (!old.has(key)) {
      requireValue(!current && r.kind === "PodDisruptionBudget", "SYSTEM_HELM_SUPPORT_COLLISION");
      const input = structuredClone(r);
      input.metadata.annotations = { ...input.metadata.annotations, "meta.helm.sh/release-name": profile.release,
        "meta.helm.sh/release-namespace": profile.namespace };
      input.metadata.labels = { ...input.metadata.labels, "app.kubernetes.io/managed-by": "Helm" };
      const proof = JSON.parse(await kube(["-n", profile.namespace, "create", "--dry-run=server", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(input)));
      // UID dry-run может отсутствовать; он не сохраняется как настоящий receipt.
      proof.metadata.uid = "dry-run"; proof.metadata.resourceVersion = "0";
      const state = systemObjectState(proof); delete state.uid;
      result.push({ ...state, uid: null, before: null, after: state.contentSHA256, recovery: state.contentSHA256 });
      continue;
    }
    requireValue(current, "SYSTEM_HELM_SOURCE_OBJECT_MISSING");
    const previous = systemObjectState(current), expected = structuredClone(current);
    if (r.kind === "Deployment" && profile.deployments.includes(r.metadata.name)) {
      expected.spec.replicas = 2;
      for (const group of ["containers", "initContainers"]) {
        const containers = r.spec.template.spec[group] ?? [];
        requireValue(containers.length === (expected.spec.template.spec[group] ?? []).length, "SYSTEM_HELM_CONTAINER_CHANGE_FORBIDDEN");
        containers.forEach((c, i) => {
          requireValue(expected.spec.template.spec[group][i].name === c.name, "SYSTEM_HELM_CONTAINER_CHANGE_FORBIDDEN");
          expected.spec.template.spec[group][i].resources = structuredClone(c.resources);
        });
      }
    } else if (r.kind === "Alertmanager" && r.metadata.name === profile.alertmanager) {
      expected.spec.replicas = 2; expected.spec.resources = structuredClone(r.spec.resources);
    }
    const next = systemObjectState(expected);
    result.push({ ...previous, before: previous.contentSHA256 ?? null, after: next.contentSHA256 ?? null,
      recovery: previous.contentSHA256 ?? null });
  }
  return result.sort((a, b) => [a.apiVersion, a.kind, a.namespace, a.name].join("/").localeCompare([b.apiVersion, b.kind, b.namespace, b.name].join("/"), "en"));
}

export function matchSystemObjects(expected, actual, phases, receipts = []) {
  requireValue(actual.size <= expected.length, "SYSTEM_HELM_OBJECT_SET_INVALID");
  const recorded = new Map(receipts.map(r => [[r.apiVersion, r.kind, r.namespace, r.name].join("/"), r.uid]));
  const observations = [];
  for (const e of expected) {
    const key = [e.apiVersion, e.kind, e.namespace, e.name].join("/"), current = actual.get(key);
    if (!current) {
      requireValue(!e.uid && phases.includes("before") && !recorded.has(key), "SYSTEM_HELM_OBJECT_MISSING"); continue;
    }
    const state = systemObjectState(current);
    requireValue(e.uid ? state.uid === e.uid : !recorded.has(key) || state.uid === recorded.get(key), "SYSTEM_HELM_OBJECT_UID_CHANGED");
    requireValue(e.resourceVersion ? state.resourceVersion === e.resourceVersion :
      phases.some(phase => e[phase] && state.contentSHA256 === e[phase]), "SYSTEM_HELM_OBJECT_DRIFT");
    if (!e.uid) requireValue(current.metadata.annotations?.["meta.helm.sh/release-name"] &&
      current.metadata.annotations?.["meta.helm.sh/release-namespace"] === e.namespace, "SYSTEM_HELM_SUPPORT_OWNER_INVALID");
    observations.push(state);
  }
  requireValue(observations.length === actual.size, "SYSTEM_HELM_OBJECT_SET_INVALID");
  return observations;
}
