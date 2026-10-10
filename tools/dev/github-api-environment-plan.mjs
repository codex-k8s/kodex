// Узкая офлайн-подготовка owner-команд #1797; HTTP и credential values отсутствуют.
import { createHash } from 'node:crypto';
import { closeSync, constants, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { environmentDraft, selectBindings } from './role-image-native-plan.mjs';

export const githubAPIRule = Object.freeze({ domainPattern: 'api.github.com', protocol: 'HTTPS', port: 443, httpMethods: Object.freeze(['GET', 'HEAD', 'POST', 'PATCH']) });
const check = (ok, code) => { if (!ok) throw new Error(code); };
const clone = value => JSON.parse(JSON.stringify(value));
const canonical = value => Array.isArray(value) ? value.map(canonical) : value && typeof value === 'object' ? Object.fromEntries(Object.keys(value).sort().map(key => [key, canonical(value[key])])) : value;
export const snapshotSHA256 = value => createHash('sha256').update(JSON.stringify(canonical(value))).digest('hex');
const bytesSHA256 = value => createHash('sha256').update(value).digest('hex');
const same = (a, b) => snapshotSHA256(a) === snapshotSHA256(b);
const lexical = (a, b) => a < b ? -1 : a > b ? 1 : 0;
const normalizedRules = rules => clone(rules).map(rule => ({ ...rule, httpMethods: rule.httpMethods.sort() })).sort((a, b) => lexical(a.domainPattern, b.domainPattern));
function canonicalDraftSpecification(specification) {
  // CP domainRuntimeEnvironmentPolicy -> NormalizeRuntimeEnvironmentPolicy ->
  // castEnvironmentDraft: сортируется policy, прочие arrays/fields не меняются.
  const result = clone(specification), policy = result.policy;
  policy.volumes.sort((a, b) => lexical(a.name, b.name));
  policy.networkDestinations.sort();
  policy.webAccess.rules = normalizedRules(policy.webAccess.rules);
  return result;
}
function bindingDigest(agentRef, environmentRef, bindingVersion) {
  const hash = createHash('sha256');
  for (const part of [agentRef, environmentRef, String(bindingVersion)]) hash.update(part).update('\0');
  return hash.digest('hex');
}
const ref = value => typeof value === 'string' && /^[A-Za-z0-9_-]{1,160}$/.test(value);
const version = value => Number.isSafeInteger(value) && value > 0;
const digest = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
function shape(value, required, optional = []) {
  check(value && typeof value === 'object' && !Array.isArray(value) && required.every(key => Object.hasOwn(value, key)) && Object.keys(value).every(key => [...required, ...optional].includes(key)), 'UNKNOWN_OR_MISSING_FIELDS');
}
const scopeKeys = ['organizationRef', 'scopeKind', 'projectRef'];
const bindingKeys = [...scopeKeys, 'consumerRef', 'consumerVersion', 'bindingRef', 'bindingVersion', 'sourceRevisionRef'];
function scope(actual, expected) {
  check(expected.scopeKind === 'PROJECT' && ref(expected.organizationRef) && ref(expected.projectRef) && scopeKeys.every(key => actual[key] === expected[key]), 'SCOPE_CHANGED');
}
function publicEnvironment(env, expected) {
  shape(env, ['scopeKind', 'organizationRef', 'ref', 'version', 'projectRef', 'name', 'description', 'state', 'currentVersion', 'updatedAt', 'ready', 'readinessBlockers', 'nextActions']);
  scope(env, expected);
  check(env.ref === expected.environmentRef && version(env.version) && env.state === 'ACTIVE' && env.ready === true && Array.isArray(env.readinessBlockers) && env.readinessBlockers.length === 0 && typeof env.name === 'string' && typeof env.description === 'string' && Array.isArray(env.nextActions), 'ENVIRONMENT_INVALID');
  const current = env.currentVersion, policy = current?.policy;
  shape(current, ['ref', 'version', 'revision', 'image', 'tools', 'values', 'secretDescriptors', 'policy', 'digest', 'createdAt']);
  check(ref(current.ref) && version(current.version) && version(current.revision) && digest(current.digest), 'REVISION_INVALID');
  shape(current.image, ['artifactRef', 'recipeRef', 'recipeGeneration', 'reference', 'digest']);
  check(ref(current.image.artifactRef) && ref(current.image.recipeRef) && version(current.image.recipeGeneration) && /^sha256:[a-f0-9]{64}$/.test(current.image.digest) && typeof current.image.reference === 'string' && current.image.reference.endsWith('@' + current.image.digest), 'IMAGE_PIN_INVALID');
  check(Array.isArray(current.tools) && current.tools.length === 38 && new Set(current.tools.map(tool => tool.command)).size === 38, 'TOOLS_CHANGED');
  for (const tool of current.tools) shape(tool, ['name', 'command', 'description', 'usageHint']);
  check(Array.isArray(current.values) && current.values.length <= 128 && new Set(current.values.map(item => item.name)).size === current.values.length, 'VALUES_INVALID');
  for (const item of current.values) { shape(item, ['name', 'value']); check(typeof item.name === 'string' && typeof item.value === 'string', 'VALUES_INVALID'); }
  check(Array.isArray(current.secretDescriptors) && current.secretDescriptors.length <= 128 && new Set(current.secretDescriptors.map(item => item.name)).size === current.secretDescriptors.length, 'SECRET_PIN_INVALID');
  for (const item of current.secretDescriptors) { shape(item, ['name', 'secretRef', 'revision', 'secretName', 'secretKey', 'secretUid', 'secretResourceVersion', 'contentSha256']); check(ref(item.secretRef) && version(item.revision) && digest(item.contentSha256), 'SECRET_PIN_INVALID'); }
  shape(policy, ['resources', 'volumes', 'network', 'kubernetesAccess', 'resourcesDigest', 'volumesDigest', 'networkDigest', 'rbacDigest']);
  shape(policy.resources, ['cpuRequestMilli', 'cpuLimitMilli', 'memoryRequestMib', 'memoryLimitMib', 'ephemeralStorageRequestMib', 'ephemeralStorageLimitMib'], ['workspaceLimits']);
  if (policy.resources.workspaceLimits) shape(policy.resources.workspaceLimits, ['maxBytes', 'maxFiles']);
  check(Array.isArray(policy.volumes) && policy.volumes.length <= 16 && new Set(policy.volumes.map(item => item.name)).size === policy.volumes.length, 'VOLUMES_INVALID');
  for (const volume of policy.volumes) { shape(volume, ['name', 'kind', 'sizeMib', 'mountPath']); check(volume.mountPath === '/workspace/.kodex/volumes/' + volume.name, 'VOLUME_UNREPRESENTABLE'); }
  shape(policy.kubernetesAccess, ['kind', 'namespace']);
  check(policy.kubernetesAccess.kind === 'NONE' && policy.kubernetesAccess.namespace === 'kodex-runtime', 'KUBERNETES_ACCESS_CHANGED');
  shape(policy.network, ['denyByDefault', 'egress', 'webAccess']);
  check(policy.network.denyByDefault === true && Array.isArray(policy.network.egress) && policy.network.egress.length === 4, 'NETWORK_INVALID');
  for (const item of policy.network.egress) shape(item, ['destination', 'protocol', 'port']);
  check(same(policy.network.egress, [{ destination: 'DNS', protocol: 'TCP', port: 53 }, { destination: 'DNS', protocol: 'UDP', port: 53 }, { destination: 'PROVIDER_PROXY', protocol: 'TCP', port: 8084 }, { destination: 'RUNTIME_CALLBACK', protocol: 'TCP', port: 8444 }]), 'NETWORK_UNREPRESENTABLE');
  shape(policy.network.webAccess, ['mode', 'rules']);
  check(policy.network.webAccess.mode === 'ALLOWLIST_FULL' && Array.isArray(policy.network.webAccess.rules), 'WEB_ACCESS_CHANGED');
  for (const rule of policy.network.webAccess.rules) shape(rule, ['domainPattern', 'protocol', 'port', 'httpMethods']);
  check(['resourcesDigest', 'volumesDigest', 'networkDigest', 'rbacDigest'].every(key => digest(policy[key])), 'POLICY_DIGEST_INVALID');
}
function baseline(input) {
  const expected = input.expected, env = input.environment;
  shape(expected, [...scopeKeys, 'environmentRef', 'environmentVersion', 'baseVersionRef', 'baseRevision', 'baseDigest', 'environmentSHA256', 'intendedBindingsSHA256']);
  check(ref(expected.environmentRef) && version(expected.environmentVersion) && ref(expected.baseVersionRef) && version(expected.baseRevision) && digest(expected.baseDigest) && digest(expected.environmentSHA256) && digest(expected.intendedBindingsSHA256), 'EXPECTED_PIN_INVALID');
  publicEnvironment(env, expected);
  check(env.version === expected.environmentVersion && env.currentVersion.ref === expected.baseVersionRef && env.currentVersion.revision === expected.baseRevision && env.currentVersion.digest === expected.baseDigest && snapshotSHA256(env) === expected.environmentSHA256, 'ENVIRONMENT_PIN_CHANGED');
  const oldRules = [{ domainPattern: 'github.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD', 'POST'] }, { domainPattern: 'raw.githubusercontent.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD'] }];
  check(same(env.currentVersion.policy.network.webAccess.rules, oldRules), 'PREDECESSOR_WEB_ACCESS_CHANGED');
  check(Array.isArray(input.intendedBindings) && input.intendedBindings.length === 1 && snapshotSHA256(input.intendedBindings) === expected.intendedBindingsSHA256, 'INTENDED_BINDINGS_CHANGED');
  const bindingRefs = new Set(), consumers = new Set();
  for (const binding of input.intendedBindings) {
    shape(binding, bindingKeys); scope(binding, expected);
    check(ref(binding.consumerRef) && version(binding.consumerVersion) && ref(binding.bindingRef) && version(binding.bindingVersion) && binding.sourceRevisionRef === expected.baseVersionRef && !bindingRefs.has(binding.bindingRef) && !consumers.has(binding.consumerRef), 'BINDING_PIN_CHANGED');
    bindingRefs.add(binding.bindingRef); consumers.add(binding.consumerRef);
  }
  const body = environmentDraft(env, env.currentVersion.image.artifactRef);
  body.specification.policy.webAccess.rules.push(clone(githubAPIRule));
  return body;
}
function pinnedDraft(draft, input, body, states) {
  shape(draft, ['scopeKind', 'organizationRef', 'ref', 'version', 'projectRef', 'expectedEnvironmentVersion', 'state', 'specification', 'diagnostics'], ['environmentRef', 'baseVersionRef', 'baseRevision', 'savedAt', 'validationDigest', 'publishedEnvironmentRef']);
  scope(draft, input.expected);
  check(ref(draft.ref) && version(draft.version) && states.includes(draft.state) && draft.environmentRef === input.expected.environmentRef && draft.expectedEnvironmentVersion === input.expected.environmentVersion && draft.baseVersionRef === input.expected.baseVersionRef && draft.baseRevision === input.expected.baseRevision && same(draft.specification, canonicalDraftSpecification(body.specification)) && Array.isArray(draft.diagnostics) && draft.diagnostics.length === 0, 'DRAFT_PIN_CHANGED');
  if (draft.state === 'VALID' || draft.state === 'PUBLISHED') check(digest(draft.validationDigest), 'DRAFT_NOT_VALID');
}
function completeImpact(pages, input, draft, state, now) {
  check(Array.isArray(pages) && pages.length > 0 && pages.length <= 100, 'EOF_REQUIRED');
  const plan = pages[0]?.plan, tokens = new Set();
  shape(plan, ['ref', 'version', 'kind', 'sourceVersion', 'draftRef', 'draftVersion', 'targetDigest', 'digest', 'total', 'state', 'createdAt', 'expiresAt'], ['sourceRef', 'sourceRevisionRef', 'publishedRevisionRef']);
  check(ref(plan.ref) && version(plan.version) && digest(plan.digest) && plan.kind === 'RUNTIME_ENVIRONMENT' && plan.state === state && plan.sourceRef === input.expected.environmentRef && plan.sourceVersion === input.expected.environmentVersion && plan.sourceRevisionRef === input.expected.baseVersionRef && plan.draftRef === draft.ref && plan.targetDigest === draft.validationDigest && plan.total === input.intendedBindings.length, 'PLAN_PIN_CHANGED');
  check(state === 'APPLIED' ? plan.draftVersion < draft.version : plan.draftVersion === draft.version && Number.isFinite(Date.parse(plan.expiresAt)) && Date.parse(plan.expiresAt) > now, 'PLAN_STALE');
  for (const [index, page] of pages.entries()) {
    shape(page, ['plan', 'items', 'total'], ['nextPageToken']);
    check(same(page.plan, plan) && page.total === plan.total && Array.isArray(page.items) && page.items.length <= 100, 'PAGE_PIN_CHANGED');
    const token = page.nextPageToken;
    check(index === pages.length - 1 ? token === undefined || token === '' : typeof token === 'string' && token.length > 0 && token.length <= 512 && !tokens.has(token), 'EOF_REQUIRED');
    if (token) tokens.add(token);
    for (const item of page.items) {
      shape(item, ['ref', ...bindingKeys, 'consumerKind', 'outcome'], ['resultRevisionRef', 'resultBindingRef', 'resultBindingVersion', 'resultConsumerVersion']);
      scope(item, input.expected);
      check(ref(item.ref) && ref(item.consumerRef) && ref(item.bindingRef) && version(item.consumerVersion) && version(item.bindingVersion) && item.sourceRevisionRef === input.expected.baseVersionRef && item.consumerKind === 'AGENT' && (state === 'PREPARED' ? item.outcome === 'PENDING' : ['APPLIED', 'NOT_SELECTED', 'CONFLICT', 'FORBIDDEN'].includes(item.outcome)), 'IMPACT_ITEM_CHANGED');
    }
  }
  const items = pages.flatMap(page => page.items);
  check(items.length === plan.total && new Set(items.map(item => item.ref)).size === items.length && new Set(items.map(item => item.bindingRef)).size === items.length, 'INCOMPLETE_OR_DUPLICATE_PLAN');
  return { plan, items };
}
function request(path, body, status, input, expectedVersion) {
  const pins = { ...clone(input.expected), intendedBindings: clone(input.intendedBindings), ...(input.draft ? { draftSHA256: snapshotSHA256(input.draft) } : {}), ...(input.impactPages ? { impactPlanSHA256: snapshotSHA256(input.impactPages[0].plan), impactPagesSHA256: snapshotSHA256(input.impactPages) } : {}) };
  const headers = { 'Content-Type': 'application/json', 'Idempotency-Key': 'github-api-env-' + snapshotSHA256({ path, body, pins, expectedVersion }).slice(0, 48) };
  if (expectedVersion !== undefined) headers['If-Match'] = '"' + expectedVersion + '"';
  return { method: 'POST', path, headers, ...(body === undefined ? {} : { body }), expectedStatus: status, pins, effect: 'NOT_RUN' };
}
export function prepare(phase, input, now = Date.now()) {
  const extra = { draft: [], validate: ['draft'], impact: ['draft'], publication: ['draft', 'impactPages'], readback: ['draft', 'impactPages', 'publishedEnvironment', 'bindingReadbacks'] }[phase];
  check(extra !== undefined, 'PHASE_INVALID');
  shape(input, ['version', 'expected', 'environment', 'intendedBindings', ...extra]);
  check(input.version === 1, 'INPUT_VERSION_INVALID');
  const body = baseline(input), draft = input.draft;
  if (phase === 'draft') return [request('/api/v1/projects/' + encodeURIComponent(input.expected.projectRef) + '/runtime-environment-drafts', body, 201, input)];
  pinnedDraft(draft, input, body, phase === 'validate' ? ['DRAFT'] : phase === 'readback' ? ['PUBLISHED'] : ['VALID']);
  const path = '/api/v1/runtime-environment-drafts/' + encodeURIComponent(draft.ref);
  if (phase === 'validate' || phase === 'impact') return [request(path + (phase === 'validate' ? '/validation' : '/impact-plans'), undefined, phase === 'validate' ? 200 : 201, input, draft.version)];
  const { plan, items } = completeImpact(input.impactPages, input, draft, phase === 'readback' ? 'APPLIED' : 'PREPARED', now);
  if (phase === 'publication') return [request(path + '/publication', selectBindings(input.impactPages, input.intendedBindings), 200, input, draft.version)];
  const published = input.publishedEnvironment;
  publicEnvironment(published, input.expected);
  check(published.version === input.environment.version + 1 && published.currentVersion.revision === input.environment.currentVersion.revision + 1 && published.currentVersion.digest === draft.validationDigest && published.currentVersion.ref === plan.publishedRevisionRef && published.currentVersion.ref !== input.expected.baseVersionRef && draft.publishedEnvironmentRef === input.expected.environmentRef, 'PUBLICATION_READBACK_CHANGED');
  const normalized = clone(published);
  for (const key of ['ref', 'version', 'revision', 'digest', 'createdAt']) normalized.currentVersion[key] = input.environment.currentVersion[key];
  normalized.currentVersion.policy.network.webAccess.rules = normalized.currentVersion.policy.network.webAccess.rules.filter(rule => rule.domainPattern !== 'api.github.com');
  normalized.currentVersion.policy.networkDigest = input.environment.currentVersion.policy.networkDigest;
  check(same(normalized.currentVersion, input.environment.currentVersion) && same(published.currentVersion.policy.network.webAccess.rules, normalizedRules(body.specification.policy.webAccess.rules)) && published.name === input.environment.name && published.description === input.environment.description, 'PRESERVATION_READBACK_CHANGED');
  check(Array.isArray(input.bindingReadbacks) && input.bindingReadbacks.length === input.intendedBindings.length, 'BINDING_READBACK_INCOMPLETE');
  const selected = new Set(input.intendedBindings.map(binding => binding.bindingRef)), seen = new Set();
  for (const intended of input.intendedBindings) {
    const matches = items.filter(item => bindingKeys.every(key => item[key] === intended[key]));
    check(matches.length === 1 && matches[0].outcome === 'APPLIED', 'SELECTED_BINDING_NOT_APPLIED');
    const item = matches[0], views = input.bindingReadbacks.filter(view => view.environmentBinding?.agentRef === intended.consumerRef);
    check(views.length === 1 && !seen.has(intended.consumerRef), 'BINDING_READBACK_INCOMPLETE'); seen.add(intended.consumerRef);
    const view = views[0], binding = view.environmentBinding;
    shape(view, ['configuration', 'publishedOverlay', 'environmentBinding', 'environment', 'safeEffectiveConfig', 'agentVersion', 'skillBindings', 'memoryBindings', 'overlaySchema'], ['draftOverlay']);
    shape(binding, ['ref', 'version', 'agentRef', 'environmentRef', 'digest'], ['versionRef']);
    check(item.resultRevisionRef === published.currentVersion.ref && binding.environmentRef === input.expected.environmentRef && binding.versionRef === item.resultRevisionRef && binding.ref === item.resultBindingRef && binding.ref === intended.bindingRef && binding.version === item.resultBindingVersion && binding.version === intended.bindingVersion + 1 && view.agentVersion === item.resultConsumerVersion && view.agentVersion === intended.consumerVersion + 1 && binding.digest === bindingDigest(intended.consumerRef, input.expected.environmentRef, binding.version) && same(view.environment, published), 'BINDING_READBACK_CHANGED');
  }
  check(items.filter(item => !selected.has(item.bindingRef)).every(item => item.outcome === 'NOT_SELECTED' && !Object.hasOwn(item, 'resultRevisionRef') && !Object.hasOwn(item, 'resultBindingRef') && !Object.hasOwn(item, 'resultBindingVersion') && !Object.hasOwn(item, 'resultConsumerVersion')), 'UNSELECTED_BINDING_CHANGED');
  return { status: 'READBACK_VERIFIED_OFFLINE', environmentRef: input.expected.environmentRef, environmentVersion: published.version, publishedVersionRef: published.currentVersion.ref, bindings: seen.size, effect: 'NOT_RUN' };
}
export function main(args) {
  const phase = args.shift(), options = {};
  while (args.length) { const key = args.shift(); check(['--input', '--input-sha256', '--output'].includes(key) && !Object.hasOwn(options, key) && args.length > 0, 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  check(Object.keys(options).length === 3 && digest(options['--input-sha256']), 'ARGUMENT_REQUIRED');
  const fd = openSync(resolve(options['--input']), constants.O_RDONLY | constants.O_NOFOLLOW);
  let bytes;
  try { const stat = fstatSync(fd); check(stat.isFile() && stat.nlink === 1 && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o600 && stat.size > 0 && stat.size <= (8 << 20), 'PRIVATE_INPUT_INVALID'); bytes = readFileSync(fd); check(bytes.length <= (8 << 20), 'PRIVATE_INPUT_INVALID'); } finally { closeSync(fd); }
  check(bytesSHA256(bytes) === options['--input-sha256'], 'INPUT_DIGEST_CHANGED');
  const input = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes)), result = prepare(phase, input);
  const output = JSON.stringify({ version: 1, kind: 'GITHUB_API_ENVIRONMENT_OFFLINE_PLAN', phase, inputSHA256: options['--input-sha256'], result, effect: 'NOT_RUN' }) + '\n';
  check(Buffer.byteLength(output) <= (8 << 20), 'OUTPUT_TOO_LARGE');
  const path = resolve(options['--output']), parent = dirname(path), stat = lstatSync(parent);
  check(realpathSync(parent) === parent && stat.isDirectory() && stat.uid === process.getuid() && (stat.mode & 0o777) === 0o700, 'PRIVATE_DIRECTORY_REQUIRED');
  const out = openSync(path, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { writeFileSync(out, output); fsyncSync(out); } finally { closeSync(out); }
  const directory = openSync(parent, constants.O_RDONLY); try { fsyncSync(directory); } finally { closeSync(directory); }
  return { status: 'PREPARED_OFFLINE', phase, intents: Array.isArray(result) ? result.length : 0, outputSHA256: bytesSHA256(output), effect: 'NOT_RUN' };
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { process.stdout.write(JSON.stringify(main(process.argv.slice(2))) + '\n'); }
  catch (error) { process.stderr.write(JSON.stringify({ status: 'FAIL', code: /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'GITHUB_API_ENVIRONMENT_PLAN_FAILED' }) + '\n'); process.exitCode = 1; }
}
