import { fingerprint } from "./stable-inventory.mjs";
import { activeDeploymentPods, isProvenStoppedPod } from "./stable-archive-transition.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
const namePattern = /^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$/;
export const systemObjectKey = r => `${r.apiVersion}/${r.kind}/${r.metadata?.namespace ?? ""}/${r.metadata?.name}`;
const metadataOnly = r => ["Secret", "ConfigMap"].includes(r.kind) && !contentConfig(r);
// Единственное изменяемое содержимое CM в public Helm scope: несекретный
// grafana.ini. Все остальные CM и любые Secret читаются только как UID/RV.
const contentConfig = r => r.kind === "ConfigMap" && r.metadata?.namespace === "observability" &&
  r.metadata.name === "kodex-monitoring-grafana";

// Status, managedFields и timestamps не являются конфигурацией. Значения
// Secret/ConfigMap для проверки drift вообще не запрашиваются: достаточно UID/RV.
export function systemObjectState(r) {
  requireValue(r?.metadata?.uid && r.metadata.resourceVersion && r.kind && r.apiVersion, "SYSTEM_HELM_OBJECT_IDENTITY_INVALID");
  const identity = { apiVersion: r.apiVersion, kind: r.kind, namespace: r.metadata.namespace ?? "", name: r.metadata.name,
    uid: r.metadata.uid };
  if (metadataOnly(r)) return { ...identity, resourceVersion: r.metadata.resourceVersion };
  const content = structuredClone(r); delete content.status;
  const annotations = { ...r.metadata.annotations };
  // Deployment controller меняет rollout revision при штатном обновлении.
  // Это наблюдаемое поколение, не source configuration и не authority.
  if (r.kind === "Deployment") delete annotations["deployment.kubernetes.io/revision"];
  content.metadata = { name: r.metadata.name, ...(r.metadata.namespace ? { namespace: r.metadata.namespace } : {}),
    labels: r.metadata.labels ?? {}, annotations,
    ...(r.metadata.ownerReferences ? { ownerReferences: r.metadata.ownerReferences } : {}),
    ...(r.metadata.finalizers ? { finalizers: r.metadata.finalizers } : {}) };
  return { ...identity, contentSHA256: fingerprint(content) };
}

