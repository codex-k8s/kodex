import { githubRepositoryScopeCatalog } from './github-repository-scope-catalog.gen.mjs';
import { validatedStablePublication } from './stable-image-publication.mjs';
import { componentMigrationCohortSchema, isComponentMigrationProfile, isComponentMigrationPublication, componentMigrationNames, componentMigrationTargetNames, componentMigrationPublication, componentMigrationTarget, planComponentMigrationHandoff, componentMigrationLifecycle, readComponentServingInventory } from './portable-component-migration.mjs';
import { fingerprint } from './scoped-release.mjs';
import { createHash, randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, lstatSync, readFileSync, readdirSync, realpathSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { inspectSource, sourceGitArguments } from './application-source.mjs';
import { privateJournal } from '../dev/role-image-acceptance.mjs';
import { activeDeploymentPods, isProvenStoppedPod } from './stable-archive-transition.mjs';
import { workflowPDLCCatalogDigest } from './workflow-pdlc-catalog.gen.mjs';
import { workflowContentPageCatalogDigest } from './workflow-content-page-catalog.gen.mjs';
import { platformOIDCIdentityCatalogDigest, platformOIDCIdentityBackupCatalogDigest } from './platform-oidc-identity-catalog.gen.mjs';
import { personalCredentialsCatalogDigest, personalCredentialsBackupCatalogDigest, personalCredentialsPurgeGraph } from './personal-credentials-catalog.gen.mjs';
import { jiraCreateFieldsCatalog } from './jira-create-fields-catalog.gen.mjs';
import { authorityProofCounterCatalog } from './authority-proof-counter-catalog.gen.mjs';

const ensure = (ok, code) => { if (!ok) throw new Error(code); };
const namespace = 'kodex-system';
const immutableProfiles = {
  'github-repository-scope': {anchorProfile:'portable',version:'20261114120000',file:'20261114120000_github_repository_scope.sql',annotation:'kodex.dev/cp-github-repository-scope-migration-plan',previousVersion:20261113000100,emptyField:'githubRepositoryScopeAbsent',preconditionFile:'github-repository-scope-migration-precondition.sql',postconditionFile:'github-repository-scope-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261114120000_github_repository_scope.sql'])},
  'github-repository-scope-backup': {anchorProfile:'portable',version:'20261114120000',file:'20261114120000_github_repository_scope.sql',annotation:'kodex.dev/cp-github-repository-scope-backup-migration-plan',previousVersion:20261113000100,emptyField:'githubRepositoryScopeAbsent',preconditionFile:'github-repository-scope-backup-migration-precondition.sql',postconditionFile:'github-repository-scope-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261114120000_github_repository_scope.sql'])},
  'workspace-unbound-stop': {
    anchorProfile:'portable',version:'20261113000100',file:'20261113000100_workspace_unbound_stop.sql',
    annotation:'kodex.dev/cp-workspace-unbound-stop-migration-plan',previousVersion:20261112000100,
    emptyField:'unboundStopAbsent',preconditionFile:'workspace-unbound-stop-migration-precondition.sql',
    postconditionFile:'workspace-unbound-stop-migration-postcondition.sql',
    requiredMigrations:Object.freeze(['20261113000100_workspace_unbound_stop.sql']),
  },
  'workspace-archive-abi': {
    anchorProfile:'portable',
    version:'20261112000100',file:'20261112000100_workspace_archive_runtime_abi.sql',
    annotation:'kodex.dev/cp-workspace-archive-abi-migration-plan',previousVersion:20261111000200,
    emptyField:'archiveGuardPredecessor',preconditionFile:'workspace-archive-abi-migration-precondition.sql',
    postconditionFile:'workspace-archive-abi-migration-postcondition.sql',
    requiredMigrations:Object.freeze(['20261112000100_workspace_archive_runtime_abi.sql']),
  },
  'opencode-native-sessions': {
    version:'20261111000200',file:'20261111000200_opencode_native_sessions.sql',
    annotation:'kodex.dev/cp-opencode-native-migration-plan',previousVersion:20261111000100,
    emptyField:'sessionDataEmpty',preconditionFile:'opencode-native-sessions-migration-precondition.sql',
    postconditionFile:'opencode-native-sessions-migration-postcondition.sql',
    requiredMigrations:Object.freeze(['20261111000200_opencode_native_sessions.sql']),
  },
  'workflow-workspace': {
    version:'20261111000100',file:'20261111000100_workflow_workspace_scope.sql',
    annotation:'kodex.dev/cp-workflow-workspace-migration-plan',previousVersion:20261110000200,
    emptyField:'workflowWorkspaceAbsent',preconditionFile:'workflow-workspace-migration-precondition.sql',
    postconditionFile:'workflow-workspace-migration-postcondition.sql',
    requiredMigrations:Object.freeze(['20261111000100_workflow_workspace_scope.sql']),
  },
  'managed-workspace': {
    version:'20261110000200',file:'20261110000200_managed_workspace.sql',
    annotation:'kodex.dev/cp-managed-workspace-migration-plan',previousVersion:20261110000100,
    emptyField:'managedWorkspaceAbsent',preconditionFile:'managed-workspace-migration-precondition.sql',
    postconditionFile:'managed-workspace-migration-postcondition.sql',
    requiredMigrations:Object.freeze(['20261110000200_managed_workspace.sql']),
  },
  'artifact-optional-antivirus': {version:'20261110000100',file:'20261110000100_artifact_optional_antivirus.sql',annotation:'kodex.dev/cp-artifact-optional-antivirus-migration-plan',previousVersion:20261109000100,emptyField:'artifactOptionalAntivirusAbsent',preconditionFile:'artifact-optional-antivirus-migration-precondition.sql',postconditionFile:'artifact-optional-antivirus-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261110000100_artifact_optional_antivirus.sql'])},
  'integration-resource-policies': {version:'20261109000100',file:'20261109000100_integration_resource_policies.sql',annotation:'kodex.dev/cp-integration-resource-policies-migration-plan',previousVersion:20261108000100,emptyField:'integrationResourcePoliciesAbsent',preconditionFile:'integration-resource-policies-migration-precondition.sql',postconditionFile:'integration-resource-policies-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261109000100_integration_resource_policies.sql'])},
  'integration-resource-policies-backup': {version:'20261109000100',file:'20261109000100_integration_resource_policies.sql',annotation:'kodex.dev/cp-integration-resource-policies-backup-migration-plan',previousVersion:20261108000100,emptyField:'integrationResourcePoliciesAbsent',preconditionFile:'integration-resource-policies-backup-migration-precondition.sql',postconditionFile:'integration-resource-policies-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261109000100_integration_resource_policies.sql'])},
  'authority-proof-counter': {version:'20261108000100',file:'20261108000100_authority_proof_counter.sql',annotation:'kodex.dev/cp-authority-proof-counter-migration-plan',previousVersion:20261107000100,emptyField:'authorityProofCounterAbsent',preconditionFile:'authority-proof-counter-migration-precondition.sql',postconditionFile:'authority-proof-counter-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261108000100_authority_proof_counter.sql'])},
  'authority-proof-counter-backup': {version:'20261108000100',file:'20261108000100_authority_proof_counter.sql',annotation:'kodex.dev/cp-authority-proof-counter-backup-migration-plan',previousVersion:20261107000100,emptyField:'authorityProofCounterAbsent',preconditionFile:'authority-proof-counter-backup-migration-precondition.sql',postconditionFile:'authority-proof-counter-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261108000100_authority_proof_counter.sql'])},
  'jira-create-fields': {version:'20261107000100',file:'20261107000100_jira_create_fields_compatibility.sql',annotation:'kodex.dev/cp-jira-create-fields-migration-plan',previousVersion:20261106000100,emptyField:'personalCredentialsCatalog',preconditionFile:'jira-create-fields-migration-precondition.sql',postconditionFile:'jira-create-fields-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261107000100_jira_create_fields_compatibility.sql'])},
  'jira-create-fields-backup': {version:'20261107000100',file:'20261107000100_jira_create_fields_compatibility.sql',annotation:'kodex.dev/cp-jira-create-fields-backup-migration-plan',previousVersion:20261106000100,emptyField:'personalCredentialsCatalog',preconditionFile:'jira-create-fields-backup-migration-precondition.sql',postconditionFile:'jira-create-fields-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261107000100_jira_create_fields_compatibility.sql'])},
  'personal-credentials': {version:'20261106000100',file:'20261106000100_personal_integration_credentials.sql',annotation:'kodex.dev/cp-personal-credentials-migration-plan',previousVersion:20261105000100,emptyField:'personalCredentialsAbsent',preconditionFile:'personal-credentials-migration-precondition.sql',postconditionFile:'personal-credentials-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261106000100_personal_integration_credentials.sql'])},
  'personal-credentials-backup': {version:'20261106000100',file:'20261106000100_personal_integration_credentials.sql',annotation:'kodex.dev/cp-personal-credentials-backup-migration-plan',previousVersion:20261105000100,emptyField:'personalCredentialsAbsent',preconditionFile:'personal-credentials-backup-migration-precondition.sql',postconditionFile:'personal-credentials-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261106000100_personal_integration_credentials.sql'])},
  'platform-oidc-identity': {version:'20261105000100',file:'20261105000100_platform_oidc_identity.sql',annotation:'kodex.dev/cp-platform-oidc-migration-plan',previousVersion:20261030000100,emptyField:'platformOIDCAbsent',preconditionFile:'platform-oidc-identity-migration-precondition.sql',postconditionFile:'platform-oidc-identity-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261105000100_platform_oidc_identity.sql'])},
  'platform-oidc-identity-backup': {version:'20261105000100',file:'20261105000100_platform_oidc_identity.sql',annotation:'kodex.dev/cp-platform-oidc-backup-migration-plan',previousVersion:20261030000100,emptyField:'platformOIDCAbsent',preconditionFile:'platform-oidc-identity-backup-migration-precondition.sql',postconditionFile:'platform-oidc-identity-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261105000100_platform_oidc_identity.sql'])},
  'workflow-content-page': {version:'20261030000100',file:'20261030000100_workflow_content_page.sql',annotation:'kodex.dev/cp-content-page-migration-plan',previousVersion:20261029000300,emptyField:'workflowContentPagePredecessor',preconditionFile:'workflow-content-page-migration-precondition.sql',postconditionFile:'workflow-content-page-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261030000100_workflow_content_page.sql'])},
  'workflow-content-page-backup': {version:'20261030000100',file:'20261030000100_workflow_content_page.sql',annotation:'kodex.dev/cp-content-page-backup-migration-plan',previousVersion:20261029000300,emptyField:'workflowContentPagePredecessor',preconditionFile:'workflow-content-page-backup-migration-precondition.sql',postconditionFile:'workflow-content-page-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261030000100_workflow_content_page.sql'])},
  'workflow-pdlc': {version:'20261029000300',file:'20261029000300_workflow_pdlc_evidence.sql',annotation:'kodex.dev/cp-workflow-pdlc-migration-plan',previousVersion:20261029000200,emptyField:'workflowPDLCAbsent',preconditionFile:'workflow-pdlc-migration-precondition.sql',postconditionFile:'workflow-pdlc-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261029000300_workflow_pdlc_evidence.sql'])},
  'workflow-pdlc-backup': {version:'20261029000300',file:'20261029000300_workflow_pdlc_evidence.sql',annotation:'kodex.dev/cp-workflow-pdlc-backup-migration-plan',previousVersion:20261029000200,emptyField:'workflowPDLCAbsent',preconditionFile:'workflow-pdlc-backup-migration-precondition.sql',postconditionFile:'workflow-pdlc-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261029000300_workflow_pdlc_evidence.sql'])},
  'github-pr': {version:'20261029000200',file:'20261029000200_github_pr_collaboration_admission.sql',annotation:'kodex.dev/cp-github-pr-migration-plan',previousVersion:20261029000100,emptyField:'githubHistoricalCatalog',preconditionFile:'github-pr-migration-precondition.sql',postconditionFile:'github-pr-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261029000200_github_pr_collaboration_admission.sql'])},
  'github-pr-backup': {version:'20261029000200',file:'20261029000200_github_pr_collaboration_admission.sql',annotation:'kodex.dev/cp-github-pr-backup-migration-plan',previousVersion:20261029000100,emptyField:'githubHistoricalCatalog',preconditionFile:'github-pr-backup-migration-precondition.sql',postconditionFile:'github-pr-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261029000200_github_pr_collaboration_admission.sql'])},
  'github-autonomous': {version:'20261029000100',file:'20261029000100_github_historical_grant_admission.sql',annotation:'kodex.dev/cp-github-autonomous-migration-plan',previousVersion:20261028000100,emptyField:'githubHistoricalAbsent',preconditionFile:'github-autonomous-migration-precondition.sql',postconditionFile:'github-autonomous-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261029000100_github_historical_grant_admission.sql'])},
  'github-autonomous-backup': {version:'20261029000100',file:'20261029000100_github_historical_grant_admission.sql',annotation:'kodex.dev/cp-github-autonomous-backup-migration-plan',previousVersion:20261028000100,emptyField:'githubHistoricalAbsent',preconditionFile:'github-autonomous-backup-migration-precondition.sql',postconditionFile:'github-autonomous-backup-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261029000100_github_historical_grant_admission.sql'])},
  'workspace-backup': {version:'20261028000100',file:'20261028000100_workspace_restore_owner.sql',annotation:'kodex.dev/cp-workspace-backup-migration-plan',previousVersion:20261026001500,emptyField:'workspaceBackupSafeToMigrate',preconditionFile:'workspace-backup-migration-precondition.sql',postconditionFile:'workspace-backup-migration-postcondition.sql',executionCommand:'up-workspace-backup-compatible',jobSuffix:'-backup',requiredMigrations:Object.freeze(['20261026001600_workspace_checkpoint_tasks.sql','20261026001700_workspace_volume_custody.sql','20261027000100_ssh_compose_runtime.sql','20261028000100_workspace_restore_owner.sql'])},
  'workspace-restore': {version:'20261028000100',file:'20261028000100_workspace_restore_owner.sql',annotation:'kodex.dev/cp-workspace-restore-migration-plan',previousVersion:20261026001000,emptyField:'workspaceVolumeSSHAbsent',preconditionFile:'workspace-restore-migration-precondition.sql',postconditionFile:'workspace-restore-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261026001100_legacy_workflow_rework.sql','20261026001200_workflow_exception_context.sql','20261026001300_workflow_upstream_rework.sql','20261026001400_workflow_review_qa_evidence.sql','20261026001500_session_workspace_storage.sql','20261026001600_workspace_checkpoint_tasks.sql','20261026001700_workspace_volume_custody.sql','20261027000100_ssh_compose_runtime.sql','20261028000100_workspace_restore_owner.sql'])},
  'workspace-volume-ssh': {version:'20261027000100',file:'20261027000100_ssh_compose_runtime.sql',annotation:'kodex.dev/cp-workspace-volume-ssh-migration-plan',previousVersion:20261026001000,emptyField:'workspaceVolumeSSHAbsent',preconditionFile:'workspace-volume-ssh-migration-precondition.sql',postconditionFile:'workspace-volume-ssh-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261026001100_legacy_workflow_rework.sql','20261026001200_workflow_exception_context.sql','20261026001300_workflow_upstream_rework.sql','20261026001400_workflow_review_qa_evidence.sql','20261026001500_session_workspace_storage.sql','20261026001600_workspace_checkpoint_tasks.sql','20261026001700_workspace_volume_custody.sql','20261027000100_ssh_compose_runtime.sql'])},
  'workflow-upstream-rework': {version:'20261026001300',file:'20261026001300_workflow_upstream_rework.sql',annotation:'kodex.dev/cp-workflow-upstream-rework-migration-plan',previousVersion:20261026001200,emptyField:'workflowUpstreamReworkAbsent',preconditionFile:'workflow-upstream-rework-migration-precondition.sql',postconditionFile:'workflow-upstream-rework-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261026000300_server_driven_workflow.sql','20261026000400_workflow_attempt_journal.sql','20261026000500_workflow_artifact_visibility_retention.sql','20261026000600_workflow_exception_episodes.sql','20261026000700_workflow_input_guard_binding.sql','20261026000800_workflow_exception_terminal_decision.sql','20261026000900_workflow_effect_rejoin.sql','20261026001000_workflow_effect_execution_rejoin.sql','20261026001100_legacy_workflow_rework.sql','20261026001200_workflow_exception_context.sql','20261026001300_workflow_upstream_rework.sql'])},
  'workflow-exception-context': {version:'20261026001200',file:'20261026001200_workflow_exception_context.sql',annotation:'kodex.dev/cp-workflow-exception-context-migration-plan',previousVersion:20261026001100,emptyField:'workflowExceptionContextAbsent',preconditionFile:'workflow-exception-context-migration-precondition.sql',postconditionFile:'workflow-exception-context-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261026000300_server_driven_workflow.sql','20261026000400_workflow_attempt_journal.sql','20261026000500_workflow_artifact_visibility_retention.sql','20261026000600_workflow_exception_episodes.sql','20261026000700_workflow_input_guard_binding.sql','20261026000800_workflow_exception_terminal_decision.sql','20261026000900_workflow_effect_rejoin.sql','20261026001000_workflow_effect_execution_rejoin.sql','20261026001100_legacy_workflow_rework.sql','20261026001200_workflow_exception_context.sql'])},
  'workflow-review-qa-evidence': {version:'20261026001400',file:'20261026001400_workflow_review_qa_evidence.sql',annotation:'kodex.dev/cp-workflow-review-qa-evidence-migration-plan',previousVersion:20261026001300,emptyField:'reviewQAEvidenceAbsent',preconditionFile:'workflow-review-qa-evidence-migration-precondition.sql',postconditionFile:'workflow-review-qa-evidence-migration-postcondition.sql',requiredMigrations:Object.freeze(["20261026000300_server_driven_workflow.sql", "20261026000400_workflow_attempt_journal.sql", "20261026000500_workflow_artifact_visibility_retention.sql", "20261026000600_workflow_exception_episodes.sql", "20261026000700_workflow_input_guard_binding.sql", "20261026000800_workflow_exception_terminal_decision.sql", "20261026000900_workflow_effect_rejoin.sql", "20261026001000_workflow_effect_execution_rejoin.sql", "20261026001100_legacy_workflow_rework.sql", "20261026001200_workflow_exception_context.sql", "20261026001300_workflow_upstream_rework.sql", "20261026001400_workflow_review_qa_evidence.sql"])},
  'legacy-workflow-rework': {version:'20261026001100',file:'20261026001100_legacy_workflow_rework.sql',annotation:'kodex.dev/cp-legacy-workflow-rework-migration-plan',previousVersion:20261026001000,emptyField:'legacyReworkAbsent',preconditionFile:'legacy-workflow-rework-migration-precondition.sql',postconditionFile:'legacy-workflow-rework-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261026000300_server_driven_workflow.sql', '20261026000400_workflow_attempt_journal.sql', '20261026000500_workflow_artifact_visibility_retention.sql', '20261026000600_workflow_exception_episodes.sql', '20261026000700_workflow_input_guard_binding.sql', '20261026000800_workflow_exception_terminal_decision.sql', '20261026000900_workflow_effect_rejoin.sql', '20261026001000_workflow_effect_execution_rejoin.sql', '20261026001100_legacy_workflow_rework.sql'])},
  'ssh-compose': {version:'20261027000100',file:'20261027000100_ssh_compose_runtime.sql',annotation:'kodex.dev/cp-ssh-compose-migration-plan',previousVersion:20261026001000,emptyField:'sshComposeAbsent',preconditionFile:'ssh-compose-migration-precondition.sql',postconditionFile:'ssh-compose-migration-postcondition.sql',requiredMigrations:Object.freeze(['20261027000100_ssh_compose_runtime.sql'])},
  'server-workflow': {version:'20261026001000',file:'20261026001000_workflow_effect_execution_rejoin.sql',annotation:'kodex.dev/cp-server-workflow-migration-plan',previousVersion:20261026000200,emptyField:'serverWorkflowAbsent',preconditionFile:'server-workflow-migration-precondition.sql',postconditionFile:'server-workflow-migration-postcondition.sql',requiredMigrations:Object.freeze(["20261026000300_server_driven_workflow.sql", "20261026000400_workflow_attempt_journal.sql", "20261026000500_workflow_artifact_visibility_retention.sql", "20261026000600_workflow_exception_episodes.sql", "20261026000700_workflow_input_guard_binding.sql", "20261026000800_workflow_exception_terminal_decision.sql", "20261026000900_workflow_effect_rejoin.sql", "20261026001000_workflow_effect_execution_rejoin.sql"])},
  'project-purge-authority': {version:'20261026000200',file:'20261026000200_project_purge_authority_acl.sql',annotation:'kodex.dev/cp-project-purge-authority-migration-plan',previousVersion:20261026000100,emptyField:'purgeAuthoritySafeToMigrate',preconditionFile:'project-purge-authority-migration-precondition.sql',postconditionFile:'project-purge-authority-migration-postcondition.sql'},
  'owner-gate-expiry': {version:'20261026000100',file:'20261026000100_owner_gate_expiry.sql',annotation:'kodex.dev/cp-owner-gate-expiry-migration-plan',previousVersion:20261025000200,emptyField:'expiryAbsent',preconditionFile:'owner-gate-expiry-migration-precondition.sql',postconditionFile:'owner-gate-expiry-migration-postcondition.sql'},
  'outbox-index': {version:'20261025000100',file:'20261025000100_outbox_unpublished_predecessor_index.sql',annotation:'kodex.dev/cp-outbox-index-migration-plan',previousVersion:20261020000200,emptyField:'indexAbsent',preconditionFile:'control-plane-migration-precondition.sql'},
  'repository-bundle': {version:'20261025000200',file:'20261025000200_repository_bundle_publications.sql',annotation:'kodex.dev/cp-repository-bundle-migration-plan',previousVersion:20261025000100,emptyField:'publicationAbsent',preconditionFile:'repository-bundle-migration-precondition.sql',postconditionFile:'repository-bundle-migration-postcondition.sql'},
};
// Один закрытый registry определяет и план, и CLI dispatch; caller не добавляет profile через payload.
export function isImmutableMigrationProfile(profile) {
  return typeof profile==='string' && Object.hasOwn(immutableProfiles,profile);
}
// Только известные immutable профили; caller не задаёт version/SQL/authority.
export function immutableMigrationProfile(profile = 'outbox-index') {
  ensure(isImmutableMigrationProfile(profile),'IMMUTABLE_MIGRATION_PROFILE_INVALID');
  return Object.freeze({...immutableProfiles[profile]});
}
const caBundleSpec = {sources:[{secret:{name:'kodex-installation-ca',key:'tls.crt'}}],
  target:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':namespace}},configMap:{key:'ca.pem'}}};
const dsnPath = '/var/run/secrets/kodex/control-plane/postgres-migration/dsn';
const waitForPostgres = {
  name:'wait-for-postgresql',image:'docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7',
  command:['/bin/sh','-ec'],args:['until pg_isready --host="$POSTGRES_HOST" --port=5432 --timeout=2; do sleep 2; done'],
  env:[{name:'POSTGRES_HOST',value:'control-plane-postgresql-rw.kodex-system.svc.cluster.local'}],
  resources:{requests:{cpu:'10m',memory:'16Mi'},limits:{memory:'64Mi'}},
  securityContext:{runAsNonRoot:true,runAsUser:70,runAsGroup:70,allowPrivilegeEscalation:false,readOnlyRootFilesystem:true,capabilities:{drop:['ALL']}},
};
const volumes = [
  { name:'postgres-migration', secret:{secretName:'control-plane-postgres-migration',defaultMode:292} },
  { name:'postgres-ca', configMap:{name:'control-plane-postgresql-ca',defaultMode:288,items:[{key:'ca.pem',path:'ca.pem'}]} },
];
const mounts = [
  {name:'postgres-migration',mountPath:'/var/run/secrets/kodex/control-plane/postgres-migration',readOnly:true},
  {name:'postgres-ca',mountPath:'/var/run/config/kodex/control-plane/postgres',readOnly:true},
];

// Повторно связывает проверенный stable build с точным publication receipt, без registry writes.
export function immutableMigrationImage(source, evidence, profile = 'outbox-index') {
  if (isComponentMigrationPublication(evidence))
    return componentMigrationPublication(evidence,source,profile).components.find(c => c.component === 'control-plane').imageRef;
  return validatedStablePublication(source.revision,evidence,'control-plane');
}

// Штатный render задаёт только существующую migrator boundary; executable spec строится закрыто заново.
function scopedCheckName(migrationVersion, profile, id) {
  ensure(typeof id === 'string' && /^[a-zA-Z0-9-]{1,128}$/.test(id), 'MIGRATION_CHECK_IDENTITY_INVALID');
  return `control-plane-migration-check-${migrationVersion}-${fingerprint({purpose:'readonly-check/v1',profile,id}).slice(0,12)}`;
}

function validateCheckIdentity(plan) {
  ensure(!Object.hasOwn(plan,'checkIdentity') || (plan.version === 2 && plan.checkIdentity === 'plan-id-v1' &&
    plan.checkJob?.metadata.name === scopedCheckName(plan.migrationVersion,plan.profile,plan.id)), 'MIGRATION_CHECK_IDENTITY_INVALID');
}

export function planImmutableControlPlaneMigration(before, source, inspect, id, evidence, attempt = 1, profile = 'outbox-index', checkIdentity = 'plan-id-v1') {
  const configuration = immutableMigrationProfile(profile);
  const {version:migrationVersion} = configuration;
  ensure([1,2].includes(attempt), 'MIGRATION_ATTEMPT_INVALID');
  ensure(checkIdentity === null || checkIdentity === 'plan-id-v1', 'MIGRATION_CHECK_IDENTITY_INVALID');
  ensure(before?.kind === 'Job' && before.apiVersion === 'batch/v1' && before.metadata?.name === 'control-plane-migrate' &&
    before.metadata.namespace === namespace && before.metadata.uid && before.metadata.resourceVersion &&
    before.metadata.labels?.['app.kubernetes.io/name'] === 'control-plane' &&
    before.metadata.labels?.['app.kubernetes.io/component'] === 'migration' &&
    [undefined,'kodex'].includes(before.metadata.labels?.['app.kubernetes.io/part-of']) &&
    before.metadata.labels?.['kodex.dev/environment'] === (configuration.anchorProfile === 'portable' ? 'production' : 'staging') &&
    (configuration.anchorProfile !== 'portable' || (before.metadata.labels?.['kodex.dev/profile'] === 'web-only' &&
      before.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' && !before.metadata.labels?.['kodex.dev/local-profile'])), 'MIGRATOR_IDENTITY_INVALID');
  const pod = before.spec?.template?.spec, container = pod?.containers?.[0];
  const migrationEnvironment = [{name:'CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE',value:dsnPath}];
  // Отключённый observability profile portable renderer сохраняется без произвольного env passthrough.
  if (configuration.anchorProfile === 'portable' && container?.env?.some(value => value.name === 'OTEL_SDK_DISABLED'))
    migrationEnvironment.push({name:'OTEL_SDK_DISABLED',value:'true'});
  // YAML 1.1 Kubernetes и YAML 1.2 yq по-разному переводят 0440 в JSON. Права из render
  // не переносятся: новый Job всегда назначает собственные явные десятичные modes.
  const refsOnly = values => (values ?? []).map(value => {
    const copy = structuredClone(value);
    if (copy.secret) delete copy.secret.defaultMode;
    if (copy.configMap) delete copy.configMap.defaultMode;
    return copy;
  });
  ensure(pod?.serviceAccountName === 'control-plane-migrator' && pod.automountServiceAccountToken === false &&
    pod.restartPolicy === 'Never' && pod.containers.length === 1 && container?.name === 'migrate' &&
    fingerprint(container.command) === fingerprint(['/usr/local/bin/control-plane-cli']) &&
    fingerprint(container.args) === fingerprint(['up']) &&
    fingerprint(container.env) === fingerprint(migrationEnvironment) &&
    !container.envFrom && fingerprint(container.volumeMounts) === fingerprint(mounts) &&
    fingerprint(refsOnly(pod.volumes)) === fingerprint(refsOnly(volumes)) &&
    !pod.hostNetwork && !pod.hostPID && !pod.hostIPC &&
    [...(pod.initContainers ?? []),...pod.containers].every(c => !(c.volumeMounts ?? []).some(m => !mounts.some(v => v.name === m.name))),
  'IMMUTABLE_MIGRATOR_BOUNDARY_INVALID');
  ensure(fingerprint(pod.initContainers) === fingerprint([waitForPostgres]), 'MIGRATION_CANONICAL_INIT_INVALID');
  ensure(/^[a-f0-9]{40}$/.test(source?.revision ?? '') && source.revision !== '0'.repeat(40) &&
    inspect(source.path).revision === source.revision, 'SOURCE_REVISION_MISMATCH');
  const image = immutableMigrationImage(source, evidence, profile);
  // Staging overlay не назначает app part-of исходному render. Новая собственная Job
  // получает фиксированную метку; actual CP/SA/Secret проверяются независимо от render.
  const labels = { ...before.metadata.labels, 'app.kubernetes.io/part-of':'kodex' };
    const job = {apiVersion:'batch/v1',kind:'Job',metadata:{name:`control-plane-migrate-${migrationVersion}${configuration.jobSuffix ?? ''}${attempt === 2 ? '-attempt2' : ''}`,namespace,labels},spec:{
    backoffLimit:0,activeDeadlineSeconds:300,ttlSecondsAfterFinished:86400,
    template:{metadata:{labels,annotations:{'kodex.dev/source-revision':source.revision,
      ...(isComponentMigrationPublication(evidence)
        ? {'kodex.dev/component-lock-sha256':evidence.components.find(c => c.component === 'control-plane').componentLockSHA256}
        : {'kodex.dev/image-context-sha256':evidence.publicationPlan.receipt.contextSHA256})}},spec:{
      serviceAccountName:'control-plane-migrator',automountServiceAccountToken:false,restartPolicy:'Never',enableServiceLinks:false,
      securityContext:{runAsNonRoot:true,runAsUser:10001,runAsGroup:10001,fsGroup:10001,fsGroupChangePolicy:'OnRootMismatch',seccompProfile:{type:'RuntimeDefault'}},
      initContainers:[structuredClone(waitForPostgres)],
      containers:[{name:'migrate',image,imagePullPolicy:'Always',command:['/usr/local/bin/control-plane-cli'],args:[configuration.executionCommand ?? 'up'],
        env:migrationEnvironment,
        securityContext:{runAsNonRoot:true,runAsUser:10001,runAsGroup:10001,readOnlyRootFilesystem:true,allowPrivilegeEscalation:false,capabilities:{drop:['ALL']}},
        resources:{requests:{cpu:'25m',memory:'32Mi'},limits:{cpu:'250m',memory:'128Mi'}},volumeMounts:structuredClone(mounts)}],volumes:structuredClone(volumes)}}}};
  const checkJob = structuredClone(job);
  checkJob.metadata.name = checkIdentity === null
    ? `control-plane-migration-check-${migrationVersion}${configuration.jobSuffix ?? ''}${attempt === 2 ? '-attempt2' : ''}`
    : scopedCheckName(migrationVersion,profile,id);
  // Отдельная readonly identity не меняет DDL name/budget. Job без TTL сохраняет
  // server-side fence неизвестного create внутри того же неизменного plan.
  delete checkJob.spec.ttlSecondsAfterFinished;
  checkJob.spec.template.spec.containers[0].args = ['check'];
  return {version:2,kind:'CONTROL_PLANE_MIGRATION',id,source,profile,migrationVersion,attempt,checkJob,
    ...(checkIdentity === null ? {} : {checkIdentity}),
    beforeUID:before.metadata.uid,beforeResourceVersion:before.metadata.resourceVersion,beforeSpecSHA256:fingerprint(before.spec),
    imageEvidence:structuredClone(evidence),job};
}

// Terminal readback требует один exact Job-owned Pod и фактический imageID, не только logs/Complete.
export function immutableMigrationExecution(job, plan, pods, expectedJob = plan.job) {
  validateCheckIdentity(plan);
  ensure(job.spec.backoffLimit === 0 && job.spec.activeDeadlineSeconds === 300 &&
    job.spec.ttlSecondsAfterFinished === expectedJob.spec.ttlSecondsAfterFinished && !job.spec.suspend && !job.spec.podFailurePolicy && !job.spec.successPolicy &&
    (job.spec.completionMode ?? 'NonIndexed') === 'NonIndexed' &&
    (job.spec.parallelism ?? 1) === 1 && (job.spec.completions ?? 1) === 1, 'MIGRATION_JOB_LIFECYCLE_CHANGED');
  const matching = pods.filter(p => p.metadata?.namespace === namespace &&
    p.metadata?.ownerReferences?.some(o => o.controller === true && o.kind === 'Job' && o.uid === job.metadata.uid));
  ensure(matching.length <= 1 && matching.length === pods.length, 'MIGRATION_EXECUTION_SCOPE_CHANGED');
  if (matching.length !== 1) return false;
  const pod = matching[0], state = pod.status?.containerStatuses?.[0], expected = expectedJob.spec.template.spec;
  const digest = isComponentMigrationPublication(plan.imageEvidence)
    ? componentMigrationPublication(plan.imageEvidence,plan.source,plan.profile).components.find(c => c.component === 'control-plane').digest
    : plan.imageEvidence.publicationPlan.image.digest;
  const expectedInit = expected.initContainers ?? [], actualInit = pod.spec?.initContainers ?? [];
  ensure(pod.metadata.uid && !pod.spec?.ephemeralContainers?.length && actualInit.length === expectedInit.length &&
    actualInit.every((item,index) => Object.keys(expectedInit[index]).every(key => fingerprint(item[key]) === fingerprint(expectedInit[index][key])) &&
      !item.envFrom && !(item.volumeMounts ?? []).length) &&
    !pod.spec?.hostNetwork && !pod.spec?.hostPID && !pod.spec?.hostIPC &&
    ['serviceAccountName','automountServiceAccountToken','restartPolicy','enableServiceLinks','securityContext','volumes']
      .every(key => fingerprint(pod.spec?.[key]) === fingerprint(expected[key])) &&
    pod.spec?.containers?.length === 1 &&
    Object.keys(expected.containers[0]).every(key => fingerprint(pod.spec.containers[0][key]) === fingerprint(expected.containers[0][key])) &&
    !pod.spec.containers[0].envFrom && (pod.status?.containerStatuses?.length ?? 0) <= 1,
  'MIGRATION_EXECUTION_CHANGED');
  if (!state) return false;
  ensure(state.name === 'migrate' && (state.restartCount ?? 0) === 0, 'MIGRATION_EXECUTION_CHANGED');
  const init = pod.status.initContainerStatuses ?? [];
  const initDone = init.length === (expected.initContainers ?? []).length && init.every((item,index) =>
    item.name === expected.initContainers[index].name && item.restartCount === 0 && item.state?.terminated?.exitCode === 0);
  return pod.status.phase === 'Succeeded' && state.state?.terminated?.exitCode === 0 && initDone &&
    (state.imageID === digest || state.imageID === `containerd://${digest}` || state.imageID?.endsWith(`@${digest}`));
}

// Ожидание не повторяет effects и получает независимый конечный бюджет после каждой мутации.
export async function waitMigrationMaintenance(observe, ready, code, {now = Date.now, sleep = ms => new Promise(resolve => setTimeout(resolve,ms)), budget = 120000} = {}) {
  const deadline = now() + budget;
  for (;;) {
    const actual = observe();
    if (ready(actual)) return actual;
    ensure(now() < deadline, code);
    await sleep(250);
  }
}

// Существующая CLI обслуживает только закрытые immutable schema-профили на прежней least-privilege границе.
export async function runImmutableControlPlaneMigration(phase, options, io) {
  const profile = options['--profile'] ?? 'outbox-index';
  const configuration = immutableMigrationProfile(profile);
  const {version:migrationVersion,file:migrationFile,annotation} = configuration;
  // Канонический portable сохраняет три replica; прежние stable profiles сохраняют две.
  const anchorReplicas = configuration.anchorProfile === 'portable' ? 3 : 2;
  const anchorLocalProfile = configuration.anchorProfile === 'portable' ? undefined : 'stable';
  const { kube, clusterUID, privateJSON, writePrivate, renderedBase, migrationReadback } = io;
  const context = options['--context'];
  const retainedPlan = phase === 'plan' ? undefined : privateJSON(options['--plan']);
  const componentPublication = phase === 'plan'
    ? (options['--component-publication'] ? privateJSON(options['--component-publication']) : undefined)
    : (isComponentMigrationPublication(retainedPlan.imageEvidence) ? retainedPlan.imageEvidence : undefined);
  const portableTarget = phase === 'plan'
    ? (options['--portable-target'] ? privateJSON(options['--portable-target']) : undefined) : retainedPlan.portableTarget;
  // Для044 прежний CP043 не является допустимым rollback после DDL. Normal и
  // backup требуют одного полного нового cohort, но разные exact schema proofs.
  ensure(!['github-repository-scope','github-repository-scope-backup'].includes(profile) || componentPublication,
    'COMPONENT_MIGRATION_COHORT_REQUIRED');
  ensure(componentPublication ? isComponentMigrationProfile(profile) && portableTarget && context === componentPublication.context
    : context === 'k3d-kodex' && !portableTarget, 'IMMUTABLE_MIGRATION_CONTEXT_INVALID');
  ensure(phase !== 'handoff' || componentPublication, 'COMPONENT_MIGRATION_HANDOFF_PROFILE_REQUIRED');
  const get = (kind, name) => JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
  const optionalJob = name => {
    const raw = kube(['-n',namespace,'get','job',name,'--ignore-not-found=true','-o','json']);
    return raw.trim() ? JSON.parse(raw) : undefined;
  };
  // Даже Secret запрашивается только узкой metadata projection: data/stringData не передаются процессу.
  const metadata = (kind, name) => {
    if (kind === 'configmap' && name === 'control-plane-postgresql-ca') {
      const raw = kube(['-n',namespace,'get',kind,name,'-o',
        'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.trust\\.cert-manager\\.io/bundle}{"|"}{.metadata.ownerReferences}']);
      const [uid,resourceVersion,bundleName,ownersJSON,...extra] = raw.trim().split('|');
      const owners = JSON.parse(ownersJSON);
      ensure(uid && /^\d+$/.test(resourceVersion ?? '') && !extra.length && bundleName === name &&
        Array.isArray(owners) && owners.length === 1 && owners[0].apiVersion === 'trust.cert-manager.io/v1alpha1' &&
        owners[0].kind === 'Bundle' && owners[0].name === name && owners[0].controller === true && owners[0].uid,
      'MIGRATOR_CA_OWNERSHIP_INVALID');
      const [bundleUID,bundleVersion,bundleSpecJSON,...rest] = kube(['get','bundles.trust.cert-manager.io',name,'-o',
        'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.spec}']).trim().split('|');
      ensure(bundleUID === owners[0].uid && /^\d+$/.test(bundleVersion ?? '') && !rest.length &&
        fingerprint(JSON.parse(bundleSpecJSON)) === fingerprint(caBundleSpec), 'MIGRATOR_CA_BUNDLE_SOURCE_INVALID');
      return {kind,name,uid,resourceVersion,bundle:{uid:bundleUID,resourceVersion:bundleVersion,specSHA256:fingerprint(caBundleSpec)}};
    }
    const raw = kube(['-n',namespace,'get',kind,name,'-o',
      'jsonpath={.metadata.uid}{"|"}{.metadata.resourceVersion}{"|"}{.metadata.labels.app\\.kubernetes\\.io/part-of}']);
    const [uid, resourceVersion, owner, ...extra] = raw.trim().split('|');
    ensure(uid && /^\d+$/.test(resourceVersion ?? '') && owner === 'kodex' && !extra.length, 'MIGRATOR_DEPENDENCY_IDENTITY_INVALID');
    return {kind,name,uid,resourceVersion};
  };
  const namespaceUID = () => kube(['get','namespace',namespace,'-o','jsonpath={.metadata.uid}']).trim();
  const dependencies = () => {
    if (profile==='workspace-backup') {
      // Пока backup SELECT временно снят, не допускается автоматический backup либо credential reconciler.
      const controllers=kube(['-n',namespace,'get','deployments,statefulsets,daemonsets,cronjobs',
        '-l','app.kubernetes.io/name=backup-controller','-o','jsonpath={.items[*].metadata.uid}']).trim();
      const consumers=kube(['-n',namespace,'get','pods','-l',
        'app.kubernetes.io/name in (backup-controller,kodex-postgresql-runtime-credentials)',
        '-o','jsonpath={range .items[*]}{.metadata.uid}{"|"}{.status.phase}{"\\n"}{end}']).trim();
      ensure(!controllers && (!consumers || consumers.split('\n').every(line=>/^[a-zA-Z0-9-]+\|(Succeeded|Failed)$/.test(line))),
        'MIGRATION_BACKUP_CONSUMERS_MUST_BE_ABSENT');
    }
    return [metadata('serviceaccount','control-plane-migrator'),
      metadata('secret','control-plane-postgres-migration'),metadata('configmap','control-plane-postgresql-ca')];
  };
  const deployment = () => {
    const value = get('deployment','control-plane');
    ensure(value.kind === 'Deployment' && value.metadata?.name === 'control-plane' && value.metadata.namespace === namespace &&
      value.metadata.uid && value.metadata.resourceVersion && value.metadata.labels?.['app.kubernetes.io/name'] === 'control-plane' &&
      value.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
      value.metadata.labels?.['kodex.dev/environment'] === (configuration.anchorProfile === 'portable' ? 'production' : 'staging') &&
      (configuration.anchorProfile !== 'portable' || (value.metadata.labels?.['kodex.dev/profile'] === 'web-only' &&
        !value.metadata.labels?.['kodex.dev/local-profile'])),
    'MIGRATION_ANCHOR_IDENTITY_INVALID');
    return value;
  };
  const before = (render, anchor) => ({...render, metadata:{...render.metadata,uid:anchor.metadata.uid,resourceVersion:anchor.metadata.resourceVersion}});
  const sourceDigest = source => {
    ensure(inspectSource(source.path).revision === source.revision, 'SOURCE_REVISION_MISMATCH');
    const bundle = JSON.parse(execFileSync('yq',['-o=json',
      'select(.kind == "Bundle" and .metadata.name == "control-plane-postgresql-ca") | .spec',
      `${source.path}/deploy/k8s/base/platform-state/ca-bundles.yaml`],
    {encoding:'utf8',timeout:5000,maxBuffer:262144,stdio:['ignore','pipe','pipe']}));
    ensure(fingerprint(bundle) === fingerprint(caBundleSpec), 'MIGRATOR_CA_CANONICAL_SOURCE_INVALID');
    const directory = `${source.path}/services/internal/control-plane/cmd/cli/migrations`;
    const names = readdirSync(directory).filter(name => /^\d{14}_.+\.sql$/.test(name)).sort();
    ensure(names.at(-1) === migrationFile, 'MIGRATION_NOT_SOURCE_HEAD');
    ensure((configuration.requiredMigrations??[]).every(name=>names.includes(name)),'MIGRATION_SOURCE_CHAIN_INVALID');
    return createHash('sha256').update(readFileSync(`${directory}/${migrationFile}`)).digest('hex');
  };
  const imageScope = (evidence, nsUID) => {
    if (isComponentMigrationPublication(evidence)) {
      const sourceRevision = phase === 'plan' ? options['--revision'] : retainedPlan.source.revision;
      const apps = componentMigrationTargetNames(evidence.profile).map(name => get('deployment',name));
      const target = componentMigrationTarget(portableTarget,evidence,{sourceRevision,context,clusterUID,namespaceUID:nsUID,
        installation:get('configmap','kodex-installation-profile'),deployments:apps,nodes:JSON.parse(kube(['get','nodes','-o','json'])).items});
      const gateway = apps.find(d => d.metadata.name === 'control-api-gateway');
      const children = JSON.parse(kube(['-n',namespace,'get','replicasets,pods','-l','app.kubernetes.io/name=control-api-gateway','-o','json']));
      ensure(gateway.status?.observedGeneration >= gateway.metadata.generation && !(gateway.status.replicas ?? 0) &&
        activeDeploymentPods(children,gateway.metadata.uid).length === 0, 'COMPONENT_MIGRATION_MAINTENANCE_JOIN_REQUIRED');
      return target;
    }
    ensure(evidence.publicationPlan.context === context && evidence.publicationPlan.clusterUID === clusterUID &&
      evidence.publicationPlan.namespaceUID === nsUID, 'MIGRATION_PUBLICATION_SCOPE_MISMATCH');
  };
  const readPublication = path => {
    path = resolve(path); const stat = lstatSync(path);
    ensure(realpathSync(path) === path && realpathSync(dirname(path)) === dirname(path) &&
      (lstatSync(dirname(path)).mode & 0o077) === 0 && stat.isFile() && stat.uid === process.getuid() &&
      stat.nlink === 1 && (stat.mode & 0o077) === 0 && stat.size < (4 << 20), 'PRIVATE_INPUT_INVALID');
    const raw = readFileSync(path,'utf8');
    ensure(raw.endsWith('\n'), 'PUBLICATION_EVIDENCE_INVALID');
    return raw.trimEnd().split('\n').map(line => JSON.parse(line));
  };
  const postgresIdentity = () => {
    const raw = kube(['-n',namespace,'get','pod','kodex-postgresql-0','-o',
      'jsonpath={.metadata.uid}{"|"}{.metadata.ownerReferences[0].kind}{"|"}{.metadata.ownerReferences[0].name}{"|"}{.metadata.ownerReferences[0].uid}{"|"}{.metadata.labels.app\\.kubernetes\\.io/name}{"|"}{.status.phase}{"|"}{.metadata.deletionTimestamp}']).trim();
    const [uid,kind,name,ownerUID,label,phase,deleting,...extra] = raw.split('|');
    ensure(uid && kind === 'StatefulSet' && name === 'kodex-postgresql' && ownerUID && label === 'kodex-postgresql' &&
      phase === 'Running' && !deleting && !extra.length, 'MIGRATION_POSTGRES_IDENTITY_INVALID');
    return {pod:'kodex-postgresql-0',uid,ownerUID};
  };
  const preconditionSQL = source => readFileSync(`${source.path}/tools/release/${configuration.preconditionFile}`);
  const drainSQL = source => readFileSync(`${source.path}/tools/release/portable-component-migration-drain.sql`);
  const precondition = (source, expectedPostgres, expectedSQL, requireStopped = false) => {
    ensure(fingerprint(postgresIdentity()) === fingerprint(expectedPostgres), 'MIGRATION_POSTGRES_IDENTITY_CHANGED');
    const sql = preconditionSQL(source);
    ensure(createHash('sha256').update(sql).digest('hex') === expectedSQL, 'MIGRATION_PRECONDITION_SOURCE_CHANGED');
    // Тот же закрытый Operator.sql transport: local psql без env/DSN/Secret values,
    // SQL только из accepted clean source; BEGIN READ ONLY ограничивает серверную сессию.
    const result = JSON.parse(kube(['-n',namespace,'exec','-i','kodex-postgresql-0','--',
      'psql','-XqAt','-U','postgres','-d','control_plane','-v','ON_ERROR_STOP=1'],sql));
    ensure(fingerprint(postgresIdentity()) === fingerprint(expectedPostgres), 'MIGRATION_POSTGRES_IDENTITY_CHANGED');
    const fields = ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
      'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks'];
    ensure(profile !== 'ssh-compose' || validServerWorkflowMigrationPostcondition(result), 'MIGRATION_OWNER_PRECONDITION_FAILED');
    ensure(result.readOnly === 'on' && result.database === 'control_plane' && result.gooseVersion === configuration.previousVersion &&
      result[configuration.emptyField] === true && fields.every(key => result[key] === 0) && Number.isSafeInteger(result.oldClaimSessions) &&
      result.oldClaimSessions >= 0 && (!requireStopped || result.oldClaimSessions === 0), 'MIGRATION_OWNER_PRECONDITION_FAILED');
    if (profile === 'workflow-review-qa-evidence') ensure(validWorkflowReviewQAEvidenceMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'legacy-workflow-rework') ensure(validServerWorkflowMigrationPostcondition(result) && result.legacyPurgeSource===true, 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workflow-exception-context')
      ensure(validWorkflowExceptionContextMigrationPrecondition(result), 'MIGRATION_OWNER_PRECONDITION_FAILED');
    if (profile === 'workflow-upstream-rework')
      ensure(validWorkflowUpstreamReworkMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workspace-volume-ssh')
      ensure(validWorkspaceVolumeSSHMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workspace-restore')
      ensure(validWorkspaceRestoreMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workspace-backup')
      ensure(validWorkspaceBackupMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'github-autonomous' || profile === 'github-autonomous-backup')
      ensure(validGitHubAutonomousMigrationPrecondition(result,profile==='github-autonomous-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'github-pr' || profile === 'github-pr-backup')
      ensure(validGitHubPRMigrationPrecondition(result,profile==='github-pr-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workflow-pdlc' || profile === 'workflow-pdlc-backup')
      ensure(validWorkflowPDLCMigrationPrecondition(result,profile==='workflow-pdlc-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workflow-content-page' || profile === 'workflow-content-page-backup')
      ensure(validWorkflowContentPageMigrationPrecondition(result,profile==='workflow-content-page-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'platform-oidc-identity' || profile === 'platform-oidc-identity-backup')
      ensure(validPlatformOIDCIdentityMigrationPrecondition(result,profile==='platform-oidc-identity-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'personal-credentials' || profile === 'personal-credentials-backup')
      ensure(validPersonalCredentialsMigrationPrecondition(result,profile==='personal-credentials-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'jira-create-fields' || profile === 'jira-create-fields-backup')
      ensure(validJiraCreateFieldsMigrationPrecondition(result,profile==='jira-create-fields-backup'), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'opencode-native-sessions')
      ensure(validOpenCodeNativeMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'github-repository-scope' || profile === 'github-repository-scope-backup') ensure(validGitHubRepositoryScopeMigrationPrecondition(result,profile.endsWith('-backup'),requireStopped),'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workspace-unbound-stop') ensure(validWorkspaceUnboundStopMigrationPrecondition(result),'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workspace-archive-abi')
      ensure(validWorkspaceArchiveABIMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'workflow-workspace')
      ensure(validWorkflowWorkspaceMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'managed-workspace')
      ensure(validManagedWorkspaceMigrationPrecondition(result), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'artifact-optional-antivirus')
      ensure(validArtifactOptionalAntivirusMigrationPrecondition(result,requireStopped), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'integration-resource-policies' || profile === 'integration-resource-policies-backup')
      ensure(validIntegrationResourcePoliciesMigrationPrecondition(result,profile==='integration-resource-policies-backup',requireStopped), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    if (profile === 'authority-proof-counter' || profile === 'authority-proof-counter-backup')
      ensure(validAuthorityProofCounterMigrationPrecondition(result,profile==='authority-proof-counter-backup',requireStopped), 'MIGRATION_PREDECESSOR_SCHEMA_INVALID');
    return result;
  };
  const terminalJob = (actual, expected, expectedHash, expectedUID) => {
    ensure(actual?.metadata?.uid === expectedUID && actual.metadata.name === expected.metadata.name && actual.metadata.namespace === namespace &&
      actual.metadata.annotations?.[annotation] === expectedHash &&
      fingerprint(io.normalizePodSpec(actual.spec?.template?.spec)) === fingerprint(io.normalizePodSpec(expected.spec.template.spec)) &&
      !actual.status?.active && !actual.status?.terminating && actual.status?.conditions?.filter(c => ['Complete','Failed'].includes(c.type) && c.status === 'True').length === 1,
    'MIGRATION_PREDECESSOR_NOT_TERMINAL');
    const pods = JSON.parse(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/job-name=${expected.metadata.name}`,'-o','json'])).items;
    ensure(pods.every(p => p.metadata?.uid && p.metadata.namespace === namespace &&
      p.metadata.ownerReferences?.some(o => o.kind === 'Job' && o.controller === true && o.uid === expectedUID) &&
      !['NodeLost','Unknown'].includes(p.status?.reason) && !p.status?.conditions?.some(c => c.status === 'Unknown') && isProvenStoppedPod(p)),
    'MIGRATION_PREDECESSOR_NOT_JOINED');
    return pods;
  };
  const predecessor = (paths, anchor, expected, currentMarker) => {
    ensure(!existsSync(`${paths.evidencePath}.lock`), 'MIGRATION_PREDECESSOR_WRITER_ACTIVE');
    const parent = privateJSON(paths.planPath), hash = fingerprint(parent);
    validateCheckIdentity(parent);
    ensure([1,2].includes(parent.version) && parent.kind === 'CONTROL_PLANE_MIGRATION' && parent.profile === profile &&
      (parent.attempt ?? 1) === 1 && !parent.predecessor && parent.migrationVersion === migrationVersion &&
      parent.clusterUID === clusterUID && parent.context === context && parent.namespaceUID === namespaceUID() &&
      parent.anchor.uid === anchor.metadata.uid && [0,anchorReplicas].includes(anchor.spec.replicas) &&
      parent.anchor.specSHA256 === fingerprint({...anchor.spec,replicas:anchorReplicas}), 'MIGRATION_PREDECESSOR_SCOPE_CHANGED');
    immutableMigrationImage(parent.source,parent.imageEvidence,parent.profile);
    const previous = privateJournal(paths.evidencePath,{version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:hash},{readOnly:true});
    let uid;
    try {
      // Terminal check ещё не даёт полномочий на STOP или DDL. Attempt2 сохраняет
      // прежнюю Job; успешный, но устаревший check заново подтверждается read-only.
      const checkOnly = !previous.events.some(e => e.type === 'CREATE_INTENT');
      const stage = checkOnly ? 'CHECK' : 'MIGRATION';
      const expectedJob = checkOnly ? parent.checkJob : parent.job;
      const intent = checkOnly ? 'CHECK_CREATE_INTENT' : 'CREATE_INTENT';
      const readback = checkOnly ? 'CHECK_READBACK' : 'READBACK';
      const receiptTypes = checkOnly ? ['CHECK_CREATE_ACK','CHECK_READBACK'] : ['CREATE_ACK','READBACK','TERMINAL_READBACK'];
      ensure(!checkOnly || (parent.version === 2 && expectedJob &&
        !previous.events.some(e => ['STOP_INTENT','STOP_ACK','RESUME_INTENT','RESUME_ACK','PAIR_STOP_INTENT','PAIR_STOP_ACK',
          'PAIR_HANDOFF_INTENT','PAIR_HANDOFF_ACK','PAIR_RESUME_INTENT','PAIR_RESUME_ACK'].includes(e.type)) &&
        !optionalJob(parent.job.metadata.name)), 'MIGRATION_PREDECESSOR_CHECK_EFFECTS_PRESENT');
      const marker = checkOnly ? undefined : hash;
      ensure([marker,...(currentMarker ? [currentMarker] : [])].includes(anchor.metadata.annotations?.[annotation]),
        'MIGRATION_PREDECESSOR_SCOPE_CHANGED');
      const receipts = previous.events.filter(e => receiptTypes.includes(e.type) && e.jobUID);
      const lastReadback=previous.events.findLast(e => e.type === readback);
      const passedCheck=checkOnly && lastReadback?.status === 'PASS';
      uid = receipts[0]?.jobUID;
      ensure(uid && receipts.every(e => e.jobUID === uid) && previous.events.filter(e => e.type === intent).length === 1 &&
        lastReadback?.status === (passedCheck ? 'PASS' : 'FAIL') &&
        (checkOnly || previous.events.some(e => e.type === 'RESUME_ACK')),
      'MIGRATION_PREDECESSOR_RECEIPT_INVALID');
      if (expected) ensure(expected.planSHA256 === hash && expected.evidenceSHA256 === previous.bytesSHA256 && expected.jobUID === uid &&
        (expected.stage ?? 'MIGRATION') === stage,
        'MIGRATION_PREDECESSOR_CHANGED');
      const actual = optionalJob(expectedJob.metadata.name);
      const pods=terminalJob(actual,expectedJob,hash,uid);
      if (passedCheck) {
        const logs=actual.status?.succeeded === 1 ? kube(['-n',namespace,'logs',`job/${actual.metadata.name}`,'-c','migrate','--limit-bytes=262144']) : '';
        const observed=migrationReadback(actual,parent,logs,pods,'check');
        ensure(observed.status === 'PASS' && ['status','jobName','jobUID','sourceRevision','operation'].every(key => lastReadback[key] === observed[key]),
          'MIGRATION_PREDECESSOR_CHECK_PASS_UNCONFIRMED');
      } else ensure(actual.status.conditions.some(c => c.type === 'Failed' && c.status === 'True'), 'MIGRATION_PREDECESSOR_NOT_FAILED');
      return {...paths,stage,planSHA256:hash,evidenceSHA256:previous.bytesSHA256,jobUID:uid,jobName:expectedJob.metadata.name};
    } finally { previous.close(); }
  };
  if (phase === 'plan') {
    const anchor = deployment(), render = renderedBase(options['--base-render']);
    const source = {path:options['--source'],revision:options['--revision']};
    const imageEvidence = componentPublication ?? {publicationPlan:privateJSON(options['--publication-plan']),publicationEvidence:readPublication(options['--publication-evidence'])};
    const nsUID = namespaceUID(); ensure(nsUID, 'MIGRATION_NAMESPACE_INVALID'); imageScope(imageEvidence,nsUID);
    const previous = options['--predecessor-plan'] ? predecessor({planPath:resolve(options['--predecessor-plan']),
      evidencePath:resolve(options['--predecessor-evidence'])},anchor) : undefined;
    const plan = planImmutableControlPlaneMigration(before(render,anchor),source,inspectSource,randomUUID(),imageEvidence,previous ? 2 : 1,profile);
    ensure(!optionalJob(plan.checkJob.metadata.name), 'MIGRATION_CHECK_ALREADY_EXISTS');
    ensure(!optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_ALREADY_EXISTS');
    const previousImage = anchor.spec?.template?.spec?.containers?.find(container => container.name === 'control-plane')?.image;
    ensure(anchor.spec.replicas === anchorReplicas && !anchor.spec.paused && anchor.metadata.labels['kodex.dev/local-profile'] === anchorLocalProfile &&
      anchor.status?.observedGeneration >= anchor.metadata.generation &&
      ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key => anchor.status[key] === anchorReplicas) &&
      (componentPublication ? previousImage === componentPublication.components.find(c => c.component === 'control-plane').expectedImageRef :
        /^registry\.local\.kodex\/kodex\/control-plane@sha256:[a-f0-9]{64}$/.test(previousImage ?? '')) &&
      !previousImage.endsWith('0'.repeat(64)) && !anchor.spec.template.spec.volumes?.some(volume => volume.hostPath), 'MIGRATION_CP_IMMUTABLE_HEALTH_REQUIRED');
    ensure(previous || !anchor.metadata.annotations?.[annotation], 'MIGRATION_ANCHOR_ALREADY_RESERVED');
    Object.assign(plan,{clusterUID,context,namespaceUID:nsUID,dependencies:dependencies(),
      anchor:{kind:'Deployment',name:'control-plane',uid:anchor.metadata.uid,resourceVersion:anchor.metadata.resourceVersion,
        specSHA256:fingerprint(anchor.spec),stoppedSpecSHA256:fingerprint({...anchor.spec,replicas:0}),previousImage},
      baseRenderPath:resolve(options['--base-render']),baseRenderSHA256:fingerprint(render),migrationFileSHA256:sourceDigest(source)});
    if (componentPublication) {
      plan.portableTarget = structuredClone(portableTarget);
      plan.componentHandoff = planComponentMigrationHandoff(componentPublication,componentMigrationNames(profile).map(name=>name==='control-plane'?anchor:get('deployment',name)),source);
      if(componentPublication.schema === componentMigrationCohortSchema)
        plan.componentDrainSQLSHA256=createHash('sha256').update(drainSQL(source)).digest('hex');
    }
    if (previous) plan.predecessor = previous;
    plan.postgres = postgresIdentity();plan.preconditionSQLSHA256 = createHash('sha256').update(preconditionSQL(source)).digest('hex');
    if(configuration.postconditionFile) plan.postconditionSQLSHA256=createHash('sha256').update(readFileSync(`${source.path}/tools/release/${configuration.postconditionFile}`)).digest('hex');
    if(profile==='workspace-backup') plan.backupACLReadbackSQLSHA256=createHash('sha256').update(readFileSync(`${source.path}/tools/release/workspace-backup-acl-readback.sql`)).digest('hex');
    precondition(source,plan.postgres,plan.preconditionSQLSHA256);
    writePrivate(options['--plan'],plan);
    process.stdout.write(`${JSON.stringify({status:'PLANNED',jobName:plan.job.metadata.name,sourceRevision:source.revision,planSHA256:fingerprint(plan)})}\n`);
    return;
  }
  const plan = retainedPlan;
  ensure([1,2].includes(plan.version) && plan.kind === 'CONTROL_PLANE_MIGRATION' && plan.profile === profile &&
    plan.migrationVersion === migrationVersion && plan.context === context && plan.clusterUID === clusterUID &&
    plan.namespaceUID === namespaceUID() && plan.anchor?.kind === 'Deployment' && plan.anchor.name === 'control-plane', 'PLAN_SCOPE_MISMATCH');
  validateCheckIdentity(plan);
  imageScope(plan.imageEvidence,plan.namespaceUID);
  const planSHA256 = fingerprint(plan);
  const revalidationRevision = options['--revalidate-postcondition-revision'];
  ensure(!revalidationRevision || (phase==='inspect' && profile==='repository-bundle' && /^[a-f0-9]{40}$/.test(revalidationRevision)),
    'MIGRATION_REVALIDATION_PHASE_INVALID');
  // После SIGKILL lock сохраняется. Inspect всё равно доступен без записи/снятия lock;
  // mutations закрыты до ручной проверки завершения прежнего процесса владельцем.
  const readOnly = !!revalidationRevision || (phase === 'inspect' && existsSync(`${resolve(options['--evidence'])}.lock`));
  const durable = privateJournal(options['--evidence'], {version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256}, {readOnly});
  const journal = readOnly ? {events:durable.events,append:()=>{},close:()=>durable.close()} : durable;
  let revalidation;
  try {
    const attempts = journal.events.filter(e => e.type === 'CREATE_INTENT');
    const checks = journal.events.filter(e => e.type === 'CHECK_CREATE_INTENT');
    ensure(attempts.length <= 1 && checks.length <= 1, 'MIGRATION_JOURNAL_INVALID');
    const output = result => {
      if (['FAIL','READBACK_UNCONFIRMED'].includes(result.status)) process.exitCode = 1;
      process.stdout.write(`${JSON.stringify({...result,...(readOnly ? {evidenceReadOnly:true} : {})})}\n`); return result;
    };
    const observeCP = () => {
      const actual = deployment(), marker = actual.metadata.annotations?.[annotation], hash = fingerprint(actual.spec);
      const previousMarker = plan.predecessor?.stage === 'CHECK' ? undefined : plan.predecessor?.planSHA256;
      ensure(actual.metadata.uid === plan.anchor.uid && (marker === previousMarker || marker === planSHA256), 'MIGRATION_ANCHOR_CHANGED');
      const resumed = journal.events.some(e => e.type === 'RESUME_INTENT');
      let stage;
      if (hash === plan.anchor.stoppedSpecSHA256 && marker === planSHA256) stage = 'STOPPED';
      else if (hash === plan.anchor.specSHA256 && marker === previousMarker && !resumed) stage = 'BEFORE';
      else if (hash === plan.anchor.specSHA256 && marker === planSHA256 && resumed) stage = 'RESUMED';
      const cpHandoff = plan.componentHandoff?.components.find(c => c.name === 'control-plane');
      if (cpHandoff && marker === planSHA256 && hash === fingerprint(cpHandoff.stagedSpec)) stage = 'STAGED';
      if (cpHandoff && marker === planSHA256 && hash === fingerprint(cpHandoff.resumedSpec) && resumed) stage = 'UPGRADED';
      ensure(stage, 'MIGRATION_CP_SPEC_CHANGED');
      const pods = activeDeploymentPods(readComponentServingInventory(kube,'control-plane',plan.anchor.uid),plan.anchor.uid);
      const joined = actual.status?.observedGeneration >= actual.metadata.generation && !(actual.status.replicas ?? 0) && pods.length === 0;
      const servingImage = stage === 'UPGRADED' ? cpHandoff.resumedSpec.template.spec.containers.find(c => c.name === 'control-plane').image : plan.anchor.previousImage;
      const digest = servingImage.split('@').at(-1);
      const ready = actual.status?.observedGeneration >= actual.metadata.generation &&
        ['replicas','updatedReplicas','readyReplicas','availableReplicas'].every(key => actual.status[key] === anchorReplicas) && pods.length === anchorReplicas &&
        pods.every(p => !p.metadata.deletionTimestamp && p.status?.conditions?.some(c => c.type === 'Ready' && c.status === 'True') &&
          p.spec.containers?.some(c => c.name === 'control-plane' && c.image === servingImage) &&
          p.status.containerStatuses?.some(c => c.name === 'control-plane' && c.ready && c.imageID?.endsWith(digest)));
      return {actual,stage,joined,ready};
    };
    const migrationPods = () => JSON.parse(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/job-name=${plan.job.metadata.name}`,'-o','json'])).items;
    const readJob = (terminalOnly = false) => {
      const actual = optionalJob(plan.job.metadata.name);
      ensure(actual, 'MIGRATION_JOB_MISSING_AFTER_INTENT');
      const observed = journal.events.find(e => ['CREATE_ACK','READBACK','TERMINAL_READBACK'].includes(e.type) && e.jobUID);
      ensure(!observed || observed.jobUID === actual.metadata.uid, 'MIGRATION_JOB_REPLACED');
      const pods = migrationPods();
      // Resume сохраняет прежний бинарник даже после failed/unconfirmed DDL, но лишь после
      // авторитетного terminal и доказанного прекращения всех Job-owned процессов.
      if (terminalOnly) {
        ensure(actual.metadata.uid && actual.metadata.annotations?.[annotation] === planSHA256 &&
          fingerprint(io.normalizePodSpec(actual.spec.template.spec)) === fingerprint(io.normalizePodSpec(plan.job.spec.template.spec)) &&
          actual.status?.conditions?.filter(c => ['Complete','Failed'].includes(c.type) && c.status === 'True').length === 1 &&
          !actual.status.active && pods.every(p => p.metadata?.uid && p.metadata.namespace === namespace &&
            p.metadata.ownerReferences?.some(o => o.kind === 'Job' && o.controller === true && o.uid === actual.metadata.uid) && isProvenStoppedPod(p)),
        'MIGRATION_TERMINAL_JOIN_REQUIRED');
        journal.append({type:'TERMINAL_READBACK',jobUID:actual.metadata.uid}); return;
      }
      ensure(fingerprint(dependencies()) === fingerprint(plan.dependencies), 'MIGRATION_DEPENDENCY_CHANGED');
      const logs = actual.status?.succeeded === 1 ? kube(['-n',namespace,'logs',`job/${plan.job.metadata.name}`,'-c','migrate','--limit-bytes=262144']) : '';
      const result = migrationReadback(actual,plan,logs,pods);
      if(result.status==='PASS' && configuration.postconditionFile) {
        const sql=readFileSync(`${plan.source.path}/tools/release/${configuration.postconditionFile}`);
        ensure(createHash('sha256').update(sql).digest('hex')===(revalidation?.sqlSHA256 ?? plan.postconditionSQLSHA256),'MIGRATION_POSTCONDITION_SOURCE_CHANGED');
        ensure(fingerprint(postgresIdentity())===fingerprint(plan.postgres),'MIGRATION_POSTGRES_IDENTITY_CHANGED');
        const proof=JSON.parse(kube(['-n',namespace,'exec','-i','kodex-postgresql-0','--','psql','-XqAt','-U','postgres','-d','control_plane','-v','ON_ERROR_STOP=1'],sql));
        ensure(fingerprint(postgresIdentity())===fingerprint(plan.postgres),'MIGRATION_POSTGRES_IDENTITY_CHANGED');
        ensure(validImmutableMigrationPostcondition(profile,proof),'MIGRATION_POSTCONDITION_FAILED');
        result.schemaPostcondition='PASS';
        if(revalidation) result.revalidation={...revalidation,proof,postgres:plan.postgres,
          execution:pods.map(p=>({podUID:p.metadata.uid,imageID:p.status.containerStatuses.find(c=>c.name==='migrate').imageID}))};
      }
      journal.append({type:'READBACK',...result}); return result;
    };
    const noActiveMigrations = () => {
      const jobs = JSON.parse(kube(['-n',namespace,'get','jobs','-l','app.kubernetes.io/name=control-plane,app.kubernetes.io/component=migration','-o','json'])).items;
      ensure(jobs.every(job => job.status?.conditions?.some(c => ['Complete','Failed'].includes(c.type) && c.status === 'True')), 'MIGRATION_ALREADY_RUNNING');
    };
    const verifyInputs = anchor => {
      ensure(plan.version === 2, 'MIGRATION_NEW_PROTOCOL_REQUIRED');
      const render = renderedBase(plan.baseRenderPath);
      const rebuilt = planImmutableControlPlaneMigration(before(render,anchor),plan.source,inspectSource,plan.id,plan.imageEvidence,plan.attempt,profile,
        Object.hasOwn(plan,'checkIdentity') ? plan.checkIdentity : null);
      ensure(fingerprint(render) === plan.baseRenderSHA256 && fingerprint(rebuilt.job) === fingerprint(plan.job) &&
        fingerprint(rebuilt.checkJob) === fingerprint(plan.checkJob) &&
        sourceDigest(plan.source) === plan.migrationFileSHA256 && fingerprint(dependencies()) === fingerprint(plan.dependencies), 'MIGRATION_PLAN_DRIFT');
      if (plan.componentHandoff) {
        const originals = plan.componentHandoff.components.map(c => ({apiVersion:'apps/v1',kind:'Deployment',metadata:{name:c.name,namespace,uid:c.uid,
          resourceVersion:'1',generation:1},spec:c.beforeSpec,status:{observedGeneration:1,replicas:c.replicas,updatedReplicas:c.replicas,readyReplicas:c.replicas,availableReplicas:c.replicas}}));
        ensure(fingerprint(planComponentMigrationHandoff(plan.imageEvidence,originals,plan.source)) === fingerprint(plan.componentHandoff), 'COMPONENT_MIGRATION_HANDOFF_PLAN_DRIFT');
      }
      if (plan.predecessor) predecessor(plan.predecessor,anchor,plan.predecessor,planSHA256);
    };
    const readCheck = (fresh = false) => {
      ensure(plan.version === 2 && checks.length === 1, 'MIGRATION_CHECK_INTENT_REQUIRED');
      const actual = optionalJob(plan.checkJob.metadata.name);
      ensure(actual, 'MIGRATION_CHECK_MISSING_AFTER_INTENT');
      const observed = journal.events.find(e => ['CHECK_CREATE_ACK','CHECK_READBACK'].includes(e.type) && e.jobUID);
      ensure(!observed || observed.jobUID === actual.metadata.uid, 'MIGRATION_CHECK_REPLACED');
      ensure(fingerprint(dependencies()) === fingerprint(plan.dependencies), 'MIGRATION_DEPENDENCY_CHANGED');
      const pods = JSON.parse(kube(['-n',namespace,'get','pods','-l',`batch.kubernetes.io/job-name=${plan.checkJob.metadata.name}`,'-o','json'])).items;
      const logs = actual.status?.succeeded === 1 ? kube(['-n',namespace,'logs',`job/${plan.checkJob.metadata.name}`,'-c','migrate','--limit-bytes=262144']) : '';
      const result = migrationReadback(actual,plan,logs,pods,'check');
      if (fresh) {
        const completed = Date.parse(actual.status?.completionTime ?? '');
        ensure(result.status === 'PASS' && Number.isFinite(completed) && completed <= Date.now() && Date.now()-completed <= 300000,
          'MIGRATION_CHECK_PASS_REQUIRED');
      }
      journal.append({type:'CHECK_READBACK',...result,dependenciesSHA256:fingerprint(plan.dependencies)});
      return result;
    };
    const transition = (step, state, replicas) => {
      const type = `${step}_INTENT`, last = journal.events.findLastIndex(e => e.type === type);
      if (last >= 0) ensure(journal.events.findLastIndex(e => e.type === 'INSPECT' && e.stage === state.stage &&
        e.resourceVersion === state.actual.metadata.resourceVersion) > last, 'MIGRATION_CP_PATCH_UNKNOWN_INSPECT_REQUIRED');
      const patch = [{op:'test',path:'/metadata/uid',value:plan.anchor.uid},
        {op:'test',path:'/metadata/resourceVersion',value:state.actual.metadata.resourceVersion},
        {op:'test',path:'/spec/replicas',value:state.actual.spec.replicas},
        {op:'replace',path:'/spec/replicas',value:replicas},
        {op:'add',path:'/metadata/annotations',value:{...(state.actual.metadata.annotations ?? {}),[annotation]:planSHA256}}];
      journal.append({type,resourceVersion:state.actual.metadata.resourceVersion});
      try { kube(['-n',namespace,'patch','deployment','control-plane','--type=json','-p',JSON.stringify(patch),'-o','json']); }
      catch(error) { journal.append({type:'UNKNOWN',step,code:error.message}); throw error; }
    };
    const pair = plan.componentHandoff ? componentMigrationLifecycle({plan,kube,journal,readJob,observeCP,preconditionSQL,drainSQL,postgresIdentity,
      activePods:activeDeploymentPods,wait:waitMigrationMaintenance,clock:io.clock,validateInputs:() => {
        ensure(sourceDigest(plan.source) === plan.migrationFileSHA256,'MIGRATION_PLAN_DRIFT');
        imageScope(plan.imageEvidence,plan.namespaceUID);
      }}) : undefined;
    if (phase === 'inspect') {
      const state = observeCP();
      const pairState = pair?.inspect();
      if(revalidationRevision) {
        // Проверка уже завершённого schema effect не даёт нового права на DDL.
        // Прежние plan/journal и applied migration остаются неизменными.
        ensure(plan.version===2 && state.stage==='RESUMED' && state.ready && attempts.length===1 &&
          journal.events.some(e=>e.type==='RESUME_ACK') && journal.events.some(e=>e.type==='TERMINAL_READBACK' && e.jobUID),
        'MIGRATION_REVALIDATION_RESUMED_REQUIRED');
        ensure(inspectSource(plan.source.path).revision===revalidationRevision &&
          sourceDigest({...plan.source,revision:revalidationRevision})===plan.migrationFileSHA256,'MIGRATION_REVALIDATION_SOURCE_INVALID');
        const path=`tools/release/${configuration.postconditionFile}`;
        let previousSQL;
        try {
          execFileSync('git',sourceGitArguments(plan.source.path,'merge-base','--is-ancestor',plan.source.revision,revalidationRevision),
            {timeout:10000,stdio:['ignore','pipe','pipe']});
          previousSQL=execFileSync('git',sourceGitArguments(plan.source.path,'show',`${plan.source.revision}:${path}`),
            {timeout:10000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});
        } catch {throw new Error('MIGRATION_REVALIDATION_SOURCE_INVALID');}
        ensure(createHash('sha256').update(previousSQL).digest('hex')===plan.postconditionSQLSHA256,
          'MIGRATION_REVALIDATION_ORIGINAL_SQL_CHANGED');
        revalidation={sourceRevision:revalidationRevision,sqlSHA256:createHash('sha256').update(readFileSync(`${plan.source.path}/${path}`)).digest('hex'),
          originalSQLSHA256:plan.postconditionSQLSHA256,originalPlanSHA256:planSHA256,originalJournalSHA256:durable.bytesSHA256};
        ensure(revalidation.sqlSHA256!==revalidation.originalSQLSHA256,'MIGRATION_REVALIDATION_SQL_UNCHANGED');
      }
      journal.append({type:'INSPECT',stage:state.stage,resourceVersion:state.actual.metadata.resourceVersion,podsGone:state.joined,ready:state.ready});
      if (!attempts.length) ensure(!optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_WITHOUT_INTENT');
      const result=attempts.length ? readJob() : checks.length ? readCheck() : {status:state.stage === 'STOPPED' ? 'STOPPED_NOT_CREATED' : 'CREATE_NOT_ATTEMPTED'};
      if(revalidation) {
        const current=observeCP();
        ensure(result.status==='PASS' && current.stage==='RESUMED' && current.ready &&
          inspectSource(plan.source.path).revision===revalidationRevision,'MIGRATION_REVALIDATION_CHANGED');
        const finalJob=optionalJob(plan.job.metadata.name), finalPods=migrationPods();
        ensure(finalJob?.metadata.uid===result.jobUID && finalJob.metadata.annotations?.[annotation]===planSHA256 &&
          finalJob.status?.succeeded===1 && !finalJob.status.active && immutableMigrationExecution(finalJob,plan,finalPods) &&
          fingerprint(finalPods.map(p=>({podUID:p.metadata.uid,imageID:p.status.containerStatuses.find(c=>c.name==='migrate').imageID})))===
            fingerprint(result.revalidation.execution),'MIGRATION_REVALIDATION_EXECUTION_CHANGED');
        const currentJournal=privateJournal(options['--evidence'],{version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256},{readOnly:true});
        try {ensure(currentJournal.bytesSHA256===revalidation.originalJournalSHA256,'MIGRATION_REVALIDATION_JOURNAL_CHANGED');}
        finally {currentJournal.close();}
      }
      return output({...result,
        cpStage:state.stage,podsGone:state.joined,cpReady:state.ready,
        ...(pairState ? {componentPair:pairState,pairReady:pairState.every(s => s.stage === 'resumedSpec' && s.ready || s.maintenance === true && s.stage === 'stagedSpec' && s.joined),forwardOnly:true} : {})});
    }
    if (phase === 'check') {
      if (!checks.length) {
        const state = observeCP();ensure(state.stage === 'BEFORE' && state.ready && !attempts.length, 'MIGRATION_CHECK_BEFORE_STOP_REQUIRED');
        verifyInputs(state.actual);noActiveMigrations();
        precondition(plan.source,plan.postgres,plan.preconditionSQLSHA256);
        ensure(!optionalJob(plan.checkJob.metadata.name), 'MIGRATION_CHECK_ALREADY_EXISTS');
        const job = structuredClone(plan.checkJob);job.metadata.annotations = {[annotation]:planSHA256};
        journal.append({type:'CHECK_CREATE_INTENT',jobName:job.metadata.name});
        try {
          const ack = JSON.parse(kube(['create','-f','-','-o','json'],JSON.stringify(job)));
          ensure(ack.metadata?.uid, 'MIGRATION_CHECK_CREATE_UNCONFIRMED');
          journal.append({type:'CHECK_CREATE_ACK',jobUID:ack.metadata.uid});
        } catch(error) {journal.append({type:'UNKNOWN',step:'CHECK',code:error.message});throw error;}
        checks.push(journal.events.find(e => e.type === 'CHECK_CREATE_INTENT'));
      }
      return output(readCheck());
    }
    if (phase === 'stop') {
      let state = observeCP();
      ensure(!journal.events.some(e => e.type === 'RESUME_INTENT') && !attempts.length, 'MIGRATION_STOP_LIFECYCLE_CLOSED');
      if (state.stage === 'BEFORE') {
        ensure(state.actual.metadata.resourceVersion === plan.anchor.resourceVersion, 'MIGRATION_PLAN_DRIFT');
        verifyInputs(state.actual);noActiveMigrations();ensure(!optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_ALREADY_EXISTS');
        readCheck(true);
        const proof = precondition(plan.source,plan.postgres,plan.preconditionSQLSHA256);
        journal.append({type:'OWNER_PREFLIGHT',...proof});
        transition('STOP',state,0);
      } else ensure(journal.events.some(e => e.type === 'STOP_INTENT'), 'MIGRATION_UNOWNED_STOP');
      state = await waitMigrationMaintenance(observeCP,s => s.stage === 'STOPPED' && s.joined,'MIGRATION_CP_STOP_TIMEOUT',io.clock);
      journal.append({type:'STOP_ACK',resourceVersion:state.actual.metadata.resourceVersion});
      if (pair) await pair.stopController();
      return output({status:'STOPPED',jobName:plan.job.metadata.name,cpStage:state.stage,podsGone:true});
    }
    if (phase === 'handoff') return output(await pair.handoff());
    if (phase === 'resume') {
      if (pair) return output(await pair.resume());
      let state = observeCP();
      if (state.stage === 'BEFORE') {
        ensure(!journal.events.some(e => e.type === 'STOP_INTENT'), 'MIGRATION_STOP_OUTCOME_UNKNOWN');
        return output({status:'UNCHANGED',cpStage:state.stage});
      }
      if (state.stage === 'STOPPED') {
        ensure(journal.events.some(e => e.type === 'STOP_INTENT') && state.joined, 'MIGRATION_STOP_JOIN_REQUIRED');
        noActiveMigrations();
        if (attempts.length) readJob(true);
        else ensure(!optionalJob(plan.job.metadata.name) && migrationPods().length === 0, 'MIGRATION_JOB_WITHOUT_INTENT');
        if(profile==='workspace-backup' && attempts.length && optionalJob(plan.job.metadata.name)?.status?.succeeded===1) {
          // Успешная Job имеет полный pinned028 proof, включая actual backup ACL.
          // Этот более сильный read path уже используется inspect; частичный каталог его не заменяет.
          const proof=readJob();
          ensure(proof.status==='PASS' && proof.schemaPostcondition==='PASS','MIGRATION_BACKUP_ACL_RESTORE_REQUIRED');
          journal.append({type:'BACKUP_ACL_RESTORED',gooseVersion:Number(plan.migrationVersion),
            sqlSHA256:plan.postconditionSQLSHA256,scope:'FULL_SCHEMA_POSTCONDITION'});
        } else if(profile==='workspace-backup') {
          const sql=readFileSync(`${plan.source.path}/tools/release/workspace-backup-acl-readback.sql`);
          ensure(createHash('sha256').update(sql).digest('hex')===plan.backupACLReadbackSQLSHA256,'MIGRATION_BACKUP_READBACK_SOURCE_CHANGED');
          const proof=JSON.parse(kube(['-n',namespace,'exec','-i','kodex-postgresql-0','--','psql','-XqAt','-U','postgres','-d','control_plane','-v','ON_ERROR_STOP=1'],sql));
          ensure(validWorkspaceBackupACLReadback(proof),'MIGRATION_BACKUP_ACL_RESTORE_REQUIRED');
          journal.append({type:'BACKUP_ACL_RESTORED',gooseVersion:proof.gooseVersion,sqlSHA256:plan.backupACLReadbackSQLSHA256});
        }
        transition('RESUME',state,anchorReplicas);
      }
      state = await waitMigrationMaintenance(observeCP,s => s.stage === 'RESUMED' && s.ready,'MIGRATION_CP_RESUME_TIMEOUT',io.clock);
      journal.append({type:'RESUME_ACK',resourceVersion:state.actual.metadata.resourceVersion});
      return output({status:'RESUMED',cpStage:state.stage,cpReady:true});
    }
    if (!attempts.length) {
      ensure(!journal.events.some(e => e.type === 'RESUME_INTENT'), 'MIGRATION_LIFECYCLE_CLOSED');
      const state = observeCP();
      ensure(state.stage === 'STOPPED' && state.joined && journal.events.some(e => e.type === 'STOP_INTENT'), 'MIGRATION_STOP_JOIN_REQUIRED');
      if (pair) pair.requireStopped();
      ensure(journal.events.some(e => e.type === 'STOP_ACK' || (e.type === 'INSPECT' && e.stage === 'STOPPED' && e.podsGone)), 'MIGRATION_STOP_READBACK_REQUIRED');
      verifyInputs(state.actual);noActiveMigrations();
      const proof = precondition(plan.source,plan.postgres,plan.preconditionSQLSHA256,true);
      journal.append({type:'OWNER_PREFLIGHT',...proof});
      const bootstrap = optionalJob('control-plane-migrate');
      if (bootstrap && configuration.anchorProfile === 'portable') {
        // Portable сохраняет bootstrap receipt. Наличие старого Job не разрешает
        // его повтор или удаление; допускается только доказанно завершённый writer.
        ensure(bootstrap.kind === 'Job' && bootstrap.metadata?.uid && bootstrap.metadata.namespace === namespace &&
          bootstrap.metadata.name === 'control-plane-migrate' &&
          bootstrap.metadata.labels?.['app.kubernetes.io/name'] === 'control-plane' &&
          bootstrap.metadata.labels?.['app.kubernetes.io/component'] === 'migration' &&
          bootstrap.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' &&
          bootstrap.metadata.labels?.['kodex.dev/environment'] === 'production' &&
          bootstrap.metadata.labels?.['kodex.dev/profile'] === 'web-only' &&
          !bootstrap.status?.active && !bootstrap.status?.terminating && bootstrap.status?.succeeded === 1 &&
          bootstrap.status?.conditions?.filter(c => ['Complete','Failed'].includes(c.type) && c.status === 'True').length === 1 &&
          bootstrap.status.conditions.some(c => c.type === 'Complete' && c.status === 'True'), 'MIGRATION_BOOTSTRAP_NOT_COMPLETE');
        const bootstrapPods = JSON.parse(kube(['-n',namespace,'get','pods','-l','batch.kubernetes.io/job-name=control-plane-migrate','-o','json'])).items;
        ensure(bootstrapPods.every(p => p.metadata?.uid && p.metadata.namespace === namespace &&
          p.metadata.ownerReferences?.some(o => o.kind === 'Job' && o.controller === true && o.uid === bootstrap.metadata.uid) &&
          isProvenStoppedPod(p)), 'MIGRATION_BOOTSTRAP_NOT_JOINED');
        journal.append({type:'BOOTSTRAP_TERMINAL_READBACK',jobUID:bootstrap.metadata.uid,
          specSHA256:fingerprint(bootstrap.spec),podUIDs:bootstrapPods.map(p => p.metadata.uid)});
      } else ensure(!bootstrap, 'MIGRATION_JOB_ALREADY_EXISTS');
      ensure(!optionalJob(plan.job.metadata.name), 'MIGRATION_JOB_ALREADY_EXISTS');
      const quiesced = observeCP();ensure(quiesced.stage === 'STOPPED' && quiesced.joined, 'MIGRATION_STOP_JOIN_REQUIRED');
      const job = structuredClone(plan.job); job.metadata.annotations = {[annotation]:planSHA256};
      journal.append({type:'CREATE_INTENT',jobName:job.metadata.name});
      try {
        const ack = JSON.parse(kube(['create','-f','-','-o','json'],JSON.stringify(job)));
        ensure(ack.metadata?.uid, 'MIGRATION_CREATE_UNCONFIRMED');
        journal.append({type:'CREATE_ACK',jobUID:ack.metadata.uid});
      } catch(error) { journal.append({type:'UNKNOWN',code:error.message}); throw error; }
    }
    const result = readJob();output(result);
    if (['FAIL','READBACK_UNCONFIRMED'].includes(result.status)) process.exitCode = 1;
  } finally { journal.close(); }
}

// Проверяет только metadata точной новой schema; Goose log сам не доказывает безопасный owner/retention/purge профиль.
export function validRepositoryBundleMigrationPostcondition(proof, expectedVersion = 20261025000200) {
  return [20261025000200,20261026000100,20261026000200].includes(expectedVersion) && proof?.readOnly==='on' && proof.database==='control_plane' && proof.gooseVersion===expectedVersion &&
    proof.tableOwner==='control_plane_owner' && proof.noBodyColumns===true && proof.foreignKeys===13 &&
    proof.deferredForeignKeys===13 && proof.guardEnabled===true && proof.retentionConsumer===true && proof.purgeGraphGuard===true &&
    proof.purgeNodes===105 && proof.purgeEdges===283 && proof.purgeNodeFingerprint==='f51a0de25bd942a250997775617eeaee' &&
    proof.purgeEdgeFingerprint==='be6e23d9e894687a69a19e9b5777ab09';
}

// Проверяет nullable schema, точные CHECK-условия и неизменяемый срок без чтения бизнес-payload.
export function validOwnerGateExpiryMigrationPostcondition(proof, expectedVersion = 20261026000100) {
  return [20261026000100,20261026000200].includes(expectedVersion) && proof?.readOnly==='on' && proof.database==='control_plane' && proof.gooseVersion===expectedVersion &&
    proof.tableOwner==='control_plane_owner' && ['expiryColumn','stateConstraint','resolutionConstraint',
      'expiryConstraint','dueIndex','immutableGuard','runtimeExecute','publicExecuteRevoked'].every(key=>proof[key]===true);
}

// Проверка включает effective grants всех runtime members и column-level privileges;
// отсутствие прямого table grant само по себе не доказывает закрытую запись.
export function validProjectPurgeAuthorityMigrationPostcondition(proof) {
  return validPurgeAuthoritySnapshot(proof,20261026000200);
}

// Общие effective ACL проверяются одинаково; версия назначается только закрытым профилем.
function validPurgeAuthoritySnapshot(proof,expectedVersion) {
  return proof?.readOnly==='on' && proof.database==='control_plane' && proof.gooseVersion===expectedVersion &&
    proof.tables===2 && Number.isSafeInteger(proof.runtimeRoles) && proof.runtimeRoles>=2 &&
    Number.isSafeInteger(proof.runtimeLogins) && proof.runtimeLogins>=1 && proof.runtimeLogins<proof.runtimeRoles &&
    ['tableOwners','runtimeRolesUnprivileged','runtimeRead','runtimeMutationDenied','runtimeColumnMutationDenied',
      'runtimeGrantOptionDenied','publicPrivilegesRevoked','ownerWrite','authorizationFunction','contextsEmpty'].every(key=>proof[key]===true);
}

// Новый профиль не расширяет принятие прежних версий и проверяет всю назначенную owner schema.
export function validServerWorkflowMigrationPostcondition(proof) {
  return validServerWorkflowSnapshot(proof,20261026001000);
}

// Версия назначается закрытым профилем, исходный server010 validator остаётся exact.
function validServerWorkflowSnapshot(proof,expectedVersion) {
  return validPurgeAuthoritySnapshot(proof,expectedVersion) && proof.serverWorkflowProfile==='SERVER_DAG_V1' &&
    proof.bundleTableOwner==='control_plane_owner' && proof.bundleNoBodyColumns===true && proof.bundleForeignKeys===13 &&
    proof.bundleDeferredForeignKeys===13 && proof.bundleGuard===true &&
    ['schemaOwner','columns','constraints','indexes','triggers','functionSources','functionMetadata','functionACL',
      'visibilitySource','retentionSource','runtimeColumnAccess','purgeGraphGuard'].every(key=>proof[key]===true) &&
    proof.workflowColumns===9 && proof.workflowConstraints===6 && proof.workflowGuards===6 && proof.workflowHelpers===4 &&
    proof.purgeNodes===105 && proof.purgeEdges===284 && proof.purgeBaselineEdges===283 &&
    proof.purgeNodeFingerprint==='f51a0de25bd942a250997775617eeaee' &&
    proof.purgeBaselineEdgeFingerprint==='be6e23d9e894687a69a19e9b5777ab09' && proof.purgeEdgeFingerprint==='6bf6ad76d8c7ae66e07f75ecf719fdd6';
}

// Отдельный011 verifier сохраняет010 закрытым. Новый hash выводится SQL только из прежнего284-графа и двух точно проверенных FK.
export function validLegacyWorkflowReworkMigrationPostcondition(proof) {
  return validWorkflowExceptionSchema(proof,20261026001100);
}

// Версия и число рёбер задаются только закрытыми профилями; payload их не выбирает.
function validWorkflowExceptionSchema(proof, version, edges = 286) {
  return validPurgeAuthoritySnapshot(proof,version) && proof.serverWorkflowProfile==='SERVER_DAG_V1' &&
    proof.legacyReworkProfile==='LEGACY_OWNER_REWORK_V1' &&
    proof.bundleTableOwner==='control_plane_owner' && proof.bundleNoBodyColumns===true && proof.bundleForeignKeys===13 &&
    proof.bundleDeferredForeignKeys===13 && proof.bundleGuard===true &&
    ['schemaOwner','columns','constraints','indexes','triggers','functionSources','functionMetadata','functionACL',
      'visibilitySource','retentionSource','runtimeColumnAccess','purgeGraphGuard','legacyColumns','legacyForeignKeys',
      'legacyConstraint','legacyIndexes','legacyTriggers','legacyFunctions','legacyColumnAccess','legacyPurgeSource'].every(key=>proof[key]===true) &&
    proof.workflowColumns===9 && proof.workflowConstraints===6 && proof.workflowGuards===6 && proof.workflowHelpers===4 &&
    proof.purgeNodes===105 && proof.purgeEdges===edges && proof.purgeBaselineEdges===283 &&
    proof.purgeNodeFingerprint==='f51a0de25bd942a250997775617eeaee' &&
    proof.purgeBaselineEdgeFingerprint==='be6e23d9e894687a69a19e9b5777ab09' &&
    proof.serverPurgeEdges===284 && proof.serverPurgeFingerprint==='6bf6ad76d8c7ae66e07f75ecf719fdd6' &&
    typeof proof.purgeEdgeFingerprint==='string' && /^[a-f0-9]{32}$/.test(proof.purgeEdgeFingerprint);
}

// До012 принимается только полная011 без частично созданных context helpers.
export function validWorkflowExceptionContextMigrationPrecondition(proof) {
  return validWorkflowExceptionSchema(proof,20261026001100) && proof.workflowExceptionContextAbsent===true;
}

// После012 exact function body/metadata/ACL обязательны; прежние010/011 validators не расширяются.
export function validWorkflowExceptionContextMigrationPostcondition(proof) {
  return validWorkflowExceptionSchema(proof,20261026001200) &&
    proof.workflowExceptionContextProfile==='SERVER_EXCEPTION_CONTEXT_V1' &&
    proof.workflowContextFunctions===true && proof.workflowContextExactFunctions===true;
}

// Перед013 проверяется полная текущая012 в том же snapshot и отсутствие всех новых объектов.
export function validWorkflowUpstreamReworkMigrationPrecondition(proof) {
  return validWorkflowExceptionContextMigrationPostcondition(proof) && proof.workflowUpstreamReworkAbsent===true;
}

// Новый013 проверяет весь предшествующий профиль с точными новыми телами и одним FK.
// Старые exported validators сохраняют прежние версии и число рёбер.
export function validWorkflowUpstreamReworkMigrationPostcondition(proof) {
  return validWorkflowExceptionSchema(proof,20261026001300,287) &&
    proof.workflowExceptionContextProfile==='SERVER_EXCEPTION_CONTEXT_V1' &&
    proof.workflowContextFunctions===true && proof.workflowContextExactFunctions===true &&
    proof.workflowUpstreamReworkProfile==='OWNER_APPROVED_UPSTREAM_V1' &&
    ['upstreamFunctions','upstreamExactFunctions','upstreamColumns','upstreamForeignKey','upstreamConstraint',
      'upstreamIndex','upstreamTriggers','upstreamColumnAccess'].every(key=>proof[key]===true);
}

// Полный014 proof сохраняет все проверки013 в том же snapshot; собственные поля
// имеют отдельное пространство имён и не затирают predecessor metadata.
function validWorkflowReviewQAEvidenceSchema(proof, functionCount) {
  return proof?.readOnly==='on' && proof.database==='control_plane' && proof.reviewQAEvidenceProfile==='GITHUB_REVIEW_QA_V1' &&
    proof.reviewQAEvidenceFunctionCount===functionCount && ['SchemaOwner','ExactFunctions','FunctionSources','FunctionMetadata','FunctionACL',
      'RuntimeRolesUnprivileged','Triggers','PurgeGuard'].every(key=>proof['reviewQAEvidence'+key]===true) &&
    proof.purgeNodes===105 && proof.purgeEdges===287 && proof.purgeNodeFingerprint==='f51a0de25bd942a250997775617eeaee' &&
    typeof proof.purgeEdgeFingerprint==='string' && /^[a-f0-9]{32}$/.test(proof.purgeEdgeFingerprint);
}
// Общая проверка оператора и disposable upgrade принимает только полную013 без объектов014.
export function validWorkflowReviewQAEvidenceMigrationPrecondition(proof) {
  return validWorkflowUpstreamReworkMigrationPostcondition(proof) &&
    validWorkflowReviewQAEvidenceSchema(proof,4) && proof.reviewQAEvidenceAbsent===true;
}
// Прежний013 verifier не принимает014; final014 проверяет весь predecessor с новыми четырьмя телами.
export function validWorkflowReviewQAEvidenceMigrationPostcondition(proof) {
  return validWorkflowExceptionSchema(proof,20261026001400,287) &&
    proof.workflowExceptionContextProfile==='SERVER_EXCEPTION_CONTEXT_V1' && proof.workflowContextFunctions===true && proof.workflowContextExactFunctions===true &&
    proof.workflowUpstreamReworkProfile==='OWNER_APPROVED_UPSTREAM_V1' &&
    ['upstreamFunctions','upstreamExactFunctions','upstreamColumns','upstreamForeignKey','upstreamConstraint',
      'upstreamIndex','upstreamTriggers','upstreamColumnAccess'].every(key=>proof[key]===true) &&
    validWorkflowReviewQAEvidenceSchema(proof,8);
}

// Отдельная015 проверяет полный actual graph и точный каталог; вложенная014
// обозначает только inverse projection, а не версию реально обслуживаемой БД.
function validWorkspaceStorageSchema(proof, profile, version, edges) {
  return proof?.profile===profile && proof.readOnly==='on' &&
    proof.gooseVersion===version && proof.workspaceCatalog===true &&
    proof.workspacePurgeGuard===true && proof.purgeNodes===106 && proof.purgeEdges===edges &&
    typeof proof.purgeNodeFingerprint==='string' && /^[a-f0-9]{32}$/.test(proof.purgeNodeFingerprint) &&
    typeof proof.purgeEdgeFingerprint==='string' && /^[a-f0-9]{32}$/.test(proof.purgeEdgeFingerprint) &&
    validWorkflowReviewQAEvidenceMigrationPostcondition(proof.predecessorProjection);
}

// Историческая015 принимает только собственную фактическую версию и граф294.
export function validSessionWorkspaceMigrationPostcondition(proof) {
  return validWorkspaceStorageSchema(proof,'SESSION_WORKSPACE_V2',20261026001500,294);
}

// Отдельная016 требует весь каталог архивов/задач и прежнюю полную inverse014.
export function validWorkspaceCheckpointMigrationPostcondition(proof) {
  return validWorkspaceStorageSchema(proof,'WORKSPACE_CHECKPOINT_V2',20261026001600,296) &&
    proof.checkpointCatalog===true;
}

// SSH-переход сохраняет весь server010/bundle/purge proof и проверяет только своё additive расширение.
export function validSSHComposeMigrationPostcondition(proof) {
  return validServerWorkflowSnapshot(proof,20261027000100) && proof.sshComposeProfile==='SSH_COMPOSE_V1' &&
    ['sshColumns','sshConstraints','sshFunctions','sshTriggers','sshRuntimeColumns'].every(key=>proof[key]===true);
}

// Combined010→017+SSH сохраняет полный010 predecessor; наличие чужой частичной схемы запрещено.
export function validWorkspaceVolumeSSHMigrationPrecondition(proof) {
  return validServerWorkflowMigrationPostcondition(proof) &&
    proof.workspaceVolumeSSHAbsent===true && proof.sshComposeAbsent===true;
}

// Полный actual109/303 и каталог017 проверяются отдельно от неизменённого inverse014.
export function validWorkspaceVolumeSSHMigrationPostcondition(proof) {
  return proof?.profile==='WORKSPACE_VOLUME_SSH_V1' && proof.readOnly==='on' &&
    proof.gooseVersion===20261027000100 && proof.workspaceCatalog===true &&
    proof.checkpointCatalog===true && proof.volumeCatalog===true && proof.workspacePurgeGuard===true &&
    proof.purgeNodes===109 && proof.purgeEdges===303 &&
    typeof proof.purgeNodeFingerprint==='string' && /^[a-f0-9]{32}$/.test(proof.purgeNodeFingerprint) &&
    typeof proof.purgeEdgeFingerprint==='string' && /^[a-f0-9]{32}$/.test(proof.purgeEdgeFingerprint) &&
    proof.sshComposeProfile==='SSH_COMPOSE_V1' &&
    ['sshColumns','sshConstraints','sshFunctions','sshTriggers','sshRuntimeColumns'].every(key=>proof[key]===true) &&
    validWorkflowReviewQAEvidenceMigrationPostcondition(proof.predecessorProjection);
}

// Новый restore-профиль принимает только исходную010 baseline и полный новый actual proof.
export function validWorkspaceRestoreMigrationPrecondition(proof) {
  return validWorkspaceVolumeSSHMigrationPrecondition(proof);
}

// Exact restoreCatalog подтверждается SQL до обратной017+SSH проекции; прежний verifier не расширяется.
export function validWorkspaceRestoreMigrationPostcondition(proof) {
  return proof?.profile==='WORKSPACE_RESTORE_V2' && proof.gooseVersion===20261028000100 &&
    proof.restoreCatalog===true && validWorkspaceVolumeSSHMigrationPostcondition({
      ...proof, profile:'WORKSPACE_VOLUME_SSH_V1',gooseVersion:20261027000100});
}

// Новый каталог GitHub проверяется до явной обратной проекции неизменённого028.
export function validGitHubAutonomousMigrationPostcondition(proof) {
  return proof?.profile==='GITHUB_AUTONOMOUS_PDLC_V1' &&
    proof.gooseVersion===20261029000100 && proof.githubHistoricalCatalog===true &&
    validWorkspaceRestoreMigrationPostcondition({
      ...proof,profile:'WORKSPACE_RESTORE_V2',gooseVersion:20261028000100});
}

// Backup ACL проверяется отдельно до inverse028; bare-профиль не принимает эту проекцию.
export function validGitHubAutonomousBackupMigrationPostcondition(proof) {
  return proof?.profile==='GITHUB_AUTONOMOUS_PDLC_BACKUP_V1' && proof.backupReaderCatalog===true &&
    validGitHubAutonomousMigrationPostcondition({...proof,profile:'GITHUB_AUTONOMOUS_PDLC_V1'});
}

// До DDL нужны точный028, прежний admission и отсутствие новой функции; payload не выбирает версию.
export function validGitHubAutonomousMigrationPrecondition(proof, backup=false) {
  const expected=backup?'GITHUB_AUTONOMOUS_PDLC_BACKUP_PREDECESSOR_V1':'GITHUB_AUTONOMOUS_PDLC_PREDECESSOR_V1';
  return proof?.profile===expected && proof.githubHistoricalAbsent===true &&
    (!backup || proof.backupReaderCatalog===true) &&
    validWorkspaceRestoreMigrationPostcondition({...proof,profile:'WORKSPACE_RESTORE_V2'});
}

// Отдельная точка входа сохраняет discriminator backup-профиля для native upgrade-проверки.
export function validGitHubAutonomousBackupMigrationPrecondition(proof) {
  return validGitHubAutonomousMigrationPrecondition(proof,true);
}

// Новый PR-каталог подтверждается exact SQL до обратной проекции frozen029; исторический verifier не расширяется.
export function validGitHubPRMigrationPostcondition(proof) {
  return proof?.profile==='GITHUB_PR_COLLABORATION_V1' && proof.gooseVersion===20261029000200 &&
    proof.githubPRCatalog===true && proof.githubAutonomousWrites===true && validGitHubAutonomousMigrationPostcondition({
      ...proof,profile:'GITHUB_AUTONOMOUS_PDLC_V1',gooseVersion:20261029000100,githubHistoricalCatalog:true});
}

// Backup authority проверяется на actual030 независимо от исторических обратных проекций.
export function validGitHubPRBackupMigrationPostcondition(proof) {
  return proof?.profile==='GITHUB_PR_COLLABORATION_BACKUP_V1' && proof.backupReaderCatalog===true &&
    validGitHubPRMigrationPostcondition({...proof,profile:'GITHUB_PR_COLLABORATION_V1'});
}

// Предшественник обязан пройти полный029 proof с неизменённым helper, а не только иметь нужную Goose version.
export function validGitHubPRMigrationPrecondition(proof, backup=false) {
  const expected='GITHUB_PR_COLLABORATION_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1';
  return proof?.profile===expected && proof.githubPRWritePredecessor===true && (!backup || proof.backupReaderCatalog===true) &&
    validGitHubAutonomousMigrationPostcondition({...proof,profile:'GITHUB_AUTONOMOUS_PDLC_V1'});
}

// Отдельная публичная точка входа сохраняет discriminator backup-precondition в native upgrade проверке.
export function validGitHubPRBackupMigrationPrecondition(proof) {
  return validGitHubPRMigrationPrecondition(proof,true);
}

// Actual031 catalog проверяется SQL до frozen030 projection; неизвестные версии и digests закрыты.
export function validWorkflowPDLCMigrationPostcondition(proof) {
  return proof?.profile==='GITHUB_PDLC_V1' && proof.gooseVersion===20261029000300 &&
    proof.workflowPDLCCatalog===true && proof.workflowPDLCFunctions===14 &&
    proof.workflowPDLCCatalogDigest===workflowPDLCCatalogDigest && validGitHubPRMigrationPostcondition({
      ...proof,profile:'GITHUB_PR_COLLABORATION_V1',gooseVersion:20261029000200});
}

// Backup031 сохраняет самостоятельный discriminator и actual backup SELECT proof.
export function validWorkflowPDLCBackupMigrationPostcondition(proof) {
  return proof?.profile==='GITHUB_PDLC_BACKUP_V1' && proof.backupReaderCatalog===true &&
    validWorkflowPDLCMigrationPostcondition({...proof,profile:'GITHUB_PDLC_V1'});
}

// Новый профиль требует весь030 predecessor, отсутствие14 функций031 и штатный idle fence.
export function validWorkflowPDLCMigrationPrecondition(proof, backup=false) {
  return proof?.profile==='GITHUB_PDLC_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' &&
    proof.workflowPDLCAbsent===true && (!backup || proof.backupReaderCatalog===true) &&
    validGitHubPRMigrationPostcondition({...proof,profile:'GITHUB_PR_COLLABORATION_V1'});
}

// Отдельная точка входа не смешивает bare и backup schema admission.
export function validWorkflowPDLCBackupMigrationPrecondition(proof) {
  return validWorkflowPDLCMigrationPrecondition(proof,true);
}

// Новый exact032 проверяется до исторической проекции; frozen031 не принимает новый digest.
export function validWorkflowContentPageMigrationPostcondition(proof) {
  return proof?.profile==='WORKFLOW_CONTENT_PAGE_V1' && proof.gooseVersion===20261030000100 &&
    proof.workflowContentPageCatalog===true && proof.workflowPDLCCatalogDigest===workflowContentPageCatalogDigest &&
    validWorkflowPDLCMigrationPostcondition({...proof,profile:'GITHUB_PDLC_V1',gooseVersion:20261029000300,
      workflowPDLCCatalogDigest});
}

// Backup authority подтверждается actual032 SQL независимо от голой схемы.
export function validWorkflowContentPageBackupMigrationPostcondition(proof) {
  return proof?.profile==='WORKFLOW_CONTENT_PAGE_BACKUP_V1' && proof.backupReaderCatalog===true &&
    validWorkflowContentPageMigrationPostcondition({...proof,profile:'WORKFLOW_CONTENT_PAGE_V1'});
}

// Применение допускает только полный actual031 и отдельный штатный idle fence.
export function validWorkflowContentPageMigrationPrecondition(proof, backup=false) {
  return proof?.profile==='WORKFLOW_CONTENT_PAGE_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' &&
    proof.workflowContentPagePredecessor===true && (!backup || proof.backupReaderCatalog===true) &&
    validWorkflowPDLCMigrationPostcondition({...proof,profile:'GITHUB_PDLC_V1'});
}

// Отдельный entrypoint сохраняет discriminator backup профиля.
export function validWorkflowContentPageBackupMigrationPrecondition(proof) {
  return validWorkflowContentPageMigrationPrecondition(proof,true);
}

// Actual backup SELECT проверен до metadata projection; прежний015 verifier не расширяется.
export function validWorkspaceBackupMigrationPrecondition(proof) {
  return proof?.profile==='WORKSPACE_BACKUP_PREDECESSOR_V1' && proof.backupReaderCatalog===true &&
    proof.database==='control_plane' && proof.workspaceBackupSafeToMigrate===true &&
    validSessionWorkspaceMigrationPostcondition({...proof,profile:'SESSION_WORKSPACE_V2'});
}

// Новый discriminator сохраняет отдельное доказательство actual backup authority и inverse frozen028.
export function validWorkspaceBackupMigrationPostcondition(proof) {
  return proof?.profile==='WORKSPACE_RESTORE_BACKUP_V1' && proof.backupReaderCatalog===true &&
    validWorkspaceRestoreMigrationPostcondition({...proof,profile:'WORKSPACE_RESTORE_V2'});
}

// Частичный terminal допускает resume только с возвращённым backup SELECT на известном actual head.
export function validWorkspaceBackupACLReadback(proof) {
  return proof?.readOnly==='on' && proof.database==='control_plane' && proof.backupReaderCatalog===true &&
    [20261026001500,20261026001600,20261026001700,20261027000100,20261028000100].includes(proof.gooseVersion);
}

// Каждый закрытый профиль выбирает свой verifier; неизвестный профиль не получает fallback.
export function validImmutableMigrationPostcondition(profile, proof) {
  switch (profile) {
    case 'github-repository-scope': return validGitHubRepositoryScopeMigrationPostcondition(proof);
    case 'github-repository-scope-backup': return validGitHubRepositoryScopeBackupMigrationPostcondition(proof);
    case 'workspace-unbound-stop': return validWorkspaceUnboundStopMigrationPostcondition(proof);
    case 'workspace-archive-abi': return validWorkspaceArchiveABIMigrationPostcondition(proof);
    case 'opencode-native-sessions': return validOpenCodeNativeMigrationPostcondition(proof);
    case 'workflow-workspace': return validWorkflowWorkspaceMigrationPostcondition(proof);
    case 'managed-workspace': return validManagedWorkspaceMigrationPostcondition(proof);
    case 'artifact-optional-antivirus': return validArtifactOptionalAntivirusMigrationPostcondition(proof);
    case 'integration-resource-policies': return validIntegrationResourcePoliciesMigrationPostcondition(proof);
    case 'integration-resource-policies-backup': return validIntegrationResourcePoliciesBackupMigrationPostcondition(proof);
    case 'authority-proof-counter': return validAuthorityProofCounterMigrationPostcondition(proof);
    case 'authority-proof-counter-backup': return validAuthorityProofCounterBackupMigrationPostcondition(proof);
    case 'jira-create-fields': return validJiraCreateFieldsMigrationPostcondition(proof);
    case 'jira-create-fields-backup': return validJiraCreateFieldsBackupMigrationPostcondition(proof);
    case 'personal-credentials': return validPersonalCredentialsMigrationPostcondition(proof);
    case 'personal-credentials-backup': return validPersonalCredentialsBackupMigrationPostcondition(proof);
    case 'platform-oidc-identity': return validPlatformOIDCIdentityMigrationPostcondition(proof);
    case 'platform-oidc-identity-backup': return validPlatformOIDCIdentityBackupMigrationPostcondition(proof);
    case 'workflow-content-page': return validWorkflowContentPageMigrationPostcondition(proof);
    case 'workflow-content-page-backup': return validWorkflowContentPageBackupMigrationPostcondition(proof);
    case 'workflow-pdlc': return validWorkflowPDLCMigrationPostcondition(proof);
    case 'workflow-pdlc-backup': return validWorkflowPDLCBackupMigrationPostcondition(proof);
    case 'github-pr': return validGitHubPRMigrationPostcondition(proof);
    case 'github-pr-backup': return validGitHubPRBackupMigrationPostcondition(proof);
    case 'github-autonomous': return validGitHubAutonomousMigrationPostcondition(proof);
    case 'github-autonomous-backup': return validGitHubAutonomousBackupMigrationPostcondition(proof);
    case 'workflow-upstream-rework': return validWorkflowUpstreamReworkMigrationPostcondition(proof);
    case 'workflow-exception-context': return validWorkflowExceptionContextMigrationPostcondition(proof);
    case 'workflow-review-qa-evidence': return validWorkflowReviewQAEvidenceMigrationPostcondition(proof);
    case 'server-workflow': return validServerWorkflowMigrationPostcondition(proof);
    case 'legacy-workflow-rework': return validLegacyWorkflowReworkMigrationPostcondition(proof);
    case 'ssh-compose': return validSSHComposeMigrationPostcondition(proof);
    case 'workspace-volume-ssh': return validWorkspaceVolumeSSHMigrationPostcondition(proof);
    case 'workspace-restore': return validWorkspaceRestoreMigrationPostcondition(proof);
    case 'workspace-backup': return validWorkspaceBackupMigrationPostcondition(proof);
    case 'project-purge-authority': return validProjectPurgeAuthorityMigrationPostcondition(proof);
    case 'owner-gate-expiry': return validOwnerGateExpiryMigrationPostcondition(proof);
    case 'repository-bundle': return validRepositoryBundleMigrationPostcondition(proof);
    default: return false;
  }
}

// Новый профиль проверяет только точный переход guard; исторические schema proofs не расширяются.
export function validWorkspaceArchiveABIMigrationPrecondition(proof) {
  return proof?.profile==='WORKSPACE_ARCHIVE_ABI5_PREDECESSOR_V1' && proof.database==='control_plane' &&
    proof.readOnly==='on' && proof.gooseVersion===20261111000200 && proof.archiveGuardPredecessor===true &&
    proof.activeRuntimeLeases===0 && proof.activeArchiveTasks===0;
}

// Exact body и действующий trigger подтверждают установленный ABI5 без переноса либо удаления данных.
export function validWorkspaceArchiveABIMigrationPostcondition(proof) {
  return proof?.profile==='WORKSPACE_ARCHIVE_ABI5_V1' && proof.database==='control_plane' &&
    proof.readOnly==='on' && proof.gooseVersion===20261112000100 && proof.archiveGuardReady===true &&
    proof.archiveTriggerReady===true;
}

// Exact033 сверяет собственные tables/constraints/ACL/guards и полный неизменный predecessor032.
// Только проверенный новый catalog позволяет историческую проекцию Goose version; старые verifiers не расширяются.
export function validPlatformOIDCIdentityMigrationPostcondition(proof) {
  return proof?.profile==='PLATFORM_OIDC_IDENTITY_V1' && proof.gooseVersion===20261105000100 &&
    proof.platformOIDCCatalog===true && proof.platformOIDCCatalogDigest===platformOIDCIdentityCatalogDigest &&
    validWorkflowContentPageMigrationPostcondition({...proof,profile:'WORKFLOW_CONTENT_PAGE_V1',gooseVersion:20261030000100});
}

// Backup033 сверяет отдельный полный catalog, включая ровно SELECT нового набора таблиц.
export function validPlatformOIDCIdentityBackupMigrationPostcondition(proof) {
  return proof?.profile==='PLATFORM_OIDC_IDENTITY_BACKUP_V1' && proof.backupReaderCatalog===true &&
    proof.platformOIDCCatalogDigest===platformOIDCIdentityBackupCatalogDigest &&
    validPlatformOIDCIdentityMigrationPostcondition({...proof,profile:'PLATFORM_OIDC_IDENTITY_V1',
      platformOIDCCatalogDigest:platformOIDCIdentityCatalogDigest});
}

// Новый profile требует actual032 без следов033; idle/claim fence дополнительно проверяет CLI.
export function validPlatformOIDCIdentityMigrationPrecondition(proof, backup=false) {
  return proof?.profile==='PLATFORM_OIDC_IDENTITY_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' &&
    proof.platformOIDCAbsent===true && (!backup || proof.backupReaderCatalog===true) &&
    validWorkflowContentPageMigrationPostcondition({...proof,profile:'WORKFLOW_CONTENT_PAGE_V1'});
}

// Закрытый native entrypoint не смешивает plain и backup precondition.
export function validPlatformOIDCIdentityBackupMigrationPrecondition(proof) {
  return validPlatformOIDCIdentityMigrationPrecondition(proof,true);
}

// Exact034 сверяет полный personal catalog, включая прежние затронутые relations и trigger
// functions. Frozen033 не расширяется: историческая проекция допускается только после exact034.
export function validPersonalCredentialsMigrationPostcondition(proof) {
  return proof?.profile==='PERSONAL_INTEGRATION_CREDENTIALS_V1' && proof.gooseVersion===20261106000100 &&
    proof.personalCredentialsCatalog===true && proof.personalCredentialsCatalogDigest===personalCredentialsCatalogDigest &&
    validPlatformOIDCIdentityMigrationPostcondition(personalCredentialsPredecessorProof(proof,false));
}

// Backup034 отдельно закрепляет ровно SELECT восьми явно перечисленных personal/lineage tables.
export function validPersonalCredentialsBackupMigrationPostcondition(proof) {
  return proof?.profile==='PERSONAL_INTEGRATION_CREDENTIALS_BACKUP_V1' && proof.gooseVersion===20261106000100 &&
    proof.personalCredentialsCatalog===true && proof.personalCredentialsCatalogDigest===personalCredentialsBackupCatalogDigest &&
    validPlatformOIDCIdentityBackupMigrationPostcondition(personalCredentialsPredecessorProof(proof,true));
}

// Только exact034 catalog и фактический полный новый graph допускают историческую projection033.
// Значения predecessor получены из того же проверенного graph за вычетом двух новых nodes;
// само тело purge и все новые FK независимо закреплены в catalog, старые verifiers не меняются.
function personalCredentialsPredecessorProof(proof,backup) {
  const graph=personalCredentialsPurgeGraph;
  if(proof.purgeNodes!==graph.nodeCount || proof.purgeEdges!==graph.edgeCount ||
    proof.purgeNodeFingerprint!==graph.nodeFingerprint || proof.purgeEdgeFingerprint!==graph.edgeFingerprint) return null;
  return {...proof,profile:backup?'PLATFORM_OIDC_IDENTITY_BACKUP_V1':'PLATFORM_OIDC_IDENTITY_V1',gooseVersion:20261105000100,
    purgeNodes:graph.predecessorNodeCount,purgeEdges:graph.predecessorEdgeCount,
    purgeNodeFingerprint:graph.predecessorNodeFingerprint,purgeEdgeFingerprint:graph.predecessorEdgeFingerprint};
}

// Precondition требует actual033 без следов034 и без незавершённых integration effects/tests.
// Общий CLI дополнительно сверяет все прежние idle/claim fences перед единственным применением.
export function validPersonalCredentialsMigrationPrecondition(proof, backup=false) {
  const previous={...proof,profile:backup?'PLATFORM_OIDC_IDENTITY_BACKUP_V1':'PLATFORM_OIDC_IDENTITY_V1'};
  return proof?.profile==='PERSONAL_INTEGRATION_CREDENTIALS_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' &&
    proof.readOnly==='on' && proof.database==='control_plane' &&
    proof.personalCredentialsAbsent===true && proof.activeIntegrationInvocations===0 && proof.activeIntegrationConnectionTests===0 &&
    (backup?validPlatformOIDCIdentityBackupMigrationPostcondition(previous):validPlatformOIDCIdentityMigrationPostcondition(previous));
}

// Отдельный native entrypoint сохраняет закрытое различие normal и backup precondition.
export function validPersonalCredentialsBackupMigrationPrecondition(proof) {
  return validPersonalCredentialsMigrationPrecondition(proof,true);
}

// Exact035 сначала проверяет собственный полный catalog; только затем проецирует неизменённый034.
// Старый verifier по-прежнему отвергает actual035, чужой digest и неполный successor.
export function validJiraCreateFieldsMigrationPostcondition(proof, backup=false) {
  return proof?.profile==='JIRA_CREATE_FIELDS_'+(backup?'BACKUP_':'')+'V1' &&
    proof.gooseVersion===20261107000100 && proof.personalCredentialsCatalog===true &&
    proof.personalCredentialsCatalogDigest===jiraCreateFieldsCatalog[backup?'backup':'normal'] &&
    (backup?validPersonalCredentialsBackupMigrationPostcondition:validPersonalCredentialsMigrationPostcondition)({
      ...proof,profile:'PERSONAL_INTEGRATION_CREDENTIALS_'+(backup?'BACKUP_':'')+'V1',gooseVersion:20261106000100,
      personalCredentialsCatalogDigest:backup?personalCredentialsBackupCatalogDigest:personalCredentialsCatalogDigest,
    });
}

// Backup035 имеет отдельную точку входа с exact SELECT ACL, без fallback на normal профиль.
export function validJiraCreateFieldsBackupMigrationPostcondition(proof) {
  return validJiraCreateFieldsMigrationPostcondition(proof,true);
}

// Forward035 разрешён лишь поверх actual034 и после завершения всех integration effects/probes.
export function validJiraCreateFieldsMigrationPrecondition(proof, backup=false) {
  return proof?.profile==='JIRA_CREATE_FIELDS_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' &&
    proof.activeIntegrationInvocations===0 && proof.activeIntegrationConnectionTests===0 && proof.activePersonalCredentialTests===0 &&
    ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
      'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','oldClaimSessions'].every(key=>proof[key]===0) &&
    (backup?validPersonalCredentialsBackupMigrationPostcondition:validPersonalCredentialsMigrationPostcondition)({
      ...proof,profile:'PERSONAL_INTEGRATION_CREDENTIALS_'+(backup?'BACKUP_':'')+'V1',
    });
}

// Отдельный backup precondition не позволяет принимать обычный catalog с отсутствующим SELECT.
export function validJiraCreateFieldsBackupMigrationPrecondition(proof) {
  return validJiraCreateFieldsMigrationPrecondition(proof,true);
}

// Exact036 проверяет counter и frozen035 отдельно; исторический verifier не принимает новый head.
export function validAuthorityProofCounterMigrationPostcondition(proof, backup=false) {
  return proof?.profile==='AUTHORITY_PROOF_COUNTER_'+(backup?'BACKUP_':'')+'V1' &&
    proof.gooseVersion===20261108000100 && proof.authorityProofCounterCatalog===true &&
    proof.authorityProofCounterCatalogDigest===authorityProofCounterCatalog[backup?'backup':'normal'] &&
    validJiraCreateFieldsMigrationPostcondition({...proof,
      profile:'JIRA_CREATE_FIELDS_'+(backup?'BACKUP_':'')+'V1',gooseVersion:20261107000100},backup);
}

// Backup036 требует SELECT-only counter ACL и полный backup035, без normal fallback.
export function validAuthorityProofCounterBackupMigrationPostcondition(proof) {
  return validAuthorityProofCounterMigrationPostcondition(proof,true);
}

// Новый allocator запускается только после exact035, отсутствующего successor и joined work.
export function validAuthorityProofCounterMigrationPrecondition(proof, backup=false, requireStopped=true) {
  return proof?.profile==='AUTHORITY_PROOF_COUNTER_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' &&
    proof.authorityProofCounterAbsent===true &&
    Number.isSafeInteger(proof.oldClaimSessions) && proof.oldClaimSessions>=0 && (!requireStopped || proof.oldClaimSessions===0) &&
    ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
      'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks',
      'activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests'].every(key=>proof[key]===0) &&
    validJiraCreateFieldsMigrationPostcondition({...proof,profile:'JIRA_CREATE_FIELDS_'+(backup?'BACKUP_':'')+'V1'},backup);
}

// Отдельный entrypoint сохраняет различие predecessor normal/backup и его exact ACL.
export function validAuthorityProofCounterBackupMigrationPrecondition(proof) {
  return validAuthorityProofCounterMigrationPrecondition(proof,true);
}

// Readback037 проверяет объекты текущей миграции; historical catalog не проецируется.
export function validIntegrationResourcePoliciesMigrationPostcondition(proof, backup=false) {
  return proof?.profile === 'INTEGRATION_RESOURCE_POLICIES_' + (backup ? 'BACKUP_' : '') + 'V2' &&
    proof.readOnly === 'on' && proof.database === 'control_plane' && proof.gooseVersion === 20261109000100 &&
    ['migrationTablesReady', 'integrationPolicyColumns', 'integrationPolicyConstraints', 'integrationPolicyFunctions',
      'integrationPolicyGuards', 'integrationPolicyPrivileges', 'authorityProofCounterReady', 'workspaceGuards', 'backupReaderSafe']
      .every(key => proof[key] === true) &&
    (!backup || proof.backupReaderConfigured === true && proof.backupReaderReady === true);
}

// Backup mode дополнительно требует фактический SELECT на ограниченном наборе таблиц.
export function validIntegrationResourcePoliciesBackupMigrationPostcondition(proof) {
  return validIntegrationResourcePoliciesMigrationPostcondition(proof, true);
}

// Перед037 проверяются его условия и отсутствие активных effects, без проекции каталога036.
export function validIntegrationResourcePoliciesMigrationPrecondition(proof, backup=false, requireStopped=true) {
  return proof?.profile === 'INTEGRATION_RESOURCE_POLICIES_' + (backup ? 'BACKUP_' : '') + 'PREDECESSOR_V2' &&
    proof.readOnly === 'on' && proof.database === 'control_plane' && proof.gooseVersion === 20261108000100 &&
    ['migrationTablesReady', 'integrationResourcePoliciesAbsent', 'authorityProofCounterReady', 'backupReaderSafe']
      .every(key => proof[key] === true) && (!backup || proof.backupReaderConfigured === true) &&
    Number.isSafeInteger(proof.oldClaimSessions) && proof.oldClaimSessions >= 0 && (!requireStopped || proof.oldClaimSessions === 0) &&
    ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
      'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations',
      'activeIntegrationConnectionTests','activePersonalCredentialTests'].every(key => proof[key] === 0);
}

// Отдельная точка backup precondition сохраняет закрытый ACL profile.
export function validIntegrationResourcePoliciesBackupMigrationPrecondition(proof){return validIntegrationResourcePoliciesMigrationPrecondition(proof,true);}

// Новый ordinary-file профиль не переименовывает отсутствие AV в CLEAN.
export function validArtifactOptionalAntivirusMigrationPostcondition(proof) {
  return proof?.profile === 'ARTIFACT_OPTIONAL_ANTIVIRUS_V1' && proof.database === 'control_plane' && proof.readOnly === 'on' &&
    proof.gooseVersion === 20261110000100 &&
    ['constraintsValidated','explicitNoScanReceipt','noForgedClean','realScanReceipt','noAbsentReceipt']
      .every(key => proof[key] === true);
}

// Переход038 требует exact037 и тишину файловых/runtime effects, без расширения backup grants.
export function validArtifactOptionalAntivirusMigrationPrecondition(proof, requireStopped=true) {
  return proof?.profile === 'ARTIFACT_OPTIONAL_ANTIVIRUS_PREDECESSOR_V1' &&
    proof.database === 'control_plane' && proof.readOnly === 'on' && proof.gooseVersion === 20261109000100 &&
    proof.artifactOptionalAntivirusAbsent === true && proof.receiptConstraintsReady === true &&
    Number.isSafeInteger(proof.oldClaimSessions) && proof.oldClaimSessions >= 0 &&
    (!requireStopped || proof.oldClaimSessions === 0) &&
    ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
      'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations',
      'activeIntegrationConnectionTests','activePersonalCredentialTests'].every(key => proof[key] === 0);
}

// Новый scope требует завершённого текущего исполнения; он не меняет данные прежних workspace.
export function validWorkflowWorkspaceMigrationPrecondition(proof) {
  return proof?.profile === 'WORKFLOW_WORKSPACE_PREDECESSOR_V1' && proof.database === 'control_plane' &&
    proof.readOnly === 'on' && proof.gooseVersion === 20261110000200 &&
    proof.workflowWorkspaceAbsent === true && proof.workspaceGuardReady === true && proof.archiveTaskGuardReady === true &&
    ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases',
      'activeWorkspaceVolumeTasks','activeWorkspaceArchiveTasks'].every(key => proof[key] === 0);
}

// Исторические schema proofs не принимают новый guard либо новый version pin.
export function validWorkflowWorkspaceMigrationPostcondition(proof) {
  return proof?.profile === 'WORKFLOW_WORKSPACE_V1' && proof.database === 'control_plane' &&
    proof.readOnly === 'on' && proof.gooseVersion === 20261111000100 &&
    proof.workflowVersionColumnReady === true && proof.rootVersionReferenceReady === true &&
    proof.scopeFunctionReady === true && proof.workspaceGuardReady === true && proof.archiveTaskGuardReady === true;
}

// Новый runtime допускается только после отдельно разрешённой очистки session data.
export function validOpenCodeNativeMigrationPrecondition(proof) {
  return proof?.profile==='OPENCODE_NATIVE_PREDECESSOR_V1' && proof.database==='control_plane' &&
    proof.readOnly==='on' && proof.gooseVersion===20261111000100 && proof.sessionDataEmpty===true &&
    proof.nativeColumnsAbsent===true && proof.activeRuntimeLeases===0;
}

// Metadata проверяет именно новый native contract; старый schema proof не заменяет его.
export function validOpenCodeNativeMigrationPostcondition(proof) {
  return proof?.profile==='OPENCODE_NATIVE_V1' && proof.database==='control_plane' &&
    proof.readOnly==='on' && proof.gooseVersion===20261111000200 &&
    ['nativeColumnsReady','legacyIdentityAbsent','nativeShapeReady','nativeIdentityGuardReady','nativeConstraintsReady','nativeWriterForeignKeysReady','nativePurgeGraphReady','nativePurgeGuardReady','runtimeFileVisibilityReady'].every(key=>proof[key]===true);
}

// Переход039 расширяет immutable policy при выключенном opt-in и не требует dump либо новых ACL.
export function validManagedWorkspaceMigrationPrecondition(proof) {
  return proof?.profile === 'MANAGED_WORKSPACE_PREDECESSOR_V1' && proof.database === 'control_plane' &&
    proof.readOnly === 'on' && proof.gooseVersion === 20261110000100 &&
    proof.environmentVersionsReady === true && proof.managedWorkspaceAbsent === true;
}

// Проверка сохраняет пригодность после включения функции: существующие true-версии не переписываются.
export function validManagedWorkspaceMigrationPostcondition(proof) {
  return proof?.profile === 'MANAGED_WORKSPACE_V1' && proof.database === 'control_plane' &&
    proof.readOnly === 'on' && proof.gooseVersion === 20261110000200 && proof.managedWorkspaceColumnReady === true;
}

// Отдельный043 проверяет predecessor042 и отсутствие новой таблицы без изменения данных.
export function validWorkspaceUnboundStopMigrationPrecondition(proof) {
 return proof?.profile==='WORKSPACE_UNBOUND_STOP_PREDECESSOR_V1' && proof.database==='control_plane' && proof.readOnly==='on' &&
  proof.gooseVersion===20261112000100 && proof.unboundStopAbsent===true && proof.workspaceGuardPredecessor===true &&
  proof.activeRuntimeLeases===0 && proof.activeArchiveTasks===0;
}
// Новый guard допускает только durable no-BIND retirement; ABI5 archive guard сохраняется.
export function validWorkspaceUnboundStopMigrationPostcondition(proof) {
 return proof?.profile==='WORKSPACE_UNBOUND_STOP_V1' && proof.database==='control_plane' && proof.readOnly==='on' &&
  proof.gooseVersion===20261113000100 && ['workspaceGuardReady','receiptGuardReady','receiptTableReady','receiptTriggerReady','archiveGuardPreserved'].every(key=>proof[key]===true);
}

// Полный catalog044 включает owner/RLS/ACL/ABI, все таблицы/functions/triggers и неизменённый purge graph.
export function validGitHubRepositoryScopeMigrationPostcondition(proof,backup=false) {
  const expected=githubRepositoryScopeCatalog[backup?'backup':'normal'].after;
  return proof?.profile==='GITHUB_REPOSITORY_SCOPE_'+(backup?'BACKUP_':'')+'V1' && proof.database==='control_plane' && proof.readOnly==='on' && proof.gooseVersion===20261114120000 && exactGitHubSchemaCatalog(proof.schemaCatalog,expected);
}
// Backup не получает fallback к normal: весь фактический SELECT/defaultACL закреплён отдельно.
export function validGitHubRepositoryScopeBackupMigrationPostcondition(proof){return validGitHubRepositoryScopeMigrationPostcondition(proof,true);}
// Precondition связывает exact043 catalog с тишиной каждого затронутого worker/effect lifecycle.
export function validGitHubRepositoryScopeMigrationPrecondition(proof,backup=false,requireStopped=true){
 return proof?.profile==='GITHUB_REPOSITORY_SCOPE_'+(backup?'BACKUP_':'')+'PREDECESSOR_V1' && proof.database==='control_plane' && proof.readOnly==='on' && proof.gooseVersion===20261113000100 && proof.githubRepositoryScopeAbsent===true && exactGitHubSchemaCatalog(proof.schemaCatalog,githubRepositoryScopeCatalog[backup?'backup':'normal'].before) && Number.isSafeInteger(proof.oldClaimSessions) && proof.oldClaimSessions>=0 && (!requireStopped||proof.oldClaimSessions===0) && ['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks'].every(key=>proof[key]===0);
}
// Каноническое сравнение включает точный набор полей; extra/missing/drift закрыто отвергаются.
function exactGitHubSchemaCatalog(actual,expected){return actual!==null && typeof actual==='object' && !Array.isArray(actual) && Object.keys(actual).length===Object.keys(expected).length && Object.entries(expected).every(([key,value])=>JSON.stringify(actual[key])===JSON.stringify(value));}
export function validGitHubRepositoryScopeBackupMigrationPrecondition(proof){return validGitHubRepositoryScopeMigrationPrecondition(proof,true);}
