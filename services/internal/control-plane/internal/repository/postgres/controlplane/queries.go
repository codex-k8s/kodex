package controlplane

import _ "embed"

//go:embed sql/mattermost_event_keyset__admit.sql
var sqlMattermostEventKeysetAdmit string

//go:embed sql/continuation_grant_keyset__admit.sql
var sqlContinuationGrantKeysetAdmit string

//go:embed sql/clock__get.sql
var sqlClockGet string

// Каждая декларация связывает конкретный именованный запрос с бинарём на
// этапе компиляции. Удалённый или переименованный SQL-файл поэтому завершает
// сборку ошибкой, а не вызывает панику уже запущенного сервиса.

//go:embed sql/audit__append.sql
var sqlAuditAppend string

//go:embed sql/audit__list.sql
var sqlAuditList string

//go:embed sql/cache_epoch__bump.sql
var sqlCacheEpochBump string

//go:embed sql/cache_epoch__get.sql
var sqlCacheEpochGet string

//go:embed sql/diagnostics__get.sql
var sqlDiagnosticsGet string

//go:embed sql/memory__search.sql
var sqlMemorySearch string

//go:embed sql/memory_projection__upsert.sql
var sqlMemoryProjectionUpsert string

//go:embed sql/image_build__next.sql
var sqlImageBuildNext string

//go:embed sql/image_admission__next.sql
var sqlImageAdmissionNext string

//go:embed sql/image_promotion__next.sql
var sqlImagePromotionNext string

//go:embed sql/image_artifact__promoted_by_spec.sql
var sqlImageArtifactPromotedBySpec string

//go:embed sql/image_build__for_recipe_update.sql
var sqlImageBuildForRecipeUpdate string

//go:embed sql/image_artifact__for_recipe_update.sql
var sqlImageArtifactForRecipeUpdate string

//go:embed sql/outbox__append.sql
var sqlOutboxAppend string

//go:embed sql/outbox__check.sql
var sqlOutboxCheck string

//go:embed sql/outbox__claim.sql
var sqlOutboxClaim string

//go:embed sql/outbox__mark_failed.sql
var sqlOutboxMarkFailed string

//go:embed sql/outbox__mark_published.sql
var sqlOutboxMarkPublished string

//go:embed sql/outbox_terminal__list.sql
var sqlOutboxTerminalList string

//go:embed sql/outbox_terminal__repair.sql
var sqlOutboxTerminalRepair string

//go:embed sql/owner_gate__next_delivery.sql
var sqlOwnerGateNextDelivery string

//go:embed sql/owner_gate__by_delivery_claim_key.sql
var sqlOwnerGateByDeliveryClaimKey string

//go:embed sql/owner_gate__next_expired.sql
var sqlOwnerGateNextExpired string

//go:embed sql/permission_index__actor_list.sql
var sqlPermissionIndexActorList string

//go:embed sql/permission_index__actor_roles.sql
var sqlPermissionIndexActorRoles string

//go:embed sql/permission_index__rebuild.sql
var sqlPermissionIndexRebuild string

//go:embed sql/process__has_active_children.sql
var sqlProcessHasActiveChildren string

//go:embed sql/process__has_open_work.sql
var sqlProcessHasOpenWork string

//go:embed sql/provider_binding__active_sessions.sql
var sqlProviderBindingActiveSessions string

//go:embed sql/runtime_execution__latest_session_codex_lineage.sql
var sqlRuntimeExecutionLatestSessionCodexLineage string

//go:embed sql/project__authorize.sql
var sqlProjectAuthorize string

//go:embed sql/project__list_eligible.sql
var sqlProjectListEligible string

//go:embed sql/proof_revision__next.sql
var sqlProofRevisionNext string

//go:embed sql/readiness__check.sql
var sqlReadinessCheck string

//go:embed sql/receipt__get.sql
var sqlReceiptGet string

//go:embed sql/receipt__save.sql
var sqlReceiptSave string

//go:embed sql/resource__get.sql
var sqlResourceGet string

//go:embed sql/resource__get_including_deleted.sql
var sqlResourceGetIncludingDeleted string

//go:embed sql/resource__get_for_update.sql
var sqlResourceGetForUpdate string

