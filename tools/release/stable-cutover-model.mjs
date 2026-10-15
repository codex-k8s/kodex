import { fingerprint } from "./stable-inventory.mjs";

// Закрытый реестр текущих исполняемых путей: произвольный binary из payload
// не получает возможность заменить штатный сервис.
const goServices = {
  "control-plane": ["services/internal/control-plane", "control-plane", "control-plane"],
  "secret-broker": ["services/internal/secret-broker", "secret-broker", "secret-broker"],
  "stt-tts-service": ["services/internal/stt-tts-service", "stt-tts-service", "stt-tts-service"],
  "email-bridge": ["services/internal/email-bridge", "email-bridge", "email-bridge"],
  "runtime-controller": ["services/internal/runtime-controller", "runtime-controller", "runtime-controller"],
  "integration-gateway": ["services/external/integration-gateway", "integration-gateway", "integration-gateway"],
  "integration-synthetic": ["services/external/integration-gateway", "integration-synthetic", "integration-synthetic"],
  "interaction-gateway": ["services/external/interaction-gateway", "interaction-gateway", "interaction-gateway"],
  "control-api-gateway": ["services/external/control-api-gateway", "control-api-gateway", "control-api-gateway"],
  "egress-gateway": ["services/external/egress-gateway", "egress-gateway", "egress-gateway"],
  "clamav-egress-gateway": ["services/external/egress-gateway", "egress-gateway", "egress-gateway"],
  "automation-scheduler": ["services/jobs/automation-scheduler", "automation-scheduler", "automation-scheduler"],
  "artifact-retention": ["services/jobs/artifact-retention", "artifact-retention", "artifact-retention"],
  "session-archive": ["services/jobs/session-archive", "session-archive", "session-archive"],
  "backup-controller": ["services/jobs/backup-controller", "backup-controller", "backup-controller"],
};
const devEnv = new Set(["GOTOOLCHAIN", "GOPATH", "GOMODCACHE", "GOCACHE", "GOSUMDB", "GOPROXY", "GOFLAGS",
  "GOWORK", "GOTMPDIR", "KODEX_DEV_AIR_VERSION", "KODEX_DEV_AIR_SHA256"]);
const imagePattern = /^[a-z0-9][a-z0-9.:/_-]*@sha256:[a-f0-9]{64}$/;
function requireValue(value, code) { if (!value) throw Error(code); }

export function validateGoReleaseReference(name, release) {
  const entry = goServices[name];
  requireValue(entry && release && /^[a-f0-9]{40}$/.test(release.sourceSHA ?? "") &&
    imagePattern.test(release.image ?? "") && !release.image.endsWith("0".repeat(64)) &&
    release.component === entry[2] && /^[a-f0-9]{64}$/.test(release.provenanceSHA256 ?? "") &&
    release.provenanceSHA256 !== "0".repeat(64), "CUTOVER_RELEASE_INVALID");
}

