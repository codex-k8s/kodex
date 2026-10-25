#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { lstatSync, realpathSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { randomUUID } from "node:crypto";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { privateJournal } from "../dev/role-image-acceptance.mjs";
import { createPrivatePrettyJSONSync } from "./private-json-write.mjs";
import { fingerprint, isApplicationTargetName, kubectlInvocation } from "./scoped-release.mjs";
import { observeApplicationRestart, planApplicationRestart, prepareApplicationRestart, restartFailureCode, restartGuard, validateRestartPlan } from "./application-restart-model.mjs";

const requireValue = restartGuard;
const safeCode = restartFailureCode;

// Проверяется только owned private путь; план/receipt создаются один раз, без overwrite и symlink traversal.
function outputPath(path) {
  requireValue(typeof path === "string", "PRIVATE_OUTPUT_REQUIRED");
  path = resolve(path);
  const parent = lstatSync(dirname(path));
  requireValue(realpathSync(dirname(path)) === dirname(path) && parent.isDirectory() && parent.uid === process.getuid() &&
    (parent.mode & 0o777) === 0o700 && lstatSync(path, { throwIfNoEntry: false }) === undefined, "PRIVATE_OUTPUT_REQUIRED");
  return path;
}

// Журнал принадлежит тому же plan/operator/cluster/target origin; readback не создаёт lock или новые записи.
function journalHeader(plan) {
  return { version: 1, kind: "APPLICATION_SAME_SOURCE_RESTART", planSHA256: fingerprint(plan),
    executorSHA: plan.executorSHA, restartID: plan.restartID, clusterUID: plan.clusterUID,
    namespaceUID: plan.namespaceUID, target: plan.target.name, deploymentUID: plan.target.uid };
}

