import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { stableChartLocks } from "./fetch-stable-charts.mjs";
import { releaseSystemHelm } from "./stable-system-helm-release.mjs";
import { systemDesiredValues, systemExpectedObjects, systemObjectKey, systemRecoveryManifest, systemRecoveryValues, systemServing } from "./stable-system-helm-state.mjs";

const profiles = JSON.parse(await readFile(new URL("../../infra/stable-system/helm-profiles.json", import.meta.url), "utf8")).profiles;
async function fixture(t) {
  const dir = await mkdtemp(tmpdir() + "/kodex-helm-release-fixture-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const source = "f".repeat(40), rows = [], states = new Map(), trace = [], flags = {}, runtime = new Map(), locks = await stableChartLocks();
  const objects = new Map(), put = r => { objects.set(systemObjectKey(r), r); return r; };
  const live = r => put({ ...structuredClone(r), metadata: { ...r.metadata, uid: r.metadata.name + "-uid",
    resourceVersion: "1", generation: 1 } });
  const workload = (kind, name, profile) => ({ apiVersion: kind === "Alertmanager" ? "monitoring.coreos.com/v1" : "apps/v1", kind,
    metadata: { name, namespace: profile.namespace, labels: { app: name } }, spec: { replicas: 1,
      ...(kind === "Alertmanager" ? { resources: {} } : { selector: { matchLabels: { app: name } },
        template: { metadata: { labels: { app: name } }, spec: { containers: [{ name: "app", image: "example.invalid/app:v1", resources: {} }] } } }) } });
  const ready = r => {
    if (!["Deployment", "StatefulSet"].includes(r.kind)) return r;
    return { ...r, status: { observedGeneration: 1, replicas: r.spec.replicas, updatedReplicas: r.spec.replicas,
      readyReplicas: r.spec.replicas, availableReplicas: r.spec.replicas, currentRevision: "same", updateRevision: "same" } };
  };
  const kube = async (argv, input) => {
    const at = argv.indexOf("get");
    if (argv.includes("create")) return input;
    if (at < 0) throw Error("FIXTURE_KUBE_COMMAND_UNEXPECTED");
    if (argv[at + 1] === "namespace") return JSON.stringify({ metadata: { uid: "cluster-uid" } });
    if (argv.includes("-f")) {
      const items = JSON.parse(input).items.map(r => objects.get(systemObjectKey(r))).filter(Boolean);
      if (argv.at(-1).startsWith("jsonpath=")) return items.map(r => [r.apiVersion, r.kind, r.metadata.namespace ?? "", r.metadata.name,
        r.metadata.uid, r.metadata.resourceVersion].join("|")).join("\n");
      return JSON.stringify({ items });
    }
    const ns = argv[argv.indexOf("-n") + 1], kind = argv[at + 1], name = argv[at + 2];
    if (["Deployment", "StatefulSet"].includes(kind)) return JSON.stringify(ready([...objects.values()].find(r => r.kind === kind && r.metadata.namespace === ns && r.metadata.name === name)));
    if (["replicasets,pods", "pods"].includes(kind)) {
      const selector = argv[argv.indexOf("-l") + 1], name = selector.slice(4);
      const d = [...objects.values()].find(r => ["Deployment", "StatefulSet"].includes(r.kind) && r.metadata.name === name && r.metadata.namespace === ns);
      const rs = { kind: "ReplicaSet", metadata: { uid: name + "-rs", ownerReferences: [{ kind: "Deployment", uid: d.metadata.uid, controller: true }] } };
      const pods = Array.from({ length: d.spec.replicas }, (_, i) => ({ kind: "Pod", metadata: { uid: name + "-pod-" + i,
        ownerReferences: [{ kind: d.kind === "Deployment" ? "ReplicaSet" : "StatefulSet", uid: d.kind === "Deployment" ? rs.metadata.uid : d.metadata.uid, controller: true }] },
      spec: structuredClone(d.spec.template.spec), status: { conditions: [{ type: "Ready", status: "True" }],
        containerStatuses: [{ name: "app", ready: true, state: { running: {} }, imageID: "containerd://sha256:" + "a".repeat(64) }] } }));
      return JSON.stringify({ items: kind === "pods" ? pods : [rs, ...pods] });
    }
    throw Error("FIXTURE_KUBE_READ_UNEXPECTED");
  };
  for (const profile of profiles) {
    const before = profile.deployments.map(name => workload("Deployment", name, profile));
    if (profile.alertmanager) before.push(workload("Alertmanager", profile.alertmanager, profile));
    before.push({ apiVersion: "v1", kind: "Secret", metadata: { name: profile.name + "-credential", namespace: profile.namespace },
      data: { value: "HELM_PRIVATE_SENTINEL" } });
    before.forEach(live);
    const after = structuredClone(before);
    for (const r of after) {
      if (r.kind === "Deployment") { r.spec.replicas = 2; r.spec.template.spec.containers[0].resources = profile.name === "cert-manager" ? profile.values.resources :
        profile.name === "trust-manager" ? profile.values.resources : profile.values["kube-state-metrics"].resources; }
      if (r.kind === "Alertmanager") { r.spec.replicas = 2; r.spec.resources = profile.values.alertmanager.alertmanagerSpec.resources; }
    }
    for (const name of [...profile.deployments, ...(profile.alertmanager ? [profile.alertmanager] : [])])
      after.push({ apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { name, namespace: profile.namespace,
        labels: { "app.kubernetes.io/managed-by": "Helm" }, annotations: { "meta.helm.sh/release-name": profile.release, "meta.helm.sh/release-namespace": profile.namespace } },
      spec: { minAvailable: 1, selector: { matchLabels: { app: name } } } });
    if (profile.alertmanager) {
      const d = live(workload("Deployment", "alertmanager-" + profile.alertmanager, profile)); d.kind = "StatefulSet";
      objects.delete("apps/v1/Deployment/" + profile.namespace + "/" + d.metadata.name);
      d.metadata.ownerReferences = [{ kind: "Alertmanager", name: profile.alertmanager, uid: profile.alertmanager + "-uid", controller: true }]; put(d);
    }
    const values = { private: "HELM_PRIVATE_SENTINEL" }, desiredValues = systemDesiredValues(values, profile.values), recoveryValues = systemRecoveryValues(profile, values);
    const recovery = systemRecoveryManifest(before, after), expected = await systemExpectedObjects(profile, before, after, objects, kube);
    const row = { name: profile.name, namespace: profile.namespace, release: profile.release, sourceRevision: 10,
      profileSHA256: fingerprint(profile), valuesSHA256: fingerprint(values), desiredValuesSHA256: fingerprint(desiredValues),
      recoveryValuesSHA256: fingerprint(recoveryValues), beforeSHA256: fingerprint(before), afterSHA256: fingerprint(after), recoverySHA256: fingerprint(recovery),
      chartSHA256: locks.find(l => l.name === profile.name).sha256, objects: expected, serving: await systemServing(profile, kube) };
    rows.push(row); runtime.set(profile.name, { chart: dir + "/fixture.tgz", desiredValues });
    states.set(profile.release, { name: profile.release, namespace: profile.namespace, version: 10, config: values,
      manifest: JSON.stringify(before), info: { status: "deployed", description: "original" }, before, after, recovery, values });
  }
  const plan = { schema: "kodex.stable-system-helm-plan/v2", source, context: "k3d-kodex", clusterUID: "cluster-uid", rows, apply: "NOT_RUN" };
  const hash = fingerprint(plan), path = dir + "/plan.json";
  await writeFile(path, JSON.stringify({ plan, planSHA256: hash }), { mode: 0o600 });
  const command = async (program, argv, input) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? source : "";
    if (program === "yq") return JSON.parse(input).map(r => JSON.stringify(r)).join("\n");
    if (program !== "helm") return kube(argv, input);
    const profile = profiles.find(p => p.release === argv[1] || p.release === argv[2]), state = states.get(profile.release);
    if (argv[0] === "status") return JSON.stringify(state);
    if (argv[0] === "get") return argv[1] === "manifest" ? JSON.stringify(Number(argv[argv.indexOf("--revision") + 1]) === 10 ? state.before : state.after) : JSON.stringify(state.values);
    if (argv[0] !== "upgrade") throw Error("FIXTURE_HELM_COMMAND_UNEXPECTED");
    const recovery = argv.includes("--dry-run=server") || argv[argv.indexOf("--description") + 1].endsWith(":rollback");
    if (argv.includes("--dry-run=server")) return JSON.stringify({ manifest: JSON.stringify(state.recovery), config: JSON.parse(input) });
    if (flags.failBeforeWrite) throw Error("FIXTURE_UPGRADE_LOST_BEFORE_EFFECT");
    state.version++; state.config = JSON.parse(input); state.manifest = JSON.stringify(recovery ? state.recovery : state.after);
    state.info = { status: flags.pending ? "pending-upgrade" : "deployed", description: argv[argv.indexOf("--description") + 1] };
    for (const r of recovery ? state.recovery : state.after) {
      const key = systemObjectKey(r), previous = objects.get(key), value = structuredClone(r);
      value.metadata = { ...value.metadata, uid: previous?.metadata.uid ?? r.metadata.name + "-created-uid", resourceVersion: "1", generation: 1 };
      put(value);
    }
    if (profile.alertmanager) objects.get("apps/v1/StatefulSet/" + profile.namespace + "/alertmanager-" + profile.alertmanager).spec.replicas = recovery ? 1 : 2;
    if (flags.lostApply && !recovery) { flags.lostApply = false; throw Error("FIXTURE_UPGRADE_ACK_LOST"); }
    if (flags.lostRollback && recovery) { flags.lostRollback = false; throw Error("FIXTURE_RECOVERY_ACK_LOST"); }
    return JSON.stringify(state);
  };
  const preflight = async (argv, port, internal) => ({ document: { plan: flags.planDrift ? { ...plan, clusterUID: "foreign" } :
    internal.names ? { ...plan, rows: rows.filter(r => internal.names.includes(r.name)) } : plan }, runtime });
  const invoke = (mode, output, extra = []) => releaseSystemHelm([mode, "--context", "k3d-kodex", "--charts", dir, "--plan", path,
    "--output", dir + "/" + output, ...(mode === "apply" ? [] : ["--journal", dir + "/apply.jsonl"]),
    ...(["apply", "rollback"].includes(mode) ? ["--approved-plan-sha", hash] : []), ...extra], command, preflight, async () => {});
  const writes = () => trace.filter(r => r.program === "helm" && r.argv[0] === "upgrade" && !r.argv.includes("--dry-run=server"));
  return { dir, plan, hash, path, invoke, trace, flags, states, objects, writes };
}

