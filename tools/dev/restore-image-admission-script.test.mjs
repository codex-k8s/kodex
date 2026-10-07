import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { SOURCE_ROOT, API_SERVER, fingerprint, refresh } from './refresh-image-admission-diagnostics.mjs';
import { traceCandidate } from './refresh-image-admission-transport-trace.mjs';
import { RESTORE_PINS, restoreCandidate, restoreWorkspaceBoundary } from './restore-image-admission-script.mjs';

const canonical = readFileSync(new URL('../../deploy/k8s/base/image-supply-chain/image-admission.sh', import.meta.url), 'utf8');
const sql = readFileSync(new URL('./supply-chain-owner-readback.sql', import.meta.url), 'utf8');
const id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
function fixture(mode = 'apply') {
  const data = Object.fromEntries(['image-admission.sh', 'provenance-policy.jq', 'vulnerability-policy.jq'].map(name => [name, readFileSync(new URL(`../../deploy/k8s/base/image-supply-chain/${name}`, import.meta.url), 'utf8')]));
  data['image-admission.sh'] = traceCandidate(canonical);
  assert.equal(fingerprint(data), RESTORE_PINS.baselineDataSHA256);
  const cm = { apiVersion: 'v1', kind: 'ConfigMap', immutable: false, metadata: { uid: RESTORE_PINS.configmapUID, resourceVersion: RESTORE_PINS.resourceVersion, namespace: 'kodex-system', name: 'kodex-image-admission', labels: { 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload', 'kodex.dev/security-profile': 'trusted-cluster' }, annotations: { retained: 'fixture' }, managedFields: [{ manager: 'kubectl-replace', operation: 'Update', fieldsType: 'FieldsV1', fieldsV1: { 'f:data': { 'f:image-admission.sh': {} } } }] }, data };
  return { cm, options: { ...RESTORE_PINS, mode, context: 'k3d-kodex', apiServer: API_SERVER, sourceRoot: SOURCE_ROOT, sourceRevision: 'b'.repeat(40), clusterUID: id, namespaceUID: id, serverNodeUID: id, agentNodeUID: id }, controllerReplicas: 0, pvc: [], jobs: [], pods: [], owner: { at: new Date().toISOString(), openBuilds: 0, pendingAdmissions: 0, pendingPromotions: 0, activeRuntimeRuns: 0, claimedRuntimeLeases: 0, promotedArtifactCount: 1, promotedPinsSHA256: 'c'.repeat(64) } };
}
function harness(f) {
  let current = structuredClone(f.cm);
  const calls = [];
  const io = { endpoint: () => f.options.apiServer, source: () => {}, kubectl: (args, input) => {
    calls.push({ args, input });
    if (args.includes('replace')) {
      assert.ok(args.includes('--field-manager=kodex-local-dev'));
      assert.ok(!args.some(arg => /force|conflict/.test(arg)));
      const sent = JSON.parse(input);
      assert.deepEqual(sent.metadata, f.cm.metadata);
      assert.equal(sent.data['image-admission.sh'], canonical);
      for (const name of ['provenance-policy.jq', 'vulnerability-policy.jq']) assert.equal(sent.data[name], f.cm.data[name]);
      const response = structuredClone(sent);
      if (!args.includes('--dry-run=server')) { response.metadata.resourceVersion = '453811'; current = response; }
      return JSON.stringify(response);
    }
    if (args.includes('exec')) { assert.equal(input, sql); return JSON.stringify(f.owner); }
    if (args.includes('nodes')) return JSON.stringify({ items: ['k3d-kodex-server-0', 'k3d-kodex-agent-0'].map(name => ({ metadata: { name, uid: id }, status: { conditions: [{ type: 'Ready', status: 'True' }] } })) });
    if (args.includes('namespace')) return JSON.stringify({ metadata: { uid: id, labels: { 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload' } } });
    if (args.includes('deployment')) return JSON.stringify({ metadata: { uid: 'b0d061c8-a12f-4366-9413-cee2a8e774dd', generation: 2 }, spec: { replicas: f.controllerReplicas }, status: { observedGeneration: 2, replicas: f.controllerReplicas } });
    for (const kind of ['persistentvolumeclaims', 'jobs', 'pods']) if (args.includes(kind)) return JSON.stringify({ items: f[kind === 'persistentvolumeclaims' ? 'pvc' : kind] });
    if (args.includes('pod')) return JSON.stringify({ metadata: { uid: '9c861600-ba8a-431a-a55c-6849955693f1' }, status: { conditions: [{ type: 'Ready', status: 'True' }] } });
    if (args.includes('validatingadmissionpolicy')) return JSON.stringify({ metadata: { name: args[2], uid: id, generation: 1 }, status: { observedGeneration: 1, typeChecking: { expressionWarnings: [] } } });
    if (args.includes('configmap')) { assert.ok(args.includes('--show-managed-fields=true')); return JSON.stringify(current); }
    throw new Error('UNEXPECTED_TEST_COMMAND');
  } };
  return { io, calls, setCurrent: value => { current = value; }, writes: () => calls.filter(c => c.args.includes('replace') && !c.args.includes('--dry-run=server')) };
}
const perform = (f, h, guard = restoreWorkspaceBoundary(() => sql)) => refresh(f.options, canonical, h.io, restoreCandidate, guard);

for (const mode of ['check', 'dry-run', 'apply']) test(`restore ${mode}: exact CAS и manager, остальные поля сохранены`, () => {
  const f = fixture(mode), h = harness(f);
  assert.equal(perform(f, h).status, { check: 'CHECKED', 'dry-run': 'DRY_RUN_PASS', apply: 'APPLIED' }[mode]);
  assert.equal(h.writes().length, mode === 'apply' ? 1 : 0);
});
for (const [name, mutate] of [
  ['UID', f => { f.cm.metadata.uid = id; }], ['RV', f => { f.cm.metadata.resourceVersion = '453811'; }],
  ['pins', f => { f.options.resourceVersion = '453811'; }], ['data', f => { f.cm.data['provenance-policy.jq'] += '\n'; }],
  ['preimage', f => { f.cm.data['image-admission.sh'] = canonical; }], ['context', f => { f.options.context = 'external'; }],
  ['controller', f => { f.controllerReplicas = 1; }], ['workspace', f => { f.pvc = [{}]; }],
  ['jobs', f => { f.jobs = [{}]; }], ['pods', f => { f.pods = [{}]; }],
  ['not idle', f => { f.owner.pendingAdmissions = 1; }], ['stale owner', f => { f.owner.at = new Date(Date.now() - 31000).toISOString(); }],
]) test(`restore отклоняет ${name} до записи`, () => {
  const f = fixture(); mutate(f); const h = harness(f);
  assert.throws(() => perform(f, h)); assert.equal(h.writes().length, 0);
});
test('неизвестный source и дополнительные изменения canonical закрыто отклоняются', () => {
  const f = fixture(), h = harness(f);
  h.io.source = () => { throw new Error('SOURCE_REVISION_MISMATCH'); };
  assert.throws(() => perform(f, h), /SOURCE_REVISION_MISMATCH/); assert.equal(h.calls.length, 0);
  assert.throws(() => restoreCandidate(f.cm, canonical + '\n', f.options), /SCRIPT_CANONICAL_MISMATCH/);
});
test('CAS drift после dry-run не усыновляется', () => {
  const f = fixture(), h = harness(f), original = h.io.kubectl;
  h.io.kubectl = (args, input) => { const value = original(args, input); if (args.includes('--dry-run=server')) { const changed = structuredClone(f.cm); changed.metadata.resourceVersion = '453812'; h.setCurrent(changed); } return value; };
  assert.throws(() => perform(f, h), /PREFLIGHT_DRIFT/); assert.equal(h.writes().length, 0);
});
test('published pins drift и заменённый owner SQL закрыто отклоняются', () => {
  const f = fixture(), h = harness(f), original = h.io.kubectl;
  h.io.kubectl = (args, input) => { const value = original(args, input); if (args.includes('--dry-run=server')) f.owner.promotedPinsSHA256 = 'd'.repeat(64); return value; };
  assert.throws(() => perform(f, h), /PUBLISHED_PINS_CHANGED/); assert.equal(h.writes().length, 0);
  const other = fixture(), attempt = harness(other);
  assert.throws(() => perform(other, attempt, restoreWorkspaceBoundary(() => sql + '\n')), /OWNER_READ_SQL_MISMATCH/); assert.equal(attempt.writes().length, 0);
});
