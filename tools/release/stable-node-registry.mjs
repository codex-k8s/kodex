#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { requireIdle } from "./runner-policy-model.mjs";
import { logicalRegistryHost, nativeRegistryMirror, nodeRegistryFiles, nodeRegistryTLSFiles, validateNodeContainer } from "./stable-node-registry-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
const workerPath = fileURLToPath(new URL("./stable-node-registry-worker.sh", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
const hash = value => createHash("sha256").update(value).digest("hex");
const encoded = value => JSON.stringify(value) + "\n";
async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(encoded(value)); await file.sync(); } finally { await file.close(); }
}

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--physical-host", "--plan", "--output", "--approved-plan-sha", "--approved-executor-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "NODE_MIRROR_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(["plan", "apply", "readback", "rollback"].includes(mode) &&
    /^k3d-[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(context ?? "") && options["--output"], "NODE_MIRROR_ARGUMENTS_INVALID");
  requireValue(!options["--approved-executor-sha"] || mode === "rollback", "NODE_MIRROR_ARGUMENTS_INVALID");
  const cluster = context.slice(4);
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  // stdout/stderr внешних программ никогда не включаются в diagnostic: среди
  // чтений есть private YAML и Docker Config.Env. В evidence только commitments.
  const run = async (program, argv, input, timeout = 25000) => {
    try {
      const pending = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 2 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("NODE_MIRROR_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const kube = argv => run("kubectl", ["--context", context, "--request-timeout=15s", ...argv]);
  const get = argv => kube([...argv, "-o", "json"]).then(JSON.parse);
  const docker = (id, argv, input) => run("docker", ["exec", ...(input === undefined ? [] : ["-i"]), id, ...argv], input);
  const executorSHA = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(executorSHA) &&
    (await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim() === "", "NODE_MIRROR_CLEAN_SOURCE_REQUIRED");
  await run("git", ["ls-files", "--error-unmatch", "tools/release/stable-node-registry.mjs",
    "tools/release/stable-node-registry-model.mjs", "tools/release/stable-node-registry-worker.sh"]);
  const identity = async () => {
    const system = await get(["get", "namespace", "kube-system"]), app = await get(["get", "namespace", "kodex-system"]);
    requireValue(system.metadata?.uid && app.metadata?.uid, "NODE_MIRROR_CLUSTER_IDENTITY_INVALID");
    return { clusterUID: system.metadata.uid, namespaceUID: app.metadata.uid };
  };
  const clusterIdentity = await identity();
  const yaml = async raw => {
    try { return JSON.parse(await run("yq", ["-o=json", "-I=0"], raw)); }
    catch { throw Error("NODE_MIRROR_YAML_INVALID"); }
  };
  const file = async (id, path, optional = false) => {
    const metadata = (await docker(id, ["sh", "-ec", 'test ! -L "$1"; if test -e "$1"; then test -f "$1"; stat -c "%u:%g:%a:%h:%d:%i:%s" "$1"; else printf ABSENT; fi', "kodex-node-file", path])).trim();
    if (metadata === "ABSENT") {
      requireValue(optional, "NODE_MIRROR_FILE_MISSING");
      return { metadata: null, sha256: "ABSENT", raw: "", value: {} };
    }
    requireValue(/^[0-9]+:[0-9]+:600:1:[0-9]+:[0-9]+:[0-9]+$/.test(metadata), "NODE_MIRROR_PRIVATE_FILE_REQUIRED");
    const owner = Number(metadata.split(":")[0]);
    requireValue([0, process.getuid()].includes(owner), "NODE_MIRROR_FILE_OWNER_INVALID");
    const raw = await docker(id, ["cat", path]);
    requireValue(Buffer.byteLength(raw) === Number(metadata.split(":").at(-1)) && raw.length <= 1 << 20,
      "NODE_MIRROR_FILE_CHANGED");
    const after = (await docker(id, ["stat", "-c", "%u:%g:%a:%h:%d:%i:%s", path])).trim();
    requireValue(metadata === after, "NODE_MIRROR_FILE_CHANGED");
    return { metadata, sha256: hash(raw), raw, value: await yaml(raw) };
  };
  const snapshot = async physicalHost => {
    requireValue(fingerprint(await identity()) === fingerprint(clusterIdentity), "NODE_MIRROR_CLUSTER_DRIFT");
    const resources = await get(["get", "nodes"]);
    requireValue(Array.isArray(resources.items) && resources.items.length > 0 && resources.items.length <= 8,
      "NODE_MIRROR_NODE_INVENTORY_INVALID");
    const result = [];
    for (const node of resources.items.sort((a, b) => a.metadata.name.localeCompare(b.metadata.name, "en"))) {
      const container = JSON.parse(await run("docker", ["inspect", "--format", "{{json .}}", node.metadata.name]));
      const binding = validateNodeContainer(node, container, cluster), id = binding.containerID;
      await docker(id, ["sh", "-ec", "test ! -e /etc/rancher/k3s/.kodex-stable-registry-lock; test ! -L /etc/rancher/k3s/.kodex-stable-registry-lock"]);
      const registry = await file(id, nodeRegistryFiles.registry), config = await file(id, nodeRegistryFiles.config, true);
      const desired = nativeRegistryMirror(registry.value, config.value, physicalHost);
      const tlsHashes = (await docker(id, ["sha256sum", ...nodeRegistryTLSFiles])).trim().split("\n").map(line => line.split(/\s+/)[0]);
      requireValue(tlsHashes.length === 3 && tlsHashes.every(v => /^[a-f0-9]{64}$/.test(v)), "NODE_MIRROR_TLS_FILES_INVALID");
      const afterRegistry = encoded(desired.registry), afterConfig = encoded(desired.config);
      result.push({ ...binding, startedAt: container.State.StartedAt,
        heartbeatAt: node.status.conditions.find(c => c.type === "Ready").lastHeartbeatTime,
        logicalMirrorPresent: Object.hasOwn(registry.value.mirrors, logicalRegistryHost),
        registry: { beforeSHA256: registry.sha256, beforeMetadata: registry.metadata, afterSHA256: hash(afterRegistry) },
        config: { beforeSHA256: config.sha256, beforeMetadata: config.metadata, afterSHA256: hash(afterConfig) }, tlsHashes,
        payload: afterRegistry + afterConfig });
    }
    requireValue(new Set(result.map(n => n.uid)).size === result.length &&
      new Set(result.map(n => n.containerID)).size === result.length, "NODE_MIRROR_NODE_DUPLICATED");
    return result;
  };
  const publicNode = node => { const { payload: _payload, heartbeatAt: _heartbeat, ...value } = node; return value; };
  if (mode === "plan") {
    requireValue(options["--physical-host"] && !options["--plan"] && !options["--approved-plan-sha"], "NODE_MIRROR_ARGUMENTS_INVALID");
    const nodes = await snapshot(options["--physical-host"]);
    requireValue(nodes.every(node => !node.logicalMirrorPresent), "NODE_MIRROR_ALREADY_CONFIGURED");
    const plan = { schema: "kodex.stable-node-registry/v1", executorSHA, context, ...clusterIdentity,
      physicalHost: options["--physical-host"], logicalHost: logicalRegistryHost, nodes: nodes.map(publicNode) };
    await createPrivate(options["--output"], plan);
    process.stdout.write(encoded({ status: "PASS", phase: "PLAN_ONLY", nodes: nodes.length, planSHA256: fingerprint(plan) }));
    return;
  }
  requireValue(options["--plan"] && !options["--physical-host"], "NODE_MIRROR_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(options["--plan"]), planSHA256 = fingerprint(plan);
  requireValue(plan.schema === "kodex.stable-node-registry/v1" && plan.context === context && /^[a-f0-9]{40}$/.test(plan.executorSHA ?? "") &&
    plan.logicalHost === logicalRegistryHost && plan.clusterUID === clusterIdentity.clusterUID && plan.namespaceUID === clusterIdentity.namespaceUID &&
    Array.isArray(plan.nodes) && plan.nodes.length > 0 && plan.nodes.length <= 8, "NODE_MIRROR_PLAN_IDENTITY_INVALID");
  requireValue(mode === "readback" ? !options["--approved-plan-sha"] : options["--approved-plan-sha"] === planSHA256,
    "NODE_MIRROR_EXACT_OWNER_CONFIRMATION_REQUIRED");
  if (plan.executorSHA !== executorSHA) {
    // Исправление observation-кода может потребоваться после partial apply.
    // Только явный forward recovery: старый immutable plan и новый executor
    // подтверждаются отдельно; bytes/model установки обязаны быть прежними.
    requireValue(mode === "rollback" && options["--approved-executor-sha"] === executorSHA,
      "NODE_MIRROR_RECOVERY_EXECUTOR_CONFIRMATION_REQUIRED");
    await run("git", ["merge-base", "--is-ancestor", plan.executorSHA, executorSHA]);
    await run("git", ["diff", "--quiet", plan.executorSHA, executorSHA, "--",
      "tools/release/stable-node-registry-model.mjs", "tools/release/stable-node-registry-worker.sh"]);
  } else requireValue(!options["--approved-executor-sha"] || options["--approved-executor-sha"] === executorSHA,
    "NODE_MIRROR_RECOVERY_EXECUTOR_CONFIRMATION_REQUIRED");
  const current = await snapshot(plan.physicalHost);
  requireValue(current.length === plan.nodes.length, "NODE_MIRROR_NODE_SET_DRIFT");
  const stableBinding = node => ({ name: node.name, uid: node.uid, containerID: node.containerID, imageID: node.imageID,
    commandSHA256: node.commandSHA256, tlsHashes: node.tlsHashes });
  const bindingFor = expected => {
    const value = current.find(n => n.uid === expected.uid);
    requireValue(value && fingerprint(stableBinding(value)) === fingerprint(stableBinding(expected)), "NODE_MIRROR_NODE_IDENTITY_DRIFT");
    return value;
  };
  for (const expected of plan.nodes) {
    const actual = bindingFor(expected);
    if (mode === "apply") requireValue(fingerprint(publicNode(actual)) === fingerprint(expected), "NODE_MIRROR_PLAN_DRIFT");
    else for (const kind of ["registry", "config"]) requireValue(
      [expected[kind].beforeSHA256, expected[kind].afterSHA256].includes(actual[kind].beforeSHA256), "NODE_MIRROR_FILE_DRIFT");
  }
  const worker = await readFile(workerPath, "utf8");
  const work = async (node, action, payload) => {
    const owner = value => value?.split(":")[0] ?? "0";
    return docker(node.containerID, ["sh", "-c", worker, "kodex-node-registry", action, planSHA256,
      node.registry.beforeSHA256, node.registry.afterSHA256, node.config.beforeSHA256, node.config.afterSHA256,
      owner(node.registry.beforeMetadata), owner(node.config.beforeMetadata)], payload);
  };
  const generatedMirror = async (node, after) => {
    const path = `/var/lib/rancher/k3s/agent/etc/containerd/certs.d/${logicalRegistryHost}/hosts.toml`;
    if (!after) {
      await docker(node.containerID, ["sh", "-ec", 'test ! -e "$1" && test ! -L "$1"', "kodex-node-hosts", path]);
      return;
    }
    const text = await docker(node.containerID, ["cat", path]);
    const urls = [...text.matchAll(/(?:^\s*server\s*=\s*"|^\s*\[host\.")([^"\n]+)"/gm)].map(m => m[1]);
    const allowed = new Set([`https://${plan.physicalHost}`, `https://${plan.physicalHost}/v2`,
      `https://${plan.physicalHost}:443`, `https://${plan.physicalHost}:443/v2`]);
    requireValue(urls.length > 0 && urls.every(url => allowed.has(url)) && !/skip_verify\s*=\s*true/.test(text),
      "NODE_MIRROR_GENERATED_TRANSPORT_INVALID");
  };
  if (mode === "readback") {
    for (const expected of plan.nodes) {
      const actual = bindingFor(expected);
      requireValue(actual.registry.beforeSHA256 === expected.registry.afterSHA256 &&
        actual.config.beforeSHA256 === expected.config.afterSHA256 && Date.parse(actual.startedAt) > Date.parse(expected.startedAt) &&
        Date.parse(actual.heartbeatAt) >= Date.parse(actual.startedAt),
        "NODE_MIRROR_READBACK_INCOMPLETE");
      await generatedMirror(expected, true);
    }
    await createPrivate(options["--output"], { schema: "kodex.stable-node-registry-readback/v1", planSHA256,
      status: "PASS", phase: "NATIVE_CONFIGURATION_ONLY", freshPull: "NOT_RUN" });
    process.stdout.write(encoded({ status: "PASS", phase: "NATIVE_CONFIGURATION_ONLY", freshPull: "NOT_RUN" }));
    return;
  }
  // Отдельный maintenance owner gate обязателен: hash сам по себе не является
  // разрешением обойти review или выполнять параллельный restart.
  const idleSQL = await readFile(fileURLToPath(new URL("../dev/supply-chain-owner-readback.sql", import.meta.url)), "utf8");
  const archiveSQL = await readFile(fileURLToPath(new URL("./stable-archive-idle.sql", import.meta.url)), "utf8");
  const idle = async () => {
    const query = input => run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-node-idle", "--context", context, "--request-timeout=15s",
      "-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
      "-U", "postgres", "-d", "control_plane"], input).then(JSON.parse);
    const result = await query(idleSQL); requireIdle(result);
    const archive = await query(archiveSQL);
    requireValue(archive.openArchiveTasks === 0 && archive.transitionalStorage === 0, "NODE_MIRROR_ARCHIVE_NOT_IDLE");
    const jobs = await get(["-n", "kodex-runtime", "get", "jobs,pods", "-l", "session-archive.kodex.dev/managed=true"]);
    requireValue(Array.isArray(jobs.items) && jobs.items.length === 0, "NODE_MIRROR_ARCHIVE_WORKERS_PRESENT");
    return { promotedArtifactCount: result.promotedArtifactCount, promotedPinsSHA256: result.promotedPinsSHA256 };
  };
  const preserved = await idle();
  const touched = new Set();
  if (mode === "rollback") for (const node of plan.nodes) {
    const state = (await docker(node.containerID, ["sh", "-ec",
      'test ! -L "$1"; if test -e "$1"; then test -d "$1"; printf PRESENT; else printf ABSENT; fi',
      "kodex-node-intent", `/etc/rancher/k3s/.kodex-stable-registry-${planSHA256}`])).trim();
    const actual = bindingFor(node);
    if (state === "ABSENT") requireValue(actual.registry.beforeSHA256 === node.registry.beforeSHA256 &&
      actual.config.beforeSHA256 === node.config.beforeSHA256, "NODE_MIRROR_MISSING_ROLLBACK_INTENT");
    else { requireValue(state === "PRESENT", "NODE_MIRROR_ROLLBACK_INTENT_INVALID"); await work(node, "verify-backup"); touched.add(node.uid); }
  }
  await createPrivate(options["--output"], { schema: "kodex.stable-node-registry-journal/v1", mode, planSHA256, executorSHA,
    planExecutorSHA: plan.executorSHA, status: "IN_PROGRESS", preserved });
  const journal = await open(options["--output"], "a");
  const record = async value => { await journal.writeFile(encoded(value)); await journal.sync(); };
  try {
    for (const node of plan.nodes) {
      if (mode === "rollback" && !touched.has(node.uid)) {
        await record({ node: node.name, status: "UNCHANGED" });
        continue;
      }
      requireValue(fingerprint(await idle()) === fingerprint(preserved), "NODE_MIRROR_PRESERVED_PINS_DRIFT");
      const fresh = (await snapshot(plan.physicalHost)).find(n => n.uid === node.uid);
      requireValue(fresh && fingerprint(stableBinding(fresh)) === fingerprint(stableBinding(node)), "NODE_MIRROR_NODE_IDENTITY_DRIFT");
      if (mode === "apply") requireValue(fingerprint(publicNode(fresh)) === fingerprint(node), "NODE_MIRROR_PLAN_DRIFT");
      await record({ node: node.name, status: "FILES_ATTEMPT" });
      await work(node, mode === "apply" ? "install" : "rollback", mode === "apply" ? fresh.payload : undefined);
      const restartAt = Date.now();
      await record({ node: node.name, status: "RESTART_ATTEMPT", at: new Date(restartAt).toISOString() });
      await run("docker", ["restart", node.containerID], undefined, 45000);
      const deadline = Date.now() + 240000;
      for (;;) {
        try {
          const runtime = JSON.parse(await docker(node.containerID, ["crictl", "--runtime-endpoint", "unix:///run/k3s/containerd/containerd.sock", "info"]));
          const observed = await get(["get", "node", node.name]);
          const ready = observed.status?.conditions?.find(c => c.type === "Ready" && c.status === "True");
          requireValue(observed.metadata.uid === node.uid && ready && Date.parse(ready.lastHeartbeatTime) >= restartAt &&
            ["RuntimeReady", "NetworkReady"].every(type => runtime.status?.conditions?.some(c => c.type === type && c.status === true)),
            "NODE_MIRROR_RESTART_NOT_READY");
          await kube(["get", "--raw=/readyz"]);
          break;
        } catch {
          requireValue(Date.now() < deadline, "NODE_MIRROR_RESTART_TIMEOUT");
          await new Promise(resolve => setTimeout(resolve, 2000));
        }
      }
      const observed = (await snapshot(plan.physicalHost)).find(n => n.uid === node.uid);
      requireValue(observed && fingerprint(stableBinding(observed)) === fingerprint(stableBinding(node)) &&
        observed.registry.beforeSHA256 === (mode === "apply" ? node.registry.afterSHA256 : node.registry.beforeSHA256) &&
        observed.config.beforeSHA256 === (mode === "apply" ? node.config.afterSHA256 : node.config.beforeSHA256), "NODE_MIRROR_FILES_READBACK_MISMATCH");
      await generatedMirror(node, mode === "apply");
      await record({ node: node.name, status: "PASS", phase: "NATIVE_CONFIGURATION_ONLY" });
    }
    requireValue(fingerprint(await idle()) === fingerprint(preserved), "NODE_MIRROR_PRESERVED_PINS_DRIFT");
    await record({ status: "PASS", phase: "NATIVE_CONFIGURATION_ONLY", freshPull: "NOT_RUN" });
    process.stdout.write(encoded({ status: "PASS", phase: "NATIVE_CONFIGURATION_ONLY", freshPull: "NOT_RUN" }));
  } catch (error) {
    await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "NODE_MIRROR_TRANSITION_FAILED" });
    throw error;
  } finally { await journal.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main(process.argv.slice(2)).catch(error => {
    process.stderr.write(encoded({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "NODE_MIRROR_TRANSITION_FAILED" }));
    process.exitCode = 1;
  });
