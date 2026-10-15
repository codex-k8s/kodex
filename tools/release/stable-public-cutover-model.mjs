import { fingerprint } from "./stable-inventory.mjs";
import { publicIdentityProfile } from "./stable-public-identity-model.mjs";
import { reverseSpecPatch, applyReverseSpec } from "./stable-reverse-patch.mjs";

function requireValue(v, code) { if (!v) throw Error(code); }
const same = (a, b) => fingerprint(a) === fingerprint(b);
export function publicCutoverContent(resource) {
  const r = structuredClone(resource); delete r.status;
  const annotations = { ...r.metadata.annotations }; if (r.kind === "Deployment") delete annotations["deployment.kubernetes.io/revision"];
  r.metadata = { name: r.metadata.name, namespace: r.metadata.namespace, labels: r.metadata.labels ?? {}, annotations,
    ...(r.metadata.ownerReferences ? { ownerReferences: r.metadata.ownerReferences } : {}),
    ...(r.metadata.finalizers ? { finalizers: r.metadata.finalizers } : {}) };
  return r;
}
export function publicCutoverRow(source, field, desired) {
  requireValue(source.metadata?.uid && source.metadata.resourceVersion && ["spec", "data"].includes(field), "PUBLIC_CUTOVER_SOURCE_INVALID");
  const after = { ...source, [field]: desired }, forward = reverseSpecPatch(desired, source[field]), inverse = reverseSpecPatch(source[field], desired);
  requireValue(same(applyReverseSpec(source[field], forward), desired), "PUBLIC_CUTOVER_PATCH_INVALID");
  const rename = operations => operations.map(o => ({ ...o, path: o.path.replace(/^\/spec/, "/" + field),
    ...(o.from ? { from: o.from.replace(/^\/spec/, "/" + field) } : {}) }));
  return { kind: source.kind, name: source.metadata.name, namespace: source.metadata.namespace, uid: source.metadata.uid, field,
    before: fingerprint(publicCutoverContent(source)), after: fingerprint(publicCutoverContent(after)),
    forward: rename(forward), inverse: rename(inverse) };
}

export function publicIngressSpec(source, endpoints, profile) {
  publicIdentityProfile(profile);
  const sso = source.metadata?.namespace === "identity" && source.metadata.name === "sso";
  const names = ["staff-control-center", "staff-control-center-api", "staff-control-center-public-assets"];
  requireValue(source.kind === "Ingress" && (sso || source.metadata?.namespace === "kodex-system" && names.includes(source.metadata.name)) &&
    !source.metadata.annotations?.["meta.helm.sh/release-name"], "PUBLIC_CUTOVER_INGRESS_SCOPE_INVALID");
  const oldHost = new URL(sso ? endpoints.before.oidcIssuer : endpoints.before.publicOrigin).hostname,
    host = sso ? profile.oidcHost : profile.platformHost, certificate = sso ? profile.oidcCertificate : profile.platformCertificate;
  requireValue(source.spec.ingressClassName === "traefik" && source.spec.rules?.length === 1 && source.spec.rules[0].host === oldHost &&
    source.spec.tls?.length === 1 && same(source.spec.tls[0].hosts, [oldHost]) && source.spec.tls[0].secretName ===
      (sso ? "sso-public-tls" : "staff-control-center-public-tls"), "PUBLIC_CUTOVER_INGRESS_SOURCE_INVALID");
  const annotations = source.metadata.annotations ?? {}, middleware = annotations["traefik.ingress.kubernetes.io/router.middlewares"], paths = source.spec.rules[0].http?.paths;
  requireValue(annotations["traefik.ingress.kubernetes.io/router.tls"] === "true" && annotations["traefik.ingress.kubernetes.io/router.entrypoints"] === "websecure" &&
    paths?.length && paths.every(p => !p.backend.resource && same(p.backend.service, { name: sso ? "sso" : "staff-control-center", port: { name: "https" } })),
  "PUBLIC_CUTOVER_INGRESS_TLS_INVALID");
  if (sso) requireValue(!middleware && paths.length === 1 && paths[0].path === "/" && paths[0].pathType === "Prefix", "PUBLIC_CUTOVER_SSO_ROUTE_INVALID");
  else if (source.metadata.name.endsWith("-public-assets")) requireValue(!middleware && paths.length === 3 &&
    paths.every(p => p.pathType === "Exact") && same(paths.map(p => p.path).sort(), ["/logo.png", "/manifest.webmanifest", "/sw.js"]), "PUBLIC_CUTOVER_PUBLIC_ASSET_INVALID");
  else if (source.metadata.name.endsWith("-api")) requireValue(middleware === "kodex-system-oauth2-control-center-auth@kubernetescrd" &&
    paths.length === 1 && paths[0].path === "/api/v1" && paths[0].pathType === "Prefix", "PUBLIC_CUTOVER_API_AUTH_INVALID");
  else requireValue(["kodex-system-oauth2-control-center-chain@kubernetescrd", "kodex-system-oauth2-control-center-chain@kubernetescrd,kodex-system-staff-control-center-retry@kubernetescrd"].includes(middleware) &&
    paths.length === 1 && paths[0].path === "/" && paths[0].pathType === "Prefix", "PUBLIC_CUTOVER_FRONTEND_AUTH_INVALID");
  const next = structuredClone(source.spec); next.rules.push({ ...structuredClone(next.rules[0]), host });
  next.tls.push({ hosts: [host], secretName: certificate }); return next;
}

