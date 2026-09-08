import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { copyFileSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { emailFixtureProfile } from './email-mailbox-acceptance-fixture.mjs';
import { validateEmailProfile } from './email-mailbox-acceptance.mjs';
const origin = 'https://fixture.invalid';
const mutationPhases = ['connection', 'credentials', 'publish', 'bind', 'grants'];
function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), 'ema-')); t.after(() => rmSync(directory, { recursive: true, force: true }));
  const repo = join(directory, 'repo'); const scripts = join(repo, 'tools/dev'); mkdirSync(scripts, { recursive: true });
  for (const name of ['email-mailbox-acceptance.mjs', 'email-mailbox-acceptance-fixture.mjs', 'role-image-acceptance.mjs', 'owner-session-client.mjs', 'owner-session-storage.mjs', 'runtime-workspace-acceptance.mjs', 'runtime-provider-catalog.mjs']) copyFileSync(new URL(name, import.meta.url), join(scripts, name));
  const contracts = join(repo, 'contracts/integrations/v1/definitions'); mkdirSync(contracts, { recursive: true }); copyFileSync(new URL('../../contracts/integrations/v1/definitions/email.yaml', import.meta.url), join(contracts, 'email.yaml'));
  const git = (...args) => execFileSync('git', args, { cwd: repo, stdio: ['ignore', 'pipe', 'pipe'] });
  git('init', '--quiet'); git('add', '.'); git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'Синтетический email owner');
  const files = Object.fromEntries(['state', 'profile', 'credentials', 'grant', 'manifest', 'storage', 'calls', 'owner', 'mode', 'loader'].map((key) => [key, join(directory, `${key}.${key === 'loader' ? 'mjs' : 'json'}`)]));
  const put = (key, value) => writeFileSync(files[key], JSON.stringify(value), { mode: 0o600 });
  put('profile', emailFixtureProfile); put('grant', { agentRef: 'agt_email', projectRef: 'prj_email', capabilities: ['email.message.send'] }); put('credentials', Object.fromEntries(validateEmailProfile(emailFixtureProfile).map((slot) => [slot.slot, `sensitive-fixture-${slot.slot}`]))); put('manifest', {}); put('calls', []); put('owner', {}); put('mode', '');
  put('storage', { cookies: ['__Host-kodex-session', '__Host-kodex-csrf'].map((name, i) => ({ name, value: i ? '1'.repeat(43) : `v1.${'1'.repeat(64)}`, domain: 'fixture.invalid', path: '/', secure: true, httpOnly: !i, sameSite: 'Strict', expires: -1 })), origins: [] });
  writeFileSync(files.loader, `import assert from 'node:assert/strict'; import {readFileSync,writeFileSync} from 'node:fs';
import {emailFixtureTransport} from ${JSON.stringify(`file://${join(scripts, 'email-mailbox-acceptance-fixture.mjs')}`)};
const files=${JSON.stringify(files)}; const read=(key)=>JSON.parse(readFileSync(files[key])); const write=(key,v)=>writeFileSync(files[key],JSON.stringify(v));
globalThis.fetch=async(url,init={})=>{
 assert.equal(url.origin,${JSON.stringify(origin)}); const path=url.pathname+url.search; const method=init.method??'GET'; const mode=read('mode'); const rows=read('calls');
 rows.push({path,method});write('calls',rows);
 if(path==='/api/v1/session'){
  if(mode==='expired')return new Response('',{status:401});
  const at=(ms)=>new Date(Date.now()+ms).toISOString();return new Response(JSON.stringify({generation:'11111111-1111-4111-8111-111111111111',version:1,sessionRevision:1,renewalMode:'BACKEND_REFRESH',serverTime:at(0),accessExpiresAt:at(300000),expiresAt:at(1800000),absoluteExpiresAt:at(3600000),renewAfter:at(180000)}),{headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});
 }
 const state=read('owner'); const body=init.body?JSON.parse(init.body):undefined;
 if(method!=='GET'){
  const headers=new Headers(init.headers); assert(headers.get('Idempotency-Key')); assert(headers.get('X-CSRF-Token'));
  if(path.endsWith('/credential')||path.endsWith('/commands')||path.endsWith('/grants'))assert.equal(headers.get('If-Match'),'"'+state.version+'"');
  if(path.endsWith('/validation')||path.endsWith('/publication')||path.endsWith('/binding'))assert.equal(headers.get('If-Match'),'"'+(state.configVersion??1)+'"');
  const events=readFileSync(files.state,'utf8').trim().split('\\n').map(JSON.parse); assert.equal(events.at(-1).type,'INTENT'); assert.equal(events.at(-1).key,headers.get('Idempotency-Key'));
  if(mode==='lost-ack')throw new Error('SYNTHETIC_UNKNOWN'); if(mode==='rejected')return new Response('{}',{status:409});
 }
 const result=emailFixtureTransport(state,path,method,body,mode);write('owner',state); if(mode==='credential-lost-ack'&&path.endsWith('/credential'))throw new Error('SYNTHETIC_LOST_ACK'); return new Response(JSON.stringify(result.body),{status:result.status??200,headers:{'Content-Type':'application/json'}});
};`, { mode: 0o600 });
  const run = (phase, extra = [], automatic = true) => {
    const flags = automatic ? mutationPhases.includes(phase) ? ['--confirm', 'CONFIGURE-STAGING-MAILBOX'] : phase === 'health' ? ['--confirm', 'CHECK-STAGING-MAILBOX-HEALTH'] : [] : [];
    if (phase === 'credentials') flags.push('--credentials', files.credentials);
    if (phase === 'grants') flags.push('--grant-input', files.grant);
    return spawnSync(process.execPath, ['--import', files.loader, join(scripts, 'email-mailbox-acceptance.mjs'), phase, '--origin', origin, '--storage-state', files.storage, '--state', files.state, '--profile', files.profile, '--serving-manifest', files.manifest, '--timeout-ms', '1000', ...flags, ...extra], { encoding: 'utf8', timeout: 8000 });
  };
  const pass = (phase, extra) => { const result = run(phase, extra); assert.equal(result.status, 0, result.stderr); assert(!/sensitive-fixture|sender@example|recipient@example/.test(result.stdout)); return JSON.parse(result.stdout); };
  return { run, pass, files, put, mode: (mode) => put('mode', mode), calls: () => JSON.parse(readFileSync(files.calls)), events: () => readFileSync(files.state, 'utf8').trim().split('\n').map(JSON.parse), setup: () => { for (const phase of ['connection', 'credentials', 'publish', 'bind']) pass(phase); } };
}
test('public CLI: plan → connection → credentials → publish → bind → readback → HEALTH → grants → exact receipt', (t) => {
  const f = fixture(t); assert.equal(f.pass('plan').providerEffect, 'NOT_RUN'); assert(f.calls().every((c) => c.method === 'GET'));
  f.setup(); assert.equal(f.pass('readback').status, 'PASS'); assert.equal(f.pass('health').providerEffect, 'HEALTH_ONLY'); f.pass('grants');
  assert.equal(f.pass('receipt', ['--invocation-ref', 'inv_email', '--project-ref', 'prj_email']).status, 'PASS');
  const count = f.calls().filter((c) => c.method !== 'GET').length;
  for (const phase of ['connection', 'credentials', 'publish', 'bind', 'health', 'grants']) f.pass(phase);
  assert.equal(f.calls().filter((c) => c.method !== 'GET').length, count);
  assert(!/sensitive-fixture|sender@example|recipient@example/.test(readFileSync(f.files.state, 'utf8')));
  assert.equal(f.events().filter((e) => e.type === 'INTENT').length, f.events().filter((e) => e.type === 'ACK').length);
});
for (const mode of ['lost-ack', 'rejected']) test(`public CLI ${mode}: unresolved intent never redispatches`, (t) => {
  const f = fixture(t); f.mode(mode); assert.notEqual(f.run('connection').status, 0); f.mode(''); assert.notEqual(f.run('connection').status, 0); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 1); assert.equal(f.events().filter((e) => e.type === 'INTENT').length, 1);
});
test('expired session rejects before business intent', (t) => { const f = fixture(t); f.mode('expired'); assert.notEqual(f.run('connection').status, 0); assert(f.calls().every((c) => c.method === 'GET')); assert(!f.events().some((e) => e.type === 'INTENT')); });
test('public CLI confirmation and unknown flags fail before network', (t) => { const f = fixture(t); assert.notEqual(f.run('connection', [], false).status, 0); assert.notEqual(f.run('plan', ['--allow-unsafe', 'yes']).status, 0); assert.equal(f.calls().length, 0); });
test('credential file only allowed in credential phase', (t) => { const f = fixture(t); assert.notEqual(f.run('plan', ['--credentials', f.files.credentials]).status, 0); assert.equal(f.calls().length, 0); });
test('changed immutable profile refuses journal continuation', (t) => { const f = fixture(t); f.pass('plan'); f.put('profile', { ...emailFixtureProfile, prefix: 'mvp1031-other' }); assert.match(f.run('connection').stderr, /JOURNAL_SCOPE_MISMATCH/); assert(f.calls().every((c) => c.method === 'GET')); });
for (const mode of ['drift', 'publication-failed', 'pending']) test(`readback ${mode} fails without mutations`, (t) => {
  const f = fixture(t); f.setup(); const count = f.calls().filter((c) => c.method !== 'GET').length; f.mode(mode); assert.notEqual(f.run('readback').status, 0); assert.equal(f.calls().filter((c) => c.method !== 'GET').length, count);
});
test('health failure preserves one acknowledged TEST without retry', (t) => { const f = fixture(t); f.setup(); f.mode('health-failed'); assert.notEqual(f.run('health').status, 0); assert.notEqual(f.run('health').status, 0); assert.equal(f.calls().filter((c) => c.path.endsWith('/commands')).length, 1); });
test('foreign recipient denies grant before mutation', (t) => { const f = fixture(t); f.setup(); f.pass('health'); f.mode('foreign-agent'); assert.notEqual(f.run('grants').status, 0); assert(!f.calls().some((c) => c.path.endsWith('/grants'))); });
for (const mode of ['foreign-receipt', 'unknown-receipt']) test(`receipt ${mode} never reconciles or resends`, (t) => {
  const f = fixture(t); f.setup(); f.mode(mode); const result = f.run('receipt', ['--invocation-ref', 'inv_email', '--project-ref', 'prj_email']); if (mode === 'foreign-receipt') assert.notEqual(result.status, 0); else { assert.equal(result.status, 0, result.stderr); assert.equal(JSON.parse(result.stdout).status, 'NOT_PASS'); }
  assert(!f.calls().some((c) => c.path.includes('reconciliation') || c.path === '/api/v1/runs'));
});
test('missing explicit CA upload blocks publication; missing policy blocks plan', (t) => { const f = fixture(t); f.pass('connection'); assert.match(f.run('publish').stderr, /CREDENTIAL_ACK_REQUIRED/); assert(!f.calls().some((c) => c.path.endsWith('/drafts'))); const p = structuredClone(emailFixtureProfile); p.specification.policies.pop(); assert.throws(() => validateEmailProfile(p), /EXPLICIT_POLICY_REQUIRED/); });
test('POP3 is a separate INBOX profile; mixed IMAP and invented folders rejected', () => { const p = structuredClone(emailFixtureProfile); p.specification.receiveProtocol = 'POP3'; p.specification.pop = { ...p.specification.imap, port: 995 }; delete p.specification.imap; assert(validateEmailProfile(p).some((s) => s.slot === 'pop-ca')); p.specification.allowedFolders.push('Archive'); assert.throws(() => validateEmailProfile(p), /POP3_PROFILE_INVALID/); });

test('credential lost ACK: inspect reads exact receipt without acknowledging or replaying intent', (t) => {
  const f = fixture(t); f.pass('connection'); f.mode('credential-lost-ack'); assert.notEqual(f.run('credentials').status, 0); f.mode('');
  const result = f.pass('inspect'); assert.equal(result.unresolvedIntents, 1); assert.equal(result.credentialReceipts[0].status, 'FOUND');
  assert.notEqual(f.run('credentials').status, 0); assert.equal(f.calls().filter((c) => c.method === 'PUT').length, 1);
  assert.equal(f.events().filter((e) => e.type === 'ACK').length, 1);
});
test('grant recipient plan remains immutable independently of mailbox profile', (t) => {
  const f = fixture(t); f.setup(); f.pass('health'); f.pass('grants'); f.put('grant', { agentRef: 'agt_other', projectRef: 'prj_email', capabilities: ['email.message.send'] });
  assert.match(f.run('grants').stderr, /GRANT_PLAN_CHANGED/); assert.equal(f.calls().filter((c) => c.path.endsWith('/grants')).length, 1);
});
test('old effect receipt readback survives publication supersession without new effect', (t) => {
  const f = fixture(t); f.setup(); f.mode('publication-failed'); assert.equal(f.pass('receipt', ['--invocation-ref', 'inv_email', '--project-ref', 'prj_email']).status, 'PASS');
});
test('acknowledged INVALID validation cannot be skipped on a later publish invocation', (t) => {
  const f = fixture(t); f.pass('connection'); f.pass('credentials'); f.mode('invalid'); assert.match(f.run('publish').stderr, /MAILBOX_VALIDATE_FAILED/); f.mode(''); assert.match(f.run('publish').stderr, /MAILBOX_VALIDATE_FAILED/);
  assert(!f.calls().some((c) => c.path.endsWith('/publication')));
});
