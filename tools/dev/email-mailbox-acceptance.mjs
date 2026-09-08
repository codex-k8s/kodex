#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { constants, closeSync, existsSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { privateJournal, mutationDriver } from './role-image-acceptance.mjs';
import { createOwnerSessionClient } from './owner-session-client.mjs';
import { exactOrigin } from './owner-session-storage.mjs';
import { boundedResponseBody } from './runtime-workspace-acceptance.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const check = (ok, code) => { if (!ok) throw new Error(code); };
const hash = (value) => createHash('sha256').update(typeof value === 'string' || Buffer.isBuffer(value) ? value : JSON.stringify(value)).digest('hex');
const ref = (v) => { check(typeof v === 'string' && /^[A-Za-z0-9_-]{1,160}$/.test(v), 'REFERENCE_INVALID'); return v; };
const positive = (v) => { check(Number.isSafeInteger(v) && v > 0, 'VERSION_INVALID'); return v; };
const digest = (v) => { check(/^[a-f0-9]{64}$/.test(v ?? ''), 'DIGEST_INVALID'); return v; };
const enc = encodeURIComponent;
const saved = (journal, step) => journal.events.findLast((e) => e.step === step && ['ACK', 'CHECKPOINT'].includes(e.type))?.result;
const checkpoint = (journal, step, result) => { journal.append({ type: 'CHECKPOINT', step, result }); return result; };
const operations = ['HEALTH', 'MAILBOXES', 'LIST', 'SEARCH', 'FETCH', 'DOWNLOAD', 'SEND', 'REPLY', 'REPLY_ALL', 'FORWARD', 'DELETE', 'RECEIPT', 'THREAD', 'ATTACHMENTS', 'MARK_READ', 'MARK_UNREAD', 'MOVE', 'ARCHIVE', 'DRAFT_CREATE', 'DRAFT_UPDATE', 'DRAFT_DELETE'];
const kinds = { ca: 'CA_CERTIFICATE', username: 'USERNAME', secret: 'AUTH_SECRET' };
const definition = JSON.parse(readFileSync(new URL('../../contracts/integrations/v1/definitions/email.yaml', import.meta.url)));
const phases = ['recover-invalid', 'plan', 'inspect', 'connection', 'credentials', 'publish', 'bind', 'readback', 'health', 'grants', 'receipt'];
const configurationPhases = ['connection', 'credentials', 'publish', 'bind', 'grants'];

