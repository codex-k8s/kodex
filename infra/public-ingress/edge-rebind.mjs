#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { promisify } from "node:util";
import { pathToFileURL, fileURLToPath } from "node:url";
import { renderPublicEdge } from "./edge-model.mjs";
import { edgeCollections, protectedCommitment, publicCAProjection, readPrivateJSON, validateEdgeArtifacts } from "./edge-plan.mjs";
import { ownedResourceMatches, resourceIntent } from "./edge-release.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const execute = promisify(execFile);
function requireValue(ok, code) { if (!ok) throw Error(code); }

// IP не является identity: новое разрешение выводится только из прежнего exact
// Docker ID и того же bridge gateway/network на всех выбранных узлах.
export function currentPeers(plan, containers) {
  const peers = plan.peers.map(peer => {
    const container = containers.get(peer.name);
    requireValue(container?.Id === peer.id && container.State?.Running, "EDGE_REBIND_NODE_IDENTITY_DRIFT");
    const networks = Object.values(container.NetworkSettings?.Networks ?? {}).filter(n => n.Gateway === plan.config.bridgeIP);
    requireValue(networks.length === 1 && /^[a-f0-9]{64}$/.test(networks[0].NetworkID ?? ""), "EDGE_REBIND_NETWORK_DRIFT");
    return { ...peer, peerIP: networks[0].IPAddress, networkID: networks[0].NetworkID };
  });
  requireValue(new Set(peers.map(p => p.networkID)).size === 1, "EDGE_REBIND_NETWORK_DRIFT");
  renderPublicEdge({ ...plan.config, allowedPeers: peers.map(p => p.peerIP) });
  return peers;
}

export function replacementUnits(source, peers) {
  const revision = fingerprint(peers);
  const rendered = renderPublicEdge({ ...source.config, allowedPeers: peers.map(p => p.peerIP) });
  return Object.fromEntries(Object.entries(rendered.units).map(([name, body]) => [name,
    `# Kodex public edge plan ${fingerprint(source)}\n# Kodex peer identity revision ${revision}\n${body}`]));
}

async function unitText(name) {
  requireValue(/^kodex-public-edge-https-[01]\.(socket|service)$/.test(name), "EDGE_REBIND_UNIT_INVALID");
  const file = await open(`/etc/systemd/system/${name}`, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    requireValue(stat.isFile() && stat.uid === 0 && stat.nlink === 1 && (stat.mode & 0o777) === 0o644 && stat.size < 16384,
      "EDGE_REBIND_UNIT_IDENTITY_INVALID");
    return await file.readFile("utf8");
  } finally { await file.close(); }
}

async function writePrivate(path, body) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(body); await file.sync(); } finally { await file.close(); }
}

