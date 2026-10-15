import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";
import { preparePlatformInfra, platformInfraProfiles, identityCachePeers, platformInfraMetadata } from "./stable-platform-infra-model.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { fingerprint } from "./stable-inventory.mjs";

const root = new URL("../../", import.meta.url).pathname, image = "registry.local.kodex/kodex/registry-pull-authorizer@sha256:" + "b".repeat(64);
const parse = (file, expression) => JSON.parse(execFileSync("yq", ["eval-all", "-N", "-o=json", "-I=0", expression, root + file], { encoding: "utf8" }));
function source(p) {
  const file = p.file ?? (p.name === "sso" ? "infra/identity/keycloak.yaml" : "deploy/k8s/base/image-supply-chain/registry.yaml");
  const d = parse(file, `select(.kind == "Deployment" and .metadata.name == "${p.name}")`);
  Object.assign(d.metadata, { namespace: p.namespace, uid: "uid-" + p.name, resourceVersion: "1", annotations: { "deployment.kubernetes.io/revision": "1", keep: "UNCHANGED" } });
  d.spec.replicas = p.count;
  for (const c of d.spec.template.spec.containers) c.image = c.image.replace(/0{64}/g, "a".repeat(64));
  const app = d.spec.template.spec.containers.find(c => c.name === p.container);
  app.env ??= []; app.env.push({ name: "UNRELATED_PRIVATE", value: "PRIVATE_SENTINEL" });
  if (p.name === "sso") {
    app.env = app.env.filter(e => !["KC_CACHE_STACK", "KC_CACHE_EMBEDDED_MTLS_ENABLED"].includes(e.name));
    app.env.find(e => e.name === "KC_CACHE").value = "local";
    app.ports = app.ports.filter(p => ![7800, 57800].includes(p.containerPort));
  } else if (p.name === "kodex-image-registry-pull") {
    app.image = image.replace("b".repeat(64), "a".repeat(64));
    d.spec.template.spec.nodeSelector = { "kubernetes.io/hostname": "node-fixture" };
  }
  return d;
}
const resources = { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "1", memory: "1Gi" } };
test("closed infrastructure transition preserves images credentials storage and has an exact inverse", () => {
  for (const p of platformInfraProfiles) {
    const d = source(p), before = structuredClone(d), result = preparePlatformInfra(d, p, { image, resources, intent: "f".repeat(32) });
    assert.deepEqual(d, before); assert.equal(result.spec.replicas, 2);
    assert.deepEqual(applyReverseSpec(result.spec, result.inverse), before.spec);
    assert.equal(JSON.stringify(result.inverse).includes("PRIVATE_SENTINEL"), false);
    for (const c of before.spec.template.spec.containers) {
      const after = result.spec.template.spec.containers.find(a => a.name === c.name);
      assert.equal(after.env.find(e => e.name === "UNRELATED_PRIVATE")?.value,
        c.env?.find(e => e.name === "UNRELATED_PRIVATE")?.value);
      if (p.name !== "kodex-image-registry-pull" || c.name !== "pull-authorizer") assert.equal(after.image, c.image);
    }
    assert.deepEqual(result.spec.template.spec.volumes, before.spec.template.spec.volumes);
  }
});
test("unknown targets host source mounts and unbound registry storage never acquire a transition", () => {
  const p = platformInfraProfiles[1], d = source(p);
  assert.throws(() => preparePlatformInfra(d, { ...p, name: "other" }, { resources, intent: "f".repeat(32) }));
  d.spec.template.spec.volumes.push({ name: "foreign", hostPath: { path: "/fixture" } });
  assert.throws(() => preparePlatformInfra(d, p, { resources, intent: "f".repeat(32) }), /PROFILE_INVALID/);
  const rp = platformInfraProfiles[0], rd = source(rp); delete rd.spec.template.spec.nodeSelector;
  assert.throws(() => preparePlatformInfra(rd, rp, { image, intent: "f".repeat(32) }), /HELPER_IMAGE_INVALID/);
});
test("Keycloak peers are exact same-namespace pod selectors with mTLS and reversible policy changes", () => {
  const p = platformInfraProfiles[3], d = source(p), result = preparePlatformInfra(d, p, { intent: "e".repeat(32) });
  const env = result.spec.template.spec.containers[0].env;
  for (const [name, value] of Object.entries({ KC_CACHE: "ispn", KC_CACHE_STACK: "jdbc-ping", KC_CACHE_EMBEDDED_MTLS_ENABLED: "true" }))
    assert.equal(env.find(e => e.name === name).value, value);
  const policy = parse("infra/identity/networkpolicy.yaml", 'select(.kind == "NetworkPolicy" and .metadata.name == "sso-exact-paths")');
  Object.assign(policy.metadata, { namespace: "identity", uid: "policy", resourceVersion: "1" });
  policy.spec.ingress.shift(); policy.spec.egress.shift();
  const next = identityCachePeers(policy);
  assert.deepEqual(next.spec.ingress[0].ports.map(p => p.port), [7800, 57800]);
  assert.equal(next.spec.ingress[0].from[0].namespaceSelector, undefined);
  assert.deepEqual(applyReverseSpec(next.spec, next.inverse), policy.spec);
  assert.throws(() => identityCachePeers({ ...policy, spec: next.spec }), /PEER_COLLISION/);
});
test("only the deployment-controller revision annotation is excluded from metadata guard", () => {
  const d = source(platformInfraProfiles[1]), hash = platformInfraMetadata(d);
  d.metadata.annotations["deployment.kubernetes.io/revision"] = "2"; assert.equal(platformInfraMetadata(d), hash);
  d.metadata.annotations.keep = "CHANGED"; assert.notEqual(platformInfraMetadata(d), hash);
});

// Проекции Secret/ConfigMap читаются по metadata. Даже fixture не должен
// превращать plan в хранилище values или полный PodSpec.
test("release driver has no secret data read or generic user-selected deployment path", () => {
  const text = readFileSync(new URL("./stable-platform-infra.mjs", import.meta.url), "utf8");
  assert.ok(!text.includes(".data[") && !text.includes("--target"));
  assert.match(text, /PLATFORM_INFRA_EXACT_APPROVAL_REQUIRED/);
  assert.match(text, /PLATFORM_INFRA_RECOVERY_INTENT_MISSING/);
  assert.equal(fingerprint(platformInfraProfiles).length, 64);
});
