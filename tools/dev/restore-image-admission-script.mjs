#!/usr/bin/env node
import { readFileSync, realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { SOURCE_ROOT, CANDIDATE_SCRIPT_SHA256, digest, fingerprint, runRefreshCLI, verifyConfigMapBoundary } from './refresh-image-admission-diagnostics.mjs';
import { traceCandidate } from './refresh-image-admission-transport-trace.mjs';
import { requireIdle } from '../release/runner-policy-model.mjs';

// Единственный forward-only возврат после terminal receipt прежней attempt.
// Field manager принадлежит обычному deploy; grants/policy/images не меняются.
export const RESTORE_PINS = Object.freeze({
  configmapUID: 'cc73eb9b-f263-496a-b383-05070e5f845f',
  resourceVersion: '453810',
  baselineDataSHA256: '73c2e05e14b769fa9a4bb39ce7c9ffff63aa37ccee59f21edee17ba6bdba44f1',
});
export const TRACE_SHA256 = '8fcdcfda8893f2f8e54ecf8f7d828eedec0d4ace0e26db1dc279adb59d50e298';
const SQL_SHA256 = '9c147119d49fbf24279ef52affecd9fcd3a9bdc113665bc804a7cb80c6e36ec4';
const namespace = 'kodex-system', id = '320b1cbe46eb01e9908e39eb630a27b2';
const check = (value, code) => { if (!value) throw new Error(code); };

export function restoreCandidate(current, canonical, options) {
  for (const [key, value] of Object.entries(RESTORE_PINS)) check(options[key] === value, 'RESTORE_PIN_MISMATCH');
  verifyConfigMapBoundary(current, options);
  check(digest(canonical) === CANDIDATE_SCRIPT_SHA256, 'SCRIPT_CANONICAL_MISMATCH');
  check(digest(current.data['image-admission.sh']) === TRACE_SHA256 && current.data['image-admission.sh'] === traceCandidate(canonical), 'SCRIPT_TRACE_MISMATCH');
  const result = structuredClone(current);
  result.data['image-admission.sh'] = canonical;
  return result;
}

// Пресет доступен только из нового CLI, без переключателя пропуска boundary.
export function restoreWorkspaceBoundary(readSQL = () => readFileSync(`${SOURCE_ROOT}/tools/dev/supply-chain-owner-readback.sql`, 'utf8')) {
  let publishedPins;
  return (get, kubectl) => {
    const controller = get('-n', namespace, 'deployment', 'image-admission-controller');
    check(controller.metadata?.uid === 'b0d061c8-a12f-4366-9413-cee2a8e774dd' && !controller.metadata.deletionTimestamp &&
      controller.spec?.replicas === 0 && controller.status?.observedGeneration === controller.metadata.generation &&
      ['replicas', 'readyReplicas', 'availableReplicas'].every(key => (controller.status[key] ?? 0) === 0), 'CONTROLLER_NOT_QUIESCED');
    const pvc = get('-n', namespace, 'persistentvolumeclaims', `--field-selector=metadata.name=mc-admit-${id}`);
    check(Array.isArray(pvc.items) && pvc.items.length === 0, 'WORKSPACE_NOT_ABSENT');
    for (const kind of ['jobs', 'pods']) {
      const result = get('-n', namespace, kind, '-l', `kodex.dev/image-admission-id=${id}`);
      check(Array.isArray(result.items) && result.items.length === 0, 'WORKSPACE_WORKLOAD_NOT_ABSENT');
    }
    const postgres = get('-n', namespace, 'pod', 'kodex-postgresql-0');
    check(postgres.metadata?.uid === '9c861600-ba8a-431a-a55c-6849955693f1' && !postgres.metadata.deletionTimestamp &&
      postgres.status?.conditions?.some(value => value.type === 'Ready' && value.status === 'True'), 'OWNER_POSTGRES_IDENTITY_MISMATCH');
    const sql = readSQL();
    check(digest(sql) === SQL_SHA256, 'OWNER_READ_SQL_MISMATCH');
    const owner = JSON.parse(kubectl(['-n', namespace, 'exec', '-i', 'kodex-postgresql-0', '--', 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', 'control_plane'], sql));
    requireIdle(owner);
    const pins = fingerprint({ promotedArtifactCount: owner.promotedArtifactCount, promotedPinsSHA256: owner.promotedPinsSHA256 });
    check(!publishedPins || publishedPins === pins, 'PUBLISHED_PINS_CHANGED');
    publishedPins = pins;
  };
}

if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { runRefreshCLI('restore-image-admission-script.mjs', value => value, restoreCandidate, restoreWorkspaceBoundary()); }
  catch (error) {
    const code = /^[A-Z_]+$/.test(error?.message ?? '') ? error.message : 'RESTORE_FAILED';
    process.stderr.write(`Image admission script restore failed: ${code}\n`);
    process.exitCode = 1;
  }
}
