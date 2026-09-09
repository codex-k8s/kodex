#!/usr/bin/env node
import { createHash, randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { lstatSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mutationDriver, privateJournal } from './role-image-acceptance.mjs';
import { createOwnerSessionClient } from './owner-session-client.mjs';
import { exactOrigin } from './owner-session-storage.mjs';
import { boundedResponseBody } from './runtime-workspace-acceptance.mjs';
import { workspaceModelQuery } from './runtime-provider-catalog.mjs';
import { fingerprint } from '../release/scoped-release.mjs';
import {writeRuntimeEvidence} from '../release/runtime-pod-observe.mjs';

import {loadCombinedProfile, combinedPlan, combinedTask, combinedRuntimeBinding, finishCombinedCapture} from './email-combined-acceptance.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const check = (value, code) => { if (!value) throw new Error(code); };
const sha = (value) => createHash('sha256').update(typeof value === 'string' || Buffer.isBuffer(value) ? value : JSON.stringify(value)).digest('hex');
const ref = (value) => { check(typeof value === 'string' && /^[A-Za-z0-9_-]{8,128}$/.test(value), 'REFERENCE_INVALID'); return value; };
const digest = (value) => { check(/^[a-f0-9]{64}$/.test(value ?? ''), 'DIGEST_INVALID'); return value; };
const version = (value) => { check(Number.isSafeInteger(value) && value > 0, 'VERSION_INVALID'); return value; };
const enc = encodeURIComponent;
const saved = (journal, step) => journal.events.findLast((e) => e.step === step && ['ACK', 'CHECKPOINT'].includes(e.type))?.result;
const checkpoint = (journal, step, result) => { journal.append({ type: 'CHECKPOINT', step, result }); return result; };
const definition = JSON.parse(readFileSync(new URL('../../contracts/integrations/v1/definitions/email.yaml', import.meta.url)));
const terminal = ['SUCCEEDED', 'FAILED', 'REJECTED', 'CANCELLED', 'UNKNOWN_OUTCOME'];

// Совпадает с encoding/json для плоского typed input: сортировка ASCII keys,
// HTML и U+2028/2029 escaping. Содержимое остаётся только в private input/Run.
export function emailInputDigest(input) {
  return sha(JSON.stringify(Object.fromEntries(Object.keys(input).sort().map((key) => [key, input[key]])))
    .replace(/[<>&\u2028\u2029]/g, (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`));
}
export function emailOperationProfile(p) {
  check(p && Object.keys(p).every((key) => ['version', 'prefix', 'projectRef', 'agentRef', 'connectionRef', 'configurationRef', 'mailboxRevisionRef', 'accountRef', 'model', 'defaultReasoningEffort', 'capabilityKey', 'input', 'combined'].includes(key)) && p.version === 1 && /^mvp[a-z0-9-]{4,56}$/.test(p.prefix ?? ''), 'PROFILE_INVALID');
  for (const key of ['projectRef', 'connectionRef', 'configurationRef', 'mailboxRevisionRef', 'accountRef']) ref(p[key]);
  if (p.agentRef) ref(p.agentRef);
  check(typeof p.model === 'string' && /^[a-zA-Z0-9._:/-]{1,128}$/.test(p.model) && /^[a-z][a-z0-9_-]{0,63}$/.test(p.defaultReasoningEffort ?? ''), 'MODEL_INVALID');
  const capability = definition.spec.capabilities.find((item) => item.key === p.capabilityKey);
  check(capability && p.input && typeof p.input === 'object' && !Array.isArray(p.input), 'CAPABILITY_INVALID');
  check(Object.keys(p.input).every((key) => capability.inputFields.some((field) => field.key === key)), 'INPUT_FIELD_INVALID');
  for (const field of capability.inputFields) {
    const value = p.input[field.key];
    if (value === undefined) { check(!field.required, 'INPUT_REQUIRED'); continue; }
    check(field.type === 'STRING' ? typeof value === 'string' && [...value].length >= (field.minimumLength ?? 0) && [...value].length <= (field.maximumLength ?? 65536) : field.type === 'INTEGER' && Number.isSafeInteger(value) && value >= (field.minimum ?? 0) && value <= (field.maximum ?? Number.MAX_SAFE_INTEGER), 'INPUT_TYPE_INVALID');
  }
  check(Buffer.byteLength(JSON.stringify(p.input)) <= 24000, 'INPUT_SIZE_INVALID');
  return capability;
}
// Узкий schema profile совпадает с Capability.InputSchema: неизвестное
// расширение закрыто отклоняется. Семантика адресов/UID остаётся у владельца.
export function emailGrantInput(input, raw) {
  let schema; try { schema = JSON.parse(raw); } catch { throw new Error('GRANT_SCHEMA_INVALID'); }
  check(schema?.type === 'object' && schema.additionalProperties === false && schema.properties && Object.keys(schema).every((k) => ['type', 'additionalProperties', 'properties', 'required'].includes(k)), 'GRANT_SCHEMA_INVALID');
  check(Object.keys(input).every((k) => Object.hasOwn(schema.properties, k)) && (!schema.required || Array.isArray(schema.required) && schema.required.every((k) => Object.hasOwn(input, k))), 'GRANT_INPUT_INVALID');
  for (const [key, value] of Object.entries(input)) {
    const field = schema.properties[key];
    check(field && Object.keys(field).every((k) => ['type', 'minLength', 'maxLength', 'enum', 'format', 'minimum', 'maximum'].includes(k)), 'GRANT_SCHEMA_UNSUPPORTED');
    check(field.type === 'string' ? typeof value === 'string' && [...value].length >= (field.minLength ?? 0) && [...value].length <= (field.maxLength ?? 65536) : field.type === 'integer' ? Number.isSafeInteger(value) && value >= (field.minimum ?? Number.MIN_SAFE_INTEGER) && value <= (field.maximum ?? Number.MAX_SAFE_INTEGER) : field.type === 'boolean' && typeof value === 'boolean', 'GRANT_INPUT_INVALID');
    check(!field.enum || Array.isArray(field.enum) && field.enum.includes(value), 'GRANT_INPUT_INVALID');
    check(!field.format || ['email', 'uri'].includes(field.format), 'GRANT_SCHEMA_UNSUPPORTED');
  }
}
export function emailServingManifest(m) {
  check(m?.version === 1 && m.profile === 'component-revisions' && m.status === 'CAPTURED' && m.namespace === 'kodex-system' && m.compatibility && Array.isArray(m.components), 'CAPTURED_MANIFEST_REQUIRED');
  const identity = Object.fromEntries(['version', 'profile', 'clusterUID', 'namespace', 'namespaceUID', 'components', 'compatibility'].map((key) => [key, m[key]]));
  check(fingerprint(identity) === m.manifestSHA256, 'MANIFEST_DIGEST_INVALID');
  for (const name of ['control-api-gateway', 'control-plane', 'runtime-controller', 'integration-gateway', 'email-bridge', 'egress-gateway']) check(m.components.filter((c) => c.kind === 'Deployment' && c.name === name).length === 1, 'MANIFEST_COMPONENT_REQUIRED');
  return digest(m.manifestSHA256);
}

export async function emailAgentPlan(profile, agentRef, get, combined) {
  emailOperationProfile(profile);
  const project = await get(`/api/v1/projects/${enc(profile.projectRef)}`);
  check(project.ref === profile.projectRef, 'PROJECT_SCOPE_MISMATCH');
  const agent = await get(`/api/v1/agents/${enc(agentRef)}`);
  check(agent.ref === agentRef && agent.projectRef === profile.projectRef && agent.enabled === true && agent.system === false && ['READY', 'RUNNING'].includes(agent.state), 'AGENT_NOT_READY');
  const runtime = await get(`/api/v1/agents/${enc(agentRef)}/runtime-configuration`);
  const c = runtime.configuration; const b = runtime.environmentBinding; const environment = runtime.environment;
  check(c?.agentRef === agentRef && runtime.agentVersion === agent.version && b?.agentRef === agentRef && environment?.ref === b.environmentRef && environment.projectRef === profile.projectRef && environment.currentVersion?.ref === b.versionRef && environment.ready === true, 'RUNTIME_PIN_INVALID');
  check(c.model === profile.model && c.providerPolicy?.mode === 'FIXED' && c.providerPolicy.accountCandidates?.length === 1, 'EXACT_ACCOUNT_REQUIRED');
  const candidate = c.providerPolicy.accountCandidates[0];
  check(candidate.accountRef === profile.accountRef && candidate.providerDefinitionKey === 'openai-codex' && candidate.defaultReasoningEffort === profile.defaultReasoningEffort, 'ACCOUNT_PIN_INVALID');
  const catalog = await get(`/api/v1/model-capabilities?${workspaceModelQuery(profile.model, profile.accountRef)}`);
  const model = catalog.items?.find((item) => item.id === profile.model && item.providerDefinitionKey === 'openai-codex');
  check(model?.available === true && model.eligibleProviderAccountRefs?.includes(profile.accountRef) && model.reasoningEfforts?.includes(profile.defaultReasoningEffort) && candidate.catalogRevision === catalog.catalogRevision && candidate.catalogDigest === catalog.catalogDigest, 'MODEL_CATALOG_CHANGED');
  const account = await get(`/api/v1/provider-accounts/${enc(profile.accountRef)}?usagePurpose=LAUNCH&usageAgentRef=${enc(agentRef)}`);
  check(account.ref === profile.accountRef && account.enabled === true && account.ready === true && account.usage?.allowedToSubmit === true && account.usage.agentVersion === agent.version && account.usage.runtimeConfigurationRef === c.ref && account.usage.runtimeConfigurationDigest === c.digest, 'ACCOUNT_NOT_READY');
  const connection = await get(`/api/v1/integration-connections/${enc(profile.connectionRef)}`);
  check(connection.ref === profile.connectionRef && connection.definitionKey === 'email' && connection.state === 'CONNECTED', 'CONNECTION_NOT_READY');
  const grants = connection.grants?.filter((g) => g.enabled && g.agentRef === agentRef && g.capabilityKey === profile.capabilityKey);
  check(grants?.length === 1, 'EXACT_GRANT_REQUIRED'); const grant = grants[0];
  check(sha(grant.inputSchema) === grant.inputSchemaSha256, 'GRANT_SCHEMA_CHANGED');
  emailGrantInput(profile.input, grant.inputSchema);
  const mailbox = await get(`/api/v1/integration-connections/${enc(profile.connectionRef)}/email-mailbox/configuration?configurationRef=${enc(profile.configurationRef)}&revisionRef=${enc(profile.mailboxRevisionRef)}`);
  check(mailbox.connectionRef === profile.connectionRef && mailbox.connectionVersion === connection.version && mailbox.configuration?.ref === profile.configurationRef && mailbox.revision?.ref === profile.mailboxRevisionRef && mailbox.revision.state === 'PUBLISHED' && mailbox.boundRevisionRef === profile.mailboxRevisionRef && mailbox.publication?.state === 'READY' && mailbox.publication.configurationRevisionRef === profile.mailboxRevisionRef, 'MAILBOX_PIN_INVALID');
  let cursor; let capabilityDigest; let found; let count = 0; const seen = new Set();
  do {
    const page = await get(`/api/v1/agents/${enc(agentRef)}/effective-capabilities?pageSize=100${cursor ? `&pageToken=${enc(cursor)}` : ''}`);
    check(page.agentRef === agentRef && page.projectRef === profile.projectRef && page.agentVersion === agent.version && page.runtimeConfigurationRef === c.ref && page.runtimeConfigurationVersion === c.version && page.environmentVersionRef === b.versionRef && page.runtimeReady === true && Array.isArray(page.items) && page.items.length <= 100 && (count += page.items.length) <= 1000, 'EFFECTIVE_SCOPE_CHANGED');
    if (capabilityDigest) check(capabilityDigest === page.digest, 'EFFECTIVE_SCOPE_CHANGED'); capabilityDigest = digest(page.digest);
    for (const item of page.items) if (item.key === profile.capabilityKey && item.connectionRef === profile.connectionRef) { check(!found, 'DUPLICATE_CAPABILITY'); found = item; }
    cursor = page.nextPageToken; check(!cursor || !seen.has(cursor), 'CURSOR_REPEATED'); if (cursor) seen.add(cursor);
  } while (cursor);
  check(found?.effective === true && found.connectionVersion === connection.version && found.grantRef === grant.ref && found.grantVersion === grant.version && found.definitionDigest === connection.definitionDigest, 'GRANT_NOT_EFFECTIVE');
  const runtimeCombined = combined ? await combinedPlan(profile, combined.context, combined.nonce, get) : undefined;
  if(runtimeCombined)check(runtimeCombined.runtime.agentVersion===agent.version&&runtimeCombined.runtime.configurationRef===c.ref&&runtimeCombined.runtime.configurationVersion===c.version&&runtimeCombined.runtime.configurationDigest===c.digest&&runtimeCombined.runtime.bindingRef===b.ref&&runtimeCombined.runtime.bindingVersion===b.version&&runtimeCombined.runtime.bindingDigest===b.digest,'COMBINED_PLAN_DRIFT');
  return { ...(runtimeCombined ? {combined:runtimeCombined} : {}), status: 'READY', projectRef: profile.projectRef, agentRef, agentVersion: version(agent.version), configurationRef: ref(c.ref), configurationVersion: version(c.version), configurationDigest: digest(c.digest), environmentBindingSHA256: sha(b), environmentVersionRef: ref(b.versionRef), environmentImageSHA256: sha(environment.currentVersion.image), overlaySHA256: sha(runtime.publishedOverlay), accountRef: account.ref, accountVersion: version(account.version), accountPolicySHA256: sha(c.providerPolicy), accountPolicyRef: ref(c.providerPolicy.ref), accountPolicyVersion: version(c.providerPolicy.version), accountPolicyDigest: digest(c.providerPolicy.digest), catalogRevision: candidate.catalogRevision, catalogDigest: digest(candidate.catalogDigest), model: profile.model, defaultReasoningEffort: profile.defaultReasoningEffort, capabilityDigest, connectionRef: connection.ref, connectionVersion: version(connection.version), definitionVersion: connection.definitionVersion, definitionDigest: digest(connection.definitionDigest), capabilityKey: profile.capabilityKey, grantRef: ref(grant.ref), grantVersion: version(grant.version), inputSchemaSHA256: digest(grant.inputSchemaSha256), mailboxRef: ref(mailbox.mailboxRef), mailboxConfigurationRevision: version(mailbox.publication.revision), mailboxRevisionDigest: digest(mailbox.revision.digest), publicationRef: ref(mailbox.publication.ref), publicationDigest: digest(mailbox.publication.digest), inputSHA256: emailInputDigest(profile.input), providerEffect: 'NOT_RUN' };
}

export function emailAgentTask(profile, plan) {
  const emailTask = `Выполни ровно один вызов invoke_integration с точным JSON ниже. Не делай других почтовых операций, повторов, исправлений входа, fallback, shell/network запросов или cleanup. При ожидании Gate, ошибке или UNKNOWN остановись; не создавай новый effect. Не выводи полученное содержимое писем, адреса, subject/body или credentials в финальный ответ. Верни только invocationRef и закрытый outcome, полученные от MCP.\n${JSON.stringify({ connection_ref: plan.connectionRef, capability_key: plan.capabilityKey, definition_version: plan.definitionVersion, definition_digest: plan.definitionDigest, input_schema_sha256: plan.inputSchemaSHA256, input: profile.input })}`;
  return plan.combined ? combinedTask(plan.combined, emailTask) : emailTask;
}
export async function emailAgentAcceptance({ phase, profile, journal, get, request, preflight, combined, getContent, observation }) {
  emailOperationProfile(profile); await preflight();
  const mutate = mutationDriver(journal, request);
  const combinedContext = combined ? {context:combined,nonce:saved(journal,'plan')?.combined?.nonce ?? randomBytes(16).toString('hex')} : undefined;
  if (phase === 'agent') {
    check(!profile.agentRef, 'EXISTING_AGENT_CREATE_FORBIDDEN');
    const project = await get(`/api/v1/projects/${enc(profile.projectRef)}`); check(project.ref === profile.projectRef, 'PROJECT_SCOPE_MISMATCH');
    return mutate('agent', 'POST', `/api/v1/projects/${enc(profile.projectRef)}/agents`, { name: `${profile.prefix} Почтовая приёмка`, purpose: 'Приёмка одной разрешённой почтовой операции', roleDescription: 'Проверочный почтовый исполнитель', initialInstructions: 'Выполняй ровно явно разрешённую операцию через MCP. Никогда не повторяй неизвестный внешний эффект.' }, 201, undefined, (body) => { check(body.projectRef === profile.projectRef, 'AGENT_OWNER_MISMATCH'); return { agentRef: ref(body.ref), agentVersion: version(body.version), providerEffect: 'NOT_RUN' }; });
  }
  const agentRef = ref(profile.agentRef ?? saved(journal, 'agent')?.agentRef);
  if (phase === 'plan') {
    check(!journal.events.some((e) => e.type === 'INTENT' && e.step !== 'agent'), 'PLAN_AFTER_RUN_FORBIDDEN');
    const plan = await emailAgentPlan(profile, agentRef, get, combinedContext);
    return checkpoint(journal, 'plan', { ...plan, planSHA256: sha(plan), taskSHA256: sha(emailAgentTask(profile, plan)) });
  }
  const plan = saved(journal, 'plan'); check(plan?.status === 'READY', 'PLAN_REQUIRED');
  if (phase === 'launch') {
    const prior = saved(journal, 'run'); if (prior) return prior;
    check(sha(await emailAgentPlan(profile, agentRef, get, combinedContext)) === plan.planSHA256, 'PLAN_STALE');
    const task = emailAgentTask(profile, plan); check(sha(task) === plan.taskSHA256, 'TASK_CHANGED');
    return mutate('run', 'POST', '/api/v1/runs', { projectRef: profile.projectRef, targetRef: agentRef, targetType: 'AGENT', title: `${profile.prefix} Почтовая операция`, task }, 201, undefined, (body) => { const r = body.run; check(r?.projectRef === profile.projectRef && r.target?.ref === agentRef && r.target.type === 'AGENT', 'RUN_OWNER_MISMATCH'); return { status: 'ACKNOWLEDGED', runRef: ref(r.ref), sessionRef: ref(r.sessionRef), attempt: version(r.attempt), providerEffect: 'MAY_HAVE_STARTED' }; });
  }
  const launched = saved(journal, 'run'); check(launched, 'RUN_ACK_REQUIRED');
  if (phase === 'runtime-binding') { const result=await combinedRuntimeBinding(profile,plan,launched,get); return checkpoint(journal,'runtime-binding',result); }
  if (phase === 'receipt') return emailAgentReceipt({ profile, journal, plan, get });
  const run = await get(`/api/v1/runs/${enc(launched.runRef)}`);
  check(run.ref === launched.runRef && run.projectRef === profile.projectRef && run.sessionRef === launched.sessionRef && run.target?.ref === agentRef && run.target.type === 'AGENT' && run.attempt === launched.attempt, 'RUN_BINDING_CHANGED');
  check(['QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING', 'SUCCEEDED', 'FAILED', 'CANCELLED'].includes(run.state), 'RUN_STATE_INVALID');
  const calls = []; let after = 0; let pages = 0;
  check(Number.isSafeInteger(run.lastEventSequence) && run.lastEventSequence >= 0, 'EVENT_SEQUENCE_INVALID');
  while (after < run.lastEventSequence) {
    check(++pages <= 25, 'EVENT_BUDGET_EXCEEDED');
    const page = await get(`/api/v1/runs/${enc(run.ref)}/events?afterSequence=${after}&limit=500`);
    check(Array.isArray(page.items) && page.items.length > 0 && page.items.length <= 500, 'EVENT_PAGE_INVALID');
    for (const event of page.items) {
      check(event.runRef === run.ref && Number.isSafeInteger(event.sequence) && event.sequence === after + 1, 'EVENT_BINDING_CHANGED'); after = event.sequence;
      if (event.type !== 'TOOL_CALL_RECORDED' || event.toolCall?.tool !== 'invoke_integration') continue;
      const tool = event.toolCall;
      check(tool.safeParameters?.connection_ref === plan.connectionRef && tool.safeParameters?.capability_key === plan.capabilityKey && tool.capabilityRef === plan.capabilityKey && tool.grantRef === plan.grantRef, 'TOOL_SCOPE_CHANGED');
      let result; try { result = JSON.parse(tool.safeResult); } catch { throw new Error('INVOCATION_RESULT_UNAVAILABLE'); }
      check(result?.version === 1 && Object.keys(result).sort().join() === 'inputSHA256,invocationRef,state,version' && terminal.includes(result.state) && result.inputSHA256 === plan.inputSHA256, 'INVOCATION_RESULT_INVALID');
      calls.push({ invocationRef: ref(result.invocationRef), invocationState: result.state, inputSHA256: digest(result.inputSHA256), nodeRef: ref(event.nodeRef), toolCallRef: ref(tool.ref), auditRef: ref(tool.auditRef), eventRef: ref(event.ref), eventSequence: version(event.sequence) });
    }
  }
  check(calls.length <= 1, 'MULTIPLE_INVOCATIONS');
  if (calls.length === 0) {
    check(!['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(run.state), 'TERMINAL_INVOCATION_MISSING');
    return checkpoint(journal, 'pending', { status: run.state === 'WAITING_HUMAN' ? 'WAITING_HUMAN' : 'PENDING', runRef: run.ref, runState: run.state, receipt: 'NOT_RUN', gateRefs: (run.gateRefs ?? []).map(ref) });
  }
  const current = calls[0];
  const diff = await get(`/api/v1/runs/${enc(run.ref)}/runtime-revision-diff`); const revision = diff.current;
  check(revision?.runRef === run.ref && revision.sessionRef === run.sessionRef && revision.attempt === run.attempt, 'REVISION_SCOPE_CHANGED');
  const component = (name) => { const values = diff.changes?.filter((c) => c.component === name); check(values?.length === 1, 'REVISION_COMPONENT_REQUIRED'); return values[0].current; };
  const configuration = component('RUNTIME_CONFIGURATION'); const policy = component('PROVIDER_POLICY');
  check(configuration.ref === plan.configurationRef && configuration.version === plan.configurationVersion && configuration.digest === plan.configurationDigest && policy.ref === plan.accountPolicyRef && policy.version === plan.accountPolicyVersion && policy.digest === plan.accountPolicyDigest && component('MODEL').ref === plan.model, 'RUNTIME_PLAN_CHANGED');
  const graph = await get(`/api/v1/runs/${enc(run.ref)}/graph`);
  const node = graph.nodes?.find((n) => n.ref === current.nodeRef);
  check(graph.runRef === run.ref && node?.runRef === run.ref && node.agentRef === agentRef && node.turnRef === revision.turnRef && node.attempt === run.attempt, 'NODE_BINDING_CHANGED');
  const old = saved(journal, 'capture');
  check(!old || old.invocationRef === current.invocationRef && old.nodeRef === current.nodeRef && old.inputSHA256 === current.inputSHA256, 'CAPTURE_CHANGED');
  const captured = checkpoint(journal, 'capture', { ...launched, ...current, status: current.invocationState === 'SUCCEEDED' ? 'CAPTURED' : current.invocationState, runtimeRevisionRef: ref(revision.ref), runtimeRevisionVersion: version(revision.version), runtimeRevisionDigest: digest(revision.revisionDigest), turnRef: ref(revision.turnRef), lastEventSequence: after, runState: run.state, runTerminal: ['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(run.state), receipt: 'NOT_RUN' });
  return finishCombinedCapture({profile,plan,journal,captured,get,getContent,observation});
}
export async function emailAgentReceipt({ profile, journal, plan, get }) {
  const captured = saved(journal, 'capture'); check(captured, 'CAPTURE_REQUIRED');
  check(captured.runTerminal === true, 'RUN_TERMINAL_REQUIRED');
  if(plan.combined)check(saved(journal,'combined-capture')?.invocationRef===captured.invocationRef && saved(journal,'combined-capture')?.runtimeRevisionDigest===captured.runtimeRevisionDigest,'COMBINED_CAPTURE_REQUIRED');
  const run = await get(`/api/v1/runs/${enc(captured.runRef)}`);
  check(run.ref === captured.runRef && run.projectRef === profile.projectRef && run.sessionRef === captured.sessionRef && run.target?.ref === plan.agentRef && run.attempt === captured.attempt && run.state === captured.runState && run.lastEventSequence === captured.lastEventSequence, 'RECEIPT_RUN_CHANGED');
  if (emailOperationProfile(profile).execution.idempotency === 'READ_ONLY') return checkpoint(journal, 'receipt', { status: captured.runState === 'SUCCEEDED' && captured.invocationState === 'SUCCEEDED' ? 'READ_COMPLETED' : 'NOT_PASS', invocationRef: captured.invocationRef, invocationState: captured.invocationState, receipt: 'NOT_APPLICABLE_READ_ONLY', contentVerification: 'NOT_RUN', delivery: 'NOT_PROVEN' });
  const view = await get(`/api/v1/integration-invocations/${enc(captured.invocationRef)}/email-effect-receipt`);
  const r = view.receipt;
  check(r?.invocationRef === captured.invocationRef && r.projectRef === profile.projectRef && r.connectionRef === plan.connectionRef && r.mailboxRef === plan.mailboxRef && r.configurationRevision === plan.mailboxConfigurationRevision, 'RECEIPT_SCOPE_CHANGED');
  check(['EFFECT_CONFIRMED', 'NO_EFFECT_CONFIRMED', 'UNKNOWN_OUTCOME'].includes(r.outcome), 'RECEIPT_OUTCOME_INVALID');
  return checkpoint(journal, 'receipt', { status: captured.runState === 'SUCCEEDED' && captured.invocationState === 'SUCCEEDED' && r.outcome === 'EFFECT_CONFIRMED' ? 'PASS' : 'NOT_PASS', invocationRef: captured.invocationRef, invocationState: captured.invocationState, outcome: r.outcome, receiptRef: ref(r.ref), receiptVersion: version(r.version), externalReceiptDigest: digest(r.externalReceiptDigest), semanticInputDigest: digest(r.semanticInputDigest), delivery: 'NOT_PROVEN' });
}

function privateInput(path) {
  check(path, 'PRIVATE_INPUT_REQUIRED'); const file = resolve(path); const info = lstatSync(file);
  check(info.isFile() && !info.isSymbolicLink() && info.nlink === 1 && (info.mode & 0o077) === 0 && info.size > 0 && info.size <= (2 << 20), 'PRIVATE_INPUT_INVALID'); return readFileSync(file);
}
async function main() {
  const args = process.argv.slice(2); const phase = args.shift(); const options = {};
  while (args.length) { const key = args.shift(); check(/^--[a-z][a-z0-9-]*$/.test(key ?? '') && args.length && !(key in options), 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  check(['agent', 'plan', 'launch', 'capture', 'receipt', 'runtime-binding'].includes(phase), 'PHASE_INVALID');
  check(Object.keys(options).every((key) => ['--origin', '--storage-state', '--state', '--profile', '--serving-manifest', '--timeout-ms', '--confirm', '--runtime-binding-output', '--runtime-observation'].includes(key)), 'ARGUMENT_UNKNOWN');
  check(options['--confirm'] === (phase === 'agent' ? 'CREATE-STAGING-EMAIL-AGENT' : phase === 'launch' ? 'START-ONE-STAGING-EMAIL-RUN' : undefined), 'CONFIRMATION_INVALID');
  const origin = exactOrigin(options['--origin'] ?? ''); check(new URL(origin).protocol === 'https:' && !/prod(?:uction)?/i.test(new URL(origin).hostname), 'STAGING_ORIGIN_REQUIRED');
  const profileBytes = privateInput(options['--profile']); const profile = JSON.parse(profileBytes); emailOperationProfile(profile);
  check(!options['--runtime-binding-output'] || phase==='runtime-binding' && profile.combined, 'BINDING_OUTPUT_PHASE_INVALID');
  check(!options['--runtime-observation'] || phase==='capture' && profile.combined, 'OBSERVATION_PHASE_INVALID');
  if(phase==='runtime-binding')check(profile.combined && options['--runtime-binding-output'],'COMBINED_BINDING_OUTPUT_REQUIRED');
  const observation=options['--runtime-observation'] ? JSON.parse(privateInput(options['--runtime-observation'])) : undefined;
  const manifestBytes = privateInput(options['--serving-manifest']); const manifest=JSON.parse(manifestBytes); emailServingManifest(manifest);
  const combined=loadCombinedProfile(profile,origin,privateInput,manifest);
  const sourceSHA = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
  check(execFileSync('git', ['status', '--porcelain'], { cwd: root, encoding: 'utf8' }).trim() === '', 'CLEAN_CHECKOUT_REQUIRED');
  const timeoutMs = Number(options['--timeout-ms'] ?? 60000); check(Number.isSafeInteger(timeoutMs) && timeoutMs >= 1000 && timeoutMs <= 1200000, 'TIMEOUT_INVALID');
  check(options['--state'] && options['--storage-state'], 'PRIVATE_INPUT_REQUIRED');
  const journal = privateJournal(options['--state'], { version: 1, kind: 'EMAIL_AGENT_ACCEPTANCE', origin, sourceSHA, profileSHA256: sha(profileBytes), servingManifestSHA256: sha(manifestBytes) });
  try {
    const client = createOwnerSessionClient({ origin, storagePath: resolve(options['--storage-state']) }); const deadline = Date.now() + timeoutMs;
    const request = (path, init = {}) => { check(Date.now() < deadline, 'REQUEST_DEADLINE'); return client.request(path, { ...init, signal: AbortSignal.timeout(Math.max(1, Math.min(30000, deadline - Date.now()))) }); };
    const get = async (path) => { const response = await request(path, { method: 'GET', headers: { Accept: 'application/json' } }); check(response.status === 200, `READ_HTTP_${response.status}`); return JSON.parse((await boundedResponseBody(response, 2 << 20)).toString('utf8')); };
    const result = await emailAgentAcceptance({ phase, profile, journal, get, request, combined, observation, getContent:async(path,maximumBytes)=>{const response=await request(path,{method:'GET'});check(response.status===200,'WORKSPACE_CONTENT_UNAVAILABLE');return boundedResponseBody(response,maximumBytes);}, preflight: async () => { const response = await client.observe('/api/v1/session', { signal: AbortSignal.timeout(Math.min(30000, timeoutMs)) }); check(response.status === 200, 'SESSION_PREFLIGHT_FAILED'); await response.body?.cancel(); } });
    if(phase==='runtime-binding' && result.status==='BOUND') writeRuntimeEvidence(options['--runtime-binding-output'],result);
    process.stdout.write(`${JSON.stringify(result)}\n`);
    if (['FAILED', 'REJECTED', 'CANCELLED', 'UNKNOWN_OUTCOME', 'NOT_PASS'].includes(result.status)) process.exitCode = 2;
  } finally { journal.close(); }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => { const code = /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'EMAIL_AGENT_ACCEPTANCE_FAILED'; process.stderr.write(`${JSON.stringify({ status: 'FAIL', code })}\n`); process.exitCode = 1; });
