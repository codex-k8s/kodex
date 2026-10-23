package platform

import _ "embed"

var (
	// queryRuntimeClaimexecutionExpireStaleLeases embeds tenant-scoped expiry of overdue CLAIMED leases and requeues their nodes only while run and root remain active, returning expired lease/run references; grant revocation and events require the owner transaction.
	// queryRuntimeClaimexecutionExpireStaleLeases содержит истечение просроченных CLAIMED аренд организации и возврат узлов в очередь лишь при активных запуске и корне с возвратом ссылок аренд и запусков; отзыв разрешений и события обеспечивает транзакция владельца.
	//go:embed sql/runtime_claimexecution_expire_stale_leases.sql
	queryRuntimeClaimexecutionExpireStaleLeases string
	// queryRuntimeClaimExecutionSelectClaimableAgentExecutions embeds SKIP LOCKED queue selection with pinned credentials/configuration/admitted image, session ordering, workflow and artifact-snapshot metadata, and capacity prefilters; atomic account locking/recounting and grant creation remain separate owner steps.
	// queryRuntimeClaimExecutionSelectClaimableAgentExecutions содержит выбор очереди с SKIP LOCKED, привязками реквизитов, конфигурации и допущенного образа, порядком сессии, метаданными процесса и снимков файлов и предварительными фильтрами лимитов; атомарная блокировка аккаунта, повторный подсчёт и создание разрешений выполняются отдельно.
	//go:embed sql/runtime_claimexecution_select_claimable_agent_executions.sql
	queryRuntimeClaimExecutionSelectClaimableAgentExecutions string
	// queryRuntimeClaimexecutionLockProviderAccount embeds exact tenant/account AUTHORIZED-and-enabled row locking and concurrency-limit retrieval; the lock serializes account admission, but this statement does not count leases or claim work.
	// queryRuntimeClaimexecutionLockProviderAccount содержит блокировку точного AUTHORIZED и включённого аккаунта организации с чтением лимита параллельности; блокировка сериализует допуск аккаунта, но запрос не считает аренды и не захватывает работу.
	//go:embed sql/runtime_claimexecution_lock_provider_account.sql
	queryRuntimeClaimexecutionLockProviderAccount string
	// queryRuntimeClaimexecutionCountActiveProviderLeases embeds the server-owned active-execution count for the exact account and organization; safe admission still requires holding the account lock across recount and lease insertion.
	// queryRuntimeClaimexecutionCountActiveProviderLeases содержит серверный подсчёт активных исполнений точного аккаунта и организации; безопасный допуск требует удерживать блокировку аккаунта между повторным подсчётом и созданием аренды.
	//go:embed sql/runtime_claimexecution_count_active_provider_leases.sql
	queryRuntimeClaimexecutionCountActiveProviderLeases string
	// queryRuntimeClaimExecutionInsertRuntimeRevision embeds immutable runtime-revision insertion with exact execution/attempt/generation, credential/configuration/image/resource digests and safe snapshot; the caller validates all pins and computes the digest before writing.
	// queryRuntimeClaimExecutionInsertRuntimeRevision содержит запись неизменяемой ревизии runtime с точными исполнением, попыткой, поколением, хешами реквизитов, конфигурации, образа и ресурсов и безопасным снимком; все привязки и расчёт хеша проверяет вызывающий код до записи.
	//go:embed sql/runtime_claimexecution_insert_runtime_revision.sql
	queryRuntimeClaimExecutionInsertRuntimeRevision string
	// queryRuntimeClaimexecutionSelectAssistantContext embeds assistant-context projection from the run actor and ACTIVE conversation/project visibility, with server-derived profile/integration operations; non-assistant runs yield empty JSON, not additional authority.
	// queryRuntimeClaimexecutionSelectAssistantContext содержит проекцию контекста помощника по автору запуска и видимости ACTIVE диалога и проекта с серверными операциями профилей и интеграций; для иных запусков возвращает пустой JSON, а не дополнительные полномочия.
	//go:embed sql/runtime_claimexecution_select_assistant_context.sql
	queryRuntimeClaimexecutionSelectAssistantContext string
	// queryRuntimeClaimexecutionInsertRuntimeLeasesRefRunIdWorkloadInstance embeds CLAIMED lease creation with resolved runtime revision, workload, fence, generation, input digest and expiry; tenant eligibility, capacity and immutable revision validation precede insertion.
	// queryRuntimeClaimexecutionInsertRuntimeLeasesRefRunIdWorkloadInstance содержит создание CLAIMED аренды с разрешёнными ревизией runtime, исполнителем, ограждением, поколением, хешем ввода и сроком; организацию, лимит и неизменяемую ревизию проверяют до записи.
	//go:embed sql/runtime_claimexecution_insert_runtime_leases_ref_run_id_workload_instance.sql
	queryRuntimeClaimexecutionInsertRuntimeLeasesRefRunIdWorkloadInstance string
	// queryRuntimeClaimexecutionUpdateRunNodesStateStartedAtVersion embeds QUEUED-turn startup and RUNNING-node assignment with preserved first start times and node version advancement; the node write is ID-only, so claim/state eligibility must already be established.
	// queryRuntimeClaimexecutionUpdateRunNodesStateStartedAtVersion содержит запуск QUEUED хода и назначение RUNNING узлу с сохранением первого времени начала и увеличением версии узла; запись узла идёт только по ID, поэтому допустимость состояния и захвата проверяют заранее.
	//go:embed sql/runtime_claimexecution_update_run_nodes_state_started_at_version.sql
	queryRuntimeClaimexecutionUpdateRunNodesStateStartedAtVersion string
	// queryRuntimeReadexecutionartifactSelectArtifactContent embeds version/digest-pinned artifact-content metadata resolution through a current fenced lease, runtime snapshot or file catalog and CLEAN lifecycle checks; it returns object descriptors, not bytes or a new antivirus result.
	// queryRuntimeReadexecutionartifactSelectArtifactContent содержит разрешение метаданных содержимого файла по привязанным версии и хешу через действующую ограждённую аренду, снимок runtime либо файловый каталог и проверки CLEAN и жизненного цикла; возвращает описание объекта, не байты и не новый результат антивируса.
	//go:embed sql/runtime_readexecutionartifact_select_artifact_content.sql
	queryRuntimeReadexecutionartifactSelectArtifactContent string
	// queryRuntimeLeaseSelectRuntimeLeasesOrganizationIdRef embeds exact tenant/ref lease and run/node/session metadata lookup without locking or state/expiry filtering; the command must validate the returned fence, generation and lifecycle.
	// queryRuntimeLeaseSelectRuntimeLeasesOrganizationIdRef содержит чтение аренды и метаданных запуска, узла и сессии по точным организации и ссылке без блокировки и фильтра состояния или срока; ограждение, поколение и жизненный цикл проверяет команда.
	//go:embed sql/runtime_lease_select_runtime_leases_organization_id_ref.sql
	queryRuntimeLeaseSelectRuntimeLeasesOrganizationIdRef string
	// queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef embeds the tenant/ref lease projection while locking only its lease row; a returned row alone does not prove an unexpired CLAIMED state or a matching caller fence.
	// queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef содержит чтение аренды по организации и ссылке с блокировкой только её строки; наличие строки не доказывает действующий CLAIMED или совпадение ограждения вызывающего.
	//go:embed sql/runtime_lease_for_update_select_runtime_leases_organization_id_ref.sql
	queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef string
	// queryRuntimeRenewexecutionUpdateRuntimeLeasesExpiresAtUpdatedAt embeds expiry/time replacement by resolved lease ID; ownership, current generation/fence, deadline and permitted renewal duration are checked before this ID-only write.
	// queryRuntimeRenewexecutionUpdateRuntimeLeasesExpiresAtUpdatedAt содержит замену срока и времени обновления по разрешённому ID аренды; владение, текущее поколение и ограждение, дедлайн и допустимую длительность продления проверяют до записи по ID.
	//go:embed sql/runtime_renewexecution_update_runtime_leases_expires_at_updated_at.sql
	queryRuntimeRenewexecutionUpdateRuntimeLeasesExpiresAtUpdatedAt string
	// queryRuntimeReportprogressUpdateRunNodesProgressSummaryVersion embeds progress-summary replacement and version advancement by resolved node ID; caller eligibility and progress sanitization are outside this statement.
	// queryRuntimeReportprogressUpdateRunNodesProgressSummaryVersion содержит замену краткого прогресса и увеличение версии по разрешённому ID узла; полномочия вызывающего и очистка текста прогресса проверяются вне запроса.
	//go:embed sql/runtime_reportprogress_update_run_nodes_progress_summary_version.sql
	queryRuntimeReportprogressUpdateRunNodesProgressSummaryVersion string
	// queryRuntimeCommitprovidercredentialrefreshLockProviderAccount embeds exact tenant/runtime-revision account locking for an enabled AUTHORIZED account or a DELETING account; it permits fenced credential reconciliation during deletion rather than granting new execution access.
	// queryRuntimeCommitprovidercredentialrefreshLockProviderAccount содержит блокировку аккаунта точной организации и ревизии runtime для включённого AUTHORIZED либо DELETING аккаунта; допускает согласование ограждённого обновления реквизитов при удалении, не выдавая новый доступ к исполнению.
	//go:embed sql/runtime_commitprovidercredentialrefresh_lock_provider_account.sql
	queryRuntimeCommitprovidercredentialrefreshLockProviderAccount string
	// queryRuntimeCommitprovidercredentialrefreshSelectPinnedCredential embeds the credential revision pinned by the exact tenant/runtime revision and account, returning references and content hash only; it does not read secret bytes or require that pin to be current.
	// queryRuntimeCommitprovidercredentialrefreshSelectPinnedCredential содержит чтение ревизии реквизитов, закреплённой точной организацией, ревизией runtime и аккаунтом, с возвратом ссылок и хеша; секретные байты не читает и текущую активность привязки не требует.
	//go:embed sql/runtime_commitprovidercredentialrefresh_select_pinned_credential.sql
	queryRuntimeCommitprovidercredentialrefreshSelectPinnedCredential string
	// queryRuntimeCommitprovidercredentialrefreshSelectExistingRevision embeds an existing credential-revision lookup by resolved account and exact secret UID/resource-version, for refresh deduplication; account ownership is established beforehand.
	// queryRuntimeCommitprovidercredentialrefreshSelectExistingRevision содержит поиск прежней ревизии реквизитов разрешённого аккаунта по точным UID и версии ресурса секрета для устранения повторов обновления; принадлежность аккаунта проверяют заранее.
	//go:embed sql/runtime_commitprovidercredentialrefresh_select_existing_revision.sql
	queryRuntimeCommitprovidercredentialrefreshSelectExistingRevision string
	// queryRuntimeCommitprovidercredentialrefreshInsertRevision embeds tenant/account credential-revision metadata insertion with max-plus-one numbering and observed time; holding the account lock is required to serialize allocation, and no raw credentials are stored.
	// queryRuntimeCommitprovidercredentialrefreshInsertRevision содержит вставку метаданных ревизии реквизитов аккаунта организации со следующим после максимального номером и временем наблюдения; распределение номеров требует блокировки аккаунта, исходные реквизиты не сохраняются.
	//go:embed sql/runtime_commitprovidercredentialrefresh_insert_revision.sql
	queryRuntimeCommitprovidercredentialrefreshInsertRevision string
	// queryRuntimeCommitprovidercredentialrefreshActivateRevision embeds expected-current credential-pointer activation for an enabled AUTHORIZED or DELETING resolved account, advancing version/time; it does not overwrite a concurrently changed credential pin.
	// queryRuntimeCommitprovidercredentialrefreshActivateRevision содержит активацию указателя реквизитов разрешённого включённого AUTHORIZED либо DELETING аккаунта лишь при ожидаемой текущей привязке с обновлением версии и времени; изменённую конкурентно привязку не перезаписывает.
	//go:embed sql/runtime_commitprovidercredentialrefresh_activate_revision.sql
	queryRuntimeCommitprovidercredentialrefreshActivateRevision string
	// queryRuntimeCompleteexecutionUpdateRuntimeLeasesStateUpdatedAt embeds COMPLETED marking and update time by resolved lease ID; full graph completion and generation/fence checks belong to the enclosing command.
	// queryRuntimeCompleteexecutionUpdateRuntimeLeasesStateUpdatedAt содержит отметку COMPLETED и времени обновления по разрешённому ID аренды; завершение всего графа и проверки поколения и ограждения обеспечивает внешняя команда.
	//go:embed sql/runtime_completeexecution_update_runtime_leases_state_updated_at.sql
	queryRuntimeCompleteexecutionUpdateRuntimeLeasesStateUpdatedAt string
	// queryRuntimeCompleteexecutionUpdateRunNodesStateProgressSummarySafeErrorCode embeds node outcome/progress/error persistence with finish time, version advancement and FAILED-only RETRY action, returning gate/turn metadata; allowed transitions must already be checked.
	// queryRuntimeCompleteexecutionUpdateRunNodesStateProgressSummarySafeErrorCode содержит запись результата, прогресса и ошибок узла с временем завершения, увеличением версии и действием RETRY только для FAILED, возвращая признаки шлюза и хода; допустимые переходы проверяют заранее.
	//go:embed sql/runtime_completeexecution_update_run_nodes_state_progress_summary_safe_error_code.sql
	queryRuntimeCompleteexecutionUpdateRunNodesStateProgressSummarySafeErrorCode string
	// queryRuntimeCompleteexecutionUpdateSessionTurnsStateCompletedAt embeds supplied turn-state and completion-time assignment by resolved turn ID, without independently constraining the prior state or tenant.
	// queryRuntimeCompleteexecutionUpdateSessionTurnsStateCompletedAt содержит назначение заданного состояния и времени завершения по разрешённому ID хода без самостоятельного ограничения прежнего состояния или организации.
	//go:embed sql/runtime_completeexecution_update_session_turns_state_completed_at.sql
	queryRuntimeCompleteexecutionUpdateSessionTurnsStateCompletedAt string
	// queryRuntimeCompleteexecutionSelectRunsId embeds session ID and target-kind lookup by resolved run ID for completion routing; the ID-only read relies on earlier owner resolution.
	// queryRuntimeCompleteexecutionSelectRunsId содержит чтение ID сессии и вида цели по разрешённому ID запуска для выбора пути завершения; чтение по ID полагается на предварительное разрешение владельцем данных.
	//go:embed sql/runtime_completeexecution_select_runs_id.sql
	queryRuntimeCompleteexecutionSelectRunsId string
	// queryRuntimeCompleteexecutionUpsertSessionStorage embeds LIVE session-storage upsert bound to the exact tenant/session/provider runtime revision, increasing content generation and superseding an AVAILABLE current archive with retained lifetime; object archiving itself is separate.
	// queryRuntimeCompleteexecutionUpsertSessionStorage содержит создание или обновление LIVE хранилища сессии, связанного с точными организацией, сессией, провайдером и ревизией runtime, увеличивая поколение содержимого и переводя текущий AVAILABLE архив в SUPERSEDED с сохранением срока; само архивирование объектов выполняется отдельно.
	//go:embed sql/runtime_completeexecution_upsert_session_storage.sql
	queryRuntimeCompleteexecutionUpsertSessionStorage string
	// queryRuntimeCompleteexecutionSelectAgentCapability embeds capability intersection between current agent and pinned runtime snapshot plus active root-actor resolution on the exact tenant/node/lease; this read does not replace lease freshness validation.
	// queryRuntimeCompleteexecutionSelectAgentCapability содержит пересечение возможностей текущего сотрудника и закреплённого снимка runtime с разрешением активного автора корня для точных организации, узла и аренды; проверку свежести аренды этим чтением не заменяют.
	//go:embed sql/runtime_completeexecution_select_agent_capability.sql
	queryRuntimeCompleteexecutionSelectAgentCapability string
	// queryRuntimeCompleteexecutionLockRootRun embeds state lookup and row locking by resolved root-run ID; tenant, graph-root identity and completion eligibility must be established before this ID-only read.
	// queryRuntimeCompleteexecutionLockRootRun содержит чтение состояния и блокировку по разрешённому ID корневого запуска; организацию, принадлежность корню и допустимость завершения проверяют до чтения по ID.
	//go:embed sql/runtime_completeexecution_lock_root_run.sql
	queryRuntimeCompleteexecutionLockRootRun string
	// queryRuntimeCompleteexecutionMarkProviderReauthorizationRequired embeds REAUTHORIZATION_REQUIRED marking only while an AUTHORIZED account still points to the credential pinned by the exact tenant/runtime revision, preserving a newer credential revision.
	// queryRuntimeCompleteexecutionMarkProviderReauthorizationRequired содержит отметку REAUTHORIZATION_REQUIRED лишь пока AUTHORIZED аккаунт указывает на реквизиты точной организации и ревизии runtime, не затрагивая новую ревизию реквизитов.
	//go:embed sql/runtime_completeexecution_mark_provider_reauthorization_required.sql
	queryRuntimeCompleteexecutionMarkProviderReauthorizationRequired string
	// queryRuntimeCompleteexecutionUpdateRunUsage embeds per-turn usage replacement and recomputation of token totals and maximum context window for one resolved run; repeated turn keys replace observations rather than double-adding them.
	// queryRuntimeCompleteexecutionUpdateRunUsage содержит замену потребления по ключу хода и пересчёт сумм токенов и максимального окна контекста разрешённого запуска; повторный ключ заменяет наблюдение, а не прибавляет его повторно.
	//go:embed sql/runtime_completeexecution_update_run_usage.sql
	queryRuntimeCompleteexecutionUpdateRunUsage string
	// queryRuntimeCompleteexecutionUpdateRootUsage embeds root token aggregation from all runs in the resolved root graph while preserving the root's own turn map and maximum context window; it records usage, not billing or provider balance.
	// queryRuntimeCompleteexecutionUpdateRootUsage содержит агрегирование токенов из всех запусков разрешённого корневого графа с сохранением собственной карты ходов корня и максимального окна контекста; фиксирует потребление, не оплату и не баланс провайдера.
	//go:embed sql/runtime_completeexecution_update_root_usage.sql
	queryRuntimeCompleteexecutionUpdateRootUsage string
	// queryRuntimeCompleteexecutionInsertArtifactsRefProjectIdNodeId embeds AGENT_RESULT artifact metadata insertion with creator derived from the tenant/run and next file-name revision in project or creator scope; supplied scan/object-receipt fields are validated outside SQL.
	// queryRuntimeCompleteexecutionInsertArtifactsRefProjectIdNodeId содержит создание метаданных AGENT_RESULT файла с автором из запуска организации и следующей ревизией имени в области проекта либо автора; заданные поля проверки и квитанции объекта проверяют вне SQL.
	//go:embed sql/runtime_completeexecution_insert_artifacts_ref_project_id_node_id.sql
	queryRuntimeCompleteexecutionInsertArtifactsRefProjectIdNodeId string
	// queryRuntimeCompleteexecutionInsertArtifactContentArtifactId embeds artifact object-version/ETag/digest/size and antivirus-report metadata insertion by resolved artifact ID; it neither uploads content nor performs antivirus scanning.
	// queryRuntimeCompleteexecutionInsertArtifactContentArtifactId содержит запись версии объекта, ETag, хеша, размера и отчёта антивируса по разрешённому ID файла; содержимое не загружает и антивирусную проверку не выполняет.
	//go:embed sql/runtime_completeexecution_insert_artifact_content_artifact_id.sql
	queryRuntimeCompleteexecutionInsertArtifactContentArtifactId string
	// queryRuntimeCompleteexecutionInsertArtifactBindingsArtifactIdTargetRef embeds RUN_RESULT binding insertion for a resolved artifact/target/creator tuple; target ownership and publication policy are checked before the write.
	// queryRuntimeCompleteexecutionInsertArtifactBindingsArtifactIdTargetRef содержит создание привязки RUN_RESULT разрешённых файла, цели и автора; принадлежность цели и правила публикации проверяют до записи.
	//go:embed sql/runtime_completeexecution_insert_artifact_bindings_artifact_id_target_ref.sql
	queryRuntimeCompleteexecutionInsertArtifactBindingsArtifactIdTargetRef string
	// queryRuntimeCompleteexecutionUpdateCurrentRunOutcome embeds non-root outcome persistence, retaining RUNNING and no finish time for proposed success with an OPEN required workflow; this does not independently decide full graph completion.
	// queryRuntimeCompleteexecutionUpdateCurrentRunOutcome содержит запись результата некорневого запуска, оставляя RUNNING без времени завершения при предложенном успехе и OPEN обязательном процессе; завершение всего графа самостоятельно не определяет.
	//go:embed sql/runtime_completeexecution_update_current_run_outcome.sql
	queryRuntimeCompleteexecutionUpdateCurrentRunOutcome string
	// queryRuntimeCompleteexecutionSelectRunEdgesRootRunIdSourceNodeIdType embeds first CALLBACK_TO edge/target lookup for the resolved root and source node in creation order; LIMIT 1 does not itself prove delegation lineage.
	// queryRuntimeCompleteexecutionSelectRunEdgesRootRunIdSourceNodeIdType содержит выбор первого CALLBACK_TO ребра и цели разрешённых корня и исходного узла по времени создания; LIMIT 1 сам не доказывает происхождение делегирования.
	//go:embed sql/runtime_completeexecution_select_run_edges_root_run_id_source_node_id_type.sql
	queryRuntimeCompleteexecutionSelectRunEdgesRootRunIdSourceNodeIdType string
	// queryRuntimeCompleteexecutionInsertCallbackReceiptsChildRunId embeds idempotent terminal-child callback receipt creation with exact delegation/runtime-attempt or required-workflow provenance and at most 16 matched direct artifact refs plus received pins; incomplete nested snapshots are rejected.
	// queryRuntimeCompleteexecutionInsertCallbackReceiptsChildRunId содержит идемпотентное создание квитанции завершённого дочернего запуска с точным происхождением делегирования и попытки runtime либо обязательного процесса, максимум 16 совпавшими прямыми файлами и полученными привязками; неполные вложенные снимки отклоняет.
	//go:embed sql/runtime_completeexecution_insert_callback_receipts_child_run_id.sql
	queryRuntimeCompleteexecutionInsertCallbackReceiptsChildRunId string
	// queryRuntimeCompleteexecutionUpdateRunNodesCallbackSummaryVersion embeds callback-summary replacement and node-version advancement by resolved node ID; receipt provenance and safe summary construction are verified before execution.
	// queryRuntimeCompleteexecutionUpdateRunNodesCallbackSummaryVersion содержит замену краткого результата обратного вызова и увеличение версии по разрешённому ID узла; происхождение квитанции и безопасное формирование текста проверяют до исполнения.
	//go:embed sql/runtime_completeexecution_update_run_nodes_callback_summary_version.sql
	queryRuntimeCompleteexecutionUpdateRunNodesCallbackSummaryVersion string
	// queryRuntimeCompleteexecutionSelectSessionsId embeds next-turn-number lookup with session-row locking by resolved ID for assistant completion history; it does not allocate the number without the enclosing insert/update transaction.
	// queryRuntimeCompleteexecutionSelectSessionsId содержит чтение следующего номера хода с блокировкой сессии по разрешённому ID для истории завершения помощника; без общей транзакции вставки и обновления номер не распределяет.
	//go:embed sql/runtime_completeexecution_select_sessions_id.sql
	queryRuntimeCompleteexecutionSelectSessionsId string
	// queryRuntimeCompleteexecutionInsertSessionTurnsRefSessionIdTurnNumber embeds assistant completion-turn insertion using the tenant's system-assistant ref and supplied state/content, recording completion time; project-assistant identity is not selected by this SQL.
	// queryRuntimeCompleteexecutionInsertSessionTurnsRefSessionIdTurnNumber содержит вставку завершённого хода с системным помощником организации, заданными состоянием и текстом и временем завершения; личность проектного помощника этот SQL не выбирает.
	//go:embed sql/runtime_completeexecution_insert_session_turns_ref_session_id_turn_number.sql
	queryRuntimeCompleteexecutionInsertSessionTurnsRefSessionIdTurnNumber string
	// queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt embeds next-turn-number/version/time advancement by resolved session ID after completion-history insertion; locking and atomic ordering are supplied by the owner transaction.
	// queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt содержит увеличение следующего номера хода и версии с обновлением времени разрешённой сессии после записи истории завершения; блокировку и атомарный порядок обеспечивает транзакция владельца.
	//go:embed sql/runtime_completeexecution_update_sessions_next_turn_number_version_updated_at.sql
	queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt string
	// queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt embeds conversation version/time advancement for a resolved session ID; it contains no independent tenant, conversation-state or expected-version predicate.
	// queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt содержит увеличение версии и обновление времени диалогов разрешённой сессии; отдельных условий организации, состояния диалога или ожидаемой версии не содержит.
	//go:embed sql/runtime_completeexecution_update_assistant_conversations_version_updated_at.sql
	queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt string
	// queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId embeds WAITING HUMAN_GATE node creation with localized display keys and OPEN/RESOLVE_GATE actions; creating a waiting node is not human approval.
	// queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId содержит создание WAITING узла HUMAN_GATE с ключами локализованного отображения и действиями OPEN/RESOLVE_GATE; создание ожидающего узла не является одобрением человека.
	//go:embed sql/runtime_completeexecution_insert_run_nodes_ref_root_run_id_parent_node_id.sql
	queryRuntimeCompleteexecutionInsertRunNodesRefRootRunIdParentNodeId string
	// queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId embeds a WAITING_FOR edge between owner-resolved nodes in one root graph; dependency eligibility and graph authority are validated before insertion.
	// queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId содержит создание WAITING_FOR ребра между разрешёнными владельцем узлами корневого графа; допустимость зависимости и полномочия на граф проверяют до вставки.
	//go:embed sql/runtime_completeexecution_insert_run_edges_ref_root_run_id_target_node_id.sql
	queryRuntimeCompleteexecutionInsertRunEdgesRefRootRunIdTargetNodeId string
	// queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId embeds an OPEN review gate with fixed localized prompt/title and APPROVE/REJECT/REQUEST_CHANGES/CANCEL choices; the server creates a request, not a decision.
	// queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId содержит создание OPEN шлюза проверки с фиксированными локализованными заголовком и вопросом и вариантами APPROVE/REJECT/REQUEST_CHANGES/CANCEL; сервер создаёт запрос, не решение.
	//go:embed sql/runtime_completeexecution_insert_owner_gates_ref_project_id_node_id.sql
	queryRuntimeCompleteexecutionInsertOwnerGatesRefProjectIdNodeId string
	// queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt embeds WAITING_HUMAN assignment with version/time advancement by resolved run ID; open-gate creation and graph invariants belong to the enclosing owner transaction.
	// queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt содержит назначение WAITING_HUMAN и обновление версии и времени по разрешённому ID запуска; создание открытого шлюза и инварианты графа обеспечивает внешняя транзакция владельца.
	//go:embed sql/runtime_completeexecution_update_runs_state_version_updated_at.sql
	queryRuntimeCompleteexecutionUpdateRunsStateVersionUpdatedAt string
	// queryRuntimeCompleteexecutionFailRootRun embeds FAILED root outcome/error/finish-time persistence with version advancement by resolved ID; it does not close sibling work, leases or external effects itself.
	// queryRuntimeCompleteexecutionFailRootRun содержит запись FAILED результата, ошибки и времени завершения корня с увеличением версии по разрешённому ID; другие работы, аренды и внешние действия самостоятельно не закрывает.
	//go:embed sql/runtime_completeexecution_fail_root_run.sql
	queryRuntimeCompleteexecutionFailRootRun string
	// queryRuntimeCompleteexecutionSelectRunNodesRootRunIdType embeds separate counts of active execution nodes/open gates/required workflows and still-PLANNED execution nodes under a resolved root; those categories are deliberately not interchangeable.
	// queryRuntimeCompleteexecutionSelectRunNodesRootRunIdType содержит раздельные количества активных узлов исполнения, открытых шлюзов и обязательных процессов и ещё PLANNED узлов разрешённого корня; эти категории намеренно не смешиваются.
	//go:embed sql/runtime_completeexecution_select_run_nodes_root_run_id_type.sql
	queryRuntimeCompleteexecutionSelectRunNodesRootRunIdType string
	// queryRuntimeCompleteexecutionCloseTerminalGraph embeds graph-scoped cancellation of remaining runs/nodes/leases/turns/gates and integration effects, preserving UNKNOWN_OUTCOME for running writes or claimed deliveries; returned refs drive separate audit/events rather than proving external cancellation.
	// queryRuntimeCompleteexecutionCloseTerminalGraph содержит отмену оставшихся запусков, узлов, аренд, ходов, шлюзов и интеграционных действий графа, сохраняя UNKNOWN_OUTCOME для начатых записей и захваченных доставок; возвращённые ссылки нужны отдельным аудиту и событиям, не доказывают внешнюю отмену.
	//go:embed sql/runtime_completeexecution_close_terminal_graph.sql
	queryRuntimeCompleteexecutionCloseTerminalGraph string
	// queryRuntimeCompleteexecutionUpdateRunsStateResultSummaryFinishedAt embeds SUCCEEDED root summary/finish-time persistence and version/time advancement by resolved ID; active, planned and owner-gate completion checks must precede this write.
	// queryRuntimeCompleteexecutionUpdateRunsStateResultSummaryFinishedAt содержит запись SUCCEEDED результата и времени завершения корня с обновлением версии и времени по разрешённому ID; активные, запланированные работы и шлюзы владельца проверяют до записи.
	//go:embed sql/runtime_completeexecution_update_runs_state_result_summary_finished_at.sql
	queryRuntimeCompleteexecutionUpdateRunsStateResultSummaryFinishedAt string
	// queryRuntimeCompleteexecutionUpdateRunNodesStateFinishedAtVersion embeds root-process node state/finish/version assignment for a resolved root, returning the affected ref; valid terminal state and node cardinality remain caller invariants.
	// queryRuntimeCompleteexecutionUpdateRunNodesStateFinishedAtVersion содержит назначение состояния, времени завершения и версии узлу корневого процесса по разрешённому корню с возвратом ссылки; допустимое конечное состояние и количество узлов обеспечивает вызывающий код.
	//go:embed sql/runtime_completeexecution_update_run_nodes_state_finished_at_version.sql
	queryRuntimeCompleteexecutionUpdateRunNodesStateFinishedAtVersion string
	// queryRuntimeCompleteexecutionUpdateScheduleOccurrencesStateLeaseRefFenceDigest embeds supplied outcome assignment only to MATERIALIZED occurrences of the resolved run, clearing lease/fence/workload metadata and returning schedule ID for separate completion bookkeeping.
	// queryRuntimeCompleteexecutionUpdateScheduleOccurrencesStateLeaseRefFenceDigest содержит назначение заданного результата только MATERIALIZED экземплярам разрешённого запуска с очисткой аренды, ограждения и исполнителя и возвратом ID расписания для отдельного учёта завершения.
	//go:embed sql/runtime_completeexecution_update_schedule_occurrences_state_lease_ref_fence_digest.sql
	queryRuntimeCompleteexecutionUpdateScheduleOccurrencesStateLeaseRefFenceDigest string
	// queryRuntimeCompleteexecutionUpdateSchedulesLastRunAtUpdatedAt embeds last-run and update-time recording by resolved schedule ID without advancing its version or changing next_run_at.
	// queryRuntimeCompleteexecutionUpdateSchedulesLastRunAtUpdatedAt содержит запись времени последнего запуска и обновления по разрешённому ID расписания без увеличения версии и изменения next_run_at.
	//go:embed sql/runtime_completeexecution_update_schedules_last_run_at_updated_at.sql
	queryRuntimeCompleteexecutionUpdateSchedulesLastRunAtUpdatedAt string
	// queryRuntimeDelegateexecutionSelectRunNodesId embeds parent-node locking and separate capability, relationship and planned-dependency checks for delegation, with workflow-step metadata; returned booleans must all be evaluated rather than treating a row as permission.
	// queryRuntimeDelegateexecutionSelectRunNodesId содержит блокировку родительского узла и отдельные проверки возможности, связи и запланированных зависимостей для делегирования с метаданными шага процесса; нужно разобрать все возвращённые признаки, а не считать строку разрешением.
	//go:embed sql/runtime_delegateexecution_select_run_nodes_id.sql
	queryRuntimeDelegateexecutionSelectRunNodesId string
	// queryRuntimeDelegateexecutionSelectAgentsOrganizationIdProjectIdRef embeds exact tenant/project/ref READY-and-enabled target-agent resolution for delegation; caller authorization and root lineage are not established by the matching row.
	// queryRuntimeDelegateexecutionSelectAgentsOrganizationIdProjectIdRef содержит разрешение точного READY и включённого сотрудника организации и проекта по ссылке для делегирования; наличие строки не устанавливает полномочия вызывающего и происхождение корня.
	//go:embed sql/runtime_delegateexecution_select_agents_organization_id_project_id_ref.sql
	queryRuntimeDelegateexecutionSelectAgentsOrganizationIdProjectIdRef string
	// queryRuntimeDelegateexecutionSelectRunsId embeds parent/root locking with exact tenant/project and matching root workflow pins, returning inherited initiator, input and workflow spec; payload parent/root IDs alone are not provenance.
	// queryRuntimeDelegateexecutionSelectRunsId содержит блокировку родителя и корня с точными организацией, проектом и совпадающими привязками процесса корня, возвращая наследуемых автора, ввод и спецификацию; одни ID родителя и корня из запроса не доказывают происхождение.
	//go:embed sql/runtime_delegateexecution_select_runs_id.sql
	queryRuntimeDelegateexecutionSelectRunsId string
	// queryRuntimeDelegateexecutionInsertChildSession embeds ACTIVE delegated-agent session creation with exact tenant/project/target/account/creator pins; it creates isolated session metadata, not a provider concurrency reservation.
	// queryRuntimeDelegateexecutionInsertChildSession содержит создание ACTIVE сессии делегированного сотрудника с точными организацией, проектом, целью, аккаунтом и автором; создаёт изолированные метаданные сессии, не резерв параллельности провайдера.
	//go:embed sql/runtime_delegateexecution_insert_child_session.sql
	queryRuntimeDelegateexecutionInsertChildSession string
	// queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId embeds RUNNING delegated-run insertion under a tenant-resolved root, inheriting its workflow version and recording parent/session/initiator pins; runtime dispatch and trusted lineage validation are separate.
	// queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId содержит создание RUNNING делегированного запуска под корнем организации с наследованием версии процесса и записью родителя, сессии и автора; выдача исполнения runtime и проверка доверенного происхождения отдельные.
	//go:embed sql/runtime_delegateexecution_insert_runs_ref_project_id_root_run_id.sql
	queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId string
	// queryRuntimeDelegateexecutionSelectSessionsId embeds resolved child-session locking and next-turn-number lookup for delegation; tenant/target/account ownership is checked before this ID-only read.
	// queryRuntimeDelegateexecutionSelectSessionsId содержит блокировку разрешённой дочерней сессии и чтение следующего номера хода для делегирования; организацию и принадлежность цели и аккаунта проверяют до чтения по ID.
	//go:embed sql/runtime_delegateexecution_select_sessions_id.sql
	queryRuntimeDelegateexecutionSelectSessionsId string
	// queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber embeds QUEUED AGENT turn insertion with resolved session/run and supplied parent-agent identity/content, returning turn ID; the owner must derive that identity from the delegation boundary.
	// queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber содержит создание QUEUED хода AGENT с разрешёнными сессией и запуском, заданными родительским сотрудником и текстом и возвратом ID хода; личность сотрудника владелец обязан вывести из границы делегирования.
	//go:embed sql/runtime_delegateexecution_insert_session_turns_ref_session_id_turn_number.sql
	queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber string
	// queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt embeds child-session next-turn-number/version/time advancement after delegation turn creation; it relies on the same owner lock and transaction as insertion.
	// queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt содержит увеличение следующего номера хода и версии с обновлением времени дочерней сессии после создания хода делегирования; требует той же блокировки владельца и транзакции, что и вставка.
	//go:embed sql/runtime_delegateexecution_update_sessions_next_turn_number_version_updated_at.sql
	queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt string
	// queryRuntimeDelegateexecutionInsertRunNodesRefRootRunIdParentNodeId embeds QUEUED delegated execution-node insertion with exact parent/root/run/agent/turn/step pins and OPEN/CANCEL actions; lease and immutable runtime revision are created later.
	// queryRuntimeDelegateexecutionInsertRunNodesRefRootRunIdParentNodeId содержит создание QUEUED узла делегированного исполнения с точными родителем, корнем, запуском, сотрудником, ходом и шагом и действиями OPEN/CANCEL; аренду и неизменяемую ревизию runtime создают позднее.
	//go:embed sql/runtime_delegateexecution_insert_run_nodes_ref_root_run_id_parent_node_id.sql
	queryRuntimeDelegateexecutionInsertRunNodesRefRootRunIdParentNodeId string
	// queryRuntimeDelegateexecutionMaterializePlannedNode embeds single PLANNED-to-MATERIALIZED/QUEUED node binding with run/turn/input assignment and version advancement; dependency and workflow authority are checked before this guarded update.
	// queryRuntimeDelegateexecutionMaterializePlannedNode содержит привязку PLANNED узла в MATERIALIZED/QUEUED с назначением запуска, хода, ввода и увеличением версии; зависимости и полномочия процесса проверяют до этого ограниченного обновления.
	//go:embed sql/runtime_delegateexecution_materialize_planned_node.sql
	queryRuntimeDelegateexecutionMaterializePlannedNode string
	// queryRuntimeProposeassistantmetadataUpdateConversation embeds agent-proposed title assignment only for ACTIVE conversations whose title is not USER_EDITED, advancing title/aggregate revisions; a user's explicit title remains untouched.
	// queryRuntimeProposeassistantmetadataUpdateConversation содержит назначение предложенного агентом названия только ACTIVE диалогу без USER_EDITED с увеличением ревизии названия и общей версии; заданное пользователем название сохраняет.
	//go:embed sql/runtime_proposeassistantmetadata_update_conversation.sql
	queryRuntimeProposeassistantmetadataUpdateConversation string
	// queryRuntimeProposerunmetadataUpdateRun embeds run activity-summary replacement and version/time advancement, changing title/source only for a nonempty proposal on a non-USER_EDITED run.
	// queryRuntimeProposerunmetadataUpdateRun содержит замену краткой активности запуска и обновление версии и времени, меняя название и источник только при непустом предложении и отсутствии USER_EDITED.
	//go:embed sql/runtime_proposerunmetadata_update_run.sql
	queryRuntimeProposerunmetadataUpdateRun string
	// queryRuntimeRecordtoolcallSelectActorAndGrant embeds actor/system-or-project-assistant attribution and tool-specific grant checks against exact runtime generation, frozen file catalog or pinned integration snapshot; the returned eligibility flags are not a substitute for command-level lease validation.
	// queryRuntimeRecordtoolcallSelectActorAndGrant содержит определение автора, системного либо проектного помощника и проверок разрешений инструмента по точному поколению runtime, замороженному файловому каталогу либо снимку интеграций; возвращённые признаки не заменяют проверку аренды самой командой.
	//go:embed sql/runtime_recordtoolcall_select_actor_and_grant.sql
	queryRuntimeRecordtoolcallSelectActorAndGrant string
	// queryRuntimeRecordtoolcallUpdateEvent embeds TOOL_CALL_RECORDED event enrichment for the exact tenant/event ref with resolved actor and safe tool-call payload; authorization and redaction precede this update.
	// queryRuntimeRecordtoolcallUpdateEvent содержит дополнение TOOL_CALL_RECORDED события точной организации и ссылки разрешённым автором и безопасными данными вызова; полномочия и удаление чувствительных данных проверяют до обновления.
	//go:embed sql/runtime_recordtoolcall_update_event.sql
	queryRuntimeRecordtoolcallUpdateEvent string
	// queryRuntimeRecordtoolcallUpdateOutbox embeds tool-call JSON replacement in the outbox payload linked through the exact tenant/run-event ref; it updates durable event data, not direct broker publication.
	// queryRuntimeRecordtoolcallUpdateOutbox содержит замену JSON вызова инструмента в содержимом outbox, связанном через точную организацию и ссылку события запуска; меняет устойчивые данные события, не публикует напрямую в брокер.
	//go:embed sql/runtime_recordtoolcall_update_outbox.sql
	queryRuntimeRecordtoolcallUpdateOutbox string
	// queryRuntimeDelegateexecutionInsertDelegationEdge embeds DELEGATED_TO edge insertion between owner-resolved parent/child nodes with exact tenant/root pins; lineage must be established before these IDs are persisted.
	// queryRuntimeDelegateexecutionInsertDelegationEdge содержит создание DELEGATED_TO ребра между разрешёнными родительским и дочерним узлами с точными организацией и корнем; происхождение связи устанавливают до записи этих ID.
	//go:embed sql/runtime_delegateexecution_insert_delegation_edge.sql
	queryRuntimeDelegateexecutionInsertDelegationEdge string
	// queryRuntimeDelegateexecutionInsertCallbackEdge embeds CALLBACK_TO edge insertion for the resolved delegated child and parent endpoints; callback receipts and any continuation use separate verified lifecycle steps.
	// queryRuntimeDelegateexecutionInsertCallbackEdge содержит создание CALLBACK_TO ребра разрешённых дочернего и родительского концов делегирования; квитанции обратного вызова и продолжение проходят отдельные проверяемые переходы.
	//go:embed sql/runtime_delegateexecution_insert_callback_edge.sql
	queryRuntimeDelegateexecutionInsertCallbackEdge string
	// queryRuntimeProposeassistantplanSelectContext embeds exact owned ACTIVE assistant-conversation locking with system/project profile lineage, active actor membership/project visibility and server-derived context operations for plan proposal; it does not approve a plan.
	// queryRuntimeProposeassistantplanSelectContext содержит блокировку точного ACTIVE диалога его автора с происхождением системного либо проектного профиля, активным членством и видимостью проекта и серверными операциями контекста для предложения плана; план не одобряет.
	//go:embed sql/runtime_proposeassistantplan_select_context.sql
	queryRuntimeProposeassistantplanSelectContext string
	// queryRuntimeCallbackSelectParentSession embeds next-turn-number retrieval and session locking through the exact tenant/parent run; callback provenance and terminal child eligibility are checked before history insertion.
	// queryRuntimeCallbackSelectParentSession содержит чтение следующего номера хода и блокировку сессии через точные организацию и родительский запуск; происхождение обратного вызова и завершение дочернего запуска проверяют до вставки истории.
	//go:embed sql/runtime_callback_select_parent_session.sql
	queryRuntimeCallbackSelectParentSession string
	// queryRuntimeCallbackInsertCompletedTurn embeds COMPLETED AGENT callback-turn insertion with the resolved child run ref as actor and completion time; the owner supplies receipt-derived content rather than arbitrary caller lineage.
	// queryRuntimeCallbackInsertCompletedTurn содержит создание COMPLETED хода AGENT обратного вызова со ссылкой разрешённого дочернего запуска как автором и временем завершения; текст берёт владелец из квитанции, не из произвольного происхождения вызывающего.
	//go:embed sql/runtime_callback_insert_completed_turn.sql
	queryRuntimeCallbackInsertCompletedTurn string
	// queryRuntimeCallbackUpdateSession embeds callback-history session number/version/time advancement by resolved session ID; the parent-session lock and receipt/turn transaction provide ordering and idempotency.
	// queryRuntimeCallbackUpdateSession содержит увеличение номера хода и версии с обновлением времени разрешённой сессии после истории обратного вызова; порядок и идемпотентность обеспечивают блокировка родительской сессии и транзакция квитанции и хода.
	//go:embed sql/runtime_callback_update_session.sql
	queryRuntimeCallbackUpdateSession string
	// queryRuntimeCallbackResolveContinuation embeds locked continuation eligibility for a SUCCEEDED parent after all callback receipts and required-workflow delivery, on a live root with no prior CONTINUES edge; returned child summaries come from receipts, not an unrestricted file list.
	// queryRuntimeCallbackResolveContinuation содержит блокировку и проверку продолжения SUCCEEDED родителя после всех квитанций обратного вызова и доставки обязательного процесса при активном корне без прежнего CONTINUES; результаты детей берёт из квитанций, не из неограниченного списка файлов.
	//go:embed sql/runtime_callback_resolve_continuation.sql
	queryRuntimeCallbackResolveContinuation string
	// queryRuntimeIntegrationResolveContinuation embeds node/root locking for integration continuation after a gate-consistent terminal invocation newer than the completed node, with exact tenant/project-or-owned-organization scope and no prior CONTINUES edge.
	// queryRuntimeIntegrationResolveContinuation содержит блокировку узла и корня для продолжения после согласованного с решением шлюза конечного вызова интеграции, обновлённого позднее завершения узла, с точной организацией, проектом либо принадлежащей организационной областью и без прежнего CONTINUES.
	//go:embed sql/runtime_integration_resolve_continuation.sql
	queryRuntimeIntegrationResolveContinuation string
	// queryRuntimeIntegrationResumeChildRun embeds tenant-scoped SUCCEEDED-to-RUNNING resumption only for a non-root run, clearing finish time and advancing version/time; it cannot revive other terminal outcomes.
	// queryRuntimeIntegrationResumeChildRun содержит возобновление SUCCEEDED в RUNNING только некорневого запуска организации с очисткой времени завершения и обновлением версии и времени; другие конечные исходы не восстанавливает.
	//go:embed sql/runtime_integration_resume_child_run.sql
	queryRuntimeIntegrationResumeChildRun string
	// queryRuntimeIntegrationResumeRootRun embeds tenant/root WAITING_HUMAN-to-RUNNING resumption only with no OPEN gates, advancing version/time; planned work and new authority are handled separately.
	// queryRuntimeIntegrationResumeRootRun содержит возобновление WAITING_HUMAN в RUNNING точного корня организации только без OPEN шлюзов с обновлением версии и времени; запланированную работу и новые полномочия обрабатывают отдельно.
	//go:embed sql/runtime_integration_resume_root_run.sql
	queryRuntimeIntegrationResumeRootRun string
	// queryRuntimeIntegrationReadRootState embeds exact tenant/root state readback without a row lock; it observes persisted lifecycle state rather than authorizing a transition.
	// queryRuntimeIntegrationReadRootState содержит чтение состояния точного корня организации без блокировки строки; наблюдает сохранённое состояние, а не разрешает переход.
	//go:embed sql/runtime_integration_read_root_state.sql
	queryRuntimeIntegrationReadRootState string
	// queryRuntimeCallbackInsertContinuationTurn embeds a QUEUED AGENT continuation turn with resolved parent session/run and agent identity; receipt eligibility and next-number locking precede insertion.
	// queryRuntimeCallbackInsertContinuationTurn содержит создание QUEUED хода продолжения AGENT с разрешёнными родительской сессией, запуском и сотрудником; допустимость квитанции и блокировка следующего номера предшествуют вставке.
	//go:embed sql/runtime_callback_insert_continuation_turn.sql
	queryRuntimeCallbackInsertContinuationTurn string
	// queryRuntimeCallbackInsertContinuationNode embeds QUEUED continuation execution-node creation with inherited parent/root/agent/step/gate/attempt pins and OPEN/CANCEL actions; fresh runtime revision and lease remain later owner steps.
	// queryRuntimeCallbackInsertContinuationNode содержит создание QUEUED узла продолжения с наследуемыми родителем, корнем, сотрудником, шагом, шлюзом и попыткой и действиями OPEN/CANCEL; свежие ревизия runtime и аренда создаются последующими шагами владельца.
	//go:embed sql/runtime_callback_insert_continuation_node.sql
	queryRuntimeCallbackInsertContinuationNode string
	// queryRuntimeCallbackInsertContinuesEdge embeds the resolved parent-to-continuation CONTINUES edge with exact tenant/root endpoints; duplicate prevention and receipt lineage checks belong to the locked owner transaction.
	// queryRuntimeCallbackInsertContinuesEdge содержит запись CONTINUES ребра от разрешённого родителя к продолжению с точными организацией и корнем; устранение повторов и происхождение квитанции обеспечивает заблокированная транзакция владельца.
	//go:embed sql/runtime_callback_insert_continues_edge.sql
	queryRuntimeCallbackInsertContinuesEdge string
)
