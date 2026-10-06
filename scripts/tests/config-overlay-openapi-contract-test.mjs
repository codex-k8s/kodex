// Публичная проверка: timeout 30s node --test scripts/tests/config-overlay-openapi-contract-test.mjs.
// Golden принадлежит producer runtimecontract; source и consumer не задают второй набор keys.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../../', import.meta.url));
const schemas = JSON.parse(execFileSync('yq', ['-o=json', '.components.schemas',
  `${root}contracts/openapi/control-api-gateway/v1/openapi.yaml`], { timeout: 10000 }));
const producer = JSON.parse(readFileSync(
  `${root}services/external/control-api-gateway/internal/transport/http/testdata/runtime_overlay_schema.json`, 'utf8'));

function checkOverlayContract(candidate) {
  const keys = producer.fields.map(field => field.key);
  const fields = candidate.ConfigOverlaySchema.properties.fields;
  assert.equal(new Set(keys).size, keys.length, 'PRODUCER_KEYS_UNIQUE');
  assert.ok(keys.includes('web_search'), 'PRODUCER_HOSTED_SEARCH');
  assert.deepEqual(candidate.ConfigOverlayField.properties.key.enum, keys, 'OVERLAY_EXACT_CANONICAL_KEYS');
  assert.equal(fields.minItems, keys.length, 'OVERLAY_EXACT_MINIMUM_FIELDS');
  assert.equal(fields.maxItems, keys.length, 'OVERLAY_EXACT_MAXIMUM_FIELDS');
  assert.equal(fields.items.$ref, '#/components/schemas/ConfigOverlayField', 'OVERLAY_TYPED_FIELDS');
  assert.equal(candidate.ConfigOverlayField.additionalProperties, false, 'OVERLAY_FIELD_CLOSED');
  assert.equal(candidate.ConfigOverlaySchema.additionalProperties, false, 'OVERLAY_SCHEMA_CLOSED');
  assert.deepEqual(candidate.ConfigOverlaySchema.properties.maximumBytes.enum, [producer.maximumBytes], 'OVERLAY_PRODUCER_BYTE_LIMIT');
}

test('OpenAPI overlay uses the complete canonical producer key set and cardinality', () => {
  checkOverlayContract(schemas);
});

test('unknown, missing, duplicate and obsolete four-field OpenAPI snapshots are rejected', () => {
  for (const mutate of [
    value => { value.ConfigOverlayField.properties.key.enum[1] = 'web_search_custom'; },
    value => { value.ConfigOverlayField.properties.key.enum.splice(1, 1); },
    value => { value.ConfigOverlayField.properties.key.enum[2] = 'web_search'; },
    value => { value.ConfigOverlaySchema.properties.fields.minItems = 4; },
    value => { value.ConfigOverlaySchema.properties.fields.maxItems = 4; },
  ]) {
    const candidate = structuredClone(schemas);
    mutate(candidate);
    assert.throws(() => checkOverlayContract(candidate), /OVERLAY_/);
  }
});
