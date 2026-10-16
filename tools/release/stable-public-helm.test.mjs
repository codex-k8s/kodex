import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { publicHelmExpectedObjects, publicHelmScope } from "./stable-public-helm.mjs";
import { publicHelmProfiles } from "./stable-public-helm-model.mjs";
import { matchSystemObjects, readSystemObjects, systemObjectKey, systemObjectState, systemServing } from "./stable-system-helm-state.mjs";

const profile = JSON.parse(readFileSync(new URL("../../infra/public-ingress/identity-profile.json", import.meta.url)));
const identify = r => ({ ...r, metadata: { ...r.metadata, uid: r.metadata.name + "-uid", resourceVersion: "1" } });

test("closed public scope uses its own exact chart locks and rejects endpoint/service drift", async () => {
  const cm = identify({ apiVersion: "v1", kind: "ConfigMap", metadata: { name: profile.endpointsName, namespace: "kodex-system" }, data: {
    publicOrigin: "https://control.example.test", oidcIssuer: "https://sso.example.test/realms/kodex",
    oidcJwksUrl: "https://sso.example.test/realms/kodex/protocol/openid-connect/certs",
    oidcConnectAddress: profile.oidcConnectAddress, oidcTlsServerName: "sso.example.test" } });
  const service = identify({ apiVersion: "v1", kind: "Service", metadata: { name: "sso", namespace: "identity" },
    spec: { type: "ClusterIP", clusterIP: "10.43.0.10", ports: [{ name: "https", port: 443 }], selector: { "app.kubernetes.io/name": "sso" } } });
  const kube = async args => JSON.stringify(args.includes("service") ? service : cm);
  const scope = await publicHelmScope({ kube });
  assert.deepEqual(scope.locks.map(l => l.name), ["oauth2-proxy", "kube-prometheus-stack"]);
  assert.ok(scope.profiles.every(p => scope.locks.some(l => l.name === p.chartName)));
  const inherited = await publicHelmScope({ kube, plan: { environment: scope.environment } });
  assert.deepEqual(inherited.environment, scope.environment);
  cm.data = structuredClone(scope.environment.endpoints.after); await scope.assertBoundary();
  cm.data.publicOrigin = "https://foreign.example.test"; await assert.rejects(scope.assertBoundary(), /BOUNDARY_CHANGED/);
  cm.data = structuredClone(scope.environment.endpoints.before); service.metadata.uid = "foreign";
  await assert.rejects(scope.assertBoundary(), /BOUNDARY_CHANGED/);
});

test("public live Grafana commitment reads only its exact nonsecret CM and detects content drift", async () => {
  const p = publicHelmProfiles[2], cm = identify({ apiVersion: "v1", kind: "ConfigMap", metadata: { name: p.grafana, namespace: p.namespace },
    data: { "grafana.ini": "PRIVATE_CONFIG_SENTINEL\n" } }), secret = identify({ apiVersion: "v1", kind: "Secret",
    metadata: { name: "credential", namespace: p.namespace }, data: { token: "PRIVATE_SECRET_SENTINEL" } });
  const live = await readSystemObjects(p, [cm, secret], async (args, input) => {
    const items = JSON.parse(input).items;
    if (args.at(-1) === "json") { assert.deepEqual(items.map(r => r.kind), ["ConfigMap"]); return JSON.stringify(cm); }
    assert.deepEqual(items.map(r => r.kind), ["Secret"]);
    return `v1|Secret|${p.namespace}|credential|credential-uid|1\n`;
  });
  const next = structuredClone(cm); next.data["grafana.ini"] = "CHANGED\n";
  const before = [cm, secret], after = [next, secret], expected = publicHelmExpectedObjects(p, before, after, live);
  assert.equal(matchSystemObjects(expected, live, ["before"]).length, 2);
  live.set(systemObjectKey(cm), next); assert.equal(matchSystemObjects(expected, live, ["after"]).length, 2);
  next.data.extra = "UNEXPECTED"; assert.throws(() => matchSystemObjects(expected, live, ["after"]), /OBJECT_DRIFT/);
  assert.equal(JSON.stringify(expected).includes("PRIVATE_"), false);
  assert.ok(systemObjectState(cm).contentSHA256); assert.ok(systemObjectState(secret).resourceVersion);
  const invalid = structuredClone(before); invalid[0].data["grafana.ini"] = "DRIFT";
  assert.throws(() => publicHelmExpectedObjects(p, invalid, after, new Map(before.map(r => [systemObjectKey(r), r]))), /LIVE_CONFIG_DRIFT/);
});

test("Grafana serving remains one PVC-backed Helm-owned instance even when shared engine expects two", async () => {
  const p = publicHelmProfiles[2], d = identify({ apiVersion: "apps/v1", kind: "StatefulSet", metadata: { name: p.grafana, namespace: p.namespace,
    generation: 1, annotations: { "meta.helm.sh/release-name": p.release, "meta.helm.sh/release-namespace": p.namespace } },
  spec: { replicas: 1, selector: { matchLabels: { app: "grafana" } }, volumeClaimTemplates: [{ name: "data" }],
    template: { spec: { containers: [{ name: "grafana", image: "example.invalid/grafana:v1" }] } } },
  status: { observedGeneration: 1, replicas: 1, updatedReplicas: 1, readyReplicas: 1, currentRevision: "v1", updateRevision: "v1" } });
  const pod = { kind: "Pod", metadata: { uid: "grafana-pod", ownerReferences: [{ kind: "StatefulSet", uid: d.metadata.uid, controller: true }] },
    spec: structuredClone(d.spec.template.spec), status: { conditions: [{ type: "Ready", status: "True" }],
      containerStatuses: [{ name: "grafana", imageID: "containerd://sha256:" + "a".repeat(64), ready: true, state: { running: {} } }] } };
  const kube = async args => JSON.stringify(args.includes("pods") ? { items: [pod] } : d);
  const proof = await systemServing(p, kube, undefined, 2); assert.equal(proof.length, 1);
  d.spec.volumeClaimTemplates = []; await assert.rejects(systemServing(p, kube, proof, 2), /SERVING_IMAGE_CHANGED/);
  d.spec.replicas = 2; await assert.rejects(systemServing(p, kube, undefined, 2), /SERVING_INCOMPLETE/);
  d.spec.replicas = 1; d.metadata.annotations["meta.helm.sh/release-name"] = "foreign";
  await assert.rejects(systemServing(p, kube), /GRAFANA_OWNER_INVALID/);
});

test("public live ingress preserves defaulted fields and refuses source spec drift", () => {
  const p = publicHelmProfiles[0], b = identify({ apiVersion: "networking.k8s.io/v1", kind: "Ingress",
    metadata: { name: p.release, namespace: p.namespace }, spec: { ingressClassName: "traefik", rules: [{ host: "example.test" }] } });
  const after = structuredClone(b); after.spec.rules[0].host = profile.platformHost;
  const expected = publicHelmExpectedObjects(p, [b], [after], new Map([[systemObjectKey(b), b]]));
  assert.notEqual(expected[0].before, expected[0].after); assert.equal(expected[0].recovery, expected[0].before);
  b.spec.ingressClassName = "foreign";
  assert.throws(() => publicHelmExpectedObjects(p, [{ ...b, spec: { ...b.spec, ingressClassName: "traefik" } }], [after],
    new Map([[systemObjectKey(b), b]])), /LIVE_INGRESS_DRIFT/);
});