//go:embed sql/resource__get_including_deleted_for_update.sql
var sqlResourceGetIncludingDeletedForUpdate string

//go:embed sql/resource__get_by_stable_key_for_update.sql
var sqlResourceGetByStableKeyForUpdate string

//go:embed sql/resource__get_by_name_for_update.sql
var sqlResourceGetByNameForUpdate string

//go:embed sql/schedule__other_session_references_for_update.sql
var sqlScheduleOtherSessionReferencesForUpdate string

//go:embed sql/project__has_live_resources.sql
var sqlProjectHasLiveResources string

//go:embed sql/resource__insert.sql
var sqlResourceInsert string

//go:embed sql/resource__list.sql
var sqlResourceList string

//go:embed sql/resource__list_tombstones.sql
var sqlResourceListTombstones string

//go:embed sql/resource__search.sql
var sqlResourceSearch string

//go:embed sql/resource__update.sql
var sqlResourceUpdate string

//go:embed sql/runtime_revision__components.sql
var sqlRuntimeRevisionComponents string

//go:embed sql/runtime_derived_resource__insert.sql
var sqlRuntimeDerivedResourceInsert string

//go:embed sql/runtime_derived_resource__get.sql
var sqlRuntimeDerivedResourceGet string

//go:embed sql/external_command_receipt__reserve.sql
var sqlExternalCommandReceiptReserve string

//go:embed sql/external_command_receipt__get.sql
var sqlExternalCommandReceiptGet string

//go:embed sql/external_command_receipt__finalize.sql
var sqlExternalCommandReceiptFinalize string

//go:embed sql/legacy_configuration_cutover__get.sql
var sqlLegacyConfigurationCutoverGet string

//go:embed sql/legacy_configuration_cutover__get_for_update.sql
var sqlLegacyConfigurationCutoverGetForUpdate string

//go:embed sql/legacy_configuration_cutover__list.sql
var sqlLegacyConfigurationCutoverList string

//go:embed sql/legacy_configuration_cutover__mark_migrated.sql
var sqlLegacyConfigurationCutoverMarkMigrated string

//go:embed sql/legacy_graph_plan__insert.sql
var sqlLegacyGraphPlanInsert string

//go:embed sql/legacy_graph_plan__get_for_update.sql
var sqlLegacyGraphPlanGetForUpdate string

//go:embed sql/legacy_graph_source__insert.sql
var sqlLegacyGraphSourceInsert string

//go:embed sql/legacy_graph_source__list.sql
var sqlLegacyGraphSourceList string

//go:embed sql/legacy_graph_operation__insert.sql
var sqlLegacyGraphOperationInsert string

//go:embed sql/legacy_graph_operation__list.sql
var sqlLegacyGraphOperationList string

//go:embed sql/legacy_graph_operation__materialize.sql
var sqlLegacyGraphOperationMaterialize string

//go:embed sql/legacy_graph_plan__terminal.sql
var sqlLegacyGraphPlanTerminal string

//go:embed sql/legacy_graph_provenance__insert.sql
var sqlLegacyGraphProvenanceInsert string

//go:embed sql/legacy_graph_provenance__projection.sql
var sqlLegacyGraphProvenanceProjection string

//go:embed sql/legacy_graph_callback_manifest__insert.sql
var sqlLegacyGraphCallbackManifestInsert string

//go:embed sql/legacy_graph_callback_delivery__insert.sql
var sqlLegacyGraphCallbackDeliveryInsert string

//go:embed sql/legacy_graph_operation__verify.sql
var sqlLegacyGraphOperationVerify string

//go:embed sql/legacy_graph_operation__custom_projection.sql
var sqlLegacyGraphOperationCustomProjection string

//go:embed sql/legacy_graph_turn_attempt__insert.sql
var sqlLegacyGraphTurnAttemptInsert string

//go:embed sql/runtime_revision__latest.sql
var sqlRuntimeRevisionLatest string

//go:embed sql/runtime_execution__get_for_update.sql
var sqlRuntimeExecutionGetForUpdate string

//go:embed sql/runtime_execution__get_by_turn_for_update.sql
var sqlRuntimeExecutionGetByTurnForUpdate string

