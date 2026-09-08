import { test } from "node:test";
import assert from "node:assert/strict";
import { verifyLeaderHandoff, describePodProcesses } from "./runtime-leader-handoff.mjs";

const a = "11111111-1111-4111-8111-111111111111", b = "22222222-2222-4222-8222-222222222222";
const start = Date.parse("2026-09-08T10:00:00Z");
const at = (seconds) => new Date(start + seconds * 1000).toISOString();
function fixture() {
  const state = (seconds, leaderUID, restarts) => ({
    at: at(seconds), clusterUID: a, namespaceUID: b, uid: a, target: "runtime-controller",
    specSHA256: "a".repeat(64), readers: { pods: [{ uid: a, revision: "a".repeat(40) }] },
    pods: [a, b].map((uid) => ({ uid, name: uid })), leaderUID, activeJobs: 0,
    podDetails: [a, b].map((uid, i) => ({ uid, restartCount: restarts[i], imageID: "sha256:app",
      others: [{ name: "native-grant-agent", restartCount: 0, imageID: "sha256:agent" }] })),
    database: { activeRuntimeRuns: 0, claimedRuntimeLeases: 0,
      floors: [{ workload: "runtime-controller", generation: 7 }],
      instances: [a, b].map((instance) => ({ workload: "runtime-controller", instance, generation: 7,
        revision: 100 + seconds, expiresAt: at(seconds + 240), updatedAt: at(seconds - 1) })) },
  });
  const proof = { version: 1, id: a, profile: "RUNTIME_LEADER_HANDOFF", status: "PASS", toolSourceSHA: "a".repeat(40),
    before: state(0, a, [0, 0]), middle: state(60, b, [1, 0]), after: state(120, a, [1, 1]) };
  // Standby не обязан иметь durable row до первого фактического RPC.
  proof.before.database.instances.pop();
  return proof;
}

test("idle A to B to A requires exact processes and real durable advancement", () => {
  const proof = fixture();
  verifyLeaderHandoff(proof, structuredClone(proof.after), start + 121000);
});

test("handoff rejects live work, changed boundary, revoked or expired grants and extra restarts", () => {
  for (const mutate of [
    (p) => { p.before.database.activeRuntimeRuns = 1; },
    (p) => { p.middle.database.claimedRuntimeLeases = 1; },
    (p) => { p.after.activeJobs = 1; },
    (p) => { p.after.leaderUID = b; },
    (p) => { p.middle.specSHA256 = "changed"; },
    (p) => { p.after.readers.pods[0].revision = "b".repeat(40); },
    (p) => { p.after.pods[0].uid = b; },
    (p) => { p.after.database.floors[0].generation++; },
    (p) => { p.after.database.instances[0].generation++; },
    (p) => { p.after.database.instances[1].expiresAt = at(100); },
    (p) => { p.after.database.instances[0].revision = p.middle.database.instances[0].revision; },
    (p) => { p.middle.database.instances[1].updatedAt = at(-1); },
    (p) => { p.after.podDetails[0].others[0].restartCount++; },
    (p) => { p.after.podDetails[1].restartCount++; },
    (p) => { p.middle.podDetails[1].restartCount++; },
    (p) => { p.after.podDetails[0].imageID = "different"; },
    (p) => { p.status = "FAIL"; },
  ]) { const p = fixture(); mutate(p); assert.throws(() => verifyLeaderHandoff(p, structuredClone(p.after), start + 121000)); }
});

test("proof cannot authorize changed or stale current state", () => {
  const p = fixture();
  for (const mutate of [
    (c) => { c.activeJobs++; },
    (c) => { c.database.claimedRuntimeLeases++; },
    (c) => { c.podDetails[0].restartCount++; },
    (c) => { c.leaderUID = b; },
    (c) => { c.database.instances[0].revision--; },
    (c) => { c.database.instances[1].expiresAt = at(120); },
  ]) { const c = structuredClone(p.after); mutate(c); assert.throws(() => verifyLeaderHandoff(p, c, start + 121000)); }
  assert.throws(() => verifyLeaderHandoff(p, p.after, start + 421000));
  assert.throws(() => verifyLeaderHandoff(p, p.after, start + 119000));
});

test("native sidecars remain part of protected process metadata", () => {
  const pod = { metadata: { uid: a }, status: { containerStatuses: [
    { name: "runtime-controller", ready: true, restartCount: 2, imageID: "app" },
  ], initContainerStatuses: [{ name: "grant", restartCount: 3, imageID: "agent" }] } };
  assert.deepEqual(describePodProcesses(pod).others, [{ name: "grant", restartCount: 3, imageID: "agent" }]);
  pod.status.containerStatuses[0].ready = false;
  assert.throws(() => describePodProcesses(pod));
});
