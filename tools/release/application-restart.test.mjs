import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, linkSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fingerprint, planTarget } from "./scoped-release.mjs";
import { observeApplicationRestart, planApplicationRestart, prepareApplicationRestart, restartAnnotation } from "./application-restart-model.mjs";
import { applicationRestartRuntime, restartCLI, restartStep } from "./application-restart.mjs";

const uid = value => `10000000-0000-4000-8000-${String(value).padStart(12, "0")}`;
const image = `registry.local.kodex/kodex/control-plane@sha256:${"a".repeat(64)}`;
const executorSHA = "e".repeat(40);
const context = "k3d-kodex", accessProfile = "kubectl";

// Реалистичный stable Deployment содержит приватный sentinel: никакой план/patch/receipt не должен скопировать его.
function fixture() {
  return { apiVersion: "apps/v1", kind: "Deployment",
    metadata: { name: "control-plane", namespace: "kodex-system", uid: uid(1), resourceVersion: "101", generation: 5,
      labels: { "app.kubernetes.io/part-of": "kodex", "kodex.dev/environment": "staging" } },
    spec: { replicas: 2, strategy: { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } },
      template: { metadata: { labels: { "app.kubernetes.io/name": "control-plane", "app.kubernetes.io/component": "control-plane", "kodex.dev/local-profile": "stable" },
        annotations: { "kubectl.kubernetes.io/default-container": "control-plane", "kodex.dev/source-sha": "b".repeat(40),
          "kodex.dev/release-provenance-sha256": "c".repeat(64), "kodex.dev/application-release": uid(2), "kodex.dev/worker-grant-format": "2" } },
      spec: { containers: [{ name: "control-plane", command: ["/usr/local/bin/control-plane"], image,
        env: [{ name: "PRIVATE_INPUT", value: "PRIVATE_ENV_SENTINEL" }] },
      { name: "platform-worker-grant-agent", image: `registry.local/kodex/grant@sha256:${"d".repeat(64)}`,
        env: [{ name: "PLATFORM_WORKER_GRANT_INSTANCE_ID", valueFrom: { fieldRef: { fieldPath: "metadata.uid" } } }] }],
      volumes: [{ name: "credentials", secret: { secretName: "PRIVATE_REFERENCE_SENTINEL" } }] } } },
    status: { observedGeneration: 5, replicas: 2, readyReplicas: 2, availableReplicas: 2, updatedReplicas: 2 } };
}

// Lineage использует настоящие Deployment→RS→Pod refs и controller hash label; старые Pods не удаляются из proof молча.
function resources(deployment, replica = 10, pods = [11, 12]) {
  const template = structuredClone(deployment.spec.template);
  template.metadata.labels["pod-template-hash"] = `hash-${replica}`;
  return { items: [{ kind: "ReplicaSet", metadata: { uid: uid(replica), namespace: "kodex-system", ownerReferences: [{ uid: deployment.metadata.uid, kind: "Deployment", controller: true }] },
    spec: { template } }, ...pods.map(value => ({ kind: "Pod", metadata: { ...structuredClone(template.metadata), uid: uid(value), namespace: "kodex-system",
      ownerReferences: [{ uid: uid(replica), kind: "ReplicaSet", controller: true }] }, spec: structuredClone(template.spec),
    status: { phase: "Running", conditions: [{ type: "Ready", status: "True" }],
      containerStatuses: template.spec.containers.map(container => ({ name: container.name, ready: true, imageID: container.image, state: { running: {} } })) } }))] };
}

function makePlan(deployment = fixture(), list = resources(deployment)) {
  return planApplicationRestart({ context, accessProfile, clusterUID: uid(3), namespaceUID: uid(4), executorSHA,
    restartID: uid(5), name: "control-plane", deployment, resources: list });
}

