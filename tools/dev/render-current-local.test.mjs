import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { chmodSync, copyFileSync, mkdtempSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const script = join(dirname(fileURLToPath(import.meta.url)), 'render-current-local.sh');
const pins = ['agent-runner', 'session-archive', 'stt-hot-reload', 'integration-hot-reload', 'backup-controller', 'role-image-builder', 'image-admission', 'image-admission-tools', 'internal-rpc-authority'];

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'kodex-fresh-render-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const repo = join(root, 'repo'), state = join(root, 'state'), bin = join(root, 'bin');
  for (const path of [repo, state, bin, join(repo, 'tools'), join(repo, 'tools/dev'), join(state, 'cache')]) mkdirSync(path, { mode: 0o700 });
  const put = (path, data) => writeFileSync(path, data, { mode: 0o600 });
  copyFileSync(script, join(repo, 'tools/dev/render-current-local.sh'));
  put(join(repo, 'tools/dev/render-local.sh'), `#!/usr/bin/env bash
set -euo pipefail
node -e 'require("node:fs").writeFileSync(process.env.FIXTURE_ARGS, JSON.stringify(process.argv.slice(1)))' -- "$@"
while (($#)); do if [[ "$1" == --output ]]; then printf 'fixture private render\\n' >"$2"; break; fi; shift; done
printf 'PRIVATE_RENDERER_MARKER\\n'
`);
  execFileSync('git', ['init', '-q', repo]);
  execFileSync('git', ['-C', repo, 'add', '.']);
  execFileSync('git', ['-C', repo, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'Оснастка']);
  const sha = execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  const tree = execFileSync('git', ['-C', repo, 'rev-parse', 'HEAD^{tree}'], { encoding: 'utf8' }).trim();
  const sourceDigest = createHash('sha256').update(`BASE_TREE\0${tree}\0`).digest('hex');
  const fingerprint = createHash('sha256').update(`${sourceDigest}\0web-only\0`).digest('hex');
  const stateFile = join(state, 'authority-source-state.json');
  put(stateFile, JSON.stringify({ version: 1, sourceRevision: 7, sourceFingerprint: fingerprint, sourceSHA: sha }));
  put(join(state, 'role-image-input.json'), JSON.stringify({ manifestDigest: `sha256:${'1'.repeat(64)}`, payloadSha256: '2'.repeat(64), sourceSha256: '3'.repeat(64) }));
  put(join(state, 'mail-source.json'), '{}');
  put(join(state, 'integration-fixture-bearer-token'), 'NOT_READ_BY_WRAPPER');
  for (const pin of pins) put(join(state, `${pin}-image`), `registry.local.kodex/kodex/${pin}@sha256:${'a'.repeat(64)}`);
  const resources = [
    { kind: 'Ingress', metadata: { name: 'staff-control-center' }, spec: { rules: [{ host: 'kodex.example.invalid' }], ingressClassName: 'traefik' } },
    { kind: 'Certificate', metadata: { name: 'staff-control-center-public' }, spec: { issuerRef: { name: 'kodex-local' } } },
    { kind: 'ConfigMap', metadata: { name: 'kodex-platform-endpoints' }, data: { oidcTlsServerName: 'sso.example.invalid' } },
    { kind: 'ConfigMap', metadata: { name: 'kodex-image-admission-policy' }, data: { pullRegistryHost: 'pull.example.invalid', providerAppArmorProfile: '' } },
    { kind: 'ConfigMap', metadata: { name: 'kodex-dev-source-provenance' }, data: { deploymentProfile: 'web-only' } },
    { kind: 'Secret', metadata: { name: 'never-print' }, data: { token: 'PRIVATE_OLD_RENDER_MARKER' } },
  ];
  put(join(state, 'render.yaml'), resources.map(JSON.stringify).join('\n---\n'));
  const workloads = { items: ['control-plane', 'control-api-gateway', 'staff-control-center'].map(name => ({
    metadata: { name }, spec: { template: { metadata: {
      labels: { 'kodex.dev/security-profile': 'trusted-cluster' },
      annotations: { 'kodex.dev/source-root': repo, 'kodex.dev/cache-root': join(state, 'cache') },
    }, spec: {
      containers: [{ name, volumeMounts: [{ name: 'source', mountPath: name === 'staff-control-center' ? '/workspace/services/staff/control-center' : '/workspace', readOnly: true }] }],
      volumes: [{ name: 'source', hostPath: { path: name === 'staff-control-center' ? `${repo}/services/staff/control-center` : repo } }],
    } } },
  })) };
  const kubeFixture = join(root, 'kube-fixture.json');
  const data = { workloads, revision: 7, endpoint: '172.19.0.2' };
  const saveKube = () => put(kubeFixture, JSON.stringify(data));
  saveKube();
  const kubeconfig = join(root, 'kubeconfig'); put(kubeconfig, 'fixture');
  const fakeKube = join(bin, 'kubectl');
  put(fakeKube, `#!/usr/bin/env node
const fs=require('node:fs'), args=process.argv.slice(2), f=JSON.parse(fs.readFileSync(process.env.FIXTURE_KUBE));
fs.appendFileSync(process.env.FIXTURE_TRACE,JSON.stringify(args)+'\\n');
if(args.includes('get-contexts')) process.stdout.write('k3d-kodex');
else if(args.includes('view')) console.log(JSON.stringify({clusters:[{cluster:{server:'https://127.0.0.1:6443'}}]}));
else if(args.includes('deployment')) console.log(JSON.stringify(f.workloads));
else if(args.includes('secret/internal-rpc-authority-snapshot')) {
  if(f.revision===null) process.exit(1);
  const payload=Buffer.from(JSON.stringify({source_revision:f.revision,private:'PRIVATE_SNAPSHOT_MARKER'})).toString('base64url');
  process.stdout.write(Buffer.from('header.'+payload+'.signature').toString('base64'));
} else if(args.includes('service')) process.stdout.write('10.43.0.1');
else if(args.includes('endpointslice')) console.log(JSON.stringify({items:[{addressType:'IPv4',ports:[{protocol:'TCP',port:6443}],endpoints:[{conditions:{ready:true},addresses:[f.endpoint]}]}]}));
else process.exit(12);
`);
  chmodSync(fakeKube, 0o700);
  const trace = join(root, 'trace'), argsFile = join(root, 'args.json');
  const run = (extra = []) => spawnSync('bash', [join(repo, 'tools/dev/render-current-local.sh'), '--context', 'k3d-kodex', '--state-directory', state, '--expected-sha', sha, ...extra], {
    encoding: 'utf8', timeout: 10000,
    env: { PATH: `${bin}:${process.env.PATH}`, LANG: 'C.UTF-8', KUBECONFIG: kubeconfig, FIXTURE_KUBE: kubeFixture, FIXTURE_TRACE: trace, FIXTURE_ARGS: argsFile },
  });
  return { repo, state, data, stateFile, fingerprint, run, saveKube, put, argsFile, trace };
}