export function prepareGoCutover(deployment, release, { storageClaims = [] } = {}) {
  const name = deployment?.metadata?.name;
  const entry = goServices[name];
  requireValue(deployment?.kind === "Deployment" && deployment.metadata.namespace === "kodex-system" && entry,
    "CUTOVER_COMPONENT_UNSUPPORTED");
  requireValue(typeof deployment.metadata.uid === "string" && deployment.metadata.uid &&
    typeof deployment.metadata.resourceVersion === "string", "CUTOVER_IDENTITY_INVALID");
  validateGoReleaseReference(name, release);
  const before = deployment.spec;
  const after = structuredClone(before);
  requireValue(!Object.hasOwn(after.selector?.matchLabels ?? {}, "kodex.dev/local-profile") &&
    !(after.selector?.matchExpressions ?? []).some(e => e.key === "kodex.dev/local-profile"), "CUTOVER_PROFILE_SELECTOR_BOUND");
  const pod = after.template.spec;
  const claims = (pod.volumes ?? []).filter(v => v.persistentVolumeClaim).map(v =>
    storageClaims.find(c => c.name === v.persistentVolumeClaim.claimName));
  requireValue(claims.every(c => c && c.bound && c.accessModes?.length > 0), "STORAGE_CLAIM_NOT_BOUND");
  const nodes = [...new Set(claims.filter(c => !c.accessModes.includes("ReadWriteMany")).map(c => c.nodeName))];
  if (nodes.length) {
    requireValue(nodes.length === 1 && nodes[0] && (!pod.nodeSelector?.["kubernetes.io/hostname"] ||
      pod.nodeSelector["kubernetes.io/hostname"] === nodes[0]), "STORAGE_NODE_PIN_REQUIRED");
    pod.nodeSelector = { ...pod.nodeSelector, "kubernetes.io/hostname": nodes[0] };
  }
  validateStorageProtocol({ metadata: deployment.metadata, spec: { template: { spec: pod } } }, { claims: storageClaims });
  const app = pod.containers.find(c => c.name === name);
  requireValue(app && app.command?.length === 1 && app.command[0] === "/workspace/tools/dev/run-go-hot-reload.sh" &&
    app.args?.[0] === entry[0] && app.args[1] === `./cmd/${entry[1]}` &&
    // clamav-egress-gateway использует то же бинарное содержимое.
    [name, entry[1]].includes(app.args[2]), "CUTOVER_SOURCE_COMMAND_MISMATCH");
  const spoolInit = name === "runtime-controller" ? pod.initContainers?.find(c => c.name === "artifact-spool-init") : undefined;
  if (spoolInit) {
    requireValue(spoolInit.command?.join(" ") === "/bin/sh -ec" && spoolInit.args?.length === 1 &&
      spoolInit.args[0].includes("go build") && spoolInit.args[0].includes("./cmd/runtime-controller") &&
      spoolInit.args[0].includes("--prepare-artifact-spool /var/lib/kodex/runtime-controller/artifact-spool") &&
      spoolInit.volumeMounts?.some(m => m.name === "artifact-spool" && m.mountPath === "/var/lib/kodex/runtime-controller/artifact-spool"),
      "CUTOVER_SPOOL_INIT_MISMATCH");
    spoolInit.image = release.image;
    spoolInit.command = ["/usr/local/bin/runtime-controller"];
    spoolInit.args = ["--prepare-artifact-spool", "/var/lib/kodex/runtime-controller/artifact-spool"];
    spoolInit.env = (spoolInit.env ?? []).filter(e => !devEnv.has(e.name));
    spoolInit.securityContext = { ...spoolInit.securityContext, runAsUser: 10001, runAsGroup: 10001 };
  }
  const devVolumes = new Set(["dev-source", "dev-go-mod", "dev-go-sumdb", "dev-go-tools", `dev-build-${name}`,
    ...(spoolInit ? ["dev-build-artifact-spool-init"] : [])]);
  const removedVolumes = new Set();
  for (const volume of pod.volumes ?? []) {
    if (devVolumes.has(volume.name)) {
      requireValue(volume.hostPath && !volume.secret && !volume.configMap && !volume.persistentVolumeClaim,
        "CUTOVER_DEV_VOLUME_TYPE_CHANGED");
      removedVolumes.add(volume.name);
    } else if (volume.name === "kodex-local-private-source-mask") {
      requireValue(volume.configMap?.name === "kodex-local-private-source-mask", "CUTOVER_SOURCE_MASK_CHANGED");
      removedVolumes.add(volume.name);
    }
  }
  const converted = [app, ...(spoolInit ? [spoolInit] : [])];
  const others = [...pod.containers, ...(pod.initContainers ?? [])].filter(c => !converted.includes(c));
  requireValue(!others.some(c => (c.volumeMounts ?? []).some(m => removedVolumes.has(m.name))),
    "CUTOVER_DEV_VOLUME_SHARED");
  app.image = release.image;
  app.command = [`/usr/local/bin/${entry[1]}`];
  app.args = app.args.slice(3);
  app.env = (app.env ?? []).filter(e => !devEnv.has(e.name));
  if (name === "session-archive") {
    const workers = app.env.filter(e => e.name === "SESSION_ARCHIVE_WORKER_IMAGE");
    requireValue(workers.length === 1 && !workers[0].valueFrom && imagePattern.test(workers[0].value ?? "") &&
      !workers[0].value.endsWith("0".repeat(64)), "CUTOVER_ARCHIVE_WORKER_PIN_INVALID");
    // Controller и worker — два режима одного проверенного binary/OCI.
    workers[0].value = release.image;
  }
  for (const container of converted)
    container.volumeMounts = (container.volumeMounts ?? []).filter(m => !removedVolumes.has(m.name));
  pod.volumes = (pod.volumes ?? []).filter(v => !removedVolumes.has(v.name));
  // UID приложения из Dockerfile; fsGroup и identity sidecar не меняются.
  // Secret references и существующие supplementary groups сохраняются.
  app.securityContext = { ...app.securityContext, runAsNonRoot: true, runAsUser: 10001, runAsGroup: 10001,
    allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: ["ALL"] } };
  requireValue(!app.volumeMounts.some(m => m.mountPath === "/workspace" || m.mountPath.startsWith("/workspace/")) &&
    !(pod.volumes ?? []).some(v => v.hostPath), "CUTOVER_HOST_MOUNT_REMAINS");
  after.replicas = 2;
  after.strategy = { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } };
  after.template.metadata.annotations = { ...after.template.metadata.annotations,
    "kodex.dev/source-sha": release.sourceSHA,
    "kodex.dev/release-provenance-sha256": release.provenanceSHA256 };
  for (const key of ["source-root", "cache-root", "host-uid", "host-gid", "source-content-sha256", "source-revision"])
    delete after.template.metadata.annotations[`kodex.dev/${key}`];
  after.template.metadata.annotations["kodex.dev/deployment-profile"] = "trusted-cluster";
  after.template.metadata.labels = { ...after.template.metadata.labels, "kodex.dev/local-profile": "stable" };
  if (name === "runtime-controller") after.template.metadata.annotations["kodex.dev/controller-image"] = release.image;
  return {
    name, uid: deployment.metadata.uid, beforeSpecSHA256: fingerprint(before), afterSpecSHA256: fingerprint(after),
    sourceSHA: release.sourceSHA,
    patch: [
      { op: "test", path: "/metadata/uid", value: deployment.metadata.uid },
      { op: "test", path: "/metadata/resourceVersion", value: deployment.metadata.resourceVersion },
      { op: "test", path: "/spec", value: before },
      { op: "add", path: "/metadata/labels", value: { ...deployment.metadata.labels, "kodex.dev/local-profile": "stable" } },
      { op: "replace", path: "/spec", value: after },
    ],
  };
}

export function validateStorageProtocol(resource, { claims, sharedHTTPSecret = false } = {}) {
  const pod = resource?.spec?.template?.spec;
  requireValue(pod && Array.isArray(claims), "STORAGE_PROTOCOL_REQUIRED");
  for (const volume of pod.volumes ?? []) {
    if (!volume.persistentVolumeClaim) continue;
    const claim = claims.find(c => c.name === volume.persistentVolumeClaim.claimName);
    requireValue(claim && claim.bound && claim.accessModes?.length > 0, "STORAGE_CLAIM_NOT_BOUND");
    requireValue(!claim.accessModes.includes("ReadWriteOncePod"), "STORAGE_SINGLE_POD_ONLY");
    if (!claim.accessModes.includes("ReadWriteMany")) {
      requireValue(claim.accessModes.includes("ReadWriteOnce") && claim.nodeName &&
        pod.nodeSelector?.["kubernetes.io/hostname"] === claim.nodeName, "STORAGE_NODE_PIN_REQUIRED");
    }
  }
  if (resource.metadata?.name?.startsWith("kodex-image-registry"))
    requireValue(sharedHTTPSecret, "REGISTRY_SHARED_HTTP_SECRET_REQUIRED");
  return true;
}
