#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Control-plane PostgreSQL test failed: %s\n' "$*" >&2
  exit 1
}

repository_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
container_name="kodex-control-plane-postgres-${BASHPID}"
go_parallelism=${KODEX_CONTROL_PLANE_TEST_GO_PARALLELISM:-2}
[[ "$go_parallelism" =~ ^[12]$ ]] || fail 'Go parallelism must be 1 or 2'
storage_mode=${KODEX_TEST_POSTGRES_STORAGE:-disk}
[[ "$storage_mode" == disk || "$storage_mode" == ram ]] || fail 'test PostgreSQL storage must be disk or ram'
postgres_image='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'
upgrade_container_id=""
kubernetes_plan_consumer_directory=""
kubernetes_plan_consumer_required=false
ssh_gateway_consumer_directory=""
ssh_gateway_consumer_required=false
ssh_current_schema_required=false
test_pattern=${1:-${KODEX_CONTROL_PLANE_TEST_FILTER:-'^(TestSentryObservabilityPersonalProfilesComponent|TestAuthorityProofCounterOwnerConcurrencyComponent|TestRoleImageOwnerTransactionRetryComponent|TestRoleImageOwnerTransactionRetryBoundsComponent|TestInstructionCommandProjectionComponent|TestIntegrationAutonomousGitHubInvocationComponent|TestIntegrationGitHubHistoricalAdmissionComponent|TestIntegrationGitHubBackupSchemaProofComponent|TestBootstrapComponent|TestConcurrentBootstrapComponent|TestWorkerGrantInstancesComponent|TestTrustedWorkloadGenerationComponent|TestServiceCredentialColdStartComponent|TestNativeSessionOwnerComponent|TestWorkspaceWriterOwnerComponent|TestWorkspaceUnboundStopOwnerComponent|TestWorkflowWorkspaceOwnerComponent|TestModelDiscoveryEligibilityComponent|TestModelProviderBootstrapComponent|TestModelProviderCatalogComponent|TestPersonalModelAccountLaunchComponent|TestModelCatalogBootstrapMethodComponent|TestAssistantCredentialScopesComponent|TestAssistantParallelLifecycleComponent|TestProjectAssistantProfilesComponent|TestOrganizationRoleImagesComponent|TestOrganizationRuntimeEnvironmentComponent|TestOrganizationRuntimeSecretComponent|TestAssistantSystemRunProjectScopeComponent|TestArtifactOptionalRuntimeComponent|TestRuntimeResourceQueueComponent|TestRuntimeTerminalStorageComponent|TestRuntimeSecretDraftReadinessComponent|TestRuntimeOverloadCompletionComponent|TestIntegrationGateFeedbackComponent|TestWorkflowMetadataDraftComponent|TestDeletedEnvironmentSecretRetentionComponent|TestHistoricalEnvironmentSecretAdmissionComponent|TestHistoricalSecretRecoveryConcurrencyComponent|TestWorkflowGateDecisionsComponent|TestCompletionReceiptComponent|TestNativeCompletionCapabilityRevokeComponent|TestUsageCompletenessComponent|TestRepositoryBundlePublicationComponent|TestPersonalGitHubScopedPolicyComponent|TestRepositoryBundlePersonalCredentialComponent|TestCommitAckArtifactComponent|TestOwnerGateExpiryComponent|TestOwnerGateServerCompletionComponent|TestOwnerGateExpiryMigrationProofComponent|TestProjectPurgeAuthorityComponent|TestProjectPurgeAuthorityMigrationProofComponent|TestServerWorkflowMigrationProofComponent|TestOverviewEligibilityComponent|TestWorkflowExceptionContextComponent|TestWorkflowExceptionContextMigrationProofComponent)$'}}

# Оба закрытых upgrade-сценария используют одну и ту же отдельную пустую БД.
upgrade_timeout=180s
owner_timeout=120s
owner_process_timeout=140s
upgrade_test='^TestWorkflowReviewQAEvidenceForwardUpgrade$'
if [[ "$test_pattern" == --artifact-retention-grant-upgrade ]]; then
  upgrade_test='^TestArtifactRetentionGrantCatalogMigration$'
  test_pattern=--review-qa-upgrade
