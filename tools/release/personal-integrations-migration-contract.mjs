import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const ensure = (ok, code) => { if (!ok) throw Error(code); };
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const manifestName = 'personal-integrations-schema-manifest.json';
const manifestRaw = readFileSync(new URL('./' + manifestName, import.meta.url));
const manifest = JSON.parse(manifestRaw);
export const isPersonalIntegrationsMigrationProfile = profile =>
  profile === 'personal-integrations' || profile === 'personal-integrations-backup';

// Historical installation seal и actual authority predecessor раздельны.
// Это только source publication shape; перед effect role/schema00200 обязан
// заново подтвердить typed owner reader с прежним migration Job/Pod receipt.
export function validPersonalIntegrationsAuthoritySchemaHeads(base,before,after){
  return [20260912000200,20261115000200].includes(base?.['internal-rpc-authority'])&&
    before?.['internal-rpc-authority']===20261115000200&&after?.['internal-rpc-authority']===20261115000200&&
    base?.['email-bridge']===before?.['email-bridge']&&before?.['email-bridge']===after?.['email-bridge'];
}

// Метаданные исполнимого протокола для привязки immutable plan. Подтверждение
// каждого этапа выдаёт его live owner reader по исходной цепочке владения.
export const personalIntegrationsDeliveryPrerequisite = Object.freeze({
  schema: 'kodex.personal-integrations-delivery-prerequisite/v1',
  protocol: 'PHASE_BOUND_OWNER_READBACK_V1', authorityPolicyRevision: 110,
  additionalIssuer: 'artifact-retention',
  requirements: Object.freeze(['SIGNED_OWNER_TERMINAL', 'EXACT_ISSUER_REGISTRY',
    'STAGED_ARTIFACT_RETENTION', 'EXACT_NETWORK_AND_PROJECTION_DELTA']),
});

const personalDeliveryPhases = new Set([
  'plan', 'inspect', 'check', 'stop', 'stopController', 'apply', 'retry',
  'handoff', 'start-readers', 'resume', 'restore-api-traffic',
]);

// Разрешает только маршруты с проверками владельца на соответствующем этапе.
// Прежний API reader режима 045 и остальные maintenance routes сюда не входят.
export function requirePersonalIntegrationsDeliveryPrerequisite(profile, phase) {
  if (!isPersonalIntegrationsMigrationProfile(profile)) return;
  ensure(personalDeliveryPhases.has(phase) ||
    (profile === 'personal-integrations-backup' && phase === 'backup-maintenance'),
  'PERSONAL_INTEGRATIONS_DELIVERY_PHASE_REJECTED');
}

// Live backup использует только canonical backup. Table-only snapshots остаются
// материалом явного offline restore, без fallback в migration admission.
export const personalIntegrationsCatalog = Object.freeze(Object.fromEntries(
  ['normal', 'backup'].map(mode => [mode, Object.freeze({
    before: manifest.snapshots[mode]['20261114140000'].catalog,
    after: manifest.snapshots[mode]['20261114170000'].catalog,
  })]),
));

// Сверяет весь captured Goose cohort, оба reader и generated proof bytes с
// исходниками plan. Clean SHA/OCI остаются отдельными существующими проверками.
// Новый DDL требует нового capture/generation, а не подмены ожидаемых digest.
export function verifyPersonalIntegrationsMigrationSources(source) {
  const read = name => readFileSync(resolve(source.path, name));
  ensure(read('tools/release/' + manifestName).equals(manifestRaw), 'PERSONAL_INTEGRATIONS_MANIFEST_SOURCE_CHANGED');
  ensure(digest(read('tools/release/managed-kubernetes-catalog.sql')) === manifest.catalogReaderSHA256,
    'PERSONAL_INTEGRATIONS_CATALOG_READER_CHANGED');
  for (const pin of manifest.migrationPins)
    ensure(digest(read('services/internal/control-plane/cmd/cli/migrations/' + pin.file)) === pin.sha256,
      'PERSONAL_INTEGRATIONS_MIGRATION_SOURCE_CHANGED');
  for (const mode of ['', '-backup']) for (const phase of ['precondition', 'postcondition', 'recovery']) {
    const name = `personal-integrations${mode}-migration-${phase}.sql`;
    ensure(read('tools/release/' + name).equals(readFileSync(new URL('./' + name, import.meta.url))),
      'PERSONAL_INTEGRATIONS_PROOF_SOURCE_CHANGED');
  }
  return {schemaManifestSHA256: digest(manifestRaw), captureSourceSHA: manifest.sourceSHA,
    captureIndexSHA256: manifest.captureIndexSHA256};
}

// Полное сравнение captured catalog, sealed history и конечных idle counters.
const exactCatalog=(actual,expected)=>actual!==null&&typeof actual==='object'&&!Array.isArray(actual)&&
  Object.keys(actual).length===Object.keys(expected).length&&Object.entries(expected).every(([key,value])=>JSON.stringify(actual[key])===JSON.stringify(value));

const personalIntegrationsIdleFields=Object.freeze(['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns',
 'claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks',
 'activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources',
 'activeConfigurationWritebacks','activeInteractionDeliveries','activeBackupSessions']);

// Только полные captured heads того же normal/canonical-backup cohort. Это не допуск writers.
export const personalIntegrationsRecoveryHeads=Object.freeze([20261114140000,20261114150000,20261114160000,20261114170000]);

// Повтор начинается с точного текущего каталога и sealed history, сохраняя весь stopped-owner drain.
// Final049 здесь также требует idle: это pre-Start rejoin, не обычный postcondition работающего cohort.
export function validPersonalIntegrationsRecoveryProof(proof,backup){
 const version=proof?.gooseVersion,mode=backup?'backup':'normal';
 return personalIntegrationsRecoveryHeads.includes(version)&&
  proof.profile==='PERSONAL_INTEGRATIONS_'+(backup?'BACKUP_':'')+'RECOVERY_V1'&&
  proof.database==='control_plane'&&proof.readOnly==='on'&&proof.serverVersionNum===180003&&
  proof.historyMatches===true&&proof.schemaMatches===true&&proof.publicSequenceSafe===true&&
  proof.backupQualification===(backup?'CANONICAL_BACKUP':'NONE')&&
  exactCatalog(proof.schemaCatalog,manifest.snapshots[mode][version].catalog)&&
  proof.workspaceOwnerIdle===true&&proof.oldClaimSessions===0&&
  personalIntegrationsIdleFields.every(key=>proof[key]===0);
}

export function validPersonalIntegrationsMigrationProof(proof, backup, before, requireStopped=true) {
 const mode=backup?'backup':'normal';
 const profile='PERSONAL_INTEGRATIONS_'+(backup?'BACKUP_':'')+(before?'PREDECESSOR_':'')+'V1';
 const version=before?20261114140000:20261114170000;
 if(proof?.profile!==profile||proof.database!=='control_plane'||proof.readOnly!=='on'||proof.gooseVersion!==version||
    proof.serverVersionNum!==180003||proof.historyMatches!==true||proof.schemaMatches!==true||proof.publicSequenceSafe!==true||
    proof.backupQualification!==(backup?'CANONICAL_BACKUP':'NONE'))return false;
 const catalog=manifest;
 if(!exactCatalog(proof.schemaCatalog,catalog.snapshots[mode][version].catalog))return false;
 return !before||(proof.workspaceOwnerIdle===true&&Number.isSafeInteger(proof.oldClaimSessions)&&proof.oldClaimSessions>=0&&
  (!requireStopped||proof.oldClaimSessions===0)&&personalIntegrationsIdleFields.every(key=>proof[key]===0));
}