test('fresh private render uses exact pins/live endpoint, hides private logs and leaves state unchanged', t => {
  const f = fixture(t), before = readFileSync(f.stateFile, 'utf8');
  const result = f.run();
  assert.equal(result.status, 0, result.stderr);
  const args = JSON.parse(readFileSync(f.argsFile)), value = key => args[args.indexOf(key) + 1];
  assert.equal(value('--authority-source-revision'), '7');
  assert.equal(value('--kubernetes-endpoint-cidr'), '172.19.0.2/32');
  assert.equal(value('--runner-image'), `registry.local.kodex/kodex/agent-runner@sha256:${'a'.repeat(64)}`);
  assert.notEqual(value('--output'), join(f.state, 'render.yaml'));
  assert.equal(statSync(value('--output')).mode & 0o777, 0o600);
  assert.equal(readFileSync(f.stateFile, 'utf8'), before);
  assert.match(result.stdout, new RegExp(f.fingerprint));
  assert.doesNotMatch(result.stdout + result.stderr, /PRIVATE_|NOT_READ_BY_WRAPPER/);
  const calls = readFileSync(f.trace, 'utf8').trim().split('\n').map(JSON.parse);
  assert.ok(calls.every(call => call.includes('k3d-kodex') && !call.some(arg => ['apply', 'create', 'delete', 'patch', 'exec'].includes(arg))));
  assert.equal(f.run().status, 0);
  assert.notEqual(JSON.parse(readFileSync(f.argsFile))[args.indexOf('--output') + 1], value('--output'));
});
test('live generation drift increments current live revision rather than stale state', t => {
  const f = fixture(t); f.data.revision = 11; f.saveKube();
  const result = f.run(); assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Authority source revision: 12/);
});
test('trusted snapshot absence retains canonical uninitialized revision one', t => {
  const f = fixture(t); f.data.revision = null; f.saveKube();
  const result = f.run(); assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Authority source revision: 1/);
});
test('same live generation with changed source fingerprint increments', t => {
  const f = fixture(t); f.put(f.stateFile, JSON.stringify({ version: 1, sourceRevision: 7, sourceFingerprint: '0'.repeat(64) }));
  const result = f.run(); assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Authority source revision: 8/);
});
for (const negative of ['dirty', 'context', 'mount', 'frontend-mount', 'mode', 'pin', 'revision', 'overflow']) {
  test(`preflight rejects ${negative} before renderer`, t => {
    const f = fixture(t);
    let extra = [];
    if (negative === 'dirty') f.put(join(f.repo, 'untracked'), 'fixture');
    if (negative === 'context') extra = ['--context', 'other-cluster'];
    if (negative === 'mount') { f.data.workloads.items[0].spec.template.spec.volumes[0].hostPath.path = '/other-source'; f.saveKube(); }
    if (negative === 'frontend-mount') { f.data.workloads.items[2].spec.template.spec.volumes[0].hostPath.path = f.repo; f.saveKube(); }
    if (negative === 'mode') chmodSync(join(f.state, 'agent-runner-image'), 0o644);
    if (negative === 'pin') f.put(join(f.state, 'agent-runner-image'), 'registry.local.kodex/kodex/agent-runner:latest');
    if (negative === 'revision') { f.data.revision = -1; f.saveKube(); }
    if (negative === 'overflow') { f.data.revision = 9007199254740991; f.saveKube(); }
    const result = f.run(extra);
    assert.notEqual(result.status, 0);
    assert.throws(() => statSync(f.argsFile));
    assert.doesNotMatch(result.stdout + result.stderr, /PRIVATE_/);
  });
}
test('help explicitly states cache build/pull/install side effects and closed orchestration', () => {
  const result = spawnSync('bash', [script, '--help'], { encoding: 'utf8' });
  assert.equal(result.status, 0);
  assert.match(result.stdout, /cache misses may build, docker pull or npm ci/);
  assert.match(result.stdout, /No separate build, apply, bootstrap or authority state commit/);
  assert.doesNotMatch(readFileSync(script, 'utf8'), /source .*dev\.sh|build-local-|materialize-secrets|reconcile-local-material/);
});
