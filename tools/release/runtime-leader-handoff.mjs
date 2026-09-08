#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { openSync, writeSync, fsyncSync, closeSync, readFileSync, mkdtempSync, rmSync } from "node:fs";
import { randomUUID } from "node:crypto";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { requireInstanceWriters } from "./worker-grant-transition.mjs";

const target = "runtime-controller";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
function requireValue(value, code) { if (!value) throw new Error(code); }

function bindings(state) {
  return { clusterUID: state.clusterUID, namespaceUID: state.namespaceUID, readers: state.readers,
    target: state.target, uid: state.uid, specSHA256: state.specSHA256,
    pods: [...state.pods].sort((a, b) => a.uid.localeCompare(b.uid)) };
}
function rows(state, uid) {
  const selected = state.database.instances.filter((row) => row.workload === target && row.instance === uid);
  requireValue(selected.length === 1, "INSTANCE_REGISTRATION_REQUIRED"); return selected[0];
}
function floor(state) {
  const selected = state.database.floors.filter((row) => row.workload === target);
  requireValue(selected.length === 1 && Number.isSafeInteger(selected[0].generation) && selected[0].generation > 0, "GENERATION_FLOOR_REQUIRED");
  return selected[0].generation;
}

export function verifyIdleSnapshot(state) {
  requireValue(state.target === target && state.pods.length === 2 &&
    new Set(state.pods.map((pod) => pod.uid)).size === 2 &&
    state.pods.every((pod) => uuid.test(pod.uid)) && state.pods.some((pod) => pod.uid === state.leaderUID), "EXACT_LEADER_PAIR_REQUIRED");
  requireValue(state.database.activeRuntimeRuns === 0 && state.database.claimedRuntimeLeases === 0 &&
    state.activeJobs === 0, "IDLE_RUNTIME_REQUIRED");
}

export function describePodProcesses(actual) {
  const app = actual.status?.containerStatuses?.filter((item) => item.name === target) ?? [];
  requireValue(app.length === 1 && app[0].ready && Number.isSafeInteger(app[0].restartCount), "APPLICATION_READY_REQUIRED");
  return { uid: actual.metadata.uid, restartCount: app[0].restartCount, imageID: app[0].imageID,
    others: [...actual.status.containerStatuses, ...(actual.status.initContainerStatuses ?? [])]
      .filter((item) => item.name !== target).map((item) => ({ name: item.name, restartCount: item.restartCount, imageID: item.imageID }))
      .sort((a, b) => a.name.localeCompare(b.name)) };
}

