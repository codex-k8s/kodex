import { fingerprint } from "./stable-inventory.mjs";
import { isIP } from "node:net";
import { frontendRuntimeSnapshot } from "./stable-frontend-transition.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
const equal = (a, b) => fingerprint(a) === fingerprint(b);
const endpointKeys = ["publicOrigin", "oidcIssuer", "oidcJwksUrl", "oidcConnectAddress", "oidcTlsServerName"];
function origin(value) {
  let url; try { url = new URL(value); } catch { throw Error("PUBLIC_IDENTITY_ORIGIN_INVALID"); }
  requireValue(url.protocol === "https:" && value === url.origin && !url.username && !url.password && !url.port &&
    url.hostname.includes(".") && url.hostname.split(".").every(p => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(p)), "PUBLIC_IDENTITY_ORIGIN_INVALID");
  return url;
}
export function publicIdentityProfile(profile) {
  requireValue(profile?.schema === "kodex.public-identity-profile/v1" && profile.platformHost === "platform.kodex.works" &&
    profile.oidcHost === "sso.kodex.works" && profile.realm === "kodex" && profile.namespace === "kodex-system" &&
    profile.identityNamespace === "identity" && profile.endpointsName === "kodex-platform-endpoints" &&
    profile.oidcConnectAddress === "sso.identity.svc.cluster.local:443" && profile.platformCertificate === "kodex-public-platform-tls" &&
    profile.oidcCertificate === "kodex-public-identity-tls", "PUBLIC_IDENTITY_PROFILE_INVALID");
  return { origin: "https://" + profile.platformHost, oidcOrigin: "https://" + profile.oidcHost,
    issuer: "https://" + profile.oidcHost + "/realms/kodex" };
}
function identity(r, kind, namespace, name) {
  requireValue(r?.kind === kind && r.metadata?.namespace === namespace && r.metadata.name === name && r.metadata.uid &&
    r.metadata.resourceVersion && !r.metadata.deletionTimestamp, "PUBLIC_IDENTITY_RESOURCE_INVALID");
}

export function publicEndpoints(source, profile) {
  const publicURLs = publicIdentityProfile(profile); identity(source, "ConfigMap", profile.namespace, profile.endpointsName);
  requireValue(equal(Object.keys(source.data ?? {}).sort(), [...endpointKeys].sort()) && !Object.keys(source.binaryData ?? {}).length && !source.immutable,
    "PUBLIC_IDENTITY_ENDPOINT_KEYS_INVALID");
  const platform = origin(source.data.publicOrigin), oldIssuer = source.data.oidcIssuer;
  requireValue(typeof oldIssuer === "string" && oldIssuer.endsWith("/realms/kodex"), "PUBLIC_IDENTITY_ISSUER_INVALID");
  const oldOIDC = origin(oldIssuer.slice(0, -"/realms/kodex".length));
  requireValue(platform.hostname !== oldOIDC.hostname && source.data.oidcJwksUrl === oldIssuer + "/protocol/openid-connect/certs" &&
    source.data.oidcTlsServerName === oldOIDC.hostname && source.data.oidcConnectAddress === profile.oidcConnectAddress &&
    platform.origin !== publicURLs.origin && oldIssuer !== publicURLs.issuer, "PUBLIC_IDENTITY_ENDPOINT_SOURCE_INVALID");
  return { uid: source.metadata.uid, resourceVersion: source.metadata.resourceVersion, beforeSHA256: fingerprint(source.data),
    before: structuredClone(source.data), after: { publicOrigin: publicURLs.origin, oidcIssuer: publicURLs.issuer,
      oidcJwksUrl: publicURLs.issuer + "/protocol/openid-connect/certs", oidcConnectAddress: profile.oidcConnectAddress, oidcTlsServerName: profile.oidcHost } };
}

export function publicFrontendRuntime(source, endpoints, profile, headers, assets) {
  const urls = publicIdentityProfile(profile), before = frontendRuntimeSnapshot(source, headers, assets), changed = structuredClone(source);
  const value = JSON.parse(changed.data["runtime-config.json"]);
  requireValue(value.oidc.authority === endpoints.before.oidcIssuer, "PUBLIC_IDENTITY_FRONTEND_ISSUER_DRIFT");
  value.oidc.authority = urls.issuer; changed.data["runtime-config.json"] = JSON.stringify(value);
  changed.data["security-headers.conf"] = headers.replaceAll("__KODEX_OIDC_ORIGIN__", urls.oidcOrigin);
  const result = frontendRuntimeSnapshot(changed, headers, assets); result.source = before.source; return result;
}

