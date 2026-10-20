import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { publicBackend, publicCertificates, publicEndpoints, publicFrontendDeployment, publicFrontendRuntime, publicKeycloak, publicOAuthProxy } from "./stable-public-identity-model.mjs";
const profile = JSON.parse(await readFile(new URL("../../infra/public-ingress/identity-profile.json", import.meta.url), "utf8"));
const headers = await readFile(new URL("../../deploy/k8s/base/staff-control-center/security-headers.conf", import.meta.url), "utf8");
const assets = await readFile(new URL("../../deploy/k8s/base/staff-control-center/public-assets.conf", import.meta.url), "utf8");
const meta = (name, namespace = "kodex-system") => ({ name, namespace, uid: name + "-uid", resourceVersion: "1" });
function endpoints() {
  return { kind: "ConfigMap", metadata: meta("kodex-platform-endpoints"), data: { publicOrigin: "https://platform.example.test",
    oidcIssuer: "https://sso.example.test/realms/kodex", oidcJwksUrl: "https://sso.example.test/realms/kodex/protocol/openid-connect/certs",
    oidcConnectAddress: profile.oidcConnectAddress, oidcTlsServerName: "sso.example.test" } };
}
function keycloak() {
  return { kind: "Deployment", metadata: meta("sso", "identity"), spec: { replicas: 1, template: { spec: { containers: [{ name: "keycloak",
    env: [{ name: "KC_HOSTNAME", value: "https://sso.example.test" }, { name: "KC_HOSTNAME_BACKCHANNEL_DYNAMIC", value: "false" },
      { name: "KC_PROXY_HEADERS", value: "xforwarded" }, { name: "KC_HTTPS_CERTIFICATE_FILE", value: "/var/run/secrets/kodex/sso/tls/tls.crt" },
      { name: "KC_HTTPS_CERTIFICATE_KEY_FILE", value: "/var/run/secrets/kodex/sso/tls/tls.key" }, { name: "KC_DB_PASSWORD", value: "PRIVATE_IDENTITY_SENTINEL" }],
    volumeMounts: [{ name: "public-tls", mountPath: "/var/run/secrets/kodex/sso/tls", readOnly: true }] }],
  volumes: [{ name: "public-tls", secret: { secretName: "sso-public-tls", defaultMode: 288 } }] } } } };
}
function proxy(control = true) {
  const host = control ? "platform.example.test" : "grafana.example.test";
  return { kind: "Deployment", metadata: meta(control ? "oauth2-control-center" : "oauth2-grafana", control ? "kodex-system" : "observability"),
    spec: { replicas: 2, template: { spec: { hostAliases: [{ ip: "10.43.1.20", hostnames: ["sso.example.test"] }], containers: [{ name: "oauth2-proxy",
      args: ["--provider=keycloak-oidc", "--oidc-issuer-url=https://sso.example.test/realms/kodex", "--redirect-url=https://" + host + "/oauth2/callback",
        "--cookie-secure=true", "--reverse-proxy=true", "--provider-ca-file=/oidc-provider-ca/ca.crt", "--whitelist-domain=" + host,
        ...(control ? ["--allowed-role=kodex-owner"] : ["--allowed-group=kodex-admins", "--allowed-group=kodex-owners", "--allowed-group=kodex-monitoring", "--allowed-group=kodex-developers", "--whitelist-domain=sso.example.test"])],
    envFrom: [{ secretRef: { name: "oauth2-credentials" } }] }] } } } };
}

test("public endpoints move issuer/origin/SNI together and keep exact private Service destination", () => {
  const source = endpoints(), before = structuredClone(source), t = publicEndpoints(source, profile);
  assert.equal(t.after.publicOrigin, "https://platform.kodex.works"); assert.equal(t.after.oidcIssuer, "https://sso.kodex.works/realms/kodex");
  assert.equal(t.after.oidcConnectAddress, source.data.oidcConnectAddress); assert.equal(t.after.oidcTlsServerName, "sso.kodex.works");
  assert.deepEqual(source, before);
  for (const mutate of [s => { s.data.oidcConnectAddress = "outside.example.test:443"; }, s => { s.data.oidcJwksUrl += "?skip=true"; },
    s => { s.data.privateKey = "PRIVATE_IDENTITY_SENTINEL"; }, s => { s.data.oidcIssuer = "http://sso.example.test/realms/kodex"; },
    s => { s.data.publicOrigin = "https://owner:PRIVATE_IDENTITY_SENTINEL@platform.example.test"; }]) {
    const s = endpoints(); mutate(s); assert.throws(() => publicEndpoints(s, profile), /PUBLIC_IDENTITY_/);
  }
});

