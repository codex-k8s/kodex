#!/usr/bin/env node
import { createHash, randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { lstatSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { admittedArtifact, mutationDriver, privateJournal } from './role-image-acceptance.mjs';
import { createOwnerSessionClient } from './owner-session-client.mjs';
import { exactOrigin } from './owner-session-storage.mjs';
import { boundedResponseBody, workspaceAcceptanceTask } from './runtime-workspace-acceptance.mjs';
import { workspaceModelQuery } from './runtime-provider-catalog.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const check = (condition, code) => { if (!condition) throw new Error(code); };
const sha = (value) => createHash('sha256').update(typeof value === 'string' || Buffer.isBuffer(value) ? value : JSON.stringify(value)).digest('hex');
const ref = (value) => { check(typeof value === 'string' && /^[A-Za-z0-9_-]{1,160}$/.test(value), 'REFERENCE_INVALID'); return value; };
const digest = (value) => { check(/^[a-f0-9]{64}$/.test(value ?? ''), 'DIGEST_INVALID'); return value; };
const positive = (value) => { check(Number.isSafeInteger(value) && value > 0, 'VERSION_INVALID'); return value; };
const enc = encodeURIComponent;
const saved = (journal, step) => journal.events.findLast((event) => event.step === step && ['ACK', 'CHECKPOINT'].includes(event.type))?.result;
const checkpoint = (journal, step, result) => { journal.append({ type: 'CHECKPOINT', step, result }); return result; };

export function existingFixture(path, expectedDigest, origin, depth = 0) {
  check(depth < 8, 'FIXTURE_CHAIN_TOO_DEEP');
  const journal = privateJournal(path, { version: 1, origin }, { readOnly: true });
  try {
    check(journal.bytesSHA256 === digest(expectedDigest), 'FIXTURE_JOURNAL_CHANGED');
    check(!journal.events.some((event) => event.type === 'INTENT' && !journal.events.some((ack) => ack.type === 'ACK' && ack.key === event.key)), 'FIXTURE_INTENT_UNRESOLVED');
    const completed = journal.events.findLast((event) => event.type === 'CHECKPOINT' && /^(prepare|advance|restore|upgrade)-complete$/.test(event.step ?? ''))?.result;
    check(completed?.status === 'PASS', 'FIXTURE_NOT_COMPLETE');
    const header = journal.events[0];
    if (header.kind === 'ROLE_IMAGE_FORWARD_UPGRADE') {
      check(journal.events.at(-1)?.step === 'upgrade-complete' && completed === journal.events.at(-1).result, 'UPGRADE_NOT_TERMINAL');
      const previous = existingFixture(header.previousState, header.previousSHA256, origin, depth + 1);
      check(sha(previous) === sha(header.pins?.fixture), 'UPGRADE_PREDECESSOR_CHANGED');
      for (const key of ['projectRef', 'agentRef', 'environmentRef', 'recipeRef']) check(completed[key] === previous[key], 'UPGRADE_SCOPE_CHANGED');
      check(completed.revisionRef !== previous.revisionRef && completed.artifactRef !== previous.artifactRef && completed.buildRef !== previous.buildRef && completed.manifestDigest !== previous.manifestDigest, 'UPGRADE_NOT_FORWARD');
      check(completed.runnerDigest === header.runnerDigest && completed.runnerProvenanceSHA256 === header.runnerProvenanceSHA256 && /^sha256:[a-f0-9]{64}$/.test(header.runnerDigest ?? '') && digest(header.runnerProvenanceSHA256), 'UPGRADE_BASE_CHANGED');
      for (const step of ['upgrade-draft', 'upgrade-validate', 'upgrade-publish', 'upgrade-promote', 'upgrade-impact', 'upgrade-rebind']) check(journal.events.some(e => e.type === 'ACK' && e.step === step), 'UPGRADE_RECEIPT_MISSING');
      const rebind = saved(journal, 'upgrade-rebind');
      check(completed.binding?.environmentRef === previous.environmentRef && completed.binding.agentRef === previous.agentRef && completed.binding.versionRef !== header.pins.binding.versionRef && completed.binding.version > header.pins.binding.version && rebind?.planRef === saved(journal, 'upgrade-impact')?.ref, 'UPGRADE_REBIND_MISSING');
    } else check(!journal.events.some(e => e.step === 'upgrade-complete'), 'UPGRADE_HEADER_MISSING');

    for (const key of ['projectRef', 'agentRef', 'environmentRef', 'recipeRef', 'revisionRef', 'artifactRef', 'buildRef']) ref(completed[key]);
    check(/^sha256:[a-f0-9]{64}$/.test(completed.manifestDigest ?? '') && completed.promotedReference?.endsWith(`@${completed.manifestDigest}`), 'FIXTURE_IMAGE_INVALID');
    digest(completed.promotionReceiptSHA256);
    return Object.fromEntries(['projectRef', 'agentRef', 'environmentRef', 'recipeRef', 'revisionRef', 'artifactRef', 'buildRef', 'manifestDigest', 'promotedReference', 'promotionReceiptSHA256'].map((key) => [key, completed[key]]));
  } finally { journal.close(); }
}

// Только owner GET projections: план не меняет account policy, capability или Run.
export async function prepareRuntimePlan(fixture, get) {
  const { projectRef, agentRef, environmentRef, recipeRef, revisionRef } = fixture;
  const agent = await get(`/api/v1/agents/${enc(agentRef)}`);
  check(agent.ref === agentRef && agent.projectRef === projectRef, 'AGENT_OWNER_MISMATCH');
  const artifact = admittedArtifact(await get(`/api/v1/projects/${enc(projectRef)}/role-image-recipes/${enc(recipeRef)}`), recipeRef, revisionRef, true);
  for (const key of ['artifactRef', 'buildRef', 'manifestDigest', 'promotedReference', 'promotionReceiptSHA256']) check(artifact[key] === fixture[key], 'ARTIFACT_PIN_CHANGED');
  const runtime = await get(`/api/v1/agents/${enc(agentRef)}/runtime-configuration`);
  const binding = runtime.environmentBinding; const environment = runtime.environment; const configuration = runtime.configuration;
  check(configuration?.agentRef === agentRef && runtime.agentVersion === agent.version && binding?.agentRef === agentRef && binding.environmentRef === environmentRef && environment?.ref === environmentRef && environment.projectRef === projectRef && binding.versionRef === environment.currentVersion?.ref && environment.currentVersion.image?.artifactRef === fixture.artifactRef && environment.currentVersion.image.reference === fixture.promotedReference, 'ENVIRONMENT_PIN_CHANGED');
  const capabilities = []; const cursors = new Set(); let cursor; let capabilityDigest;
  let runtimeReady;
  do {
    const page = await get(`/api/v1/agents/${enc(agentRef)}/effective-capabilities?pageSize=100${cursor ? `&pageToken=${enc(cursor)}` : ''}`);
    check(page.agentRef === agentRef && page.agentVersion === agent.version && page.runtimeConfigurationRef === configuration.ref && page.runtimeConfigurationVersion === configuration.version && page.environmentVersionRef === binding.versionRef && Array.isArray(page.items) && capabilities.length + page.items.length <= 1000, 'CAPABILITY_SCOPE_CHANGED');
    if (capabilityDigest) check(capabilityDigest === page.digest && runtimeReady === page.runtimeReady, 'CAPABILITY_SCOPE_CHANGED');
    capabilityDigest = digest(page.digest); runtimeReady = page.runtimeReady; capabilities.push(...page.items);
    cursor = page.nextPageToken; check(!cursor || !cursors.has(cursor), 'CAPABILITY_CURSOR_REPEATED'); if (cursor) cursors.add(cursor);
  } while (cursor);
  const blockers = [];
  if (environment.ready !== true || runtimeReady !== true) blockers.push('RUNTIME_NOT_READY');
  if (!capabilities.some((item) => item.key === 'platform.artifact.manage' && item.effective === true)) blockers.push('ARTIFACT_CAPABILITY_REQUIRED');
  const policy = configuration.providerPolicy;
  const account = policy?.mode === 'FIXED' && policy.accountCandidates?.length === 1 ? policy.accountCandidates[0] : undefined;
  let accountPin;
  if (!account) blockers.push('EXACT_FIXED_ACCOUNT_REQUIRED');
  else {
    const catalog = await get(`/api/v1/model-capabilities?${workspaceModelQuery(configuration.model, account.accountRef)}`);
    const model = catalog.items?.find((item) => item.id === configuration.model && item.providerDefinitionKey === 'openai-codex' && item.available === true && item.eligibleProviderAccountRefs?.includes(account.accountRef));
    if (!model || account.providerDefinitionKey !== 'openai-codex' || account.catalogRevision !== catalog.catalogRevision || account.catalogDigest !== catalog.catalogDigest) blockers.push('ACCOUNT_MODEL_CATALOG_CHANGED');
    accountPin = { accountRef: ref(account.accountRef), policyRef: ref(policy.ref), policyVersion: positive(policy.version), policyDigest: digest(policy.digest), catalogRevision: account.catalogRevision, catalogDigest: account.catalogDigest };
  }
  return {
    status: blockers.length ? 'BLOCKED' : 'READY', blockers, ...fixture,
    agentVersion: positive(agent.version), configurationRef: ref(configuration.ref), configurationVersion: positive(configuration.version), configurationDigest: digest(configuration.digest),
    bindingRef: ref(binding.ref), bindingVersion: positive(binding.version), bindingDigest: digest(binding.digest), environmentVersionRef: ref(binding.versionRef), capabilityDigest,
    ...(accountPin ? { accountPin } : {}),
    providerEffect: 'NOT_RUN', runtimePod: 'NOT_RUN', runningRunnerBinary: 'NOT_RUN',
  };
}

export async function runtimeProof({ phase, fixture, journal, get, request, preflight, timeoutMs = 1200000, now = Date.now, sleep = (ms) => new Promise((done) => setTimeout(done, ms)) }) {
  await preflight();
  if (phase === 'plan') {
    check(!journal.events.some((item) => item.type === 'INTENT'), 'PLAN_AFTER_INTENT_FORBIDDEN');
    const result = await prepareRuntimePlan(fixture, get);
    return checkpoint(journal, 'plan', { ...result, planSHA256: sha(result), nonce: saved(journal, 'plan')?.nonce ?? randomBytes(16).toString('hex') });
  }
  const plan = saved(journal, 'plan'); check(plan, 'PLAN_REQUIRED');
  check(plan.planSHA256 === sha(Object.fromEntries(Object.entries(plan).filter(([key]) => !['planSHA256', 'nonce'].includes(key)))) && /^[a-f0-9]{32}$/.test(plan.nonce), 'PLAN_DIGEST_INVALID');
  const runPath = '/api/v1/runs';
  if (phase === 'launch') {
    check(plan.status === 'READY', 'PLAN_BLOCKED');
    const previous = saved(journal, 'run'); if (previous) return { status: 'ACKNOWLEDGED', ...previous, providerEffect: 'MAY_HAVE_STARTED' };
    check(!journal.events.some((event) => event.type === 'INTENT'), 'UNRESOLVED_INTENT_READBACK_REQUIRED');
    check(sha(await prepareRuntimePlan(fixture, get)) === plan.planSHA256, 'PLAN_STALE');
    const result = await mutationDriver(journal, request)('run', 'POST', runPath, { projectRef: fixture.projectRef, targetRef: fixture.agentRef, targetType: 'AGENT', title: 'Приёмка закреплённого образа и workspace', task: workspaceAcceptanceTask(plan.nonce) }, 201, undefined, (body) => {
      check(body.run?.projectRef === fixture.projectRef && body.run.target?.ref === fixture.agentRef && body.run.target?.type === 'AGENT', 'RUN_OWNER_MISMATCH');
      return { runRef: ref(body.run.ref), sessionRef: ref(body.run.sessionRef), attempt: positive(body.run.attempt), taskSHA256: sha(workspaceAcceptanceTask(plan.nonce)) };
    });
    return { status: 'ACKNOWLEDGED', ...result, providerEffect: 'MAY_HAVE_STARTED' };
  }
  const launched = saved(journal, 'run'); check(launched, 'RUN_ACK_REQUIRED');
  const deadline = now() + timeoutMs;
  for (;;) {
    check(now() < deadline, 'CAPTURE_DEADLINE');
    const run = await get(`${runPath}/${enc(launched.runRef)}`);
    check(run.ref === launched.runRef && run.projectRef === fixture.projectRef && run.target?.ref === fixture.agentRef && run.sessionRef === launched.sessionRef && run.attempt === launched.attempt, 'RUN_BINDING_CHANGED');
    if (run.state === 'QUEUED') { await sleep(1000); continue; }
    check(['RUNNING', 'SUCCEEDED', 'FAILED', 'WAITING_HUMAN', 'CANCELLING', 'CANCELLED'].includes(run.state), 'RUN_STATE_INVALID');
    const diff = await get(`${runPath}/${enc(run.ref)}/runtime-revision-diff`); const revision = diff.current;
    check(revision?.runRef === run.ref && revision.sessionRef === run.sessionRef && revision.attempt === run.attempt, 'REVISION_BINDING_CHANGED');
    const image = diff.changes?.find((item) => item.component === 'IMAGE')?.current;
    check(image?.digest?.replace(/^sha256:/, '') === fixture.manifestDigest.slice(7), 'RUNTIME_IMAGE_CHANGED');
    const binding = { runRef: run.ref, sessionRef: run.sessionRef, turnRef: ref(revision.turnRef), attempt: run.attempt, revisionRef: ref(revision.ref), revisionVersion: positive(revision.version), revisionDigest: digest(revision.revisionDigest), projectHash: sha(fixture.projectRef).slice(0, 16), sessionHash: sha(run.sessionRef).slice(0, 16), turnHash: sha(revision.turnRef).slice(0, 16), imageManifestDigest: fixture.manifestDigest, imageReference: fixture.promotedReference };
    return checkpoint(journal, 'capture', { status: 'PASS', runState: run.state, ...binding, runtimePod: 'NOT_RUN', runningRunnerBinary: 'NOT_RUN', workspaceResult: 'NOT_RUN' });
  }
}

function privateInput(path, maximum = 1 << 20) {
  check(path, 'PRIVATE_INPUT_REQUIRED'); const file = resolve(path); const info = lstatSync(file);
  check(info.isFile() && !info.isSymbolicLink() && info.nlink === 1 && (info.mode & 0o077) === 0 && info.size > 0 && info.size <= maximum, 'PRIVATE_INPUT_INVALID');
  return readFileSync(file);
}
async function main() {
  const args = process.argv.slice(2); const phase = args.shift(); const options = {};
  while (args.length) { const key = args.shift(); check(/^--[a-z][a-z0-9-]*$/.test(key ?? '') && args.length && !(key in options), 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  check(['plan', 'launch', 'capture'].includes(phase), 'PHASE_INVALID');
  check(Object.keys(options).every((key) => ['--origin', '--storage-state', '--state', '--fixture-state', '--fixture-sha256', '--serving-manifest', '--timeout-ms', '--confirm'].includes(key)), 'ARGUMENT_UNKNOWN');
  check(phase === 'launch' ? options['--confirm'] === 'START-ONE-STAGING-AGENT-RUN' : options['--confirm'] === undefined, 'CONFIRMATION_INVALID');
  const origin = exactOrigin(options['--origin'] ?? ''); check(new URL(origin).protocol === 'https:' && !/prod(?:uction)?/i.test(new URL(origin).hostname), 'STAGING_ORIGIN_REQUIRED');
  check(options['--storage-state'] && options['--state'] && options['--fixture-state'], 'PRIVATE_INPUT_REQUIRED');
  const fixture = existingFixture(options['--fixture-state'], options['--fixture-sha256'], origin);
  const sourceSHA = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
  check(execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim() === '', 'CLEAN_CHECKOUT_REQUIRED');
  const timeoutMs = Number(options['--timeout-ms'] ?? 1200000); check(Number.isSafeInteger(timeoutMs) && timeoutMs >= 60000 && timeoutMs <= 1800000, 'TIMEOUT_INVALID');
  const journal = privateJournal(options['--state'], { version: 1, kind: 'ROLE_IMAGE_RUNTIME_PROOF', origin, sourceSHA, fixtureJournalSHA256: options['--fixture-sha256'], servingManifestSHA256: sha(privateInput(options['--serving-manifest'])) });
  try {
    const client = createOwnerSessionClient({ origin, storagePath: resolve(options['--storage-state']) }); const deadline = Date.now() + timeoutMs;
    const request = (path, options = {}) => { check(Date.now() < deadline, 'REQUEST_DEADLINE'); return client.request(path, { ...options, signal: AbortSignal.timeout(Math.max(1, Math.min(30000, deadline - Date.now()))) }); };
    const get = async (path) => { const response = await request(path, { method: 'GET', headers: { Accept: 'application/json' } }); check(response.status === 200, `READ_HTTP_${response.status}`); return JSON.parse((await boundedResponseBody(response, 2 << 20)).toString('utf8')); };
    const result = await runtimeProof({ phase, fixture, journal, get, request, timeoutMs, preflight: async () => { const response = await client.observe('/api/v1/session', { signal: AbortSignal.timeout(30000) }); check(response.status === 200, 'SESSION_PREFLIGHT_FAILED'); await response.body?.cancel(); } });
    process.stdout.write(`${JSON.stringify(result)}\n`);
  } finally { journal.close(); }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => {
  const code = /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'ROLE_IMAGE_RUNTIME_PROOF_FAILED';
  process.stderr.write(`${JSON.stringify({ status: 'FAIL', code })}\n`); process.exitCode = 1;
});
