// Закрытый состав перехода ART 050 → 051. Старые файлы миграций не переписываются.
export const workflowFileProvenanceMigration = Object.freeze({
  profile:'workflow-file-provenance-backup',
  before:20261121000100,
  after:20261121000200,
  command:'up-workflow-file-provenance',
  receipt:'Workflow file provenance migration completed at version 20261121000200',
  proofFile:'workflow-file-provenance-backup-migration.sql',
  artifactFile:'20261121000100_control_plane_artifact_retention_grant.sql',
  artifactSHA256:'c09bb5bc3959d260dba7e164a09339f48ff9860a8eb07d3f8f89fa7a35ccd8f1',
  migrationFile:'20261121000200_workflow_file_provenance.sql',
  migrationSHA256:'aa30021850a2ead2898508d25c86e4a1520029b50c9b1f6cbca30b880f4eade6',
});

export function isWorkflowFileProvenanceMigrationProfile(profile) {
  return profile===workflowFileProvenanceMigration.profile;
}

// Для 050→051 используются строго канонические полномочия backup-reader.
function validWorkflowFileProvenanceBackupCatalog(proof,version,catalogDigest) {
  return proof?.profile === 'WORKFLOW_FILE_PROVENANCE_BACKUP_051' && proof.database === 'control_plane' &&
    proof.readOnly === 'on' && proof.gooseVersion === version && proof.serverVersionNum === 180003 &&
    proof.backupQualification === 'CANONICAL_BACKUP' && proof.publicSequenceSafe === true &&
    proof.historyMatches === true && proof.schemaMatches === true && typeof proof.artifactPredecessorApplied === 'boolean' &&
    proof.schemaCatalog?.catalogDigest === catalogDigest;
}

// Счётчики из owner PostgreSQL покрывают runtime, workspace, images, integration
// и configuration producers. CLAIM соединения проверяются после positive join.
const workflowFileProvenanceDrainCounters = Object.freeze([
  'activeRuntimeLeases', 'activeArchiveTasks', 'activeRuntimeRuns', 'activeRunNodes', 'activeSessionTurns',
  'claimedRuntimeLeases', 'openBuilds', 'pendingAdmissions', 'pendingPromotions', 'activeModelCatalogTasks',
  'activeRenewalTasks', 'activeIntegrationInvocations', 'activeIntegrationConnectionTests',
  'activePersonalCredentialTests', 'activeRepositoryBundles', 'activeConfigurationSources',
  'activeConfigurationWritebacks', 'activeInteractionDeliveries',
]);

// Перед запуском Job нужен ровно ART 050, отсутствие активной работы и join старых claims.
// До остановки компонента допустимы соединения, ожидающие новых claims.
export function validWorkflowFileProvenanceBackupMigrationPrecondition(proof,requireStopped=false) {
  return validWorkflowFileProvenancePredecessorCatalog(proof) &&
    proof.workspaceVolumeIdle === true && workflowFileProvenanceDrainCounters.every(key => proof[key] === 0) &&
    Number.isSafeInteger(proof.oldClaimSessions) && proof.oldClaimSessions >= 0 &&
    (!requireStopped || proof.oldClaimSessions === 0);
}

// Проверяет обе воспроизводимые цепочки, не смешивая их отпечатки.
export function validWorkflowFileProvenanceBackupMigrationPostcondition(proof) {
  const catalogDigest = proof?.artifactPredecessorApplied === true
    ? '31d4205868c2e96b508f8c1ce3572a55477407580486053a23c16044227d80ad'
    : 'f5116ee01372580ef4b06106afa3cdbb1a79c3f424740711d281aaf8879e4788';
  return validWorkflowFileProvenanceBackupCatalog(proof,20261121000200,catalogDigest);
}

// Допуск развёрнутого стенда требует сохранения точного ART 050 в истории Goose.
export function validWorkflowFileProvenanceComposedPostcondition(proof) {
  return proof?.artifactPredecessorApplied===true&&validWorkflowFileProvenanceBackupMigrationPostcondition(proof);
}


// Каталог ART 050 подтверждает отсутствие DDL даже в живом прежнем runtime.
export function validWorkflowFileProvenancePredecessorCatalog(proof) {
  return proof?.artifactPredecessorApplied===true &&
    validWorkflowFileProvenanceBackupCatalog(proof,workflowFileProvenanceMigration.before,'58532c2f03d49c8499e9113ce9e6d5a687c92d41581641885bf98396d4ec6ab2');
}

export const workflowFileAbortIntent='WORKFLOW_FILE_PROVENANCE_ABORT_INTENT';
export const workflowFileAbortAck='WORKFLOW_FILE_PROVENANCE_ABORT_ACK';

export const workflowFileCancelledCloseAnnotation='kodex.dev/workflow-file-provenance-cancelled-close';

export const workflowFileUnboundRestoreIntent='WORKFLOW_FILE_PROVENANCE_UNBOUND_RESTORE_INTENT';
