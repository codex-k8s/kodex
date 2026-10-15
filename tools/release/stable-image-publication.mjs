#!/usr/bin/env node
import { execFile, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { mkdir, open } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
const namespace = "kodex-system", secretName = "kodex-image-promotion-writer";
const registryObjects = [["deployment", "kodex-image-registry-promotion"], ["service", "kodex-image-registry-promotion"],
  ["deployment", "kodex-image-registry-pull"], ["service", "kodex-image-registry-pull"]];
const components = new Set(["control-plane", "secret-broker", "email-bridge", "runtime-controller", "integration-gateway",
  "integration-synthetic", "control-api-gateway", "staff-control-center", "egress-gateway", "automation-scheduler",
  "artifact-retention", "session-archive", "registry-pull-authorizer"]);
function requireValue(value, code) { if (!value) throw Error(code); }
const env = () => Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
async function run(program, args, timeout = 35000) {
  try { return (await execute(program, args, { cwd: root, env: env(), timeout, maxBuffer: 8 << 20 })).stdout.trim(); }
  catch { throw Error("STABLE_PUBLICATION_OPERATION_FAILED_OR_UNKNOWN"); }
}

export function validateStableReceipt(receipt) {
  requireValue(receipt?.schema === "kodex.stable-local-build/v1" && components.has(receipt.component) &&
    /^[a-f0-9]{40}$/.test(receipt.sourceSHA ?? "") && receipt.sourceSHA !== "0".repeat(40) &&
    /^[a-f0-9]{64}$/.test(receipt.contextSHA256 ?? "") && /^[a-f0-9]{64}$/.test(receipt.archiveSHA256 ?? "") &&
    /^sha256:[a-f0-9]{64}$/.test(receipt.imageDigest ?? "") && !receipt.imageDigest.endsWith("0".repeat(64)) &&
    receipt.publication === "NOT_RUN" && receipt.admission === "NOT_RUN" && receipt.nodePull === "NOT_RUN",
    "STABLE_BUILD_RECEIPT_INVALID");
  return { component: receipt.component, sourceSHA: receipt.sourceSHA, digest: receipt.imageDigest,
    tag: `stable-${receipt.sourceSHA}-${receipt.imageDigest.slice(-12)}` };
}

export async function privateArchiveHash(path) {
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const before = await file.stat();
    requireValue(before.isFile() && before.uid === process.getuid() && before.nlink === 1 &&
      (before.mode & 0o777) === 0o400 && before.size > 0 && before.size <= 8 * 1024 ** 3, "STABLE_PRIVATE_ARCHIVE_REQUIRED");
    const hash = createHash("sha256"), stream = file.createReadStream({ autoClose: false });
    for await (const block of stream) hash.update(block);
    const after = await file.stat();
    requireValue(before.dev === after.dev && before.ino === after.ino && before.size === after.size &&
      before.mtimeMs === after.mtimeMs && before.ctimeMs === after.ctimeMs, "STABLE_ARCHIVE_CHANGED");
    return hash.digest("hex");
  } finally { await file.close(); }
}

async function sourceArchiveHash(sourceSHA) {
  const child = spawn("git", ["archive", sourceSHA], { cwd: root, env: env(), stdio: ["ignore", "pipe", "ignore"] });
  const done = new Promise(resolve => { child.once("error", () => resolve(false)); child.once("close", code => resolve(code === 0)); });
  const timer = setTimeout(() => child.kill("SIGKILL"), 120000);
  try {
    const hash = createHash("sha256"); let bytes = 0;
    for await (const block of child.stdout) { bytes += block.length; requireValue(bytes <= 2 * 1024 ** 3, "STABLE_SOURCE_TOO_LARGE"); hash.update(block); }
    requireValue(await done, "STABLE_SOURCE_ARCHIVE_FAILED");
    return hash.digest("hex");
  } finally { clearTimeout(timer); if (child.exitCode === null && child.signalCode === null) child.kill("SIGKILL"); await done; }
}

