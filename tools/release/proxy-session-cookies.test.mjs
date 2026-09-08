import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { planProxySessionCookies, proxyResponseCookies } from "./proxy-session-cookies.mjs";

const uid = "11111111-1111-4111-8111-111111111111";
const middleware = () => ({ apiVersion: "traefik.io/v1alpha1", kind: "Middleware", metadata: { name: "oauth2-control-center-auth", namespace: "kodex-system", uid, resourceVersion: "1" }, spec: { forwardAuth: { address: "http://oauth2-control-center.kodex-system.svc.cluster.local/oauth2/auth", trustForwardHeader: true, authResponseHeaders: ["X-Auth-Request-User", "X-Auth-Request-Email", "X-Auth-Request-Groups"] } } });

test("plan changes only exact auth response cookie list with UID/RV/spec CAS", () => {
  const input = middleware(), before = structuredClone(input), plan = planProxySessionCookies(input);
  assert.deepEqual(input, before); assert.equal(plan.alreadyPresent, false);
  assert.deepEqual(plan.patch.map(item => [item.op, item.path]), [["test", "/metadata/uid"], ["test", "/metadata/resourceVersion"], ["test", "/spec"], ["add", "/spec/forwardAuth/addAuthCookiesToResponse"]]);
  assert.deepEqual(proxyResponseCookies, ["_kodex_control_center_oauth2", ...[0, 1, 2, 3].map(i => `_kodex_control_center_oauth2_${i}`)]);
  input.spec.forwardAuth.addAuthCookiesToResponse = proxyResponseCookies;
  assert.equal(planProxySessionCookies(input).alreadyPresent, true);
});

for (const scenario of ["foreign", "trust", "headers", "address", "csrf", "overflow", "wildcard"]) test(`plan rejects ${scenario} boundary drift`, () => {
  const value = middleware();
  if (scenario === "foreign") value.metadata.name = "oauth2-grafana-auth";
  if (scenario === "trust") value.spec.forwardAuth.trustForwardHeader = false;
  if (scenario === "headers") value.spec.forwardAuth.authResponseHeaders.push("Set-Cookie");
  if (scenario === "address") value.spec.forwardAuth.address = "http://foreign.invalid/auth";
  if (["csrf", "overflow", "wildcard"].includes(scenario)) value.spec.forwardAuth.addAuthCookiesToResponse = [scenario === "csrf" ? "__Host-kodex-csrf" : scenario === "overflow" ? "_kodex_control_center_oauth2_4" : "*"];
  assert.throws(() => planProxySessionCookies(value));
});

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), "proxycas-")); t.after(() => rmSync(directory, { recursive: true, force: true }));
  const statePath = join(directory, "state.json"), plan = join(directory, "plan.json"), evidence = join(directory, "evidence.jsonl"), binary = join(directory, "kubectl");
  const state = { middleware: middleware(), cluster: { metadata: { uid } }, namespace: { metadata: { uid: "22222222-2222-4222-8222-222222222222", labels: { "kodex.dev/environment": "staging" } } }, patches: 0 };
  const save = (value) => writeFileSync(statePath, JSON.stringify(value), { mode: 0o600 }); save(state);
  writeFileSync(binary, `#!/usr/bin/env node
const fs=require('node:fs'), assert=require('node:assert/strict');const p=process.env.FIXTURE_STATE;const s=JSON.parse(fs.readFileSync(p));const a=process.argv.slice(2);assert.deepEqual(a.slice(0,4),['--context','fixture','--namespace','kodex-system']);const args=a.slice(4);
if(args[0]==='get'){const value=args[1]==='namespace'?(args[2]==='kube-system'?s.cluster:s.namespace):s.middleware;process.stdout.write(JSON.stringify(value));}
else {assert.deepEqual(args.slice(0,4),['patch','middleware','oauth2-control-center-auth','--type=json']);const patch=JSON.parse(fs.readFileSync(args[4].slice('--patch-file='.length)));for(const op of patch){const segments=op.path.slice(1).split('/');let parent=s.middleware;for(const part of segments.slice(0,-1))parent=parent[part];const key=segments.at(-1);if(op.op==='test')assert.deepEqual(parent[key],op.value);else{assert.equal(op.path,'/spec/forwardAuth/addAuthCookiesToResponse');parent[key]=op.value;}}
s.middleware.metadata.resourceVersion=String(Number(s.middleware.metadata.resourceVersion)+1);s.patches++;fs.writeFileSync(p,JSON.stringify(s));if(process.env.FIXTURE_LOST_ACK==='true')process.exit(9);process.stdout.write('{}');}
`, { mode: 0o700 }); chmodSync(binary, 0o700);
  const run = (command, extra = [], env = {}) => spawnSync(process.execPath, [fileURLToPath(new URL("./proxy-session-cookies.mjs", import.meta.url)), command, "--context", "fixture", ...extra], { encoding: "utf8", timeout: 10000, env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, FIXTURE_STATE: statePath, ...env } });
  const read = () => JSON.parse(readFileSync(statePath));
  const planned = () => { const result = run("plan", ["--output", plan]); assert.equal(result.status, 0, result.stderr); assert.equal(read().patches, 0); };
  const apply = (env) => run("apply", ["--plan", plan, "--evidence", evidence, "--confirm", "PROPAGATE-STAGING-PROXY-SESSION-COOKIES"], env);
  return { run, plan, evidence, read, save, planned, apply };
}

test("public CLI plan/apply preserves auth spec and records exact readback", (t) => {
  const f = fixture(t); f.planned(); assert.equal(f.apply().status, 0);
  assert.equal(f.read().patches, 1); assert.deepEqual(f.read().middleware.spec.forwardAuth.addAuthCookiesToResponse, proxyResponseCookies);
  assert.deepEqual(readFileSync(f.evidence, "utf8").trim().split("\n").map(x => JSON.parse(x).status), ["STARTED", "INTENT", "PASS"]);
  assert.equal(f.apply().status, 1); assert.equal(f.read().patches, 1);
});
for (const drift of ["uid", "resourceVersion", "namespace", "cluster", "spec"]) test(`public CLI rejects ${drift} drift before mutation`, (t) => {
  const f = fixture(t); f.planned(); const state = f.read();
  if (["uid", "resourceVersion"].includes(drift)) state.middleware.metadata[drift] = drift === "uid" ? "33333333-3333-4333-8333-333333333333" : "2";
  if (["namespace", "cluster"].includes(drift)) state[drift].metadata.uid = "44444444-4444-4444-8444-444444444444";
  if (drift === "spec") state.middleware.spec.forwardAuth.authResponseHeaders.push("Set-Cookie");
  f.save(state); assert.equal(f.apply().status, 1); assert.equal(f.read().patches, 0); assert.equal(existsSync(f.evidence), false);
});
test("public CLI rejects non-staging namespace and preserves UNKNOWN without retry", (t) => {
  const f = fixture(t); const state = f.read(); state.namespace.metadata.labels["kodex.dev/environment"] = "production"; f.save(state);
  assert.equal(f.run("plan", ["--output", f.plan]).status, 1); assert.equal(existsSync(f.plan), false);
  state.namespace.metadata.labels["kodex.dev/environment"] = "staging"; f.save(state); f.planned();
  assert.equal(f.apply({ FIXTURE_LOST_ACK: "true" }).status, 1); assert.equal(f.read().patches, 1);
  assert.equal(JSON.parse(readFileSync(f.evidence, "utf8").trim().split("\n").at(-1)).status, "UNKNOWN");
  assert.equal(f.apply().status, 1); assert.equal(f.read().patches, 1);
});
