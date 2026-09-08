#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { closeSync, fsyncSync, openSync, writeSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";

const component = "staff-control-center";
const hook = { sleep: { seconds: 10 } };
function requireValue(condition, code) { if (!condition) throw new Error(code); }

export function planFrontendDrainProfile(deployment) {
  const { metadata: m, spec, status } = deployment ?? {};
  requireValue(deployment?.apiVersion === "apps/v1" && deployment.kind === "Deployment" &&
    m?.name === component && m.namespace === "kodex-system" && typeof m.uid === "string" &&
    typeof m.resourceVersion === "string" && m.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    m.labels?.["kodex.dev/local-profile"] === "hot-reload", "FRONTEND_PROFILE_IDENTITY_INVALID");
  requireValue(!spec.paused && Number.isSafeInteger(spec.replicas) && spec.replicas > 0 &&
    spec.strategy?.type === "RollingUpdate" && spec.strategy.rollingUpdate?.maxUnavailable === 0 &&
    Number.isSafeInteger(spec.strategy.rollingUpdate.maxSurge) && spec.strategy.rollingUpdate.maxSurge > 0 &&
    status?.observedGeneration >= m.generation && status.availableReplicas >= spec.replicas &&
    status.updatedReplicas === spec.replicas && status.replicas === spec.replicas, "FRONTEND_ROLLOUT_NOT_READY");
  const containers = spec.template?.spec?.containers;
  requireValue(containers?.length === 1 && containers[0].name === component &&
    spec.template.metadata?.annotations?.["kubectl.kubernetes.io/default-container"] === component,
  "FRONTEND_CONTAINER_AMBIGUOUS");
  const beforeHook = containers[0].lifecycle?.preStop;
  requireValue(beforeHook === undefined || fingerprint(beforeHook) === fingerprint(hook), "FRONTEND_CUSTOM_PRESTOP_PRESENT");
  const lifecycle = { ...(containers[0].lifecycle ?? {}), preStop: hook };
  const grace = spec.template.spec.terminationGracePeriodSeconds ?? 30;
  requireValue(Number.isSafeInteger(grace) && grace >= 0, "FRONTEND_GRACE_INVALID");
  const annotations = { ...spec.template.metadata.annotations, "kodex.dev/frontend-drain-profile": "1" };
  const after = structuredClone(spec);
  after.template.spec.containers[0].lifecycle = lifecycle;
  after.template.spec.terminationGracePeriodSeconds = Math.max(grace, 45);
  after.template.metadata.annotations = annotations;
  return {
    uid: m.uid, beforeSpecSHA256: fingerprint(spec), afterSpecSHA256: fingerprint(after),
    patch: [
      { op: "test", path: "/metadata/uid", value: m.uid },
      { op: "test", path: "/metadata/resourceVersion", value: m.resourceVersion },
      { op: "test", path: "/spec/template/spec/containers/0/name", value: component },
      { op: "add", path: "/spec/template/spec/containers/0/lifecycle", value: lifecycle },
      { op: "add", path: "/spec/template/spec/terminationGracePeriodSeconds", value: after.template.spec.terminationGracePeriodSeconds },
      { op: "add", path: "/spec/template/metadata/annotations", value: annotations },
    ],
  };
}

export function supportsFrontendDrain(version) {
  const match = /^v?(\d+)\.(\d+)(?:\.|$)/.exec(version ?? "");
  return !!match && (Number(match[1]) > 1 || (Number(match[1]) === 1 && Number(match[2]) >= 34));
}

function main(args) {
  requireValue(args.length === 6 && args[0] === "--context" && args[2] === "--evidence" &&
    args[4] === "--confirm" && args[5] === "APPLY-STAGING-FRONTEND-DRAIN" &&
    /^[A-Za-z0-9_.:@/-]{1,160}$/.test(args[1]) && !/prod(?:uction)?/i.test(args[1]), "FRONTEND_DRAIN_ARGUMENTS_INVALID");
  const kubectl = (options, timeout = 35000) => execFileSync("kubectl", ["--context", args[1], "--request-timeout=30s", ...options],
    { encoding: "utf8", timeout, maxBuffer: 4 << 20, stdio: ["ignore", "pipe", "pipe"] });
  const get = () => JSON.parse(kubectl(["-n", "kodex-system", "get", "deployment", component, "-o", "json"]));
  const version = JSON.parse(kubectl(["version", "-o", "json"]));
  const nodes = JSON.parse(kubectl(["get", "nodes", "-o", "json"]));
  requireValue(supportsFrontendDrain(version.serverVersion?.gitVersion) && nodes.items?.length > 0 &&
    nodes.items.every((node) => supportsFrontendDrain(node.status?.nodeInfo?.kubeletVersion)), "FRONTEND_SLEEP_ACTION_UNSUPPORTED");
  const before = get(), plan = planFrontendDrainProfile(before);
  const descriptor = openSync(args[3], "wx", 0o600);
  const record = (value) => { writeSync(descriptor, `${JSON.stringify({ at: new Date().toISOString(), ...value })}\n`); fsyncSync(descriptor); };
  try {
    record({ status: "IN_PROGRESS", uid: plan.uid, beforeSpecSHA256: plan.beforeSpecSHA256, afterSpecSHA256: plan.afterSpecSHA256 });
    if (plan.beforeSpecSHA256 !== plan.afterSpecSHA256) {
      record({ status: "PATCH_ATTEMPT" });
      kubectl(["-n", "kodex-system", "patch", "deployment", component, "--type=json", "-p", JSON.stringify(plan.patch), "-o", "name"]);
      kubectl(["-n", "kodex-system", "rollout", "status", `deployment/${component}`, "--timeout=300s"], 335000);
    }
    const after = get();
    requireValue(after.metadata.uid === plan.uid && fingerprint(after.spec) === plan.afterSpecSHA256,
      "FRONTEND_DRAIN_READBACK_MISMATCH");
    planFrontendDrainProfile(after);
    record({ status: "PASS", uid: plan.uid, afterSpecSHA256: plan.afterSpecSHA256 });
    process.stdout.write("Frontend drain profile PASS\n");
  } catch (error) { record({ status: "FAIL", code: /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "FRONTEND_DRAIN_OPERATION_FAILED" }); throw error; }
  finally { closeSync(descriptor); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); }
  catch (error) {
    const code = /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "FRONTEND_DRAIN_OPERATION_FAILED";
    process.stderr.write(`Frontend drain profile failed: ${code}\n`);
    process.exitCode = 1;
  }
}
