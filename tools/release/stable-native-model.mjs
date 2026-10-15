import { createHash } from "node:crypto";
import { fingerprint } from "./stable-inventory.mjs";
import { boundedResources } from "./stable-resources.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
const digestPattern = /^sha256:[a-f0-9]{64}$/;
export const nativeSkipPaths = Object.freeze({
  coredns: "/var/lib/rancher/k3s/server/manifests/coredns.yaml.skip",
  "metrics-server": "/var/lib/rancher/k3s/server/manifests/metrics-server/metrics-server-deployment.yaml.skip",
});

export function verifyNativeImage(profile, indexBytes, manifestBytes, configBytes) {
  const parse = (bytes, digest) => {
    requireValue(digestPattern.test(digest ?? "") && Buffer.isBuffer(bytes) && bytes.length > 0 && bytes.length <= (1 << 20) &&
      "sha256:" + createHash("sha256").update(bytes).digest("hex") === digest, "NATIVE_IMAGE_DESCRIPTOR_INVALID");
    try { return JSON.parse(bytes.toString("utf8")); } catch { throw Error("NATIVE_IMAGE_DESCRIPTOR_INVALID"); }
  };
  const index = parse(indexBytes, profile.indexDigest), manifest = parse(manifestBytes, profile.manifestDigest), config = parse(configBytes, profile.configDigest);
  const children = index.manifests?.filter(m => m.platform?.os === "linux" && m.platform.architecture === "amd64" &&
    !m.platform.variant && !m.platform["os.version"]);
  requireValue(index.schemaVersion === 2 && ["application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json"].includes(index.mediaType) &&
    children?.length === 1 && children[0].digest === profile.manifestDigest && children[0].size === manifestBytes.length &&
    manifest.schemaVersion === 2 && ["application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"].includes(manifest.mediaType) &&
    manifest.config?.digest === profile.configDigest && manifest.config.size === configBytes.length &&
    config.os === "linux" && config.architecture === "amd64", "NATIVE_IMAGE_PLATFORM_INVALID");
  return { index: profile.indexDigest, manifest: profile.manifestDigest, config: profile.configDigest, platform: "linux/amd64" };
}

export function prepareNativeDeployment(deployment, profile) {
  requireValue(["coredns", "metrics-server", "traefik"].includes(profile?.name) && deployment?.kind === "Deployment" &&
    deployment.metadata?.namespace === "kube-system" && deployment.metadata.name === profile.name &&
    deployment.metadata.uid && deployment.metadata.resourceVersion && deployment.spec?.replicas === 1 && !deployment.spec.paused,
  "NATIVE_SOURCE_IDENTITY_INVALID");
  const after = structuredClone(deployment.spec), pod = after.template?.spec;
  requireValue(pod?.containers?.length === 1 && !pod.initContainers?.length &&
    !pod.volumes?.some(v => v.hostPath || v.persistentVolumeClaim) && pod.containers[0].name === profile.container &&
    pod.containers[0].image === profile.sourceImage && /^docker\.io\/rancher\/[a-z0-9-]+$/.test(profile.repository ?? "") &&
    digestPattern.test(profile.indexDigest ?? ""), "NATIVE_SOURCE_PROFILE_INVALID");
  const c = pod.containers[0]; c.image = (profile.owner === "helmchart" ? profile.sourceImage : profile.repository) + "@" + profile.indexDigest;
  c.resources = boundedResources(profile.resources, c.resources);
  after.replicas = 2; after.strategy = { type: "RollingUpdate", rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } };
  return { beforeSHA256: fingerprint(deployment.spec), afterSHA256: fingerprint(after), after,
    inverse: reverseSpecPatch(deployment.spec, after), uid: deployment.metadata.uid };
}

export function nativeAddonBinding(profile, addon, fileHash) {
  requireValue(profile.owner === "addon" && Object.hasOwn(nativeSkipPaths, profile.name) &&
    profile.manifest + ".skip" === nativeSkipPaths[profile.name] && addon?.kind === "Addon" &&
    addon.apiVersion === "k3s.cattle.io/v1" && addon.metadata?.namespace === "kube-system" &&
    addon.metadata.name === profile.addon && addon.metadata.uid && !addon.metadata.deletionTimestamp &&
    addon.spec?.source === profile.manifest && /^[a-f0-9]{64}$/.test(fileHash ?? "") && addon.spec.checksum === fileHash,
  "NATIVE_ADDON_BINDING_INVALID");
  return { name: profile.addon, uid: addon.metadata.uid, specSHA256: fingerprint(addon.spec), fileSHA256: fileHash,
    skipPath: nativeSkipPaths[profile.name] };
}