export function validateEmailProfile(profile) {
  check(profile && Object.keys(profile).every((k) => ['prefix', 'specification'].includes(k)) && /^mvp[a-z0-9-]{4,56}$/.test(profile.prefix ?? ''), 'PROFILE_INVALID');
  const spec = profile.specification;
  check(spec?.enabled === true && ['IMAP', 'POP3'].includes(spec.receiveProtocol) && typeof spec.sender === 'string' && spec.sender.length > 0 && Array.isArray(spec.recipients) && spec.recipients.length > 0 && spec.recipients.length <= 10, 'PROFILE_INVALID');
  check(Array.isArray(spec.allowedFolders) && spec.allowedFolders.length > 0 && spec.allowedFolders.includes(spec.folder), 'EXPLICIT_FOLDER_REQUIRED');
  check(spec.receiveProtocol !== 'POP3' || spec.folder === 'INBOX' && spec.allowedFolders.length === 1 && !spec.imap && !spec.archiveFolder && !spec.draftsFolder, 'POP3_PROFILE_INVALID');
  check(Array.isArray(spec.policies) && spec.policies.length === operations.length && new Set(spec.policies.map((p) => p.operation)).size === operations.length && spec.policies.every((p) => operations.includes(p.operation) && ['ALLOW', 'DENY', 'HUMAN_GATE'].includes(p.policy) && Array.isArray(p.folders) && p.folders.every((f) => spec.allowedFolders.includes(f))), 'EXPLICIT_POLICY_REQUIRED');
  check(spec.policies.find((p) => p.operation === 'HEALTH')?.policy === 'ALLOW', 'HEALTH_POLICY_REQUIRED');
  const protocols = ['smtp', spec.receiveProtocol === 'IMAP' ? 'imap' : 'pop'];
  check(!spec[spec.receiveProtocol === 'IMAP' ? 'pop' : 'imap'], 'MIXED_RECEIVE_PROFILE_FORBIDDEN');
  for (const protocol of protocols) {
    const endpoint = spec[protocol];
    check(endpoint && typeof endpoint.host === 'string' && endpoint.host === endpoint.serverName && ['IMPLICIT', 'STARTTLS'].includes(endpoint.tlsMode) && endpoint.authMethod === 'PASSWORD' && Object.keys(kinds).every((field) => endpoint[field] === undefined), 'ENDPOINT_PROFILE_INVALID');
  }
  check(spec.limits && Number.isInteger(spec.limits.timeoutSeconds) && spec.limits.timeoutSeconds >= 1 && spec.limits.timeoutSeconds <= 60, 'LIMITS_REQUIRED');
  return protocols.flatMap((protocol) => Object.entries(kinds).map(([field, kind]) => ({ slot: `${protocol}-${field}`, protocol, field, kind })));
}
function connectionPin(body, expected) {
  check(body?.definitionKey === 'email' && (!expected || body.ref === expected), 'CONNECTION_SCOPE_MISMATCH');
  return { connectionRef: ref(body.ref), connectionVersion: positive(body.version) };
}
function mailboxPin(body, connectionRef) {
  check(body?.connectionRef === connectionRef && body.configuration?.kind === 'EMAIL_MAILBOX' && body.configuration.managedBy === 'UI', 'MAILBOX_SCOPE_MISMATCH');
  check(['DRAFT', 'VALID', 'INVALID', 'PUBLISHED'].includes(body.revision?.state), 'REVISION_STATE_INVALID');
  const result = { connectionRef, mailboxRef: ref(body.mailboxRef), connectionVersion: positive(body.connectionVersion), configurationRef: ref(body.configuration.ref), configurationVersion: positive(body.configuration.version), revisionRef: ref(body.revision.ref), revisionDigest: digest(body.revision.digest), state: body.revision.state };
  if (body.publication) {
    const p = body.publication;
    check(p.configurationRevisionRef === result.revisionRef && ['PENDING', 'READY', 'FAILED', 'SUPERSEDED'].includes(p.state), 'PUBLICATION_SCOPE_MISMATCH');
    result.publication = { ref: ref(p.ref), revision: positive(p.revision), digest: digest(p.digest), state: p.state };
  }
  return result;
}

