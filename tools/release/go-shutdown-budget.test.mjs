import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

test("Air runtime budget encloses the actual controller drain and fits the Pod grace", () => {
  const script = fileURLToPath(new URL("../dev/go-shutdown-budget.sh", import.meta.url));
  const actual = execFileSync("sh", [script, "runtime-controller"], { encoding: "utf8" }).trim();
  assert.match(actual, /^\d+s$/);
  const budget = Number(actual.slice(0, -1));
  const manager = readFileSync(new URL("../../services/internal/runtime-controller/internal/workload/manager.go", import.meta.url), "utf8");
  const deployment = readFileSync(new URL("../../deploy/k8s/base/runtime-controller/deployment.yaml", import.meta.url), "utf8");
  const drain = Number(/ControllerShutdownDrain = (\d+) \* time.Second/.exec(manager)?.[1]);
  const grace = Number(/terminationGracePeriodSeconds: (\d+)/.exec(deployment)?.[1]);
  assert.ok(Number.isFinite(drain) && Number.isFinite(grace));
  assert.ok(budget >= drain + 20, "Air must allow controller drain and bounded cleanup");
  assert.ok(grace >= budget + 10, "Pod grace must also cover native sidecar shutdown");
  assert.equal(execFileSync("sh", [script, "control-plane"], { encoding: "utf8" }).trim(), "90s");
});
