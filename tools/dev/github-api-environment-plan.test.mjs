import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { chmodSync, lstatSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { githubAPIRule, prepare, snapshotSHA256 } from './github-api-environment-plan.mjs';
const sha = value => createHash('sha256').update(value).digest('hex');
const scope = { organizationRef: 'org_fixture', projectRef: 'prj_fixture', scopeKind: 'PROJECT' };
function fixture() {
  const environment = { ...scope, ref: 'renv_fixture', version: 16, name: 'Синтетическое окружение', description: 'Синтетические данные', state: 'ACTIVE', updatedAt: '2099-01-01T00:00:00Z', ready: true, readinessBlockers: [], nextActions: [], currentVersion: {
    ref: 'renvv_fixture', version: 16, revision: 12, digest: 'a'.repeat(64), createdAt: '2099-01-01T00:00:00Z', image: { artifactRef: 'imgart_fixture', recipeRef: 'imgrec_fixture', recipeGeneration: 3, reference: 'pull.fixture.invalid/image@sha256:' + 'a'.repeat(64), digest: 'sha256:' + 'a'.repeat(64) },
    tools: Array.from({ length: 38 }, (_, i) => ({ name: 'Инструмент' + i, command: 'tool' + i, description: 'Синтетический инструмент', usageHint: 'Синтетическая подсказка' })), values: [{ name: 'MODE', value: 'SYNTHETIC_PRIVATE_VALUE' }], secretDescriptors: [{ name: 'GH_TOKEN', secretRef: 'rsec_fixture', revision: 1, secretName: 'synthetic', secretKey: 'synthetic', secretUid: 'synthetic', secretResourceVersion: '1', contentSha256: 'a'.repeat(64) }],
    policy: { resources: { cpuRequestMilli: 2000, cpuLimitMilli: 2000, memoryRequestMib: 4096, memoryLimitMib: 4096, ephemeralStorageRequestMib: 1024, ephemeralStorageLimitMib: 4096, workspaceLimits: { maxBytes: 1024, maxFiles: 10 } }, volumes: [{ name: 'work', kind: 'EPHEMERAL_DISK', sizeMib: 444, mountPath: '/workspace/.kodex/volumes/work' }], network: { denyByDefault: true, egress: [{ destination: 'DNS', protocol: 'TCP', port: 53 }, { destination: 'DNS', protocol: 'UDP', port: 53 }, { destination: 'PROVIDER_PROXY', protocol: 'TCP', port: 8084 }, { destination: 'RUNTIME_CALLBACK', protocol: 'TCP', port: 8444 }], webAccess: { mode: 'ALLOWLIST_FULL', rules: [{ domainPattern: 'github.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD', 'POST'] }, { domainPattern: 'raw.githubusercontent.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD'] }] } }, kubernetesAccess: { kind: 'NONE', namespace: 'kodex-runtime' }, resourcesDigest: 'a'.repeat(64), volumesDigest: 'b'.repeat(64), networkDigest: 'c'.repeat(64), rbacDigest: 'd'.repeat(64) } } };
  const intendedBindings = [{ ...scope, consumerRef: 'agt_fixture', consumerVersion: 20, bindingRef: 'aenv_fixture', bindingVersion: 17, sourceRevisionRef: environment.currentVersion.ref }];
  return { version: 1, expected: { ...scope, environmentRef: environment.ref, environmentVersion: environment.version, baseVersionRef: environment.currentVersion.ref, baseRevision: environment.currentVersion.revision, baseDigest: environment.currentVersion.digest, environmentSHA256: snapshotSHA256(environment), intendedBindingsSHA256: snapshotSHA256(intendedBindings) }, environment, intendedBindings };
}
function withDraft(state = 'VALID') {
  const input = fixture();
  input.draft = { ...scope, ref: 'renvd_fixture', version: 3, state, environmentRef: input.expected.environmentRef, expectedEnvironmentVersion: 16, baseVersionRef: input.expected.baseVersionRef, baseRevision: 12, specification: prepare('draft', input)[0].body.specification, diagnostics: [] };
  // Фактический CP draft readback: policy проходит canonical normalizer до ответа.
  input.draft.specification.policy.networkDestinations = ['DNS', 'PROVIDER_PROXY', 'RUNTIME_CALLBACK'];
  input.draft.specification.policy.webAccess.rules = [
    { domainPattern: 'api.github.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD', 'PATCH', 'POST'] },
    { domainPattern: 'github.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD', 'POST'] },
    { domainPattern: 'raw.githubusercontent.com', protocol: 'HTTPS', port: 443, httpMethods: ['GET', 'HEAD'] },
  ];
  if (state === 'VALID') input.draft.validationDigest = 'e'.repeat(64);
  return input;
}
function withImpact() {
  const input = withDraft();
  input.impactPages = [{ plan: { ref: 'rvip_fixture', version: 1, kind: 'RUNTIME_ENVIRONMENT', sourceRef: input.expected.environmentRef, sourceVersion: 16, sourceRevisionRef: input.expected.baseVersionRef, draftRef: input.draft.ref, draftVersion: 3, targetDigest: input.draft.validationDigest, digest: 'f'.repeat(64), total: 1, state: 'PREPARED', createdAt: '2020-01-01T00:00:00Z', expiresAt: '2099-01-01T00:00:00Z' }, items: [{ ref: 'rvit_fixture', ...input.intendedBindings[0], consumerKind: 'AGENT', outcome: 'PENDING' }], total: 1, nextPageToken: '' }];
  return input;
}
function readback() {
  const input = withImpact(), page = input.impactPages[0];
  Object.assign(input.draft, { state: 'PUBLISHED', version: 4, publishedEnvironmentRef: input.expected.environmentRef });
  Object.assign(page.plan, { state: 'APPLIED', version: 2, publishedRevisionRef: 'renvv_published' });
  input.publishedEnvironment = structuredClone(input.environment);
  input.publishedEnvironment.version++;
  Object.assign(input.publishedEnvironment.currentVersion, { ref: 'renvv_published', version: 17, revision: 13, digest: input.draft.validationDigest });
  const policy = input.publishedEnvironment.currentVersion.policy;
  policy.networkDigest = 'e'.repeat(64);
  policy.network.webAccess.rules.unshift({ ...structuredClone(githubAPIRule), httpMethods: ['GET', 'HEAD', 'PATCH', 'POST'] });
  Object.assign(page.items[0], { outcome: 'APPLIED', resultRevisionRef: 'renvv_published', resultBindingRef: 'aenv_fixture', resultBindingVersion: 18, resultConsumerVersion: 21 });
  input.bindingReadbacks = [{ configuration: {}, publishedOverlay: {}, environmentBinding: { ref: 'aenv_fixture', version: 18, agentRef: 'agt_fixture', environmentRef: 'renv_fixture', versionRef: 'renvv_published', digest: sha(['agt_fixture', 'renv_fixture', '18', ''].join('\0')) }, environment: input.publishedEnvironment, safeEffectiveConfig: '{}', agentVersion: 21, skillBindings: [], memoryBindings: [], overlaySchema: {} }];
  return input;
}
test('Один draft добавляет только exact HTTPS api.github.com с GET/HEAD/POST/PATCH и сохраняет38 tools/image/secret revision1/policy', () => {
  const input = fixture(), original = structuredClone(input), result = prepare('draft', input);
  assert.deepEqual(input, original); assert.equal(result.length, 1); assert.equal(result[0].path, '/api/v1/projects/prj_fixture/runtime-environment-drafts');
  const spec = result[0].body.specification;
  assert.equal(spec.imageArtifactRef, input.environment.currentVersion.image.artifactRef);
  assert.deepEqual(spec.tools, input.environment.currentVersion.tools); assert.deepEqual(spec.values, input.environment.currentVersion.values);
  assert.deepEqual(spec.secretBindings, [{ name: 'GH_TOKEN', secretRef: 'rsec_fixture', revision: 1 }]);
  assert.deepEqual(spec.policy.resources, input.environment.currentVersion.policy.resources);
  assert.deepEqual(spec.policy.webAccess.rules.slice(0, 2), input.environment.currentVersion.policy.network.webAccess.rules);
  assert.deepEqual(spec.policy.webAccess.rules[2], githubAPIRule); assert.equal(spec.policy.kubernetesAccess, 'NONE');
  assert.equal(result[0].effect, 'NOT_RUN'); assert.deepEqual(result, prepare('draft', input));
});
for (const [name, mutate] of Object.entries({ top_unknown: x => x.unknown = true, expected_unknown: x => x.expected.unknown = true, env_unknown: x => x.environment.unknown = true, scope: x => x.environment.organizationRef = 'org_other', target: x => x.expected.environmentRef = 'renv_other', source_version: x => x.environment.version++, source_ref: x => x.environment.currentVersion.ref = 'renvv_other', source_revision: x => x.environment.currentVersion.revision++, hash: x => x.expected.environmentSHA256 = 'b'.repeat(64), tools: x => x.environment.currentVersion.tools.pop(), values: x => x.environment.currentVersion.values[0].value = 'ALTERED', image: x => x.environment.currentVersion.image.artifactRef = 'imgart_other', secret: x => x.environment.currentVersion.secretDescriptors[0].revision++, policy: x => x.environment.currentVersion.policy.resources.cpuLimitMilli++, bindings: x => x.intendedBindings[0].bindingVersion++, binding_unknown: x => x.intendedBindings[0].unknown = true, cardinality: x => x.intendedBindings.push(structuredClone(x.intendedBindings[0])), missing: x => delete x.environment.currentVersion.secretDescriptors, not_ready: x => x.environment.ready = false })) test('Закрытый отказ baseline: ' + name, () => { const input = fixture(); mutate(input); assert.throws(() => prepare('draft', input)); });
for (const [name, mutate] of Object.entries({ wildcard: x => x.environment.currentVersion.policy.network.webAccess.rules[0].domainPattern = '*.github.com', full_public: x => x.environment.currentVersion.policy.network.webAccess.mode = 'FULL_PUBLIC', delete: x => x.environment.currentVersion.policy.network.webAccess.rules[0].httpMethods.push('DELETE'), extra_domain: x => x.environment.currentVersion.policy.network.webAccess.rules.push(structuredClone(githubAPIRule)), extra_egress: x => x.environment.currentVersion.policy.network.egress[0].port = 443, volume: x => x.environment.currentVersion.policy.volumes[0].mountPath = '/foreign', unknown_policy: x => x.environment.currentVersion.policy.extra = true, unknown_image: x => x.environment.currentVersion.image.extra = true, secret_value: x => x.environment.currentVersion.secretDescriptors[0].value = 'FORBIDDEN' })) test('Даже перепиненный неизвестный/непредставимый source закрыт: ' + name, () => { const input = fixture(); mutate(input); input.expected.environmentSHA256 = snapshotSHA256(input.environment); assert.throws(() => prepare('draft', input)); });
test('Validate/impact используют точный draft OCC и пустой HTTP body', () => {
  for (const [phase, state, suffix, status] of [['validate', 'DRAFT', '/validation', 200], ['impact', 'VALID', '/impact-plans', 201]]) { const input = withDraft(state), result = prepare(phase, input)[0]; assert.equal(result.path, '/api/v1/runtime-environment-drafts/renvd_fixture' + suffix); assert.equal(result.headers['If-Match'], '"3"'); assert.equal(result.expectedStatus, status); assert.equal(Object.hasOwn(result, 'body'), false); }
});
test('Canonical actual-order draft: destinations и api rule/methods отсортированы; исходный intent/input неизменны', () => {
  const input = withDraft('DRAFT'), frozen = structuredClone(input);
  assert.equal(prepare('validate', input)[0].expectedStatus, 200);
  assert.deepEqual(input, frozen);
  const source = fixture(), intent = prepare('draft', source)[0];
  assert.deepEqual(intent.body.specification.policy.networkDestinations, ['DNS', 'RUNTIME_CALLBACK', 'PROVIDER_PROXY']);
  assert.equal(intent.body.specification.policy.webAccess.rules[2].domainPattern, 'api.github.com');
  assert.deepEqual(intent.body.specification.policy.webAccess.rules[2].httpMethods, ['GET', 'HEAD', 'POST', 'PATCH']);
});
for (const [name, mutate] of Object.entries({ extra_field: x => x.draft.specification.policy.extra = true, unknown_destination: x => x.draft.specification.policy.networkDestinations[1] = 'UNKNOWN', duplicate_destination: x => x.draft.specification.policy.networkDestinations[1] = 'DNS', extra_destination: x => x.draft.specification.policy.networkDestinations.push('UNKNOWN'), delete_method: x => x.draft.specification.policy.webAccess.rules[0].httpMethods.push('DELETE'), duplicate_method: x => x.draft.specification.policy.webAccess.rules[0].httpMethods.push('GET'), missing_method: x => x.draft.specification.policy.webAccess.rules[0].httpMethods.pop(), lower_method: x => x.draft.specification.policy.webAccess.rules[0].httpMethods[0] = 'get', wildcard: x => x.draft.specification.policy.webAccess.rules[0].domainPattern = '*.github.com', rule_unknown: x => x.draft.specification.policy.webAccess.rules[0].extra = true, changed_scope: x => x.draft.projectRef = 'prj_other', unordered_projection: x => x.draft.specification.policy.networkDestinations.reverse() })) test('Canonical comparison сохраняет закрытый отказ: ' + name, () => { const input = withDraft('DRAFT'); mutate(input); assert.throws(() => prepare('validate', input), /DRAFT_PIN_CHANGED|SCOPE_CHANGED/); });
for (const mutate of [x => x.draft.specification.tools.pop(), x => x.draft.specification.policy.webAccess.rules[2].httpMethods.push('DELETE'), x => x.draft.specification.imageArtifactRef = 'imgart_other', x => x.draft.baseRevision++, x => x.draft.baseVersionRef = 'renvv_other', x => x.draft.expectedEnvironmentVersion++, x => x.draft.diagnostics.push('FAIL'), x => x.draft.state = 'DRAFT', x => x.draft.extra = true]) test('Изменённый draft закрыт', () => { const input = withDraft(); mutate(input); assert.throws(() => prepare('impact', input)); });
test('Publication выбирает только единственный intended binding из exact EOF и сохраняет draft OCC', () => { const input = withImpact(), result = prepare('publication', input)[0]; assert.deepEqual(result.body, { planRef: 'rvip_fixture', selectedItemRefs: ['rvit_fixture'] }); assert.equal(result.headers['If-Match'], '"3"'); });
for (const [name, mutate] of Object.entries({ non_eof: x => x.impactPages[0].nextPageToken = 'next', partial: x => x.impactPages[0].items = [], duplicate: x => x.impactPages[0].items.push(structuredClone(x.impactPages[0].items[0])), total: x => x.impactPages[0].plan.total++, page_total: x => x.impactPages[0].total++, unknown: x => x.impactPages[0].plan.unknown = true, stale: x => x.impactPages[0].plan.draftVersion++, expired: x => x.impactPages[0].plan.expiresAt = '2020-01-01T00:00:00Z', foreign: x => x.impactPages[0].plan.sourceRef = 'renv_other', source_version: x => x.impactPages[0].plan.sourceVersion++, source_revision: x => x.impactPages[0].plan.sourceRevisionRef = 'renvv_other', consumer: x => x.impactPages[0].items[0].consumerRef = 'agt_other', binding_version: x => x.impactPages[0].items[0].bindingVersion++, binding_scope: x => x.impactPages[0].items[0].organizationRef = 'org_other', item_unknown: x => x.impactPages[0].items[0].unknown = true, changed_page: x => { const page = structuredClone(x.impactPages[0]); page.plan.digest = 'b'.repeat(64); x.impactPages[0].nextPageToken = 'next'; x.impactPages.push(page); } })) test('Закрытый отказ publication: ' + name, () => { const input = withImpact(); mutate(input); assert.throws(() => prepare('publication', input)); });
test('Readback учитывает canonical sort policy и самостоятельный binding digest', () => { const input = readback(); assert.equal(prepare('readback', input).status, 'READBACK_VERIFIED_OFFLINE'); });
function actionProjectionReadback() {
  const input = readback();
  input.publishedEnvironment.nextActions = ['OPEN', 'UPDATE', 'ROLLBACK', 'DISABLE'];
  input.bindingReadbacks[0].environment = structuredClone(input.publishedEnvironment);
  input.bindingReadbacks[0].environment.nextActions = [];
  return input;
}
test('Direct GET и вложенный runtime GET различаются только permission hints nextActions; полный readback принят', () => {
  const input = actionProjectionReadback(), frozen = structuredClone(input);
  assert.equal(prepare('readback', input).status, 'READBACK_VERIFIED_OFFLINE');
  assert.deepEqual(input, frozen);
});
for (const [name, mutate] of Object.entries({ unknown_action: x => x.bindingReadbacks[0].environment.nextActions = ['UNKNOWN'], foreign_action: x => x.bindingReadbacks[0].environment.nextActions = ['DELETE'], duplicate_action: x => x.bindingReadbacks[0].environment.nextActions = ['OPEN', 'OPEN'], action_shape: x => x.bindingReadbacks[0].environment.nextActions = [{ key: 'OPEN' }], missing_hints: x => delete x.bindingReadbacks[0].environment.nextActions, unknown_field: x => x.bindingReadbacks[0].environment.extra = true, identity: x => x.bindingReadbacks[0].environment.ref = 'renv_other', scope: x => x.bindingReadbacks[0].environment.organizationRef = 'org_other', version: x => x.bindingReadbacks[0].environment.version++, readiness: x => x.bindingReadbacks[0].environment.ready = false, blockers: x => x.bindingReadbacks[0].environment.readinessBlockers.push('UNKNOWN'), timestamp: x => x.bindingReadbacks[0].environment.updatedAt = '2099-02-01T00:00:00Z', name: x => x.bindingReadbacks[0].environment.name = 'Другое имя', image: x => x.bindingReadbacks[0].environment.currentVersion.image.artifactRef = 'imgart_other', source_revision: x => x.bindingReadbacks[0].environment.currentVersion.revision++, policy: x => x.bindingReadbacks[0].environment.currentVersion.policy.resources.cpuLimitMilli++, direct_unknown_action: x => x.publishedEnvironment.nextActions = ['UNKNOWN'] })) test('Projection comparison закрывает всё кроме известных permission hints: ' + name, () => { const input = actionProjectionReadback(); mutate(input); assert.throws(() => prepare('readback', input)); });
for (const [name, mutate] of Object.entries({ image: x => x.publishedEnvironment.currentVersion.image.artifactRef = 'imgart_other', secret: x => x.publishedEnvironment.currentVersion.secretDescriptors[0].revision++, policy: x => x.publishedEnvironment.currentVersion.policy.resources.cpuLimitMilli++, methods: x => x.publishedEnvironment.currentVersion.policy.network.webAccess.rules[0].httpMethods.push('DELETE'), missing_rule: x => x.publishedEnvironment.currentVersion.policy.network.webAccess.rules.shift(), wrong_result: x => x.impactPages[0].items[0].resultRevisionRef = 'renvv_other', conflict: x => x.impactPages[0].items[0].outcome = 'CONFLICT', incomplete: x => x.bindingReadbacks = [], hash: x => x.bindingReadbacks[0].environmentBinding.digest = 'f'.repeat(64), extra_binding: x => x.bindingReadbacks.push(structuredClone(x.bindingReadbacks[0])), digest: x => x.publishedEnvironment.currentVersion.digest = 'f'.repeat(64) })) test('Закрытый отказ readback: ' + name, () => { const input = readback(); mutate(input); assert.throws(() => prepare('readback', input)); });
function cli(t) {
  const dir = mkdtempSync(join(tmpdir(), 'github-api-environment-')); t.after(() => rmSync(dir, { recursive: true, force: true }));
  const input = join(dir, 'input.json'), output = join(dir, 'output.json'), bytes = Buffer.from(JSON.stringify(fixture()));
  writeFileSync(input, bytes, { mode: 0o600 });
  const run = (extra = [], inputHash = sha(bytes), source = input) => spawnSync(process.execPath, [new URL('./github-api-environment-plan.mjs', import.meta.url).pathname, 'draft', '--input', source, '--input-sha256', inputHash, '--output', output, ...extra], { encoding: 'utf8', timeout: 5000 });
  return { dir, input, output, bytes, run };
}
test('CLI приватный input/output, отсутствие values/metadata в stdout, stable hash и запрет overwrite', t => { const x = cli(t), result = x.run(); assert.equal(result.status, 0, result.stderr); assert.equal(lstatSync(x.output).mode & 0o777, 0o600); assert.deepEqual(readFileSync(x.input), x.bytes); assert.equal(result.stdout.includes('SYNTHETIC_PRIVATE_VALUE'), false); assert.equal(result.stdout.includes('rsec_fixture'), false); assert.equal(JSON.parse(result.stdout).outputSHA256, sha(readFileSync(x.output))); assert.notEqual(x.run().status, 0); });
test('CLI отвергает неизвестные args/hash/symlink/mode/неполный EOF JSON без эффекта', t => {
  const x = cli(t); assert.notEqual(x.run(['--unknown', 'x']).status, 0); assert.notEqual(x.run([], 'f'.repeat(64)).status, 0);
  const link = join(x.dir, 'link.json'); symlinkSync(x.input, link); assert.notEqual(x.run([], sha(x.bytes), link).status, 0);
  chmodSync(x.input, 0o644); assert.notEqual(x.run().status, 0); chmodSync(x.input, 0o600);
  const partial = Buffer.from(x.bytes.subarray(0, x.bytes.length - 1)); writeFileSync(x.input, partial); const result = x.run([], sha(partial)); assert.notEqual(result.status, 0); assert.equal(result.stderr.includes('SYNTHETIC_PRIVATE_VALUE'), false);
});
