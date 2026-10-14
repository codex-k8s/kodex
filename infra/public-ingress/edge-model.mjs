import { isIP } from "node:net";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

function requireValue(value, code) { if (!value) throw Error(code); }
function privateIPv4(ip) {
  if (isIP(ip) !== 4) return false;
  const [a, b] = ip.split(".").map(Number);
  return a === 10 || a === 172 && b >= 16 && b <= 31 || a === 192 && b === 168;
}
function dnsName(value) {
  return typeof value === "string" && value.length <= 253 && value.includes(".") &&
    value.split(".").every(s => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(s));
}

// Рендерится только добавочный Host/SNI route. Действующие ingress/80/443,
// чужие Secrets и firewall rules не входят в набор управляемых ресурсов.
export function renderPublicEdge(config) {
  const { platformHost, oidcHost, namespace, bridgeIP, allowedPeers, backendIP, relayPorts,
    upstreamServerName, certResolver } = config ?? {};
  requireValue(dnsName(platformHost) && dnsName(oidcHost) && platformHost !== oidcHost,
    "EDGE_EXACT_HOSTS_REQUIRED");
  requireValue(typeof namespace === "string" && /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/.test(namespace),
    "EDGE_NAMESPACE_INVALID");
  requireValue(privateIPv4(bridgeIP) && isIP(backendIP) === 4 && backendIP.startsWith("127.") &&
    backendIP !== "127.0.0.0", "EDGE_PRIVATE_BIND_REQUIRED");
  requireValue(Array.isArray(allowedPeers) && allowedPeers.length > 0 && allowedPeers.length <= 8 &&
    allowedPeers.every(privateIPv4) && new Set(allowedPeers).size === allowedPeers.length &&
    !allowedPeers.includes(bridgeIP), "EDGE_EXACT_PEERS_REQUIRED");
  requireValue(Array.isArray(relayPorts) && relayPorts.length === 2 && new Set(relayPorts).size === 2 &&
    relayPorts.every(port => Number.isSafeInteger(port) && port >= 1024 && port <= 65535), "EDGE_PRIVATE_PORTS_REQUIRED");
  requireValue(dnsName(upstreamServerName) && typeof certResolver === "string" &&
    /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/.test(certResolver), "EDGE_TLS_BOUNDARY_REQUIRED");
  const labels = { "app.kubernetes.io/managed-by": "kodex-public-edge", "kodex.dev/capability": "public-edge" };
  const meta = name => ({ name, namespace, labels });
  const service = "kodex-public-edge";
  const hostRule = `Host(\`${platformHost}\`) || Host(\`${oidcHost}\`)`;
  const resources = [
    { apiVersion: "v1", kind: "Service", metadata: meta(service), spec: { ports: [
      { name: "https", protocol: "TCP", port: 443, targetPort: "https" },
    ] } },
    ...relayPorts.map((port, index) => ({ apiVersion: "discovery.k8s.io/v1", kind: "EndpointSlice", metadata: { ...meta(`${service}-${index}`), labels: {
      ...labels, "kubernetes.io/service-name": service, "endpointslice.kubernetes.io/managed-by": "kodex-public-edge" } },
    addressType: "IPv4", endpoints: [{ addresses: [bridgeIP], conditions: { ready: true } }],
    ports: [{ name: "https", protocol: "TCP", port }] })),
    { apiVersion: "traefik.io/v1alpha1", kind: "ServersTransport", metadata: meta("kodex-public-edge-upstream"),
      spec: { serverName: upstreamServerName, insecureSkipVerify: false,
        rootCAsSecrets: ["kodex-public-edge-upstream-ca"],
        forwardingTimeouts: { dialTimeout: "5s", responseHeaderTimeout: "30s", idleConnTimeout: "90s" } } },
    // ACME остаётся у существующего внешнего ingress. Его глобальный redirect,
    // challenge handler и certificate storage не меняются. Private keys не
    // копируются между кластерами; вверх передаётся только публичная CA.
    { apiVersion: "traefik.io/v1alpha1", kind: "IngressRoute", metadata: meta("kodex-public-edge-tls"),
      spec: { entryPoints: ["websecure"], routes: [{ kind: "Rule", priority: 10000, match: hostRule,
        services: [{ name: service, port: "https", scheme: "https", passHostHeader: true,
          serversTransport: "kodex-public-edge-upstream" }],
      }], tls: { certResolver, domains: [{ main: platformHost, sans: [oidcHost] }] } } },
  ];
  const units = {};
  for (const [index, port] of relayPorts.entries()) {
    const protocol = `https-${index}`, upstream = 443;
    const name = `kodex-public-edge-${protocol}`;
    const peerRules = allowedPeers.map(ip => `IPAddressAllow=${ip}/32`).join("\n");
    units[`${name}.socket`] = `[Unit]\nDescription=Kodex exact public edge ${protocol} listener\nAfter=network-online.target docker.service\nWants=network-online.target\n\n[Socket]\nListenStream=${bridgeIP}:${port}\nAccept=no\nIPAddressDeny=any\n${peerRules}\n\n[Install]\nWantedBy=sockets.target\n`;
    units[`${name}.service`] = `[Unit]\nDescription=Kodex isolated ${protocol} stream relay\nRequires=${name}.socket\nAfter=network-online.target docker.service\n\n[Service]\nExecStart=/usr/lib/systemd/systemd-socket-proxyd --connections-max=2048 ${backendIP}:${upstream}\nDynamicUser=yes\nNoNewPrivileges=yes\nProtectSystem=strict\nProtectHome=yes\nPrivateTmp=yes\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX\nIPAddressDeny=any\n${peerRules}\nIPAddressAllow=${backendIP}/32\nRestart=on-failure\n`;
  }
  return { schema: "kodex.public-edge/v1", resources, units,
    contentSHA256: fingerprint({ config, resources, units }) };
}