//go:embed sql/runtime_execution__get_by_turn.sql
var sqlRuntimeExecutionGetByTurn string

//go:embed sql/resource_retention_policy__current.sql
var sqlResourceRetentionPolicyCurrent string

//go:embed sql/resource_retention_policy__get_for_update.sql
var sqlResourceRetentionPolicyGetForUpdate string

//go:embed sql/resource_retention_policy__get_version_for_update.sql
var sqlResourceRetentionPolicyGetVersionForUpdate string

//go:embed sql/resource_retention_policy__insert.sql
var sqlResourceRetentionPolicyInsert string

//go:embed sql/resource_retention_policy__retire.sql
var sqlResourceRetentionPolicyRetire string

//go:embed sql/runtime_retention_hold__active_for_update.sql
var sqlRuntimeRetentionHoldActiveForUpdate string

//go:embed sql/runtime_retention_hold__get_for_update.sql
var sqlRuntimeRetentionHoldGetForUpdate string

//go:embed sql/runtime_retention_hold__insert.sql
var sqlRuntimeRetentionHoldInsert string

//go:embed sql/runtime_retention_hold__release.sql
var sqlRuntimeRetentionHoldRelease string

//go:embed sql/runtime_execution__session_has_live.sql
var sqlRuntimeExecutionSessionHasLive string

//go:embed sql/runtime_execution__session_has_unverified_archive.sql
var sqlRuntimeExecutionSessionHasUnverifiedArchive string

//go:embed sql/runtime_execution__session_has_active_cleanup.sql
var sqlRuntimeExecutionSessionHasActiveCleanup string

//go:embed sql/runtime_execution__latest_session_archive_for_restore.sql
var sqlRuntimeExecutionLatestSessionArchiveForRestore string

//go:embed sql/runtime_execution__insert.sql
var sqlRuntimeExecutionInsert string

//go:embed sql/runtime_execution__update.sql
var sqlRuntimeExecutionUpdate string

//go:embed sql/runtime_execution__next_expired.sql
var sqlRuntimeExecutionNextExpired string

//go:embed sql/runtime_backup__list.sql
var sqlRuntimeBackupList string

//go:embed sql/runtime_backup__get.sql
var sqlRuntimeBackupGet string

//go:embed sql/runtime_restore_operation__insert.sql
var sqlRuntimeRestoreOperationInsert string

//go:embed sql/runtime_restore_operation__get.sql
var sqlRuntimeRestoreOperationGet string

//go:embed sql/runtime_restore_operation__get_by_backup.sql
var sqlRuntimeRestoreOperationGetByBackup string

//go:embed sql/runtime_restore_operation__owner_get.sql
var sqlRuntimeRestoreOperationOwnerGet string

//go:embed sql/runtime_restore_operation__owner_list.sql
var sqlRuntimeRestoreOperationOwnerList string

//go:embed sql/runtime_restore_operation__advance.sql
var sqlRuntimeRestoreOperationAdvance string

//go:embed sql/runtime_restore_operation__consume.sql
var sqlRuntimeRestoreOperationConsume string

//go:embed sql/runtime_restore_operation__revoke.sql
var sqlRuntimeRestoreOperationRevoke string

//go:embed sql/runtime_restore_effect__authorize.sql
var sqlRuntimeRestoreEffectAuthorize string

//go:embed sql/runtime_incident__insert.sql
var sqlRuntimeIncidentInsert string

//go:embed sql/runtime_incident__list.sql
var sqlRuntimeIncidentList string

//go:embed sql/runtime_incident__get_for_update.sql
var sqlRuntimeIncidentGetForUpdate string

//go:embed sql/runtime_incident__owner_get.sql
var sqlRuntimeIncidentOwnerGet string

//go:embed sql/runtime_incident__update.sql
var sqlRuntimeIncidentUpdate string

//go:embed sql/runtime_incident_history__insert.sql
var sqlRuntimeIncidentHistoryInsert string

//go:embed sql/runtime_incident_history__record.sql
var sqlRuntimeIncidentHistoryRecord string

//go:embed sql/runtime_incident_history__list.sql
var sqlRuntimeIncidentHistoryList string

//go:embed sql/interaction_delivery__workspace_open_for_update.sql
var sqlInteractionDeliveryWorkspaceOpenForUpdate string

