#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { mkdir, open, readdir, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }
export async function stableChartLocks() {
  const sources = [["tools/install/components.lock.json", "cert-manager", "oci://quay.io/jetstack/charts/cert-manager"],
    ["infra/service-infrastructure/charts.lock.json", "trust-manager", "oci://quay.io/jetstack/charts/trust-manager"],
    ["infra/management-surfaces/charts.lock.json", "kube-prometheus-stack", "https://prometheus-community.github.io/helm-charts"]];
  return Promise.all(sources.map(async ([path, name, repository]) => {
    const lock = JSON.parse(await readFile(root + path, "utf8"));
    const matches = lock.charts?.filter(c => c.name === name);
    requireValue(lock.schemaVersion === 1 && matches?.length === 1, "CHART_LOCK_INVALID");
    const c = matches[0];
    requireValue(c.repository === repository && /^[v]?[0-9]+\.[0-9]+\.[0-9]+$/.test(c.version) &&
      /^[a-f0-9]{64}$/.test(c.sha256) && (!c.chart || c.chart === name), "CHART_LOCK_INVALID");
    return { name, repository, version: c.version, sha256: c.sha256 };
  }));
}

export async function verifyStableChart(path, lock) {
  const f = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const s = await f.stat();
    requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 && !(s.mode & 0o077) &&
      s.size > 0 && s.size < (32 << 20), "CHART_FILE_REJECTED");
    const bytes = await f.readFile();
    requireValue(createHash("sha256").update(bytes).digest("hex") === lock.sha256, "CHART_HASH_MISMATCH");
    return bytes;
  } finally { await f.close(); }
}

export async function fetchStableCharts(output, port) {
  requireValue(typeof output === "string" && output.startsWith("/") && !output.split("/").includes(".."), "CHART_OUTPUT_INVALID");
  // Новый приватный каталог; пользовательские Helm credentials и repositories не читаются.
  await mkdir(output, { mode: 0o700 });
  for (const sub of ["config", "cache", "data"]) await mkdir(output + "/" + sub, { mode: 0o700 });
  const env = { PATH: process.env.PATH, HOME: output, LANG: "C.UTF-8", HELM_CONFIG_HOME: output + "/config",
    HELM_CACHE_HOME: output + "/cache", HELM_DATA_HOME: output + "/data" };
  const run = port ?? (async args => {
    try { return (await execute("helm", args, { env, timeout: 120000, maxBuffer: 2 << 20 })).stdout; }
    catch { throw Error("CHART_DOWNLOAD_FAILED"); }
  });
  const source = (await execute("git", ["rev-parse", "HEAD"], { cwd: root, env, timeout: 10000 })).stdout.trim();
  requireValue(/^[a-f0-9]{40}$/.test(source), "CHART_SOURCE_INVALID");
  const result = [];
  for (const lock of await stableChartLocks()) {
    const args = ["pull", ...(lock.repository.startsWith("oci:") ? [lock.repository] : [lock.name, "--repo", lock.repository]),
      "--version", lock.version, "--destination", output];
    await run(args);
    const files = (await readdir(output)).filter(name => name.startsWith(lock.name + "-") && name.endsWith(".tgz"));
    requireValue(files.length === 1, "CHART_ARCHIVE_AMBIGUOUS");
    const path = output + "/" + files[0];
    const f = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const s = await f.stat();
      requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 && s.size > 0 && s.size < (32 << 20),
        "CHART_FILE_REJECTED");
      await f.chmod(0o600);
    } finally { await f.close(); }
    await verifyStableChart(path, lock);
    result.push({ ...lock, file: files[0] });
  }
  const manifest = { schema: "kodex.stable-charts/v1", source, charts: result };
  const f = await open(output + "/manifest.json", "wx", 0o600);
  try { await f.writeFile(JSON.stringify(manifest) + "\n"); await f.sync(); } finally { await f.close(); }
  return manifest;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const args = process.argv.slice(2);
  (async () => {
    requireValue(args.length === 2 && args[0] === "--output", "CHART_ARGUMENTS_INVALID");
    const result = await fetchStableCharts(args[1]);
    console.log(JSON.stringify({ status: "PASS", ...result }));
  })().catch(error => { console.error(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message ?? "") ?
    error.message : "CHART_FETCH_FAILED" })); process.exitCode = 1; });
}
