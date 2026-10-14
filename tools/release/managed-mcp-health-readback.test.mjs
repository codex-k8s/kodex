import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { diagnoseManagedMCPHealth, readManagedMCPHealth, validateHealthReadbackInput, healthReadbackEnvironment } from './managed-mcp-health-readback.mjs';

const input = { connectionRef: 'int_fixture01', agentRef: 'agt_fixture01', connectionVersion: 26, resolveRef: 'grt_resolve01', resolveVersion: 1, queryRef: 'grt_query001', queryVersion: 1 };
const receipt = () => ({ ref: 'ict_fixture01', purpose: 'MANAGED_MCP_REFRESH', state: 'SUCCEEDED', attempt: 1, createdAt: '2026-10-10T15:00:00Z', completedAt: '2026-10-10T15:00:01Z', workerValid: true, connectionRefMatches: true, connectionVersionMatches: true, configurationMatches: true, definitionMatches: true, credentialMatches: true });
const evidence = () => ({ observedAt: '2026-10-10T15:00:02Z', connectionRef: input.connectionRef, agentRef: input.agentRef, connectionVersion: 26, expectedConnectionVersion: 26, connectionState: 'CONNECTED', definitionVersion: 'v1', requiredGrantCount: 2, matchedGrantCount: 2, receipts: [receipt()], scopeFound: true, connectionVersionMatches: true, connectionEnabled: true, connectionActive: true, agentEnabled: true, definitionReady: true, credentialConfigured: true, freshHealthReceipt: true, refreshEligible: true, pendingChainComplete: false, pendingWithinWindow: false });

test('fresh readback is current-only and never proves historical failure or acceptance', () => {
  const result = diagnoseManagedMCPHealth(input, evidence());
  assert.equal(result.status, 'CURRENT_HEALTH_FRESH');
  assert.equal(result.historicalFailure, 'UNKNOWN');
  assert.deepEqual(result.causes, []);
});
test('pending accepted only with fresh window; expiry is a conflict candidate', () => {
  const value = evidence(); value.freshHealthReceipt = false; value.pendingChainComplete = true; value.pendingWithinWindow = true;
  assert.equal(diagnoseManagedMCPHealth(input, value).status, 'CURRENT_HEALTH_PENDING');
  value.pendingWithinWindow = false;
  assert.deepEqual(diagnoseManagedMCPHealth(input, value).causes, ['CURRENT_HEALTH_PENDING_WINDOW_EXPIRED', 'CURRENT_HEALTH_RECEIPT_STALE']);
});
test('independent connection, definition and grant pin failures remain visible', () => {
  const value = evidence(); value.connectionVersionMatches = false; value.definitionReady = false; value.matchedGrantCount = 1;
  assert.deepEqual(diagnoseManagedMCPHealth(input, value).causes, ['CURRENT_CONNECTION_VERSION_MISMATCH', 'CURRENT_DEFINITION_NOT_READY', 'CURRENT_GRANT_PAIR_MISMATCH']);
});
test('missing, mismatched, stale and ineligible health are distinguished', () => {
  const value = evidence(); value.freshHealthReceipt = false;
  assert.ok(diagnoseManagedMCPHealth(input, value).causes.includes('CURRENT_HEALTH_RECEIPT_STALE'));
  value.receipts[0].configurationMatches = false;
  assert.ok(diagnoseManagedMCPHealth(input, value).causes.includes('CURRENT_HEALTH_RECEIPT_INPUT_MISMATCH'));
  value.receipts = []; value.refreshEligible = false;
  assert.deepEqual(diagnoseManagedMCPHealth(input, value).causes, ['CURRENT_REFRESH_INELIGIBLE', 'CURRENT_HEALTH_RECEIPT_MISSING']);
});
test('exact refs and closed input disallow injection and unspecified versions', () => {
  for (const mutated of [{ ...input, agentRef: "agt_x';DROP TABLE x;--" }, { ...input, connectionVersion: 0 }, { ...input, queryRef: input.resolveRef }, { ...input, sql: 'arbitrary' }]) assert.throws(() => validateHealthReadbackInput(mutated), /^Error: READBACK_/);
});
test('unknown fields, secret-bearing fields and malformed receipts fail closed', () => {
  for (const value of [{ ...evidence(), configuration: {} }, { ...evidence(), agentRef: 'agt_foreign01' }, { ...evidence(), requiredGrantCount: '2' }, { ...evidence(), receipts: [{ ...receipt(), credentialSHA256: 'untrusted' }] }, { ...evidence(), receipts: Array.from({ length: 17 }, receipt) }]) assert.throws(() => diagnoseManagedMCPHealth(input, value), /^Error: READBACK_/);
});
test('transport is inherited, isolated and verified; unsafe service does not bypass TLS', () => {
  assert.throws(() => healthReadbackEnvironment({}), /READBACK_TRANSPORT_REQUIRED/);
  assert.throws(() => healthReadbackEnvironment({ PGSERVICE: 'owner' }), /READBACK_VERIFIED_TRANSPORT_REQUIRED/);
  const env = healthReadbackEnvironment({ PGHOST: '/run/postgresql', PGDATABASE: 'control_plane', PGUSER: 'owner_observer', PGPASSWORD: 'synthetic-only', PGOPTIONS: '-c role=owner', GH_TOKEN: 'synthetic-only', NODE_OPTIONS: 'unsafe' });
  assert.equal(env.PGPASSWORD, 'synthetic-only');
  assert.equal(env.PGOPTIONS, undefined); assert.equal(env.GH_TOKEN, undefined); assert.equal(env.NODE_OPTIONS, undefined);
});
test('execution pins SQL file and suppresses raw stderr, DSN and password argv', () => {
  const source = { PGHOST: '/run/postgresql', PGDATABASE: 'control_plane', PGUSER: 'owner_observer', PGPASSWORD: 'synthetic-only' };
  const result = readManagedMCPHealth(input, source, (file, args, options) => {
    assert.equal(file, 'psql'); assert.ok(args.includes('-X')); assert.ok(args.includes('ON_ERROR_STOP=1'));
    assert.ok(args.at(-1).endsWith('/managed-mcp-health-readback.sql')); assert.ok(!args.join(' ').includes(source.PGPASSWORD));
    assert.equal(options.timeout, 15000); assert.equal(options.maxBuffer, 128 << 10);
    return JSON.stringify(evidence());
  });
  assert.equal(result.status, 'CURRENT_HEALTH_FRESH');
  assert.throws(() => readManagedMCPHealth(input, source, () => { throw new Error('raw synthetic private output'); }), /^Error: READBACK_QUERY_FAILED$/);
});
test('SQL is read-only, bounded, uses quoted psql variables and closed projection', () => {
  const sql = readFileSync(new URL('./managed-mcp-health-readback.sql', import.meta.url), 'utf8');
  assert.match(sql, /REPEATABLE READ READ ONLY/); assert.match(sql, /statement_timeout = '10s'/); assert.match(sql, /LIMIT 16/); assert.match(sql, /a\.depth<3/);
  assert.ok(!/\b(INSERT|UPDATE|DELETE|ALTER|SET ROLE|CREATE|DROP)\b/i.test(sql));
  assert.ok(!/'(?:public_configuration|input_snapshot|credentialSHA256|credentialRevisionRef|secret_ref)'\s*,/.test(sql));
  for (const key of ['connection_ref', 'agent_ref', 'connection_version', 'resolve_ref', 'resolve_version', 'query_ref', 'query_version']) assert.ok(sql.includes(`:'${key}'`));
});