// Transport читает только namespace и выбранный Deployment/RS/Pods; Secret, exec и arbitrary kubectl verbs отсутствуют.
export function applicationRestartRuntime(context, accessProfile, executorSHA, budgetSeconds, execute = execFileSync) {
  const deadline = Date.now() + budgetSeconds * 1000;
  const env = Object.fromEntries(["PATH", "HOME", "KUBECONFIG"].filter(key => process.env[key]).map(key => [key, process.env[key]]));
  const kube = (args, input, requested = 30_000) => {
    requireValue(Date.now() < deadline, "APPLICATION_RESTART_BUDGET_EXHAUSTED");
    const invocation = kubectlInvocation(context, args, accessProfile === "k3s-sudo");
    try {
      return execute(invocation.file, invocation.arguments, { input, env, encoding: "utf8", stdio: ["pipe", "pipe", "pipe"],
        maxBuffer: 4 << 20, timeout: Math.max(1, Math.min(requested, deadline - Date.now())) });
    } catch { requireValue(false, "KUBERNETES_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const get = (kind, name, namespace = "kodex-system") => JSON.parse(kube(["-n", namespace, "get", kind, name, "-o", "json"]));
  return { context, accessProfile, executorSHA,
    identities() {
      const cluster = get("namespace", "kube-system"), namespace = get("namespace", "kodex-system");
      requireValue(namespace.metadata.labels?.["kodex.dev/environment"] === "staging", "STAGING_NAMESPACE_REQUIRED");
      return { clusterUID: cluster.metadata.uid, namespaceUID: namespace.metadata.uid };
    },
    snapshot(name) {
      requireValue(isApplicationTargetName(name), "INVALID_TARGET");
      const deployment = get("deployment", name);
      const component = deployment.spec?.template?.metadata?.labels?.["app.kubernetes.io/component"];
      requireValue(/^[a-z][a-z0-9-]{0,62}$/.test(component ?? ""), "APPLICATION_COMPONENT_REQUIRED");
      const resources = JSON.parse(kube(["-n", "kodex-system", "get", "replicasets,pods", "-l",
        `app.kubernetes.io/name=${name},app.kubernetes.io/component=${component}`, "-o", "json"]));
      return { deployment, resources };
    },
    patch(name, patch) { kube(["-n", "kodex-system", "patch", "deployment", name, "--type=json", "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify(patch)); },
    wait(name, seconds) { kube(["-n", "kodex-system", "rollout", "status", `deployment/${name}`, `--timeout=${seconds}s`], undefined, (seconds + 5) * 1000); },
  };
}

// Выбор режима закрыт: apply допускает один intent/CAS, а любой неизвестный исход продолжается только readback.
// Runtime port нужен также offline regression; он не является CLI-входом и не расширяет Kubernetes authority.
export function restartStep(command, plan, approvedSHA, evidence, rt, seconds = 300) {
  validateRestartPlan(plan);
  requireValue(Number.isSafeInteger(seconds) && seconds >= 30 && seconds <= 600, "APPLICATION_RESTART_BUDGET_INVALID");
  requireValue(fingerprint(plan) === approvedSHA && rt.context === plan.context && rt.accessProfile === plan.accessProfile &&
    rt.executorSHA === plan.executorSHA, "APPLICATION_RESTART_PLAN_MISMATCH");
  const identities = rt.identities();
  requireValue(identities.clusterUID === plan.clusterUID && identities.namespaceUID === plan.namespaceUID, "APPLICATION_RESTART_SCOPE_CHANGED");
  requireValue(["apply", "readback"].includes(command), "INVALID_COMMAND");
  if (command === "readback") {
    const info = lstatSync(evidence);
    requireValue(info.isFile() && info.uid === process.getuid() && (info.mode & 0o777) === 0o600 && info.nlink === 1,
      "PRIVATE_JOURNAL_REQUIRED");
  }
  const journal = privateJournal(evidence, journalHeader(plan), { createOnly: command === "apply", readOnly: command === "readback" });
  try {
    if (command === "apply") {
      const before = rt.snapshot(plan.target.name);
      const patch = prepareApplicationRestart(plan, before.deployment, before.resources);
      journal.append({ type: "INTENT", restartID: plan.restartID, beforeSpecSHA256: plan.target.beforeSpecSHA256,
        afterSpecSHA256: plan.target.afterSpecSHA256, resourceVersion: plan.target.resourceVersion });
      try {
        rt.patch(plan.target.name, patch);
        journal.append({ type: "ACK", restartID: plan.restartID });
        rt.wait(plan.target.name, seconds);
      } catch (error) {
        const result = { status: "UNKNOWN", code: safeCode(error) };
        journal.append({ type: "UNKNOWN", ...result });
        return result;
      }
    } else {
      const intents = journal.events.filter(event => event.type === "INTENT");
      requireValue(intents.length <= 1 && intents.every(event => event.restartID === plan.restartID &&
        event.beforeSpecSHA256 === plan.target.beforeSpecSHA256 && event.afterSpecSHA256 === plan.target.afterSpecSHA256 &&
        event.resourceVersion === plan.target.resourceVersion), "APPLICATION_RESTART_INTENT_MISMATCH");
      if (intents.length === 0) return { status: "NOT_STARTED", code: "APPLICATION_RESTART_NO_INTENT" };
    }
    let observed;
    try {
      const after = rt.snapshot(plan.target.name);
      observed = observeApplicationRestart(plan, after.deployment, after.resources);
    } catch (error) { observed = { status: "UNKNOWN", code: safeCode(error) }; }
    if (command === "apply") journal.append({ type: "RESULT", ...observed });
    return observed;
  } finally { journal.close(); }
}

// Private план создаётся read-only наблюдением. Approval — canonical plan hash, а не новый release/provenance ID.
export async function restartCLI(args, makeRuntime) {
  const command = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--target", "--plan", "--output", "--evidence", "--approved-plan-sha", "--confirm", "--timeout-seconds", "--k3s-sudo"].includes(key) &&
      !Object.hasOwn(options, key), "INVALID_ARGUMENTS");
    if (key === "--k3s-sudo") options[key] = true;
    else { requireValue(args.length > 0, "INVALID_ARGUMENTS"); options[key] = args.shift(); }
  }
  requireValue(["plan", "apply", "readback"].includes(command), "INVALID_COMMAND");
  const context = options["--context"], accessProfile = options["--k3s-sudo"] ? "k3s-sudo" : "kubectl";
  requireValue(typeof context === "string" && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(context) && !/prod(?:uction)?/i.test(context), "EXACT_STAGING_CONTEXT_REQUIRED");
  const seconds = Number(options["--timeout-seconds"] ?? 300);
  requireValue(Number.isSafeInteger(seconds) && seconds >= 30 && seconds <= 600, "APPLICATION_RESTART_BUDGET_INVALID");
  const output = outputPath(options["--output"]);
  if (command === "plan") requireValue(isApplicationTargetName(options["--target"]) &&
    !options["--plan"] && !options["--evidence"] && !options["--approved-plan-sha"] && !options["--confirm"], "INVALID_PLAN_ARGUMENTS");
  else requireValue(options["--plan"] && options["--evidence"] && /^[a-f0-9]{64}$/.test(options["--approved-plan-sha"] ?? "") &&
    !options["--target"] && (command === "apply" ? options["--confirm"] === "RESTART-STAGING-APPLICATION" : options["--confirm"] === undefined), "APPLICATION_RESTART_CONFIRMATION_REQUIRED");
  if (command !== "plan") requireValue(new Set([output, resolve(options["--plan"]), resolve(options["--evidence"])]).size === 3,
    "APPLICATION_RESTART_PATHS_OVERLAP");
  const rt = makeRuntime(context, accessProfile, seconds + 120);
  if (command === "plan") {
    const identities = rt.identities(), snapshot = rt.snapshot(options["--target"]);
    const plan = planApplicationRestart({ context, accessProfile, executorSHA: rt.executorSHA, restartID: randomUUID(),
      ...identities, name: options["--target"], ...snapshot });
    createPrivatePrettyJSONSync(output, plan);
    return { status: "PLANNED", restartID: plan.restartID, planSHA256: fingerprint(plan) };
  }
  const plan = await readPrivateJSON(options["--plan"]);
  const result = restartStep(command, plan, options["--approved-plan-sha"], options["--evidence"], rt, seconds);
  createPrivatePrettyJSONSync(output, { schema: "kodex.application-restart-receipt/v1", restartID: plan.restartID,
    planSHA256: fingerprint(plan), ...result });
  return { status: result.status, restartID: plan.restartID, planSHA256: fingerprint(plan) };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const root = fileURLToPath(new URL("../../", import.meta.url));
    const git = (...args) => execFileSync("git", ["-C", root, ...args], { encoding: "utf8", stdio: "pipe", timeout: 10_000 }).trim();
    const executorSHA = git("rev-parse", "HEAD");
    requireValue(/^[a-f0-9]{40}$/.test(executorSHA) && git("status", "--porcelain", "--untracked-files=all") === "", "CLEAN_EXECUTOR_SOURCE_REQUIRED");
    const result = await restartCLI(process.argv.slice(2), (context, profile, budget) => applicationRestartRuntime(context, profile, executorSHA, budget));
    process.stdout.write(JSON.stringify(result) + "\n");
    if (!["PLANNED", "PASS"].includes(result.status)) process.exitCode = 2;
  } catch (error) { process.stderr.write(safeCode(error) + "\n"); process.exitCode = 1; }
}
