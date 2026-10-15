#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { lstat, open, readFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { randomBytes, X509Certificate } from "node:crypto";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { publicIdentityProfile, publicBackend, publicFrontendDeployment, publicKeycloak } from "./stable-public-identity-model.mjs";
import { publicCanonicalRoutes, publicCutoverContent, publicCutoverRow, publicIngressSpec, publicSSOTransport } from "./stable-public-cutover-model.mjs";
import { systemServing } from "./stable-system-helm-state.mjs";
import { requireIdle } from "./runner-policy-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(v, code) { if (!v) throw Error(code); }
const same = (a, b) => fingerprint(a) === fingerprint(b);
const key = r => [r.namespace ?? r.metadata?.namespace, r.kind, r.name ?? r.metadata?.name].join("/");
const scopes = {
  prepare: ["kodex-system/ConfigMap", "kodex-system/Certificate", "identity/Certificate"],
  identity: ["identity/Deployment/sso", "identity/ServersTransport/sso-public", "identity/Ingress/sso"],
  platform: ["kodex-system/ConfigMap/kodex-platform-endpoints", "kodex-system/Deployment/staff-control-center", "kodex-system/Deployment/control-plane",
    "kodex-system/Deployment/control-api-gateway", "kodex-system/Ingress/staff-control-center", "kodex-system/Ingress/staff-control-center-api", "kodex-system/Ingress/staff-control-center-public-assets"],
  canonical: ["kodex-system/Middleware", "kodex-system/Ingress", "identity/Middleware", "identity/Ingress"],
};
const mapping = { Deployment: ["apps/v1", "deployments"], ConfigMap: ["v1", "configmaps"], Certificate: ["cert-manager.io/v1", "certificates"],
  Ingress: ["networking.k8s.io/v1", "ingresses"], ServersTransport: ["traefik.io/v1alpha1", "serverstransports"], Middleware: ["traefik.io/v1alpha1", "middlewares"] };

export function publicLeafProof(pem, caPEM, hostname) {
  let leaf, ca; try { leaf = new X509Certificate(pem); ca = new X509Certificate(caPEM); } catch { throw Error("PUBLIC_CUTOVER_CERTIFICATE_INVALID"); }
  requireValue(ca.ca && !leaf.ca && leaf.verify(ca.publicKey) && leaf.checkHost(hostname, { wildcards: false, subject: "never" }) === hostname &&
    Date.parse(leaf.validFrom) <= Date.now() && Date.parse(leaf.validTo) > Date.now() + 86400000 &&
    Date.parse(ca.validFrom) <= Date.now() && Date.parse(ca.validTo) > Date.parse(leaf.validTo), "PUBLIC_CUTOVER_CERTIFICATE_INVALID");
  return { hostname, fingerprint: leaf.fingerprint256, caFingerprint: ca.fingerprint256, validTo: leaf.validTo };
}

