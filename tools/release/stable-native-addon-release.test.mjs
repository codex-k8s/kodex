import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { validateNodeContainer } from "./stable-node-registry-model.mjs";
import { nativeAddonBinding, nativeDeploymentMetadataHash, prepareNativeDeployment } from "./stable-native-model.mjs";
import { systemObjectKey, systemObjectState } from "./stable-system-helm-state.mjs";
import { releaseNativeAddons } from "./stable-native-addon-release.mjs";
const config = JSON.parse(await readFile(new URL("../../infra/stable-system/native-profiles.json", import.meta.url), "utf8"));

async function fixture(t) {
  const dir = await mkdtemp(tmpdir() + "/kodex-native-addon-fixture-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const source = "f".repeat(40), trace = [], state = new Map(), markerState = new Map(), flags = {}, sourceHash = "c".repeat(64);
  const node = { metadata: { name: "k3d-kodex-server-0", uid: "node-uid" }, status: { conditions: [{ type: "Ready", status: "True" }] } };
  const container = { Id: "a".repeat(64), Image: "sha256:" + "b".repeat(64), Name: "/" + node.metadata.name,
    Config: { Labels: { "k3d.cluster": "kodex", "k3d.role": "server" }, Cmd: ["server"], Env: [] }, State: { Running: true } };
  const binding = { ...validateNodeContainer(node, container, "kodex"), role: "server" };
  const targets = [], addons = [], intent = "d".repeat(64);
  const ready = d => ({ ...d, status: { observedGeneration: d.metadata.generation, replicas: d.spec.replicas, updatedReplicas: d.spec.replicas,
    readyReplicas: d.spec.replicas, availableReplicas: d.spec.replicas } });
  for (const p of config.profiles.filter(p => p.owner === "addon")) {
    const d = { apiVersion: "apps/v1", kind: "Deployment", metadata: { name: p.name, namespace: "kube-system", uid: p.name + "-uid", resourceVersion: "1",
      generation: 1, labels: { app: p.name }, annotations: { "deployment.kubernetes.io/revision": "1" } }, spec: { replicas: 1,
      selector: { matchLabels: { app: p.name } }, template: { metadata: { labels: { app: p.name } },
        spec: { containers: [{ name: p.container, image: p.sourceImage, resources: {} }] } } } };
    state.set(systemObjectKey(d), d);
    const cm = { apiVersion: "v1", kind: "ConfigMap", metadata: { name: p.name + "-config", namespace: "kube-system", uid: p.name + "-config-uid", resourceVersion: "1" },
      data: { PRIVATE: "PRIVATE_NATIVE_ADDON_SENTINEL" } }; state.set(systemObjectKey(cm), cm);
    const change = prepareNativeDeployment(d, p), expected = { ...d, spec: change.after }, { after: _after, ...transition } = change;
    const imageID = "containerd://" + p.configDigest;
    targets.push({ name: p.name, ...transition, metadataSHA256: nativeDeploymentMetadataHash(d), serving: { kind: "Deployment", name: p.name,
      uid: d.metadata.uid, images: [{ name: "containers/" + p.container, imageID }] } });
    const addon = { apiVersion: "k3s.cattle.io/v1", kind: "Addon", metadata: { name: p.addon, namespace: "kube-system", uid: p.addon + "-addon" },
      spec: { source: p.manifest, checksum: sourceHash } }; state.set(systemObjectKey(addon), addon);
    const before = systemObjectState(d), cmState = systemObjectState(cm);
    addons.push({ name: p.name, files: [{ ...nativeAddonBinding(p, addon, sourceHash), serverUID: binding.uid, containerID: binding.containerID,
      metadata: "0:0:644:1:1:100:10" }], objects: [{ ...before, before: before.contentSHA256, after: systemObjectState(expected).contentSHA256, recovery: before.contentSHA256 },
      { ...cmState, before: null, after: null, recovery: null }],
    pdb: { apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { name: "kodex-stable-" + p.name, namespace: "kube-system",
      labels: { "app.kubernetes.io/managed-by": "kodex-stable-native" }, annotations: { "kodex.dev/stable-native-intent": intent } },
    spec: { minAvailable: 1, selector: structuredClone(d.spec.selector) } } });
  }
  const plan = { schema: "kodex.stable-native-plan/v2", source, context: "k3d-kodex", clusterUID: "cluster", intent, profileSHA256: fingerprint(config),
    nodes: [binding], targets, addons, traefik: { chartPath: dir + "/chart.tgz" } }, hash = fingerprint(plan), path = dir + "/plan.json";
  await writeFile(path, JSON.stringify({ plan, planSHA256: hash }), { mode: 0o600 });
  const command = async (program, argv, input) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? source : "";
    if (program === "docker") {
      if (argv[0] === "inspect") return JSON.stringify(container);
      const at = argv.indexOf("kodex-native-marker"), action = argv[at + 1], name = argv[at + 2], expectedReceipt = argv.at(-1);
      if (action === "inspect") {
        const value = markerState.get(name) ?? "ABSENT";
        if (value !== "ABSENT" && expectedReceipt && value !== expectedReceipt) throw Error("NATIVE_ADDON_MARKER_DRIFT");
        return value;
      }
      if (action === "install") {
        assert.equal(markerState.has(name), false); markerState.set(name, "0:0:600:1:9:101:197");
        if (flags.lostMarker) { flags.lostMarker = false; throw Error("FIXTURE_MARKER_ACK_LOST"); }
        return markerState.get(name);
      }
      if (action === "remove") {
        assert.equal(state.get("apps/v1/Deployment/kube-system/" + name).spec.replicas, 1);
        assert.equal(state.has("policy/v1/PodDisruptionBudget/kube-system/kodex-stable-" + name), false);
        assert.equal(markerState.get(name), expectedReceipt); markerState.delete(name); return "REMOVED";
      }
      throw Error("FIXTURE_WORKER_COMMAND_UNEXPECTED");
    }
    if (argv.includes("rollout")) return "";
    const get = argv.indexOf("get");
    if (get >= 0) {
      if (argv.includes("--raw=/apis/metrics.k8s.io/v1beta1/nodes")) return JSON.stringify({ apiVersion: "metrics.k8s.io/v1beta1", kind: "NodeMetricsList", items: [{ metadata: { name: node.metadata.name } }] });
      const kind = argv[get + 1], name = argv[get + 2];
      if (kind === "namespace") return JSON.stringify({ metadata: { uid: "cluster" } });
      if (kind === "nodes") return JSON.stringify({ items: [node] });
      if (argv.includes("-f")) {
        const items = JSON.parse(input).items.map(d => state.get(systemObjectKey(d))).filter(Boolean);
        if (argv.at(-1).startsWith("jsonpath=")) return items.map(r => [r.apiVersion, r.kind, r.metadata.namespace, r.metadata.name, r.metadata.uid, r.metadata.resourceVersion].join("|")).join("\n");
        return JSON.stringify({ items: items.map(r => r.kind === "Deployment" ? ready(r) : r) });
      }
      if (kind === "replicasets,pods") {
        const selected = argv[argv.indexOf("-l") + 1].slice(4), d = state.get("apps/v1/Deployment/kube-system/" + selected), p = config.profiles.find(p => p.name === selected);
        const rs = { kind: "ReplicaSet", metadata: { uid: selected + "-rs", ownerReferences: [{ kind: "Deployment", uid: d.metadata.uid, controller: true }] } };
        return JSON.stringify({ items: [rs, ...Array.from({ length: d.spec.replicas }, (_, i) => ({ kind: "Pod", metadata: { uid: selected + "-pod-" + i,
          ownerReferences: [{ kind: "ReplicaSet", uid: rs.metadata.uid, controller: true }] }, spec: structuredClone(d.spec.template.spec),
        status: { conditions: [{ type: "Ready", status: "True" }], containerStatuses: [{ name: p.container, ready: true, state: { running: {} }, imageID: "containerd://" + p.configDigest }] } }))] });
      }
      const actualKind = { deployment: "Deployment", poddisruptionbudget: "PodDisruptionBudget", "addons.k3s.cattle.io": "Addon" }[kind] ?? kind;
      const r = [...state.values()].find(r => r.kind === actualKind && r.metadata.name === name);
      if (!r) return ""; return JSON.stringify(r.kind === "Deployment" ? ready(r) : r);
    }
    if (argv.includes("create")) {
      const r = JSON.parse(input); r.metadata.uid = r.metadata.name + "-uid"; r.metadata.resourceVersion = "1"; state.set(systemObjectKey(r), r);
      if (flags.lostPdb) { flags.lostPdb = false; throw Error("FIXTURE_PDB_ACK_LOST"); } return JSON.stringify(r);
    }
    const patch = argv.indexOf("patch");
    if (patch >= 0) {
      const d = state.get("apps/v1/Deployment/kube-system/" + argv[patch + 2]), ops = JSON.parse(input);
      assert.equal(ops[0].value, d.metadata.uid); assert.equal(ops[1].value, d.metadata.resourceVersion); assert.deepEqual(ops[2].value, d.spec);
      d.spec = ops[3].value; d.metadata.generation++; d.metadata.resourceVersion = String(Number(d.metadata.resourceVersion) + 1);
      d.metadata.annotations["deployment.kubernetes.io/revision"] = String(d.metadata.generation);
      if (flags.lostPatch) { flags.lostPatch = false; throw Error("FIXTURE_PATCH_ACK_LOST"); } return JSON.stringify(ready(d));
    }
    if (argv.includes("delete")) {
      const name = argv[argv.indexOf("--raw") + 1].split("/").at(-1), key = "policy/v1/PodDisruptionBudget/kube-system/" + name, r = state.get(key), p = JSON.parse(input).preconditions;
      assert.equal(p.uid, r.metadata.uid); assert.equal(p.resourceVersion, r.metadata.resourceVersion); state.delete(key); return "{}";
    }
    throw Error("FIXTURE_COMMAND_UNEXPECTED");
  };
  const invoke = (mode, output, recovery) => releaseNativeAddons([mode, "--context", "k3d-kodex", "--plan", path, "--output", dir + "/" + output,
    ...(mode === "apply" ? [] : ["--journal", dir + "/apply.jsonl"]), ...(["apply", "rollback"].includes(mode) ? ["--approved-plan-sha", hash] : []),
    ...(recovery ? ["--recovery-journal", dir + "/" + recovery] : [])], command, async () => ({ plan: structuredClone(plan) }));
  const writes = () => trace.filter(r => r.program === "docker" && ["install", "remove", "finalize"].includes(r.argv[r.argv.indexOf("kodex-native-marker") + 1]) ||
    r.program !== "docker" && r.argv.some(a => ["create", "patch", "delete"].includes(a)));
  return { dir, invoke, flags, state, markerState, trace, writes };
}

