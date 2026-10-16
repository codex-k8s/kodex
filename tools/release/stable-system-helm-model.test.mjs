import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { systemHelmTransition } from "./stable-system-helm-model.mjs";

const profiles = JSON.parse(await readFile(new URL("../../infra/stable-system/helm-profiles.json", import.meta.url), "utf8")).profiles;
const bounds = { requests: { cpu: "50m", memory: "64Mi" }, limits: { cpu: "500m", memory: "512Mi" } };
function fixture(profile = profiles[0]) {
  const before = profile.deployments.map(name => ({ apiVersion: "apps/v1", kind: "Deployment",
    metadata: { name, namespace: profile.namespace }, spec: { replicas: 1,
      template: { metadata: { labels: { app: name } }, spec: { containers: [{ name: "app", image: "example.invalid/app:v1",
        env: [{ name: "EXAMPLE", value: "PRIVATE_SENTINEL_1810" }], resources: {} }] } } } }));
  if (profile.alertmanager) before.push({ apiVersion: "monitoring.coreos.com/v1", kind: "Alertmanager",
    metadata: { name: profile.alertmanager, namespace: profile.namespace }, spec: { replicas: 1, resources: {},
      storage: { volumeClaimTemplate: { spec: { resources: { requests: { storage: "4Gi" } } } } } } });
  before.push({ apiVersion: "v1", kind: "Secret", metadata: { name: "credential", namespace: profile.namespace },
    data: { password: "PRIVATE_SECRET_SENTINEL_1810" } });
  const after = structuredClone(before);
  for (const a of after) {
    if (a.kind === "Deployment") { a.spec.replicas = 2; a.spec.template.spec.containers[0].resources = structuredClone(bounds); }
    if (a.kind === "Alertmanager") { a.spec.replicas = 2; a.spec.resources = structuredClone(bounds); }
  }
  for (const name of [...profile.deployments, ...(profile.alertmanager ? [profile.alertmanager] : [])])
    after.push({ apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { namespace: profile.namespace, name },
      spec: { minAvailable: 1, selector: { matchLabels: profile.deployments.includes(name) ? { app: name } : { alertmanager: name } } } });
  return { profile, before, after };
}

test("all three profiles preserve unrelated manifests and expose commitments only", () => {
  for (const profile of profiles) {
    const f = fixture(profile), before = structuredClone(f);
    const result = systemHelmTransition(f.profile, f.before, f.after);
    assert.deepEqual(f, before); assert.equal(result.changes.length, (profile.deployments.length + (profile.alertmanager ? 1 : 0)) * 2);
    assert.equal(JSON.stringify(result).includes("PRIVATE_"), false);
  }
});

test("helm render cannot alter keys, certificates, RBAC, images, env or claim templates", () => {
  for (const mutate of [f => { f.after.find(a => a.kind === "Secret").data.password = "changed"; },
    f => { f.after[0].spec.template.spec.containers[0].image = "example.invalid/app:v2"; },
    f => { f.after[0].spec.template.spec.containers[0].env = []; },
    f => { f.after[0].spec.template.spec.serviceAccountName = "other"; },
    f => { f.after.find(a => a.kind === "Alertmanager").spec.storage.volumeClaimTemplate.spec.resources.requests.storage = "8Gi"; }]) {
    const f = fixture(profiles[2]); mutate(f);
    assert.throws(() => systemHelmTransition(f.profile, f.before, f.after), /SYSTEM_HELM_UNRELATED_CHANGE_FORBIDDEN/);
  }
});

test("no resource removal, foreign addition or broad PDB can enter a plan", () => {
  for (const mutate of [f => f.after.splice(0, 1),
    f => f.after.push({ apiVersion: "v1", kind: "Secret", metadata: { name: "new", namespace: f.profile.namespace } }),
    f => { f.after.at(-1).spec.selector.matchLabels = {}; },
    f => { f.after.at(-1).spec.selector.matchLabels = { app: "foreign" }; },
    f => { f.after.at(-1).metadata.namespace = "foreign"; }]) {
    const f = fixture(); mutate(f); assert.throws(() => systemHelmTransition(f.profile, f.before, f.after), /SYSTEM_HELM_/);
  }
});

test("replica counts, bounded resources and exact target set are mandatory", () => {
  for (const mutate of [f => { f.after[0].spec.replicas = 3; },
    f => { f.after[0].spec.template.spec.containers[0].resources.limits = {}; },
    f => { f.before[0].metadata.name = "foreign"; f.after[0].metadata.name = "foreign"; },
    f => { f.after[0].spec.template.spec.containers.push({ name: "foreign" }); }]) {
    const f = fixture(); mutate(f); assert.throws(() => systemHelmTransition(f.profile, f.before, f.after));
  }
});

test("Prometheus operator and data replicas cannot change in monitoring render", () => {
  for (const [kind, name] of [["Deployment", "kodex-monitoring-kube-prom-operator"], ["Prometheus", "kodex-monitoring-kube-prom-prometheus"]]) {
    const f = fixture(profiles[2]), extra = { apiVersion: "apps/v1", kind,
      metadata: { name, namespace: "observability" }, spec: { replicas: 1 } };
    f.before.push(extra); f.after.push({ ...structuredClone(extra), spec: { replicas: 2 } });
    assert.throws(() => systemHelmTransition(f.profile, f.before, f.after), /SYSTEM_HELM_UNRELATED_CHANGE_FORBIDDEN/);
  }
});

test("only the exact Alertmanager PDB permits the pinned chart eviction policy", () => {
  const f = fixture(profiles[2]);
  f.after.at(-1).spec.unhealthyPodEvictionPolicy = "AlwaysAllow";
  assert.ok(systemHelmTransition(f.profile, f.before, f.after));
  f.after.at(-2).spec.unhealthyPodEvictionPolicy = "AlwaysAllow";
  assert.throws(() => systemHelmTransition(f.profile, f.before, f.after), /SYSTEM_HELM_RESOURCE_ADDITION_FORBIDDEN/);
  delete f.after.at(-2).spec.unhealthyPodEvictionPolicy;
  f.after.at(-1).spec.unhealthyPodEvictionPolicy = "Unknown";
  assert.throws(() => systemHelmTransition(f.profile, f.before, f.after), /SYSTEM_HELM_RESOURCE_ADDITION_FORBIDDEN/);
});
