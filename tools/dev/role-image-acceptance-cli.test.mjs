import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { privateJournal } from "./role-image-acceptance.mjs";

const hash = (value) => createHash("sha256").update(value).digest("hex");
const digest = "a".repeat(64);
const prefix = "fixture-cli";
const origin = "https://fixture.invalid";

function fixture(t, mode = "ready") {
  const directory = mkdtempSync(join(tmpdir(), "ricli-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const repository = join(directory, "repository"); const scripts = join(repository, "tools", "dev"); mkdirSync(scripts, { recursive: true });
  for (const name of ["role-image-acceptance.mjs", "role-image-acceptance-fixture.mjs", "owner-session-client.mjs", "owner-session-storage.mjs", "runtime-workspace-acceptance.mjs"]) copyFileSync(new URL(name, import.meta.url), join(scripts, name));
  const git = (...args) => execFileSync("git", args, { cwd: repository, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
  git("init", "--quiet"); git("add", "tools"); git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "Синтетический CLI checkout");
  const sourceSHA = git("rev-parse", "HEAD");
  const state = join(directory, "state.jsonl"); const previous = join(directory, "previous.jsonl"); const manifest = join(directory, "manifest.json"); const storage = join(directory, "session.json"); const transport = join(directory, "transport.json"); const loader = join(directory, "loader.mjs");
  writeFileSync(manifest, "{}", { mode: 0o600 }); writeFileSync(transport, "[]", { mode: 0o600 });
  writeFileSync(storage, JSON.stringify({ cookies: ["__Host-kodex-session", "__Host-kodex-csrf"].map((name, index) => ({ name, value: index ? "1".repeat(43) : `v1.${"1".repeat(64)}`, domain: "fixture.invalid", path: "/", secure: true, httpOnly: !index, sameSite: "Strict", expires: -1 })), origins: [] }), { mode: 0o600 });
  writeFileSync(loader, `import assert from 'node:assert/strict';
import { readFileSync, writeFileSync } from 'node:fs';
import { roleImageTransportFixture } from ${JSON.stringify(`file://${join(scripts, "role-image-acceptance-fixture.mjs")}`)};
const path = ${JSON.stringify(transport)}; const calls = JSON.parse(readFileSync(path)); const fixture = roleImageTransportFixture();
for (const call of calls) await fixture.request(call.path, call.options);
globalThis.fetch = async (url, init = {}) => {
 assert.equal(url.origin, 'https://fixture.invalid');
 if (url.pathname === '/api/v1/session') {
  if (${JSON.stringify(mode)} === 'expired') return new Response('', {status:401});
  const now = Date.now(); const at = (offset) => new Date(now + offset).toISOString();
  return new Response(JSON.stringify({ generation:'11111111-1111-4111-8111-111111111111', version:1, sessionRevision:1, renewalMode:'BACKEND_REFRESH', serverTime:at(0), accessExpiresAt:at(300000), expiresAt:at(1800000), absoluteExpiresAt:at(3600000), renewAfter:at(180000) }), {headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});
 }
 const options = {method:init.method ?? 'GET', ...(init.body ? {body:init.body} : {})};
 const call = {path:url.pathname + url.search, options, key:new Headers(init.headers).get('Idempotency-Key')};
 if (${JSON.stringify(mode)} === 'lost-ack' && call.path === '/api/v1/projects' && options.method === 'POST') { calls.push(call); writeFileSync(path, JSON.stringify(calls)); throw new Error('Synthetic lost response'); }
 const response = await fixture.request(call.path, options); calls.push(call); writeFileSync(path, JSON.stringify(calls)); return response;
};\n`, { mode: 0o600 });
  const scope = { version: 1, origin, prefix, runnerDigest: `sha256:${digest}`, servingManifestSHA256: hash("{}"), sourceSHA };
  const run = (phase, extra = []) => spawnSync(process.execPath, ["--import", loader, join(scripts, "role-image-acceptance.mjs"), phase, "--origin", origin, "--storage-state", storage, "--state", state, "--prefix", prefix, "--runner-digest", scope.runnerDigest, "--serving-manifest", manifest, "--timeout-ms", "60000", ...extra], { encoding: "utf8", timeout: 10000 });
  return { state, previous, scope, run, calls: () => JSON.parse(readFileSync(transport)), preparePrevious() {
    const journal = privateJournal(previous, scope); const key = "11111111-1111-4111-8111-111111111111";
    journal.append({ type: "INTENT", step: "project", method: "POST", path: "/api/v1/projects", key, bodySHA256: hash(JSON.stringify({ name: `${prefix} RoleImage`, purpose: "Приёмка новой базы образа без provider запуска", language: "ru" })) });
    journal.append({ type: "STOP", step: "project", key, outcome: "UNKNOWN" }); journal.close();
    const bytes = readFileSync(previous); return { bytes, key, args: ["--previous-state", previous, "--previous-sha256", hash(bytes), "--confirm", "RECOVER-SAME-STAGING-PROJECT-INTENT"] };
  } };
}

for (const first of ["prepare", "recover-project"]) test(`public CLI ${first} → advance → restore → inspect uses valid argv and private fixture`, (t) => {
  const f = fixture(t); const prior = first === "recover-project" ? f.preparePrevious() : undefined;
  const execute = (phase, args) => { const result = f.run(phase, args); assert.equal(result.status, 0, result.stderr); return JSON.parse(result.stdout); };
  const prepared = execute(first, prior?.args ?? ["--confirm", "APPLY-STAGING-ROLE-IMAGE-FIXTURE"]); assert.equal(prepared.status, "PASS");
  if (prior) { assert.deepEqual(readFileSync(f.previous), prior.bytes); assert.equal(f.calls().find((call) => call.path === "/api/v1/projects" && call.options.method === "POST").key, prior.key); }
  for (const phase of ["advance", "restore"]) assert.equal(execute(phase, ["--confirm", "APPLY-STAGING-ROLE-IMAGE-FIXTURE"]).status, "PASS");
  execute("inspect", []);
  const commands = f.calls().filter((call) => call.options.method !== "GET"); assert.equal(commands.filter((call) => call.path === "/api/v1/projects").length, 1); assert.equal(commands.filter((call) => call.path.endsWith("/publication")).length, 3); assert.equal(commands.some((call) => call.path.endsWith("/runs")), false);
});

for (const variant of ["unknown", "duplicate", "confirmation", "digest", "foreign-phase"]) test(`public CLI rejects ${variant} before journal mutation`, (t) => {
  const f = fixture(t); const prior = f.preparePrevious(); let args = [...prior.args]; let phase = "recover-project";
  if (variant === "unknown") args.push("--unknown2", "value");
  if (variant === "duplicate") args.push("--previous-sha256", hash(prior.bytes));
  if (variant === "confirmation") args[args.length - 1] = "wrong";
  if (variant === "digest") args[3] = "b".repeat(64);
  if (variant === "foreign-phase") { phase = "prepare"; args[args.length - 1] = "APPLY-STAGING-ROLE-IMAGE-FIXTURE"; }
  const result = f.run(phase, args); assert.equal(result.status, 1); assert.equal(existsSync(f.state), false); assert.deepEqual(readFileSync(f.previous), prior.bytes); assert.deepEqual(f.calls(), []);
});

for (const mode of ["expired", "lost-ack"]) test(`public CLI recovery ${mode} preserves predecessor and never retries`, (t) => {
  const f = fixture(t, mode); const prior = f.preparePrevious(); const result = f.run("recover-project", prior.args);
  assert.equal(result.status, 1); assert.deepEqual(readFileSync(f.previous), prior.bytes);
  const events = readFileSync(f.state, "utf8").trimEnd().split("\n").map(JSON.parse);
  if (mode === "expired") { assert.equal(events.length, 1); assert.deepEqual(f.calls(), []); }
  else { assert.equal(events.at(-1).outcome, "UNKNOWN"); const posts = f.calls().filter((call) => call.options.method === "POST"); assert.equal(posts.length, 1); assert.equal(posts[0].key, prior.key); }
});