// Применяется только разрешённый annotation CAS; тестовый transport не вызывает kubectl/cluster и считает каждую mutation.
function fakeRuntime() {
  let deployment = fixture(), list = resources(deployment);
  const rt = { context, accessProfile, executorSHA, patches: 0, waits: 0, snapshots: 0, loseACK: false, failWait: false,
    scope: { clusterUID: uid(3), namespaceUID: uid(4) },
    identities() { return { ...this.scope }; },
    snapshot() { this.snapshots++; return structuredClone({ deployment, resources: list }); },
    patch(name, patch) {
      this.patches++;
      assert.equal(name, "control-plane");
      assert.deepEqual(patch.slice(0, 2), [{ op: "test", path: "/metadata/uid", value: deployment.metadata.uid },
        { op: "test", path: "/metadata/resourceVersion", value: deployment.metadata.resourceVersion }]);
      assert.equal(patch.length, 3);
      assert.equal(patch[2].path, "/spec/template/metadata/annotations/kodex.dev~1application-restart");
      assert.equal(JSON.stringify(patch).includes("PRIVATE_"), false);
      deployment.spec.template.metadata.annotations[restartAnnotation] = patch[2].value;
      deployment.metadata.generation++;
      deployment.metadata.resourceVersion = "102";
      deployment.status.observedGeneration++;
      list = resources(deployment, 20, [21, 22]);
      if (this.loseACK) throw Error("KUBERNETES_OPERATION_FAILED_OR_UNKNOWN");
    },
    wait() { this.waits++; if (this.failWait) throw Error("KUBERNETES_OPERATION_FAILED_OR_UNKNOWN"); },
    mutate(change) { change(deployment, list); },
  };
  return rt;
}

