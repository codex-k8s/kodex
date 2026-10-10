import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { requireIdle } from './runner-policy-model.mjs';
const sql = readFileSync(new URL('./runner-pending-promotions-readback.sql', import.meta.url), 'utf8');
const body = sql.replace(/--[^\n]*/g, '');
test('pending promotion diagnostic is one bounded read-only snapshot with no caller parameters', () => {
  assert.match(body, /^\s*BEGIN TRANSACTION READ ONLY;/);
  assert.match(body, /SET LOCAL statement_timeout = '10s';/);
  assert.match(body, /WITH pending AS MATERIALIZED/);
  assert.match(body, /SELECT id, ref FROM pending ORDER BY ref LIMIT 64/);
  assert.match(body, /octet_length\(body::text\) <= 131072/);
  assert.match(body, /'status', 'BODY_LIMIT_EXCEEDED', 'complete', false, 'items', '\[\]'::jsonb/);
  assert.match(body, /COMMIT;\s*$/);
  assert.doesNotMatch(body, /\b(?:UPDATE|INSERT|DELETE|MERGE|COPY|CALL|DO|ALTER|CREATE|DROP|TRUNCATE|GRANT|REVOKE|FOR\s+UPDATE|FOR\s+SHARE)\b/i);
  assert.doesNotMatch(body.replace(/'(?:[^']|'')*'/g, '').replace(/::[a-z_]+/gi, ''), /\$\d|@[a-z_]|:[a-z_]|SELECT\s+[a-z_.]*\*/i);
  assert.equal(body.split(';').filter(statement => statement.trim()).length, 4);
});
test('JSON projection is exact metadata whitelist; no source, specification, credential or token values', () => {
  const keys = [...body.matchAll(/'([a-z][A-Za-z0-9]*)'\s*,/g)].map(match => match[1]);
  assert.deepEqual([...new Set(keys)].sort(), [
    'admissionState', 'artifactBuildVersion', 'artifactRecipeGeneration', 'artifactRecipeVersion', 'artifactRef', 'artifactVersion',
    'at', 'authorizationExpired', 'buildRef', 'buildState', 'buildVersion', 'claimExpired', 'complete', 'items', 'kind', 'limit',
    'latestNativeBuild', 'nativeCandidateStructurallyVisible', 'nativeEvidenceComplete', 'organizationRef', 'ownerScopeConsistent',
    'projectRef', 'promotedReferencePresent', 'promotionRequestRef', 'promotionRequested', 'promotionState', 'recipeGeneration',
    'recipePinsMatch', 'recipeRef', 'recipeState', 'recipeVersion', 'requestState', 'requestTupleMatches', 'scopeKind', 'status', 'total',
    'unrequestedCurrentCandidate', 'version', 'workerClaimStructurallySelectable',
  ].sort());
  // Predicate проверяет наличие claim, но его значения не входят в SELECT/JSON.
  const projection = body.slice(body.indexOf('facts AS ('));
  assert.doesNotMatch(projection, /\bspecification\b|\b(?:admission|promotion)_(?:claim|authorization)_token|\bcredentials?\b|\b(?:dsn|password|private_key|requested_by|staging_reference|tool_inventory_json)\b/i);
  assert.match(body, /'complete', \(SELECT count\(\*\) FROM pending\) <= 64/);
});
test('structural diagnostics use exact revised candidate predicate and distinguish rejected admission from effect-bearing promotion', () => {
  const idleSQL = readFileSync(new URL('./runner-policy-readback.sql', import.meta.url), 'utf8');
  const predicate = text => text.slice(text.indexOf("WHERE (artifact.admission_state = 'ACCEPTED'"),text.indexOf("\n)",text.indexOf("WHERE (artifact.admission_state = 'ACCEPTED'"))).trim();
  assert.equal(predicate(body),predicate(idleSQL));
  assert.match(body, /artifact\.admission_state = 'ACCEPTED' AND artifact\.promotion_state = 'PENDING'/);
  assert.match(body, /artifact\.promotion_state IN \('CLAIMED', 'AUTHORIZED'\)/);
  assert.match(body, /artifact\.admission_state NOT IN \('PENDING', 'CLAIMED', 'ACCEPTED', 'REJECTED', 'FAILED'\)/);
  for (const pin of ['version', 'generation', 'spec_sha256', 'policy_revision', 'policy_sha256', 'role_runtime_contract_revision', 'role_runtime_contract_sha256']) {
    const right = pin === 'version' ? 'recipe_version' : pin === 'generation' ? 'recipe_generation' : pin;
    assert.ok(body.includes(`recipe.${pin} = artifact.${right}`), pin);
  }
  assert.match(body, /ORDER BY candidate\.created_at DESC, candidate\.updated_at DESC, candidate\.attempt DESC, candidate\.ref ASC/);
  assert.match(body, /AND promotion_state = 'PENDING' AND NOT promotion_requested/);
  assert.match(body, /request_state = 'PROMOTING' AND promotion_state = 'CLAIMED' AND claim_expired/);
  assert.match(body, /request_state = 'PROMOTING' AND promotion_state = 'AUTHORIZED' AND authorization_expired/);
  const idle = {at:new Date().toISOString(),openBuilds:0,pendingAdmissions:0,pendingPromotions:1,activeRuntimeRuns:0,claimedRuntimeLeases:0,promotedArtifactCount:55,promotedPinsSHA256:'a'.repeat(64)};
  assert.throws(() => requireIdle(idle), /FRESH_IDLE_OWNER_STATE_REQUIRED/);
});
test('bounded metadata body fits budget even with maximum canonical refs and int64 versions', () => {
  const itemBlock = body.slice(body.indexOf("'scopeKind'"), body.indexOf(') AS item FROM facts'));
  const item = Object.fromEntries([...itemBlock.matchAll(/'([A-Za-z][A-Za-z0-9]*)'\s*,/g)].map(([_, key]) => [key, key.endsWith('Ref') ? 'x'.repeat(96) : /(?:Version|Generation)$/.test(key) ? 9223372036854775807n.toString() : key.endsWith('State') ? 'AUTHORIZED' : key === 'scopeKind' ? 'ORGANIZATION' : true]));
  const output = {version:1,kind:'RUNNER_PENDING_PROMOTIONS_READBACK',status:'OBSERVED',at:new Date().toISOString(),total:64,limit:64,complete:true,items:Array(64).fill(item)};
  assert.ok(Buffer.byteLength(JSON.stringify(output)) < 131072);
});
