#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { lstat, open, readFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { randomBytes } from "node:crypto";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { preparePlatformInfra, platformInfraProfiles, identityCachePeers, platformInfraMetadata } from "./stable-platform-infra-model.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { activeDeploymentPods } from "./stable-archive-transition.mjs";
import { requireIdle } from "./runner-policy-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(v, code) { if (!v) throw Error(code); }
export async function releasePlatformInfra(args, port) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift(); requireValue(["--context", "--bootstrap-plan", "--plan", "--journal", "--output", "--approved-plan-sha"].includes(key) &&
      !Object.hasOwn(options, key) && args.length, "PLATFORM_INFRA_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(["plan", "apply", "inspect", "readback", "rollback"].includes(mode) && options["--context"] === "k3d-kodex" &&
    options["--output"]?.startsWith("/") && (mode === "plan" ? options["--bootstrap-plan"] && !options["--plan"] : options["--plan"] && !options["--bootstrap-plan"]),
  "PLATFORM_INFRA_ARGUMENTS_INVALID");
  requireValue(["plan", "apply"].includes(mode) ? !options["--journal"] : options["--journal"], "PLATFORM_INFRA_JOURNAL_REQUIRED");
  const parent = await lstat(dirname(options["--output"]));
  requireValue(parent.isDirectory() && parent.uid === process.getuid() && (parent.mode & 0o777) === 0o700, "PLATFORM_INFRA_PRIVATE_DIRECTORY_REQUIRED");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 35000) => {
    try { const p = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 8 << 20 });
      if (input !== undefined) { p.child.stdin.on("error", () => {}); p.child.stdin.end(input); } return (await p).stdout;
    } catch { throw Error("PLATFORM_INFRA_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const context = options["--context"], kube = (argv, input, timeout) => input === undefined ?
    run("kubectl", ["--context", context, "--request-timeout=20s", ...argv], undefined, timeout) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-platform-infra", "--context", context, "--request-timeout=20s", ...argv], input, timeout);
  const get = async (kind, name, ns, optional = false) => {
    const raw = await kube([...(ns ? ["-n", ns] : []), "get", kind, name, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const source = (await run("git", ["rev-parse", "HEAD"])).trim(), clusterUID = (await get("namespace", "kube-system")).metadata.uid;
  const assertSource = async () => requireValue(/^[a-f0-9]{40}$/.test(source) && (await run("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim() &&
    (await get("namespace", "kube-system")).metadata.uid === clusterUID, "PLATFORM_INFRA_EXECUTOR_OR_CLUSTER_CHANGED");
  await assertSource();
  const privateWrite = async value => { const f = await open(options["--output"], "wx", 0o600);
    try { await f.writeFile(JSON.stringify(value) + "\n"); await f.sync(); } finally { await f.close(); } };
  const refs = async () => {
    const found = new Map(), result = [];
    for (const p of platformInfraProfiles) {
      const d = await get("deployment", p.name, p.namespace), pod = d.spec.template.spec;
      const add = (kind, name) => { if (name) found.set([p.namespace, kind, name].join("/"), { kind, name, namespace: p.namespace }); };
      for (const v of pod.volumes ?? []) {
        add("secret", v.secret?.secretName); add("configmap", v.configMap?.name);
        for (const s of v.projected?.sources ?? []) { add("secret", s.secret?.name); add("configmap", s.configMap?.name); }
        if (v.persistentVolumeClaim) {
          const claim = await get("pvc", v.persistentVolumeClaim.claimName, p.namespace), pv = await get("pv", claim.spec.volumeName);
          requireValue(claim.status?.phase === "Bound", "PLATFORM_INFRA_STORAGE_UNBOUND");
          result.push({ kind: "storage", name: claim.metadata.name, uid: claim.metadata.uid,
            hash: fingerprint({ claim: claim.spec, pvUID: pv.metadata.uid, pv: pv.spec }) });
        }
      }
      for (const c of [...pod.containers, ...(pod.initContainers ?? [])]) {
        for (const e of c.env ?? []) { add("secret", e.valueFrom?.secretKeyRef?.name); add("configmap", e.valueFrom?.configMapKeyRef?.name); }
        for (const e of c.envFrom ?? []) { add("secret", e.secretRef?.name); add("configmap", e.configMapRef?.name); }
      }
      for (const s of pod.imagePullSecrets ?? []) add("secret", s.name);
    }
    for (const r of [...found.values()].sort((a, b) => [a.namespace, a.kind, a.name].join("/").localeCompare([b.namespace, b.kind, b.name].join("/"), "en"))) {
      const identity = (await kube(["-n", r.namespace, "get", r.kind, r.name, "-o", 'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}'])).trim();
      requireValue(/^[a-zA-Z0-9-]+\|[0-9]+$/.test(identity), "PLATFORM_INFRA_REFERENCE_INVALID"); result.push({ ...r, identity });
    }
    for (const ns of ["kodex-system", "identity"]) {
      const list = JSON.parse(await kube(["-n", ns, "get", "services,networkpolicies", "-o", "json"]));
      for (const r of list.items.filter(r => !(r.kind === "NetworkPolicy" && ns === "identity" && r.metadata.name === "sso-exact-paths"))
        .sort((a, b) => (a.kind + a.metadata.name).localeCompare(b.kind + b.metadata.name, "en")))
        result.push({ kind: r.kind, namespace: ns, name: r.metadata.name, uid: r.metadata.uid, hash: fingerprint(r.spec) });
    }
    return result;
  };
  const pods = async (d, ns) => {
    const labels = d.spec.selector?.matchLabels;
    requireValue(labels && Object.keys(labels).length && !d.spec.selector.matchExpressions?.length &&
      Object.entries(labels).every(([k, v]) => /^[A-Za-z0-9_./-]+$/.test(k) && /^[A-Za-z0-9_.-]+$/.test(v)), "PLATFORM_INFRA_SELECTOR_INVALID");
    return activeDeploymentPods(JSON.parse(await kube(["-n", ns, "get", "replicasets,pods", "-l",
      Object.entries(labels).sort().map(([k, v]) => k + "=" + v).join(","), "-o", "json"])), d.metadata.uid);
  };
  const serving = async (d, p, expectedDigests) => {
    requireValue(d.status?.observedGeneration >= d.metadata.generation &&
      ["replicas", "readyReplicas", "availableReplicas", "updatedReplicas"].every(k => d.status[k] === d.spec.replicas), "PLATFORM_INFRA_NOT_SERVING");
    const list = await pods(d, p.namespace), images = {};
    requireValue(list.length === d.spec.replicas && list.every(p => !p.metadata.deletionTimestamp &&
      p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True")), "PLATFORM_INFRA_NOT_SERVING");
    for (const pod of list) for (const [group, field] of [["containers", "containerStatuses"], ["initContainers", "initContainerStatuses"]]) {
      for (const c of d.spec.template.spec[group] ?? []) {
        const s = pod.status[field]?.find(s => s.name === c.name), key = group + "/" + c.name;
        const digest = s?.imageID?.match(/sha256:[a-f0-9]{64}$/)?.[0];
        requireValue(pod.spec[group]?.some(a => a.name === c.name && a.image === c.image) && digest &&
          (group === "containers" || c.restartPolicy === "Always" ? s.ready && s.state?.running : s.state?.terminated?.exitCode === 0) &&
          (!images[key] || images[key] === digest), "PLATFORM_INFRA_SERVING_IMAGE_INVALID"); images[key] = digest;
      }
    }
    requireValue(!expectedDigests || fingerprint(images) === fingerprint(expectedDigests), "PLATFORM_INFRA_SERVING_IMAGE_DRIFT"); return images;
  };
  let cacheProof = 0;
  const bootstrap = async path => {
    const b = await readPrivateJSON(path);
    requireValue(b.schema === "kodex.stable-pull-bootstrap/v1" && b.executorSHA === source && b.context === context &&
      b.clusterUID === clusterUID, "PLATFORM_INFRA_BOOTSTRAP_INVALID");
    const result = JSON.parse(await run(process.execPath, [root + "tools/release/stable-pull-bootstrap.mjs", "readback", "--context", context,
      "--plan", path, "--output", options["--output"] + ".cache-" + cacheProof++ + ".json"], undefined, 120000));
    requireValue(result.status === "PASS" && result.phase === "NODE_CACHE_ONLY", "PLATFORM_INFRA_HELPER_CACHE_REQUIRED"); return b;
  };
  if (mode === "plan") {
    requireValue(!options["--approved-plan-sha"], "PLATFORM_INFRA_ARGUMENTS_INVALID");
    const b = await bootstrap(options["--bootstrap-plan"]), intent = randomBytes(16).toString("hex"), references = await refs(), rows = [];
    const network = await get("networkpolicy", "sso-exact-paths", "identity"), peer = identityCachePeers(network);
    for (const p of platformInfraProfiles) {
      const d = await get("deployment", p.name, p.namespace);
      let resources;
      if (p.file) resources = JSON.parse(await run("yq", ["eval-all", "-N", "-o=json", "-I=0",
        `select(.kind == "Deployment" and .metadata.name == "${p.name}") | .spec.template.spec.containers[] | select(.name == "${p.container}") | .resources`, root + p.file]));
      const transition = preparePlatformInfra(d, p, { image: b.image.image, resources, intent }), images = await serving(d, p);
      const dry = JSON.parse(await kube(["-n", p.namespace, "patch", "deployment", p.name, "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "json"],
        JSON.stringify([{ op: "test", path: "/metadata/uid", value: d.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: d.metadata.resourceVersion },
          { op: "replace", path: "/spec", value: transition.spec }])));
      requireValue(fingerprint(dry.spec) === transition.afterSHA256, "PLATFORM_INFRA_SERVER_DEFAULTS_MISMATCH");
      const { spec: _spec, ...safe } = transition;
      rows.push({ name: p.name, namespace: p.namespace, ...safe, images,
        desiredImages: p.name === "kodex-image-registry-pull" ? { ...images, "containers/pull-authorizer": b.image.digest } : images });
    }
    await kube(["-n", "identity", "patch", "networkpolicy", "sso-exact-paths", "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "name"],
      JSON.stringify([{ op: "test", path: "/metadata/uid", value: peer.uid }, { op: "replace", path: "/spec", value: peer.spec }]));
    const { spec: _spec, ...safePeer } = peer;
    requireValue(fingerprint(await refs()) === fingerprint(references), "PLATFORM_INFRA_REFERENCES_CHANGED"); await assertSource();
    const plan = { schema: "kodex.stable-platform-infra/v1", source, context, clusterUID, intent,
      bootstrapPath: options["--bootstrap-plan"], bootstrapSHA256: fingerprint(b), image: b.image.image, references, rows, peer: safePeer };
    const document = { plan, planSHA256: fingerprint(plan) }; await privateWrite(document); return document;
  }
  const document = await readPrivateJSON(options["--plan"]), { plan } = document, hash = fingerprint(plan);
  requireValue(plan.schema === "kodex.stable-platform-infra/v1" && document.planSHA256 === hash && plan.source === source &&
    plan.context === context && plan.clusterUID === clusterUID && /^[a-f0-9]{32}$/.test(plan.intent) && plan.rows?.length === 4 &&
    plan.rows.every((r, i) => r.name === platformInfraProfiles[i].name && r.namespace === platformInfraProfiles[i].namespace), "PLATFORM_INFRA_PLAN_INVALID");
  requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"], "PLATFORM_INFRA_EXACT_APPROVAL_REQUIRED");
  requireValue(fingerprint(await refs()) === fingerprint(plan.references), "PLATFORM_INFRA_REFERENCES_CHANGED");
  let records = [];
  if (options["--journal"]) {
    const f = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    try { const s = await f.stat(); requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 && (s.mode & 0o777) === 0o600 && s.size < 1 << 20,
      "PLATFORM_INFRA_PRIVATE_JOURNAL_REQUIRED"); records = (await f.readFile("utf8")).trim().split("\n").map(JSON.parse); } finally { await f.close(); }
    requireValue(records[0]?.schema === "kodex.stable-platform-infra-journal/v1" && records[0].mode === "apply" && records[0].planSHA256 === hash &&
      ["COMPLETE", "UNKNOWN"].includes(records.at(-1)?.status), "PLATFORM_INFRA_JOURNAL_INVALID");
  }
  const policy = async () => { const d = await get("networkpolicy", "sso-exact-paths", "identity"), h = fingerprint(d.spec);
    requireValue(d.metadata.uid === plan.peer.uid && [plan.peer.beforeSHA256, plan.peer.afterSHA256].includes(h), "PLATFORM_INFRA_POLICY_DRIFT"); return { d, after: h === plan.peer.afterSHA256 }; };
  const observe = async row => { const d = await get("deployment", row.name, row.namespace), h = fingerprint(d.spec);
    requireValue(d.metadata.uid === row.uid && platformInfraMetadata(d) === row.metadataSHA256 && [row.beforeSHA256, row.afterSHA256].includes(h), "PLATFORM_INFRA_DEPLOYMENT_DRIFT");
    return { d, after: h === row.afterSHA256 }; };
  const patch = async (kind, name, ns, d, spec) => kube(["-n", ns, "patch", kind, name, "--type=json", "--patch-file=/dev/stdin", "-o", "json"],
    JSON.stringify([{ op: "test", path: "/metadata/uid", value: d.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: d.metadata.resourceVersion },
      { op: "test", path: "/spec", value: d.spec }, { op: "replace", path: "/spec", value: spec }]));
  const idle = async () => {
    const r = JSON.parse(await kube(["-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"],
      await readFile(root + "tools/dev/supply-chain-owner-readback.sql", "utf8"))); requireIdle(r);
    return { count: r.promotedArtifactCount, pinsSHA256: r.promotedPinsSHA256 };
  };
  const prove = async (row, after) => {
    const deadline = Date.now() + 180000;
    for (;;) {
      const v = await observe(row); requireValue(v.after === after, "PLATFORM_INFRA_PHASE_CHANGED");
      try { return await serving(v.d, platformInfraProfiles.find(p => p.name === row.name), after ? row.desiredImages : row.images); }
      catch (e) { requireValue(["PLATFORM_INFRA_NOT_SERVING"].includes(e.message) && Date.now() < deadline, e.message); }
      await new Promise(resolve => setTimeout(resolve, 500));
    }
  };
  const observations = [];
  for (const row of plan.rows) observations.push(await observe(row));
  const peerState = await policy();
  if (mode === "apply") {
    requireValue(observations.every(o => !o.after) && !peerState.after &&
      fingerprint(await bootstrap(plan.bootstrapPath)) === plan.bootstrapSHA256, "PLATFORM_INFRA_SOURCE_DRIFT");
    for (let i = 0; i < plan.rows.length; i++) await prove(plan.rows[i], false);
  }
  if (mode === "readback") {
    requireValue(peerState.after && observations.every(o => o.after), "PLATFORM_INFRA_INCOMPLETE");
    for (const row of plan.rows) await prove(row, true);
  }
  const preserved = await idle();
  if (records.length) requireValue(fingerprint(preserved) === fingerprint(records[0].preserved), "PLATFORM_INFRA_PINS_CHANGED");
  const output = await open(options["--output"], "wx", 0o600), record = async r => { await output.writeFile(JSON.stringify(r) + "\n"); await output.sync(); };
  try {
    await record({ schema: "kodex.stable-platform-infra-journal/v1", mode, planSHA256: hash, preserved });
    if (["inspect", "readback"].includes(mode)) { await record({ status: mode === "readback" ? "SERVING" : "OBSERVED", complete: mode === "readback" }); return; }
    if (mode === "apply") {
      await assertSource(); await record({ status: "POLICY_INTENT" });
      await patch("networkpolicy", "sso-exact-paths", "identity", peerState.d, identityCachePeers(peerState.d).spec);
      for (const row of plan.rows) {
        requireValue(fingerprint(await refs()) === fingerprint(plan.references) && fingerprint(await idle()) === fingerprint(preserved), "PLATFORM_INFRA_DEPENDENCIES_CHANGED");
        const v = await observe(row), p = platformInfraProfiles.find(p => p.name === row.name);
        let resources;
        if (p.file) resources = JSON.parse(await run("yq", ["eval-all", "-N", "-o=json", "-I=0",
          `select(.kind == "Deployment" and .metadata.name == "${p.name}") | .spec.template.spec.containers[] | select(.name == "${p.container}") | .resources`, root + p.file]));
        const desired = preparePlatformInfra(v.d, p, { image: plan.image, resources, intent: plan.intent });
        requireValue(!v.after && desired.afterSHA256 === row.afterSHA256, "PLATFORM_INFRA_PLAN_DRIFT");
        await assertSource(); await record({ status: "PATCH_INTENT", name: row.name }); await patch("deployment", row.name, row.namespace, v.d, desired.spec);
        await kube(["-n", row.namespace, "rollout", "status", "deployment/" + row.name, "--timeout=300s"], undefined, 330000);
        await prove(row, true); await record({ status: "APPLIED", name: row.name });
      }
    } else {
      for (const row of [...plan.rows].reverse()) {
        const v = await observe(row); if (!v.after) { await prove(row, false); continue; }
        requireValue(records.some(r => r.status === "PATCH_INTENT" && r.name === row.name), "PLATFORM_INFRA_RECOVERY_INTENT_MISSING");
        const spec = applyReverseSpec(v.d.spec, row.inverse);
        requireValue(fingerprint(spec) === row.beforeSHA256, "PLATFORM_INFRA_RECOVERY_INVALID");
        await assertSource(); await record({ status: "RESTORE_INTENT", name: row.name }); await patch("deployment", row.name, row.namespace, v.d, spec);
        await kube(["-n", row.namespace, "rollout", "status", "deployment/" + row.name, "--timeout=300s"], undefined, 330000); await prove(row, false);
      }
      const v = await policy(); if (v.after) {
        requireValue(records.some(r => r.status === "POLICY_INTENT"), "PLATFORM_INFRA_RECOVERY_INTENT_MISSING");
        const spec = applyReverseSpec(v.d.spec, plan.peer.inverse); requireValue(fingerprint(spec) === plan.peer.beforeSHA256, "PLATFORM_INFRA_RECOVERY_INVALID");
        await record({ status: "POLICY_RESTORE_INTENT" }); await patch("networkpolicy", "sso-exact-paths", "identity", v.d, spec);
      }
    }
    requireValue(fingerprint(await refs()) === fingerprint(plan.references) && fingerprint(await idle()) === fingerprint(preserved), "PLATFORM_INFRA_PINS_OR_REFERENCES_CHANGED");
    await record({ status: "COMPLETE" });
  } catch (e) { await record({ status: "UNKNOWN" }); throw e; } finally { await output.close(); }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  releasePlatformInfra(process.argv.slice(2)).then(d => console.log(JSON.stringify({ status: "PASS", phase: process.argv[2],
    ...(d ? { source: d.plan.source, planSHA256: d.planSHA256 } : {}) })))
    .catch(e => { console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(e.message ?? "") ? e.message : "PLATFORM_INFRA_FAILED" })); process.exitCode = 1; });