elif [[ "$test_pattern" == --artifact-retention-claim-types ]]; then
  upgrade_test='^TestArtifactRetentionClaimParameterTypes$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --integration-cohort-schema-capture ]]; then
  [[ "${KODEX_INTEGRATION_COHORT_CAPTURE:-}" == 1 &&
     -n ${KODEX_INTEGRATION_COHORT_CAPTURE_DIRECTORY:-} && "$storage_mode" == ram ]] || fail 'cohort capture requires explicit private RAM profile'
  upgrade_timeout=600s
  upgrade_test='^TestIntegrationCohortCatalogCapture$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --telegram-notifications-capture ]]; then
  upgrade_timeout=600s
  [[ -n ${KODEX_TELEGRAM_NOTIFICATIONS_SCHEMA_CAPTURE_DIRECTORY:-} ]] || fail 'Telegram schema capture requires a private capture directory'
  upgrade_test='^TestTelegramNotificationsCatalogCapture$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --personal-integrations-upgrade || "$test_pattern" == --personal-integrations-backup-upgrade ]]; then
  upgrade_timeout=240s
  if [[ "$test_pattern" == --personal-integrations-backup-upgrade ]]; then
    upgrade_test='^TestPersonalIntegrationsBackupUpgrade$'
  else
    upgrade_test='^TestPersonalIntegrationsNormalUpgrade$'
  fi
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --telegram-notifications-upgrade || "$test_pattern" == --telegram-notifications-backup-upgrade ]]; then
  upgrade_timeout=600s
  if [[ "$test_pattern" == --telegram-notifications-backup-upgrade ]]; then upgrade_test='^TestTelegramNotificationsBackupUpgrade$'; else upgrade_test='^TestTelegramNotificationsUpgrade$'; fi
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --sentry-observability-upgrade ]]; then
  upgrade_timeout=600s
  upgrade_test='^TestSentryObservabilityUpgrade$'
  if [[ -n ${KODEX_SENTRY_OBSERVABILITY_SCHEMA_CAPTURE_DIRECTORY:-} ]]; then upgrade_test='^TestSentryObservabilityCatalogCapture$'; fi
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --sentry-observability-backup-upgrade ]]; then
  upgrade_timeout=600s
  upgrade_test='^TestSentryObservabilityBackupUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --github-repository-scope-upgrade || "$test_pattern" == --github-repository-scope-backup-upgrade ]]; then
  if [[ "$test_pattern" == --github-repository-scope-backup-upgrade ]]; then export KODEX_GITHUB_UPGRADE_MODE=backup; else export KODEX_GITHUB_UPGRADE_MODE=normal; fi
  command -v node >/dev/null 2>&1 || fail 'node is required for schema verifier'
  upgrade_timeout=600s
  upgrade_test='^TestGitHubRepositoryScopeUpgrade$'
  if [[ -n ${KODEX_GITHUB_SCHEMA_CAPTURE_DIRECTORY:-} ]]; then upgrade_test='^TestGitHubRepositoryScopeCatalogCapture$'; fi
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --github-repository-scope-retained-workspace ]]; then
  upgrade_test='^TestGitHubBackupRetainedWorkspace$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --github-repository-scope-public-sequence-negative ]]; then
  upgrade_test='^TestGitHubRepositoryScopePublicSequenceRestore$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-unbound-stop-upgrade ]]; then
  upgrade_test='^TestWorkspaceUnboundStopUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-unbound-stop-fresh-install ]]; then
  upgrade_test='^TestWorkspaceUnboundStopFreshInstall$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-archive-abi-fresh-install ]]; then
  upgrade_test='^TestWorkspaceArchiveABIFreshInstall$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-archive-abi-upgrade ]]; then
  upgrade_test='^TestWorkspaceArchiveABIUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --opencode-native-upgrade ]]; then
  upgrade_test='^TestOpenCodeNativeSessionCutover$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workflow-workspace-upgrade ]]; then
  upgrade_test='^TestWorkflowWorkspaceScopeUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --managed-workspace-upgrade ]]; then
  upgrade_test='^TestManagedWorkspaceColumnUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --artifact-optional-antivirus-upgrade ]]; then
  upgrade_test='^TestArtifactOptionalAntivirusMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --integration-resource-policies-upgrade ]]; then
  if ! command -v pg_dump >/dev/null || ! command -v pg_restore >/dev/null; then fail 'resource policy recovery requires PostgreSQL dump/restore clients'; fi
  upgrade_test='^TestIntegrationResourcePolicyMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --integration-resource-policies-fresh-install ]]; then
  upgrade_test='^TestIntegrationResourcePolicyFreshInstall$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --authority-proof-counter-upgrade ]]; then
  if ! command -v pg_dump >/dev/null || ! command -v pg_restore >/dev/null; then fail 'proof counter recovery requires PostgreSQL dump/restore clients'; fi
  upgrade_test='^TestAuthorityProofCounterMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --authority-proof-counter-fresh-install ]]; then
  if ! command -v pg_dump >/dev/null || ! command -v pg_restore >/dev/null; then fail 'proof counter recovery requires PostgreSQL dump/restore clients'; fi
  upgrade_test='^TestAuthorityProofCounterFreshInstall$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --jira-create-fields-upgrade ]]; then
  upgrade_test='^TestJiraCreateFieldsMigration$'
  test_pattern=--review-qa-upgrade
fi
# Явный metadata capture не является portable-release qualification либо install permission.
if [[ "$test_pattern" == --personal-credentials-schema-capture ]]; then
  [[ "${KODEX_PERSONAL_SCHEMA_CAPTURE:-}" == 1 ]] || fail 'personal schema capture requires explicit opt-in'
  upgrade_test='^TestPersonalIntegrationCredentialCatalog$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --personal-credentials-catalog ]]; then
  [[ "${KODEX_PERSONAL_SCHEMA_CAPTURE:-}" != 1 ]] || fail 'catalog qualification cannot use capture mode'
  upgrade_test='^TestPersonalIntegrationCredentialCatalog$'
  test_pattern=--review-qa-upgrade