export function verifyLeaderHandoff(proof, current, now = Date.now()) {
  requireValue(proof.version === 1 && proof.profile === "RUNTIME_LEADER_HANDOFF" && proof.status === "PASS" &&
    uuid.test(proof.id) && /^[a-f0-9]{40}$/.test(proof.toolSourceSHA), "HANDOFF_PROOF_REQUIRED");
  const { before, middle, after } = proof;
  for (const state of [before, middle, after, current]) verifyIdleSnapshot(state);
  requireValue(fingerprint(bindings(before)) === fingerprint(bindings(middle)) &&
    fingerprint(bindings(before)) === fingerprint(bindings(after)) &&
    fingerprint(bindings(after)) === fingerprint(bindings(current)), "HANDOFF_BINDING_CHANGED");
  requireValue(before.leaderUID !== middle.leaderUID && after.leaderUID === before.leaderUID, "A_B_A_HANDOFF_REQUIRED");
  requireValue(current.leaderUID === after.leaderUID &&
    fingerprint([...current.podDetails].sort((a, b) => a.uid.localeCompare(b.uid))) ===
      fingerprint([...after.podDetails].sort((a, b) => a.uid.localeCompare(b.uid))), "PROCESSES_CHANGED_AFTER_HANDOFF");
  const start = Date.parse(before.at), second = Date.parse(middle.at), finish = Date.parse(after.at);
  requireValue(start < second && second < finish && finish - start <= 600_000 &&
    finish <= now && now - finish <= 300_000, "FRESH_BOUNDED_HANDOFF_REQUIRED");
  const generation = floor(before);
  requireValue([middle, after, current].every((state) => floor(state) === generation), "GENERATION_CHANGED");
  for (const uid of before.pods.map((pod) => pod.uid)) {
    const row = rows(after, uid), actual = rows(current, uid);
    requireValue(row.generation === generation && actual.generation === generation &&
      actual.revision >= row.revision && Date.parse(row.expiresAt) > finish &&
      Date.parse(actual.expiresAt) > now, "LIVE_INSTANCE_REQUIRED");
    const a = before.podDetails.find((pod) => pod.uid === uid);
    const b = middle.podDetails.find((pod) => pod.uid === uid);
    const c = after.podDetails.find((pod) => pod.uid === uid);
    requireValue(a && b && c && b.restartCount === a.restartCount + (uid === before.leaderUID ? 1 : 0) &&
      c.restartCount === a.restartCount + 1 &&
      fingerprint(a.others) === fingerprint(b.others) && fingerprint(a.others) === fingerprint(c.others) &&
      a.imageID === b.imageID && a.imageID === c.imageID, "EXACT_APPLICATION_RESTARTS_REQUIRED");
  }
  requireValue(rows(after, before.leaderUID).revision > rows(middle, before.leaderUID).revision &&
    Date.parse(rows(middle, middle.leaderUID).updatedAt) > start &&
    Date.parse(rows(after, after.leaderUID).updatedAt) > second, "PROTECTED_RPC_AFTER_HANDOFF_REQUIRED");
}

