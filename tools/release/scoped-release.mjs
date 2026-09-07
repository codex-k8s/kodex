#!/usr/bin/env node

import { execFile } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import { open, readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { planSourceChange, validSource } from "./application-source.mjs";

const execute = promisify(execFile);
const releaseAnnotation = "kodex.dev/application-release";
const applicationNames = new Set([
  "control-plane", "secret-broker", "email-bridge", "runtime-controller",
  "integration-gateway", "interaction-gateway", "automation-scheduler",
  "session-archive", "role-image-builder", "control-api-gateway",
  "egress-gateway", "stt-tts-service", "staff-control-center",
]);
const digestImage = /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/;
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;

function requireValue(condition, code) {
  if (!condition) throw new Error(code);
}

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === "object")
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
  return value;
}

export function fingerprint(value) {
  return createHash("sha256").update(JSON.stringify(canonical(value))).digest("hex");
}

function exactKeys(value, required, optional = []) {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    required.every((key) => Object.hasOwn(value, key)) &&
    Object.keys(value).every((key) => required.includes(key) || optional.includes(key));
}

export function validateManifest(value) {
  requireValue(exactKeys(value, ["version", "targets"]) && value.version === 1 &&
    Array.isArray(value.targets) && value.targets.length > 0 && value.targets.length <= 32,
  "INVALID_MANIFEST");
  const selected = new Set();
  for (const target of value.targets) {
    requireValue(exactKeys(target, ["name"], ["image", "source", "expectedSource", "expectedImage", "rollbackOf"]) &&
      applicationNames.has(target.name) && !selected.has(target.name) &&
      (target.image !== undefined || target.source !== undefined), "INVALID_TARGET");
    if (target.image !== undefined)
      requireValue(digestImage.test(target.image) && !target.image.endsWith("0".repeat(64)), "INVALID_TARGET_IMAGE");
    if (target.source !== undefined) requireValue(validSource(target.source), "INVALID_APPLICATION_SOURCE");
    if (target.rollbackOf !== undefined || target.expectedImage !== undefined)
      requireValue(uuid.test(target.rollbackOf ?? "") && digestImage.test(target.expectedImage ?? ""),
        "INVALID_ROLLBACK_TARGET");
    if (target.expectedSource !== undefined)
      requireValue(target.rollbackOf !== undefined && target.source !== undefined && validSource(target.expectedSource), "INVALID_ROLLBACK_SOURCE");
    selected.add(target.name);
  }
  return value;
}

