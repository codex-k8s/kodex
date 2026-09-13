#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { closeSync, fsyncSync, lstatSync, openSync, readFileSync, writeFileSync, writeSync } from "node:fs";
import { randomUUID } from "node:crypto";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";

const namespace = "kodex-system";
const requireValue = (condition, code) => { if (!condition) throw new Error(code); };

function literal(deployment, containerName, key) {
  const containers = deployment.spec?.template?.spec?.containers?.filter((item) => item.name === containerName) ?? [];
  const entries = containers[0]?.env?.filter((item) => item.name === key) ?? [];
  requireValue(containers.length === 1 && entries.length === 1 && typeof entries[0].value === "string" && !entries[0].valueFrom,
    "EXACT_LITERAL_CONFIG_REQUIRED");
  return entries[0].value;
}

export function planRuntimeRoleBinding(controller, runtimeController, policy, binding, operationID = randomUUID()) {
  const runner = literal(runtimeController, "runtime-controller", "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE");
  return planRuntimeRoleBindingForRunner(controller, runner, policy, binding, operationID);
}

export function planRuntimeRoleBindingForRunner(controller, runner, policy, binding, operationID = randomUUID()) {
  const policyName = literal(controller, "image-admission-controller", "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP");
  requireValue(/^pull\.kodex\.works\/kodex\/agent-runner@sha256:[a-f0-9]{64}$/.test(runner), "EXACT_RUNNER_REFERENCE_REQUIRED");
  requireValue(policy?.apiVersion === "v1" && policy.kind === "ConfigMap" && policy.metadata?.name === policyName &&
    policy.metadata.namespace === namespace && policy.immutable === true && policy.data?.nodeReadbackImage === runner,
  "ACTIVE_POLICY_AND_RUNNER_MISMATCH");
  requireValue(binding?.apiVersion === "admissionregistration.k8s.io/v1" && binding.kind === "ValidatingAdmissionPolicyBinding" &&
    binding.metadata?.name === "runtime-role-pod-exact-secret-projection" && /^[a-f0-9-]{36}$/.test(binding.metadata.uid ?? "") &&
    binding.spec?.policyName === binding.metadata.name && binding.spec.paramRef?.namespace === namespace &&
    binding.spec.paramRef.parameterNotFoundAction === "Deny" && fingerprint(binding.spec.validationActions) === fingerprint(["Deny"]),
  "CLOSED_RUNTIME_BINDING_REQUIRED");
  const after = structuredClone(binding.spec);
  after.paramRef.name = policyName;
  return { version: 1, kind: "RUNTIME_ROLE_POLICY_BINDING", id: operationID, policyName, runner,
    uid: binding.metadata.uid, resourceVersion: binding.metadata.resourceVersion,
    beforeSHA256: fingerprint(binding.spec), afterSHA256: fingerprint(after), changed: binding.spec.paramRef.name !== policyName,
    patch: [{ op: "test", path: "/metadata/uid", value: binding.metadata.uid },
      { op: "test", path: "/metadata/resourceVersion", value: binding.metadata.resourceVersion },
      { op: "replace", path: "/spec", value: after }] };
}

export function sameRuntimeRoleBindingPlan(saved, current, context) {
  requireValue(fingerprint({ ...current, id: saved.id, context }) === fingerprint(saved), "PLAN_PRECONDITION_CHANGED");
}

export function runtimeRoleRunner(command, options, runtimeController, saved) {
  if (command === "apply") {
    requireValue(saved && typeof saved.runner === "string" && !options["--runner-reference"], "APPLY_ARGUMENTS_INVALID");
    return saved.runner;
  }
  return options["--runner-reference"] ?? literal(runtimeController, "runtime-controller", "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE");
}

function privateJSON(path) {
  const stat = lstatSync(path);
  requireValue(stat.isFile() && stat.nlink === 1 && (stat.mode & 0o077) === 0 && stat.size < 8 << 20, "PRIVATE_INPUT_REQUIRED");
  return JSON.parse(readFileSync(path, "utf8"));
}