export function publicBackend(deployment, endpoints, profile) {
  publicIdentityProfile(profile);
  const cp = deployment.metadata?.name === "control-plane", name = cp ? "control-plane" : "control-api-gateway";
  identity(deployment, "Deployment", "kodex-system", name);
  const app = deployment.spec?.template?.spec?.containers?.filter(c => c.name === name);
  requireValue(app?.length === 1, "PUBLIC_IDENTITY_BACKEND_APPLICATION_INVALID");
  const prefix = cp ? "CONTROL_PLANE_" : "CONTROL_API_GATEWAY_", entries = app[0].env ?? [];
  const required = { OIDC_ISSUER: "oidcIssuer", OIDC_JWKS_URL: "oidcJwksUrl", OIDC_CONNECT_ADDRESS: "oidcConnectAddress", OIDC_TLS_SERVER_NAME: "oidcTlsServerName",
    ...(cp ? {} : { OIDC_BROWSER_ORIGIN: "publicOrigin", ALLOWED_ORIGINS: "publicOrigin" }) };
  for (const [field, key] of Object.entries(required)) {
    const actual = entries.filter(e => e.name === prefix + field);
    requireValue(actual.length === 1 && actual[0].value === undefined && equal(actual[0].valueFrom,
      { configMapKeyRef: { name: profile.endpointsName, key } }), "PUBLIC_IDENTITY_BACKEND_REFERENCE_INVALID");
  }
  requireValue(entries.filter(e => e.name === "KODEX_RPC_PROFILE").length === 1 &&
    entries.find(e => e.name === "KODEX_RPC_PROFILE").value === "trusted-cluster", "PUBLIC_IDENTITY_BACKEND_PROFILE_INVALID");
  const spec = structuredClone(deployment.spec);
  spec.template.metadata.annotations = { ...spec.template.metadata.annotations, "kodex.dev/public-identity-endpoints": fingerprint(endpoints.after) };
  return transition(deployment, spec);
}

export function publicFrontendDeployment(deployment, runtime) {
  identity(deployment, "Deployment", "kodex-system", "staff-control-center");
  const spec = structuredClone(deployment.spec), volumes = spec.template?.spec?.volumes?.filter(v => v.name === "runtime-config");
  requireValue(volumes?.length === 1 && volumes[0].configMap?.name === runtime.source.name, "PUBLIC_IDENTITY_FRONTEND_RUNTIME_REFERENCE_INVALID");
  volumes[0].configMap.name = runtime.name;
  spec.template.metadata.annotations = { ...spec.template.metadata.annotations, "kodex.dev/public-identity-runtime": runtime.revision };
  return transition(deployment, spec);
}

function transition(deployment, spec) {
  return { uid: deployment.metadata.uid, resourceVersion: deployment.metadata.resourceVersion, beforeSHA256: fingerprint(deployment.spec),
    afterSHA256: fingerprint(spec), spec, inverse: reverseSpecPatch(deployment.spec, spec) };
}
export function publicKeycloak(deployment, endpoints, profile) {
  publicIdentityProfile(profile); identity(deployment, "Deployment", "identity", "sso");
  const spec = structuredClone(deployment.spec), containers = spec.template?.spec?.containers;
  requireValue(containers?.length === 1 && containers[0].name === "keycloak", "PUBLIC_IDENTITY_KEYCLOAK_PROFILE_INVALID");
  const env = containers[0].env, expected = { KC_HOSTNAME: new URL(endpoints.before.oidcIssuer).origin,
    KC_HOSTNAME_BACKCHANNEL_DYNAMIC: "false", KC_PROXY_HEADERS: "xforwarded",
    KC_HTTPS_CERTIFICATE_FILE: "/var/run/secrets/kodex/sso/tls/tls.crt", KC_HTTPS_CERTIFICATE_KEY_FILE: "/var/run/secrets/kodex/sso/tls/tls.key" };
  for (const [name, value] of Object.entries(expected)) {
    const fields = env?.filter(e => e.name === name);
    requireValue(fields?.length === 1 && fields[0].value === value && !fields[0].valueFrom, "PUBLIC_IDENTITY_KEYCLOAK_ENV_INVALID");
  }
  requireValue(!env.some(e => ["KC_HOSTNAME_ADMIN", "KC_HOSTNAME_URL", "KC_HOSTNAME_STRICT"].includes(e.name)), "PUBLIC_IDENTITY_KEYCLOAK_OVERRIDE_INVALID");
  const volume = spec.template.spec.volumes?.filter(v => v.name === "public-tls"), mount = containers[0].volumeMounts?.filter(m => m.name === "public-tls");
  requireValue(volume?.length === 1 && volume[0].secret?.secretName === "sso-public-tls" && mount?.length === 1 &&
    mount[0].readOnly === true && mount[0].mountPath === "/var/run/secrets/kodex/sso/tls", "PUBLIC_IDENTITY_KEYCLOAK_TLS_INVALID");
  env.find(e => e.name === "KC_HOSTNAME").value = "https://" + profile.oidcHost; volume[0].secret.secretName = profile.oidcCertificate;
  return transition(deployment, spec);
}

