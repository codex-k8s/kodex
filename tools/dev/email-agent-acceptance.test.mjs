import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { copyFileSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { emailAgentFixture, emailAgentTransport } from './email-agent-acceptance-fixture.mjs';
import { emailInputDigest, emailServingManifest, emailAgentReceipt } from './email-agent-acceptance.mjs';
import {createHash} from 'node:crypto';
import {privateJournal} from './role-image-acceptance.mjs';
import {observeRuntimePod} from '../release/runtime-pod-observe.mjs';
import {runtimePodFixture,runtimeCRIFixture} from '../release/runtime-pod-observe-fixture.mjs';
import { fingerprint } from '../release/scoped-release.mjs';
const origin = 'https://fixture.invalid';
function fixture(t, createAgent = false, combined = false) {
  const directory = mkdtempSync(join(tmpdir(), 'email-agent-')); t.after(() => rmSync(directory, { recursive: true, force: true }));
  const repository = join(directory, 'repo'); const scripts = join(repository, 'tools/dev'); const release = join(repository, 'tools/release'); mkdirSync(scripts, { recursive: true }); mkdirSync(release, { recursive: true });
  for (const name of ['email-agent-acceptance.mjs', 'email-combined-acceptance.mjs', 'role-image-runtime-proof.mjs', 'email-agent-acceptance-fixture.mjs', 'role-image-acceptance.mjs', 'owner-session-client.mjs', 'owner-session-storage.mjs', 'runtime-workspace-acceptance.mjs', 'runtime-provider-catalog.mjs']) copyFileSync(new URL(name, import.meta.url), join(scripts, name));
  for (const name of ['scoped-release.mjs', 'application-source.mjs', 'runtime-pod-observe.mjs', 'authority-executable-readback.mjs']) copyFileSync(new URL(`../release/${name}`, import.meta.url), join(release, name));
  const definitions = join(repository, 'contracts/integrations/v1/definitions'); mkdirSync(definitions, { recursive: true }); copyFileSync(new URL('../../contracts/integrations/v1/definitions/email.yaml', import.meta.url), join(definitions, 'email.yaml'));
  const git = (...args) => execFileSync('git', args, { cwd: repository, stdio: ['ignore', 'pipe', 'pipe'] });
  git('init', '--quiet'); git('add', '.'); git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'Локальная оснастка');
  const state = join(directory, 'state.jsonl'); const profile = join(directory, 'profile.json'); const manifest = join(directory, 'manifest.json'); const storage = join(directory, 'session.json'); const calls = join(directory, 'calls.json'); const loader = join(directory, 'loader.mjs'); const mode = join(directory, 'mode');
  const input = structuredClone(emailAgentFixture); if (createAgent) delete input.agentRef;
  if(combined){const previous=join(directory,'cfg.jsonl'),provenance=join(directory,'provenance.json'),h='a'.repeat(64);const j=privateJournal(previous,{version:1,origin,runnerDigest:`sha256:${h}`});j.append({type:'CHECKPOINT',step:'restore-complete',result:{status:'PASS',projectRef:input.projectRef,agentRef:input.agentRef,environmentRef:'env_fixture',recipeRef:'recipe_fixture',revisionRef:'mrev_fixture',artifactRef:'imgart_fixture',buildRef:'imgbld_fixture',manifestDigest:`sha256:${h}`,promotedReference:`pull.fixture.invalid/kodex/roles@sha256:${h}`,promotionReceiptSHA256:h}});j.close();writeFileSync(provenance,JSON.stringify({version:1,kind:'RUNNER_BINARY_PROVENANCE',sourceRevision:'b'.repeat(40),baseImage:`registry.fixture.invalid/runner@sha256:${h}`,binaryPath:'/usr/local/bin/kodex-agent-runner',binarySHA256:'b'.repeat(64)}),{mode:0o600});const digest=p=>createHash('sha256').update(readFileSync(p)).digest('hex');input.combined={version:1,fixtureState:previous,fixtureSHA256:digest(previous),runnerProvenance:provenance,runnerProvenanceSHA256:digest(provenance)};}
  writeFileSync(profile, JSON.stringify(input), { mode: 0o600 });
  const identity = { version: 1, profile: 'component-revisions', clusterUID: '33333333-3333-4333-8333-333333333333', namespace: 'kodex-system', namespaceUID: '44444444-4444-4444-8444-444444444444', components: ['control-api-gateway', 'control-plane', 'runtime-controller', 'integration-gateway', 'email-bridge', 'egress-gateway'].map((name) => ({ kind: 'Deployment', name })), compatibility: { version: 1 } };
  writeFileSync(manifest, JSON.stringify({ ...identity, manifestSHA256: fingerprint(identity), status: 'CAPTURED' }), { mode: 0o600 });
  writeFileSync(calls, '[]', { mode: 0o600 }); writeFileSync(mode, '', { mode: 0o600 });
  writeFileSync(storage, JSON.stringify({ cookies: ['__Host-kodex-session', '__Host-kodex-csrf'].map((name, index) => ({ name, value: index ? '1'.repeat(43) : `v1.${'1'.repeat(64)}`, domain: 'fixture.invalid', path: '/', secure: true, httpOnly: !index, sameSite: 'Strict', expires: -1 })), origins: [] }), { mode: 0o600 });
  writeFileSync(loader, `import {readFileSync,writeFileSync} from 'node:fs';
import {emailAgentTransport} from ${JSON.stringify(`file://${join(scripts, 'email-agent-acceptance-fixture.mjs')}`)};
const mode=readFileSync(${JSON.stringify(mode)},'utf8');const profile=JSON.parse(readFileSync(${JSON.stringify(profile)}));let nonce;try{nonce=readFileSync(${JSON.stringify(state)},'utf8').trim().split('\\n').map(JSON.parse).findLast(e=>e.step==='plan')?.result.combined?.nonce;}catch{};const transport=emailAgentTransport(mode,{combined:Boolean(profile.combined),nonce});const path=${JSON.stringify(calls)};
globalThis.fetch=async(url,init={})=>{if(url.origin!==${JSON.stringify(origin)})throw new Error('ORIGIN_UNEXPECTED');
 const rows=JSON.parse(readFileSync(path));rows.push({path:url.pathname+url.search,method:init.method??'GET',key:new Headers(init.headers).get('Idempotency-Key')});writeFileSync(path,JSON.stringify(rows));
 if(url.pathname==='/api/v1/session'){if(mode==='expired')return new Response('',{status:401});const at=(ms)=>new Date(Date.now()+ms).toISOString();return new Response(JSON.stringify({generation:'11111111-1111-4111-8111-111111111111',version:1,sessionRevision:1,renewalMode:'BACKEND_REFRESH',serverTime:at(0),accessExpiresAt:at(300000),expiresAt:at(1800000),absoluteExpiresAt:at(3600000),renewAfter:at(180000)}),{headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});}
 if(init.method==='POST'&&mode==='lost-ack')throw new Error('FIXTURE_UNKNOWN');if(init.method==='POST'&&mode==='rejected-http')return new Response('{}',{status:409});
 const body=transport(url.pathname+url.search,init.method??'GET');return new Response(Buffer.isBuffer(body)?body:JSON.stringify(body),{status:init.method==='POST'?201:200,headers:{'Content-Type':'application/json'}});
};`, { mode: 0o600 });
  const run = (phase, extra = []) => spawnSync(process.execPath, ['--import', loader, join(scripts, 'email-agent-acceptance.mjs'), phase, '--origin', origin, '--storage-state', storage, '--state', state, '--profile', profile, '--serving-manifest', manifest, '--timeout-ms', '60000', ...extra], { encoding: 'utf8', timeout: 10000 });
  return { directory, run, state, profile, manifest, setMode: (value) => writeFileSync(mode, value), calls: () => JSON.parse(readFileSync(calls)), events: () => readFileSync(state, 'utf8').trim().split('\n').map(JSON.parse) };
}
const confirmation = ['--confirm', 'START-ONE-STAGING-EMAIL-RUN'];
function planned(t, create = false) { const f = fixture(t, create); if (create) { const r = f.run('agent', ['--confirm', 'CREATE-STAGING-EMAIL-AGENT']); assert.equal(r.status, 0, r.stderr); } const r = f.run('plan'); assert.equal(r.status, 0, r.stderr); return f; }
function launched(t) { const f = planned(t); const r = f.run('launch', confirmation); assert.equal(r.status, 0, r.stderr); return f; }
test('public CLI creates separate Agent then plan/launch/capture/receipt with safe exact event', (t) => {
  const f = planned(t, true); const agent = f.run('agent', ['--confirm', 'CREATE-STAGING-EMAIL-AGENT']); assert.equal(agent.status, 0, agent.stderr);
  for (const phase of ['launch', 'launch', 'capture', 'receipt', 'receipt']) { const r = f.run(phase, phase === 'launch' ? confirmation : []); assert.equal(r.status, 0, r.stderr); }
  assert.equal(f.events().findLast((e) => e.step === 'receipt').result.status, 'PASS'); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 2);
  const raw = readFileSync(f.state, 'utf8'); for (const value of ['qa@example.invalid', 'private marker', 'private <input>', 'body_text']) assert(!raw.includes(value));
});
for (const mode of ['lost-ack', 'rejected-http', 'expired', 'stale', 'missing-grant', 'ineffective', 'account', 'catalog', 'publication', 'disabled', 'foreign', 'narrowed']) test(`public CLI launch rejects ${mode} without second Run`, (t) => {
  const f = planned(t); f.setMode(mode); assert.equal(f.run('launch', confirmation).status, 1); assert.equal(f.run('launch', confirmation).status, 1);
  assert.equal(f.calls().filter((c) => c.method === 'POST').length, ['lost-ack', 'rejected-http'].includes(mode) ? 1 : 0);
});
for (const mode of ['attempt', 'foreign-event', 'wrong-grant', 'input-changed', 'multiple', 'malformed-ref', 'old-reader', 'runtime-drift', 'foreign-node']) test(`public CLI capture rejects ${mode}`, (t) => { const f = launched(t); f.setMode(mode); const r = f.run('capture'); assert.equal(r.status, 1, r.stdout); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 1); });
for (const state of ['FAILED', 'REJECTED', 'CANCELLED', 'UNKNOWN_OUTCOME']) test(`public CLI preserves terminal ${state} without new launch`, (t) => { const f = launched(t); f.setMode(state); const r = f.run('capture'); assert.equal(r.status, 2, r.stderr); assert.equal(JSON.parse(r.stdout).status, state); assert.equal(f.run('launch', confirmation).status, 0); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 1); });
for (const mode of ['gate', 'pending']) test(`public CLI observes ${mode} without retry or Gate resolution`, (t) => { const f = launched(t); f.setMode(mode); const r = f.run('capture'); assert.equal(r.status, 0, r.stderr); assert.equal(JSON.parse(r.stdout).status, mode === 'gate' ? 'WAITING_HUMAN' : 'PENDING'); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 1); });
for (const mode of ['foreign-receipt', 'unknown-receipt']) test(`public CLI receipt ${mode} never produces PASS or external retry`, (t) => { const f = launched(t); assert.equal(f.run('capture').status, 0); f.setMode(mode); const r = f.run('receipt'); assert.equal(r.status, mode === 'foreign-receipt' ? 1 : 2); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 1); });
test('public CLI changed private profile rejects before further request', (t) => { const f = planned(t); const p = JSON.parse(readFileSync(f.profile)); p.input.subject = 'changed'; writeFileSync(f.profile, JSON.stringify(p)); const before = f.calls().length; assert.equal(f.run('launch', confirmation).status, 1); assert.equal(f.calls().length, before); });
for (const [phase, flags] of [['launch', []], ['capture', confirmation], ['agent', []], ['unknown', []], ['plan', ['--unknown', 'x']], ['plan', ['--profile', 'other']]]) test(`public CLI rejects argv ${phase} ${flags[0] ?? ''}`, (t) => { const f = fixture(t); assert.equal(f.run(phase, flags).status, 1); assert.deepEqual(f.calls(), []); });
test('input digest matches Go JSON escaping and ignores field order', () => { assert.equal(emailInputDigest({ to: 'x', body_text: '<>&\u2028\u2029Пример' }), 'de21782cb0dcacb339a20ae7affa83faedf9b7e876dc5e52268e300f5d767181'); assert.equal(emailInputDigest({ to: 'x', body_text: '<>&\u2028\u2029Пример' }), emailInputDigest({ body_text: '<>&\u2028\u2029Пример', to: 'x' })); });
test('manifest refuses inventory and altered semantic bytes', () => { assert.throws(() => emailServingManifest({ status: 'INVENTORIED' }), /CAPTURED_MANIFEST_REQUIRED/); });

test('public CLI Agent lost ACK never creates a second Agent', (t) => { const f = fixture(t, true); f.setMode('lost-ack'); for (let i = 0; i < 2; i++) assert.equal(f.run('agent', ['--confirm', 'CREATE-STAGING-EMAIL-AGENT']).status, 1); assert.equal(f.calls().filter((c) => c.method === 'POST').length, 1); });
test('public CLI altered captured manifest fails before network', (t) => { const f = fixture(t); const m = JSON.parse(readFileSync(f.manifest)); m.clusterUID = 'changed'; writeFileSync(f.manifest, JSON.stringify(m)); assert.equal(f.run('plan').status, 1); assert.equal(f.calls().length, 0); });
test('READ_ONLY capture does not invent an effect receipt or content proof', async () => {
  const profile = { ...emailAgentFixture, capabilityKey: 'email.mailbox.list', input: {} };
  const captured = { invocationRef: 'inv_fixture', invocationState: 'SUCCEEDED', runRef: 'run_fixture', sessionRef: 'ses_fixture', attempt: 1, runState: 'SUCCEEDED', runTerminal: true, lastEventSequence: 1 };
  const journal = { events: [{ step: 'capture', type: 'CHECKPOINT', result: captured }], append() {} }; const paths = [];
  const result = await emailAgentReceipt({ profile, journal, plan: { agentRef: profile.agentRef }, get: async (path) => { paths.push(path); return { ref: captured.runRef, projectRef: profile.projectRef, sessionRef: captured.sessionRef, target: { ref: profile.agentRef }, attempt: 1, state: 'SUCCEEDED', lastEventSequence: 1 }; } });
  assert.equal(result.status, 'READ_COMPLETED'); assert.equal(result.receipt, 'NOT_APPLICABLE_READ_ONLY'); assert.equal(result.contentVerification, 'NOT_RUN'); assert.equal(paths.length, 1);
});

function combinedLaunched(t){const f=fixture(t,false,true);for(const phase of ['plan','launch']){const r=f.run(phase,phase==='launch'?confirmation:[]);assert.equal(r.status,0,r.stderr);}return f;}
function observationFor(f){const bindingPath=join(f.directory,'binding.json');const r=f.run('runtime-binding',['--runtime-binding-output',bindingPath]);assert.equal(r.status,0,r.stderr);const b=JSON.parse(readFileSync(bindingPath));const pod=runtimePodFixture(b);const proof=observeRuntimePod(b,pod,{readCRI:()=>runtimeCRIFixture(pod),readProcess:()=>({pid:20,initPID:10,binarySHA256:'b'.repeat(64),startTicks:'100',device:'1',inode:'2'}),readPod:()=>pod});const path=join(f.directory,'observation.json');writeFileSync(path,JSON.stringify(proof),{mode:0o600});return {path,proof,bindingPath};}
test('combined public CLI uses one Run, real workspace verifier and exact Pod/binary/receipt',t=>{const f=combinedLaunched(t);const o=observationFor(f);let r=f.run('capture',['--runtime-observation',o.path]);assert.equal(r.status,0,r.stderr);assert.equal(JSON.parse(r.stdout).workspace.checks.nativeAgentShell,'PASS');r=f.run('receipt');assert.equal(r.status,0,r.stderr);assert.equal(JSON.parse(r.stdout).status,'PASS');assert.equal(f.calls().filter(c=>c.method==='POST').length,1);assert.equal(f.events().filter(e=>e.type==='INTENT').length,1);});
for(const mode of ['partial-workspace','multiple','UNKNOWN_OUTCOME','old-reader','wrong-image'])test(`combined public CLI rejects ${mode} without another effect`,t=>{const f=combinedLaunched(t);const o=observationFor(f);f.setMode(mode);const r=f.run('capture',['--runtime-observation',o.path]);assert.notEqual(r.status,0);assert.notEqual(f.run('receipt').status,0);assert.equal(f.run('launch',confirmation).status,0);assert.equal(f.calls().filter(c=>c.method==='POST').length,1);});
for(const mode of ['gate','pending'])test(`combined ${mode} is not PASS`,t=>{const f=combinedLaunched(t);f.setMode(mode);const r=f.run('capture');assert.equal(r.status,0,r.stderr);assert.equal(JSON.parse(r.stdout).status,mode==='gate'?'WAITING_HUMAN':'PENDING');assert.notEqual(f.run('receipt').status,0);});
test('combined missing observer or mismatched binary cannot produce receipt PASS',t=>{const f=combinedLaunched(t);assert.equal(f.run('capture').status,1);const o=observationFor(f);o.proof.process.binarySHA256='c'.repeat(64);writeFileSync(o.path,JSON.stringify(o.proof));assert.equal(f.run('capture',['--runtime-observation',o.path]).status,1);assert.equal(f.run('receipt').status,1);});
test('combined lost Run ACK does not retry',t=>{const f=fixture(t,false,true);assert.equal(f.run('plan').status,0);f.setMode('lost-ack');assert.equal(f.run('launch',confirmation).status,1);assert.equal(f.run('launch',confirmation).status,1);assert.equal(f.calls().filter(c=>c.method==='POST').length,1);});

for(const field of ['fixtureState','runnerProvenance'])test(`combined changed ${field} fails before requests`,t=>{const f=fixture(t,false,true);assert.equal(f.run('plan').status,0);const p=JSON.parse(readFileSync(f.profile));const path=p.combined[field];writeFileSync(path,readFileSync(path,'utf8')+'\n');const count=f.calls().length;assert.equal(f.run('launch',confirmation).status,1);assert.equal(f.calls().length,count);});
test('combined malformed invocation result remains closed',t=>{const f=combinedLaunched(t);f.setMode('malformed-ref');assert.equal(f.run('capture').status,1);assert.equal(f.calls().filter(c=>c.method==='POST').length,1);});

for (const combined of [false, true]) {
  test(`${combined ? 'combined' : 'standalone'} plan accepts ordinary Agent explicit false in READY and RUNNING`, t => {
    const f = fixture(t, false, combined);
    for (const mode of ['', 'agent-running']) {
      f.setMode(mode);
      const result = f.run('plan');
      assert.equal(result.status, 0, result.stderr);
    }
    assert.equal(f.calls().filter(call => call.method !== 'GET').length, 0);
  });
  for (const mode of ['missing-system', 'invalid-system', 'system-agent', 'agent-ref', 'foreign', 'missing-project', 'disabled', 'missing-enabled', 'invalid-enabled', 'agent-draft', 'agent-archived', 'agent-disabled', 'agent-unknown']) {
    test(`${combined ? 'combined' : 'standalone'} plan rejects Agent ${mode} before mutation`, t => {
      const f = fixture(t, false, combined);
      f.setMode(mode);
      const result = f.run('plan');
      assert.equal(result.status, 1, result.stdout);
      assert.match(result.stderr, /AGENT_NOT_READY/);
      assert.equal(f.calls().filter(call => call.method !== 'GET').length, 0);
      assert.equal(f.events().filter(event => event.type === 'INTENT').length, 0);
    });
  }
}

for (const combined of [false, true]) {
  test(`${combined ? 'combined' : 'standalone'} plan reads actual grant without schema and pinned capability candidate`, t => {
    const grant = emailAgentTransport()('/api/v1/integration-connections/int_fixture').grants[0];
    assert.equal(Object.hasOwn(grant, 'inputSchema'), false);
    assert.equal(Object.hasOwn(grant, 'inputSchemaSha256'), false);
    const f = fixture(t, false, combined);
    const result = f.run('plan');
    assert.equal(result.status, 0, result.stderr);
    const plan = f.events().findLast(event => event.step === 'plan').result;
    assert.equal(plan.projectVersion, 1);
    assert.equal(plan.candidateContextDigest, 'a'.repeat(64));
    assert.match(plan.inputSchemaSHA256, /^[a-f0-9]{64}$/);
    const reads = f.calls().filter(call => call.path.startsWith('/api/v1/integration-grant-candidates/capabilities?'));
    assert.equal(reads.length, 1);
    assert.equal(reads[0].method, 'GET');
    assert.equal(f.calls().filter(call => call.method !== 'GET').length, 0);
  });
  test(`${combined ? 'combined' : 'standalone'} candidate pagination keeps snapshot and finds one exact key`, t => {
    const f = fixture(t, false, combined); f.setMode('candidate-paged');
    const result = f.run('plan'); assert.equal(result.status, 0, result.stderr);
    assert.equal(f.calls().filter(call => call.path.startsWith('/api/v1/integration-grant-candidates/capabilities?')).length, 2);
  });
  for (const mode of ['grant-disabled', 'grant-duplicate', 'candidate-missing', 'candidate-foreign-key', 'candidate-duplicate', 'candidate-disabled', 'candidate-unknown', 'candidate-grant-ref', 'candidate-grant-version',
    'candidate-context-connectionRef', 'candidate-context-projectRef', 'candidate-context-recipientKind', 'candidate-context-recipientRef', 'candidate-context-workflowRef',
    'candidate-pin-connectionVersion', 'candidate-pin-definitionVersion', 'candidate-pin-definitionDigest', 'candidate-pin-projectVersion', 'candidate-pin-recipientVersion',
    'candidate-context-digest', 'candidate-item-pin', 'candidate-schema-missing', 'candidate-schema-digest', 'candidate-total', 'candidate-page-drift', 'candidate-page-duplicate', 'candidate-cursor']) {
    test(`${combined ? 'combined' : 'standalone'} candidate ${mode} rejects before intent`, t => {
      const f = fixture(t, false, combined); f.setMode(mode);
      const result = f.run('plan'); assert.equal(result.status, 1, result.stdout);
      assert.match(result.stderr, /CANDIDATE|GRANT/);
      assert.equal(f.calls().filter(call => call.method !== 'GET').length, 0);
      assert.equal(f.events().filter(event => event.type === 'INTENT').length, 0);
    });
  }
  for (const mode of ['candidate-pin-recipientVersion', 'candidate-grant-version', 'candidate-schema-digest']) {
    test(`${combined ? 'combined' : 'standalone'} launch rereads candidate ${mode} before Run intent`, t => {
      const f = fixture(t, false, combined); assert.equal(f.run('plan').status, 0);
      f.setMode(mode); assert.equal(f.run('launch', confirmation).status, 1);
      assert.equal(f.calls().filter(call => call.method !== 'GET').length, 0);
      assert.equal(f.events().filter(event => event.type === 'INTENT').length, 0);
    });
  }
}
