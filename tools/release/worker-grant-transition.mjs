#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { openSync, writeSync, fsyncSync, closeSync, readFileSync, mkdtempSync, writeFileSync, rmSync, lstatSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint, workerGrantAgents } from "./scoped-release.mjs";
import { inspectSource } from "./application-source.mjs";

const namespace = "kodex-system";
const instanceEnv = "PLATFORM_WORKER_GRANT_INSTANCE_ID";
const formatAnnotation = "kodex.dev/worker-grant-format";
const compatibleReaderCommit = "29652a817cc548282f03747da3ea98717f9e32af";
const workers = new Set(["control-plane", "secret-broker", "email-bridge", "runtime-controller",
  "integration-gateway", "interaction-gateway", "automation-scheduler", "session-archive", "role-image-builder"]);
const phases = new Set(["activate", "overlap", "rolling", "settle"]);
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
function requireValue(value, code) { if (!value) throw new Error(code); }

function healthy(deployment) {
  const { metadata: m, spec: s, status: t } = deployment;
  requireValue(deployment.apiVersion === "apps/v1" && deployment.kind === "Deployment" &&
    workers.has(m?.name) && m.namespace === namespace && uuid.test(m.uid) && /^\d+$/.test(m.resourceVersion) &&
    !m.deletionTimestamp && m.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    m.labels?.["kodex.dev/local-profile"] === "hot-reload", "EXACT_HOT_RELOAD_WORKER_REQUIRED");
  requireValue(!s?.paused && Number.isSafeInteger(s?.replicas) && s.replicas > 0 &&
    t?.observedGeneration >= m.generation && t.replicas === s.replicas &&
    t.updatedReplicas === s.replicas && t.readyReplicas === s.replicas &&
    t.availableReplicas === s.replicas, "COMPLETED_HEALTHY_ROLLOUT_REQUIRED");
}

// Deployment rollout может завершиться до удаления старого terminating Pod.
// После единственного PATCH ждём только точный readback, не повторяя mutation.
export async function waitForWorkerDrain({ readDeployment, readPods, uid, specSHA256, timeoutMs = 300_000, pollIntervalMs = 250 }) {
  requireValue(Number.isSafeInteger(timeoutMs) && timeoutMs > 0 && timeoutMs <= 300_000 &&
    Number.isSafeInteger(pollIntervalMs) && pollIntervalMs > 0, "BOUNDED_DRAIN_WAIT_REQUIRED");
  const deadline = performance.now() + timeoutMs;
  while (true) {
    const deployment = await readDeployment();
    requireValue(deployment.metadata.uid === uid && fingerprint(deployment.spec) === specSHA256, "TRANSITION_READBACK_MISMATCH");
    requireValue(performance.now() < deadline, "WORKER_DRAIN_TIMEOUT");
    try {
      healthy(deployment);
      const pods = await readPods(deployment);
      requireValue(performance.now() < deadline, "WORKER_DRAIN_TIMEOUT");
      return { deployment, pods };
    } catch (error) {
      if (!["COMPLETED_HEALTHY_ROLLOUT_REQUIRED", "ALL_READER_PODS_REQUIRED"].includes(error?.message)) throw error;
    }
    const remaining = deadline - performance.now();
    requireValue(remaining > 0, "WORKER_DRAIN_TIMEOUT");
    await new Promise((done) => setTimeout(done, Math.min(pollIntervalMs, remaining)));
  }
}

export function requireInstanceWriters(template) {
  const agents = workerGrantAgents(template.spec);
  requireValue(agents.length > 0 && template.metadata?.annotations?.[formatAnnotation] === "2", "INSTANCE_GRANTS_REQUIRED");
  for (const agent of agents) {
    const fields = (agent.env ?? []).filter((item) => item.name === instanceEnv);
    requireValue(fields.length === 1 && fields[0].value === undefined &&
      fields[0].valueFrom?.fieldRef?.fieldPath === "metadata.uid" &&
      [undefined, "v1"].includes(fields[0].valueFrom.fieldRef.apiVersion), "POD_UID_REQUIRED");
  }
}