function main(args) {
  const command = args.shift(), options = {};
  requireValue(["plan", "apply"].includes(command), "INVALID_COMMAND");
  while (args.length) {
    const key = args.shift();
    if (key === "--k3s-sudo") { options[key] = true; continue; }
    requireValue(/^--[a-z-]+$/.test(key ?? "") && args.length && !Object.hasOwn(options, key), "INVALID_ARGUMENTS");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(typeof context === "string" && context && !/prod/i.test(context), "STAGING_CONTEXT_REQUIRED");
  const kubectl = (argv, input) => execFileSync(options["--k3s-sudo"] ? "sudo" : "kubectl",
    [...(options["--k3s-sudo"] ? ["-n", "k3s", "kubectl"] : []), "--context", context, ...argv],
    { input, encoding: "utf8", timeout: 310000, maxBuffer: 8 << 20, stdio: [input ? "pipe" : "ignore", "pipe", "pipe"] });
  const get = (kind, name) => JSON.parse(kubectl(["-n", namespace, "get", kind, name, "-o", "json"]));
  const controller = get("deployment", "image-admission-controller"), runtimeController = get("deployment", "runtime-controller");
  const policyName = literal(controller, "image-admission-controller", "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP");
  if (command === "plan") {
    const targetRunner = runtimeRoleRunner(command, options, runtimeController);
    const current = planRuntimeRoleBindingForRunner(controller, targetRunner, get("configmap", policyName),
      get("validatingadmissionpolicybinding", "runtime-role-pod-exact-secret-projection"));
    requireValue(options["--output"] && !options["--confirm"] && current.changed &&
      Object.keys(options).every((key) => ["--context", "--output", "--runner-reference", "--k3s-sudo"].includes(key)), "PLAN_ARGUMENTS_INVALID");
    writeFileSync(options["--output"], `${JSON.stringify({ ...current, context })}\n`, { flag: "wx", mode: 0o600 });
    process.stdout.write(`${JSON.stringify({ status: "PLANNED", id: current.id, policyName: current.policyName })}\n`);
    return;
  }
  requireValue(options["--plan"] && options["--evidence"] && options["--confirm"] === "APPLY-STAGING-RUNTIME-BINDING" &&
    !options["--runner-reference"] && Object.keys(options).every((key) =>
      ["--context", "--plan", "--evidence", "--confirm", "--k3s-sudo"].includes(key)), "APPLY_ARGUMENTS_INVALID");
  const saved = privateJSON(options["--plan"]);
  requireValue(saved.context === context, "PLAN_PRECONDITION_CHANGED");
  const current = planRuntimeRoleBindingForRunner(controller, runtimeRoleRunner(command, options, runtimeController, saved), get("configmap", policyName),
    get("validatingadmissionpolicybinding", "runtime-role-pod-exact-secret-projection"));
  sameRuntimeRoleBindingPlan(saved, current, context);
  const fd = openSync(options["--evidence"], "wx", 0o600);
  try {
    writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), id: saved.id, status: "INTENT", policyName: saved.policyName })}\n`); fsyncSync(fd);
    kubectl(["-n", namespace, "patch", "validatingadmissionpolicybinding", "runtime-role-pod-exact-secret-projection", "--type=json", "-p", JSON.stringify(saved.patch)]);
    const after = get("validatingadmissionpolicybinding", "runtime-role-pod-exact-secret-projection");
    requireValue(after.metadata.uid === saved.uid && fingerprint(after.spec) === saved.afterSHA256, "BINDING_READBACK_MISMATCH");
    writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), id: saved.id, status: "PASS", policyName: saved.policyName })}\n`); fsyncSync(fd);
    process.stdout.write(`${JSON.stringify({ status: "PASS", id: saved.id })}\n`);
  } catch { writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), id: saved.id, status: "UNKNOWN" })}\n`); fsyncSync(fd); throw new Error("RUNTIME_BINDING_TRANSITION_FAILED"); }
  finally { closeSync(fd); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); } catch (error) { process.stderr.write(`${/^[A-Z_]+$/.test(error.message) ? error.message : "RUNTIME_BINDING_TRANSITION_FAILED"}\n`); process.exitCode = 1; }
}
