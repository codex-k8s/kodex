#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { constants, lstatSync, openSync, readFileSync, writeFileSync, closeSync } from "node:fs";
import { resolve } from "node:path";
import { fingerprint } from "./scoped-release.mjs";

const namespace = "kodex-system";
const deploymentName = "integration-gateway";
function requireValue(condition, code) { if (!condition) throw new Error(code); }
async function main(args) {
  const phase = args.shift();
  const options = {};
  while (args.length) {
    const name = args.shift();
    requireValue(["--context", "--plan"].includes(name) && !Object.hasOwn(options, name) && args.length, "INVALID_ARGUMENTS");
    options[name] = args.shift();
  }
  requireValue(["plan", "pause", "resume"].includes(phase) && options["--context"] === "k3d-kodex", "EXACT_LOCAL_CONTEXT_REQUIRED");
  const planPath = resolve(options["--plan"] ?? "");
  requireValue(typeof options["--plan"] === "string" && options["--plan"].startsWith("/"), "ABSOLUTE_PLAN_REQUIRED");
  const kubectl = (arguments_, input) => {
    try {
      return execFileSync("kubectl", ["--context", "k3d-kodex", "--request-timeout=15s", "-n", namespace, ...arguments_],
        { encoding: "utf8", timeout: 20000, maxBuffer: 4 << 20, input, stdio: ["pipe", "pipe", "pipe"] });
    } catch { throw new Error("KUBERNETES_OPERATION_FAILED"); }
  };
  const deployment = () => JSON.parse(kubectl(["get", "deployment", deploymentName, "-o", "json"]));
  const current = deployment();
  requireValue(current.metadata?.name === deploymentName && current.metadata.namespace === namespace &&
    current.metadata.labels?.["kodex.dev/environment"] === "staging" &&
    current.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex", "DEPLOYMENT_IDENTITY_MISMATCH");
  const image = current.spec.template.spec.containers.find((item) => item.name === deploymentName)?.image;
  requireValue(typeof image === "string" && /@sha256:[a-f0-9]{64}$/.test(image), "IMMUTABLE_IMAGE_REQUIRED");
  const clusterUID = JSON.parse(kubectl(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid;
  if (phase === "plan") {
    requireValue(current.spec.replicas === 2 && current.status.availableReplicas >= 2, "TWO_AVAILABLE_REPLICAS_REQUIRED");
    const paused = structuredClone(current.spec);
    paused.replicas = 0;
    const plan = { version: 1, context: "k3d-kodex", namespace, deploymentName, clusterUID,
      uid: current.metadata.uid, resourceVersion: current.metadata.resourceVersion,
      replicas: 2, image, specSHA256: fingerprint(current.spec), pausedSpecSHA256: fingerprint(paused) };
    const fd = openSync(planPath, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
    try { writeFileSync(fd, `${JSON.stringify(plan)}\n`); } finally { closeSync(fd); }
    process.stdout.write("Mailbox worker pause plan ready\n");
    return;
  }
  const stat = lstatSync(planPath);
  requireValue(stat.isFile() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o600 && stat.nlink === 1 && stat.size < 8192,
    "PRIVATE_OWNED_PLAN_REQUIRED");
  const plan = JSON.parse(readFileSync(planPath, "utf8"));
  requireValue(plan.version === 1 && plan.context === "k3d-kodex" && plan.namespace === namespace &&
    plan.deploymentName === deploymentName && plan.clusterUID === clusterUID && plan.uid === current.metadata.uid &&
    plan.replicas === 2 && plan.image === image, "PLAN_IDENTITY_MISMATCH");
  const before = phase === "pause" ? 2 : 0;
  const after = phase === "pause" ? 0 : 2;
  const expectedSHA = phase === "pause" ? plan.specSHA256 : plan.pausedSpecSHA256;
  requireValue(current.spec.replicas === before && fingerprint(current.spec) === expectedSHA, "SPEC_CHANGED_SINCE_PLAN");
  const patch = [
    { op: "test", path: "/metadata/uid", value: plan.uid },
    { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
    { op: "test", path: "/spec/replicas", value: before },
    { op: "replace", path: "/spec/replicas", value: after },
  ];
  // Patch содержит только публичные UID/RV и replicas, без env и credential.
  kubectl(["patch", "deployment", deploymentName, "--type=json", "-p", JSON.stringify(patch), "-o", "name"]);
  const deadline = Date.now() + 120000;
  const selector = Object.entries(current.spec.selector.matchLabels).map(([key, value]) => `${key}=${value}`).join(",");
  let ready = false;
  while (Date.now() < deadline) {
    const observed = deployment();
    requireValue(observed.metadata.uid === plan.uid && fingerprint(observed.spec) === (phase === "pause" ? plan.pausedSpecSHA256 : plan.specSHA256),
      "DEPLOYMENT_CHANGED_DURING_WAIT");
    const pods = JSON.parse(kubectl(["get", "pods", "-l", selector, "-o", "json"])).items;
    ready = phase === "pause" ? pods.every((pod) => ["Succeeded", "Failed"].includes(pod.status?.phase)) : observed.status.observedGeneration >= observed.metadata.generation &&
      observed.status.readyReplicas === 2 && observed.status.availableReplicas === 2 && observed.status.updatedReplicas === 2;
    if (ready) break;
    await new Promise((done) => setTimeout(done, 1000));
  }
  requireValue(ready, "WORKER_TRANSITION_TIMEOUT");
  process.stdout.write(`Mailbox worker ${phase} verified: replicas=${after}\n`);
}
try { await main(process.argv.slice(2)); }
catch (error) {
  const code = /^[A-Z_]{1,80}$/.test(error.message) ? error.message : "MAILBOX_WORKER_OPERATION_FAILED";
  process.stderr.write(`Mailbox worker operation failed: ${code}\n`);
  process.exitCode = 1;
}
