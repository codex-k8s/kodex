import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { releaseRegistry } from "./stable-registry-release.mjs";
import { prepareRegistry, registryRoles, httpStateName } from "./stable-registry-model.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { activeDeploymentPods } from "./stable-archive-transition.mjs";

const base = fileURLToPath(new URL("../../deploy/k8s/base/image-supply-chain/", import.meta.url));
const names = Object.keys(registryRoles), helperImage = "registry.local.kodex/kodex/registry-pull-authorizer@sha256:" + "b".repeat(64);
const templates = ["registry.yaml", "evidence-registry.yaml", "staging-read-registry.yaml"].flatMap(file =>
  execFileSync("yq", ["eval-all", "-N", "-o=json", "-I=0", 'select(.kind == "Deployment")', base + file], { encoding: "utf8" }).trim().split("\n").filter(Boolean).map(JSON.parse));

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), "kodex-registry-transition-fixture-")), resources = new Map(), trace = [], flags = {};
  const put = value => { const r = structuredClone(value); r.metadata.namespace = "kodex-system"; r.metadata.uid ??= r.kind + "-" + r.metadata.name;
    r.metadata.resourceVersion ??= "1"; r.metadata.generation ??= 1; resources.set(`${r.kind}/${r.metadata.name}`, r); return r; };
  for (const name of names) {
    const templateName = name;
    const d = put(templates.find(t => t.metadata.name === templateName)); d.metadata.name = name;
    resources.delete("Deployment/" + templateName); resources.set("Deployment/" + name, d); d.spec.replicas = 1;
    for (const c of d.spec.template.spec.containers) c.image = c.image.replace(/0{64}$/, "d".repeat(64));
    d.spec.template.spec.containers[0].env.push({ name: "PRIVATE_UNCHANGED", value: "REGISTRY_PRIVATE_SENTINEL" });
    d.status = { observedGeneration: 1, replicas: 1, updatedReplicas: 1, readyReplicas: 1, availableReplicas: 1 };
    const claim = registryRoles[name].claim;
    put({ kind: "PersistentVolumeClaim", metadata: { name: claim }, spec: { volumeName: "pv-" + claim, accessModes: ["ReadWriteOnce"] }, status: { phase: "Bound" } });
    put({ kind: "PersistentVolume", metadata: { name: "pv-" + claim }, spec: { nodeAffinity: { required: { nodeSelectorTerms: [{
      matchExpressions: [{ key: "kubernetes.io/hostname", operator: "In", values: ["node-fixture"] }] }] } } } });
  }
  put({ kind: "Deployment", metadata: { name: "control-plane" }, spec: { private: "DEPENDENT_PRIVATE_SENTINEL" } });
  const original = new Map(names.map(n => [n, structuredClone(resources.get("Deployment/" + n).spec)]));
  writeFileSync(join(directory, "bootstrap.json"), JSON.stringify({ schema: "kodex.stable-pull-bootstrap/v1", executorSHA: "f".repeat(40),
    context: "k3d-fixture", clusterUID: "system", appUID: "app", image: { image: helperImage, digest: "sha256:" + "b".repeat(64) } }), { mode: 0o600 });
  const types = { deployment: "Deployment", secret: "Secret", pvc: "PersistentVolumeClaim", pv: "PersistentVolume" };
  const port = async (program, argv, input) => {
    trace.push({ program, argv }); // stdin содержит новые Secret: его не журналируем.
    if (program === "git") return argv[0] === "rev-parse" ? "f".repeat(40) : "";
    if (program === process.execPath) {
      if (argv.includes("readback") && flags.notSeeded) throw Error("CACHE_NOT_READY");
      return JSON.stringify(argv.includes("readback") ? { status: "PASS", phase: "NODE_CACHE_ONLY" } : { status: "OBSERVED" });
    }
    if (program === "/bin/sh") argv = argv.slice(3);
    if (argv.includes("exec")) return JSON.stringify({ at: new Date().toISOString(), openBuilds: 0, pendingAdmissions: 0, pendingPromotions: 0,
      activeRuntimeRuns: flags.busy ? 1 : 0, claimedRuntimeLeases: 0, promotedPinsSHA256: (flags.pinsChanged ? "e" : "a").repeat(64), promotedArtifactCount: 85 });
    const i = argv.indexOf("get");
    if (i >= 0) {
      const kind = argv[i + 1], name = argv[i + 2];
      if (kind === "namespace") return JSON.stringify({ metadata: { uid: name === "kube-system" ? "system" : "app" } });
      if (kind === "deployments") {
        if (flags.statusOnlyRVDrift) for (const r of resources.values()) if (r.kind === "Deployment")
          r.metadata.resourceVersion = String(Number(r.metadata.resourceVersion) + 1);
        return JSON.stringify({ items: [...resources.values()].filter(r => r.kind === "Deployment") });
      }
      if (kind === "replicasets") return JSON.stringify({ items: names.map(name => ({ metadata: { uid: "rs-Deployment-" + name,
        ownerReferences: [{ kind: "Deployment", uid: "Deployment-" + name, controller: true }] } })) });
      if (kind === "pods" && flags.ownHistory) return JSON.stringify({ items: [{ metadata: { ownerReferences: [{ kind: "ReplicaSet",
        uid: "rs-Deployment-kodex-image-registry-push", controller: true }] }, spec: { containers: [{ name: "old-registry", env: [{ name: "STATE", valueFrom: {
        secretKeyRef: { name: httpStateName("kodex-image-registry-push"), key: "secret" } } }] }] }, status: { phase: "Failed",
        containerStatuses: [{ name: "old-registry", state: { terminated: { exitCode: 1 } } }] } }] });
      if (kind === "pods") return JSON.stringify({ items: flags.foreignConsumer ? [{ spec: { containers: [{ env: [{ name: "STATE", valueFrom: {
        secretKeyRef: { name: httpStateName("kodex-image-registry-push"), key: "secret" } } }] }] } }] : [] });
      if (kind === "services,networkpolicies") return JSON.stringify({ items: names.map(name => ({ kind: "Service", metadata: { name, uid: name }, spec: { selector: { name } } })) });
      if (kind === "replicasets,pods") {
        const selector = argv[argv.indexOf("-l") + 1], pairs = selector.split(",").map(s => s.split("="));
        const d = [...resources.values()].find(r => r.kind === "Deployment" && r.spec.selector && pairs.every(([k, v]) => r.spec.selector.matchLabels[k] === v));
        return JSON.stringify({ items: [{ kind: "ReplicaSet", metadata: { uid: "rs-" + d.metadata.uid, ownerReferences: [{ kind: "Deployment", uid: d.metadata.uid, controller: true }] } },
          ...Array.from({ length: d.spec.replicas }, (_, index) => ({ kind: "Pod", metadata: { uid: d.metadata.uid + "-pod-" + index,
            ownerReferences: [{ kind: "ReplicaSet", uid: "rs-" + d.metadata.uid, controller: true }] },
          spec: { ...d.spec.template.spec, nodeName: "node-fixture" }, status: { conditions: [{ type: "Ready", status: "True" }],
            containerStatuses: d.spec.template.spec.containers.map(c => ({ name: c.name, ready: true, imageID: c.image })) } }))] });
      }
      if (argv.some(a => a.startsWith("jsonpath="))) return `reference-${name}|${flags.referenceDrift ? "2" : "1"}`;
      const value = resources.get(types[kind] + "/" + name);
      if (!value && !argv.includes("--ignore-not-found")) throw Error("FIXTURE_NOT_FOUND");
      return value ? JSON.stringify(value) : "";
    }
    if (argv.includes("create")) {
      const value = put(JSON.parse(input));
      if (flags.loseCreate) throw Error("LOST_SECRET_RESPONSE");
      return JSON.stringify(value);
    }
    if (argv.includes("delete")) {
      const name = argv[argv.indexOf("--raw") + 1].split("/").at(-1), current = resources.get("Secret/" + name), body = JSON.parse(input);
      assert.equal(body.preconditions.uid, current.metadata.uid); assert.equal(body.preconditions.resourceVersion, current.metadata.resourceVersion);
      resources.delete("Secret/" + name); return "{}";
    }
    const p = argv.indexOf("patch");
    if (p >= 0) {
      const key = "Deployment/" + argv[p + 2], before = resources.get(key), result = structuredClone(before), patch = JSON.parse(input);
      for (const op of patch) {
        if (op.op === "test") {
          const value = op.path === "/spec" ? before.spec : op.path === "/metadata/uid" ? before.metadata.uid : before.metadata.resourceVersion;
          assert.equal(fingerprint(value), fingerprint(op.value));
        } else { assert.equal(op.path, "/spec"); result.spec = op.value; }
      }
      if (argv.includes("--dry-run=server")) return JSON.stringify(result);
      result.metadata.resourceVersion = String(Number(before.metadata.resourceVersion) + 1); result.metadata.generation++;
      result.status = { observedGeneration: result.metadata.generation, replicas: result.spec.replicas, readyReplicas: result.spec.replicas,
        updatedReplicas: result.spec.replicas, availableReplicas: result.spec.replicas }; resources.set(key, result);
      if (flags.loseActivate && result.spec.replicas === 2) throw Error("LOST_ACTIVATION_RESPONSE");
      return JSON.stringify(result);
    }
    if (argv.includes("rollout")) return "ready";
    throw Error("UNEXPECTED_FIXTURE_COMMAND");
  };
  const run = (mode, file, extra = []) => releaseRegistry([mode, "--context", "k3d-fixture", "--output", join(directory, file), ...extra], port);
  const plan = async () => { await run("plan", "plan.json", ["--bootstrap-plan", join(directory, "bootstrap.json")]); return JSON.parse(readFileSync(join(directory, "plan.json"), "utf8")); };
  const writes = () => trace.filter(r => (r.argv.includes("create") || r.argv.includes("delete") || r.argv.includes("patch")) && !r.argv.includes("--dry-run=server")).length;
  return { directory, resources, flags, trace, original, run, plan, writes };
}