async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(JSON.stringify(value) + "\n"); await file.sync(); } finally { await file.close(); }
}

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--receipt", "--archive", "--tool-state-directory", "--output", "--plan", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "STABLE_PUBLICATION_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(["plan", "apply", "readback"].includes(mode) && /^[A-Za-z0-9_.:@/-]{1,160}$/.test(context ?? "") &&
    !/prod(?:uction)?/i.test(context) && options["--output"], "STABLE_PUBLICATION_ARGUMENTS_INVALID");
  const kube = args => run("kubectl", ["--context", context, "--request-timeout=30s", ...args]);
  const get = async args => JSON.parse(await kube([...args, "-o", "json"]));
  const snapshot = async () => {
    const cluster = await get(["get", "namespace", "kube-system"]), ns = await get(["get", "namespace", namespace]);
    const registries = [];
    for (const [kind, name] of registryObjects) {
      const resource = await get(["-n", namespace, "get", kind, name]);
      requireValue(resource.metadata?.namespace === namespace && resource.metadata.name === name && resource.metadata.uid &&
        resource.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex" && resource.spec, "STABLE_REGISTRY_IDENTITY_INVALID");
      registries.push({ kind, name, uid: resource.metadata.uid, specSHA256: fingerprint(resource.spec) });
    }
    const secret = await kube(["-n", namespace, "get", "secret", secretName, "-o",
      'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}']);
    requireValue(/^[a-zA-Z0-9-]+\|[0-9]+$/.test(secret) && cluster.metadata?.uid && ns.metadata?.uid, "STABLE_REGISTRY_IDENTITY_INVALID");
    return { clusterUID: cluster.metadata.uid, namespaceUID: ns.metadata.uid, registries, writerMetadata: secret };
  };
  if (mode === "plan") {
    requireValue(options["--receipt"] && options["--archive"] && options["--tool-state-directory"] && !options["--plan"] &&
      !options["--approved-plan-sha"], "STABLE_PUBLICATION_ARGUMENTS_INVALID");
    const receipt = await readPrivateJSON(resolve(options["--receipt"])), image = validateStableReceipt(receipt);
    const source = await run("git", ["rev-parse", "HEAD"]);
    requireValue(await run("git", ["status", "--porcelain", "--untracked-files=no"]) === "", "STABLE_CLEAN_SOURCE_REQUIRED");
    await run("git", ["merge-base", "--is-ancestor", image.sourceSHA, source]);
    requireValue(await sourceArchiveHash(image.sourceSHA) === receipt.contextSHA256, "STABLE_BUILD_CONTEXT_MISMATCH");
    // Разные source commits разрешены лишь при неизменных build inputs компонента.
    await run("git", ["diff", "--quiet", image.sourceSHA, source, "--", dirname(receipt.dockerfile), "libs", "contracts",
      "go.mod", "go.sum", "go.work", "go.work.sum"]);
    const toolState = resolve(options["--tool-state-directory"]);
    const tagFile = await open(join(toolState, "image-supply-chain-tools-docker-tag"), constants.O_RDONLY | constants.O_NOFOLLOW);
    let tag;
    try {
      const stat = await tagFile.stat();
      requireValue(stat.isFile() && stat.uid === process.getuid() && !(stat.mode & 0o022) && stat.size < 256,
        "STABLE_TOOLS_TAG_INVALID");
      tag = (await tagFile.readFile("utf8")).trim();
    } finally { await tagFile.close(); }
    requireValue(/^kodex-local\/image-admission-tools:[a-f0-9]{64}$/.test(tag), "STABLE_TOOLS_TAG_INVALID");
    const toolsImageID = await run("docker", ["image", "inspect", "--format", "{{.Id}}", tag]);
    requireValue(/^sha256:[a-f0-9]{64}$/.test(toolsImageID), "STABLE_TOOLS_IMAGE_INVALID");
    const bound = await snapshot(), output = resolve(options["--output"]);
    await mkdir(output, { mode: 0o700 });
    await run("python3", [join(root, "tools/dev/verify-platform-image-archive.py"), "--stable", resolve(options["--receipt"]),
      resolve(options["--archive"]), join(output, "image.oci.tar")], 120000);
    const archiveSHA256 = await privateArchiveHash(join(output, "image.oci.tar"));
    requireValue(archiveSHA256 === receipt.archiveSHA256, "STABLE_ARCHIVE_MISMATCH");
    const plan = { schema: "kodex.stable-image-publication/v1", context, ...bound, source, receipt, image,
      toolsImageID, archiveSHA256 };
    await createPrivate(join(output, "plan.json"), plan);
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", component: image.component,
      digest: image.digest, planSHA256: fingerprint(plan) }) + "\n");
    return;
  }
  requireValue(options["--plan"] && !options["--receipt"] && !options["--archive"] && !options["--tool-state-directory"],
    "STABLE_PUBLICATION_ARGUMENTS_INVALID");
  const planPath = resolve(options["--plan"]), plan = await readPrivateJSON(planPath), hash = fingerprint(plan);
  requireValue(plan.schema === "kodex.stable-image-publication/v1" && plan.context === context &&
    fingerprint(validateStableReceipt(plan.receipt)) === fingerprint(plan.image), "STABLE_PUBLICATION_PLAN_INVALID");
  requireValue(mode === "readback" ? !options["--approved-plan-sha"] : options["--approved-plan-sha"] === hash,
    "STABLE_EXACT_OWNER_CONFIRMATION_REQUIRED");
  requireValue(await run("git", ["rev-parse", "HEAD"]) === plan.source &&
    await run("git", ["status", "--porcelain", "--untracked-files=no"]) === "", "STABLE_EXECUTOR_SOURCE_DRIFT");
  requireValue(await privateArchiveHash(join(dirname(planPath), "image.oci.tar")) === plan.archiveSHA256 &&
    plan.archiveSHA256 === plan.receipt.archiveSHA256, "STABLE_ARCHIVE_MISMATCH");
  const bound = { clusterUID: plan.clusterUID, namespaceUID: plan.namespaceUID, registries: plan.registries, writerMetadata: plan.writerMetadata };
  requireValue(fingerprint(await snapshot()) === fingerprint(bound), "STABLE_REGISTRY_DRIFT");
  const journal = await open(resolve(options["--output"]), "wx", 0o600);
  const record = async value => { await journal.writeFile(JSON.stringify(value) + "\n"); await journal.sync(); };
  try {
    await record({ schema: "kodex.stable-image-publication-attempt/v1", status: "INTENT", mode, planSHA256: hash,
      component: plan.image.component, digest: plan.image.digest });
    await run("bash", [join(root, "tools/release/publish-stable-image-worker.sh"), "--context", context,
      "--plan", planPath, "--mode", mode], 180000);
    requireValue(fingerprint(await snapshot()) === fingerprint(bound), "STABLE_REGISTRY_DRIFT");
    await record({ status: "PASS", phase: "REGISTRY_PUBLICATION_ONLY", nodePull: "NOT_RUN" });
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "REGISTRY_PUBLICATION_ONLY", component: plan.image.component,
      digest: plan.image.digest, nodePull: "NOT_RUN" }) + "\n");
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "STABLE_PUBLICATION_FAILED" }); throw error; }
  finally { await journal.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main(process.argv.slice(2)).catch(error => {
    process.stderr.write(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "STABLE_PUBLICATION_FAILED" }) + "\n");
    process.exitCode = 1;
  });
