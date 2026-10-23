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
	// queryCommandsCreateagentInsertAgentsRefProjectIdPurpose embeds tenant/project agent creation in READY/enabled state with role, runtime and capability metadata; creation does not prove provider/runtime readiness.
	// queryCommandsCreateagentInsertAgentsRefProjectIdPurpose содержит создание tenant/project сотрудника в READY/enabled с role, runtime и capability metadata; запись не доказывает provider/runtime readiness.
	//go:embed sql/commands_createagent_insert_agents_ref_project_id_purpose.sql
	queryCommandsCreateagentInsertAgentsRefProjectIdPurpose string
	// queryCommandsCreateagentSelectRoleDefinitionsOrganizationIdProjectIdRef embeds exact tenant/project/ref selection of an ACTIVE role definition; caller authorization is not inferred from a matching row.
	// queryCommandsCreateagentSelectRoleDefinitionsOrganizationIdProjectIdRef содержит точный tenant/project/ref выбор ACTIVE role definition; совпадение строки не устанавливает caller authorization.
	//go:embed sql/commands_createagent_select_role_definitions_organization_id_project_id_ref.sql
	queryCommandsCreateagentSelectRoleDefinitionsOrganizationIdProjectIdRef string
	// queryCommandsCreateagentInsertRoleDefinitionsRefOrganizationIdProjectIdName embeds CUSTOM project-role insertion with empty default policies and supplied creator; it does not create access bindings or grant permissions.
	// queryCommandsCreateagentInsertRoleDefinitionsRefOrganizationIdProjectIdName содержит вставку CUSTOM project-role с пустыми default policies и заданным creator; access bindings и permissions не выдаёт.
	//go:embed sql/commands_createagent_insert_role_definitions_ref_organization_id_project_id_name.sql
	queryCommandsCreateagentInsertRoleDefinitionsRefOrganizationIdProjectIdName string
	// queryCommandsCreateagentInsertInstructionVersionsRefAgentIdState embeds initial instruction version 1 as PUBLISHED with supplied content, digest, creator and publication time; digest validation belongs to callers.
	// queryCommandsCreateagentInsertInstructionVersionsRefAgentIdState содержит начальную instruction version 1 в PUBLISHED с заданными content, digest, creator и publication time; digest проверяет вызывающий код.
	//go:embed sql/commands_createagent_insert_instruction_versions_ref_agent_id_state.sql
	queryCommandsCreateagentInsertInstructionVersionsRefAgentIdState string
	// queryCommandsResolveEnabledRuntimeProfile embeds stable-key lookup restricted to enabled runtime profiles, returning provider/model/revision metadata rather than live model availability.
	// queryCommandsResolveEnabledRuntimeProfile содержит stable-key поиск только enabled runtime profiles с provider/model/revision metadata, не проверку live model availability.
	//go:embed sql/commands_resolve_enabled_runtime_profile.sql
	queryCommandsResolveEnabledRuntimeProfile string
	// queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle embeds exact tenant/ref ACTIVE-project resolution with FOR KEY SHARE; permission checks and version fencing remain outside this lookup.
	// queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle содержит точное tenant/ref разрешение ACTIVE проекта с FOR KEY SHARE; permission checks и version fence остаются вне lookup.
	//go:embed sql/commands_mustprojectid_select_projects_organization_id_ref_lifecycle.sql
	queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle string
	// queryCommandsChangeagentUpdateAgentsNamePurposeRoleDescription embeds tenant/ref/OCC metadata update of a non-system agent, preserving empty runtime/role selectors and resolving replacement roles within its active project scope.
	// queryCommandsChangeagentUpdateAgentsNamePurposeRoleDescription содержит tenant/ref/OCC metadata update несистемного сотрудника, сохраняет пустые runtime/role selectors и разрешает заменяемую ACTIVE role в его project scope.
	//go:embed sql/commands_changeagent_update_agents_name_purpose_role_description.sql
	queryCommandsChangeagentUpdateAgentsNamePurposeRoleDescription string
	// queryCommandsChangeagentEnableAgent embeds version-fenced enabled/state changes for a non-system, non-ARCHIVED agent; the supplied new state still requires command-level policy.
	// queryCommandsChangeagentEnableAgent содержит version-fenced изменение enabled/state несистемного, не ARCHIVED сотрудника; новый state требует command-level policy.
	//go:embed sql/commands_changeagent_enable_agent.sql
	queryCommandsChangeagentEnableAgent string
	// queryCommandsChangeagentArchiveAgent embeds non-system agent archival with OCC, disabling it only when no matching active run exists; broader schedule/cancellation handling is separate.
	// queryCommandsChangeagentArchiveAgent содержит архивирование несистемного сотрудника с OCC и отключением только при отсутствии matching active run; обработка расписаний и отмены отдельная.
	//go:embed sql/commands_changeagent_archive_agent.sql
	queryCommandsChangeagentArchiveAgent string
	// queryCommandsChangeagentSelectAgentsRef embeds ref-only agent readback with current runtime config, role, avatar revision and active CLEAN knowledge refs; tenant eligibility must already be resolved.
	// queryCommandsChangeagentSelectAgentsRef содержит ref-only readback сотрудника с current runtime config, role, avatar revision и active CLEAN knowledge refs; tenant eligibility должна быть разрешена заранее.
	//go:embed sql/commands_changeagent_select_agents_ref.sql
	queryCommandsChangeagentSelectAgentsRef string
	// queryCommandsSuspendTargetSchedules embeds scoped target schedule suspension and cancellation of its pending/claimed/retry/dead-letter occurrences, clearing leases; it returns suspended count/minimum ref, not execution completion.
	// queryCommandsSuspendTargetSchedules содержит scoped остановку target schedules и отмену pending/claimed/retry/dead-letter occurrences с очисткой leases; возвращает count/minimum ref, не завершение исполнения.
	//go:embed sql/commands_suspend_target_schedules.sql
	queryCommandsSuspendTargetSchedules string
	// queryCommandsChangeinstructionsSelectAgentsOrganizationIdRef embeds tenant/ref agent locking with nullable project/system metadata and version, allowing the command to distinguish system and project instruction paths.
	// queryCommandsChangeinstructionsSelectAgentsOrganizationIdRef содержит tenant/ref блокировку сотрудника с nullable project/system metadata и версией для различения системных и проектных instruction paths.
	//go:embed sql/commands_changeinstructions_select_agents_organization_id_ref.sql
	queryCommandsChangeinstructionsSelectAgentsOrganizationIdRef string
	// queryCommandsChangeinstructionsSelectNextDraftVersion embeds max-plus-one instruction numbering with zero fallback for an empty history; caller-held serialization is required to prevent concurrent allocation races.
	// queryCommandsChangeinstructionsSelectNextDraftVersion содержит max-plus-one нумерацию инструкций с zero fallback пустой истории; от concurrent allocation race защищает сериализация вызывающего кода.
	//go:embed sql/commands_changeinstructions_select_next_draft_version.sql
	queryCommandsChangeinstructionsSelectNextDraftVersion string
	// queryCommandsChangeinstructionsUpdateCurrentDraft embeds rewriting editable DRAFT/VALID/INVALID rows to DRAFT with supplied content/digest/creator and cleared diagnostics, without changing published history.
	// queryCommandsChangeinstructionsUpdateCurrentDraft содержит перезапись редактируемых DRAFT/VALID/INVALID в DRAFT с content/digest/creator и очисткой diagnostics без изменения published history.
	//go:embed sql/commands_changeinstructions_update_current_draft.sql
	queryCommandsChangeinstructionsUpdateCurrentDraft string
	// queryCommandsChangeinstructionsInsertDraftVersion embeds a supplied-number DRAFT instruction insertion; uniqueness, actor eligibility and content validation are not established by loading this SQL.
	// queryCommandsChangeinstructionsInsertDraftVersion содержит вставку DRAFT инструкции с заданным номером; загрузка SQL не устанавливает uniqueness, actor eligibility и допустимость content.
	//go:embed sql/commands_changeinstructions_insert_draft_version.sql
	queryCommandsChangeinstructionsInsertDraftVersion string
	// queryCommandsChangeinstructionsSelectCurrentDraft embeds editable instruction ref/content lookup by resolved agent ID with FOR UPDATE, excluding PUBLISHED versions.
	// queryCommandsChangeinstructionsSelectCurrentDraft содержит поиск ref/content редактируемой инструкции по разрешённому agent ID с FOR UPDATE, исключая PUBLISHED версии.
	//go:embed sql/commands_changeinstructions_select_current_draft.sql
	queryCommandsChangeinstructionsSelectCurrentDraft string
	// queryCommandsChangeinstructionsUpdateInstructionVersionsStateValidationProblems embeds ref-only assignment of instruction validation state/problems; the owning command must first resolve the exact authorized draft.
	// queryCommandsChangeinstructionsUpdateInstructionVersionsStateValidationProblems содержит ref-only назначение instruction validation state/problems; владелец команды должен заранее разрешить точный authorized draft.
	//go:embed sql/commands_changeinstructions_update_instruction_versions_state_validation_problems.sql
	queryCommandsChangeinstructionsUpdateInstructionVersionsStateValidationProblems string
	// queryCommandsChangeinstructionsUpdateInstructionVersionsStatePublishedAt embeds promotion of VALID instructions for one resolved agent to PUBLISHED with database time; binding and audit changes require the enclosing transaction.
	// queryCommandsChangeinstructionsUpdateInstructionVersionsStatePublishedAt содержит перевод VALID инструкций разрешённого сотрудника в PUBLISHED с временем БД; binding и audit меняются enclosing transaction.
	//go:embed sql/commands_changeinstructions_update_instruction_versions_state_published_at.sql
	queryCommandsChangeinstructionsUpdateInstructionVersionsStatePublishedAt string
	// queryCommandsChangeinstructionsSelectInstructionVersionsAgentIdRefState embeds exact agent/ref PUBLISHED-content lookup for rollback selection, not arbitrary draft content or actor authorization.
	// queryCommandsChangeinstructionsSelectInstructionVersionsAgentIdRefState содержит точный agent/ref поиск PUBLISHED content для rollback selection, не произвольный draft content и не actor authorization.
	//go:embed sql/commands_changeinstructions_select_instruction_versions_agent_id_ref_state.sql
	queryCommandsChangeinstructionsSelectInstructionVersionsAgentIdRefState string
	// queryCommandsChangeinstructionsSelectNextRollbackVersion embeds max-plus-one rollback numbering without an empty-history fallback; existing history and serialization are caller preconditions.
	// queryCommandsChangeinstructionsSelectNextRollbackVersion содержит max-plus-one rollback numbering без fallback пустой истории; наличие истории и сериализация — preconditions вызывающего кода.
	//go:embed sql/commands_changeinstructions_select_next_rollback_version.sql
	queryCommandsChangeinstructionsSelectNextRollbackVersion string
	// queryCommandsChangeinstructionsInsertRollbackVersion embeds a new PUBLISHED instruction version with supplied parent lineage and database publication time; it preserves rather than edits the historical source version.
	// queryCommandsChangeinstructionsInsertRollbackVersion содержит новую PUBLISHED instruction version с заданным parent lineage и временем БД; историческая исходная версия сохраняется, а не редактируется.
	//go:embed sql/commands_changeinstructions_insert_rollback_version.sql
	queryCommandsChangeinstructionsInsertRollbackVersion string
	// queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt embeds version/time advancement by an already resolved agent ID; it contains no independent tenant, expected-version or permission predicate.
	// queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt содержит увеличение версии/времени по уже разрешённому agent ID; самостоятельных tenant, expected-version и permission predicates не содержит.
	//go:embed sql/commands_changeinstructions_update_agents_version_updated_at.sql
	queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt string
	// queryCommandsChangeagentbindingSelectAgentsOrganizationIdRef embeds tenant/ref locking of a non-system project agent and its project row before binding changes, returning the agent version for owner checks.
	// queryCommandsChangeagentbindingSelectAgentsOrganizationIdRef содержит tenant/ref блокировку несистемного project agent и его project row до binding changes с возвратом версии для owner checks.
	//go:embed sql/commands_changeagentbinding_select_agents_organization_id_ref.sql
	queryCommandsChangeagentbindingSelectAgentsOrganizationIdRef string
	// queryCommandsChangeagentbindingSelectEnabledCapability embeds exact stable-key selection of an enabled capability; catalog availability alone does not authorize granting or executing it.
	// queryCommandsChangeagentbindingSelectEnabledCapability содержит точный stable-key выбор enabled capability; доступность в каталоге сама не разрешает выдачу и исполнение.
	//go:embed sql/commands_changeagentbinding_select_enabled_capability.sql
	queryCommandsChangeagentbindingSelectEnabledCapability string
	// queryCommandsChangeagentbindingActorCanGrant embeds supplied owner/admin-role or active project-membership checks through a closed capability-to-permission mapping with deny fallback; caller identity must already be verified.
	// queryCommandsChangeagentbindingActorCanGrant содержит проверку заданной owner/admin-role либо active project membership по закрытому capability-to-permission mapping с deny fallback; caller identity проверяется заранее.
	//go:embed sql/commands_changeagentbinding_actor_can_grant.sql
	queryCommandsChangeagentbindingActorCanGrant string
	// queryCommandsChangeagentbindingEnableIntegrationGrant embeds enabling the exact tenant/ref grant bound to the selected AGENT target, advancing version/time without its own expected-version predicate.
	// queryCommandsChangeagentbindingEnableIntegrationGrant содержит включение точного tenant/ref grant выбранного AGENT target с увеличением версии/времени без собственного expected-version predicate.
	//go:embed sql/commands_changeagentbinding_enable_integration_grant.sql
	queryCommandsChangeagentbindingEnableIntegrationGrant string
	// queryCommandsChangeagentbindingRevokeIntegrationGrant embeds disabling the exact tenant/ref AGENT-target grant and advancing version/time; terminal invocation/lease revocation is not performed here.
	// queryCommandsChangeagentbindingRevokeIntegrationGrant содержит отключение точного tenant/ref AGENT-target grant с увеличением версии/времени; terminal invocation и lease revocation здесь не выполняются.
	//go:embed sql/commands_changeagentbinding_revoke_integration_grant.sql
	queryCommandsChangeagentbindingRevokeIntegrationGrant string
	// queryCommandsChangeagentbindingAppendCapability embeds duplicate-free capability-array append for the tenant/ref agent; agent version advancement is a separate statement in the owning transaction.
	// queryCommandsChangeagentbindingAppendCapability содержит добавление capability в массив tenant/ref сотрудника без дубликата; версия сотрудника увеличивается отдельным statement owner transaction.
	//go:embed sql/commands_changeagentbinding_append_capability.sql
	queryCommandsChangeagentbindingAppendCapability string
	// queryCommandsChangeagentbindingRemoveCapability embeds removal of an existing capability from the tenant/ref agent array; it does not independently revoke integration grants or advance agent version.
	// queryCommandsChangeagentbindingRemoveCapability содержит удаление существующей capability из массива tenant/ref сотрудника; самостоятельно integration grants не отзывает и agent version не увеличивает.
	//go:embed sql/commands_changeagentbinding_remove_capability.sql
	queryCommandsChangeagentbindingRemoveCapability string
	// queryCommandsChangeagentbindingUpdateAgentsVersionUpdatedAt embeds tenant/ref agent version/time advancement after binding changes; the caller retains the lock/OCC and atomic audit/event obligations.
	// queryCommandsChangeagentbindingUpdateAgentsVersionUpdatedAt содержит tenant/ref увеличение версии/времени сотрудника после binding changes; lock/OCC и атомарные audit/event обеспечивает вызывающий код.
	//go:embed sql/commands_changeagentbinding_update_agents_version_updated_at.sql
	queryCommandsChangeagentbindingUpdateAgentsVersionUpdatedAt string
	// queryCommandsChangeagentbindingSelectAgentReadback embeds tenant/ref post-binding readback with role/runtime configuration, avatar and ordered active CLEAN knowledge refs; it does not reveal raw knowledge contents.
	// queryCommandsChangeagentbindingSelectAgentReadback содержит tenant/ref readback после binding changes с role/runtime configuration, avatar и ordered active CLEAN knowledge refs; raw knowledge contents не раскрывает.
	//go:embed sql/commands_changeagentbinding_select_agent_readback.sql
	queryCommandsChangeagentbindingSelectAgentReadback string
	// queryCommandsChangeworkflowInsertWorkflowsRefProjectIdPurpose embeds DRAFT workflow insertion with coordinator resolved in the same tenant/project; readiness and permission checks are not implied by that ref lookup.
	// queryCommandsChangeworkflowInsertWorkflowsRefProjectIdPurpose содержит вставку DRAFT workflow с coordinator того же tenant/project; поиск ref не подразумевает readiness и permission checks.
	//go:embed sql/commands_changeworkflow_insert_workflows_ref_project_id_purpose.sql
	queryCommandsChangeworkflowInsertWorkflowsRefProjectIdPurpose string
	// queryCommandsChangeworkflowSelectWorkflowsOrganizationIdRef embeds exact tenant/ref workflow and project row locking, returning state/version for the command's lifecycle and OCC validation.
	// queryCommandsChangeworkflowSelectWorkflowsOrganizationIdRef содержит точную tenant/ref блокировку workflow и project row с возвратом state/version для lifecycle и OCC проверки команды.
	//go:embed sql/commands_changeworkflow_select_workflows_organization_id_ref.sql
	queryCommandsChangeworkflowSelectWorkflowsOrganizationIdRef string
	// queryCommandsChangeworkflowUpdateWorkflowsDraftSpecStateVersion embeds draft metadata/spec update by resolved workflow ID only if the selected same-project coordinator is enabled and READY, then advances workflow version/time.
	// queryCommandsChangeworkflowUpdateWorkflowsDraftSpecStateVersion содержит draft metadata/spec update разрешённого workflow ID только при enabled READY coordinator того же проекта с увеличением версии/времени.
	//go:embed sql/commands_changeworkflow_update_workflows_draft_spec_state_version.sql
	queryCommandsChangeworkflowUpdateWorkflowsDraftSpecStateVersion string
	// queryCommandsChangeworkflowSelectDraftForValidation embeds draft-spec read by resolved workflow ID; it does not validate the spec or repeat tenant/permission checks itself.
	// queryCommandsChangeworkflowSelectDraftForValidation содержит чтение draft spec по разрешённому workflow ID; spec не проверяет и tenant/permission checks самостоятельно не повторяет.
	//go:embed sql/commands_changeworkflow_select_draft_for_validation.sql
	queryCommandsChangeworkflowSelectDraftForValidation string
	// queryCommandsChangeworkflowMarkWorkflowValid embeds VALID state and version/time advancement by workflow ID; prior spec validation and locked lifecycle eligibility belong to the command.
	// queryCommandsChangeworkflowMarkWorkflowValid содержит назначение VALID и увеличение версии/времени по workflow ID; spec validation и locked lifecycle eligibility принадлежат команде.
	//go:embed sql/commands_changeworkflow_mark_workflow_valid.sql
	queryCommandsChangeworkflowMarkWorkflowValid string
	// queryCommandsChangeworkflowSelectDraftForPublish embeds draft-spec and next published-version selection by resolved workflow ID; it does not reserve the version or publish content itself.
	// queryCommandsChangeworkflowSelectDraftForPublish содержит выбор draft spec и следующей published version по разрешённому workflow ID; версию не резервирует и content сам не публикует.
	//go:embed sql/commands_changeworkflow_select_draft_for_publish.sql
	queryCommandsChangeworkflowSelectDraftForPublish string
	// queryCommandsChangeworkflowInsertWorkflowVersionsRefWorkflowIdSpec embeds immutable workflow-version insertion with tenant/workflow, number, spec/digest and creator metadata; digest/number consistency remains an owner precondition.
	// queryCommandsChangeworkflowInsertWorkflowVersionsRefWorkflowIdSpec содержит вставку immutable workflow version с tenant/workflow, номером, spec/digest и creator metadata; согласованность digest/номера обеспечивает владелец.
	//go:embed sql/commands_changeworkflow_insert_workflow_versions_ref_workflow_id_spec.sql
	queryCommandsChangeworkflowInsertWorkflowVersionsRefWorkflowIdSpec string
	// queryCommandsChangeworkflowUpdateWorkflowsPublishedSpecPublishedVersionState embeds selected published-spec/version and PUBLISHED-state assignment with version/time advancement; the matching version row and atomic publication must already be ensured.
	// queryCommandsChangeworkflowUpdateWorkflowsPublishedSpecPublishedVersionState содержит назначение published spec/version и PUBLISHED с увеличением версии/времени; соответствующую version row и атомарность публикации обеспечивает вызывающий код.
	//go:embed sql/commands_changeworkflow_update_workflows_published_spec_published_version_state.sql
	queryCommandsChangeworkflowUpdateWorkflowsPublishedSpecPublishedVersionState string
	// queryCommandsChangeworkflowArchiveWorkflow embeds archival of a resolved workflow only when no active WORKFLOW run targets it; schedule suspension and broader graph cleanup remain separate.
	// queryCommandsChangeworkflowArchiveWorkflow содержит архивирование разрешённого workflow только без активного WORKFLOW run, нацеленного на него; остановка расписаний и graph cleanup отдельные.
	//go:embed sql/commands_changeworkflow_archive_workflow.sql
	queryCommandsChangeworkflowArchiveWorkflow string
	// queryCommandsChangeworkflowSelectAuthoritativeReadback embeds tenant/ref workflow readback with coordinator, draft/published specs and matching published revision ref; reading metadata does not grant execution authority.
	// queryCommandsChangeworkflowSelectAuthoritativeReadback содержит tenant/ref workflow readback с coordinator, draft/published specs и matching published revision ref; чтение metadata не выдаёт execution authority.
	//go:embed sql/commands_changeworkflow_select_authoritative_readback.sql
	queryCommandsChangeworkflowSelectAuthoritativeReadback string
	// queryCommandsChangeworkflowValidateAgentCapabilities embeds READY/enabled same-project agent capability coverage, accepting listed capabilities or enabled grants on connected connections; it is not full invocation authorization.
	// queryCommandsChangeworkflowValidateAgentCapabilities содержит проверку покрытия capabilities у READY/enabled сотрудника того же проекта через массив либо enabled grants connected connections; это не полная invocation authorization.
	//go:embed sql/commands_changeworkflow_validate_agent_capabilities.sql
	queryCommandsChangeworkflowValidateAgentCapabilities string
	// queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef embeds launch-target eligibility for the exact tenant/project/ref READY enabled agent with a bound PUBLISHED instruction; runtime resource readiness is checked elsewhere.
	// queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef содержит launch-target eligibility точного tenant/project/ref READY enabled сотрудника с привязанной PUBLISHED инструкцией; runtime resource readiness проверяется отдельно.
	//go:embed sql/commands_launchrun_select_agents_organization_id_project_id_ref.sql
	queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef string
	// queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef embeds exact PUBLISHED workflow/version and enabled READY same-project coordinator selection for launch, returning immutable spec/digest pins rather than starting execution.
	// queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef содержит выбор точных PUBLISHED workflow/version и enabled READY coordinator того же проекта для запуска; возвращает immutable spec/digest pins, не начинает исполнение.
	//go:embed sql/commands_launchrun_select_workflows_organization_id_project_id_ref.sql
	queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef string
	// queryCommandsLaunchrunValidateAgentRuntimeContract embeds the owner runtime-contract readiness function call with exact tenant/project, agent refs, contract revision/digest and default-image digest; result handling belongs to the command.
	// queryCommandsLaunchrunValidateAgentRuntimeContract содержит вызов owner runtime-contract readiness function с точными tenant/project, agent refs, contract revision/digest и default-image digest; результат обрабатывает команда.
	//go:embed sql/commands_launchrun_validate_agent_runtime_contract.sql
	queryCommandsLaunchrunValidateAgentRuntimeContract string
	// queryCommandsLaunchrunInsertSessionsRefProjectIdTargetRef embeds ACTIVE session creation with tenant/project, exact target, provider account and creator pins; it does not reserve provider capacity or establish actor permissions.
	// queryCommandsLaunchrunInsertSessionsRefProjectIdTargetRef содержит создание ACTIVE session с tenant/project, точным target, provider account и creator pins; provider capacity не резервирует и actor permissions не устанавливает.
	//go:embed sql/commands_launchrun_insert_sessions_ref_project_id_target_ref.sql
	queryCommandsLaunchrunInsertSessionsRefProjectIdTargetRef string
	// queryCommandsLaunchrunSelectSessionsOrganizationIdProjectIdRef embeds exact ACTIVE tenant/project/session/target reuse with FOR UPDATE only when no QUEUED/RUNNING turn exists, serializing the caller's session decision.
	// queryCommandsLaunchrunSelectSessionsOrganizationIdProjectIdRef содержит exact ACTIVE tenant/project/session/target reuse с FOR UPDATE только без QUEUED/RUNNING turn, сериализуя решение вызывающего кода о session.
	//go:embed sql/commands_launchrun_select_sessions_organization_id_project_id_ref.sql
	queryCommandsLaunchrunSelectSessionsOrganizationIdProjectIdRef string
	// queryCommandsLaunchrunValidateInputArtifacts embeds exact tenant/project ACTIVE CLEAN artifact coverage and a 512-MiB aggregate bound; distinct-count equality also rejects missing or duplicate requested refs.
	// queryCommandsLaunchrunValidateInputArtifacts содержит покрытие exact tenant/project ACTIVE CLEAN artifacts и aggregate bound 512 МиБ; равенство distinct count отклоняет недостающие и повторные requested refs.
	//go:embed sql/commands_launchrun_validate_input_artifacts.sql
	queryCommandsLaunchrunValidateInputArtifacts string
	// queryCommandsLaunchrunInsertRunsRefProjectIdTargetType embeds QUEUED run insertion with exact session/workflow-version/target, title/task/input, initiator and concurrency metadata; it does not claim work or call a provider.
	// queryCommandsLaunchrunInsertRunsRefProjectIdTargetType содержит вставку QUEUED run с session/workflow-version/target, title/task/input, initiator и concurrency metadata; work не захватывает и провайдера не вызывает.
	//go:embed sql/commands_launchrun_insert_runs_ref_project_id_target_type.sql
	queryCommandsLaunchrunInsertRunsRefProjectIdTargetType string
	// queryCommandsLaunchrunUpdateRunsRootRunId embeds assigning a resolved run as its own root; tenant resolution and the complete launch graph transaction must precede this ID-only update.
	// queryCommandsLaunchrunUpdateRunsRootRunId содержит назначение разрешённого run его собственным root; ID-only update требует предварительного tenant resolution и полной launch graph transaction.
	//go:embed sql/commands_launchrun_update_runs_root_run_id.sql
	queryCommandsLaunchrunUpdateRunsRootRunId string
	// queryCommandsLaunchrunInsertSessionTurnsRefSessionIdTurnNumber embeds a QUEUED USER turn using the resolved session's next_turn_number; locking, numbering advancement and actor provenance remain with the owning transaction.
	// queryCommandsLaunchrunInsertSessionTurnsRefSessionIdTurnNumber содержит QUEUED USER turn с next_turn_number разрешённой session; locking, продвижение номера и actor provenance обеспечивает owner transaction.
	//go:embed sql/commands_launchrun_insert_session_turns_ref_session_id_turn_number.sql
	queryCommandsLaunchrunInsertSessionTurnsRefSessionIdTurnNumber string
	// queryCommandsLaunchrunUpdateSessionsNextTurnNumberVersionUpdatedAt embeds incrementing a resolved session's next turn number, version and time; it is not a standalone concurrent turn-allocation protocol.
	// queryCommandsLaunchrunUpdateSessionsNextTurnNumberVersionUpdatedAt содержит увеличение next turn number, версии и времени разрешённой session; самостоятельным concurrent turn-allocation protocol не является.
	//go:embed sql/commands_launchrun_update_sessions_next_turn_number_version_updated_at.sql
	queryCommandsLaunchrunUpdateSessionsNextTurnNumberVersionUpdatedAt string
	// queryCommandsLaunchrunInsertRunNodesRefRootRunIdType embeds the RUNNING ROOT_PROCESS presentation node with localized coordination role, turn/input pins and OPEN/CANCEL actions; execution readiness is not inferred from that presentation state.
	// queryCommandsLaunchrunInsertRunNodesRefRootRunIdType содержит RUNNING ROOT_PROCESS presentation node с локализованной coordination role, turn/input pins и OPEN/CANCEL; presentation state не доказывает execution readiness.
	//go:embed sql/commands_launchrun_insert_run_nodes_ref_root_run_id_type.sql
	queryCommandsLaunchrunInsertRunNodesRefRootRunIdType string
	// queryCommandsLaunchrunUpdateRunNodesWorkflowStepKeyHumanGateAfter embeds attaching workflow-step and human-gate metadata to a resolved node ID; it does not create a Human Gate or approve a step.
	// queryCommandsLaunchrunUpdateRunNodesWorkflowStepKeyHumanGateAfter содержит назначение workflow-step и human-gate metadata разрешённому node ID; Human Gate не создаёт и шаг не подтверждает.
	//go:embed sql/commands_launchrun_update_run_nodes_workflow_step_key_human_gate_after.sql
	queryCommandsLaunchrunUpdateRunNodesWorkflowStepKeyHumanGateAfter string
	// queryCommandsLaunchrunInsertRunEdgesRefRootRunIdTargetNodeId embeds a WAITING_FOR edge with supplied tenant/root/source/target IDs; graph lineage and permission validation belong to the enclosing launch command.
	// queryCommandsLaunchrunInsertRunEdgesRefRootRunIdTargetNodeId содержит WAITING_FOR edge с заданными tenant/root/source/target IDs; graph lineage и permission validation принадлежат enclosing launch command.
	//go:embed sql/commands_launchrun_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsLaunchrunInsertRunEdgesRefRootRunIdTargetNodeId string
	// queryCommandsLaunchrunUpdateRunsStateStartedAtVersion embeds RUNNING/start-time and version assignment by resolved run ID; it contains no local expected-state, tenant or OCC predicate.
	// queryCommandsLaunchrunUpdateRunsStateStartedAtVersion содержит назначение RUNNING/start-time и увеличение версии по разрешённому run ID; локальных expected-state, tenant и OCC predicates не содержит.
	//go:embed sql/commands_launchrun_update_runs_state_started_at_version.sql
	queryCommandsLaunchrunUpdateRunsStateStartedAtVersion string
	// queryCommandsInsertagentnodeSelectAgentsOrganizationIdRefState embeds tenant/ref READY/enabled agent resolution with role metadata; it does not independently constrain project or instruction publication.
	// queryCommandsInsertagentnodeSelectAgentsOrganizationIdRefState содержит tenant/ref разрешение READY/enabled сотрудника с role metadata; project и instruction publication самостоятельно не ограничивает.
	//go:embed sql/commands_insertagentnode_select_agents_organization_id_ref_state.sql
	queryCommandsInsertagentnodeSelectAgentsOrganizationIdRefState string
	// queryCommandsInsertagentnodeInsertRunNodesRefRootRunIdParentNodeId embeds a QUEUED AGENT_EXECUTION node with supplied root/run/parent/agent/turn IDs and OPEN/CANCEL actions; lineage authority must already be resolved.
	// queryCommandsInsertagentnodeInsertRunNodesRefRootRunIdParentNodeId содержит QUEUED AGENT_EXECUTION node с заданными root/run/parent/agent/turn IDs и OPEN/CANCEL; lineage authority разрешается заранее.
	//go:embed sql/commands_insertagentnode_insert_run_nodes_ref_root_run_id_parent_node_id.sql
	queryCommandsInsertagentnodeInsertRunNodesRefRootRunIdParentNodeId string
	// queryCommandsInsertagentnodeInsertRunEdgesRefRootRunIdTargetNodeId embeds the DELEGATED_TO edge for resolved graph endpoints; inserting metadata alone does not authenticate delegation origin.
	// queryCommandsInsertagentnodeInsertRunEdgesRefRootRunIdTargetNodeId содержит DELEGATED_TO edge разрешённых graph endpoints; вставка metadata сама не аутентифицирует delegation origin.
	//go:embed sql/commands_insertagentnode_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsInsertagentnodeInsertRunEdgesRefRootRunIdTargetNodeId string
	// queryCommandsLaunchrunInsertPlannedWorkflowNode embeds a PLANNED execution node only for an enabled READY same-project agent, preserving step/gate/input pins without materializing execution.
	// queryCommandsLaunchrunInsertPlannedWorkflowNode содержит PLANNED execution node только enabled READY сотрудника того же проекта, сохраняя step/gate/input pins без materialization исполнения.
	//go:embed sql/commands_launchrun_insert_planned_workflow_node.sql
	queryCommandsLaunchrunInsertPlannedWorkflowNode string
	// queryCommandsLaunchrunInsertPlannedWorkflowEdge embeds a supplied-type workflow edge between resolved nodes; edge-type validity and graph eligibility remain command-level checks.
	// queryCommandsLaunchrunInsertPlannedWorkflowEdge содержит workflow edge заданного типа между разрешёнными nodes; допустимость edge type и graph eligibility проверяет команда.
	//go:embed sql/commands_launchrun_insert_planned_workflow_edge.sql
	queryCommandsLaunchrunInsertPlannedWorkflowEdge string
	// queryCommandsEmitplatformeventUpdateInstallationPlatformSequence embeds singleton platform-sequence increment and readback; event/outbox insertion must share the owner transaction.
	// queryCommandsEmitplatformeventUpdateInstallationPlatformSequence содержит увеличение singleton platform sequence с readback; event/outbox insertion должна входить в owner transaction.
	//go:embed sql/commands_emitplatformevent_update_installation_platform_sequence.sql
	queryCommandsEmitplatformeventUpdateInstallationPlatformSequence string
	// queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload embeds platform outbox insertion with exact event/subject/order/sequence/payload; it neither publishes to the broker nor acknowledges delivery.
	// queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload содержит platform outbox insertion с event/subject/order/sequence/payload; в broker не публикует и delivery не подтверждает.
	//go:embed sql/commands_emitplatformevent_insert_outbox_events_event_id_ordering_key_payload.sql
	queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload string
	// queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt embeds resolved-run event-sequence/graph-revision advancement with update time, returning the unchanged aggregate version alongside sequence.
	// queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt содержит увеличение event sequence/graph revision разрешённого run с update time, возвращает неизменённую aggregate version вместе с sequence.
	//go:embed sql/commands_emitrunevent_update_runs_event_sequence_graph_revision_updated_at.sql
	queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt string
	// queryCommandsEmitruneventSelectProjectsId embeds project-ref lookup by an already resolved internal ID; it contains no independent tenant or visibility predicate.
	// queryCommandsEmitruneventSelectProjectsId содержит project-ref lookup по уже разрешённому внутреннему ID; самостоятельных tenant и visibility predicates не содержит.
	//go:embed sql/commands_emitrunevent_select_projects_id.sql
	queryCommandsEmitruneventSelectProjectsId string
	// queryCommandsEmitruneventSelectRunDelta embeds tenant/root-ID run metadata and ordered graph artifact/gate refs for an event delta; consumer eligibility is not established here.
	// queryCommandsEmitruneventSelectRunDelta содержит tenant/root-ID run metadata и ordered graph artifact/gate refs для event delta; consumer eligibility здесь не устанавливается.
	//go:embed sql/commands_emitrunevent_select_run_delta.sql
	queryCommandsEmitruneventSelectRunDelta string
	// queryCommandsEmitruneventSelectNodeDelta embeds tenant/node-ref event metadata with child-run refs only through materialized delegation/callback or required-workflow provenance joins; it does not grant child execution authority.
	// queryCommandsEmitruneventSelectNodeDelta содержит tenant/node-ref event metadata с child-run refs только через materialized delegation/callback либо required-workflow provenance joins; child execution authority не выдаёт.
	//go:embed sql/commands_emitrunevent_select_node_delta.sql
	queryCommandsEmitruneventSelectNodeDelta string
	// queryCommandsEmitruneventSelectEdgeDelta embeds tenant/edge-ref projection of root/source/target refs and type/label; it reads a stored relation rather than authorizing a new edge.
	// queryCommandsEmitruneventSelectEdgeDelta содержит tenant/edge-ref проекцию root/source/target refs и type/label; читает сохранённую связь, не разрешает новое ребро.
	//go:embed sql/commands_emitrunevent_select_edge_delta.sql
	queryCommandsEmitruneventSelectEdgeDelta string
	// queryCommandsEmitruneventSelectGateDelta embeds tenant/gate-ref event metadata with requester/resolver and attachment context, requiring project presence or owned organization-assistant scope; it does not resolve the gate.
	// queryCommandsEmitruneventSelectGateDelta содержит tenant/gate-ref event metadata с requester/resolver и attachment context, требует project presence либо owned organization-assistant scope; gate не разрешает.
	//go:embed sql/commands_emitrunevent_select_gate_delta.sql
	queryCommandsEmitruneventSelectGateDelta string
	// queryCommandsEmitruneventSelectArtifactDelta embeds tenant/artifact-ref metadata, lifecycle/scan states and knowledge-binding refs; reading scan-state metadata is not a new scan or content download.
	// queryCommandsEmitruneventSelectArtifactDelta содержит tenant/artifact-ref metadata, lifecycle/scan states и knowledge-binding refs; чтение scan-state metadata не является новым scan и content download.
	//go:embed sql/commands_emitrunevent_select_artifact_delta.sql
	queryCommandsEmitruneventSelectArtifactDelta string
	// queryCommandsEmitruneventInsertRunEventsEventIdOrganizationIdRootRunId embeds durable run-event insertion with aggregate/graph pins and supplied safe_delta/tool_call JSON; producers must sanitize payloads before this statement.
	// queryCommandsEmitruneventInsertRunEventsEventIdOrganizationIdRootRunId содержит durable run-event insertion с aggregate/graph pins и заданным safe_delta/tool_call JSON; producer обязан очистить payload до statement.
	//go:embed sql/commands_emitrunevent_insert_run_events_event_id_organization_id_root_run_id.sql
	queryCommandsEmitruneventInsertRunEventsEventIdOrganizationIdRootRunId string
	// queryCommandsEmitruneventInsertOutboxEventsEventIdOrderingKeyPayload embeds ordered run-event outbox persistence, not direct broker publication; atomic business/event commit belongs to the enclosing transaction.
	// queryCommandsEmitruneventInsertOutboxEventsEventIdOrderingKeyPayload содержит ordered run-event outbox persistence, не прямую публикацию broker; атомарный business/event commit принадлежит enclosing transaction.
	//go:embed sql/commands_emitrunevent_insert_outbox_events_event_id_ordering_key_payload.sql
	queryCommandsEmitruneventInsertOutboxEventsEventIdOrderingKeyPayload string
	// queryCommandsReadrungraphtxSelectRunNodesOrganizationIdRootRunIdRef embeds tenant-filtered nodes in the resolved root or its connected edges, ordered by creation with empty artifact/child placeholders; root-ref eligibility is a caller obligation.
	// queryCommandsReadrungraphtxSelectRunNodesOrganizationIdRootRunIdRef содержит tenant-filtered nodes разрешённого root либо его связанных edges, упорядоченные по creation с пустыми artifact/child placeholders; root-ref eligibility обеспечивает вызывающий код.
	//go:embed sql/commands_readrungraphtx_select_run_nodes_organization_id_root_run_id_ref.sql
	queryCommandsReadrungraphtxSelectRunNodesOrganizationIdRootRunIdRef string
	// queryCommandsReadrungraphtxSelectRunEdgesOrganizationIdRootRunIdRef embeds tenant-filtered edges for the root resolved from a run ref, projecting endpoint refs in creation order; no locks or new authority are created.
	// queryCommandsReadrungraphtxSelectRunEdgesOrganizationIdRootRunIdRef содержит tenant-filtered edges root, найденного по run ref, с endpoint refs в creation order; блокировки и новые полномочия не создаёт.
	//go:embed sql/commands_readrungraphtx_select_run_edges_organization_id_root_run_id_ref.sql
	queryCommandsReadrungraphtxSelectRunEdgesOrganizationIdRootRunIdRef string
	// queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState embeds tenant/ref ACTIVE-session and project locking for another turn, returning exact target metadata; it does not allocate the turn itself.
	// queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState содержит tenant/ref блокировку ACTIVE session и project для нового turn с точными target metadata; сам turn не выделяет.
	//go:embed sql/commands_addsessionturn_select_sessions_organization_id_ref_state.sql
	queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState string
	// queryCommandsAddsessionturnSelectRunsOrganizationIdRef embeds root lookup constrained by tenant, run ref and expected session ref; subsequent continuation lineage still needs owner validation.
	// queryCommandsAddsessionturnSelectRunsOrganizationIdRef содержит root lookup с tenant, run ref и ожидаемой session ref; дальнейший continuation lineage требует owner validation.
	//go:embed sql/commands_addsessionturn_select_runs_organization_id_ref.sql
	queryCommandsAddsessionturnSelectRunsOrganizationIdRef string
	// queryCommandsAddsessionturnSelectRunsRef embeds ref-only root-ID lookup after authoritative resolution; unlike the scoped lookup it contains no tenant/session predicate.
	// queryCommandsAddsessionturnSelectRunsRef содержит ref-only root-ID lookup после authoritative resolution; в отличие от scoped lookup tenant/session predicate не содержит.
	//go:embed sql/commands_addsessionturn_select_runs_ref.sql
	queryCommandsAddsessionturnSelectRunsRef string
	// queryCommandsAddsessionturnSelectPreviousRootNode embeds selection of one ROOT_PROCESS node by resolved root ID for continuation linkage; uniqueness and ownership are not enforced by LIMIT 1.
	// queryCommandsAddsessionturnSelectPreviousRootNode содержит выбор одного ROOT_PROCESS node по разрешённому root ID для continuation linkage; LIMIT 1 не обеспечивает uniqueness и ownership.
	//go:embed sql/commands_addsessionturn_select_previous_root_node.sql
	queryCommandsAddsessionturnSelectPreviousRootNode string
	// queryCommandsAddsessionturnSelectContinuationRootNode embeds the new continuation ROOT_PROCESS-node lookup by resolved root ID; it does not itself create or validate a continuation edge.
	// queryCommandsAddsessionturnSelectContinuationRootNode содержит поиск нового continuation ROOT_PROCESS node по разрешённому root ID; continuation edge сам не создаёт и не проверяет.
	//go:embed sql/commands_addsessionturn_select_continuation_root_node.sql
	queryCommandsAddsessionturnSelectContinuationRootNode string
	// queryCommandsAddsessionturnInsertRunEdgesRefRootRunIdTargetNodeId embeds a CONTINUES edge between owner-resolved nodes with supplied tenant/root pins; session and predecessor eligibility must be checked beforehand.
	// queryCommandsAddsessionturnInsertRunEdgesRefRootRunIdTargetNodeId содержит CONTINUES edge между owner-resolved nodes с tenant/root pins; session и predecessor eligibility проверяются заранее.
	//go:embed sql/commands_addsessionturn_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsAddsessionturnInsertRunEdgesRefRootRunIdTargetNodeId string
	// queryCommandsChangerunSelectRunsOrganizationIdRef embeds exact tenant/run-ref locking with root, nullable project, state/version/attempt and target type for lifecycle commands.
	// queryCommandsChangerunSelectRunsOrganizationIdRef содержит exact tenant/run-ref блокировку с root, nullable project, state/version/attempt и target type для lifecycle commands.
	//go:embed sql/commands_changerun_select_runs_organization_id_ref.sql
	queryCommandsChangerunSelectRunsOrganizationIdRef string
	// queryCommandsChangerunSelectRootRunID embeds tenant/run-ref root-ID resolution without locking; the caller separately fences the mutable cancellation graph.
	// queryCommandsChangerunSelectRootRunID содержит tenant/run-ref разрешение root ID без блокировки; mutable cancellation graph отдельно ограждает вызывающий код.
	//go:embed sql/commands_changerun_select_root_run_id.sql
	queryCommandsChangerunSelectRootRunID string
	// queryCommandsChangerunLockRunNodes embeds deterministic ID-order locking of PLANNED/QUEUED/RUNNING/WAITING nodes under a resolved root, not a complete graph authorization check.
	// queryCommandsChangerunLockRunNodes содержит блокировку PLANNED/QUEUED/RUNNING/WAITING nodes разрешённого root в ID order, не полную graph authorization check.
	//go:embed sql/commands_changerun_lock_run_nodes.sql
	queryCommandsChangerunLockRunNodes string
	// queryCommandsChangerunUpdateRunsStateSafeErrorCodeSafeErrorMessage embeds cancellation of active root-graph runs with a fixed owner-cancel code, finish/update time and version advancement; terminal rows remain unchanged.
	// queryCommandsChangerunUpdateRunsStateSafeErrorCodeSafeErrorMessage содержит отмену active root-graph runs с fixed owner-cancel code, finish/update time и увеличением версии; terminal rows не меняются.
	//go:embed sql/commands_changerun_update_runs_state_safe_error_code_safe_error_message.sql
	queryCommandsChangerunUpdateRunsStateSafeErrorCodeSafeErrorMessage string
	// queryCommandsChangerunUpdateRunNodesStateNextActionsFinishedAt embeds active/planned node cancellation with OPEN/RETRY actions, finish time and advanced version, returning affected refs for owner event handling.
	// queryCommandsChangerunUpdateRunNodesStateNextActionsFinishedAt содержит отмену active/planned nodes с OPEN/RETRY, finish time и advanced version, возвращает refs для owner event handling.
	//go:embed sql/commands_changerun_update_run_nodes_state_next_actions_finished_at.sql
	queryCommandsChangerunUpdateRunNodesStateNextActionsFinishedAt string
	// queryCommandsChangerunUpdateRuntimeLeasesStateUpdatedAt embeds cancellation of CLAIMED runtime leases for runs under the resolved root; other lease states and external workloads are not changed here.
	// queryCommandsChangerunUpdateRuntimeLeasesStateUpdatedAt содержит отмену CLAIMED runtime leases запусков разрешённого root; остальные lease states и внешние workloads здесь не меняются.
	//go:embed sql/commands_changerun_update_runtime_leases_state_updated_at.sql
	queryCommandsChangerunUpdateRuntimeLeasesStateUpdatedAt string
	// queryCommandsChangerunUpdateScheduleOccurrencesStateCancelled embeds cancellation of MATERIALIZED occurrences for one resolved run, clearing lease/fence/workload pins and advancing completion/version metadata.
	// queryCommandsChangerunUpdateScheduleOccurrencesStateCancelled содержит отмену MATERIALIZED occurrences разрешённого run с очисткой lease/fence/workload pins и увеличением completion/version metadata.
	//go:embed sql/commands_changerun_update_schedule_occurrences_state_cancelled.sql
	queryCommandsChangerunUpdateScheduleOccurrencesStateCancelled string
	// queryCommandsChangerunUpdateSessionTurnsStateCompletedAt embeds cancellation/completion-time assignment for QUEUED/RUNNING turns belonging to the resolved root graph, leaving terminal turns unchanged.
	// queryCommandsChangerunUpdateSessionTurnsStateCompletedAt содержит назначение cancellation/completion-time QUEUED/RUNNING turns разрешённого root graph без изменения terminal turns.
	//go:embed sql/commands_changerun_update_session_turns_state_completed_at.sql
	queryCommandsChangerunUpdateSessionTurnsStateCompletedAt string
	// queryCommandsChangerunCloseIntegrationEffects embeds graph-scoped invocation/delivery closure, preserving UNKNOWN_OUTCOME for running writes or claimed deliveries and clearing effect leases; it returns affected invocation/node refs.
	// queryCommandsChangerunCloseIntegrationEffects содержит graph-scoped закрытие invocation/delivery, сохраняя UNKNOWN_OUTCOME для running writes и claimed deliveries, очищает effect leases и возвращает invocation/node refs.
	//go:embed sql/commands_changerun_close_integration_effects.sql
	queryCommandsChangerunCloseIntegrationEffects string
	// queryCommandsChangerunUpdateOwnerGatesStateDecisionDecisionComment embeds cancellation of OPEN gates under a resolved root with owner actor/comment/time/version and affected node refs; actual human approval is not fabricated.
	// queryCommandsChangerunUpdateOwnerGatesStateDecisionDecisionComment содержит отмену OPEN gates разрешённого root с owner actor/comment/time/version и node refs; фактическое human approval не подделывает.
	//go:embed sql/commands_changerun_update_owner_gates_state_decision_decision_comment.sql
	queryCommandsChangerunUpdateOwnerGatesStateDecisionDecisionComment string
	// queryCommandsChangerunSelectRunsId embeds resolved-run retry-source metadata including target, task, input and attachment-purpose refs; it does not independently establish retry eligibility.
	// queryCommandsChangerunSelectRunsId содержит retry-source metadata разрешённого run: target, task, input и attachment-purpose refs; retry eligibility самостоятельно не устанавливает.
	//go:embed sql/commands_changerun_select_runs_id.sql
	queryCommandsChangerunSelectRunsId string
	// queryCommandsChangerunSelectRunsRef embeds ref-only run/root-ID readback after owner resolution; tenant or session authority is not proven by this statement.
	// queryCommandsChangerunSelectRunsRef содержит ref-only run/root-ID readback после owner resolution; tenant и session authority этим statement не доказываются.
	//go:embed sql/commands_changerun_select_runs_ref.sql
	queryCommandsChangerunSelectRunsRef string
	// queryCommandsChangerunUpdateRunsRetryOfRunIdAttempt embeds retry predecessor/attempt assignment to a resolved new run ID; fresh grants, claims and events require separate owner-transaction steps.
	// queryCommandsChangerunUpdateRunsRetryOfRunIdAttempt содержит назначение retry predecessor/attempt разрешённому новому run ID; fresh grants, claims и events требуют отдельных owner-transaction шагов.
	//go:embed sql/commands_changerun_update_runs_retry_of_run_id_attempt.sql
	queryCommandsChangerunUpdateRunsRetryOfRunIdAttempt string
	// queryCommandsChangerunSelectPreviousRootNode embeds one prior ROOT_PROCESS-node lookup by resolved root ID for retry linkage; LIMIT 1 does not check lineage or actor eligibility.
	// queryCommandsChangerunSelectPreviousRootNode содержит поиск одного прежнего ROOT_PROCESS node по разрешённому root ID для retry linkage; LIMIT 1 не проверяет lineage и actor eligibility.
	//go:embed sql/commands_changerun_select_previous_root_node.sql
	queryCommandsChangerunSelectPreviousRootNode string
	// queryCommandsChangerunSelectRetryRootNode embeds the retry root's ROOT_PROCESS-node lookup; it reads a prepared graph anchor rather than creating or authorizing retry state.
	// queryCommandsChangerunSelectRetryRootNode содержит поиск ROOT_PROCESS node retry root; читает подготовленный graph anchor, не создаёт и не разрешает retry state.
	//go:embed sql/commands_changerun_select_retry_root_node.sql
	queryCommandsChangerunSelectRetryRootNode string
	// queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId embeds a RETRY_OF edge with resolved graph endpoint/root pins; attempt lineage and cancellation cleanup remain owner responsibilities.
	// queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId содержит RETRY_OF edge с разрешёнными graph endpoint/root pins; attempt lineage и cancellation cleanup обеспечивает владелец.
	//go:embed sql/commands_changerun_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId string
	// queryCommandsResolvegateSelectOwnerGatesOrganizationIdRefState embeds exact tenant/ref OPEN-gate locking with predecessor/session/invocation metadata and project-or-owned-organization scope; decision eligibility remains with the command.
	// queryCommandsResolvegateSelectOwnerGatesOrganizationIdRefState содержит exact tenant/ref блокировку OPEN gate с predecessor/session/invocation metadata и project-or-owned-organization scope; decision eligibility проверяет команда.
	//go:embed sql/commands_resolvegate_select_owner_gates_organization_id_ref_state.sql
	queryCommandsResolvegateSelectOwnerGatesOrganizationIdRefState string
	// queryCommandsResolvegateSelectScopedIntegrationInvocation embeds locked WAITING_APPROVAL invocation/grant/connection resolution bound to an OPEN gate, waiting root and exact definition/grant/scope pins; supplied IDs alone are insufficient.
	// queryCommandsResolvegateSelectScopedIntegrationInvocation содержит locked разрешение WAITING_APPROVAL invocation/grant/connection с OPEN gate, waiting root и exact definition/grant/scope pins; заданных IDs недостаточно.
	//go:embed sql/commands_resolvegate_select_scoped_integration_invocation.sql
	queryCommandsResolvegateSelectScopedIntegrationInvocation string
	// queryCommandsResolvegateInsertIntegrationApprovalScope embeds a 24-hour scoped approval record with exact owner/resource/input pins, maximum 100 effects and one reservation; trusted scope construction must precede insertion.
	// queryCommandsResolvegateInsertIntegrationApprovalScope содержит 24-часовую scoped approval запись с owner/resource/input pins, максимумом 100 effects и одной reservation; trusted scope construction предшествует вставке.
	//go:embed sql/commands_resolvegate_insert_integration_approval_scope.sql
	queryCommandsResolvegateInsertIntegrationApprovalScope string
	// queryCommandsResolvegateBindIntegrationApprovalScope embeds first scope attachment only to WAITING_APPROVAL HUMAN_SCOPED invocations without an existing scope; validity of the selected scope is checked elsewhere.
	// queryCommandsResolvegateBindIntegrationApprovalScope содержит первую привязку scope только WAITING_APPROVAL HUMAN_SCOPED invocation без прежнего scope; допустимость выбранного scope проверяется отдельно.
	//go:embed sql/commands_resolvegate_bind_integration_approval_scope.sql
	queryCommandsResolvegateBindIntegrationApprovalScope string
	// queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment embeds resolved-gate state/decision/comment/actor assignment with completion time and advanced version; it relies on prior locking and decision policy validation.
	// queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment содержит state/decision/comment/actor assignment разрешённого gate с completion time и advanced version; зависит от предварительных locking и decision policy validation.
	//go:embed sql/commands_resolvegate_update_owner_gates_state_decision_decision_comment.sql
	queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment string
	// queryCommandsResolvegateUpdateIntegrationInvocation embeds state/error/version advancement only from WAITING_APPROVAL by resolved invocation ID; it does not execute the integration effect.
	// queryCommandsResolvegateUpdateIntegrationInvocation содержит state/error/version advancement только из WAITING_APPROVAL по разрешённому invocation ID; integration effect не исполняет.
	//go:embed sql/commands_resolvegate_update_integration_invocation.sql
	queryCommandsResolvegateUpdateIntegrationInvocation string
	// queryCommandsResolvegateInsertChangeRequestTurn embeds a QUEUED USER change-request turn using the locked session's next number; actor provenance and attachment checks belong to the command.
	// queryCommandsResolvegateInsertChangeRequestTurn содержит QUEUED USER change-request turn с next number заблокированной session; actor provenance и attachments проверяет команда.
	//go:embed sql/commands_resolvegate_insert_change_request_turn.sql
	queryCommandsResolvegateInsertChangeRequestTurn string
	// queryCommandsResolvegateUpdateChangeRequestSession embeds next-turn/version/time advancement by resolved session ID after change-request insertion, requiring the same enclosing transaction.
	// queryCommandsResolvegateUpdateChangeRequestSession содержит next-turn/version/time advancement разрешённой session после change-request insertion, требует той же enclosing transaction.
	//go:embed sql/commands_resolvegate_update_change_request_session.sql
	queryCommandsResolvegateUpdateChangeRequestSession string
	// queryCommandsResolvegateRequeuePredecessorNode embeds SUCCEEDED-to-QUEUED predecessor continuation with new turn/input, incremented attempt/version and cleared progress/errors/times; grants are materialized separately.
	// queryCommandsResolvegateRequeuePredecessorNode содержит SUCCEEDED-to-QUEUED predecessor continuation с новыми turn/input, увеличенными attempt/version и очищенными progress/errors/time; grants материализуются отдельно.
	//go:embed sql/commands_resolvegate_requeue_predecessor_node.sql
	queryCommandsResolvegateRequeuePredecessorNode string
	// queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion embeds final node state/time/version and OPEN-action assignment by resolved ID; allowed transitions are determined before executing this statement.
	// queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion содержит final node state/time/version и OPEN-action assignment по разрешённому ID; допустимые переходы определяются до statement.
	//go:embed sql/commands_resolvegate_update_run_nodes_state_finished_at_version.sql
	queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion string
	// queryCommandsResolvegateUpdateRootNodeState embeds root-process state/finish/version updates for a resolved graph, returning refs; node uniqueness and terminal eligibility are caller invariants.
	// queryCommandsResolvegateUpdateRootNodeState содержит root-process state/finish/version update разрешённого graph с возвратом refs; node uniqueness и terminal eligibility обеспечивает вызывающий код.
	//go:embed sql/commands_resolvegate_update_root_node_state.sql
	queryCommandsResolvegateUpdateRootNodeState string
	// queryCommandsResolvegateSelectActiveAgentNodes embeds the count of QUEUED/RUNNING/WAITING execution nodes plus OPEN required-workflow launches; it excludes planned and terminal nodes.
	// queryCommandsResolvegateSelectActiveAgentNodes содержит подсчёт QUEUED/RUNNING/WAITING execution nodes и OPEN required-workflow launches; planned и terminal nodes исключены.
	//go:embed sql/commands_resolvegate_select_active_agent_nodes.sql
	queryCommandsResolvegateSelectActiveAgentNodes string
	// queryCommandsResolvegateCountOpenGates embeds exact tenant/root OPEN-gate counting for the owner's completion decision; zero alone is not full graph or authority validation.
	// queryCommandsResolvegateCountOpenGates содержит exact tenant/root подсчёт OPEN gates для owner completion decision; одного zero недостаточно для полной graph и authority validation.
	//go:embed sql/commands_resolvegate_count_open_gates.sql
	queryCommandsResolvegateCountOpenGates string
	// queryCommandsResolvegateCompleteRootRun embeds SUCCEEDED/finish/version assignment by resolved root-run ID; prior graph completion checks and atomic events are owner obligations.
	// queryCommandsResolvegateCompleteRootRun содержит SUCCEEDED/finish/version assignment по разрешённому root-run ID; предварительные graph completion checks и atomic events обеспечивает владелец.
	//go:embed sql/commands_resolvegate_complete_root_run.sql
	queryCommandsResolvegateCompleteRootRun string
	// queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt embeds run state/version/time assignment, setting finish time only for FAILED/CANCELLED and otherwise clearing it; caller policy constrains the supplied state.
	// queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt содержит run state/version/time assignment, задаёт finish time лишь FAILED/CANCELLED, иначе очищает; допустимый заданный state ограничивает caller policy.
	//go:embed sql/commands_resolvegate_update_runs_state_version_updated_at.sql
	queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt string
	// queryCommandsMustrunrefSelectRunsId embeds public run-ref lookup from an owner-resolved internal ID; it performs no independent tenant or permission filtering.
	// queryCommandsMustrunrefSelectRunsId содержит public run-ref lookup по owner-resolved внутреннему ID; самостоятельную tenant и permission фильтрацию не выполняет.
	//go:embed sql/commands_mustrunref_select_runs_id.sql
	queryCommandsMustrunrefSelectRunsId string
)
