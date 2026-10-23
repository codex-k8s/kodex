package platform

import _ "embed"

var (
	// queryWorkersScheduleEventScope embeds schedule/project/version lookup through an exact tenant occurrence ID for event scope; it does not filter schedule lifecycle or authorize publication by itself.
	// queryWorkersScheduleEventScope содержит поиск расписания, проекта и версии через точный ID экземпляра организации для области события; жизненный цикл расписания не фильтрует и публикацию самостоятельно не разрешает.
	//go:embed sql/workers_schedule_event_scope.sql
	queryWorkersScheduleEventScope string
	// queryWorkersScheduleClock embeds the database wall-clock read used by schedule calculations; clock_timestamp advances independently of the transaction-start timestamp.
	// queryWorkersScheduleClock содержит чтение текущего времени базы для расчётов расписания; clock_timestamp изменяется независимо от времени начала транзакции.
	//go:embed sql/workers_schedule_clock.sql
	queryWorkersScheduleClock string
	// queryWorkersReconcilewarmruntimeLockSessionBinding embeds system warm-session/account binding lookup with runtime/agent/session/account locks and an eligibility flag based on active session, READY agent, authorized provider, catalog binding and policy membership; the caller must inspect that flag.
	// queryWorkersReconcilewarmruntimeLockSessionBinding содержит чтение привязки тёплой системной сессии к аккаунту с блокировкой runtime, сотрудника, сессии и аккаунта и признаком допустимости по активной сессии, READY сотруднику, авторизованному провайдеру, каталогу и политике; признак обязан проверить вызывающий код.
	//go:embed sql/workers_reconcilewarmruntime_lock_session_binding.sql
	queryWorkersReconcilewarmruntimeLockSessionBinding string
	// queryWorkersInitialWarmSessionBinding embeds initial system-session bootstrap resolution only when system_session_ref is NULL, with exact tenant service-subject provenance and runtime/agent locks; it does not select a human owner.
	// queryWorkersInitialWarmSessionBinding содержит разрешение начальной системной сессии только при NULL system_session_ref с точным происхождением служебного субъекта организации и блокировкой runtime и сотрудника; человека-владельца не выбирает.
	//go:embed sql/workers__initial_warm_session_binding.sql
	queryWorkersInitialWarmSessionBinding string
	// queryWorkersReconcilewarmruntimeSelectSystemProviderPolicy embeds locked system runtime/current-policy metadata and an ordered same-provider credentialed account pool, preferring device-code accounts when present; REAUTHORIZATION_REQUIRED candidates are included and enabled is not filtered here.
	// queryWorkersReconcilewarmruntimeSelectSystemProviderPolicy содержит блокировку метаданных системного runtime и текущей политики и упорядоченный пул аккаунтов того же провайдера с реквизитами, предпочитая device-code при наличии; включает REAUTHORIZATION_REQUIRED и здесь не фильтрует enabled.
	//go:embed sql/workers_reconcilewarmruntime_select_system_provider_policy.sql
	queryWorkersReconcilewarmruntimeSelectSystemProviderPolicy string
	// queryWorkersReconcilewarmruntimePublishSystemProviderPolicy embeds versioned provider-policy/config insertion, expected-current agent pointer replacement, RECOVERING runtime reset and audit in one statement; caller transaction rollback is still required if its final joined result is absent.
	// queryWorkersReconcilewarmruntimePublishSystemProviderPolicy содержит вставку версионированных политики и конфигурации провайдера, замену ожидаемого текущего указателя сотрудника, сброс runtime в RECOVERING и аудит одним запросом; отсутствие итоговой строки всё равно требует отката внешней транзакции.
	//go:embed sql/workers_reconcilewarmruntime_publish_system_provider_policy.sql
	queryWorkersReconcilewarmruntimePublishSystemProviderPolicy string
	// queryWorkersReconcilewarmruntimeCloseSession embeds exact tenant/session ACTIVE-to-CLOSED transition with version/time advancement; storage cleanup and workload cancellation are not performed by closing this row.
	// queryWorkersReconcilewarmruntimeCloseSession содержит перевод точной ACTIVE сессии организации в CLOSED с обновлением версии и времени; закрытие строки не очищает хранилище и не отменяет исполнителя.
	//go:embed sql/workers_reconcilewarmruntime_close_session.sql
	queryWorkersReconcilewarmruntimeCloseSession string
	// queryWorkersReconcilewarmruntimeInsertSession embeds an ACTIVE projectless SYSTEM_ASSISTANT session for the fixed system-assistant target and supplied account/creator; it is a warm-session record, not a provider slot claim.
	// queryWorkersReconcilewarmruntimeInsertSession содержит создание ACTIVE сессии SYSTEM_ASSISTANT без проекта для фиксированной цели system-assistant и заданных аккаунта и автора; это запись тёплой сессии, не захват слота провайдера.
	//go:embed sql/workers_reconcilewarmruntime_insert_session.sql
	queryWorkersReconcilewarmruntimeInsertSession string
	// queryWorkersReconcilewarmruntimeSwitchSession embeds tenant warm-session reference replacement only while the previous nullable session ref still matches, resetting runtime to RECOVERING and clearing instance/heartbeat metadata.
	// queryWorkersReconcilewarmruntimeSwitchSession содержит замену ссылки тёплой сессии организации лишь при совпадении прежней nullable ссылки со сбросом runtime в RECOVERING и очисткой экземпляра и heartbeat.
	//go:embed sql/workers_reconcilewarmruntime_switch_session.sql
	queryWorkersReconcilewarmruntimeSwitchSession string
	// queryWorkersReconcilewarmruntimeMarkUnavailable embeds session-ref-fenced UNAVAILABLE marking with instance/heartbeat clearing, advancing version/time only when observable state changes; repeated identical reports do not create new versions.
	// queryWorkersReconcilewarmruntimeMarkUnavailable содержит отметку UNAVAILABLE с ограждением ссылкой сессии и очисткой экземпляра и heartbeat, обновляя версию и время лишь при изменении наблюдаемого состояния; одинаковые повторы новых версий не создают.
	//go:embed sql/workers_reconcilewarmruntime_mark_unavailable.sql
	queryWorkersReconcilewarmruntimeMarkUnavailable string
	// queryWorkersReconcilewarmruntimeSelectAssistantRuntimeOrganizationId embeds locked system runtime specification with exact credential/config/environment descriptors and ACCEPTED/PROMOTED image contract pins; returned metadata is not a live model probe or loaded secret content.
	// queryWorkersReconcilewarmruntimeSelectAssistantRuntimeOrganizationId содержит блокировку спецификации системного runtime с точными описаниями реквизитов, конфигурации и окружения и привязками контракта ACCEPTED/PROMOTED образа; метаданные не являются живой проверкой модели или содержимым секрета.
	//go:embed sql/workers_reconcilewarmruntime_select_assistant_runtime_organization_id.sql
	queryWorkersReconcilewarmruntimeSelectAssistantRuntimeOrganizationId string
	// queryWorkersReconcilewarmruntimeUpdateAssistantRuntimeRuntimeStateWarmInstanceRefVersion embeds tenant runtime RECOVERING/instance assignment and version/time advancement without its own expected-version or session predicate; owner fencing must precede this write.
	// queryWorkersReconcilewarmruntimeUpdateAssistantRuntimeRuntimeStateWarmInstanceRefVersion содержит назначение RECOVERING и экземпляра runtime организации с обновлением версии и времени без собственной проверки ожидаемой версии или сессии; ограждение обеспечивает владелец данных до записи.
	//go:embed sql/workers_reconcilewarmruntime_update_assistant_runtime_runtime_state_warm_instance_ref_version.sql
	queryWorkersReconcilewarmruntimeUpdateAssistantRuntimeRuntimeStateWarmInstanceRefVersion string
	// queryWorkersReportwarmruntimeUpdateAssistantRuntimeRuntimeStateRuntimeRevisionWarmInstanceRef embeds locked heartbeat/state reporting only for the desired runtime revision and matching-or-unassigned workload, advancing version only on state/revision/instance changes while refreshing heartbeat every accepted report.
	// queryWorkersReportwarmruntimeUpdateAssistantRuntimeRuntimeStateRuntimeRevisionWarmInstanceRef содержит запись heartbeat и состояния с блокировкой только для желаемой ревизии runtime и совпадающего либо ещё не назначенного исполнителя, увеличивая версию лишь при изменении состояния, ревизии или экземпляра и обновляя heartbeat при каждом принятом отчёте.
	//go:embed sql/workers_reportwarmruntime_update_assistant_runtime_runtime_state_runtime_revision_warm_instance_ref.sql
	queryWorkersReportwarmruntimeUpdateAssistantRuntimeRuntimeStateRuntimeRevisionWarmInstanceRef string
	// queryWorkersClaimdueschedulesSelectSchedulesOrganizationId embeds SKIP LOCKED due ACTIVE/enabled schedule selection in ACTIVE tenant projects, pinning current revisions and respecting DEAD_LETTER/overlap exclusions; occurrence insertion is a separate owner step.
	// queryWorkersClaimdueschedulesSelectSchedulesOrganizationId содержит выбор наступивших ACTIVE включённых расписаний ACTIVE проектов организации с SKIP LOCKED, привязкой текущей ревизии и исключениями DEAD_LETTER и перекрытия; экземпляр запуска создают отдельным шагом владельца.
	//go:embed sql/workers_claimdueschedules_select_schedules_organization_id.sql
	queryWorkersClaimdueschedulesSelectSchedulesOrganizationId string
	// queryWorkersClaimdueschedulesSelectExpiredOccurrences embeds SKIP LOCKED selection of RETRY_WAIT or expired CLAIMED occurrences below three attempts on ACTIVE/enabled schedules and projects; retry-wait rows need no expiry predicate.
	// queryWorkersClaimdueschedulesSelectExpiredOccurrences содержит выбор с SKIP LOCKED экземпляров RETRY_WAIT либо просроченных CLAIMED с менее чем тремя попытками в ACTIVE включённых расписаниях и проектах; RETRY_WAIT не требует условия истечения аренды.
	//go:embed sql/workers_claimdueschedules_select_expired_occurrences.sql
	queryWorkersClaimdueschedulesSelectExpiredOccurrences string
	// queryWorkersClaimdueschedulesReclaimExpiredOccurrence embeds expected-generation reclaim of RETRY_WAIT or expired CLAIMED occurrences below three attempts, replacing lease/fence/workload and advancing attempt/generation/version together.
	// queryWorkersClaimdueschedulesReclaimExpiredOccurrence содержит повторный захват RETRY_WAIT либо просроченного CLAIMED экземпляра с ожидаемым поколением и менее чем тремя попытками, заменяя аренду, ограждение и исполнителя и одновременно увеличивая попытку, поколение и версию.
	//go:embed sql/workers_claimdueschedules_reclaim_expired_occurrence.sql
	queryWorkersClaimdueschedulesReclaimExpiredOccurrence string
	// queryWorkersClaimdueschedulesUpdateSchedulesNextRunAt embeds next-run-time replacement only while the resolved schedule still has the expected previous timestamp; it updates time but does not advance aggregate version.
	// queryWorkersClaimdueschedulesUpdateSchedulesNextRunAt содержит замену времени следующего запуска лишь при ожидаемом прежнем времени разрешённого расписания; время обновления меняет, общую версию не увеличивает.
	//go:embed sql/workers_claimdueschedules_update_schedules_next_run_at.sql
	queryWorkersClaimdueschedulesUpdateSchedulesNextRunAt string
	// queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState embeds immutable-input occurrence creation as SKIPPED/completed without lease metadata or CLAIMED with generation one and supplied lease pins; this statement does not create a run.
	// queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState содержит создание экземпляра с неизменяемым вводом либо SKIPPED с завершением без аренды, либо CLAIMED первого поколения с заданными привязками аренды; запуск этим запросом не создаётся.
	//go:embed sql/workers_claimdueschedules_insert_schedule_occurrences_ref_schedule_id_state.sql
	queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState string
	// queryWorkersClaimdueschedulesInsertAttempt embeds CLAIMED occurrence-attempt history with exact attempt/generation/lease/input/revision and credential-generation pins; the owner must serialize it with occurrence claiming.
	// queryWorkersClaimdueschedulesInsertAttempt содержит историю CLAIMED попытки экземпляра с точными попыткой, поколением, арендой, вводом, ревизией и поколением реквизитов; запись сериализуют вместе с захватом экземпляра.
	//go:embed sql/workers_claimdueschedules_insert_attempt.sql
	queryWorkersClaimdueschedulesInsertAttempt string
	// queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef embeds exact tenant/ref/lease occurrence locking with a CLAIMED attempt of the current generation and expected credential generation, returning immutable materialization/session-policy inputs; expiry/fence checks remain with the command.
	// queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef содержит блокировку точного экземпляра организации по ссылке и аренде при CLAIMED попытке текущего поколения с ожидаемым поколением реквизитов, возвращая неизменяемый ввод материализации и политику сессии; срок и ограждение проверяет команда.
	//go:embed sql/workers_changeoccurrence_select_schedule_occurrences_organization_id_ref_lease_ref.sql
	queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef string
	// queryWorkersChangeoccurrenceSelectSchedulesId embeds resolved schedule readback with project/target names and timing/lifecycle metadata by internal ID; it performs no independent tenant or actor authorization.
	// queryWorkersChangeoccurrenceSelectSchedulesId содержит чтение разрешённого расписания, имён проекта и цели и метаданных времени и жизненного цикла по внутреннему ID; самостоятельную проверку организации и автора не выполняет.
	//go:embed sql/workers_changeoccurrence_select_schedules_id.sql
	queryWorkersChangeoccurrenceSelectSchedulesId string
	// queryWorkersChangeoccurrenceSelectRunsRef embeds ref-only run-ID lookup after owner resolution; a matching public ref does not itself prove tenant or occurrence ownership.
	// queryWorkersChangeoccurrenceSelectRunsRef содержит поиск ID запуска лишь по ссылке после разрешения владельцем данных; совпадение публичной ссылки само не доказывает организацию или принадлежность экземпляру расписания.
	//go:embed sql/workers_changeoccurrence_select_runs_ref.sql
	queryWorkersChangeoccurrenceSelectRunsRef string
	// queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion embeds CLAIMED-to-MATERIALIZED run binding with lease clearing and conditional CONTINUE_ONE session-pointer update only when current and pinned policies/targets agree.
	// queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion содержит привязку запуска при переходе CLAIMED в MATERIALIZED с очисткой аренды и обновлением указателя сессии CONTINUE_ONE лишь при совпадении текущих и закреплённых правил и целей.
	//go:embed sql/workers_changeoccurrence_update_schedule_occurrences_state_run_id_version.sql
	queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion string
	// queryWorkersScheduleAttemptFinish embeds completion of the exact CLAIMED occurrence attempt/generation, recording run/session/first-turn provenance only for MATERIALIZED outcome; other transitions leave those derived refs NULL.
	// queryWorkersScheduleAttemptFinish содержит завершение точной CLAIMED попытки и поколения экземпляра с записью происхождения запуска, сессии и первого хода только для MATERIALIZED; для других переходов производные ссылки остаются NULL.
	//go:embed sql/workers_schedule_attempt_finish.sql
	queryWorkersScheduleAttemptFinish string
	// queryWorkersScheduleOccurrenceFail embeds CLAIMED failure to RETRY_WAIT only for retryable outcomes below three attempts, otherwise DEAD_LETTER, clearing leases and assigning terminal timestamps only to the latter.
	// queryWorkersScheduleOccurrenceFail содержит переход ошибки CLAIMED в RETRY_WAIT лишь при повторяемой ошибке и менее чем трёх попытках, иначе в DEAD_LETTER, очищая аренду и назначая конечные времена только последнему.
	//go:embed sql/workers_schedule_occurrence_fail.sql
	queryWorkersScheduleOccurrenceFail string
	// queryWorkersScheduleOccurrenceRenew embeds a 30-second renewal of an unexpired exact tenant/ref/lease/fence/generation CLAIMED occurrence and its matching attempt/credential generation, returning the jointly renewed lease pins.
	// queryWorkersScheduleOccurrenceRenew содержит продление на 30 секунд действующего CLAIMED экземпляра с точными организацией, ссылкой, арендой, ограждением и поколением и соответствующей попытки и поколения реквизитов, возвращая совместно обновлённые привязки аренды.
	//go:embed sql/workers_schedule_occurrence_renew.sql
	queryWorkersScheduleOccurrenceRenew string
	// queryWorkersScheduleOccurrenceExpireDeadLetter embeds bounded SKIP LOCKED expiry of CLAIMED occurrences at three or more attempts, first expiring the matching current attempt and then marking the occurrence DEAD_LETTER with cleared lease metadata.
	// queryWorkersScheduleOccurrenceExpireDeadLetter содержит ограниченную выборку SKIP LOCKED просроченных CLAIMED экземпляров с тремя и более попытками, сначала завершая соответствующую текущую попытку как EXPIRED, затем переводя экземпляр в DEAD_LETTER с очисткой аренды.
	//go:embed sql/workers_schedule_occurrence_expire_dead_letter.sql
	queryWorkersScheduleOccurrenceExpireDeadLetter string
	// queryWorkersClaimintegrationtestsExpireStaleTestLeases embeds expiry requeue only for CLAIMED OWNER_TEST tasks of the exact tenant/claimed workload, clearing lease metadata and advancing attempt/version; managed-refresh tasks follow a separate path.
	// queryWorkersClaimintegrationtestsExpireStaleTestLeases содержит возврат просроченных CLAIMED задач OWNER_TEST точных организации и захватившего исполнителя в очередь с очисткой аренды и увеличением попытки и версии; управляемое обновление проверок идёт отдельным путём.
	//go:embed sql/workers_claimintegrationtests_expire_stale_test_leases.sql
	queryWorkersClaimintegrationtestsExpireStaleTestLeases string
	// queryWorkersClaimintegrationtestsSelectIntegrationConnectionTestsOrganizationIdState embeds SKIP LOCKED DUE health-test selection for the exact adapter route with current connection/configuration/definition/credential snapshot matches and retry delay; it does not contact the integration.
	// queryWorkersClaimintegrationtestsSelectIntegrationConnectionTestsOrganizationIdState содержит выбор DUE проверок с SKIP LOCKED для точного маршрута адаптера при совпадении текущих подключения, конфигурации, определения и снимка реквизитов и с задержкой повторов; интеграцию не вызывает.
	//go:embed sql/workers_claimintegrationtests_select_integration_connection_tests_organization_id_state.sql
	queryWorkersClaimintegrationtestsSelectIntegrationConnectionTestsOrganizationIdState string
	// queryWorkersClaimintegrationtestsClaimTestLease embeds DUE-to-CLAIMED test transition with assigned current and claimed workload/lease/fence pins, generation, expiry and version/time advancement; snapshot eligibility is checked before this ID-only claim.
	// queryWorkersClaimintegrationtestsClaimTestLease содержит переход проверки DUE в CLAIMED с назначением текущих и захвативших исполнителя, аренды и ограждения, поколения, срока и обновлением версии и времени; допустимость снимка проверяют до захвата по ID.
	//go:embed sql/workers_claimintegrationtests_claim_test_lease.sql
	queryWorkersClaimintegrationtestsClaimTestLease string
	// queryWorkersCompleteintegrationtestSelectIntegrationConnectionTestsOrganizationIdRef embeds tenant/ref/claimed-workload health-test and connection locking, rechecking managed-refresh configuration/credential pins while OWNER_TEST uses its separate branch; returned lease freshness must still be validated.
	// queryWorkersCompleteintegrationtestSelectIntegrationConnectionTestsOrganizationIdRef содержит блокировку проверки и подключения по организации, ссылке и захватившему исполнителю с повторной проверкой конфигурации и реквизитов управляемого обновления, тогда как OWNER_TEST имеет отдельную ветку; свежесть возвращённой аренды всё ещё проверяют отдельно.
	//go:embed sql/workers_completeintegrationtest_select_integration_connection_tests_organization_id_ref.sql
	queryWorkersCompleteintegrationtestSelectIntegrationConnectionTestsOrganizationIdRef string
	// queryWorkersCompleteintegrationtestRequeueTransientHealth embeds transient OWNER_TEST requeue only for the matching CLAIMED lease/generation and unchanged ready managed-MCP connection snapshot, clearing lease pins and advancing attempt/version.
	// queryWorkersCompleteintegrationtestRequeueTransientHealth содержит возврат временно неуспешного OWNER_TEST в очередь лишь для совпавших CLAIMED аренды и поколения и неизменённого снимка подключения готового управляемого MCP с очисткой аренды и увеличением попытки и версии.
	//go:embed sql/workers_completeintegrationtest_requeue_transient_health.sql
	queryWorkersCompleteintegrationtestRequeueTransientHealth string
	// queryWorkersCompleteintegrationtestUpdateIntegrationConnectionTestsStateResultSummarySafeErrorCode embeds resolved test outcome/summary/error persistence with lease clearing, completion time and a 30-second retry_after only for failed managed refresh; allowed outcome and lease checks precede this write.
	// queryWorkersCompleteintegrationtestUpdateIntegrationConnectionTestsStateResultSummarySafeErrorCode содержит запись результата, краткого описания и ошибки разрешённой проверки с очисткой аренды, временем завершения и retry_after на 30 секунд лишь при неуспехе управляемого обновления; допустимый результат и аренду проверяют до записи.
	//go:embed sql/workers_completeintegrationtest_update_integration_connection_tests_state_result_summary_safe_error_code.sql
	queryWorkersCompleteintegrationtestUpdateIntegrationConnectionTestsStateResultSummarySafeErrorCode string
	// queryWorkersCompleteintegrationtestUpdateIntegrationConnectionsStateMaskedCredentialsStateLastTestSummary embeds enabled-connection health-result persistence, forcing NOT_CONFIGURED when no credential revision exists and advancing test/version/time metadata; it records a supplied result rather than performing the probe.
	// queryWorkersCompleteintegrationtestUpdateIntegrationConnectionsStateMaskedCredentialsStateLastTestSummary содержит запись результата проверки включённого подключения с принудительным NOT_CONFIGURED при отсутствии ревизии реквизитов и обновлением проверки, версии и времени; фиксирует переданный результат, а не выполняет проверку.
	//go:embed sql/workers_completeintegrationtest_update_integration_connections_state_masked_credentials_state_last_test_summary.sql
	queryWorkersCompleteintegrationtestUpdateIntegrationConnectionsStateMaskedCredentialsStateLastTestSummary string
	// queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef embeds locked running-node/connection/grant invocation resolution with exact tenant/run/node/capability and current definition/grant snapshot pins, allowing only configured adapter routes; runtime lease authority is separately verified.
	// queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef содержит разрешение вызова с блокировкой работающего узла, подключения и разрешения по точным организации, запуску, узлу, возможности и текущим привязкам определения и снимка разрешения лишь для настроенных маршрутов адаптера; полномочия аренды runtime проверяют отдельно.
	//go:embed sql/workers_resolveintegrationinvocation_select_runs_id_organization_id_ref.sql
	queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef string
	// queryWorkersResolveintegrationinvocationSelectApprovalScope embeds newest unrevoked/unexpired approval-scope locking with exact tenant/project/root/agent/connection/grant/version/schema/scope pins and remaining effect budget; lookup does not reserve that budget.
	// queryWorkersResolveintegrationinvocationSelectApprovalScope содержит блокировку самого нового неотозванного и действующего подтверждения с точными организацией, проектом, корнем, сотрудником, подключением, разрешением, версией, схемой и областью и оставшимся бюджетом действий; чтение бюджет не резервирует.
	//go:embed sql/workers_resolveintegrationinvocation_select_approval_scope.sql
	queryWorkersResolveintegrationinvocationSelectApprovalScope string
	// queryWorkersResolveintegrationinvocationReserveApprovalScope embeds one-effect reservation increment for a resolved unrevoked/unexpired approval scope below its maximum, without changing scope version or expiration.
	// queryWorkersResolveintegrationinvocationReserveApprovalScope содержит увеличение резерва на одно действие для разрешённой неотозванной и действующей области подтверждения ниже максимума, не меняя её версию или срок.
	//go:embed sql/workers_resolveintegrationinvocation_reserve_approval_scope.sql
	queryWorkersResolveintegrationinvocationReserveApprovalScope string
	// queryWorkersResolveintegrationinvocationInsertIntegrationInvocationsRefRunIdConnectionId embeds invocation insertion with exact intent/input/definition/grant/policy/effect pins and node-scoped idempotency replay only when intent digest, positive grant version and approval policy match.
	// queryWorkersResolveintegrationinvocationInsertIntegrationInvocationsRefRunIdConnectionId содержит создание вызова с точными намерением, вводом, определением, разрешением, политикой и действием и идемпотентный повтор в области узла лишь при совпадении хеша намерения, положительной версии разрешения и политики подтверждения.
	//go:embed sql/workers_resolveintegrationinvocation_insert_integration_invocations_ref_run_id_connection_id.sql
	queryWorkersResolveintegrationinvocationInsertIntegrationInvocationsRefRunIdConnectionId string
	// queryWorkersResolveintegrationinvocationSelectGate embeds gate-ref readback by an owner-resolved invocation ID; it does not independently constrain tenant, gate state or human decision eligibility.
	// queryWorkersResolveintegrationinvocationSelectGate содержит чтение ссылки шлюза по разрешённому владельцем ID вызова; самостоятельно организацию, состояние шлюза и допустимость человеческого решения не ограничивает.
	//go:embed sql/workers_resolveintegrationinvocation_select_gate.sql
	queryWorkersResolveintegrationinvocationSelectGate string
	// queryWorkersResolveintegrationinvocationInsertGateNode embeds a WAITING HUMAN_GATE node for an integration effect with fixed localized labels and OPEN/RESOLVE_GATE actions; the node records pending review, not approval.
	// queryWorkersResolveintegrationinvocationInsertGateNode содержит создание WAITING узла HUMAN_GATE действия интеграции с фиксированными локализованными подписями и действиями OPEN/RESOLVE_GATE; узел фиксирует ожидание рассмотрения, не одобрение.
	//go:embed sql/workers_resolveintegrationinvocation_insert_gate_node.sql
	queryWorkersResolveintegrationinvocationInsertGateNode string
	// queryWorkersResolveintegrationinvocationInsertGateEdge embeds a WAITING_FOR integration-gate edge with supplied resolved tenant/root/node endpoints; authorization and graph consistency are checked before insertion.
	// queryWorkersResolveintegrationinvocationInsertGateEdge содержит создание WAITING_FOR ребра шлюза интеграции с заданными разрешёнными организацией, корнем и узлами; полномочия и согласованность графа проверяют до вставки.
	//go:embed sql/workers_resolveintegrationinvocation_insert_gate_edge.sql
	queryWorkersResolveintegrationinvocationInsertGateEdge string
	// queryWorkersResolveintegrationinvocationInsertOwnerGate embeds an OPEN invocation-bound owner gate with optional project, fixed effect-review prompt and APPROVE/REJECT/CANCEL choices; it does not manufacture a human decision.
	// queryWorkersResolveintegrationinvocationInsertOwnerGate содержит создание OPEN шлюза владельца, связанного с вызовом, с необязательным проектом, фиксированным вопросом проверки действия и вариантами APPROVE/REJECT/CANCEL; решение человека не подделывает.
	//go:embed sql/workers_resolveintegrationinvocation_insert_owner_gate.sql
	queryWorkersResolveintegrationinvocationInsertOwnerGate string
	// queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman embeds RUNNING-to-WAITING_HUMAN transition across runs of a resolved root with version/time advancement; atomic gate creation and trusted root resolution belong to the enclosing transaction.
	// queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman содержит перевод RUNNING в WAITING_HUMAN у запусков разрешённого корня с обновлением версии и времени; атомарное создание шлюза и доверенное разрешение корня обеспечивает внешняя транзакция.
	//go:embed sql/workers_resolveintegrationinvocation_update_run_waiting_human.sql
	queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman string
	// queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases embeds exact tenant/workload expiry handling for RUNNING invocations, requeueing READ as READY but retaining writes as UNKNOWN_OUTCOME and clearing leases; it does not blindly retry uncertain external effects.
	// queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases содержит обработку истечения RUNNING вызовов точных организации и исполнителя, возвращая READ в READY, но оставляя записи как UNKNOWN_OUTCOME с очисткой аренды; неопределённые внешние действия вслепую не повторяет.
	//go:embed sql/workers_claimintegrationinvocations_expire_stale_invocation_leases.sql
	queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases string
	// queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState embeds SKIP LOCKED invocation eligibility with exact grant/definition/runtime-snapshot pins, deadline and policy-specific approval checks including mailbox gates; UNKNOWN_OUTCOME retry is limited to synthetic journal writes, not arbitrary uncertain external writes.
	// queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState содержит выбор допустимых вызовов с SKIP LOCKED при точных привязках разрешения, определения и снимка runtime, дедлайне и проверках подтверждения по политике, включая почтовые шлюзы; повтор UNKNOWN_OUTCOME ограничен синтетической записью журнала, не произвольными неопределёнными внешними записями.
	//go:embed sql/workers_claimintegrationinvocations_select_integration_invocations_organization_id_state.sql
	queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState string
	// queryWorkersClaimintegrationinvocationsClaimInvocationLease embeds expected-state invocation claim as RUNNING with new lease/fence/generation/workload/expiry and durable claimed-workload pins; the preceding locked selection supplies eligibility.
	// queryWorkersClaimintegrationinvocationsClaimInvocationLease содержит захват вызова ожидаемого состояния в RUNNING с новыми арендой, ограждением, поколением, исполнителем, сроком и сохранёнными привязками захватившего исполнителя; допустимость обеспечивает предварительная выборка с блокировкой.
	//go:embed sql/workers_claimintegrationinvocations_claim_invocation_lease.sql
	queryWorkersClaimintegrationinvocationsClaimInvocationLease string
	// queryWorkersGetintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef embeds tenant/ref invocation status, safe result/error, optional gate and effect-receipt refs without locking; status readback does not execute or retry the effect.
	// queryWorkersGetintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef содержит чтение статуса вызова организации по ссылке, безопасных результата и ошибки и необязательных ссылок шлюза и квитанции действия без блокировки; действие не выполняет и не повторяет.
	//go:embed sql/workers_getintegrationinvocation_select_integration_invocations_organization_id_ref.sql
	queryWorkersGetintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef string
	// queryWorkersCompleteintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef embeds exact tenant/ref/claimed-workload invocation locking with positive grant version, owned project-or-organization assistant scope and optional receipt pins; fence/generation/expiry validation remains with completion logic.
	// queryWorkersCompleteintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef содержит блокировку вызова по точным организации, ссылке и захватившему исполнителю при положительной версии разрешения, принадлежащей области проекта либо организационного помощника и необязательных привязках квитанции; ограждение, поколение и срок проверяет логика завершения.
	//go:embed sql/workers_completeintegrationinvocation_select_integration_invocations_organization_id_ref.sql
	queryWorkersCompleteintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef string
	// queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode embeds resolved invocation outcome/result/error/receipt assignment with lease clearing and version/time advancement; exact receipt/lease ownership and allowed outcome must be checked before this ID-only update.
	// queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode содержит назначение результата, описания, ошибки и квитанции разрешённого вызова с очисткой аренды и обновлением версии и времени; точную принадлежность квитанции и аренды и допустимый результат проверяют до обновления по ID.
	//go:embed sql/workers_completeintegrationinvocation_update_integration_invocations_state_result_summary_safe_error_code.sql
	queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode string
	// queryWorkersCompleteintegrationinvocationInsertEffectReceipt embeds durable effect-receipt metadata with exact invocation/effect/input/provider-result pins and returns its ID/ref; this records an observed effect and does not itself send, approve or guarantee exactly-once delivery.
	// queryWorkersCompleteintegrationinvocationInsertEffectReceipt содержит устойчивую запись метаданных квитанции с точными привязками вызова, действия, ввода и результата провайдера и возвратом ID и ссылки; фиксирует наблюдённое действие, не отправляет, не одобряет и не гарантирует доставку ровно один раз.
	//go:embed sql/workers_completeintegrationinvocation_insert_effect_receipt.sql
	queryWorkersCompleteintegrationinvocationInsertEffectReceipt string
)
