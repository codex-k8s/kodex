import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {authorityReadbackContract} from './authority-readback-targets.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {telegramPolicySource, telegramRegistrySource, validateTelegramRegistry} from './authority-telegram-transition.mjs';
import {fingerprint} from './scoped-release.mjs';

const policySHA256 = '08417f7f073c6fdaf2f0a0d1d2d803fa3997acaec39dae84746251fa467681fb';
const registrySHA256 = 'bd9e0e20e729e68839dfa540f9f71f73a70fddfce84cffe6e25ebc8d99549a00';
const desiredPolicy = new URL('../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json', import.meta.url);
const desiredRegistry = new URL('../../deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml', import.meta.url);
const newWorkload = 'artifact-retention';

export const personalIntegrationsAuthorityTransition = 'PERSONAL_INTEGRATIONS_107_110';

const sha256 = value => createHash('sha256').update(value).digest('hex');
const requireValue = (condition, code) => { if (!condition) throw Error(code); };

function encodeRegistry(value, output) {
  return execFileSync('yq', ['-o=' + output, '-I=' + (output === 'json' ? '0' : '2'), '.', '-'], {
    input: value, encoding: 'utf8', timeout: 10000, maxBuffer: 1 << 20, stdio: ['pipe', 'pipe', 'pipe'],
    env: {PATH: process.env.PATH ?? '/usr/local/bin:/usr/bin:/bin', LANG: 'C.UTF-8'},
  });
}

function uniqueBy(items, key) {
  const value = new Map(items.map(item => [item[key], item]));
  requireValue(value.size === items.length, 'PERSONAL_INTEGRATIONS_AUTHORITY_DUPLICATE_IDENTITY');
  return value;
}

// Проверяет исходники 107→110. Существующие привязки и producer permissions
// сохраняются, новые operation IDs разрешены только зарегистрированными producers.
export function verifyPersonalIntegrationsAuthorityPolicy() {
  const predecessor = telegramPolicySource('desired');
  const raw = readFileSync(desiredPolicy, 'utf8');
  requireValue(sha256(raw) === policySHA256, 'PERSONAL_INTEGRATIONS_POLICY_SOURCE_CHANGED');
  const before = JSON.parse(predecessor.raw), after = JSON.parse(raw);
  requireValue(before.policy_revision === 107 && after.policy_revision === 110 &&
    after.policy.authority_abi_version === before.policy.authority_abi_version,
    'PERSONAL_INTEGRATIONS_POLICY_VERSION_REJECTED');
  const existing = uniqueBy(before.policy.operation_bindings, 'operation_id');
  const target = uniqueBy(after.policy.operation_bindings, 'operation_id');
  requireValue(existing.size === 475 && target.size === 552 &&
    [...existing].every(([operation, binding]) => target.has(operation) && fingerprint(target.get(operation)) === fingerprint(binding)),
    'PERSONAL_INTEGRATIONS_EXISTING_BINDINGS_CHANGED');
  const additions = new Set([...target.keys()].filter(operation => !existing.has(operation)));
  requireValue([...additions].every(operation => target.get(operation).permission === operation),
    'PERSONAL_INTEGRATIONS_ADDED_PERMISSION_REJECTED');
  const producers = uniqueBy(before.policy.authority_proof_producers, 'producer_id');
  const nextProducers = uniqueBy(after.policy.authority_proof_producers, 'producer_id');
  const produced = [];
  for (const [id, producer] of nextProducers) {
    const retained = producer.allowed_operation_ids.filter(operation => !additions.has(operation));
    const added = producer.allowed_operation_ids.filter(operation => additions.has(operation));
    requireValue(producers.has(id)
      ? fingerprint({...producer, allowed_operation_ids: retained}) === fingerprint(producers.get(id))
      : retained.length === 0 && added.length > 0,
    'PERSONAL_INTEGRATIONS_EXISTING_PRODUCER_CHANGED');
    produced.push(...added);
  }
  requireValue([...producers.keys()].every(id => nextProducers.has(id)) &&
    additions.size === produced.length && fingerprint([...additions].sort()) === fingerprint(produced.sort()),
    'PERSONAL_INTEGRATIONS_ADDED_PRODUCER_CLOSURE_REJECTED');
  const normalized = structuredClone(after);
  normalized.policy_revision = before.policy_revision;
  normalized.policy.operation_bindings = before.policy.operation_bindings;
  normalized.policy.authority_proof_producers = before.policy.authority_proof_producers;
  requireValue(fingerprint(normalized) === fingerprint(before), 'PERSONAL_INTEGRATIONS_EXISTING_POLICY_CHANGED');
  return {predecessor, desired: {raw, revision: after.policy_revision, sourceSHA256: policySHA256}, addedOperations: [...additions].sort()};
}

