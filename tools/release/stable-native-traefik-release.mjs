#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { validateNodeContainer } from "./stable-node-registry-model.mjs";
import { verifyStableChart } from "./fetch-stable-charts.mjs";
import { matchSystemObjects, readSystemObjects, systemDesiredValues, systemServing } from "./stable-system-helm-state.mjs";
import { nativeChartMetadataHash, nativeDeploymentMetadataHash, nativeSupportMatches, traefikHelmConfig } from "./stable-native-model.mjs";
import { nativeHelmJob } from "./stable-native-helm-job.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
const hash = bytes => createHash("sha256").update(bytes).digest("hex");
async function verifyArtifact(plan, config, run) {
  await verifyStableChart(plan.traefik.chartPath, { sha256: config.traefikChart.sha256 });
  const bytes = await run("kubectl", ["--context", plan.context, "--request-timeout=15s", "get", "--raw=" + config.traefikChart.apiPath]);
  requireValue(hash(bytes) === plan.traefik.chartSHA256 && hash(bytes) === config.traefikChart.sha256, "NATIVE_TRAEFIK_CHART_CHANGED");
}

export async function releaseNativeTraefik(args, port, artifactPort = verifyArtifact) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--plan", "--journal", "--recovery-journal", "--output", "--approved-plan-sha"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "NATIVE_TRAEFIK_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(["apply", "inspect", "readback", "rollback"].includes(mode) && options["--context"] === "k3d-kodex" && options["--plan"] && options["--output"] &&
    (mode === "apply" ? !options["--journal"] : options["--journal"]), "NATIVE_TRAEFIK_ARGUMENTS_INVALID");
  requireValue(!options["--recovery-journal"] || mode === "rollback", "NATIVE_TRAEFIK_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 30000) => {
    try {
      const p = execute(program, argv, { cwd: root, env, timeout, encoding: "buffer", maxBuffer: 16 << 20 });
      if (input !== undefined) { p.child.stdin.on("error", () => {}); p.child.stdin.end(input); }
      return (await p).stdout;
    } catch { throw Error("NATIVE_TRAEFIK_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const text = async (...a) => (await run(...a)).toString("utf8"), context = options["--context"];
  const kube = (argv, input) => input === undefined ? text("kubectl", ["--context", context, "--request-timeout=15s", ...argv]) :
    text("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-native-traefik", "--context", context, "--request-timeout=15s", ...argv], input, 180000);
  const get = async (kind, name, optional = false) => {
    const raw = await kube(["-n", "kube-system", "get", kind, name, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const source = (await text("git", ["rev-parse", "HEAD"])).trim();
  const assertSource = async () => requireValue(/^[a-f0-9]{40}$/.test(source) && (await text("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await text("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "NATIVE_TRAEFIK_CLEAN_SOURCE_REQUIRED");
  await assertSource();
  const document = await readPrivateJSON(options["--plan"]), { plan } = document, planHash = fingerprint(plan), row = plan.traefik;
  const config = JSON.parse(await readFile(root + "infra/stable-system/native-profiles.json", "utf8")), profile = config.profiles.find(p => p.name === "traefik");
  requireValue(plan.schema === "kodex.stable-native-plan/v2" && document.planSHA256 === planHash && plan.source === source && plan.context === context &&
    plan.profileSHA256 === fingerprint(config) && row?.sourceJob?.complete && row.sourceJob.terminal && Array.isArray(row.objects) && row.objects.length > 0 &&
    /^[a-f0-9]{64}$/.test(row.recoverySHA256 ?? "") && plan.targets.some(t => t.name === "traefik"), "NATIVE_TRAEFIK_PLAN_INVALID");
  requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === planHash : !options["--approved-plan-sha"],
    "NATIVE_TRAEFIK_EXACT_OWNER_CONFIRMATION_REQUIRED");
  const target = plan.targets.find(t => t.name === "traefik"), knownJobs = new Set([row.sourceJob.uid]);
  let intent = false, hccUID = row.sourceConfig?.uid, objectReceipts = [];
  const journal = async path => {
    const f = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const s = await f.stat(); requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 && (s.mode & 0o777) === 0o600 &&
        s.size > 0 && s.size <= (1 << 20), "NATIVE_TRAEFIK_PRIVATE_JOURNAL_REQUIRED");
      const records = (await f.readFile("utf8")).trim().split("\n").map(JSON.parse);
      requireValue(records.length <= 33, "NATIVE_TRAEFIK_JOURNAL_MISMATCH"); return records;
    } finally { await f.close(); }
  };
  const receipt = r => {
    requireValue(intent && r.job?.uid && Array.isArray(r.objects), "NATIVE_TRAEFIK_JOURNAL_MISMATCH");
    knownJobs.add(r.job.uid); objectReceipts = r.objects;
  };
  if (mode !== "apply") {
    const records = await journal(options["--journal"]);
    const header = records.shift();
    requireValue(header.schema === "kodex.stable-native-traefik-journal/v1" && header.mode === "apply" && header.planSHA256 === planHash && records.length <= 32,
      "NATIVE_TRAEFIK_JOURNAL_MISMATCH");
    if (mode === "rollback") requireValue(["COMPLETE", "UNKNOWN"].includes(records.at(-1)?.status), "NATIVE_TRAEFIK_EXECUTOR_TERMINAL_UNPROVEN");
    for (const r of records) {
      requireValue(["APPLY_INTENT", "HCC_READY", "OBSERVED_JOB", "APPLIED", "UNKNOWN", "COMPLETE"].includes(r.status), "NATIVE_TRAEFIK_JOURNAL_MISMATCH");
      if (r.status === "APPLY_INTENT") { requireValue(!intent, "NATIVE_TRAEFIK_JOURNAL_MISMATCH"); intent = true; }
      if (r.status === "HCC_READY") { requireValue(intent && r.uid && (!hccUID || hccUID === r.uid), "NATIVE_TRAEFIK_JOURNAL_MISMATCH"); hccUID = r.uid; }
      if (["OBSERVED_JOB", "APPLIED"].includes(r.status)) receipt(r);
    }
  }
  if (options["--recovery-journal"]) {
    const records = await journal(options["--recovery-journal"]), header = records.shift();
    requireValue(header.schema === "kodex.stable-native-traefik-journal/v1" && header.mode === "rollback" && header.planSHA256 === planHash &&
      records.at(-1)?.status === "COMPLETE", "NATIVE_TRAEFIK_RECOVERY_JOURNAL_MISMATCH");
    for (const r of records) {
      requireValue(["RECOVERY_INTENT", "RESTORED_HCC_PDB_RETAINED", "COMPLETE"].includes(r.status), "NATIVE_TRAEFIK_RECOVERY_JOURNAL_MISMATCH");
      if (r.status !== "COMPLETE") {
        requireValue(r.uid && (!hccUID || r.uid === hccUID), "NATIVE_TRAEFIK_RECOVERY_JOURNAL_MISMATCH"); hccUID = r.uid; receipt(r);
      }
    }
  }
  const identity = async () => {
    await assertSource();
    requireValue((await get("namespace", "kube-system")).metadata.uid === plan.clusterUID, "NATIVE_TRAEFIK_CLUSTER_CHANGED");
    const nodes = JSON.parse(await kube(["get", "nodes", "-o", "json"]));
    requireValue(nodes.items.length === plan.nodes.length, "NATIVE_TRAEFIK_NODES_CHANGED");
    for (const node of nodes.items) {
      const c = JSON.parse(await text("docker", ["inspect", "--format", "{{json .}}", node.metadata.name]));
      requireValue(fingerprint({ ...validateNodeContainer(node, c, "kodex"), role: c.Config.Labels["k3d.role"] }) ===
        fingerprint(plan.nodes.find(n => n.uid === node.metadata.uid)), "NATIVE_TRAEFIK_NODES_CHANGED");
    }
    const hc = await get("helmchart", "traefik");
    requireValue(hc.metadata.uid === row.uid && fingerprint(hc.spec) === row.specSHA256 && nativeChartMetadataHash(hc) === row.chartMetadataSHA256 &&
      fingerprint(traefikHelmConfig(hc, profile, plan.intent)) === fingerprint(row.hcc) &&
      fingerprint(traefikHelmConfig(hc, profile, plan.intent, true)) === fingerprint(row.recoveryHcc), "NATIVE_TRAEFIK_CHART_BOUNDARY_CHANGED");
    return hc;
  };
  const decode = async raw => (await text("yq", ["eval-all", "-N", "-o=json", "-I=0", ".", "-"], raw)).trim().split("\n").filter(Boolean).map(JSON.parse).filter(Boolean);
  const helm = (argv, input, timeout) => text("helm", [...argv, "--namespace", "kube-system", "--kube-context", context], input, timeout);
  const read = async () => {
    await identity(); const hcc = await get("helmchartconfig", "traefik", true);
    requireValue(!hcc || (!hccUID || hcc.metadata.uid === hccUID), "NATIVE_TRAEFIK_HCC_UID_CHANGED");
    const sourceHcc = row.sourceConfig && { ...traefikHelmConfig(await get("helmchart", "traefik"), profile, row.sourceConfig.intent, true) };
    const before = !hcc ? !row.sourceConfig : row.sourceConfig && hcc.metadata.uid === row.sourceConfig.uid && nativeSupportMatches(hcc, sourceHcc);
    const after = hcc && nativeSupportMatches(hcc, row.hcc), recovery = hcc && nativeSupportMatches(hcc, row.recoveryHcc);
    requireValue(before || intent && (after || recovery), "NATIVE_TRAEFIK_HCC_DRIFT");
    const d = await get("deployment", "traefik"), dHash = fingerprint(d.spec);
    requireValue(d.metadata.uid === target.uid && nativeDeploymentMetadataHash(d) === target.metadataSHA256 &&
      [target.beforeSHA256, target.afterSHA256].includes(dHash), "NATIVE_TRAEFIK_DEPLOYMENT_DRIFT");
    const objects = await readSystemObjects({ namespace: "kube-system" }, row.objects.map(o => ({ apiVersion: o.apiVersion, kind: o.kind,
      metadata: { name: o.name, ...(o.namespace ? { namespace: o.namespace } : {}) } })), kube);
    const observed = matchSystemObjects(row.objects, objects, intent ? ["before", "after", "recovery"] : ["before"], objectReceipts);
    const job = await get("job", "helm-install-traefik", true);
    requireValue(job || intent, "NATIVE_TRAEFIK_SOURCE_JOB_MISSING");
    const proof = job ? nativeHelmJob(job, JSON.parse(await kube(["-n", "kube-system", "get", "pods", "-l", "job-name=helm-install-traefik", "-o", "json"])), row.uid, [...knownJobs]) : null;
    requireValue(!proof || proof.image === row.sourceJob.image, "NATIVE_TRAEFIK_JOB_IMAGE_CHANGED");
    const metadata = JSON.parse(await helm(["get", "metadata", "traefik", "-o", "json"]));
    const rendered = await decode(await helm(["get", "manifest", "traefik", "--revision", String(metadata.revision)]));
    return { hcc, before, after, recovery, d, dHash, objects, observed, job: proof, metadata, manifestSHA256: fingerprint(rendered) };
  };
  const inputs = async (state, desired) => {
    requireValue(state.job.valuesKeys.includes("HelmChartValuesContent") && state.job.valuesKeys.includes("HelmChartConfigValuesContent") &&
      state.job.failurePolicy === "abort", "NATIVE_TRAEFIK_JOB_INPUT_PROFILE_INVALID");
    // Только два конкретных native values key; ни SA token, ни прочие Secret
    // data не читаются. Декодированные bytes остаются в памяти, наружу только hash.
    const raw = await kube(["-n", "kube-system", "get", "secret", "chart-values-traefik", "-o",
      'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.data.HelmChartValuesContent}{"|"}{.data.HelmChartConfigValuesContent}']);
    const fields = raw.split("|");
    requireValue(fields.length === 4 && fields[0] && /^[0-9]+$/.test(fields[1]) &&
      hash(Buffer.from(fields[2], "base64")) === row.sourceValuesSHA256 && hash(Buffer.from(fields[3], "base64")) === hash(Buffer.from(desired.spec.valuesContent)),
    "NATIVE_TRAEFIK_JOB_INPUT_CHANGED");
    return { uid: fields[0], resourceVersion: fields[1], sourceSHA256: row.sourceValuesSHA256, configSHA256: hash(Buffer.from(desired.spec.valuesContent)) };
  };
  const prove = async (state, phase) => {
    const after = phase === "after";
    requireValue(state.job?.terminal && state.job.complete && !state.job.failed && state.metadata.status === "deployed" &&
      state.metadata.revision === row.releaseRevision + (after ? 1 : 2) &&
      state.manifestSHA256 === (after ? row.afterSHA256 : row.recoverySHA256) && state.job.uid !== row.sourceJob.uid &&
      (after ? state.after : state.recovery) && state.dHash === (after ? target.afterSHA256 : target.beforeSHA256), "NATIVE_TRAEFIK_SERVING_INCOMPLETE");
    await inputs(state, after ? row.hcc : row.recoveryHcc);
    matchSystemObjects(row.objects, state.objects, [after ? "after" : "recovery"], objectReceipts);
    await systemServing({ namespace: "kube-system", deployments: ["traefik"] }, kube, [target.serving], after ? 2 : 1);
  };
  const wait = async (phase, predecessor) => {
    const deadline = Date.now() + 240000;
    for (;;) {
      const state = await read();
      requireValue(!state.job?.failed && state.metadata.status !== "failed", "NATIVE_TRAEFIK_FAILED_JOB_REQUIRES_OWNER");
      if (state.job && state.job.uid !== predecessor && state.job.terminal && state.job.complete && state.metadata.status === "deployed" &&
        state.metadata.revision === row.releaseRevision + (phase === "after" ? 1 : 2) &&
        state.manifestSHA256 === (phase === "after" ? row.afterSHA256 : row.recoverySHA256)) return state;
      requireValue(Date.now() < deadline, "NATIVE_TRAEFIK_JOB_HANDOFF_TIMEOUT"); await new Promise(resolve => setTimeout(resolve, 500));
    }
  };
  const waitServing = async phase => {
    await text("kubectl", ["--context", context, "--request-timeout=200s", "-n", "kube-system", "rollout", "status", "deployment/traefik", "--timeout=180s"], undefined, 200000);
    const observed = await read(); await prove(observed, phase); return observed;
  };
  const sourceState = state => requireValue(state.before && state.job?.uid === row.sourceJob.uid && state.job.specSHA256 === row.sourceJob.specSHA256 && state.job.terminal &&
      state.job.complete && state.metadata.revision === row.releaseRevision && state.metadata.status === "deployed" &&
      fingerprint(state.metadata) === row.metadataSHA256 && state.manifestSHA256 === row.beforeSHA256, "NATIVE_TRAEFIK_SOURCE_CHANGED");
  let state = await read();
  if (["apply", "rollback"].includes(mode)) await artifactPort(plan, config, run);
  if (mode === "apply") {
    sourceState(state);
    const hc = await identity(), values = (await decode(hc.spec.valuesContent))[0];
    if (Object.hasOwn(hc.spec.set ?? {}, "global.systemDefaultRegistry")) values.global = { ...values.global, systemDefaultRegistry: hc.spec.set["global.systemDefaultRegistry"] };
    for (const [hcc, expected] of [[row.hcc, row.afterSHA256], [row.recoveryHcc, row.recoverySHA256]]) {
      const dry = JSON.parse(await helm(["upgrade", "traefik", row.chartPath, "--reset-values", "--values", "-", "--dry-run=server", "--no-hooks", "-o", "json"],
        JSON.stringify(systemDesiredValues(values, JSON.parse(hcc.spec.valuesContent))), 180000));
      requireValue(fingerprint(await decode(dry.manifest)) === expected, "NATIVE_TRAEFIK_RENDER_DRIFT");
    }
    state = await read(); sourceState(state);
  }
  if (mode === "readback") await prove(state, "after");
  const output = await open(options["--output"], "wx", 0o600), record = async r => { await output.writeFile(JSON.stringify(r) + "\n"); await output.sync(); };
  const change = async (current, desired) => {
    await identity();
    if (!current) return JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(desired)));
    return JSON.parse(await kube(["-n", "kube-system", "patch", "helmchartconfig", "traefik", "--type=json", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify([
      { op: "test", path: "/metadata/uid", value: current.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
      { op: "test", path: "/spec", value: current.spec }, { op: "replace", path: "/spec", value: desired.spec },
      { op: "test", path: "/metadata/annotations", value: current.metadata.annotations }, { op: "replace", path: "/metadata/annotations", value: desired.metadata.annotations }])));
  };
  try {
    await record({ schema: "kodex.stable-native-traefik-journal/v1", mode, planSHA256: planHash });
    if (["inspect", "readback"].includes(mode)) { await record({ status: mode === "inspect" ? "OBSERVED" : "SERVING", complete: mode === "readback",
      job: state.job, hccUID: state.hcc?.metadata.uid ?? null, revision: state.metadata.revision }); return; }
    if (mode === "apply") {
      await record({ status: "APPLY_INTENT" }); intent = true;
      const hcc = await change(state.hcc, row.hcc);
      requireValue(nativeSupportMatches(hcc, row.hcc), "NATIVE_TRAEFIK_HCC_RECEIPT_INVALID"); hccUID = hcc.metadata.uid;
      await record({ status: "HCC_READY", uid: hccUID });
      state = await wait("after", row.sourceJob.uid); knownJobs.add(state.job.uid); objectReceipts = state.observed;
      await record({ status: "OBSERVED_JOB", job: state.job, objects: state.observed });
      state = await waitServing("after"); await record({ status: "APPLIED", job: state.job, objects: state.observed });
    } else {
      requireValue(intent, "NATIVE_TRAEFIK_APPLY_INTENT_REQUIRED");
      if (state.recovery && state.metadata.revision === row.releaseRevision + 2) { await prove(state, "recovery"); }
      else if (state.before && state.metadata.revision === row.releaseRevision && !state.after) {
        requireValue(row.sourceConfig && state.job?.terminal && state.job.complete && state.manifestSHA256 === row.beforeSHA256,
          "NATIVE_TRAEFIK_UNKNOWN_CREATE_UNRESOLVED");
        await systemServing({ namespace: "kube-system", deployments: ["traefik"] }, kube, [target.serving], 1);
      } else {
        state = await wait("after", row.sourceJob.uid); await inputs(state, row.hcc);
        hccUID = state.hcc.metadata.uid; knownJobs.add(state.job.uid); objectReceipts = state.observed;
        await record({ status: "RECOVERY_INTENT", uid: hccUID, job: state.job, objects: state.observed });
        const predecessor = state.job.uid, restored = await change(state.hcc, row.recoveryHcc);
        requireValue(restored.metadata.uid === hccUID && nativeSupportMatches(restored, row.recoveryHcc), "NATIVE_TRAEFIK_RECOVERY_RECEIPT_INVALID");
        state = await wait("recovery", predecessor); knownJobs.add(state.job.uid);
        state = await waitServing("recovery");
      }
      await record({ status: "RESTORED_HCC_PDB_RETAINED", uid: state.hcc?.metadata.uid ?? hccUID, job: state.job, objects: state.observed });
    }
    await record({ status: "COMPLETE" });
  } catch (error) { await record({ status: "UNKNOWN" }); throw error; }
  finally { await output.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releaseNativeTraefik(process.argv.slice(2)).then(() => console.log(JSON.stringify({ status: "PASS", phase: process.argv[2], directDeploymentPatch: false })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "NATIVE_TRAEFIK_RELEASE_FAILED" })); process.exitCode = 1; });