test("synthetic addon apply and rollback retain all source objects and re-enable native reconciliation last", async t => {
  const f = await fixture(t); await f.invoke("apply", "apply.jsonl"); await f.invoke("readback", "readback.jsonl");
  assert.equal(f.writes().length, 6); await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.writes().length, 12);
  await f.invoke("rollback", "rollback-again.jsonl"); assert.equal(f.writes().length, 12); assert.equal(f.markerState.size, 0);
  assert.equal(JSON.stringify(f.trace).includes("PRIVATE_NATIVE_ADDON_SENTINEL"), false);
  for (const path of ["apply.jsonl", "readback.jsonl", "rollback.jsonl"]) assert.equal((await readFile(f.dir + "/" + path, "utf8")).includes("PRIVATE_NATIVE_ADDON_SENTINEL"), false);
});

test("lost marker, PDB and patch responses never replay the write; source recovery remains auditable", async t => {
  for (const flag of ["lostMarker", "lostPdb", "lostPatch"]) {
    const f = await fixture(t); f.flags[flag] = true;
    await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_/); const count = f.writes().length;
    await f.invoke("inspect", "inspect.jsonl"); assert.equal(f.writes().length, count);
    await f.invoke("rollback", "rollback.jsonl"); const restored = f.writes().length;
    await f.invoke("rollback", "rollback-again.jsonl", "rollback.jsonl"); assert.equal(f.writes().length, restored);
  }
});

test("foreign deployment, marker inode and protected ConfigMap version stop before rollback mutations", async t => {
  for (const type of ["deployment", "marker", "config"]) {
    const f = await fixture(t); await f.invoke("apply", "apply.jsonl");
    if (type === "deployment") f.state.get("apps/v1/Deployment/kube-system/coredns").metadata.uid = "foreign";
    else if (type === "config") f.state.get("v1/ConfigMap/kube-system/coredns-config").metadata.resourceVersion = "2";
    else f.markerState.set("coredns", "0:0:600:1:9:999:197");
    const count = f.writes().length;
    await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /NATIVE_|SYSTEM_HELM_/);
    assert.equal(f.writes().length, count);
  }
});
