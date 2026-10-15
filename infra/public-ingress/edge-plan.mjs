#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { mkdir, open } from "node:fs/promises";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { X509Certificate } from "node:crypto";
import { renderPublicEdge } from "./edge-model.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const execute = promisify(execFile);
const managedObjects = new Set(["Service/kodex-public-edge", "EndpointSlice/kodex-public-edge-0",
  "EndpointSlice/kodex-public-edge-1", "IngressRoute/kodex-public-edge-tls", "ServersTransport/kodex-public-edge-upstream"]);
export const edgeCollections = "deployments,services,endpointslices,ingresses,ingressroutes.traefik.io,ingressroutetcps.traefik.io,middlewares.traefik.io,serverstransports.traefik.io";
function requireValue(value, code) { if (!value) throw Error(code); }

export async function readPrivateJSON(path) {
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    requireValue(stat.isFile() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o600 &&
      stat.nlink === 1 && stat.size > 0 && stat.size <= 1 << 20, "EDGE_PRIVATE_FILE_REQUIRED");
    return JSON.parse(await file.readFile("utf8"));
  } finally { await file.close(); }
}

export function protectedCommitment(resources) {
  requireValue(Array.isArray(resources), "EDGE_BASELINE_INVALID");
  const rows = resources.filter(r => !(managedObjects.has(`${r.kind}/${r.metadata?.name}`) &&
    r.metadata?.labels?.["app.kubernetes.io/managed-by"] === "kodex-public-edge"))
    .map(r => {
      requireValue(r.metadata?.uid && r.kind && (r.spec || r.kind === "EndpointSlice"), "EDGE_BASELINE_INVALID");
      const content = r.kind === "EndpointSlice" ? { addressType: r.addressType, endpoints: r.endpoints, ports: r.ports } : r.spec;
      return { kind: r.kind, name: r.metadata.name, namespace: r.metadata.namespace, uid: r.metadata.uid,
        contentSHA256: fingerprint({ content, annotations: r.metadata.annotations ?? {}, labels: r.metadata.labels ?? {} }) };
    }).sort((a, b) => `${a.kind}/${a.name}`.localeCompare(`${b.kind}/${b.name}`, "en"));
  requireValue(rows.some(r => r.kind === "Deployment" && r.name === "traefik"), "EDGE_INGRESS_REQUIRED");
  return fingerprint(rows);
}

export function publicCAProjection(configMap, namespace) {
  requireValue(typeof namespace === "string" && /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/.test(namespace) &&
    configMap?.kind === "ConfigMap" && configMap.metadata?.namespace === "kodex-system" &&
    configMap.metadata.name === "kodex-oidc-ca" && configMap.metadata.uid &&
    typeof configMap.data?.["ca.pem"] === "string" && configMap.data["ca.pem"].length <= 65536 &&
    !configMap.data["ca.pem"].includes("PRIVATE KEY"), "EDGE_SOURCE_CA_INVALID");
  const pem = configMap.data["ca.pem"];
  requireValue(/^\s*-----BEGIN CERTIFICATE-----[A-Za-z0-9+/=\r\n]+-----END CERTIFICATE-----\s*$/.test(pem),
    "EDGE_SOURCE_CA_INVALID");
  let certificate;
  try { certificate = new X509Certificate(pem); } catch { throw Error("EDGE_SOURCE_CA_INVALID"); }
  requireValue(certificate.ca && Date.parse(certificate.validTo) > Date.now() + 86400000 &&
    Date.parse(certificate.validFrom) <= Date.now(), "EDGE_SOURCE_CA_INVALID");
  return { apiVersion: "v1", kind: "Secret", metadata: { name: "kodex-public-edge-upstream-ca", namespace,
    labels: { "app.kubernetes.io/managed-by": "kodex-public-edge", "kodex.dev/capability": "public-edge" } },
  type: "Opaque", data: { "ca.crt": Buffer.from(pem).toString("base64") } };
}

