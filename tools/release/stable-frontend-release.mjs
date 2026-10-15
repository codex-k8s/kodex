#!/usr/bin/env node
import { execFile } from "node:child_process";
import { createHash, X509Certificate } from "node:crypto";
import { constants } from "node:fs";
import { open, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { readPrivateJSON } from "../../infra/public-ingress/edge-plan.mjs";
import { fingerprint } from "./stable-inventory.mjs";
import { prepareFrontendCutover } from "./stable-frontend-model.mjs";
import { frontendIntentAnnotation, frontendRuntimeSnapshot, frontendRouting, frontendTransportAnnotation } from "./stable-frontend-transition.mjs";
import { applyReverseSpec } from "./stable-reverse-patch.mjs";
import { ownedDeploymentPods } from "./stable-archive-transition.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
const folder = fileURLToPath(new URL("../../deploy/k8s/base/staff-control-center/", import.meta.url));
const namespace = "kodex-system", name = "staff-control-center";
const kinds = { Deployment: "deployment", Service: "service", Ingress: "ingress", NetworkPolicy: "networkpolicy", ConfigMap: "configmap", ServersTransport: "serverstransport" };
function requireValue(value, code) { if (!value) throw Error(code); }
async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(JSON.stringify(value) + "\n"); await file.sync(); } finally { await file.close(); }
}
const annotationsHash = value => fingerprint(value.metadata.annotations ?? {});
const publicChange = value => { const { patch: _patch, ...result } = value; return result; };