fi
volume_owner=false
owner_test='^TestWorkspaceWriterOwnerComponent$'
owner_timeout=120s
# Текущий SSH-профиль использует normal full schema и прежний exact native consumer.
# Исторический development seed049 остаётся отдельной неизменённой веткой.
case "$test_pattern" in
  --ssh-identity-owner-current)
    [[ ! -v KODEX_CONTROL_PLANE_TEST_FILTER ]] || fail 'current SSH owner profile forbids an external test filter'
    [[ $# -eq 1 && "$storage_mode" == ram ]] || fail 'current SSH owner profile requires one explicit argument and RAM storage'
    ssh_gateway_consumer_required=true
    ssh_current_schema_required=true
    test_pattern='^TestSSHExecutionOwnerComponent$'
    ;;
  --ssh-identity-owner-current*) fail 'current SSH owner profile is invalid' ;;
esac
if [[ "$test_pattern" == --workflow-file-provenance-backup ]]; then
  upgrade_timeout=180s
  upgrade_test='^TestWorkflowFileProvenanceBackupUpgrade$'
  volume_owner=true
  owner_timeout=240s
  owner_test='^(TestWorkflowExceptionContextComponent|TestWorkflowCaptureProgressComponent)$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --managed-kubernetes-owner-regression || "$test_pattern" == --managed-kubernetes-setup-ledger || "$test_pattern" == --managed-kubernetes-health || "$test_pattern" == --managed-kubernetes-invocation || "$test_pattern" == --managed-kubernetes-output || "$test_pattern" == --managed-kubernetes-port-forward || "$test_pattern" == --managed-kubernetes-file || "$test_pattern" == --managed-kubernetes-capture || "$test_pattern" == --managed-kubernetes-effect-acl || "$test_pattern" == --integration-pending-root-owner-regression ]]; then
  # Owner Bootstrap обрабатывает все поставляемые определения. SSH_TARGET
  # допускается с 048, общие health-запросы используют 049.
  volume_owner=true
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^(TestPersonalIntegrationCredentialsComponent|TestKubernetesOperatorRegistryComponent|TestKubernetesIdentitySetupLedgerComponent|TestKubernetesIdentityAcknowledgementOwnerComponent|TestKubernetesIdentityAcknowledgementConflictOwnerComponent)$'
  if [[ "$test_pattern" == --managed-kubernetes-setup-ledger ]]; then owner_test='^TestKubernetesIdentity(SetupLedger|AcknowledgementOwner|AcknowledgementConflictOwner)Component$'; fi
  if [[ "$test_pattern" == --managed-kubernetes-health ]]; then owner_test='^TestKubernetes(IdentityHealth|AuthorityFailure)OwnerComponent$'; fi
  if [[ "$test_pattern" == --managed-kubernetes-effect-acl ]]; then owner_test='^TestKubernetes(EffectACLDeadline|SharedIdentity)OwnerComponent$'; fi
  if [[ "$test_pattern" == --integration-pending-root-owner-regression ]]; then owner_test='^TestIntegrationPendingRootOwnerComponent$'; fi
  if [[ "$test_pattern" == --managed-kubernetes-invocation || "$test_pattern" == --managed-kubernetes-output || "$test_pattern" == --managed-kubernetes-port-forward || "$test_pattern" == --managed-kubernetes-file || "$test_pattern" == --managed-kubernetes-capture ]]; then
    # Полный owner-набор включает последовательные Stop/Gate/continuation fixtures
    # и два OUTPUT-сценария с настоящим ожиданием expiry. Их бюджет явный и конечный.
    # Запас внешнего timeout оставляет Go время вывести диагностику зависания.
    owner_timeout=480s
    owner_process_timeout=510s
    owner_test='^(TestKubernetes(Invocation|Grant|ResourcePolicy|Effect|Metadata|ChildGate|Delivery|Input|InputDisclosureDeadline|InputPreludeDeadline|Tool|InvocationStreamResult|CompletionContinuation|ReadinessACL|CaptureSeal|CaptureDeadline|CapturePreparedCleanup|StreamOutput|StreamOutputDeadline|PortForward|PortForwardPreflight|File)OwnerComponent|TestKubernetesInvocationAggregate|TestKubernetesOutputDeadlineNeverWidensNativeCredit|TestKubernetesOutputStageRejectsDamagedReceipt)$'
    if [[ "$test_pattern" == --managed-kubernetes-port-forward ]]; then owner_test='^TestKubernetesPortForward(Preflight)?OwnerComponent$'; fi
    if [[ "$test_pattern" == --managed-kubernetes-file ]]; then
      owner_timeout=120s
      owner_process_timeout=140s
      owner_test='^TestKubernetesFileOwnerComponent$'
    fi
    if [[ "$test_pattern" == --managed-kubernetes-capture ]]; then
      owner_timeout=80s
      owner_process_timeout=100s
      owner_test='^TestKubernetesCapture(Seal|Deadline|PreparedCleanup)OwnerComponent$'
    fi
    kubernetes_plan_consumer_required=true
    if [[ "$test_pattern" == --managed-kubernetes-output ]]; then
      owner_timeout=80s
      owner_process_timeout=100s
      owner_test='^(TestKubernetesStreamOutput(Deadline)?OwnerComponent|TestAccessDisclosureDeadlineUsesOnlyCurrentWinningAlternatives|TestAccessDisclosureDeadlineRejectsUnresolvedDecision)$'
    fi
  fi
  test_pattern=--review-qa-upgrade
fi
owner_go_arguments=()
if [[ "$test_pattern" == --integration-resource-policies-owner ]]; then
  volume_owner=true
  upgrade_test='^TestIntegrationResourcePolicyOwnerRegressionSeed$'
  owner_test='^(TestScopedIntegrationApprovalComponent|TestIntegrationGateFeedbackComponent|TestIntegrationAutonomousGitHubInvocationComponent|TestIntegrationGitHubHistoricalAdmissionComponent|TestPersonalIntegrationCredentialsComponent|TestIntegrationResourcePoliciesComponent|TestPersonalGitLabScopedPolicyComponent|TestIntegrationHealthSnapshotComponent|TestManagedMCPStartupRecoveryComponent|TestManagedMCPHealthReadbackComponent)$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --kubernetes-effect-acl-owner-development ]]; then
  volume_owner=true
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^TestKubernetes(EffectACLDeadline|NonstreamCredit|SharedIdentity|IdentityHealth|AuthorityFailure|ReadinessACL)OwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --ssh-identity-owner-development ]]; then
  ssh_gateway_consumer_required=true
  volume_owner=true
  # Рабочие выборки SSH используют общий mailbox guard из049.
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^(TestSSHIdentitySetupOwnerComponent|TestSSHIdentityUnreportedObserveOwnerComponent|TestSSHIdentityAuthorityFailureOwnerComponent|TestSSHIdentityRevocationOwnerComponent|TestSSHIdentitySchemaContractOwnerComponent|TestSSHExecution(RevokeContinuation|ReceiptScope|AuthorityDeadline)?OwnerComponent)$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --ssh-integration-pending-root-owner-development ]]; then
  volume_owner=true
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^TestIntegrationPendingRootOwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --ssh-receipt-scope-owner-development ]]; then
  volume_owner=true
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^TestSSHExecutionReceiptScopeOwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --ssh-execution-deadline-owner-development ]]; then
  volume_owner=true
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^TestSSHExecutionAuthorityDeadlineOwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --ssh-identity-revoke-owner-development ]]; then
  volume_owner=true
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^TestSSHIdentityRevocationOwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --shared-integration-health-development ]]; then
  volume_owner=true
  # Общая выборка проверок использует mailbox guard, введённый схемой049.
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^TestSharedIntegrationHealthOwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --telegram-notifications-owner ]]; then
  volume_owner=true
  upgrade_test='^TestTelegramNotificationsOwnerSeed$'
  owner_test='^TestTelegramSubscriptionOwnerComponent$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --personal-email-owner-development ]]; then
  # Только функциональная разработка owner path. Этот профиль не заменяет
  # закрытые full-schema proofs049 и не объявляет predecessor047/048 готовыми.
  volume_owner=true
  owner_go_arguments=(-race)
  # Полный профиль с двумя проверками истечения после COMMIT занял 119 с.
  owner_timeout=150s
  upgrade_test='^TestPersonalEmailOwnerDevelopmentSeed$'
  owner_test='^(TestPersonalEmailMailboxOwnerDevelopmentComponent|TestEmailReceiptDisclosureDeadlineComponent)$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --authority-proof-counter-owner-regression ]]; then
  volume_owner=true
  upgrade_test='^TestAuthorityProofCounterOwnerRegressionSeed$'
  owner_test='^(TestAuthorityProofCounterOwnerConcurrencyComponent|TestRoleImageOwnerTransactionRetryComponent|TestRoleImageOwnerTransactionRetryBoundsComponent)$'
  test_pattern=--review-qa-upgrade