export async function rebindPublicEdge(args) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--source-plan", "--source-journal", "--plan", "--output", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "EDGE_REBIND_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  requireValue(["plan", "apply", "readback"].includes(mode) && options["--output"], "EDGE_REBIND_ARGUMENTS_INVALID");
  const intent = mode === "plan" ? null : await readPrivateJSON(resolve(options["--plan"]));
  const sourcePath = mode === "plan" ? options["--source-plan"] : intent?.sourcePlan;
  const journalPath = mode === "plan" ? options["--source-journal"] : intent?.sourceJournal;
  requireValue(typeof sourcePath === "string" && sourcePath.startsWith("/") && typeof journalPath === "string" && journalPath.startsWith("/"),
    "EDGE_REBIND_SOURCE_REQUIRED");
  const source = await readPrivateJSON(sourcePath), sourceSHA256 = fingerprint(source), directory = dirname(sourcePath);
  const rendered = renderPublicEdge(source.config);
  const originalUnits = {};
  for (const name of Object.keys(rendered.units)) originalUnits[name] = await readPrivateText(join(directory, name));
  const bundle = await readPrivateJSON(join(directory, "resources.json"));
  validateEdgeArtifacts(source, bundle, originalUnits);
  const originalJournal = (await readPrivateText(journalPath)).trim().split("\n").map(JSON.parse);
  requireValue(originalJournal[0]?.schema === "kodex.public-edge-apply/v1" && originalJournal[0].planSHA256 === sourceSHA256 &&
    originalJournal.at(-1)?.status === "PASS", "EDGE_REBIND_SUCCESSFUL_SOURCE_REQUIRED");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = async (command, argv, input) => {
    try {
      const call = execute(command, argv, { env, timeout: 30000, maxBuffer: 8 << 20 });
      if (input !== undefined) { call.child.stdin.on("error", () => {}); call.child.stdin.end(input); }
      return (await call).stdout;
    } catch { throw Error("EDGE_REBIND_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const inspect = async name => JSON.parse(await run("docker", ["inspect", name]))[0];
  const read = async (argv, isSource = false) => JSON.parse(await run(isSource ? "kubectl" : "docker", isSource ?
    ["--kubeconfig", source.source.kubeconfig, "--context", source.source.context, "--request-timeout=15s", ...argv, "-o", "json"] :
    ["exec", source.edgeNode.name, "kubectl", "--request-timeout=15s", ...argv, "-o", "json"]));
  // Чужие EndpointSlice закономерно меняются при перезапуске. Новый read-only
  // план закрепляет их текущее состояние, а apply/readback запрещают его drift.
  // Возвращать чужие объекты к историческому snapshot запрещено.
  let protectedSHA256 = intent?.protectedSHA256;
  const verify = async () => {
    requireValue(source.edgeNode && /^k3d-[a-z0-9-]+-server-[0-9]+$/.test(source.edgeNode.name), "EDGE_REBIND_EDGE_NODE_REQUIRED");
    const edge = await inspect(source.edgeNode.name);
    requireValue(edge?.Id === source.edgeNode.id && edge.State?.Running && edge.Config?.Labels?.["k3d.cluster"] !== "kodex" &&
      edge.Config?.Labels?.["k3d.role"] === "server", "EDGE_REBIND_EDGE_IDENTITY_DRIFT");
    const edgeCluster = await read(["get", "namespace", "kube-system"]);
    const sourceCluster = await read(["get", "namespace", "kube-system"], true);
    const namespace = await read(["get", "namespace", source.config.namespace]);
    requireValue(edgeCluster.metadata.uid === source.clusterUID && sourceCluster.metadata.uid === source.source.clusterUID &&
      namespace.metadata.uid === source.namespaceUID, "EDGE_REBIND_CLUSTER_DRIFT");
    const ca = await read(["-n", "kodex-system", "get", "configmap", "kodex-oidc-ca"], true);
    requireValue(ca.metadata.uid === source.source.caUID && fingerprint(publicCAProjection(ca, source.config.namespace)) ===
      source.source.caProjectionSHA256, "EDGE_REBIND_CA_DRIFT");
    const resources = await read(["-n", source.config.namespace, "get", edgeCollections]);
    const currentProtected = protectedCommitment(resources.items);
    if (protectedSHA256 === undefined && mode === "plan") protectedSHA256 = currentProtected;
    requireValue(currentProtected === protectedSHA256, "EDGE_REBIND_PROTECTED_DRIFT");
    const nodes = await read(["get", "nodes"]);
    const containers = new Map();
    for (const peer of source.peers) {
      requireValue(nodes.items.some(n => n.metadata.name === peer.name), "EDGE_REBIND_NODE_MISSING");
      containers.set(peer.name, await inspect(peer.name));
    }
    for (const resource of bundle.items) {
      const current = resource.kind === "Secret" ? await read(["-n", resource.metadata.namespace, "get", "secret", resource.metadata.name]) :
        resources.items.find(r => r.kind === resource.kind && r.metadata.name === resource.metadata.name);
      const receipt = originalJournal.find(row => row.status === "RESOURCE_CREATED" && row.kind === resource.kind && row.name === resource.metadata.name);
      requireValue(receipt && current?.metadata?.uid === receipt.uid && ownedResourceMatches(current, resourceIntent(resource, sourceSHA256)),
        "EDGE_REBIND_RESOURCE_DRIFT");
    }
    return currentPeers(source, containers);
  };
  const peers = await verify(), after = replacementUnits(source, peers);
  if (mode === "plan") {
    const before = {};
    for (const [name, body] of Object.entries(originalUnits)) {
      before[name] = await unitText(name);
      requireValue(before[name] === `# Kodex public edge plan ${sourceSHA256}\n${body}`, "EDGE_REBIND_UNIT_DRIFT");
    }
    const plan = { schema: "kodex.public-edge-peer-rebind/v1", sourcePlan: sourcePath, sourceJournal: journalPath,
      sourceSHA256, protectedSHA256, peers, before, after };
    await writePrivate(options["--output"], JSON.stringify(plan));
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(plan) }) + "\n");
    return;
  }
  requireValue(intent?.schema === "kodex.public-edge-peer-rebind/v1" && intent.sourceSHA256 === sourceSHA256 &&
    fingerprint(intent.peers) === fingerprint(peers) && fingerprint(intent.after) === fingerprint(after), "EDGE_REBIND_PLAN_DRIFT");
  requireValue(fingerprint(Object.keys(intent.before).sort()) === fingerprint(Object.keys(after).sort()), "EDGE_REBIND_UNIT_DRIFT");
  for (const [name, body] of Object.entries(originalUnits)) requireValue(intent.before[name] === `# Kodex public edge plan ${sourceSHA256}\n${body}`,
    "EDGE_REBIND_UNIT_DRIFT");
  if (mode === "apply") requireValue(options["--approved-plan-sha"] === fingerprint(intent), "EDGE_REBIND_EXACT_APPROVAL_REQUIRED");
  const output = await open(options["--output"], "wx", 0o600);
  const record = async value => { await output.writeFile(JSON.stringify(value) + "\n"); await output.sync(); };
  try {
    await record({ schema: "kodex.public-edge-peer-rebind-journal/v1", mode, planSHA256: fingerprint(intent), status: "IN_PROGRESS" });
    for (const name of Object.keys(after)) {
      const current = await unitText(name);
      requireValue(current === after[name] || mode === "apply" && current === intent.before[name], "EDGE_REBIND_UNIT_DRIFT");
    }
    if (mode === "apply") {
      await record({ status: "UNIT_REPLACE_ATTEMPT", unitSHA256: fingerprint(after) });
      await run("sudo", ["-n", process.execPath, fileURLToPath(new URL("./edge-unit-worker.mjs", import.meta.url))],
        JSON.stringify({ before: intent.before, after }));
      await run("sudo", ["-n", "systemctl", "daemon-reload"]);
      for (const name of Object.keys(after).filter(name => name.endsWith(".socket"))) {
        requireValue(fingerprint(await verify()) === fingerprint(peers), "EDGE_REBIND_NODE_IDENTITY_DRIFT");
        await record({ status: "RELAY_RESTART_ATTEMPT", name });
        await run("sudo", ["-n", "systemctl", "stop", name.replace(/\.socket$/, ".service")]);
        await run("sudo", ["-n", "systemctl", "restart", name]);
      }
    }
    for (const [name, body] of Object.entries(after)) {
      requireValue(await unitText(name) === body, "EDGE_REBIND_UNIT_DRIFT");
      if (name.endsWith(".socket")) requireValue((await run("systemctl", ["is-active", name])).trim() === "active" &&
        (await run("systemctl", ["is-enabled", name])).trim() === "enabled", "EDGE_REBIND_SOCKET_NOT_ACTIVE");
    }
    requireValue(fingerprint(await verify()) === fingerprint(peers), "EDGE_REBIND_NODE_IDENTITY_DRIFT");
    await record({ status: "PASS", phase: "EXACT_PEERS_AND_SOCKETS", publicHTTPS: "NOT_RUN" });
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "EXACT_PEERS_AND_SOCKETS", publicHTTPS: "NOT_RUN" }) + "\n");
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message) ? error.message : "EDGE_REBIND_FAILED" }); throw error; }
  finally { await output.close(); }
}

async function readPrivateText(path) {
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try { const stat = await file.stat(); requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 &&
    (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size < 1 << 20, "EDGE_REBIND_PRIVATE_FILE_REQUIRED"); return await file.readFile("utf8"); }
  finally { await file.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  rebindPublicEdge(process.argv.slice(2)).catch(error => {
    process.stderr.write(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message) ? error.message : "EDGE_REBIND_FAILED" }) + "\n");
    process.exitCode = 1;
  });
