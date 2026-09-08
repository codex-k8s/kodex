#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";

export const legacyEnv = "KODEX_WS_LEGACY_UNTIL";
const retiredAnnotation = "kodex.dev/ws-v1-retired";
function requireValue(value, code) { if (!value) throw new Error(code); }

// Изменяется только cutoff приложения; identity, Secrets, images и sidecars сохраняются.
export function planWebSocketTransition(deployment, until, now = Date.now()) {
  const metadata = deployment?.metadata;
  const spec = deployment?.spec;
  requireValue(deployment?.apiVersion === "apps/v1" && deployment.kind === "Deployment" &&
    metadata?.name === "control-api-gateway" && metadata.namespace === "kodex-system" &&
    /^[a-f0-9-]{36}$/.test(metadata.uid ?? "") && /^[0-9]+$/.test(metadata.resourceVersion ?? "") &&
    metadata.labels?.["app.kubernetes.io/part-of"] === "kodex", "DEPLOYMENT_IDENTITY_MISMATCH");
  requireValue(metadata.labels?.["kodex.dev/environment"] === "staging" || metadata.labels?.["kodex.dev/local-profile"] === "hot-reload", "STAGING_PROFILE_REQUIRED");
  requireValue(spec?.paused !== true && Number.isSafeInteger(spec?.replicas) && spec.replicas > 0 &&
    spec.strategy?.type === "RollingUpdate" && spec.strategy.rollingUpdate?.maxUnavailable === 0 &&
    Number.isSafeInteger(spec.strategy.rollingUpdate.maxSurge) && spec.strategy.rollingUpdate.maxSurge > 0 &&
    deployment.status?.observedGeneration >= metadata.generation && deployment.status.updatedReplicas === spec.replicas &&
    deployment.status.availableReplicas >= spec.replicas && deployment.status.replicas === spec.replicas, "SAFE_COMPLETED_ROLLOUT_REQUIRED");
  const containers = spec.template?.spec?.containers;
  requireValue(Array.isArray(containers) && containers.filter((item) => item.name === metadata.name).length === 1, "APPLICATION_CONTAINER_AMBIGUOUS");
  const index = containers.findIndex((item) => item.name === metadata.name);
  const env = containers[index].env ?? [];
  requireValue(Array.isArray(env) && env.filter((item) => item.name === legacyEnv).length <= 1, "CUTOFF_ENV_AMBIGUOUS");
  const previous = env.find((item) => item.name === legacyEnv);
  requireValue(!previous || typeof previous.value === "string" && !previous.valueFrom && Number.isFinite(Date.parse(previous.value)), "CUTOFF_SOURCE_INVALID");
  if (until !== null) {
    requireValue(typeof until === "string" && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/.test(until) && Number.isFinite(Date.parse(until)) &&
      Date.parse(until) > now && Date.parse(until) <= now + 86_400_000, "CUTOFF_MUST_BE_WITHIN_24_HOURS");
    requireValue(metadata.annotations?.[retiredAnnotation] !== "true", "LEGACY_PROTOCOL_RETIRED");
    requireValue(!previous || Date.parse(until) <= Date.parse(previous.value), "CUTOFF_EXTENSION_FORBIDDEN");
  }
  const nextEnv = env.filter((item) => item.name !== legacyEnv);
  if (until !== null) nextEnv.push({ name: legacyEnv, value: until });
  const annotations = { ...(metadata.annotations ?? {}) };
  if (until === null) annotations[retiredAnnotation] = "true";
  const patch = [
    { op: "test", path: "/metadata/uid", value: metadata.uid },
    { op: "test", path: "/metadata/resourceVersion", value: metadata.resourceVersion },
    { op: "test", path: `/spec/template/spec/containers/${index}/name`, value: metadata.name },
    { op: "add", path: `/spec/template/spec/containers/${index}/env`, value: nextEnv },
    { op: "add", path: "/metadata/annotations", value: annotations },
  ];
  const after = structuredClone(spec);
  after.template.spec.containers[index].env = nextEnv;
  return { patch, uid: metadata.uid, afterSpecSHA256: fingerprint(after), until, retirement: until === null };
}

function main(args) {
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--until", "--confirm"].includes(key) && !Object.hasOwn(options, key) && args.length, "INVALID_ARGUMENTS");
    options[key] = args.shift();
  }
  requireValue(typeof options["--context"] === "string" && options["--context"].length > 0 && options["--until"], "CONTEXT_AND_CUTOFF_REQUIRED");
  requireValue(!options["--confirm"] || options["--confirm"] === "WS_TICKET_TRANSITION", "INVALID_CONFIRMATION");
  const prefix = ["--context", options["--context"], "--namespace", "kodex-system"];
  const kubectl = (command) => execFileSync("kubectl", [...prefix, ...command], { encoding: "utf8", timeout: 190_000, maxBuffer: 4 * 1024 * 1024, stdio: ["ignore", "pipe", "pipe"] });
  const read = () => JSON.parse(kubectl(["get", "deployment", "control-api-gateway", "-o", "json"]));
  const plan = planWebSocketTransition(read(), options["--until"] === "retire" ? null : options["--until"]);
  if (!options["--confirm"]) {
    process.stdout.write(`${JSON.stringify({ status: "PLANNED", target: "control-api-gateway", until: plan.until, retirement: plan.retirement })}\n`);
    return;
  }
  const directory = mkdtempSync(join(tmpdir(), "kodex-ws-transition-"));
  try {
    const patchPath = join(directory, `${randomUUID()}.json`);
    writeFileSync(patchPath, JSON.stringify(plan.patch), { mode: 0o600, flag: "wx" });
    kubectl(["patch", "deployment", "control-api-gateway", "--type=json", "--patch-file", patchPath]);
    const current = read();
    requireValue(current.metadata?.uid === plan.uid && fingerprint(current.spec) === plan.afterSpecSHA256 &&
      (!plan.retirement || current.metadata.annotations?.[retiredAnnotation] === "true"), "TRANSITION_READBACK_MISMATCH");
    kubectl(["rollout", "status", "deployment/control-api-gateway", "--timeout=180s"]);
    process.stdout.write(`${JSON.stringify({ status: "PASS", target: "control-api-gateway", until: plan.until, retirement: plan.retirement })}\n`);
  } finally { rmSync(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); }
  catch { process.stderr.write("WebSocket transition failed; inspect the scoped deployment safely\n"); process.exitCode = 1; }
}
