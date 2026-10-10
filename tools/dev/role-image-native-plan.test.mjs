import test from 'node:test';
import assert from 'node:assert/strict';
import { recipeContent, organizationSourceJSON, environmentDraft, environmentPreservationSHA256, verifiedArtifactCommands, selectBindings, prepareRecipes, prepareEnvironments, prepareSelections } from './role-image-native-plan.mjs';
import { createHash } from 'node:crypto';
import { chmodSync, lstatSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnSync } from 'node:child_process';
const prior = 'pull.fixture.invalid/runner@sha256:' + 'a'.repeat(64), next = 'pull.fixture.invalid/runner@sha256:' + 'b'.repeat(64);
const document = { name: 'Synthetic', roleImage: { roleDefinitionRef: 'role_fixture', environment: { environmentKey: 'standard', packageKeys: ['git'], toolKeys: ['git'], installationBlock: 'RUN setup-synthetic', dockerfile: '# fixture\nFROM ' + prior + ' AS runner\nUSER root\nRUN printf synthetic\nUSER 1000\n' } } };
test('Сложный Dockerfile и все role settings сохраняются, меняется только FROM', () => {
  const result = recipeContent(JSON.stringify(document), prior, next), actual = JSON.parse(result.content);
  assert.equal(actual.roleImage.environment.dockerfile, document.roleImage.environment.dockerfile.replace(prior, next));
  actual.roleImage.environment.dockerfile = document.roleImage.environment.dockerfile;
  assert.deepEqual(actual, document);
});
for (const dockerfile of ['FROM ' + prior + '\nFROM ' + prior + '\n', 'ARG BASE=x\nFROM ' + prior + '\n', '#syntax=foreign\nFROM ' + prior + '\n', 'FROM ' + next + '\n']) test('Неподдерживаемый источник закрыто отвергается', () => {
  const changed = structuredClone(document); changed.roleImage.environment.dockerfile = dockerfile;
  assert.throws(() => recipeContent(JSON.stringify(changed), prior, next));
});
const probeNames = ['bash', 'curl', 'git', 'gh', 'jq', 'yq', 'ripgrep', 'make', 'just', 'go', 'goimports', 'gofumpt', 'golangci-lint', 'staticcheck', 'goose', 'sqlc', 'buf', 'protoc', 'protoc-gen-go', 'protoc-gen-go-grpc', 'grpcurl', 'mockgen', 'oapi-codegen', 'node', 'npm', 'pnpm', 'yarn', 'typescript', 'eslint', 'prettier', 'vite', 'vue-tsc', 'vitest', 'playwright', 'chromium', 'playwright-mcp', 'wscat', 'codex', 'corepack', 'python3', 'pip', 'kubectl', 'kustomize', 'helm', 'buildctl', 'docker', 'shellcheck', 'hadolint', 'govulncheck', 'gitleaks'];
const commandFor = name => name === 'ripgrep' ? 'rg' : name === 'typescript' ? 'tsc' : name;
const env = () => ({ ref: 'renv_fixture', state: 'ACTIVE', ready: true, readinessBlockers: [], version: 7, name: 'Environment', description: 'Synthetic', currentVersion: { ref: 'renvv_fixture', revision: 4, tools: probeNames.slice(0,38).map((name, i) => ({ name: `tool${i}`, command: commandFor(name), description: 'Synthetic', usageHint: 'Synthetic' })), values: [{ name: 'MODE', value: 'synthetic' }], secretDescriptors: [{ name: 'TOKEN', secretRef: 'rsec_fixture', revision: 6, secretName: 'synthetic', secretUid: 'synthetic' }], policy: { resources: { cpuLimitMilli: 1234 }, volumes: [{ name: 'work', kind: 'EPHEMERAL_DISK', sizeMib: 444, mountPath: '/synthetic' }], network: { denyByDefault: true, egress: ['DNS', 'DNS', 'RUNTIME_CALLBACK', 'PROVIDER_PROXY'].map(destination => ({ destination })), webAccess: { mode: 'ALLOWLIST_READ_ONLY', rules: [{ domainPattern: 'fixture.invalid', protocol: 'HTTPS', port: 443, httpMethods: ['GET'] }] } }, kubernetesAccess: { kind: 'NONE', namespace: 'kodex-runtime' } } } });
test('ENV сохраняет38 tools, values, policy и exact Secret revision; source не меняется', () => {
  const e = env(); e.currentVersion.version = 19; const old = structuredClone(e), body = environmentDraft(e, 'imgart_next');
  assert.deepEqual(e, old); assert.deepEqual(body.specification.tools, e.currentVersion.tools); assert.deepEqual(body.specification.values, e.currentVersion.values);
  assert.deepEqual(body.specification.secretBindings, [{ name: 'TOKEN', secretRef: 'rsec_fixture', revision: 6 }]); assert.deepEqual(body.specification.policy.resources, e.currentVersion.policy.resources); assert.deepEqual(body.specification.policy.webAccess, e.currentVersion.policy.network.webAccess);
  assert.equal(body.expectedEnvironmentVersion, 7); assert.equal(body.specification.imageArtifactRef, 'imgart_next'); assert.equal(Object.hasOwn(body.specification.secretBindings[0], 'secretUid'), false);
});
for (const change of [e => e.currentVersion.secretDescriptors[0].revision = 0, e => e.currentVersion.tools.pop(), e => e.currentVersion.policy.kubernetesAccess.kind = 'UNKNOWN', e => e.currentVersion.policy.network.denyByDefault = false]) test('ENV неполные/неизвестные pins закрыты', () => { const e = env(); change(e); assert.throws(() => environmentDraft(e, 'imgart_next')); });
test('Сохранность ENV исключает только image-bound revision identity и включает все non-image настройки', () => {
  const original = env(), changed = structuredClone(original), before = environmentPreservationSHA256(original);
  Object.assign(changed.currentVersion, { ref: 'renvv_next', revision: 5, version: 20, digest: 'next', createdAt: '2099-01-01', image: { artifactRef: 'imgart_next' } });
  assert.equal(environmentPreservationSHA256(changed), before);
  for (const mutate of [e => e.currentVersion.tools[0].description = 'changed', e => e.currentVersion.values[0].value = 'changed', e => e.currentVersion.secretDescriptors[0].revision++, e => e.currentVersion.policy.resources.cpuLimitMilli++, e => e.name = 'changed']) { const e = structuredClone(changed); mutate(e); assert.notEqual(environmentPreservationSHA256(e), before); }
});
const binding = { consumerRef: 'agt_intended', consumerVersion: 8, bindingRef: 'renvb_intended', bindingVersion: 4, sourceRevisionRef: 'renvv_fixture', projectRef: 'prj_fixture', scopeKind: 'PROJECT', organizationRef: 'org_fixture' };
const page = () => ({ plan: { ref: 'rvip_fixture', digest: 'a'.repeat(64), total: 2, state: 'PREPARED', kind: 'RUNTIME_ENVIRONMENT' }, items: [{ ref: 'rvit_intended', consumerKind: 'AGENT', outcome: 'PENDING', ...binding }, { ref: 'rvit_other', consumerKind: 'AGENT', outcome: 'PENDING', ...binding, bindingRef: 'renvb_other', consumerRef: 'agt_other' }], nextPageToken: '' });
test('Выбор только exact intended binding, не всех consumers', () => { assert.deepEqual(selectBindings([page()], [binding]), { planRef: 'rvip_fixture', selectedItemRefs: ['rvit_intended'] }); });
for (const change of [p => p.nextPageToken = 'next', p => p.items.pop(), p => p.items[0].bindingVersion++, p => p.items[0].sourceRevisionRef = 'renvv_other', p => p.items[1].ref = p.items[0].ref]) test('Не-EOF/неполный/изменившийся план закрыт', () => { const p = page(); change(p); assert.throws(() => selectBindings([p], [binding])); });

