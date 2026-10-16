import { fingerprint } from "./stable-inventory.mjs";
import { boundedResources } from "./stable-resources.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";

const equal = (a, b) => fingerprint(a) === fingerprint(b);
function requireValue(v, code) { if (!v) throw Error(code); }
export function platformInfraMetadata(d) {
  const annotations = { ...d.metadata.annotations }; delete annotations["deployment.kubernetes.io/revision"];
  return fingerprint({ labels: d.metadata.labels ?? {}, annotations });
}
export const platformInfraProfiles = [
  { namespace: "kodex-system", name: "kodex-image-registry-pull", count: 2, container: "pull-authorizer" },
  { namespace: "kodex-system", name: "kodex-buildkit", count: 1, container: "buildkitd",
    file: "deploy/k8s/base/image-supply-chain/buildkit.yaml" },
  { namespace: "kodex-system", name: "role-image-builder", count: 1, container: "role-image-builder",
    file: "deploy/k8s/base/role-image-builder/deployment.yaml" },
  { namespace: "identity", name: "sso", count: 1, container: "keycloak" },
];

export function preparePlatformInfra(d, profile, { image, resources, intent } = {}) {
  requireValue(platformInfraProfiles.some(p => equal(p, profile)) && d?.kind === "Deployment" &&
    d.metadata?.namespace === profile.namespace && d.metadata.name === profile.name && d.metadata.uid &&
    d.metadata.resourceVersion && !d.metadata.deletionTimestamp && !d.spec?.paused && d.spec.replicas === profile.count,
  "PLATFORM_INFRA_SOURCE_INVALID");
  const spec = structuredClone(d.spec), pod = spec.template?.spec, app = pod?.containers?.find(c => c.name === profile.container);
  requireValue(app && (pod.containers ?? []).every(c => /@sha256:[a-f0-9]{64}$/.test(c.image)) &&
    !(pod.volumes ?? []).some(v => v.hostPath || v.persistentVolumeClaim && profile.name !== "kodex-image-registry-pull") && /^[a-f0-9]{32}$/.test(intent ?? "") &&
    !Object.hasOwn(spec.template.metadata.annotations ?? {}, "kodex.dev/stable-platform-infra"), "PLATFORM_INFRA_PROFILE_INVALID");
  const beforeImage = app.image;
  if (profile.name === "kodex-image-registry-pull") {
    // Общий registry PVC уже проверен отдельным workflow; здесь меняется
    // только standalone helper, не storage, credentials или writable role.
    const claims = pod.volumes.filter(v => v.persistentVolumeClaim);
    requireValue(claims.length === 1 && claims[0].persistentVolumeClaim.claimName === "kodex-image-registry-promoted" &&
      claims[0].persistentVolumeClaim.readOnly === true && pod.nodeSelector?.["kubernetes.io/hostname"] &&
      /^registry\.local\.kodex\/kodex\/registry-pull-authorizer@sha256:[a-f0-9]{64}$/.test(image ?? "") &&
      /^registry\.local\.kodex\/kodex\/registry-pull-authorizer@sha256:[a-f0-9]{64}$/.test(beforeImage) && image !== beforeImage,
    "PLATFORM_INFRA_HELPER_IMAGE_INVALID"); app.image = image;
  } else {
    requireValue(pod.containers.length === 1, "PLATFORM_INFRA_CONTAINER_INVALID");
    spec.replicas = 2; spec.strategy = { type: "RollingUpdate", rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } };
    if (profile.name === "sso") {
      const env = app.env ?? [], cache = env.filter(e => e.name === "KC_CACHE");
      requireValue(cache.length === 1 && cache[0].value === "local" && !cache[0].valueFrom &&
        !env.some(e => ["KC_CACHE_STACK", "KC_CACHE_EMBEDDED_MTLS_ENABLED"].includes(e.name)) &&
        env.some(e => e.name === "KC_DB" && e.value === "postgres") && app.args?.join(" ") === "start",
      "PLATFORM_INFRA_IDENTITY_CACHE_INVALID");
      cache[0].value = "ispn"; env.push({ name: "KC_CACHE_STACK", value: "jdbc-ping" },
        { name: "KC_CACHE_EMBEDDED_MTLS_ENABLED", value: "true" });
      requireValue(!(app.ports ?? []).some(p => [7800, 57800].includes(p.containerPort) || ["cache", "cache-failure"].includes(p.name)),
        "PLATFORM_INFRA_IDENTITY_PORT_COLLISION");
      app.ports.push({ name: "cache", containerPort: 7800, protocol: "TCP" }, { name: "cache-failure", containerPort: 57800, protocol: "TCP" });
    } else {
      const scratch = pod.volumes?.filter(v => v.name === (profile.name === "kodex-buildkit" ? "state" : "work"));
      requireValue(scratch?.length === 1 && scratch[0].emptyDir &&
        app.command?.join(" ") !== "/workspace/tools/dev/run-go-hot-reload.sh", "PLATFORM_INFRA_SCRATCH_INVALID");
      app.resources = boundedResources(resources, app.resources);
    }
  }
  spec.template.metadata.annotations = { ...spec.template.metadata.annotations, "kodex.dev/stable-platform-infra": intent };
  return { uid: d.metadata.uid, beforeImage, beforeSHA256: fingerprint(d.spec), afterSHA256: fingerprint(spec),
    metadataSHA256: platformInfraMetadata(d),
    inverse: reverseSpecPatch(d.spec, spec), spec };
}

export function identityCachePeers(policy) {
  requireValue(policy?.kind === "NetworkPolicy" && policy.metadata?.namespace === "identity" &&
    policy.metadata.name === "sso-exact-paths" && policy.metadata.uid && policy.metadata.resourceVersion &&
    !policy.metadata.deletionTimestamp && equal(policy.spec?.podSelector?.matchLabels,
      { "app.kubernetes.io/name": "sso", "app.kubernetes.io/component": "identity-provider" }) &&
    equal(policy.spec.policyTypes, ["Ingress", "Egress"]), "PLATFORM_INFRA_IDENTITY_POLICY_INVALID");
  const spec = structuredClone(policy.spec), peer = { podSelector: { matchLabels: structuredClone(spec.podSelector.matchLabels) } };
  for (const [key, direction] of [["ingress", "from"], ["egress", "to"]]) {
    requireValue(Array.isArray(spec[key]) && !spec[key].some(r => r[direction]?.some(p => equal(p, peer))),
      "PLATFORM_INFRA_IDENTITY_PEER_COLLISION");
    spec[key].unshift({ [direction]: [peer], ports: [{ protocol: "TCP", port: 7800 }, { protocol: "TCP", port: 57800 }] });
  }
  return { uid: policy.metadata.uid, beforeSHA256: fingerprint(policy.spec), afterSHA256: fingerprint(spec),
    inverse: reverseSpecPatch(policy.spec, spec), spec };
}
