#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { closeSync, fsyncSync, lstatSync, mkdtempSync, openSync, readFileSync, rmSync, writeFileSync, writeSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { namespace, policyBase, policyDigest, policyToolsDigest, requireStaging, toolsDigestAnnotation } from "./runner-policy-model.mjs";

const requireValue = (value, code) => { if (!value) throw new Error(code); };
function privateRead(path) {
  const stat = lstatSync(path);
  requireValue(stat.isFile() && (stat.mode & 0o077) === 0 && stat.size < 2 << 20, "PRIVATE_INPUT_REQUIRED");
  return JSON.parse(readFileSync(path, "utf8"));
}

// Исправляется только metadata выбранной immutable policy. Существующий build,
// payload, revision, authority и старый journal не переписываются.
export function planToolsMetadata(policy, parameters, binding, controller) {
  requireStaging(policy); requireStaging(parameters); requireStaging(controller);
  requireValue(policy.kind === "ConfigMap" && policy.immutable === true &&
    policy.metadata.labels["kodex.dev/owner-intent"] === "true" &&
    policyDigest(policy.data) === policy.data.policySHA256 &&
    policy.metadata.name === `${policyBase}-${policy.data.policySHA256.slice(0, 32)}` &&
    parameters.kind === "ImageAdmissionPolicyParameters" && parameters.metadata.name === policy.metadata.name &&
    fingerprint(parameters.spec) === fingerprint(policy.data), "EXACT_IMMUTABLE_POLICY_REQUIRED");
  requireValue(binding.metadata.name === "kodex-image-admission-controller-jobs" &&
    binding.spec.paramRef?.name === policy.metadata.name && binding.spec.paramRef.namespace === namespace &&
    binding.spec.paramRef.parameterNotFoundAction === "Deny" && fingerprint(binding.spec.validationActions) === fingerprint(["Deny"]), "EXACT_CLOSED_BINDING_REQUIRED");
  const app = controller.spec.template.spec.containers.find(item => item.name === "image-admission-controller");
  const selected = app?.env?.filter(item => item.name === "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP");
  requireValue(controller.metadata.name === "image-admission-controller" && selected?.length === 1 && selected[0].value === policy.metadata.name, "SERVING_POLICY_MISMATCH");
  const digest = policyToolsDigest(policy), annotations = policy.metadata.annotations ?? {};
  requireValue(annotations[toolsDigestAnnotation] === undefined || annotations[toolsDigestAnnotation] === digest, "EXISTING_ANNOTATION_CONFLICT");
  return { version: 1, name: policy.metadata.name, uid: policy.metadata.uid, resourceVersion: policy.metadata.resourceVersion,
    dataSHA256: fingerprint(policy.data), toolsDigest: digest,
    guards: { parametersUID: parameters.metadata.uid, parametersSHA256: fingerprint(parameters.spec), bindingUID: binding.metadata.uid,
      bindingSHA256: fingerprint(binding.spec), controllerUID: controller.metadata.uid, controllerSHA256: fingerprint(controller.spec) },
    annotationsBeforeSHA256: fingerprint(annotations), annotationsAfter: { ...annotations, [toolsDigestAnnotation]: digest },
    alreadyPresent: annotations[toolsDigestAnnotation] === digest };
}