fi
# Явная disposable draft034 regression не расширяет штатный release/schema verifier.
if [[ "$test_pattern" == --personal-credentials-assistant-regression ]]; then
  volume_owner=true
  owner_test='^(TestSystemAssistantIntegrationGrantsComponent|TestProjectAssistantIntegrationGrantsComponent|TestAssistantSessionResumeComponent)$'
  upgrade_test='^TestPersonalIntegrationCredentialMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --personal-credentials-invocation-regression ]]; then
  volume_owner=true
  owner_test='^(TestIntegrationAutonomousGitHubInvocationComponent|TestScopedIntegrationApprovalComponent|TestIntegrationGateFeedbackComponent|TestIntegrationGitHubHistoricalAdmissionComponent)$'
  upgrade_test='^TestPersonalIntegrationCredentialMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --personal-credentials-shared-regression ]]; then
  volume_owner=true
  owner_test='^TestBootstrapComponent$'
  upgrade_test='^TestPersonalIntegrationCredentialMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --personal-credentials-upgrade ]]; then
  volume_owner=true
  owner_test='^TestPersonalIntegrationCredentialsComponent$'
  upgrade_test='^TestPersonalIntegrationCredentialMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --pdlc-owner || "$test_pattern" == --pdlc-owner-legacy ]]; then
  volume_owner=true
  owner_test='^TestWorkflowPDLCFile(Intent|Result)OwnerComponent$'
  if [[ "$test_pattern" == --pdlc-owner-legacy ]]; then
    owner_test='^(TestWorkflowEffectRejoinComponent|TestWorkflowReviewQAEvidenceComponent)$/^(known-success|current-grant-revoked|same-node-new-generation|same-business-continues|review-effect-first|review-model-first)$'
  fi
  upgrade_test='^TestWorkflowContentPageMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --pdlc-functions ]]; then
  upgrade_test='^TestWorkflowPDLCMigrationFunctions$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --pdlc-upgrade ]]; then
  upgrade_test='^TestWorkflowPDLCForwardUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --content-page-upgrade ]]; then
  upgrade_test='^TestWorkflowContentPageMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --platform-oidc-upgrade ]]; then
  upgrade_test='^TestPlatformOIDCIdentityMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --github-pr-upgrade ]]; then
  upgrade_test='^(TestGitHubAutonomousForwardUpgrade|TestGitHubPRForwardUpgrade)$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --github-autonomous-upgrade ]]; then
  upgrade_test='^TestGitHubAutonomousForwardUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-backup-upgrade ]]; then
  upgrade_test='^TestWorkspaceBackupDefaultACLUpgrade$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --restore-owner ]]; then
  volume_owner=true
  upgrade_test='^TestWorkspaceRestoreOwnerMigration$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --volume-owner ]]; then
  volume_owner=true
  upgrade_test='^TestWorkspaceVolumeMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --volume-predecessor ]]; then
  upgrade_test='^TestWorkspaceVolumeMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --checkpoint-predecessor ]]; then
  upgrade_test='^TestWorkspaceCheckpointMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi
if [[ "$test_pattern" == --workspace-predecessor ]]; then
  upgrade_test='^TestSessionWorkspaceMigrationPredecessor$'
  test_pattern=--review-qa-upgrade
fi

# Новый upgrade-контур принимает только созданную ниже пустую disposable БД.
if [[ "$test_pattern" == --review-qa-upgrade ]]; then
  for key in KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE; do
    [[ ! -v "$key" ]] || fail "external fixture configuration is forbidden: $key"
  done
  unset PGPASSWORD PGSERVICE PGSERVICEFILE PGOPTIONS PGHOST PGPORT PGUSER PGDATABASE
  export PGPASSFILE=/dev/null
fi
if [[ "$storage_mode" == ram ]]; then
  for key in KODEX_CONTROL_PLANE_TEST_DSN KODEX_CONTROL_PLANE_TEST_ADMIN_DSN; do
    [[ ! -v "$key" ]] || fail "external fixture configuration is forbidden: $key"
  done
  unset PGPASSWORD PGSERVICE PGSERVICEFILE PGOPTIONS PGHOST PGPORT PGUSER PGDATABASE
  export PGPASSFILE=/dev/null
fi

