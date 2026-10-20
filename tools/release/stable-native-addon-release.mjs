#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { planNative } from "./stable-native-plan.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { validateNodeContainer } from "./stable-node-registry-model.mjs";
import { matchSystemObjects, readSystemObjects, systemServing } from "./stable-system-helm-state.mjs";
import { nativeAddonBinding, nativeDeploymentMetadataHash, nativeSkipPaths, nativeSupportMatches, prepareNativeDeployment } from "./stable-native-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
const definitions = rows => rows.map(o => ({ apiVersion: o.apiVersion, kind: o.kind,
  metadata: { name: o.name, ...(o.namespace ? { namespace: o.namespace } : {}) } }));

export async function releaseNativeAddons(args, port, planPort = planNative) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--plan", "--journal", "--recovery-journal", "--output", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "NATIVE_ADDON_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(["apply", "inspect", "readback", "rollback"].includes(mode) && options["--context"] === "k3d-kodex" &&
    options["--plan"] && options["--output"] && (mode === "apply" ? !options["--journal"] : options["--journal"]), "NATIVE_ADDON_ARGUMENTS_INVALID");
  requireValue(!options["--recovery-journal"] || mode === "rollback", "NATIVE_ADDON_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 30000) => {
    try {
      const p = execute(program, argv, { cwd: root, env, timeout, encoding: "buffer", maxBuffer: 16 << 20 });
      if (input !== undefined) { p.child.stdin.on("error", () => {}); p.child.stdin.end(input); }
      return (await p).stdout;
    } catch { throw Error("NATIVE_ADDON_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const text = async (...a) => (await run(...a)).toString("utf8"), context = options["--context"];
  const kube = (argv, input) => input === undefined ? text("kubectl", ["--context", context, "--request-timeout=15s", ...argv]) :
    text("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-native-pipe", "--context", context, "--request-timeout=15s", ...argv], input, 180000);
  const get = async (kind, name, optional = false) => {
    const raw = await kube(["-n", "kube-system", "get", kind, name, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const source = (await text("git", ["rev-parse", "HEAD"])).trim();
  const assertSource = async () => requireValue((await text("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await text("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "NATIVE_ADDON_CLEAN_SOURCE_REQUIRED");
  await assertSource();
  const document = await readPrivateJSON(options["--plan"]), { plan } = document, hash = fingerprint(plan);
  const config = JSON.parse(await readFile(root + "infra/stable-system/native-profiles.json", "utf8"));
  const profiles = config.profiles.filter(p => p.owner === "addon");
  requireValue(plan.schema === "kodex.stable-native-plan/v2" && document.planSHA256 === hash && plan.source === source && plan.context === context &&
    plan.profileSHA256 === fingerprint(config) && plan.addons?.length === 2 && profiles.every((p, i) => plan.addons[i].name === p.name &&
      Array.isArray(plan.addons[i].objects) && plan.addons[i].objects.length > 0 && plan.targets.some(t => t.name === p.name)), "NATIVE_ADDON_PLAN_INVALID");
  for (const row of plan.addons) {
    const servers = plan.nodes.filter(n => n.role === "server");
    requireValue(row.files?.length === servers.length && new Set(row.files.map(f => f.serverUID)).size === servers.length &&
      row.files.every(f => servers.some(s => s.uid === f.serverUID && s.containerID === f.containerID) &&
        f.skipPath === nativeSkipPaths[row.name]) && row.pdb?.apiVersion === "policy/v1" && row.pdb.kind === "PodDisruptionBudget" &&
      row.pdb.metadata.namespace === "kube-system" && row.pdb.metadata.name === "kodex-stable-" + row.name &&
      row.pdb.metadata.labels?.["app.kubernetes.io/managed-by"] === "kodex-stable-native" &&
      row.pdb.metadata.annotations?.["kodex.dev/stable-native-intent"] === plan.intent && row.pdb.spec.minAvailable === 1 &&
      row.pdb.spec.maxUnavailable === undefined, "NATIVE_ADDON_SUPPORT_SCOPE_INVALID");
  }
  requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"],
    "NATIVE_ADDON_EXACT_OWNER_CONFIRMATION_REQUIRED");
  const identity = async () => {
    requireValue((await get("namespace", "kube-system")).metadata.uid === plan.clusterUID, "NATIVE_ADDON_CLUSTER_CHANGED");
    const nodes = JSON.parse(await kube(["get", "nodes", "-o", "json"]));
    requireValue(nodes.items.length === plan.nodes.length, "NATIVE_ADDON_NODE_SET_CHANGED");
    for (const node of nodes.items) {
      const c = JSON.parse(await text("docker", ["inspect", "--format", "{{json .}}", node.metadata.name]));
      const binding = { ...validateNodeContainer(node, c, "kodex"), role: c.Config.Labels["k3d.role"] };
      requireValue(fingerprint(binding) === fingerprint(plan.nodes.find(n => n.uid === node.metadata.uid)), "NATIVE_ADDON_NODE_CHANGED");
    }
    for (const row of plan.addons) {
      const p = profiles.find(p => p.name === row.name), addon = await get("addons.k3s.cattle.io", p.addon);
      for (const file of row.files) requireValue(fingerprint(nativeAddonBinding(p, addon, file.fileSHA256)) ===
        fingerprint({ name: file.name, uid: file.uid, specSHA256: file.specSHA256, fileSHA256: file.fileSHA256, skipPath: file.skipPath }), "NATIVE_ADDON_OWNER_CHANGED");
    }
  };
  await identity();
  const worker = await readFile(root + "tools/release/stable-native-marker-worker.sh", "utf8");
  const marker = async (row, file, action, receipt = "") => (await text("docker", ["exec", file.containerID, "sh", "-c", worker,
    "kodex-native-marker", action, row.name, hash, plan.intent, file.fileSHA256, file.metadata, receipt])).trim();
  const intents = new Set(), markers = new Map(), pdbs = new Map(), patched = new Set();
  const key = (row, file) => row.name + "/" + file.serverUID;
  const journal = async path => {
    const f = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const s = await f.stat(); requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 && (s.mode & 0o777) === 0o600 &&
        s.size > 0 && s.size <= (1 << 20), "NATIVE_ADDON_PRIVATE_JOURNAL_REQUIRED");
      return (await f.readFile("utf8")).trim().split("\n").map(JSON.parse);
    } finally { await f.close(); }
  };
  if (mode !== "apply") {
    const records = await journal(options["--journal"]);
    const header = records.shift();
    requireValue(header.schema === "kodex.stable-native-addon-journal/v1" && header.mode === "apply" && header.planSHA256 === hash && records.length <= 64,
      "NATIVE_ADDON_JOURNAL_MISMATCH");
    if (mode === "rollback") requireValue(["COMPLETE", "UNKNOWN"].includes(records.at(-1)?.status), "NATIVE_ADDON_ORIGINAL_EXECUTOR_TERMINAL_UNPROVEN");
    for (const r of records) {
      requireValue(["MARKER_INTENT", "MARKER_READY", "PDB_INTENT", "PDB_READY", "PATCH_INTENT", "PATCHED", "COMPLETE", "UNKNOWN"].includes(r.status),
        "NATIVE_ADDON_JOURNAL_MISMATCH");
      if (r.status.endsWith("_INTENT")) intents.add(r.status + "/" + r.name + (r.serverUID ? "/" + r.serverUID : ""));
      if (r.status === "MARKER_READY") markers.set(r.name + "/" + r.serverUID, r.metadata);
      if (r.status === "PDB_READY") pdbs.set(r.name, r.uid);
      if (r.status === "PATCHED") patched.add(r.name);
    }
  }
  if (options["--recovery-journal"]) {
    const rows = await journal(options["--recovery-journal"]), h = rows.shift();
    requireValue(h.schema === "kodex.stable-native-addon-journal/v1" && h.mode === "rollback" && h.planSHA256 === hash &&
      rows.length <= 64 && rows.at(-1)?.status === "COMPLETE" && rows.at(-1).phase === "NATIVE_ADDONS_SOURCE_RESTORED",
    "NATIVE_ADDON_RECOVERY_JOURNAL_MISMATCH");
    for (const r of rows) {
      requireValue(["FINALIZE_INTENT", "MARKER_FINALIZED", "RESTORE_INTENT", "PDB_DELETE_INTENT", "MARKER_REMOVE_INTENT", "RESTORED", "COMPLETE"].includes(r.status) &&
        (r.status === "COMPLETE" || plan.addons.some(a => a.name === r.name)), "NATIVE_ADDON_RECOVERY_JOURNAL_MISMATCH");
      if (r.status === "RESTORE_INTENT") patched.add(r.name);
      if (r.status === "PDB_DELETE_INTENT") pdbs.set(r.name, r.uid);
      if (r.status === "MARKER_REMOVE_INTENT") {
        requireValue(plan.addons.find(a => a.name === r.name).files.some(f => f.serverUID === r.serverUID), "NATIVE_ADDON_RECOVERY_JOURNAL_MISMATCH");
        markers.set(r.name + "/" + r.serverUID, r.metadata);
      }
    }
  }
  const observe = async () => {
    await assertSource(); await identity(); const result = [];
    for (const row of plan.addons) {
      const target = plan.targets.find(t => t.name === row.name), d = await get("deployment", row.name), spec = fingerprint(d.spec);
      requireValue(d.metadata.uid === target.uid && nativeDeploymentMetadataHash(d) === target.metadataSHA256 &&
        [target.beforeSHA256, target.afterSHA256].includes(spec), "NATIVE_ADDON_DEPLOYMENT_DRIFT");
      const phase = spec === target.afterSHA256 ? "after" : "before";
      requireValue(fingerprint(row.pdb.spec.selector) === fingerprint(d.spec.selector), "NATIVE_ADDON_PDB_SELECTOR_CHANGED");
      if (phase === "after" && intents.has("PATCH_INTENT/" + row.name)) patched.add(row.name);
      matchSystemObjects(row.objects, await readSystemObjects({ namespace: "kube-system" }, definitions(row.objects), kube), [phase]);
      const files = [];
      for (const f of row.files) {
        const state = await marker(row, f, "inspect", markers.get(key(row, f)) ?? "");
        if (state !== "ABSENT") requireValue(intents.has("MARKER_INTENT/" + key(row, f)), "NATIVE_ADDON_FOREIGN_MARKER");
        if (mode === "rollback" && state === "ABSENT" && intents.has("MARKER_INTENT/" + key(row, f)) && !markers.has(key(row, f)))
          throw Error("NATIVE_ADDON_MARKER_CREATION_UNRESOLVED");
        files.push({ ...f, state });
      }
      const pdb = await get("poddisruptionbudget", row.pdb.metadata.name, true);
      requireValue(!pdb || intents.has("PDB_INTENT/" + row.name) && nativeSupportMatches(pdb, row.pdb) &&
        (!pdbs.has(row.name) || pdb.metadata.uid === pdbs.get(row.name)), "NATIVE_ADDON_PDB_DRIFT");
      if (mode === "rollback" && !pdb && intents.has("PDB_INTENT/" + row.name) && !pdbs.has(row.name))
        throw Error("NATIVE_ADDON_PDB_CREATION_UNRESOLVED");
      if (mode === "rollback" && phase === "before" && intents.has("PATCH_INTENT/" + row.name) && !patched.has(row.name) &&
        (pdb || files.some(f => f.state !== "ABSENT")))
        throw Error("NATIVE_ADDON_PATCH_TERMINAL_UNPROVEN");
      result.push({ row, target, d, phase, files, pdb });
    }
    return result;
  };
  if (mode === "apply") {
    const fresh = await planPort(["--context", context, "--output", options["--output"] + ".preflight"]);
    fresh.plan.traefik.chartPath = plan.traefik.chartPath;
    requireValue(fingerprint(fresh.plan) === hash, "NATIVE_ADDON_PLAN_DRIFT");
  }
  let current = await observe();
  const prove = async (item, after) => {
    requireValue(item.phase === (after ? "after" : "before") && (!after || item.pdb && item.files.every(f => f.state !== "ABSENT" && !f.state.startsWith("LINKED|"))),
      "NATIVE_ADDON_SERVING_INCOMPLETE");
    await systemServing({ namespace: "kube-system", deployments: [item.row.name] }, kube, [item.target.serving], after ? 2 : 1);
  };
  const metrics = async () => {
    const reply = JSON.parse(await kube(["get", "--raw=/apis/metrics.k8s.io/v1beta1/nodes"]));
    requireValue(reply.apiVersion === "metrics.k8s.io/v1beta1" && reply.kind === "NodeMetricsList" && Array.isArray(reply.items) &&
      reply.items.length === plan.nodes.length && plan.nodes.every(n => reply.items.some(m => m.metadata?.name === n.name)),
    "NATIVE_ADDON_METRICS_API_UNAVAILABLE");
  };
  if (mode === "readback") { for (const item of current) await prove(item, true); await metrics(); }
  const output = await open(options["--output"], "wx", 0o600), record = async r => { await output.writeFile(JSON.stringify(r) + "\n"); await output.sync(); };
  const patch = async (d, spec) => kube(["-n", "kube-system", "patch", "deployment", d.metadata.name, "--type=json", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify([
    { op: "test", path: "/metadata/uid", value: d.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: d.metadata.resourceVersion },
    { op: "test", path: "/spec", value: d.spec }, { op: "replace", path: "/spec", value: spec }]));
  const wait = async name => text("kubectl", ["--context", context, "--request-timeout=15s", "-n", "kube-system", "rollout", "status",
    "deployment/" + name, "--timeout=180s"], undefined, 200000);
  try {
    await record({ schema: "kodex.stable-native-addon-journal/v1", mode, planSHA256: hash });
    if (["inspect", "readback"].includes(mode)) {
      await record({ status: mode === "inspect" ? "OBSERVED" : "SERVING", complete: mode === "readback",
        targets: current.map(i => ({ name: i.row.name, phase: i.phase, markers: i.files.map(f => f.state), pdbUID: i.pdb?.metadata.uid ?? null })) }); return;
    }
    if (mode === "apply") {
      for (const row of plan.addons) {
        for (const file of row.files) {
          await observe(); await record({ status: "MARKER_INTENT", name: row.name, serverUID: file.serverUID });
          intents.add("MARKER_INTENT/" + key(row, file));
          const metadata = await marker(row, file, "install");
          requireValue(/^0:0:600:1:[0-9]+:[0-9]+:[0-9]+$/.test(metadata), "NATIVE_ADDON_MARKER_RECEIPT_INVALID");
          markers.set(key(row, file), metadata); await record({ status: "MARKER_READY", name: row.name, serverUID: file.serverUID, metadata });
        }
        await observe(); await record({ status: "PDB_INTENT", name: row.name }); intents.add("PDB_INTENT/" + row.name);
        const pdb = JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(row.pdb)));
        requireValue(nativeSupportMatches(pdb, row.pdb), "NATIVE_ADDON_PDB_RECEIPT_INVALID");
        pdbs.set(row.name, pdb.metadata.uid); await record({ status: "PDB_READY", name: row.name, uid: pdb.metadata.uid });
        const item = (await observe()).find(i => i.row.name === row.name), profile = profiles.find(p => p.name === row.name);
        requireValue(item.phase === "before", "NATIVE_ADDON_APPLY_PHASE_INVALID");
        const next = prepareNativeDeployment(item.d, profile); requireValue(next.afterSHA256 === item.target.afterSHA256, "NATIVE_ADDON_TARGET_CHANGED");
        await record({ status: "PATCH_INTENT", name: row.name }); intents.add("PATCH_INTENT/" + row.name);
        await patch(item.d, next.after); patched.add(row.name); await record({ status: "PATCHED", name: row.name });
        await wait(row.name); await prove((await observe()).find(i => i.row.name === row.name), true);
      }
    } else {
      for (const original of [...current].reverse()) {
        const item = (await observe()).find(i => i.row.name === original.row.name), { row } = item;
        for (const f of item.files.filter(f => f.state.startsWith("LINKED|"))) {
          await record({ status: "FINALIZE_INTENT", name: row.name, serverUID: f.serverUID, metadata: f.state.slice(7) });
          const metadata = await marker(row, f, "finalize", f.state.slice(7)); markers.set(key(row, f), metadata);
          await record({ status: "MARKER_FINALIZED", name: row.name, serverUID: f.serverUID, metadata });
        }
        if (item.phase === "after") {
          const spec = applyReverseSpec(item.d.spec, item.target.inverse);
          requireValue(fingerprint(spec) === item.target.beforeSHA256, "NATIVE_ADDON_RECOVERY_SPEC_INVALID");
          await record({ status: "RESTORE_INTENT", name: row.name }); await patch(item.d, spec); await wait(row.name);
        }
        await prove((await observe()).find(i => i.row.name === row.name), false);
        const fresh = (await observe()).find(i => i.row.name === row.name);
        if (fresh.pdb) {
          await record({ status: "PDB_DELETE_INTENT", name: row.name, uid: fresh.pdb.metadata.uid });
          pdbs.set(row.name, fresh.pdb.metadata.uid);
          await kube(["delete", "--raw", `/apis/policy/v1/namespaces/kube-system/poddisruptionbudgets/${row.pdb.metadata.name}`, "-f", "/dev/stdin"],
            JSON.stringify({ apiVersion: "v1", kind: "DeleteOptions", preconditions: { uid: fresh.pdb.metadata.uid, resourceVersion: fresh.pdb.metadata.resourceVersion } }));
        }
        for (const f of (await observe()).find(i => i.row.name === row.name).files.filter(f => f.state !== "ABSENT")) {
          await record({ status: "MARKER_REMOVE_INTENT", name: row.name, serverUID: f.serverUID, metadata: f.state });
          markers.set(key(row, f), f.state);
          requireValue(await marker(row, f, "remove", f.state) === "REMOVED", "NATIVE_ADDON_REMOVE_UNPROVEN");
        }
        const restored = (await observe()).find(i => i.row.name === row.name);
        requireValue(!restored.pdb && restored.files.every(f => f.state === "ABSENT"), "NATIVE_ADDON_RECOVERY_INCOMPLETE"); await prove(restored, false);
        await record({ status: "RESTORED", name: row.name });
      }
    }
    current = await observe();
    for (const item of current) await prove(item, mode === "apply");
    await metrics();
    await record({ status: "COMPLETE", phase: mode === "apply" ? "NATIVE_ADDONS_TWO_READY" : "NATIVE_ADDONS_SOURCE_RESTORED" });
  } catch (error) { await record({ status: "UNKNOWN" }); throw error; }
  finally { await output.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releaseNativeAddons(process.argv.slice(2)).then(() => console.log(JSON.stringify({ status: "PASS", phase: process.argv[2], traefik: "NOT_CHANGED" })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "NATIVE_ADDON_RELEASE_FAILED" })); process.exitCode = 1; });
