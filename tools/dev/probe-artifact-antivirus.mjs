#!/usr/bin/env node
// Только безопасный контроль CP-owned Unix scanner, вне писем и файлов агента.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const fail = (ok, code) => { if (!ok) throw Error(code); };
const configPath = "/etc/kodex-skill-scanner/clamd.conf";
const namespace = "kodex-system";
const eicar = "X5O!P%@AP[4\\PZX54(P^)7CC)7}$" + "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*";

export function verifyScannerControl(clean, infected) {
  fail(clean.status === 0 && /(?:^|\n)(?:stdin|stream): OK(?:\r?\n|$)/.test(clean.stdout ?? ""), "ANTIVIRUS_CLEAN_CONTROL_FAILED");
  fail(infected.status === 1 && /(?:EICAR|Eicar)[^\r\n]* FOUND/.test(infected.stdout ?? ""), "ANTIVIRUS_EICAR_CONTROL_FAILED");
  return { clean: "PASS", eicar: "PASS" };
}

export function probe(context) {
  fail(typeof context === "string" && /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/.test(context), "EXACT_KUBERNETES_CONTEXT_REQUIRED");
  const execute = (args, input) => spawnSync("kubectl", ["--context", context, "-n", namespace, ...args],
    { encoding: "utf8", input, timeout: 90000, maxBuffer: 1 << 20, stdio: ["pipe", "pipe", "pipe"] });
  const get = (kind, name) => {
    const result = execute(["get", kind, ...(name ? [name] : ["-l", "app.kubernetes.io/name=control-plane"]), "-o", "json"]);
    fail(result.status === 0, "ANTIVIRUS_KUBERNETES_READ_FAILED");
    return JSON.parse(result.stdout);
  };
  const deployment = get("deployment", "control-plane");
  fail(deployment.metadata?.uid && deployment.spec?.replicas === 2 && deployment.status?.readyReplicas === 2,
    "ANTIVIRUS_DEPLOYMENT_NOT_READY");
  const owners = new Set(get("replicasets").items.filter(row => row.metadata.ownerReferences?.some(owner =>
    owner.controller === true && owner.kind === "Deployment" && owner.uid === deployment.metadata.uid)).map(row => row.metadata.uid));
  const pods = get("pods").items.filter(row => row.metadata.ownerReferences?.some(owner => owner.controller === true && owners.has(owner.uid)) &&
    !row.metadata.deletionTimestamp && row.status?.phase === "Running");
  fail(pods.length === 2, "ANTIVIRUS_POD_CARDINALITY_INVALID");
  const expected = createHash("sha256").update(readFileSync(new URL("../../deploy/k8s/base/control-plane/skill-scanner/clamd.conf", import.meta.url))).digest("hex");
  const results = [];
  for (const pod of pods) {
    const status = pod.status.containerStatuses?.find(container => container.name === "skill-scanner");
    fail(status?.ready && status.containerID && status.imageID, "ANTIVIRUS_SCANNER_NOT_READY");
    const exec = (args, input) => execute(["exec", ...(input === undefined ? [] : ["-i"]), pod.metadata.name, "-c", "skill-scanner", "--", ...args], input);
    const hash = exec(["sha256sum", configPath]);
    fail(hash.status === 0 && hash.stdout.trim().split(/\s+/)[0] === expected, "ANTIVIRUS_CONFIGURATION_DRIFT");
    const version = exec(["clamdscan", `--config-file=${configPath}`, "--version"]);
    fail(version.status === 0 && /^ClamAV [^\r\n]{1,249}\r?\n?$/.test(version.stdout), "ANTIVIRUS_VERSION_UNAVAILABLE");
    const command = ["clamdscan", `--config-file=${configPath}`, "--stream", "--no-summary", "-"];
    const controls = verifyScannerControl(exec(command, "Harmless bounded antivirus control.\n"), exec(command, eicar));
    const after = get("pod", pod.metadata.name);
    const afterStatus = after.status?.containerStatuses?.find(container => container.name === "skill-scanner");
    fail(after.metadata.uid === pod.metadata.uid && afterStatus?.ready && afterStatus.containerID === status.containerID &&
      afterStatus.imageID === status.imageID && afterStatus.restartCount === status.restartCount, "ANTIVIRUS_INSTANCE_CHANGED");
    results.push({ pod: pod.metadata.name, podUID: pod.metadata.uid, imageID: status.imageID,
      engine: version.stdout.trim(), configurationSHA256: expected, ...controls });
  }
  return { status: "PASS", scope: "live-clamd-unix-controls-only", deploymentUID: deployment.metadata.uid, results };
}

if (process.argv[1] && pathToFileURL(process.argv[1]).href === import.meta.url) {
  try {
    fail(process.argv.length === 4 && process.argv[2] === "--context", "USAGE_PROBE_ARTIFACT_ANTIVIRUS_CONTEXT");
    process.stdout.write(`${JSON.stringify(probe(process.argv[3]))}\n`);
  } catch (error) {
    process.stderr.write(`Artifact antivirus probe failed: ${error.message}\n`);
    process.exitCode = 1;
  }
}
