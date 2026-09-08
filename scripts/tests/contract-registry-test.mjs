// Реестр активных контрактов не может ссылаться на отсутствующие исходники или generated code.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../../', import.meta.url));
const registry = JSON.parse(execFileSync('yq', ['-o=json', '.', `${root}contracts/registry.yaml`], { timeout: 10000 }));
const tracked = execFileSync('git', ['ls-files', '-z'], { cwd: root, timeout: 10000 }).toString().split('\0').filter(Boolean);

function checkPaths(packages, files) {
  const ids = new Set();
  for (const entry of packages) {
    assert.equal(typeof entry.id, 'string', 'PACKAGE_ID_REQUIRED');
    assert.equal(ids.has(entry.id), false, `DUPLICATE_PACKAGE: ${entry.id}`);
    ids.add(entry.id);
    for (const [kind, path] of [['source', entry.source], ...Object.entries(entry.generated ?? {})]) {
      assert.equal(typeof path, 'string', `CONTRACT_PATH_REQUIRED: ${entry.id}/${kind}`);
      assert.equal(path.split('/').every(part => part && part !== '.' && part !== '..'), true, `CONTRACT_PATH_INVALID: ${entry.id}/${kind}`);
      assert.equal(files.some(file => file === path || file.startsWith(`${path}/`)), true, `CONTRACT_PATH_MISSING: ${entry.id}/${kind}`);
    }
  }
}

test('active registry source and generated paths contain tracked files', () => {
  assert.equal(registry.version, 1);
  assert.ok(registry.packages.length > 0);
  checkPaths(registry.packages, tracked);
});

test('missing source and generated paths fail independently; adjacent names do not match', () => {
  const entry = { id: 'fixture-v1', source: 'contracts/fixture/v1', generated: { go: 'libs/go/fixture/gen' } };
  const files = ['contracts/fixture/v1/api.proto', 'libs/go/fixture/gen/api.pb.go'];
  checkPaths([entry], files);
  assert.throws(() => checkPaths([entry], files.slice(1)), /CONTRACT_PATH_MISSING: fixture-v1\/source/);
  assert.throws(() => checkPaths([entry], files.slice(0, 1)), /CONTRACT_PATH_MISSING: fixture-v1\/go/);
  assert.throws(() => checkPaths([entry], ['contracts/fixture/v10/api.proto', files[1]]), /CONTRACT_PATH_MISSING/);
  assert.throws(() => checkPaths([entry, entry], files), /DUPLICATE_PACKAGE/);
});

test('integration worker consumes existing CP, package and email contracts', () => {
  for (const id of ['control-plane-v1', 'integration-package-v1', 'email-bridge-api-v1']) {
    const entry = registry.packages.find(value => value.id === id);
    assert.ok(entry?.consumers.includes('integration-gateway'), `INTEGRATION_CONSUMER_MISSING: ${id}`);
  }
  assert.equal(registry.packages.some(entry => entry.format === 'proto' && entry.owner === 'integration-gateway'), false);
});