cleanup() {
  if [[ -n "$kubernetes_plan_consumer_directory" ]]; then rm -rf -- "$kubernetes_plan_consumer_directory"; fi
  if [[ -n "$ssh_gateway_consumer_directory" ]]; then rm -rf -- "$ssh_gateway_consumer_directory"; fi
  if [[ "$test_pattern" == --review-qa-upgrade || "$storage_mode" == ram ]]; then
    # Новый контур никогда не очищает лишь совпавшее имя после неизвестного create.
    if [[ "$upgrade_container_id" =~ ^[a-f0-9]{64}$ ]]; then
      docker stop --time 5 "$upgrade_container_id" >/dev/null 2>&1 || true
      # stop может вернуться до завершения AutoRemove; проверяем exact ID через
      # успешную авторитетную выборку, а не считаем ошибку inspect отсутствием.
      local cleanup_ids
      for _ in {1..40}; do
        cleanup_ids=$(docker ps -a --no-trunc --filter "id=$upgrade_container_id" --format '{{.ID}}') ||
          fail 'disposable PostgreSQL cleanup readback failed'
        [[ -n "$cleanup_ids" ]] || return 0
        [[ "$cleanup_ids" == "$upgrade_container_id" ]] || fail 'disposable PostgreSQL cleanup identity changed'
        sleep 0.25
      done
      fail 'disposable PostgreSQL cleanup absence is not confirmed'
    fi
    return
  fi
  docker stop --time 5 "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
if [[ "$ssh_gateway_consumer_required" == true ]]; then
  [[ ! -v KODEX_SSH_GATEWAY_NATIVE_CONSUMER_BINARY && ! -v KODEX_SSH_GATEWAY_NATIVE_CONSUMER_SHA256 ]] || fail 'external SSH native consumer configuration is forbidden'
  ssh_gateway_consumer_directory=$(mktemp -d "${TMPDIR:-/tmp}/kodex-ssh-gateway-consumer.XXXXXXXX")
  chmod 0700 "$ssh_gateway_consumer_directory"
  # Exact gateway consumer собирается до PostgreSQL и не получает DSN либо key bytes.
  (cd "$repository_root/services/external/integration-gateway" && timeout 180s go test -race -p 1 -c -o "$ssh_gateway_consumer_directory/consumer.test" ./internal/integration)
  chmod 0500 "$ssh_gateway_consumer_directory/consumer.test"
  export KODEX_SSH_GATEWAY_NATIVE_CONSUMER_BINARY="$ssh_gateway_consumer_directory/consumer.test"
  ssh_gateway_consumer_digest=$(sha256sum "$ssh_gateway_consumer_directory/consumer.test")
  export KODEX_SSH_GATEWAY_NATIVE_CONSUMER_SHA256="${ssh_gateway_consumer_digest%% *}"
fi
if [[ "$kubernetes_plan_consumer_required" == true ]]; then
    kubernetes_plan_consumer_directory=$(mktemp -d "${TMPDIR:-/tmp}/kodex-kubernetes-plan-consumer.XXXXXXXX")
    chmod 0700 "$kubernetes_plan_consumer_directory"
    # Все три exact binaries собираются до Docker; PG budget принадлежит только seed/run.
    (cd "$repository_root/services/internal/secret-broker" && timeout 180s go test -race -p 1 -c -o "$kubernetes_plan_consumer_directory/consumer.test" ./internal/controlplane)
    (cd "$repository_root/services/internal/control-plane" && env -u GOFLAGS GOENV=off GOWORK=off timeout 240s go test -p 1 -c -o "$kubernetes_plan_consumer_directory/seed.test" ./cmd/cli)
    (cd "$repository_root/services/internal/control-plane" && env -u GOFLAGS GOENV=off GOWORK=off timeout 360s go test -race -p 1 -c -o "$kubernetes_plan_consumer_directory/owner.test" ./internal/repository/postgres/platform)
    (cd "$repository_root/services/internal/control-plane/internal/repository/postgres/platform" && timeout 20s "$kubernetes_plan_consumer_directory/owner.test" -test.v -test.count=1 -test.timeout=20s -test.run='^Test(KubernetesAssignedCapability|IntegrationCapabilityAuthoritySeparates|KubernetesStreamHistory)')
    export KODEX_KUBERNETES_NATIVE_PLAN_CONSUMER_BINARY="$kubernetes_plan_consumer_directory/consumer.test"
fi

command -v docker >/dev/null 2>&1 || fail 'docker is required'
command -v pg_isready >/dev/null 2>&1 || fail 'pg_isready is required'
command -v psql >/dev/null 2>&1 || fail 'psql is required'

# Host-вариант нужен на локальных Linux-стендах, где Docker DNAT закрыт
# политикой хоста. PostgreSQL слушает только loopback; firewall не изменяется.
network_mode=${KODEX_TEST_POSTGRES_NETWORK:-bridge}
network_arguments=(-p 127.0.0.1::5432)
postgres_arguments=()
case "$network_mode" in
  bridge) ;;
  host)
    command -v node >/dev/null 2>&1 || fail 'node is required for host network fixture'
    [[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
       "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
       "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'host fixture requires local Linux Docker'
    port=$(node --input-type=module -e '
      import {createServer} from "node:net";
      const server = createServer();
      server.on("error", () => process.exit(1));
      server.listen(0, "127.0.0.1", () => {
        process.stdout.write(String(server.address().port));
        server.close();
      });
    ') || fail 'host fixture port allocation failed'
    [[ "$port" =~ ^[0-9]+$ && "$port" -ge 1024 && "$port" -le 65535 ]] || fail 'host fixture port is invalid'
    network_arguments=(--network host)
    postgres_arguments=(postgres -c listen_addresses=127.0.0.1 -p "$port")
    ;;
  *) fail 'test PostgreSQL network must be bridge or host' ;;
esac

storage_arguments=()
if [[ "$storage_mode" == ram ]]; then
  command -v jq >/dev/null 2>&1 || fail 'jq is required for RAM fixture readback'
  [[ ( -z "${DOCKER_HOST:-}" || "$DOCKER_HOST" == unix:///* ) &&
     "$(docker context inspect --format '{{.Endpoints.docker.Host}}')" == unix:///* &&
     "$(docker info --format '{{.OSType}}')" == linux ]] || fail 'RAM fixture requires local Linux Docker'
  # Только уже установленный exact image; данные и WAL не попадают в anonymous volume.
  cached_image_id=$(docker image inspect --format '{{.Id}}' "$postgres_image") || fail 'cached PostgreSQL image is required for RAM fixture'
  [[ "$cached_image_id" =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'cached PostgreSQL identity is invalid'
  storage_arguments=(--pull never --read-only --memory 1073741824 --memory-swap 1073741824
    --cpus 1 --pids-limit 128 --log-driver none
    --tmpfs '/var/lib/postgresql:rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70'
    --tmpfs '/var/run/postgresql:rw,noexec,nosuid,nodev,size=16777216,mode=0775'
    --tmpfs '/tmp:rw,noexec,nosuid,nodev,size=16777216,mode=1777')
  if (( ${#postgres_arguments[@]} == 0 )); then postgres_arguments=(postgres); fi
  postgres_arguments+=(-c shared_buffers=32MB -c max_wal_size=64MB -c min_wal_size=32MB -c checkpoint_timeout=30s)
fi

# У rootless Docker выбранный автоматически порт иногда успевает занять другой
# локальный процесс до того, как RootlessKit создаст listener. Повторяем только
# этот отказ до запуска теста; уже созданную БД и тестовые эффекты не повторяем.
for attempt in 1 2 3 4; do
  if docker_run_output=$(docker run --rm -d --name "$container_name" \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    "${network_arguments[@]}" \
    "${storage_arguments[@]}" \
    "$postgres_image" \
    "${postgres_arguments[@]}" \
    2>&1); then
    break
  fi
  if [[ "$storage_mode" == ram ]]; then
    # Не повторяем неизвестный Create и не очищаем контейнер лишь по совпавшему имени.
    fail 'RAM fixture start is unconfirmed; no retry performed'
  fi
  if [[ "$docker_run_output" != *"RootlessKit PortManager.AddPort()"* ||
        "$docker_run_output" != *"bind: address already in use"* ]]; then
    fail "docker run: $docker_run_output"
  fi
  if (( attempt == 4 )); then
    fail 'disposable PostgreSQL port allocation exhausted'
  fi
  sleep 1
done

if [[ "$test_pattern" == --review-qa-upgrade || "$storage_mode" == ram ]]; then
  [[ "$docker_run_output" =~ ^[a-f0-9]{64}$ ]] || fail 'disposable container identity is unconfirmed'
  upgrade_container_id="$docker_run_output"
fi

if [[ "$storage_mode" == ram ]]; then
  # Проверяем actual cgroup и mounts до bootstrap, не полагаясь лишь на argv Docker.
  memory_max=$(docker exec "$upgrade_container_id" cat /sys/fs/cgroup/memory.max) || fail 'RAM fixture memory readback failed'
  cpu_max=$(docker exec "$upgrade_container_id" cat /sys/fs/cgroup/cpu.max) || fail 'RAM fixture CPU readback failed'
  filesystem=$(docker exec "$upgrade_container_id" stat -f -c %T /var/lib/postgresql) || fail 'RAM fixture filesystem readback failed'
  data_owner=$(docker exec "$upgrade_container_id" stat -c %u:%g:%a /var/lib/postgresql) || fail 'RAM fixture data owner readback failed'
  [[ "$memory_max" == 1073741824 && "$cpu_max" == '100000 100000' && "$filesystem" == tmpfs && "$data_owner" == 70:70:700 ]] || fail 'RAM fixture effective boundary differs'
  boundary=$(docker inspect --format '{"id":{{json .Id}},"image":{{json .Image}},"readonly":{{json .HostConfig.ReadonlyRootfs}},"memory":{{json .HostConfig.Memory}},"swap":{{json .HostConfig.MemorySwap}},"cpu":{{json .HostConfig.NanoCpus}},"pids":{{json .HostConfig.PidsLimit}},"tmpfs":{{json .HostConfig.Tmpfs}},"mounts":{{json .Mounts}}}' "$upgrade_container_id") || fail 'RAM fixture mount readback failed'
  jq -e --arg id "$upgrade_container_id" --arg image "$cached_image_id" '
    .id == $id and .image == $image and .readonly == true and
    .memory == 1073741824 and .swap == 1073741824 and .cpu == 1000000000 and .pids == 128 and
    (.tmpfs | keys == ["/tmp", "/var/lib/postgresql", "/var/run/postgresql"]) and
    .tmpfs["/var/lib/postgresql"] == "rw,noexec,nosuid,nodev,size=536870912,mode=0700,uid=70,gid=70" and
    .tmpfs["/var/run/postgresql"] == "rw,noexec,nosuid,nodev,size=16777216,mode=0775" and
    .tmpfs["/tmp"] == "rw,noexec,nosuid,nodev,size=16777216,mode=1777" and
    all(.mounts[]; .Type == "tmpfs")
  ' <<<"$boundary" >/dev/null || fail 'RAM fixture mount boundary differs'
  printf 'Disposable PostgreSQL RAM boundary verified\n'
fi

if [[ "$network_mode" == bridge ]]; then
  port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container_name")
fi
[[ "$port" =~ ^[0-9]+$ ]] || fail 'disposable PostgreSQL port is invalid'
for _ in $(seq 1 30); do
  if pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
pg_isready -h 127.0.0.1 -p "$port" -U postgres -t 1 >/dev/null 2>&1 ||
  fail 'disposable PostgreSQL did not become ready'

admin_dsn="postgresql://postgres@127.0.0.1:${port}/postgres?sslmode=disable"
psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 --file \
  "$repository_root/deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql" \
  >/dev/null
dsn="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane?sslmode=disable"
runtime_dsn="postgresql://control_plane_runtime_g1@127.0.0.1:${port}/control_plane?sslmode=disable"
run_migration() {
  CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE=<(printf '%s' "$dsn") \
    env -u GOFLAGS GOENV=off GOWORK=off go run -p "$go_parallelism" ./cmd/cli "$@"
}

if [[ "$test_pattern" == --review-qa-upgrade ]]; then
  (
    if [[ "$kubernetes_plan_consumer_required" == true ]]; then
      cd -- "$repository_root/services/internal/control-plane/cmd/cli"
      KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN="$dsn" \
        KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE=dedicated-loopback-container \
        timeout "$upgrade_timeout" "$kubernetes_plan_consumer_directory/seed.test" -test.v -test.count=1 -test.timeout="$upgrade_timeout" -test.run="$upgrade_test"
    else
      cd -- "$repository_root/services/internal/control-plane"
      KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN="$dsn" \
        KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE=dedicated-loopback-container \
        env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -v -count=1 -timeout="$upgrade_timeout" \
        ./cmd/cli -run "$upgrade_test"
    fi
  )
  if [[ "$volume_owner" == true ]]; then
    # Native upgrade проверен выше; release-verifiers прежних схем не расширяются.
    psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
      -c 'CREATE DATABASE control_plane_component_template WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
    (
      if [[ "$kubernetes_plan_consumer_required" == true ]]; then
        cd -- "$repository_root/services/internal/control-plane/internal/repository/postgres/platform"
        KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
          KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
          KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
          timeout "$owner_process_timeout" "$kubernetes_plan_consumer_directory/owner.test" -test.v -test.count=1 -test.timeout="$owner_timeout" -test.run="$owner_test"
      else
        cd -- "$repository_root/services/internal/control-plane"
        KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
          KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
          KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
          env -u GOFLAGS GOENV=off GOWORK=off go test "${owner_go_arguments[@]}" -p "$go_parallelism" -v -count=1 -timeout="$owner_timeout" \
          ./internal/repository/postgres/platform -run "$owner_test"
      fi
    )
  fi
  exit
fi

(
  cd -- "$repository_root/services/internal/control-plane"
  if [[ -n "${KODEX_CONTROL_PLANE_TEST_FILTER:-}" ]]; then
    psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
      -c 'CREATE DATABASE control_plane_scheduler_upgrade WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
    # Копируем ещё пустую canonical bootstrap-БД: Goose table/sequence принадлежат
    # owner, migrator получает только точные grants. NOINHERIT не заменяет это.
    psql "postgresql://postgres@127.0.0.1:${port}/control_plane_scheduler_upgrade?sslmode=disable" --no-password -v ON_ERROR_STOP=1 \
      -c 'REVOKE CONNECT ON DATABASE control_plane_scheduler_upgrade FROM PUBLIC; GRANT CONNECT, TEMPORARY ON DATABASE control_plane_scheduler_upgrade TO control_plane_migrator; REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE, CREATE ON SCHEMA public TO control_plane_owner, control_plane_migrator' >/dev/null
    KODEX_CONTROL_PLANE_MIGRATION_TEST_DSN="postgresql://control_plane_migrator@127.0.0.1:${port}/control_plane_scheduler_upgrade?sslmode=disable" \
      KODEX_CONTROL_PLANE_MIGRATION_TEST_DISPOSABLE=dedicated-loopback-container \
      env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -count=1 -timeout=90s ./cmd/cli -run '^TestScheduleProtocolUpgrade$'
  fi
  run_migration up
  run_migration status >/dev/null
  run_migration up
  # Каждый target проверяется собственным закрытым verifier.010 не принимает011;
  # неизвестная схема не получает обхода либо fallback к старому профилю.
  schema_version=$(psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" --no-password -X -qAt -v ON_ERROR_STOP=1 \
    -c 'SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version')
  if [[ "$ssh_current_schema_required" == true && "$schema_version" != 20261121000200 ]]; then
    fail 'current SSH owner profile requires schema 051'
  fi
  case "$schema_version" in
    20261124000100|20261125000100|20261125000200)
      schema_proof=workflow-file-provenance-migration-postcondition.sql
      schema_verifier=validateCurrentMigrationFixture
      expected_history=$(printf '0\n'; find "$repository_root/services/internal/control-plane/cmd/cli/migrations" -maxdepth 1 -name '*.sql' -printf '%f\n' | cut -d_ -f1 | sort)
      applied_history=$(psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
        --no-password -X -qAt -v ON_ERROR_STOP=1 \
        -c "SELECT CASE WHEN is_applied THEN version_id::text ELSE 'reverted' END FROM public.goose_db_version ORDER BY id")
      [[ "$applied_history" == "$expected_history" ]] || fail 'current fixture migration history changed'
      ;;
    20261121000200) schema_proof=workflow-file-provenance-migration-postcondition.sql; schema_verifier=validWorkflowFileProvenanceMigrationPostcondition ;;
    20261114170000) schema_proof=personal-integrations-migration-postcondition.sql; schema_verifier=validPersonalIntegrationsMigrationPostcondition ;;
    20261114140000) schema_proof=telegram-notifications-migration-postcondition.sql; schema_verifier=validTelegramNotificationsMigrationPostcondition ;;
    20261114130000) schema_proof=sentry-observability-migration-postcondition.sql; schema_verifier=validSentryObservabilityMigrationPostcondition ;;
    20261114120000) schema_proof=github-repository-scope-migration-postcondition.sql; schema_verifier=validGitHubRepositoryScopeMigrationPostcondition ;;
    20261113000100) schema_proof=workspace-unbound-stop-migration-postcondition.sql; schema_verifier=validWorkspaceUnboundStopMigrationPostcondition ;;
    20261112000100) schema_proof=workspace-archive-abi-migration-postcondition.sql; schema_verifier=validWorkspaceArchiveABIMigrationPostcondition ;;
    20261111000200) schema_proof=opencode-native-sessions-migration-postcondition.sql; schema_verifier=validOpenCodeNativeMigrationPostcondition ;;
    20261111000100) schema_proof=workflow-workspace-migration-postcondition.sql; schema_verifier=validWorkflowWorkspaceMigrationPostcondition ;;
    20261110000200) schema_proof=managed-workspace-migration-postcondition.sql; schema_verifier=validManagedWorkspaceMigrationPostcondition ;;
    20261110000100) schema_proof=artifact-optional-antivirus-migration-postcondition.sql; schema_verifier=validArtifactOptionalAntivirusMigrationPostcondition ;;
    20261107000100) schema_proof=jira-create-fields-migration-postcondition.sql; schema_verifier=validJiraCreateFieldsMigrationPostcondition ;;
    20261109000100) schema_proof=integration-resource-policies-migration-postcondition.sql; schema_verifier=validIntegrationResourcePoliciesMigrationPostcondition ;;
    20261108000100) schema_proof=authority-proof-counter-migration-postcondition.sql; schema_verifier=validAuthorityProofCounterMigrationPostcondition ;;
    20261105000100) schema_proof=platform-oidc-identity-migration-postcondition.sql; schema_verifier=validPlatformOIDCIdentityMigrationPostcondition ;;
    20261106000100) schema_proof=personal-credentials-migration-postcondition.sql; schema_verifier=validPersonalCredentialsMigrationPostcondition ;;
    20261030000100) schema_proof=workflow-content-page-migration-postcondition.sql; schema_verifier=validWorkflowContentPageMigrationPostcondition ;;
    20261029000300) schema_proof=workflow-pdlc-migration-postcondition.sql; schema_verifier=validWorkflowPDLCMigrationPostcondition ;;
    20261029000200) schema_proof=github-pr-migration-postcondition.sql; schema_verifier=validGitHubPRMigrationPostcondition ;;
    20261029000100) schema_proof=github-autonomous-migration-postcondition.sql; schema_verifier=validGitHubAutonomousMigrationPostcondition ;;
    20261028000100) schema_proof=workspace-restore-migration-postcondition.sql; schema_verifier=validWorkspaceRestoreMigrationPostcondition ;;
    20261027000100)
      # Одинаковая конечная Goose version не отличает010+SSH от полного017+SSH.
      # Выбираем отдельный closed verifier по фактически применённому017;
      # прежняя SSH-проверка не принимает новые таблицы либо урезанную проекцию.
      volume_migration_count=$(psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" --no-password -X -qAt -v ON_ERROR_STOP=1 \
        -c 'SELECT count(*) FROM public.goose_db_version WHERE version_id=20261026001700 AND is_applied')
      case "$volume_migration_count" in
        0) schema_proof=ssh-compose-migration-postcondition.sql; schema_verifier=validSSHComposeMigrationPostcondition ;;
        1) schema_proof=workspace-volume-ssh-migration-postcondition.sql; schema_verifier=validWorkspaceVolumeSSHMigrationPostcondition ;;
        *) fail 'workspace volume migration history is ambiguous' ;;
      esac ;;
    20261026001600) schema_proof=workspace-checkpoint-migration-postcondition.sql; schema_verifier=validWorkspaceCheckpointMigrationPostcondition ;;
    20261026001500) schema_proof=session-workspace-migration-postcondition.sql; schema_verifier=validSessionWorkspaceMigrationPostcondition ;;
    20261026001000) schema_proof=server-workflow-migration-postcondition.sql; schema_verifier=validServerWorkflowMigrationPostcondition ;;
    20261026001400) schema_proof=workflow-review-qa-evidence-migration-postcondition.sql; schema_verifier=validWorkflowReviewQAEvidenceMigrationPostcondition ;;
    20261026001100) schema_proof=legacy-workflow-rework-migration-postcondition.sql; schema_verifier=validLegacyWorkflowReworkMigrationPostcondition ;;
    20261026001200) schema_proof=workflow-exception-context-migration-postcondition.sql; schema_verifier=validWorkflowExceptionContextMigrationPostcondition ;;
    20261026001300) schema_proof=workflow-upstream-rework-migration-postcondition.sql; schema_verifier=validWorkflowUpstreamReworkMigrationPostcondition ;;
    *) fail 'unsupported current workflow schema target' ;;
  esac
  psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
    --no-password -X -qAt -v ON_ERROR_STOP=1 \
    --file "$repository_root/tools/release/$schema_proof" | \
    node --input-type=module -e '
      const {pathToFileURL} = await import("node:url");
      const verifiers = await import(pathToFileURL(process.argv[1]));
      let input = "";
      for await (const chunk of process.stdin) input += chunk;
      const proof = JSON.parse(input);
      // Фикстуры новой установки с отдельными точными схемами.
      const fixtureCatalogs = {
        20261124000100: "aa75f206a40fb6f7fe333798b7e90eb434cb8a8b79fd1c9a3425bf14fdede0ad",
        20261125000100: "a04adb5753bf6aba49a6f3320261edc388f5d43737cd745f0f7e4bdaac086c3e",
        20261125000200: "574c1da3cbcbf9be071397c68f642ab098547e8521d206bf10bc38a6c516eed4",
      };
      const currentFixture = process.argv[2] === "validateCurrentMigrationFixture";
      const valid = currentFixture
        ? proof.database === "control_plane" && proof.readOnly === "on" &&
          proof.gooseVersion === Number(process.argv[3]) && proof.serverVersionNum === 180003 &&
          proof.backupQualification === "NONE" && proof.publicSequenceSafe === true &&
          proof.schemaCatalog?.catalogDigest === fixtureCatalogs[proof.gooseVersion]
        : Object.hasOwn(verifiers, process.argv[2]) && verifiers[process.argv[2]](proof);
      if (!valid) throw new Error(`Current workflow schema postcondition failed for ${proof.gooseVersion} (${proof.schemaCatalog?.catalogDigest ?? "no catalog"})`);
      process.stdout.write("Current workflow schema read-only query passed\n");
    ' "$repository_root/tools/release/control-plane-immutable-migration.mjs" "$schema_verifier" "$schema_version"
  retention_reference_count=$(psql "$runtime_dsn" --no-password -X -qAt -v ON_ERROR_STOP=1 \
    -c "SELECT control_plane.skill_artifact_reference_count('00000000-0000-4000-8000-000000000001'::uuid,'art_component',1,'sha256:' || repeat('0',64))")
  [[ "$retention_reference_count" == "0" ]] || fail 'artifact retention trigger dependency is unavailable'
  # Новые самостоятельные suites получают отдельную копию ещё пустой схемы,
  # чтобы terminal fixtures одного сценария не загрязняли bootstrap другого.
  # Эти координаты существуют только внутри disposable контейнера этой оснастки.
  psql "$admin_dsn" --no-password -v ON_ERROR_STOP=1 \
    -c 'CREATE DATABASE control_plane_component_template WITH TEMPLATE control_plane OWNER control_plane_owner' >/dev/null
  # Полный последовательный baseline вырабатывает прежние 15m до поздних
  # сценариев. Бюджет полного прогона отдельный от deadlines отдельных тестов.
  component_timeout=15m
  if [[ $# -eq 0 && -z ${KODEX_CONTROL_PLANE_TEST_FILTER:-} ]]; then
    component_timeout=20m
  fi
  KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
    KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
    KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
    env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -v -count=1 -timeout="$component_timeout" \
      ./internal/repository/postgres/platform -run "$test_pattern"
  # Та же read-only диагностика, которую release использует на staging.
  psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
    --no-password -X -qAt -v ON_ERROR_STOP=1 \
    --file "$repository_root/tools/release/worker-grant-readback.sql" | \
    node --input-type=module -e '
      let input = "";
      for await (const chunk of process.stdin) input += chunk;
      const state = JSON.parse(input);
      if (!Number.isFinite(Date.parse(state.at)) || !Array.isArray(state.floors) || !Array.isArray(state.instances))
        throw new Error("Worker grant readback shape is invalid");
      process.stdout.write("Worker grant read-only query passed\n");
    '
  psql "postgresql://postgres@127.0.0.1:${port}/control_plane?sslmode=disable" \
    --no-password -X -qAt -v ON_ERROR_STOP=1 \
    --file "$repository_root/tools/release/runner-policy-readback.sql" | \
    node --input-type=module -e '
      let input = "";
      for await (const chunk of process.stdin) input += chunk;
      const state = JSON.parse(input);
      if (!Number.isFinite(Date.parse(state.at)) ||
          !["openBuilds", "pendingAdmissions", "pendingPromotions", "activeRuntimeRuns", "claimedRuntimeLeases", "promotedArtifactCount"]
            .every((key) => Number.isSafeInteger(state[key]) && state[key] >= 0) ||
          !/^[a-f0-9]{64}$/.test(state.promotedPinsSHA256))
        throw new Error("Runner policy readback shape is invalid");
      process.stdout.write("Runner policy read-only query passed\n");
    '
  if [[ $# -eq 0 && -z "${KODEX_CONTROL_PLANE_TEST_FILTER:-}" ]]; then
    KODEX_CONTROL_PLANE_TEST_DSN="$runtime_dsn" \
      KODEX_CONTROL_PLANE_TEST_ADMIN_DSN="$admin_dsn" \
      KODEX_CONTROL_PLANE_TEST_TEMPLATE_DATABASE=control_plane_component_template \
      env -u GOFLAGS GOENV=off GOWORK=off go test -p "$go_parallelism" -count=1 \
        ./internal/repository/postgres/platform -run '^TestAvatarLifecycleComponent$'
  fi
)

printf 'Control-plane PostgreSQL tests passed\n'
