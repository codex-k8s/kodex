#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { open } from "node:fs/promises";
import { promisify } from "node:util";
import { fileURLToPath, pathToFileURL } from "node:url";
import { currentPeers, replacementUnits } from "./edge-rebind.mjs";
import { readPrivateJSON } from "./edge-plan.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const execute = promisify(execFile), equal = (a, b) => fingerprint(a) === fingerprint(b);
const requireValue = (ok, code) => { if (!ok) throw Error(code); };

export function bootUnits(before) {
  requireValue(Object.keys(before).length === 4 && Object.keys(before).every(n => /^kodex-public-edge-https-[01]\.(socket|service)$/.test(n)),
    "EDGE_BOOT_UNIT_SET_INVALID");
  return Object.fromEntries(Object.entries(before).map(([name, body]) => {
    requireValue(typeof body === "string" && body.startsWith("# Kodex public edge plan "), "EDGE_BOOT_UNIT_INVALID");
    if (name.endsWith(".service")) return [name, body];
    if (body.includes("DefaultDependencies=no\n")) {
      requireValue(body.includes("Before=shutdown.target\n") && body.includes("Conflicts=shutdown.target\n") &&
        body.includes("WantedBy=multi-user.target\n") && !body.includes("WantedBy=sockets.target\n"), "EDGE_BOOT_SOURCE_UNSUPPORTED");
      return [name, body];
    }
    requireValue(body.includes("[Unit]\n") && body.includes("WantedBy=sockets.target\n") && !body.includes("DefaultDependencies="), "EDGE_BOOT_SOURCE_UNSUPPORTED");
    return [name, body.replace("[Unit]\n", "[Unit]\nDefaultDependencies=no\nBefore=shutdown.target\nConflicts=shutdown.target\n")
      .replace("WantedBy=sockets.target\n", "WantedBy=multi-user.target\n")];
  }));
}

export function canonicalObservedUnits(source, peerPins, observed) {
  requireValue(Object.keys(observed).length === 4, "EDGE_BOOT_UNIT_DRIFT");
  for (const [name, body] of Object.entries(observed)) {
    const ips = [...body.matchAll(/^IPAddressAllow=([0-9.]+)\/32$/gm)].map(m => m[1]);
    if (name.endsWith(".service")) requireValue(ips.pop() === source.config.backendIP, "EDGE_BOOT_UNIT_DRIFT");
    requireValue(ips.length === peerPins.length, "EDGE_BOOT_UNIT_DRIFT");
    const historical = peerPins.map((p, i) => ({ ...p, peerIP: ips[i] }));
    requireValue(body === bootUnits(replacementUnits(source, historical))[name], "EDGE_BOOT_UNIT_DRIFT");
  }
  return observed;
}

