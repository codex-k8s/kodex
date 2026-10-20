import { createHash } from "node:crypto";
import { fingerprint } from "./stable-inventory.mjs";
import { reverseSpecPatch } from "./stable-reverse-patch.mjs";

export const frontendTransportAnnotation = "traefik.ingress.kubernetes.io/service.serverstransport";
export const frontendIntentAnnotation = "kodex.dev/stable-frontend-plan";
const namespace = "kodex-system", frontend = "staff-control-center";
const equal = (a, b) => fingerprint(a) === fingerprint(b);
function requireValue(value, code) { if (!value) throw Error(code); }
const keys = (value, expected) => value && equal(Object.keys(value).sort(), [...expected].sort());

export function frontendRuntimeSnapshot(source, headersTemplate, publicAssets) {
  requireValue(source?.kind === "ConfigMap" && source.metadata?.namespace === namespace && source.metadata.uid &&
    source.metadata.resourceVersion && keys(source.data, ["runtime-config.json", "security-headers.conf", "public-assets.conf", "readiness.json"]) &&
    !Object.keys(source.binaryData ?? {}).length, "FRONTEND_RUNTIME_SOURCE_INVALID");
  const config = JSON.parse(source.data["runtime-config.json"]);
  requireValue(keys(config, ["revision", "environment", "apiBaseUrl", "realtimeUrl", "requestTimeoutMs", "oidc"]) &&
    /^[a-f0-9]{64}$/.test(config.revision ?? "") && ["development", "staging", "production"].includes(config.environment) &&
    config.apiBaseUrl === "/" && config.realtimeUrl === "/api/v1" && Number.isSafeInteger(config.requestTimeoutMs) &&
    config.requestTimeoutMs >= 1000 && config.requestTimeoutMs <= 120000 &&
    keys(config.oidc, ["authority", "clientId", "redirectUri", "postLogoutRedirectUri", "scope"]) &&
    config.oidc.clientId === "kodex-control-center" && config.oidc.redirectUri === "/auth/callback" &&
    config.oidc.postLogoutRedirectUri === "/" && /^[a-zA-Z0-9_.: -]{1,256}$/.test(config.oidc.scope), "FRONTEND_RUNTIME_CONFIGURATION_INVALID");
  const authority = new URL(config.oidc.authority);
  requireValue(authority.protocol === "https:" && authority.hostname.includes(".") && !authority.username && !authority.password &&
    !authority.search && !authority.hash && authority.pathname === "/realms/kodex" && ["", "443"].includes(authority.port),
    "FRONTEND_RUNTIME_AUTHORITY_INVALID");
  const headers = headersTemplate.replaceAll("__KODEX_OIDC_ORIGIN__", authority.origin).trimEnd() + "\n";
  requireValue(source.data["security-headers.conf"].trimEnd() + "\n" === headers &&
    source.data["public-assets.conf"].trimEnd() + "\n" === publicAssets.trimEnd() + "\n", "FRONTEND_RUNTIME_POLICY_DRIFT");
  const withoutRevision = structuredClone(config); delete withoutRevision.revision;
  const revision = createHash("sha256").update(JSON.stringify(withoutRevision)).update("\0").update(headers).digest("hex");
  const data = { "runtime-config.json": JSON.stringify({ revision, ...withoutRevision }) + "\n", "security-headers.conf": headers,
    "public-assets.conf": publicAssets.trimEnd() + "\n",
    "readiness.json": JSON.stringify({ status: "ready", runtimeConfigRevision: revision, tlsMode: "mutual-verified" }) + "\n" };
  const name = `staff-control-center-runtime-stable-${fingerprint(data).slice(0, 16)}`;
  return { name, revision, source: { name: source.metadata.name, uid: source.metadata.uid,
    resourceVersion: source.metadata.resourceVersion, dataSHA256: fingerprint(source.data) },
    resource: { apiVersion: "v1", kind: "ConfigMap", metadata: { name, namespace,
      labels: { "app.kubernetes.io/name": frontend, "app.kubernetes.io/managed-by": "kodex-stable-release" } }, immutable: true, data } };
}

