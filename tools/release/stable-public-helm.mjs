#!/usr/bin/env node
import { readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { fingerprint } from "./stable-inventory.mjs";
import { stableChartLocks, verifyStableChart } from "./fetch-stable-charts.mjs";
import { planSystemHelm } from "./stable-system-helm-plan.mjs";
import { releaseSystemHelm } from "./stable-system-helm-release.mjs";
import { systemObjectKey, systemObjectState } from "./stable-system-helm-state.mjs";
import { publicEndpoints, publicIdentityProfile, publicOAuthProxy } from "./stable-public-identity-model.mjs";
import { publicHelmProfiles, publicHelmTransition, publicHelmValues } from "./stable-public-helm-model.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(v, code) { if (!v) throw Error(code); }
const same = (a, b) => fingerprint(a) === fingerprint(b);

export function publicHelmExpectedObjects(p, before, after, live, endpoints, profile, connectIP) {
  const source = new Map(before.map(r => [systemObjectKey(r), r]));
  return after.map(r => {
    const key = systemObjectKey(r), b = source.get(key), current = live.get(key);
    requireValue(b && current, "PUBLIC_HELM_LIVE_OBJECT_MISSING");
    const previous = systemObjectState(current), expected = structuredClone(current);
    if (r.kind === "Deployment" && p.deployments.includes(r.metadata.name)) {
      requireValue(same(current.spec.template.spec.containers.map(c => c.args), b.spec.template.spec.containers.map(c => c.args)) &&
        same(current.spec.template.spec.hostAliases, b.spec.template.spec.hostAliases), "PUBLIC_HELM_LIVE_PROXY_DRIFT");
      expected.spec = publicOAuthProxy(current, endpoints, profile, connectIP).spec;
    } else if (p.release === "oauth2-control-center" && r.kind === "Ingress" && r.metadata.name === p.release) {
      requireValue(same(current.spec, b.spec), "PUBLIC_HELM_LIVE_INGRESS_DRIFT"); expected.spec = structuredClone(r.spec);
    } else if (p.grafana && r.metadata.name === p.grafana && r.kind === "ConfigMap") {
      requireValue(same(current.data, b.data) && !current.binaryData && !current.immutable, "PUBLIC_HELM_LIVE_CONFIG_DRIFT");
      expected.data = structuredClone(r.data);
    } else if (p.grafana && r.metadata.name === p.grafana && r.kind === "StatefulSet") {
      requireValue(current.spec.replicas === 1 && current.spec.template.metadata.annotations?.["checksum/config"] ===
        b.spec.template.metadata.annotations["checksum/config"], "PUBLIC_HELM_LIVE_GRAFANA_DRIFT");
      expected.spec.template.metadata.annotations["checksum/config"] = r.spec.template.metadata.annotations["checksum/config"];
    }
    const next = systemObjectState(expected);
    return { ...previous, before: previous.contentSHA256 ?? null, after: next.contentSHA256 ?? null,
      recovery: previous.contentSHA256 ?? null };
  }).sort((a, b) => [a.apiVersion, a.kind, a.namespace, a.name].join("/").localeCompare([b.apiVersion, b.kind, b.namespace, b.name].join("/"), "en"));
}

export async function publicHelmScope({ kube, plan, environment: inherited }) {
  const profile = JSON.parse(await readFile(root + "infra/public-ingress/identity-profile.json", "utf8")); publicIdentityProfile(profile);
  const get = async (ns, kind, name) => JSON.parse(await kube(["-n", ns, "get", kind, name, "-o", "json"]));
  const cm = await get("kodex-system", "configmap", profile.endpointsName), service = await get("identity", "service", "sso");
  const environment = plan?.environment ?? inherited ?? { endpoints: publicEndpoints(cm, profile),
    serviceUID: service.metadata.uid, serviceSHA256: fingerprint(service.spec), profileSHA256: fingerprint(profile) };
  requireValue(environment.profileSHA256 === fingerprint(profile), "PUBLIC_HELM_PROFILE_CHANGED");
  const endpoints = publicEndpoints({ ...cm, metadata: { ...cm.metadata, uid: environment.endpoints.uid,
    resourceVersion: environment.endpoints.resourceVersion }, data: environment.endpoints.before }, profile);
  requireValue(same(endpoints, environment.endpoints) && service.spec.type === "ClusterIP" &&
    service.spec.selector?.["app.kubernetes.io/name"] === "sso" && service.spec.ports?.some(p => p.name === "https" && p.port === 443) &&
    !service.spec.externalIPs?.length && !service.spec.externalName, "PUBLIC_HELM_ENVIRONMENT_INVALID");
  const assertBoundary = async () => {
    const current = await get("kodex-system", "configmap", profile.endpointsName), s = await get("identity", "service", "sso");
    requireValue(current.metadata.uid === endpoints.uid && !current.metadata.deletionTimestamp &&
      [endpoints.before, endpoints.after].some(data => same(current.data, data)) && !current.binaryData && !current.immutable &&
      s.metadata.uid === environment.serviceUID && !s.metadata.deletionTimestamp && fingerprint(s.spec) === environment.serviceSHA256,
    "PUBLIC_HELM_BOUNDARY_CHANGED");
  };
  await assertBoundary();
  return { profiles: publicHelmProfiles, locks: await stableChartLocks("public-identity"), environment,
    planSchema: "kodex.public-helm-plan/v1", assertBoundary,
    desiredValues: (p, v) => publicHelmValues(v, p, endpoints, profile, service.spec.clusterIP),
    recoveryValues: (_p, v) => structuredClone(v),
    transition: (p, b, a) => publicHelmTransition(p, b, a, endpoints, profile, service.spec.clusterIP),
    expectedObjects: (p, b, a, live) => publicHelmExpectedObjects(p, b, a, live, endpoints, profile, service.spec.clusterIP) };
}

export const planPublicHelm = (args, port, internal = {}) => planSystemHelm(args, port,
  { ...internal, scopeFactory: input => publicHelmScope({ ...input, environment: internal.environment }) });
export const releasePublicHelm = (args, port) => releaseSystemHelm(args, port, planPublicHelm, verifyStableChart, publicHelmScope);

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const mode = process.argv[2];
  const task = mode === "plan" ? planPublicHelm(process.argv.slice(3)).then(d => ({ status: "PASS", phase: mode,
    source: d.plan.source, planSHA256: d.planSHA256, releases: d.plan.rows.map(r => r.release), apply: "NOT_RUN" })) :
    releasePublicHelm(process.argv.slice(2)).then(() => ({ status: "PASS", phase: mode }));
  task.then(r => console.log(JSON.stringify(r))).catch(error => {
    console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "PUBLIC_HELM_FAILED" })); process.exitCode = 1;
  });
}
