#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { open } from "node:fs/promises";
import { promisify } from "node:util";
import { pathToFileURL } from "node:url";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { inspectSource } from "./application-source.mjs";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";

const execute = promisify(execFile);
const namespace = "kodex-system";
const allowed = new Set(["control-plane", "control-api-gateway"]);
const fail = (ok, code) => { if (!ok) throw Error(code); };
const equal = (a, b) => fingerprint(a) === fingerprint(b);

export function hotSpec(deployment, recovery, source) {
  const name = deployment?.metadata?.name;
  fail(allowed.has(name) && deployment.metadata.namespace === namespace && deployment.metadata.uid &&
    deployment.spec.template.metadata.labels["kodex.dev/security-profile"] === "trusted-cluster" &&
    recovery?.name === name && recovery.uid === deployment.metadata.uid && recovery.recovery?.mode === "RESTORE_SOURCE",
  "HOT_RESTORE_BOUNDARY_INVALID");
  const after = deployment.spec.template.metadata.labels["kodex.dev/local-profile"] === "hot-reload" ?
    structuredClone(deployment.spec) : applyReverseSpec(deployment.spec, recovery.recovery.inverse);
  // Это forward development transition, не откат реплик, конфигурации или сети.
  after.replicas = deployment.spec.replicas;
  after.strategy = structuredClone(deployment.spec.strategy);
  if (deployment.spec.template.spec.nodeSelector) after.template.spec.nodeSelector = structuredClone(deployment.spec.template.spec.nodeSelector);
  const original = deployment.spec.template.spec.containers.find(c => c.name === name);
  const main = after.template.spec.containers.find(c => c.name === name);
  fail(main?.command?.[0] === "/workspace/tools/dev/run-go-hot-reload.sh" &&
    /^docker\.io\/library\/golang:[^@]+@sha256:[a-f0-9]{64}$/.test(main.image), "HOT_RESTORE_TOOLCHAIN_INVALID");
  const nonDev = values => (values ?? []).filter(e => !/^(?:GOMODCACHE|GOCACHE|GOWORK|GOTOOLCHAIN|GOTMPDIR|HOME|KODEX_DEV_[A-Z0-9_]+)$/.test(e.name));
  fail(equal(nonDev(main.env), nonDev(original.env)), "HOT_RESTORE_APPLICATION_CONFIG_DRIFT");
  main.env = [...(main.env ?? []).filter(e => e.name !== "KODEX_DEV_POD_UID"),
    { name: "KODEX_DEV_POD_UID", valueFrom: { fieldRef: { apiVersion: "v1", fieldPath: "metadata.uid" } } }];
  fail(new Set(main.env.map(e => e.name)).size === main.env.length, "HOT_RESTORE_DUPLICATE_ENV");
  main.resources = { requests: { cpu: "200m", memory: "512Mi" }, limits: { cpu: "2", memory: "2Gi" } };
  const mount = main.volumeMounts.find(m => m.mountPath === "/workspace");
  const volume = after.template.spec.volumes.find(v => v.name === mount?.name);
  fail(mount?.readOnly === true && volume?.hostPath?.type === "Directory", "HOT_RESTORE_SOURCE_BOUNDARY_INVALID");
  volume.hostPath.path = source.path;
  const annotations = after.template.metadata.annotations;
  annotations["kodex.dev/source-root"] = source.path;
  annotations["kodex.dev/source-revision"] = source.revision;
  annotations["kodex.dev/source-content-sha256"] = source.contentSHA256;
  annotations["kodex.dev/deployment-profile"] = "web-only";
  after.template.metadata.labels["kodex.dev/local-profile"] = "hot-reload";
  // Никакие sidecars, init, runtime grants, Secrets, PVC и Service не меняются.
  fail(equal(after.template.spec.containers.filter(c => c.name !== name), deployment.spec.template.spec.containers.filter(c => c.name !== name)) &&
    equal(after.template.spec.initContainers ?? [], deployment.spec.template.spec.initContainers ?? []), "HOT_RESTORE_SIDECAR_DRIFT");
  const nonDevVolumes = values => values.filter(v => !/^dev-|^kodex-local-private-source-mask$/.test(v.name));
  fail(equal(nonDevVolumes(after.template.spec.volumes), nonDevVolumes(deployment.spec.template.spec.volumes)), "HOT_RESTORE_STORAGE_DRIFT");
  return after;
}

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) { const k = args.shift(); fail(["--context", "--recovery-plan", "--source", "--targets", "--plan", "--output", "--approved-plan-sha"].includes(k) &&
    !Object.hasOwn(options, k) && args.length, "HOT_RESTORE_ARGUMENTS_INVALID"); options[k] = args.shift(); }
  fail(["plan", "apply", "readback"].includes(mode) && options["--output"] && options["--context"] === "k3d-kodex", "HOT_RESTORE_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const kube = async (argv, input) => {
    try {
      const call = input === undefined ? execute("kubectl", ["--context", options["--context"], "--request-timeout=30s", ...argv], { env, timeout: 35000, maxBuffer: 8 << 20 }) :
        execute("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-hot-restore", "--context", options["--context"], "--request-timeout=30s", ...argv],
        { env, timeout: 35000, maxBuffer: 8 << 20 });
      if (input !== undefined) { call.child.stdin.on("error", () => {}); call.child.stdin.end(input); }
      return (await call).stdout;
    } catch { throw Error("HOT_RESTORE_KUBERNETES_FAILED_OR_UNKNOWN"); }
  };
  const get = async (kind, name, ns = namespace) => JSON.parse(await kube(["-n", ns, "get", kind, name, "-o", "json"]));
  const cluster = await get("namespace", "kube-system", "default");
  const inspect = async path => {
    const source = inspectSource(path);
    const tree = (await execute("git", ["-C", path, "rev-parse", "HEAD^{tree}"], { env, timeout: 10000 })).stdout.trim();
    return { path, revision: source.revision, contentSHA256: createHash("sha256").update(`BASE_TREE\0${tree}\0`).digest("hex") };
  };
  if (mode === "plan") {
    const recovery = await readPrivateJSON(options["--recovery-plan"]), source = await inspect(options["--source"]);
    fail(recovery.context === options["--context"] && recovery.clusterUID === cluster.metadata.uid, "HOT_RESTORE_CLUSTER_DRIFT");
    const names = options["--targets"].split(",");
    fail(names.length > 0 && new Set(names).size === names.length && names.every(n => allowed.has(n)), "HOT_RESTORE_TARGET_INVALID");
    const targets = [];
    for (const name of names) {
      const current = await get("deployment", name);
      fail(current.status.observedGeneration === current.metadata.generation && current.status.availableReplicas === current.spec.replicas &&
        current.status.updatedReplicas === current.spec.replicas && current.spec.replicas === 2, "HOT_RESTORE_TARGET_NOT_READY");
      const afterSpec = hotSpec(current, recovery.targets.find(t => t.name === name), source);
      const afterLabels = { ...current.metadata.labels, "kodex.dev/local-profile": "hot-reload" };
      const patch = [{ op: "test", path: "/metadata/uid", value: current.metadata.uid },
        { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
        { op: "test", path: "/spec", value: current.spec }, { op: "test", path: "/metadata/labels", value: current.metadata.labels },
        { op: "add", path: "/metadata/labels", value: afterLabels }, { op: "replace", path: "/spec", value: afterSpec }];
      await kube(["-n", namespace, "patch", "deployment", name, "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "name"], JSON.stringify(patch));
      targets.push({ name, uid: current.metadata.uid, beforeSpec: current.spec, afterSpec, beforeLabels: current.metadata.labels, afterLabels });
    }
    const plan = { schema: "kodex.hot-go-restore/v2", context: options["--context"], clusterUID: cluster.metadata.uid, source,
      recoveryPlan: options["--recovery-plan"], recoverySHA256: fingerprint(recovery), targets };
    const output = await open(options["--output"], "wx", 0o600);
    try { await output.writeFile(JSON.stringify(plan)); await output.sync(); } finally { await output.close(); }
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(plan), sourceSHA: source.revision, targets: names }) + "\n");
    return;
  }
  const plan = await readPrivateJSON(options["--plan"]);
  fail(plan.schema === "kodex.hot-go-restore/v2" && plan.context === options["--context"] && plan.clusterUID === cluster.metadata.uid &&
    equal(await inspect(plan.source.path), plan.source) && plan.targets?.length > 0 && plan.targets.every(t => allowed.has(t.name)), "HOT_RESTORE_PLAN_DRIFT");
  const recovery = await readPrivateJSON(plan.recoveryPlan);
  fail(fingerprint(recovery) === plan.recoverySHA256, "HOT_RESTORE_RECOVERY_DRIFT");
  for (const target of plan.targets) fail(equal(hotSpec({ metadata: { name: target.name, namespace, uid: target.uid }, spec: target.beforeSpec },
    recovery.targets.find(t => t.name === target.name), plan.source), target.afterSpec) &&
    equal(target.afterLabels, { ...target.beforeLabels, "kodex.dev/local-profile": "hot-reload" }), "HOT_RESTORE_PLAN_DRIFT");
  if (mode === "apply") fail(options["--approved-plan-sha"] === fingerprint(plan), "HOT_RESTORE_EXACT_APPROVAL_REQUIRED");
  const output = await open(options["--output"], "wx", 0o600);
  const record = async row => { await output.writeFile(JSON.stringify(row) + "\n"); await output.sync(); };
  try {
    await record({ schema: "kodex.hot-go-restore-journal/v1", mode, planSHA256: fingerprint(plan), sourceSHA: plan.source.revision, status: "IN_PROGRESS" });
    for (const target of plan.targets) {
      let current = await get("deployment", target.name);
      fail(current.metadata.uid === target.uid && (equal(current.spec, target.afterSpec) || mode === "apply" && equal(current.spec, target.beforeSpec)) &&
        (equal(current.metadata.labels, target.afterLabels) || mode === "apply" && equal(current.metadata.labels, target.beforeLabels)), "HOT_RESTORE_TARGET_DRIFT");
      if (mode === "apply" && (!equal(current.spec, target.afterSpec) || !equal(current.metadata.labels, target.afterLabels))) {
        fail(equal(await inspect(plan.source.path), plan.source), "HOT_RESTORE_SOURCE_CHANGED");
        await record({ name: target.name, status: "PATCH_ATTEMPT", before: fingerprint(target.beforeSpec), after: fingerprint(target.afterSpec) });
        await kube(["-n", namespace, "patch", "deployment", target.name, "--type=json", "--patch-file=/dev/stdin", "-o", "name"], JSON.stringify([
          { op: "test", path: "/metadata/uid", value: target.uid }, { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
          { op: "test", path: "/spec", value: current.spec }, { op: "test", path: "/metadata/labels", value: current.metadata.labels },
          { op: "add", path: "/metadata/labels", value: target.afterLabels }, { op: "replace", path: "/spec", value: target.afterSpec }]));
        await record({ name: target.name, status: "PATCH_APPLIED" });
      }
      current = await get("deployment", target.name);
      fail(current.metadata.uid === target.uid && equal(current.spec, target.afterSpec) && equal(current.metadata.labels, target.afterLabels), "HOT_RESTORE_READBACK_MISMATCH");
      await record({ name: target.name, status: "SPEC_READBACK", ready: current.status.availableReplicas ?? 0 });
    }
    await record({ status: "PASS", phase: "SPEC_ONLY", servingRuntime: "NOT_RUN" });
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "SPEC_ONLY", servingRuntime: "NOT_RUN" }) + "\n");
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message) ? error.message : "HOT_RESTORE_FAILED" }); throw error; }
  finally { await output.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main(process.argv.slice(2)).catch(error => {
  process.stderr.write(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message) ? error.message : "HOT_RESTORE_FAILED" }) + "\n"); process.exitCode = 1;
});