// Только приложение. Полный pod template, Secrets и общие ConfigMaps не применяются.
export function planTarget(deployment, target, releaseID, sourceInspector) {
  validateManifest({ version: 1, targets: [target] });
  requireValue(uuid.test(releaseID), "INVALID_RELEASE_ID");
  const metadata = deployment?.metadata;
  const spec = deployment?.spec;
  const pod = spec?.template;
  const status = deployment?.status;
  requireValue(deployment?.apiVersion === "apps/v1" && deployment.kind === "Deployment" &&
    metadata?.name === target.name && metadata.namespace === "kodex-system" &&
    uuid.test(metadata.uid) && typeof metadata.resourceVersion === "string" &&
    metadata.labels?.["app.kubernetes.io/part-of"] === "kodex", "DEPLOYMENT_IDENTITY_MISMATCH");
  requireValue(metadata.labels?.["kodex.dev/local-profile"] === "hot-reload" ||
    metadata.labels?.["kodex.dev/environment"] === "staging", "STAGING_PROFILE_REQUIRED");
  requireValue(spec.paused !== true && Number.isSafeInteger(spec.replicas) && spec.replicas > 0 &&
    spec.strategy?.type === "RollingUpdate" && spec.strategy.rollingUpdate?.maxUnavailable === 0 &&
    Number.isSafeInteger(spec.strategy.rollingUpdate.maxSurge) && spec.strategy.rollingUpdate.maxSurge > 0,
  "SAFE_ROLLING_STRATEGY_REQUIRED");
  requireValue(status?.observedGeneration >= metadata.generation &&
    status.availableReplicas >= spec.replicas, "AVAILABLE_REPLICAS_REQUIRED");
  if (!target.rollbackOf)
    requireValue(status.updatedReplicas === spec.replicas && status.replicas === spec.replicas,
      "PREVIOUS_ROLLOUT_INCOMPLETE");
  const containers = pod?.spec?.containers;
  const application = pod?.metadata?.annotations?.["kubectl.kubernetes.io/default-container"] ?? target.name;
  requireValue(application === target.name && Array.isArray(containers), "APPLICATION_CONTAINER_AMBIGUOUS");
  const index = containers.findIndex((item) => item.name === application);
  requireValue(index >= 0 && containers.filter((item) => item.name === application).length === 1,
    "APPLICATION_CONTAINER_MISSING");
  const beforeImage = containers[index].image;
  const image = target.image ?? beforeImage;
  requireValue(digestImage.test(beforeImage) && !beforeImage.endsWith("0".repeat(64)), "EXACT_CURRENT_IMAGE_REQUIRED");
  const afterSpec = structuredClone(spec);
  const source = target.source ? planSourceChange(deployment, afterSpec, index, target.source, sourceInspector) : null;
  requireValue(beforeImage !== image || source?.changed, "APPLICATION_IMAGE_UNCHANGED");
  if (target.expectedSource)
    requireValue(source?.before.path === target.expectedSource.path && source.before.revision === target.expectedSource.revision, "ROLLBACK_SOURCE_MISMATCH");
  for (const container of containers.filter((item) => item.name.endsWith("platform-worker-grant-agent"))) {
    const instance = (container.env ?? []).filter((item) => item.name === "PLATFORM_WORKER_GRANT_INSTANCE_ID");
    requireValue(pod.metadata?.annotations?.["kodex.dev/worker-grant-format"] === "2" &&
      instance.length === 1 && instance[0].value === undefined &&
      instance[0].valueFrom?.fieldRef?.fieldPath === "metadata.uid", "INSTANCE_GRANTS_REQUIRED");
  }
  if (target.rollbackOf)
    requireValue(pod.metadata?.annotations?.[releaseAnnotation] === target.rollbackOf &&
      beforeImage === target.expectedImage, "ROLLBACK_RELEASE_MISMATCH");
  const annotations = { ...(pod.metadata?.annotations ?? {}), [releaseAnnotation]: releaseID };
  if (target.source) annotations["kodex.dev/application-source-sha"] = target.source.revision;
  const patch = [
    { op: "test", path: "/metadata/uid", value: metadata.uid },
    { op: "test", path: "/metadata/resourceVersion", value: metadata.resourceVersion },
    { op: "test", path: `/spec/template/spec/containers/${index}/name`, value: application },
    { op: "test", path: `/spec/template/spec/containers/${index}/image`, value: beforeImage },
    { op: "replace", path: `/spec/template/spec/containers/${index}/image`, value: image },
    ...(source?.patch ?? []),
    { op: "add", path: "/spec/template/metadata/annotations", value: annotations },
  ];
  afterSpec.template.spec.containers[index].image = image;
  afterSpec.template.metadata.annotations = annotations;
  return {
    name: target.name, uid: metadata.uid, beforeImage, image,
    beforeSpecSHA256: fingerprint(spec), afterSpecSHA256: fingerprint(afterSpec), patch,
    rollback: { name: target.name, image: beforeImage, expectedImage: image, rollbackOf: releaseID,
      ...(source ? { source: source.before, expectedSource: target.source } : {}) },
  };
}

export async function boundedBatch(items, parallelism, operation) {
  requireValue(Number.isSafeInteger(parallelism) && parallelism >= 1 && parallelism <= 8, "INVALID_PARALLELISM");
  let next = 0;
  const results = new Array(items.length);
  await Promise.all(Array.from({ length: Math.min(parallelism, items.length) }, async () => {
    while (next < items.length) {
      const index = next++;
      try { results[index] = await operation(items[index]); }
      catch { results[index] = { name: items[index].name, status: "FAIL", code: "RELEASE_OPERATION_FAILED" }; }
    }
  }));
  return results;
}

async function createPrivate(path, value) {
  const handle = await open(path, "wx", 0o600);
  try { await handle.writeFile(`${JSON.stringify(value)}\n`); await handle.sync(); }
  finally { await handle.close(); }
}