async function unitText(name) {
  requireValue(/^kodex-public-edge-https-[01]\.(socket|service)$/.test(name), "EDGE_BOOT_UNIT_INVALID");
  const file = await open(`/etc/systemd/system/${name}`, constants.O_RDONLY | constants.O_NOFOLLOW);
  try { const s = await file.stat(); requireValue(s.isFile() && s.uid === 0 && s.nlink === 1 && (s.mode & 0o777) === 0o644 && s.size < 16384,
    "EDGE_BOOT_UNIT_IDENTITY_INVALID"); return await file.readFile("utf8"); } finally { await file.close(); }
}

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) { const key = args.shift(); requireValue(["--peer-plan", "--plan", "--output", "--approved-plan-sha"].includes(key) &&
    !Object.hasOwn(options, key) && args.length, "EDGE_BOOT_ARGUMENTS_INVALID"); options[key] = args.shift(); }
  requireValue(["plan", "apply", "readback", "reconcile"].includes(mode) && (options["--output"] || mode === "reconcile"), "EDGE_BOOT_ARGUMENTS_INVALID");
  const plan = mode === "plan" ? null : await readPrivateJSON(options["--plan"]);
  const peerPath = mode === "plan" ? options["--peer-plan"] : plan?.peerPlan;
  const peers = await readPrivateJSON(peerPath), source = await readPrivateJSON(peers.sourcePlan);
  requireValue(peers.schema === "kodex.public-edge-peer-rebind/v1" && fingerprint(source) === peers.sourceSHA256 &&
    equal(bootUnits(peers.after), replacementUnits(source, peers.peers)), "EDGE_BOOT_SOURCE_INVALID");
  let after = bootUnits(peers.after);
  const env = Object.fromEntries(["HOME", "PATH", "LANG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = async (command, argv, input) => {
    try { const call = execute(command, argv, { env, timeout: 30000, maxBuffer: 4 << 20 });
      if (input !== undefined) { call.child.stdin.on("error", () => {}); call.child.stdin.end(input); }
      return (await call).stdout;
    } catch { throw Error("EDGE_BOOT_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const verify = async () => {
    const nodes = new Map();
    for (const peer of source.peers) nodes.set(peer.name, JSON.parse(await run("docker", ["inspect", peer.name]))[0]);
    const actual = currentPeers(source, nodes);
    requireValue(mode === "reconcile" ? actual.every((p, i) => p.networkID === peers.peers[i].networkID) : equal(actual, peers.peers), "EDGE_BOOT_PEERS_DRIFT");
    for (const name of Object.keys(after)) requireValue((await run("systemctl", ["show", name, "--property=DropInPaths", "--value"])).trim() === "",
      "EDGE_BOOT_UNEXPECTED_DROPIN");
    return actual;
  };
  let targetPeers = await verify();
  const desired = { schema: "kodex.public-edge-boot-plan/v1", peerPlan: peerPath, peerPlanSHA256: fingerprint(peers), before: peers.after, after };
  if (mode === "plan") {
    for (const name of Object.keys(after)) requireValue(await unitText(name) === peers.after[name], "EDGE_BOOT_UNIT_DRIFT");
    const output = await open(options["--output"], "wx", 0o600);
    try { await output.writeFile(JSON.stringify(desired)); await output.sync(); } finally { await output.close(); }
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(desired) }) + "\n"); return;
  }
  requireValue(equal(plan, desired), "EDGE_BOOT_PLAN_DRIFT");
  if (mode === "apply" || mode === "reconcile") requireValue(options["--approved-plan-sha"] === fingerprint(plan), "EDGE_BOOT_EXACT_APPROVAL_REQUIRED");
  const output = options["--output"] ? await open(options["--output"], "wx", 0o600) : null;
  const record = async row => { if (output) { await output.writeFile(JSON.stringify(row) + "\n"); await output.sync(); } };
  try {
    await record({ schema: "kodex.public-edge-boot-journal/v1", mode, planSHA256: fingerprint(plan), status: "IN_PROGRESS" });
    let before = plan.before;
    if (mode === "reconcile") {
      // IP в текущем unit — только preimage. Новый адрес всегда берётся из
      // прежней Docker identity, а весь остальной unit обязан быть canonical.
      const names = Object.keys(after), observed = {};
      for (const name of names) observed[name] = await unitText(name);
      // Частичная атомарная замена четырёх файлов после crash разрешается
      // лишь как набор exact canonical preimage, не как произвольный текст.
      before = canonicalObservedUnits(source, peers.peers, observed);
      targetPeers = await verify();
      after = bootUnits(replacementUnits(source, targetPeers));
    }
    for (const name of Object.keys(after)) {
      const body = await unitText(name); requireValue(body === after[name] || ["apply", "reconcile"].includes(mode) && body === before[name], "EDGE_BOOT_UNIT_DRIFT");
    }
    if (mode === "apply" || mode === "reconcile" && !equal(before, after)) {
      await record({ status: "UNIT_REPLACE_ATTEMPT" });
      await run("sudo", ["-n", process.execPath, fileURLToPath(new URL("./edge-unit-worker.mjs", import.meta.url))], JSON.stringify({ before, after }));
      await run("sudo", ["-n", "systemctl", "daemon-reload"]);
      for (const name of Object.keys(after).filter(n => n.endsWith(".socket"))) {
        requireValue(equal(await verify(), targetPeers), "EDGE_BOOT_PEERS_DRIFT"); await record({ status: "SOCKET_REENABLE_ATTEMPT", name });
        if (mode === "apply") await run("sudo", ["-n", "systemctl", "reenable", name]);
        await run("sudo", ["-n", "systemctl", "stop", name.replace(/\.socket$/, ".service")]);
        await run("sudo", ["-n", "systemctl", "restart", name]);
      }
    }
    if (mode === "reconcile") for (const name of Object.keys(after).filter(n => n.endsWith(".socket"))) {
      const state = (await run("systemctl", ["show", name, "--property=ActiveState", "--value"])).trim();
      requireValue(["active", "inactive", "failed"].includes(state), "EDGE_BOOT_SOCKET_TRANSITIONAL");
      if (state !== "active") await run("sudo", ["-n", "systemctl", "start", name]);
    }
    for (const [name, body] of Object.entries(after)) {
      requireValue(await unitText(name) === body, "EDGE_BOOT_UNIT_DRIFT");
      if (name.endsWith(".socket")) requireValue((await run("systemctl", ["show", name, "--property=DefaultDependencies", "--value"])).trim() === "no" &&
        (await run("systemctl", ["is-active", name])).trim() === "active" && (await run("systemctl", ["is-enabled", name])).trim() === "enabled",
      "EDGE_BOOT_SOCKET_READBACK_FAILED");
    }
    requireValue(equal(await verify(), targetPeers), "EDGE_BOOT_PEERS_DRIFT"); await record({ status: "PASS", phase: "BOOT_ORDER_AND_SOCKET_READBACK", actualHostReboot: "NOT_RUN" });
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "BOOT_ORDER_AND_SOCKET_READBACK", actualHostReboot: "NOT_RUN" }) + "\n");
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message) ? error.message : "EDGE_BOOT_FAILED" }); throw error; }
  finally { await output?.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main(process.argv.slice(2)).catch(error => {
  process.stderr.write(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message) ? error.message : "EDGE_BOOT_FAILED" }) + "\n"); process.exitCode = 1;
});
