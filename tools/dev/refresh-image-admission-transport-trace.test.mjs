import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { digest, fingerprint, refresh, verifySourceProof, SOURCE_ROOT } from './refresh-image-admission-diagnostics.mjs';
import { BASELINE_SCRIPT_SHA256, CURRENT_SCRIPT_SHA256, CANDIDATE_SCRIPT_SHA256, FAILURE_INVOCATION, CURRENT_TRACE_INVOCATION, TRACE_INVOCATION, TRACE_CHILD, candidateConfigMap, traceCandidate } from './refresh-image-admission-transport-trace.mjs';

const baseline = readFileSync(new URL('../../deploy/k8s/base/image-supply-chain/image-admission.sh', import.meta.url), 'utf8');
const candidate = traceCandidate(baseline);
const currentScript = baseline.replace(FAILURE_INVOCATION, CURRENT_TRACE_INVOCATION);
const id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa';
const options = { mode: 'apply', context: 'k3d-kodex', sourceRoot: SOURCE_ROOT, sourceRevision: 'a'.repeat(40), apiServer: 'https://127.0.0.2:6443', clusterUID: id, namespaceUID: id, configmapUID: id, resourceVersion: '100', serverNodeUID: id, agentNodeUID: id };
function fixture() {
  const cm = { apiVersion: 'v1', kind: 'ConfigMap', metadata: { name: 'kodex-image-admission', namespace: 'kodex-system', uid: id, resourceVersion: '100', labels: { 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload', 'kodex.dev/security-profile': 'trusted-cluster' } }, data: { 'image-admission.sh': currentScript, 'provenance-policy.jq': 'immutable fixture' } };
  return { cm, o: { ...options, baselineDataSHA256: fingerprint(cm.data) } };
}
function mock(f) {
  let current = structuredClone(f.cm), writes = 0;
  return { writes: () => writes, io: {
    endpoint: () => f.o.apiServer,
    source: () => verifySourceProof(f.o.sourceRevision, '', candidate, candidate, f.o),
    kubectl: (args, input) => {
      if (args.includes('replace')) {
        const sent = JSON.parse(input); assert.equal(sent.metadata.uid, id); assert.equal(sent.metadata.resourceVersion, '100');
        assert.equal(sent.data['provenance-policy.jq'], f.cm.data['provenance-policy.jq']);
        if (!args.includes('--dry-run=server')) { writes++; sent.metadata.resourceVersion = '101'; current = sent; }
        return JSON.stringify(sent);
      }
      if (args.includes('configmap')) return JSON.stringify(current);
      if (args.includes('namespace')) return JSON.stringify({ metadata: { uid: id, labels: { 'app.kubernetes.io/part-of': 'kodex', 'kodex.dev/local-profile': 'hot-reload' } } });
      if (args.includes('nodes')) return JSON.stringify({ items: ['k3d-kodex-server-0', 'k3d-kodex-agent-0'].map(name => ({ metadata: { name, uid: id }, status: { conditions: [{ type: 'Ready', status: 'True' }] } })) });
      if (args.includes('persistentvolumeclaim')) return JSON.stringify({ metadata: { uid: '7bba3b9e-a3ff-4102-b484-300b733e416f', labels: { 'kodex.dev/image-admission-id': '320b1cbe46eb01e9908e39eb630a27b2' }, annotations: { 'kodex.dev/admission-run-sha256': '320b1cbe46eb01e9908e39eb630a27b28ca7ece389f5bdcf01d4a67103d14c7e' } } });
      if (args.includes('validatingadmissionpolicy')) return JSON.stringify({ metadata: { name: args[2], uid: id, generation: 1 }, status: { observedGeneration: 1 } });
      throw new Error('UNEXPECTED_TEST_COMMAND');
    },
  } };
}

test('dev candidate заменяет только одну fail invocation; production source неизменён', () => {
  assert.equal(digest(baseline), BASELINE_SCRIPT_SHA256);
  assert.equal(digest(currentScript), CURRENT_SCRIPT_SHA256);
  assert.equal(digest(candidate), CANDIDATE_SCRIPT_SHA256);
  assert.equal(candidate.replace(TRACE_INVOCATION, FAILURE_INVOCATION), baseline);
  assert.equal(candidate.split('image-admission-bridge fail').length, 2);
  assert.equal(TRACE_CHILD.split('sleep 2 || exit 1').length, 2);
  assert.equal(spawnSync('sh', ['-n'], { input: candidate, encoding: 'utf8' }).status, 0);
});

for (const [message, kind] of [
  ['dial tcp: lookup private.invalid: no such host', 'DNS'],
  ['dial tcp private.invalid: connection refused', 'REFUSED'],
  ['dial tcp private.invalid: i/o timeout', 'TIMEOUT'],
  ['transport: error reading server preface: EOF', 'PREFACE'],
  ['rpc error: code = Unavailable desc = private detail', 'OTHER'],
]) test(`stream projection ${kind} не раскрывает raw data`, () => {
  const secret = 'SYNTHETIC_TOKEN_DO_NOT_PRINT';
  const env = { PATH: process.env.PATH, BASH_ENV: '/dev/null', ENV: '/dev/null', IMAGE_OWNER_ADMISSION_FAILURE_CODE: 'ADMISSION_WORKER_FAILED',
    'BASH_FUNC_image-admission-bridge%%': `() { test "$1" = fail && test "$GRPC_GO_LOG_SEVERITY_LEVEL" = info && test "$GRPC_GO_LOG_VERBOSITY_LEVEL" = 2 && test "$IMAGE_OWNER_ADMISSION_FAILURE_CODE" = ADMISSION_WORKER_FAILED || return 99; printf '%s\\n' '${secret}' 'Authorization: Bearer ${secret}' 'https://private.invalid/${secret}' '${message}' >&2; printf '%s' '${secret}'; return 23; }` };
  const result = spawnSync('bash', ['--noprofile', '--norc', '-c', TRACE_CHILD], { env, encoding: 'utf8', timeout: 3000 });
  assert.equal(result.status, 23);
  assert.equal(result.stdout, '');
  assert.equal(result.stderr, `{"event":"IMAGE_ADMISSION_TRANSPORT_DIAGNOSTIC","class":"${kind}"}\n`);
  assert.ok(!result.stderr.includes(secret) && !result.stderr.includes('private.invalid'));
});

for (const status of [0, 1, 7, 127]) test(`callback exitcode ${status} и один вызов сохраняются`, () => {
  const result = spawnSync('bash', ['--noprofile', '--norc', '-c', TRACE_CHILD], { encoding: 'utf8', timeout: 3000, env: { PATH: process.env.PATH, BASH_ENV: '/dev/null',
    'BASH_FUNC_image-admission-bridge%%': `() { printf '%s\\n' 'connection refused' >&2; return ${status}; }` } });
  assert.equal(result.status, status); assert.equal(result.stdout, '');
  assert.equal(result.stderr.split('\n').filter(Boolean).length, 1);
});

test('POSIX parent исполняет только fixed Bash child и сохраняет return 1', () => {
  const script = `record_failure() {\n failure_code=ADMISSION_WORKER_FAILED\n${TRACE_INVOCATION}}\nrecord_failure\n`;
  // На test-host worker binary отсутствует: закрытая child ошибка не даёт
  // raw command line и превращается в прежний нормализованный return 1.
  const result = spawnSync('sh', ['-c', script], { encoding: 'utf8', timeout: 3000,
    env: { PATH: '/usr/bin:/bin', BASH_ENV: '/dev/null', ENV: '/dev/null' } });
  assert.equal(result.status, 1);
  assert.equal(result.stdout, '');
  assert.equal(result.stderr, '{"event":"IMAGE_ADMISSION_TRANSPORT_DIAGNOSTIC","class":"OTHER"}\n');
});

test('многострочные raw metadata и большой record остаются только в pipe', () => {
  const result = spawnSync('bash', ['--noprofile', '--norc', '-c', TRACE_CHILD], { encoding: 'utf8', timeout: 3000,
    env: { PATH: process.env.PATH, BASH_ENV: '/dev/null',
      'BASH_FUNC_image-admission-bridge%%': '() { printf "%s\\n" "PRIVATE_CLAIM_TOKEN" "authorization=PRIVATE_CLAIM_TOKEN" >&2; printf "%100000s\\n" PRIVATE_CLAIM_TOKEN >&2; printf "%s\\n" "connection refused" >&2; return 7; }' } });
  assert.equal(result.status, 7);
  assert.equal(result.stdout, '');
  assert.equal(result.stderr, '{"event":"IMAGE_ADMISSION_TRANSPORT_DIAGNOSTIC","class":"REFUSED"}\n');
});

test('refresh сохраняет exact CAS и data: одна effect запись', () => {
  const f = fixture(), h = mock(f);
  assert.equal(refresh(f.o, candidate, h.io, candidateConfigMap).status, 'APPLIED');
  assert.equal(h.writes(), 1);
});
for (const [name, mutate] of [
  ['UID', f => { f.cm.metadata.uid = 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb'; }],
  ['RV', f => { f.cm.metadata.resourceVersion = '101'; }],
  ['data', f => { f.cm.data['provenance-policy.jq'] = 'drift'; }],
  ['script', f => { f.cm.data['image-admission.sh'] += '\n'; f.o.baselineDataSHA256 = fingerprint(f.cm.data); }],
  ['profile', f => { f.cm.metadata.labels['kodex.dev/security-profile'] = 'protected'; }],
]) test(`closed preflight ${name} не пишет CM`, () => {
  const f = fixture(); mutate(f); const h = mock(f);
  assert.throws(() => refresh(f.o, candidate, h.io, candidateConfigMap)); assert.equal(h.writes(), 0);
});
test('additional source effect и source HEAD drift отклоняются', () => {
  const f = fixture(), h = mock(f);
  assert.throws(() => refresh(f.o, candidate + '\n# unexpected effect', h.io, candidateConfigMap), /SCRIPT_CHANGE_NOT_APPROVED/);
  assert.equal(h.writes(), 0);
  h.io.source = () => verifySourceProof('b'.repeat(40), '', candidate, candidate, f.o);
  assert.throws(() => refresh(f.o, candidate, h.io, candidateConfigMap), /SOURCE_REVISION_MISMATCH/);
  assert.equal(h.writes(), 0);
});
