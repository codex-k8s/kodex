import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { promisify } from "node:util";
import { pathToFileURL } from "node:url";

const execute = promisify(execFile);
const namespaces = ["kodex-system", "kodex-runtime", "identity", "observability", "kube-system", "cert-manager"];
const types = new Set(["Deployment", "StatefulSet", "DaemonSet", "CronJob", "Job", "Pod"]);
const namePattern = /^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$/;
const digestPattern = /^sha256:[a-f0-9]{64}$/;
const dataNames = new Set(["kodex-postgresql", "email-bridge-postgresql", "keycloak-postgresql", "kodex-nats", "seaweedfs", "proxy-session-store"]);
const approvedSingletons = new Set([
  "kube-system/Deployment/local-path-provisioner",
  "observability/StatefulSet/kodex-monitoring-grafana",
]);

function requireValue(value, code) {
  if (!value) throw Error(code);
}

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === "object")
    return Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])]));
  return value;
}

export function fingerprint(value) {
  return createHash("sha256").update(JSON.stringify(canonical(value))).digest("hex");
}

// В отчёт не копируются spec/env/args, Secret contents, paths или user payload.
// Полный spec используется только для commitment текущего read-only наблюдения.
export function summarizeWorkload(resource) {
  const { metadata: meta, spec, status = {}, kind } = resource ?? {};
  requireValue(types.has(kind) && meta && namespaces.includes(meta.namespace) &&
    namePattern.test(meta.name ?? "") && typeof meta.uid === "string" && meta.uid.length > 0 &&
    typeof meta.resourceVersion === "string" && spec && typeof spec === "object", "WORKLOAD_IDENTITY_INVALID");
  const pod = kind === "Pod" ? spec : kind === "CronJob" ? spec.jobTemplate?.spec?.template?.spec : spec.template?.spec;
  requireValue(pod && Array.isArray(pod.containers) && pod.containers.length > 0 && pod.containers.length <= 32,
    "POD_TEMPLATE_INVALID");
  const containers = [...pod.containers, ...(pod.initContainers ?? [])];
  requireValue(containers.length <= 64 && containers.every(c => namePattern.test(c.name ?? "") && typeof c.image === "string"),
    "CONTAINER_IDENTITY_INVALID");
  const hostVolumes = new Set((pod.volumes ?? []).filter(v => v.hostPath).map(v => v.name));
  const sourceMounts = containers.reduce((total, c) => total + (c.volumeMounts ?? []).filter(m =>
    hostVolumes.has(m.name) && (m.mountPath === "/workspace" || m.mountPath?.startsWith("/workspace/"))).length, 0);
  const hotContainers = containers.filter(c => [...(c.command ?? []), ...(c.args ?? [])].some(a =>
    typeof a === "string" && /(?:run-go-hot-reload\.sh|run-frontend\.sh|(?:^|\/)air(?:$|\s)|(?:^|\/)vite(?:$|\s))/.test(a)));
  const category = kind === "Pod" ? "INSTANCE" : kind === "Job" || kind === "CronJob" ? "EXECUTION" :
    kind === "DaemonSet" ? "PER_NODE" : approvedSingletons.has(`${meta.namespace}/${kind}/${meta.name}`) ? "SINGLETON" : dataNames.has(meta.name) ||
      kind === "StatefulSet" && meta.name.startsWith("prometheus-") ? "DATA" : "SERVICE";
  const replicas = ["Deployment", "StatefulSet"].includes(kind) ? spec.replicas ?? 1 : null;
  requireValue(replicas === null || Number.isSafeInteger(replicas) && replicas >= 0 && replicas <= 256, "REPLICAS_INVALID");
  return {
    kind, namespace: meta.namespace, name: meta.name, uid: meta.uid, resourceVersion: meta.resourceVersion,
    specSHA256: fingerprint(spec), category, currentReplicas: replicas,
    targetReplicas: category === "SERVICE" ? 2 : replicas,
    readyReplicas: status.readyReplicas ?? null,
    hostSourceMountCount: sourceMounts,
    hotContainerCount: hotContainers.length,
    persistentVolumeCount: (pod.volumes ?? []).filter(v => v.persistentVolumeClaim).length,
    images: containers.map(c => ({ name: c.name, immutable: digestPattern.test(c.image.split("@").at(-1)) })),
    requiresStorageReview: category === "SERVICE" && (kind === "StatefulSet" ||
      (pod.volumes ?? []).some(v => v.persistentVolumeClaim)),
  };
}

export function summarizeInventory(resources) {
  requireValue(Array.isArray(resources) && resources.length > 0 && resources.length <= 512, "INVENTORY_BOUND_INVALID");
  const rows = resources.map(summarizeWorkload);
  const keys = rows.map(r => `${r.namespace}/${r.kind}/${r.name}`);
  requireValue(new Set(keys).size === keys.length, "DUPLICATE_WORKLOAD");
  rows.sort((a, b) => `${a.namespace}/${a.kind}/${a.name}`.localeCompare(`${b.namespace}/${b.kind}/${b.name}`, "en"));
  return { schema: "kodex.stable-inventory/v1", rows, inventorySHA256: fingerprint(rows) };
}

async function main() {
  const args = process.argv.slice(2);
  requireValue(args.length === 2 && args[0] === "--context" && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(args[1]), "EXACT_CONTEXT_REQUIRED");
  const resources = [];
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(key => process.env[key]).map(key => [key, process.env[key]]));
  for (const namespace of namespaces) {
    const { stdout } = await execute("kubectl", ["--context", args[1], "--request-timeout=15s", "-n", namespace,
      "get", "deployments,statefulsets,daemonsets,cronjobs,jobs,pods", "-o", "json"],
    { env, timeout: 20000, maxBuffer: 16 << 20 });
    const list = JSON.parse(stdout);
    requireValue(Array.isArray(list.items) && list.items.length <= 512, "INVENTORY_BOUND_INVALID");
    resources.push(...list.items);
  }
  console.log(JSON.stringify(summarizeInventory(resources)));
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main().catch(error => {
    const code = /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "INVENTORY_READ_FAILED";
    console.error(JSON.stringify({ status: "FAIL", code })); process.exitCode = 1;
  });
