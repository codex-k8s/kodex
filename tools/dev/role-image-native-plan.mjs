// Чистые преобразования и offline CLI. План не выполняет HTTP или owner command.
import { createHash } from 'node:crypto';
import { closeSync, constants, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const fail = code => { throw new Error(code); };
const check = (ok, code) => { if (!ok) fail(code); };
const clone = value => JSON.parse(JSON.stringify(value));
const sha = value => createHash('sha256').update(typeof value === 'string' || Buffer.isBuffer(value) ? value : JSON.stringify(value)).digest('hex');
const base = value => /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(value ?? '');
function optionalEnvironmentSettings(environment, source) {
  // Go source []string допускает null/отсутствие/[], public projection опускает пустые поля.
  check(environment && typeof environment === 'object' && !Array.isArray(environment), 'SOURCE_RECIPE_MISMATCH');
  const installationBlock = environment.installationBlock === undefined ? '' : environment.installationBlock;
  check(typeof installationBlock === 'string' && Buffer.byteLength(installationBlock) <= 65536 && !installationBlock.includes('\0'), 'SOURCE_RECIPE_MISMATCH');
  const keys = (field, limit) => {
    const value = environment[field] === undefined || source && environment[field] === null ? [] : environment[field];
    check(Array.isArray(value) && value.length <= limit && value.every(key => typeof key === 'string' && key.length <= 100 && /^[a-z][a-z0-9.-]*$/.test(key)) && new Set(value).size === value.length, 'SOURCE_RECIPE_MISMATCH');
    return value;
  };
  return { installationBlock, packageKeys: keys('packageKeys', 256), toolKeys: keys('toolKeys', 128) };
}
export function recipeContent(content, previousBase, trustedBase) {
  check(typeof content === 'string' && Buffer.byteLength(content) <= 262144 && base(previousBase) && base(trustedBase) && previousBase !== trustedBase, 'INPUT_INVALID');
  const old = JSON.parse(content), next = clone(old), e = next.roleImage?.environment;
  check(typeof e?.dockerfile === 'string' && !/[\r\0]/.test(e.dockerfile), 'SOURCE_INVALID');
  let from = 0;
  const lines = e.dockerfile.split('\n').map(line => {
    const trimmed = line.trim();
    check(!/^#\s*syntax\s*=/i.test(trimmed), 'SOURCE_INVALID');
    if (trimmed === '' || trimmed.startsWith('#')) return line;
    if (/^FROM\s/i.test(trimmed)) {
      check(++from === 1 && !trimmed.endsWith('\\'), 'MULTISTAGE_UNSUPPORTED');
      const match = /^(\s*FROM\s+)(\S+)(\s+AS\s+\S+)?(\s*)$/i.exec(line);
      check(match && match[2] === previousBase, 'PREDECESSOR_BASE_CHANGED');
      return match[1] + trustedBase + (match[3] ?? '') + match[4];
    }
    check(from === 1, 'PRE_FROM_INSTRUCTION_UNSUPPORTED');
    return line;
  });
  check(from === 1, 'SOURCE_INVALID');
  e.dockerfile = lines.join('\n');
  const result = JSON.stringify(next);
  check(Buffer.byteLength(result) <= 262144, 'INPUT_INVALID');
  return { content: result, previousContentSHA256: sha(content), nextContentSHA256: sha(result), unchangedRoleSettingsSHA256: sha({ ...old.roleImage, environment: { ...old.roleImage.environment, dockerfile: null } }) };
}
export function environmentDraft(environment, artifactRef, expectedToolCount = 38) {
  const current = environment.currentVersion, policy = current?.policy;
  check(environment.state === 'ACTIVE' && environment.enabled === true && Number.isSafeInteger(environment.version) && environment.version > 0 && current?.ref && current.revision > 0 && typeof artifactRef === 'string' && artifactRef.length > 0, 'ENVIRONMENT_INVALID');
  check(current.tools?.length === expectedToolCount && Array.isArray(current.values) && Array.isArray(current.secretDescriptors), 'ENVIRONMENT_FIELDS_MISSING');
  check(policy?.kubernetesAccess?.kind === 'NONE' && policy.network?.denyByDefault === true && Array.isArray(policy.volumes) && policy.network.webAccess, 'POLICY_UNSUPPORTED');
  const destinations = [...new Set(policy.network.egress.map(item => item.destination))].sort();
  check(JSON.stringify(destinations) === JSON.stringify(['DNS', 'PROVIDER_PROXY', 'RUNTIME_CALLBACK']), 'POLICY_UNSUPPORTED');
  const secretBindings = current.secretDescriptors.map(({ name, secretRef, revision }) => {
    check(name && secretRef && Number.isSafeInteger(revision) && revision > 0, 'SECRET_PIN_INVALID');
    return { name, secretRef, revision };
  });
  const specification = clone({ name: environment.name, description: environment.description, imageArtifactRef: artifactRef, tools: current.tools, values: current.values, secretBindings,
    policy: { resources: policy.resources, volumes: policy.volumes.map(({ name, kind, sizeMib }) => ({ name, kind, sizeMib })), networkDestinations: ['DNS', 'RUNTIME_CALLBACK', 'PROVIDER_PROXY'], kubernetesAccess: 'NONE', webAccess: policy.network.webAccess } });
  return { environmentRef: environment.ref, expectedEnvironmentVersion: environment.version, specification };
}
export function environmentPreservationSHA256(environment) {
  const current = environment.currentVersion;
  // Новый образ меняет revision/ref/createdAt/digest, но не эти входные настройки.
  return sha({ name: environment.name, description: environment.description, tools: current.tools, values: current.values, secretDescriptors: current.secretDescriptors, policy: current.policy });
}
export function selectBindings(pages, intended) {
  check(Array.isArray(pages) && pages.length > 0 && (pages.at(-1).nextPageToken === undefined || pages.at(-1).nextPageToken === ''), 'EOF_REQUIRED');
  const plan = pages[0].plan;
  check(plan?.state === 'PREPARED' && plan.kind === 'RUNTIME_ENVIRONMENT' && plan.total <= 1000 && Array.isArray(intended) && intended.length > 0, 'PLAN_INVALID');
  const items = pages.flatMap(page => { check(page.plan.ref === plan.ref && page.plan.digest === plan.digest && Array.isArray(page.items), 'PLAN_CHANGED'); return page.items; });
  check(items.length === plan.total && new Set(items.map(item => item.ref)).size === items.length && new Set(intended.map(item => item.bindingRef)).size === intended.length, 'INCOMPLETE_OR_DUPLICATE_PLAN');
  const keys = ['consumerRef', 'consumerVersion', 'bindingRef', 'bindingVersion', 'sourceRevisionRef', 'projectRef', 'scopeKind', 'organizationRef'];
  const selectedItemRefs = intended.map(expected => {
    check(keys.every(key => Object.hasOwn(expected, key)), 'BINDING_PIN_MISSING');
    const found = items.filter(item => item.consumerKind === 'AGENT' && item.outcome === 'PENDING' && keys.every(key => item[key] === expected[key]));
    check(found.length === 1, 'EXACT_BINDING_MISSING');
    return found[0].ref;
  });
  return { planRef: plan.ref, selectedItemRefs };
}

const ref = value => typeof value === 'string' && /^[A-Za-z0-9_-]{1,160}$/.test(value);
const digest = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
const version = value => Number.isSafeInteger(value) && value > 0;
const shape = (value, keys) => value && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).sort().join() === [...keys].sort().join();
const enc = encodeURIComponent;
const scopeKeys = ['organizationRef', 'scopeKind', 'projectRef'];
function cohort(scopes, organizationCount) {
  check(scopes.filter(s => s.scopeKind === 'ORGANIZATION').length === organizationCount && new Set(scopes.map(s => s.organizationRef)).size === 1 && new Set(scopes.filter(s => s.scopeKind === 'PROJECT').map(s => s.projectRef)).size === 1, 'COHORT_SCOPE_CHANGED');
}
function sameScope(actual, expected) {
  check(ref(expected.organizationRef) && ['PROJECT', 'ORGANIZATION'].includes(expected.scopeKind) && (expected.scopeKind === 'PROJECT' ? ref(expected.projectRef) : expected.projectRef === ''), 'SCOPE_INVALID');
  check(scopeKeys.every(key => actual[key] === expected[key]), 'SCOPE_CHANGED');
}
function completePages(pages, key) {
  check(Array.isArray(pages) && pages.length > 0 && pages.length <= 100, 'PAGES_INVALID');
  const tokens = new Set();
  for (let i = 0; i < pages.length; i++) {
    const page = pages[i], token = page.nextPageToken;
    check(Array.isArray(page.items) && page.items.length <= 100 && page[key]?.ref === pages[0][key]?.ref && sha(page[key]) === sha(pages[0][key]), 'PAGE_PIN_CHANGED');
    check(i === pages.length - 1 ? token === undefined || token === '' : typeof token === 'string' && token !== '' && !tokens.has(token), 'EOF_REQUIRED');
    if (token) tokens.add(token);
  }
  const items = pages.flatMap(page => page.items);
  const total = key === 'plan' ? pages[0].plan.total : pages[0].total;
  check(Number.isSafeInteger(total) && total >= 0 && items.length === total && new Set(items.map(item => item.ref)).size === items.length, 'INCOMPLETE_OR_DUPLICATE_PLAN');
  return items;
}
function request(path, body, expectedStatus, expectedVersion, pins) {
  const headers = { 'Content-Type': 'application/json', 'Idempotency-Key': 'complex-' + sha({ path, body, expectedVersion, pins }).slice(0, 48) };
  if (expectedVersion !== undefined) headers['If-Match'] = '"' + expectedVersion + '"';
  return { method: 'POST', path, headers, body, bodySHA256: sha(body), expectedStatus, pins, effect: 'NOT_RUN' };
}
export function prepareRecipes(input) {
  check(shape(input, ['version', 'catalog', 'runnerDigest', 'recipes']) && input.version === 1 && /^sha256:[a-f0-9]{64}$/.test(input.runnerDigest ?? '') && input.recipes?.length === 3, 'INPUT_INVALID');
  check(Array.isArray(input.catalog?.items), 'CATALOG_INVALID');
  cohort(input.recipes.map(entry => entry.expected), 1);
  const configs = new Set(), recipes = new Set();
  return input.recipes.map(entry => {
    check(shape(entry, ['expected', 'detail', 'historyPages']), 'INPUT_INVALID');
    const e = entry.expected, recipe = entry.detail?.recipe;
    check(shape(e, [...scopeKeys, 'recipeRef', 'configurationRef', 'publishedRevisionRef']) && ref(e.recipeRef) && ref(e.configurationRef) && ref(e.publishedRevisionRef), 'PIN_INVALID');
    sameScope(recipe, e);
    check(recipe.ref === e.recipeRef && recipe.state === 'ACTIVE' && recipe.sourceAvailable === true && version(recipe.version) && version(recipe.generation) && recipe.managedLineage?.managedBy === 'UI' && recipe.managedLineage.configurationRef === e.configurationRef && recipe.managedLineage.revisionRef === e.publishedRevisionRef, 'RECIPE_PIN_CHANGED');
    check(!configs.has(e.configurationRef) && !recipes.has(e.recipeRef), 'DUPLICATE_RESOURCE'); configs.add(e.configurationRef); recipes.add(e.recipeRef);
    const all = completePages(entry.historyPages, 'configuration'), c = entry.historyPages[0].configuration;
    check(c.ref === e.configurationRef && c.kind === 'ROLE_IMAGE' && c.managedBy === 'UI' && c.archived === false && c.sourceEditable === true && version(c.version) && (c.projectRef ?? '') === e.projectRef && c.currentRevision?.ref === e.publishedRevisionRef, 'CONFIGURATION_PIN_CHANGED');
    check(!all.some(r => ['DRAFT', 'VALID', 'INVALID'].includes(r.state)), 'EXISTING_DRAFT_REQUIRES_OWNER');
    const r = all.find(r => r.ref === e.publishedRevisionRef);
    check(r?.state === 'PUBLISHED' && r.contentFormat === 'JSON' && r.sourceAvailable === true && typeof r.content === 'string' && sha(r.content) === r.digest, 'SOURCE_UNAVAILABLE');
    const source = JSON.parse(r.content), environment = source.roleImage?.environment;
    check(source.roleImage?.roleDefinitionRef === recipe.roleDefinitionRef && environment?.environmentKey === recipe.environment?.environmentKey && environment.dockerfile === recipe.environment.dockerfile && sha(optionalEnvironmentSettings(environment, true)) === sha(optionalEnvironmentSettings(recipe.environment, false)), 'SOURCE_RECIPE_MISMATCH');
    const catalog = input.catalog.items.filter(item => item.key === environment.environmentKey);
    check(catalog.length === 1 && catalog[0].available === true, 'CATALOG_INVALID');
    const match = /^FROM ([a-z0-9][a-z0-9./:_-]*@(sha256:[a-f0-9]{64}))\n$/.exec(catalog[0].dockerfileTemplate ?? '');
    check(match && match[2] === input.runnerDigest, 'TRUSTED_BASE_CHANGED');
    const previous = /^\s*FROM\s+(\S+)/im.exec(environment.dockerfile);
    check(previous, 'SOURCE_INVALID');
    const changed = recipeContent(r.content, previous[1], match[1]);
    const body = { configurationRef: c.ref, name: c.name, contentFormat: 'JSON', content: changed.content, ...(e.scopeKind === 'PROJECT' ? { projectRef: e.projectRef } : {}) };
    return request('/api/v1/role-image-configurations/drafts', body, 201, c.version, { ...e, recipeVersion: recipe.version, recipeGeneration: recipe.generation, previousContentSHA256: r.digest, nextContentSHA256: changed.nextContentSHA256, trustedBaseImage: match[1], catalogSHA256: sha(catalog[0]), continuation: 'VALIDATE_PUBLISH_AUTOBUILD_HUMAN_RISK_IF_REQUIRED_PROMOTE' });
  });
}
export function prepareEnvironments(input) {
  check(shape(input, ['version', 'environments']) && input.version === 1 && input.environments?.length === 4, 'INPUT_INVALID');
  cohort(input.environments.map(entry => entry.expected), 1);
  const refs = new Set();
  return input.environments.map(entry => {
    check(shape(entry, ['expected', 'environment', 'artifactDetail', 'bindings']), 'INPUT_INVALID');
    const e = entry.expected, env = entry.environment, detail = entry.artifactDetail, artifact = detail?.activeArtifact;
    check(shape(e, [...scopeKeys, 'environmentRef', 'environmentVersion', 'baseVersionRef', 'recipeRef', 'publishedRevisionRef']), 'PIN_INVALID');
    sameScope(env, e); sameScope(detail.recipe, e); sameScope(artifact, e);
    check(env.ref === e.environmentRef && env.version === e.environmentVersion && env.currentVersion?.ref === e.baseVersionRef && !refs.has(env.ref), 'ENVIRONMENT_PIN_CHANGED'); refs.add(env.ref);
    check(detail.recipe.ref === e.recipeRef && detail.recipe.managedLineage?.revisionRef === e.publishedRevisionRef && detail.recipe.promotedImageReady === true && artifact.recipeRef === e.recipeRef && artifact.recipeGeneration === detail.recipe.generation && artifact.admissionVerdict === 'ACCEPTED' && artifact.promotionState === 'PROMOTED' && base(artifact.promotedReference) && artifact.promotedReference.endsWith('@' + artifact.manifestDigest) && digest(artifact.provenanceSha256) && digest(artifact.promotionReceiptSha256) && artifact.verifiedToolInventory?.status === 'VERIFIED', 'ARTIFACT_NOT_READY');
    check(artifact.ref !== env.currentVersion.image.artifactRef && Array.isArray(entry.bindings) && entry.bindings.length > 0 && entry.bindings.length <= 1000, 'UNCHANGED_OR_MISSING_BINDINGS');
    const names = new Set(artifact.declaredTools?.map(tool => tool.name));
    check(env.currentVersion.tools.every(tool => names.has(tool.command)), 'DECLARED_TOOLS_MISSING');
    const body = environmentDraft(env, artifact.ref);
    for (const b of entry.bindings) { sameScope(b, e); check(ref(b.consumerRef) && version(b.consumerVersion) && ref(b.bindingRef) && version(b.bindingVersion) && b.sourceRevisionRef === e.baseVersionRef, 'BINDING_PIN_CHANGED'); }
    const path = e.scopeKind === 'ORGANIZATION' ? '/api/v1/organization/runtime-environment-drafts' : '/api/v1/projects/' + enc(e.projectRef) + '/runtime-environment-drafts';
    return request(path, body, 201, undefined, { ...e, artifactRef: artifact.ref, artifactDigest: artifact.manifestDigest, unchangedEnvironmentSHA256: environmentPreservationSHA256(env), intendedBindings: clone(entry.bindings), continuation: 'VALIDATE_PREPUBLICATION_IMPACT_EOF_SELECT_PUBLISH_READBACK' });
  });
}
export function prepareSelections(input, now = Date.now()) {
  check(shape(input, ['version', 'selections']) && input.version === 1 && input.selections?.length === 4, 'INPUT_INVALID');
  cohort(input.selections.map(entry => entry.draft), 1);
  const drafts = new Set(), environments = new Set(), bindings = new Set();
  return input.selections.map(entry => {
    check(shape(entry, ['draft', 'pages', 'intendedBindings']), 'INPUT_INVALID');
    const draft = entry.draft, plan = entry.pages?.[0]?.plan;
    const items = completePages(entry.pages, 'plan');
    check(draft.state === 'VALID' && ref(draft.ref) && version(draft.version) && digest(draft.validationDigest) && !drafts.has(draft.ref), 'DRAFT_NOT_VALID'); drafts.add(draft.ref);
    check(plan.state === 'PREPARED' && plan.draftRef === draft.ref && plan.draftVersion === draft.version && plan.sourceRef === draft.environmentRef && plan.sourceVersion === draft.expectedEnvironmentVersion && plan.sourceRevisionRef === draft.baseVersionRef && plan.targetDigest === draft.validationDigest && digest(plan.digest) && Number.isFinite(Date.parse(plan.expiresAt)) && Date.parse(plan.expiresAt) > now, 'PLAN_STALE');
    check(!environments.has(draft.environmentRef), 'DUPLICATE_RESOURCE'); environments.add(draft.environmentRef);
    for (const item of items) sameScope(item, draft);
    for (const b of entry.intendedBindings) { sameScope(b, draft); check(!bindings.has(b.bindingRef), 'DUPLICATE_RESOURCE'); bindings.add(b.bindingRef); }
    const body = selectBindings(entry.pages, entry.intendedBindings);
    return request('/api/v1/runtime-environment-drafts/' + enc(draft.ref) + '/publication', body, 200, draft.version, { draftRef: draft.ref, draftVersion: draft.version, planRef: plan.ref, planDigest: plan.digest, intendedBindings: clone(entry.intendedBindings), continuation: 'FULL_EOF_OUTCOMES_AND_EXACT_ENVIRONMENT_BINDING_READBACK' });
  });
}

function readPrivate(path) {
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const s = fstatSync(fd);
    check(s.isFile() && s.nlink === 1 && s.uid === process.getuid() && (s.mode & 0o777) === 0o600 && s.size > 0 && s.size <= (8 << 20), 'PRIVATE_INPUT_INVALID');
    const bytes = readFileSync(fd); check(bytes.length <= (8 << 20), 'PRIVATE_INPUT_INVALID'); return bytes;
  } finally { closeSync(fd); }
}
export function main(args) {
  const phase = args.shift(), options = {};
  check(['recipes', 'environments', 'selections'].includes(phase), 'PHASE_INVALID');
  while (args.length) { const key = args.shift(); check(['--input', '--input-sha256', '--output'].includes(key) && !Object.hasOwn(options, key) && args.length > 0, 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  check(Object.keys(options).length === 3 && digest(options['--input-sha256']), 'ARGUMENT_REQUIRED');
  const bytes = readPrivate(resolve(options['--input'])); check(sha(bytes) === options['--input-sha256'], 'INPUT_DIGEST_CHANGED');
  const input = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
  const requests = { recipes: prepareRecipes, environments: prepareEnvironments, selections: prepareSelections }[phase](input);
  const body = JSON.stringify({ version: 1, kind: 'ROLE_IMAGE_NATIVE_OFFLINE_PLAN', phase, inputSHA256: options['--input-sha256'], requests, effect: 'NOT_RUN', humanRiskDecision: 'NOT_AUTOMATED' }) + '\n';
  check(Buffer.byteLength(body) <= (8 << 20), 'OUTPUT_TOO_LARGE');
  const path = resolve(options['--output']), parent = dirname(path), info = lstatSync(parent);
  check(realpathSync(parent) === parent && info.isDirectory() && info.uid === process.getuid() && (info.mode & 0o777) === 0o700, 'PRIVATE_DIRECTORY_REQUIRED');
  const fd = openSync(path, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { writeFileSync(fd, body); fsyncSync(fd); } finally { closeSync(fd); }
  const directory = openSync(parent, constants.O_RDONLY); try { fsyncSync(directory); } finally { closeSync(directory); }
  return { status: 'PREPARED_OFFLINE', phase, intents: requests.length, outputSHA256: sha(body), effect: 'NOT_RUN', humanRiskDecision: 'NOT_AUTOMATED' };
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { process.stdout.write(JSON.stringify(main(process.argv.slice(2))) + '\n'); }
  catch (error) { process.stderr.write(JSON.stringify({ status: 'FAIL', code: /^[A-Z][A-Z0-9_]{1,80}$/.test(error?.message ?? '') ? error.message : 'NATIVE_PLAN_FAILED' }) + '\n'); process.exitCode = 1; }
}
