import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { publicCanonicalRoutes, publicCutoverContent, publicCutoverRow, publicIngressSpec, publicSSOTransport } from "./stable-public-cutover-model.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { publicLeafProof, publicCutover } from "./stable-public-cutover.mjs";

const profile = JSON.parse(await readFile(new URL("../../infra/public-ingress/identity-profile.json", import.meta.url), "utf8"));
const endpoints = { before: { publicOrigin: "https://control.example.test", oidcIssuer: "https://sso.example.test/realms/kodex" },
  after: { publicOrigin: "https://platform.kodex.works", oidcIssuer: "https://sso.kodex.works/realms/kodex" } };
function ingress(name) {
  const sso = name === "sso", ns = sso ? "identity" : "kodex-system", host = sso ? "sso.example.test" : "control.example.test";
  return { apiVersion: "networking.k8s.io/v1", kind: "Ingress", metadata: { name, namespace: ns, uid: "uid-" + name, resourceVersion: "1", annotations: {
    "traefik.ingress.kubernetes.io/router.tls": "true", "traefik.ingress.kubernetes.io/router.entrypoints": "websecure",
    ...(!sso && !name.endsWith("-public-assets") ? { "traefik.ingress.kubernetes.io/router.middlewares":
      name.endsWith("-api") ? "kodex-system-oauth2-control-center-auth@kubernetescrd" : "kodex-system-oauth2-control-center-chain@kubernetescrd" } : {}) } },
  spec: { ingressClassName: "traefik", rules: [{ host, http: { paths: (name.endsWith("-public-assets") ? ["/manifest.webmanifest", "/logo.png", "/sw.js"] :
    [name.endsWith("-api") ? "/api/v1" : "/"]).map(path => ({ path, pathType: name.endsWith("-public-assets") ? "Exact" : "Prefix",
      backend: { service: { name: sso ? "sso" : "staff-control-center", port: { name: "https" } } } })) } }],
  tls: [{ hosts: [host], secretName: sso ? "sso-public-tls" : "staff-control-center-public-tls" }] } };
}

test("public ingress appends only exact public host/TLS and preserves old SNI and authentication paths", () => {
  for (const name of ["sso", "staff-control-center", "staff-control-center-api", "staff-control-center-public-assets"]) {
    const r = ingress(name), snapshot = structuredClone(r), spec = publicIngressSpec(r, endpoints, profile);
    assert.deepEqual(r, snapshot); assert.deepEqual(spec.rules[0], r.spec.rules[0]); assert.deepEqual(spec.tls[0], r.spec.tls[0]);
    assert.equal(spec.rules[1].host, name === "sso" ? profile.oidcHost : profile.platformHost);
    assert.deepEqual(spec.rules[1].http, r.spec.rules[0].http);
    const row = publicCutoverRow(r, "spec", spec);
    assert.deepEqual(applyReverseSpec(r.spec, row.forward), spec); assert.deepEqual(applyReverseSpec(spec, row.inverse), r.spec);
  }
  for (const mutate of [r => { delete r.metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"]; },
    r => { r.spec.rules[0].http.paths[0].backend.service.name = "control-api-gateway"; },
    r => { r.spec.tls[0].secretName = "foreign"; }, r => { r.metadata.annotations["meta.helm.sh/release-name"] = "other"; }]) {
    const r = ingress("staff-control-center-api"); mutate(r); assert.throws(() => publicIngressSpec(r, endpoints, profile), /PUBLIC_CUTOVER_/);
  }
});

test("SSO backend keeps exact CA and TLS verification while selecting its new leaf SNI", () => {
  const r = { apiVersion: "traefik.io/v1alpha1", kind: "ServersTransport", metadata: { name: "sso-public", namespace: "identity" },
    spec: { insecureSkipVerify: false, rootCAsSecrets: ["sso-public-tls"], serverName: "sso.example.test" } };
  assert.deepEqual(publicSSOTransport(r, endpoints, profile), { ...r.spec, serverName: profile.oidcHost });
  r.spec.insecureSkipVerify = true; assert.throws(() => publicSSOTransport(r, endpoints, profile), /TRANSPORT_INVALID/);
});

test("canonical redirect never forwards code, tokens, fragments or arbitrary old paths across hosts", () => {
  const resources = publicCanonicalRoutes(endpoints, profile); assert.equal(resources.length, 4);
  for (const r of resources.filter(r => r.kind === "Middleware")) {
    const old = r.metadata.namespace === "identity" ? "https://sso.example.test" : "https://control.example.test",
      dest = r.metadata.namespace === "identity" ? "https://sso.kodex.works/" : "https://platform.kodex.works/";
    const { regex, replacement } = r.spec.redirectRegex;
    assert.equal((old + "/auth/callback?code=PRIVATE_SENTINEL#PRIVATE_SENTINEL").replace(new RegExp(regex), replacement), dest);
    assert.equal(new RegExp(regex).test("https://ssoXexampleXtest/auth"), false);
    assert.equal(new RegExp(regex).test("https://foreign.example.test/auth"), false);
  }
  for (const r of resources.filter(r => r.kind === "Ingress")) {
    assert.equal(r.spec.tls[0].hosts[0], r.spec.rules[0].host);
    assert.equal(r.metadata.annotations["traefik.ingress.kubernetes.io/router.priority"], "99999");
  }
});

test("public field patches never serialize unchanged credentials; status-only changes do not alter content", () => {
  const d = { kind: "Deployment", metadata: { name: "sso", namespace: "identity", uid: "source", resourceVersion: "1" },
    spec: { replicas: 2, env: [{ name: "unchanged", value: "PRIVATE_CREDENTIAL_SENTINEL" }], host: "old" } };
  const row = publicCutoverRow(d, "spec", { ...d.spec, host: "new" });
  assert.equal(JSON.stringify(row).includes("PRIVATE_CREDENTIAL_SENTINEL"), false);
  const updated = structuredClone(d); updated.status = { readyReplicas: 2 }; updated.metadata.resourceVersion = "200";
  updated.metadata.annotations = { "deployment.kubernetes.io/revision": "10" };
  assert.deepEqual(publicCutoverContent(d), publicCutoverContent(updated));
  const cm = { kind: "ConfigMap", metadata: d.metadata, data: { host: "old" } }, next = publicCutoverRow(cm, "data", { host: "new" });
  assert.equal(next.forward[0].path, "/data/host"); assert.equal(next.inverse[0].path, "/data/host");
});

test("public leaf proof rejects malformed certificates; invalid scope never touches a cluster", async () => {
  assert.throws(() => publicLeafProof("not a certificate", "bad CA", profile.oidcHost), /CERTIFICATE_INVALID/);
  let calls = 0;
  await assert.rejects(publicCutover(["plan", "--context", "foreign", "--stage", "identity", "--baseline", "/tmp/example", "--output", "/tmp/example"],
    async () => { calls++; return ""; }), /ARGUMENTS_INVALID/); assert.equal(calls, 0);
});