async function main(args) {
  const command = args.shift();
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--manifest", "--plan", "--output", "--evidence", "--parallelism", "--timeout-seconds", "--confirm"].includes(key) &&
      !Object.hasOwn(options, key) && args.length > 0, "INVALID_ARGUMENTS");
    options[key] = args.shift();
  }
  requireValue(["plan", "apply"].includes(command), "INVALID_COMMAND");
  const context = options["--context"];
  requireValue(typeof context === "string" && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(context) &&
    !/prod(?:uction)?/i.test(context), "EXACT_STAGING_CONTEXT_REQUIRED");
  const kubectl = async (arguments_, timeout = 35000) => {
    try {
      const { stdout } = await execute("kubectl", ["--context", context, "--request-timeout=30s", ...arguments_],
        { timeout, maxBuffer: 4 << 20, encoding: "utf8" });
      return stdout;
    } catch { throw new Error("KUBERNETES_OPERATION_FAILED"); }
  };
  const get = async (kind, name, namespace = "kodex-system") => JSON.parse(await kubectl(["-n", namespace, "get", kind, name, "-o", "json"]));
  const identity = (await get("namespace", "kube-system")).metadata.uid;
  requireValue(uuid.test(identity), "INVALID_CLUSTER_IDENTITY");
  if (command === "plan") {
    requireValue(options["--manifest"] && options["--output"] && !options["--plan"] && !options["--confirm"], "INVALID_PLAN_ARGUMENTS");
    const manifest = validateManifest(JSON.parse(await readFile(options["--manifest"], "utf8")));
    const releaseID = randomUUID();
    const targets = [];
    for (const target of manifest.targets) {
      const deployment = await get("deployment", target.name);
      const planned = planTarget(deployment, target, releaseID);
      // Не сохраняем env, annotations или security references в плане и журнале.
      const { patch: _patch, ...safe } = planned;
      targets.push({ ...safe, requested: target });
    }
    await createPrivate(options["--output"], { version: 1, releaseID, context, clusterUID: identity, targets });
    process.stdout.write(`Release plan ready: targets=${targets.length} release=${releaseID}\n`);
    return;
  }
  requireValue(options["--plan"] && options["--evidence"] && options["--confirm"] === "APPLY-STAGING-APPLICATIONS", "STAGING_CONFIRMATION_REQUIRED");
  const parallelism = Number(options["--parallelism"] ?? 2);
  const seconds = Number(options["--timeout-seconds"] ?? 300);
  requireValue(Number.isSafeInteger(seconds) && seconds >= 30 && seconds <= 1800 &&
    Number.isSafeInteger(parallelism) && parallelism >= 1 && parallelism <= 8, "INVALID_RELEASE_BUDGET");
  const plan = JSON.parse(await readFile(options["--plan"], "utf8"));
  requireValue(plan.version === 1 && plan.context === context && plan.clusterUID === identity && uuid.test(plan.releaseID) &&
    Array.isArray(plan.targets), "RELEASE_PLAN_IDENTITY_MISMATCH");
  validateManifest({ version: 1, targets: plan.targets.map((item) => item.requested) });
  // O_EXCL + fsync до первого PATCH: повтор после неизвестного исхода требует readback.
  await createPrivate(options["--evidence"], { version: 1, releaseID: plan.releaseID, status: "IN_PROGRESS" });
  const journal = await open(options["--evidence"], "a");
  const record = async (value) => { await journal.writeFile(`${JSON.stringify(value)}\n`); await journal.sync(); };
  try {
    const results = await boundedBatch(plan.targets, parallelism, async (item) => {
      let stage = "PREFLIGHT";
      try {
        const current = await get("deployment", item.name);
        requireValue(current.metadata.uid === item.uid && fingerprint(current.spec) === item.beforeSpecSHA256,
          "DEPLOYMENT_CHANGED_AFTER_PLAN");
        const prepared = planTarget(current, item.requested, plan.releaseID);
        requireValue(prepared.afterSpecSHA256 === item.afterSpecSHA256, "PLAN_CONTENT_MISMATCH");
        await record({ name: item.name, status: "PATCH_ATTEMPT", beforeImage: prepared.beforeImage, image: prepared.image,
          rollback: prepared.rollback });
        stage = "PATCH_ATTEMPT";
        await kubectl(["-n", "kodex-system", "patch", "deployment", item.name, "--type=json", "-p", JSON.stringify(prepared.patch), "-o", "name"]);
        stage = "ROLLOUT";
        await kubectl(["-n", "kodex-system", "rollout", "status", `deployment/${item.name}`, `--timeout=${seconds}s`], (seconds + 35) * 1000);
        const observed = await get("deployment", item.name);
        requireValue(observed.metadata.uid === item.uid && fingerprint(observed.spec) === prepared.afterSpecSHA256 &&
          observed.status.observedGeneration >= observed.metadata.generation &&
          observed.status.availableReplicas >= observed.spec.replicas &&
          observed.status.updatedReplicas === observed.spec.replicas && observed.status.replicas === observed.spec.replicas,
        "ROLLOUT_READBACK_MISMATCH");
        const result = { name: item.name, status: "PASS", image: prepared.image };
        await record(result);
        return result;
      } catch (error) {
        const result = { name: item.name, status: "FAIL", stage,
          code: /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "RELEASE_OPERATION_FAILED" };
        await record(result);
        return result;
      }
    });
    const success = results.every((result) => result.status === "PASS");
    await record({ status: success ? "PASS" : "FAIL", results });
    process.stdout.write(`${JSON.stringify({ releaseID: plan.releaseID, results })}\n`);
    if (!success) process.exitCode = 1;
  } finally { await journal.close(); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { await main(process.argv.slice(2)); }
  catch (error) {
    const code = /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "RELEASE_FAILED";
    process.stderr.write(`Scoped release failed: ${code}\n`);
    process.exitCode = 1;
  }
}