// Apply обязан заново рендерить намерение, а не доверять файлам рядом с планом.
// CA отдельно связывается с exact source cluster/ConfigMap UID.
export function validateEdgeArtifacts(plan, bundle, units) {
  requireValue(plan?.schema === "kodex.public-edge-plan/v2" && typeof plan.clusterUID === "string" && plan.clusterUID &&
    typeof plan.namespaceUID === "string" && plan.namespaceUID && typeof plan.source?.clusterUID === "string" &&
    plan.source.clusterUID && plan.source.clusterUID !== plan.clusterUID && typeof plan.source.caUID === "string" &&
    plan.source.caUID, "EDGE_PLAN_IDENTITY_INVALID");
  const rendered = renderPublicEdge(plan.config);
  requireValue(rendered.contentSHA256 === plan.contentSHA256 && fingerprint(units) === fingerprint(rendered.units) &&
    bundle?.apiVersion === "v1" && bundle.kind === "List" && Array.isArray(bundle.items) && bundle.items.length === 6,
    "EDGE_ARTIFACT_MISMATCH");
  const ca = bundle.items[0];
  requireValue(typeof ca?.data?.["ca.crt"] === "string", "EDGE_SOURCE_CA_INVALID");
  const pem = Buffer.from(ca.data["ca.crt"], "base64").toString("utf8");
  const expectedCA = publicCAProjection({ kind: "ConfigMap", metadata: { namespace: "kodex-system",
    name: "kodex-oidc-ca", uid: plan.source.caUID }, data: { "ca.pem": pem } }, plan.config.namespace);
  requireValue(fingerprint(ca) === plan.source.caProjectionSHA256 && fingerprint(ca) === fingerprint(expectedCA) &&
    fingerprint(bundle.items.slice(1)) === fingerprint(rendered.resources), "EDGE_ARTIFACT_MISMATCH");
  return { resources: bundle.items, units: rendered.units };
}

async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(value); await file.sync(); } finally { await file.close(); }
}