// Только owner API. HEALTH — отдельный effect gate; SMTP/IMAP операции выполняет
// настоящий Runtime/MCP, а этот driver лишь читает его типизированный receipt.
export async function emailAcceptance({ phase, profile, credentials, grantInput, recovery, journal, get, request, preflight, invocationRef, projectRef, timeoutMs = 1200000, now = Date.now, sleep = (ms) => new Promise((done) => setTimeout(done, ms)) }) {
  const slots = validateEmailProfile(profile); await preflight();
  check(!journal.events[0]?.predecessorSHA256 || !['connection', 'credentials'].includes(phase), 'RECOVERY_CREATE_FORBIDDEN');
  if (phase === 'recover-invalid') return recoverInvalidMailbox({ recovery, profile, journal, get, request });
  if (phase === 'plan') return checkpoint(journal, 'plan', { status: 'PLANNED', protocol: profile.specification.receiveProtocol, credentialSlots: slots.map((s) => s.slot), operations: profile.specification.policies.map((p) => ({ operation: p.operation, policy: p.policy })), providerEffect: 'NOT_RUN' });
  const mutate = mutationDriver(journal, request);
  if (phase === 'connection') {
    return mutate('connection', 'POST', '/api/v1/integration-connections', { definitionKey: 'email', name: profile.prefix, publicConfiguration: { base_url: definition.spec.configurationFields.find((f) => f.key === 'base_url').allowedValues[0], from_address: profile.specification.sender, mailbox_id: profile.prefix } }, 201, undefined, (body) => connectionPin(body));
  }
  if (phase === 'inspect') {
    const found = []; const seen = new Set(); let cursor;
    do {
      const page = await get(`/api/v1/integration-connections?definitionKey=email&query=${enc(profile.prefix)}&pageSize=30${cursor ? `&pageToken=${enc(cursor)}` : ''}`);
      check(Array.isArray(page.items) && page.items.length <= 30 && seen.size < 30, 'INSPECT_PAGE_INVALID');
      for (const value of page.items) if (value.name === profile.prefix) found.push(connectionPin(value));
      cursor = page.nextPageToken; check(!cursor || !seen.has(cursor), 'INSPECT_CURSOR_REPEATED'); if (cursor) seen.add(cursor);
    } while (cursor);
    const unresolved = journal.events.filter((e) => e.type === 'INTENT' && !journal.events.some((a) => a.type === 'ACK' && a.key === e.key));
    const receipts = [];
    for (const intent of unresolved) {
      if (intent.method !== 'PUT' || !intent.path.endsWith('/email-mailbox/credential')) continue;
      const response = await request(`${intent.path}-receipt?idempotencyKey=${enc(intent.key)}`, { method: 'GET', headers: { Accept: 'application/json' } });
      check([200, 404].includes(response.status), 'CREDENTIAL_RECEIPT_READ_FAILED');
      if (response.status === 404) { await response.body?.cancel(); receipts.push({ step: intent.step, status: 'NOT_FOUND' }); continue; }
      const value = JSON.parse((await boundedResponseBody(response, 2 << 20)).toString('utf8'));
      check(value.connectionRef === saved(journal, 'connection')?.connectionRef, 'CREDENTIAL_SCOPE_MISMATCH');
      receipts.push({ step: intent.step, status: 'FOUND', name: ref(value.name), generation: positive(value.generation), connectionVersion: positive(value.connectionVersion) });
    }
    return { status: 'OBSERVED', connections: found, unresolvedIntents: unresolved.length, credentialReceipts: receipts, replay: 'FORBIDDEN', providerEffect: 'NOT_RUN' };
  }
  const connection = saved(journal, 'connection'); check(connection, 'CONNECTION_ACK_REQUIRED');
  const path = `/api/v1/integration-connections/${enc(connection.connectionRef)}`;
  const currentConnection = async () => { const body = await get(path); connectionPin(body, connection.connectionRef); return body; };
  if (phase === 'credentials') {
    check(credentials && Object.keys(credentials).length === slots.length && slots.every((s) => typeof credentials[s.slot] === 'string' && credentials[s.slot].length > 0), 'CREDENTIAL_INPUT_INVALID');
    for (const slot of slots) {
      const body = { kind: slot.kind, value: credentials[slot.slot] };
      const current = saved(journal, slot.slot) ? undefined : await currentConnection();
      await mutate(slot.slot, 'PUT', `${path}/email-mailbox/credential`, body, 200, current?.version, (value) => {
        check(value.connectionRef === connection.connectionRef && value.kind === slot.kind, 'CREDENTIAL_SCOPE_MISMATCH');
        return { name: ref(value.name), generation: positive(value.generation), kind: slot.kind, connectionRef: connection.connectionRef, connectionVersion: positive(value.connectionVersion) };
      });
    }
    return { status: 'ACKNOWLEDGED', credentials: slots.length };
  }
  const readMailbox = async () => {
    const pin = saved(journal, 'draft'); check(pin, 'DRAFT_ACK_REQUIRED');
    const current = mailboxPin(await get(`${path}/email-mailbox/configuration?configurationRef=${enc(pin.configurationRef)}&revisionRef=${enc(pin.revisionRef)}`), connection.connectionRef);
    check(current.configurationRef === pin.configurationRef && current.revisionRef === pin.revisionRef && current.mailboxRef === pin.mailboxRef && current.revisionDigest === pin.revisionDigest, 'MAILBOX_REVISION_CHANGED'); return current;
  };
  const revisionPath = (pin) => `/api/v1/email-mailbox-configurations/${enc(pin.configurationRef)}/revisions/${enc(pin.revisionRef)}`;
  if (phase === 'publish') {
    const specification = structuredClone(profile.specification);
    for (const slot of slots) { const descriptor = saved(journal, slot.slot); check(descriptor, 'CREDENTIAL_ACK_REQUIRED'); specification[slot.protocol][slot.field] = { name: descriptor.name, generation: descriptor.generation }; }
    if (!saved(journal, 'draft')) await mutate('draft', 'POST', `${path}/email-mailbox/drafts`, { name: profile.prefix, content: { specification } }, 201, undefined, (body) => mailboxPin(body, connection.connectionRef));
    for (const [step, suffix, expected] of [['validate', 'validation', 'VALID'], ['publish', 'publication', 'PUBLISHED']]) {
      const previous = saved(journal, step); if (previous) { check(previous.state === expected, `MAILBOX_${step.toUpperCase()}_FAILED`); continue; }
      const pin = await readMailbox();
      const receipt = await mutate(step, 'POST', `${revisionPath(pin)}/${suffix}`, undefined, 200, pin.configurationVersion, (body) => mailboxPin(body, connection.connectionRef));
      check(receipt.state === expected, `MAILBOX_${step.toUpperCase()}_FAILED`);
    }
    const pin = await readMailbox(); check(pin.state === 'PUBLISHED', 'PUBLISHED_READBACK_FAILED');
    return checkpoint(journal, 'published', { status: 'PASS', ...pin, delivery: pin.publication?.state ?? 'NOT_BOUND', providerEffect: 'NOT_RUN' });
  }
  if (phase === 'bind') {
    if (saved(journal, 'bind')) return saved(journal, 'bind');
    check(saved(journal, 'publish'), 'PUBLISH_ACK_REQUIRED');
    const pin = await readMailbox(); const current = await currentConnection();
    check(pin.state === 'PUBLISHED' && pin.connectionVersion === current.version, 'BINDING_VERSION_CHANGED');
    return mutate('bind', 'POST', `${revisionPath(pin)}/binding`, { connectionRef: connection.connectionRef, expectedConnectionVersion: current.version }, 200, pin.configurationVersion, (body) => { const result = mailboxPin(body, connection.connectionRef); check(result.publication, 'PUBLICATION_REQUIRED'); return result; });
  }
  const bound = saved(journal, 'bind'); check(bound?.publication, 'BIND_ACK_REQUIRED');
  const publication = async () => {
    const pin = await readMailbox(); check(pin.publication?.ref === bound.publication.ref && pin.publication.revision === bound.publication.revision && pin.publication.digest === bound.publication.digest && pin.revisionDigest === bound.revisionDigest, 'PUBLICATION_CHANGED');
    check(!['FAILED', 'SUPERSEDED'].includes(pin.publication.state), 'PUBLICATION_FAILED'); return pin;
  };
  if (phase === 'readback') {
    const deadline = now() + timeoutMs;
    for (;;) {
      check(now() < deadline, 'PUBLICATION_READBACK_DEADLINE'); const pin = await publication();
      if (pin.publication.state === 'READY') return checkpoint(journal, 'ready', { status: 'PASS', ...pin, providerEffect: 'NOT_RUN', perPodReadback: 'NOT_RUN' });
      await sleep(1000);
    }
  }
  if (phase === 'receipt') {
    ref(invocationRef); ref(projectRef);
  const value = (await get(`/api/v1/integration-invocations/${enc(invocationRef)}/email-effect-receipt`)).receipt;
  check(value?.invocationRef === invocationRef && value.connectionRef === connection.connectionRef && value.projectRef === projectRef && value.mailboxRef === bound.mailboxRef && value.configurationRevision === bound.publication.revision, 'EFFECT_RECEIPT_SCOPE_MISMATCH');
  check(['UNKNOWN_OUTCOME', 'EFFECT_CONFIRMED', 'NO_EFFECT_CONFIRMED'].includes(value.outcome), 'EFFECT_OUTCOME_INVALID');
  return checkpoint(journal, `receipt-${invocationRef}`, { status: value.outcome === 'EFFECT_CONFIRMED' ? 'PASS' : 'NOT_PASS', receiptRef: ref(value.ref), receiptVersion: positive(value.version), invocationRef, outcome: value.outcome, externalReceiptDigest: digest(value.externalReceiptDigest), semanticInputDigest: digest(value.semanticInputDigest), delivery: 'NOT_PROVEN' });
  }
  check((await publication()).publication.state === 'READY', 'PUBLICATION_NOT_READY');
  if (phase === 'health') {
    if (!saved(journal, 'health')) {
      const current = await currentConnection();
      await mutate('health', 'POST', `${path}/commands`, { action: 'TEST' }, 200, current.version, (body) => connectionPin(body, connection.connectionRef));
    }
    const deadline = now() + timeoutMs;
    for (;;) {
      check(now() < deadline, 'HEALTH_READBACK_DEADLINE'); const current = await currentConnection();
      if (current.state === 'TESTING') { await sleep(1000); continue; }
      check(current.state === 'CONNECTED' && current.version >= saved(journal, 'health').connectionVersion, 'MAILBOX_HEALTH_FAILED');
      return checkpoint(journal, 'healthy', { status: 'PASS', ...connectionPin(current, connection.connectionRef), providerEffect: 'HEALTH_ONLY', messageEffects: 'NOT_RUN' });
    }
  }
  if (phase === 'grants') {
    check(grantInput && Object.keys(grantInput).every((key) => ['agentRef', 'projectRef', 'capabilities'].includes(key)) && Array.isArray(grantInput.capabilities) && grantInput.capabilities.length > 0 && grantInput.capabilities.every((key) => definition.spec.capabilities.some((c) => c.key === key)) && new Set(grantInput.capabilities).size === grantInput.capabilities.length && saved(journal, 'healthy'), 'HEALTHY_RECIPIENT_REQUIRED');
    ref(grantInput.agentRef); ref(grantInput.projectRef);
    const priorPlan = saved(journal, 'grant-plan'); check(!priorPlan || priorPlan.digest === hash(grantInput), 'GRANT_PLAN_CHANGED');
    if (!priorPlan) checkpoint(journal, 'grant-plan', { digest: hash(grantInput) });
    const agent = await get(`/api/v1/agents/${enc(grantInput.agentRef)}`);
    check(agent.ref === grantInput.agentRef && agent.projectRef === grantInput.projectRef, 'RECIPIENT_SCOPE_MISMATCH');
    for (const capabilityKey of grantInput.capabilities) {
      const step = `grant-${capabilityKey}`; if (saved(journal, step)) continue;
      const current = await currentConnection(); check(current.state === 'CONNECTED', 'CONNECTION_NOT_READY');
      await mutate(step, 'POST', `${path}/grants`, { capabilityKey, agentRef: agent.ref, enabled: true }, 200, current.version, (body) => {
        const pin = connectionPin(body, connection.connectionRef); const grant = body.grants?.find((g) => g.capabilityKey === capabilityKey && g.agentRef === agent.ref && g.enabled);
        check(grant, 'GRANT_READBACK_MISMATCH'); return { ...pin, grantRef: ref(grant.ref), grantVersion: positive(grant.version), capabilityKey, agentRef: agent.ref };
      });
    }
    return { status: 'ACKNOWLEDGED', grants: grantInput.capabilities.length, providerEffect: 'NOT_RUN' };
  }
  throw new Error('PHASE_INVALID');
}

