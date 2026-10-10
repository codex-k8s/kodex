#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

const ensure = (ok, code) => { if (!ok) throw new Error(code); };
const refPattern = /^[A-Za-z0-9_-]{8,96}$/;
const inputKeys = ['connectionRef', 'agentRef', 'connectionVersion', 'resolveRef', 'resolveVersion', 'queryRef', 'queryVersion'];
export function validateHealthReadbackInput(input) {
  ensure(input && Object.keys(input).length === inputKeys.length && inputKeys.every((k) => Object.hasOwn(input, k)), 'READBACK_ARGUMENT_INVALID');
  for (const key of ['connectionRef', 'agentRef', 'resolveRef', 'queryRef']) ensure(typeof input[key] === 'string' && refPattern.test(input[key]), 'READBACK_REF_INVALID');
  for (const key of ['connectionVersion', 'resolveVersion', 'queryVersion']) ensure(Number.isSafeInteger(input[key]) && input[key] > 0, 'READBACK_VERSION_INVALID');
  ensure(input.resolveRef !== input.queryRef, 'READBACK_GRANT_PAIR_INVALID');
}

const booleanKeys = ['scopeFound', 'connectionVersionMatches', 'connectionEnabled', 'connectionActive', 'agentEnabled', 'definitionReady', 'credentialConfigured', 'freshHealthReceipt', 'refreshEligible', 'pendingChainComplete', 'pendingWithinWindow'];
const resultKeys = ['observedAt', 'connectionRef', 'agentRef', 'connectionVersion', 'expectedConnectionVersion', 'connectionState', 'definitionVersion', 'requiredGrantCount', 'matchedGrantCount', 'receipts', ...booleanKeys];
const receiptBooleans = ['workerValid', 'connectionRefMatches', 'connectionVersionMatches', 'configurationMatches', 'definitionMatches', 'credentialMatches'];
const receiptKeys = ['ref', 'purpose', 'state', 'attempt', 'createdAt', 'completedAt', ...receiptBooleans];
const timestamp = (value, nullable = false) => (nullable && value === null) || (typeof value === 'string' && value.length < 48 && Number.isFinite(Date.parse(value)));
const exactKeys = (value, keys) => value && !Array.isArray(value) && Object.keys(value).length === keys.length && keys.every((k) => Object.hasOwn(value, k));

// Закрытая проекция: неизвестный ключ отвергается, а не пересылается в stdout.
export function diagnoseManagedMCPHealth(input, evidence) {
  validateHealthReadbackInput(input);
  ensure(exactKeys(evidence, resultKeys) && booleanKeys.every((k) => typeof evidence[k] === 'boolean'), 'READBACK_ENVELOPE_INVALID');
  ensure(evidence.connectionRef === input.connectionRef && evidence.agentRef === input.agentRef && evidence.expectedConnectionVersion === input.connectionVersion, 'READBACK_SCOPE_MISMATCH');
  ensure(timestamp(evidence.observedAt) && (evidence.connectionVersion === null || Number.isSafeInteger(evidence.connectionVersion) && evidence.connectionVersion > 0), 'READBACK_TIME_OR_VERSION_INVALID');
  ensure(evidence.connectionState === null || ['NOT_CONNECTED', 'CONNECTED', 'DEGRADED', 'DISABLED', 'TESTING'].includes(evidence.connectionState), 'READBACK_CONNECTION_STATE_INVALID');
  ensure(evidence.definitionVersion === null || typeof evidence.definitionVersion === 'string' && /^[A-Za-z0-9_.-]{1,96}$/.test(evidence.definitionVersion), 'READBACK_DEFINITION_VERSION_INVALID');
  ensure(['requiredGrantCount', 'matchedGrantCount'].every((k) => Number.isSafeInteger(evidence[k]) && evidence[k] >= 0), 'READBACK_GRANT_COUNT_INVALID');
  ensure(Array.isArray(evidence.receipts) && evidence.receipts.length <= 16, 'READBACK_RECEIPT_COUNT_INVALID');
  for (const receipt of evidence.receipts) {
    ensure(exactKeys(receipt, receiptKeys) && refPattern.test(receipt.ref ?? '') && ['OWNER_TEST', 'MANAGED_MCP_REFRESH'].includes(receipt.purpose) && ['DUE', 'CLAIMED', 'SUCCEEDED', 'FAILED', 'CANCELLED'].includes(receipt.state), 'READBACK_RECEIPT_INVALID');
    ensure(Number.isSafeInteger(receipt.attempt) && receipt.attempt > 0 && timestamp(receipt.createdAt) && timestamp(receipt.completedAt, true) && receiptBooleans.every((k) => typeof receipt[k] === 'boolean'), 'READBACK_RECEIPT_INVALID');
  }
  const causes = [];
  if (!evidence.scopeFound) causes.push('CURRENT_SCOPE_NOT_FOUND');
  else {
    if (!evidence.agentEnabled) causes.push('CURRENT_AGENT_DISABLED');
    if (!evidence.connectionEnabled || !evidence.connectionActive) causes.push('CURRENT_CONNECTION_DISABLED');
    if (evidence.connectionState !== 'CONNECTED') causes.push('CURRENT_CONNECTION_NOT_CONNECTED');
    if (!evidence.connectionVersionMatches) causes.push('CURRENT_CONNECTION_VERSION_MISMATCH');
    if (!evidence.definitionReady) causes.push('CURRENT_DEFINITION_NOT_READY');
    if (!evidence.credentialConfigured) causes.push('CURRENT_CREDENTIAL_NOT_CONFIGURED');
    if (evidence.requiredGrantCount !== 2 || evidence.matchedGrantCount !== 2) causes.push('CURRENT_GRANT_PAIR_MISMATCH');
    if (!evidence.freshHealthReceipt) {
      if (!evidence.refreshEligible) causes.push('CURRENT_REFRESH_INELIGIBLE');
      if (evidence.pendingChainComplete && !evidence.pendingWithinWindow) causes.push('CURRENT_HEALTH_PENDING_WINDOW_EXPIRED');
      if (!evidence.pendingWithinWindow) {
        const successful = evidence.receipts.find((r) => r.state === 'SUCCEEDED');
        if (!successful) causes.push('CURRENT_HEALTH_RECEIPT_MISSING');
        else if (!successful.workerValid || !successful.connectionRefMatches || !successful.configurationMatches || !successful.definitionMatches || !successful.credentialMatches) causes.push('CURRENT_HEALTH_RECEIPT_INPUT_MISMATCH');
        else causes.push('CURRENT_HEALTH_RECEIPT_STALE');
      }
    }
  }
  return {
    status: causes.length ? 'CURRENT_CONFLICT_CANDIDATE' : evidence.freshHealthReceipt ? 'CURRENT_HEALTH_FRESH' : 'CURRENT_HEALTH_PENDING',
    historicalFailure: 'UNKNOWN', scopeKind: 'AGENT', causes, evidence,
  };
}

