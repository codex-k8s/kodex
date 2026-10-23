package platform

import _ "embed"

var (
	// queryConfigurationLockAssistantBindingEnvironment embeds tenant/project/environment resolution restricted to ACTIVE project and environment, taking FOR SHARE on the environment only; permission checks remain outside this read.
	// queryConfigurationLockAssistantBindingEnvironment содержит поиск ACTIVE проекта и окружения по точным организации, проекту и окружению с FOR SHARE только для строки окружения; полномочия проверяют отдельно.
	//go:embed sql/configuration_lock_assistant_binding_environment.sql
	queryConfigurationLockAssistantBindingEnvironment string
	// queryConfigurationChangescheduleInsertSchedulesRefProjectIdTargetType embeds enabled schedule creation with immutable revision/target pins, timing/materialization policies and next-run time; it does not claim an occurrence or validate the supplied policies.
	// queryConfigurationChangescheduleInsertSchedulesRefProjectIdTargetType содержит создание включённого расписания с привязками неизменяемой ревизии и цели, правилами времени и обработки запусков и временем следующего запуска; экземпляр запуска не захватывает и правила не валидирует.
	//go:embed sql/configuration_changeschedule_insert_schedules_ref_project_id_target_type.sql
	queryConfigurationChangescheduleInsertSchedulesRefProjectIdTargetType string
	// queryConfigurationChangescheduleInsertScheduleRevision embeds a full schedule revision snapshot including target digest, timing, overlap/misfire and prompt inputs; digest/revision consistency belongs to the owner transaction.
	// queryConfigurationChangescheduleInsertScheduleRevision содержит запись полного снимка ревизии расписания с хешем цели, временем, правилами перекрытия и пропущенных запусков и входами промпта; согласованность хеша и ревизии обеспечивает внешняя транзакция.
	//go:embed sql/configuration_changeschedule_insert_schedule_revision.sql
	queryConfigurationChangescheduleInsertScheduleRevision string
	// queryConfigurationChangescheduleUpdateSchedulesNameTargetTypeTargetRef embeds tenant/ref/version-fenced ACTIVE schedule update, replacing revision/target/policy pins and setting next_run_at only when currently enabled.
	// queryConfigurationChangescheduleUpdateSchedulesNameTargetTypeTargetRef содержит обновление ACTIVE расписания по организации, ссылке и ожидаемой версии с заменой привязок ревизии, цели и правил; next_run_at назначает только уже включённому расписанию.
	//go:embed sql/configuration_changeschedule_update_schedules_name_target_type_target_ref.sql
	queryConfigurationChangescheduleUpdateSchedulesNameTargetTypeTargetRef string
	// queryConfigurationChangescheduleUpdateSchedulesEnabledVersionUpdatedAt embeds exact ACTIVE schedule OCC enable/disable, clearing next_run_at when disabled and advancing version/time; occurrence cancellation is separate.
	// queryConfigurationChangescheduleUpdateSchedulesEnabledVersionUpdatedAt содержит включение или отключение точного ACTIVE расписания с проверкой ожидаемой версии, очисткой next_run_at при отключении и обновлением версии и времени; отмена экземпляров запуска выполняется отдельно.
	//go:embed sql/configuration_changeschedule_update_schedules_enabled_version_updated_at.sql
	queryConfigurationChangescheduleUpdateSchedulesEnabledVersionUpdatedAt string
	// queryConfigurationChangescheduleSelectScheduleForUpdate embeds tenant/ref schedule and current-revision snapshot locking, excluding DELETED but allowing other lifecycle states for command-side validation.
	// queryConfigurationChangescheduleSelectScheduleForUpdate содержит чтение и блокировку расписания организации и его текущей ревизии; исключает DELETED, оставляя остальные состояния для проверки командой.
	//go:embed sql/configuration_changeschedule_select_schedule_for_update.sql
	queryConfigurationChangescheduleSelectScheduleForUpdate string
	// queryConfigurationChangescheduleSelectAgentTarget embeds same-project non-system READY/enabled agent selection with a digest of ref and version; that metadata digest is not image admission or runtime readiness.
	// queryConfigurationChangescheduleSelectAgentTarget содержит выбор включённого несистемного сотрудника READY того же проекта с хешем ссылки и версии; этот хеш метаданных не является допуском образа или проверкой готовности runtime.
	//go:embed sql/configuration_changeschedule_select_agent_target.sql
	queryConfigurationChangescheduleSelectAgentTarget string
	// queryConfigurationChangescheduleSelectWorkflowTarget embeds exact PUBLISHED same-project workflow/version selection with its stored digest; it does not independently validate coordinator or execution availability.
	// queryConfigurationChangescheduleSelectWorkflowTarget содержит выбор точных PUBLISHED процесса и версии того же проекта с сохранённым хешем; готовность координатора и доступность исполнения отдельно не проверяет.
	//go:embed sql/configuration_changeschedule_select_workflow_target.sql
	queryConfigurationChangescheduleSelectWorkflowTarget string
	// queryConfigurationChangescheduleCancelClaimedOccurrences embeds cancellation of DUE/CLAIMED/RETRY_WAIT/DEAD_LETTER occurrences for a resolved schedule, clearing lease/fence/workload and dead-letter markers while advancing version/time.
	// queryConfigurationChangescheduleCancelClaimedOccurrences содержит отмену DUE, CLAIMED, RETRY_WAIT и DEAD_LETTER экземпляров разрешённого расписания с очисткой аренды, ограждения, исполнителя и признаков окончательного отказа и обновлением версии и времени.
	//go:embed sql/configuration_changeschedule_cancel_claimed_occurrences.sql
	queryConfigurationChangescheduleCancelClaimedOccurrences string
	// queryConfigurationChangescheduleArchiveSchedule embeds tenant/ref/OCC ACTIVE-to-ARCHIVED schedule transition, disabling it and clearing its next-run time; physical deletion and active work cleanup are not included.
	// queryConfigurationChangescheduleArchiveSchedule содержит перевод ACTIVE расписания организации в ARCHIVED с проверкой ожидаемой версии, отключением и очисткой времени следующего запуска; физическое удаление и очистку активной работы не выполняет.
	//go:embed sql/configuration_changeschedule_archive_schedule.sql
	queryConfigurationChangescheduleArchiveSchedule string
	// queryConfigurationChangescheduleDeleteSchedule embeds version-fenced logical deletion only from ARCHIVED, leaving the schedule row disabled with no next run; it is not physical history removal.
	// queryConfigurationChangescheduleDeleteSchedule содержит логическое удаление только ARCHIVED расписания с проверкой ожидаемой версии, оставляя строку отключённой без следующего запуска; историю физически не удаляет.
	//go:embed sql/configuration_changeschedule_delete_schedule.sql
	queryConfigurationChangescheduleDeleteSchedule string
	// queryConfigurationChangeconnectionInsertIntegrationConnectionsRefDefinitionKeyState embeds NOT_CONNECTED enabled connection creation only from an enabled definition matching exact version/digest pins; it creates metadata, not a successful provider connection.
	// queryConfigurationChangeconnectionInsertIntegrationConnectionsRefDefinitionKeyState содержит создание включённого подключения NOT_CONNECTED только из включённого определения с точными версией и хешем; создаёт метаданные, а не успешное соединение с провайдером.
	//go:embed sql/configuration_changeconnection_insert_integration_connections_ref_definition_key_state.sql
	queryConfigurationChangeconnectionInsertIntegrationConnectionsRefDefinitionKeyState string
	// queryConfigurationChangeconnectionInsertCredentialRevision embeds max-plus-one credential revision metadata for a resolved connection, pinning secret ref/UID/resource-version/content hash without storing raw credential bytes; serialization is a caller obligation.
	// queryConfigurationChangeconnectionInsertCredentialRevision содержит следующую после максимальной ревизию реквизитов разрешённого подключения с привязками ссылки секрета, UID, версии ресурса и хеша без исходных секретных байтов; сериализацию обеспечивает вызывающий код.
	//go:embed sql/configuration_changeconnection_insert_credential_revision.sql
	queryConfigurationChangeconnectionInsertCredentialRevision string
	// queryConfigurationChangeconnectionActivateCredentialRevision embeds OCC credential-revision/materialization activation on a resolved connection, marking CONFIGURED but resetting connection state to NOT_CONNECTED and clearing the last test summary.
	// queryConfigurationChangeconnectionActivateCredentialRevision содержит активацию ревизии реквизитов и материализации разрешённого подключения с проверкой ожидаемой версии; назначает CONFIGURED, но сбрасывает подключение в NOT_CONNECTED и очищает итог последней проверки.
	//go:embed sql/configuration_changeconnection_activate_credential_revision.sql
	queryConfigurationChangeconnectionActivateCredentialRevision string
	// queryConfigurationChangeconnectionSelectCredentialTarget embeds exact tenant/ref/version ACTIVE-lifecycle connection locking with an enabled definition; it does not require connection enabled or prove credential availability.
	// queryConfigurationChangeconnectionSelectCredentialTarget содержит блокировку подключения организации с ACTIVE жизненным циклом по точным ссылке и версии и с включённым определением; включения самого подключения не требует и доступность реквизитов не доказывает.
	//go:embed sql/configuration_changeconnection_select_credential_target.sql
	queryConfigurationChangeconnectionSelectCredentialTarget string
	// queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled embeds enabled definition version/digest selection restricted to ready managed-MCP or interaction routes; route readiness metadata is not a live provider probe.
	// queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled содержит выбор версии и хеша включённого определения только для готовых управляемых MCP или interaction маршрутов; метаданные готовности маршрута не являются живой проверкой провайдера.
	//go:embed sql/configuration_changeconnection_select_integration_definitions_stable_key_enabled.sql
	queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled string
	// queryConfigurationChangeconnectionLock embeds tenant/ref ACTIVE-lifecycle connection locking and returns enabled/state/definition pins for separate command policy checks, rather than requiring CONNECTED itself.
	// queryConfigurationChangeconnectionLock содержит блокировку ACTIVE подключения организации с возвратом включения, состояния и привязок определения для отдельных проверок команды; CONNECTED сам не требует.
	//go:embed sql/configuration_changeconnection_lock.sql
	queryConfigurationChangeconnectionLock string
	// queryConfigurationChangeconnectionCountActiveDependencies embeds unsettled test/invocation/delivery counts, including UNKNOWN_OUTCOME, plus enabled grant count for a resolved connection; it does not close or retry effects.
	// queryConfigurationChangeconnectionCountActiveDependencies содержит количества незавершённых проверок, вызовов и доставок, включая UNKNOWN_OUTCOME, и включённых разрешений разрешённого подключения; внешние действия не закрывает и не повторяет.
	//go:embed sql/configuration_changeconnection_count_active_dependencies.sql
	queryConfigurationChangeconnectionCountActiveDependencies string
	// queryConfigurationChangeconnectionUpdate embeds ACTIVE connection OCC metadata update, resetting health to NOT_CONNECTED or DISABLED and clearing test times; dependency and configuration validation belong to the command.
	// queryConfigurationChangeconnectionUpdate содержит обновление метаданных ACTIVE подключения с проверкой ожидаемой версии, сбросом в NOT_CONNECTED либо DISABLED и очисткой времени проверки; зависимости и конфигурацию валидирует команда.
	//go:embed sql/configuration_changeconnection_update.sql
	queryConfigurationChangeconnectionUpdate string
	// queryConfigurationChangeconnectionDelete embeds OCC logical DELETED transition only for an already disabled ACTIVE-lifecycle connection; credential cleanup and external provider deletion are not performed here.
	// queryConfigurationChangeconnectionDelete содержит логический переход в DELETED только уже отключённого подключения с ACTIVE жизненным циклом и ожидаемой версией; реквизиты и внешнее подключение провайдера не удаляет.
	//go:embed sql/configuration_changeconnection_delete.sql
	queryConfigurationChangeconnectionDelete string
	// queryConfigurationChangeconnectionUpdateIntegrationConnectionsStateLastTestSummaryVersion embeds tenant/ref/OCC entry into TESTING only for enabled ACTIVE connections not already testing; creating and executing the health-test task are separate.
	// queryConfigurationChangeconnectionUpdateIntegrationConnectionsStateLastTestSummaryVersion содержит переход точного включённого ACTIVE подключения организации в TESTING с проверкой ожидаемой версии и запретом повторного TESTING; задачу проверки создают и исполняют отдельно.
	//go:embed sql/configuration_changeconnection_update_integration_connections_state_last_test_summary_version.sql
	queryConfigurationChangeconnectionUpdateIntegrationConnectionsStateLastTestSummaryVersion string
	// queryConfigurationChangeconnectionUpdateIntegrationConnectionsEnabledStateVersion embeds tenant/ref/OCC enabled and supplied-state assignment for an ACTIVE connection; the owner must validate state/flag consistency before execution.
	// queryConfigurationChangeconnectionUpdateIntegrationConnectionsEnabledStateVersion содержит назначение флага включения и заданного состояния ACTIVE подключения организации с проверкой ожидаемой версии; согласованность флага и состояния проверяет владелец данных до исполнения.
	//go:embed sql/configuration_changeconnection_update_integration_connections_enabled_state_version.sql
	queryConfigurationChangeconnectionUpdateIntegrationConnectionsEnabledStateVersion string
	// queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt embeds batch disabling of enabled grants on the tenant/ref connection with version/time advancement; existing invocation closure needs its own lifecycle path.
	// queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt содержит массовое отключение включённых разрешений точного подключения организации с обновлением версии и времени; существующие вызовы закрывают отдельным переходом жизненного цикла.
	//go:embed sql/configuration_changeconnection_update_integration_grants_enabled_version_updated_at.sql
	queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt string
	// queryConfigurationChangeconnectionInsertIntegrationConnectionTestsRefConnectionIdCreatedBy embeds DUE health-test task insertion with resolved tenant/connection/creator pins; it neither contacts the provider nor creates a successful readiness result.
	// queryConfigurationChangeconnectionInsertIntegrationConnectionTestsRefConnectionIdCreatedBy содержит создание DUE задачи проверки с разрешёнными организацией, подключением и автором; провайдера не вызывает и успешную готовность не устанавливает.
	//go:embed sql/configuration_changeconnection_insert_integration_connection_tests_ref_connection_id_created_by.sql
	queryConfigurationChangeconnectionInsertIntegrationConnectionTestsRefConnectionIdCreatedBy string
	// queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest embeds cancellation of DUE/CLAIMED tests for the exact tenant/ref connection, clearing lease/fence/workload pins and recording completion/version metadata.
	// queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest содержит отмену DUE и CLAIMED проверок точного подключения организации с очисткой аренды, ограждения и исполнителя и записью завершения, версии и времени.
	//go:embed sql/configuration_changeconnection_update_integration_connection_tests_state_lease_ref_fence_digest.sql
	queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest string
	// queryConfigurationChangeintegrationgrantSelectIntegrationConnectionsOrganizationIdRefEnabled embeds ACTIVE-lifecycle tenant/ref connection locking with returned enabled/state/version/configuration/definition metadata; caller policy decides grant eligibility.
	// queryConfigurationChangeintegrationgrantSelectIntegrationConnectionsOrganizationIdRefEnabled содержит блокировку точного ACTIVE подключения организации с метаданными включения, состояния, версии, конфигурации и определения; допустимость выдачи разрешения определяют правила вызывающей команды.
	//go:embed sql/configuration_changeintegrationgrant_select_integration_connections_organization_id_ref_enabled.sql
	queryConfigurationChangeintegrationgrantSelectIntegrationConnectionsOrganizationIdRefEnabled string
	// queryConfigurationChangeintegrationgrantSelectAgentOrganizationIdRef embeds tenant/ref READY/enabled agent resolution in an ACTIVE project for a grant target; it does not itself authorize granting capabilities.
	// queryConfigurationChangeintegrationgrantSelectAgentOrganizationIdRef содержит разрешение включённого READY сотрудника ACTIVE проекта организации как цели разрешения; права выдавать возможности самостоятельно не устанавливает.
	//go:embed sql/configuration_changeintegrationgrant_select_agent_organization_id_ref.sql
	queryConfigurationChangeintegrationgrantSelectAgentOrganizationIdRef string
	// queryConfigurationChangeintegrationgrantSelectWorkflowOrganizationIdRef embeds same-tenant workflow target resolution in an ACTIVE project for DRAFT/VALID/PUBLISHED states, not only executable published workflows.
	// queryConfigurationChangeintegrationgrantSelectWorkflowOrganizationIdRef содержит разрешение процесса организации в ACTIVE проекте как цели в состояниях DRAFT, VALID и PUBLISHED, а не только опубликованных исполняемых процессов.
	//go:embed sql/configuration_changeintegrationgrant_select_workflow_organization_id_ref.sql
	queryConfigurationChangeintegrationgrantSelectWorkflowOrganizationIdRef string
	// queryConfigurationChangeintegrationgrantSelectIntegrationDefinitionsStableKey embeds capability catalog lookup for ready managed-MCP/interaction definition routes; unlike creation lookup it contains no explicit enabled-definition predicate.
	// queryConfigurationChangeintegrationgrantSelectIntegrationDefinitionsStableKey содержит чтение каталога возможностей готового управляемого MCP либо interaction маршрута определения; в отличие от создания подключения явного условия включённого определения нет.
	//go:embed sql/configuration_changeintegrationgrant_select_integration_definitions_stable_key.sql
	queryConfigurationChangeintegrationgrantSelectIntegrationDefinitionsStableKey string
	// queryConfigurationChangeintegrationgrantInsertIntegrationGrantsRefConnectionIdTargetKind embeds enabled grant upsert by connection/capability/target tuple, replacing policy/resource/definition/scope pins and advancing an existing version; authorization and idempotency remain owner responsibilities.
	// queryConfigurationChangeintegrationgrantInsertIntegrationGrantsRefConnectionIdTargetKind содержит создание или обновление включённого разрешения по подключению, возможности и цели с заменой правил, ресурсов, определения и областей действия и увеличением прежней версии; полномочия и идемпотентность обеспечивает владелец данных.
	//go:embed sql/configuration_changeintegrationgrant_insert_integration_grants_ref_connection_id_target_kind.sql
	queryConfigurationChangeintegrationgrantInsertIntegrationGrantsRefConnectionIdTargetKind string
	// queryConfigurationChangeintegrationgrantUpdateIntegrationGrantsEnabledVersionUpdatedAt embeds exact tenant/connection/capability/target grant disabling with version/time advancement, without a per-grant expected-version predicate.
	// queryConfigurationChangeintegrationgrantUpdateIntegrationGrantsEnabledVersionUpdatedAt содержит отключение разрешения точной организации, подключения, возможности и цели с увеличением версии и обновлением времени без отдельной проверки ожидаемой версии разрешения.
	//go:embed sql/configuration_changeintegrationgrant_update_integration_grants_enabled_version_updated_at.sql
	queryConfigurationChangeintegrationgrantUpdateIntegrationGrantsEnabledVersionUpdatedAt string
	// queryConfigurationChangeintegrationgrantUpdateIntegrationConnectionsVersion embeds expected-version advancement of a resolved connection after grant changes; locking and atomic grant/audit/event updates belong to the enclosing transaction.
	// queryConfigurationChangeintegrationgrantUpdateIntegrationConnectionsVersion содержит увеличение ожидаемой версии разрешённого подключения после изменения разрешений; блокировку и атомарность разрешений, аудита и событий обеспечивает внешняя транзакция.
	//go:embed sql/configuration_changeintegrationgrant_update_integration_connections_version.sql
	queryConfigurationChangeintegrationgrantUpdateIntegrationConnectionsVersion string
	// queryConfigurationCreateassistantconversationInsertSessionsRefProjectIdTargetRef embeds ACTIVE assistant-session creation with nullable project, exact assistant target/provider-account and creator pins; it does not reserve provider concurrency.
	// queryConfigurationCreateassistantconversationInsertSessionsRefProjectIdTargetRef содержит создание ACTIVE сессии помощника с необязательным проектом, точными целью, аккаунтом провайдера и автором; слот параллельного исполнения провайдера не резервирует.
	//go:embed sql/configuration_createassistantconversation_insert_sessions_ref_project_id_target_ref.sql
	queryConfigurationCreateassistantconversationInsertSessionsRefProjectIdTargetRef string
	// queryConfigurationCreateassistantconversationInsertAssistantConversationsRefProjectIdTitle embeds ACTIVE conversation creation with SERVER_DEFAULT title, context metadata and tenant-resolved optional profile; scope/profile eligibility is a separate owner check.
	// queryConfigurationCreateassistantconversationInsertAssistantConversationsRefProjectIdTitle содержит создание ACTIVE диалога с исходным SERVER_DEFAULT названием, контекстом и необязательным профилем организации; допустимость области и профиля отдельно проверяет владелец данных.
	//go:embed sql/configuration_createassistantconversation_insert_assistant_conversations_ref_project_id_title.sql
	queryConfigurationCreateassistantconversationInsertAssistantConversationsRefProjectIdTitle string
	// queryConfigurationUpdateassistantconversationtitleSelectConversation embeds exact tenant/ref conversation locking with version/title revision/state; the query itself does not restrict lifecycle state or authorize a rename.
	// queryConfigurationUpdateassistantconversationtitleSelectConversation содержит блокировку точного диалога организации с состоянием, версией и ревизией названия; сам запрос состояние жизненного цикла не ограничивает и право переименования не устанавливает.
	//go:embed sql/configuration_updateassistantconversationtitle_select_conversation.sql
	queryConfigurationUpdateassistantconversationtitleSelectConversation string
	// queryConfigurationUpdateassistantconversationtitleUpdateConversation embeds title/source assignment with both title-revision and aggregate-version advancement by resolved conversation ID; validation and OCC precede this ID-only write.
	// queryConfigurationUpdateassistantconversationtitleUpdateConversation содержит запись названия и его источника с увеличением ревизии названия и общей версии по разрешённому ID диалога; валидация и проверка ожидаемой версии предшествуют этой записи по ID.
	//go:embed sql/configuration_updateassistantconversationtitle_update_conversation.sql
	queryConfigurationUpdateassistantconversationtitleUpdateConversation string
	// queryConfigurationResolveassistantcontextSelectConnection embeds tenant/ref connection name/version lookup for display context, without an ACTIVE/CONNECTED or actor-visibility predicate.
	// queryConfigurationResolveassistantcontextSelectConnection содержит поиск названия и версии подключения организации для отображаемого контекста без условий ACTIVE, CONNECTED или видимости автору.
	//go:embed sql/configuration_resolveassistantcontext_select_connection.sql
	queryConfigurationResolveassistantcontextSelectConnection string
	// queryConfigurationResolveassistantcontextSelectResource embeds tenant/ref/kind metadata lookup across project, agent, workflow and run; LIMIT 1 does not establish lifecycle eligibility or actor permissions.
	// queryConfigurationResolveassistantcontextSelectResource содержит поиск метаданных проекта, сотрудника, процесса или запуска по организации, ссылке и виду; LIMIT 1 не устанавливает допустимость состояния или полномочия автора.
	//go:embed sql/configuration_resolveassistantcontext_select_resource.sql
	queryConfigurationResolveassistantcontextSelectResource string
	// queryConfigurationHydrateassistantoperationSelectProject embeds exact organization/ref lookup of an ACTIVE project's descriptive fields and version; it does not establish actor permissions.
	// queryConfigurationHydrateassistantoperationSelectProject содержит поиск описания и версии ACTIVE проекта по точным организации и ссылке; полномочия пользователя не устанавливает.
	//go:embed sql/configuration_hydrateassistantoperation_select_project.sql
	queryConfigurationHydrateassistantoperationSelectProject string
	// queryConfigurationHydrateassistantoperationSelectAgent embeds non-system, non-ARCHIVED agent hydration within an ACTIVE tenant/project, without requiring READY or enabled.
	// queryConfigurationHydrateassistantoperationSelectAgent содержит чтение несистемного, не ARCHIVED сотрудника ACTIVE проекта организации; READY и enabled не требует.
	//go:embed sql/configuration_hydrateassistantoperation_select_agent.sql
	queryConfigurationHydrateassistantoperationSelectAgent string
	// queryConfigurationHydrateassistantoperationSelectAgentCapability embeds agent capability metadata plus an enabled catalog-key existence check for a non-archived project agent; catalog existence is not an execution grant.
	// queryConfigurationHydrateassistantoperationSelectAgentCapability содержит метаданные возможностей сотрудника проекта и проверку существования включённого ключа каталога; существование ключа не даёт права исполнения.
	//go:embed sql/configuration_hydrateassistantoperation_select_agent_capability.sql
	queryConfigurationHydrateassistantoperationSelectAgentCapability string
	// queryConfigurationHydrateassistantoperationSelectIntegrationGrant embeds server admission evaluation and exact target/capability grant metadata, including the admission reason; the caller must interpret that reason rather than treating a returned row as approval.
	// queryConfigurationHydrateassistantoperationSelectIntegrationGrant содержит серверную проверку допуска и метаданные разрешения точной цели и возможности, включая причину решения; вызывающий код обязан разобрать причину, а не считать наличие строки одобрением.
	//go:embed sql/configuration_hydrateassistantoperation_select_integration_grant.sql
	queryConfigurationHydrateassistantoperationSelectIntegrationGrant string
	// queryConfigurationAssistantScheduleSnapshot embeds tenant/project schedule hydration with the exact current revision and an ACTIVE project, excluding only DELETED schedules; archived schedules can still be returned.
	// queryConfigurationAssistantScheduleSnapshot содержит чтение расписания и точной текущей ревизии в ACTIVE проекте организации; исключает только DELETED расписания, поэтому может вернуть архивное.
	//go:embed sql/configuration_assistant_schedule_snapshot.sql
	queryConfigurationAssistantScheduleSnapshot string
	// queryConfigurationAssistantRoleImageAgent embeds active-role reference and agent version lookup for a non-system, non-archived agent in an ACTIVE project; it neither builds nor admits a role image.
	// queryConfigurationAssistantRoleImageAgent содержит поиск ссылки на активную роль и версии несистемного, неархивного сотрудника ACTIVE проекта; образ роли не собирает и не допускает.
	//go:embed sql/configuration_assistant_role_image_agent.sql
	queryConfigurationAssistantRoleImageAgent string
	// queryConfigurationAddassistantturncommandSelectAssistantRuntimeOrganizationId embeds runtime freshness metadata evaluation requiring READY/BUSY, matching runtime/desired revisions and a heartbeat within 45 seconds; this is not a live readiness probe.
	// queryConfigurationAddassistantturncommandSelectAssistantRuntimeOrganizationId содержит проверку свежести метаданных runtime: READY/BUSY, равенство текущей и желаемой ревизий и heartbeat моложе 45 секунд; не выполняет живую проверку готовности.
	//go:embed sql/configuration_addassistantturncommand_select_assistant_runtime_organization_id.sql
	queryConfigurationAddassistantturncommandSelectAssistantRuntimeOrganizationId string
	// queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState embeds exact tenant/ref ACTIVE conversation resolution with session/project context, locking the conversation row only; session serialization is acquired separately.
	// queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState содержит разрешение ACTIVE диалога по точным организации и ссылке с контекстом сессии и проекта; блокирует лишь диалог, а блокировку сессии получают отдельно.
	//go:embed sql/configuration_addassistantturncommand_select_assistant_conversations_organization_id_ref_state.sql
	queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState string
	// queryConfigurationAddassistantturncommandSelectSessionsId embeds next-turn-number lookup and row locking by an already resolved session ID; tenant and conversation ownership must be established before this ID-only read.
	// queryConfigurationAddassistantturncommandSelectSessionsId содержит чтение номера следующего хода с блокировкой по уже разрешённому ID сессии; организацию и владение диалогом проверяют до этого чтения.
	//go:embed sql/configuration_addassistantturncommand_select_sessions_id.sql
	queryConfigurationAddassistantturncommandSelectSessionsId string
	// queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind embeds QUEUED user-turn insertion with session/run/turn-number and actor/content pins; trusted actor provenance and execution admission are owner responsibilities.
	// queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind содержит создание QUEUED хода пользователя с привязками сессии, запуска, номера хода, автора и текста; происхождение автора и допуск исполнения обеспечивает владелец данных.
	//go:embed sql/configuration_addassistantturncommand_insert_session_turns_ref_session_id_actor_kind.sql
	queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind string
	// queryConfigurationAddassistantturncommandSelectActiveRun embeds earliest eligible active run/node selection within the exact ACTIVE conversation session; selection alone does not claim a provider slot or serialize unrelated chats.
	// queryConfigurationAddassistantturncommandSelectActiveRun содержит выбор первого подходящего активного запуска и узла в сессии точного ACTIVE диалога; сам выбор не резервирует слот провайдера и не сериализует другие чаты.
	//go:embed sql/configuration_addassistantturncommand_select_active_run.sql
	queryConfigurationAddassistantturncommandSelectActiveRun string
	// queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt embeds next-turn-number and version/time advancement by resolved session ID; the enclosing transaction supplies locking and tenant authority.
	// queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt содержит увеличение номера следующего хода и версии с обновлением времени по разрешённому ID сессии; блокировку и полномочия организации обеспечивает внешняя транзакция.
	//go:embed sql/configuration_addassistantturncommand_update_sessions_next_turn_number_version_updated_at.sql
	queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt string
	// queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType embeds RUNNING system-assistant run creation with session, creator, optional project, context and dispatch priority; persisted RUNNING is not proof that provider execution has started.
	// queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType содержит создание RUNNING запуска системного помощника с сессией, автором, необязательным проектом, контекстом и приоритетом; записанный RUNNING не доказывает начало работы провайдера.
	//go:embed sql/configuration_addassistantturncommand_insert_runs_ref_project_id_target_type.sql
	queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType string
	// queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt embeds self-root assignment and start-time recording by resolved run ID, without advancing the run version in this statement.
	// queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt содержит назначение самого запуска корнем и запись времени начала по разрешённому ID; версию запуска этот запрос не увеличивает.
	//go:embed sql/configuration_addassistantturncommand_update_runs_root_run_id_started_at.sql
	queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt string
	// queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType embeds QUEUED agent-execution node creation from an exact enabled, non-archived tenant agent, with turn/input and cancellation pins; READY is not required by this statement.
	// queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType содержит создание QUEUED узла исполнения из точного включённого, неархивного сотрудника организации с привязками хода, ввода и отмены; READY этот запрос не требует.
	//go:embed sql/configuration_addassistantturncommand_insert_run_nodes_ref_root_run_id_type.sql
	queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType string
	// queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations embeds revision-one DRAFT plan insertion with conversation, operations and content digest; creation neither validates nor approves those operations.
	// queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations содержит создание DRAFT плана первой ревизии с диалогом, операциями и хешем содержимого; создание не валидирует и не одобряет операции.
	//go:embed sql/configuration_addassistantturncommand_insert_assistant_plans_ref_conversation_ref_operations.sql
	queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations string
	// queryConfigurationInsertAssistantPlanRevision embeds a plan-revision history snapshot with operations, digest and creator provenance, returning its creation time; owner-side serialization preserves revision consistency.
	// queryConfigurationInsertAssistantPlanRevision содержит запись снимка ревизии плана с операциями, хешем и происхождением автора, возвращая время создания; согласованность ревизий обеспечивает сериализация владельцем данных.
	//go:embed sql/configuration_insert_assistant_plan_revision.sql
	queryConfigurationInsertAssistantPlanRevision string
	// queryConfigurationUpdateassistantplandraftSelectPlan embeds tenant/ref plan locking with conversation/project and current operations/revision metadata, without restricting plan state; command policy checks precede mutation.
	// queryConfigurationUpdateassistantplandraftSelectPlan содержит блокировку плана по организации и ссылке с диалогом, проектом, операциями и ревизией без ограничения состояния; правила команды проверяют до изменения.
	//go:embed sql/configuration_updateassistantplandraft_select_plan.sql
	queryConfigurationUpdateassistantplandraftSelectPlan string
	// queryConfigurationUpdateassistantplandraftUpdatePlan embeds resolved-plan draft replacement, advancing the revision/version and clearing validation pins, problems and timestamp; this statement does not update updated_at.
	// queryConfigurationUpdateassistantplandraftUpdatePlan содержит замену черновика разрешённого плана с увеличением ревизии и версии и очисткой результатов, привязки и времени валидации; updated_at запрос не изменяет.
	//go:embed sql/configuration_updateassistantplandraft_update_plan.sql
	queryConfigurationUpdateassistantplandraftUpdatePlan string
	// queryConfigurationValidateassistantplanSelectPlan embeds validation-input locking for exact tenant/ref plans in DRAFT, VALID, INVALID or STALE, returning current revision/digest and project context.
	// queryConfigurationValidateassistantplanSelectPlan содержит блокировку входа валидации точного плана организации в DRAFT, VALID, INVALID или STALE с текущими ревизией, хешем и контекстом проекта.
	//go:embed sql/configuration_validateassistantplan_select_plan.sql
	queryConfigurationValidateassistantplanSelectPlan string
	// queryConfigurationValidateassistantplanUpdatePlan embeds validation state/revision/problems/timestamp persistence with version advancement by resolved plan ID; validation itself and OCC checks happen before this write.
	// queryConfigurationValidateassistantplanUpdatePlan содержит запись состояния, ревизии, ошибок и времени валидации с увеличением версии по разрешённому ID плана; сама валидация и проверка ожидаемой версии предшествуют записи.
	//go:embed sql/configuration_validateassistantplan_update_plan.sql
	queryConfigurationValidateassistantplanUpdatePlan string
	// queryConfigurationValidateassistantplanSelectTargetVersion embeds target-version lookup across a closed resource-kind set with kind-specific lifecycle filters; a matching version does not establish actor authorization or universal execution readiness.
	// queryConfigurationValidateassistantplanSelectTargetVersion содержит поиск версии цели в закрытом наборе видов ресурсов с отдельными для каждого вида фильтрами состояния; совпадение версии не устанавливает полномочия автора или общую готовность исполнения.
	//go:embed sql/configuration_validateassistantplan_select_target_version.sql
	queryConfigurationValidateassistantplanSelectTargetVersion string
	// queryConfigurationInsertAssistantPlanReceipt embeds a plan-revision receipt containing outcome, operation receipts, conflicts, audit and created-resource references; the receipt does not execute or approve operations by itself.
	// queryConfigurationInsertAssistantPlanReceipt содержит запись результата ревизии плана с итогом, квитанциями операций, конфликтами, аудитом и ссылками созданных ресурсов; сама квитанция операции не выполняет и не одобряет.
	//go:embed sql/configuration_insert_assistant_plan_receipt.sql
	queryConfigurationInsertAssistantPlanReceipt string
	// queryConfigurationMarkAssistantPlanStale embeds STALE marking, problem recording and version advancement by resolved plan ID; existing validated-revision metadata is not cleared here.
	// queryConfigurationMarkAssistantPlanStale содержит отметку STALE, запись проблем и увеличение версии по разрешённому ID плана; прежнюю привязку валидированной ревизии здесь не очищает.
	//go:embed sql/configuration_mark_assistant_plan_stale.sql
	queryConfigurationMarkAssistantPlanStale string
	// queryConfigurationRejectassistantplanSelectPlan embeds exact tenant/ref plan locking with state/version/revision for rejection checks, without enforcing a rejectable state in SQL.
	// queryConfigurationRejectassistantplanSelectPlan содержит блокировку точного плана организации с состоянием, версией и ревизией для проверки отклонения; допустимость состояния для отклонения SQL не ограничивает.
	//go:embed sql/configuration_rejectassistantplan_select_plan.sql
	queryConfigurationRejectassistantplanSelectPlan string
	// queryConfigurationRejectassistantplanUpdatePlan embeds REJECTED state assignment and version advancement by resolved plan ID; state eligibility, expected version and authority must already be checked.
	// queryConfigurationRejectassistantplanUpdatePlan содержит назначение REJECTED и увеличение версии по разрешённому ID плана; допустимость состояния, ожидаемую версию и полномочия проверяют заранее.
	//go:embed sql/configuration_rejectassistantplan_update_plan.sql
	queryConfigurationRejectassistantplanUpdatePlan string
	// queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt embeds latest-plan pointer replacement and version/time advancement by resolved conversation ID; plan/conversation ownership is established before this write.
	// queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt содержит замену ссылки на последний план и увеличение версии с обновлением времени по разрешённому ID диалога; принадлежность плана диалогу проверяют до записи.
	//go:embed sql/configuration_addassistantturncommand_update_assistant_conversations_latest_plan_id_version_updated_at.sql
	queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt string
	// queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt embeds context/allowed-operation replacement, latest-plan clearing and version/time advancement; a nonempty fallback title replaces only the untouched SERVER_DEFAULT placeholder and advances title revision without changing title source.
	// queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt содержит замену контекста и разрешённых операций, очистку последнего плана и обновление версии и времени; непустой запасной заголовок заменяет только исходную SERVER_DEFAULT заглушку с увеличением ревизии названия, не меняя его источник.
	//go:embed sql/configuration_addassistantturncommand_update_assistant_conversations_version_updated_at.sql
	queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt string
	// queryConfigurationApplyassistantplancommandSelectAssistantPlansOrganizationIdRefState embeds exact tenant/ref VALID plan resolution while locking both plan and conversation; current versus validated revision equality is checked by the caller, not this SQL predicate.
	// queryConfigurationApplyassistantplancommandSelectAssistantPlansOrganizationIdRefState содержит разрешение VALID плана по точным организации и ссылке с блокировкой плана и диалога; равенство текущей и валидированной ревизий проверяет вызывающий код, а не условие SQL.
	//go:embed sql/configuration_applyassistantplancommand_select_assistant_plans_organization_id_ref_state.sql
	queryConfigurationApplyassistantplancommandSelectAssistantPlansOrganizationIdRefState string
	// queryConfigurationApplyassistantplancommandPromoteSessionProject embeds owner-bound promotion of an ACTIVE unbound session to the exact ACTIVE project, requiring a matching ACTIVE unbound conversation and no nonterminal session runs; storage promotion remains separate.
	// queryConfigurationApplyassistantplancommandPromoteSessionProject содержит привязку ACTIVE сессии без проекта к точному ACTIVE проекту с проверкой автора, такого же непривязанного ACTIVE диалога и отсутствия незавершённых запусков; перенос связанного хранилища выполняется отдельно.
	//go:embed sql/configuration_applyassistantplancommand_promote_session_project.sql
	queryConfigurationApplyassistantplancommandPromoteSessionProject string
	// queryConfigurationApplyassistantplancommandPromoteSessionStorageProject embeds a locked session/project boundary check followed by NULL-project propagation across listed run, artifact, attachment, archive and storage rows; it returns boundary eligibility and does not rewrite already assigned project links or issue new grants.
	// queryConfigurationApplyassistantplancommandPromoteSessionStorageProject содержит проверку границы заблокированной сессии и проекта с заполнением NULL-привязок перечисленных запусков, артефактов, вложений, архивов и хранилища; возвращает допустимость границы, не переписывает назначенные проекты и не выдаёт новые разрешения.
	//go:embed sql/configuration_applyassistantplancommand_promote_session_storage_project.sql
	queryConfigurationApplyassistantplancommandPromoteSessionStorageProject string
	// queryConfigurationApplyassistantplancommandPromoteConversationProject embeds owner-bound ACTIVE conversation promotion only after its session is bound to the exact ACTIVE project; context name/version/operations come from the server projection rather than client payload.
	// queryConfigurationApplyassistantplancommandPromoteConversationProject содержит привязку ACTIVE диалога его автора только после привязки сессии к точному ACTIVE проекту; название, версию и операции контекста берёт из серверной проекции, а не запроса клиента.
	//go:embed sql/configuration_applyassistantplancommand_promote_conversation_project.sql
	queryConfigurationApplyassistantplancommandPromoteConversationProject string
	// queryConfigurationApplyassistantplancommandUpdateAssistantPlansStateVersionAppliedAt embeds APPLIED state and apply-time persistence with version advancement by resolved plan ID; validated revision pins and the required owner decision must be checked beforehand.
	// queryConfigurationApplyassistantplancommandUpdateAssistantPlansStateVersionAppliedAt содержит запись APPLIED, времени применения и увеличенной версии по разрешённому ID плана; привязки валидированной ревизии и необходимое решение владельца проверяют заранее.
	//go:embed sql/configuration_applyassistantplancommand_update_assistant_plans_state_version_applied_at.sql
	queryConfigurationApplyassistantplancommandUpdateAssistantPlansStateVersionAppliedAt string
	// queryConfigurationAuditassistantoperationInsertAuditEventsRefProjectIdAssistantAgentId embeds successful assistant-operation audit insertion using the assistant agent resolved from the exact tenant/conversation creator binding; atomicity with business changes belongs to the enclosing transaction.
	// queryConfigurationAuditassistantoperationInsertAuditEventsRefProjectIdAssistantAgentId содержит запись успешного аудита операции с помощником, полученным из точной привязки организации, диалога и автора; атомарность с бизнес-изменениями обеспечивает внешняя транзакция.
	//go:embed sql/configuration_auditassistantoperation_insert_audit_events_ref_project_id_assistant_agent_id.sql
	queryConfigurationAuditassistantoperationInsertAuditEventsRefProjectIdAssistantAgentId string
	// queryConfigurationUpdateassistantinstructionsUpdateAssistantRuntimeOwnerInstructionsVersionUpdatedAt embeds tenant/OCC owner-instruction replacement and version/time advancement, returning runtime and core-prompt metadata without modifying the core prompt or invoking a model.
	// queryConfigurationUpdateassistantinstructionsUpdateAssistantRuntimeOwnerInstructionsVersionUpdatedAt содержит замену пользовательских инструкций с проверкой организации и ожидаемой версии и обновлением версии и времени; возвращает метаданные runtime и базового промпта, не меняя базовый промпт и не вызывая модель.
	//go:embed sql/configuration_updateassistantinstructions_update_assistant_runtime_owner_instructions_version_updated_at.sql
	queryConfigurationUpdateassistantinstructionsUpdateAssistantRuntimeOwnerInstructionsVersionUpdatedAt string
	// queryConfigurationRecoverassistantUpdateAssistantRuntimeRuntimeStateWarmInstanceRefLastHeartbeatAt embeds tenant/OCC transition to RECOVERING while clearing warm-instance and heartbeat metadata and advancing version/time; this records a recovery request, not completed runtime recovery.
	// queryConfigurationRecoverassistantUpdateAssistantRuntimeRuntimeStateWarmInstanceRefLastHeartbeatAt содержит переход в RECOVERING с проверкой организации и ожидаемой версии, очисткой тёплого экземпляра и heartbeat и обновлением версии и времени; фиксирует запрос восстановления, а не завершённое восстановление runtime.
	//go:embed sql/configuration_recoverassistant_update_assistant_runtime_runtime_state_warm_instance_ref_last_heartbeat_at.sql
	queryConfigurationRecoverassistantUpdateAssistantRuntimeRuntimeStateWarmInstanceRefLastHeartbeatAt string
	// queryConfigurationGetassistanttxSelectAssistantRuntimeOrganizationId embeds tenant runtime/assistant-agent configuration lookup including instruction and revision metadata, without state, enabled or freshness predicates; returned configuration alone does not prove readiness.
	// queryConfigurationGetassistanttxSelectAssistantRuntimeOrganizationId содержит чтение конфигурации runtime и помощника организации, включая инструкции и ревизии, без фильтров состояния, включения и свежести; наличие конфигурации не доказывает готовность.
	//go:embed sql/configuration_getassistanttx_select_assistant_runtime_organization_id.sql
	queryConfigurationGetassistanttxSelectAssistantRuntimeOrganizationId string
)