export async function readSystemObjects(profile, rendered, kube) {
  requireValue(rendered.length > 0 && rendered.length < 512 && new Set(rendered.map(systemObjectKey)).size === rendered.length,
    "SYSTEM_HELM_OBJECT_SET_INVALID");
  const definitions = rendered.map(r => {
    requireValue(/^[A-Za-z][A-Za-z0-9]{0,100}$/.test(r.kind ?? "") && /^[a-z0-9./-]{1,150}$/.test(r.apiVersion ?? "") &&
      (namePattern.test(r.metadata?.name ?? "") || ["Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding"].includes(r.kind) &&
        /^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,252}$/.test(r.metadata?.name ?? "")) &&
      (!r.metadata.namespace || namePattern.test(r.metadata.namespace)),
    "SYSTEM_HELM_OBJECT_SCOPE_INVALID");
    return { apiVersion: r.apiVersion, kind: r.kind, metadata: { name: r.metadata.name,
      ...(r.metadata.namespace ? { namespace: r.metadata.namespace } : {}) } };
  });
  const result = new Map();
  for (const onlyMetadata of [false, true]) {
    const items = definitions.filter(r => metadataOnly(r) === onlyMetadata);
    if (!items.length) continue;
    const record = '{.apiVersion}{"|"}{.kind}{"|"}{.metadata.namespace}{"|"}{.metadata.name}{"|"}{.metadata.uid}{"|"}{.metadata.resourceVersion}{"\\n"}';
    // kubectl возвращает одиночный object даже для входного List из одного
    // элемента. Читаются обе формы; служебная строка корневого List отсекается.
    const format = onlyMetadata ? 'jsonpath={range .items[*]}' + record + '{end}' + record : "json";
    // Helm владеет также exact Role в leader/trust namespaces. -n здесь
    // принудительно заменил бы namespace документов и сломал cross-namespace read.
    const raw = await kube(["get", "-f", "/dev/stdin", "--ignore-not-found", "-o", format],
      JSON.stringify({ apiVersion: "v1", kind: "List", items }));
    const resources = onlyMetadata ? raw.trim().split("\n").filter(Boolean).map(line => {
      const values = line.split("|");
      if (values.length === 6 && values[0] === "v1" && values[1] === "List" && !values[2] && !values[3] && !values[4]) return null;
      requireValue(values.length === 6 && values.every((v, i) => i === 2 || v.length > 0), "SYSTEM_HELM_METADATA_INVALID");
      return { apiVersion: values[0], kind: values[1], metadata: { namespace: values[2] || undefined,
        name: values[3], uid: values[4], resourceVersion: values[5] } };
    }).filter(Boolean) : (() => { const result = JSON.parse(raw); return result.items ?? [result]; })();
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

export async function systemServing(profile, kube, baseline, count) {
  const targets = profile.deployments.map(name => ({ kind: "Deployment", name }));
  if (profile.alertmanager) targets.push({ kind: "StatefulSet", name: "alertmanager-" + profile.alertmanager });
  if (profile.grafana) targets.push({ kind: "StatefulSet", name: profile.grafana, singleton: true });
  const proof = [];
  for (const target of targets) {
    const d = JSON.parse(await kube(["-n", profile.namespace, "get", target.kind, target.name, "-o", "json"]));
    const replicas = target.singleton ? 1 : count ?? d.spec.replicas, previous = baseline?.find(r => r.kind === target.kind && r.name === target.name);
    requireValue([1, 2].includes(replicas) && d.spec.replicas === replicas && d.metadata.uid && !d.metadata.deletionTimestamp &&
      (!baseline || previous && previous.uid === d.metadata.uid) && d.status?.observedGeneration >= d.metadata.generation &&
      ["replicas", "updatedReplicas", "readyReplicas"].every(k => d.status[k] === replicas) &&
      (target.kind !== "Deployment" || d.status.availableReplicas === replicas) &&
      (target.kind !== "StatefulSet" || d.status.currentRevision === d.status.updateRevision), "SYSTEM_HELM_SERVING_INCOMPLETE");
    const labels = d.spec.selector?.matchLabels;
    requireValue(labels && Object.keys(labels).length && !d.spec.selector.matchExpressions?.length &&
      Object.entries(labels).every(([k, v]) => /^[A-Za-z0-9_./-]{1,200}$/.test(k) && /^[A-Za-z0-9_.-]{1,63}$/.test(v)),
    "SYSTEM_HELM_SELECTOR_INVALID");
    const selector = Object.entries(labels).sort().map(([k, v]) => `${k}=${v}`).join(",");
    const list = JSON.parse(await kube(["-n", profile.namespace, "get", target.kind === "Deployment" ? "replicasets,pods" : "pods",
      "-l", selector, "-o", "json"]));
    let pods, operatorOwnerUID;
    if (target.kind === "Deployment") pods = activeDeploymentPods(list, d.metadata.uid);
    else {
      if (target.singleton) {
        requireValue(!d.metadata.ownerReferences?.length && d.metadata.annotations?.["meta.helm.sh/release-name"] === profile.release &&
          d.metadata.annotations?.["meta.helm.sh/release-namespace"] === profile.namespace,
        "SYSTEM_HELM_GRAFANA_OWNER_INVALID");
      } else {
        const owners = d.metadata.ownerReferences?.filter(o => o.kind === "Alertmanager" && o.name === profile.alertmanager && o.controller === true) ?? [];
        requireValue(owners.length === 1 && owners[0].uid, "SYSTEM_HELM_OPERATED_OWNER_INVALID"); operatorOwnerUID = owners[0].uid;
      }
      requireValue(["whenScaled", "whenDeleted"].every(k => (d.spec.persistentVolumeClaimRetentionPolicy?.[k] ?? "Retain") === "Retain"),
        "SYSTEM_HELM_OPERATED_STORAGE_RETENTION_INVALID");
      requireValue(Array.isArray(list.items) && list.items.every(p => p.metadata.ownerReferences?.some(o => o.kind === "StatefulSet" &&
        o.uid === d.metadata.uid && o.controller === true)), "SYSTEM_HELM_FOREIGN_SERVING_POD");
      pods = list.items.filter(p => !isProvenStoppedPod(p));
    }
    requireValue(pods.length === replicas && new Set(pods.map(p => p.metadata.uid)).size === replicas, "SYSTEM_HELM_SERVING_INCOMPLETE");
    const images = new Map();
    for (const p of pods) {
      requireValue(!p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True"),
        "SYSTEM_HELM_SERVING_INCOMPLETE");
      for (const [group, statuses] of [["containers", "containerStatuses"], ["initContainers", "initContainerStatuses"]]) {
        const containers = d.spec.template.spec[group] ?? [], actual = p.spec[group] ?? [], states = p.status[statuses] ?? [];
        requireValue(actual.length === containers.length && states.length === containers.length &&
          new Set(states.map(s => s.name)).size === states.length, "SYSTEM_HELM_SERVING_CONTAINER_INVALID");
        for (const c of containers) {
          const state = states.find(s => s.name === c.name), key = group + "/" + c.name;
          requireValue(actual.some(a => a.name === c.name && a.image === c.image) && state &&
            /(?:^|[/:@])sha256:[a-f0-9]{64}$/.test(state.imageID ?? "") &&
            (group === "containers" || c.restartPolicy === "Always" ? state.ready && state.state?.running : state.state?.terminated?.exitCode === 0) &&
            (!images.has(key) || images.get(key) === state.imageID), "SYSTEM_HELM_SERVING_IMAGE_INVALID");
          images.set(key, state.imageID);
        }
      }
    }
    const observed = { kind: target.kind, name: target.name, uid: d.metadata.uid,
      ...(target.kind === "StatefulSet" ? { ...(operatorOwnerUID ? { operatorOwnerUID } : {}), storageSHA256: fingerprint({ claims: d.spec.volumeClaimTemplates ?? [],
        retention: d.spec.persistentVolumeClaimRetentionPolicy ?? {} }) } : {}),
      images: [...images].sort().map(([name, imageID]) => ({ name, imageID })) };
    requireValue(!previous || fingerprint(observed) === fingerprint(previous), "SYSTEM_HELM_SERVING_IMAGE_CHANGED");
    proof.push(observed);
  }
  return proof;
}
