import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import test from "node:test";
import { fingerprint } from "./stable-inventory.mjs";
import { validateNodeContainer } from "./stable-node-registry-model.mjs";
import { nativeChartMetadataHash, nativeDeploymentMetadataHash, prepareNativeDeployment, traefikHelmConfig } from "./stable-native-model.mjs";
import { nativeHelmJob } from "./stable-native-helm-job.mjs";
import { systemObjectKey, systemObjectState } from "./stable-system-helm-state.mjs";
import { releaseNativeTraefik } from "./stable-native-traefik-release.mjs";

const config = JSON.parse(await readFile(new URL("../../infra/stable-system/native-profiles.json", import.meta.url), "utf8"));
const profile = config.profiles.find(p => p.name === "traefik"), sha = bytes => createHash("sha256").update(bytes).digest("hex");

async function fixture(t) {
  const dir = await mkdtemp(tmpdir() + "/kodex-native-traefik-fixture-"); t.after(() => rm(dir, { recursive: true, force: true }));
  const source = "a".repeat(40), intent = "b".repeat(64), trace = [], flags = {}, objects = new Map();
  const node = { metadata: { name: "k3d-kodex-server-0", uid: "node-uid" }, status: { conditions: [{ type: "Ready", status: "True" }] } };
  const container = { Id: "c".repeat(64), Image: "sha256:" + "d".repeat(64), Name: "/" + node.metadata.name,
    Config: { Labels: { "k3d.cluster": "kodex", "k3d.role": "server" }, Cmd: ["server"], Env: [] }, State: { Running: true } };
  const chart = { apiVersion: "helm.cattle.io/v1", kind: "HelmChart", metadata: { name: "traefik", namespace: "kube-system", uid: "chart-uid" },
    spec: { chart: "https://%{KUBERNETES_API}%" + config.traefikChart.apiPath, set: { "global.systemDefaultRegistry": "" },
      valuesContent: JSON.stringify({ deployment: { replicas: 1 }, resources: {} }) } };
  const beforeD = { apiVersion: "apps/v1", kind: "Deployment", metadata: { name: "traefik", namespace: "kube-system", uid: "deployment-uid",
    resourceVersion: "1", generation: 1, labels: { app: "traefik" }, annotations: { "deployment.kubernetes.io/revision": "1" } },
  spec: { replicas: 1, selector: { matchLabels: { app: "traefik" } }, strategy: { type: "RollingUpdate", rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } },
    template: { metadata: { labels: { app: "traefik" } }, spec: { containers: [{ name: "traefik", image: profile.sourceImage, resources: {},
      env: [{ name: "POD_NAME", value: "pod" }, { name: "POD_NAMESPACE", value: "kube-system" }, { name: "USER", value: "65532" }] }] } } } };
  const transition = prepareNativeDeployment(beforeD, profile), afterD = { ...structuredClone(beforeD), spec: transition.after };
  const service = { apiVersion: "v1", kind: "Service", metadata: { name: "traefik", namespace: "kube-system", uid: "service-uid", resourceVersion: "1" },
    spec: { selector: { app: "traefik" }, ports: [{ port: 443, targetPort: 8443 }] } };
  const pdb = { apiVersion: "policy/v1", kind: "PodDisruptionBudget", metadata: { name: "traefik", namespace: "kube-system", uid: "pdb-uid", resourceVersion: "1",
    annotations: { "meta.helm.sh/release-name": "traefik", "meta.helm.sh/release-namespace": "kube-system" }, labels: { "app.kubernetes.io/managed-by": "Helm" } },
  spec: { minAvailable: 1, selector: { matchLabels: { app: "traefik" } } } };
  const before = [beforeD, service], after = [afterD, service, pdb], recovery = [beforeD, service, pdb];
  let revision = 1, releaseStatus = "deployed", job, pods = [], jobCount = 0, hcc, ready = true;
  const makeJob = desired => {
    jobCount++;
    job = { apiVersion: "batch/v1", kind: "Job", metadata: { name: "helm-install-traefik", namespace: "kube-system", uid: "job-" + jobCount,
      ownerReferences: [{ apiVersion: chart.apiVersion, kind: chart.kind, name: chart.metadata.name, uid: chart.metadata.uid }] },
    spec: { template: { metadata: { annotations: { "helmcharts.helm.cattle.io/configHash": "SHA256=" + String(jobCount).repeat(64) } },
      spec: { serviceAccountName: "helm-traefik", containers: [{ name: "helm", image: "rancher/klipper-helm:v0.10.0-build20260513",
        env: [{ name: "INSECURE_SKIP_TLS_VERIFY", value: "false" }, { name: "PLAIN_HTTP", value: "false" },
          { name: "FAILURE_POLICY", value: desired ? "abort" : "reinstall" }] }],
      volumes: [{ name: "values", projected: { sources: [{ secret: { name: "chart-values-traefik", items: [
        { key: "HelmChartValuesContent", path: "source.yaml" }, ...(desired ? [{ key: "HelmChartConfigValuesContent", path: "config.yaml" }] : [])] } }] } }] } } },
    status: { succeeded: 1, conditions: [{ type: "Complete", status: "True" }] } };
    pods.push({ kind: "Pod", metadata: { namespace: "kube-system", uid: "helm-pod-" + jobCount,
      ownerReferences: [{ kind: "Job", name: job.metadata.name, uid: job.metadata.uid, controller: true }] },
    spec: { containers: [{ name: "helm" }] }, status: { phase: "Succeeded", containerStatuses: [{ name: "helm", state: { terminated: { exitCode: 0 } } }] } });
  };
  makeJob(); objects.set(systemObjectKey(beforeD), structuredClone(beforeD)); objects.set(systemObjectKey(service), structuredClone(service));
  const guards = before.map(r => {
    const old = systemObjectState(r), desired = systemObjectState(after.find(n => systemObjectKey(n) === systemObjectKey(r)));
    return { ...old, before: old.contentSHA256, after: desired.contentSHA256, recovery: old.contentSHA256 };
  });
  const p = systemObjectState(pdb); guards.push({ ...p, uid: null, before: null, after: p.contentSHA256, recovery: p.contentSHA256 });
  const { after: _spec, ...safe } = transition;
  const metadata = () => ({ name: "traefik", revision, status: releaseStatus });
  const target = { name: "traefik", ...safe, metadataSHA256: nativeDeploymentMetadataHash(beforeD),
    serving: { kind: "Deployment", name: "traefik", uid: beforeD.metadata.uid,
      images: [{ name: "containers/traefik", imageID: "containerd://" + profile.configDigest }] } };
  const plan = { schema: "kodex.stable-native-plan/v2", source, context: "k3d-kodex", clusterUID: "cluster-uid", intent,
    profileSHA256: fingerprint(config), nodes: [{ ...validateNodeContainer(node, container, "kodex"), role: "server" }], targets: [target],
    traefik: { uid: chart.metadata.uid, specSHA256: fingerprint(chart.spec), chartMetadataSHA256: nativeChartMetadataHash(chart), objects: guards,
      sourceConfig: null, releaseRevision: 1, metadataSHA256: fingerprint(metadata()), chartSHA256: config.traefikChart.sha256, chartPath: dir + "/chart.tgz",
      hcc: traefikHelmConfig(chart, profile, intent), recoveryHcc: traefikHelmConfig(chart, profile, intent, true),
      beforeSHA256: fingerprint(before), afterSHA256: fingerprint(after), recoverySHA256: fingerprint(recovery),
      sourceValuesSHA256: sha(chart.spec.valuesContent), sourceJob: nativeHelmJob(job, { items: pods }, chart.metadata.uid) } };
  const planHash = fingerprint(plan), path = dir + "/plan.json";
  await writeFile(path, JSON.stringify({ plan, planSHA256: planHash }), { mode: 0o600 });
  const liveD = () => {
    const d = structuredClone(objects.get(systemObjectKey(beforeD)));
    d.status = { observedGeneration: d.metadata.generation, replicas: d.spec.replicas, updatedReplicas: d.spec.replicas,
      readyReplicas: ready ? d.spec.replicas : 0, availableReplicas: ready ? d.spec.replicas : 0 }; return d;
  };
  const update = desired => {
    hcc = structuredClone(desired); hcc.metadata.uid = "hcc-uid"; hcc.metadata.resourceVersion = String(jobCount);
    const forward = JSON.parse(hcc.spec.valuesContent).deployment.replicas === 2;
    revision++; makeJob(desired);
    const d = structuredClone(forward ? afterD : beforeD); d.metadata.generation = revision; d.metadata.resourceVersion = String(revision);
    d.metadata.annotations["deployment.kubernetes.io/revision"] = String(revision); objects.set(systemObjectKey(d), d);
    if (!objects.has(systemObjectKey(pdb))) objects.set(systemObjectKey(pdb), structuredClone(pdb));
    ready = !flags.delayedReady;
    if (flags.failed) { job.status = { failed: 1, conditions: [{ type: "Failed", status: "True" }] }; releaseStatus = "failed"; }
  };
  const command = async (program, argv, input) => {
    trace.push({ program, argv });
    if (program === "git") return argv[0] === "rev-parse" ? source : "";
    if (program === "docker") return JSON.stringify(container);
    if (program === "yq") {
      try { return JSON.stringify(JSON.parse(input)); } catch { return input; }
    }
    if (program === "helm") {
      if (argv[0] === "upgrade") {
        if (flags.renderDrift) objects.get(systemObjectKey(service)).spec.ports[0].port = 80;
        return JSON.stringify({ manifest: (JSON.parse(input).deployment.replicas === 2 ? after : recovery).map(r => JSON.stringify(r)).join("\n") });
      }
      if (argv[1] === "metadata") return JSON.stringify(metadata());
      if (argv[1] === "manifest") return (revision === 1 ? before : revision === 2 ? after : recovery).map(r => JSON.stringify(r)).join("\n");
    }
    if (argv.includes("rollout")) { ready = true; return ""; }
    const get = argv.indexOf("get");
    if (get >= 0) {
      const kind = argv[get + 1];
      if (kind === "namespace") return JSON.stringify({ metadata: { uid: "cluster-uid" } });
      if (kind === "nodes") return JSON.stringify({ items: [node] });
      if (kind === "helmchart") return JSON.stringify(chart);
      if (kind === "helmchartconfig") return hcc ? JSON.stringify(hcc) : "";
      if (kind === "job") {
        if (jobCount > 1 && flags.gap) { flags.gap = false; return ""; } return JSON.stringify(job);
      }
      if (kind === "pods") return JSON.stringify({ items: pods });
      if (kind === "secret") {
        assert.equal(argv.at(-1).includes(".data.token"), false);
        return "values-uid|" + revision + "|" + Buffer.from(chart.spec.valuesContent).toString("base64") + "|" +
          Buffer.from(flags.badValues ? "FOREIGN_VALUES" : hcc.spec.valuesContent).toString("base64");
      }
      if (argv.includes("-f")) return JSON.stringify({ items: JSON.parse(input).items.map(r => objects.get(systemObjectKey(r))).filter(Boolean)
        .map(r => r.kind === "Deployment" ? liveD() : r) });
      if (kind.toLowerCase() === "deployment") return JSON.stringify(liveD());
      if (kind === "replicasets,pods") {
        const d = liveD(), rs = { kind: "ReplicaSet", metadata: { uid: "rs-" + revision,
          ownerReferences: [{ kind: "Deployment", uid: d.metadata.uid, controller: true }] } };
        return JSON.stringify({ items: [rs, ...Array.from({ length: d.spec.replicas }, (_, i) => ({ kind: "Pod", metadata: { uid: "serving-" + revision + "-" + i,
          ownerReferences: [{ kind: "ReplicaSet", uid: rs.metadata.uid, controller: true }] }, spec: structuredClone(d.spec.template.spec),
        status: { conditions: [{ type: "Ready", status: "True" }], containerStatuses: [{ name: "traefik", ready: true, state: { running: {} },
          imageID: "containerd://" + profile.configDigest }] } }))] });
      }
    }
    if (argv.includes("create")) {
      assert.equal(hcc, undefined); update(JSON.parse(input));
      if (flags.lostCreate) { flags.lostCreate = false; throw Error("FIXTURE_CREATE_ACK_LOST"); } return JSON.stringify(hcc);
    }
    if (argv.includes("patch")) {
      const ops = JSON.parse(input); assert.equal(ops[0].value, hcc.metadata.uid); assert.equal(ops[1].value, hcc.metadata.resourceVersion);
      assert.deepEqual(ops[2].value, hcc.spec); assert.deepEqual(ops[4].value, hcc.metadata.annotations);
      update({ ...hcc, spec: ops[3].value, metadata: { ...hcc.metadata, annotations: ops[5].value } }); return JSON.stringify(hcc);
    }
    throw Error("FIXTURE_COMMAND_UNEXPECTED");
  };
  const invoke = (mode, output, recoveryJournal) => releaseNativeTraefik([mode, "--context", "k3d-kodex", "--plan", path, "--output", dir + "/" + output,
    ...(mode === "apply" ? [] : ["--journal", dir + "/apply.jsonl"]), ...(["apply", "rollback"].includes(mode) ? ["--approved-plan-sha", planHash] : []),
    ...(recoveryJournal ? ["--recovery-journal", dir + "/" + recoveryJournal] : [])], command, async p => assert.equal(p.traefik.chartSHA256, config.traefikChart.sha256));
  const writes = () => trace.filter(r => r.argv.some(a => ["create", "patch", "delete"].includes(a)));
  return { dir, invoke, flags, objects, chart, trace, writes, hcc: () => hcc, job: () => job };
}