async function main(args) {
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--evidence", "--output", "--confirm"].includes(key) && !Object.hasOwn(options, key) && args.length, "INVALID_ARGUMENTS");
    options[key] = args.shift();
  }
  requireValue(options["--context"] && options["--evidence"] && options["--output"] &&
    options["--confirm"] === "OBSERVE-IDLE-RUNTIME-LEADER-HANDOFF", "IDLE_HANDOFF_CONFIRMATION_REQUIRED");
  const root = fileURLToPath(new URL("../../", import.meta.url));
  const git = (...args) => execFileSync("git", ["-C", root, ...args], { encoding: "utf8", stdio: "pipe", timeout: 10_000 }).trim();
  const toolSourceSHA = git("rev-parse", "HEAD");
  requireValue(/^[a-f0-9]{40}$/.test(toolSourceSHA) && git("status", "--porcelain", "--untracked-files=all") === "", "EXACT_TOOL_SOURCE_REQUIRED");
  const context = options["--context"];
  const kubectl = (args) => execFileSync("kubectl", ["--context", context, "--namespace", "kodex-system", ...args], {
    encoding: "utf8", stdio: "pipe", timeout: 30_000, maxBuffer: 16 << 20,
  });
  const get = (kind, name) => JSON.parse(kubectl(["get", kind, name, "-o", "json"]));
  const directory = mkdtempSync(join(tmpdir(), "kodex-leader-handoff-"));
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const output = openSync(options["--output"], "wx", 0o600);
  const id = randomUUID();
  const record = (value) => { writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), id, ...value })}\n`); fsyncSync(fd); };
  const inspect = () => {
    const output = join(directory, `${randomUUID()}.json`);
    execFileSync(process.execPath, [join(root, "tools/release/worker-grant-transition.mjs"), "inspect",
      "--context", context, "--target", target, "--output", output], { stdio: "pipe", timeout: 120_000 });
    const state = JSON.parse(readFileSync(output, "utf8"));
    const deployment = get("deployment", target); requireInstanceWriters(deployment.spec.template);
    requireValue(deployment.metadata.uid === state.uid && fingerprint(deployment.spec) === state.specSHA256, "DEPLOYMENT_CHANGED");
    state.leaderUID = get("lease", "runtime-controller-leader").spec.holderIdentity;
    state.activeJobs = JSON.parse(kubectl(["--namespace", "kodex-runtime", "get", "jobs", "-o", "json"])).items
      .filter((job) => !job.status?.conditions?.some((item) => ["Complete", "Failed"].includes(item.type) && item.status === "True")).length;
    state.podDetails = state.pods.map((pod) => {
      const actual = get("pod", pod.name);
      requireValue(actual.metadata.uid === pod.uid, "POD_CHANGED");
      return describePodProcesses(actual);
    });
    verifyIdleSnapshot(state); return state;
  };
  const handoff = async (initial, expected) => {
    const leader = initial.pods.find((pod) => pod.uid === initial.leaderUID);
    const unchanged = inspect();
    requireValue(fingerprint(bindings(initial)) === fingerprint(bindings(unchanged)) && unchanged.leaderUID === initial.leaderUID, "HANDOFF_PRECONDITION_CHANGED");
    record({ status: "SIGNAL_INTENT", podUID: leader.uid, expectedLeaderUID: expected });
    // Только штатный supervisor процесса. Файлы, lease, grant и environment не меняются.
    // Downward API внутри exec защищает от пересоздания Pod с тем же именем.
    const signal = '[ "$POD_UID" = "$1" ] && [ "$(readlink /proc/1/exe)" = "/go/tools/air" ] && kill -TERM 1';
    try { kubectl(["exec", leader.name, "--container", target, "--", "sh", "-c", signal, "kodex-handoff", leader.uid]); }
    catch { record({ status: "SIGNAL_ACK_UNCERTAIN", podUID: leader.uid }); }
    const deadline = Date.now() + 240_000;
    while (Date.now() < deadline) {
      await new Promise((done) => setTimeout(done, 5000));
      try {
        const state = inspect();
        requireValue(fingerprint(bindings(initial)) === fingerprint(bindings(state)), "HANDOFF_BINDING_CHANGED");
        const detail = state.podDetails.find((pod) => pod.uid === leader.uid);
        const prior = initial.podDetails.find((pod) => pod.uid === leader.uid);
        requireValue(detail.restartCount <= prior.restartCount + 1, "UNEXPECTED_APPLICATION_RESTART");
        if (state.leaderUID === expected && detail.restartCount === prior.restartCount + 1 &&
          state.database.instances.some((row) => row.workload === target && row.instance === expected &&
            Date.parse(row.updatedAt) > Date.parse(initial.at))) {
          record({ status: "HANDOFF_CONFIRMED", snapshot: state }); return state;
        }
      } catch (error) {
        // Неизменность scope и отсутствие новой работы не являются retryable.
        if (/^[A-Z_]+$/.test(error.message)) throw error;
        // Восстановление после ожидаемой недоступности; повторного сигнала нет.
      }
    }
    throw new Error("HANDOFF_READBACK_TIMEOUT");
  };
  try {
    const before = inspect(); record({ status: "PREFLIGHT", toolSourceSHA, snapshot: before });
    const second = before.pods.find((pod) => pod.uid !== before.leaderUID).uid;
    const middle = await handoff(before, second);
    const after = await handoff(middle, before.leaderUID);
    const proof = { version: 1, profile: "RUNTIME_LEADER_HANDOFF", status: "PASS", id, toolSourceSHA, before, middle, after };
    verifyLeaderHandoff(proof, after);
    writeSync(output, `${JSON.stringify(proof)}\n`); fsyncSync(output);
    record({ status: "PASS", proofSHA256: fingerprint(proof) });
    process.stdout.write(`${JSON.stringify({ status: "PASS", id, profile: proof.profile })}\n`);
  } catch {
    record({ status: "FAIL", code: "HANDOFF_REQUIRES_AUTHORITATIVE_READBACK" });
    writeSync(output, `${JSON.stringify({ status: "FAIL", id })}\n`); fsyncSync(output);
    throw new Error("HANDOFF_FAILED");
  } finally { closeSync(output); closeSync(fd); rmSync(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch(() => { process.stderr.write("Runtime leader handoff failed; do not repeat the signal without authoritative readback\n"); process.exitCode = 1; });
}
