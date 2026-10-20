import { fingerprint } from "./stable-inventory.mjs";
import { publicIdentityProfile, publicOAuthProxy } from "./stable-public-identity-model.mjs";

function requireValue(v, code) { if (!v) throw Error(code); }
const same = (a, b) => fingerprint(a) === fingerprint(b);
const escape = s => encodeURIComponent(s).replace(/[!'()*]/g, c => "%" + c.charCodeAt(0).toString(16).toUpperCase());
export const publicHelmProfiles = [
  { name: "public-control-proxy", namespace: "kodex-system", release: "oauth2-control-center", chartName: "oauth2-proxy", deployments: ["oauth2-control-center"] },
  { name: "public-grafana-proxy", namespace: "observability", release: "oauth2-grafana", chartName: "oauth2-proxy", deployments: ["oauth2-grafana"] },
  { name: "public-grafana-signout", namespace: "observability", release: "kodex-monitoring", chartName: "kube-prometheus-stack", deployments: [], grafana: "kodex-monitoring-grafana" },
];
function profileValid(p) { requireValue(publicHelmProfiles.some(x => same(x, p)), "PUBLIC_HELM_PROFILE_INVALID"); }
export function publicGrafanaSignout(source, endpoints, profile) {
  const urls = publicIdentityProfile(profile); let outer, logout;
  try { outer = new URL(source); logout = new URL(outer.searchParams.get("rd")); } catch { throw Error("PUBLIC_HELM_SIGNOUT_INVALID"); }
  requireValue(outer.protocol === "https:" && !outer.username && !outer.password && !outer.port && outer.pathname === "/oauth2/sign_out" && !outer.hash &&
    same([...outer.searchParams.keys()], ["rd"]) && same([...logout.searchParams.keys()], ["id_token_hint", "post_logout_redirect_uri"]) &&
    logout.searchParams.get("id_token_hint") === "{id_token}" && logout.searchParams.get("post_logout_redirect_uri") === outer.origin + "/",
  "PUBLIC_HELM_SIGNOUT_INVALID");
  const build = issuer => outer.origin + "/oauth2/sign_out?rd=" + escape(issuer + "/protocol/openid-connect/logout?id_token_hint={id_token}&post_logout_redirect_uri=" + escape(outer.origin + "/"));
  requireValue(source === build(endpoints.before.oidcIssuer), "PUBLIC_HELM_SIGNOUT_SOURCE_DRIFT"); return build(urls.issuer);
}

export function publicHelmValues(values, p, endpoints, profile, connectIP) {
  profileValid(p); const urls = publicIdentityProfile(profile), v = structuredClone(values);
  if (p.grafana) {
    requireValue(v.grafana?.replicas === 1 && v.grafana.admin?.existingSecret && v.grafana["grafana.ini"]?.auth,
      "PUBLIC_HELM_GRAFANA_SOURCE_INVALID");
    v.grafana["grafana.ini"].auth.signout_redirect_url = publicGrafanaSignout(v.grafana["grafana.ini"].auth.signout_redirect_url, endpoints, profile);
    return v;
  }
  requireValue(v.replicaCount === 2 && v.config?.existingSecret === p.release &&
    !v.config.clientSecret && !v.config.cookieSecret && same(v.hostAliases, [{ ip: connectIP, hostnames: [new URL(endpoints.before.oidcIssuer).hostname] }]),
  "PUBLIC_HELM_PROXY_SOURCE_INVALID");
  const list = Array.isArray(v.extraArgs) ? [...v.extraArgs] : Object.entries(v.extraArgs ?? {}).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)
    .map(([k, val]) => { requireValue(/^[a-z][a-z0-9-]+$/.test(k) && (val === null || ["string", "number", "boolean"].includes(typeof val)),
      "PUBLIC_HELM_PROXY_ARGUMENT_INVALID"); return "--" + k + (val === null ? "" : "=" + String(val)); });
  const control = p.release === "oauth2-control-center", oldPlatform = new URL(endpoints.before.publicOrigin).hostname, oldSSO = new URL(endpoints.before.oidcIssuer).hostname;
  requireValue(list.filter(a => a === "--oidc-issuer-url=" + endpoints.before.oidcIssuer).length === 1 &&
    (!control || list.filter(a => a === "--redirect-url=" + endpoints.before.publicOrigin + "/oauth2/callback").length === 1),
  "PUBLIC_HELM_PROXY_ARGUMENT_INVALID");
  v.extraArgs = list.map(a => a === "--oidc-issuer-url=" + endpoints.before.oidcIssuer ? "--oidc-issuer-url=" + urls.issuer :
    control && a === "--redirect-url=" + endpoints.before.publicOrigin + "/oauth2/callback" ? "--redirect-url=" + urls.origin + "/oauth2/callback" :
      a === "--whitelist-domain=" + oldSSO ? "--whitelist-domain=" + profile.oidcHost :
        control && a === "--whitelist-domain=" + oldPlatform ? "--whitelist-domain=" + profile.platformHost : a);
  if (!v.extraArgs.includes("--whitelist-domain=" + profile.oidcHost)) v.extraArgs.push("--whitelist-domain=" + profile.oidcHost);
  v.hostAliases[0].hostnames = [profile.oidcHost];
  if (control) {
    requireValue(v.ingress?.enabled === true && same(v.ingress.hosts, [oldPlatform]) && v.ingress.tls?.length === 1 &&
      same(v.ingress.tls[0].hosts, [oldPlatform]) && v.ingress.tls[0].secretName === "staff-control-center-public-tls", "PUBLIC_HELM_PROXY_INGRESS_INVALID");
    v.ingress.hosts = [profile.platformHost]; v.ingress.tls[0] = { ...v.ingress.tls[0], hosts: [profile.platformHost], secretName: profile.platformCertificate };
  }
  return v;
}

const key = r => [r.apiVersion, r.kind, r.metadata?.namespace ?? "", r.metadata?.name].join("/");
export function publicHelmTransition(p, before, after, endpoints, profile, connectIP) {
  profileValid(p); publicIdentityProfile(profile);
  requireValue(Array.isArray(before) && before.length > 0 && before.length < 512 && after.length === before.length &&
    new Set(before.map(key)).size === before.length && new Set(after.map(key)).size === after.length, "PUBLIC_HELM_RENDER_INVALID");
  const desired = new Map(after.map(r => [key(r), r])), changes = [], targets = new Set();
  for (const b of before) {
    const a = desired.get(key(b)); requireValue(a, "PUBLIC_HELM_OBJECT_SET_CHANGED");
    let expected = structuredClone(b);
    if (b.metadata.namespace === p.namespace && b.kind === "Deployment" && p.deployments.includes(b.metadata.name)) {
      const d = { ...b, metadata: { ...b.metadata, uid: "render", resourceVersion: "0" } };
      expected.spec = publicOAuthProxy(d, endpoints, profile, connectIP).spec; targets.add(b.metadata.name);
    } else if (p.release === "oauth2-control-center" && b.kind === "Ingress" && b.metadata.namespace === p.namespace && b.metadata.name === p.release) {
      requireValue(b.spec.rules?.length === 1 && b.spec.rules[0].host === new URL(endpoints.before.publicOrigin).hostname && b.spec.tls?.length === 1 &&
        same(b.spec.tls[0].hosts, [b.spec.rules[0].host]) && b.spec.tls[0].secretName === "staff-control-center-public-tls", "PUBLIC_HELM_PROXY_INGRESS_INVALID");
      expected.spec.rules[0].host = profile.platformHost;
      expected.spec.tls[0] = { ...expected.spec.tls[0], hosts: [profile.platformHost], secretName: profile.platformCertificate }; targets.add("Ingress");
    } else if (p.grafana && b.metadata.namespace === p.namespace && b.metadata.name === p.grafana && b.kind === "ConfigMap") {
      requireValue(same(Object.keys(b.data ?? {}), ["grafana.ini"]) && !b.binaryData && !b.immutable, "PUBLIC_HELM_GRAFANA_CONFIG_INVALID");
      const rows = b.data["grafana.ini"].split("\n"), signout = rows.filter(l => l.startsWith("signout_redirect_url = "));
      requireValue(signout.length === 1, "PUBLIC_HELM_GRAFANA_CONFIG_INVALID");
      const original = signout[0].slice("signout_redirect_url = ".length), next = publicGrafanaSignout(original, endpoints, profile);
      expected.data["grafana.ini"] = rows.map(l => l === signout[0] ? "signout_redirect_url = " + next : l).join("\n"); targets.add("ConfigMap");
    } else if (p.grafana && b.metadata.namespace === p.namespace && b.metadata.name === p.grafana && b.kind === "StatefulSet") {
      requireValue(b.spec.replicas === 1 && a.spec.replicas === 1 &&
        [b, a].every(r => /^[a-f0-9]{64}$/.test(r.spec.template.metadata.annotations?.["checksum/config"] ?? "")) &&
        b.spec.template.metadata.annotations["checksum/config"] !== a.spec.template.metadata.annotations["checksum/config"], "PUBLIC_HELM_GRAFANA_CHECKSUM_INVALID");
      expected.spec.template.metadata.annotations["checksum/config"] = a.spec.template.metadata.annotations["checksum/config"]; targets.add("StatefulSet");
    }
    requireValue(same(expected, a), "PUBLIC_HELM_UNRELATED_CHANGE_FORBIDDEN");
    if (!same(b, a)) changes.push({ kind: b.kind, name: b.metadata.name, beforeSHA256: fingerprint(b), afterSHA256: fingerprint(a) });
  }
  requireValue(p.grafana ? targets.has("ConfigMap") && targets.has("StatefulSet") : p.deployments.every(n => targets.has(n)) &&
    (p.release !== "oauth2-control-center" || targets.has("Ingress")), "PUBLIC_HELM_TARGET_MISSING");
  return { beforeSHA256: fingerprint(before), afterSHA256: fingerprint(after), changes };
}