test("synthetic Helm apply/readback/forward recovery preserves PDB UID, secrets and original values", async t => {
  const f = await fixture(t); await f.invoke("apply", "apply.jsonl"); await f.invoke("readback", "readback.jsonl");
  const pdbs = [...f.objects.values()].filter(r => r.kind === "PodDisruptionBudget").map(r => [r.metadata.name, r.metadata.uid]);
  await f.invoke("rollback", "rollback.jsonl");
  assert.deepEqual([...f.objects.values()].filter(r => r.kind === "PodDisruptionBudget").map(r => [r.metadata.name, r.metadata.uid]), pdbs);
  assert.equal(f.writes().length, 6); await f.invoke("rollback", "rollback-again.jsonl"); assert.equal(f.writes().length, 6);
  for (const state of f.states.values()) assert.equal(state.version, 12);
  for (const file of ["apply.jsonl", "readback.jsonl", "rollback.jsonl", "rollback-again.jsonl"])
    assert.equal((await readFile(f.dir + "/" + file, "utf8")).includes("HELM_PRIVATE_SENTINEL"), false);
  assert.equal(JSON.stringify(f.trace).includes("HELM_PRIVATE_SENTINEL"), false);
  assert.equal(JSON.stringify(f.plan).includes("HELM_PRIVATE_SENTINEL"), false);
});

