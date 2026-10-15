#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { admissionName as name, admissionLease, admissionIntent, admissionRole, prepareAdmission, admissionSupportMatches } from "./stable-admission-model.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { activeDeploymentPods } from "./stable-archive-transition.mjs";
import { requireIdle } from "./runner-policy-model.mjs";
import { validateStableReceipt } from "./stable-image-publication.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url)), ns = "kodex-system";
const manifest = root + "deploy/k8s/base/image-supply-chain/image-admission-controller.yaml";
function requireValue(value, code) { if (!value) throw Error(code); }
const metadataHash = r => fingerprint({ labels: r.metadata.labels ?? {}, annotations: r.metadata.annotations ?? {} });
async function privateWrite(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(JSON.stringify(value) + "\n"); await file.sync(); } finally { await file.close(); }
}

export async function releaseAdmission(args, commandPort) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--publication-plan", "--plan", "--output", "--journal", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "ADMISSION_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  const context = options["--context"], output = options["--output"];
  requireValue(["plan", "apply", "inspect", "readback", "rollback"].includes(mode) && /^k3d-[a-z0-9-]+$/.test(context ?? "") &&
    !/prod(?:uction)?/i.test(context) && output, "ADMISSION_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = commandPort ?? (async (program, argv, input, timeout = 35000) => {
    try {
      const pending = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 8 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("ADMISSION_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const kube = (argv, input, timeout) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=30s", ...argv], undefined, timeout) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-admission-pipe", "--context", context, "--request-timeout=30s", ...argv], input, timeout);
  const get = async (kind, target, optional = false) => {
    const raw = await kube(["-n", ns, "get", kind, target, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const source = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "ADMISSION_CLEAN_SOURCE_REQUIRED");
  const clusterUID = (await get("namespace", "kube-system")).metadata.uid, appUID = (await get("namespace", ns)).metadata.uid;
  const canonical = (await run("yq", ["eval-all", "-N", "-o=json", "-I=0", ".", manifest])).trim().split("\n").map(JSON.parse);
  const template = canonical.find(r => r.kind === "Deployment"), leaseRule = canonical.find(r => r.kind === "Role")?.rules.find(r => r.apiGroups.includes("coordination.k8s.io"));
  const support = canonical.filter(r => ["Lease", "PodDisruptionBudget"].includes(r.kind)).map(r => ({ ...r, metadata: { ...r.metadata, namespace: ns } }));
  requireValue(support.length === 2 && support.some(r => r.kind === "Lease" && r.metadata.name === admissionLease) &&
    support.some(r => r.kind === "PodDisruptionBudget" && r.metadata.name === name), "ADMISSION_SUPPORT_PROFILE_INVALID");
  const pods = async d => {
    requireValue(!d.spec.selector.matchExpressions?.length && Object.keys(d.spec.selector.matchLabels ?? {}).length, "ADMISSION_SELECTOR_INVALID");
    const selector = Object.entries(d.spec.selector.matchLabels).sort().map(([k, v]) => `${k}=${v}`).join(",");
    return activeDeploymentPods(JSON.parse(await kube(["-n", ns, "get", "replicasets,pods", "-l", selector, "-o", "json"])), d.metadata.uid);
  };
  const references = async () => {
    const d = await get("deployment", name), p = d.spec.template.spec, refs = new Map();
    const add = (kind, target) => { if (target) refs.set(kind + "/" + target, { kind, name: target }); };
    add("serviceaccount", p.serviceAccountName);
    for (const v of p.volumes ?? []) {
      add("secret", v.secret?.secretName); add("configmap", v.configMap?.name);
      for (const s of v.projected?.sources ?? []) { add("secret", s.secret?.name); add("configmap", s.configMap?.name); }
    }
    for (const s of p.imagePullSecrets ?? []) add("secret", s.name);
    for (const c of p.containers) {
      for (const e of c.env ?? []) { add("secret", e.valueFrom?.secretKeyRef?.name); add("configmap", e.valueFrom?.configMapKeyRef?.name); }
      for (const e of c.envFrom ?? []) { add("secret", e.secretRef?.name); add("configmap", e.configMapRef?.name); }
      add("configmap", c.env?.find(e => e.name === "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP")?.value);
    }
    const rows = [];
    for (const r of [...refs.values()].sort((a, b) => (a.kind + a.name).localeCompare(b.kind + b.name, "en"))) {
      const identity = (await kube(["-n", ns, "get", r.kind, r.name, "-o", 'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}'])).trim();
      requireValue(/^[a-zA-Z0-9-]+\|[0-9]+$/.test(identity), "ADMISSION_REFERENCE_INVALID"); rows.push({ ...r, identity });
    }
    const routes = JSON.parse(await kube(["-n", ns, "get", "services,networkpolicies", "-o", "json"]));
    for (const r of routes.items.filter(r => r.metadata.name.includes("image-admission-controller")))
      rows.push({ kind: r.kind, name: r.metadata.name, uid: r.metadata.uid, hash: fingerprint(r.spec) });
    return rows;
  };
  const idle = async () => {
    const state = JSON.parse(await kube(["-n", ns, "exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"],
      await readFile(root + "tools/dev/supply-chain-owner-readback.sql", "utf8")));
    requireIdle(state);
    const work = JSON.parse(await kube(["-n", ns, "get", "jobs,pods,persistentvolumeclaims", "-l", "kodex.dev/image-admission-orchestrated=true", "-o", "json"]));
    requireValue(work.items?.length === 0, "ADMISSION_ORCHESTRATION_NOT_IDLE");
    return { promotedPinsSHA256: state.promotedPinsSHA256, promotedArtifactCount: state.promotedArtifactCount };
  };
  let proofSequence = 0;
  const publication = async (path, strict) => {
    const p = await readPrivateJSON(path);
    requireValue(p.schema === "kodex.stable-image-publication/v2" && p.artifactProfile === "built" && p.source === source &&
      p.context === context && p.clusterUID === clusterUID && p.namespaceUID === appUID && p.image?.component === name &&
      p.receipt?.component === name && p.receipt.imageDigest === p.image.digest && p.receipt.sourceSHA === p.image.sourceSHA,
      "ADMISSION_PUBLICATION_IDENTITY_INVALID");
    requireValue(fingerprint(validateStableReceipt(p.receipt)) === fingerprint(p.image), "ADMISSION_PUBLICATION_IMAGE_INVALID");
    if (strict) {
      const observed = JSON.parse(await run(process.execPath, [root + "tools/release/stable-image-publication.mjs", "readback", "--context", context,
        "--plan", path, "--output", output + ".publication-" + proofSequence++ + ".jsonl"], undefined, 220000));
      requireValue(observed.status === "PASS" && observed.phase === "REGISTRY_PUBLICATION_ONLY" && observed.digest === p.image.digest,
        "ADMISSION_PUBLICATION_READBACK_FAILED");
    }
    return p;
  };
  const prepare = async image => {
    const d = await get("deployment", name), prepared = prepareAdmission(d, template, image), list = await pods(d);
    requireValue(d.status?.observedGeneration >= d.metadata.generation &&
      ["replicas", "updatedReplicas", "readyReplicas", "availableReplicas"].every(k => d.status[k] === 1) && list.length === 1 &&
      !list[0].metadata.deletionTimestamp && list[0].status?.conditions?.some(c => c.type === "Ready" && c.status === "True"), "ADMISSION_SOURCE_NOT_READY");
    const status = list[0].status.containerStatuses?.find(s => s.name === name && s.ready), digest = status?.imageID?.match(/sha256:[a-f0-9]{64}$/)?.[0];
    requireValue(digest && list[0].spec.containers.some(c => c.name === name && c.image === d.spec.template.spec.containers[0].image), "ADMISSION_SOURCE_SERVING_INVALID");
    const dry = JSON.parse(await kube(["-n", ns, "patch", "deployment", name, "--type=json", "--dry-run=server", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify(prepared.patch)));
    requireValue(fingerprint(dry.spec) === prepared.afterSpecSHA256, "ADMISSION_SERVER_DEFAULTS_MISMATCH");
    const { patch: _patch, ...item } = prepared;
    const role = admissionRole(await get("role", name), await get("rolebinding", name), leaseRule);
    requireValue(role.policyName === d.spec.template.spec.containers[0].env.find(e => e.name === "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP").value,
      "ADMISSION_POLICY_BINDING_MISMATCH");
    return { ...item, sourceDigest: digest, role };
  };
  const supportSnapshot = async () => {
    const rows = [];
    for (const t of support) {
      const current = await get(t.kind, t.metadata.name, true);
      requireValue(!current || admissionSupportMatches(current, t), "ADMISSION_SUPPORT_COLLISION");
      if (!current) await kube(["create", "--dry-run=server", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(t));
      rows.push({ kind: t.kind, name: t.metadata.name, uid: current?.metadata.uid ?? null,
        metadataSHA256: current ? metadataHash(current) : null });
    }
    return rows;
  };
  if (mode === "plan") {
    requireValue(options["--publication-plan"] && !options["--plan"] && !options["--journal"] && !options["--approved-plan-sha"], "ADMISSION_ARGUMENTS_INVALID");
    const p = await publication(options["--publication-plan"], false);
    const plan = { schema: "kodex.stable-admission/v1", executorSHA: source, context, clusterUID, appUID,
      publicationPath: options["--publication-plan"], publicationSHA256: fingerprint(p), image: p.image,
      target: await prepare(p.image), support: await supportSnapshot(), references: await references() };
    await privateWrite(output, plan); process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(plan) }) + "\n"); return;
  }
  requireValue(options["--plan"] && !options["--publication-plan"], "ADMISSION_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(options["--plan"]), hash = fingerprint(plan), item = plan.target;
  requireValue(plan.schema === "kodex.stable-admission/v1" && plan.executorSHA === source && plan.context === context && plan.clusterUID === clusterUID && plan.appUID === appUID &&
    item?.uid && Array.isArray(item.inverse) && item.inverse.length <= 512 &&
    ["beforeSpecSHA256", "afterSpecSHA256", "quiescedBeforeSHA256", "quiescedAfterSHA256", "beforeLabelsSHA256", "afterLabelsSHA256"].every(k => /^[a-f0-9]{64}$/.test(item[k] ?? "")) &&
    fingerprint(plan.support?.map(r => [r.kind, r.name])) === fingerprint(support.map(r => [r.kind, r.metadata.name])), "ADMISSION_PLAN_IDENTITY_INVALID");
  requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"], "ADMISSION_EXACT_OWNER_CONFIRMATION_REQUIRED");
  requireValue(mode === "apply" ? !options["--journal"] : options["--journal"], "ADMISSION_APPLY_JOURNAL_REQUIRED");
  const receipts = new Map(); let preserved;
  if (options["--journal"]) {
    const file = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = await file.stat(); requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 && (stat.mode & 0o777) === 0o600 && stat.size <= 1 << 20,
        "ADMISSION_PRIVATE_JOURNAL_REQUIRED");
      const rows = (await file.readFile("utf8")).trim().split("\n").map(JSON.parse), header = rows.shift();
      requireValue(rows.length <= 64 && header.schema === "kodex.stable-admission-journal/v1" && header.mode === "apply" && header.planSHA256 === hash,
        "ADMISSION_JOURNAL_MISMATCH"); preserved = header.preserved;
      for (const r of rows) {
        if (r.status === "RESOURCE_INTENT") { requireValue(support.some(t => t.kind === r.kind && t.metadata.name === r.name) && !receipts.has(r.kind), "ADMISSION_JOURNAL_MISMATCH"); receipts.set(r.kind, {}); }
        if (r.status === "RESOURCE_CREATED") { requireValue(receipts.has(r.kind) && !receipts.get(r.kind).uid && r.uid, "ADMISSION_JOURNAL_MISMATCH"); receipts.get(r.kind).uid = r.uid; }
      }
    } finally { await file.close(); }
  }
  const assertReferences = async () => requireValue(fingerprint(await references()) === fingerprint(plan.references), "ADMISSION_REFERENCE_DRIFT");
  const assertPins = async () => requireValue(fingerprint(await idle()) === fingerprint(preserved), "ADMISSION_PINS_CHANGED");
  const checkSupport = async (required = false) => {
    const observed = new Map();
    for (const s of plan.support) {
      const t = support.find(t => t.kind === s.kind), actual = await get(s.kind, s.name, true);
      if (!actual) { requireValue(!required && !s.uid && !receipts.get(s.kind)?.uid, "ADMISSION_SUPPORT_MISSING"); continue; }
      requireValue(admissionSupportMatches(actual, t) && (s.uid ? actual.metadata.uid === s.uid && metadataHash(actual) === s.metadataSHA256 :
        receipts.has(s.kind) && (!receipts.get(s.kind).uid || actual.metadata.uid === receipts.get(s.kind).uid) &&
        metadataHash(actual) === metadataHash({ metadata: { ...t.metadata, annotations: { [admissionIntent]: hash } } })), "ADMISSION_SUPPORT_DRIFT");
      observed.set(s.kind, actual);
    }
    return observed;
  };
  const observe = async () => {
    const d = await get("deployment", name), h = fingerprint(d.spec), compiled = [item.afterSpecSHA256, item.quiescedAfterSHA256].includes(h);
    requireValue(d.metadata.uid === item.uid && [item.beforeSpecSHA256, item.afterSpecSHA256, item.quiescedBeforeSHA256, item.quiescedAfterSHA256].includes(h) &&
      fingerprint(d.metadata.labels) === (compiled ? item.afterLabelsSHA256 : item.beforeLabelsSHA256), "ADMISSION_DEPLOYMENT_DRIFT"); return d;
  };
  const rolePhase = async () => {
    const r = await get("role", name), binding = await get("rolebinding", name), value = fingerprint(r.rules);
    requireValue(r.kind === "Role" && r.metadata.namespace === ns && r.metadata.name === name && r.metadata.uid === item.role.uid &&
      binding.kind === "RoleBinding" && binding.metadata.namespace === ns && binding.metadata.name === name && binding.metadata.uid === item.role.bindingUID &&
      fingerprint({ roleRef: binding.roleRef, subjects: binding.subjects }) === item.role.bindingSHA256 &&
      [item.role.beforeRulesSHA256, item.role.afterRulesSHA256].includes(value), "ADMISSION_ROLE_DRIFT"); return r;
  };
  const patch = (r, field, value, labels) => kube(["-n", ns, "patch", r.kind, r.metadata.name, "--type=json", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify([
    { op: "test", path: "/metadata/uid", value: r.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: r.metadata.resourceVersion },
    { op: "test", path: "/" + field, value: r[field] }, { op: "replace", path: "/" + field, value },
    ...(labels ? [{ op: "test", path: "/metadata/labels", value: r.metadata.labels }, { op: "replace", path: "/metadata/labels", value: labels }] : [])]));
  const quiesce = async () => {
    let d = await observe(); if (d.spec.replicas !== 0) await patch(d, "spec", { ...d.spec, replicas: 0 });
    const deadline = Date.now() + 120000;
    for (;;) {
      d = await observe(); requireValue(d.spec.replicas === 0, "ADMISSION_QUIESCE_DRIFT");
      if (d.status?.observedGeneration >= d.metadata.generation && !(d.status.replicas ?? 0) && !(await pods(d)).length) return d;
      requireValue(Date.now() < deadline, "ADMISSION_QUIESCE_TIMEOUT"); await new Promise(resolve => setTimeout(resolve, 250));
    }
  };
  const prove = async after => {
    const d = await observe(), count = after ? 2 : 1, list = await pods(d);
    const serving = (deployment, instances) => fingerprint(deployment.spec) === (after ? item.afterSpecSHA256 : item.beforeSpecSHA256) &&
      deployment.status?.observedGeneration >= deployment.metadata.generation &&
      ["replicas", "updatedReplicas", "readyReplicas", "availableReplicas"].every(k => deployment.status[k] === count) && instances.length === count &&
      instances.every(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True") &&
        p.spec.containers.some(c => c.name === name && c.image === deployment.spec.template.spec.containers[0].image) &&
        p.status.containerStatuses?.some(s => s.name === name && s.ready && s.imageID?.endsWith(after ? plan.image.digest : item.sourceDigest)));
    requireValue(serving(d, list), "ADMISSION_SERVING_INCOMPLETE");
    const r = await rolePhase(); requireValue(fingerprint(r.rules) === (after ? item.role.afterRulesSHA256 : item.role.beforeRulesSHA256), "ADMISSION_ROLE_PHASE_MISMATCH");
    if (!after) return;
    const deadline = Date.now() + 60000;
    for (;;) {
      // Используется тот же snapshot, в котором проверены exact UID/metadata,
      // а не отдельный повторный GET с несвязанным holder.
      const lease = (await checkSupport(true)).get("Lease"), renewed = Date.parse(lease.spec?.renewTime);
      if (list.some(p => p.metadata.uid === lease.spec.holderIdentity) && Number.isFinite(renewed) && renewed <= Date.now() + 5000 &&
        Date.now() - renewed <= 45000 && lease.spec.leaseDurationSeconds === 45) {
        const current = await observe(), currentPods = await pods(current);
        requireValue(serving(current, currentPods) && currentPods.some(p => p.metadata.uid === lease.spec.holderIdentity), "ADMISSION_SERVING_INCOMPLETE");
        await rolePhase(); break;
      }
      requireValue(Date.now() < deadline, "ADMISSION_CURRENT_LEADER_UNPROVEN"); await new Promise(resolve => setTimeout(resolve, 250));
    }
  };
  await assertReferences(); await rolePhase(); await checkSupport();
  if (mode === "inspect") {
    const d = await observe(), result = { status: "OBSERVED", phase: "ADMISSION_INSPECTION_ONLY", complete: false, specSHA256: fingerprint(d.spec), replicas: d.spec.replicas };
    await privateWrite(output, result); process.stdout.write(JSON.stringify(result) + "\n"); return;
  }
  if (mode === "readback") {
    const p = await publication(plan.publicationPath, true); requireValue(fingerprint(p) === plan.publicationSHA256, "ADMISSION_PUBLICATION_DRIFT");
    await prove(true); await assertPins(); const result = { status: "PASS", phase: "ADMISSION_TWO_READY_WITH_LEADER", publicBrowser: "NOT_RUN" };
    await privateWrite(output, result); process.stdout.write(JSON.stringify(result) + "\n"); return;
  }
  if (mode === "apply") {
    const p = await publication(plan.publicationPath, true); requireValue(fingerprint(p) === plan.publicationSHA256 && fingerprint(p.image) === fingerprint(plan.image), "ADMISSION_PUBLICATION_DRIFT");
    requireValue(fingerprint(await prepare(plan.image)) === fingerprint(item) && fingerprint(await supportSnapshot()) === fingerprint(plan.support), "ADMISSION_SOURCE_PLAN_DRIFT"); preserved = await idle();
  } else {
    const d = await observe(); await assertPins();
    if (fingerprint(d.spec) === item.beforeSpecSHA256 && fingerprint((await rolePhase()).rules) === item.role.beforeRulesSHA256) {
      await prove(false);
      const result = { status: "PASS", phase: "SOURCE_ALREADY_RESTORED_LEASE_RETAINED", publicBrowser: "NOT_RUN" };
      await privateWrite(output, result); process.stdout.write(JSON.stringify(result) + "\n"); return;
    }
  }
  await privateWrite(output, { schema: "kodex.stable-admission-journal/v1", mode, planSHA256: hash, preserved, status: "IN_PROGRESS" });
  const file = await open(output, "a"), record = async row => { await file.writeFile(JSON.stringify(row) + "\n"); await file.sync(); };
  try {
    await record({ status: "QUIESCE_ATTEMPT" }); const stopped = await quiesce();
    await assertReferences(); await assertPins(); await checkSupport();
    if (mode === "apply") for (const s of plan.support.filter(s => !s.uid)) {
      const resource = structuredClone(support.find(t => t.kind === s.kind));
      resource.metadata.annotations = { [admissionIntent]: hash };
      await record({ status: "RESOURCE_INTENT", kind: s.kind, name: s.name }); receipts.set(s.kind, {});
      const created = JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(resource)));
      requireValue(admissionSupportMatches(created, resource) && created.metadata.annotations?.[admissionIntent] === hash, "ADMISSION_SUPPORT_DRIFT");
      receipts.get(s.kind).uid = created.metadata.uid; await record({ status: "RESOURCE_CREATED", kind: s.kind, uid: created.metadata.uid });
    }
    const role = await rolePhase();
    if (fingerprint(role.rules) !== (mode === "apply" ? item.role.afterRulesSHA256 : item.role.beforeRulesSHA256)) {
      const rules = mode === "apply" ? [...role.rules, leaseRule] : role.rules.filter(r => fingerprint(r) !== fingerprint(leaseRule));
      requireValue(fingerprint(rules) === (mode === "apply" ? item.role.afterRulesSHA256 : item.role.beforeRulesSHA256), "ADMISSION_ROLE_RESTORE_INVALID");
      await record({ status: "ROLE_PATCH_ATTEMPT" }); await patch(role, "rules", rules);
    }
    await assertReferences(); await assertPins(); await checkSupport(mode === "apply"); await rolePhase();
    let desired, labels;
    if (mode === "apply") {
      requireValue(fingerprint(stopped.spec) === item.quiescedBeforeSHA256, "ADMISSION_SOURCE_QUIESCE_DRIFT");
      const original = { ...stopped, spec: { ...stopped.spec, replicas: 1 } };
      const prepared = prepareAdmission(original, template, plan.image); desired = prepared.patch.find(p => p.path === "/spec" && p.op === "replace").value;
      labels = { ...stopped.metadata.labels, "kodex.dev/local-profile": "stable" };
      requireValue(fingerprint(desired) === item.afterSpecSHA256, "ADMISSION_ACTIVATION_DRIFT");
    } else {
      desired = fingerprint(stopped.spec) === item.quiescedAfterSHA256 ? applyReverseSpec({ ...stopped.spec, replicas: 2 }, item.inverse) : { ...stopped.spec, replicas: 1 };
      labels = { ...stopped.metadata.labels, "kodex.dev/local-profile": "hot-reload" };
      requireValue(fingerprint(desired) === item.beforeSpecSHA256 && fingerprint(labels) === item.beforeLabelsSHA256, "ADMISSION_RESTORE_INVERSE_INVALID");
    }
    await record({ status: mode === "apply" ? "ACTIVATE_ATTEMPT" : "RESTORE_ATTEMPT" }); await patch(stopped, "spec", desired, labels);
    await kube(["-n", ns, "rollout", "status", "deployment/" + name, "--timeout=300s"], undefined, 335000);
    await prove(mode === "apply"); await assertReferences(); await assertPins();
    await record({ status: "PASS", phase: mode === "apply" ? "ADMISSION_TWO_READY_WITH_LEADER" : "SOURCE_RESTORED_LEASE_RETAINED", publicBrowser: "NOT_RUN" });
    process.stdout.write(JSON.stringify({ status: "PASS", phase: mode === "apply" ? "ADMISSION_TWO_READY_WITH_LEADER" : "SOURCE_RESTORED_LEASE_RETAINED" }) + "\n");
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "ADMISSION_TRANSITION_FAILED" }); throw error; }
  finally { await file.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releaseAdmission(process.argv.slice(2)).catch(error => { process.stderr.write(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code:
    /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "ADMISSION_TRANSITION_FAILED" }) + "\n"); process.exitCode = 1; });