// Сравнивает точные байты policy projection с исходником и окружением установки.
export function validatePersonalIntegrationsAuthorityPolicy(raw, role, environment) {
  requireValue(role === 'predecessor' || role === 'desired', 'PERSONAL_INTEGRATIONS_POLICY_ROLE_REJECTED');
  const source = verifyPersonalIntegrationsAuthorityPolicy()[role];
  const rendered = renderAuthorityPolicyEnvironment(source.raw, environment);
  requireValue(raw === rendered.raw, 'PERSONAL_INTEGRATIONS_POLICY_RENDER_CHANGED');
  return {revision: source.revision, sha256: rendered.renderedSHA256, sourceSHA256: source.sourceSHA256};
}

// Создаёт только следующий revision реестра. Старые 17 получателей остаются
// прежними. Появляется один обязательный issuer artifact-retention.
export function planPersonalIntegrationsAuthorityRegistry(liveRaw) {
  const predecessor = validateTelegramRegistry(liveRaw, 'desired');
  const source = readFileSync(desiredRegistry, 'utf8');
  requireValue(sha256(source) === registrySHA256, 'PERSONAL_INTEGRATIONS_REGISTRY_SOURCE_CHANGED');
  const next = JSON.parse(encodeRegistry(source, 'json'));
  const frozen = structuredClone(next);
  frozen.source_revision = predecessor.sourceRevision;
  frozen.targets = frozen.targets.filter(target => target.workload_id !== newWorkload);
  const added = next.targets.filter(target => target.workload_id === newWorkload);
  requireValue(fingerprint(frozen) === fingerprint(predecessor.value) && added.length === 1 &&
    added[0].role === 'AUTHORIZATION_ISSUER' && added[0].workload_generation === 1 &&
    added[0].startup_readback_required === true &&
    Number.isSafeInteger(predecessor.sourceRevision + 1), 'PERSONAL_INTEGRATIONS_REGISTRY_PREDECESSOR_CHANGED');
  next.source_revision = predecessor.sourceRevision + 1;
  const raw = encodeRegistry(JSON.stringify(next), 'yaml');
  requireValue(fingerprint(JSON.parse(encodeRegistry(raw, 'json'))) === fingerprint(next),
    'PERSONAL_INTEGRATIONS_REGISTRY_RENDER_CHANGED');
  const readbacks = authorityReadbackContract(raw);
  requireValue(readbacks.registeredReadbackCount === 18 && readbacks.expectedReadbackCount === 16,
    'PERSONAL_INTEGRATIONS_REGISTRY_READBACKS_REJECTED');
  return {
    raw, predecessorRevision: predecessor.sourceRevision, predecessorSHA256: predecessor.sourceDigestSHA256,
    revision: next.source_revision, sha256: sha256(raw), sourceSHA256: registrySHA256, ...readbacks,
  };
}

// Состав обязательных readback выводится из закрытых pins source и 107 input.
export function personalIntegrationsAuthorityReadbackContract() {
  const {registeredReadbackCount, expectedReadbackCount, requiredReadbackTargetsSHA256} =
    planPersonalIntegrationsAuthorityRegistry(telegramRegistrySource('desired').raw);
  return {registeredReadbackCount, expectedReadbackCount, requiredReadbackTargetsSHA256};
}

// На переходе проверяемся по точным байтам, полученным из прежнего registry.
export function validatePersonalIntegrationsAuthorityRegistry(raw, predecessorRaw) {
  const expected = planPersonalIntegrationsAuthorityRegistry(predecessorRaw);
  requireValue(raw === expected.raw, 'PERSONAL_INTEGRATIONS_REGISTRY_RENDER_CHANGED');
  return expected;
}