test("frontend public issuer and CSP share a new immutable revision, APIs and OIDC privileges remain unchanged", () => {
  const e = publicEndpoints(endpoints(), profile), source = { kind: "ConfigMap", metadata: meta("staff-control-center-runtime"), data: {
    "runtime-config.json": JSON.stringify({ revision: "a".repeat(64), environment: "staging", apiBaseUrl: "/", realtimeUrl: "/api/v1", requestTimeoutMs: 15000,
      oidc: { authority: e.before.oidcIssuer, clientId: "kodex-control-center", redirectUri: "/auth/callback", postLogoutRedirectUri: "/", scope: "openid kodex.owner" } }),
    "security-headers.conf": headers.replaceAll("__KODEX_OIDC_ORIGIN__", "https://sso.example.test"), "public-assets.conf": assets, "readiness.json": "{}" } };
  const before = structuredClone(source), result = publicFrontendRuntime(source, e, profile, headers, assets), value = JSON.parse(result.resource.data["runtime-config.json"]);
  assert.deepEqual(source, before); assert.equal(result.source.uid, source.metadata.uid); assert.equal(result.resource.immutable, true);
  assert.equal(value.oidc.authority, e.after.oidcIssuer); assert.equal(value.apiBaseUrl, "/"); assert.equal(value.realtimeUrl, "/api/v1");
  assert.equal(value.oidc.scope, "openid kodex.owner"); assert.equal(JSON.parse(result.resource.data["readiness.json"]).runtimeConfigRevision, value.revision);
  assert.equal(result.resource.data["security-headers.conf"].includes("https://sso.example.test"), false);
});

test("Keycloak transition changes only hostname and dedicated TLS reference, inverse excludes unchanged credentials", () => {
  const d = keycloak(), t = publicKeycloak(d, publicEndpoints(endpoints(), profile), profile);
  assert.deepEqual(applyReverseSpec(t.spec, t.inverse), d.spec); assert.equal(JSON.stringify(t.inverse).includes("PRIVATE_IDENTITY_SENTINEL"), false);
  assert.equal(t.spec.template.spec.containers[0].env[0].value, "https://sso.kodex.works");
  assert.equal(t.spec.template.spec.volumes[0].secret.secretName, profile.oidcCertificate); assert.equal(t.spec.replicas, d.spec.replicas);
  d.spec.template.spec.containers[0].env.push({ name: "KC_HOSTNAME_ADMIN", value: "https://foreign.example.test" });
  assert.throws(() => publicKeycloak(d, publicEndpoints(endpoints(), profile), profile), /PUBLIC_IDENTITY_KEYCLOAK_OVERRIDE_INVALID/);
});

test("both OAuth proxies retain roles/groups/session credentials/CA and direct private issuer routing", () => {
  const e = publicEndpoints(endpoints(), profile);
  for (const control of [true, false]) {
    const d = proxy(control), original = structuredClone(d), t = publicOAuthProxy(d, e, profile, "10.43.1.20"), app = t.spec.template.spec.containers[0];
    assert.deepEqual(d, original); assert.deepEqual(applyReverseSpec(t.spec, t.inverse), d.spec);
    assert.deepEqual(app.envFrom, d.spec.template.spec.containers[0].envFrom);
    assert.ok(app.args.includes("--oidc-issuer-url=" + e.after.oidcIssuer)); assert.ok(app.args.includes("--whitelist-domain=sso.kodex.works"));
    assert.ok(app.args.includes("--redirect-url=https://" + (control ? "platform.kodex.works" : "grafana.example.test") + "/oauth2/callback"));
    assert.deepEqual(app.args.filter(a => /^--allowed-/.test(a)), d.spec.template.spec.containers[0].args.filter(a => /^--allowed-/.test(a)));
    assert.deepEqual(t.spec.template.spec.hostAliases, [{ ip: "10.43.1.20", hostnames: ["sso.kodex.works"] }]);
  }
});

test("proxy stale issuer, public or invalid alias IP, unsafe auth overrides and inline credentials fail closed", () => {
  const e = publicEndpoints(endpoints(), profile);
  for (const arg of ["--cookie-domain=.example.test", "--skip-auth-regex=.*", "--insecure-oidc-skip-issuer-verification=true", "--client-secret=PRIVATE_IDENTITY_SENTINEL",
    "--redis-connection-url=rediss://user:PRIVATE_IDENTITY_SENTINEL@redis.example.test", "--oidc-issuer-url=https://foreign.example.test/realms/kodex"]) {
    const d = proxy(); d.spec.template.spec.containers[0].args.push(arg); assert.throws(() => publicOAuthProxy(d, e, profile, "10.43.1.20"), /PUBLIC_IDENTITY_/);
  }
  for (const ip of ["999.43.1.20", "8.8.8.8", "127.0.0.1", "10.43.1.21"]) assert.throws(() => publicOAuthProxy(proxy(), e, profile, ip), /PUBLIC_IDENTITY_PROXY_DNS_INVALID/);
});