function identity(resource, kind, name) {
  requireValue(resource?.kind === kind && resource.metadata?.namespace === namespace && resource.metadata.name === name &&
    resource.metadata.uid && resource.metadata.resourceVersion && resource.spec, "FRONTEND_ROUTE_IDENTITY_INVALID");
}
function change(resource, afterSpec, annotation) {
  const beforeAnnotations = resource.metadata.annotations ?? {};
  const annotations = annotation ? { ...beforeAnnotations, [frontendTransportAnnotation]: annotation } : beforeAnnotations;
  return { kind: resource.kind, name: resource.metadata.name, uid: resource.metadata.uid,
    beforeSpecSHA256: fingerprint(resource.spec), afterSpecSHA256: fingerprint(afterSpec),
    beforeAnnotationsSHA256: fingerprint(beforeAnnotations), afterAnnotationsSHA256: fingerprint(annotations),
    inverse: reverseSpecPatch(resource.spec, afterSpec),
    ...(annotation ? { annotationBefore: beforeAnnotations[frontendTransportAnnotation] ?? null, annotationAfter: annotation } : {}),
    patch: [{ op: "test", path: "/metadata/uid", value: resource.metadata.uid },
      { op: "test", path: "/metadata/resourceVersion", value: resource.metadata.resourceVersion },
      { op: "test", path: "/spec", value: resource.spec },
      ...(annotation ? [{ op: "add", path: "/metadata/annotations", value: annotations }] : []),
      { op: "replace", path: "/spec", value: afterSpec }] };
}

