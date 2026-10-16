#!/usr/bin/env node
import { execFile } from "node:child_process";
import { lstat, open, readFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { fingerprint } from "./stable-inventory.mjs";
import { publicBackend, publicCertificates, publicEndpoints, publicFrontendDeployment, publicFrontendRuntime, publicIdentityProfile, publicKeycloak, publicOAuthProxy } from "./stable-public-identity-model.mjs";

const execute = promisify(execFile), root = fileURLToPath(new URL("../../", import.meta.url));
function requireValue(value, code) { if (!value) throw Error(code); }

export async function planPublicIdentity(args, port) {
  const options = {};
  while (args.length) {
    const key = args.shift(); requireValue(["--context", "--output"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "PUBLIC_IDENTITY_ARGUMENTS_INVALID"); options[key] = args.shift();
  }
  requireValue(options["--context"] === "k3d-kodex" && options["--output"]?.startsWith("/"), "PUBLIC_IDENTITY_ARGUMENTS_INVALID");
  const parent = await lstat(dirname(options["--output"]));
  requireValue(parent.isDirectory() && parent.uid === process.getuid() && (parent.mode & 0o777) === 0o700, "PUBLIC_IDENTITY_PRIVATE_DIRECTORY_REQUIRED");
  const context = options["--context"], env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const run = port ?? (async (program, argv, input) => {
    try {
      const pending = execute(program, argv, { cwd: root, env, encoding: "utf8", timeout: 45000, maxBuffer: 8 << 20 });
      if (input !== undefined) { pending.child.stdin.on("error", () => {}); pending.child.stdin.end(input); }
      return (await pending).stdout;
    } catch { throw Error("PUBLIC_IDENTITY_PREFLIGHT_FAILED"); }
  });
  const kube = (argv, input) => input === undefined ? run("kubectl", ["--context", context, "--request-timeout=30s", ...argv]) :
    run("/bin/sh", ["-c", 'cat | kubectl "$@"', "kodex-public-identity-plan", "--context", context, "--request-timeout=30s", ...argv], input);
  const get = async (kind, name, namespace) => JSON.parse(await kube([...(namespace ? ["-n", namespace] : []), "get", kind, name, "-o", "json"]));
  const source = (await run("git", ["rev-parse", "HEAD"])).trim();
  const assertSource = async () => requireValue(/^[a-f0-9]{40}$/.test(source) && (await run("git", ["rev-parse", "HEAD"])).trim() === source &&
    !(await run("git", ["status", "--porcelain", "--untracked-files=normal"])).trim(), "PUBLIC_IDENTITY_CLEAN_SOURCE_REQUIRED");
  await assertSource();
  const profile = JSON.parse(await readFile(root + "infra/public-ingress/identity-profile.json", "utf8")); publicIdentityProfile(profile);
  const dependencies = [], read = async (kind, name, namespace) => {
    const r = await get(kind, name, namespace); requireValue(r.metadata?.uid && r.metadata.resourceVersion, "PUBLIC_IDENTITY_RESOURCE_INVALID");
    dependencies.push({ kind, name, namespace: namespace ?? null, uid: r.metadata.uid, resourceVersion: r.metadata.resourceVersion,
      contentSHA256: fingerprint({ spec: r.spec ?? null, data: r.data ?? null, labels: r.metadata.labels ?? {}, annotations: r.metadata.annotations ?? {} }) }); return r;
  };
  const clusterUID = (await read("namespace", "kube-system")).metadata.uid;
  requireValue(!(await kube(["-n", "kodex-system", "get", "deployment", "internal-rpc-authority-publisher", "--ignore-not-found", "-o", "name"])).trim(),
    "PUBLIC_IDENTITY_PROTECTED_PUBLISHER_REQUIRES_SEPARATE_TRANSITION");
  const endpoints = publicEndpoints(await read("configmap", profile.endpointsName, "kodex-system"), profile);
  const sso = await read("deployment", "sso", "identity"), front = await read("deployment", "staff-control-center", "kodex-system");
  const runtimeName = front.spec.template.spec.volumes?.find(v => v.name === "runtime-config")?.configMap?.name;
  requireValue(/^[a-z0-9-]{1,253}$/.test(runtimeName ?? ""), "PUBLIC_IDENTITY_FRONTEND_RUNTIME_REFERENCE_INVALID");
  const runtime = publicFrontendRuntime(await read("configmap", runtimeName, "kodex-system"), endpoints, profile,
    await readFile(root + "deploy/k8s/base/staff-control-center/security-headers.conf", "utf8"),
    await readFile(root + "deploy/k8s/base/staff-control-center/public-assets.conf", "utf8"));
  const service = await read("service", "sso", "identity"), connectIP = service.spec.clusterIP;
  requireValue((service.spec.type ?? "ClusterIP") === "ClusterIP" && service.spec.ports.some(p => p.port === 443 && p.name === "https") &&
    service.spec.selector?.["app.kubernetes.io/name"] === "sso" && !service.spec.externalIPs && !service.spec.externalName,
    "PUBLIC_IDENTITY_SERVICE_BOUNDARY_INVALID");
  const keycloak = publicKeycloak(sso, endpoints, profile), frontend = publicFrontendDeployment(front, runtime), changes = [
    { namespace: "identity", name: "sso", ...keycloak }, { namespace: "kodex-system", name: "staff-control-center", ...frontend }];
  for (const [namespace, name] of [["kodex-system", "oauth2-control-center"], ["observability", "oauth2-grafana"]]) {
    changes.push({ namespace, name, ...publicOAuthProxy(await read("deployment", name, namespace), endpoints, profile, connectIP) });
  }
  for (const name of ["control-plane", "control-api-gateway"]) {
    changes.push({ namespace: "kodex-system", name, ...publicBackend(await read("deployment", name, "kodex-system"), endpoints, profile) });
  }
  const certificates = publicCertificates(await read("certificate", "staff-control-center-public", "kodex-system"),
    await read("certificate", "sso-public-tls", "identity"), endpoints, profile);
  for (const resource of [...certificates, runtime.resource]) {
    const collision = await kube(["-n", resource.metadata.namespace, "get", resource.kind, resource.metadata.name, "--ignore-not-found", "-o", "name"]);
    requireValue(!collision.trim(), "PUBLIC_IDENTITY_NEW_RESOURCE_COLLISION");
    await kube(["create", "--dry-run=server", "-f", "/dev/stdin", "-o", "name"], JSON.stringify(resource));
  }
  for (const row of changes) {
    const dry = JSON.parse(await kube(["-n", row.namespace, "patch", "deployment", row.name, "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "json"],
      JSON.stringify([{ op: "test", path: "/metadata/uid", value: row.uid }, { op: "test", path: "/metadata/resourceVersion", value: row.resourceVersion },
        { op: "replace", path: "/spec", value: row.spec }])));
    requireValue(fingerprint(dry.spec) === row.afterSHA256, "PUBLIC_IDENTITY_DEPLOYMENT_DRY_RUN_DRIFT");
  }
  await kube(["-n", "kodex-system", "patch", "configmap", profile.endpointsName, "--type=json", "--patch-file=/dev/stdin", "--dry-run=server", "-o", "name"],
    JSON.stringify([{ op: "test", path: "/metadata/uid", value: endpoints.uid }, { op: "test", path: "/metadata/resourceVersion", value: endpoints.resourceVersion },
      { op: "test", path: "/data", value: endpoints.before }, { op: "replace", path: "/data", value: endpoints.after }]));
  for (const expected of dependencies) {
    const actual = await get(expected.kind, expected.name, expected.namespace);
    requireValue(actual.metadata.uid === expected.uid && actual.metadata.resourceVersion === expected.resourceVersion &&
      fingerprint({ spec: actual.spec ?? null, data: actual.data ?? null, labels: actual.metadata.labels ?? {}, annotations: actual.metadata.annotations ?? {} }) === expected.contentSHA256,
    "PUBLIC_IDENTITY_SOURCE_CHANGED_DURING_PLAN");
  }
  await assertSource();
  const plan = { schema: "kodex.public-identity-plan/v1", source, context, clusterUID, profileSHA256: fingerprint(profile), dependencies, endpoints, runtime, certificates,
    deployments: changes.map(({ spec: _spec, ...safe }) => safe),
    remaining: ["keycloak-client-readback-and-transition", "public-ingress-and-canonical-redirects", "helm-owner-consistent-proxy-update", "grafana-signout-issuer",
      "issued-certificate-and-ca-proof", "apply-readback-recovery"], apply: "NOT_RUN" };
  const document = { plan, planSHA256: fingerprint(plan) }, file = await open(options["--output"], "wx", 0o600);
  try { await file.writeFile(JSON.stringify(document) + "\n"); await file.sync(); } finally { await file.close(); }
  return document;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  planPublicIdentity(process.argv.slice(2)).then(d => console.log(JSON.stringify({ status: "PASS", phase: "CONFIGURATION_PLAN_ONLY", source: d.plan.source,
    planSHA256: d.planSHA256, deployments: d.plan.deployments.length, apply: "NOT_RUN" })))
    .catch(error => { console.error(JSON.stringify({ status: "FAIL", code: /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "PUBLIC_IDENTITY_PLAN_FAILED" })); process.exitCode = 1; });