test("dedicated public inner certificates reuse exact existing CA without overwriting original TLS Secrets", () => {
  const e = publicEndpoints(endpoints(), profile), issuerRef = { kind: "ClusterIssuer", name: "fixture-installation-ca", group: "cert-manager.io" };
  const front = { apiVersion: "cert-manager.io/v1", kind: "Certificate", metadata: meta("staff-control-center-public"),
    spec: { secretName: "staff-control-center-public-tls", issuerRef, dnsNames: ["platform.example.test"] } };
  const sso = { apiVersion: front.apiVersion, kind: "Certificate", metadata: meta("sso-public-tls", "identity"),
    spec: { secretName: "sso-public-tls", issuerRef, dnsNames: ["sso.example.test"] } };
  const before = structuredClone([front, sso]), result = publicCertificates(front, sso, e, profile);
  assert.deepEqual([front, sso], before); assert.equal(result[0].spec.secretName, profile.platformCertificate);
  assert.deepEqual(result[1].spec.dnsNames, [profile.oidcHost]); assert.deepEqual(result[1].spec.issuerRef, issuerRef);
  sso.spec.issuerRef = { ...issuerRef, name: "foreign" }; assert.throws(() => publicCertificates(front, sso, e, profile), /PUBLIC_IDENTITY_CERTIFICATE_SOURCE_INVALID/);
});

test("backend rollout is bound to exact shared endpoint references and trusted profile without changing credentials", () => {
  const e = publicEndpoints(endpoints(), profile);
  for (const name of ["control-plane", "control-api-gateway"]) {
    const cp = name === "control-plane", prefix = cp ? "CONTROL_PLANE_" : "CONTROL_API_GATEWAY_";
    const fields = { OIDC_ISSUER: "oidcIssuer", OIDC_JWKS_URL: "oidcJwksUrl", OIDC_CONNECT_ADDRESS: "oidcConnectAddress", OIDC_TLS_SERVER_NAME: "oidcTlsServerName",
      ...(cp ? {} : { OIDC_BROWSER_ORIGIN: "publicOrigin", ALLOWED_ORIGINS: "publicOrigin" }) };
    const d = { kind: "Deployment", metadata: meta(name), spec: { replicas: 2, template: { metadata: { annotations: { source: "fixed" } }, spec: { containers: [{ name,
      env: [{ name: "KODEX_RPC_PROFILE", value: "trusted-cluster" }, ...Object.entries(fields).map(([field, key]) => ({ name: prefix + field,
        valueFrom: { configMapKeyRef: { name: profile.endpointsName, key } } })), { name: "UNCHANGED", value: "PRIVATE_IDENTITY_SENTINEL" }] }] } } } };
    const t = publicBackend(d, e, profile); assert.deepEqual(applyReverseSpec(t.spec, t.inverse), d.spec);
    assert.equal(JSON.stringify(t.inverse).includes("PRIVATE_IDENTITY_SENTINEL"), false);
    assert.deepEqual(t.spec.template.spec, d.spec.template.spec); assert.notDeepEqual(t.spec.template.metadata, d.spec.template.metadata);
    d.spec.template.spec.containers[0].env[1].valueFrom.configMapKeyRef.name = "foreign";
    assert.throws(() => publicBackend(d, e, profile), /PUBLIC_IDENTITY_BACKEND_REFERENCE_INVALID/);
  }
});

test("frontend deployment changes only runtime reference and rollout marker", () => {
  const d = { kind: "Deployment", metadata: meta("staff-control-center"), spec: { replicas: 2, template: { metadata: { annotations: {} }, spec: {
    volumes: [{ name: "runtime-config", configMap: { name: "source-runtime" } }], containers: [{ name: "frontend", image: "example.invalid/frontend@sha256:" + "a".repeat(64) }] } } } };
  const runtime = { source: { name: "source-runtime" }, name: "desired-runtime", revision: "b".repeat(64) }, t = publicFrontendDeployment(d, runtime);
  assert.deepEqual(applyReverseSpec(t.spec, t.inverse), d.spec); assert.equal(t.spec.template.spec.volumes[0].configMap.name, runtime.name);
  assert.deepEqual(t.spec.template.spec.containers, d.spec.template.spec.containers);
  runtime.source.name = "foreign"; assert.throws(() => publicFrontendDeployment(d, runtime), /PUBLIC_IDENTITY_FRONTEND_RUNTIME_REFERENCE_INVALID/);
});
