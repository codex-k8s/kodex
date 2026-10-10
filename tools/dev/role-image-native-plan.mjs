// Чистые преобразования и offline CLI. План не выполняет HTTP или owner command.
import { createHash } from 'node:crypto';
import { closeSync, constants, fstatSync, fsyncSync, lstatSync, openSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, posix, resolve } from 'node:path';
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
export function organizationSourceJSON(source, dockerfile) {
  const environment = source.roleImage?.environment;
  check(shape(source, ['name', 'roleImage']) && shape(source.roleImage, ['roleDefinitionRef', 'environment']) && shape(environment, ['environmentKey', 'dockerfile', 'installationBlock', 'packageKeys', 'toolKeys']), 'ORGANIZATION_SOURCE_UNREPRESENTABLE');
  optionalEnvironmentSettings(environment, true);
  // Server-owned reconstruction json.Marshal(map) сохраняет null Go slices,
  // но превратит []/отсутствие в null: такой исходник здесь закрыто отвергается.
  check(['packageKeys', 'toolKeys'].every(key => environment[key] === null || environment[key].length > 0), 'ORGANIZATION_SOURCE_UNREPRESENTABLE');
  const canonical = { name: source.name, roleImage: { environment: { dockerfile, environmentKey: environment.environmentKey, installationBlock: environment.installationBlock, packageKeys: environment.packageKeys, toolKeys: environment.toolKeys }, roleDefinitionRef: source.roleImage.roleDefinitionRef } };
  // Go encoding/json сортирует map keys и экранирует HTML/U+2028/U+2029.
  return JSON.stringify(canonical).replace(/[<>&\u2028\u2029]/g, value => '\\u' + value.charCodeAt(0).toString(16).padStart(4, '0'));
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
  check(environment.state === 'ACTIVE' && environment.ready === true && Array.isArray(environment.readinessBlockers) && environment.readinessBlockers.length === 0 && Number.isSafeInteger(environment.version) && environment.version > 0 && current?.ref && current.revision > 0 && typeof artifactRef === 'string' && artifactRef.length > 0, 'ENVIRONMENT_INVALID');
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
// Public ImageToolObservation и ImageToolProbes: закрытый профиль v1, не capability.
const inventoryProbeNames = Object.freeze(['bash', 'curl', 'git', 'gh', 'jq', 'yq', 'ripgrep', 'make', 'just', 'go', 'goimports', 'gofumpt', 'golangci-lint', 'staticcheck', 'goose', 'sqlc', 'buf', 'protoc', 'protoc-gen-go', 'protoc-gen-go-grpc', 'grpcurl', 'mockgen', 'oapi-codegen', 'node', 'npm', 'pnpm', 'yarn', 'typescript', 'eslint', 'prettier', 'vite', 'vue-tsc', 'vitest', 'playwright', 'chromium', 'playwright-mcp', 'wscat', 'codex', 'corepack', 'python3', 'pip', 'kubectl', 'kustomize', 'helm', 'buildctl', 'docker', 'shellcheck', 'hadolint', 'govulncheck', 'gitleaks']);
function inventoryProbePaths(name) {
  if (name === 'chromium') return ['/usr/lib/chromium/chromium', '/usr/local/bin/chromium', '/usr/local/bin/chromium-browser', '/usr/local/bin/google-chrome', '/usr/bin/chromium', '/usr/bin/chromium-browser', '/usr/bin/google-chrome'];
  if (name === 'playwright-mcp') return ['/usr/local/bin/playwright-mcp', '/usr/local/bin/mcp-server-playwright', '/usr/bin/playwright-mcp', '/usr/bin/mcp-server-playwright'];
  const command = name === 'ripgrep' ? 'rg' : name === 'typescript' ? 'tsc' : name;
  return [...(name === 'bash' ? ['/bin/bash'] : name === 'go' ? ['/usr/local/go/bin/go'] : []), '/usr/local/bin/' + command, '/usr/bin/' + command];
}
export function verifiedArtifactCommands(artifact) {
  const inventory = artifact.verifiedToolInventory;
  check(shape(inventory, ['status', 'sha256', 'imageDigest', 'provenanceSha256', 'platforms']) && inventory.status === 'VERIFIED' && digest(inventory.sha256) && inventory.imageDigest === artifact.manifestDigest && inventory.provenanceSha256 === artifact.provenanceSha256 && Array.isArray(inventory.platforms) && inventory.platforms.length >= 1 && inventory.platforms.length <= 2, 'INVENTORY_BINDING_INVALID');
  const platforms = new Set(); let common;
  for (const platform of inventory.platforms) {
    check(shape(platform, ['platform', 'platformDigest', 'manifestSha256', 'tools']) && ['linux/amd64', 'linux/arm64'].includes(platform.platform) && !platforms.has(platform.platform) && /^sha256:[a-f0-9]{64}$/.test(platform.platformDigest ?? '') && digest(platform.manifestSha256) && Array.isArray(platform.tools) && platform.tools.length === inventoryProbeNames.length, 'INVENTORY_PLATFORM_INVALID');
    platforms.add(platform.platform); const commands = new Set();
    for (const [index, tool] of platform.tools.entries()) {
      check(shape(tool, ['name', 'status', 'path', 'version', 'sha256', 'required']) && tool.name === inventoryProbeNames[index] && tool.required === (index < 38) && ['VERIFIED', 'MISSING', 'PROBE_FAILED'].includes(tool.status), 'INVENTORY_OBSERVATION_INVALID');
      if (tool.status === 'MISSING') { check(tool.path === '' && tool.version === '' && tool.sha256 === '', 'INVENTORY_OBSERVATION_INVALID'); continue; }
      check(inventoryProbePaths(tool.name).includes(tool.path) && digest(tool.sha256), 'INVENTORY_OBSERVATION_INVALID');
      if (tool.status === 'PROBE_FAILED') { check(tool.version === '', 'INVENTORY_OBSERVATION_INVALID'); continue; }
      check(typeof tool.version === 'string' && tool.version.length <= 80 && /^v?[0-9]+(\.[0-9]+){0,3}([-+][A-Za-z0-9.-]{1,48})?$/.test(tool.version), 'INVENTORY_OBSERVATION_INVALID');
      commands.add(posix.basename(tool.path));
    }
    common = common === undefined ? commands : new Set([...common].filter(command => commands.has(command)));
  }
  return common;
}
const enc = encodeURIComponent;
const scopeKeys = ['organizationRef', 'scopeKind', 'projectRef'];
function cohort(scopes, organizationCount) {
  check(scopes.filter(s => s.scopeKind === 'ORGANIZATION').length === organizationCount && new Set(scopes.map(s => s.organizationRef)).size === 1 && new Set(scopes.filter(s => s.scopeKind === 'PROJECT').map(s => s.projectRef)).size === 1, 'COHORT_SCOPE_CHANGED');
}
function sameScope(actual, expected) {
  check(ref(expected.organizationRef) && ['PROJECT', 'ORGANIZATION'].includes(expected.scopeKind) && (expected.scopeKind === 'PROJECT' ? ref(expected.projectRef) : expected.projectRef === ''), 'SCOPE_INVALID');
  check(actual.organizationRef === expected.organizationRef && actual.scopeKind === expected.scopeKind && (expected.scopeKind === 'ORGANIZATION' ? actual.projectRef === undefined || actual.projectRef === '' : actual.projectRef === expected.projectRef), 'SCOPE_CHANGED');
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
function request(path, body, expectedStatus, expectedVersion, pins, method = 'POST') {
  const headers = { 'Content-Type': 'application/json', 'Idempotency-Key': 'complex-' + sha({ method, path, body, expectedVersion, pins }).slice(0, 48) };
  if (expectedVersion !== undefined) headers['If-Match'] = '"' + expectedVersion + '"';
  return { method, path, headers, body, bodySHA256: sha(body), expectedStatus, pins, effect: 'NOT_RUN' };
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
    if (e.scopeKind === 'ORGANIZATION') {
      // ORG typed UPDATE атомарно записывает canonical source и запускает build.
      // Его native API не принимает raw JSON: неизвестные поля нельзя молча потерять.
      check(source.name === recipe.name && typeof recipe.name === 'string' && Buffer.byteLength(recipe.name) <= 160, 'ORGANIZATION_SOURCE_UNREPRESENTABLE');
      const selection = { ...clone(environment), dockerfile: JSON.parse(changed.content).roleImage.environment.dockerfile };
      const expectedSource = organizationSourceJSON(source, selection.dockerfile);
      for (const key of ['packageKeys', 'toolKeys']) if (selection[key] === null) delete selection[key];
      const body = { name: recipe.name, environment: selection };
      return request('/api/v1/organization/role-image-recipes/' + enc(recipe.ref), body, 200, recipe.version, { ...e, recipeVersion: recipe.version, recipeGeneration: recipe.generation, previousContentSHA256: r.digest, nextContentSHA256: sha(expectedSource), expectedPublishedSource: JSON.parse(expectedSource), trustedBaseImage: match[1], catalogSHA256: sha(catalog[0]), continuation: 'ATOMIC_MANAGED_PUBLICATION_AUTOBUILD_SOURCE_READBACK_HUMAN_RISK_IF_REQUIRED_PROMOTE' }, 'PATCH');
    }
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
    check(detail.recipe.ref === e.recipeRef && detail.recipe.state === 'ACTIVE' && version(detail.recipe.generation) && detail.recipe.managedLineage?.revisionRef === e.publishedRevisionRef && detail.recipe.promotedImageReady === true && detail.recipe.activeImageArtifactRef === artifact.ref && detail.recipe.promotedImageReference === artifact.promotedReference && ref(artifact.ref) && version(artifact.version) && ref(artifact.buildRef) && artifact.recipeRef === e.recipeRef && artifact.recipeGeneration === detail.recipe.generation && artifact.admissionVerdict === 'ACCEPTED' && artifact.promotionState === 'PROMOTED' && typeof artifact.promotionRequested === 'boolean' && base(artifact.promotedReference) && artifact.promotedReference.endsWith('@' + artifact.manifestDigest) && digest(artifact.provenanceSha256) && digest(artifact.promotionReceiptSha256) && Number.isFinite(Date.parse(artifact.promotedAt)) && Array.isArray(artifact.declaredTools) && artifact.declaredTools.length <= 128, 'ARTIFACT_NOT_READY');
    check(artifact.ref !== env.currentVersion.image.artifactRef && Array.isArray(entry.bindings) && entry.bindings.length > 0 && entry.bindings.length <= 1000, 'UNCHANGED_OR_MISSING_BINDINGS');
    const commands = verifiedArtifactCommands(artifact);
    check(Array.isArray(env.currentVersion.tools) && new Set(env.currentVersion.tools.map(tool => tool.command)).size === env.currentVersion.tools.length && env.currentVersion.tools.every(tool => commands.has(tool.command)), 'VERIFIED_TOOLS_MISSING');
    const body = environmentDraft(env, artifact.ref);
    for (const b of entry.bindings) { sameScope(b, e); check(ref(b.consumerRef) && version(b.consumerVersion) && ref(b.bindingRef) && version(b.bindingVersion) && b.sourceRevisionRef === e.baseVersionRef, 'BINDING_PIN_CHANGED'); }
    const path = e.scopeKind === 'ORGANIZATION' ? '/api/v1/organization/runtime-environment-drafts' : '/api/v1/projects/' + enc(e.projectRef) + '/runtime-environment-drafts';
    return request(path, body, 201, undefined, { ...e, artifactRef: artifact.ref, artifactVersion: artifact.version, artifactDigest: artifact.manifestDigest, artifactBuildRef: artifact.buildRef, artifactProvenanceSHA256: artifact.provenanceSha256, artifactPromotionReceiptSHA256: artifact.promotionReceiptSha256, inventorySHA256: artifact.verifiedToolInventory.sha256, inventoryProjectionSHA256: sha(artifact.verifiedToolInventory), inventoryPlatformPins: artifact.verifiedToolInventory.platforms.map(({ platform, platformDigest, manifestSha256 }) => ({ platform, platformDigest, manifestSha256 })), unchangedEnvironmentSHA256: environmentPreservationSHA256(env), intendedBindings: clone(entry.bindings), continuation: 'VALIDATE_PREPUBLICATION_IMPACT_EOF_SELECT_PUBLISH_READBACK' });
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
