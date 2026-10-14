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
const devEnv = new Set(["GOTOOLCHAIN", "GOPATH", "GOMODCACHE", "GOCACHE", "GOSUMDB", "GOPROXY", "GOFLAGS"]);
const imagePattern = /^[a-z0-9][a-z0-9.:/_-]*@sha256:[a-f0-9]{64}$/;
function requireValue(value, code) { if (!value) throw Error(code); }

export function prepareGoCutover(deployment, release) {
  const name = deployment?.metadata?.name;
  const entry = goServices[name];
  requireValue(deployment?.kind === "Deployment" && deployment.metadata.namespace === "kodex-system" && entry,
    "CUTOVER_COMPONENT_UNSUPPORTED");
  requireValue(typeof deployment.metadata.uid === "string" && deployment.metadata.uid &&
    typeof deployment.metadata.resourceVersion === "string", "CUTOVER_IDENTITY_INVALID");
  requireValue(release && /^[a-f0-9]{40}$/.test(release.sourceSHA ?? "") && imagePattern.test(release.image ?? "") &&
    !release.image.endsWith("0".repeat(64)) && release.component === entry[2] &&
    /^[a-f0-9]{64}$/.test(release.provenanceSHA256 ?? ""), "CUTOVER_RELEASE_INVALID");
  const before = deployment.spec;
  const after = structuredClone(before);
  const pod = after.template.spec;
  const app = pod.containers.find(c => c.name === name);
  requireValue(app && app.command?.length === 1 && app.command[0] === "/workspace/tools/dev/run-go-hot-reload.sh" &&
    app.args?.[0] === entry[0] && app.args[1] === `./cmd/${entry[1]}` &&
    // clamav-egress-gateway использует то же бинарное содержимое.
    [name, entry[1]].includes(app.args[2]), "CUTOVER_SOURCE_COMMAND_MISMATCH");
  const devVolumes = new Set(["dev-source", "dev-go-mod", "dev-go-sumdb", "dev-go-tools", `dev-build-${name}`]);
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
  const others = [...pod.containers, ...(pod.initContainers ?? [])].filter(c => c !== app);
  requireValue(!others.some(c => (c.volumeMounts ?? []).some(m => removedVolumes.has(m.name))),
    "CUTOVER_DEV_VOLUME_SHARED");
  app.image = release.image;
  app.command = [`/usr/local/bin/${entry[1]}`];
  app.args = app.args.slice(3);
  app.env = (app.env ?? []).filter(e => !devEnv.has(e.name));
  app.volumeMounts = (app.volumeMounts ?? []).filter(m => !removedVolumes.has(m.name));
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
  return {
    name, uid: deployment.metadata.uid, beforeSpecSHA256: fingerprint(before), afterSpecSHA256: fingerprint(after),
    sourceSHA: release.sourceSHA,
    patch: [
      { op: "test", path: "/metadata/uid", value: deployment.metadata.uid },
      { op: "test", path: "/metadata/resourceVersion", value: deployment.metadata.resourceVersion },
      { op: "test", path: "/spec", value: before },
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