//go:embed sql/audit__run_timeline.sql
var sqlAuditRunTimeline string

//go:embed sql/run_graph__nodes.sql
var sqlRunGraphNodes string

//go:embed sql/run_graph__artifacts.sql
var sqlRunGraphArtifacts string

//go:embed sql/project__owner_list_for_update.sql
var sqlProjectOwnerListForUpdate string

//go:embed sql/session__historical_owner_list_for_update.sql
var sqlSessionHistoricalOwnerListForUpdate string

//go:embed sql/transaction__switch_workspace_scope.sql
var sqlTransactionSwitchWorkspaceScope string

//go:embed sql/workspace_recovery__next_candidate.sql
var sqlWorkspaceRecoveryNextCandidate string

//go:embed sql/workspace_recovery__readiness.sql
var sqlWorkspaceRecoveryReadiness string

//go:embed sql/protected_history__insert.sql
var sqlProtectedHistoryInsert string

//go:embed sql/protected_history__list.sql
var sqlProtectedHistoryList string

//go:embed sql/protected_history__get_version.sql
var sqlProtectedHistoryGetVersion string

//go:embed sql/instruction_history__get_content_version.sql
var sqlInstructionHistoryGetContentVersion string

//go:embed sql/owner_session__admit.sql
var sqlOwnerSessionAdmit string

//go:embed sql/owner_session__require.sql
var sqlOwnerSessionRequire string

//go:embed sql/owner_session__revoke.sql
var sqlOwnerSessionRevoke string

//go:embed sql/gateway_public_tls__prepare.sql
var sqlGatewayPublicTLSPrepare string

//go:embed sql/gateway_public_tls__confirm.sql
var sqlGatewayPublicTLSConfirm string

//go:embed sql/gateway_public_tls__check.sql
var sqlGatewayPublicTLSCheck string

//go:embed sql/integration_continuation__get_for_update.sql
var sqlIntegrationContinuationGetForUpdate string

//go:embed sql/integration_continuation__get.sql
var sqlIntegrationContinuationGet string

//go:embed sql/integration_continuation__get_by_continuation_turn.sql
var sqlIntegrationContinuationGetByContinuationTurn string

//go:embed sql/integration_continuation__next_expired.sql
var sqlIntegrationContinuationNextExpired string

//go:embed sql/integration_continuation__blocks_cleanup.sql
var sqlIntegrationContinuationBlocksCleanup string

//go:embed sql/integration_continuation__insert.sql
var sqlIntegrationContinuationInsert string

//go:embed sql/integration_continuation__update.sql
var sqlIntegrationContinuationUpdate string

//go:embed sql/schedule__due.sql
var sqlScheduleDue string

//go:embed sql/automation_scheduler_project__next.sql
var sqlAutomationSchedulerProjectNext string

//go:embed sql/schedule_occurrence__get.sql
var sqlScheduleOccurrenceGet string

//go:embed sql/schedule_occurrence__get_for_update.sql
var sqlScheduleOccurrenceGetForUpdate string

//go:embed sql/schedule_occurrence__get_by_current_turn.sql
var sqlScheduleOccurrenceGetByCurrentTurn string

//go:embed sql/schedule_occurrence__get_by_claim_key.sql
var sqlScheduleOccurrenceGetByClaimKey string

//go:embed sql/schedule_occurrence__has_open.sql
var sqlScheduleOccurrenceHasOpen string

//go:embed sql/schedule_occurrence__has_blocking_execution.sql
var sqlScheduleOccurrenceHasBlockingExecution string

//go:embed sql/schedule_occurrence__list.sql
var sqlScheduleOccurrenceList string

//go:embed sql/schedule_occurrence__next.sql
var sqlScheduleOccurrenceNext string

//go:embed sql/schedule_occurrence__lock_expired.sql
var sqlScheduleOccurrenceExpiredCandidates string

//go:embed sql/schedule_occurrence__save.sql
var sqlScheduleOccurrenceSave string

//go:embed sql/schedule_occurrence__skip_overlap.sql
var sqlScheduleOccurrenceSkipOverlap string

//go:embed sql/schedule_occurrence__update.sql
var sqlScheduleOccurrenceUpdate string

