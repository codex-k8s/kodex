import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { copyFileSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { privateJournal } from './role-image-acceptance.mjs';
import { runtimeProofFixture, runtimeProofTransport } from './role-image-runtime-proof-fixture.mjs';
import { prepareRuntimePlan, runtimeProof } from './role-image-runtime-proof.mjs';
const hash = (value) => createHash('sha256').update(value).digest('hex');
const origin = 'https://fixture.invalid';

function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), 'rpr-')); t.after(() => rmSync(directory, { recursive: true, force: true }));
  const repository = join(directory, 'repo'); const scripts = join(repository, 'tools/dev'); mkdirSync(scripts, { recursive: true });
  for (const name of ['role-image-runtime-proof.mjs', 'role-image-runtime-proof-fixture.mjs', 'role-image-acceptance.mjs', 'owner-session-client.mjs', 'owner-session-storage.mjs', 'runtime-workspace-acceptance.mjs', 'runtime-provider-catalog.mjs']) copyFileSync(new URL(name, import.meta.url), join(scripts, name));
  const git = (...args) => execFileSync('git', args, { cwd: repository, stdio: ['ignore', 'pipe', 'pipe'] });
  git('init', '--quiet'); git('add', 'tools'); git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'Синтетическая оснастка');
  const state = join(directory, 'runtime.jsonl'); const previous = join(directory, 'cfg.jsonl'); const manifest = join(directory, 'manifest.json'); const storage = join(directory, 'session.json'); const calls = join(directory, 'calls.json'); const loader = join(directory, 'loader.mjs'); const mode = join(directory, 'mode');
  const j = privateJournal(previous, { version: 1, origin }); j.append({ type: 'CHECKPOINT', step: 'restore-complete', result: { status: 'PASS', ...runtimeProofFixture } }); j.close();
  const previousBytes = readFileSync(previous); writeFileSync(manifest, '{}', { mode: 0o600 }); writeFileSync(calls, '[]', { mode: 0o600 }); writeFileSync(mode, '', { mode: 0o600 });
  writeFileSync(storage, JSON.stringify({ cookies: ['__Host-kodex-session', '__Host-kodex-csrf'].map((name, index) => ({ name, value: index ? '1'.repeat(43) : `v1.${'1'.repeat(64)}`, domain: 'fixture.invalid', path: '/', secure: true, httpOnly: !index, sameSite: 'Strict', expires: -1 })), origins: [] }), { mode: 0o600 });
  writeFileSync(loader, `import {readFileSync,writeFileSync} from 'node:fs';
import {runtimeProofTransport} from ${JSON.stringify(`file://${join(scripts, 'role-image-runtime-proof-fixture.mjs')}`)};
const mode=readFileSync(${JSON.stringify(mode)},'utf8'); const transport=runtimeProofTransport(mode); const path=${JSON.stringify(calls)};
globalThis.fetch=async(url,init={})=>{
 if(url.origin!==${JSON.stringify(origin)})throw new Error('ORIGIN_UNEXPECTED');
 const rows=JSON.parse(readFileSync(path)); rows.push({path:url.pathname+url.search,method:init.method??'GET',key:new Headers(init.headers).get('Idempotency-Key')}); writeFileSync(path,JSON.stringify(rows));
 if(url.pathname==='/api/v1/session'){
  if(mode==='expired')return new Response('',{status:401});
  const at=(ms)=>new Date(Date.now()+ms).toISOString();return new Response(JSON.stringify({generation:'11111111-1111-4111-8111-111111111111',version:1,sessionRevision:1,renewalMode:'BACKEND_REFRESH',serverTime:at(0),accessExpiresAt:at(300000),expiresAt:at(1800000),absoluteExpiresAt:at(3600000),renewAfter:at(180000)}),{headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});
 }
 if(init.method==='POST'&&mode==='lost-ack')throw new Error('SYNTHETIC_UNKNOWN');
 if(init.method==='POST'&&mode==='rejected')return new Response('{}',{status:409});
 return new Response(JSON.stringify(transport(url.pathname+url.search,init.method??'GET')),{status:init.method==='POST'?201:200,headers:{'Content-Type':'application/json'}});
};`, { mode: 0o600 });
  const run = (phase, extra = []) => spawnSync(process.execPath, ['--import', loader, join(scripts, 'role-image-runtime-proof.mjs'), phase, '--origin', origin, '--storage-state', storage, '--state', state, '--fixture-state', previous, '--fixture-sha256', hash(previousBytes), '--serving-manifest', manifest, '--timeout-ms', '60000', ...extra], { encoding: 'utf8', timeout: 10000 });
  return { run, state, previous, previousBytes, setMode: (value) => writeFileSync(mode, value), calls: () => JSON.parse(readFileSync(calls)), events: () => readFileSync(state, 'utf8').trim().split('\n').map(JSON.parse) };
}
const confirm = ['--confirm', 'START-ONE-STAGING-AGENT-RUN'];
test('public CLI plan → launch → capture uses actual argv, exact pins and one durable Run', (t) => {
  const f = fixture(t);
  let result = f.run('plan'); assert.equal(result.status, 0, result.stderr); assert.equal(JSON.parse(result.stdout).status, 'READY'); assert(f.calls().every((call) => call.method === 'GET'));
  result = f.run('launch', confirm); assert.equal(result.status, 0, result.stderr); assert.equal(JSON.parse(result.stdout).providerEffect, 'MAY_HAVE_STARTED');
  result = f.run('launch', confirm); assert.equal(result.status, 0, result.stderr);
  result = f.run('capture'); assert.equal(result.status, 0, result.stderr); const captured = JSON.parse(result.stdout); assert.equal(captured.turnRef, 'turn_fixture'); assert.equal(captured.imageManifestDigest, runtimeProofFixture.manifestDigest); assert.equal(captured.runtimePod, 'NOT_RUN');
  assert.equal(f.calls().filter((call) => call.method === 'POST').length, 1); assert.equal(f.events().filter((event) => event.type === 'INTENT').length, 1); assert.deepEqual(readFileSync(f.previous), f.previousBytes);
  assert(!readFileSync(f.state, 'utf8').includes('```javascript'));
});
for (const mode of ['expired', 'lost-ack', 'rejected', 'stale', 'wrong-image', 'foreign']) test(`public CLI ${mode} prevents a second mutation`, (t) => {
  const f = fixture(t); assert.equal(f.run('plan').status, 0); f.setMode(mode);
  const result = f.run('launch', confirm); assert.equal(result.status, 1); assert.equal(f.run('launch', confirm).status, 1);
  assert.equal(f.calls().filter((call) => call.method === 'POST').length, ['lost-ack', 'rejected'].includes(mode) ? 1 : 0);
  if (mode === 'lost-ack') assert.equal(f.events().at(-1).outcome, 'UNKNOWN');
  assert.deepEqual(readFileSync(f.previous), f.previousBytes);
});
for (const mode of ['no-capability', 'catalog-changed', 'no-fixed-account']) test(`public CLI plan returns ${mode} blocker without mutation`, (t) => {
  const f = fixture(t); f.setMode(mode); const result = f.run('plan'); assert.equal(result.status, 0, result.stderr); assert.equal(JSON.parse(result.stdout).status, 'BLOCKED'); assert.equal(f.run('launch', confirm).status, 1); assert(f.calls().every((call) => call.method === 'GET'));
});
for (const mode of ['attempt-changed', 'missing-turn', 'revision-image-changed']) test(`public CLI capture rejects ${mode}`, (t) => {
  const f = fixture(t); assert.equal(f.run('plan').status, 0); assert.equal(f.run('launch', confirm).status, 0); f.setMode(mode); assert.equal(f.run('capture').status, 1); assert.equal(f.calls().filter((call) => call.method === 'POST').length, 1);
});
for (const [phase, args] of [['launch', []], ['plan', confirm], ['plan', ['--fixture-sha256', 'a'.repeat(64)]], ['unknown', []], ['plan', ['--unknown2', 'x']]]) test(`public CLI rejects invalid phase/flags ${phase} ${args[0] ?? ''}`, (t) => {
  const f = fixture(t); const result = f.run(phase, args); assert.equal(result.status, 1); assert.deepEqual(f.calls(), []);
});
test('changed predecessor blocks public CLI before session access', (t) => { const f = fixture(t); writeFileSync(f.previous, `${f.previousBytes}\n`); assert.equal(f.run('plan').status, 1); assert.deepEqual(f.calls(), []); });
test('bounded capture QUEUED stops without creating Run', async () => {
  let clock = 0; const plan = await prepareRuntimePlan(runtimeProofFixture, async (path) => runtimeProofTransport()(path));
  const result = { ...plan, planSHA256: hash(JSON.stringify(plan)), nonce: '1'.repeat(32) }; const journal = { events: [{ type: 'CHECKPOINT', step: 'plan', result }, { type: 'ACK', step: 'run', result: { runRef: 'run_fixture', sessionRef: 'ses_fixture', attempt: 1 } }] };
  await assert.rejects(runtimeProof({ phase: 'capture', fixture: runtimeProofFixture, journal, preflight: async () => {}, get: async () => ({ ref: 'run_fixture', sessionRef: 'ses_fixture', attempt: 1, projectRef: runtimeProofFixture.projectRef, target: { ref: runtimeProofFixture.agentRef }, state: 'QUEUED' }), request: () => assert.fail('mutation forbidden'), now: () => clock, timeoutMs: 10, sleep: async () => { clock += 10; } }), /CAPTURE_DEADLINE/);
});

test('public CLI disallows capture before launch and plan after intent', (t) => { const f = fixture(t); assert.equal(f.run('plan').status, 0); assert.equal(f.run('capture').status, 1); assert.equal(f.run('launch', confirm).status, 0); assert.equal(f.run('plan').status, 1); assert.equal(f.calls().filter((call) => call.method === 'POST').length, 1); });