function privateDirectory(t) {
  const directory = mkdtempSync(join(tmpdir(), "kodex-application-restart-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}

test("same-source restart changes exactly its own annotation and leaves release guard intact", () => {
  const deployment = fixture(), before = structuredClone(deployment);
  const plan = makePlan(deployment), patch = prepareApplicationRestart(plan, deployment, resources(deployment));
  assert.equal(patch.length, 3);
  assert.equal(patch[2].path, "/spec/template/metadata/annotations/kodex.dev~1application-restart");
  assert.equal(plan.target.image, image);
  assert.equal(plan.target.releaseID, deployment.spec.template.metadata.annotations["kodex.dev/application-release"]);
  assert.equal(JSON.stringify(plan).includes("PRIVATE_"), false);
  assert.deepEqual(deployment, before);
  assert.throws(() => planTarget(deployment, { name: "control-plane", image }, uid(50)), /APPLICATION_IMAGE_UNCHANGED/);
});

for (const [name, mutate] of Object.entries({
  image: d => { d.spec.template.spec.containers[0].image = image.replace("a".repeat(64), "f".repeat(64)); },
  source: d => { d.spec.template.metadata.annotations["kodex.dev/source-sha"] = "f".repeat(40); },
  provenance: d => { d.spec.template.metadata.annotations["kodex.dev/release-provenance-sha256"] = "f".repeat(64); },
  release: d => { d.spec.template.metadata.annotations["kodex.dev/application-release"] = uid(99); },
  spec: d => { d.spec.template.spec.containers[0].env[0].value = "CHANGED"; },
  uid: d => { d.metadata.uid = uid(99); },
  rv: d => { d.metadata.resourceVersion = "999"; },
  namespace: d => { d.metadata.namespace = "foreign"; },
  hotreload: d => { d.spec.template.spec.containers[0].command = ["/workspace/tools/dev/run-go-hot-reload.sh"]; },
  hostPath: d => { d.spec.template.spec.volumes.push({ name: "host", hostPath: { path: "/private" } }); },
  rollout: d => { d.status.updatedReplicas = 1; },
  strategy: d => { d.spec.strategy.rollingUpdate.maxUnavailable = 1; },
  writer: d => { d.spec.template.spec.containers[1].env[0].valueFrom.fieldRef.fieldPath = "metadata.name"; },
})) test(`stale or unsafe ${name} rejects before patch`, t => {
  const directory = privateDirectory(t), rt = fakeRuntime(), plan = makePlan();
  rt.mutate(mutate);
  assert.throws(() => restartStep("apply", plan, fingerprint(plan), join(directory, "journal"), rt));
  assert.equal(rt.patches, 0);
});

for (const key of ["clusterUID", "namespaceUID"]) test(`foreign ${key} rejects before journal or mutation`, t => {
  const directory = privateDirectory(t), rt = fakeRuntime(), plan = makePlan();
  rt.scope[key] = uid(99);
  assert.throws(() => restartStep("apply", plan, fingerprint(plan), join(directory, "journal"), rt), /SCOPE_CHANGED/);
  assert.equal(rt.patches, 0);
});

test("one intent/CAS produces new exact Pods, repeat apply rejects and readback stays read-only", t => {
  const directory = privateDirectory(t), path = join(directory, "journal"), rt = fakeRuntime(), plan = makePlan();
  const result = restartStep("apply", plan, fingerprint(plan), path, rt);
  assert.equal(result.status, "PASS");
  assert.deepEqual(result.podUIDs, [uid(21), uid(22)]);
  const journal = readFileSync(path, "utf8");
  assert.equal(journal.includes("PRIVATE_"), false);
  assert.equal(journal.trimEnd().split("\n").filter(line => JSON.parse(line).type === "INTENT").length, 1);
  assert.throws(() => restartStep("apply", plan, fingerprint(plan), path, rt), /JOURNAL_ALREADY_EXISTS/);
  assert.equal(restartStep("readback", plan, fingerprint(plan), path, rt).status, "PASS");
  assert.equal(rt.patches, 1);
  assert.equal(readFileSync(path, "utf8"), journal);
});

for (const fault of ["loseACK", "failWait"]) test(`${fault} preserves intent and can only rejoin by readback`, t => {
  const directory = privateDirectory(t), path = join(directory, "journal"), rt = fakeRuntime(), plan = makePlan();
  rt[fault] = true;
  assert.equal(restartStep("apply", plan, fingerprint(plan), path, rt).status, "UNKNOWN");
  assert.throws(() => restartStep("apply", plan, fingerprint(plan), path, rt), /JOURNAL_ALREADY_EXISTS/);
  const bytes = readFileSync(path, "utf8");
  assert.equal(restartStep("readback", plan, fingerprint(plan), path, rt).status, "PASS");
  assert.equal(readFileSync(path, "utf8"), bytes);
  assert.equal(rt.patches, 1);
});

test("unknown without a current restart remains inconclusive and never replays mutation", t => {
  const directory = privateDirectory(t), path = join(directory, "journal"), rt = fakeRuntime(), plan = makePlan();
  rt.patch = function () { this.patches++; throw Error("transport result unavailable"); };
  assert.equal(restartStep("apply", plan, fingerprint(plan), path, rt).status, "UNKNOWN");
  const bytes = readFileSync(path, "utf8");
  assert.equal(restartStep("readback", plan, fingerprint(plan), path, rt).status, "NOT_CURRENT");
  assert.equal(readFileSync(path, "utf8"), bytes);
  assert.throws(() => restartStep("apply", plan, fingerprint(plan), path, rt), /JOURNAL_ALREADY_EXISTS/);
  assert.equal(rt.patches, 1);
});

test("failed lineage never yields PASS or another patch", t => {
  const directory = privateDirectory(t), path = join(directory, "journal"), rt = fakeRuntime(), plan = makePlan();
  rt.failWait = true;
  restartStep("apply", plan, fingerprint(plan), path, rt);
  for (const mutate of [
    (_d, list) => { list.items[1].metadata.ownerReferences[0].uid = uid(99); },
    (_d, list) => { list.items[1].status.containerStatuses[0].imageID = image.replace("a".repeat(64), "f".repeat(64)); },
    (_d, list) => { list.items[1].metadata.annotations["kodex.dev/source-sha"] = "f".repeat(40); },
    (_d, list) => { list.items[1].metadata.annotations[restartAnnotation] = uid(99); },
  ]) {
    const current = rt.snapshot();
    mutate(current.deployment, current.resources);
    assert.throws(() => observeApplicationRestart(plan, current.deployment, current.resources));
  }
  rt.mutate((_d, list) => { list.items[1].status.phase = "Pending"; });
  assert.equal(restartStep("readback", plan, fingerprint(plan), path, rt).status, "UNKNOWN");
  assert.equal(rt.patches, 1);
});

test("only proven terminal old Pods are ignored", t => {
  const rt = fakeRuntime(), plan = makePlan();
  const before = rt.snapshot();
  rt.patch("control-plane", prepareApplicationRestart(plan, before.deployment, before.resources));
  const after = rt.snapshot();
  const old = before.resources.items[1];
  old.status.phase = "Succeeded";
  old.status.containerStatuses.forEach(state => { state.state = { terminated: { exitCode: 0 } }; });
  after.resources.items.push(before.resources.items[0], old);
  assert.equal(observeApplicationRestart(plan, after.deployment, after.resources).status, "PASS");
  old.status.containerStatuses[1].state = { running: {} };
  assert.throws(() => observeApplicationRestart(plan, after.deployment, after.resources));
});

test("CLI plan/apply/readback use real private files and the same one-effect driver", async t => {
  const directory = privateDirectory(t), planPath = join(directory, "plan.json"), journal = join(directory, "journal"), rt = fakeRuntime();
  const factory = () => rt;
  const planned = await restartCLI(["plan", "--context", context, "--target", "control-plane", "--output", planPath], factory);
  assert.equal(planned.status, "PLANNED");
  assert.equal(statSync(planPath).mode & 0o777, 0o600);
  assert.equal(readFileSync(planPath, "utf8").includes("PRIVATE_"), false);
  const common = ["--context", context, "--plan", planPath, "--approved-plan-sha", planned.planSHA256, "--evidence", journal];
  const applied = await restartCLI(["apply", ...common, "--output", join(directory, "apply.json"), "--confirm", "RESTART-STAGING-APPLICATION"], factory);
  assert.equal(applied.status, "PASS");
  const read = await restartCLI(["readback", ...common, "--output", join(directory, "read.json")], factory);
  assert.equal(read.status, "PASS");
  assert.equal(rt.patches, 1);
});

test("wrong hash, source and private-path substitution fail before effect", async t => {
  const directory = privateDirectory(t), rt = fakeRuntime(), plan = makePlan(), planPath = join(directory, "plan.json");
  assert.throws(() => restartStep("apply", plan, "0".repeat(64), join(directory, "journal"), rt), /PLAN_MISMATCH/);
  rt.executorSHA = "f".repeat(40);
  assert.throws(() => restartStep("apply", plan, fingerprint(plan), join(directory, "journal"), rt), /PLAN_MISMATCH/);
  rt.executorSHA = executorSHA;
  writeFileSync(planPath, JSON.stringify(plan), { mode: 0o600 });
  const common = ["apply", "--context", context, "--plan", planPath, "--approved-plan-sha", fingerprint(plan),
    "--evidence", join(directory, "journal"), "--confirm", "RESTART-STAGING-APPLICATION"];
  const link = join(directory, "link"); symlinkSync(planPath, link);
  await assert.rejects(() => restartCLI([...common, "--output", link], () => rt), /PRIVATE_OUTPUT_REQUIRED/);
  chmodSync(planPath, 0o640);
  await assert.rejects(() => restartCLI([...common, "--output", join(directory, "output")], () => rt), /PRIVATE_FILE_REQUIRED/);
  chmodSync(planPath, 0o600);
  const hard = join(directory, "hard"); linkSync(planPath, hard);
  await assert.rejects(() => restartCLI([...common, "--output", join(directory, "output")], () => rt));
  assert.equal(rt.patches, 0);
});

test("backend error values never enter UNKNOWN receipt or private journal", t => {
  const directory = privateDirectory(t), path = join(directory, "journal"), rt = fakeRuntime(), plan = makePlan();
  rt.patch = function () { this.patches++; throw Error("PRIVATE_BACKEND_SENTINEL"); };
  const result = restartStep("apply", plan, fingerprint(plan), path, rt);
  assert.equal(result.status, "UNKNOWN");
  assert.equal(result.code, "APPLICATION_RESTART_FAILED");
  assert.equal(JSON.stringify(result).includes("PRIVATE_"), false);
  assert.equal(readFileSync(path, "utf8").includes("PRIVATE_"), false);
});

test("production context, unknown target and overlapping paths are refused before adapter creation", async t => {
  const directory = privateDirectory(t);
  const forbidden = () => { throw Error("adapter must not be created"); };
  await assert.rejects(() => restartCLI(["plan", "--context", "production", "--target", "control-plane", "--output", join(directory, "a")], forbidden), /STAGING_CONTEXT/);
  await assert.rejects(() => restartCLI(["plan", "--context", context, "--target", "postgresql", "--output", join(directory, "b")], forbidden), /PLAN_ARGUMENTS/);
  const path = join(directory, "same");
  await assert.rejects(() => restartCLI(["apply", "--context", context, "--plan", path, "--evidence", path,
    "--approved-plan-sha", "a".repeat(64), "--confirm", "RESTART-STAGING-APPLICATION", "--output", path], forbidden), /PATHS_OVERLAP/);
});

test("real operator adapter emits only exact GET/list, bounded annotation PATCH and rollout without stdin", t => {
  const deployment = fixture(), plan = makePlan(), calls = [];
  const previous = process.env.UNRELATED_SECRET;
  process.env.UNRELATED_SECRET = "PRIVATE_ENV_SENTINEL";
  t.after(() => { if (previous === undefined) delete process.env.UNRELATED_SECRET; else process.env.UNRELATED_SECRET = previous; });
  const execute = (file, args, options) => {
    calls.push({ file, args, options });
    assert.equal(file, "kubectl");
    assert.deepEqual(args.slice(0, 3), ["--context", context, "--request-timeout=30s"]);
    assert.equal(Object.hasOwn(options.env, "UNRELATED_SECRET"), false);
    assert.ok(Object.keys(options.env).every(key => ["PATH", "HOME", "KUBECONFIG"].includes(key)));
    assert.ok(options.timeout > 0 && options.timeout <= 35_000);
    assert.equal(options.maxBuffer, 4 << 20);
    const action = args.slice(3);
    if (action[2] === "get") {
      assert.deepEqual(action.slice(-2), ["-o", "json"]);
      if (action[3] === "namespace") return JSON.stringify({ metadata: { uid: action[4] === "kube-system" ? uid(3) : uid(4), labels: { "kodex.dev/environment": "staging" } } });
      if (action[3] === "deployment") { assert.equal(action[4], "control-plane"); return JSON.stringify(deployment); }
      assert.deepEqual(action.slice(2, 6), ["get", "replicasets,pods", "-l", "app.kubernetes.io/name=control-plane,app.kubernetes.io/component=control-plane"]);
      return JSON.stringify(resources(deployment));
    }
    if (action[2] === "patch") {
      const patch = prepareApplicationRestart(plan, deployment, resources(deployment));
      assert.deepEqual(action, ["-n", "kodex-system", "patch", "deployment", "control-plane", "--type=json", "--patch", JSON.stringify(patch), "-o", "name"]);
      assert.equal(options.input, undefined);
      assert.equal(action.includes("--patch-file=/dev/stdin"), false);
      assert.equal(action.join(" ").includes("PRIVATE_"), false);
      return "deployment.apps/control-plane";
    }
    assert.deepEqual(action, ["-n", "kodex-system", "rollout", "status", "deployment/control-plane", "--timeout=30s"]);
    return "rolled out";
  };
  const rt = applicationRestartRuntime(context, accessProfile, executorSHA, 60, execute);
  assert.deepEqual(rt.identities(), { clusterUID: uid(3), namespaceUID: uid(4) });
  rt.snapshot("control-plane");
  rt.patch("control-plane", prepareApplicationRestart(plan, deployment, resources(deployment)));
  rt.wait("control-plane", 30);
  assert.equal(calls.length, 6);
  assert.equal(calls.filter(call => call.args.includes("patch")).length, 1);
  assert.equal(calls.some(call => call.args.includes("secret") || call.args.includes("exec") || call.args.includes("delete")), false);
});

test("real kubectl local patch accepts the operator transport without reopening child stdin", t => {
  const directory = privateDirectory(t), deployment = fixture(), plan = makePlan();
  const path = join(directory, "deployment.json");
  writeFileSync(path, JSON.stringify(deployment), { mode: 0o600 });
  let result, calls = 0;
  // Настоящий kubectl обрабатывает только синтетический файл: --local и пустой kubeconfig исключают кластер.
  const execute = (file, args, options) => {
    calls++;
    assert.equal(options.input, undefined);
    assert.deepEqual(JSON.parse(args[args.indexOf("--patch") + 1]),
      prepareApplicationRestart(plan, deployment, resources(deployment)));
    const patchArgs = args.slice(args.indexOf("--type=json"), args.lastIndexOf("-o"));
    const output = execFileSync(file, ["patch", "--local=true", "-f", path, ...patchArgs, "-o", "json"],
      { ...options, env: { PATH: process.env.PATH, HOME: directory, KUBECONFIG: "/dev/null" } });
    result = JSON.parse(output);
    return "deployment.apps/control-plane";
  };
  const rt = applicationRestartRuntime(context, accessProfile, executorSHA, 30, execute);
  rt.patch("control-plane", prepareApplicationRestart(plan, deployment, resources(deployment)));
  assert.equal(calls, 1);
  const expected = structuredClone(deployment);
  expected.spec.template.metadata.annotations[restartAnnotation] = plan.restartID;
  assert.deepEqual(result, expected);
});