// Узкий перенос только terminal INVALID draft с девятью подтверждёнными командами.
// Старый journal/profile остаются неизменными; импорт ниже — CHECKPOINT, не новый ACK.
export function emailRecoveryPredecessor(path, expectedDigest, profileBytes, origin) {
  const previous = privateJournal(path, { version: 1, kind: 'EMAIL_MAILBOX_ACCEPTANCE', origin }, { readOnly: true });
  try {
    check(previous.bytesSHA256 === digest(expectedDigest), 'PREDECESSOR_DIGEST_MISMATCH');
    const header = previous.events[0]; const profile = JSON.parse(profileBytes); const slots = validateEmailProfile(profile);
    check(header.profileSHA256 === hash(profileBytes) && /^[a-f0-9]{40}$/.test(header.sourceSHA ?? '') && !profile.specification.replyTo, 'PREDECESSOR_PROFILE_MISMATCH');
    const intents = previous.events.filter((e) => e.type === 'INTENT'); const acks = previous.events.filter((e) => e.type === 'ACK');
    const steps = ['connection', ...slots.map((slot) => slot.slot), 'draft', 'validate'];
    check(intents.length === 9 && acks.length === 9 && new Set(intents.map((e) => e.step)).size === 9 && intents.every((e) => steps.includes(e.step) && /^[a-f0-9-]{36}$/.test(e.key ?? '') && /^[a-f0-9]{64}$/.test(e.bodySHA256 ?? '') && acks.filter((a) => a.key === e.key && a.step === e.step).length === 1) && !previous.events.some((e) => e.type === 'STOP'), 'PREDECESSOR_NOT_COMPLETE_INVALID_DRAFT');
    const connection = saved(previous, 'connection'); const invalid = saved(previous, 'validate'); const draft = saved(previous, 'draft');
    ref(connection?.connectionRef); positive(connection.connectionVersion);
    check(invalid?.state === 'INVALID' && draft?.state === 'DRAFT' && invalid.configurationRef === draft.configurationRef && invalid.revisionRef === draft.revisionRef && invalid.revisionDigest === draft.revisionDigest && invalid.connectionRef === connection.connectionRef && !invalid.publication, 'PREDECESSOR_REVISION_INVALID');
    const receipts = slots.map((slot) => {
      const receipt = saved(previous, slot.slot); const intent = intents.find((e) => e.step === slot.slot);
      check(receipt?.kind === slot.kind && receipt.connectionRef === connection.connectionRef && intent.method === 'PUT' && intent.path === `/api/v1/integration-connections/${enc(connection.connectionRef)}/email-mailbox/credential`, 'PREDECESSOR_CREDENTIAL_INVALID');
      ref(receipt.name); positive(receipt.generation); positive(receipt.connectionVersion);
      return { ...slot, receipt, key: intent.key };
    });
    const expectedSpec = structuredClone(profile.specification);
    for (const slot of receipts) expectedSpec[slot.protocol][slot.field] = { name: slot.receipt.name, generation: slot.receipt.generation };
    const draftIntent = intents.find((e) => e.step === 'draft'); const validateIntent = intents.find((e) => e.step === 'validate');
    check(draftIntent.method === 'POST' && draftIntent.path === `/api/v1/integration-connections/${enc(connection.connectionRef)}/email-mailbox/drafts` && draftIntent.bodySHA256 === hash({ name: profile.prefix, content: { specification: expectedSpec } }) && validateIntent.method === 'POST' && validateIntent.path === `/api/v1/email-mailbox-configurations/${enc(invalid.configurationRef)}/revisions/${enc(invalid.revisionRef)}/validation` && validateIntent.bodySHA256 === hash(null), 'PREDECESSOR_COMMAND_CHANGED');
    profile.specification.replyTo = profile.specification.sender;
    return { sha256: expectedDigest, sourceSHA: header.sourceSHA, profileSHA256: header.profileSHA256, manifestSHA256: digest(header.servingManifestSHA256), profile, connection, invalid, receipts };
  } finally { previous.close(); }
}
async function recoverInvalidMailbox({ recovery, profile, journal, get, request }) {
  check(recovery && journal.events[0].predecessorSHA256 === recovery.sha256, 'RECOVERY_PREDECESSOR_REQUIRED');
  const acknowledged = saved(journal, 'recover-draft');
  if (acknowledged) { if (!saved(journal, 'draft')) checkpoint(journal, 'draft', acknowledged); return { status: 'ACKNOWLEDGED', ...acknowledged }; }
  const { connection, invalid, receipts } = recovery; const path = `/api/v1/integration-connections/${enc(connection.connectionRef)}`;
  const current = await get(path); connectionPin(current, connection.connectionRef);
  check(current.version === receipts.at(-1).receipt.connectionVersion, 'PREDECESSOR_CONNECTION_CHANGED');
  const view = await get(`${path}/email-mailbox/configuration?configurationRef=${enc(invalid.configurationRef)}&revisionRef=${enc(invalid.revisionRef)}`); const pin = mailboxPin(view, connection.connectionRef);
  check(pin.configurationRef === invalid.configurationRef && pin.configurationVersion === invalid.configurationVersion && pin.revisionRef === invalid.revisionRef && pin.revisionDigest === invalid.revisionDigest && pin.state === 'INVALID' && !pin.publication && !view.boundRevisionRef && !view.configuration.currentRevision, 'PREDECESSOR_REVISION_CHANGED');
  for (const slot of receipts) {
    const value = await get(`${path}/email-mailbox/credential-receipt?idempotencyKey=${enc(slot.key)}`);
    check(value.connectionRef === slot.receipt.connectionRef && value.connectionVersion === slot.receipt.connectionVersion && value.kind === slot.kind && value.name === slot.receipt.name && value.generation === slot.receipt.generation, 'PREDECESSOR_CREDENTIAL_CHANGED');
  }
  if (!saved(journal, 'connection')) checkpoint(journal, 'connection', connection);
  for (const slot of receipts) if (!saved(journal, slot.slot)) checkpoint(journal, slot.slot, slot.receipt);
  const specification = structuredClone(profile.specification);
  for (const slot of receipts) specification[slot.protocol][slot.field] = { name: slot.receipt.name, generation: slot.receipt.generation };
  const result = await mutationDriver(journal, request)('recover-draft', 'POST', `/api/v1/email-mailbox-configurations/${enc(invalid.configurationRef)}/revisions/${enc(invalid.revisionRef)}/saves`, { specification }, 200, invalid.configurationVersion, (body) => {
    const value = mailboxPin(body, connection.connectionRef);
    check(value.configurationRef === invalid.configurationRef && value.configurationVersion > invalid.configurationVersion && value.revisionRef !== invalid.revisionRef && value.state === 'DRAFT' && body.revision.parentRevisionRef === invalid.revisionRef && !value.publication, 'RECOVERY_FORWARD_REVISION_INVALID');
    return value;
  });
  checkpoint(journal, 'draft', result); return { status: 'ACKNOWLEDGED', ...result, providerEffect: 'NOT_RUN' };
}