//go:embed sql/schedule_capability__insert.sql
var sqlScheduleCapabilityInsert string

//go:embed sql/schedule_capability__get_for_update.sql
var sqlScheduleCapabilityGetForUpdate string

//go:embed sql/schedule_capability__get_by_occurrence_for_update.sql
var sqlScheduleCapabilityGetByOccurrenceForUpdate string

//go:embed sql/schedule_capability__update.sql
var sqlScheduleCapabilityUpdate string

//go:embed sql/scheduled_run__save.sql
var sqlScheduledRunSave string

//go:embed sql/scheduled_run__get_for_update.sql
var sqlScheduledRunGetForUpdate string

//go:embed sql/scheduled_run__get_by_current_turn_for_update.sql
var sqlScheduledRunGetByCurrentTurnForUpdate string

//go:embed sql/scheduled_run__wait_owner.sql
var sqlScheduledRunWaitOwner string

//go:embed sql/scheduled_run__suspend_external.sql
var sqlScheduledRunSuspendExternal string

//go:embed sql/scheduled_run__continue.sql
var sqlScheduledRunContinue string

//go:embed sql/scheduled_run__rebind.sql
var sqlScheduledRunRebind string

//go:embed sql/scheduled_run__finish.sql
var sqlScheduledRunFinish string

//go:embed sql/session__open_turns.sql
var sqlSessionOpenTurns string

//go:embed sql/session__blocks_runtime_cleanup.sql
var sqlSessionBlocksRuntimeCleanup string

//go:embed sql/schedule_session__project_fence.sql
var sqlScheduleSessionProjectFence string

//go:embed sql/schedule_session__conversation_for_update.sql
var sqlScheduleSessionConversationForUpdate string

//go:embed sql/instruction_object_readiness__fence.sql
var sqlInstructionObjectReadinessFence string

//go:embed sql/transaction__set_scope.sql
var sqlTransactionSetScope string

//go:embed sql/turn__expired_claimed.sql
var sqlTurnExpiredClaimed string

//go:embed sql/turn__next_queued.sql
var sqlTurnNextQueued string

//go:embed sql/turn_attempt__finish.sql
var sqlTurnAttemptFinish string

//go:embed sql/turn_attempt__get_for_update.sql
var sqlTurnAttemptGetForUpdate string

//go:embed sql/turn_attempt__save.sql
var sqlTurnAttemptSave string

//go:embed sql/interaction_delivery__enqueue.sql
var sqlInteractionDeliveryEnqueue string

//go:embed sql/interaction_delivery__claim.sql
var sqlInteractionDeliveryClaim string

//go:embed sql/interaction_delivery__complete.sql
var sqlInteractionDeliveryComplete string

//go:embed sql/interaction_delivery_readback__insert.sql
var sqlInteractionDeliveryReadbackInsert string

//go:embed sql/interaction_delivery_readback__validate.sql
var sqlInteractionDeliveryReadbackValidate string

//go:embed sql/interaction_delivery_readback_keyset__admit.sql
var sqlInteractionDeliveryReadbackKeysetAdmit string

//go:embed sql/turn_lease__delete.sql
var sqlTurnLeaseDelete string

//go:embed sql/turn_lease__get_for_update.sql
var sqlTurnLeaseGetForUpdate string

//go:embed sql/turn_lease__renew.sql
var sqlTurnLeaseRenew string

//go:embed sql/turn_lease__save.sql
var sqlTurnLeaseSave string

//go:embed sql/turn_lease__validate.sql
var sqlTurnLeaseValidate string

//go:embed sql/delegation_edge__save.sql
var sqlDelegationEdgeSave string

//go:embed sql/delegation_edge__get_by_target_turn.sql
var sqlDelegationEdgeGetByTargetTurn string

//go:embed sql/provider_pool__next_slot.sql
var sqlProviderPoolNextSlot string

//go:embed sql/work_claim__active_for_update.sql
var sqlWorkClaimActiveForUpdate string

//go:embed sql/process_turn__active_candidates.sql
var sqlProcessTurnActiveCandidates string

//go:embed sql/owner_gate__active_by_process.sql
var sqlOwnerGateActiveByProcess string