export function publicOAuthProxy(deployment, endpoints, profile, connectIP) {
  const urls = publicIdentityProfile(profile), control = deployment.metadata?.name === "oauth2-control-center";
  identity(deployment, "Deployment", control ? "kodex-system" : "observability", control ? "oauth2-control-center" : "oauth2-grafana");
  const spec = structuredClone(deployment.spec), containers = spec.template?.spec?.containers;
  requireValue(containers?.length === 1 && containers[0].name === "oauth2-proxy", "PUBLIC_IDENTITY_PROXY_PROFILE_INVALID");
  const args = containers[0].args, oldOIDC = new URL(endpoints.before.oidcIssuer), oldPlatform = new URL(endpoints.before.publicOrigin);
  const exact = (key, expected) => {
    const entries = args?.filter(a => a.startsWith("--" + key + "="));
    requireValue(entries?.length === 1 && (expected === undefined || entries[0] === "--" + key + "=" + expected), "PUBLIC_IDENTITY_PROXY_ARGUMENT_INVALID");
    return entries[0];
  };
  exact("provider", "keycloak-oidc"); exact("oidc-issuer-url", endpoints.before.oidcIssuer);
  exact("cookie-secure", "true"); exact("reverse-proxy", "true"); exact("provider-ca-file", "/oidc-provider-ca/ca.crt");
  requireValue(!args.some(a => /^--(?:cookie-domain|insecure-oidc[^=]*|ssl-insecure-skip-verify|skip-jwt-bearer-tokens|skip-auth-regex|skip-auth-route)(?:=|$)/.test(a)),
    "PUBLIC_IDENTITY_PROXY_UNSAFE_OVERRIDE");
  requireValue(!args.some(a => /^--(?:client-secret|cookie-secret|redis-password|redis-sentinel-password)=/.test(a) || /:\/\/[^/\s]*@/.test(a)),
    "PUBLIC_IDENTITY_PROXY_INLINE_CREDENTIAL_REJECTED");
  const redirect = exact("redirect-url").slice("--redirect-url=".length), redirectURL = new URL(redirect);
  origin(redirectURL.origin);
  requireValue(redirect === redirectURL.origin + "/oauth2/callback" && (!control || redirectURL.origin === oldPlatform.origin),
    "PUBLIC_IDENTITY_PROXY_REDIRECT_INVALID");
  const aliases = spec.template.spec.hostAliases;
  const octets = connectIP?.split(".").map(Number);
  requireValue(isIP(connectIP ?? "") === 4 && (octets[0] === 10 || octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31 || octets[0] === 192 && octets[1] === 168) &&
    equal(aliases, [{ ip: connectIP, hostnames: [oldOIDC.hostname] }]),
    "PUBLIC_IDENTITY_PROXY_DNS_INVALID");
  let platformWhitelist = 0;
  containers[0].args = args.map(a => {
    if (a === "--oidc-issuer-url=" + endpoints.before.oidcIssuer) return "--oidc-issuer-url=" + urls.issuer;
    if (control && a === "--redirect-url=" + redirect) return "--redirect-url=" + urls.origin + "/oauth2/callback";
    if (a === "--whitelist-domain=" + oldOIDC.hostname) return "--whitelist-domain=" + profile.oidcHost;
    if (control && a === "--whitelist-domain=" + oldPlatform.hostname) { platformWhitelist++; return "--whitelist-domain=" + profile.platformHost; }
    return a;
  });
  requireValue(!control || platformWhitelist === 1, "PUBLIC_IDENTITY_PROXY_WHITELIST_INVALID");
  if (!containers[0].args.includes("--whitelist-domain=" + profile.oidcHost)) containers[0].args.push("--whitelist-domain=" + profile.oidcHost);
  aliases[0].hostnames = [profile.oidcHost]; return transition(deployment, spec);
}

export function publicCertificates(platformSource, oidcSource, endpoints, profile) {
  publicIdentityProfile(profile);
  const sources = [[platformSource, "kodex-system", "staff-control-center-public", profile.platformCertificate, profile.platformHost, new URL(endpoints.before.publicOrigin).hostname],
    [oidcSource, "identity", "sso-public-tls", profile.oidcCertificate, profile.oidcHost, new URL(endpoints.before.oidcIssuer).hostname]];
  let issuer;
  return sources.map(([source, namespace, name, target, host, oldHost]) => {
    identity(source, "Certificate", namespace, name);
    requireValue(source.apiVersion === "cert-manager.io/v1" && source.spec?.issuerRef?.kind === "ClusterIssuer" &&
      source.spec.issuerRef.name && equal(source.spec.dnsNames, [oldHost]) && source.spec.secretName === (namespace === "identity" ? "sso-public-tls" : "staff-control-center-public-tls") &&
      !source.spec.isCA && (!issuer || equal(issuer, source.spec.issuerRef)), "PUBLIC_IDENTITY_CERTIFICATE_SOURCE_INVALID");
    issuer = source.spec.issuerRef;
    return { apiVersion: "cert-manager.io/v1", kind: "Certificate", metadata: { name: target, namespace,
      labels: { "app.kubernetes.io/managed-by": "kodex-public-identity" } }, spec: { secretName: target, issuerRef: structuredClone(issuer), dnsNames: [host],
      privateKey: { algorithm: "ECDSA", size: 256, rotationPolicy: "Always" }, usages: ["digital signature", "server auth"] } };
  });
}