function privateInput(path) {
  check(path, 'PRIVATE_INPUT_REQUIRED'); const info = lstatSync(path);
  check(info.isFile() && !info.isSymbolicLink() && info.nlink === 1 && (info.mode & 0o077) === 0 && info.size > 0 && info.size <= (2 << 20), 'PRIVATE_INPUT_INVALID'); return readFileSync(path);
}
async function main() {
  const args = process.argv.slice(2); const phase = args.shift(); const options = {};
  while (args.length) { const key = args.shift(); check(/^--[a-z][a-z0-9-]*$/.test(key ?? '') && args.length && !(key in options), 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  check(phases.includes(phase), 'PHASE_INVALID');
  check(Object.keys(options).every((k) => ['--origin', '--storage-state', '--state', '--profile', '--previous-state', '--previous-sha256', '--previous-profile', '--credentials', '--grant-input', '--serving-manifest', '--timeout-ms', '--confirm', '--invocation-ref', '--project-ref'].includes(k)), 'ARGUMENT_UNKNOWN');
  check(options['--confirm'] === (phase === 'recover-invalid' ? 'RECOVER-STAGING-INVALID-MAILBOX' : configurationPhases.includes(phase) ? 'CONFIGURE-STAGING-MAILBOX' : phase === 'health' ? 'CHECK-STAGING-MAILBOX-HEALTH' : undefined), 'CONFIRMATION_INVALID');
  check((phase === 'credentials') === Boolean(options['--credentials']), 'CREDENTIAL_PHASE_REQUIRED');
  check((phase === 'grants') === Boolean(options['--grant-input']), 'GRANT_PHASE_REQUIRED');
  check(phase === 'receipt' ? Boolean(options['--invocation-ref'] && options['--project-ref']) : !options['--invocation-ref'] && !options['--project-ref'], 'RECEIPT_ARGUMENTS_INVALID');
  const origin = exactOrigin(options['--origin'] ?? ''); check(new URL(origin).protocol === 'https:' && !/prod(?:uction)?/i.test(new URL(origin).hostname), 'STAGING_ORIGIN_REQUIRED');
  const recoveryArguments = ['--previous-state', '--previous-sha256', '--previous-profile'];
  check(phase === 'recover-invalid' ? recoveryArguments.every((key) => options[key]) : recoveryArguments.every((key) => !options[key]), 'RECOVERY_ARGUMENTS_INVALID');
  const recovery = phase === 'recover-invalid' ? emailRecoveryPredecessor(options['--previous-state'], options['--previous-sha256'], privateInput(options['--previous-profile']), origin) : undefined;
  if (recovery) {
    const path = resolve(options['--profile']); check(path !== resolve(options['--previous-profile']) && resolve(options['--state']) !== resolve(options['--previous-state']), 'RECOVERY_PATH_REUSED');
    const bytes = Buffer.from(`${JSON.stringify(recovery.profile)}\n`); const directory = dirname(path);
    check(realpathSync(directory) === directory && (lstatSync(directory).mode & 0o077) === 0, 'PRIVATE_DIRECTORY_REQUIRED');
    if (existsSync(path)) check(hash(privateInput(path)) === hash(bytes), 'RECOVERED_PROFILE_CHANGED');
    else { const fd = openSync(path, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600); try { writeFileSync(fd, bytes); fsyncSync(fd); } finally { closeSync(fd); } const dirFD = openSync(directory, constants.O_RDONLY); try { fsyncSync(dirFD); } finally { closeSync(dirFD); } }
  }
  const raw = privateInput(options['--profile']); const profile = JSON.parse(raw); validateEmailProfile(profile);
  const credentials = options['--credentials'] ? JSON.parse(privateInput(options['--credentials'])) : undefined;
  const grantInput = options['--grant-input'] ? JSON.parse(privateInput(options['--grant-input'])) : undefined;
  const sourceSHA = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
  check(execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim() === '', 'CLEAN_CHECKOUT_REQUIRED');
  const timeoutMs = Number(options['--timeout-ms'] ?? 1200000); check(Number.isSafeInteger(timeoutMs) && timeoutMs >= 1000 && timeoutMs <= 1200000, 'TIMEOUT_INVALID');
  const journal = privateJournal(options['--state'], { version: 1, kind: 'EMAIL_MAILBOX_ACCEPTANCE', origin, sourceSHA, profileSHA256: hash(raw), servingManifestSHA256: hash(privateInput(options['--serving-manifest'])), ...(recovery ? { predecessorSHA256: recovery.sha256, predecessorSourceSHA: recovery.sourceSHA, predecessorProfileSHA256: recovery.profileSHA256, predecessorManifestSHA256: recovery.manifestSHA256 } : {}) });
  try {
    const client = createOwnerSessionClient({ origin, storagePath: resolve(options['--storage-state']) }); const deadline = Date.now() + timeoutMs;
    const request = (path, init = {}) => { check(Date.now() < deadline, 'REQUEST_DEADLINE'); return client.request(path, { ...init, signal: AbortSignal.timeout(Math.max(1, Math.min(30000, deadline - Date.now()))) }); };
    const get = async (path) => { const response = await request(path, { method: 'GET', headers: { Accept: 'application/json' } }); check(response.status === 200, `READ_HTTP_${response.status}`); return JSON.parse((await boundedResponseBody(response, 2 << 20)).toString('utf8')); };
    const result = await emailAcceptance({ phase, profile, credentials, grantInput, recovery, journal, request, get, timeoutMs, invocationRef: options['--invocation-ref'], projectRef: options['--project-ref'], preflight: async () => { const response = await client.observe('/api/v1/session', { signal: AbortSignal.timeout(30000) }); check(response.status === 200, 'SESSION_PREFLIGHT_FAILED'); await response.body?.cancel(); } });
    process.stdout.write(`${JSON.stringify(result)}\n`);
  } finally { journal.close(); }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => {
  const code = /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'EMAIL_ACCEPTANCE_FAILED';
  process.stderr.write(`${JSON.stringify({ status: 'FAIL', code })}\n`); process.exitCode = 1;
});