async function main(args) {
  const mode = args.shift(), options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--release", "--plan", "--output", "--approved-plan-sha", "--journal"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "FRONTEND_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(["plan", "apply", "readback", "inspect", "rollback"].includes(mode) &&
    /^k3d-[a-z0-9-]+$/.test(context ?? "") && options["--output"], "FRONTEND_ARGUMENTS_INVALID");
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = async (program, argv, input, timeout = 35000) => {
    try {
      const pending = execute(program, argv, { cwd: root, env, timeout, maxBuffer: 8 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("FRONTEND_OPERATION_FAILED_OR_UNKNOWN"); }
  };
  const kube = (argv, input, timeout) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=30s", ...argv], undefined, timeout) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-frontend-pipe", "--context", context, "--request-timeout=30s", ...argv], input, timeout);
  const get = async (kind, resourceName, optional = false) => {
    const raw = await kube(["-n", namespace, "get", kinds[kind], resourceName, "-o", "json", ...(optional ? ["--ignore-not-found"] : [])]);
    return raw.trim() ? JSON.parse(raw) : null;
  };
  const source = (await run("git", ["rev-parse", "HEAD"])).trim();
  requireValue(/^[a-f0-9]{40}$/.test(source) && (await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim() === "",
    "FRONTEND_CLEAN_SOURCE_REQUIRED");
  const clusterUID = JSON.parse(await kube(["get", "namespace", "kube-system", "-o", "json"])).metadata.uid;
  const appUID = JSON.parse(await kube(["get", "namespace", namespace, "-o", "json"])).metadata.uid;
  const yaml = async path => JSON.parse(await run("yq", ["-o=json", "-I=0", path]));
  const canonical = await yaml(folder + "deployment.yaml"), transport = await yaml(folder + "servers-transport.yaml");
  transport.metadata.namespace = namespace;
  const podList = async () => JSON.parse(await kube(["-n", namespace, "get", "replicasets,pods", "-l", "app.kubernetes.io/name=" + name, "-o", "json"]));
  const referencePins = async runtimeName => {
    const runtime = await get("ConfigMap", runtimeName);
    const pins = [{ kind: "ConfigMap", name: runtimeName, uid: runtime.metadata.uid, version: runtime.metadata.resourceVersion,
      dataSHA256: fingerprint(runtime.data) }];
    for (const [secretName, serverName] of [["staff-control-center-backend-tls", "staff-control-center.kodex-system.svc.cluster.local"],
      ["staff-control-center-ingress-client-tls", null], ["control-api-gateway-public-tls-material", "control-api.kodex.local"]]) {
      // Только публичные certificates/CA и metadata, private key не читается.
      const raw = await kube(["-n", namespace, "get", "secret", secretName, "-o",
        'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.data.tls\\.crt}{"|"}{.data.ca\\.crt}']);
      const [uid, version, certificate, ca] = raw.split("|");
      requireValue(uid && /^[0-9]+$/.test(version ?? "") && certificate && ca, "FRONTEND_TLS_REFERENCE_INVALID");
      const cert = new X509Certificate(Buffer.from(certificate, "base64")), authority = new X509Certificate(Buffer.from(ca, "base64"));
      requireValue(cert.verify(authority.publicKey) && Date.parse(cert.validFrom) <= Date.now() && Date.parse(cert.validTo) > Date.now() &&
        (!serverName || cert.checkHost(serverName)), "FRONTEND_TLS_IDENTITY_INVALID");
      pins.push({ kind: "Secret", name: secretName, uid, version, certificateSHA256: createHash("sha256").update(cert.raw).digest("hex"),
        caSHA256: createHash("sha256").update(authority.raw).digest("hex") });
    }
    for (const policyName of ["staff-control-center-exact-egress", "control-api-gateway-exact-runtime-paths"]) {
      const policy = await get("NetworkPolicy", policyName);
      pins.push({ kind: "NetworkPolicy", name: policyName, uid: policy.metadata.uid, specSHA256: fingerprint(policy.spec) });
    }
    return pins;
  };
  const prepare = async release => {
    const deployment = await get("Deployment", name);
    const runtimeName = deployment.spec.template.spec.volumes.find(v => v.name === "runtime-config")?.configMap?.name;
    requireValue(runtimeName, "FRONTEND_RUNTIME_CONFIG_REQUIRED");
    const runtime = frontendRuntimeSnapshot(await get("ConfigMap", runtimeName), await readFile(folder + "security-headers.conf", "utf8"),
      await readFile(folder + "public-assets.conf", "utf8"));
    const desired = prepareFrontendCutover(deployment, canonical.spec.template, release, runtime);
    const dry = JSON.parse(await kube(["-n", namespace, "patch", "deployment", name, "--type=json", "--dry-run=server",
      "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify(desired.patch)));
    requireValue(fingerprint(dry.spec) === desired.afterSpecSHA256, "FRONTEND_SERVER_DEFAULTS_MISMATCH");
    const routes = frontendRouting(await get("Service", name), await Promise.all([name, name + "-api", name + "-public-assets"].map(n => get("Ingress", n))),
      await get("NetworkPolicy", "staff-control-center-ingress"), transport);
    const created = [transport, runtime.resource];
    for (const value of created) requireValue(await get(value.kind, value.metadata.name, true) === null, "FRONTEND_NEW_RESOURCE_COLLISION");
    const before = deployment.spec, after = desired.patch.at(-1).value;
    return { release, deployment: publicChange(desired), routes: routes.map(publicChange), created, beforeReplicas: before.replicas ?? 1,
      references: await referencePins(runtimeName), sourceRuntimeName: runtimeName,
      quiescedBeforeSpecSHA256: fingerprint({ ...before, replicas: 0 }), quiescedAfterSpecSHA256: fingerprint({ ...after, replicas: 0 }),
      prepared: { deployment: desired, routes } };
  };
  if (mode === "plan") {
    requireValue(options["--release"] && !options["--plan"] && !options["--approved-plan-sha"] && !options["--journal"], "FRONTEND_ARGUMENTS_INVALID");
    const { prepared: _prepared, ...value } = await prepare(await readPrivateJSON(options["--release"]));
    const plan = { schema: "kodex.stable-frontend/v1", context, executorSHA: source, clusterUID, appUID, ...value };
    await createPrivate(options["--output"], plan);
    process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", planSHA256: fingerprint(plan) }) + "\n"); return;
  }
  requireValue(options["--plan"] && !options["--release"], "FRONTEND_ARGUMENTS_INVALID");
  const plan = await readPrivateJSON(options["--plan"]), hash = fingerprint(plan);
  requireValue(plan.schema === "kodex.stable-frontend/v1" && plan.context === context && plan.executorSHA === source &&
    plan.clusterUID === clusterUID && plan.appUID === appUID && plan.routes?.length === 5 && plan.created?.length === 2,
    "FRONTEND_PLAN_IDENTITY_INVALID");
  requireValue(["readback", "inspect"].includes(mode) ? !options["--approved-plan-sha"] : options["--approved-plan-sha"] === hash,
    "FRONTEND_EXACT_OWNER_CONFIRMATION_REQUIRED");
  const expectedKeys = ["NetworkPolicy/staff-control-center-ingress", "Service/staff-control-center", "Ingress/staff-control-center",
    "Ingress/staff-control-center-api", "Ingress/staff-control-center-public-assets"].sort();
  requireValue(fingerprint(plan.routes.map(r => `${r.kind}/${r.name}`).sort()) === fingerprint(expectedKeys) &&
    plan.created.every(r => r.metadata?.namespace === namespace) && plan.created.filter(r => r.kind === "ServersTransport" && r.metadata.name === name).length === 1 &&
    plan.created.filter(r => r.kind === "ConfigMap" && /^staff-control-center-runtime-stable-[a-f0-9]{16}$/.test(r.metadata.name)).length === 1,
    "FRONTEND_PLAN_RESOURCE_SCOPE_INVALID");
  const createdUIDs = new Map();
  requireValue(mode === "apply" ? !options["--journal"] : mode === "inspect" || options["--journal"], "FRONTEND_APPLY_JOURNAL_REQUIRED");
  if (options["--journal"]) {
    const file = await open(options["--journal"], constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = await file.stat();
      requireValue(stat.isFile() && stat.uid === process.getuid() && stat.nlink === 1 && (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= 1 << 20,
        "FRONTEND_PRIVATE_JOURNAL_REQUIRED");
      const rows = (await file.readFile("utf8")).trim().split("\n").map(JSON.parse), header = rows.shift();
      requireValue(header.schema === "kodex.stable-frontend-journal/v1" && header.mode === "apply" && header.planSHA256 === hash && rows.length <= 256,
        "FRONTEND_APPLY_JOURNAL_MISMATCH");
      for (const row of rows.filter(r => r.status === "CREATED")) {
        const key = `${row.kind}/${row.name}`;
        requireValue(typeof row.uid === "string" && row.uid && !createdUIDs.has(key) && plan.created.some(r => `${r.kind}/${r.metadata.name}` === key),
          "FRONTEND_APPLY_JOURNAL_MISMATCH");
        createdUIDs.set(key, row.uid);
      }
    } finally { await file.close(); }
  }
  const assertReferences = async () => requireValue(fingerprint(await referencePins(plan.sourceRuntimeName)) === fingerprint(plan.references), "FRONTEND_REFERENCE_DRIFT");
  const matches = (resource, entry, phase) => resource?.metadata?.uid === entry.uid && fingerprint(resource.spec) === entry[phase + "SpecSHA256"] &&
    (!entry.beforeAnnotationsSHA256 || annotationsHash(resource) === entry[phase + "AnnotationsSHA256"]);
  const patch = async (kind, resourceName, body) => kube(["-n", namespace, "patch", kinds[kind], resourceName, "--type=json", "--patch-file=/dev/stdin", "-o", "json"], JSON.stringify(body));
  const ownedCreated = (resource, expected) => resource?.metadata?.namespace === namespace && resource.metadata.name === expected.metadata.name &&
    (!createdUIDs.has(`${expected.kind}/${expected.metadata.name}`) || createdUIDs.get(`${expected.kind}/${expected.metadata.name}`) === resource.metadata.uid) &&
    resource.metadata.annotations?.[frontendIntentAnnotation] === hash && !resource.metadata.ownerReferences?.length && !resource.metadata.finalizers?.length &&
    (expected.kind === "ConfigMap" ? resource.immutable === true && fingerprint(resource.data) === fingerprint(expected.data) && !Object.keys(resource.binaryData ?? {}).length :
      fingerprint(resource.spec) === fingerprint(expected.spec));
  const quiesce = async () => {
    const current = await get("Deployment", name);
    requireValue(current.metadata.uid === plan.deployment.uid && [plan.deployment.beforeSpecSHA256, plan.deployment.afterSpecSHA256,
      plan.quiescedBeforeSpecSHA256, plan.quiescedAfterSpecSHA256].includes(fingerprint(current.spec)), "FRONTEND_QUIESCE_DRIFT");
    await patch("Deployment", name, [{ op: "test", path: "/metadata/uid", value: current.metadata.uid },
      { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
      { op: "test", path: "/spec", value: current.spec }, { op: "replace", path: "/spec/replicas", value: 0 }]);
    const deadline = Date.now() + 120000;
    for (;;) {
      const value = await get("Deployment", name);
      requireValue(value.metadata.uid === plan.deployment.uid && [plan.quiescedBeforeSpecSHA256, plan.quiescedAfterSpecSHA256].includes(fingerprint(value.spec)), "FRONTEND_QUIESCE_DRIFT");
      if (value.status?.observedGeneration >= value.metadata.generation && !(value.status.replicas ?? 0) &&
        ownedDeploymentPods(await podList(), plan.deployment.uid).length === 0) return value;
      requireValue(Date.now() < deadline, "FRONTEND_QUIESCE_TIMEOUT"); await new Promise(resolve => setTimeout(resolve, 250));
    }
  };
  const readback = async () => {
    await assertReferences();
    const current = await get("Deployment", name), entry = plan.deployment;
    requireValue(matches(current, entry, "after") && current.status.observedGeneration >= current.metadata.generation &&
      ["replicas", "readyReplicas", "availableReplicas", "updatedReplicas"].every(k => current.status[k] === 2), "FRONTEND_READBACK_INCOMPLETE");
    const pods = ownedDeploymentPods(await podList(), entry.uid), digest = plan.release.image.split("@").at(-1);
    requireValue(pods.length === 2 && pods.every(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === "Ready" && c.status === "True") &&
      p.spec.containers?.length === 1 && p.spec.containers[0].image === plan.release.image && !p.spec.containers[0].command &&
      !p.spec.volumes?.some(v => v.hostPath) && p.status.containerStatuses?.some(c => c.ready && c.imageID?.endsWith(digest))), "FRONTEND_SERVING_POD_MISMATCH");
    for (const route of plan.routes) requireValue(matches(await get(route.kind, route.name), route, "after"), "FRONTEND_ROUTE_READBACK_MISMATCH");
    for (const resource of plan.created) requireValue(ownedCreated(await get(resource.kind, resource.metadata.name), resource), "FRONTEND_CREATED_RESOURCE_DRIFT");
    return { status: "PASS", phase: "COMPILED_FRONTEND_ROUTE_ONLY", pods: pods.map(p => p.metadata.uid), browser: "NOT_RUN" };
  };
  if (mode === "readback") {
    const result = await readback(); await createPrivate(options["--output"], { planSHA256: hash, ...result });
    process.stdout.write(JSON.stringify(result) + "\n"); return;
  }
  if (mode === "inspect") {
    await assertReferences(); const current = await get("Deployment", name), spec = fingerprint(current.spec);
    requireValue(current.metadata.uid === plan.deployment.uid, "FRONTEND_INSPECTION_IDENTITY_DRIFT");
    const phase = new Map([[plan.deployment.beforeSpecSHA256, "SOURCE_UNCHANGED"], [plan.quiescedBeforeSpecSHA256, "SOURCE_QUIESCED"],
      [plan.deployment.afterSpecSHA256, "COMPILED"], [plan.quiescedAfterSpecSHA256, "COMPILED_QUIESCED"]]).get(spec);
    requireValue(phase, "FRONTEND_INSPECTION_SPEC_DRIFT");
    const routes = [];
    for (const route of plan.routes) {
      const value = await get(route.kind, route.name);
      requireValue(matches(value, route, "before") || matches(value, route, "after"), "FRONTEND_ROUTE_DRIFT");
      routes.push({ name: route.name, kind: route.kind, phase: matches(value, route, "after") ? "AFTER" : "BEFORE" });
    }
    const created = [];
    for (const resource of plan.created) {
      const current = await get(resource.kind, resource.metadata.name, true);
      requireValue(current === null || ownedCreated(current, resource), "FRONTEND_CREATED_RESOURCE_DRIFT");
      created.push({ kind: resource.kind, name: resource.metadata.name, phase: current === null ? "ABSENT" : "OWNED", uid: current?.metadata.uid });
    }
    const result = { status: "OBSERVED", phase, complete: false, routes, created };
    await createPrivate(options["--output"], { planSHA256: hash, ...result }); process.stdout.write(JSON.stringify(result) + "\n"); return;
  }
  await assertReferences();
  if (mode === "apply") {
    const { prepared: _prepared, ...fresh } = await prepare(plan.release);
    const { schema: _schema, context: _context, executorSHA: _source, clusterUID: _cluster, appUID: _app, ...expected } = plan;
    requireValue(fingerprint(fresh) === fingerprint(expected), "FRONTEND_PLAN_DRIFT");
  } else {
    const current = await get("Deployment", name);
    requireValue(current.metadata.uid === plan.deployment.uid && [plan.deployment.beforeSpecSHA256, plan.deployment.afterSpecSHA256,
      plan.quiescedBeforeSpecSHA256, plan.quiescedAfterSpecSHA256].includes(fingerprint(current.spec)), "FRONTEND_ROLLBACK_DEPLOYMENT_DRIFT");
    for (const route of plan.routes) { const value = await get(route.kind, route.name);
      requireValue(matches(value, route, "before") || matches(value, route, "after"), "FRONTEND_ROLLBACK_ROUTE_DRIFT"); }
    for (const value of plan.created) { const current = await get(value.kind, value.metadata.name, true);
      requireValue(current === null || ownedCreated(current, value), "FRONTEND_ROLLBACK_CREATED_RESOURCE_DRIFT"); }
  }
  await createPrivate(options["--output"], { schema: "kodex.stable-frontend-journal/v1", mode, planSHA256: hash, status: "IN_PROGRESS" });
  const journal = await open(options["--output"], "a");
  const record = async value => { await journal.writeFile(JSON.stringify(value) + "\n"); await journal.sync(); };
  try {
    if (mode === "apply") {
      for (const value of plan.created) {
        const resource = structuredClone(value); resource.metadata.annotations = { ...resource.metadata.annotations, [frontendIntentAnnotation]: hash };
        requireValue(await get(value.kind, value.metadata.name, true) === null, "FRONTEND_NEW_RESOURCE_COLLISION");
        await record({ kind: value.kind, name: value.metadata.name, status: "CREATE_ATTEMPT" });
        const created = JSON.parse(await kube(["create", "-f", "/dev/stdin", "-o", "json"], JSON.stringify(resource)));
        requireValue(ownedCreated(created, value), "FRONTEND_CREATED_RESOURCE_DRIFT");
        createdUIDs.set(`${value.kind}/${value.metadata.name}`, created.metadata.uid);
        await record({ kind: value.kind, name: value.metadata.name, uid: created.metadata.uid, status: "CREATED" });
      }
      await record({ name, status: "QUIESCE_ATTEMPT" }); const stopped = await quiesce();
      requireValue(fingerprint(stopped.spec) === plan.quiescedBeforeSpecSHA256, "FRONTEND_SOURCE_QUIESCE_DRIFT");
      for (const route of plan.routes.filter(r => r.kind === "NetworkPolicy")) {
        const current = await get(route.kind, route.name); requireValue(matches(current, route, "before"), "FRONTEND_ROUTE_DRIFT");
        const after = applyReverseSpec(current.spec, []); after.ingress[0].ports = [{ port: 8443, protocol: "TCP" }];
        requireValue(fingerprint(after) === route.afterSpecSHA256, "FRONTEND_ROUTE_PLAN_INVALID");
        await record({ name: route.name, status: "POLICY_PATCH_ATTEMPT" });
        await patch(route.kind, route.name, [{ op: "test", path: "/metadata/uid", value: current.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion },
          { op: "test", path: "/spec", value: current.spec }, { op: "replace", path: "/spec", value: after }]);
      }
      const runtime = plan.created.find(r => r.kind === "ConfigMap");
      const desired = prepareFrontendCutover(stopped, canonical.spec.template, plan.release,
        { name: runtime.metadata.name, revision: JSON.parse(runtime.data["runtime-config.json"]).revision });
      requireValue(desired.afterSpecSHA256 === plan.deployment.afterSpecSHA256, "FRONTEND_ACTIVATION_DRIFT");
      await assertReferences(); await record({ name, status: "COMPILED_PATCH_ATTEMPT" }); await patch("Deployment", name, desired.patch);
      await kube(["-n", namespace, "rollout", "status", "deployment/" + name, "--timeout=300s"], undefined, 335000);
      for (const entry of plan.routes.filter(r => r.kind !== "NetworkPolicy")) {
        const current = await get(entry.kind, entry.name); requireValue(matches(current, entry, "before"), "FRONTEND_ROUTE_DRIFT");
        const after = structuredClone(current.spec);
        if (entry.kind === "Service") after.ports = [{ name: "https", port: 8443, protocol: "TCP", targetPort: "https" }];
        else for (const path of after.rules[0].http.paths) path.backend.service = { name, port: { name: "https" } };
        requireValue(fingerprint(after) === entry.afterSpecSHA256, "FRONTEND_ROUTE_PLAN_INVALID");
        await record({ kind: entry.kind, name: entry.name, status: "ROUTE_PATCH_ATTEMPT" });
        await patch(entry.kind, entry.name, [{ op: "test", path: "/metadata/uid", value: current.metadata.uid },
          { op: "test", path: "/metadata/resourceVersion", value: current.metadata.resourceVersion }, { op: "test", path: "/spec", value: current.spec },
          ...(entry.annotationAfter ? [{ op: "add", path: "/metadata/annotations", value: { ...current.metadata.annotations, [frontendTransportAnnotation]: entry.annotationAfter } }] : []),
          { op: "replace", path: "/spec", value: after }]);
      }
      const result = await readback(); await record(result); process.stdout.write(JSON.stringify(result) + "\n");
    } else {
      const current = await get("Deployment", name), changed = fingerprint(current.spec) !== plan.deployment.beforeSpecSHA256;
      if (changed) { await record({ name, status: "QUIESCE_ATTEMPT" }); await quiesce(); }
      for (const entry of [...plan.routes].reverse()) {
        const value = await get(entry.kind, entry.name);
        if (matches(value, entry, "before")) continue;
        requireValue(matches(value, entry, "after"), "FRONTEND_ROLLBACK_ROUTE_DRIFT");
        requireValue(fingerprint(applyReverseSpec(value.spec, entry.inverse)) === entry.beforeSpecSHA256, "FRONTEND_ROLLBACK_RECIPE_INVALID");
        await record({ kind: entry.kind, name: entry.name, status: "ROLLBACK_PATCH_ATTEMPT" });
        await patch(entry.kind, entry.name, [{ op: "test", path: "/metadata/uid", value: value.metadata.uid },
          { op: "test", path: "/metadata/resourceVersion", value: value.metadata.resourceVersion }, { op: "test", path: "/spec", value: value.spec }, ...entry.inverse,
          ...(entry.annotationAfter ? [{ op: entry.annotationBefore === null ? "remove" : "replace", path: "/metadata/annotations/traefik.ingress.kubernetes.io~1service.serverstransport",
            ...(entry.annotationBefore === null ? {} : { value: entry.annotationBefore }) }] : [])]);
      }
      if (changed) {
        const stopped = await get("Deployment", name), spec = structuredClone(stopped.spec); spec.replicas = 2;
        const inverse = fingerprint(stopped.spec) === plan.quiescedAfterSpecSHA256 ? plan.deployment.inverse : [];
        const restored = inverse.length ? applyReverseSpec(spec, inverse) : { ...stopped.spec, replicas: plan.beforeReplicas };
        requireValue(fingerprint(restored) === plan.deployment.beforeSpecSHA256, "FRONTEND_ROLLBACK_RECIPE_INVALID");
        await record({ name, status: "SOURCE_RESTORE_ATTEMPT" });
        await patch("Deployment", name, [{ op: "test", path: "/metadata/uid", value: stopped.metadata.uid },
          { op: "test", path: "/metadata/resourceVersion", value: stopped.metadata.resourceVersion }, { op: "test", path: "/spec", value: stopped.spec },
          { op: "replace", path: "/spec", value: restored }]);
        await kube(["-n", namespace, "rollout", "status", "deployment/" + name, "--timeout=300s"], undefined, 335000);
      }
      for (const value of [...plan.created].reverse()) {
        const resource = await get(value.kind, value.metadata.name, true); if (resource === null) continue;
        requireValue(ownedCreated(resource, value), "FRONTEND_ROLLBACK_CREATED_RESOURCE_DRIFT");
        await record({ kind: value.kind, name: value.metadata.name, status: "DELETE_ATTEMPT" });
        const prefix = value.kind === "ConfigMap" ? "/api/v1" : "/apis/traefik.io/v1alpha1";
        await kube(["delete", `--raw=${prefix}/namespaces/${namespace}/${value.kind === "ConfigMap" ? "configmaps" : "serverstransports"}/${value.metadata.name}`, "-f", "/dev/stdin"],
          JSON.stringify({ apiVersion: "v1", kind: "DeleteOptions", preconditions: { uid: resource.metadata.uid, resourceVersion: resource.metadata.resourceVersion } }));
      }
      await assertReferences(); const restored = await get("Deployment", name);
      requireValue(matches(restored, plan.deployment, "before"), "FRONTEND_ROLLBACK_READBACK_MISMATCH");
      for (const route of plan.routes) requireValue(matches(await get(route.kind, route.name), route, "before"), "FRONTEND_ROLLBACK_ROUTE_DRIFT");
      await record({ status: "PASS", phase: "SOURCE_PROFILE_RESTORED", browser: "NOT_RUN" });
      process.stdout.write(JSON.stringify({ status: "PASS", phase: "SOURCE_PROFILE_RESTORED", browser: "NOT_RUN" }) + "\n");
    }
  } catch (error) { await record({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "FRONTEND_TRANSITION_FAILED" }); throw error; }
  finally { await journal.close(); }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main(process.argv.slice(2)).catch(error => { process.stderr.write(JSON.stringify({ status: "FAIL_OR_UNKNOWN", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "FRONTEND_TRANSITION_FAILED" }) + "\n"); process.exitCode = 1; });
