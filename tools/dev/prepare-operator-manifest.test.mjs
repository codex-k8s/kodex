import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { planOperatorEvidence, prepareOperatorManifest } from './prepare-operator-manifest.mjs';
import { fingerprint } from '../release/scoped-release.mjs';
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
function fixture(t) {
  const directory = mkdtempSync(join(tmpdir(), 'kodex-operator-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const source = join(directory, 'source.json'), output = join(directory, 'copy.json'), proof = join(directory, 'proof.json');
  const proofBytes = Buffer.from('{"status":"PASS"}'); writeFileSync(proof, proofBytes, { mode: 0o600 });
  const value = { version: 1, profile: 'component-revisions', clusterUID: 'cluster', namespace: 'kodex-system', namespaceUID: 'namespace',
    components: [{ name: 'fixture' }], compatibility: { version: 1, evidence: [{ path: proof, sha256: hash(proofBytes) }] } };
  value.manifestSHA256 = fingerprint(value);
  const bytes = JSON.stringify(value); writeFileSync(source, bytes, { mode: 0o600 });
  return { directory, source, output, bytes, value, proof, proofBytes, hash: hash(bytes) };
}

test('operator copy preserves exact bytes, verifies every proof, writes 0600 and refuses overwrite', t => {
  const f = fixture(t);
  const result = prepareOperatorManifest(f.source, f.hash, f.output);
  assert.equal(result.originalSHA256, f.hash); assert.equal(result.outputSHA256, f.hash);
  assert.equal(result.checkedEvidence, 1); assert.equal(result.copiedEvidence, 0);
  assert.equal(readFileSync(f.output, 'utf8'), f.bytes);
  assert.equal(statSync(f.output).mode & 0o777, 0o600);
  assert.throws(() => prepareOperatorManifest(f.source, f.hash, f.output));
});

test('root-owned proof is copied with exact bytes and explicit new compatibility identity', t => {
  const f = fixture(t);
  const plan = planOperatorEvidence(f.value, f.output, () => ({ bytes: f.proofBytes, owner: 0 }));
  assert.equal(plan.copies.length, 1);
  assert.deepEqual(plan.copies[0].bytes, f.proofBytes);
  assert.equal(plan.value.compatibility.evidence[0].sha256, hash(f.proofBytes));
  assert.equal(plan.value.compatibility.evidence[0].path, `${f.output}.evidence-0.json`);
  assert.notEqual(plan.value.manifestSHA256, f.value.manifestSHA256);
  assert.equal(f.value.compatibility.evidence[0].path, f.proof);
});

test('missing/mismatched evidence or identity never creates output', t => {
  const f = fixture(t);
  writeFileSync(f.proof, 'changed');
  assert.throws(() => prepareOperatorManifest(f.source, f.hash, f.output), /CHANGED/);
  assert.equal(existsSync(f.output), false);
  rmSync(f.proof);
  assert.throws(() => prepareOperatorManifest(f.source, f.hash, f.output));
  assert.equal(existsSync(f.output), false);
  assert.throws(() => planOperatorEvidence({ ...f.value, manifestSHA256: 'a'.repeat(64) }, f.output), /INVALID/);
});

test('wrong digest, public file, public output parent and symlink never create copy', t => {
  const f = fixture(t);
  assert.throws(() => prepareOperatorManifest(f.source, 'a'.repeat(64), f.output), /CHANGED/);
  chmodSync(f.source, 0o644);
  assert.throws(() => prepareOperatorManifest(f.source, f.hash, f.output), /PRIVATE_REQUIRED/);
  chmodSync(f.source, 0o600); chmodSync(f.directory, 0o755);
  assert.throws(() => prepareOperatorManifest(f.source, f.hash, f.output), /PRIVATE_REQUIRED/);
  chmodSync(f.directory, 0o700);
  const link = join(f.directory, 'link'); symlinkSync(f.source, link);
  assert.throws(() => prepareOperatorManifest(link, f.hash, f.output), /PATH_INVALID/);
  assert.equal(existsSync(f.output), false);
});