const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const internal = 'kodex-image-registry.kodex-system.svc.cluster.local:5000/kodex/agent-runner@sha256:' + 'b'.repeat(64);
const scope = i => ({ organizationRef: 'org_fixture', scopeKind: i === 0 ? 'ORGANIZATION' : 'PROJECT', projectRef: i === 0 ? '' : 'prj_fixture' });
function recipesInput() {
  return { version: 1, runnerDigest: 'sha256:' + 'b'.repeat(64), catalog: { items: [{ key: 'standard', available: true, dockerfileTemplate: 'FROM ' + internal + '\n' }] }, recipes: [0, 1, 2].map(i => {
    const s = scope(i), content = JSON.stringify(document), published = { ref: 'mrev_' + i, revision: 10, state: 'PUBLISHED', contentFormat: 'JSON', sourceAvailable: true, content, digest: sha(content) };
    const expected = { ...s, recipeRef: 'imgrec_' + i, configurationRef: 'mcfg_' + i, publishedRevisionRef: published.ref };
    const recipe = { ...s, ref: expected.recipeRef, name: document.name, state: 'ACTIVE', sourceAvailable: true, version: 9, generation: 8, roleDefinitionRef: document.roleImage.roleDefinitionRef, environment: structuredClone(document.roleImage.environment), managedLineage: { managedBy: 'UI', configurationRef: expected.configurationRef, revisionRef: published.ref } };
    const configuration = { ref: expected.configurationRef, version: 12, name: 'Synthetic', kind: 'ROLE_IMAGE', managedBy: 'UI', projectRef: s.projectRef, archived: false, sourceEditable: true, currentRevision: published };
    return { expected, detail: { recipe }, historyPages: [{ configuration, items: [published], total: 1, nextPageToken: '' }] };
  }) };
}
function nativeEmptyProjectionInput(sourceKeys = null) {
  const input = recipesInput();
  for (const [i, entry] of input.recipes.entries()) {
    const revision = entry.historyPages[0].items[0], source = JSON.parse(revision.content);
    Object.assign(source.roleImage.environment, { installationBlock: '', packageKeys: i === 0 ? null : sourceKeys, toolKeys: i === 0 ? null : sourceKeys });
    if (i !== 0 && sourceKeys === undefined) { delete source.roleImage.environment.packageKeys; delete source.roleImage.environment.toolKeys; }
    revision.content = JSON.stringify(source); revision.digest = sha(revision.content);
    for (const key of ['installationBlock', 'packageKeys', 'toolKeys']) delete entry.detail.recipe.environment[key];
  }
  return input;
}
for (const sourceKeys of [null, [], undefined]) test('Native пустой optional projection равен source null/[]/missing; полный source сохраняется', () => {
  const input = nativeEmptyProjectionInput(sourceKeys), frozen = structuredClone(input), intents = prepareRecipes(input);
  assert.deepEqual(input, frozen);
  for (const [i, r] of intents.entries()) { const before = JSON.parse(input.recipes[i].historyPages[0].items[0].content), after = i === 0 ? structuredClone(r.pins.expectedPublishedSource) : JSON.parse(r.body.content); after.roleImage.environment.dockerfile = before.roleImage.environment.dockerfile; assert.deepEqual(after, before); assert.equal(after.roleImage.environment.installationBlock, ''); if (i === 0) { assert.equal(r.body.environment.installationBlock, ''); assert.notEqual(r.body.environment.packageKeys, null); assert.notEqual(r.body.environment.toolKeys, null); } }
});
for (const [field, value, projection] of [['installationBlock', 'RUN nonempty', false], ['packageKeys', ['git'], false], ['toolKeys', ['git'], false], ['installationBlock', 0, false], ['installationBlock', null, false], ['packageKeys', '', false], ['packageKeys', {}, false], ['packageKeys', [0], false], ['packageKeys', ['bad_key'], false], ['toolKeys', ['git', 'git'], false], ['toolKeys', false, false], ['packageKeys', null, true], ['installationBlock', 'RUN nonempty', true]]) test('Optional nonempty/absent/malformed source и projection не маскируются', () => {
  const input = nativeEmptyProjectionInput();
  if (projection) input.recipes[0].detail.recipe.environment[field] = value;
  else { const revision = input.recipes[0].historyPages[0].items[0], source = JSON.parse(revision.content); source.roleImage.environment[field] = value; revision.content = JSON.stringify(source); revision.digest = sha(revision.content); }
  assert.throws(() => prepareRecipes(input), /SOURCE_RECIPE_MISMATCH/);
});
test('Три complex recipes используют actual internal catalog FROM и стабильные exact draft intents', () => {
  const input = recipesInput(), frozen = structuredClone(input), intents = prepareRecipes(input);
  assert.deepEqual(input, frozen); assert.equal(intents.length, 3); assert.deepEqual(intents, prepareRecipes(input));
  for (const [i, r] of intents.entries()) { assert.equal(r.headers['If-Match'], i === 0 ? '"9"' : '"12"'); assert.equal(r.expectedStatus, i === 0 ? 200 : 201); assert.equal(r.effect, 'NOT_RUN'); assert.match(i === 0 ? r.body.environment.dockerfile : JSON.parse(r.body.content).roleImage.environment.dockerfile, /kodex-image-registry\.kodex-system/); assert.equal(Object.hasOwn(r.body, 'projectRef'), i !== 0); assert.equal(r.method, i === 0 ? 'PATCH' : 'POST'); assert.equal(r.path, i === 0 ? '/api/v1/organization/role-image-recipes/imgrec_0' : '/api/v1/role-image-configurations/drafts'); }
});
test('ORG typed UPDATE сохраняет весь complex environment и source pin без payload project/role authority', () => {
  const input = recipesInput(), r = prepareRecipes(input)[0], old = JSON.parse(input.recipes[0].historyPages[0].items[0].content);
  assert.deepEqual(Object.keys(r.body).sort(), ['environment', 'name']);
  const selection = structuredClone(r.body.environment); selection.dockerfile = old.roleImage.environment.dockerfile;
  assert.deepEqual(selection, old.roleImage.environment); assert.equal(r.body.name, old.name);
  assert.equal(Object.hasOwn(r.body, 'roleDefinitionRef'), false); assert.equal(Object.hasOwn(r.body, 'projectRef'), false);
  assert.equal(r.pins.continuation, 'ATOMIC_MANAGED_PUBLICATION_AUTOBUILD_SOURCE_READBACK_HUMAN_RISK_IF_REQUIRED_PROMOTE');
});
for (const mutate of [s => s.description = 'must-not-drop', s => s.roleImage.extra = 'must-not-drop', s => s.roleImage.environment.extra = 'must-not-drop', s => s.name = 'changed']) test('ORG source не представимый typed UPDATE закрыт без молчаливой потери JSON', () => { const input = recipesInput(), revision = input.recipes[0].historyPages[0].items[0], source = JSON.parse(revision.content); mutate(source); revision.content = JSON.stringify(source); revision.digest = sha(revision.content); assert.throws(() => prepareRecipes(input), /ORGANIZATION_SOURCE_UNREPRESENTABLE/); });
test('ORG ожидаемый source hash использует exact server Go map JSON и HTML escaping', () => {
  const source = structuredClone(document); source.roleImage.environment.packageKeys = null; source.roleImage.environment.toolKeys = null;
  const dockerfile = 'FROM synthetic\nRUN a && b < c > d\n#\u2028\u2029';
  const encoded = organizationSourceJSON(source, dockerfile);
  assert.match(encoded, /\\u0026\\u0026/); assert.match(encoded, /\\u003c/); assert.match(encoded, /\\u003e/); assert.match(encoded, /\\u2028\\u2029/);
  assert.equal(encoded, '{"name":"Synthetic","roleImage":{"environment":{"dockerfile":"FROM synthetic\\nRUN a \\u0026\\u0026 b \\u003c c \\u003e d\\n#\\u2028\\u2029","environmentKey":"standard","installationBlock":"RUN setup-synthetic","packageKeys":null,"toolKeys":null},"roleDefinitionRef":"role_fixture"}}');
});
for (const mutate of [s => s.roleImage.environment.packageKeys = [], s => delete s.roleImage.environment.toolKeys, s => delete s.roleImage.environment.installationBlock]) test('ORG скрытая null/empty/missing source normalization закрыта', () => { const source = structuredClone(document); mutate(source); assert.throws(() => organizationSourceJSON(source, 'FROM synthetic'), /ORGANIZATION_SOURCE_UNREPRESENTABLE/); });
for (const change of [x => x.runnerDigest = 'sha256:' + 'c'.repeat(64), x => x.catalog.items.push(x.catalog.items[0]), x => x.recipes[0].historyPages[0].nextPageToken = 'more', x => x.recipes[0].detail.recipe.managedLineage.managedBy = 'GIT', x => x.recipes[0].historyPages[0].configuration.sourceEditable = false, x => x.recipes[0].historyPages[0].items[0].digest = 'a'.repeat(64), x => x.recipes[1].detail.recipe.organizationRef = 'org_other', x => x.recipes[0].historyPages[0].items[0].state = 'DRAFT']) test('Recipe catalog/source/owner/draft/EOF drift закрыто отвергается', () => { const x = recipesInput(); change(x); assert.throws(() => prepareRecipes(x)); });
function environmentsInput() {
  return { version: 1, environments: [0, 1, 2, 3].map(i => {
    const s = scope(i), environment = { ...env(), ...s, ref: 'renv_' + i }; if (i === 0) delete environment.projectRef; environment.currentVersion.ref = 'renvv_' + i; environment.currentVersion.image = { artifactRef: 'imgart_old' }; environment.currentVersion.version = 19;
    const recipeIndex = [0, 1, 1, 2][i], recipeRef = 'imgrec_' + recipeIndex, revision = 'mrev_new_' + recipeIndex;
    const activeArtifact = { ...s, ref: 'imgart_new_' + recipeIndex, version: 6, buildRef: 'imgbld_' + recipeIndex, recipeRef, recipeGeneration: 10, admissionVerdict: 'ACCEPTED', promotionState: 'PROMOTED', promotionRequested: false, promotedAt: '2026-10-10T17:00:00Z', promotedReference: 'pull.fixture.invalid/roles@sha256:' + 'c'.repeat(64), manifestDigest: 'sha256:' + 'c'.repeat(64), provenanceSha256: 'd'.repeat(64), promotionReceiptSha256: 'e'.repeat(64), declaredTools: [] };
    activeArtifact.verifiedToolInventory = inventoryFixture(activeArtifact);
    const recipe = { ...s, ref: recipeRef, state: 'ACTIVE', generation: 10, activeImageArtifactRef: activeArtifact.ref, promotedImageReference: activeArtifact.promotedReference, promotedImageReady: true, managedLineage: { revisionRef: revision } };
    return { expected: { ...s, environmentRef: environment.ref, environmentVersion: 7, baseVersionRef: environment.currentVersion.ref, recipeRef, publishedRevisionRef: revision }, environment, artifactDetail: { recipe, activeArtifact }, bindings: [{ ...binding, ...s, consumerRef: 'agt_' + i, bindingRef: 'renvb_' + i, sourceRevisionRef: environment.currentVersion.ref }] };
  }) };
}
function inventoryFixture(artifact, platform = 'linux/amd64') {
  return { status: 'VERIFIED', sha256: 'a'.repeat(64), imageDigest: artifact.manifestDigest, provenanceSha256: artifact.provenanceSha256, platforms: [{ platform, platformDigest: artifact.manifestDigest, manifestSha256: 'b'.repeat(64), tools: probeNames.map((name, index) => ({ name, status: 'VERIFIED', path: '/usr/local/bin/' + commandFor(name), version: '1.2.3', sha256: 'a'.repeat(64), required: index < 38 })) }] };
}
test('Четыре ENV сохраняют38 tools/полные values/policy/exact Secret; wrapper OCC не currentVersion.version', () => {
  const x = environmentsInput(), old = structuredClone(x), intents = prepareEnvironments(x); assert.deepEqual(x, old); assert.equal(intents.length, 4);
  for (const [i, r] of intents.entries()) { assert.equal(r.body.expectedEnvironmentVersion, 7); assert.deepEqual(r.body.specification.tools, x.environments[i].environment.currentVersion.tools); assert.deepEqual(r.body.specification.values, x.environments[i].environment.currentVersion.values); assert.equal(r.body.specification.secretBindings[0].revision, 6); assert.equal(Object.hasOwn(r.headers, 'If-Match'), false); }
  assert.equal(intents[0].path, '/api/v1/organization/runtime-environment-drafts'); assert.equal(intents[1].path, '/api/v1/projects/prj_fixture/runtime-environment-drafts');
});
for (const change of [x => x.environments.pop(), x => x.environments[0].artifactDetail.activeArtifact.admissionVerdict = 'PENDING', x => x.environments[0].artifactDetail.activeArtifact.promotionState = 'AUTHORIZED', x => x.environments[0].artifactDetail.activeArtifact.verifiedToolInventory.status = 'UNAVAILABLE', x => x.environments[0].artifactDetail.activeArtifact.verifiedToolInventory.platforms[0].tools.pop(), x => x.environments[0].bindings[0].sourceRevisionRef = 'renvv_foreign', x => x.environments[0].environment.version++, x => x.environments[1].artifactDetail.activeArtifact.organizationRef = 'org_foreign']) test('ENV partial/admission/inventory/pins/scope drift закрыт', () => { const x = environmentsInput(); change(x); assert.throws(() => prepareEnvironments(x)); });
test('Inherited38 base commands допускаются с declaredTools=[] только по verified inventory', () => {
  const input = environmentsInput(), frozen = structuredClone(input), requests = prepareEnvironments(input);
  assert.deepEqual(input, frozen);
  for (const [index, request] of requests.entries()) { const artifact = input.environments[index].artifactDetail.activeArtifact; assert.deepEqual(artifact.declaredTools, []); assert.deepEqual(request.body.specification.tools, input.environments[index].environment.currentVersion.tools); assert.equal(request.pins.inventorySHA256, artifact.verifiedToolInventory.sha256); assert.equal(request.pins.artifactVersion, 6); assert.equal(request.pins.inventoryPlatformPins.length, 1); }
});
test('Выбираются basename actualpath rg/tsc, не display names ripgrep/typescript; aliases platform intersection', () => {
  const artifact = environmentsInput().environments[0].artifactDetail.activeArtifact, inventory = artifact.verifiedToolInventory;
  const second = inventoryFixture(artifact, 'linux/arm64').platforms[0]; second.tools[35].path = '/usr/bin/mcp-server-playwright'; inventory.platforms.push(second);
  const commands = verifiedArtifactCommands(artifact); assert.equal(commands.has('rg'), true); assert.equal(commands.has('tsc'), true); assert.equal(commands.has('ripgrep'), false); assert.equal(commands.has('typescript'), false); assert.equal(commands.has('playwright-mcp'), false); assert.equal(commands.has('mcp-server-playwright'), false);
});
test('Display declaration не заменяет missing/probe_failed evidence; union платформ запрещён', () => {
  for (const status of ['MISSING', 'PROBE_FAILED']) {
    const input = environmentsInput(), artifact = input.environments[0].artifactDetail.activeArtifact;
    artifact.declaredTools = [{ name: 'git', version: '1.2.3' }]; const second = inventoryFixture(artifact, 'linux/arm64').platforms[0];
    Object.assign(second.tools[2], status === 'MISSING' ? { status, path: '', version: '', sha256: '' } : { status, version: '' }); artifact.verifiedToolInventory.platforms.push(second);
    assert.throws(() => prepareEnvironments(input), /VERIFIED_TOOLS_MISSING/);
  }
});
for (const mutate of [a => a.verifiedToolInventory.imageDigest = 'sha256:' + 'f'.repeat(64), a => a.verifiedToolInventory.provenanceSha256 = 'f'.repeat(64), a => a.verifiedToolInventory.sha256 = '', a => a.verifiedToolInventory.platforms = [], a => a.verifiedToolInventory.platforms.push(structuredClone(a.verifiedToolInventory.platforms[0])), a => a.verifiedToolInventory.platforms[0].platform = 'windows/amd64', a => a.verifiedToolInventory.platforms[0].platformDigest = 'bad', a => a.verifiedToolInventory.platforms[0].manifestSha256 = '', a => a.verifiedToolInventory.platforms[0].tools[0].name = 'unknown', a => a.verifiedToolInventory.platforms[0].tools[0].required = false, a => a.verifiedToolInventory.platforms[0].tools[0].status = 'UNKNOWN', a => a.verifiedToolInventory.platforms[0].tools[0].path = '/tmp/bash', a => a.verifiedToolInventory.platforms[0].tools[0].version = '', a => a.verifiedToolInventory.platforms[0].tools[0].sha256 = '', a => a.verifiedToolInventory.platforms[0].extra = true]) test('Foreign/empty/malformed inventory и незарегистрированные observation закрыты', () => { const artifact = environmentsInput().environments[0].artifactDetail.activeArtifact; mutate(artifact); assert.throws(() => verifiedArtifactCommands(artifact), /INVENTORY_/); });
for (const mutate of [x => x.environments[0].artifactDetail.recipe.state = 'ARCHIVED', x => x.environments[0].artifactDetail.recipe.activeImageArtifactRef = 'imgart_foreign', x => x.environments[0].artifactDetail.recipe.promotedImageReference = 'pull.foreign.invalid/a@sha256:' + 'c'.repeat(64), x => x.environments[0].artifactDetail.activeArtifact.version = 0, x => x.environments[0].artifactDetail.activeArtifact.buildRef = '', x => x.environments[0].artifactDetail.activeArtifact.promotedAt = 'invalid', x => x.environments[0].artifactDetail.activeArtifact.promotionReceiptSha256 = '', x => delete x.environments[0].artifactDetail.activeArtifact.declaredTools, x => x.environments[0].environment.currentVersion.tools[0].command = 'ripgrep', x => x.environments[0].environment.currentVersion.tools[1].command = x.environments[0].environment.currentVersion.tools[0].command]) test('ActiveArtifact currentscope/promotion/readiness и executable command pins закрыты', () => { const input = environmentsInput(); mutate(input); assert.throws(() => prepareEnvironments(input)); });
test('Offline закрытый probe catalog соответствует текущему OpenAPI inventory profile', () => {
  const schema = readFileSync(new URL('../../contracts/openapi/control-api-gateway/v1/openapi.yaml', import.meta.url), 'utf8').split('    ImageToolObservation:')[1].split('    RoleImageRecipeCommandReceipt:')[0];
  const names = /name: \{ type: string, enum: \[([^\]]+)\]/.exec(schema)[1].split(',').map(value => value.trim()); assert.deepEqual(probeNames, names); assert.equal(probeNames.length, 50);
});
for (const change of [x => x.environments[0].environment.ready = false, x => x.environments[0].environment.readinessBlockers.push('UNKNOWN'), x => delete x.environments[0].environment.ready, x => x.environments[0].environment.projectRef = 'prj_wrong', x => delete x.environments[1].environment.projectRef]) test('Native ENV без enabled: readiness и ORG/PROJECT scope закрыты', () => { const x = environmentsInput(); change(x); assert.throws(() => prepareEnvironments(x)); });
function selectionsInput() {
  return { version: 1, selections: [0, 1, 2, 3].map(i => {
    const s = scope(i), intendedBindings = [{ ...binding, ...s, consumerRef: 'agt_' + i, bindingRef: 'renvb_' + i }], draft = { ...s, ref: 'renvd_' + i, version: 3, state: 'VALID', environmentRef: 'renv_' + i, expectedEnvironmentVersion: 7, baseVersionRef: binding.sourceRevisionRef, validationDigest: 'a'.repeat(64) };
    const p = page(); p.items[0] = { ...p.items[0], ...intendedBindings[0] }; Object.assign(p.items[1], s); Object.assign(p.plan, { ref: 'rvip_' + i, draftRef: draft.ref, draftVersion: 3, sourceRef: draft.environmentRef, sourceVersion: 7, sourceRevisionRef: draft.baseVersionRef, targetDigest: draft.validationDigest, expiresAt: '2099-01-01T00:00:00Z' });
    return { draft, pages: [p], intendedBindings };
  }) };
}
test('Четыре publication intents выбирают только intended binding из fullEOF impact и current draft OCC', () => {
  for (const r of prepareSelections(selectionsInput())) { assert.equal(r.headers['If-Match'], '"3"'); assert.deepEqual(r.body.selectedItemRefs, ['rvit_intended']); assert.equal(Object.hasOwn(r.body, 'impactDigest'), false); assert.equal(r.expectedStatus, 200); }
});
for (const change of [x => x.selections[0].pages[0].plan.expiresAt = '2001-01-01T00:00:00Z', x => x.selections[0].draft.version++, x => x.selections[0].pages[0].plan.sourceRevisionRef = 'renvv_other', x => x.selections[0].intendedBindings[0].organizationRef = 'org_foreign', x => x.selections[0].pages[0].items[0].bindingVersion++]) test('Publication stale/expired/foreign/consumer mismatch закрыт', () => { const x = selectionsInput(); change(x); assert.throws(() => prepareSelections(x)); });
function cli(t, input = recipesInput()) {
  const dir = mkdtempSync(join(tmpdir(), 'complex-role-native-')); t.after(() => rmSync(dir, { recursive: true, force: true }));
  const inputPath = join(dir, 'input.json'), output = join(dir, 'intents.json'), bytes = Buffer.from(JSON.stringify(input)); writeFileSync(inputPath, bytes, { mode: 0o600 });
  const run = (phase = 'recipes', extra = []) => spawnSync(process.execPath, [new URL('./role-image-native-plan.mjs', import.meta.url).pathname, phase, '--input', inputPath, '--input-sha256', sha(bytes), '--output', output, ...extra], { encoding: 'utf8', timeout: 5000 });
  return { dir, inputPath, output, bytes, run };
}
test('Actual CLI argv пишет только0600 immutable private output; stdout не source/values; input unchanged; overwrite закрыт', t => {
  const f = cli(t), result = f.run(); assert.equal(result.status, 0, result.stderr); const out = JSON.parse(result.stdout); assert.equal(out.status, 'PREPARED_OFFLINE'); assert.equal(out.intents, 3); assert.equal(out.effect, 'NOT_RUN'); assert.equal(out.humanRiskDecision, 'NOT_AUTOMATED'); assert.equal(lstatSync(f.output).mode & 0o777, 0o600); assert.equal(out.outputSHA256, sha(readFileSync(f.output))); assert.deepEqual(readFileSync(f.inputPath), f.bytes); assert.doesNotMatch(result.stdout, /dockerfile|RUN printf|SECRET/); const priorOutput = readFileSync(f.output); assert.equal(f.run().status, 1); assert.deepEqual(readFileSync(f.output), priorOutput);
});
for (const [phase, fixture] of [['environments', environmentsInput], ['selections', selectionsInput]]) test('Actual CLI '+phase+' готовит4 native intents без live effects', t => { const f = cli(t, fixture()), r = f.run(phase); assert.equal(r.status, 0, r.stderr); assert.equal(JSON.parse(r.stdout).intents, 4); });
for (const mode of ['permissions', 'symlink', 'digest', 'unknown-argument', 'malformed', 'directory']) test('CLI закрытый filesystem/input/argv '+mode, t => {
  const f = cli(t); if (mode === 'permissions') chmodSync(f.inputPath, 0o644); if (mode === 'symlink') { const renamed = join(f.dir, 'original.json'); writeFileSync(renamed, f.bytes, { mode: 0o600 }); rmSync(f.inputPath); symlinkSync(renamed, f.inputPath); } if (mode === 'digest') writeFileSync(f.inputPath, '{}'); if (mode === 'malformed') writeFileSync(f.inputPath, 'synthetic-SECRET'); if (mode === 'directory') chmodSync(f.dir, 0o755);
  const r = f.run('recipes', mode === 'unknown-argument' ? ['--apply', 'yes'] : []); assert.equal(r.status, 1); assert.doesNotMatch(r.stderr, /synthetic-SECRET|dockerfile|token|password/); assert.equal(r.stdout, '');
});