test("native Traefik HCC handoff waits for rollout and retains exact HCC/PDB on forward recovery", async t => {
  const f = await fixture(t); f.flags.delayedReady = true; f.flags.gap = true;
  await f.invoke("apply", "apply.jsonl"); await f.invoke("readback", "readback.jsonl"); assert.equal(f.writes().length, 1);
  const pdbUID = f.objects.get("policy/v1/PodDisruptionBudget/kube-system/traefik").metadata.uid;
  await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.writes().length, 2);
  await f.invoke("rollback", "rollback-again.jsonl"); assert.equal(f.writes().length, 2);
  assert.equal(f.objects.get("policy/v1/PodDisruptionBudget/kube-system/traefik").metadata.uid, pdbUID);
  assert.equal(f.trace.filter(r => r.argv.includes("rollout")).length, 2);
  assert.equal(f.trace.some(r => r.argv.includes("uninstall") || r.argv.includes("delete") || r.argv.includes("--force")), false);
});

test("lost HCC create ACK is observed, not replayed; completed recovery journal preserves predecessor lineage", async t => {
  const f = await fixture(t); f.flags.lostCreate = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl"), /FIXTURE_CREATE_ACK_LOST/);
  await f.invoke("inspect", "inspect.jsonl"); assert.equal(f.writes().length, 1);
  await f.invoke("rollback", "rollback.jsonl"); assert.equal(f.writes().length, 2);
  await assert.rejects(f.invoke("rollback", "without-lineage.jsonl"), /NATIVE_HELM_JOB_FOREIGN_POD/);
  await f.invoke("rollback", "with-lineage.jsonl", "rollback.jsonl"); assert.equal(f.writes().length, 2);
});

