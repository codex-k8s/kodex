import { fingerprint } from "./stable-inventory.mjs";
function requireValue(value, code) { if (!value) throw Error(code); }

// canonicalTemplate загружается адаптером из repo-owned Deployment, не из
// пользовательского API. Здесь не открываются Secret data или runtime config.
export function prepareFrontendCutover(deployment, canonicalTemplate, release) {
  const meta = deployment?.metadata, before = deployment?.spec;
  requireValue(deployment?.kind === "Deployment" && meta?.namespace === "kodex-system" &&
    meta.name === "staff-control-center" && meta.uid && meta.resourceVersion && before?.template?.spec,
    "FRONTEND_IDENTITY_INVALID");
  requireValue(release?.component === "staff-control-center" && /^[a-f0-9]{40}$/.test(release.sourceSHA ?? "") &&
    /^[a-f0-9]{64}$/.test(release.provenanceSHA256 ?? "") && release.provenanceSHA256 !== "0".repeat(64) &&
    /^[a-z0-9][a-z0-9.:/_-]*\/staff-control-center@sha256:[a-f0-9]{64}$/.test(release.image ?? "") &&
    !release.image.endsWith("0".repeat(64)), "FRONTEND_RELEASE_INVALID");
  const old = before.template.spec.containers?.find(c => c.name === meta.name);
  requireValue(old?.command?.length === 1 && old.command[0] === "/workspace/tools/dev/run-frontend.sh" &&
    before.template.spec.containers.length === 1 && !(before.template.spec.initContainers ?? []).length,
    "FRONTEND_SOURCE_COMMAND_MISMATCH");
  const pod = structuredClone(canonicalTemplate?.spec);
  requireValue(pod && pod.serviceAccountName === before.template.spec.serviceAccountName &&
    pod.automountServiceAccountToken === false && pod.containers?.length === 1 &&
    !(pod.volumes ?? []).some(v => v.hostPath), "FRONTEND_CANONICAL_TEMPLATE_INVALID");
  const app = pod.containers[0];
  requireValue(app.name === meta.name && !app.command && !app.args &&
    app.ports?.some(p => p.name === "https" && p.containerPort === 8443) &&
    app.readinessProbe?.httpGet?.scheme === "HTTPS" && app.securityContext?.runAsUser === 101,
    "FRONTEND_CANONICAL_RUNTIME_INVALID");
  const runtime = before.template.spec.volumes?.find(v => v.name === "runtime-config");
  const desiredRuntime = pod.volumes?.find(v => v.name === "runtime-config");
  requireValue(runtime?.configMap?.name && desiredRuntime?.configMap &&
    app.volumeMounts?.some(m => m.name === "runtime-config" &&
      m.mountPath === "/var/run/config/kodex/staff-control-center/runtime" && m.readOnly === true),
    "FRONTEND_RUNTIME_CONFIG_REQUIRED");
  desiredRuntime.configMap.name = runtime.configMap.name;
  app.image = release.image;
  const after = structuredClone(before);
  after.replicas = 2;
  after.strategy = { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } };
  after.template.spec = pod;
  after.template.metadata.labels = { ...after.template.metadata.labels, ...canonicalTemplate.metadata?.labels,
    "kodex.dev/local-profile": "stable" };
  after.template.metadata.annotations = { ...after.template.metadata.annotations,
    "kodex.dev/source-sha": release.sourceSHA, "kodex.dev/release-provenance-sha256": release.provenanceSHA256 };
  for (const key of ["source-root", "cache-root", "host-uid", "host-gid", "source-content-sha256", "source-revision"])
    delete after.template.metadata.annotations[`kodex.dev/${key}`];
  return { name: meta.name, uid: meta.uid, beforeSpecSHA256: fingerprint(before), afterSpecSHA256: fingerprint(after),
    // Service/transport переключаются отдельной фазой под maintenance: old Vite
    // слушает8080, новый nginx8443. Не объявляем этот переход zero downtime.
    requiresServiceTLSCutover: true,
    patch: [{ op: "test", path: "/metadata/uid", value: meta.uid },
      { op: "test", path: "/metadata/resourceVersion", value: meta.resourceVersion },
      { op: "test", path: "/spec", value: before },
      { op: "replace", path: "/spec", value: after }] };
}
