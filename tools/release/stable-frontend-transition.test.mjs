import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { frontendRouting, frontendRuntimeSnapshot } from "./stable-frontend-transition.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";

const base = fileURLToPath(new URL("../../deploy/k8s/base/staff-control-center/", import.meta.url));
const headers = readFileSync(base + "security-headers.conf", "utf8"), assets = readFileSync(base + "public-assets.conf", "utf8");
const selector = { "app.kubernetes.io/name": "staff-control-center", "app.kubernetes.io/component": "staff-frontend" };
function runtime() {
  return { kind: "ConfigMap", metadata: { name: "source-runtime", namespace: "kodex-system", uid: "runtime-uid", resourceVersion: "1" },
    data: { "runtime-config.json": JSON.stringify({ revision: "a".repeat(64), environment: "development", apiBaseUrl: "/", realtimeUrl: "/api/v1",
      requestTimeoutMs: 15000, oidc: { authority: "https://sso.example.test/realms/kodex", clientId: "kodex-control-center",
        redirectUri: "/auth/callback", postLogoutRedirectUri: "/", scope: "openid kodex.owner" } }),
    "security-headers.conf": headers.replaceAll("__KODEX_OIDC_ORIGIN__", "https://sso.example.test"),
    "public-assets.conf": assets, "readiness.json": '{"status":"ready","runtimeConfigRevision":"stale"}' } };
}
function routes() {
  const resource = (kind, name, spec) => ({ kind, metadata: { namespace: "kodex-system", name, uid: kind + "-" + name, resourceVersion: "1" }, spec });
  const service = resource("Service", "staff-control-center", { selector, type: "ClusterIP", clusterIP: "10.1.1.1",
    ports: [{ name: "http", port: 8080, targetPort: "http", protocol: "TCP" }] });
  const policy = resource("NetworkPolicy", "staff-control-center-ingress", { podSelector: { matchLabels: selector }, policyTypes: ["Ingress"],
    ingress: [{ from: [{ namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": "kube-system" } },
      podSelector: { matchLabels: { "app.kubernetes.io/name": "traefik" } } }], ports: [{ port: 8080, protocol: "TCP" }] }] });
  const ingresses = ["staff-control-center", "staff-control-center-api", "staff-control-center-public-assets"].map(name => {
    const api = name.endsWith("-api"), pub = name.endsWith("-assets");
    const paths = (api ? ["/api/v1"] : pub ? ["/manifest.webmanifest", "/logo.png", "/sw.js"] : ["/"]).map(path => ({ path,
      pathType: pub ? "Exact" : "Prefix", backend: { service: { name: api ? "control-api-gateway" : "staff-control-center", port: { name: api ? "https" : "http" } } } }));
    const value = resource("Ingress", name, { rules: [{ host: "platform.example.test", http: { paths } }],
      tls: [{ hosts: ["platform.example.test"], secretName: "staff-control-center-public-tls" }] });
    value.metadata.annotations = { "traefik.ingress.kubernetes.io/router.tls": "true", "traefik.ingress.kubernetes.io/router.entrypoints": "websecure",
      ...(!pub ? { "traefik.ingress.kubernetes.io/router.middlewares": api ? "kodex-system-oauth2-control-center-auth@kubernetescrd" : "kodex-system-oauth2-control-center-chain@kubernetescrd" } : {}) };
    return value;
  });
  const transport = JSON.parse(execFileSync("yq", ["-o=json", "-I=0", base + "servers-transport.yaml"], { encoding: "utf8" }));
  return { service, policy, ingresses, transport };
}
test("runtime snapshot preserves public configuration and seals one consistent revision", () => {
  const value = runtime(), original = structuredClone(value), prepared = frontendRuntimeSnapshot(value, headers, assets);
  assert.deepEqual(value, original); assert.equal(prepared.resource.immutable, true);
  const config = JSON.parse(prepared.resource.data["runtime-config.json"]), ready = JSON.parse(prepared.resource.data["readiness.json"]);
  assert.equal(config.revision, prepared.revision); assert.equal(ready.runtimeConfigRevision, prepared.revision);
  assert.equal(config.oidc.authority, "https://sso.example.test/realms/kodex");
  assert.equal(prepared.source.uid, value.metadata.uid);
});
test("runtime snapshot rejects secrets arbitrary nginx directives and foreign authority shape", () => {
  for (const scenario of ["secret", "headers", "authority"]) {
    const value = runtime(), config = JSON.parse(value.data["runtime-config.json"]);
    if (scenario === "secret") config.clientSecret = "PRIVATE_SENTINEL";
    if (scenario === "authority") config.oidc.authority = "https://user:PRIVATE_SENTINEL@sso.example.test/realms/kodex";
    if (scenario === "headers") value.data["security-headers.conf"] += "proxy_pass http://foreign;";
    value.data["runtime-config.json"] = JSON.stringify(config);
    assert.throws(() => frontendRuntimeSnapshot(value, headers, assets), /FRONTEND_RUNTIME_/);
  }
});
test("routing changes only backend ports while SSO peers and rollback remain exact", () => {
  const f = routes(), changes = frontendRouting(f.service, f.ingresses, f.policy, f.transport);
  assert.equal(changes.length, 5);
  for (const change of changes) {
    const source = [f.service, f.policy, ...f.ingresses].find(r => r.metadata.uid === change.uid);
    assert.deepEqual(applyReverseSpec(change.patch.at(-1).value, change.inverse), source.spec);
    if (change.kind === "Ingress") assert.equal(change.beforeAnnotationsSHA256, change.afterAnnotationsSHA256);
  }
  for (const modify of [f => { delete f.ingresses[1].metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"]; },
    f => { delete f.policy.spec.ingress[0].from[0].podSelector; }, f => { f.transport.spec.insecureSkipVerify = true; },
    f => { f.ingresses[2].spec.rules[0].http.paths.push(f.ingresses[0].spec.rules[0].http.paths[0]); }]) {
    const altered = routes(); modify(altered);
    assert.throws(() => frontendRouting(altered.service, altered.ingresses, altered.policy, altered.transport), /FRONTEND_/);
  }
});
test("nginx request logs use bounded classifications and never raw URL headers or identity", () => {
  const config = readFileSync(fileURLToPath(new URL("../../services/staff/control-center/nginx.conf", import.meta.url)), "utf8");
  const format = config.match(/log_format kodex[^;]+;/)?.[0];
  assert.ok(format?.includes("escape=json"));
  const variables = format.match(/\$[a-zA-Z0-9_]+/g);
  assert.deepEqual(variables, ["$kodex_method", "$kodex_route", "$status", "$body_bytes_sent", "$request_time"]);
  assert.ok(config.includes("error_log /dev/null crit;"));
  assert.ok(config.includes("default OTHER;")); assert.ok(config.includes("default frontend;"));
  assert.ok(config.includes("location ~ ^/api/v1(?:/|$)"));
  assert.ok(config.includes("client_max_body_size 512m;"));
  assert.ok(config.includes("proxy_request_buffering off;")); assert.ok(config.includes("proxy_buffering off;"));
  const gateway = readFileSync(fileURLToPath(new URL("../../services/external/control-api-gateway/internal/transport/http/command_endpoints.go", import.meta.url)), "utf8");
  assert.match(gateway, /maximumArtifactBytes\s*=\s*512\s*<<\s*20/);
});