export function traefikHelmConfig(chart, profile, intent, rollback = false) {
  requireValue(profile.name === "traefik" && profile.owner === "helmchart" && chart?.kind === "HelmChart" &&
    chart.apiVersion === "helm.cattle.io/v1" && chart.metadata?.namespace === "kube-system" && chart.metadata.name === "traefik" &&
    chart.metadata.uid && !chart.metadata.deletionTimestamp && /^[a-f0-9]{64}$/.test(intent ?? "") &&
    !chart.spec?.valuesSecrets?.length && !chart.spec?.targetNamespace && !chart.spec?.releaseName &&
    Object.keys(chart.spec.set ?? {}).every(k => k === "global.systemDefaultRegistry"), "NATIVE_TRAEFIK_CHART_INVALID");
  const values = { deployment: { replicas: rollback ? 1 : 2 }, resources: rollback ? {} : boundedResources(profile.resources),
    image: { tag: profile.sourceImage.split(":").at(-1) + (rollback ? "" : "@" + profile.indexDigest) },
    updateStrategy: { type: "RollingUpdate", rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } },
    podDisruptionBudget: { enabled: true, minAvailable: 1 } };
  return { apiVersion: "helm.cattle.io/v1", kind: "HelmChartConfig", metadata: { name: "traefik", namespace: "kube-system",
    labels: { "app.kubernetes.io/managed-by": "kodex-stable-native" }, annotations: { "kodex.dev/stable-native-intent": intent },
    ownerReferences: [{ apiVersion: chart.apiVersion, kind: chart.kind, name: chart.metadata.name, uid: chart.metadata.uid }] },
  spec: { failurePolicy: "abort", valuesContent: JSON.stringify(values) } };
}

export function nativeTraefikTransition(before, desired, profile) {
  const key = r => `${r.apiVersion}/${r.kind}/${r.metadata?.namespace ?? ""}/${r.metadata?.name}`;
  requireValue(Array.isArray(before) && Array.isArray(desired) && before.length > 0 && before.length < 100 &&
    desired.length === before.length + 1 && new Set(before.map(key)).size === before.length &&
    new Set(desired.map(key)).size === desired.length, "NATIVE_TRAEFIK_RENDER_INVALID");
  const old = new Map(before.map(r => [key(r), r])); let deployment, pdb;
  for (const r of desired) {
    const source = old.get(key(r));
    if (!source) {
      requireValue(!pdb && r.kind === "PodDisruptionBudget" && r.apiVersion === "policy/v1" && r.metadata?.name === "traefik" &&
        r.metadata.namespace === "kube-system" && r.spec?.minAvailable === 1 && r.spec.maxUnavailable == null &&
        Object.keys(r.spec).every(k => ["minAvailable", "maxUnavailable", "selector"].includes(k)) &&
        Object.keys(r.spec.selector?.matchLabels ?? {}).length > 0 && !r.spec.selector?.matchExpressions?.length,
      "NATIVE_TRAEFIK_ADDITION_FORBIDDEN"); pdb = r; continue;
    }
    const expected = structuredClone(source);
    if (r.kind === "Deployment" && r.metadata?.name === "traefik" && r.metadata.namespace === "kube-system") {
      requireValue(!deployment, "NATIVE_TRAEFIK_RENDER_INVALID");
      expected.spec = prepareNativeDeployment({ ...source, metadata: { ...source.metadata, uid: "render", resourceVersion: "0" } }, profile).after;
      deployment = r;
    }
    requireValue(fingerprint(expected) === fingerprint(r), "NATIVE_TRAEFIK_UNRELATED_CHANGE_FORBIDDEN");
  }
  requireValue(deployment && pdb && Object.entries(pdb.spec.selector.matchLabels).every(([k, v]) =>
    deployment.spec.template.metadata.labels[k] === v), "NATIVE_TRAEFIK_PDB_SELECTOR_INVALID");
  return { beforeSHA256: fingerprint(before), afterSHA256: fingerprint(desired) };
}
