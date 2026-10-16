#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { lstat, mkdir, open } from "node:fs/promises";
import { homedir, userInfo } from "node:os";
import { isIP } from "node:net";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { fingerprint } from "../release/stable-inventory.mjs";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";

const execute = promisify(execFile), namespace = "kodex-system", context = "k3d-kodex";
const bindings = new Map([
  ["control-plane-exact-runtime-paths", "control-plane"], ["runtime-controller-exact-paths", "runtime-controller"],
  ["secret-broker-exact-runtime-paths", "secret-broker"], ["session-archive-exact-paths", "session-archive"],
  ["kodex-image-admission-controller-exact-paths", "image-admission-controller"],
  ...["control-plane", "runtime-controller", "secret-broker", "session-archive"].map(n => [`${n}-kubernetes-api-current`, n]),
]);
const requireValue = (ok, code) => { if (!ok) throw Error(code); };
const equal = (a, b) => fingerprint(a) === fingerprint(b);

export function refreshAPIRules(policy, endpointIPs, endpointPorts) {
  const component = bindings.get(policy?.metadata?.name);
  requireValue(component && policy.metadata.namespace === namespace &&
    policy.spec?.podSelector?.matchLabels?.["app.kubernetes.io/name"] === component &&
    endpointIPs.length > 0 && endpointIPs.length <= 4 && endpointIPs.every(ip => isIP(ip) === 4) &&
    new Set(endpointIPs).size === endpointIPs.length && equal(endpointPorts, [6443]), "API_EGRESS_BOUNDARY_INVALID");
  const after = structuredClone(policy.spec);
  let count = 0;
  after.egress = after.egress.map(rule => {
    if (!(rule.ports ?? []).some(p => p.port === 6443)) return rule;
    requireValue(equal(Object.keys(rule).sort(), ["ports", "to"]) &&
      equal(rule.ports, [{ port: 6443, protocol: "TCP" }]) && rule.to.length > 0 && rule.to.every(peer =>
        equal(Object.keys(peer), ["ipBlock"]) && equal(Object.keys(peer.ipBlock), ["cidr"]) &&
        peer.ipBlock.cidr.endsWith("/32") && isIP(peer.ipBlock.cidr.slice(0, -3)) === 4), "API_EGRESS_RULE_AMBIGUOUS");
    count++;
    return { ...rule, to: [...endpointIPs].sort().map(ip => ({ ipBlock: { cidr: `${ip}/32` } })) };
  });
  requireValue(count === 1, "API_EGRESS_RULE_MISSING_OR_DUPLICATE");
  return after;
}

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) { const key = args.shift(); requireValue(["--plan", "--output", "--approved-plan-sha", "--edge-plan", "--edge-plan-sha"].includes(key) &&
    !Object.hasOwn(options, key) && args.length, "API_EGRESS_ARGUMENTS_INVALID"); options[key] = args.shift(); }
  requireValue(["plan", "reconcile", "install-timer"].includes(mode), "API_EGRESS_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = async (command, argv, input) => {
    try { const pending = execute(command, argv, { env, timeout: 25000, maxBuffer: 4 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("API_EGRESS_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const kube = (argv, input) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=15s", ...argv]) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-api-egress", "--context", context, "--request-timeout=15s", ...argv], input);
  const get = async argv => JSON.parse(await kube([...argv, "-o", "json"]));
  const cluster = await get(["get", "namespace", "kube-system"]), ns = await get(["get", "namespace", namespace]);
  const policies = (await get(["-n", namespace, "get", "networkpolicies"])).items.filter(p => bindings.has(p.metadata.name));
  const slices = await get(["-n", "default", "get", "endpointslices", "-l", "kubernetes.io/service-name=kubernetes"]);
  const endpointIPs = [...new Set(slices.items.filter(s => s.addressType === "IPv4").flatMap(s =>
    s.endpoints.filter(e => e.conditions?.ready === true).flatMap(e => e.addresses)))].sort();
  const endpointPorts = [...new Set(slices.items.flatMap(s => s.ports.filter(p => (p.protocol ?? "TCP") === "TCP").map(p => p.port)))].sort();
  const nodes = await get(["get", "nodes", "-l", "node-role.kubernetes.io/control-plane"]);
  const nodeAddresses = nodes.items.filter(n => n.status.conditions.some(c => c.type === "Ready" && c.status === "True"))
    .flatMap(n => n.status.addresses.filter(a => a.type === "InternalIP").map(a => a.address));
  requireValue(endpointIPs.every(ip => nodeAddresses.includes(ip)), "API_EGRESS_ENDPOINT_NOT_READY_CONTROL_PLANE");
  for (const policy of policies) refreshAPIRules(policy, endpointIPs, endpointPorts);
  const snapshot = { schema: "kodex.local-api-egress/v1", context, clusterUID: cluster.metadata.uid, namespaceUID: ns.metadata.uid,
    policies: policies.map(p => ({ name: p.metadata.name, uid: p.metadata.uid })).sort((a, b) => a.name.localeCompare(b.name)) };
  requireValue(snapshot.policies.length >= 5 && snapshot.policies.length <= 9 && snapshot.policies.every(p => p.uid) &&
    [...bindings.keys()].slice(0, 5).every(name => policies.some(p => p.metadata.name === name)), "API_EGRESS_POLICY_SET_INCOMPLETE");
  if (mode === "plan") {
    requireValue(options["--output"], "API_EGRESS_OUTPUT_REQUIRED");
    const output = await open(options["--output"], "wx", 0o600);
    try { await output.writeFile(JSON.stringify(snapshot)); await output.sync(); } finally { await output.close(); }
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(snapshot), policyCount: policies.length }) + "\n"); return;
  }
  const plan = await readPrivateJSON(options["--plan"]);
  requireValue(equal(plan, snapshot) && options["--approved-plan-sha"] === fingerprint(plan), "API_EGRESS_PLAN_OR_APPROVAL_DRIFT");
  if (mode === "install-timer") {
    const script = fileURLToPath(import.meta.url), file = options["--plan"], home = homedir(), account = userInfo();
    requireValue([script, file, home, process.execPath].every(v => /^\/[A-Za-z0-9_.@/-]+$/.test(v)) &&
      account.uid === process.getuid() && account.uid > 0 && /^[a-z_][a-z0-9_-]*$/.test(account.username), "API_EGRESS_USER_IDENTITY_INVALID");
    const key = `${home}/.kube/config`, metadata = await lstat(key);
    requireValue(metadata.isFile() && !metadata.isSymbolicLink() && metadata.uid === account.uid && (metadata.mode & 0o777) === 0o600,
      "API_EGRESS_OPERATOR_KUBECONFIG_INVALID");
    const dir = `${home}/.config/systemd/user`; await mkdir(dir, { recursive: true, mode: 0o700 });
    const directoryMetadata = await lstat(dir);
    requireValue(directoryMetadata.isDirectory() && !directoryMetadata.isSymbolicLink() && directoryMetadata.uid === account.uid &&
      (directoryMetadata.mode & 0o022) === 0, "API_EGRESS_USER_UNIT_DIRECTORY_INVALID");
    const units = {
      "kodex-api-egress-reconcile.service": `[Unit]\nDescription=Reconcile exact Kodex local Kubernetes API destinations\nAfter=docker.service\n\n[Service]\nType=oneshot\nEnvironment=KUBECONFIG=${key}\nEnvironment=PATH=${home}/.local/state/kodex-dev/tools/bin:/usr/local/bin:/usr/bin:/bin\nExecStart=${process.execPath} ${script} reconcile --plan ${file} --approved-plan-sha ${fingerprint(plan)}\nTimeoutStartSec=60\nNoNewPrivileges=yes\n`,
      "kodex-api-egress-reconcile.timer": "[Unit]\nDescription=Refresh exact Kodex API destinations after local node restart\n\n[Timer]\nOnStartupSec=30s\nOnUnitActiveSec=60s\nAccuracySec=5s\n\n[Install]\nWantedBy=timers.target\n",
    };
    if (options["--edge-plan"]) {
      const edge = await readPrivateJSON(options["--edge-plan"]);
      requireValue(edge.schema === "kodex.public-edge-boot-plan/v1" && /^\/[A-Za-z0-9_.@/-]+$/.test(options["--edge-plan"]) &&
        options["--edge-plan-sha"] === fingerprint(edge), "API_EGRESS_EDGE_PLAN_OR_APPROVAL_INVALID");
      const edgeScript = fileURLToPath(new URL("../../infra/public-ingress/edge-boot-recovery.mjs", import.meta.url));
      units["kodex-public-edge-reconcile.service"] = `[Unit]\nDescription=Reconcile approved Kodex public relay node identities\n\n[Service]\nType=oneshot\nEnvironment=PATH=${home}/.local/state/kodex-dev/tools/bin:/usr/local/bin:/usr/bin:/bin\nExecStart=${process.execPath} ${edgeScript} reconcile --plan ${options["--edge-plan"]} --approved-plan-sha ${fingerprint(edge)}\nTimeoutStartSec=90\n`;
      units["kodex-public-edge-reconcile.timer"] = "[Unit]\nDescription=Restore approved Kodex public relay after local node restart\n\n[Timer]\nOnStartupSec=35s\nOnUnitActiveSec=60s\nAccuracySec=5s\n\n[Install]\nWantedBy=timers.target\n";
    }
    for (const [name, body] of Object.entries(units)) {
      const path = `${dir}/${name}`;
      let handle;
      try { handle = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW); }
      catch (e) { if (e.code !== "ENOENT") throw e; }
      if (handle) { try { const stat = await handle.stat(); requireValue(stat.isFile() && stat.uid === account.uid && stat.nlink === 1 &&
        await handle.readFile("utf8") === body, "API_EGRESS_USER_UNIT_COLLISION"); } finally { await handle.close(); } }
    }
    for (const [name, body] of Object.entries(units)) {
      try { const handle = await open(`${dir}/${name}`, "wx", 0o644); try { await handle.writeFile(body); await handle.sync(); } finally { await handle.close(); } }
      catch (e) { if (e.code !== "EEXIST") throw e; }
    }
    if ((await run("loginctl", ["show-user", account.username, "--property=Linger", "--value"])).trim() !== "yes")
      await run("sudo", ["-n", "loginctl", "enable-linger", account.username]);
    await run("systemctl", ["--user", "daemon-reload"]);
    const timers = Object.keys(units).filter(n => n.endsWith(".timer"));
    await run("systemctl", ["--user", "enable", "--now", ...timers]);
    const starts = await Promise.allSettled(timers.map(n => run("systemctl", ["--user", "start", n.replace(/\.timer$/, ".service")])));
    requireValue(starts.every(r => r.status === "fulfilled"), "API_EGRESS_RECONCILE_SERVICE_FAILED");
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "OPERATOR_TIMER_INSTALLED", policyCount: policies.length }) + "\n"); return;
  }
  let changed = 0;
  for (const policy of policies) {
    const after = refreshAPIRules(policy, endpointIPs, endpointPorts);
    if (equal(after, policy.spec)) continue;
    await kube(["-n", namespace, "patch", "networkpolicy", policy.metadata.name, "--type=json", "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify([
      { op: "test", path: "/metadata/uid", value: policy.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: policy.metadata.resourceVersion },
      { op: "test", path: "/spec", value: policy.spec }, { op: "replace", path: "/spec", value: after }]));
    const current = await get(["-n", namespace, "get", "networkpolicy", policy.metadata.name]);
    requireValue(current.metadata.uid === policy.metadata.uid && equal(current.spec, after), "API_EGRESS_READBACK_MISMATCH"); changed++;
  }
  process.stdout.write(JSON.stringify({ status: "PASS", phase: "EXACT_API_DESTINATIONS", policyCount: policies.length, changed }) + "\n");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main(process.argv.slice(2)).catch(error => {
  process.stderr.write(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message) ? error.message : "API_EGRESS_RECONCILE_FAILED" }) + "\n"); process.exitCode = 1;
});