async function main(args) {
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--config", "--context", "--output", "--kodex-context", "--kodex-kubeconfig", "--edge-node"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "EDGE_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context ?? "") && options["--config"] && options["--output"],
    "EDGE_ARGUMENTS_INVALID");
  requireValue(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(options["--kodex-context"] ?? "") &&
    options["--kodex-kubeconfig"]?.startsWith("/"), "EDGE_SOURCE_CONTEXT_REQUIRED");
  const config = await readPrivateJSON(resolve(options["--config"]));
  const rendered = renderPublicEdge(config);
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  let edgeNode;
  if (options["--edge-node"]) {
    requireValue(/^k3d-[a-z0-9-]+-server-[0-9]+$/.test(options["--edge-node"]), "EDGE_NODE_IDENTITY_INVALID");
    const node = JSON.parse((await execute("docker", ["inspect", options["--edge-node"]], { env, timeout: 15000, maxBuffer: 1 << 20 })).stdout)[0];
    requireValue(node?.State?.Running && node.Config?.Labels?.["k3d.cluster"] && node.Config.Labels["k3d.cluster"] !== "kodex" &&
      node.Config.Labels["k3d.role"] === "server" && /^[a-f0-9]{64}$/.test(node.Id ?? ""), "EDGE_NODE_IDENTITY_INVALID");
    edgeNode = { name: options["--edge-node"], id: node.Id };
  }
  const get = async arguments_ => {
    const { stdout } = await execute(edgeNode ? "docker" : "kubectl", edgeNode ?
      ["exec", edgeNode.name, "kubectl", "--request-timeout=15s", ...arguments_, "-o", "json"] :
      ["--context", context, "--request-timeout=15s", ...arguments_, "-o", "json"],
      { env, timeout: 20000, maxBuffer: 8 << 20 });
    return JSON.parse(stdout);
  };
  const cluster = await get(["get", "namespace", "kube-system"]);
  const existing = await get(["-n", config.namespace, "get", edgeCollections]);
  requireValue(cluster.metadata.uid && Array.isArray(existing.items), "EDGE_CLUSTER_IDENTITY_INVALID");
  const ingress = existing.items.find(r => r.kind === "Deployment" && r.metadata.name === "traefik");
  requireValue(ingress?.spec.template.spec.containers.some(c => (c.args ?? []).some(arg =>
    arg.startsWith(`--certificatesresolvers.${config.certResolver}.acme.`))), "EDGE_EXISTING_ACME_RESOLVER_REQUIRED");
  // Начальный plan создаёт только новые ресурсы. Recovery продолжает прежний
  // журнал, а не принимает существующий объект по одному managed-by label.
  requireValue(!existing.items.some(r => managedObjects.has(`${r.kind}/${r.metadata.name}`)), "EDGE_NAME_COLLISION");
  const caArgs = ["--request-timeout=15s", "-n", config.namespace, "get", "secret", "kodex-public-edge-upstream-ca", "--ignore-not-found", "-o", "name"];
  const caName = await execute(edgeNode ? "docker" : "kubectl", edgeNode ? ["exec", edgeNode.name, "kubectl", ...caArgs] : ["--context", context, ...caArgs],
  { env, timeout: 20000, maxBuffer: 4096 });
  requireValue(caName.stdout.trim() === "", "EDGE_NAME_COLLISION");
  const namespace = await get(["get", "namespace", config.namespace]);
  const nodes = await get(["get", "nodes"]);
  const peers = [];
  for (const node of nodes.items ?? []) {
    const name = node.metadata?.name;
    requireValue(typeof name === "string" && /^k3d-[a-z0-9-]+$/.test(name), "EDGE_LOCAL_NODE_REQUIRED");
    const { stdout } = await execute("docker", ["inspect", name], { env, timeout: 15000, maxBuffer: 1 << 20 });
    const container = JSON.parse(stdout)[0];
    requireValue(container?.State?.Running && /^[a-f0-9]{64}$/.test(container.Id ?? ""), "EDGE_NODE_IDENTITY_INVALID");
    const addresses = Object.values(container.NetworkSettings?.Networks ?? {}).map(n => n.IPAddress);
    for (const peerIP of config.allowedPeers.filter(ip => addresses.includes(ip))) peers.push({ name, id: container.Id, peerIP });
  }
  requireValue(peers.length === config.allowedPeers.length && new Set(peers.map(p => p.peerIP)).size === peers.length,
    "EDGE_PEER_NODE_BINDING_INVALID");
  const sourceFile = await open(options["--kodex-kubeconfig"], constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await sourceFile.stat();
    requireValue(stat.isFile() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o600 && stat.nlink === 1,
      "EDGE_PRIVATE_KUBECONFIG_REQUIRED");
  } finally { await sourceFile.close(); }
  const sourceGet = async arguments_ => {
    const { stdout } = await execute("kubectl", ["--kubeconfig", options["--kodex-kubeconfig"], "--context",
      options["--kodex-context"], "--request-timeout=15s", ...arguments_, "-o", "json"], { env, timeout: 20000, maxBuffer: 1 << 20 });
    return JSON.parse(stdout);
  };
  const sourceCluster = await sourceGet(["get", "namespace", "kube-system"]);
  requireValue(typeof sourceCluster.metadata?.uid === "string" && sourceCluster.metadata.uid &&
    sourceCluster.metadata.uid !== cluster.metadata.uid, "EDGE_SEPARATE_CLUSTERS_REQUIRED");
  const sourceCA = await sourceGet(["-n", "kodex-system", "get", "configmap", "kodex-oidc-ca"]);
  const caProjection = publicCAProjection(sourceCA, config.namespace);
  rendered.resources.unshift(caProjection);
  // Создание плана не устанавливает units, не меняет Docker networks или ingress.
  const directory = resolve(options["--output"]);
  await mkdir(directory, { mode: 0o700 });
  const plan = { schema: "kodex.public-edge-plan/v2", context, clusterUID: cluster.metadata.uid,
    ...(edgeNode ? { edgeNode } : {}),
    namespaceUID: namespace.metadata.uid, config, contentSHA256: rendered.contentSHA256,
    protectedSHA256: protectedCommitment(existing.items), peers, source: { context: options["--kodex-context"],
      kubeconfig: options["--kodex-kubeconfig"], clusterUID: sourceCluster.metadata.uid,
      caUID: sourceCA.metadata.uid, caProjectionSHA256: fingerprint(caProjection) } };
  validateEdgeArtifacts(plan, { apiVersion: "v1", kind: "List", items: rendered.resources }, rendered.units);
  await createPrivate(join(directory, "plan.json"), JSON.stringify(plan) + "\n");
  await createPrivate(join(directory, "resources.json"), JSON.stringify({ apiVersion: "v1", kind: "List", items: rendered.resources }) + "\n");
  for (const [name, value] of Object.entries(rendered.units)) await createPrivate(join(directory, name), value);
  process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", resources: rendered.resources.length,
    units: Object.keys(rendered.units).length, planSHA256: fingerprint(plan) }) + "\n");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main(process.argv.slice(2)).catch(error => {
    const code = /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "EDGE_PLAN_FAILED";
    process.stderr.write(JSON.stringify({ status: "FAIL", code }) + "\n"); process.exitCode = 1;
  });