export function publicSSOTransport(source, endpoints, profile) {
  publicIdentityProfile(profile);
  requireValue(source.kind === "ServersTransport" && source.apiVersion === "traefik.io/v1alpha1" && source.metadata.namespace === "identity" &&
    source.metadata.name === "sso-public" && source.spec.serverName === new URL(endpoints.before.oidcIssuer).hostname &&
    source.spec.insecureSkipVerify === false && same(source.spec.rootCAsSecrets, ["sso-public-tls"]) &&
    same(Object.keys(source.spec).sort(), ["insecureSkipVerify", "rootCAsSecrets", "serverName"]), "PUBLIC_CUTOVER_SSO_TRANSPORT_INVALID");
  return { ...source.spec, serverName: profile.oidcHost };
}

export function publicCanonicalRoutes(endpoints, profile) {
  publicIdentityProfile(profile);
  return [["kodex-system", "platform", endpoints.before.publicOrigin, endpoints.after.publicOrigin, "staff-control-center", "staff-control-center-public-tls"],
    ["identity", "identity", new URL(endpoints.before.oidcIssuer).origin, new URL(endpoints.after.oidcIssuer).origin, "sso", "sso-public-tls"]]
    .flatMap(([namespace, suffix, before, after, service, certificate]) => {
      const name = "kodex-public-canonical-" + suffix, host = new URL(before).hostname, meta = { name, namespace,
        labels: { "app.kubernetes.io/managed-by": "kodex-public-identity" } };
      return [{ apiVersion: "traefik.io/v1alpha1", kind: "Middleware", metadata: meta,
        spec: { redirectRegex: { regex: "^https://" + host.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "(?::443)?/.*$",
          replacement: after + "/", permanent: false } } },
      { apiVersion: "networking.k8s.io/v1", kind: "Ingress", metadata: { ...structuredClone(meta), annotations: {
        "traefik.ingress.kubernetes.io/router.entrypoints": "websecure", "traefik.ingress.kubernetes.io/router.tls": "true",
        "traefik.ingress.kubernetes.io/router.priority": "99999", "traefik.ingress.kubernetes.io/router.middlewares": namespace + "-" + name + "@kubernetescrd" } },
      spec: { ingressClassName: "traefik", rules: [{ host, http: { paths: [{ path: "/", pathType: "Prefix",
        backend: { service: { name: service, port: { name: "https" } } } }] } }], tls: [{ hosts: [host], secretName: certificate }] } }];
    });
}
