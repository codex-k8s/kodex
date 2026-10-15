import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { fetchStableCharts, stableChartLocks, verifyStableChart } from "./fetch-stable-charts.mjs";

test("chart locks contain only the three existing installed chart families", async () => {
  const locks = await stableChartLocks();
  assert.deepEqual(locks.map(l => l.name), ["cert-manager", "trust-manager", "kube-prometheus-stack"]);
  assert.equal(locks.every(l => /^[a-f0-9]{64}$/.test(l.sha256)), true);
});

test("chart verifier rejects changed content, permissions and symlink", async t => {
  const dir = await mkdtemp(tmpdir() + "/kodex-chart-fixture-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const path = dir + "/chart.tgz", data = Buffer.from("fixture chart bytes"), lock = { sha256: createHash("sha256").update(data).digest("hex") };
  await writeFile(path, data, { mode: 0o600 });
  assert.deepEqual(await verifyStableChart(path, lock), data);
  await assert.rejects(verifyStableChart(path, { sha256: "a".repeat(64) }), /CHART_HASH_MISMATCH/);
  await chmod(path, 0o644); await assert.rejects(verifyStableChart(path, lock), /CHART_FILE_REJECTED/);
  await chmod(path, 0o600); await symlink(path, dir + "/link"); await assert.rejects(verifyStableChart(dir + "/link", lock));
});

test("public identity downloads use only the two locked existing chart families", async () => {
  const locks = await stableChartLocks("public-identity");
  assert.deepEqual(locks.map(l => l.name), ["oauth2-proxy", "kube-prometheus-stack"]);
  assert.equal(locks[0].version, "10.7.0");
  await assert.rejects(stableChartLocks("arbitrary"), /CHART_PROFILE_INVALID/);
});

test("fetch neither overwrites a directory nor accepts relative targets", async t => {
  const dir = await mkdtemp(tmpdir() + "/kodex-chart-existing-"); t.after(() => rm(dir, { recursive: true, force: true }));
  let calls = 0; const port = async () => { calls++; };
  await assert.rejects(fetchStableCharts("relative", port), /CHART_OUTPUT_INVALID/);
  await assert.rejects(fetchStableCharts(dir, port), /EEXIST/);
  assert.equal(calls, 0);
});