export async function publicCutover(args, port) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const k = args.shift(); requireValue(["--context", "--stage", "--baseline", "--plan", "--journal", "--output", "--approved-plan-sha"].includes(k) &&
      !Object.hasOwn(options, k) && args.length, "PUBLIC_CUTOVER_ARGUMENTS_INVALID"); options[k] = args.shift();
  }
  requireValue(["plan", "apply", "inspect", "readback", "rollback"].includes(mode) && options["--context"] === "k3d-kodex" &&
    options["--output"]?.startsWith("/") && (mode === "plan" ? options["--baseline"] && Object.hasOwn(scopes, options["--stage"]) && !options["--plan"] :
      options["--plan"] && !options["--stage"] && !options["--baseline"]) &&
    (["plan", "apply"].includes(mode) ? !options["--journal"] : options["--journal"]), "PUBLIC_CUTOVER_ARGUMENTS_INVALID");
  const parent = await lstat(dirname(options["--output"]));
  requireValue(parent.isDirectory() && parent.uid === process.getuid() && (parent.mode & 0o777) === 0o700, "PUBLIC_CUTOVER_PRIVATE_DIRECTORY_REQUIRED");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input, timeout = 35000) => {
    try { const p = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 8 << 20 });
      if (input !== undefined) { p.child.stdin.on("error", () => {}); p.child.stdin.end(input); } return (await p).stdout;
    } catch { throw Error("PUBLIC_CUTOVER_OPERATION_FAILED_OR_UNKNOWN"); }
  });
  const context = options["--context"], source = (await run("git", ["rev-parse", "HEAD"])).trim();
  const kube = (argv, input, timeout) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=20s", ...argv], undefined, timeout) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-public-cutover", "--context", context, "--request-timeout=20s", ...argv], input, timeout);
  const get = async (kind, name, ns, optional = false) => {
    const raw = await kube([...(ns ? ["-n", ns] : []), "get", kind, name, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const clusterUID = (await get("namespace", "kube-system")).metadata.uid;
  const assertSource = async () => requireValue(/^[a-f0-9]{40}$/.test(source) && (await run("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim() &&
    (await get("namespace", "kube-system")).metadata.uid === clusterUID, "PUBLIC_CUTOVER_EXECUTOR_OR_CLUSTER_CHANGED");
  await assertSource();
  const profile = JSON.parse(await readFile(root + "infra/public-ingress/identity-profile.json", "utf8")); publicIdentityProfile(profile);
  const idle = async () => {
    const state = JSON.parse(await kube(["-n", "kodex-system", "exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"],
      await readFile(root + "tools/dev/supply-chain-owner-readback.sql", "utf8"))); requireIdle(state);
    return { count: state.promotedArtifactCount, pinsSHA256: state.promotedPinsSHA256 };
  };
  const ca = await get("configmap", "kodex-oidc-ca", "kodex-system");
  requireValue(ca.metadata.uid && typeof ca.data?.["ca.pem"] === "string" && !ca.data["ca.pem"].includes("PRIVATE KEY"), "PUBLIC_CUTOVER_CA_INVALID");
  const boundary = { clusterUID, caUID: ca.metadata.uid, caSHA256: fingerprint(ca.data), owner: await idle() };
  const write = async value => { const f = await open(options["--output"], "wx", 0o600);
    try { await f.writeFile(JSON.stringify(value) + "\n"); await f.sync(); } finally { await f.close(); } };
  const deploymentProfile = (namespace, name) => ({ namespace, name, deployments: [name] });
  const leafProofs = async baseline => {
    const result = [];
    for (const certificate of baseline.certificates) {
      const d = await get("certificate", certificate.metadata.name, certificate.metadata.namespace);
      requireValue(["secretName", "issuerRef", "dnsNames"].every(k => same(d.spec[k], certificate.spec[k])) && d.status?.conditions?.some(c => c.type === "Ready" && c.status === "True" &&
        (c.observedGeneration ?? d.status.observedGeneration) === d.metadata.generation), "PUBLIC_CUTOVER_CERTIFICATE_NOT_READY");
      // Только публичная часть TLS Secret. Private key не читается.
      const pem = await kube(["-n", certificate.metadata.namespace, "get", "secret", certificate.spec.secretName, "-o", "jsonpath={.data.tls\\.crt}"]);
      result.push(publicLeafProof(Buffer.from(pem, "base64"), ca.data["ca.pem"], certificate.spec.dnsNames[0]));
    }
    return result;
  };
  let plan, hash;
  if (mode === "plan") {
    requireValue(!options["--approved-plan-sha"], "PUBLIC_CUTOVER_ARGUMENTS_INVALID");
    const b = await readPrivateJSON(options["--baseline"]), stage = options["--stage"], sourcePlan = b.plan;
    requireValue(b.planSHA256 === fingerprint(sourcePlan) && sourcePlan.source === source && (stage === "prepare" ?
      sourcePlan.schema === "kodex.public-identity-plan/v1" && sourcePlan.clusterUID === clusterUID && sourcePlan.profileSHA256 === fingerprint(profile) :
      sourcePlan.schema === "kodex.public-cutover-plan/v1" && sourcePlan.stage === "prepare" && same(sourcePlan.boundary, boundary)), "PUBLIC_CUTOVER_BASELINE_INVALID");
    const baseline = structuredClone(stage === "prepare" ? sourcePlan : sourcePlan.baseline);
    if (stage === "prepare") {
      baseline.serving = [];
      for (const [namespace, name] of [["identity", "sso"], ...["staff-control-center", "control-plane", "control-api-gateway"].map(n => ["kodex-system", n])])
        baseline.serving.push({ namespace, name, proof: await systemServing(deploymentProfile(namespace, name), kube) });
    }
    const operation = randomBytes(16).toString("hex"), rows = [], serving = [];
    const patchRow = async (kind, name, ns, transform, field = "spec") => {
      const r = await get(kind, name, ns), row = publicCutoverRow(r, field, transform(r));
      const dry = JSON.parse(await kube(["-n", ns, "patch", kind, name, "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "json"],
        JSON.stringify([{ op: "test", path: "/metadata/uid", value: r.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: r.metadata.resourceVersion }, ...row.forward])));
      requireValue(fingerprint(publicCutoverContent(dry)) === row.after, "PUBLIC_CUTOVER_DRY_RUN_DRIFT"); rows.push(row);
      if (kind === "Deployment") {
        const initial = baseline.serving?.find(s => s.namespace === ns && s.name === name);
        requireValue(initial?.proof[0]?.uid === r.metadata.uid && r.spec.replicas === 2, "PUBLIC_CUTOVER_SERVING_BASELINE_MISSING");
        serving.push(initial);
      }
    };
    const createRow = async resource => {
      const r = structuredClone(resource); r.metadata.annotations = { ...r.metadata.annotations, "kodex.dev/public-cutover-operation": operation };
      requireValue(!await get(r.kind, r.metadata.name, r.metadata.namespace, true), "PUBLIC_CUTOVER_CREATE_COLLISION");
      if (r.kind === "Certificate") requireValue(!(await kube(["-n", r.metadata.namespace, "get", "secret", r.spec.secretName, "--ignore-not-found", "-o", "name"])).trim(),
        "PUBLIC_CUTOVER_CERTIFICATE_SECRET_COLLISION");
      const dry = JSON.parse(await kube(["create", "--dry-run=server", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(r)));
      rows.push({ kind: r.kind, name: r.metadata.name, namespace: r.metadata.namespace, uid: null, resource: r,
        after: fingerprint(publicCutoverContent(dry)), before: null });
    };
    if (stage === "prepare") {
      for (const r of [...baseline.certificates, baseline.runtime.resource]) await createRow(r);
    } else if (stage === "identity") {
      await leafProofs(baseline);
      await patchRow("Ingress", "sso", "identity", r => publicIngressSpec(r, baseline.endpoints, profile));
      await patchRow("ServersTransport", "sso-public", "identity", r => publicSSOTransport(r, baseline.endpoints, profile));
      await patchRow("Deployment", "sso", "identity", r => publicKeycloak(r, baseline.endpoints, profile).spec);
    } else if (stage === "platform") {
      await leafProofs(baseline);
      await patchRow("ConfigMap", profile.endpointsName, "kodex-system", r => {
        requireValue(r.metadata.uid === baseline.endpoints.uid && same(r.data, baseline.endpoints.before), "PUBLIC_CUTOVER_ENDPOINT_DRIFT"); return baseline.endpoints.after;
      }, "data");
      for (const name of ["staff-control-center", "control-plane", "control-api-gateway"]) await patchRow("Deployment", name, "kodex-system", r =>
        (name === "staff-control-center" ? publicFrontendDeployment(r, baseline.runtime) : publicBackend(r, baseline.endpoints, profile)).spec);
      for (const name of ["staff-control-center", "staff-control-center-api", "staff-control-center-public-assets"])
        await patchRow("Ingress", name, "kodex-system", r => publicIngressSpec(r, baseline.endpoints, profile));
    } else {
      requireValue(same((await get("configmap", profile.endpointsName, "kodex-system")).data, baseline.endpoints.after), "PUBLIC_CUTOVER_PUBLIC_ENDPOINTS_REQUIRED");
      for (const s of baseline.serving) await systemServing(deploymentProfile(s.namespace, s.name), kube, s.proof, 2);
      for (const r of publicCanonicalRoutes(baseline.endpoints, profile)) await createRow(r);
    }
    plan = { schema: "kodex.public-cutover-plan/v1", source, context, stage, operation, boundary, baselineSHA256: b.planSHA256,
      baseline: { endpoints: baseline.endpoints, certificates: baseline.certificates, runtime: baseline.runtime, serving: baseline.serving }, rows, serving, apply: "NOT_RUN" };
    await assertSource(); requireValue(same(await idle(), boundary.owner), "PUBLIC_CUTOVER_OWNER_CHANGED");
    hash = fingerprint(plan); await write({ plan, planSHA256: hash }); return { status: "PASS", phase: mode, stage, source, planSHA256: hash, apply: "NOT_RUN" };
  }
  const document = await readPrivateJSON(options["--plan"]); plan = document.plan; hash = fingerprint(plan);
  requireValue(document.planSHA256 === hash && plan.schema === "kodex.public-cutover-plan/v1" && plan.source === source && plan.context === context &&
    same(plan.boundary, boundary) && Object.hasOwn(scopes, plan.stage) && /^[a-f0-9]{32}$/.test(plan.operation ?? "") &&
    plan.rows?.length > 0 && plan.rows.length < 12 && new Set(plan.rows.map(key)).size === plan.rows.length && plan.rows.every(r =>
      mapping[r.kind] && ["kodex-system", "identity"].includes(r.namespace) && /^[a-z0-9-]{1,253}$/.test(r.name) &&
      scopes[plan.stage].some(s => key(r) === s || key(r).startsWith(s + "/"))), "PUBLIC_CUTOVER_PLAN_INVALID");
  const initialServing = [["identity", "sso"], ...["staff-control-center", "control-plane", "control-api-gateway"].map(n => ["kodex-system", n])];
  requireValue(plan.baseline?.serving?.length === 4 && initialServing.every(([ns, name]) => plan.baseline.serving.some(s =>
    s.namespace === ns && s.name === name && s.proof?.length === 1 && s.proof[0].name === name && s.proof[0].uid)) &&
    plan.baseline.certificates?.length === 2 && plan.baseline.certificates.every(c =>
      c.kind === "Certificate" && c.metadata.name === (c.metadata.namespace === "identity" ? profile.oidcCertificate : profile.platformCertificate)),
  "PUBLIC_CUTOVER_BASELINE_INVALID");
  if (["prepare", "canonical"].includes(plan.stage)) {
    const expected = plan.stage === "prepare" ? [...plan.baseline.certificates, plan.baseline.runtime.resource] : publicCanonicalRoutes(plan.baseline.endpoints, profile);
    requireValue(plan.rows.length === expected.length && plan.rows.every(row => !row.uid && expected.some(r => key(r) === key(row) &&
      same(row.resource, { ...r, metadata: { ...r.metadata, annotations: { ...r.metadata.annotations, "kodex.dev/public-cutover-operation": plan.operation } } }))),
    "PUBLIC_CUTOVER_CREATE_SCOPE_INVALID");
  } else requireValue(plan.rows.length === scopes[plan.stage].length && plan.rows.every(row => row.uid &&
    row.field === (row.kind === "ConfigMap" ? "data" : "spec")), "PUBLIC_CUTOVER_PATCH_SCOPE_INVALID");
  requireValue(["apply", "rollback"].includes(mode) ? options["--approved-plan-sha"] === hash : !options["--approved-plan-sha"], "PUBLIC_CUTOVER_EXACT_CONFIRMATION_REQUIRED");
  let records = [], receipts = new Map(), intents = new Set();
  if (mode !== "apply") {
    const f = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const s = await f.stat(); requireValue(s.isFile() && s.uid === process.getuid() && s.nlink === 1 && (s.mode & 0o777) === 0o600 && s.size < (1 << 20), "PUBLIC_CUTOVER_JOURNAL_INVALID");
      records = (await f.readFile("utf8")).trim().split("\n").map(JSON.parse);
    } finally { await f.close(); }
    const header = records.shift(); requireValue(header.schema === "kodex.public-cutover-journal/v1" && header.mode === "apply" &&
      header.planSHA256 === hash && ["COMPLETE", "UNKNOWN"].includes(records.at(-1)?.status), "PUBLIC_CUTOVER_JOURNAL_INVALID");
    for (const r of records) {
      if (r.status === "INTENT") { requireValue(plan.rows.some(row => key(row) === r.key) && !intents.has(r.key), "PUBLIC_CUTOVER_JOURNAL_INVALID"); intents.add(r.key); }
      if (r.status === "APPLIED") { requireValue(intents.has(r.key) && r.uid, "PUBLIC_CUTOVER_JOURNAL_INVALID"); receipts.set(r.key, r.uid); }
    }
  }
  const boundaryProof = async () => {
    await assertSource(); const currentCA = await get("configmap", "kodex-oidc-ca", "kodex-system");
    requireValue(currentCA.metadata.uid === boundary.caUID && fingerprint(currentCA.data) === boundary.caSHA256 &&
      same(await idle(), boundary.owner), "PUBLIC_CUTOVER_BOUNDARY_CHANGED");
  };
  const observe = async row => {
    const r = await get(row.kind, row.name, row.namespace, !row.uid);
    if (!r) return { phase: "before", resource: null };
    const h = fingerprint(publicCutoverContent(r));
    requireValue(!r.metadata.deletionTimestamp && (row.uid ? r.metadata.uid === row.uid :
      intents.has(key(row)) && r.metadata.annotations?.["kodex.dev/public-cutover-operation"] === plan.operation &&
        (!receipts.has(key(row)) || receipts.get(key(row)) === r.metadata.uid)), "PUBLIC_CUTOVER_FOREIGN_RESOURCE");
    requireValue(h === row.after || row.uid && h === row.before, "PUBLIC_CUTOVER_RESOURCE_DRIFT");
    return { phase: h === row.after ? "after" : "before", resource: r };
  };
  const prove = async row => {
    const state = await observe(row); requireValue(state.phase === "after", "PUBLIC_CUTOVER_INCOMPLETE");
    if (row.kind === "Deployment") {
      const p = deploymentProfile(row.namespace, row.name), previous = plan.serving.find(s => s.namespace === row.namespace && s.name === row.name);
      requireValue(previous, "PUBLIC_CUTOVER_SERVING_BASELINE_MISSING"); await systemServing(p, kube, previous.proof, 2);
    }
  };
  await boundaryProof();
  for (const row of plan.rows) {
    const state = await observe(row); if (mode === "apply") requireValue(state.phase === "before", "PUBLIC_CUTOVER_APPLY_ALREADY_STARTED");
  }
  if (mode === "readback") {
    for (const row of plan.rows) await prove(row);
    if (plan.stage === "prepare") await leafProofs(plan.baseline);
    await boundaryProof(); await write({ schema: "kodex.public-cutover-readback/v1", status: "PASS", source, stage: plan.stage, planSHA256: hash,
      certificateProofs: plan.stage === "prepare" ? await leafProofs(plan.baseline) : [], owner: await idle() });
    return { status: "PASS", phase: mode, stage: plan.stage, source, planSHA256: hash };
  }
  if (mode === "inspect") { await write({ status: "PASS", planSHA256: hash, states: await Promise.all(plan.rows.map(async r => ({ key: key(r), phase: (await observe(r)).phase }))) });
    return { status: "PASS", phase: mode, stage: plan.stage }; }
  // Один canonical recovery claim: новый --output не разрешает повторить
  // восстановление после неизвестного результата предыдущего rollback.
  let recovery;
  if (mode === "rollback") {
    // Prepare нельзя удалять после того, как ссылки уже активированы.
    if (plan.rows.some(r => !r.uid)) for (const ns of ["identity", "kodex-system"]) {
      const resources = JSON.parse(await kube(["-n", ns, "get", "deployments,ingresses", "-o", "json"]));
      requireValue(!plan.rows.some(row => row.namespace === ns && !row.uid && resources.items.some(r =>
        !plan.rows.some(own => !own.uid && own.namespace === ns && own.kind === r.kind && own.name === r.metadata.name) &&
        JSON.stringify(r.spec).includes('"' + row.name + '"'))), "PUBLIC_CUTOVER_CREATED_RESOURCE_IN_USE");
    }
    recovery = await open(options["--journal"] + ".recovery.jsonl", "wx", 0o600);
  }
  const f = await open(options["--output"], "wx", 0o600);
  const record = async value => { const line = JSON.stringify(value) + "\n"; await f.writeFile(line); await f.sync();
    if (recovery) { await recovery.writeFile(line); await recovery.sync(); } };
  try {
    await record({ schema: "kodex.public-cutover-journal/v1", mode, source, planSHA256: hash });
    for (const row of mode === "rollback" ? [...plan.rows].reverse() : plan.rows) {
      await boundaryProof(); const state = await observe(row);
      if (mode === "rollback" && state.phase === "before") continue;
      requireValue(mode === "rollback" ? state.phase === "after" : state.phase === "before", "PUBLIC_CUTOVER_PHASE_CHANGED");
      await record({ status: mode === "apply" ? "INTENT" : "RESTORE_INTENT", key: key(row) });
      if (mode === "apply") intents.add(key(row));
      let changed;
      if (!row.uid && mode === "apply") changed = JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(row.resource)));
      else if (!row.uid) {
        const [api, plural] = mapping[row.kind], path = (api === "v1" ? "/api/v1" : "/apis/" + api) + "/namespaces/" + row.namespace + "/" + plural + "/" + row.name;
        await kube(["delete", "--raw", path, "-f", "/dev/stdin"], JSON.stringify({ apiVersion: "v1", kind: "DeleteOptions", preconditions: {
          uid: state.resource.metadata.uid, resourceVersion: state.resource.metadata.resourceVersion } }));
        requireValue(!await get(row.kind, row.name, row.namespace, true), "PUBLIC_CUTOVER_DELETE_PENDING");
      } else changed = JSON.parse(await kube(["-n", row.namespace, "patch", row.kind, row.name, "--type=json", "--patch-file=/dev/stdin", "-o", "json"],
        JSON.stringify([{ op: "test", path: "/metadata/uid", value: row.uid }, { op: "test", path: "/metadata/resourceVersion", value: state.resource.metadata.resourceVersion },
          { op: "test", path: "/" + row.field, value: state.resource[row.field] }, ...(mode === "apply" ? row.forward : row.inverse)])));
      if (changed) {
        requireValue(fingerprint(publicCutoverContent(changed)) === (mode === "apply" ? row.after : row.before), "PUBLIC_CUTOVER_ACTIVATION_UNPROVEN");
        receipts.set(key(row), changed.metadata.uid);
        if (row.kind === "Deployment") {
          await kube(["-n", row.namespace, "rollout", "status", "deployment/" + row.name, "--timeout=180s"], undefined, 195000);
          const previous = plan.serving.find(s => s.namespace === row.namespace && s.name === row.name);
          await systemServing(deploymentProfile(row.namespace, row.name), kube, previous.proof, 2);
        }
      }
      await record({ status: mode === "apply" ? "APPLIED" : "RESTORED", key: key(row), uid: changed?.metadata.uid ?? state.resource?.metadata.uid });
    }
    if (mode === "apply" && plan.stage === "prepare") {
      for (const cert of plan.baseline.certificates) await kube(["-n", cert.metadata.namespace, "wait", "--for=condition=Ready", "certificate/" + cert.metadata.name, "--timeout=90s"], undefined, 105000);
      await leafProofs(plan.baseline);
    }
    await boundaryProof(); await record({ status: "COMPLETE" });
    return { status: "PASS", phase: mode, stage: plan.stage, source, planSHA256: hash };
  } catch (error) { await record({ status: "UNKNOWN" }); throw error; }
  finally { await f.close(); await recovery?.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  publicCutover(process.argv.slice(2)).then(r => console.log(JSON.stringify(r))).catch(error => {
    console.error(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "PUBLIC_CUTOVER_FAILED" })); process.exitCode = 1;
  });
