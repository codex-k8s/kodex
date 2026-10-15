import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { releaseAdmission } from "./stable-admission-release.mjs";
import { admissionName as name, admissionLease, admissionRole, prepareAdmission } from "./stable-admission-model.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";

const rows = execFileSync("yq", ["eval-all", "-N", "-o=json", "-I=0", ".",
  fileURLToPath(new URL("../../deploy/k8s/base/image-supply-chain/image-admission-controller.yaml", import.meta.url))],
{ encoding: "utf8" }).trim().split("\n").map(JSON.parse);
const image = { component: name, sourceSHA: "a".repeat(40), digest: "sha256:" + "b".repeat(64), tag: "stable-" + "a".repeat(40) + "-" + "b".repeat(12) };

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), "kodex-stable-admission-fixture-")), resources = new Map(), trace = [], flags = {};
  const put = raw => {
    const r = structuredClone(raw); r.metadata.namespace = "kodex-system"; r.metadata.uid ??= r.kind + "-" + r.metadata.name;
    r.metadata.resourceVersion ??= "1"; r.metadata.generation ??= 1; resources.set(r.kind + "/" + r.metadata.name, r); return r;
  };
  const d = put(rows.find(r => r.kind === "Deployment"));
  d.metadata.labels["kodex.dev/local-profile"] = "hot-reload"; d.spec.template.metadata.labels["kodex.dev/local-profile"] = "hot-reload";
  d.spec.replicas = 1; d.spec.template.spec.containers[0].image = "registry.local.kodex/kodex/image-admission@sha256:" + "d".repeat(64);
  d.spec.template.spec.containers[0].env = d.spec.template.spec.containers[0].env.filter(e => e.name !== "POD_UID");
  d.spec.template.spec.containers[0].env.push({ name: "PRIVATE_UNCHANGED", value: "ADMISSION_PRIVATE_SENTINEL" },
    { name: "KODEX_RPC_PROFILE", value: "trusted-cluster" });
  d.status = { observedGeneration: 1, replicas: 1, updatedReplicas: 1, readyReplicas: 1, availableReplicas: 1 };
  const original = structuredClone(d);
  const role = put(rows.find(r => r.kind === "Role")); role.rules = role.rules.filter(r => !r.apiGroups.includes("coordination.k8s.io"));
  const beforeRules = structuredClone(role.rules);
  const binding = put(rows.find(r => r.kind === "RoleBinding")); binding.subjects[0].namespace = "kodex-system";
  writeFileSync(join(directory, "publication.json"), JSON.stringify({ schema: "kodex.stable-image-publication/v2", artifactProfile: "built", source: "f".repeat(40),
    context: "k3d-fixture", clusterUID: "cluster", namespaceUID: "app", image, receipt: { schema: "kodex.stable-local-build/v1",
      component: name, imageDigest: image.digest, sourceSHA: image.sourceSHA, contextSHA256: "c".repeat(64), archiveSHA256: "d".repeat(64),
      admission: "NOT_RUN", publication: "NOT_RUN", nodePull: "NOT_RUN" } }), { mode: 0o600 });
  const kinds = { deployment: "Deployment", role: "Role", rolebinding: "RoleBinding", lease: "Lease", poddisruptionbudget: "PodDisruptionBudget" };
  const port = async (program, argv, input) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? "f".repeat(40) : "";
    if (program === "yq") return rows.map(r => JSON.stringify(r)).join("\n");
    if (program === process.execPath) {
      if (flags.publicationFailed) throw Error("PUBLICATION_FIXTURE_FAILED");
      return JSON.stringify({ status: "PASS", phase: "REGISTRY_PUBLICATION_ONLY", digest: image.digest });
    }
    if (argv.includes("exec")) return JSON.stringify({ at: new Date().toISOString(), openBuilds: flags.busy ? 1 : 0,
      activeRuntimeRuns: 0, pendingAdmissions: 0, pendingPromotions: 0, claimedRuntimeLeases: 0,
      promotedArtifactCount: 3, promotedPinsSHA256: (flags.pinsChanged ? "c" : "d").repeat(64) });
    const g = argv.indexOf("get");
    if (g >= 0) {
      const kind = argv[g + 1], target = argv[g + 2];
      if (kind === "namespace") return JSON.stringify({ metadata: { uid: target === "kube-system" ? "cluster" : "app" } });
      if (kind === "jobs,pods,persistentvolumeclaims") return JSON.stringify({ items: flags.workBusy ? [{ kind: "Job" }] : [] });
      if (kind === "services,networkpolicies") return JSON.stringify({ items: [] });
      if (argv.some(a => a.startsWith("jsonpath="))) return `reference-${fingerprint(target).slice(0, 8)}|${flags.referenceDrift ? 2 : 1}`;
      if (kind === "replicasets,pods") {
        const live = resources.get("Deployment/" + name), compiled = live.spec.template.spec.containers[0].image.endsWith(image.digest);
        return JSON.stringify({ items: [{ kind: "ReplicaSet", metadata: { uid: "rs", ownerReferences: [{ kind: "Deployment", uid: live.metadata.uid, controller: true }] } },
          ...Array.from({ length: live.spec.replicas }, (_, i) => ({ kind: "Pod", metadata: { uid: compiled ?
            i ? "22222222-2222-4222-8222-222222222222" : "11111111-1111-4111-8111-111111111111" : `old-pod-${i}`,
            ownerReferences: [{ kind: "ReplicaSet", uid: "rs", controller: true }] }, spec: live.spec.template.spec,
          status: { conditions: [{ type: "Ready", status: "True" }], containerStatuses: [{ name, ready: true, imageID: live.spec.template.spec.containers[0].image }] } }))] });
      }
      const r = resources.get((kinds[kind] ?? kind) + "/" + target);
      if (kind === "Lease" && flags.disruptServing && ++flags.leaseReads === 2)
        resources.get("Deployment/" + name).status.readyReplicas = 1;
      if (!r && !argv.includes("--ignore-not-found")) throw Error("FIXTURE_RESOURCE_NOT_FOUND");
      return r ? JSON.stringify(r) : "";
    }
    if (argv.includes("create")) {
      const r = JSON.parse(input);
      if (argv.includes("--dry-run=server")) return JSON.stringify(r);
      const created = put(r);
      if (flags.loseCreate && (!flags.loseCreateKind || flags.loseCreateKind === created.kind)) throw Error("CREATE_RESPONSE_UNKNOWN");
      return JSON.stringify(created);
    }
    const p = argv.indexOf("patch");
    if (p >= 0) {
      const kind = kinds[argv[p + 1]] ?? argv[p + 1], key = kind + "/" + argv[p + 2], r = resources.get(key), next = structuredClone(r);
      for (const op of JSON.parse(input)) {
        const parts = op.path.slice(1).split("/"); let parent = next;
        for (const part of parts.slice(0, -1)) parent = parent[part];
        const last = parts.at(-1);
        if (op.op === "test") assert.equal(fingerprint(parent[last]), fingerprint(op.value));
        else { assert.equal(op.op, "replace"); parent[last] = op.value; }
      }
      if (argv.includes("--dry-run=server")) return JSON.stringify(next);
      next.metadata.resourceVersion = String(Number(r.metadata.resourceVersion) + 1); next.metadata.generation++;
      if (kind === "Deployment") {
        const count = next.spec.replicas;
        next.status = { observedGeneration: next.metadata.generation, replicas: count, updatedReplicas: count, readyReplicas: count, availableReplicas: count };
        if (count === 2) {
          const lease = resources.get("Lease/" + admissionLease);
          lease.spec = { holderIdentity: "11111111-1111-4111-8111-111111111111", leaseDurationSeconds: 45, leaseTransitions: (lease.spec.leaseTransitions ?? 0) + 1,
            acquireTime: new Date().toISOString(), renewTime: new Date().toISOString() };
        }
      }
      resources.set(key, next);
      if (flags.loseActivate && kind === "Deployment" && next.spec.replicas === 2 || flags.loseRole && kind === "Role" ||
        flags.loseRestore && kind === "Deployment" && next.spec.replicas === 1) throw Error("PATCH_RESPONSE_UNKNOWN");
      return JSON.stringify(next);
    }
    if (argv.includes("rollout")) return "complete";
    throw Error("FIXTURE_UNEXPECTED_OPERATION");
  };
  const run = (mode, output, options = []) => releaseAdmission([mode, "--context", "k3d-fixture", "--output", join(directory, output), ...options], port);
  return { directory, resources, trace, flags, original, beforeRules, run,
    async plan() { await run("plan", "plan.json", ["--publication-plan", join(directory, "publication.json")]); return JSON.parse(readFileSync(join(directory, "plan.json"), "utf8")); } };
}
const writes = f => f.trace.filter(t => (t.argv.includes("create") || t.argv.includes("patch")) && !t.argv.includes("--dry-run=server"));
const args = f => ["--plan", join(f.directory, "plan.json")];
const approval = plan => ["--approved-plan-sha", fingerprint(plan)];
const journal = f => ["--journal", join(f.directory, "apply.jsonl")];