// Два чтения доказывают продвижение обоих durable instances, а не только Ready Pods.
export function verifyOverlap(before, after, workload, podUIDs) {
  requireValue(podUIDs.length === 2 && new Set(podUIDs).size === 2 && podUIDs.every((id) => uuid.test(id)), "TWO_PODS_REQUIRED");
  const elapsed = Date.parse(after.at) - Date.parse(before.at);
  requireValue(elapsed >= 90_000 && elapsed <= 300_000, "BOUNDED_OBSERVATION_REQUIRED");
  const floor = (state) => state.floors.filter((row) => row.workload === workload);
  const oldFloor = floor(before), newFloor = floor(after);
  requireValue(oldFloor.length === 1 && newFloor.length === 1 &&
    oldFloor[0].generation === newFloor[0].generation, "GENERATION_CHANGED");
  for (const uid of podUIDs) {
    const select = (state) => state.instances.filter((row) => row.workload === workload && row.instance === uid);
    const oldRows = select(before), rows = select(after);
    requireValue(oldRows.length === 1 && rows.length === 1 && rows[0].generation === newFloor[0].generation &&
      oldRows[0].generation === rows[0].generation && rows[0].revision > oldRows[0].revision &&
      Date.parse(rows[0].expiresAt) > Date.parse(after.at) &&
      Date.parse(rows[0].updatedAt) > Date.parse(before.at), "BOTH_INSTANCES_MUST_ADVANCE");
  }
}

export function planWorkerTransition(deployment, phase) {
  healthy(deployment);
  requireValue(phases.has(phase), "INVALID_TRANSITION_PHASE");
  const { metadata, spec } = deployment;
  const next = structuredClone(spec);
  const agents = workerGrantAgents(next.template.spec);
  requireValue(agents.length > 0, "GRANT_WRITER_MISSING");
  if (phase === "activate") {
    requireValue(spec.replicas === 1 && fingerprint(spec.strategy) === fingerprint({ type: "Recreate" }), "SINGLE_RECREATE_REQUIRED");
    requireValue(!spec.template.metadata?.annotations?.[formatAnnotation] &&
      agents.every((agent) => !(agent.env ?? []).some((item) => item.name === instanceEnv)), "LEGACY_WRITERS_REQUIRED");
    next.template.metadata ??= {};
    next.template.metadata.annotations = { ...(next.template.metadata.annotations ?? {}), [formatAnnotation]: "2" };
    for (const agent of agents) {
      agent.env = [...(agent.env ?? []), { name: instanceEnv, valueFrom: { fieldRef: { apiVersion: "v1", fieldPath: "metadata.uid" } } }];
    }
  } else {
    requireInstanceWriters(spec.template);
    if (phase === "overlap" || phase === "rolling") {
      requireValue(fingerprint(spec.strategy) === fingerprint({ type: "Recreate" }) &&
        spec.replicas === (phase === "overlap" ? 1 : 2), "TRANSITION_ORDER_REQUIRED");
      if (phase === "overlap") next.replicas = 2;
      else next.strategy = { type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } };
    } else {
      requireValue(spec.replicas === 2 && fingerprint(spec.strategy) === fingerprint({ type: "RollingUpdate", rollingUpdate: { maxUnavailable: 0, maxSurge: 1 } }), "TRANSITION_ORDER_REQUIRED");
      next.replicas = 1;
    }
  }
  // Только собственная security-фаза. Application source, images, keys и floors сохраняются.
  const patch = [
    { op: "test", path: "/metadata/uid", value: metadata.uid },
    { op: "test", path: "/metadata/resourceVersion", value: metadata.resourceVersion },
  ];
  if (phase === "activate") {
    patch.push({ op: "add", path: "/spec/template/metadata/annotations", value: next.template.metadata.annotations });
    for (const field of ["containers", "initContainers"]) {
      for (const [index, agent] of (next.template.spec[field] ?? []).entries()) {
        if (agents.includes(agent)) patch.push({ op: "add", path: `/spec/template/spec/${field}/${index}/env`, value: agent.env });
      }
    }
  } else if (phase === "rolling") patch.push({ op: "replace", path: "/spec/strategy", value: next.strategy });
  else patch.push({ op: "replace", path: "/spec/replicas", value: next.replicas });
  return { target: metadata.name, phase, uid: metadata.uid, resourceVersion: metadata.resourceVersion,
    beforeSpecSHA256: fingerprint(spec), afterSpecSHA256: fingerprint(next), patch };
}