export function frontendRouting(service, ingresses, ingressPolicy, transport) {
  identity(service, "Service", frontend);
  identity(ingressPolicy, "NetworkPolicy", "staff-control-center-ingress");
  const selector = { "app.kubernetes.io/component": "staff-frontend", "app.kubernetes.io/name": frontend };
  requireValue(equal(service.spec.selector, selector) && (service.spec.type ?? "ClusterIP") === "ClusterIP" &&
    !service.spec.externalIPs && !service.spec.externalName && equal(service.spec.ports, [{ name: "http", port: 8080, protocol: "TCP", targetPort: "http" }]) &&
    !service.metadata.annotations?.[frontendTransportAnnotation], "FRONTEND_SOURCE_SERVICE_UNSUPPORTED");
  requireValue(transport?.apiVersion === "traefik.io/v1alpha1" && transport.kind === "ServersTransport" && transport.metadata?.name === frontend &&
    transport.spec?.serverName === "staff-control-center.kodex-system.svc.cluster.local" && transport.spec.insecureSkipVerify === false &&
    equal(transport.spec.rootCAs, [{ secret: "staff-control-center-backend-tls" }]) &&
    equal(transport.spec.certificatesSecrets, ["staff-control-center-ingress-client-tls"]) &&
    keys(transport.spec, ["serverName", "insecureSkipVerify", "rootCAs", "certificatesSecrets"]), "FRONTEND_DESIRED_TRANSPORT_INVALID");
  requireValue(equal(ingressPolicy.spec.podSelector?.matchLabels, selector) && equal(ingressPolicy.spec.policyTypes, ["Ingress"]) &&
    ingressPolicy.spec.ingress?.length === 1 && equal(ingressPolicy.spec.ingress[0].ports, [{ port: 8080, protocol: "TCP" }]) &&
    ingressPolicy.spec.ingress[0].from?.length === 1, "FRONTEND_SOURCE_NETWORK_POLICY_UNSUPPORTED");
  const peer = ingressPolicy.spec.ingress[0].from[0];
  requireValue(keys(peer, ["namespaceSelector", "podSelector"]) &&
    keys(peer.namespaceSelector, ["matchLabels"]) && keys(peer.namespaceSelector.matchLabels, ["kubernetes.io/metadata.name"]) &&
    keys(peer.podSelector, ["matchLabels"]) && keys(peer.podSelector.matchLabels, ["app.kubernetes.io/name"]) &&
    /^[a-z0-9-]+$/.test(peer.namespaceSelector.matchLabels["kubernetes.io/metadata.name"]) &&
    /^[a-z0-9-]+$/.test(peer.podSelector.matchLabels["app.kubernetes.io/name"]), "FRONTEND_INGRESS_PEER_NOT_EXACT");
  const serviceAfter = structuredClone(service.spec);
  serviceAfter.ports = [{ name: "https", port: 8443, protocol: "TCP", targetPort: "https" }];
  const policyAfter = structuredClone(ingressPolicy.spec);
  policyAfter.ingress[0].ports = [{ port: 8443, protocol: "TCP" }];
  const changes = [change(ingressPolicy, policyAfter), change(service, serviceAfter, "kodex-system-staff-control-center@kubernetescrd")];
  requireValue(Array.isArray(ingresses) && ingresses.length === 3 && new Set(ingresses.map(i => i.metadata?.name)).size === 3,
    "FRONTEND_INGRESS_SET_INVALID");
  let host;
  for (const name of [frontend, "staff-control-center-api", "staff-control-center-public-assets"]) {
    const ingress = ingresses.find(i => i.metadata?.name === name); identity(ingress, "Ingress", name);
    const rule = ingress.spec.rules?.[0], tls = ingress.spec.tls?.[0];
    requireValue(ingress.spec.rules.length === 1 && ingress.spec.tls?.length === 1 && rule.host &&
      tls.secretName === "staff-control-center-public-tls" && equal(tls.hosts, [rule.host]) &&
      (!host || host === rule.host), "FRONTEND_INGRESS_TLS_BOUNDARY_INVALID");
    host = rule.host;
    const middleware = ingress.metadata.annotations?.["traefik.ingress.kubernetes.io/router.middlewares"];
    requireValue(ingress.metadata.annotations?.["traefik.ingress.kubernetes.io/router.tls"] === "true" &&
      ingress.metadata.annotations["traefik.ingress.kubernetes.io/router.entrypoints"] === "websecure", "FRONTEND_INGRESS_TLS_BOUNDARY_INVALID");
    const paths = rule.http?.paths;
    if (name === frontend) requireValue(["kodex-system-oauth2-control-center-chain@kubernetescrd",
      "kodex-system-oauth2-control-center-chain@kubernetescrd,kodex-system-staff-control-center-retry@kubernetescrd"].includes(middleware) &&
      paths?.length === 1 && paths[0].path === "/" && paths[0].pathType === "Prefix", "FRONTEND_SSO_BOUNDARY_INVALID");
    else if (name.endsWith("-api")) requireValue(middleware === "kodex-system-oauth2-control-center-auth@kubernetescrd" &&
      paths?.length === 1 && paths[0].path === "/api/v1" && paths[0].pathType === "Prefix", "FRONTEND_SSO_BOUNDARY_INVALID");
    else requireValue(!middleware && paths?.length === 3 && paths.every(p => p.pathType === "Exact") &&
      equal(paths.map(p => p.path).sort(), ["/logo.png", "/manifest.webmanifest", "/sw.js"]), "FRONTEND_PUBLIC_PATHS_INVALID");
    const after = structuredClone(ingress.spec);
    for (const path of after.rules[0].http.paths) {
      const expected = name.endsWith("-api") ? { name: "control-api-gateway", port: { name: "https" } } : { name: frontend, port: { name: "http" } };
      requireValue(equal(path.backend?.service, expected) && !path.backend.resource, "FRONTEND_SOURCE_BACKEND_UNSUPPORTED");
      path.backend.service = { name: frontend, port: { name: "https" } };
    }
    changes.push(change(ingress, after));
  }
  return changes;
}