test("lost upgrade response is inspected without retry and recovered only by a new forward revision", async t => {
  const f = await fixture(t); f.flags.lostApply = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_UPGRADE_ACK_LOST/);
  assert.equal(f.writes().length, 1); await f.invoke("inspect", "inspect.jsonl"); assert.equal(f.writes().length, 1);
  await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.writes().length, 2);
  assert.equal(f.states.get("cert-manager").version, 12); assert.equal(f.states.get("kodex-trust-manager").version, 10);
});

test("foreign UID, credential RV and unsolicited Helm revision block rollback before writes", async t => {
  for (const type of ["uid", "credential", "revision", "description"]) {
    const f = await fixture(t); await f.invoke("apply", "apply.jsonl");
    if (type === "revision") f.states.get("kodex-monitoring").version++;
    else if (type === "description") f.states.get("kodex-monitoring").info.description = "foreign operation";
    else {
      const r = [...f.objects.values()].find(r => r.kind === (type === "uid" ? "Deployment" : "Secret"));
      if (type === "uid") r.metadata.uid = "foreign"; else r.metadata.resourceVersion = "2";
    }
    const count = f.writes().length;
    await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /SYSTEM_HELM_/); assert.equal(f.writes().length, count);
  }
});

test("pending Helm operation is observable but never unlocked or repeated automatically", async t => {
  const f = await fixture(t); f.flags.pending = true; f.flags.lostApply = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl")); await f.invoke("inspect", "inspect.jsonl");
  await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /SYSTEM_HELM_PENDING_OPERATION_UNRESOLVED/);
  assert.equal(f.writes().length, 1);
});

test("lost recovery response is proven from its exact forward revision without repeating upgrade", async t => {
  const f = await fixture(t); await f.invoke("apply", "apply.jsonl"); f.flags.lostRollback = true;
  await assert.rejects(f.invoke("rollback", "rollback-lost.jsonl"), /FIXTURE_RECOVERY_ACK_LOST/);
  assert.equal(f.writes().length, 4); await f.invoke("rollback", "rollback-resolved.jsonl");
  assert.equal(f.writes().length, 6); await f.invoke("rollback", "rollback-again.jsonl"); assert.equal(f.writes().length, 6);
});

test("changed plan and duplicate approval arguments are rejected before an upgrade", async t => {
  const f = await fixture(t); f.flags.planDrift = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl"), /SYSTEM_HELM_PLAN_DRIFT/); assert.equal(f.writes().length, 0);
  await assert.rejects(f.invoke("apply", "apply-other.jsonl", ["--approved-plan-sha", "wrong"]), /SYSTEM_HELM_ARGUMENTS_INVALID/);
  assert.equal(f.writes().length, 0);
});
