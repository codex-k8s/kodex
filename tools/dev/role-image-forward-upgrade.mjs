#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, lstatSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { privateJournal, mutationDriver, admittedArtifact, bindingPin, managed } from './role-image-acceptance.mjs';
import { existingFixture } from './role-image-runtime-proof.mjs';
import { createOwnerSessionClient } from './owner-session-client.mjs';
import { exactOrigin } from './owner-session-storage.mjs';
import { boundedResponseBody } from './runtime-workspace-acceptance.mjs';
const check = (ok, code) => { if (!ok) throw new Error(code); };
const hash = value => createHash('sha256').update(typeof value === 'string' || Buffer.isBuffer(value) ? value : JSON.stringify(value)).digest('hex');
const digest = value => /^[a-f0-9]{64}$/.test(value ?? '');
const enc = encodeURIComponent;
const saved = (j, step) => j.events.findLast(e => e.step === step && ['ACK', 'CHECKPOINT'].includes(e.type))?.result;
const unresolved = j => j.events.filter(e => e.type === 'INTENT' && !j.events.some(a => a.type === 'ACK' && a.key === e.key));
const root = fileURLToPath(new URL('../../', import.meta.url));
export function privateInput(path) {
  const stat = lstatSync(path);
  check(stat.isFile() && !stat.isSymbolicLink() && stat.nlink === 1 && (stat.mode & 0o077) === 0 && stat.size > 0 && stat.size <= (8 << 20), 'PRIVATE_INPUT_INVALID');
  return readFileSync(path);
}
export function upgradeInputs(profile, origin) {
  check(profile.version === 1 && Object.keys(profile).sort().join() === 'previousSHA256,previousState,runnerProvenance,runnerProvenanceSHA256,version' && digest(profile.previousSHA256) && digest(profile.runnerProvenanceSHA256), 'UPGRADE_PROFILE_INVALID');
  const fixture = existingFixture(profile.previousState, profile.previousSHA256, origin);
  const prior = privateJournal(profile.previousState, { version: 1, origin }, { readOnly: true });
  let old;
  try {
    check(prior.bytesSHA256 === profile.previousSHA256 && unresolved(prior).length === 0 && prior.events.at(-1)?.type === 'CHECKPOINT' && /^(prepare|advance|restore|upgrade)-complete$/.test(prior.events.at(-1)?.step ?? ''), 'PREDECESSOR_NOT_TERMINAL');
    old = prior.events[0];
    check(/^[a-f0-9]{40}$/.test(old.sourceSHA ?? '') && /^sha256:[a-f0-9]{64}$/.test(old.runnerDigest ?? ''), 'PREDECESSOR_HEADER_INVALID');
  } finally { prior.close(); }
  const bytes = privateInput(profile.runnerProvenance); check(hash(bytes) === profile.runnerProvenanceSHA256, 'PROVENANCE_DIGEST_CHANGED');
  const p = JSON.parse(bytes);
  check(p.version === 1 && p.kind === 'RUNNER_BINARY_PROVENANCE' && /^[a-f0-9]{40}$/.test(p.sourceRevision ?? '') && p.binaryPath === '/usr/local/bin/kodex-agent-runner' && digest(p.binarySHA256) && /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(p.baseImage ?? ''), 'PROVENANCE_INVALID');
  const runnerDigest = p.baseImage.split('@')[1]; check(runnerDigest !== old.runnerDigest, 'NEW_RUNNER_REQUIRED');
  return { fixture, runnerDigest, previousRunnerDigest: old.runnerDigest, baseImage: p.baseImage, previousSourceSHA: old.sourceSHA, runnerSourceSHA: p.sourceRevision, runnerProvenanceSHA256: profile.runnerProvenanceSHA256 };
}
async function pages(get, path) {
  const items = [], seen = new Set(); let cursor;
  do {
    const value = await get(`${path}?pageSize=50${cursor ? `&pageToken=${enc(cursor)}` : ''}`);
    check(Array.isArray(value.items), 'PAGE_INVALID'); items.push(...value.items);
    cursor = value.nextPageToken; if (cursor) { check(typeof cursor === 'string' && !seen.has(cursor) && seen.size < 100, 'CURSOR_INVALID'); seen.add(cursor); }
  } while (cursor);
  return items;
}
const recipePath = f => `/api/v1/projects/${enc(f.projectRef)}/role-image-recipes/${enc(f.recipeRef)}`;
async function source(get, configRef, revisionRef) {
  const history = await get(`/api/v1/managed-configurations/${enc(configRef)}/revisions?pageSize=50`);
  const revisions = await pages(get, `/api/v1/managed-configurations/${enc(configRef)}/revisions`);
  const matches = revisions.filter(r => r.ref === revisionRef); check(matches.length === 1, 'EXACT_SOURCE_MISSING');
  const r = matches[0]; check(r.sourceAvailable === true && typeof r.content === 'string' && hash(r.content) === r.digest, 'SOURCE_UNAVAILABLE');
  return { configuration: history.configuration, revision: r };
}
function newContent(previous, baseImage, previousRunnerDigest) {
  const document = JSON.parse(previous);
  const environment = document.roleImage?.environment;
  // Узкий fixture: только одна точная FROM-строка и прежние CFG-комментарии.
  check(typeof environment?.dockerfile === 'string' && /^FROM [a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}\n(?:\s*#[^\r\n]*\n)*$/.test(environment.dockerfile), 'FIXTURE_DOCKERFILE_UNSUPPORTED');
  check(environment.dockerfile.split('\n')[0].endsWith(`@${previousRunnerDigest}`), 'PREDECESSOR_BASE_CHANGED');
  environment.dockerfile = environment.dockerfile.replace(/^FROM [^\n]+/, `FROM ${baseImage}`);
  return JSON.stringify(document);
}
// Источник runtime repository — только защищённый CP catalog GET. OCI provenance
// сохраняет исходное имя сборки; одинаковый digest связывает обе projections.
export async function trustedRunnerCatalog(get, runnerDigest, expected) {
  check(/^sha256:[a-f0-9]{64}$/.test(runnerDigest ?? ''), 'RUNNER_DIGEST_INVALID');
  const catalog = await get('/api/v1/role-environments');
  const standard = catalog.items?.filter(e => e.key === 'standard');
  check(standard?.length === 1 && standard[0].available === true, 'TRUSTED_CATALOG_UNAVAILABLE');
  const template = standard[0].dockerfileTemplate;
  const match = typeof template === 'string' && /^FROM ([a-z0-9][a-z0-9./:_-]*@(sha256:[a-f0-9]{64}))\n$/.exec(template);
  check(match && match[2] === runnerDigest, 'TRUSTED_BASE_CHANGED');
  const pin = { trustedBaseImage: match[1], templateSHA256: hash(template), catalogEntrySHA256: hash(standard[0]) };
  if (expected) check(Object.entries(pin).every(([key, value]) => expected[key] === value), 'TRUSTED_CATALOG_CHANGED');
  return pin;
}
export async function upgradePlan(inputs, get) {
  const f = inputs.fixture, detail = await get(recipePath(f));
  const artifact = admittedArtifact(detail, f.recipeRef, f.revisionRef, true);
  for (const k of ['artifactRef', 'buildRef', 'manifestDigest', 'promotedReference', 'promotionReceiptSHA256']) check(artifact[k] === f[k], 'PREDECESSOR_PIN_CHANGED');
  const configRef = detail.recipe.managedLineage.configurationRef;
  const { configuration: c, revision: r } = await source(get, configRef, f.revisionRef);
  check(c?.ref === configRef && c.kind === 'ROLE_IMAGE' && c.managedBy === 'UI' && c.projectRef === f.projectRef && c.currentRevision?.ref === f.revisionRef && c.version > 0 && r.state === 'PUBLISHED', 'PUBLISHED_PIN_CHANGED');
  const agent = await get(`/api/v1/agents/${enc(f.agentRef)}`);
  check(agent.ref === f.agentRef && agent.projectRef === f.projectRef && agent.enabled === true && agent.state === 'READY' && agent.roleDefinitionRef === detail.recipe.roleDefinitionRef, 'AGENT_SCOPE_CHANGED');
  const runtime = await get(`/api/v1/agents/${enc(f.agentRef)}/runtime-configuration`), binding = bindingPin(runtime.environmentBinding);
  check(binding.agentRef === f.agentRef && binding.environmentRef === f.environmentRef && runtime.environment?.ref === f.environmentRef && runtime.environment.currentVersion?.ref === binding.versionRef && runtime.environment.currentVersion.image?.artifactRef === f.artifactRef && runtime.environment.currentVersion.image.reference === f.promotedReference, 'OLD_BINDING_CHANGED');
  const catalogPin = await trustedRunnerCatalog(get, inputs.runnerDigest);
  const content = newContent(r.content, catalogPin.trustedBaseImage, inputs.previousRunnerDigest);
  check(content !== r.content && JSON.parse(content).roleImage.roleDefinitionRef === agent.roleDefinitionRef, 'FORWARD_SOURCE_INVALID');
  return { fixture: f, configurationRef: configRef, configurationVersion: c.version, revision: r.revision, contentSHA256: r.digest, name: c.name, binding, recipeVersion: detail.recipe.version, recipeGeneration: detail.recipe.generation, nextContentSHA256: hash(content), ...catalogPin };
}
// Inspect не превращает отсутствие ответа в ACK и не посылает повторный command.
export async function inspectUpgrade(journal, get) {
  const h = journal.events[0];
  await trustedRunnerCatalog(get, h.runnerDigest, h.pins);
  const detail = await get(recipePath(h.pins.fixture));
  const history = await get(`/api/v1/managed-configurations/${enc(h.pins.configurationRef)}/revisions?pageSize=50`);
  const runtime = await get(`/api/v1/agents/${enc(h.pins.fixture.agentRef)}/runtime-configuration`);
  return { status: unresolved(journal).length ? 'UNKNOWN_READBACK_REQUIRED' : saved(journal, 'upgrade-complete') ? 'COMPLETED' : 'IN_PROGRESS', unresolved: unresolved(journal).map(e => ({ step: e.step, key: e.key, method: e.method, bodySHA256: e.bodySHA256 })), matchingRevisions: (await pages(get, `/api/v1/managed-configurations/${enc(h.pins.configurationRef)}/revisions`)).filter(r => r.digest === h.pins.nextContentSHA256).map(r => ({ ref: r.ref, revision: r.revision, state: ['DRAFT', 'VALID', 'INVALID', 'PUBLISHED', 'SUPERSEDED', 'DISCARDED'].includes(r.state) ? r.state : 'UNKNOWN', digest: r.digest })), configurationVersion: history.configuration?.version, publishedRevisionRef: history.configuration?.currentRevision?.ref, recipeGeneration: detail.recipe?.generation, builds: detail.builds?.map(b => ({ ref: b.ref, revisionRef: b.configurationRevisionRef, stage: ['COMPLETED', 'FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER'].includes(b.stage) ? b.stage : 'PENDING' })), artifact: detail.activeArtifact ? { ref: detail.activeArtifact.ref, digest: detail.activeArtifact.manifestDigest } : undefined, binding: bindingPin(runtime.environmentBinding), providerEffect: 'NOT_RUN' };
}
export async function applyUpgrade(journal, inputs, get, request, { sleep = ms => new Promise(r => setTimeout(r, ms)) } = {}) {
  const h = journal.events[0], pins = h.pins, f = pins.fixture;
  check(hash(inputs.fixture) === hash(f) && inputs.runnerDigest === h.runnerDigest && inputs.runnerProvenanceSHA256 === h.runnerProvenanceSHA256, 'UPGRADE_INPUT_CHANGED');
  check(unresolved(journal).length === 0, 'UNRESOLVED_INTENT_READBACK_REQUIRED');
  await trustedRunnerCatalog(get, h.runnerDigest, pins);
  if (saved(journal, 'upgrade-complete')) return saved(journal, 'upgrade-complete');
  if (!journal.events.some(e => e.type === 'INTENT')) check(hash(await upgradePlan(inputs, get)) === hash(pins), 'UPGRADE_PLAN_STALE');
  const old = await source(get, pins.configurationRef, f.revisionRef);
  check(old.revision.digest === pins.contentSHA256, 'PREDECESSOR_SOURCE_CHANGED');
  const content = newContent(old.revision.content, pins.trustedBaseImage, inputs.previousRunnerDigest); check(hash(content) === pins.nextContentSHA256, 'UPGRADE_SOURCE_CHANGED');
  const send = mutationDriver(journal, request);
  const mutate = async (...args) => {
    if (!saved(journal, args[0])) await trustedRunnerCatalog(get, h.runnerDigest, pins);
    return send(...args);
  };
  const checkpoint = (step, result) => { journal.append({ type: 'CHECKPOINT', step, result }); return result; };
  const draft = await mutate('upgrade-draft', 'POST', '/api/v1/role-image-configurations/drafts', { configurationRef: pins.configurationRef, projectRef: f.projectRef, name: pins.name, contentFormat: 'JSON', content }, 201, pins.configurationVersion, v => managed(v, 'DRAFT'));
  check(draft.configurationRef === pins.configurationRef && draft.revision > pins.revision && draft.parentRevisionRef === f.revisionRef && draft.publishedRevisionRef === f.revisionRef && draft.contentSHA256 === pins.nextContentSHA256, 'FORWARD_REVISION_REQUIRED');
  const path = `/api/v1/role-image-configurations/${enc(pins.configurationRef)}/revisions/${enc(draft.revisionRef)}`;
  const valid = await mutate('upgrade-validate', 'POST', `${path}/validation`, undefined, 200, draft.version, v => managed(v, 'VALID'));
  check(valid.configurationRef === pins.configurationRef && valid.contentSHA256 === pins.nextContentSHA256 && valid.revisionRef === draft.revisionRef && valid.publishedRevisionRef === f.revisionRef, 'VALIDATION_PIN_CHANGED');
  const published = await mutate('upgrade-publish', 'POST', `${path}/publication`, undefined, 200, valid.version, v => managed(v, 'PUBLISHED'));
  check(published.configurationRef === pins.configurationRef && published.contentSHA256 === pins.nextContentSHA256 && published.revisionRef === draft.revisionRef && published.publishedRevisionRef === draft.revisionRef, 'PUBLICATION_PIN_CHANGED');
  const wait = async promoted => {
    for (;;) {
      const detail = await get(recipePath(f));
      check(detail.recipe?.managedLineage?.revisionRef === draft.revisionRef && detail.recipe.generation > pins.recipeGeneration, 'UPGRADE_RECIPE_CHANGED');
      check(!detail.builds?.some(b => b.configurationRevisionRef === draft.revisionRef && ['FAILED', 'CANCELLED', 'EXPIRED', 'DEAD_LETTER'].includes(b.stage)), 'BUILD_TERMINAL_FAILURE');
      const a = promoted ? detail.activeArtifact : detail.promotionCandidate;
      check(a?.recipeGeneration !== detail.recipe.generation || a.admissionVerdict !== 'REJECTED', 'ADMISSION_REJECTED');
      if (a?.recipeGeneration === detail.recipe.generation && a.admissionVerdict === 'ACCEPTED' && (!promoted || detail.recipe.promotedImageReady)) return admittedArtifact(detail, f.recipeRef, draft.revisionRef, promoted);
      await sleep(2000);
    }
  };
  const candidate = saved(journal, 'upgrade-candidate') ?? checkpoint('upgrade-candidate', await wait(false));
  check(candidate.artifactRef !== f.artifactRef && candidate.buildRef !== f.buildRef && candidate.manifestDigest !== f.manifestDigest, 'NEW_BUILD_REQUIRED');
  await mutate('upgrade-promote', 'POST', `${recipePath(f)}/promotions`, { imageArtifactRef: candidate.artifactRef, expectedProvenanceSha256: candidate.provenanceSHA256 }, 202, candidate.recipeVersion, v => {
    check(v.recipeRef === f.recipeRef && v.imageArtifactRef === candidate.artifactRef && ['QUEUED', 'PROMOTING', 'PROMOTED'].includes(v.state), 'PROMOTION_RECEIPT_INVALID'); return { ref: v.ref, artifactRef: candidate.artifactRef };
  });
  const artifact = await wait(true);
  check(artifact.artifactRef === candidate.artifactRef && artifact.manifestDigest === candidate.manifestDigest && artifact.provenanceSHA256 === candidate.provenanceSHA256, 'PROMOTED_ARTIFACT_CHANGED');
  if (!saved(journal, 'upgrade-rebind')) check(hash(bindingPin((await get(`/api/v1/agents/${enc(f.agentRef)}/runtime-configuration`)).environmentBinding)) === hash(pins.binding), 'PUBLICATION_CHANGED_OLD_BINDING');
  const plan = await mutate('upgrade-impact', 'POST', `${path}/impact-plans`, undefined, 201, published.version, v => {
    check(v.configurationRef === pins.configurationRef && v.revisionRef === draft.revisionRef && v.artifactRef === artifact.artifactRef && v.total >= 2 && digest(v.digest), 'IMPACT_PIN_CHANGED'); return { ref: v.ref, digest: v.digest };
  });
  let selected = saved(journal, 'upgrade-selection');
  if (!selected) {
    const items = await pages(get, `/api/v1/role-image-impact-plans/${enc(plan.ref)}`);
    check(items.length >= 2 && items.every(i => i.projectRef === f.projectRef && i.environmentRef === f.environmentRef) && new Set(items.map(i => i.ref)).size === items.length, 'IMPACT_SCOPE_CHANGED');
    selected = checkpoint('upgrade-selection', items.map(i => i.ref));
  }
  await mutate('upgrade-rebind', 'POST', `${path}/consumer-bindings`, { planRef: plan.ref, impactDigest: plan.digest, selectedItemRefs: selected }, 200, published.version, v => { check(v.plan?.ref === plan.ref && v.plan.state === 'APPLIED', 'REBIND_NOT_APPLIED'); return { planRef: plan.ref }; });
  const outcomes = await pages(get, `/api/v1/role-image-impact-plans/${enc(plan.ref)}`);
  check(outcomes.length === selected.length && outcomes.every(i => selected.includes(i.ref) && i.outcome === 'APPLIED' && i.projectRef === f.projectRef && i.environmentRef === f.environmentRef), 'REBIND_OUTCOME_INVALID');
  const runtime = await get(`/api/v1/agents/${enc(f.agentRef)}/runtime-configuration`), binding = bindingPin(runtime.environmentBinding);
  check(binding.agentRef === f.agentRef && binding.environmentRef === f.environmentRef && binding.version > pins.binding.version && binding.versionRef !== pins.binding.versionRef && runtime.environment?.ref === f.environmentRef && runtime.environment.currentVersion?.ref === binding.versionRef && runtime.environment.currentVersion.image?.reference === artifact.promotedReference && runtime.environment.currentVersion.image.artifactRef === artifact.artifactRef, 'FINAL_BINDING_IMAGE_MISMATCH');
  return checkpoint('upgrade-complete', { status: 'PASS', ...f, configurationRef: pins.configurationRef, ...artifact, binding, runnerDigest: h.runnerDigest, runnerProvenanceSHA256: h.runnerProvenanceSHA256, providerEffect: 'NOT_RUN', runtimePod: 'NOT_RUN' });
}
export async function main(args) {
  const phase = args.shift(), o = {};
  while (args.length) { const k = args.shift(); check(['--origin', '--storage-state', '--profile', '--serving-manifest', '--state', '--timeout-ms', '--confirm'].includes(k) && !Object.hasOwn(o, k) && args.length, 'ARGUMENT_INVALID'); o[k] = args.shift(); }
  check(['plan', 'apply', 'inspect'].includes(phase), 'PHASE_INVALID');
  check(o['--confirm'] === (phase === 'apply' ? 'APPLY-STAGING-ROLE-IMAGE-UPGRADE' : undefined), 'CONFIRMATION_INVALID');
  for (const k of ['--origin', '--storage-state', '--profile', '--serving-manifest', '--state']) check(o[k], 'ARGUMENT_REQUIRED');
  const origin = exactOrigin(o['--origin']); check(new URL(origin).protocol === 'https:' && !/prod(?:uction)?/i.test(new URL(origin).hostname), 'STAGING_ORIGIN_REQUIRED');
  const sourceSHA = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
  check(execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim() === '', 'CLEAN_CHECKOUT_REQUIRED');
  const profileBytes = privateInput(o['--profile']), profile = JSON.parse(profileBytes), inputs = upgradeInputs(profile, origin);
  execFileSync('git', ['merge-base', '--is-ancestor', inputs.previousSourceSHA, sourceSHA], { cwd: root, stdio: 'pipe' });
  execFileSync('git', ['merge-base', '--is-ancestor', inputs.runnerSourceSHA, sourceSHA], { cwd: root, stdio: 'pipe' });
  const manifestBytes = privateInput(o['--serving-manifest']);
  const header = { version: 1, kind: 'ROLE_IMAGE_FORWARD_UPGRADE', origin, sourceSHA, profileSHA256: hash(profileBytes), servingManifestSHA256: hash(manifestBytes), previousState: resolve(profile.previousState), previousSHA256: profile.previousSHA256, runnerDigest: inputs.runnerDigest, runnerProvenanceSHA256: inputs.runnerProvenanceSHA256 };
  check(phase !== 'plan' || !existsSync(o['--state']), 'UPGRADE_PLAN_EXISTS');
  const timeout = Number(o['--timeout-ms'] ?? 1200000); check(Number.isSafeInteger(timeout) && timeout >= 1000 && timeout <= 1200000, 'TIMEOUT_INVALID');
  const client = createOwnerSessionClient({ origin, storagePath: resolve(o['--storage-state']) }), deadline = Date.now() + timeout;
  const request = (path, init = {}) => { check(Date.now() < deadline, 'READBACK_DEADLINE'); return client.request(path, { ...init, signal: AbortSignal.timeout(Math.max(1, Math.min(30000, deadline - Date.now()))) }); };
  const get = async path => { const r = await request(path, { method: 'GET', headers: { Accept: 'application/json' } }); check(r.status === 200, 'OWNER_READBACK_FAILED'); return JSON.parse((await boundedResponseBody(r, 2 << 20)).toString('utf8')); };
  const session = await client.observe('/api/v1/session', { signal: AbortSignal.timeout(Math.min(timeout, 30000)) }); check(session.status === 200, 'SESSION_PREFLIGHT_FAILED'); await session.body?.cancel();
  if (phase === 'plan') {
    const pins = await upgradePlan(inputs, get), j = privateJournal(o['--state'], { ...header, pins }, { createOnly: true });
    try { j.append({ type: 'CHECKPOINT', step: 'upgrade-plan', result: { status: 'READY', pinsSHA256: hash(pins), providerEffect: 'NOT_RUN' } }); } finally { j.close(); }
    return { status: 'READY', runnerDigest: inputs.runnerDigest, providerEffect: 'NOT_RUN' };
  }
  check(existsSync(o['--state']), 'UPGRADE_PLAN_REQUIRED');
  const journal = privateJournal(o['--state'], header, { readOnly: phase === 'inspect' });
  try {
    check(saved(journal, 'upgrade-plan')?.pinsSHA256 === hash(journal.events[0].pins), 'UPGRADE_PLAN_INVALID');
    return phase === 'inspect' ? await inspectUpgrade(journal, get) : await applyUpgrade(journal, inputs, get, request);
  } finally { journal.close(); }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main(process.argv.slice(2)).then(r => process.stdout.write(`${JSON.stringify(r)}\n`)).catch(e => { process.stderr.write(`${JSON.stringify({ status: 'FAIL', code: /^[A-Z][A-Z0-9_]{1,80}$/.test(e.message ?? '') ? e.message : 'ROLE_IMAGE_UPGRADE_FAILED' })}\n`); process.exitCode = 1; });
