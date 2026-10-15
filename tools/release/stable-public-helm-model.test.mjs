import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { publicGrafanaSignout, publicHelmValues, publicHelmProfiles, publicHelmTransition } from "./stable-public-helm-model.mjs";
import { publicOAuthProxy } from "./stable-public-identity-model.mjs";

const profile = JSON.parse(readFileSync(new URL("../../infra/public-ingress/identity-profile.json", import.meta.url)));
const endpoints = { before: { publicOrigin: "https://control.example.test", oidcIssuer: "https://sso.example.test/realms/kodex" } }, ip = "10.43.0.10";
const enc = s => encodeURIComponent(s).replace(/[!'()*]/g, c => "%" + c.charCodeAt(0).toString(16).toUpperCase());
const signout = issuer => "https://grafana.example.test/oauth2/sign_out?rd=" + enc(issuer + "/protocol/openid-connect/logout?id_token_hint={id_token}&post_logout_redirect_uri=" + enc("https://grafana.example.test/"));
function proxy(p) {
  const control = p.name === "public-control-proxy";
  const args = { provider: "keycloak-oidc", "oidc-issuer-url": endpoints.before.oidcIssuer, "cookie-secure": true,
    "reverse-proxy": true, "provider-ca-file": "/oidc-provider-ca/ca.crt", "redirect-url": (control ? endpoints.before.publicOrigin : "https://grafana.example.test") + "/oauth2/callback",
    "whitelist-domain": control ? "control.example.test" : "grafana.example.test", "allowed-role": "kodex-owner" };
  const list = Object.entries(args).sort(([a], [b]) => a < b ? -1 : 1).map(([k, v]) => `--${k}=${v}`);
  if (!control) list.push("--whitelist-domain=sso.example.test");
  return { replicaCount: 2, config: { existingSecret: p.release }, extraArgs: control ? args : list,
    hostAliases: [{ ip, hostnames: ["sso.example.test"] }], ingress: { enabled: true, hosts: [control ? "control.example.test" : "grafana.example.test"],
      tls: [{ hosts: [control ? "control.example.test" : "grafana.example.test"], secretName: control ? "staff-control-center-public-tls" : "grafana-tls" }] } };
}
test("public Helm values preserve proxy credentials permissions and redirect Grafana only through its new issuer", () => {
  for (const p of publicHelmProfiles.slice(0, 2)) {
    const v = proxy(p), source = structuredClone(v), next = publicHelmValues(v, p, endpoints, profile, ip);
    assert.deepEqual(v, source); assert.deepEqual(next.config, source.config);
    assert.ok(next.extraArgs.includes("--allowed-role=kodex-owner"));
    assert.ok(next.extraArgs.includes("--whitelist-domain=sso.kodex.works"));
    assert.ok(next.extraArgs.includes("--oidc-issuer-url=https://sso.kodex.works/realms/kodex"));
    if (p.name === "public-grafana-proxy") assert.deepEqual(next.ingress, source.ingress);
    else assert.deepEqual(next.ingress.tls, [{ hosts: ["platform.kodex.works"], secretName: profile.platformCertificate }]);
  }
  const p = publicHelmProfiles[2], v = { grafana: { replicas: 1, admin: { existingSecret: "existing" }, "grafana.ini": { auth: { signout_redirect_url: signout(endpoints.before.oidcIssuer) } } } };
  const next = publicHelmValues(v, p, endpoints, profile, ip);
  assert.equal(next.grafana["grafana.ini"].auth.signout_redirect_url, signout("https://sso.kodex.works/realms/kodex"));
  assert.throws(() => publicGrafanaSignout(signout("https://wrong.example.test/realms/kodex"), endpoints, profile), /SOURCE_DRIFT/);
});
test("complete proxy render allows exact issuer aliases callback and ingress but no unrelated Secret or role change", () => {
  const p = publicHelmProfiles[0], v = proxy(p);
  const d = { apiVersion: "apps/v1", kind: "Deployment", metadata: { namespace: p.namespace, name: p.release }, spec: { replicas: 2,
    template: { spec: { hostAliases: structuredClone(v.hostAliases), containers: [{ name: "oauth2-proxy", args: ["--http-address=:4180",
      ...Object.entries(v.extraArgs).sort(([a], [b]) => a < b ? -1 : 1).map(([k, v]) => `--${k}=${v}`), "--config=/config"] }] } } } };
  const ingress = { apiVersion: "networking.k8s.io/v1", kind: "Ingress", metadata: { namespace: p.namespace, name: p.release },
    spec: { rules: [{ host: "control.example.test", http: { paths: [{ backend: { unchanged: true } }] } }], tls: structuredClone(v.ingress.tls) } };
  const secret = { apiVersion: "v1", kind: "Secret", metadata: { namespace: p.namespace, name: "existing" }, data: { untouched: "PRIVATE_SENTINEL" } };
  const before = [d, ingress, secret], after = structuredClone(before);
  after[0].spec = publicOAuthProxy({ ...d, metadata: { ...d.metadata, uid: "render", resourceVersion: "0" } }, endpoints, profile, ip).spec;
  after[1].spec.rules[0].host = profile.platformHost; after[1].spec.tls[0] = { hosts: [profile.platformHost], secretName: profile.platformCertificate };
  assert.equal(publicHelmTransition(p, before, after, endpoints, profile, ip).changes.length, 2);
  const changed = structuredClone(after); changed[2].data.untouched = "CHANGED";
  assert.throws(() => publicHelmTransition(p, before, changed, endpoints, profile, ip), /UNRELATED_CHANGE/);
  const unsafe = structuredClone(after); unsafe[0].spec.template.spec.containers[0].args.push("--skip-jwt-bearer-tokens=true");
  assert.throws(() => publicHelmTransition(p, before, unsafe, endpoints, profile, ip), /UNRELATED_CHANGE/);
});
test("Grafana render changes only its known ini signout and rollout checksum while SQLite remains singleton", () => {
  const p = publicHelmProfiles[2], before = [
    { apiVersion: "v1", kind: "ConfigMap", metadata: { namespace: p.namespace, name: p.grafana },
      data: { "grafana.ini": "[auth]\nsignout_redirect_url = " + signout(endpoints.before.oidcIssuer) + "\n" } },
    { apiVersion: "apps/v1", kind: "StatefulSet", metadata: { namespace: p.namespace, name: p.grafana }, spec: { replicas: 1,
      volumeClaimTemplates: [{ untouched: true }], template: { metadata: { annotations: { "checksum/config": "a".repeat(64) } }, spec: { containers: [{ image: "UNCHANGED" }] } } } },
  ], after = structuredClone(before);
  after[0].data["grafana.ini"] = "[auth]\nsignout_redirect_url = " + signout("https://sso.kodex.works/realms/kodex") + "\n";
  after[1].spec.template.metadata.annotations["checksum/config"] = "b".repeat(64);
  assert.equal(publicHelmTransition(p, before, after, endpoints, profile, ip).changes.length, 2);
  for (const mutate of [r => { r[1].spec.replicas = 2; }, r => { r[1].spec.volumeClaimTemplates = []; }, r => { r[0].data.extra = "INJECTED"; }]) {
    const invalid = structuredClone(after); mutate(invalid); assert.throws(() => publicHelmTransition(p, before, invalid, endpoints, profile, ip));
  }
});
