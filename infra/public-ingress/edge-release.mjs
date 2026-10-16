#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { lstat, open } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { edgeCollections, protectedCommitment, publicCAProjection, readPrivateJSON, validateEdgeArtifacts } from "./edge-plan.mjs";
import { renderPublicEdge } from "./edge-model.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const execute = promisify(execFile);
const resources = new Map([
  ["Secret", ["v1", "secrets"]], ["Service", ["v1", "services"]],
  ["EndpointSlice", ["discovery.k8s.io/v1", "endpointslices"]],
  ["ServersTransport", ["traefik.io/v1alpha1", "serverstransports"]],
  ["IngressRoute", ["traefik.io/v1alpha1", "ingressroutes"]],
]);
function requireValue(value, code) { if (!value) throw Error(code); }

export function ownedResourcePath(resource) {
  const mapping = resources.get(resource?.kind);
  requireValue(mapping && mapping[0] === resource.apiVersion &&
    /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/.test(resource.metadata?.namespace ?? "") &&
    /^kodex-public-edge(?:-[a-z0-9-]+)?$/.test(resource.metadata?.name ?? ""), "EDGE_RESOURCE_IDENTITY_INVALID");
  const prefix = mapping[0] === "v1" ? "/api/v1" : `/apis/${mapping[0]}`;
  return `${prefix}/namespaces/${resource.metadata.namespace}/${mapping[1]}/${resource.metadata.name}`;
}

// API defaults допускаются, но все явно заданные поля и длины массивов точны.
export function matchesDesired(current, desired) {
  if (desired === null || typeof desired !== "object") return current === desired;
  if (Array.isArray(desired)) return Array.isArray(current) && current.length === desired.length &&
    desired.every((value, index) => matchesDesired(current[index], value));
  return current !== null && typeof current === "object" && !Array.isArray(current) &&
    Object.entries(desired).every(([key, value]) => Object.hasOwn(current, key) && matchesDesired(current[key], value));
}

export function resourceIntent(resource, planSHA256) {
  ownedResourcePath(resource);
  requireValue(/^[a-f0-9]{64}$/.test(planSHA256 ?? ""), "EDGE_PLAN_HASH_INVALID");
  const desired = structuredClone(resource);
  desired.metadata.annotations = { ...desired.metadata.annotations, "kodex.dev/public-edge-plan": planSHA256 };
  return desired;
}

export function ownedResourceMatches(current, desired) {
  if (!matchesDesired(current, desired) || current.metadata?.deletionTimestamp ||
    current.metadata?.ownerReferences?.length || current.metadata?.finalizers?.length) return false;
  if (desired.kind === "Secret") return current.type === "Opaque" && fingerprint(current.data) === fingerprint(desired.data) &&
    !current.stringData && !current.immutable;
  if (desired.kind === "EndpointSlice") return fingerprint({ addressType: current.addressType, endpoints: current.endpoints,
    ports: current.ports }) === fingerprint({ addressType: desired.addressType, endpoints: desired.endpoints, ports: desired.ports });
  if (desired.kind !== "Service") return fingerprint(current.spec) === fingerprint(desired.spec);
  const spec = current.spec, copy = structuredClone(spec);
  // Только известные defaults ClusterIP, не selector/ExternalName/NodePort.
  if ((spec.type ?? "ClusterIP") !== "ClusterIP" || (spec.sessionAffinity ?? "None") !== "None" ||
    (spec.internalTrafficPolicy ?? "Cluster") !== "Cluster" || spec.ipFamilyPolicy && spec.ipFamilyPolicy !== "SingleStack" ||
    spec.ipFamilies && fingerprint(spec.ipFamilies) !== fingerprint(["IPv4"])) return false;
  for (const key of ["clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy", "internalTrafficPolicy", "sessionAffinity", "type"])
    delete copy[key];
  return fingerprint(copy) === fingerprint(desired.spec);
}

async function privateText(path) {
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 &&
      (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= 1 << 20, "EDGE_PRIVATE_FILE_REQUIRED");
    return await file.readFile("utf8");
  } finally { await file.close(); }
}

