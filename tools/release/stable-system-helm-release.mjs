#!/usr/bin/env node
import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { planSystemHelm } from "./stable-system-helm-plan.mjs";
import { stableChartLocks, verifyStableChart } from "./fetch-stable-charts.mjs";
import { matchSystemObjects, readSystemObjects, systemDesiredValues, systemRecoveryManifest,
  systemRecoveryValues, systemServing, waitSystemServing } from "./stable-system-helm-state.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
const operationDescription = (operation, phase) => `kodex-1810:${operation}:${phase}`;
const definitions = row => row.objects.map(o => ({ apiVersion: o.apiVersion, kind: o.kind,
  metadata: { name: o.name, ...(o.namespace ? { namespace: o.namespace } : {}) } }));
async function processIdentity(pid) {
  try {
    const [boot, stat] = await Promise.all([readFile("/proc/sys/kernel/random/boot_id", "utf8"), readFile(`/proc/${pid}/stat`, "utf8")]);
    return fingerprint({ boot: boot.trim(), start: stat.slice(stat.lastIndexOf(")") + 2).split(" ")[19] });
  } catch (error) { if (error.code === "ENOENT") return null; throw Error("SYSTEM_HELM_PROCESS_READ_FAILED"); }
}

export async function releaseSystemHelm(args, port, planPort = planSystemHelm, verifyChart = verifyStableChart, scopeFactory) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--charts", "--plan", "--journal", "--output", "--approved-plan-sha", "--approved-executor-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "SYSTEM_HELM_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(["apply", "continue", "inspect", "readback", "rollback"].includes(mode) && options["--context"] === "k3d-kodex" &&
    options["--plan"] && options["--output"] && options["--charts"], "SYSTEM_HELM_ARGUMENTS_INVALID");
  requireValue(mode === "apply" ? !options["--journal"] : options["--journal"], "SYSTEM_HELM_JOURNAL_REQUIRED");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 30000) => {
    try {
      const p = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 64 << 20 });
      if (input !== undefined) { p.child.stdin.on("error", () => {}); p.child.stdin.end(input); }
      return (await p).stdout;
    } catch { throw Error("SYSTEM_HELM_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const context = options["--context"], source = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(),
    "SYSTEM_HELM_CLEAN_SOURCE_REQUIRED");
  const document = await readPrivateJSON(options["--plan"]), { plan } = document, hash = fingerprint(plan);
  if (plan.source !== source) {
    requireValue(mode !== "apply" && options["--approved-executor-sha"] === source && /^[a-f0-9]{40}$/.test(plan.source ?? ""), "SYSTEM_HELM_EXECUTOR_CONFIRMATION_REQUIRED");
    await run("git", ["merge-base", "--is-ancestor", plan.source, source]);
    await run("git", ["diff", "--quiet", plan.source, source, "--", "tools/release/stable-system-helm-model.mjs", "tools/release/stable-public-helm-model.mjs",
      "tools/release/stable-public-identity-model.mjs", "infra/public-ingress/identity-profile.json", "infra/stable-system/helm-profiles.json"]);
  }
  const contextKube = (argv, input) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=15s", ...argv]) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-system-helm-pipe", "--context", context, "--request-timeout=15s", ...argv], input, 180000);
  const scope = scopeFactory ? await scopeFactory({ kube: contextKube, run, source, plan }) : null;
  const profiles = scope?.profiles ?? JSON.parse(await readFile(root + "infra/stable-system/helm-profiles.json", "utf8")).profiles;
  const desiredValues = scope?.desiredValues ?? ((p, v) => systemDesiredValues(v, p.values));
  const recoveryValuesFor = scope?.recoveryValues ?? systemRecoveryValues;
  requireValue(plan.schema === (scope?.planSchema ?? "kodex.stable-system-helm-plan/v2") && document.planSHA256 === hash &&
    (!scope || fingerprint(plan.environment) === fingerprint(scope.environment)) &&
    plan.context === context && plan.rows?.length === profiles.length && profiles.every((p, i) => {
      const r = plan.rows[i]; return r.name === p.name && r.namespace === p.namespace && r.release === p.release &&
        r.profileSHA256 === fingerprint(p) && Number.isSafeInteger(r.sourceRevision) && r.sourceRevision > 0 &&
        Array.isArray(r.objects) && r.objects.length > 0 && r.objects.length < 512 && Array.isArray(r.serving);
    }), "SYSTEM_HELM_PLAN_IDENTITY_INVALID");
  requireValue(["apply", "continue", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"],
    "SYSTEM_HELM_EXACT_OWNER_CONFIRMATION_REQUIRED");
  const kube = (argv, input) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=15s", ...argv]) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-system-helm-pipe", "--context", context, "--request-timeout=15s", ...argv], input, 180000);
  requireValue(JSON.parse(await kube(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid === plan.clusterUID,
    "SYSTEM_HELM_CLUSTER_CHANGED");
  await scope?.assertBoundary();
  const decode = async raw => (await run("yq", ["eval-all", "-N", "-o=json", "-I=0", ".", "-"], raw))
    .trim().split("\n").filter(Boolean).map(JSON.parse).filter(Boolean);
  const helm = (row, argv, input, timeout) => run("helm", [...argv, "--namespace", row.namespace, "--kube-context", context], input, timeout);
  let operation, journalRows = [], header;
  const intents = new Set(), receipts = new Map();
  if (mode !== "apply") {
    const file = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const s = await file.stat(); requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 &&
        (s.mode & 0o777) === 0o600 && s.size > 0 && s.size < (1 << 20), "SYSTEM_HELM_PRIVATE_JOURNAL_REQUIRED");
      journalRows = (await file.readFile("utf8")).trim().split("\n").map(JSON.parse);
    } finally { await file.close(); }
    header = journalRows.shift(); operation = header.operation;
    requireValue(header.schema === "kodex.stable-system-helm-journal/v1" && header.mode === "apply" &&
      header.planSHA256 === hash && /^[a-f0-9]{32}$/.test(operation ?? "") && journalRows.length <= 32,
    "SYSTEM_HELM_JOURNAL_MISMATCH");
    requireValue(Number.isSafeInteger(header.pid) && header.pid > 0 && /^[a-f0-9]{64}$/.test(header.processIdentity ?? ""),
      "SYSTEM_HELM_JOURNAL_PROCESS_INVALID");
    for (const r of journalRows) {
      requireValue(["APPLY_INTENT", "OBJECTS_OBSERVED", "APPLIED", "UNKNOWN", "COMPLETE"].includes(r.status), "SYSTEM_HELM_JOURNAL_MISMATCH");
      if (r.status === "APPLY_INTENT") {
        requireValue(plan.rows.some(p => p.name === r.name) && !intents.has(r.name), "SYSTEM_HELM_JOURNAL_MISMATCH"); intents.add(r.name);
      }
      if (["OBJECTS_OBSERVED", "APPLIED"].includes(r.status)) {
        requireValue(intents.has(r.name) && Array.isArray(r.objects) &&
          (!receipts.has(r.name) || fingerprint(receipts.get(r.name)) === fingerprint(r.objects)), "SYSTEM_HELM_JOURNAL_MISMATCH");
        receipts.set(r.name, r.objects);
      }
    }
    if (["continue", "rollback"].includes(mode) && !["COMPLETE", "UNKNOWN"].includes(journalRows.at(-1)?.status))
      requireValue(await processIdentity(header.pid) !== header.processIdentity, "SYSTEM_HELM_ORIGINAL_PROCESS_STILL_RUNNING");
  } else operation = randomBytes(16).toString("hex");
  const readRow = async row => {
    const status = JSON.parse(await helm(row, ["status", row.release, "-o", "json"]));
    requireValue(status.name === row.release && status.namespace === row.namespace &&
      ["deployed", "failed", "pending-upgrade", "pending-rollback"].includes(status.info?.status), "SYSTEM_HELM_RELEASE_STATUS_INVALID");
    const rendered = await decode(status.manifest), valuesSHA = fingerprint(status.config), manifestSHA = fingerprint(rendered);
    let phase;
    if (status.version === row.sourceRevision && manifestSHA === row.beforeSHA256 && valuesSHA === row.valuesSHA256) phase = "before";
    else if (intents.has(row.name) && status.version === row.sourceRevision + 1 &&
      status.info.description === operationDescription(operation, "apply") && manifestSHA === row.afterSHA256 && valuesSHA === row.desiredValuesSHA256) phase = "after";
    else if (intents.has(row.name) && status.version === row.sourceRevision + 2 &&
      status.info.description === operationDescription(operation, "rollback") && manifestSHA === row.recoverySHA256 && valuesSHA === row.recoveryValuesSHA256) phase = "recovery";
    requireValue(phase, "SYSTEM_HELM_FOREIGN_RELEASE_REVISION");
    const profile = profiles.find(p => p.name === row.name), live = await readSystemObjects(profile, definitions(row), kube);
    const observed = matchSystemObjects(row.objects, live, phase === "before" ? ["before"] : phase === "after" ? ["before", "after"] : ["before", "after", "recovery"], receipts.get(row.name));
    return { phase, status: status.info.status, observed, profile };
  };
  const prove = async (row, current) => {
    const phase = current.phase;
    requireValue(current.status === "deployed", "SYSTEM_HELM_SERVING_INCOMPLETE");
    matchSystemObjects(row.objects, await readSystemObjects(current.profile, definitions(row), kube), [phase], receipts.get(row.name));
    await waitSystemServing(current.profile, kube, row.serving, phase === "after" ? 2 : undefined);
  };
  const preflight = names => planPort(["--context", context, "--charts", options["--charts"], "--output", options["--output"] + ".preflight"], run,
    { inMemory: true, ...(scope ? { environment: scope.environment } : {}), ...(names ? { names } : {}) });
  const assertSource = async () => { await scope?.assertBoundary(); requireValue((await run("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim() &&
    JSON.parse(await kube(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid === plan.clusterUID,
  "SYSTEM_HELM_EXECUTOR_OR_CLUSTER_CHANGED"); };
  if (mode === "apply") {
    const fresh = await preflight();
    requireValue(fingerprint(fresh.document.plan) === hash, "SYSTEM_HELM_PLAN_DRIFT");
  }
  const observed = [];
  if (mode !== "apply") for (const row of plan.rows) observed.push({ row, current: await readRow(row) });
  if (mode === "continue") requireValue(observed.every(({ row, current }) =>
    current.phase === "after" && current.status === "deployed" && intents.has(row.name) ||
    current.phase === "before" && current.status === "deployed" && !intents.has(row.name)), "SYSTEM_HELM_UNKNOWN_MUTATION_REQUIRES_OWNER");
  if (mode === "readback") {
    requireValue(observed.every(({ current }) => current.phase === "after"), "SYSTEM_HELM_RELEASE_INCOMPLETE");
    for (const { row, current } of observed) await prove(row, current);
  }
  const output = await open(options["--output"], "wx", 0o600);
  const record = async r => { await output.writeFile(JSON.stringify(r) + "\n"); await output.sync(); };
  try {
    await record({ schema: "kodex.stable-system-helm-journal/v1", mode: mode === "continue" ? "apply" : mode, planSHA256: hash, operation,
      ...(["apply", "continue"].includes(mode) ? { pid: process.pid, processIdentity: await processIdentity(process.pid) } : {}),
      ...(mode !== "apply" ? { originalJournalSHA256: fingerprint([header, ...journalRows]) } : {}) });
    if (["inspect", "readback"].includes(mode)) {
      await record({ status: mode === "inspect" ? "OBSERVED" : "SERVING", complete: mode === "readback",
        releases: observed.map(({ row, current }) => ({ name: row.name, phase: current.phase, status: current.status })) }); return;
    }
    if (["apply", "continue"].includes(mode)) {
      for (const row of plan.rows) {
        if (mode === "continue" && intents.has(row.name)) {
          const current = await readRow(row); requireValue(current.phase === "after", "SYSTEM_HELM_CONTINUATION_DRIFT");
          await prove(row, current); receipts.set(row.name, current.observed);
          await record({ status: "APPLY_INTENT", name: row.name });
          await record({ status: "APPLIED", name: row.name, objects: current.observed }); continue;
        }
        const fresh = await preflight([row.name]), candidate = fresh.document.plan.rows[0], runtime = fresh.runtime.get(row.name);
        requireValue(fingerprint(candidate) === fingerprint(row), "SYSTEM_HELM_PLAN_DRIFT");
        await assertSource();
        await record({ status: "APPLY_INTENT", name: row.name }); intents.add(row.name);
        await helm(row, ["upgrade", row.release, runtime.chart, "--reset-values", "--values", "-", "--no-hooks",
          "--description", operationDescription(operation, "apply"), "--wait=watcher", "--timeout=180s", "-o", "json"],
        JSON.stringify(runtime.desiredValues), 210000);
        const current = await readRow(row);
        requireValue(current.phase === "after", "SYSTEM_HELM_ACTIVATION_UNPROVEN");
        receipts.set(row.name, current.observed); await record({ status: "OBJECTS_OBSERVED", name: row.name, objects: current.observed });
        await prove(row, current); await record({ status: "APPLIED", name: row.name, objects: current.observed });
      }
    } else {
      const locks = scope?.locks ?? await stableChartLocks();
      // Все releases проверены до первого restore; pending Helm operation не
      // разблокируется ручной правкой Secret и не повторяется вслепую.
      requireValue(observed.every(o => !o.current.status.startsWith("pending")), "SYSTEM_HELM_PENDING_OPERATION_UNRESOLVED");
      for (const { row, current } of [...observed].reverse()) {
        if (current.phase !== "after") { await prove(row, current); continue; }
        const lock = locks.find(l => l.name === (current.profile.chartName ?? row.name)), chart = `${options["--charts"]}/${lock.name}-${lock.version}.tgz`;
        requireValue(lock.sha256 === row.chartSHA256, "SYSTEM_HELM_CHART_CHANGED"); await verifyChart(chart, lock);
        const before = await decode(await helm(row, ["get", "manifest", row.release, "--revision", String(row.sourceRevision)]));
        const values = JSON.parse(await helm(row, ["get", "values", row.release, "--revision", String(row.sourceRevision), "-o", "json"]));
        const recovery = recoveryValuesFor(current.profile, values);
        requireValue(fingerprint(before) === row.beforeSHA256 && fingerprint(values) === row.valuesSHA256 &&
          fingerprint(desiredValues(current.profile, values)) === row.desiredValuesSHA256 &&
          fingerprint(recovery) === row.recoveryValuesSHA256, "SYSTEM_HELM_HISTORY_CHANGED");
        const dry = JSON.parse(await helm(row, ["upgrade", row.release, chart, "--reset-values", "--values", "-", "--dry-run=server", "--no-hooks", "-o", "json"], JSON.stringify(recovery), 180000));
        const rendered = await decode(dry.manifest);
        requireValue(fingerprint(rendered) === row.recoverySHA256 && fingerprint(dry.config) === row.recoveryValuesSHA256 &&
          fingerprint(systemRecoveryManifest(before, await decode(await helm(row, ["get", "manifest", row.release, "--revision", String(row.sourceRevision + 1)])))) === row.recoverySHA256,
        "SYSTEM_HELM_RECOVERY_RENDER_DRIFT");
        const fresh = await readRow(row); requireValue(fresh.phase === "after" && !fresh.status.startsWith("pending"), "SYSTEM_HELM_RECOVERY_DRIFT");
        await assertSource();
        await record({ status: "ROLLBACK_INTENT", name: row.name, objects: fresh.observed });
        receipts.set(row.name, fresh.observed);
        await helm(row, ["upgrade", row.release, chart, "--reset-values", "--values", "-", "--no-hooks",
          "--description", operationDescription(operation, "rollback"), "--wait=watcher", "--timeout=180s", "-o", "json"], JSON.stringify(recovery), 210000);
        const restored = await readRow(row);
        requireValue(restored.phase === "recovery", "SYSTEM_HELM_RECOVERY_UNPROVEN"); await prove(row, restored);
        await record({ status: "RESTORED", name: row.name, objects: restored.observed });
      }
    }
    await assertSource(); await record({ status: "COMPLETE" });
  } catch (error) { await record({ status: "UNKNOWN" }); throw error; }
  finally { await output.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releaseSystemHelm(process.argv.slice(2)).then(() => console.log(JSON.stringify({ status: "PASS", phase: process.argv[2] })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ?
      error.message : "SYSTEM_HELM_RELEASE_FAILED" })); process.exitCode = 1; });
