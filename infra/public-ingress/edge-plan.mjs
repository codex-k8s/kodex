#!/usr/bin/env node
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import { mkdir, open } from "node:fs/promises";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { renderPublicEdge } from "./edge-model.mjs";
import { fingerprint } from "../../tools/release/stable-inventory.mjs";

const execute = promisify(execFile);
const managedNames = new Set(["kodex-public-edge", "kodex-public-edge-tls", "kodex-public-edge-upstream"]);
function requireValue(value, code) { if (!value) throw Error(code); }

export async function readPrivateJSON(path) {
  const file = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await file.stat();
    requireValue(stat.isFile() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o600 &&
      stat.nlink === 1 && stat.size > 0 && stat.size <= 1 << 20, "EDGE_PRIVATE_FILE_REQUIRED");
    return JSON.parse(await file.readFile("utf8"));
  } finally { await file.close(); }
}

export function protectedCommitment(resources) {
  requireValue(Array.isArray(resources), "EDGE_BASELINE_INVALID");
  const rows = resources.filter(r => !(managedNames.has(r.metadata?.name) &&
    r.metadata?.labels?.["app.kubernetes.io/managed-by"] === "kodex-public-edge"))
    .map(r => {
      requireValue(r.metadata?.uid && r.kind && r.spec, "EDGE_BASELINE_INVALID");
      return { kind: r.kind, name: r.metadata.name, uid: r.metadata.uid,
        contentSHA256: fingerprint({ spec: r.spec, annotations: r.metadata.annotations ?? {} }) };
    }).sort((a, b) => `${a.kind}/${a.name}`.localeCompare(`${b.kind}/${b.name}`, "en"));
  requireValue(rows.some(r => r.kind === "Deployment" && r.name === "traefik"), "EDGE_INGRESS_REQUIRED");
  return fingerprint(rows);
}

async function createPrivate(path, value) {
  const file = await open(path, "wx", 0o600);
  try { await file.writeFile(value); await file.sync(); } finally { await file.close(); }
}

async function main(args) {
  const options = {};
  while (args.length) {
    const key = args.shift();
    requireValue(["--config", "--context", "--output"].includes(key) && !Object.hasOwn(options, key) && args.length,
      "EDGE_ARGUMENTS_INVALID");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context ?? "") && options["--config"] && options["--output"],
    "EDGE_ARGUMENTS_INVALID");
  const config = await readPrivateJSON(resolve(options["--config"]));
  const rendered = renderPublicEdge(config);
  const env = Object.fromEntries(["HOME", "PATH", "LANG", "KUBECONFIG"].filter(k => process.env[k]).map(k => [k, process.env[k]]));
  const get = async arguments_ => {
    const { stdout } = await execute("kubectl", ["--context", context, "--request-timeout=15s", ...arguments_, "-o", "json"],
      { env, timeout: 20000, maxBuffer: 8 << 20 });
    return JSON.parse(stdout);
  };
  const cluster = await get(["get", "namespace", "kube-system"]);
  const existing = await get(["-n", config.namespace, "get", "deployments,services,ingresses,ingressroutes.traefik.io,ingressroutetcps.traefik.io,middlewares.traefik.io"]);
  requireValue(cluster.metadata.uid && Array.isArray(existing.items), "EDGE_CLUSTER_IDENTITY_INVALID");
  const ingress = existing.items.find(r => r.kind === "Deployment" && r.metadata.name === "traefik");
  requireValue(ingress?.spec.template.spec.containers.some(c => (c.args ?? []).some(arg =>
    arg.startsWith(`--certificatesresolvers.${config.certResolver}.acme.`))), "EDGE_EXISTING_ACME_RESOLVER_REQUIRED");
  requireValue(!existing.items.some(r => managedNames.has(r.metadata.name) &&
    r.metadata.labels?.["app.kubernetes.io/managed-by"] !== "kodex-public-edge"), "EDGE_NAME_COLLISION");
  const namespace = await get(["get", "namespace", config.namespace]);
  // Создание плана не устанавливает units, не меняет Docker networks или ingress.
  const directory = resolve(options["--output"]);
  await mkdir(directory, { mode: 0o700 });
  const plan = { schema: "kodex.public-edge-plan/v1", context, clusterUID: cluster.metadata.uid,
    namespaceUID: namespace.metadata.uid, config, contentSHA256: rendered.contentSHA256,
    protectedSHA256: protectedCommitment(existing.items) };
  await createPrivate(join(directory, "plan.json"), JSON.stringify(plan) + "\n");
  await createPrivate(join(directory, "resources.json"), JSON.stringify({ apiVersion: "v1", kind: "List", items: rendered.resources }) + "\n");
  for (const [name, value] of Object.entries(rendered.units)) await createPrivate(join(directory, name), value);
  process.stdout.write(JSON.stringify({ status: "PASS", phase: "PLAN_ONLY", resources: rendered.resources.length,
    units: Object.keys(rendered.units).length, planSHA256: fingerprint(plan) }) + "\n");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  main(process.argv.slice(2)).catch(error => {
    const code = /^[A-Z_]+$/.test(error.message ?? "") ? error.message : "EDGE_PLAN_FAILED";
    process.stderr.write(JSON.stringify({ status: "FAIL", code }) + "\n"); process.exitCode = 1;
  });