export async function releasePublicEdge(args, hostFilesystem = { lstat, open }) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--plan", "--journal", "--output", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "EDGE_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  requireValue(["apply", "readback", "recover", "rollback"].includes(mode) && options["--plan"] && options["--output"] &&
    /^[A-Za-z0-9_.:@/-]{1,160}$/.test(options["--context"] ?? ""), "EDGE_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(resolve(options["--plan"])), hash = fingerprint(plan);
  requireValue(plan.context === options["--context"] &&
    /^[A-Za-z0-9_.:@/-]{1,160}$/.test(plan.source?.context ?? "") &&
    typeof plan.source?.kubeconfig === "string" && plan.source.kubeconfig.startsWith("/"), "EDGE_PLAN_IDENTITY_INVALID");
  requireValue(mode === "readback" ? !options["--approved-plan-sha"] : options["--approved-plan-sha"] === hash,
    "EDGE_EXACT_OWNER_CONFIRMATION_REQUIRED");
  requireValue(mode === "apply" ? !options["--journal"] : Boolean(options["--journal"]), "EDGE_ARGUMENTS_INVALID");
  const directory = dirname(resolve(options["--plan"])), rendered = renderPublicEdge(plan.config), units = {};
  for (const name of Object.keys(rendered.units)) units[name] = await privateText(join(directory, name));
  const bundle = await readPrivateJSON(join(directory, "resources.json"));
  validateEdgeArtifacts(plan, bundle, units);
  const desired = bundle.items.map(resource => resourceIntent(resource, hash));
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = async (file, argv, input) => {
    try {
      const invocation = input === undefined ? execute(file, argv, { env, timeout: 35000, maxBuffer: 8 << 20 }) :
        execute("/bin/sh", ["-c", 'cat | "$@"', "kodex-edge-pipe", file, ...argv], { env, timeout: 35000, maxBuffer: 8 << 20 });
      if (input !== undefined) { invocation.child.stdin.on("error", () => {}); invocation.child.stdin.end(input); }
      return (await invocation).stdout;
    } catch { throw Error("EDGE_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const kube = (argv, input, source = false) => !source && plan.edgeNode ? run("docker", ["exec", "-i", plan.edgeNode.name, "kubectl", "--request-timeout=30s", ...argv], input) : run("kubectl", source ? ["--kubeconfig", plan.source.kubeconfig,
    "--context", plan.source.context, "--request-timeout=30s", ...argv] :
  ["--context", plan.context, "--request-timeout=30s", ...argv], input);
  const get = async (argv, source = false) => JSON.parse(await kube([...argv, "-o", "json"], undefined, source));
  const identities = async () => {
    if (plan.edgeNode) {
      requireValue(/^k3d-[a-z0-9-]+-server-[0-9]+$/.test(plan.edgeNode.name ?? "") && /^[a-f0-9]{64}$/.test(plan.edgeNode.id ?? ""), "EDGE_NODE_IDENTITY_INVALID");
      const node = JSON.parse(await run("docker", ["inspect", plan.edgeNode.name]))[0];
      requireValue(node?.Id === plan.edgeNode.id && node.State?.Running && node.Config?.Labels?.["k3d.cluster"] &&
        node.Config.Labels["k3d.cluster"] !== "kodex" && node.Config.Labels["k3d.role"] === "server", "EDGE_NODE_IDENTITY_INVALID");
    }
    const sourceFile = await open(plan.source.kubeconfig, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = await sourceFile.stat();
      requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 && (stat.mode & 0o777) === 0o600,
        "EDGE_PRIVATE_KUBECONFIG_REQUIRED");
    } finally { await sourceFile.close(); }
    const edge = await get(["get", "namespace", "kube-system"]), source = await get(["get", "namespace", "kube-system"], true);
    const namespace = await get(["get", "namespace", plan.config.namespace]);
    requireValue(edge.metadata.uid === plan.clusterUID && source.metadata.uid === plan.source.clusterUID &&
      namespace.metadata.uid === plan.namespaceUID, "EDGE_CLUSTER_DRIFT");
    const ca = await get(["-n", "kodex-system", "get", "configmap", "kodex-oidc-ca"], true);
    requireValue(ca.metadata.uid === plan.source.caUID && fingerprint(publicCAProjection(ca, plan.config.namespace)) ===
      plan.source.caProjectionSHA256, "EDGE_SOURCE_CA_DRIFT");
    requireValue(Array.isArray(plan.peers) && plan.peers.length === plan.config.allowedPeers.length &&
      new Set(plan.peers.map(peer => peer.peerIP)).size === plan.peers.length, "EDGE_PEER_NODE_BINDING_INVALID");
    const nodes = await get(["get", "nodes"]);
    requireValue(plan.peers.every(peer => nodes.items.some(node => node.metadata.name === peer.name) &&
      /^k3d-[a-z0-9-]+$/.test(peer.name) && plan.config.allowedPeers.includes(peer.peerIP)), "EDGE_PEER_NODE_BINDING_INVALID");
    for (const peer of plan.peers) {
      const container = JSON.parse(await run("docker", ["inspect", peer.name]))[0];
      requireValue(container?.Id === peer.id && container.State?.Running &&
        Object.values(container.NetworkSettings?.Networks ?? {}).some(network => network.IPAddress === peer.peerIP),
        "EDGE_PEER_NODE_DRIFT");
    }
  };
  const baseline = async () => {
    const existing = await get(["-n", plan.config.namespace, "get", edgeCollections]);
    requireValue(protectedCommitment(existing.items) === plan.protectedSHA256, "EDGE_PROTECTED_STATE_DRIFT");
  };
  const lookup = async resource => {
    const list = await kube(["-n", resource.metadata.namespace, "get", resources.get(resource.kind)[1],
      resource.metadata.name, "--ignore-not-found", "-o", "json"]);
    return list.trim() ? JSON.parse(list) : null;
  };
  const unitBody = name => `# Kodex public edge plan ${hash}\n${units[name]}`;
  const inspectUnit = async (name, optional = false) => {
    const path = `/etc/systemd/system/${name}`;
    let stat;
    try { stat = await hostFilesystem.lstat(path); } catch (error) { if (optional && error.code === "ENOENT") return null; throw error; }
    requireValue(stat.isFile() && !stat.isSymbolicLink() && stat.uid === 0 && stat.nlink === 1 &&
      (stat.mode & 0o777) === 0o644, "EDGE_INSTALLED_UNIT_IDENTITY_INVALID");
    const file = await hostFilesystem.open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const actual = await file.stat();
      requireValue(actual.dev === stat.dev && actual.ino === stat.ino && actual.size <= 16384 &&
        await file.readFile("utf8") === unitBody(name), "EDGE_INSTALLED_UNIT_DRIFT");
    } finally { await file.close(); }
    return path;
  };
  await identities();
  await baseline();
  let prior;
  if (mode !== "apply") {
    prior = (await privateText(options["--journal"])).trim().split("\n").map(line => JSON.parse(line));
    requireValue(prior[0]?.schema === "kodex.public-edge-apply/v1" && prior[0].planSHA256 === hash,
      "EDGE_JOURNAL_IDENTITY_INVALID");
  }
  // Новый журнал обязателен и для rollback; неизвестный outcome не повторяется.
  const journal = await open(options["--output"], "wx", 0o600);
  const record = async value => { await journal.writeFile(JSON.stringify(value) + "\n"); await journal.sync(); };
  try {
    await record({ schema: mode === "apply" ? "kodex.public-edge-apply/v1" : `kodex.public-edge-${mode}/v1`,
      planSHA256: hash, status: "IN_PROGRESS" });
    if (mode === "recover") {
      requireValue(prior.some(row => row.status === "PASS" && row.phase === "EDGE_RESOURCES_ONLY"),
        "EDGE_SUCCESSFUL_APPLY_REQUIRED");
      // Восстановление запускает только прежние exact socket units. До первого
      // эффекта проверяются все ресурсы, UID receipts и полные тела unit-файлов.
      for (const resource of desired) {
        const current = await lookup(resource);
        const receipt = prior.find(row => row.status === "RESOURCE_CREATED" && row.kind === resource.kind &&
          row.name === resource.metadata.name);
        requireValue(receipt && current?.metadata?.uid === receipt.uid && ownedResourceMatches(current, resource),
          "EDGE_RECOVERY_RESOURCE_DRIFT");
      }
      for (const name of Object.keys(units)) await inspectUnit(name);
      for (const name of Object.keys(units).filter(name => name.endsWith(".socket"))) {
        requireValue((await run("systemctl", ["is-enabled", name])).trim() === "enabled", "EDGE_SOCKET_NOT_ENABLED");
      }
      for (const name of Object.keys(units).filter(name => name.endsWith(".socket"))) {
        await identities(); await baseline(); await inspectUnit(name);
        await record({ status: "SOCKET_START_ATTEMPT", name });
        await run("sudo", ["-n", "systemctl", "start", name]);
        await record({ status: "SOCKET_STARTED", name });
      }
    }
    if (mode === "apply") {
      for (const resource of desired) requireValue(await lookup(resource) === null, "EDGE_NAME_COLLISION");
      for (const name of Object.keys(units)) {
        requireValue(await inspectUnit(name, true) === null, "EDGE_UNIT_COLLISION");
        requireValue((await run("systemctl", ["show", name, "--property=LoadState", "--value"])).trim() === "not-found",
          "EDGE_UNIT_COLLISION");
      }
      for (const name of Object.keys(units)) {
        await baseline();
        await record({ status: "UNIT_CREATE_ATTEMPT", name });
        // noclobber запрещает перезаписать чужой unit даже после preflight.
        await run("sudo", ["-n", "/bin/sh", "-c", 'set -eC; umask 022; /usr/bin/cat > "$1"; /usr/bin/sync -f "$1"',
          "kodex-edge-exclusive-unit", `/etc/systemd/system/${name}`], unitBody(name));
        await inspectUnit(name);
        await record({ status: "UNIT_CREATED", name });
      }
      await run("sudo", ["-n", "systemctl", "daemon-reload"]);
      for (const name of Object.keys(units).filter(name => name.endsWith(".socket"))) {
        await record({ status: "SOCKET_ENABLE_ATTEMPT", name });
        await run("sudo", ["-n", "systemctl", "enable", "--now", name]);
      }
      for (const resource of desired) {
        await baseline();
        if (["Secret", "IngressRoute"].includes(resource.kind)) await identities();
        await record({ status: "RESOURCE_CREATE_ATTEMPT", kind: resource.kind, name: resource.metadata.name });
        const created = JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(resource)));
        requireValue(created.metadata?.uid && ownedResourceMatches(created, resource), "EDGE_RESOURCE_READBACK_MISMATCH");
        await record({ status: "RESOURCE_CREATED", kind: resource.kind, name: resource.metadata.name, uid: created.metadata.uid });
      }
    }
    if (mode === "rollback") {
      // Полный read-only preflight до первого удаления, затем свежие UID/RV
      // preconditions для каждого DELETE. Чужая замена не удаляется.
      for (const resource of desired) {
        if (!prior.some(row => row.status === "RESOURCE_CREATE_ATTEMPT" && row.kind === resource.kind &&
          row.name === resource.metadata.name)) continue;
        const current = await lookup(resource);
        if (!current) continue;
        const receipt = prior.find(row => row.status === "RESOURCE_CREATED" && row.kind === resource.kind && row.name === resource.metadata.name);
        requireValue(current.metadata?.uid && (!receipt || receipt.uid === current.metadata.uid) &&
          ownedResourceMatches(current, resource), "EDGE_ROLLBACK_RESOURCE_DRIFT");
      }
      for (const name of Object.keys(units))
        if (prior.some(row => row.status === "UNIT_CREATE_ATTEMPT" && row.name === name)) await inspectUnit(name, true);
      for (const resource of [...desired].reverse()) {
        const attempt = prior.find(row => row.status === "RESOURCE_CREATE_ATTEMPT" &&
          row.kind === resource.kind && row.name === resource.metadata.name);
        if (!attempt) continue;
        const current = await lookup(resource);
        if (!current) continue;
        const receipt = prior.find(row => row.status === "RESOURCE_CREATED" && row.kind === resource.kind && row.name === resource.metadata.name);
        requireValue(current.metadata?.uid && (!receipt || receipt.uid === current.metadata.uid) &&
          ownedResourceMatches(current, resource), "EDGE_ROLLBACK_RESOURCE_DRIFT");
        await record({ status: "RESOURCE_DELETE_ATTEMPT", kind: resource.kind, name: resource.metadata.name, uid: current.metadata.uid });
        await kube(["delete", "--raw", ownedResourcePath(resource), "-f", "/dev/stdin"], JSON.stringify({
          apiVersion: "v1", kind: "DeleteOptions", preconditions: { uid: current.metadata.uid, resourceVersion: current.metadata.resourceVersion } }));
        requireValue(await lookup(resource) === null, "EDGE_ROLLBACK_DELETE_PENDING");
      }
      for (const name of Object.keys(units).filter(name => name.endsWith(".socket"))) {
        if (!prior.some(row => row.status === "UNIT_CREATE_ATTEMPT" && row.name === name)) continue;
        if (await inspectUnit(name, true) === null) continue;
        await run("sudo", ["-n", "systemctl", "disable", "--now", name]);
        const service = name.replace(/\.socket$/, ".service");
        if (await inspectUnit(service, true)) await run("sudo", ["-n", "systemctl", "stop", service]);
      }
      for (const name of Object.keys(units)) {
        if (!prior.some(row => row.status === "UNIT_CREATE_ATTEMPT" && row.name === name)) continue;
        const path = await inspectUnit(name, true);
        if (path) { await record({ status: "UNIT_DELETE_ATTEMPT", name }); await run("sudo", ["-n", "rm", "--", path]); }
      }
      await run("sudo", ["-n", "systemctl", "daemon-reload"]);
    } else {
      for (const resource of desired) {
        const current = await lookup(resource);
        requireValue(current?.metadata?.uid && ownedResourceMatches(current, resource), "EDGE_RESOURCE_READBACK_MISMATCH");
        if (prior) {
          const receipt = prior.find(row => row.status === "RESOURCE_CREATED" && row.kind === resource.kind && row.name === resource.metadata.name);
          requireValue(receipt && receipt.uid === current.metadata.uid, "EDGE_RESOURCE_RECEIPT_REQUIRED");
        }
        await record({ status: "RESOURCE_READBACK", kind: resource.kind, name: resource.metadata.name, uid: current.metadata.uid });
      }
      for (const name of Object.keys(units)) {
        await inspectUnit(name);
        // socket activation не требует заранее работающего service без трафика.
        if (name.endsWith(".socket")) requireValue((await run("systemctl", ["is-active", name])).trim() === "active" &&
          (await run("systemctl", ["is-enabled", name])).trim() === "enabled", "EDGE_SOCKET_NOT_ACTIVE");
      }
    }
    await identities(); await baseline();
    await record({ status: "PASS", phase: mode === "rollback" ? "EDGE_REMOVED" : "EDGE_RESOURCES_ONLY" });
    process.stdout.write(JSON.stringify({ status: "PASS", phase: mode === "rollback" ? "EDGE_REMOVED" : "EDGE_RESOURCES_ONLY",
      publicTLS: "NOT_RUN", sso: "NOT_RUN" }) + "\n");
  } catch (error) {
    await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "EDGE_RELEASE_FAILED" });
    throw error;
  } finally { await journal.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releasePublicEdge(process.argv.slice(2)).catch(error => {
    const code = /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "EDGE_RELEASE_FAILED";
    process.stderr.write(JSON.stringify({ status: "FAIL", code }) + "\n"); process.exitCode = 1;
  });