// Только унаследованный transport владельца; нет поиска/чтения DSN или Secrets.
// Ни PGOPTIONS, ни произвольный executable/SQL/connection string не принимаются.
export function healthReadbackEnvironment(source) {
  const env = { PATH: source.PATH ?? '/usr/bin:/bin', LC_ALL: 'C', PGCONNECT_TIMEOUT: '5' };
  for (const key of ['PGHOST', 'PGHOSTADDR', 'PGPORT', 'PGDATABASE', 'PGUSER', 'PGPASSWORD', 'PGPASSFILE', 'PGSERVICE', 'PGSERVICEFILE', 'PGSSLMODE', 'PGSSLROOTCERT', 'PGSSLCERT', 'PGSSLKEY']) if (source[key] !== undefined) env[key] = source[key];
  ensure(env.PGSERVICE || env.PGHOST && env.PGDATABASE && env.PGUSER, 'READBACK_TRANSPORT_REQUIRED');
  ensure(env.PGHOST?.startsWith('/') || env.PGSSLMODE === 'verify-full', 'READBACK_VERIFIED_TRANSPORT_REQUIRED');
  return env;
}

export function readManagedMCPHealth(input, source = process.env, execute = execFileSync) {
  validateHealthReadbackInput(input);
  const env = healthReadbackEnvironment(source);
  const names = ['connection_ref', 'agent_ref', 'connection_version', 'resolve_ref', 'resolve_version', 'query_ref', 'query_version'];
  const args = ['-X', '-qAt', '--no-password', '-v', 'ON_ERROR_STOP=1', ...inputKeys.flatMap((key, i) => ['-v', `${names[i]}=${input[key]}`]), '-f', fileURLToPath(new URL('./managed-mcp-health-readback.sql', import.meta.url))];
  let raw;
  try { raw = execute('psql', args, { encoding: 'utf8', env, timeout: 15000, maxBuffer: 128 << 10, stdio: ['ignore', 'pipe', 'pipe'] }); }
  catch { throw new Error('READBACK_QUERY_FAILED'); }
  let evidence;
  try { evidence = JSON.parse(raw); } catch { throw new Error('READBACK_RESULT_INVALID'); }
  return diagnoseManagedMCPHealth(input, evidence);
}

function main() {
  const names = { '--connection-ref': 'connectionRef', '--agent-ref': 'agentRef', '--connection-version': 'connectionVersion', '--resolve-ref': 'resolveRef', '--resolve-version': 'resolveVersion', '--query-ref': 'queryRef', '--query-version': 'queryVersion' };
  const args = process.argv.slice(2), input = {};
  while (args.length) {
    const key = names[args.shift()]; ensure(key && args.length && !Object.hasOwn(input, key), 'READBACK_ARGUMENT_INVALID');
    const value = args.shift();
    ensure(!key.endsWith('Version') || /^[1-9][0-9]{0,15}$/.test(value), 'READBACK_VERSION_INVALID');
    input[key] = key.endsWith('Version') ? Number(value) : value;
  }
  process.stdout.write(`${JSON.stringify(readManagedMCPHealth(input))}\n`);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); } catch (error) { process.stderr.write(`${JSON.stringify({ status: 'FAIL', code: /^READBACK_[A-Z_]+$/.test(error.message) ? error.message : 'READBACK_FAILED' })}\n`); process.exitCode = 1; }
}