test("foreign HC/HCC/PDB/Job ownership blocks recovery before any mutation", async t => {
  for (const mutate of [f => { f.chart.metadata.uid = "foreign"; }, f => { f.hcc().metadata.uid = "foreign"; },
    f => { f.objects.get("policy/v1/PodDisruptionBudget/kube-system/traefik").metadata.uid = "foreign"; },
    f => { f.job().metadata.ownerReferences[0].uid = "foreign"; }]) {
    const f = await fixture(t); await f.invoke("apply", "apply.jsonl"); mutate(f); const count = f.writes().length;
    await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /NATIVE_|SYSTEM_HELM_/); assert.equal(f.writes().length, count);
  }
});

test("failed Helm job and mismatched projected values never trigger automatic reinstall or recovery mutation", async t => {
  for (const flag of ["failed", "badValues"]) {
    const f = await fixture(t); f.flags[flag] = true;
    await assert.rejects(f.invoke("apply", "apply.jsonl"), /NATIVE_TRAEFIK_/);
    await assert.rejects(f.invoke("rollback", "rollback.jsonl"), /NATIVE_TRAEFIK_/);
    assert.equal(f.writes().length, 1);
    const journal = await readFile(f.dir + "/apply.jsonl", "utf8"); assert.equal(journal.includes("FOREIGN_VALUES"), false);
    assert.equal(journal.includes("valuesContent"), false);
  }
});

test("full source is rechecked after server dry-run before the first HCC write", async t => {
  const f = await fixture(t); f.flags.renderDrift = true;
  await assert.rejects(f.invoke("apply", "apply.jsonl"), /SYSTEM_HELM_OBJECT_DRIFT/);
  assert.equal(f.writes().length, 0);
});
