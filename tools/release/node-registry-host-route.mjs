#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { constants, openSync, fstatSync, readFileSync, writeFileSync, closeSync, fsyncSync, writeSync } from "node:fs";
import { isIP } from "node:net";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { policyDigest, runnerPullReference } from "./runner-policy-model.mjs";

const ensure = (ok, code) => { if (!ok) throw Error(code); };
const hash = value => createHash("sha256").update(value).digest("hex");
export function repairHosts(before, host, address) {
  ensure(typeof before === "string" && before.length < 65536 && !before.includes("\0") &&
    /^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(host) &&
    isIP(address) === 4 && !/^(?:0|127|22[4-9]|23[0-9])\./.test(address), "HOST_ROUTE_INPUT_INVALID");
  const retained = before.split("\n").filter(line => {
    const fields = line.split("#", 1)[0].trim().split(/\s+/);
    if (!fields.slice(1).includes(host)) return true;
    ensure(fields.length === 2 && fields[1] === host && isIP(fields[0]) === 4, "HOST_ROUTE_AMBIGUOUS");
    return false;
  }).join("\n");
  return retained.replace(/\n*$/, "\n") + `${address} ${host}\n`;
}
function privateJSON(path) {
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const st = fstatSync(fd);
    ensure(st.isFile() && st.uid === process.getuid() && (st.mode & 0o777) === 0o600 && st.nlink === 1 && st.size < 1 << 20, "PRIVATE_PLAN_REQUIRED");
    return JSON.parse(readFileSync(fd, "utf8"));
  } finally { closeSync(fd); }
}
function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) { const key = args.shift(); ensure(["--context", "--output", "--plan", "--evidence", "--confirm"].includes(key) && !Object.hasOwn(options, key) && args.length, "ARGUMENT_INVALID"); options[key] = args.shift(); }
  ensure(["plan", "apply", "readback"].includes(mode) && options["--context"] === "k3d-kodex", "EXACT_LOCAL_CLUSTER_REQUIRED");
  const run = (file, argv, input) => { try { return execFileSync(file, argv, { input, encoding: "utf8", timeout: 30000, maxBuffer: 2 << 20, stdio: [input === undefined ? "ignore" : "pipe", "pipe", "pipe"] }); } catch { throw Error("HOST_ROUTE_OPERATION_FAILED_OR_UNKNOWN"); } };
  const kube = (...argv) => JSON.parse(run("kubectl", ["--context", "k3d-kodex", "--request-timeout=15s", ...argv, "-o", "json"]));
  const git = (...argv) => run("git", ["-C", fileURLToPath(new URL("../../", import.meta.url)), ...argv]).trimEnd();
  const executorSHA = git("rev-parse", "HEAD");
  ensure(/^[a-f0-9]{40}$/.test(executorSHA) && git("status", "--porcelain", "--untracked-files=normal") === "", "CLEAN_SOURCE_REQUIRED");
  const app = kube("get", "namespace", "kodex-system"), system = kube("get", "namespace", "kube-system");
  ensure(app.metadata.labels?.["kodex.dev/environment"] === "staging" && app.metadata.labels?.["app.kubernetes.io/part-of"] === "kodex", "STAGING_NAMESPACE_REQUIRED");
  const controller = kube("-n", "kodex-system", "get", "deployment", "image-admission-controller");
  const policyName = controller.spec.template.spec.containers.find(c => c.name === "image-admission-controller")?.env.find(e => e.name === "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP")?.value;
  ensure(/^kodex-image-admission-policy(?:-[a-f0-9]{32})?$/.test(policyName ?? ""), "POLICY_IDENTITY_REQUIRED");
  const policy = kube("-n", "kodex-system", "get", "configmap", policyName);
  ensure(policy.immutable === true && policy.metadata.labels?.["kodex.dev/owner-intent"] === "true", "IMMUTABLE_POLICY_REQUIRED");
  const host = policy.data.pullRegistryHost;
  ensure(policyDigest(policy.data) === policy.data.policySHA256 && policy.data.nodeReadbackImage === runnerPullReference(policy.data, policy.data.trustedRoleBaseDigest), "POLICY_DIGEST_REQUIRED");
  const inspect = name => { const rows = JSON.parse(run("docker", ["inspect", name])); ensure(rows.length === 1, "EXACT_CONTAINER_REQUIRED"); const d = rows[0]; ensure(d.Name === `/${name}` && d.State.Running && d.Config.Labels?.["k3d.cluster"] === "kodex" && /^[a-f0-9]{64}$/.test(d.Id), "EXACT_CONTAINER_REQUIRED"); return d; };
  const lb = inspect("k3d-kodex-serverlb"), address = lb.NetworkSettings.Networks["k3d-kodex"]?.IPAddress;
  const nodeCommand = (id, argv, input) => run("docker", ["exec", ...(input === undefined ? [] : ["-i"]), id, ...argv], input);
  const inventory = kube("get", "nodes").items;
  ensure(inventory.length > 0 && inventory.length <= 8 && new Set(inventory.map(n => n.metadata.name)).size === inventory.length, "NODE_INVENTORY_INVALID");
  const nodes = inventory.map(n => { ensure(/^k3d-kodex-(?:server|agent)-[0-9]+$/.test(n.metadata.name), "NODE_IDENTITY_INVALID"); const d = inspect(n.metadata.name); const before = nodeCommand(d.Id, ["cat", "/etc/hosts"]); return { name: n.metadata.name, uid: n.metadata.uid, id: d.Id, beforeSHA256: hash(before), after: repairHosts(before, host, address) }; });
  const identity = { clusterUID: system.metadata.uid, namespaceUID: app.metadata.uid, policyUID: policy.metadata.uid, policySHA256: fingerprint(policy.data), lbID: lb.Id, address, host, executorSHA };
  if (mode === "plan") { ensure(options["--output"] && !options["--plan"] && !options["--evidence"] && !options["--confirm"], "PLAN_ARGUMENT_INVALID"); writeFileSync(options["--output"], JSON.stringify({ schema: "kodex.node-host-route/v1", ...identity, nodes }) + "\n", { mode: 0o600, flag: "wx" }); console.log(JSON.stringify({ status: "PLANNED", nodes: nodes.length })); return; }
  ensure(options["--plan"] && options["--evidence"] && !options["--output"] && options["--confirm"] === (mode === "apply" ? "REPAIR-STAGING-REGISTRY-HOST-ROUTES" : undefined), "OPERATION_ARGUMENT_INVALID");
  const plan = privateJSON(options["--plan"]);
  ensure(plan.schema === "kodex.node-host-route/v1" && Object.entries(identity).every(([key, value]) => plan[key] === value) && plan.nodes.length === nodes.length, "HOST_ROUTE_PLAN_DRIFT");
  for (const node of nodes) { const expected = plan.nodes.find(n => n.id === node.id && n.uid === node.uid && n.name === node.name); ensure(expected && repairHosts(expected.after, host, address) === expected.after && (mode === "apply" ? node.beforeSHA256 === expected.beforeSHA256 && node.after === expected.after : node.beforeSHA256 === hash(expected.after)), "NODE_HOSTS_DRIFT"); }
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const record = value => { writeSync(fd, JSON.stringify({ at: new Date().toISOString(), ...value }) + "\n"); fsyncSync(fd); };
  try {
    for (const node of nodes) {
      if (mode === "apply" && node.beforeSHA256 !== hash(node.after)) {
        record({ status: "INTENT", node: node.name });
        nodeCommand(node.id, ["sh", "-ec", 'mkdir /run/kodex-registry-host-route.lock; trap "rmdir /run/kodex-registry-host-route.lock" EXIT; test ! -L /etc/hosts; test -f /etc/hosts; test "$(sha256sum /etc/hosts | cut -d " " -f1)" = "$1"; cat > /etc/hosts', "kodex-host-route", node.beforeSHA256], node.after);
      }
      ensure(nodeCommand(node.id, ["cat", "/etc/hosts"]) === node.after && inspect(node.name).Id === node.id, "NODE_HOSTS_READBACK_MISMATCH");
      record({ status: "PASS", node: node.name, afterSHA256: hash(node.after) });
    }
    console.log(JSON.stringify({ status: "PASS", phase: "HOST_ROUTE_ONLY", nodes: nodes.length, freshPull: "NOT_RUN" }));
  } catch (error) { record({ status: "UNKNOWN", code: error.message }); throw error; } finally { closeSync(fd); }
}
if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  try { main(process.argv.slice(2)); } catch (error) { console.error(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message) ? error.message : "HOST_ROUTE_FAILED" })); process.exitCode = 1; }
}
