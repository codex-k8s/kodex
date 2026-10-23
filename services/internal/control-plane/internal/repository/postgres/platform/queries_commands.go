package platform

import _ "embed"

var (
	// queryCommandsGetrunforgraphSelectRunsOrganizationIdRef embeds the tenant/run-ref lookup of run, session/root/parent/retry and target metadata with ordered artifact/gate ref arrays. It reads a graph-facing snapshot without locking or independently establishing the caller's visibility.
	// queryCommandsGetrunforgraphSelectRunsOrganizationIdRef содержит tenant/run-ref чтение metadata запуска, session/root/parent/retry и target с упорядоченными массивами artifact/gate refs. Читает snapshot для графа без блокировки и самостоятельного установления видимости caller.
	//go:embed sql/commands_getrunforgraph_select_runs_organization_id_ref.sql
	queryCommandsGetrunforgraphSelectRunsOrganizationIdRef string
	// queryCommandsExecuteLockIdempotencyScope embeds a transaction advisory lock on the 64-bit hash of four delimiter-separated scope strings. It serializes matching scope attempts but does not itself compare intents or create a durable idempotency receipt; hash collisions can add contention.
	// queryCommandsExecuteLockIdempotencyScope содержит transaction advisory lock по 64-битному hash четырёх scope strings с разделителем. Сериализует попытки одной области, но сам не сравнивает intents и не создаёт durable idempotency receipt; hash collisions могут добавить ожидание.
	//go:embed sql/commands_execute_lock_idempotency_scope.sql
	queryCommandsExecuteLockIdempotencyScope string
	// queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation embeds the exact organization/actor/operation/key receipt lookup, requiring unexpired server-clock time and locking the row. Returned intent digest and response payload still require caller-side replay consistency handling.
	// queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation содержит точный organization/actor/operation/key поиск receipt с неистёкшим server-clock сроком и блокировкой строки. Возвращённые intent digest и response payload требуют обработки согласованности replay вызывающим кодом.
	//go:embed sql/commands_execute_select_idempotency_receipts_organization_id_actor_id_operation.sql
	queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation string
	// queryCommandsExecuteInsertAuditEventsRefProjectIdAction embeds insertion of an audit event with fixed SUCCEEDED outcome and supplied tenant/project/actor/resource/correlation metadata. Executing and committing it atomically with the business command belongs to the owner transaction, not this embedded string.
	// queryCommandsExecuteInsertAuditEventsRefProjectIdAction содержит вставку audit event с фиксированным SUCCEEDED и заданными tenant/project/actor/resource/correlation metadata. Атомарное исполнение и commit вместе с business command принадлежат owner transaction, не этой embedded string.
	//go:embed sql/commands_execute_insert_audit_events_ref_project_id_action.sql
	queryCommandsExecuteInsertAuditEventsRefProjectIdAction string
	// queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest embeds durable receipt insertion for the exact organization/actor/operation/key, intent and typed response, with expiry 24 hours from the database clock. It does not implement retries or resolve conflicting existing receipts by itself.
	// queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest содержит вставку durable receipt точных organization/actor/operation/key, intent и typed response с expiry через 24 часа по часам БД. Самостоятельно retry и разрешение конфликтующих receipts не реализует.
	//go:embed sql/commands_execute_insert_idempotency_receipts_organization_id_operation_intent_digest.sql
	queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest string
	// queryCommandsCompleteonboardingUpdateInstallationOnboardingCompletedAt embeds the singleton installation update that preserves an existing onboarding completion timestamp via COALESCE. It changes no project setup data and contains no authorization check; the command owner controls when it executes.
	// queryCommandsCompleteonboardingUpdateInstallationOnboardingCompletedAt содержит singleton installation update с сохранением существующего времени завершения onboarding через COALESCE. Данные настройки проекта не меняет и authorization check не содержит; момент исполнения контролирует владелец команды.
	//go:embed sql/commands_completeonboarding_update_installation_onboarding_completed_at.sql
	queryCommandsCompleteonboardingUpdateInstallationOnboardingCompletedAt string
	// queryCommandsCreateprojectInsertProjectsRefNameLanguage embeds project insertion with the supplied organization and creator, returning public metadata and database-assigned lifecycle/version/times. Caller authority and creation audit/idempotency are separate owner-transaction responsibilities.
	// queryCommandsCreateprojectInsertProjectsRefNameLanguage содержит вставку проекта с заданными organization и creator, возвращает public metadata и назначенные БД lifecycle/version/time. Caller authority, creation audit и idempotency отдельно обеспечиваются owner transaction.
	//go:embed sql/commands_createproject_insert_projects_ref_name_language.sql
	queryCommandsCreateprojectInsertProjectsRefNameLanguage string
	// queryCommandsCreateprojectInsertMembershipsRefProjectIdRole embeds the project-membership stored-function call after resolving the project inside the supplied organization, using the same subject as member and actor. It forwards requested permissions rather than granting authority merely by loading this SQL text.
	// queryCommandsCreateprojectInsertMembershipsRefProjectIdRole содержит вызов stored function проектного участия после разрешения проекта внутри заданной organization, с одним subject как member и actor. Передаёт requested permissions, а не выдаёт полномочия загрузкой SQL text.
	//go:embed sql/commands_createproject_insert_memberships_ref_project_id_role.sql
	queryCommandsCreateprojectInsertMembershipsRefProjectIdRole string
	// queryCommandsUpdateprojectUpdateProjectsNamePurposeLanguage embeds a tenant/ref/version-fenced update of ACTIVE project name, purpose and language, incrementing version and database update time. No match represents a failed eligibility/OCC predicate for the caller to classify; it is not an unconditional write.
	// queryCommandsUpdateprojectUpdateProjectsNamePurposeLanguage содержит tenant/ref/version-fenced изменение имени, цели и языка ACTIVE проекта с увеличением версии и времени БД. Отсутствие строки означает невыполнение eligibility/OCC predicate для классификации вызывающим кодом; это не безусловная запись.
	//go:embed sql/commands_updateproject_update_projects_name_purpose_language.sql
	queryCommandsUpdateprojectUpdateProjectsNamePurposeLanguage string
	// queryProjectTrashLock embeds the exact organization/project-ref metadata read with FOR UPDATE, including deletion and retention times. It acquires the project row lock but does not itself require a lifecycle, version or actor permission.
	// queryProjectTrashLock содержит точное organization/project-ref чтение metadata с FOR UPDATE, включая время удаления и хранения. Берёт row lock проекта, но сам не требует конкретные lifecycle, version и actor permission.
	//go:embed sql/project_trash__lock.sql
	queryProjectTrashLock string
	// queryProjectTrashActiveRunRoots embeds one active run ref/version per root in the exact tenant/project, preferring an active root row over its children. The selected ref may be a child when the root is no longer active; the query does not cancel any run itself.
	// queryProjectTrashActiveRunRoots содержит выбор одной active run ref/version на root в точных tenant/project с приоритетом active root перед детьми. Ref может указывать на ребёнка, если root уже не active; сам запрос запуски не отменяет.
	//go:embed sql/project_trash__active_run_roots.sql
	queryProjectTrashActiveRunRoots string
	// queryProjectTrashCancelOccurrences embeds cancellation of DUE/CLAIMED/RETRY_WAIT occurrences belonging to the exact tenant/project schedules, clearing lease/fence/workload pins and advancing versions. It returns affected occurrence IDs; the broader run/grant/event transition belongs to the owner transaction.
	// queryProjectTrashCancelOccurrences содержит отмену DUE/CLAIMED/RETRY_WAIT occurrences расписаний точных tenant/project с очисткой lease/fence/workload pins и увеличением версий. Возвращает затронутые occurrence IDs; полный run/grant/event transition принадлежит owner transaction.
	//go:embed sql/project_trash__cancel_occurrences.sql
	queryProjectTrashCancelOccurrences string
	// queryProjectTrashMark embeds the ACTIVE-to-TRASHED transition fenced by tenant, project ID and expected version, recording actor and a 30-day retention window using statement time. It updates project metadata only, not the complete cancellation graph or physical storage cleanup.
	// queryProjectTrashMark содержит ACTIVE-to-TRASHED переход с tenant/project ID/expected version fence, actor и 30-дневным сроком хранения по statement time. Меняет только project metadata, не полный cancellation graph и не физическую очистку.
	//go:embed sql/project_trash__mark.sql
	queryProjectTrashMark string
	// queryProjectTrashRestore embeds the version-fenced TRASHED-to-ACTIVE transition only before purge_after, clearing deletion metadata and advancing version/time. It does not restart cancelled runs, recreate grants or restore already physically purged rows.
	// queryProjectTrashRestore содержит version-fenced TRASHED-to-ACTIVE только до purge_after с очисткой deletion metadata и увеличением версии/времени. Отменённые запуски не возобновляет, grants не пересоздаёт и физически удалённые строки не восстанавливает.
	//go:embed sql/project_trash__restore.sql
	queryProjectTrashRestore string
	// queryProjectTrashPurge embeds the tenant/project/version-fenced TRASHED-to-PURGE_PENDING transition and returns metadata. This marks a request for later cleanup, not a completed purge or deletion of the project's graph.
	// queryProjectTrashPurge содержит tenant/project/version-fenced TRASHED-to-PURGE_PENDING и возвращает metadata. Отмечает запрос последующей очистки, не завершённый purge и не удаление графа проекта.
	//go:embed sql/project_trash__purge.sql
	queryProjectTrashPurge string
	// queryProjectTrashPurgeReceipt embeds a PENDING purge receipt with exact project, organization, public ref and deleting actor pins. Receipt creation must share the owner's transition transaction; it does not prove inventory cleanup or terminal completion.
	// queryProjectTrashPurgeReceipt содержит PENDING purge receipt с точными project, organization, public ref и deleting actor pins. Создание receipt должно входить в owner transition transaction; очистку inventory и terminal completion не доказывает.
	//go:embed sql/project_trash__purge_receipt.sql
	queryProjectTrashPurgeReceipt string
	// queryProjectPurgeCancelReadyArchiveTasks embeds cancellation of only READY archive tasks for the exact tenant/project with a purge-requested code and completion/update times. Claimed or running archive work is not changed by this statement and needs its own lifecycle handling.
	// queryProjectPurgeCancelReadyArchiveTasks содержит отмену только READY archive tasks точных tenant/project с purge-requested code и completion/update time. Claimed и running архивирование этим statement не меняется и требует собственной lifecycle обработки.
	//go:embed sql/project_purge__cancel_ready_archive_tasks.sql
	queryProjectPurgeCancelReadyArchiveTasks string
	// queryProjectTrashList embeds the tenant trash/purge-pending catalog with deleted-time/ref keyset ordering, bounded page, resource counts and pending owner-content indicator. Actor eligibility is outside this query; the pending indicator is not permission to recover or approve content.
	// queryProjectTrashList содержит tenant каталог trash/purge-pending с deleted-time/ref keyset, ограниченной страницей, resource counts и признаком pending owner-content. Actor eligibility находится вне запроса; индикатор не разрешает recovery и подтверждение content.
	//go:embed sql/project_trash__list.sql
	queryProjectTrashList string
	// queryProjectMembershipInsert embeds membership creation for an active verified-OIDC subject with an active platform membership, delegating binding creation to the stored function. It returns presentation metadata and requested permissions; actor grant eligibility is not inferred from those returned fields.
	// queryProjectMembershipInsert содержит создание участия active verified-OIDC subject с active platform membership, делегирует binding creation stored function. Возвращает presentation metadata и requested permissions; actor grant eligibility из этих полей не выводится.
	//go:embed sql/project_membership__insert.sql
	queryProjectMembershipInsert string
	// queryProjectMembershipResolveForUpdate embeds exact tenant/project/membership resolution through the PROJECT_MEMBERSHIP binding and role version, locking project and binding rows. It returns subject/platform-role/activity and projected permissions for separate policy/OCC checks, not an automatic authorization verdict.
	// queryProjectMembershipResolveForUpdate содержит точное tenant/project/membership разрешение через PROJECT_MEMBERSHIP binding и role version с блокировкой project и binding rows. Возвращает subject/platform-role/activity и projected permissions для отдельных policy/OCC checks, не автоматический authorization verdict.
	//go:embed sql/project_membership__resolve_for_update.sql
	queryProjectMembershipResolveForUpdate string
	// queryProjectMembershipCanGrant embeds the supplied owner/admin role shortcut or an active project-membership permission-subset check. The role and actor parameters must come from verified owner context; evaluating this parameterized predicate is not itself proof of caller identity.
	// queryProjectMembershipCanGrant содержит shortcut заданной owner/admin role либо permission-subset check active project membership. Role и actor параметры должны происходить из verified owner context; вычисление параметризованного predicate само по себе caller identity не доказывает.
	//go:embed sql/project_membership__can_grant.sql
	queryProjectMembershipCanGrant string
	// queryProjectMembershipUpdate embeds the stored-function update of an exact project membership with expected version, permissions, active flag and actor, then maps its returned binding version/state. It delegates mutation semantics to that function rather than directly rewriting a broad membership set.
	// queryProjectMembershipUpdate содержит stored-function update точного project membership с expected version, permissions, active flag и actor, затем проецирует binding version/state. Делегирует mutation semantics функции, а не напрямую переписывает широкий набор memberships.
	//go:embed sql/project_membership__update.sql
	queryProjectMembershipUpdate string
	// queryProjectMembershipDeactivate embeds an ACTIVE-to-REVOKED update of the exact tenant/project membership binding with OCC and version advancement, returning projected permissions and state. It neither checks last-owner rules nor revokes unrelated bindings by itself.
	// queryProjectMembershipDeactivate содержит ACTIVE-to-REVOKED update точного tenant/project membership binding с OCC и увеличением версии, возвращает projected permissions и state. Сам last-owner rules не проверяет и посторонние bindings не отзывает.
	//go:embed sql/project_membership__deactivate.sql
	queryProjectMembershipDeactivate string
	// queryPlatformMembershipInsert embeds organization-wide binding creation for an active verified-OIDC subject with no existing platform presentation and a current ACTIVE SYSTEM role. Actor authorization and last-owner constraints are separate from this selected-role insertion statement.
	// queryPlatformMembershipInsert содержит создание organization binding active verified-OIDC subject без существующего platform presentation с текущей ACTIVE SYSTEM role. Actor authorization и last-owner constraints отделены от этого statement вставки выбранной роли.
	//go:embed sql/platform_membership__insert.sql
	queryPlatformMembershipInsert string
	// queryPlatformMembershipResolveForUpdate embeds exact organization/platform-binding lookup for a verified-OIDC subject and assigned SYSTEM role, locking organization and binding. It returns current activity/role/version metadata for caller policy checks rather than requiring every returned object to be active itself.
	// queryPlatformMembershipResolveForUpdate содержит точный organization/platform-binding lookup verified-OIDC subject и назначенной SYSTEM role с блокировкой organization и binding. Возвращает activity/role/version metadata для caller policy checks, не требует активности каждого возвращённого объекта сам.
	//go:embed sql/platform_membership__resolve_for_update.sql
	queryPlatformMembershipResolveForUpdate string
	// queryPlatformMembershipCountOtherActiveOwners embeds the count of other ACTIVE SYSTEM OWNER platform bindings within their database-clock validity windows, excluding the selected binding ID. It supports a last-owner check but does not lock or validate every subject's effective eligibility by itself.
	// queryPlatformMembershipCountOtherActiveOwners содержит подсчёт остальных ACTIVE SYSTEM OWNER platform bindings в database-clock validity windows с исключением выбранного binding ID. Поддерживает last-owner check, но сам не блокирует и не проверяет effective eligibility каждого subject.
	//go:embed sql/platform_membership__count_other_active_owners.sql
	queryPlatformMembershipCountOtherActiveOwners string
	// queryPlatformMembershipUpdate embeds an OCC-fenced platform binding role/state update using the current ACTIVE SYSTEM role selected by stable key. It sets ACTIVE or REVOKED from the supplied flag and advances version; authority and preservation of another owner must be checked by the command.
	// queryPlatformMembershipUpdate содержит OCC-fenced update роли/состояния platform binding с текущей ACTIVE SYSTEM role по stable key. Задаёт ACTIVE или REVOKED из флага и увеличивает версию; полномочия и сохранение другого owner проверяет команда.
	//go:embed sql/platform_membership__update.sql
	queryPlatformMembershipUpdate string
	// queryPlatformMembershipDeactivate embeds version-fenced revocation of an ACTIVE platform binding and returns its SYSTEM role presentation after the update. It targets one tenant binding and does not independently enforce the last-active-owner invariant.
	// queryPlatformMembershipDeactivate содержит version-fenced отзыв ACTIVE platform binding с возвратом SYSTEM role presentation после update. Нацелен на один tenant binding и самостоятельно last-active-owner invariant не обеспечивает.
	//go:embed sql/platform_membership__deactivate.sql
	queryPlatformMembershipDeactivate string
	// queryPlatformMembershipDeactivateProjects embeds batch revocation and version advancement of all ACTIVE PROJECT_MEMBERSHIP presentations for one tenant subject. It uses no per-binding expected version and affects neither other subjects nor non-project presentation kinds; the enclosing owner transaction supplies the lifecycle fence.
	// queryPlatformMembershipDeactivateProjects содержит batch отзыв и увеличение версии всех ACTIVE PROJECT_MEMBERSHIP presentations одного tenant subject. Не использует per-binding expected version, не затрагивает других subjects и непроектные presentation kinds; lifecycle fence задаёт enclosing owner transaction.
	//go:embed sql/platform_membership__deactivate_projects.sql
	queryPlatformMembershipDeactivateProjects string
	//go:embed sql/commands_createagent_insert_agents_ref_project_id_purpose.sql
	queryCommandsCreateagentInsertAgentsRefProjectIdPurpose string
	//go:embed sql/commands_createagent_select_role_definitions_organization_id_project_id_ref.sql
	queryCommandsCreateagentSelectRoleDefinitionsOrganizationIdProjectIdRef string
	//go:embed sql/commands_createagent_insert_role_definitions_ref_organization_id_project_id_name.sql
	queryCommandsCreateagentInsertRoleDefinitionsRefOrganizationIdProjectIdName string
	//go:embed sql/commands_createagent_insert_instruction_versions_ref_agent_id_state.sql
	queryCommandsCreateagentInsertInstructionVersionsRefAgentIdState string
	//go:embed sql/commands_resolve_enabled_runtime_profile.sql
	queryCommandsResolveEnabledRuntimeProfile string
	//go:embed sql/commands_mustprojectid_select_projects_organization_id_ref_lifecycle.sql
	queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle string
	//go:embed sql/commands_changeagent_update_agents_name_purpose_role_description.sql
	queryCommandsChangeagentUpdateAgentsNamePurposeRoleDescription string
	//go:embed sql/commands_changeagent_enable_agent.sql
	queryCommandsChangeagentEnableAgent string
	//go:embed sql/commands_changeagent_archive_agent.sql
	queryCommandsChangeagentArchiveAgent string
	//go:embed sql/commands_changeagent_select_agents_ref.sql
	queryCommandsChangeagentSelectAgentsRef string
	//go:embed sql/commands_suspend_target_schedules.sql
	queryCommandsSuspendTargetSchedules string
	//go:embed sql/commands_changeinstructions_select_agents_organization_id_ref.sql
	queryCommandsChangeinstructionsSelectAgentsOrganizationIdRef string
	//go:embed sql/commands_changeinstructions_select_next_draft_version.sql
	queryCommandsChangeinstructionsSelectNextDraftVersion string
	//go:embed sql/commands_changeinstructions_update_current_draft.sql
	queryCommandsChangeinstructionsUpdateCurrentDraft string
	//go:embed sql/commands_changeinstructions_insert_draft_version.sql
	queryCommandsChangeinstructionsInsertDraftVersion string
	//go:embed sql/commands_changeinstructions_select_current_draft.sql
	queryCommandsChangeinstructionsSelectCurrentDraft string
	//go:embed sql/commands_changeinstructions_update_instruction_versions_state_validation_problems.sql
	queryCommandsChangeinstructionsUpdateInstructionVersionsStateValidationProblems string
	//go:embed sql/commands_changeinstructions_update_instruction_versions_state_published_at.sql
	queryCommandsChangeinstructionsUpdateInstructionVersionsStatePublishedAt string
	//go:embed sql/commands_changeinstructions_select_instruction_versions_agent_id_ref_state.sql
	queryCommandsChangeinstructionsSelectInstructionVersionsAgentIdRefState string
	//go:embed sql/commands_changeinstructions_select_next_rollback_version.sql
	queryCommandsChangeinstructionsSelectNextRollbackVersion string
	//go:embed sql/commands_changeinstructions_insert_rollback_version.sql
	queryCommandsChangeinstructionsInsertRollbackVersion string
	//go:embed sql/commands_changeinstructions_update_agents_version_updated_at.sql
	queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt string
	//go:embed sql/commands_changeagentbinding_select_agents_organization_id_ref.sql
	queryCommandsChangeagentbindingSelectAgentsOrganizationIdRef string
	//go:embed sql/commands_changeagentbinding_select_enabled_capability.sql
	queryCommandsChangeagentbindingSelectEnabledCapability string
	//go:embed sql/commands_changeagentbinding_actor_can_grant.sql
	queryCommandsChangeagentbindingActorCanGrant string
	//go:embed sql/commands_changeagentbinding_enable_integration_grant.sql
	queryCommandsChangeagentbindingEnableIntegrationGrant string
	//go:embed sql/commands_changeagentbinding_revoke_integration_grant.sql
	queryCommandsChangeagentbindingRevokeIntegrationGrant string
	//go:embed sql/commands_changeagentbinding_append_capability.sql
	queryCommandsChangeagentbindingAppendCapability string
	//go:embed sql/commands_changeagentbinding_remove_capability.sql
	queryCommandsChangeagentbindingRemoveCapability string
	//go:embed sql/commands_changeagentbinding_update_agents_version_updated_at.sql
	queryCommandsChangeagentbindingUpdateAgentsVersionUpdatedAt string
	//go:embed sql/commands_changeagentbinding_select_agent_readback.sql
	queryCommandsChangeagentbindingSelectAgentReadback string
	//go:embed sql/commands_changeworkflow_insert_workflows_ref_project_id_purpose.sql
	queryCommandsChangeworkflowInsertWorkflowsRefProjectIdPurpose string
	//go:embed sql/commands_changeworkflow_select_workflows_organization_id_ref.sql
	queryCommandsChangeworkflowSelectWorkflowsOrganizationIdRef string
	//go:embed sql/commands_changeworkflow_update_workflows_draft_spec_state_version.sql
	queryCommandsChangeworkflowUpdateWorkflowsDraftSpecStateVersion string
	//go:embed sql/commands_changeworkflow_select_draft_for_validation.sql
	queryCommandsChangeworkflowSelectDraftForValidation string
	//go:embed sql/commands_changeworkflow_mark_workflow_valid.sql
	queryCommandsChangeworkflowMarkWorkflowValid string
	//go:embed sql/commands_changeworkflow_select_draft_for_publish.sql
	queryCommandsChangeworkflowSelectDraftForPublish string
	//go:embed sql/commands_changeworkflow_insert_workflow_versions_ref_workflow_id_spec.sql
	queryCommandsChangeworkflowInsertWorkflowVersionsRefWorkflowIdSpec string
	//go:embed sql/commands_changeworkflow_update_workflows_published_spec_published_version_state.sql
	queryCommandsChangeworkflowUpdateWorkflowsPublishedSpecPublishedVersionState string
	//go:embed sql/commands_changeworkflow_archive_workflow.sql
	queryCommandsChangeworkflowArchiveWorkflow string
	//go:embed sql/commands_changeworkflow_select_authoritative_readback.sql
	queryCommandsChangeworkflowSelectAuthoritativeReadback string
	//go:embed sql/commands_changeworkflow_validate_agent_capabilities.sql
	queryCommandsChangeworkflowValidateAgentCapabilities string
	//go:embed sql/commands_launchrun_select_agents_organization_id_project_id_ref.sql
	queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef string
	//go:embed sql/commands_launchrun_select_workflows_organization_id_project_id_ref.sql
	queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef string
	//go:embed sql/commands_launchrun_validate_agent_runtime_contract.sql
	queryCommandsLaunchrunValidateAgentRuntimeContract string
	//go:embed sql/commands_launchrun_insert_sessions_ref_project_id_target_ref.sql
	queryCommandsLaunchrunInsertSessionsRefProjectIdTargetRef string
	//go:embed sql/commands_launchrun_select_sessions_organization_id_project_id_ref.sql
	queryCommandsLaunchrunSelectSessionsOrganizationIdProjectIdRef string
	//go:embed sql/commands_launchrun_validate_input_artifacts.sql
	queryCommandsLaunchrunValidateInputArtifacts string
	//go:embed sql/commands_launchrun_insert_runs_ref_project_id_target_type.sql
	queryCommandsLaunchrunInsertRunsRefProjectIdTargetType string
	//go:embed sql/commands_launchrun_update_runs_root_run_id.sql
	queryCommandsLaunchrunUpdateRunsRootRunId string
	//go:embed sql/commands_launchrun_insert_session_turns_ref_session_id_turn_number.sql
	queryCommandsLaunchrunInsertSessionTurnsRefSessionIdTurnNumber string
	//go:embed sql/commands_launchrun_update_sessions_next_turn_number_version_updated_at.sql
	queryCommandsLaunchrunUpdateSessionsNextTurnNumberVersionUpdatedAt string
	//go:embed sql/commands_launchrun_insert_run_nodes_ref_root_run_id_type.sql
	queryCommandsLaunchrunInsertRunNodesRefRootRunIdType string
	//go:embed sql/commands_launchrun_update_run_nodes_workflow_step_key_human_gate_after.sql
	queryCommandsLaunchrunUpdateRunNodesWorkflowStepKeyHumanGateAfter string
	//go:embed sql/commands_launchrun_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsLaunchrunInsertRunEdgesRefRootRunIdTargetNodeId string
	//go:embed sql/commands_launchrun_update_runs_state_started_at_version.sql
	queryCommandsLaunchrunUpdateRunsStateStartedAtVersion string
	//go:embed sql/commands_insertagentnode_select_agents_organization_id_ref_state.sql
	queryCommandsInsertagentnodeSelectAgentsOrganizationIdRefState string
	//go:embed sql/commands_insertagentnode_insert_run_nodes_ref_root_run_id_parent_node_id.sql
	queryCommandsInsertagentnodeInsertRunNodesRefRootRunIdParentNodeId string
	//go:embed sql/commands_insertagentnode_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsInsertagentnodeInsertRunEdgesRefRootRunIdTargetNodeId string
	//go:embed sql/commands_launchrun_insert_planned_workflow_node.sql
	queryCommandsLaunchrunInsertPlannedWorkflowNode string
	//go:embed sql/commands_launchrun_insert_planned_workflow_edge.sql
	queryCommandsLaunchrunInsertPlannedWorkflowEdge string
	//go:embed sql/commands_emitplatformevent_update_installation_platform_sequence.sql
	queryCommandsEmitplatformeventUpdateInstallationPlatformSequence string
	//go:embed sql/commands_emitplatformevent_insert_outbox_events_event_id_ordering_key_payload.sql
	queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload string
	//go:embed sql/commands_emitrunevent_update_runs_event_sequence_graph_revision_updated_at.sql
	queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt string
	//go:embed sql/commands_emitrunevent_select_projects_id.sql
	queryCommandsEmitruneventSelectProjectsId string
	//go:embed sql/commands_emitrunevent_select_run_delta.sql
	queryCommandsEmitruneventSelectRunDelta string
	//go:embed sql/commands_emitrunevent_select_node_delta.sql
	queryCommandsEmitruneventSelectNodeDelta string
	//go:embed sql/commands_emitrunevent_select_edge_delta.sql
	queryCommandsEmitruneventSelectEdgeDelta string
	//go:embed sql/commands_emitrunevent_select_gate_delta.sql
	queryCommandsEmitruneventSelectGateDelta string
	//go:embed sql/commands_emitrunevent_select_artifact_delta.sql
	queryCommandsEmitruneventSelectArtifactDelta string
	//go:embed sql/commands_emitrunevent_insert_run_events_event_id_organization_id_root_run_id.sql
	queryCommandsEmitruneventInsertRunEventsEventIdOrganizationIdRootRunId string
	//go:embed sql/commands_emitrunevent_insert_outbox_events_event_id_ordering_key_payload.sql
	queryCommandsEmitruneventInsertOutboxEventsEventIdOrderingKeyPayload string
	//go:embed sql/commands_readrungraphtx_select_run_nodes_organization_id_root_run_id_ref.sql
	queryCommandsReadrungraphtxSelectRunNodesOrganizationIdRootRunIdRef string
	//go:embed sql/commands_readrungraphtx_select_run_edges_organization_id_root_run_id_ref.sql
	queryCommandsReadrungraphtxSelectRunEdgesOrganizationIdRootRunIdRef string
	//go:embed sql/commands_addsessionturn_select_sessions_organization_id_ref_state.sql
	queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState string
	//go:embed sql/commands_addsessionturn_select_runs_organization_id_ref.sql
	queryCommandsAddsessionturnSelectRunsOrganizationIdRef string
	//go:embed sql/commands_addsessionturn_select_runs_ref.sql
	queryCommandsAddsessionturnSelectRunsRef string
	//go:embed sql/commands_addsessionturn_select_previous_root_node.sql
	queryCommandsAddsessionturnSelectPreviousRootNode string
	//go:embed sql/commands_addsessionturn_select_continuation_root_node.sql
	queryCommandsAddsessionturnSelectContinuationRootNode string
	//go:embed sql/commands_addsessionturn_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsAddsessionturnInsertRunEdgesRefRootRunIdTargetNodeId string
	//go:embed sql/commands_changerun_select_runs_organization_id_ref.sql
	queryCommandsChangerunSelectRunsOrganizationIdRef string
	//go:embed sql/commands_changerun_select_root_run_id.sql
	queryCommandsChangerunSelectRootRunID string
	//go:embed sql/commands_changerun_lock_run_nodes.sql
	queryCommandsChangerunLockRunNodes string
	//go:embed sql/commands_changerun_update_runs_state_safe_error_code_safe_error_message.sql
	queryCommandsChangerunUpdateRunsStateSafeErrorCodeSafeErrorMessage string
	//go:embed sql/commands_changerun_update_run_nodes_state_next_actions_finished_at.sql
	queryCommandsChangerunUpdateRunNodesStateNextActionsFinishedAt string
	//go:embed sql/commands_changerun_update_runtime_leases_state_updated_at.sql
	queryCommandsChangerunUpdateRuntimeLeasesStateUpdatedAt string
	//go:embed sql/commands_changerun_update_schedule_occurrences_state_cancelled.sql
	queryCommandsChangerunUpdateScheduleOccurrencesStateCancelled string
	//go:embed sql/commands_changerun_update_session_turns_state_completed_at.sql
	queryCommandsChangerunUpdateSessionTurnsStateCompletedAt string
	//go:embed sql/commands_changerun_close_integration_effects.sql
	queryCommandsChangerunCloseIntegrationEffects string
	//go:embed sql/commands_changerun_update_owner_gates_state_decision_decision_comment.sql
	queryCommandsChangerunUpdateOwnerGatesStateDecisionDecisionComment string
	//go:embed sql/commands_changerun_select_runs_id.sql
	queryCommandsChangerunSelectRunsId string
	//go:embed sql/commands_changerun_select_runs_ref.sql
	queryCommandsChangerunSelectRunsRef string
	//go:embed sql/commands_changerun_update_runs_retry_of_run_id_attempt.sql
	queryCommandsChangerunUpdateRunsRetryOfRunIdAttempt string
	//go:embed sql/commands_changerun_select_previous_root_node.sql
	queryCommandsChangerunSelectPreviousRootNode string
	//go:embed sql/commands_changerun_select_retry_root_node.sql
	queryCommandsChangerunSelectRetryRootNode string
	//go:embed sql/commands_changerun_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId string
	//go:embed sql/commands_resolvegate_select_owner_gates_organization_id_ref_state.sql
	queryCommandsResolvegateSelectOwnerGatesOrganizationIdRefState string
	//go:embed sql/commands_resolvegate_select_scoped_integration_invocation.sql
	queryCommandsResolvegateSelectScopedIntegrationInvocation string
	//go:embed sql/commands_resolvegate_insert_integration_approval_scope.sql
	queryCommandsResolvegateInsertIntegrationApprovalScope string
	//go:embed sql/commands_resolvegate_bind_integration_approval_scope.sql
	queryCommandsResolvegateBindIntegrationApprovalScope string
	//go:embed sql/commands_resolvegate_update_owner_gates_state_decision_decision_comment.sql
	queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment string
	//go:embed sql/commands_resolvegate_update_integration_invocation.sql
	queryCommandsResolvegateUpdateIntegrationInvocation string
	//go:embed sql/commands_resolvegate_insert_change_request_turn.sql
	queryCommandsResolvegateInsertChangeRequestTurn string
	//go:embed sql/commands_resolvegate_update_change_request_session.sql
	queryCommandsResolvegateUpdateChangeRequestSession string
	//go:embed sql/commands_resolvegate_requeue_predecessor_node.sql
	queryCommandsResolvegateRequeuePredecessorNode string
	//go:embed sql/commands_resolvegate_update_run_nodes_state_finished_at_version.sql
	queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion string
	//go:embed sql/commands_resolvegate_update_root_node_state.sql
	queryCommandsResolvegateUpdateRootNodeState string
	//go:embed sql/commands_resolvegate_select_active_agent_nodes.sql
	queryCommandsResolvegateSelectActiveAgentNodes string
	//go:embed sql/commands_resolvegate_count_open_gates.sql
	queryCommandsResolvegateCountOpenGates string
	//go:embed sql/commands_resolvegate_complete_root_run.sql
	queryCommandsResolvegateCompleteRootRun string
	//go:embed sql/commands_resolvegate_update_runs_state_version_updated_at.sql
	queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt string
	//go:embed sql/commands_mustrunref_select_runs_id.sql
	queryCommandsMustrunrefSelectRunsId string
)
