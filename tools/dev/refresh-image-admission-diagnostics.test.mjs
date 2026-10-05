import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { BASELINE_SCRIPT_SHA256, CANDIDATE_SCRIPT_SHA256, SOURCE_ROOT, candidateConfigMap, digest, fingerprint, refresh, validateOptions, verifyReadback, verifySourceProof } from './refresh-image-admission-diagnostics.mjs';

const candidate = readFileSync(new URL('../../deploy/k8s/base/image-supply-chain/image-admission.sh', import.meta.url), 'utf8');
const baseline = candidate.replace('  # Bridge печатает только закрытый gRPC code, без remote error или claim.\n  # Сбой callback должен оставаться наблюдаемым, пока durable receipt не получен.\n', '').replace('image-admission-bridge fail || return 1', 'image-admission-bridge fail 2>/dev/null || return 1');
const id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
function fixture(mode = 'apply') {
  const cm = { apiVersion: 'v1', kind: 'ConfigMap', immutable: false, metadata: { name: 'kodex-image-admission', namespace: 'kodex-system', uid: id, resourceVersion: '100', labels: { 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload', 'kodex.dev/security-profile': 'trusted-cluster' }, annotations: { untouched: 'fixture' } }, data: { 'image-admission.sh': baseline, 'provenance-policy.jq': 'immutable fixture' } };
  const options = { mode, context: 'k3d-kodex', sourceRoot: SOURCE_ROOT, sourceRevision: 'a'.repeat(40), apiServer: 'https://127.0.0.2:6443', clusterUID: id, namespaceUID: id, configmapUID: id, resourceVersion: '100', baselineDataSHA256: fingerprint(cm.data), serverNodeUID: id, agentNodeUID: id };
  return { cm, options };
}
function harness(f) {
  let current = structuredClone(f.cm);
  const calls = [];
  const io = {
    endpoint: () => f.options.apiServer,
    source: () => {},
    kubectl: (args, input) => {
      calls.push({ args, input });
      if (args.includes('replace')) {
        const sent = JSON.parse(input);
        assert.equal(sent.metadata.uid, f.options.configmapUID);
        assert.equal(sent.metadata.resourceVersion, f.options.resourceVersion);
        assert.deepEqual(sent.metadata, f.cm.metadata);
        assert.equal(sent.data['provenance-policy.jq'], f.cm.data['provenance-policy.jq']);
        const response = structuredClone(sent);
        if (!args.includes('--dry-run=server')) { response.metadata.resourceVersion = '101'; current = response; }
        return JSON.stringify(response);
      }
      if (args.includes('nodes')) return JSON.stringify({ items: ['k3d-kodex-server-0', 'k3d-kodex-agent-0'].map(name => ({ metadata: { name, uid: id }, status: { conditions: [{ type: 'Ready', status: 'True' }] } })) });
      if (args.includes('namespace')) return JSON.stringify({ metadata: { uid: id, labels: { 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload' } } });
      if (args.includes('persistentvolumeclaim')) return JSON.stringify({ metadata: { uid: '7bba3b9e-a3ff-4102-b484-300b733e416f', labels: { 'kodex.dev/image-admission-id': '320b1cbe46eb01e9908e39eb630a27b2' }, annotations: { 'kodex.dev/admission-run-sha256': '320b1cbe46eb01e9908e39eb630a27b28ca7ece389f5bdcf01d4a67103d14c7e' } } });
      if (args.includes('validatingadmissionpolicy')) return JSON.stringify({ metadata: { name: args[2], uid: id, generation: 1 }, status: { observedGeneration: 1, typeChecking: { expressionWarnings: [] } } });
      if (args.includes('configmap')) return JSON.stringify(current);
      throw new Error('UNEXPECTED_TEST_COMMAND');
    },
  };
  return { io, calls, replaceCurrent: value => { current = value; }, writes: () => calls.filter(call => call.args.includes('replace') && !call.args.includes('--dry-run=server')) };
}

test('точные baseline и candidate принадлежат разрешённому source', () => {
  assert.equal(digest(baseline), BASELINE_SCRIPT_SHA256);
  assert.equal(digest(candidate), CANDIDATE_SCRIPT_SHA256);
  const f = fixture(), output = candidateConfigMap(f.cm, candidate, f.options);
  assert.deepEqual(output.metadata, f.cm.metadata);
  assert.equal(output.data['provenance-policy.jq'], f.cm.data['provenance-policy.jq']);
  assert.equal(output.data['image-admission.sh'], candidate);
});
test('отсутствующий immutable означает Kubernetes default false без добавления поля', () => {
  const f = fixture(); delete f.cm.immutable;
  const h = harness(f);
  assert.equal(refresh(f.options, candidate, h.io).status, 'APPLIED');
  assert.equal(Object.hasOwn(JSON.parse(h.writes()[0].input), 'immutable'), false);
  for (const invalid of [null, 'false', 0]) {
    const negative = fixture(); negative.cm.immutable = invalid;
    const attempt = harness(negative);
    assert.throws(() => refresh(negative.options, candidate, attempt.io), /CONFIGMAP_SHAPE_INVALID/);
    assert.equal(attempt.writes().length, 0);
  }
});
for (const mode of ['check', 'dry-run', 'apply']) test(`режим ${mode} соблюдает effect budget`, () => {
  const f = fixture(mode), h = harness(f), result = refresh(f.options, candidate, h.io);
  assert.equal(result.status, { check: 'CHECKED', 'dry-run': 'DRY_RUN_PASS', apply: 'APPLIED' }[mode]);
  assert.equal(h.writes().length, mode === 'apply' ? 1 : 0);
  assert.equal(h.calls.filter(call => call.args.includes('--dry-run=server')).length, mode === 'check' ? 0 : 1);
  assert.ok(h.calls.every(call => !call.args.some(arg => /secret|delete|apply|force|impersonate/.test(arg))));
});

for (const [name, mutate] of [
  ['UID', f => { f.cm.metadata.uid = 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb'; }],
  ['resourceVersion', f => { f.cm.metadata.resourceVersion = '101'; }],
  ['data', f => { f.cm.data['provenance-policy.jq'] += ' drift'; }],
  ['immutable', f => { f.cm.immutable = true; }],
  ['binaryData', f => { f.cm.binaryData = {}; }],
  ['profile', f => { f.cm.metadata.labels['kodex.dev/security-profile'] = 'protected'; }],
  ['script baseline', f => { f.cm.data['image-admission.sh'] += '\n'; f.options.baselineDataSHA256 = fingerprint(f.cm.data); }],
  ['context', f => { f.options.context = 'production'; }],
  ['source root', f => { f.options.sourceRoot = '/tmp/fixture'; }],
  ['source revision', f => { f.options.sourceRevision = 'main'; }],
  ['endpoint', f => { f.options.apiServer = 'https://external.example:6443'; }],
]) test(`preflight отклоняет ${name} до записи`, () => {
  const f = fixture(); mutate(f); const h = harness(f);
  assert.throws(() => refresh(f.options, candidate, h.io));
  assert.equal(h.writes().length, 0);
});
test('полный source diff не допускается даже с верным current data hash', () => {
  const f = fixture(), h = harness(f);
  assert.throws(() => refresh(f.options, `${candidate}\n# additional effect\n`, h.io), /SCRIPT_CHANGE_NOT_APPROVED/);
  assert.equal(h.writes().length, 0);
});
test('source HEAD drift и смена loopback target закрыто отклоняются', () => {
  for (const key of ['source', 'endpoint']) {
    const f = fixture(), h = harness(f);
    if (key === 'source') h.io.source = () => { throw new Error('SOURCE_REVISION_MISMATCH'); };
    else h.io.endpoint = () => 'https://127.0.0.1:6551';
    assert.throws(() => refresh(f.options, candidate, h.io)); assert.equal(h.writes().length, 0);
  }
});
test('дрейф CM после dry-run не усыновляется', () => {
  const f = fixture(), h = harness(f), original = h.io.kubectl;
  h.io.kubectl = (args, input) => {
    const response = original(args, input);
    if (args.includes('--dry-run=server')) { const changed = structuredClone(f.cm); changed.metadata.resourceVersion = '102'; h.replaceCurrent(changed); }
    return response;
  };
  assert.throws(() => refresh(f.options, candidate, h.io), /PREFLIGHT_DRIFT/); assert.equal(h.writes().length, 0);
});
test('warnings и stale observedGeneration не обходятся', () => {
  for (const warning of [true, false]) {
    const f = fixture(), h = harness(f), original = h.io.kubectl;
    h.io.kubectl = (args, input) => {
      const raw = original(args, input);
      if (!args.includes('validatingadmissionpolicy')) return raw;
      const policy = JSON.parse(raw);
      if (warning) policy.status.typeChecking.expressionWarnings.push({ fieldRef: 'spec.validations[0]' });
      else policy.status.observedGeneration = 0;
      return JSON.stringify(policy);
    };
    assert.throws(() => refresh(f.options, candidate, h.io), /ADMISSION_POLICY_NOT_COMPILED/); assert.equal(h.writes().length, 0);
  }
});
test('неизвестный результат replace не повторяется', () => {
  const f = fixture(), h = harness(f), original = h.io.kubectl;
  h.io.kubectl = (args, input) => { if (args.includes('replace') && !args.includes('--dry-run=server')) { original(args, input); throw new Error('COMMAND_FAILED'); } return original(args, input); };
  assert.throws(() => refresh(f.options, candidate, h.io), /COMMAND_FAILED/); assert.equal(h.writes().length, 1);
});
test('readback отклоняет замену UID и любой незапрошенный data/metadata effect', () => {
  const f = fixture(), expected = candidateConfigMap(f.cm, candidate, f.options);
  for (const mutate of [value => { value.metadata.uid = 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb'; }, value => { value.data['provenance-policy.jq'] = 'drift'; }, value => { value.metadata.annotations.untouched = 'drift'; }, value => { value.metadata.resourceVersion = '100'; }]) {
    const actual = structuredClone(expected); actual.metadata.resourceVersion = '101'; mutate(actual);
    assert.throws(() => verifyReadback(actual, expected, '100'));
  }
});
test('ConfigMap UID и все обязательные pins обязательны', () => {
  const f = fixture(); for (const key of Object.keys(f.options)) { const missing = { ...f.options }; delete missing[key]; assert.throws(() => validateOptions(missing)); }
});
test('source proof отклоняет tracked и untracked изменения', () => {
  const f = fixture();
  assert.doesNotThrow(() => verifySourceProof(f.options.sourceRevision, '', candidate, candidate, f.options));
  for (const status of [' M docs/fixture.md', 'M  tools/fixture.mjs', '?? private-fixture-input']) {
    assert.throws(() => verifySourceProof(f.options.sourceRevision, status, candidate, candidate, f.options), /SOURCE_WORKTREE_DIRTY/);
  }
  assert.throws(() => verifySourceProof('b'.repeat(40), '', candidate, candidate, f.options), /SOURCE_REVISION_MISMATCH/);
  assert.throws(() => verifySourceProof(f.options.sourceRevision, '', `${candidate}\n`, candidate, f.options), /SOURCE_REVISION_MISMATCH/);
});
