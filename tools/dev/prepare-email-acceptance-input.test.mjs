import test from 'node:test';
import assert from 'node:assert/strict';
import { rootCertificates } from 'node:tls';
import { chmodSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { prepareEmailInput } from './prepare-email-acceptance-input.mjs';
const makeEnv = (ca = 'fixture-ca') => ({ KODEX_QA_EMAIL_ADDRESS: 'sender@example.invalid', KODEX_QA_EMAIL_RECIPIENT: 'recipient@example.invalid', KODEX_QA_EMAIL_CA_PEM_PATH: ca, ...Object.fromEntries(['SMTP', 'IMAP', 'POP3'].flatMap((p) => Object.entries({ HOST: `${p.toLowerCase()}.example.invalid`, PORT: { SMTP: '465', IMAP: '993', POP3: '995' }[p], TLS_MODE: 'implicit', USERNAME: 'fixture-sensitive-username', PASSWORD: 'fixture-sensitive-password' }).map(([k, v]) => [`KODEX_QA_EMAIL_${p}_${k}`, v]))) });
for (const protocol of ['IMAP', 'POP3']) test(`root input ${protocol}: six exact slots, no credential values in profile, 21 explicit policies`, () => {
  const result = prepareEmailInput(makeEnv(), protocol, 'mvp1031-fixture', () => rootCertificates[0]);
  assert.equal(result.profile.specification.replyTo, result.profile.specification.sender); assert.equal(Object.keys(result.credentials).length, 6); assert.equal(result.profile.specification.policies.length, 21); assert.deepEqual(result.profile.specification.allowedFolders, ['INBOX']);
  assert(!JSON.stringify(result.profile).includes('fixture-sensitive')); assert.equal(result.profile.specification.policies.find((p) => p.operation === 'SEND').policy, 'HUMAN_GATE');
  assert.equal(result.profile.specification.policies.find((p) => p.operation === 'DELETE').policy, 'DENY');
});
test('empty credential, wrong TLS port, non-CA data and unrecognized protocol fail before output', () => {
  for (const change of [{ KODEX_QA_EMAIL_IMAP_PASSWORD: '' }, { KODEX_QA_EMAIL_SMTP_TLS_MODE: 'plaintext' }, { KODEX_QA_EMAIL_SMTP_PORT: '25' }, { KODEX_QA_EMAIL_IMAP_PASSWORD: 'line\nbreak' }]) assert.throws(() => prepareEmailInput({ ...makeEnv(), ...change }, 'IMAP', 'mvp1031-fixture', () => rootCertificates[0]));
  assert.throws(() => prepareEmailInput(makeEnv(), 'IMAP', 'mvp1031-fixture', () => `${rootCertificates[0]}private material`), /CA_BUNDLE_INVALID/);
  assert.throws(() => prepareEmailInput(makeEnv(), 'UNKNOWN', 'mvp1031-fixture', () => rootCertificates[0]), /PROTOCOL_INVALID/);
});
test('public root input CLI writes exclusive 0600 files and does not print addresses or credentials', (t) => {
  const dir = mkdtempSync(join(tmpdir(), 'emi-')); t.after(() => rmSync(dir, { recursive: true, force: true })); const ca = join(dir, 'ca.pem'); writeFileSync(ca, rootCertificates[0], { mode: 0o600 });
  const run = () => spawnSync(process.execPath, [new URL('./prepare-email-acceptance-input.mjs', import.meta.url).pathname, '--protocol', 'IMAP', '--prefix', 'mvp1031-fixture', '--directory', dir, '--confirm', 'PREPARE-STAGING-MAILBOX-INPUT'], { env: { ...process.env, ...makeEnv(ca) }, encoding: 'utf8' });
  let result = run(); assert.equal(result.status, 0, result.stderr); assert(!/fixture-sensitive|sender@example|recipient@example|BEGIN CERTIFICATE/.test(result.stdout + result.stderr));
  for (const file of ['profile.json', 'credentials.json']) assert.equal(statSync(join(dir, file)).mode & 0o077, 0);
  const old = readFileSync(join(dir, 'credentials.json')); result = run(); assert.notEqual(result.status, 0); assert.deepEqual(readFileSync(join(dir, 'credentials.json')), old);
  chmodSync(dir, 0o755); result = run(); assert.match(result.stderr, /PRIVATE_DIRECTORY_REQUIRED/);
});
