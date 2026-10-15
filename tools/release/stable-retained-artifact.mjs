import { fingerprint } from "./stable-inventory.mjs";

const consumers = {
  "image-admission": new Set(["image-admission-controller", "kodex-image-registry-pull", "kodex-image-registry-push", "kodex-image-registry-evidence"]),
  "image-admission-tools": new Set(["kodex-image-registry-pull", "kodex-image-registry-promotion", "kodex-image-registry-evidence"]),
};
function requireValue(value, code) { if (!value) throw Error(code); }
function componentDigest(image, component) {
  return typeof image === "string" && new RegExp(`^[a-z0-9][a-z0-9.:/_-]*/kodex/${component}@sha256:[a-f0-9]{64}$`).test(image) ? image.split("@")[1] : null;
}

// Подтверждается сохранение уже работающего byte artifact, не новая сборка,
// SLSA attestation или domain admission. Все locator/owner выводятся из API.
export function retainedServingEvidence(component, objects) {
  requireValue(Object.hasOwn(consumers, component) && Array.isArray(objects) && objects.length <= 512,
    "RETAINED_COMPONENT_INVALID");
  const deployments = objects.filter(r => r.kind === "Deployment" && r.metadata?.namespace === "kodex-system" &&
    r.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" && consumers[component].has(r.metadata.name));
  requireValue(deployments.length > 0 && new Set(deployments.map(d => d.metadata.uid)).size === deployments.length &&
    deployments.every(d => typeof d.metadata.uid === "string" && d.metadata.uid && d.spec?.template?.spec), "RETAINED_OWNER_INVALID");
  const ownerByUID = new Map(deployments.map(d => [d.metadata.uid, d]));
  const sets = objects.filter(r => r.kind === "ReplicaSet" && r.metadata?.namespace === "kodex-system" && r.metadata.uid);
  const ownerForSet = new Map();
  for (const set of sets) {
    const owners = (set.metadata.ownerReferences ?? []).filter(o => o.kind === "Deployment" && o.controller === true && ownerByUID.has(o.uid));
    if (owners.length === 1) ownerForSet.set(set.metadata.uid, owners[0].uid);
  }
  const candidates = [], digests = new Set(), ownerDigests = new Map();
  for (const d of deployments) {
    for (const c of [...(d.spec.template.spec.containers ?? []), ...(d.spec.template.spec.initContainers ?? [])]) {
      const digest = componentDigest(c.image, component);
      if (digest) { digests.add(digest); ownerDigests.set(`${d.metadata.uid}/${c.name}`, digest); }
    }
  }
  requireValue(digests.size === 1, "RETAINED_DIGEST_AMBIGUOUS");
  const digest = [...digests][0];
  for (const pod of objects.filter(r => r.kind === "Pod" && r.metadata?.namespace === "kodex-system" &&
    !r.metadata.deletionTimestamp && r.status?.conditions?.some(c => c.type === "Ready" && c.status === "True"))) {
    const owners = (pod.metadata.ownerReferences ?? []).filter(o => o.kind === "ReplicaSet" && o.controller === true && ownerForSet.has(o.uid));
    if (owners.length !== 1) continue;
    const deploymentUID = ownerForSet.get(owners[0].uid);
    for (const c of [...(pod.spec?.containers ?? []), ...(pod.spec?.initContainers ?? [])]) {
      if (componentDigest(c.image, component) !== digest || ownerDigests.get(`${deploymentUID}/${c.name}`) !== digest) continue;
      const status = [...(pod.status.containerStatuses ?? []), ...(pod.status.initContainerStatuses ?? [])].find(s => s.name === c.name);
      requireValue(pod.metadata.uid && status?.ready === true && typeof status.imageID === "string" &&
        (status.imageID === digest || status.imageID.endsWith("@" + digest)), "RETAINED_SERVING_DIGEST_MISMATCH");
      candidates.push({ deploymentUID, podUID: pod.metadata.uid, container: c.name, imageID: status.imageID });
    }
  }
  requireValue(candidates.length > 0 && new Set(candidates.map(c => `${c.podUID}/${c.container}`)).size === candidates.length,
    "RETAINED_SERVING_REQUIRED");
  const active = new Set(candidates.map(c => c.deploymentUID));
  return { component, digest, deployments: deployments.filter(d => active.has(d.metadata.uid)).map(d => ({ name: d.metadata.name,
    uid: d.metadata.uid, specSHA256: fingerprint(d.spec) })).sort((a, b) => a.name.localeCompare(b.name, "en")),
  serving: candidates.sort((a, b) => `${a.podUID}/${a.container}`.localeCompare(`${b.podUID}/${b.container}`, "en")) };
}

export function retainedPublicationImage(proof) {
  requireValue(proof && Object.hasOwn(consumers, proof.component) && /^sha256:[a-f0-9]{64}$/.test(proof.digest ?? "") &&
    !proof.digest.endsWith("0".repeat(64)) && Array.isArray(proof.deployments) && proof.deployments.length > 0 &&
    Array.isArray(proof.serving) && proof.serving.length > 0, "RETAINED_PROOF_INVALID");
  return { component: proof.component, digest: proof.digest, tag: "retained-" + proof.digest.slice(7) };
}