function main(args) {
  const command = args.shift(), options = {};
  requireValue(["plan", "apply"].includes(command), "INVALID_COMMAND");
  while (args.length) { const key = args.shift(); requireValue(["--context", "--policy", "--output", "--plan", "--evidence", "--confirm"].includes(key) && !options[key] && args.length, "INVALID_ARGUMENT"); options[key] = args.shift(); }
  const context = options["--context"];
  requireValue(context && !/prod/i.test(context), "STAGING_CONTEXT_REQUIRED");
  const kubectl = (args, input) => execFileSync("kubectl", ["--context", context, "--namespace", namespace, ...args], { input, encoding: "utf8", timeout: 30000, maxBuffer: 2 << 20, stdio: [input ? "pipe" : "ignore", "pipe", "pipe"] });
  const get = (kind, name) => JSON.parse(kubectl(["get", kind, name, "-o", "json"]));
  const saved = command === "apply" ? privateRead(options["--plan"]) : null;
  const name = saved?.name ?? options["--policy"];
  requireValue(new RegExp(`^${policyBase}-[a-f0-9]{32}$`).test(name ?? ""), "VERSIONED_POLICY_REQUIRED");
  const clusterUID = get("namespace", "kube-system").metadata.uid;
  const ns = get("namespace", namespace);
  requireValue(ns.metadata.labels?.["kodex.dev/environment"] === "staging", "STAGING_NAMESPACE_REQUIRED");
  const policy = get("configmap", name);
  const plan = { ...planToolsMetadata(policy, get("imageadmissionpolicyparameters", name), get("validatingadmissionpolicybinding", "kodex-image-admission-controller-jobs"), get("deployment", "image-admission-controller")), context, clusterUID, namespaceUID: ns.metadata.uid };
  // Фактический shell renderer проверяется до планирования/записи metadata.
  const repaired = { ...policy, metadata: { ...policy.metadata, annotations: plan.annotationsAfter } };
  for (const phase of ["claim", "scan", "sign", "admit", "promote"]) execFileSync("bash", [fileURLToPath(new URL("../render-image-admission-job.sh", import.meta.url)), "staging", `v20260908000000-${policy.data.orchestrationRevision}`, phase],
    { env: { PATH: process.env.PATH, IMAGE_ADMISSION_POLICY_JSON: JSON.stringify(repaired) }, timeout: 10000, maxBuffer: 2 << 20, stdio: ["ignore", "pipe", "pipe"] });
  if (command === "plan") {
    requireValue(options["--output"] && !options["--confirm"], "READ_ONLY_PLAN_REQUIRED");
    writeFileSync(options["--output"], `${JSON.stringify(plan)}\n`, { flag: "wx", mode: 0o600 });
    process.stdout.write(`${JSON.stringify({ status: "PLANNED", name, dataSHA256: plan.dataSHA256, alreadyPresent: plan.alreadyPresent })}\n`); return;
  }
  requireValue(options["--confirm"] === "REPAIR-STAGING-POLICY-TOOLS-METADATA" && options["--evidence"] && fingerprint(saved) === fingerprint(plan), "EXACT_PLAN_CONFIRMATION_REQUIRED");
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const record = (event) => { writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), name, uid: plan.uid, ...event })}\n`); fsyncSync(fd); };
  let attempted = false;
  try {
    record({ status: "STARTED", dataSHA256: plan.dataSHA256 });
    if (!plan.alreadyPresent) {
      record({ status: "INTENT", toolsDigest: plan.toolsDigest }); attempted = true;
      const patch = [{ op: "test", path: "/metadata/uid", value: plan.uid }, { op: "test", path: "/metadata/resourceVersion", value: plan.resourceVersion },
        { op: "add", path: "/metadata/annotations", value: plan.annotationsAfter }];
      // Node передаёт child stdin через socket: повторное открытие /dev/stdin
      // из kubectl завершается ENXIO. Файл существует только в private temp dir.
      const directory = mkdtempSync(join(tmpdir(), "kodex-policy-patch-"));
      try {
        const path = join(directory, "patch.json");
        writeFileSync(path, JSON.stringify(patch), { flag: "wx", mode: 0o600 });
        kubectl(["patch", "configmap", name, "--type=json", `--patch-file=${path}`]);
      } finally { rmSync(directory, { recursive: true, force: true }); }
    }
    const actual = get("configmap", name);
    requireValue(actual.metadata.uid === plan.uid && actual.immutable === true && fingerprint(actual.data) === plan.dataSHA256 &&
      fingerprint(actual.metadata.annotations) === fingerprint(plan.annotationsAfter), "READBACK_MISMATCH");
    record({ status: "PASS", dataUnchanged: true });
    process.stdout.write(`${JSON.stringify({ status: "PASS", name, dataUnchanged: true })}\n`);
  } catch { record({ status: attempted ? "UNKNOWN" : "FAIL", code: "POLICY_METADATA_REPAIR_FAILED" }); throw new Error("POLICY_METADATA_REPAIR_FAILED"); }
  finally { closeSync(fd); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); } catch { process.stderr.write("Policy metadata operation failed; inspect authoritative state before another attempt\n"); process.exitCode = 1; }
}
