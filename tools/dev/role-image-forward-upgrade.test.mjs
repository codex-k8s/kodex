import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { privateJournal } from './role-image-acceptance.mjs';
import { existingFixture } from './role-image-runtime-proof.mjs';
import { loadCombinedProfile } from './email-combined-acceptance.mjs';
import { upgradeInputs, upgradePlan, applyUpgrade, inspectUpgrade } from './role-image-forward-upgrade.mjs';
import { sha, a, b, origin, fixture, initialState, upgradeTransportFixture } from './role-image-forward-upgrade-fixture.mjs';
function local(t, sourceSHA = '1'.repeat(40), directory) {
  directory ??= mkdtempSync(join(tmpdir(), 'riu-')); t.after(() => rmSync(directory, { recursive: true, force: true }));
  const previous = join(directory, 'previous.jsonl'), provenance = join(directory, 'provenance.json'), state = join(directory, 'upgrade.jsonl');
  const j = privateJournal(previous, { version: 1, origin, sourceSHA, runnerDigest: `sha256:${a}` }); j.append({ type: 'CHECKPOINT', step: 'restore-complete', result: { status: 'PASS', ...fixture } }); j.close();
  const bytes = readFileSync(previous);
  writeFileSync(provenance, JSON.stringify({ version: 1, kind: 'RUNNER_BINARY_PROVENANCE', sourceRevision: sourceSHA, baseImage: `registry.fixture.invalid/runner@sha256:${b}`, binaryPath: '/usr/local/bin/kodex-agent-runner', binarySHA256: b }), { mode: 0o600 });
  const profile = { version: 1, previousState: previous, previousSHA256: sha(bytes), runnerProvenance: provenance, runnerProvenanceSHA256: sha(readFileSync(provenance)) };
  const inputs = upgradeInputs(profile, origin), transport = upgradeTransportFixture();
  return { directory, previous, provenance, state, bytes, profile, inputs, transport, async journal() {
    const pins = await upgradePlan(inputs, transport.get);
    const j = privateJournal(state, { version: 1, kind: 'ROLE_IMAGE_FORWARD_UPGRADE', origin, sourceSHA, previousState: previous, previousSHA256: profile.previousSHA256, runnerDigest: inputs.runnerDigest, runnerProvenanceSHA256: inputs.runnerProvenanceSHA256, pins }, { createOnly: true });
    j.append({ type: 'CHECKPOINT', step: 'upgrade-plan', result: { status: 'READY', pinsSHA256: sha(pins) } }); return j;
  } };
}
test('полный forward pipeline сохраняет predecessor и выдаёт exact upgraded combined fixture', async t => {
  const f = local(t), j = await f.journal();
  const result = await applyUpgrade(j, f.inputs, f.transport.get, f.transport.request); j.close();
  assert.equal(result.status, 'PASS'); assert.equal(result.manifestDigest, `sha256:${b}`); assert.equal(result.binding.version, 2);
  assert.deepEqual(readFileSync(f.previous), f.bytes);
  assert.equal(f.transport.state.calls.length, 6); assert.equal(new Set(f.transport.state.calls.map(c => c.key)).size, 6);
  assert.equal(f.transport.state.calls.some(c => /runs|credentials|agents$/.test(c.path)), false);
  const next = existingFixture(f.state, sha(readFileSync(f.state)), origin); assert.equal(next.agentRef, fixture.agentRef); assert.equal(next.artifactRef, 'artifact2');
  const profile = { projectRef: fixture.projectRef, agentRef: fixture.agentRef, combined: { version: 1, fixtureState: f.state, fixtureSHA256: sha(readFileSync(f.state)), runnerProvenance: f.provenance, runnerProvenanceSHA256: f.profile.runnerProvenanceSHA256 } };
  assert.equal(loadCombinedProfile(profile, origin, readFileSync, { clusterUID: 'cluster', namespaceUID: 'namespace' }).fixture.manifestDigest, `sha256:${b}`);
  assert.throws(() => loadCombinedProfile({ ...profile, combined: { ...profile.combined, fixtureState: f.previous, fixtureSHA256: f.profile.previousSHA256 } }, origin, readFileSync, { clusterUID: 'cluster', namespaceUID: 'namespace' }), /RUNNER_PROVENANCE_INVALID/);
  const again = privateJournal(f.state, { version: 1, origin }); await applyUpgrade(again, f.inputs, f.transport.get, f.transport.request); again.close(); assert.equal(f.transport.state.calls.length, 6);
  const nextInputs = { ...f.profile, previousState: f.state, previousSHA256: sha(readFileSync(f.state)) };
  assert.throws(() => upgradeInputs(nextInputs, origin), /NEW_RUNNER_REQUIRED/);
});
for (const mode of ['foreign', 'base-drift']) test(`plan закрывает ${mode} до mutation`, async t => {
  const f = local(t); f.transport.state.mode = mode;
  await assert.rejects(upgradePlan(f.inputs, f.transport.get)); assert.equal(f.transport.state.calls.length, 0); assert.equal(existsSync(f.state), false);
});
test('план закрепляет fresh version, apply закрывает stale pins до первого INTENT', async t => {
  const f = local(t), j = await f.journal(); f.transport.state.configVersion++;
  await assert.rejects(applyUpgrade(j, f.inputs, f.transport.get, f.transport.request), /UPGRADE_PLAN_STALE/); assert.equal(j.events.some(e => e.type === 'INTENT'), false); j.close();
});
for (const mode of ['lost-ack', 'build-failed', 'missing-rebind']) test(`partial ${mode} не становится combined PASS`, async t => {
  const f = local(t), j = await f.journal(); f.transport.state.mode = mode;
  await assert.rejects(applyUpgrade(j, f.inputs, f.transport.get, f.transport.request));
  const before = f.transport.state.calls.length;
  if (mode === 'lost-ack') {
    assert.equal((await inspectUpgrade(j, f.transport.get)).status, 'UNKNOWN_READBACK_REQUIRED');
    await assert.rejects(applyUpgrade(j, f.inputs, f.transport.get, f.transport.request), /UNRESOLVED_INTENT_READBACK_REQUIRED/);
    assert.equal(f.transport.state.calls.length, before); assert.equal(f.transport.state.calls.filter(c => c.path.endsWith('/publication')).length, 1);
  }
  j.close(); assert.throws(() => existingFixture(f.state, sha(readFileSync(f.state)), origin), /FIXTURE_INTENT_UNRESOLVED|FIXTURE_NOT_COMPLETE/);
  assert.deepEqual(readFileSync(f.previous), f.bytes);
});
for (const mode of ['predecessor-bytes', 'unresolved', 'nonterminal', 'provenance-bytes', 'same-base']) test(`inputs reject ${mode}`, t => {
  const f = local(t), p = { ...f.profile };
  if (mode === 'predecessor-bytes') p.previousSHA256 = b;
  if (mode === 'unresolved' || mode === 'nonterminal') { const j = privateJournal(f.previous, { version: 1, origin }); j.append(mode === 'unresolved' ? { type: 'INTENT', key: 'pending', step: 'next' } : { type: 'CHECKPOINT', step: 'other', result: {} }); j.close(); p.previousSHA256 = sha(readFileSync(f.previous)); }
  if (mode === 'provenance-bytes') p.runnerProvenanceSHA256 = a;
  if (mode === 'same-base') { const value = JSON.parse(readFileSync(f.provenance)); value.baseImage = `registry.fixture.invalid/runner@sha256:${a}`; writeFileSync(f.provenance, JSON.stringify(value)); p.runnerProvenanceSHA256 = sha(readFileSync(f.provenance)); }
  assert.throws(() => upgradeInputs(p, origin));
});
for (const mode of ['missing-rebind-ack', 'wrong-header', 'changed-predecessor', 'wrong-new-provenance']) test(`completed chain rejects ${mode}`, async t => {
  const f = local(t), j = await f.journal(); await applyUpgrade(j, f.inputs, f.transport.get, f.transport.request); j.close();
  let events = readFileSync(f.state, 'utf8').trimEnd().split('\n').map(JSON.parse);
  if (mode === 'missing-rebind-ack') events = events.filter(e => !(e.type === 'ACK' && e.step === 'upgrade-rebind'));
  if (mode === 'wrong-header') delete events[0].kind;
  if (mode === 'changed-predecessor') events[0].previousSHA256 = a;
  if (mode === 'wrong-new-provenance') events[0].runnerProvenanceSHA256 = a;
  writeFileSync(f.state, `${events.map(JSON.stringify).join('\n')}\n`);
  assert.throws(() => existingFixture(f.state, sha(readFileSync(f.state)), origin));
});
function cli(t, mode = '') {
  const directory = mkdtempSync(join(tmpdir(), 'riucli-')), repository = join(directory, 'repo'), scripts = join(repository, 'tools/dev'); mkdirSync(scripts, { recursive: true });
  for (const name of ['role-image-forward-upgrade.mjs', 'role-image-forward-upgrade-fixture.mjs', 'role-image-runtime-proof.mjs', 'role-image-acceptance.mjs', 'owner-session-client.mjs', 'owner-session-storage.mjs', 'runtime-workspace-acceptance.mjs', 'runtime-provider-catalog.mjs']) copyFileSync(new URL(name, import.meta.url), join(scripts, name));
  const git = (...args) => execFileSync('git', args, { cwd: repository, encoding: 'utf8', stdio: 'pipe' }).trim();
  git('init', '-q'); git('add', 'tools'); git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'Синтетическая оснастка');
  const f = local(t, git('rev-parse', 'HEAD'), directory), profilePath = join(directory, 'profile.json'), manifest = join(directory, 'manifest.json'), storage = join(directory, 'session.json'), transport = join(directory, 'transport.json'), loader = join(directory, 'loader.mjs');
  writeFileSync(profilePath, JSON.stringify(f.profile), { mode: 0o600 }); writeFileSync(manifest, '{}', { mode: 0o600 }); writeFileSync(transport, JSON.stringify({ ...initialState(), mode }), { mode: 0o600 });
  writeFileSync(storage, JSON.stringify({ cookies: ['__Host-kodex-session', '__Host-kodex-csrf'].map((name, i) => ({ name, value: i ? '1'.repeat(43) : `v1.${'1'.repeat(64)}`, domain: 'fixture.invalid', path: '/', secure: true, httpOnly: !i, sameSite: 'Strict', expires: -1 })), origins: [] }), { mode: 0o600 });
  writeFileSync(loader, `import {readFileSync,writeFileSync} from 'node:fs';
import {upgradeTransportFixture} from ${JSON.stringify(`file://${join(scripts, 'role-image-forward-upgrade-fixture.mjs')}`)};
const path=${JSON.stringify(transport)}, t=upgradeTransportFixture(JSON.parse(readFileSync(path)));
globalThis.fetch=async(url,init={})=>{
 url=new URL(url); if(url.origin!=='https://fixture.invalid')throw new Error('ORIGIN_INVALID');
 if(url.pathname==='/api/v1/session'){
  if(${JSON.stringify(mode)}==='expired')return new Response('',{status:401});
  const at=ms=>new Date(Date.now()+ms).toISOString();
  return new Response(JSON.stringify({generation:'11111111-1111-4111-8111-111111111111',version:1,sessionRevision:1,renewalMode:'BACKEND_REFRESH',serverTime:at(0),accessExpiresAt:at(300000),expiresAt:at(1800000),absoluteExpiresAt:at(3600000),renewAfter:at(180000)}),{headers:{'Content-Type':'application/json','Cache-Control':'no-store'}});
 }
 try{return await t.request(url.pathname+url.search,init);}finally{writeFileSync(path,JSON.stringify(t.state));}
};`, { mode: 0o600 });
  return { ...f, calls: () => JSON.parse(readFileSync(transport)).calls, run(phase, extra = []) { return spawnSync(process.execPath, ['--import', loader, join(scripts, 'role-image-forward-upgrade.mjs'), phase, '--origin', origin, '--storage-state', storage, '--profile', profilePath, '--serving-manifest', manifest, '--state', f.state, '--timeout-ms', '10000', ...extra], { encoding: 'utf8', timeout: 15000 }); } };
}
test('public CLI actual argv plan→apply→inspect; old journal immutable; apply never creates second build', t => {
  const f = cli(t);
  for (const phase of ['plan', 'apply', 'inspect', 'apply']) { const r = f.run(phase, phase === 'apply' ? ['--confirm', 'APPLY-STAGING-ROLE-IMAGE-UPGRADE'] : []); assert.equal(r.status, 0, r.stderr); assert.match(r.stdout, /READY|PASS|COMPLETED/); }
  assert.equal(f.calls().length, 6); assert.deepEqual(readFileSync(f.previous), f.bytes);
  assert.equal(f.run('plan').status, 1); assert.equal(f.calls().length, 6);
});
test('public CLI lost ACK → authoritative inspect, apply blocked without repeat publication', t => {
  const f = cli(t, 'lost-ack'); assert.equal(f.run('plan').status, 0);
  assert.equal(f.run('apply', ['--confirm', 'APPLY-STAGING-ROLE-IMAGE-UPGRADE']).status, 1);
  const inspect = f.run('inspect'); assert.equal(inspect.status, 0, inspect.stderr); assert.equal(JSON.parse(inspect.stdout).status, 'UNKNOWN_READBACK_REQUIRED');
  assert.equal(JSON.parse(inspect.stdout).matchingRevisions[0].ref, 'revision2');
  assert.equal(f.run('apply', ['--confirm', 'APPLY-STAGING-ROLE-IMAGE-UPGRADE']).status, 1);
  assert.equal(f.calls().filter(c => c.path.endsWith('/publication')).length, 1);
});
for (const mode of ['expired', 'base-drift', 'foreign']) test(`public CLI ${mode} refuses before new journal`, t => {
  const f = cli(t, mode); assert.equal(f.run('plan').status, 1); assert.equal(existsSync(f.state), false); assert.equal(f.calls().length, 0);
});
for (const args of [['--unknown', 'x'], ['--timeout-ms', '2000'], ['--confirm', 'wrong']]) test('public CLI closed argument validation', t => {
  const f = cli(t); assert.equal(f.run('plan', args).status, 1); assert.equal(existsSync(f.state), false);
});
