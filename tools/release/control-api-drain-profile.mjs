#!/usr/bin/env node

import { execFile } from "node:child_process";
import { randomUUID } from "node:crypto";
import { open } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { fingerprint } from "./scoped-release.mjs";

const execute = promisify(execFile);
const annotation = "kodex.dev/http-drain-profile";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
function requireValue(condition, code) { if (!condition) throw new Error(code); }

// Однократная миграция собственного lifecycle, не обычный application release.
// Образы, исходники, Secrets и состояние authority не меняются.
export function planDrainProfile(deployment) {
  const metadata = deployment?.metadata;
  const spec = deployment?.spec;
  requireValue(deployment?.kind === "Deployment" && deployment.apiVersion === "apps/v1" &&
    metadata?.name === "control-api-gateway" && metadata.namespace === "kodex-system" && uuid.test(metadata.uid) &&
    metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    (metadata.labels?.["kodex.dev/local-profile"] === "hot-reload" || metadata.labels?.["kodex.dev/environment"] === "staging"), "STAGING_API_IDENTITY_REQUIRED");
  requireValue(spec.strategy?.type === "RollingUpdate" && spec.strategy.rollingUpdate?.maxUnavailable === 0 &&
    Number.isSafeInteger(spec.replicas) && spec.replicas > 0 &&
    deployment.status?.observedGeneration >= metadata.generation && deployment.status.availableReplicas >= spec.replicas &&
    deployment.status.updatedReplicas === spec.replicas && deployment.status.replicas === spec.replicas, "COMPLETE_AVAILABLE_ROLLOUT_REQUIRED");
  const after = structuredClone(spec);
  const patch = [{ op: "test", path: "/metadata/uid", value: metadata.uid },
    { op: "test", path: "/metadata/resourceVersion", value: metadata.resourceVersion }];
  for (const [name, seconds] of [["control-api-gateway", 10], ["internal-rpc-authority-issuer", 40]]) {
    const index = spec.template.spec.containers.findIndex((container) => container.name === name);
    requireValue(index >= 0 && spec.template.spec.containers.filter((container) => container.name === name).length === 1, "DRAIN_CONTAINER_MISSING");
    const container = spec.template.spec.containers[index];
    if (name === "control-api-gateway")
      requireValue((container.env ?? []).filter((item) => item.name === "CONTROL_API_GATEWAY_SHUTDOWN_TIMEOUT" && item.value === "20s").length === 1, "DRAIN_BUDGET_MISMATCH");
    const preStop = { sleep: { seconds } };
    requireValue(!container.lifecycle?.preStop || fingerprint(container.lifecycle.preStop) === fingerprint(preStop), "EXISTING_DRAIN_HOOK_CONFLICT");
    const lifecycle = { ...(container.lifecycle ?? {}), preStop };
    after.template.spec.containers[index].lifecycle = lifecycle;
    patch.push({ op: "test", path: `/spec/template/spec/containers/${index}/name`, value: name },
      { op: "add", path: `/spec/template/spec/containers/${index}/lifecycle`, value: lifecycle });
  }
  after.template.spec.terminationGracePeriodSeconds = Math.max(spec.template.spec.terminationGracePeriodSeconds ?? 30, 120);
  after.template.metadata.annotations = { ...(spec.template.metadata.annotations ?? {}), [annotation]: "1" };
  patch.push({ op: "add", path: "/spec/template/spec/terminationGracePeriodSeconds", value: after.template.spec.terminationGracePeriodSeconds },
    { op: "add", path: "/spec/template/metadata/annotations", value: after.template.metadata.annotations });
  return { patch, beforeSpecSHA256: fingerprint(spec), afterSpecSHA256: fingerprint(after), changed: fingerprint(spec) !== fingerprint(after) };
}

function supportsSleep(version) { const match = /^v1\.(\d+)\./.exec(version ?? ""); return match && Number(match[1]) >= 34; }

async function main() {
  const args = process.argv.slice(2);
  requireValue(args.length === 6 && args[0] === "--context" && args[2] === "--evidence" && args[4] === "--confirm" &&
    args[5] === "APPLY-STAGING-API-DRAIN", "INVALID_DRAIN_ARGUMENTS");
  const context = args[1];
  requireValue(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context) && !/prod(?:uction)?/i.test(context), "STAGING_CONTEXT_REQUIRED");
  const kubectl = async (command, timeout = 35000) => {
    try { return (await execute("kubectl", ["--context", context, "--request-timeout=30s", ...command], { timeout, maxBuffer: 4 << 20, encoding: "utf8" })).stdout; }
    catch { throw new Error("KUBERNETES_OPERATION_FAILED"); }
  };
  requireValue(supportsSleep(JSON.parse(await kubectl(["version", "-o", "json"])).serverVersion?.gitVersion), "KUBERNETES_SLEEP_SUPPORT_REQUIRED");
  const nodes = JSON.parse(await kubectl(["get", "nodes", "-o", "json"])).items;
  requireValue(nodes.length > 0 && nodes.every((node) => supportsSleep(node.status?.nodeInfo?.kubeletVersion)), "KUBELET_SLEEP_SUPPORT_REQUIRED");
  const clusterUID = JSON.parse(await kubectl(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid;
  const get = async () => JSON.parse(await kubectl(["-n", "kodex-system", "get", "deployment", "control-api-gateway", "-o", "json"]));
  const before = await get();
  const plan = planDrainProfile(before);
  const journal = await open(args[3], "wx", 0o600);
  const record = async (value) => { await journal.writeFile(`${JSON.stringify({ at: new Date().toISOString(), ...value })}\n`); await journal.sync(); };
  try {
    await record({ version: 1, operationID: randomUUID(), clusterUID, deploymentUID: before.metadata.uid,
      beforeSpecSHA256: plan.beforeSpecSHA256, afterSpecSHA256: plan.afterSpecSHA256, status: plan.changed ? "PATCH_ATTEMPT" : "ALREADY_CONFIGURED" });
    if (plan.changed) {
      await kubectl(["-n", "kodex-system", "patch", "deployment", "control-api-gateway", "--type=json", "-p", JSON.stringify(plan.patch), "-o", "name"]);
      await kubectl(["-n", "kodex-system", "rollout", "status", "deployment/control-api-gateway", "--timeout=300s"], 335000);
    }
    const after = await get();
    requireValue(after.metadata.uid === before.metadata.uid && fingerprint(after.spec) === plan.afterSpecSHA256 &&
      after.status.observedGeneration >= after.metadata.generation && after.status.availableReplicas >= after.spec.replicas &&
      after.status.updatedReplicas === after.spec.replicas && after.status.replicas === after.spec.replicas, "DRAIN_READBACK_MISMATCH");
    await record({ status: "PASS", profile: "1" });
    process.stdout.write("Control API drain profile PASS\n");
  } catch (error) { await record({ status: "FAIL", code: /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "DRAIN_PROFILE_FAILED" }); throw error; }
  finally { await journal.close(); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { await main(); }
  catch (error) { process.stderr.write(`Control API drain profile failed: ${/^[A-Z_]{1,80}$/.test(error.message) ? error.message : "DRAIN_PROFILE_FAILED"}\n`); process.exitCode = 1; }
}
