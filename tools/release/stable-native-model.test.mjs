import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { nativeAddonBinding, nativeSkipPaths, nativeTraefikTransition, prepareNativeDeployment, traefikHelmConfig, verifyNativeImage } from "./stable-native-model.mjs";
const profiles = JSON.parse(await readFile(new URL("../../infra/stable-system/native-profiles.json", import.meta.url), "utf8")).profiles;
const fixture = profile => ({ apiVersion: "apps/v1", kind: "Deployment", metadata: { name: profile.name, namespace: "kube-system", uid: "uid", resourceVersion: "1" },
  spec: { replicas: 1, template: { metadata: { labels: { app: profile.name } }, spec: { containers: [{ name: profile.container, image: profile.sourceImage,
    resources: {}, env: [{ name: "EXAMPLE", value: "PRIVATE_NATIVE_SENTINEL" }] }] } } } });

test("native cutover changes only replica/strategy/resources and verified digest, inverse preserves source", () => {
  for (const p of profiles) {
    const d = fixture(p), original = structuredClone(d), transition = prepareNativeDeployment(d, p);
    assert.equal(transition.after.replicas, 2); assert.equal(transition.after.template.spec.containers[0].image.endsWith("@" + p.indexDigest), true);
    assert.deepEqual(transition.after.template.spec.containers[0].env, d.spec.template.spec.containers[0].env);
    assert.deepEqual(applyReverseSpec(transition.after, transition.inverse), d.spec); assert.deepEqual(d, original);
    assert.equal(JSON.stringify(transition.inverse).includes("PRIVATE_NATIVE_SENTINEL"), false);
  }
});

test("unknown native sources, paused deployments, changed images and shared storage fail closed", () => {
  for (const mutate of [d => { d.metadata.namespace = "foreign"; }, d => { d.spec.paused = true; }, d => { d.spec.replicas = 2; },
    d => { d.spec.template.spec.containers[0].image = "example.invalid/foreign:v1"; },
    d => { d.spec.template.spec.volumes = [{ name: "data", persistentVolumeClaim: { claimName: "data" } }]; }]) {
    const p = profiles[0], d = fixture(p); mutate(d); assert.throws(() => prepareNativeDeployment(d, p), /NATIVE_/);
  }
});

test("addon binds only two exact paths, original UID and actual source bytes", () => {
  for (const p of profiles.filter(p => p.owner === "addon")) {
    const hash = "a".repeat(64), addon = { apiVersion: "k3s.cattle.io/v1", kind: "Addon", metadata: { namespace: "kube-system", name: p.addon, uid: "addon-uid" },
      spec: { source: p.manifest, checksum: hash } };
    assert.equal(nativeAddonBinding(p, addon, hash).skipPath, nativeSkipPaths[p.name]);
    assert.throws(() => nativeAddonBinding(p, addon, "b".repeat(64)), /NATIVE_ADDON_BINDING_INVALID/);
    assert.throws(() => nativeAddonBinding({ ...p, manifest: "/foreign" }, addon, hash), /NATIVE_ADDON_BINDING_INVALID/);
  }
});

test("image index proof rejects incorrect bytes, platform and descriptor lineage", () => {
  const hash = bytes => "sha256:" + createHash("sha256").update(bytes).digest("hex"), config = Buffer.from(JSON.stringify({ os: "linux", architecture: "amd64" }));
  const manifest = Buffer.from(JSON.stringify({ schemaVersion: 2, mediaType: "application/vnd.oci.image.manifest.v1+json", config: { digest: hash(config), size: config.length } }));
  const makeIndex = platform => Buffer.from(JSON.stringify({ schemaVersion: 2, mediaType: "application/vnd.oci.image.index.v1+json", manifests: [{ digest: hash(manifest), size: manifest.length, platform }] }));
  const index = makeIndex({ os: "linux", architecture: "amd64" }), p = { indexDigest: hash(index), manifestDigest: hash(manifest), configDigest: hash(config) };
  assert.equal(verifyNativeImage(p, index, manifest, config).platform, "linux/amd64");
  assert.throws(() => verifyNativeImage(p, Buffer.from("changed"), manifest, config), /NATIVE_IMAGE_DESCRIPTOR_INVALID/);
  const foreign = makeIndex({ os: "linux", architecture: "arm64" });
  assert.throws(() => verifyNativeImage({ ...p, indexDigest: hash(foreign) }, foreign, manifest, config), /NATIVE_IMAGE_PLATFORM_INVALID/);
});

test("Traefik HCC keeps native release, exact HC UID and no extra ingress or certificates", () => {
  const p = profiles.find(p => p.name === "traefik"), chart = { apiVersion: "helm.cattle.io/v1", kind: "HelmChart",
    metadata: { name: "traefik", namespace: "kube-system", uid: "chart-uid" }, spec: { set: { "global.systemDefaultRegistry": "" } } };
  const result = traefikHelmConfig(chart, p, "a".repeat(64)), values = JSON.parse(result.spec.valuesContent);
  assert.equal(result.metadata.ownerReferences[0].uid, "chart-uid"); assert.equal(values.deployment.replicas, 2);
  assert.equal(values.image.tag.endsWith("@" + p.indexDigest), true); assert.equal(values.podDisruptionBudget.minAvailable, 1);
  assert.deepEqual(Object.keys(values).sort(), ["deployment", "image", "podDisruptionBudget", "resources", "updateStrategy"]);
  chart.spec.set["deployment.replicas"] = "1";
  assert.throws(() => traefikHelmConfig(chart, p, "a".repeat(64)), /NATIVE_TRAEFIK_CHART_INVALID/);
});

test("Traefik full render rejects changes to services, TLS, RBAC and wrong PDB selector", () => {
  const p = profiles.find(p => p.name === "traefik"), d = fixture(p);
  const source = [d, { apiVersion: "v1", kind: "Service", metadata: { name: "traefik", namespace: "kube-system" }, spec: { type: "LoadBalancer" } }];
  const desired = [structuredClone(d), structuredClone(source[1]), { apiVersion: "policy/v1", kind: "PodDisruptionBudget",
    metadata: { name: "traefik", namespace: "kube-system" }, spec: { minAvailable: 1, selector: { matchLabels: { app: "traefik" } } } }];
  desired[0].spec = prepareNativeDeployment(d, p).after;
  assert.ok(nativeTraefikTransition(source, desired, p));
  desired[1].spec.type = "NodePort"; assert.throws(() => nativeTraefikTransition(source, desired, p), /NATIVE_TRAEFIK_UNRELATED_CHANGE_FORBIDDEN/);
  desired[1].spec.type = "LoadBalancer"; desired[2].spec.selector.matchLabels.app = "foreign";
  assert.throws(() => nativeTraefikTransition(source, desired, p), /NATIVE_TRAEFIK_PDB_SELECTOR_INVALID/);
});
