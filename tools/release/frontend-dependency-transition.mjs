#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { randomUUID, createHash } from "node:crypto";
import {
  closeSync,
  constants,
  fsyncSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
  writeSync,
} from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { inspectSource, validSource } from "./application-source.mjs";
import { fingerprint } from "./scoped-release.mjs";
import {
  frontendDependencyState,
  planFrontendDependencies,
} from "./frontend-dependency-model.mjs";

const component = "staff-control-center",
  namespace = "kodex-system";
const requireValue = (value, code) => {
  if (!value) throw new Error(code);
};
const digest = (value) => createHash("sha256").update(value).digest("hex");
const run = (command, args, timeout = 30000) =>
  execFileSync(command, args, {
    encoding: "utf8",
    timeout,
    maxBuffer: 8 << 20,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();

function privateParent(path) {
  const parent = dirname(resolve(path)),
    stat = lstatSync(parent);
  requireValue(
    realpathSync(parent) === parent &&
      stat.isDirectory() &&
      !stat.isSymbolicLink() &&
      (stat.mode & 0o077) === 0 &&
      stat.uid === process.getuid(),
    "PRIVATE_DIRECTORY_REQUIRED",
  );
}
function privateJSON(path) {
  privateParent(path);
  const stat = lstatSync(path);
  requireValue(
    stat.isFile() &&
      !stat.isSymbolicLink() &&
      stat.uid === process.getuid() &&
      (stat.mode & 0o077) === 0 &&
      stat.size > 0 &&
      stat.size < 1 << 20,
    "PRIVATE_INPUT_INVALID",
  );
  return JSON.parse(readFileSync(path, "utf8"));
}
function publish(path, value) {
  privateParent(path);
  const fd = openSync(
    path,
    constants.O_WRONLY |
      constants.O_CREAT |
      constants.O_EXCL |
      constants.O_NOFOLLOW,
    0o600,
  );
  try {
    writeSync(fd, JSON.stringify(value) + "\n");
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
  const directory = openSync(
    dirname(resolve(path)),
    constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW,
  );
  try {
    fsyncSync(directory);
  } finally {
    closeSync(directory);
  }
}
function record(path, value) {
  privateParent(path);
  const stat = lstatSync(path);
  requireValue(
    stat.isFile() &&
      !stat.isSymbolicLink() &&
      stat.uid === process.getuid() &&
      (stat.mode & 0o077) === 0,
    "PRIVATE_EVIDENCE_INVALID",
  );
  const fd = openSync(
    path,
    constants.O_WRONLY | constants.O_APPEND | constants.O_NOFOLLOW,
  );
  try {
    writeSync(
      fd,
      JSON.stringify({ at: new Date().toISOString(), ...value }) + "\n",
    );
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
}

export function verifyFrontendCache(source, cachePath, image, execute = run) {
  requireValue(validSource(source), "FRONTEND_SOURCE_INVALID");
  const inspected = inspectSource(source.path, true);
  requireValue(
    inspected.revision === source.revision && inspected.mountpointsReady,
    "FRONTEND_SOURCE_NOT_PREPARED",
  );
  const directory = lstatSync(cachePath);
  requireValue(
    realpathSync(cachePath) === cachePath &&
      directory.isDirectory() &&
      !directory.isSymbolicLink() &&
      (directory.mode & 0o222) === 0,
    "FRONTEND_CACHE_NOT_READONLY",
  );
  const identityPath = `${cachePath}/.kodex-cache-identity`,
    identityStat = lstatSync(identityPath);
  requireValue(
    identityStat.isFile() &&
      !identityStat.isSymbolicLink() &&
      (identityStat.mode & 0o222) === 0,
    "FRONTEND_CACHE_RECEIPT_INVALID",
  );
  const identity = readFileSync(identityPath, "utf8").trim();
  requireValue(
    /^[a-f0-9]{64}$/.test(identity) &&
      cachePath.endsWith(`/frontend-v1/${identity}/node_modules`),
    "FRONTEND_CACHE_IDENTITY_INVALID",
  );
  const frontend = `${source.path}/services/staff/control-center`;
  for (const name of ["package.json", "package-lock.json"]) {
    const cached = `${dirname(cachePath)}/${name}`,
      stat = lstatSync(cached);
    requireValue(
      stat.isFile() &&
        !stat.isSymbolicLink() &&
        (stat.mode & 0o222) === 0 &&
        readFileSync(cached).equals(readFileSync(`${frontend}/${name}`)),
      "FRONTEND_CACHE_MANIFEST_MISMATCH",
    );
  }
  const dockerfile = readFileSync(`${frontend}/Dockerfile`, "utf8");
  requireValue(
    dockerfile.match(
      /^FROM (docker\.io\/library\/node:[^ ]+) AS build$/m,
    )?.[1] === image,
    "FRONTEND_IMAGE_CHANGE_REQUIRES_SEPARATE_DELIVERY",
  );
  let security;
  try {
    security = JSON.parse(
      execute("docker", ["info", "--format", "{{json .SecurityOptions}}"]),
    );
  } catch {
    throw new Error("FRONTEND_DOCKER_SECURITY_INVALID");
  }
  requireValue(
    Array.isArray(security) &&
      security.every((value) => typeof value === "string"),
    "FRONTEND_DOCKER_SECURITY_INVALID",
  );
  // Prime публикует parent0500 владельца cache. Не расширяем его права и
  // capabilities: rootless container0 отображается в того же host operator.
  const containerUser = security.some(
    (value) => value === "name=rootless" || value === "rootless",
  )
    ? "0:0"
    : `${process.getuid()}:${process.getgid()}`;
  const actual = execute("docker", [
    "run",
    "--pull=never",
    "--rm",
    "--read-only",
    "--user",
    containerUser,
    "--network",
    "none",
    "--cap-drop",
    "ALL",
    "--security-opt",
    "no-new-privileges",
    "--tmpfs",
    "/tmp:rw,nosuid,nodev,mode=1777",
    "-e",
    "HOME=/tmp",
    "-e",
    `KODEX_DEV_NODE_IMAGE=${image}`,
    "-v",
    `${frontend}:/input:ro`,
    "-v",
    `${source.path}/tools/dev/frontend-cache-identity.sh:/identity.sh:ro`,
    "-w",
    "/input",
    image,
    "sh",
    "/identity.sh",
  ]);
  requireValue(actual === identity, "FRONTEND_CACHE_RUNTIME_MISMATCH");
  execute("docker", [
    "run",
    "--pull=never",
    "--rm",
    "--read-only",
    "--user",
    containerUser,
    "--network",
    "none",
    "--cap-drop",
    "ALL",
    "--security-opt",
    "no-new-privileges",
    "--tmpfs",
    "/tmp:rw,nosuid,nodev,mode=1777",
    "-v",
    `${dirname(cachePath)}:/install:ro`,
    "-w",
    "/install",
    image,
    "node",
    "--input-type=module",
    "-e",
    'import {transformSync} from "esbuild";transformSync("const n=1");await import("vite")',
  ]);
  return {
    source: source.path,
    revision: source.revision,
    image,
    path: cachePath,
    identity,
    manifestsSHA256: inspected.dependenciesSHA256,
  };
}

function verifyWrappers(state, source) {
  for (const [path, name] of [
    [state.runner.path, "run-frontend.sh"],
    [state.identity.path, "frontend-cache-identity.sh"],
  ])
    requireValue(
      digest(readFileSync(path)) ===
        digest(readFileSync(`${source.path}/tools/dev/${name}`)),
      "FRONTEND_WRAPPER_CHANGE_REQUIRES_SEPARATE_DELIVERY",
    );
}

export function main(args, io = {}) {
  const execute = io.run ?? run,
    inspect = io.inspectSource ?? inspectSource,
    verifyCache = io.verifyCache ?? verifyFrontendCache;
  const command = args.shift(),
    options = {};
  requireValue(
    ["plan", "rollback-plan", "apply", "observe"].includes(command),
    "INVALID_COMMAND",
  );
  while (args.length) {
    const key = args.shift();
    requireValue(
      [
        "--context",
        "--source",
        "--revision",
        "--cache",
        "--output",
        "--plan",
        "--evidence",
        "--confirm",
        "--k3s-sudo",
      ].includes(key) && !Object.hasOwn(options, key),
      "INVALID_ARGUMENT",
    );
    options[key] = key === "--k3s-sudo" ? true : args.shift();
    requireValue(options[key], "ARGUMENT_VALUE_REQUIRED");
  }
  const context = options["--context"];
  requireValue(
    typeof context === "string" &&
      /^[A-Za-z0-9_.:@/-]{1,160}$/.test(context) &&
      !/prod/i.test(context),
    "EXACT_STAGING_CONTEXT_REQUIRED",
  );
  const kube = (args, timeout) =>
    options["--k3s-sudo"]
      ? execute(
          "sudo",
          [
            "-n",
            "k3s",
            "kubectl",
            "--context",
            context,
            "--request-timeout=30s",
            ...args,
          ],
          timeout,
        )
      : execute(
          "kubectl",
          ["--context", context, "--request-timeout=30s", ...args],
          timeout,
        );
  const get = () =>
    JSON.parse(
      kube(["get", "deployment", component, "-n", namespace, "-o", "json"]),
    );
  const namespaceUID = JSON.parse(
    kube(["get", "namespace", namespace, "-o", "json"]),
  ).metadata.uid;
  const neighbors = () =>
    fingerprint(
      JSON.parse(kube(["get", "deployments", "-n", namespace, "-o", "json"]))
        .items.filter((item) => item.metadata.name !== component)
        .map((item) => ({
          name: item.metadata.name,
          uid: item.metadata.uid,
          spec: fingerprint(item.spec),
        }))
        .sort((a, b) => a.name.localeCompare(b.name)),
    );
  let deployment = get();
  if (command === "plan" || command === "rollback-plan") {
    const state = frontendDependencyState(deployment);
    let source = { path: options["--source"], revision: options["--revision"] },
      cachePath = options["--cache"];
    if (command === "rollback-plan") {
      const old = privateJSON(options["--plan"]);
      requireValue(
        old.version === 1 &&
          old.kind === "FRONTEND_DEPENDENCIES" &&
          old.context === context &&
          old.namespaceUID === namespaceUID &&
          old.target.uid === deployment.metadata.uid &&
          old.target.afterSpecSHA256 === fingerprint(deployment.spec),
        "FRONTEND_ROLLBACK_STATE_MISMATCH",
      );
      source = old.target.previousSource;
      cachePath = old.target.previousCache;
    }
    const beforeNeighbors = neighbors(),
      previousSource = {
        path: state.source.root,
        revision: inspect(state.source.root, true).revision,
      };
    verifyWrappers(state, source);
    const cache = verifyCache(source, cachePath, state.image, execute);
    const target = planFrontendDependencies(deployment, {
      source,
      previousSource,
      cache,
      releaseID: randomUUID(),
    });
    // Серверная dry-run сохраняет admission checks, не применяя Deployment.
    const dry = JSON.parse(
      kube([
        "patch",
        "deployment",
        component,
        "-n",
        namespace,
        "--type=json",
        "-p",
        JSON.stringify(target.patch),
        "--dry-run=server",
        "-o",
        "json",
      ]),
    );
    requireValue(
      dry.metadata.uid === target.uid &&
        fingerprint(dry.spec) === target.afterSpecSHA256 &&
        neighbors() === beforeNeighbors,
      "FRONTEND_PLAN_READBACK_MISMATCH",
    );
    const plan = {
      version: 1,
      kind: "FRONTEND_DEPENDENCIES",
      context,
      namespaceUID,
      neighborsSHA256: beforeNeighbors,
      target,
    };
    publish(options["--output"], plan);
    return { status: "PLANNED", planSHA256: fingerprint(plan) };
  }
  const plan = privateJSON(options["--plan"]),
    target = plan.target;
  requireValue(
    plan.version === 1 &&
      plan.kind === "FRONTEND_DEPENDENCIES" &&
      plan.context === context &&
      plan.namespaceUID === namespaceUID &&
      target?.name === component &&
      target.uid === deployment.metadata.uid &&
      neighbors() === plan.neighborsSHA256,
    "FRONTEND_PLAN_BOUNDARY_CHANGED",
  );
  const cache = verifyCache(
    target.source,
    target.cache.path,
    target.image,
    execute,
  );
  requireValue(
    fingerprint(cache) === fingerprint(target.cache),
    "FRONTEND_PREPARED_CACHE_CHANGED",
  );
  const intentPath = `${resolve(options["--plan"])}.intent`;
  if (command === "apply") {
    requireValue(
      options["--confirm"] === "APPLY-STAGING-FRONTEND-DEPENDENCIES" &&
        options["--evidence"],
      "STAGING_CONFIRMATION_REQUIRED",
    );
    requireValue(
      fingerprint(deployment.spec) === target.beforeSpecSHA256 &&
        deployment.metadata.resourceVersion === target.resourceVersion,
      "FRONTEND_PRECONDITION_CHANGED",
    );
    const state = frontendDependencyState(deployment);
    verifyWrappers(state, target.source);
    requireValue(
      inspect(target.previousSource.path, true).revision ===
        target.previousSource.revision &&
        fingerprint(planFrontendDependencies(deployment, target)) ===
          fingerprint(target),
      "FRONTEND_PLAN_CHANGED",
    );
    const intent = {
      status: "INTENT",
      at: new Date().toISOString(),
      planSHA256: fingerprint(plan),
      evidence: resolve(options["--evidence"]),
    };
    publish(intentPath, intent);
    publish(intent.evidence, intent);
    try {
      kube([
        "patch",
        "deployment",
        component,
        "-n",
        namespace,
        "--type=json",
        "-p",
        JSON.stringify(target.patch),
        "-o",
        "name",
      ]);
    } catch {
      record(intent.evidence, { status: "UNKNOWN", operation: "PATCH" });
      return { status: "UNKNOWN" };
    }
  }
  const intent = privateJSON(intentPath);
  requireValue(
    intent.planSHA256 === fingerprint(plan) && intent.status === "INTENT",
    "FRONTEND_INTENT_MISMATCH",
  );
  deployment = get();
  if (fingerprint(deployment.spec) === target.beforeSpecSHA256)
    return { status: "UNKNOWN" };
  requireValue(
    deployment.metadata.uid === target.uid &&
      fingerprint(deployment.spec) === target.afterSpecSHA256,
    "FRONTEND_RESULT_DRIFT",
  );
  try {
    kube(
      [
        "rollout",
        "status",
        `deployment/${component}`,
        "-n",
        namespace,
        "--timeout=300s",
      ],
      335000,
    );
  } catch {
    record(intent.evidence, { status: "UNKNOWN", operation: "ROLLOUT" });
    return { status: "UNKNOWN" };
  }
  deployment = get();
  requireValue(
    deployment.metadata.uid === target.uid &&
      fingerprint(deployment.spec) === target.afterSpecSHA256 &&
      neighbors() === plan.neighborsSHA256,
    "FRONTEND_RESULT_DRIFT",
  );
  frontendDependencyState(deployment);
  record(intent.evidence, {
    status: "PASS",
    uid: target.uid,
    revision: target.source.revision,
    cacheIdentity: target.cache.identity,
    specSHA256: target.afterSpecSHA256,
  });
  return {
    status: "PASS",
    releaseID: target.releaseID,
    revision: target.source.revision,
    cacheIdentity: target.cache.identity,
  };
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    const result = main(process.argv.slice(2));
    process.stdout.write(JSON.stringify(result) + "\n");
    if (result.status === "UNKNOWN") process.exitCode = 2;
  } catch (error) {
    process.stderr.write(
      `Frontend dependency transition failed: ${/^[A-Z0-9_]+$/.test(error.message) ? error.message : "OPERATION_FAILED"}\n`,
    );
    process.exitCode = 1;
  }
}
