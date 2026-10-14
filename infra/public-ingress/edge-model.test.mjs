import assert from "node:assert/strict";
import test from "node:test";
import { renderPublicEdge } from "./edge-model.mjs";
const fixture = { platformHost: "platform.example.test", oidcHost: "sso.example.test", namespace: "kube-system",
  bridgeIP: "172.20.0.1", allowedPeers: ["172.20.0.2", "172.20.0.3"], backendIP: "127.0.0.2", httpsPort: 18443,
  upstreamServerName: "upstream.example.test", certResolver: "public" };

test("exact public TLS routing re-encrypts upstream and preserves ingress ownership", () => {
  const input = structuredClone(fixture), plan = renderPublicEdge(input);
  assert.deepEqual(input, fixture);
  assert.equal(plan.resources.length, 4);
  assert.equal(plan.resources.some(r => ["Deployment", "Secret", "Ingress", "ClusterRole"].includes(r.kind)), false);
  const tls = plan.resources.find(r => r.kind === "IngressRoute");
  assert.equal(tls.spec.tls.certResolver, "public");
  assert.deepEqual(tls.spec.tls.domains, [{ main: fixture.platformHost, sans: [fixture.oidcHost] }]);
  assert.equal(tls.spec.routes[0].match.includes("*"), false);
  assert.equal(tls.spec.routes[0].services[0].scheme, "https");
  const transport = plan.resources.find(r => r.kind === "ServersTransport");
  assert.equal(transport.spec.insecureSkipVerify, false);
  assert.equal(transport.spec.serverName, fixture.upstreamServerName);
  assert.equal(Object.keys(plan.units).length, 2);
  assert.equal(plan.units["kodex-public-edge-https.socket"].includes("ListenStream=172.20.0.1:18443"), true);
  assert.equal(plan.units["kodex-public-edge-https.socket"].includes("IPAddressDeny=any"), true);
  assert.equal(plan.units["kodex-public-edge-https.service"].includes("127.0.0.2:443"), true);
});

test("public binds wildcard hosts shell injection broad peers and public ports are rejected", () => {
  for (const patch of [{ bridgeIP: "0.0.0.0" }, { bridgeIP: "203.0.113.1" }, { backendIP: "10.1.1.1" },
    { platformHost: "*.example.test" }, { oidcHost: "sso.example.test\nExecStart=bad" },
    { allowedPeers: ["172.20.0.0/16"] }, { allowedPeers: [] }, { httpsPort: 443 },
    { upstreamServerName: "*.example.test" }, { certResolver: "public\nExecStart=bad" }])
    assert.throws(() => renderPublicEdge({ ...fixture, ...patch }));
});

test("all managed objects are bounded and the commitment changes on upstream drift", () => {
  const plan = renderPublicEdge(fixture);
  assert.equal(plan.resources.every(r => r.metadata.namespace === fixture.namespace &&
    r.metadata.name.startsWith("kodex-public-edge")), true);
  assert.notEqual(plan.contentSHA256, renderPublicEdge({ ...fixture, backendIP: "127.0.0.3" }).contentSHA256);
});
