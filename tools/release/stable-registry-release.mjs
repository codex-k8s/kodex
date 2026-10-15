#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { prepareRegistry, registryRoles, registryIntent, httpStateName } from "./stable-registry-model.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { activeDeploymentPods } from "./stable-archive-transition.mjs";
import { requireIdle } from "./runner-policy-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url)), ns = "kodex-system";
const names = Object.keys(registryRoles);
const stateNames = new Set(names.filter(n => !registryRoles[n].readOnly).map(httpStateName));
function requireValue(value, code) { if (!value) throw Error(code); }
async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(JSON.stringify(value) + "\n"); await file.sync(); } finally { await file.close(); }
}
const withoutPatch = ({ patch: _patch, ...value }) => value;

export async function releaseRegistry(args, commandPort) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--bootstrap-plan", "--plan", "--output", "--approved-plan-sha", "--journal"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "REGISTRY_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  const context = options["--context"], output = options["--output"];
  requireValue(["plan", "apply", "readback", "inspect", "rollback"].includes(mode) && /^k3d-[a-z0-9-]+$/.test(context ?? "") &&
    !/prod(?:uction)?/i.test(context) && output, "REGISTRY_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = commandPort ?? (async (program, argv, input, timeout = 35000) => {
    try {
      const pending = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 8 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("REGISTRY_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const kube = (argv, input, timeout) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=30s", ...argv], undefined, timeout) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-registry-pipe", "--context", context, "--request-timeout=30s", ...argv], input, timeout);
  const get = async (kind, name, optional = false) => {
    const value = await kube(["-n", ns, "get", kind, name, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return value.trim() ? JSON.parse(value) : null;
  };
  const source = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "REGISTRY_CLEAN_SOURCE_REQUIRED");
  const clusterUID = (await get("namespace", "kube-system")).metadata.uid, appUID = (await get("namespace", ns)).metadata.uid;
  const storage = async deployment => {
    const volumes = deployment.spec.template.spec.volumes.filter(v => v.persistentVolumeClaim);
    requireValue(volumes.length === 1, "REGISTRY_EXACT_STORAGE_REQUIRED");
    const claim = await get("pvc", volumes[0].persistentVolumeClaim.claimName);
    requireValue(claim.status?.phase === "Bound" && claim.spec.volumeName, "REGISTRY_STORAGE_UNBOUND");
    const pv = await get("pv", claim.spec.volumeName), terms = pv.spec.nodeAffinity?.required?.nodeSelectorTerms;
    const expressions = terms?.[0]?.matchExpressions;
    const node = expressions?.find(e => e.key === "kubernetes.io/hostname" && e.operator === "In" && e.values?.length === 1)?.values[0];
    requireValue(terms?.length === 1 && node, "REGISTRY_STORAGE_NODE_UNKNOWN");
    return { claim: claim.metadata.name, bound: true, accessModes: claim.spec.accessModes, node,
      commitment: fingerprint({ claimUID: claim.metadata.uid, claimSpec: claim.spec, pvUID: pv.metadata.uid, pvSpec: pv.spec }) };
  };
  const pods = async deployment => {
    requireValue(!deployment.spec.selector.matchExpressions?.length && Object.keys(deployment.spec.selector.matchLabels ?? {}).length,
      "REGISTRY_SELECTOR_UNSUPPORTED");
    const selector = Object.entries(deployment.spec.selector.matchLabels).sort().map(([k, v]) => `${k}=${v}`).join(",");
    const list = JSON.parse(await kube(["-n", ns, "get", "replicasets,pods", "-l", selector, "-o", "json"]));
    return activeDeploymentPods(list, deployment.metadata.uid);
  };
  const dependencies = async () => {
    const list = JSON.parse(await kube(["-n", ns, "get", "deployments", "-o", "json"]));
    return list.items.filter(d => !names.includes(d.metadata.name)).map(d => ({ name: d.metadata.name, uid: d.metadata.uid,
      specSHA256: fingerprint(d.spec) })).sort((a, b) => a.name.localeCompare(b.name, "en"));
  };
  const references = async () => {
    const refs = new Map();
    for (const name of names) {
      const d = await get("deployment", name), p = d.spec.template.spec;
      const add = (kind, name) => { if (name) refs.set(kind + "/" + name, { kind, name }); };
      add("serviceaccount", p.serviceAccountName);
      for (const v of p.volumes ?? []) {
        add("secret", v.secret?.secretName); add("configmap", v.configMap?.name);
        for (const source of v.projected?.sources ?? []) { add("secret", source.secret?.name); add("configmap", source.configMap?.name); }
      }
      for (const s of p.imagePullSecrets ?? []) add("secret", s.name);
      for (const c of [...p.containers, ...(p.initContainers ?? [])]) {
        for (const e of c.env ?? []) { add("secret", e.valueFrom?.secretKeyRef?.name); add("configmap", e.valueFrom?.configMapKeyRef?.name); }
        for (const e of c.envFrom ?? []) { add("secret", e.secretRef?.name); add("configmap", e.configMapRef?.name); }
      }
    }
    const rows = [];
    for (const ref of [...refs.values()].filter(r => !(r.kind === "secret" && stateNames.has(r.name))).sort((a, b) => (a.kind + a.name).localeCompare(b.kind + b.name, "en"))) {
      const raw = await kube(["-n", ns, "get", ref.kind, ref.name, "-o", 'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}']);
      requireValue(/^[a-zA-Z0-9-]+\|[0-9]+$/.test(raw.trim()), "REGISTRY_REFERENCE_INVALID"); rows.push({ ...ref, identity: raw.trim() });
    }
    const routes = JSON.parse(await kube(["-n", ns, "get", "services,networkpolicies", "-o", "json"]));
    for (const r of routes.items.filter(r => r.metadata.name.startsWith("kodex-image-registry") || r.metadata.name === "kodex-image-supply-chain-deny-all"))
      rows.push({ kind: r.kind, name: r.metadata.name, uid: r.metadata.uid, hash: fingerprint({ spec: r.spec, annotations: r.metadata.annotations ?? {} }) });
    return rows;
  };
  let cacheSequence = 0;
  const cacheProof = async (bootstrapPath, strict) => {
    const bootstrap = await readPrivateJSON(bootstrapPath);
    requireValue(bootstrap.schema === "kodex.stable-pull-bootstrap/v1" && bootstrap.executorSHA === source && bootstrap.context === context &&
      bootstrap.clusterUID === clusterUID && bootstrap.appUID === appUID, "REGISTRY_BOOTSTRAP_IDENTITY_INVALID");
    const observed = JSON.parse(await run(process.execPath, [root + "tools/release/stable-pull-bootstrap.mjs", strict ? "readback" : "inspect", "--context", context,
      "--plan", bootstrapPath, "--output", output + ".cache-" + cacheSequence++ + ".json"]));
    requireValue(strict ? observed.status === "PASS" && observed.phase === "NODE_CACHE_ONLY" : observed.status === "OBSERVED",
      "REGISTRY_BOOTSTRAP_PROOF_INVALID");
    return bootstrap;
  };
  const prepare = async bootstrap => {
    const targets = [];
    for (const name of names) {
      const current = await get("deployment", name), item = prepareRegistry(current, await storage(current), bootstrap.image.image, source);
      const serving = await pods(current);
      requireValue(current.status?.observedGeneration >= current.metadata.generation && current.status.replicas === 1 && current.status.updatedReplicas === 1 &&
        current.status.readyReplicas === 1 && current.status.availableReplicas === 1 && serving.length === 1 &&
        !serving[0].metadata.deletionTimestamp && serving[0].status?.conditions?.some(c => c.type === "Ready" && c.status === "True"), "REGISTRY_SOURCE_NOT_READY");
      const servingDigests = {};
      for (const c of current.spec.template.spec.containers) {
        const status = serving[0].status.containerStatuses?.find(s => s.name === c.name && s.ready);
        const digest = status?.imageID?.match(/sha256:[a-f0-9]{64}$/)?.[0];
        requireValue(digest && serving[0].spec.containers.some(actual => actual.name === c.name && actual.image === c.image), "REGISTRY_SOURCE_SERVING_INVALID");
        servingDigests[c.name] = digest;
      }
      const beforeServingDigests = { ...servingDigests };
      if (name === "kodex-image-registry-pull") servingDigests["pull-authorizer"] = bootstrap.image.digest;
      const dry = JSON.parse(await kube(["-n", ns, "patch", "deployment", name, "--type=json", "--dry-run=server", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify(item.patch)));
      requireValue(fingerprint(dry.spec) === item.afterSpecSHA256, "REGISTRY_SERVER_DEFAULTS_MISMATCH");
      if (item.secretName) requireValue(await get("secret", item.secretName, true) === null, "REGISTRY_HTTP_STATE_COLLISION");
      targets.push({ ...withoutPatch(item), servingDigests, beforeServingDigests });
    }
    return targets;
  };
  if (mode === "plan") {
    requireValue(options["--bootstrap-plan"] && !options["--plan"] && !options["--journal"] && !options["--approved-plan-sha"], "REGISTRY_ARGUMENTS_INVALID");
    const bootstrap = await cacheProof(options["--bootstrap-plan"], false);
    const plan = { schema: "kodex.stable-registry/v1", executorSHA: source, context, clusterUID, appUID,
      bootstrapPath: options["--bootstrap-plan"], bootstrapSHA256: fingerprint(bootstrap), targets: await prepare(bootstrap),
      dependencies: await dependencies(), references: await references() };
    await createPrivate(output, plan); process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(plan) }) + "\n"); return;
  }
  requireValue(options["--plan"] && !options["--bootstrap-plan"], "REGISTRY_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(options["--plan"]), hash = fingerprint(plan);
  requireValue(plan.schema === "kodex.stable-registry/v1" && plan.executorSHA === source && plan.context === context && plan.clusterUID === clusterUID &&
    plan.appUID === appUID && fingerprint(plan.targets?.map(t => t.name)) === fingerprint(names), "REGISTRY_PLAN_IDENTITY_INVALID");
  for (const item of plan.targets) requireValue(item.secretName === (registryRoles[item.name].readOnly ? null : httpStateName(item.name)) &&
    item.storage?.claim === registryRoles[item.name].claim && Array.isArray(item.inverse) && item.inverse.length <= 512 &&
    ["beforeSpecSHA256", "afterSpecSHA256", "quiescedBeforeSHA256", "quiescedAfterSHA256", "labelsSHA256"].every(k => /^[a-f0-9]{64}$/.test(item[k] ?? "")),
    "REGISTRY_TARGET_SCOPE_INVALID");
  requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"], "REGISTRY_EXACT_OWNER_CONFIRMATION_REQUIRED");
  requireValue(mode === "apply" ? !options["--journal"] : options["--journal"], "REGISTRY_APPLY_JOURNAL_REQUIRED");
  const receipts = new Map(); let preserved;
  if (options["--journal"]) {
    const file = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = await file.stat(); requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 && (stat.mode & 0o777) === 0o600 && stat.size <= 1 << 20,
        "REGISTRY_PRIVATE_JOURNAL_REQUIRED");
      const rows = (await file.readFile("utf8")).trim().split("\n").map(JSON.parse), header = rows.shift();
      requireValue(rows.length <= 256 && header.schema === "kodex.stable-registry-journal/v1" && header.mode === "apply" && header.planSHA256 === hash,
        "REGISTRY_JOURNAL_MISMATCH"); preserved = header.preserved;
      for (const row of rows) {
        if (row.status === "SECRET_INTENT") {
          requireValue(plan.targets.some(t => t.secretName === row.name) && !receipts.has(row.name) && /^[a-f0-9]{64}$/.test(row.dataSHA256 ?? ""), "REGISTRY_JOURNAL_MISMATCH");
          receipts.set(row.name, { dataSHA256: row.dataSHA256 });
        } else if (row.status === "SECRET_CREATED") {
          requireValue(receipts.has(row.name) && !receipts.get(row.name).uid && row.uid, "REGISTRY_JOURNAL_MISMATCH"); receipts.get(row.name).uid = row.uid;
        }
      }
    } finally { await file.close(); }
  }
  const idle = async () => {
    const state = JSON.parse(await kube(["-n", ns, "exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"],
      await readFile(root + "tools/dev/supply-chain-owner-readback.sql", "utf8")));
    requireIdle(state); return { promotedPinsSHA256: state.promotedPinsSHA256, promotedArtifactCount: state.promotedArtifactCount };
  };
  const assertReferences = async () => requireValue(fingerprint(await references()) === fingerprint(plan.references), "REGISTRY_REFERENCE_DRIFT");
  const assertDependencies = async () => requireValue(fingerprint(await dependencies()) === fingerprint(plan.dependencies), "REGISTRY_DEPENDENT_DEPLOYMENT_DRIFT");
  const assertBootstrap = async strict => requireValue(fingerprint(await cacheProof(plan.bootstrapPath, strict)) === plan.bootstrapSHA256, "REGISTRY_BOOTSTRAP_DRIFT");
  const ownedSecret = secret => {
    const receipt = receipts.get(secret?.metadata?.name);
    return receipt && secret.metadata.uid && /^[0-9]+$/.test(secret.metadata.resourceVersion ?? "") && secret.metadata.namespace === ns && secret.metadata.annotations?.[registryIntent] === hash &&
      (!receipt.uid || receipt.uid === secret.metadata.uid) && secret.immutable === true && secret.type === "Opaque" &&
      !secret.metadata.ownerReferences?.length && !secret.metadata.finalizers?.length && Object.keys(secret.data ?? {}).join() === "secret" && fingerprint(secret.data) === receipt.dataSHA256;
  };
  const observe = async item => {
    const current = await get("deployment", item.name), sha = fingerprint(current.spec);
    requireValue(current.metadata.uid === item.uid && fingerprint(current.metadata.labels ?? {}) === item.labelsSHA256 &&
      [item.beforeSpecSHA256, item.afterSpecSHA256, item.quiescedBeforeSHA256, item.quiescedAfterSHA256].includes(sha) &&
      fingerprint(await storage(current)) === fingerprint(item.storage), "REGISTRY_DEPLOYMENT_OR_STORAGE_DRIFT");
    return current;
  };
  const patch = (current, spec) => kube(["-n", ns, "patch", "deployment", current.metadata.name, "--type=json", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify([
    { op: "test", path: "/metadata/uid", value: current.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
    { op: "test", path: "/spec", value: current.spec }, { op: "replace", path: "/spec", value: spec }]));
  const quiesce = async item => {
    let current = await observe(item);
    if (current.spec.replicas !== 0) await patch(current, { ...current.spec, replicas: 0 });
    const deadline = Date.now() + 120000;
    for (;;) {
      current = await observe(item);
      requireValue(current.spec.replicas === 0, "REGISTRY_QUIESCE_DRIFT");
      if (current.status?.observedGeneration >= current.metadata.generation && !(current.status.replicas ?? 0) && !(await pods(current)).length) return current;
      requireValue(Date.now() < deadline, "REGISTRY_QUIESCE_TIMEOUT"); await new Promise(resolve => setTimeout(resolve, 250));
    }
  };
  const prove = async (item, after) => {
    const d = await observe(item), expected = after ? item.afterSpecSHA256 : item.beforeSpecSHA256, count = after ? 2 : 1, list = await pods(d);
    requireValue(fingerprint(d.spec) === expected && d.status?.observedGeneration >= d.metadata.generation &&
      ["replicas", "readyReplicas", "availableReplicas", "updatedReplicas"].every(k => d.status[k] === count) && list.length === count &&
      list.every(p => !p.metadata.deletionTimestamp && p.spec.nodeName === item.storage.node && p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True") &&
        d.spec.template.spec.containers.every(c => p.spec.containers.some(a => a.name === c.name && a.image === c.image) && p.status.containerStatuses?.some(s => s.name === c.name && s.ready &&
          s.imageID?.endsWith((after ? item.servingDigests : item.beforeServingDigests)[c.name])))), "REGISTRY_SERVING_READBACK_INCOMPLETE");
    if (after && item.secretName) requireValue(ownedSecret(await get("secret", item.secretName)), "REGISTRY_HTTP_STATE_DRIFT");
  };
  await assertReferences();
  if (mode === "readback") {
    await assertBootstrap(true); for (const item of plan.targets) await prove(item, true);
    requireValue(fingerprint(await idle()) === fingerprint(preserved), "REGISTRY_PINS_CHANGED");
    const result = { status: "PASS", phase: "REGISTRY_TWO_READY", publicBrowser: "NOT_RUN", freshNodePull: "NOT_RUN" };
    await createPrivate(output, result); process.stdout.write(JSON.stringify(result) + "\n"); return;
  }
  if (mode === "inspect") {
    const rows = [];
    for (const item of plan.targets) { const d = await observe(item); rows.push({ name: item.name, specSHA256: fingerprint(d.spec), replicas: d.spec.replicas }); }
    for (const item of plan.targets.filter(t => t.secretName)) { const s = await get("secret", item.secretName, true); requireValue(!s || ownedSecret(s), "REGISTRY_HTTP_STATE_DRIFT"); }
    const result = { status: "OBSERVED", phase: "REGISTRY_INSPECTION_ONLY", complete: false, rows };
    await createPrivate(output, result); process.stdout.write(JSON.stringify(result) + "\n"); return;
  }
  await assertDependencies();
  if (mode === "apply") {
    await assertBootstrap(true);
    const bootstrap = await readPrivateJSON(plan.bootstrapPath);
    requireValue(fingerprint(await prepare(bootstrap)) === fingerprint(plan.targets), "REGISTRY_SOURCE_PLAN_DRIFT");
    preserved = await idle();
  } else {
    requireValue(fingerprint(await idle()) === fingerprint(preserved), "REGISTRY_PINS_CHANGED");
    for (const item of plan.targets) await observe(item);
    for (const item of plan.targets.filter(t => t.secretName)) { const s = await get("secret", item.secretName, true); requireValue(!s || ownedSecret(s), "REGISTRY_HTTP_STATE_DRIFT"); }
  }
  await createPrivate(output, { schema: "kodex.stable-registry-journal/v1", mode, planSHA256: hash, preserved, status: "IN_PROGRESS" });
  const journal = await open(output, "a"), record = async row => { await journal.writeFile(JSON.stringify(row) + "\n"); await journal.sync(); };
  try {
    if (mode === "apply") {
      for (const item of plan.targets.filter(t => t.secretName)) {
        await assertDependencies(); await assertReferences(); requireValue(fingerprint(await idle()) === fingerprint(preserved), "REGISTRY_PINS_CHANGED");
        requireValue(await get("secret", item.secretName, true) === null, "REGISTRY_HTTP_STATE_COLLISION");
        const secret = { apiVersion: "v1", kind: "Secret", metadata: { namespace: ns, name: item.secretName, annotations: { [registryIntent]: hash } },
          immutable: true, type: "Opaque", data: { secret: Buffer.from(randomBytes(32).toString("hex")).toString("base64") } };
        const dataSHA256 = fingerprint(secret.data); receipts.set(item.secretName, { dataSHA256 });
        await record({ name: item.secretName, status: "SECRET_INTENT", dataSHA256 });
        const created = JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(secret)));
        requireValue(ownedSecret(created), "REGISTRY_HTTP_STATE_DRIFT"); receipts.get(item.secretName).uid = created.metadata.uid;
        await record({ name: item.secretName, status: "SECRET_CREATED", uid: created.metadata.uid });
      }
    }
    for (const item of mode === "apply" ? plan.targets : [...plan.targets].reverse()) {
      await assertDependencies(); await assertReferences(); requireValue(fingerprint(await idle()) === fingerprint(preserved), "REGISTRY_PINS_CHANGED");
      const initial = await observe(item);
      if (mode === "rollback" && fingerprint(initial.spec) === item.beforeSpecSHA256) { await prove(item, false); continue; }
      await record({ name: item.name, status: "QUIESCE_ATTEMPT" }); const stopped = await quiesce(item);
      let desired;
      if (mode === "apply") {
        requireValue(fingerprint(stopped.spec) === item.quiescedBeforeSHA256, "REGISTRY_SOURCE_QUIESCE_DRIFT");
        const original = { ...stopped, spec: { ...stopped.spec, replicas: 1 } };
        const bootstrap = await readPrivateJSON(plan.bootstrapPath);
        desired = prepareRegistry(original, await storage(original), bootstrap.image.image, source).patch.at(-1).value;
        requireValue(fingerprint(desired) === item.afterSpecSHA256, "REGISTRY_ACTIVATION_DRIFT");
      } else {
        const compiled = fingerprint(stopped.spec) === item.quiescedAfterSHA256;
        desired = compiled ? applyReverseSpec({ ...stopped.spec, replicas: 2 }, item.inverse) : { ...stopped.spec, replicas: 1 };
        requireValue(fingerprint(desired) === item.beforeSpecSHA256, "REGISTRY_RESTORE_INVERSE_MISMATCH");
      }
      await assertDependencies(); requireValue(fingerprint(await idle()) === fingerprint(preserved), "REGISTRY_PINS_CHANGED");
      await record({ name: item.name, status: mode === "apply" ? "ACTIVATE_ATTEMPT" : "RESTORE_ATTEMPT" });
      await patch(stopped, desired);
      await kube(["-n", ns, "rollout", "status", `deployment/${item.name}`, "--timeout=300s"], undefined, 335000);
      await prove(item, mode === "apply"); await record({ name: item.name, status: "PASS" });
    }
    for (const item of plan.targets) await prove(item, mode === "apply");
    await assertReferences(); requireValue(fingerprint(await idle()) === fingerprint(preserved), "REGISTRY_PINS_CHANGED");
    if (mode === "rollback") for (const item of plan.targets.filter(t => t.secretName)) {
      const s = await get("secret", item.secretName, true); if (!s) continue;
      const allPods = JSON.parse(await kube(["-n", ns, "get", "pods", "-o", "json"]));
      const used = allPods.items.some(p => (p.spec.volumes ?? []).some(v => v.secret?.secretName === item.secretName ||
        v.projected?.sources?.some(s => s.secret?.name === item.secretName)) || (p.spec.imagePullSecrets ?? []).some(s => s.name === item.secretName) ||
        [...p.spec.containers, ...(p.spec.initContainers ?? [])].some(c => (c.env ?? []).some(e => e.valueFrom?.secretKeyRef?.name === item.secretName) ||
          (c.envFrom ?? []).some(e => e.secretRef?.name === item.secretName)));
      requireValue(!used, "REGISTRY_HTTP_STATE_STILL_REFERENCED"); await assertDependencies();
      requireValue(ownedSecret(s), "REGISTRY_HTTP_STATE_DRIFT"); await record({ name: item.secretName, status: "SECRET_DELETE_INTENT", uid: s.metadata.uid });
      await kube(["delete", "--raw", `/api/v1/namespaces/${ns}/secrets/${item.secretName}`, "-f", "/dev/stdin"], JSON.stringify({ apiVersion: "v1", kind: "DeleteOptions",
        preconditions: { uid: s.metadata.uid, resourceVersion: s.metadata.resourceVersion } }));
      requireValue(await get("secret", item.secretName, true) === null, "REGISTRY_HTTP_STATE_DELETE_INCOMPLETE");
    }
    const result = { status: "PASS", phase: mode === "apply" ? "REGISTRY_TWO_READY" : "REGISTRY_SOURCE_RESTORED", publicBrowser: "NOT_RUN", freshNodePull: "NOT_RUN" };
    await record(result); process.stdout.write(JSON.stringify(result) + "\n");
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "REGISTRY_TRANSITION_FAILED" }); throw error; }
  finally { await journal.close(); }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releaseRegistry(process.argv.slice(2)).catch(error => { process.stderr.write(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code:
    /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "REGISTRY_TRANSITION_FAILED" }) + "\n"); process.exitCode = 1; });