test("admission model preserves source/private fields and grants only one fixed Lease", () => {
  const f = fixture();
  try {
    const d = f.resources.get("Deployment/" + name), before = structuredClone(d);
    const result = prepareAdmission(d, rows.find(r => r.kind === "Deployment"), image);
    const after = result.patch.find(p => p.path === "/spec" && p.op === "replace").value;
    assert.deepEqual(d, before); assert.equal(after.replicas, 2);
    assert.deepEqual(applyReverseSpec(after, result.inverse), before.spec);
    assert.equal(JSON.stringify(result.inverse).includes("PRIVATE_SENTINEL"), false);
    const rule = rows.find(r => r.kind === "Role").rules[0], r = f.resources.get("Role/" + name), b = f.resources.get("RoleBinding/" + name);
    assert.equal(admissionRole(r, b, rule).afterRulesSHA256, fingerprint([...r.rules, rule]));
    assert.throws(() => admissionRole(r, b, { ...rule, verbs: ["*"] }), /LEASE_RULE_INVALID/);
    const changed = structuredClone(d); changed.spec.template.spec.containers[0].env.push({ name: "POD_UID", value: "forged" });
    assert.throws(() => prepareAdmission(changed, rows.find(r => r.kind === "Deployment"), image), /SOURCE_PROFILE_INVALID/);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("apply drains old binary, grants exact lock, proves two pods/current leader and restores without rewinding Lease", async () => {
  const f = fixture();
  try {
    const plan = await f.plan();
    assert.equal(JSON.stringify(plan).includes("PRIVATE_SENTINEL"), false);
    await f.run("apply", "apply.jsonl", [...args(f), ...approval(plan)]);
    await f.run("readback", "readback.json", [...args(f), ...journal(f)]);
    const state = structuredClone(f.resources.get("Lease/" + admissionLease));
    const intent = writes(f); assert.equal(intent[0].argv[intent[0].argv.indexOf("patch") + 1], "Deployment");
    await f.run("rollback", "rollback.jsonl", [...args(f), ...approval(plan), ...journal(f)]);
    assert.deepEqual(f.resources.get("Deployment/" + name).spec, f.original.spec);
    assert.deepEqual(f.resources.get("Role/" + name).rules, f.beforeRules);
    assert.deepEqual(f.resources.get("Lease/" + admissionLease), state);
    assert.equal(readFileSync(join(f.directory, "apply.jsonl"), "utf8").includes("PRIVATE_SENTINEL"), false);
    const count = writes(f).length;
    await f.run("rollback", "already.json", [...args(f), ...approval(plan), ...journal(f)]);
    assert.equal(writes(f).length, count);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("unknown create, role patch and activation responses never retry and can restore source", async () => {
  for (const flag of ["loseCreate", "loseCreatePDB", "loseRole", "loseActivate"]) {
    const f = fixture();
    try {
      const plan = await f.plan(); f.flags[flag] = true;
      if (flag === "loseCreatePDB") { f.flags.loseCreate = true; f.flags.loseCreateKind = "PodDisruptionBudget"; }
      await assert.rejects(f.run("apply", "apply.jsonl", [...args(f), ...approval(plan)]), /UNKNOWN/);
      const count = writes(f).length;
      await assert.rejects(f.run("apply", "retry.jsonl", [...args(f), ...approval(plan)]));
      assert.equal(writes(f).length, count);
      f.flags[flag] = false; f.flags.loseCreate = false;
      await f.run("inspect", "inspect.json", [...args(f), ...journal(f)]);
      await f.run("rollback", "rollback.jsonl", [...args(f), ...approval(plan), ...journal(f)]);
      assert.deepEqual(f.resources.get("Deployment/" + name).spec, f.original.spec);
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("wrong approval, owner/Job activity and credential drift block all mutations", async () => {
  for (const flag of ["approval", "busy", "workBusy", "referenceDrift", "publicationFailed"]) {
    const f = fixture();
    try {
      const plan = await f.plan(); f.flags[flag] = true;
      await assert.rejects(f.run("apply", "apply.jsonl", [...args(f), ...approval(flag === "approval" ? {} : plan)]));
      assert.equal(writes(f).length, 0, flag);
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("replacement support UID and changed binding block rollback before any effect", async () => {
  for (const kind of ["Lease", "PodDisruptionBudget", "RoleBinding"]) {
    const f = fixture();
    try {
      const plan = await f.plan(); await f.run("apply", "apply.jsonl", [...args(f), ...approval(plan)]);
      f.resources.get(kind + "/" + (kind === "Lease" ? admissionLease : name)).metadata.uid = "foreign";
      const count = writes(f).length;
      await assert.rejects(f.run("rollback", "rollback.jsonl", [...args(f), ...approval(plan), ...journal(f)]), /DRIFT/);
      assert.equal(writes(f).length, count);
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("lost restore response is resolved by readback without re-patching source", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(); await f.run("apply", "apply.jsonl", [...args(f), ...approval(plan)]);
    f.flags.loseRestore = true;
    await assert.rejects(f.run("rollback", "rollback.jsonl", [...args(f), ...approval(plan), ...journal(f)]), /UNKNOWN/);
    f.flags.loseRestore = false; const count = writes(f).length;
    await f.run("rollback", "resolved.json", [...args(f), ...approval(plan), ...journal(f)]);
    assert.equal(writes(f).length, count);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("two Ready pods without their current lease holder do not pass acceptance", async t => {
  const f = fixture();
  try {
    const plan = await f.plan(); await f.run("apply", "apply.jsonl", [...args(f), ...approval(plan)]);
    f.resources.get("Lease/" + admissionLease).spec.holderIdentity = "33333333-3333-4333-8333-333333333333";
    const base = Date.now(); let step = 0;
    t.mock.method(Date, "now", () => base + step++ * 60001);
    await assert.rejects(f.run("readback", "readback.json", [...args(f), ...journal(f)]), /CURRENT_LEADER_UNPROVEN/);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("fresh forward plan reuses retained Lease UID instead of recreating or clearing its transitions", async () => {
  const f = fixture();
  try {
    const first = await f.plan(); await f.run("apply", "apply.jsonl", [...args(f), ...approval(first)]);
    await f.run("rollback", "rollback.jsonl", [...args(f), ...approval(first), ...journal(f)]);
    const lease = structuredClone(f.resources.get("Lease/" + admissionLease));
    const creates = writes(f).filter(t => t.argv.includes("create")).length;
    await f.run("plan", "forward-plan.json", ["--publication-plan", join(f.directory, "publication.json")]);
    const forward = JSON.parse(readFileSync(join(f.directory, "forward-plan.json"), "utf8"));
    assert.equal(forward.support.find(r => r.kind === "Lease").uid, lease.metadata.uid);
    await f.run("apply", "forward-apply.jsonl", ["--plan", join(f.directory, "forward-plan.json"), ...approval(forward)]);
    assert.equal(writes(f).filter(t => t.argv.includes("create")).length, creates);
    assert.equal(f.resources.get("Lease/" + admissionLease).metadata.uid, lease.metadata.uid);
    assert.equal(f.resources.get("Lease/" + admissionLease).spec.leaseTransitions, lease.spec.leaseTransitions + 1);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("leader proof rechecks currently serving pods instead of accepting an earlier ready snapshot", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(); await f.run("apply", "apply.jsonl", [...args(f), ...approval(plan)]);
    f.flags.disruptServing = true; f.flags.leaseReads = 0;
    await assert.rejects(f.run("readback", "readback.json", [...args(f), ...journal(f)]), /SERVING_INCOMPLETE/);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});
