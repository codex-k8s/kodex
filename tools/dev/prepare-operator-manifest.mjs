#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { closeSync, constants, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { fingerprint } from '../release/scoped-release.mjs';

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const identity = value => Object.fromEntries(['version', 'profile', 'clusterUID', 'namespace', 'namespaceUID', 'components', 'compatibility'].map(key => [key, value[key]]));

function readPrivateInput(source, sha256) {
  const uid = process.getuid();
  if (typeof source !== 'string' || realpathSync(source) !== source || !/^[a-f0-9]{64}$/.test(sha256 ?? '')) throw new Error('OPERATOR_MANIFEST_PATH_INVALID');
  const before = lstatSync(source);
  if (!before.isFile() || ![0, uid].includes(before.uid) || (before.mode & 0o077) !== 0 || before.size > (8 << 20)) throw new Error('OPERATOR_MANIFEST_PRIVATE_REQUIRED');
  // sudo читает только exact regular input; payload остаётся в памяти.
  const bytes = before.uid === uid ? readFileSync(source) : execFileSync('sudo', ['-n', 'cat', '--', source], { timeout: 10000, maxBuffer: 8 << 20, stdio: ['ignore', 'pipe', 'pipe'] });
  const after = lstatSync(source);
  if (before.dev !== after.dev || before.ino !== after.ino || before.size !== after.size || before.mtimeMs !== after.mtimeMs || before.ctimeMs !== after.ctimeMs || hash(bytes) !== sha256) throw new Error('OPERATOR_MANIFEST_CHANGED');
  return { bytes, owner: before.uid };
}

// Сначала проверяется весь набор. Ошибка любого proof не создаёт ни копий,
// ни нового manifest. Изменение path явно меняет compatibility identity.
export function planOperatorEvidence(input, output, read = readPrivateInput) {
  const value = structuredClone(input), copies = [];
  if (value.version !== 1 || value.profile !== 'component-revisions' || !Array.isArray(value.components) || !value.components.length ||
    value.compatibility?.version !== 1 || !Array.isArray(value.compatibility.evidence) || !value.compatibility.evidence.length || value.compatibility.evidence.length > 256 ||
    value.manifestSHA256 !== fingerprint(identity(value))) throw new Error('OPERATOR_MANIFEST_INVALID');
  let total = 0;
  for (const [index, evidence] of value.compatibility.evidence.entries()) {
    if (Object.keys(evidence).sort().join() !== 'path,sha256') throw new Error('OPERATOR_MANIFEST_EVIDENCE_INVALID');
    const { bytes, owner } = read(evidence.path, evidence.sha256);
    total += bytes.length;
    if (total > (64 << 20) || hash(bytes) !== evidence.sha256) throw new Error('OPERATOR_MANIFEST_EVIDENCE_INVALID');
    if (owner !== process.getuid()) {
      if (owner !== 0) throw new Error('OPERATOR_MANIFEST_PRIVATE_REQUIRED');
      evidence.path = `${output}.evidence-${index}.json`;
      copies.push({ path: evidence.path, bytes });
    }
  }
  value.manifestSHA256 = fingerprint(identity(value));
  return { value, copies };
}

function writePrivateNew(output, bytes) {
  const descriptor = openSync(output, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    const stat = fstatSync(descriptor);
    if (stat.uid !== process.getuid() || (stat.mode & 0o077) !== 0) throw new Error('OPERATOR_MANIFEST_OUTPUT_INVALID');
    writeFileSync(descriptor, bytes); fsyncSync(descriptor);
  } finally { closeSync(descriptor); }
}

export function prepareOperatorManifest(source, sha256, output) {
  const uid = process.getuid();
  if (uid === 0 || !/^[a-f0-9]{64}$/.test(sha256 ?? '')) throw new Error('OPERATOR_MANIFEST_ARGUMENTS_INVALID');
  if (realpathSync(dirname(output)) !== dirname(output) || resolve(output) !== output) throw new Error('OPERATOR_MANIFEST_PATH_INVALID');
  const parent = lstatSync(dirname(output));
  if (!parent.isDirectory() || parent.uid !== uid || (parent.mode & 0o077) !== 0) throw new Error('OPERATOR_MANIFEST_PRIVATE_REQUIRED');
  try { lstatSync(output); throw new Error('OPERATOR_MANIFEST_OUTPUT_EXISTS'); }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
  const { bytes } = readPrivateInput(source, sha256), original = JSON.parse(bytes);
  const { value, copies } = planOperatorEvidence(original, output);
  for (const copy of copies) {
    try { lstatSync(copy.path); throw new Error('OPERATOR_MANIFEST_OUTPUT_EXISTS'); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  const outputBytes = copies.length ? Buffer.from(`${JSON.stringify(value, null, 2)}\n`) : bytes;
  for (const copy of copies) writePrivateNew(copy.path, copy.bytes);
  writePrivateNew(output, outputBytes);
  return { status: 'PREPARED', originalSHA256: sha256, outputSHA256: hash(outputBytes),
    originalManifestIdentity: original.manifestSHA256, outputManifestIdentity: value.manifestSHA256,
    checkedEvidence: value.compatibility.evidence.length, copiedEvidence: copies.length };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2);
    if (args.length !== 6 || args[0] !== '--source' || args[2] !== '--sha256' || args[4] !== '--output') throw new Error('OPERATOR_MANIFEST_ARGUMENTS_INVALID');
    process.stdout.write(`${JSON.stringify(prepareOperatorManifest(args[1], args[3], args[5]))}\n`);
  } catch (error) {
    const code = /^OPERATOR_MANIFEST_[A-Z_]+$/.test(error.message) ? error.message : 'OPERATOR_MANIFEST_FAILED';
    process.stderr.write(`Operator manifest preparation failed: ${code}\n`); process.exitCode = 1;
  }
}
