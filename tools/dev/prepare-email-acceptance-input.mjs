#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { constants, closeSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { X509Certificate } from 'node:crypto';
import { validateEmailProfile } from './email-mailbox-acceptance.mjs';
const check = (ok, code) => { if (!ok) throw new Error(code); };
const hash = (bytes) => createHash('sha256').update(bytes).digest('hex');

// Запускает root: читаются только перечисленные env, никогда полный env dump.
// Начальный профиль разрешает HEALTH и bounded чтение; SEND требует Human Gate.
// Остальные effects остаются DENY до отдельной утверждённой forward revision.
export function prepareEmailInput(env, protocol, prefix, readCA = (path) => readFileSync(path, 'utf8')) {
  check(['IMAP', 'POP3'].includes(protocol), 'PROTOCOL_INVALID');
  const value = (name) => { const v = env[`KODEX_QA_EMAIL_${name}`]; check(typeof v === 'string' && v.length > 0 && v.length <= 65536 && !/[\x00]/.test(v), 'EMAIL_ENV_REQUIRED'); return v; };
  const ca = readCA(value('CA_PEM_PATH')); check(Buffer.byteLength(ca) <= 65536, 'CA_BUNDLE_INVALID');
  const certs = ca.match(/-----BEGIN CERTIFICATE-----[\s\S]+?-----END CERTIFICATE-----/g);
  check(certs?.length > 0 && ca.replace(/-----BEGIN CERTIFICATE-----[\s\S]+?-----END CERTIFICATE-----/g, '').trim() === '' && certs.length <= 32 && certs.every((pem) => new X509Certificate(pem).ca), 'CA_BUNDLE_INVALID');
  const credentials = {}; const endpoint = (kind, slot) => {
    const host = value(`${kind}_HOST`); const port = Number(value(`${kind}_PORT`)); const tlsMode = value(`${kind}_TLS_MODE`).toUpperCase();
    check(/^[a-z0-9.-]+$/.test(host) && Number.isInteger(port) && (tlsMode === 'IMPLICIT' && port === { SMTP: 465, IMAP: 993, POP3: 995 }[kind] || tlsMode === 'STARTTLS' && port === { SMTP: 587, IMAP: 143, POP3: 110 }[kind]), 'EMAIL_ENDPOINT_INVALID');
    const username = value(`${kind}_USERNAME`); const secret = value(`${kind}_PASSWORD`);
    check(Buffer.byteLength(username) <= 320 && Buffer.byteLength(secret) <= 4096 && !/[\r\n]/.test(username + secret), 'EMAIL_CREDENTIAL_INVALID');
    credentials[`${slot}-ca`] = ca; credentials[`${slot}-username`] = username; credentials[`${slot}-secret`] = secret;
    return { host, port, serverName: host, tlsMode, authMethod: 'PASSWORD' };
  };
  const all = ['HEALTH', 'MAILBOXES', 'LIST', 'SEARCH', 'FETCH', 'DOWNLOAD', 'SEND', 'REPLY', 'REPLY_ALL', 'FORWARD', 'DELETE', 'RECEIPT', 'THREAD', 'ATTACHMENTS', 'MARK_READ', 'MARK_UNREAD', 'MOVE', 'ARCHIVE', 'DRAFT_CREATE', 'DRAFT_UPDATE', 'DRAFT_DELETE'];
  const reads = new Set(['HEALTH', 'LIST', 'SEARCH', 'FETCH', 'RECEIPT', ...(protocol === 'IMAP' ? ['MAILBOXES', 'DOWNLOAD', 'THREAD', 'ATTACHMENTS'] : [])]);
  const profile = { prefix, specification: { enabled: true, receiveProtocol: protocol, sender: value('ADDRESS'), recipients: [value('RECIPIENT')], allowedFolders: ['INBOX'], folder: 'INBOX', helloName: 'kodex.works', smtp: endpoint('SMTP', 'smtp'), [protocol === 'IMAP' ? 'imap' : 'pop']: endpoint(protocol, protocol === 'IMAP' ? 'imap' : 'pop'), limits: { attachmentBytes: 1048576, maxAttachments: 1, maxRecipients: 1, messageBytes: 2097152, pageSize: 5, scanMessages: 20, timeoutSeconds: 30 }, policies: all.map((operation) => ({ operation, policy: reads.has(operation) ? 'ALLOW' : operation === 'SEND' ? 'HUMAN_GATE' : 'DENY', folders: ['INBOX'] })) } };
  validateEmailProfile(profile);
  return { profile, credentials, caSHA256: hash(ca), caCertificates: certs.length };
}
function main() {
  const args = process.argv.slice(2); const options = {};
  while (args.length) { const key = args.shift(); check(['--protocol', '--prefix', '--directory', '--confirm'].includes(key) && args.length && !(key in options), 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  check(options['--confirm'] === 'PREPARE-STAGING-MAILBOX-INPUT', 'CONFIRMATION_INVALID');
  check(options['--directory'], 'PRIVATE_DIRECTORY_REQUIRED'); const directory = resolve(options['--directory']); const info = lstatSync(directory);
  check(info.isDirectory() && !info.isSymbolicLink() && realpathSync(directory) === directory && (info.mode & 0o077) === 0, 'PRIVATE_DIRECTORY_REQUIRED');
  const input = prepareEmailInput(process.env, options['--protocol'], options['--prefix']);
  const write = (name, value) => { const fd = openSync(join(directory, name), constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600); try { writeFileSync(fd, `${JSON.stringify(value)}\n`); fsyncSync(fd); } finally { closeSync(fd); } };
  write('profile.json', input.profile); write('credentials.json', input.credentials);
  const fd = openSync(directory, constants.O_RDONLY); try { fsyncSync(fd); } finally { closeSync(fd); }
  process.stdout.write(`${JSON.stringify({ status: 'PREPARED', protocol: options['--protocol'], credentials: 6, caSHA256: input.caSHA256, caCertificates: input.caCertificates, mutations: 'NOT_RUN', messageEffects: 'NOT_RUN' })}\n`);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) { try { main(); } catch (error) { const code = /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'EMAIL_INPUT_PREPARATION_FAILED'; process.stderr.write(`${JSON.stringify({ status: 'FAIL', code })}\n`); process.exitCode = 1; } }