function privateRecord(path, value) {
  const fd = openSync(path, "wx", 0o600);
  try { writeSync(fd, `${JSON.stringify(value)}\n`); fsyncSync(fd); } finally { closeSync(fd); }
}

export function requireUnchangedPlan(saved, current) {
  requireValue(saved.version === 1 && uuid.test(saved.id) &&
    ["context", "target", "phase", "uid", "resourceVersion", "beforeSpecSHA256", "afterSpecSHA256", "clusterUID", "namespaceUID", "handoffProofSHA256"]
      .every((key) => saved[key] === current[key]) &&
    fingerprint(saved.readers) === fingerprint(current.readers), "PLAN_PRECONDITION_CHANGED");
}

async function main(args) {
  const command = args.shift(), options = {};
  requireValue(["inspect", "plan", "apply"].includes(command), "INVALID_COMMAND");
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--target", "--phase", "--output", "--plan", "--evidence", "--confirm", "--handoff-proof"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "INVALID_ARGUMENTS"); options[key] = args.shift();
  }
  requireValue(options["--context"] && workers.has(options["--target"]), "CONTEXT_AND_TARGET_REQUIRED");
  const prefix = ["--context", options["--context"], "--namespace", namespace];
  const kubectl = (args, input) => execFileSync("kubectl", [...prefix, ...args], {
    encoding: "utf8", timeout: 310_000, maxBuffer: 16 << 20, input, stdio: [input ? "pipe" : "ignore", "pipe", "pipe"],
  });
  const get = (kind, name) => JSON.parse(kubectl(["get", kind, name, "-o", "json"]));
  const readDB = () => JSON.parse(kubectl(["exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"], readFileSync(new URL("./worker-grant-readback.sql", import.meta.url), "utf8")));
  const clusterUID = get("namespace", "kube-system").metadata.uid;
  const namespaceUID = get("namespace", namespace).metadata.uid;
  requireValue(uuid.test(clusterUID) && uuid.test(namespaceUID), "CLUSTER_IDENTITY_REQUIRED");
  const sourceCache = new Map();
  const source = (podSpec, container) => {
    const mounts = (container.volumeMounts ?? []).filter((item) => item.mountPath === "/workspace");
    requireValue(mounts.length === 1 && mounts[0].readOnly === true && !mounts[0].subPath && !mounts[0].subPathExpr, "EXACT_READER_SOURCE_REQUIRED");
    const volumes = (podSpec.volumes ?? []).filter((item) => item.name === mounts[0].name);
    requireValue(volumes.length === 1 && volumes[0].hostPath?.path, "EXACT_READER_SOURCE_REQUIRED");
    const path = volumes[0].hostPath.path;
    if (!sourceCache.has(path)) {
      const inspected = inspectSource(path);
      execFileSync("git", ["-C", path, "merge-base", "--is-ancestor", compatibleReaderCommit, inspected.revision], { stdio: "pipe", timeout: 10_000 });
      sourceCache.set(path, inspected.revision);
    }
    return { path, revision: sourceCache.get(path) };
  };
  const podsFor = (deployment) => {
    healthy(deployment);
    const sets = JSON.parse(kubectl(["get", "replicasets", "-o", "json"])).items
      .filter((rs) => rs.metadata.ownerReferences?.some((owner) => owner.controller && owner.uid === deployment.metadata.uid));
    const ids = new Set(sets.map((rs) => rs.metadata.uid));
    const pods = JSON.parse(kubectl(["get", "pods", "-o", "json"])).items
      .filter((pod) => pod.metadata.ownerReferences?.some((owner) => owner.controller && ids.has(owner.uid)));
    requireValue(pods.length === deployment.spec.replicas && pods.every((pod) => !pod.metadata.deletionTimestamp &&
      pod.status?.phase === "Running" && pod.status.conditions?.some((condition) => condition.type === "Ready" && condition.status === "True")), "ALL_READER_PODS_REQUIRED");
    return pods;
  };
  const readers = () => {
    const cp = get("deployment", "control-plane");
    return { uid: cp.metadata.uid, specSHA256: fingerprint(cp.spec), pods: podsFor(cp).map((pod) => {
      const containers = pod.spec.containers.filter((c) => c.name === "control-plane");
      requireValue(containers.length === 1, "READER_CONTAINER_REQUIRED");
      return { uid: pod.metadata.uid, source: source(pod.spec, containers[0]) };
    }).sort((a, b) => a.uid.localeCompare(b.uid)) };
  };
  const deployment = get("deployment", options["--target"]);
  const readerState = readers();
  const database = readDB();
  const pods = podsFor(deployment);
  for (const pod of pods) for (const agent of workerGrantAgents(pod.spec)) source(pod.spec, agent);
  const snapshot = { at: new Date().toISOString(), clusterUID, namespaceUID, readers: readerState,
    target: deployment.metadata.name, uid: deployment.metadata.uid, specSHA256: fingerprint(deployment.spec),
    pods: pods.map((pod) => ({ uid: pod.metadata.uid, name: pod.metadata.name })), database };
  if (command === "inspect") {
    requireValue(options["--output"] && !options["--confirm"], "INSPECTION_OUTPUT_REQUIRED");
    privateRecord(options["--output"], snapshot);
    process.stdout.write(`${JSON.stringify({ status: "PASS", target: snapshot.target, pods: snapshot.pods.length, readers: readerState.pods.length, instanceCount: database.instances.filter((row) => row.workload === snapshot.target).length })}\n`);
    return;
  }
  const saved = command === "apply" ? JSON.parse(readFileSync(options["--plan"], "utf8")) : null;
  const phase = saved?.phase ?? options["--phase"];
  const plan = planWorkerTransition(deployment, phase);
  let handoffProofSHA256;
  if (options["--handoff-proof"]) {
    requireValue(phase === "rolling" && snapshot.target === "runtime-controller", "HANDOFF_PROFILE_MISMATCH");
    const file = lstatSync(options["--handoff-proof"]);
    requireValue(file.isFile() && (file.mode & 0o077) === 0 && file.size > 0 && file.size <= 1 << 20, "PRIVATE_HANDOFF_PROOF_REQUIRED");
    const proof = JSON.parse(readFileSync(options["--handoff-proof"], "utf8"));
    const { verifyLeaderHandoff, describePodProcesses } = await import("./runtime-leader-handoff.mjs");
    snapshot.leaderUID = get("lease", "runtime-controller-leader").spec.holderIdentity;
    snapshot.podDetails = pods.map(describePodProcesses);
    snapshot.activeJobs = JSON.parse(kubectl(["--namespace", "kodex-runtime", "get", "jobs", "-o", "json"])).items
      .filter((job) => !job.status?.conditions?.some((item) => ["Complete", "Failed"].includes(item.type) && item.status === "True")).length;
    verifyLeaderHandoff(proof, snapshot);
    handoffProofSHA256 = fingerprint(proof);
  }
  const identity = { clusterUID, namespaceUID, readers: readerState };
  const safePlan = { version: 1, id: saved?.id ?? randomUUID(), at: new Date().toISOString(), context: options["--context"],
    ...identity, ...Object.fromEntries(Object.entries(plan).filter(([key]) => key !== "patch")),
    ...(handoffProofSHA256 ? { handoffProofSHA256 } : {}) };
  if (command === "plan") {
    requireValue(options["--output"] && !options["--confirm"], "PLAN_OUTPUT_REQUIRED");
    privateRecord(options["--output"], safePlan);
    process.stdout.write(`${JSON.stringify({ status: "PLANNED", ...safePlan })}\n`);
    return;
  }
  requireValue(options["--confirm"] === "TRANSITION-STAGING-WORKER-GRANTS" && options["--evidence"], "TRANSITION_CONFIRMATION_REQUIRED");
  requireUnchangedPlan(saved, safePlan);
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const record = (value) => { writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), id: safePlan.id, target: snapshot.target, phase, ...value })}\n`); fsyncSync(fd); };
  const directory = mkdtempSync(join(tmpdir(), "kodex-grant-transition-"));
  let applied = false;
  let patchAttempted = false;
  try {
    record({ status: "INTENT", plan: safePlan });
    if (phase === "rolling" && !handoffProofSHA256) {
      const probeReaders = async () => {
        if (snapshot.target !== "control-plane") return;
        const { probeControlPlaneReader } = await import("./control-plane-reader-probe.mjs");
        for (const expected of pods) {
          const actual = get("pod", expected.metadata.name);
          requireValue(actual.metadata.uid === expected.metadata.uid, "READER_POD_CHANGED");
          record({ status: "READER_PROBE_ATTEMPT", podUID: actual.metadata.uid });
          record({ status: "READER_PROBE", result: await probeControlPlaneReader(actual) });
        }
      };
      // CP own-grant readiness вызывается адресно, а не через sticky Service connection.
      await probeReaders();
      const before = readDB(); record({ status: "OBSERVATION_START", database: before });
      process.stdout.write("{\"status\":\"OBSERVING_DURABLE_INSTANCES\",\"seconds\":95}\n");
      await new Promise((done) => setTimeout(done, 95_000));
      await probeReaders();
      const after = readDB(); record({ status: "OBSERVATION_END", database: after });
      verifyOverlap(before, after, snapshot.target, pods.map((pod) => pod.metadata.uid));
      sourceCache.clear();
      requireValue(fingerprint(readers()) === fingerprint(readerState), "READERS_CHANGED_DURING_OBSERVATION");
      const current = get("deployment", snapshot.target);
      requireValue(current.metadata.resourceVersion === plan.resourceVersion, "PLAN_PRECONDITION_CHANGED");
      requireValue(fingerprint(podsFor(current).map((pod) => pod.metadata.uid).sort()) === fingerprint(pods.map((pod) => pod.metadata.uid).sort()), "PODS_CHANGED_DURING_OBSERVATION");
    }
    const path = join(directory, "patch.json"); writeFileSync(path, JSON.stringify(plan.patch), { mode: 0o600, flag: "wx" });
    record({ status: "PATCH_ATTEMPT" });
    patchAttempted = true;
    kubectl(["patch", "deployment", snapshot.target, "--type=json", "--patch-file", path]);
    const actual = get("deployment", snapshot.target);
    requireValue(actual.metadata.uid === plan.uid && fingerprint(actual.spec) === plan.afterSpecSHA256, "TRANSITION_READBACK_MISMATCH");
    applied = true;
    record({ status: "APPLIED", afterSpecSHA256: plan.afterSpecSHA256 });
    kubectl(["rollout", "status", `deployment/${snapshot.target}`, "--timeout=300s"]);
    record({ status: "WAITING_FOR_POD_DRAIN" });
    const final = await waitForWorkerDrain({ readDeployment: () => get("deployment", snapshot.target), readPods: podsFor,
      uid: plan.uid, specSHA256: plan.afterSpecSHA256 });
    record({ status: "PASS", pods: final.pods.map((pod) => ({ uid: pod.metadata.uid, name: pod.metadata.name })), database: readDB() });
    process.stdout.write(`${JSON.stringify({ status: "PASS", id: safePlan.id, target: snapshot.target, phase })}\n`);
  } catch {
    record({ status: !patchAttempted || applied ? "FAIL" : "UNKNOWN", code: "AUTHORITATIVE_READBACK_REQUIRED" }); throw new Error("AUTHORITATIVE_READBACK_REQUIRED");
  } finally { closeSync(fd); rmSync(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch(() => { process.stderr.write("Worker grant transition failed; preserve evidence and inspect authoritative state\n"); process.exitCode = 1; });
}
