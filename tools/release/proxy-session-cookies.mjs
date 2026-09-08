#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { closeSync, fsyncSync, lstatSync, mkdtempSync, openSync, readFileSync, rmSync, writeFileSync, writeSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { maximumProxyCookies, proxyCookieName } from "../dev/owner-session-storage.mjs";

const namespace = "kodex-system", target = "oauth2-control-center-auth";
const confirmation = "PROPAGATE-STAGING-PROXY-SESSION-COOKIES";
const requireValue = (value, code) => { if (!value) throw new Error(code); };
export const proxyResponseCookies = [proxyCookieName, ...Array.from({ length: maximumProxyCookies }, (_, index) => `${proxyCookieName}_${index}`)];

export function planProxySessionCookies(middleware) {
  const m = middleware?.metadata;
  requireValue(middleware?.apiVersion === "traefik.io/v1alpha1" && middleware.kind === "Middleware" && m?.name === target && m.namespace === namespace && /^[a-f0-9-]{36}$/.test(m.uid ?? "") && /^[0-9]+$/.test(m.resourceVersion ?? ""), "MIDDLEWARE_IDENTITY_MISMATCH");
  const before = structuredClone(middleware.spec), normalized = structuredClone(before);
  const current = normalized?.forwardAuth?.addAuthCookiesToResponse;
  requireValue(current == null || Array.isArray(current) && (current.length === 0 || fingerprint(current) === fingerprint(proxyResponseCookies)), "EXISTING_COOKIE_POLICY_CONFLICT");
  if (normalized?.forwardAuth) delete normalized.forwardAuth.addAuthCookiesToResponse;
  requireValue(fingerprint(normalized) === fingerprint({ forwardAuth: {
    address: "http://oauth2-control-center.kodex-system.svc.cluster.local/oauth2/auth", trustForwardHeader: true,
    authResponseHeaders: ["X-Auth-Request-User", "X-Auth-Request-Email", "X-Auth-Request-Groups"],
  } }), "EXACT_AUTH_BOUNDARY_REQUIRED");
  const after = structuredClone(before); after.forwardAuth.addAuthCookiesToResponse = proxyResponseCookies;
  return { version: 1, name: target, uid: m.uid, resourceVersion: m.resourceVersion,
    beforeSpecSHA256: fingerprint(before), afterSpecSHA256: fingerprint(after),
    cookieNames: proxyResponseCookies, alreadyPresent: fingerprint(current ?? []) === fingerprint(proxyResponseCookies),
    patch: [{ op: "test", path: "/metadata/uid", value: m.uid }, { op: "test", path: "/metadata/resourceVersion", value: m.resourceVersion },
      { op: "test", path: "/spec", value: before }, { op: "add", path: "/spec/forwardAuth/addAuthCookiesToResponse", value: proxyResponseCookies }] };
}

function privateRead(path) {
  const info = lstatSync(path);
  requireValue(info.isFile() && info.nlink === 1 && (info.mode & 0o077) === 0 && info.size <= 1 << 20, "PRIVATE_PLAN_REQUIRED");
  return JSON.parse(readFileSync(path, "utf8"));
}

function main(args) {
  const command = args.shift(), options = {};
  requireValue(["plan", "apply"].includes(command), "INVALID_COMMAND");
  while (args.length) { const key = args.shift(); requireValue(["--context", "--output", "--plan", "--evidence", "--confirm"].includes(key) && !Object.hasOwn(options, key) && args.length, "INVALID_ARGUMENT"); options[key] = args.shift(); }
  const context = options["--context"];
  requireValue(context && !/prod/i.test(context), "STAGING_CONTEXT_REQUIRED");
  const kubectl = (args) => execFileSync("kubectl", ["--context", context, "--namespace", namespace, ...args], { encoding: "utf8", timeout: 30000, maxBuffer: 1 << 20, stdio: ["ignore", "pipe", "pipe"] });
  const get = (kind, name) => JSON.parse(kubectl(["get", kind, name, "-o", "json"]));
  const cluster = get("namespace", "kube-system"), ns = get("namespace", namespace);
  requireValue(ns.metadata.labels?.["kodex.dev/environment"] === "staging" && /^[a-f0-9-]{36}$/.test(ns.metadata.uid ?? "") && /^[a-f0-9-]{36}$/.test(cluster.metadata.uid ?? ""), "STAGING_NAMESPACE_REQUIRED");
  const current = get("middleware", target);
  const plan = { ...planProxySessionCookies(current), context, clusterUID: cluster.metadata.uid, namespaceUID: ns.metadata.uid };
  if (command === "plan") {
    requireValue(options["--output"] && !options["--plan"] && !options["--evidence"] && !options["--confirm"], "READ_ONLY_PLAN_REQUIRED");
    writeFileSync(options["--output"], `${JSON.stringify(plan)}\n`, { flag: "wx", mode: 0o600 });
    process.stdout.write(`${JSON.stringify({ status: "PLANNED", target, uid: plan.uid, cookieNames: plan.cookieNames, alreadyPresent: plan.alreadyPresent })}\n`); return;
  }
  requireValue(options["--plan"] && options["--evidence"] && !options["--output"] && options["--confirm"] === confirmation && fingerprint(privateRead(options["--plan"])) === fingerprint(plan), "EXACT_PLAN_CONFIRMATION_REQUIRED");
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const record = (event) => { writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), target, uid: plan.uid, ...event })}\n`); fsyncSync(fd); };
  let dispatched = false;
  try {
    record({ status: "STARTED", beforeSpecSHA256: plan.beforeSpecSHA256 });
    if (!plan.alreadyPresent) {
      const directory = mkdtempSync(join(tmpdir(), "kodex-proxy-cookie-"));
      try {
        const path = join(directory, "patch.json"); writeFileSync(path, JSON.stringify(plan.patch), { flag: "wx", mode: 0o600 });
        record({ status: "INTENT", afterSpecSHA256: plan.afterSpecSHA256 }); dispatched = true;
        kubectl(["patch", "middleware", target, "--type=json", `--patch-file=${path}`]);
      } finally { rmSync(directory, { recursive: true, force: true }); }
    }
    const after = get("middleware", target);
    requireValue(after.metadata.uid === plan.uid && fingerprint(after.spec) === plan.afterSpecSHA256, "READBACK_MISMATCH");
    record({ status: "PASS", afterSpecSHA256: plan.afterSpecSHA256 });
    process.stdout.write(`${JSON.stringify({ status: "PASS", target, uid: plan.uid, cookieNames: plan.cookieNames })}\n`);
  } catch { record({ status: dispatched ? "UNKNOWN" : "FAIL", code: "PROXY_COOKIE_OPERATION_FAILED" }); throw new Error("PROXY_COOKIE_OPERATION_FAILED"); }
  finally { closeSync(fd); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); } catch { process.stderr.write("Proxy cookie operation failed; inspect authoritative state before another attempt\n"); process.exitCode = 1; }
}