test("five registry roles use exact PVC node and writer state without widening readonly boundaries", () => {
  const f = fixture();
  try {
    for (const name of names) {
      const source = f.resources.get("Deployment/" + name), original = structuredClone(source);
      const result = prepareRegistry(source, { claim: registryRoles[name].claim, bound: true, accessModes: ["ReadWriteOnce"], node: "node-fixture", commitment: "c".repeat(64) }, helperImage, "f".repeat(40));
      const after = result.patch.at(-1).value;
      assert.deepEqual(source, original); assert.equal(after.replicas, 2); assert.equal(after.template.spec.nodeSelector["kubernetes.io/hostname"], "node-fixture");
      assert.equal(after.template.spec.containers[0].env.some(e => e.name === "REGISTRY_HTTP_SECRET"), !registryRoles[name].readOnly);
      assert.equal(JSON.stringify(result.inverse).includes("REGISTRY_PRIVATE_SENTINEL"), false);
      assert.throws(() => prepareRegistry(source, { ...result.storage, accessModes: ["ReadWriteOncePod"] }, helperImage, "f".repeat(40)), /EXACT_STORAGE_REQUIRED/);
    }
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("registry apply creates private state then proves two pods and rollback restores five exact specs", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(), args = ["--plan", join(f.directory, "plan.json")], approval = ["--approved-plan-sha", fingerprint(plan)];
    assert.equal(JSON.stringify(plan).includes("PRIVATE_SENTINEL"), false);
    await f.run("apply", "apply.jsonl", [...args, ...approval]);
    assert.equal([...f.resources.values()].filter(r => r.kind === "Secret").length, 3);
    await f.run("readback", "readback.json", [...args, "--journal", join(f.directory, "apply.jsonl")]);
    f.flags.ownHistory = true;
    await f.run("rollback", "rollback.jsonl", [...args, ...approval, "--journal", join(f.directory, "apply.jsonl")]);
    for (const name of names) assert.deepEqual(f.resources.get("Deployment/" + name).spec, f.original.get(name));
    assert.equal([...f.resources.values()].filter(r => r.kind === "Secret").length, 0);
    assert.equal(readFileSync(join(f.directory, "apply.jsonl"), "utf8").includes("PRIVATE_SENTINEL"), false);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("status-only resource version changes between dependency reads do not replay a patch", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(); f.flags.statusOnlyRVDrift = true;
    await f.run("apply", "apply.jsonl", ["--plan", join(f.directory, "plan.json"), "--approved-plan-sha", fingerprint(plan)]);
    const patches = f.trace.filter(r => r.argv.includes("patch") && !r.argv.includes("--dry-run=server"));
    assert.equal(patches.length, names.length * 2);
    assert.ok(names.every(n => f.resources.get("Deployment/" + n).spec.replicas === 2));
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("unknown secret creation is resolved by exact intent without re-create", async () => {
  const f = fixture();
  try {
    const plan = await f.plan(), args = ["--plan", join(f.directory, "plan.json"), "--approved-plan-sha", fingerprint(plan)]; f.flags.loseCreate = true;
    await assert.rejects(f.run("apply", "unknown.jsonl", args), /LOST_SECRET_RESPONSE/);
    assert.equal([...f.resources.values()].filter(r => r.kind === "Secret").length, 1);
    const count = f.writes();
    await assert.rejects(f.run("apply", "retry.jsonl", args), /COLLISION/); assert.equal(f.writes(), count);
    await f.run("rollback", "rollback.jsonl", [...args, "--journal", join(f.directory, "unknown.jsonl")]);
    assert.equal([...f.resources.values()].filter(r => r.kind === "Secret").length, 0);
  } finally { rmSync(f.directory, { recursive: true, force: true }); }
});

test("unknown activation is inspected and restored; foreign state UID blocks all rollback effects", async () => {
  for (const foreign of [false, true]) {
    const f = fixture();
    try {
      const plan = await f.plan(), args = ["--plan", join(f.directory, "plan.json")], approval = ["--approved-plan-sha", fingerprint(plan)]; f.flags.loseActivate = true;
      await assert.rejects(f.run("apply", "unknown.jsonl", [...args, ...approval]), /LOST_ACTIVATION_RESPONSE/);
      await f.run("inspect", "inspect.json", [...args, "--journal", join(f.directory, "unknown.jsonl")]);
      if (foreign) f.resources.get("Secret/" + httpStateName("kodex-image-registry-push")).metadata.uid = "replacement";
      const count = f.writes();
      const rollback = f.run("rollback", "rollback.jsonl", [...args, ...approval, "--journal", join(f.directory, "unknown.jsonl")]);
      if (foreign) { await assert.rejects(rollback, /HTTP_STATE_DRIFT/); assert.equal(f.writes(), count); }
      else { await rollback; for (const name of names) assert.deepEqual(f.resources.get("Deployment/" + name).spec, f.original.get(name)); }
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("cache prerequisite busy owner or changed credential reference blocks before effects", async () => {
  for (const scenario of ["cache", "busy", "reference"]) {
    const f = fixture();
    try {
      const plan = await f.plan(); f.flags[scenario === "cache" ? "notSeeded" : scenario === "busy" ? "busy" : "referenceDrift"] = true;
      await assert.rejects(f.run("apply", "apply.jsonl", ["--plan", join(f.directory, "plan.json"), "--approved-plan-sha", fingerprint(plan)]));
      assert.equal(f.writes(), 0);
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("changed downstream deployment blocks rollback and a foreign consumer retains referenced state", async () => {
  for (const scenario of ["dependent", "consumer"]) {
    const f = fixture();
    try {
      const plan = await f.plan(), args = ["--plan", join(f.directory, "plan.json"), "--approved-plan-sha", fingerprint(plan)];
      await f.run("apply", "apply.jsonl", args);
      if (scenario === "dependent") f.resources.get("Deployment/control-plane").spec.changed = true;
      else f.flags.foreignConsumer = true;
      const count = f.writes(), rollback = f.run("rollback", "rollback.jsonl", [...args, "--journal", join(f.directory, "apply.jsonl")]);
      await assert.rejects(rollback, scenario === "dependent" ? /DEPENDENT_DEPLOYMENT_DRIFT/ : /STILL_REFERENCED/);
      if (scenario === "dependent") assert.equal(f.writes(), count);
      assert.ok(f.resources.has("Secret/" + httpStateName("kodex-image-registry-push")));
    } finally { rmSync(f.directory, { recursive: true, force: true }); }
  }
});

test("terminal history is excluded only with complete stopped container proof and owned lineage", () => {
  const rs = { kind: "ReplicaSet", metadata: { uid: "rs", ownerReferences: [{ kind: "Deployment", uid: "deployment", controller: true }] } };
  const historical = { kind: "Pod", metadata: { uid: "history", ownerReferences: [{ kind: "ReplicaSet", uid: "rs", controller: true }] },
    spec: { containers: [{ name: "registry" }], initContainers: [{ name: "init" }] }, status: { phase: "Failed",
      containerStatuses: [{ name: "registry", state: { terminated: { exitCode: 1 } } }],
      initContainerStatuses: [{ name: "init", state: { terminated: { exitCode: 0 } } }] } };
  const list = { items: [rs, historical] };
  assert.equal(activeDeploymentPods(list, "deployment").length, 0);
  for (const mutate of [p => { p.status.containerStatuses = []; }, p => { p.status.initContainerStatuses[0].state = { running: {} }; },
    p => { p.status.phase = "Unknown"; }, p => { p.spec.ephemeralContainers = [{ name: "debug" }]; }]) {
    const p = structuredClone(historical); mutate(p);
    assert.equal(activeDeploymentPods({ items: [rs, p] }, "deployment").length, 1);
  }
  const foreign = structuredClone(historical); foreign.metadata.ownerReferences[0].uid = "foreign-rs";
  assert.throws(() => activeDeploymentPods({ items: [rs, foreign] }, "deployment"), /FOREIGN_SERVING_POD/);
});
