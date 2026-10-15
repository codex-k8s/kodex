import { fingerprint } from "./stable-inventory.mjs";
import { isProvenStoppedPod } from "./stable-archive-transition.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
export function nativeHelmJob(job, pods, chartUID, knownJobUIDs = []) {
  requireValue(job?.apiVersion === "batch/v1" && job.kind === "Job" && job.metadata?.namespace === "kube-system" &&
    job.metadata.name === "helm-install-traefik" && job.metadata.uid && !job.metadata.deletionTimestamp &&
    job.metadata.ownerReferences?.some(o => o.apiVersion === "helm.cattle.io/v1" && o.kind === "HelmChart" &&
      o.name === "traefik" && o.uid === chartUID) && job.spec?.template?.spec?.containers?.length === 1 &&
    job.spec.template.spec.containers[0].name === "helm" && job.spec.template.spec.serviceAccountName === "helm-traefik",
  "NATIVE_HELM_JOB_OWNER_INVALID");
  const container = job.spec.template.spec.containers[0], configHash = job.spec.template.metadata?.annotations?.["helmcharts.helm.cattle.io/configHash"];
  requireValue(/^rancher\/klipper-helm:v[A-Za-z0-9_.-]+$/.test(container.image) && /^SHA256=[A-Fa-f0-9]{64}$/.test(configHash ?? "") &&
    ["INSECURE_SKIP_TLS_VERIFY", "PLAIN_HTTP"].every(name => container.env?.find(e => e.name === name)?.value === "false"),
  "NATIVE_HELM_JOB_PROFILE_INVALID");
  requireValue(Array.isArray(pods?.items), "NATIVE_HELM_JOB_PODS_INVALID");
  const owners = new Set([job.metadata.uid, ...knownJobUIDs]);
  requireValue(pods.items.every(p => p.kind === "Pod" && p.metadata?.namespace === "kube-system" && p.metadata.ownerReferences?.some(o =>
    o.kind === "Job" && o.name === "helm-install-traefik" && o.controller === true && owners.has(o.uid))), "NATIVE_HELM_JOB_FOREIGN_POD");
  const stopped = pods.items.every(isProvenStoppedPod), active = job.status?.active ?? 0;
  const complete = job.status?.conditions?.some(c => c.type === "Complete" && c.status === "True") === true;
  const failed = job.status?.conditions?.some(c => c.type === "Failed" && c.status === "True") === true;
  return { uid: job.metadata.uid, configHash, specSHA256: fingerprint(job.spec), image: container.image,
    terminal: (complete || failed) && active === 0 && !(job.status?.terminating ?? 0) && stopped, complete, failed,
    podUIDs: pods.items.map(p => p.metadata.uid).sort(),
    failurePolicy: container.env.find(e => e.name === "FAILURE_POLICY")?.value,
    valuesKeys: (job.spec.template.spec.volumes?.find(v => v.name === "values")?.projected?.sources ?? []).flatMap(s =>
      s.secret?.name === "chart-values-traefik" ? (s.secret.items ?? []).map(i => i.key).sort() : []) };
}
