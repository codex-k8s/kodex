#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { lstat, open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { fingerprint } from "./stable-inventory.mjs";
import { stableChartLocks, verifyStableChart } from "./fetch-stable-charts.mjs";
import { systemHelmTransition } from "./stable-system-helm-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }

export async function planSystemHelm(args, port) {
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--charts", "--output"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "SYSTEM_HELM_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(options["--context"] === "k3d-kodex" && options["--charts"]?.startsWith("/") &&
    options["--output"]?.startsWith("/"), "SYSTEM_HELM_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 30000) => {
    try {
      const p = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 64 << 20 });
      if (input !== undefined) { p.child.stdin.on("error", () => {}); p.child.stdin.end(input); }
      return (await p).stdout;
    } catch { throw Error("SYSTEM_HELM_PREFLIGHT_FAILED"); }
  });
  const source = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(),
    "SYSTEM_HELM_CLEAN_SOURCE_REQUIRED");
  const dir = await lstat(options["--charts"]);
  requireValue(dir.isDirectory() && dir.uid === process.getuid() && (dir.mode & 0o777) === 0o700,
    "SYSTEM_HELM_CHART_DIRECTORY_INVALID");
  const profiles = JSON.parse(await readFile(root + "infra/stable-system/helm-profiles.json", "utf8"));
  requireValue(profiles.schema === "kodex.stable-system-helm/v1" && profiles.profiles.length === 3, "SYSTEM_HELM_PROFILE_INVALID");
  const locks = await stableChartLocks(), context = options["--context"];
  const kube = args => run("kubectl", ["--context", context, "--request-timeout=15s", ...args]);
  const clusterUID = JSON.parse(await kube(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid;
  const decode = async raw => (await run("yq", ["eval-all", "-N", "-o=json", "-I=0", ".", "-"], raw))
    .trim().split("\n").filter(Boolean).map(JSON.parse).filter(Boolean);
  const rows = [];
  for (const profile of profiles.profiles) {
    const lock = locks.find(l => l.name === profile.name);
    requireValue(lock, "SYSTEM_HELM_CHART_LOCK_MISSING");
    const chart = `${options["--charts"]}/${lock.name}-${lock.version}.tgz`;
    await verifyStableChart(chart, lock);
    const helm = args => run("helm", [...args, "--namespace", profile.namespace, "--kube-context", context], undefined, 180000);
    const metadata = JSON.parse(await helm(["get", "metadata", profile.release, "-o", "json"]));
    requireValue(metadata.name === profile.release && metadata.namespace === profile.namespace &&
      metadata.chart === lock.name && metadata.version === lock.version && metadata.status === "deployed" &&
      Number.isSafeInteger(metadata.revision) && metadata.revision > 0, "SYSTEM_HELM_RELEASE_IDENTITY_INVALID");
    const revision = String(metadata.revision);
    const values = JSON.parse(await helm(["get", "values", profile.release, "--revision", revision, "-o", "json"]));
    const before = await decode(await helm(["get", "manifest", profile.release, "--revision", revision]));
    // --dry-run=server может содержать Secret. Весь ответ остаётся в памяти;
    // наружу выдаются только commitment и закрытый перечень разрешённых объектов.
    const dryRun = JSON.parse(await run("helm", ["upgrade", profile.release, chart, "--namespace", profile.namespace,
      "--kube-context", context, "--reuse-values", "--values", "-", "--dry-run=server", "--no-hooks", "-o", "json"],
    JSON.stringify(profile.values), 180000));
    requireValue(typeof dryRun.manifest === "string" && dryRun.name === profile.release &&
      dryRun.namespace === profile.namespace && dryRun.version === metadata.revision + 1,
    "SYSTEM_HELM_DRY_RUN_IDENTITY_INVALID");
    const after = await decode(dryRun.manifest), transition = systemHelmTransition(profile, before, after), targets = [];
    for (const name of profile.deployments) {
      const live = JSON.parse(await kube(["-n", profile.namespace, "get", "deployment", name, "-o", "json"]));
      requireValue(live.metadata?.uid && live.metadata?.resourceVersion &&
        live.metadata.annotations?.["meta.helm.sh/release-name"] === profile.release &&
        live.metadata.annotations?.["meta.helm.sh/release-namespace"] === profile.namespace &&
        live.status?.observedGeneration === live.metadata.generation && live.status.readyReplicas === live.spec.replicas &&
        [1, 2].includes(live.spec.replicas), "SYSTEM_HELM_LIVE_SOURCE_INVALID");
      const args = live.spec.template.spec.containers.flatMap(c => [...(c.command ?? []), ...(c.args ?? [])]);
      requireValue(!args.some(a => /--leader-elect(?:ion)?=false/.test(a)), "SYSTEM_HELM_LEADER_DISABLED");
      targets.push({ name, uid: live.metadata.uid, resourceVersion: live.metadata.resourceVersion, specSHA256: fingerprint(live.spec) });
    }
    const fresh = JSON.parse(await helm(["get", "metadata", profile.release, "-o", "json"]));
    const freshValues = JSON.parse(await helm(["get", "values", profile.release, "--revision", revision, "-o", "json"]));
    requireValue(fingerprint(fresh) === fingerprint(metadata) && fingerprint(freshValues) === fingerprint(values),
      "SYSTEM_HELM_SOURCE_CHANGED_DURING_PLAN");
    rows.push({ name: profile.name, namespace: profile.namespace, release: profile.release, sourceRevision: metadata.revision,
      metadataSHA256: fingerprint(metadata), valuesSHA256: fingerprint(values), profileSHA256: fingerprint(profile),
      chartSHA256: lock.sha256, targets, ...transition });
  }
  requireValue(JSON.parse(await kube(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid === clusterUID,
    "SYSTEM_HELM_CLUSTER_CHANGED");
  const plan = { schema: "kodex.stable-system-helm-plan/v1", source, context, clusterUID, rows, apply: "NOT_RUN" };
  const document = { plan, planSHA256: fingerprint(plan) }, output = await open(options["--output"],
    constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await output.writeFile(JSON.stringify(document) + "\n"); await output.sync(); } finally { await output.close(); }
  return document;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  planSystemHelm(process.argv.slice(2)).then(d => console.log(JSON.stringify({ status: "PASS", source: d.plan.source,
    planSHA256: d.planSHA256, releases: d.plan.rows.map(r => ({ name: r.release, sourceRevision: r.sourceRevision,
      changes: r.changes.map(c => ({ kind: c.kind, name: c.name })) })), apply: "NOT_RUN" })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message ?? "") ?
      error.message : "SYSTEM_HELM_PLAN_FAILED" })); process.exitCode = 1; });
