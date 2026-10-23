package platform

import _ "embed"

var (
	// queryQueriesGetbootstrapstateSelectProjectsOrganizationIdLifecycleSingleton reads installation bootstrap/onboarding timestamps and the resolved organization ACTIVE-project count; caller scope authorization is separate from these aggregate values.
	// queryQueriesGetbootstrapstateSelectProjectsOrganizationIdLifecycleSingleton читает installation timestamps bootstrap/onboarding и количество ACTIVE-проектов разрешённой организации; авторизация области вызывающего выполняется отдельно от этих агрегатов.
	//go:embed sql/queries_getbootstrapstate_select_projects_organization_id_lifecycle_singleton.sql
	queryQueriesGetbootstrapstateSelectProjectsOrganizationIdLifecycleSingleton string
	// queryQueriesGetplatformeventcursorSelectInstallationPlatformSequence reads the singleton platform event high-watermark without changing it; the cursor alone grants no tenant subscription or resource access.
	// queryQueriesGetplatformeventcursorSelectInstallationPlatformSequence читает singleton high-watermark платформенных событий без изменения; сам курсор не предоставляет подписку арендатора и доступ к ресурсу.
	//go:embed sql/queries_getplatformeventcursor_select_installation_platform_sequence.sql
	queryQueriesGetplatformeventcursorSelectInstallationPlatformSequence string
	// queryQueriesGetoverviewSelectProjectsOrganizationIdLifecycleState counts organization ACTIVE projects, nonarchived ordinary agents, live runs and open owner gates; this aggregate query does not perform per-resource actor eligibility checks.
	// queryQueriesGetoverviewSelectProjectsOrganizationIdLifecycleState считает ACTIVE-проекты организации, неархивных обычных агентов, живые запуски и открытые owner gates; этот агрегирующий запрос не проверяет доступ actor к каждому ресурсу.
	//go:embed sql/queries_getoverview_select_projects_organization_id_lifecycle_state.sql
	queryQueriesGetoverviewSelectProjectsOrganizationIdLifecycleState string
	// queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled lists enabled platform capability definitions with their declared risk in name order; these definitions describe available operations rather than granting them to the reader.
	// queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled перечисляет включённые определения возможностей платформы с заявленным риском по имени; определения описывают доступные операции, а не предоставляют их читателю.
	//go:embed sql/queries_listcapabilities_select_platform_capabilities_enabled.sql
	queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled string
	// queryQueriesListruntimesSelectRuntimeProfilesEnabled lists enabled runtime profile keys with provider/model/revision ordered by name and stable key, without claiming provider-account readiness or model eligibility.
	// queryQueriesListruntimesSelectRuntimeProfilesEnabled перечисляет включённые runtime profile keys с provider/model/revision по имени и стабильному ключу без утверждения о готовности аккаунта провайдера и доступности модели.
	//go:embed sql/queries_listruntimes_select_runtime_profiles_enabled.sql
	queryQueriesListruntimesSelectRuntimeProfilesEnabled string
	// queryQueriesSearchSelectEligibleResources collects organization/project text-search candidates across projects, agents, workflows, runs and active artifacts, ranking exact/prefix/substring names. Search applies actor resourceVisible eligibility to these candidates before cursor/page limits.
	// queryQueriesSearchSelectEligibleResources собирает текстовых кандидатов организации/проекта среди проектов, агентов, процессов, запусков и активных артефактов с приоритетом точного имени, префикса и подстроки. Search применяет actor resourceVisible eligibility к кандидатам до курсора и лимита страницы.
	//go:embed sql/queries_search_select_eligible_resources.sql
	queryQueriesSearchSelectEligibleResources string
	// queryQueriesListprojectsSelectProjectsOrganizationIdProjectIdSubjectId lists ACTIVE organization projects after exact authority-project and assistant-context eligibility checks, with text filtering and descending updated-at/ref keyset pagination. Membership permissions and resource counts are presentation fields, not substitutes for the eligibility predicate.
	// queryQueriesListprojectsSelectProjectsOrganizationIdProjectIdSubjectId перечисляет ACTIVE-проекты организации после точных authority-project и assistant-context проверок, с текстовым фильтром и убывающей updated-at/ref keyset-пагинацией. Membership permissions и счётчики ресурсов служат представлению и не заменяют eligibility predicate.
	//go:embed sql/queries_listprojects_select_projects_organization_id_project_id_subject_id.sql
	queryQueriesListprojectsSelectProjectsOrganizationIdProjectIdSubjectId string
	// queryQueriesGetprojectSelectProjectsOrganizationIdRefProjectId reads one organization project and resource counts only when the actor/authority-project context projection exists; absent projection hides the row rather than fabricating membership-based authority.
	// queryQueriesGetprojectSelectProjectsOrganizationIdRefProjectId читает проект организации и счётчики ресурсов лишь при наличии actor/authority-project context projection; отсутствие проекции скрывает строку вместо назначения прав по membership.
	//go:embed sql/queries_getproject_select_projects_organization_id_ref_project_id.sql
	queryQueriesGetprojectSelectProjectsOrganizationIdRefProjectId string
	// queryPlatformMembershipList lists platform-scoped memberships and verified-OIDC subject metadata, including active flags, ordered by display name/ref with a page limit. Management authorization belongs to its owner query path, not this row projection.
	// queryPlatformMembershipList перечисляет платформенные memberships и метаданные verified-OIDC субъектов, включая признаки активности, по display name/ref с лимитом страницы. Авторизацией управления владеет вызывающий owner query path, а не проекция строки.
	//go:embed sql/platform_membership__list.sql
	queryPlatformMembershipList string
	// queryPlatformMembershipListCandidates selects active verified-OIDC organization subjects with no platform membership row, optionally matching display name/masked email and ordered by name/ref. A missing membership row is candidate selection, not a grant to add it.
	// queryPlatformMembershipListCandidates выбирает активных verified-OIDC субъектов организации без строки платформенного membership, при необходимости фильтруя display name/masked email и сортируя по name/ref. Отсутствие membership выбирает кандидата, но не разрешает его добавление.
	//go:embed sql/platform_membership__list_candidates.sql
	queryPlatformMembershipListCandidates string
	// queryProjectMembershipList joins project/platform memberships with subject metadata and enforces authority-project plus catalog access.manage visibility before ref-keyset pagination. Returned membership states/permissions are descriptive and do not bypass command-time authorization.
	// queryProjectMembershipList связывает проектные/платформенные memberships с субъектами и проверяет authority-project и catalog access.manage до ref-keyset-пагинации. Возвращённые состояния/permissions описывают membership и не обходят авторизацию команды.
	//go:embed sql/project_membership__list.sql
	queryProjectMembershipList string
	// queryProjectMembershipListCandidates lists active verified-OIDC platform members missing any membership in the ACTIVE target project, requiring owner/admin or active MANAGE_MEMBERS presentation membership. Caller/query guards still own the full access boundary.
	// queryProjectMembershipListCandidates перечисляет активных verified-OIDC участников платформы без membership в целевом ACTIVE-проекте, требуя owner/admin либо активный MANAGE_MEMBERS membership. Полной границей доступа по-прежнему владеют guards вызывающего/query.
	//go:embed sql/project_membership__list_candidates.sql
	queryProjectMembershipListCandidates string
	// queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId lists nonarchived ordinary agents with exact runtime config, role, clean active knowledge refs and separate manage/launch hints. Organization/authority-project, state/text/cursor filters and catalog agent.view eligibility apply before the ref-ordered page limit.
	// queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId перечисляет неархивных обычных агентов с точной runtime config, ролью, clean active knowledge refs и раздельными manage/launch подсказками. Organization/authority-project, state/text/cursor и catalog agent.view проверяются до лимита страницы по ref.
	//go:embed sql/queries_listagents_select_agents_organization_id_ref_project_id.sql
	queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId string
	// queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey reads one organization agent with runtime/knowledge metadata, admitting a projectless system assistant via organization.view or an ACTIVE project agent via agent.view. Management/launch hints remain separate from the row eligibility condition.
	// queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey читает агента организации с runtime/knowledge метаданными, допуская системного помощника без проекта через organization.view либо агента ACTIVE-проекта через agent.view. Manage/launch подсказки отделены от условия доступности строки.
	//go:embed sql/queries_getagent_select_agents_organization_id_ref_system_key.sql
	queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey string
	// queryQueriesAttachinstructionsSelectInstructionVersionsOrganizationIdAgentIdRef attaches all instruction versions for an already authorized organization agent in descending version order, including active binding identity and whether a managed prompt-template binding is absent. It does not authorize changing instructions itself.
	// queryQueriesAttachinstructionsSelectInstructionVersionsOrganizationIdAgentIdRef добавляет все версии инструкций уже разрешённого агента организации по убыванию версии, включая identity активной привязки и отсутствие managed prompt-template binding. Самостоятельно изменение инструкций не разрешает.
	//go:embed sql/queries_attachinstructions_select_instruction_versions_organization_id_agent_id_ref.sql
	queryQueriesAttachinstructionsSelectInstructionVersionsOrganizationIdAgentIdRef string
	// queryQueriesAttachagentgrantsSelectIntegrationGrantsOrganizationIdTargetKindTargetRef lists enabled AGENT-target grant refs for the exact organization/agent, in ref order, after the caller has authorized that agent; it returns no credential bytes or newly issued grants.
	// queryQueriesAttachagentgrantsSelectIntegrationGrantsOrganizationIdTargetKindTargetRef перечисляет включённые AGENT-target grant refs точных organization/agent по ref после авторизации агента вызывающим; байты credential и новые grants не выдаёт.
	//go:embed sql/queries_attachagentgrants_select_integration_grants_organization_id_target_kind_target_ref.sql
	queryQueriesAttachagentgrantsSelectIntegrationGrantsOrganizationIdTargetKindTargetRef string
	// queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId lists nonarchived workflow metadata, exact published version and coordinator with management/launch hints. It applies organization/project/state/text/ref filters and catalog workflow.view eligibility before the page limit.
	// queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId перечисляет метаданные неархивных процессов, точную опубликованную версию и координатора с manage/launch подсказками. Применяет organization/project/state/text/ref фильтры и catalog workflow.view до лимита страницы.
	//go:embed sql/queries_listworkflows_select_workflows_organization_id_ref_project_id.sql
	queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId string
	// queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId reads one organization workflow, its published-version ref and coordinator only after catalog workflow.view eligibility; projected manage/launch membership hints do not authorize execution or edits.
	// queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId читает процесс организации, ref опубликованной версии и координатора лишь после catalog workflow.view; membership manage/launch подсказки не разрешают исполнение и изменение.
	//go:embed sql/queries_getworkflow_select_workflows_organization_id_ref_project_id.sql
	queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId string
	// queryQueriesListrunsSelectRunsOrganizationIdRefProjectId lists scoped run/session/root/parent/retry metadata, usage and root artifact/gate refs with state/text filters and descending created-at/ref keyset pagination. Nonprivileged rows require catalog run.view; cancel/launch fields are independent presentation hints.
	// queryQueriesListrunsSelectRunsOrganizationIdRefProjectId перечисляет scoped run/session/root/parent/retry метаданные, расход и root artifact/gate refs с state/text фильтрами и убывающей created-at/ref keyset-пагинацией. Непривилегированные строки требуют catalog run.view; cancel/launch поля остаются отдельными подсказками.
	//go:embed sql/queries_listruns_select_runs_organization_id_ref_project_id.sql
	queryQueriesListrunsSelectRunsOrganizationIdRefProjectId string
	// queryQueriesGetrunSelectRunsOrganizationIdRefProjectId reads an exact organization run with lineage, session, input attachments and usage under authority-project and catalog run.view checks, hiding inactive project rows. Cancel/target-launch booleans are separately derived from catalog permissions and do not issue a command grant.
	// queryQueriesGetrunSelectRunsOrganizationIdRefProjectId читает точный запуск организации с lineage, сессией, входными вложениями и расходом под authority-project и catalog run.view, скрывая строки неактивного проекта. Cancel/target-launch выводятся отдельно из catalog permissions и не выдают grant команды.
	//go:embed sql/queries_getrun_select_runs_organization_id_ref_project_id.sql
	queryQueriesGetrunSelectRunsOrganizationIdRefProjectId string
	// queryQueriesGetrungraphSelectArtifactsNodeIdRef loads organization graph nodes and edge-adjacent nodes plus artifact refs; child run refs require materialized delegation/callback or exact required-workflow launch lineage. The caller separately authorizes the requested root and validates the resulting graph topology.
	// queryQueriesGetrungraphSelectArtifactsNodeIdRef загружает узлы графа организации и смежные по рёбрам узлы с artifact refs; дочерние run refs требуют materialized delegation/callback либо точный required-workflow launch lineage. Вызывающий отдельно авторизует корень и проверяет топологию полученного графа.
	//go:embed sql/queries_getrungraph_select_artifacts_node_id_ref.sql
	queryQueriesGetrungraphSelectArtifactsNodeIdRef string
	// queryQueriesGetrungraphSelectRunEdgesOrganizationIdRef reads edge refs, endpoints, types and labels for one exact organization root in creation order; graph authorization and cross-root lineage validation remain in the owner read path.
	// queryQueriesGetrungraphSelectRunEdgesOrganizationIdRef читает ref рёбер, концы, виды и метки точного корня организации по созданию; авторизация графа и проверка cross-root lineage остаются в пути чтения владельца.
	//go:embed sql/queries_getrungraph_select_run_edges_organization_id_ref.sql
	queryQueriesGetrungraphSelectRunEdgesOrganizationIdRef string
	// queryQueriesListruneventsSelectRunEventsOrganizationIdRef rejoins one authorized root event stream after an exact sequence cursor with ascending sequence and a bounded page, including safe deltas and actor/message/tool projections rather than runtime-private traces.
	// queryQueriesListruneventsSelectRunEventsOrganizationIdRef присоединяет поток событий разрешённого корня после точного sequence cursor по возрастанию номера с ограниченной страницей, включая safe deltas и actor/message/tool проекции вместо приватных runtime-трасс.
	//go:embed sql/queries_listrunevents_select_run_events_organization_id_ref.sql
	queryQueriesListruneventsSelectRunEventsOrganizationIdRef string
	// queryQueriesListownergatesSelectOwnerGatesOrganizationIdRefState computes visible project/owned-organization assistant gates before descending time/ref pagination and returns the total even for an empty page. Organization gates additionally require unscoped owner/admin and organization.manage; filters include states and display text.
	// queryQueriesListownergatesSelectOwnerGatesOrganizationIdRefState вычисляет видимые проектные/gates собственного системного помощника до убывающей time/ref-пагинации и возвращает общее количество даже при пустой странице. Gates организации дополнительно требуют unscoped owner/admin и organization.manage; фильтры включают состояния и отображаемый текст.
	//go:embed sql/queries_listownergates_select_owner_gates_organization_id_ref_state.sql
	queryQueriesListownergatesSelectOwnerGatesOrganizationIdRefState string
	// queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId reads one gate with requester/resolver, resolution attachments, scope and version; project membership or owner/admin permits row visibility, while organization scope also requires an owned organization-assistant root. The resolve-permission hint does not record or replace a server-owned decision.
	// queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId читает gate с инициатором/решающим, вложениями решения, scope и версией; membership проекта либо owner/admin допускает строку, область организации также требует собственный organization-assistant root. Resolve-подсказка не фиксирует и не заменяет решение сервера.
	//go:embed sql/queries_getownergate_select_owner_gates_organization_id_ref_project_id.sql
	queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId string
	// queryQueriesListartifactsSelectArtifactBindingsArtifactIdIdOrganizationId lists artifact metadata and knowledge bindings with lifecycle/scan/source/type/text filters, exact authority-project and catalog artifact.view eligibility. Descending created-at/ref pagination applies after visibility; management hints and scan state do not themselves authorize downloading content.
	// queryQueriesListartifactsSelectArtifactBindingsArtifactIdIdOrganizationId перечисляет метаданные артефактов и knowledge bindings с lifecycle/scan/source/type/text фильтрами, точным authority-project и catalog artifact.view. Убывающая created-at/ref-пагинация следует после видимости; manage-подсказки и scan state сами по себе не разрешают скачивание содержимого.
	//go:embed sql/queries_listartifacts_select_artifact_bindings_artifact_id_id_organization_id.sql
	queryQueriesListartifactsSelectArtifactBindingsArtifactIdIdOrganizationId string
	// queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId reads exact organization artifact metadata/current revision, lineage and knowledge refs only through catalog artifact.view. The optional manage hint is presentation; fetching bytes still requires the separate owner download path.
	// queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId читает точные метаданные/текущую ревизию артефакта организации, lineage и knowledge refs лишь через catalog artifact.view. Необязательная manage-подсказка служит представлению; получение байтов требует отдельного пути скачивания владельца.
	//go:embed sql/queries_getartifact_select_artifact_bindings_artifact_id_id_organization_id.sql
	queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId string
	// queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId lists nondeleted schedules with their exact immutable current revision, target, continuation session and latest occurrence summary. Text/ref/project filters and catalog schedule.view run before ascending-ref pagination; the manage hint does not create scheduler authority.
	// queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId перечисляет неудалённые расписания с точной неизменяемой текущей ревизией, целью, сессией продолжения и последней occurrence summary. Text/ref/project и catalog schedule.view применяются до страницы по возрастанию ref; manage-подсказка не создаёт полномочий планировщика.
	//go:embed sql/queries_listschedules_select_schedules_organization_id_ref_project_id.sql
	queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId string
	// queryQueriesGetscheduleSelectSchedulesOrganizationIdRef reads one nondeleted schedule and its exact revision/target/session/occurrence projection, using the existing owner/admin or active VIEW membership row condition. Management is only a returned hint; caller domain checks still define the public read boundary.
	// queryQueriesGetscheduleSelectSchedulesOrganizationIdRef читает неудалённое расписание и точную revision/target/session/occurrence проекцию с прежним условием owner/admin либо активного VIEW membership. Management возвращается лишь подсказкой; публичную границу чтения определяют и доменные проверки вызывающего.
	//go:embed sql/queries_getschedule_select_schedules_organization_id_ref.sql
	queryQueriesGetscheduleSelectSchedulesOrganizationIdRef string
	// queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef reads active membership permissions for the exact organization/project/actor solely to shape action hints after independent resource eligibility. No membership row means no hints, not a replacement denial or grant for the resource.
	// queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef читает permissions активного membership точных organization/project/actor лишь для action hints после независимой проверки ресурса. Отсутствие membership означает отсутствие подсказок, а не заменяющий отказ или разрешение на ресурс.
	//go:embed sql/queries_project_action_permissions_select_memberships_organization_id_ref.sql
	queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef string
	// queryQueriesListintegrationdefinitionsSelectIntegrationDefinitionsCategory lists integration definition schema/provenance/adapter metadata with category/text/stable-key pagination and organization connection/connected counts. Definitions and aggregate connection health do not issue execution grants.
	// queryQueriesListintegrationdefinitionsSelectIntegrationDefinitionsCategory перечисляет schema/provenance/adapter метаданные определений интеграций с category/text/stable-key страницей и счётчиками подключений/CONNECTED организации. Определения и агрегированное здоровье подключений не выдают grants исполнения.
	//go:embed sql/queries_listintegrationdefinitions_select_integration_definitions_category.sql
	queryQueriesListintegrationdefinitionsSelectIntegrationDefinitionsCategory string
	// queryQueriesListintegrationconnectionsSelectIntegrationConnectionsOrganizationIdDefinitionKey lists ACTIVE organization connections with public configuration, versioned definition pins and current credential descriptor metadata, using definition/text/ref filters and a ref-ordered limit. Secret values and authorization for dangerous operations are not returned.
	// queryQueriesListintegrationconnectionsSelectIntegrationConnectionsOrganizationIdDefinitionKey перечисляет ACTIVE-подключения организации с публичной конфигурацией, versioned definition pins и текущими метаданными credential descriptor, с definition/text/ref фильтрами и лимитом по ref. Значения секретов и полномочия опасных операций не возвращаются.
	//go:embed sql/queries_listintegrationconnections_select_integration_connections_organization_id_definition_key.sql
	queryQueriesListintegrationconnectionsSelectIntegrationConnectionsOrganizationIdDefinitionKey string
	// queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef reads an exact ACTIVE organization connection with public configuration, definition pins and optional current credential descriptor; the caller has to authorize the connection, and this query never retrieves the credential value.
	// queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef читает точное ACTIVE-подключение организации с публичной конфигурацией, definition pins и необязательным текущим credential descriptor; подключение авторизует вызывающий, значение credential запрос не получает.
	//go:embed sql/queries_getintegrationconnection_select_integration_connections_organization_id_ref.sql
	queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef string
	// queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef attaches grants only for visible agent/workflow targets, or the explicitly owned projectless assistant with unscoped owner/admin plus organization.manage and integration.manage checks. It returns declared risk/resource/approval scope in creation order without issuing or consuming an invocation grant.
	// queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef добавляет grants лишь видимых agent/workflow targets либо явно собственного помощника без проекта при unscoped owner/admin и проверках organization.manage и integration.manage. Возвращает заявленные risk/resource/approval scope по созданию без выдачи и использования invocation grant.
	//go:embed sql/queries_attachconnection_select_integration_grants_organization_id_connection_id_ref.sql
	queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef string
	// queryQueriesGetassistantSelectAssistantRuntimeOrganizationId reads one organization assistant runtime record and agent identity, including current/desired revision, owner instructions, heartbeat/resource limits and version. Authorization is established by the owner read path rather than this metadata lookup.
	// queryQueriesGetassistantSelectAssistantRuntimeOrganizationId читает runtime запись помощника организации и identity агента, включая текущую/желаемую ревизию, owner instructions, heartbeat/resource limits и версию. Авторизация выполняется путём чтения владельца, а не этим поиском метаданных.
	//go:embed sql/queries_getassistant_select_assistant_runtime_organization_id.sql
	queryQueriesGetassistantSelectAssistantRuntimeOrganizationId string
	// queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef filters the initiating actor conversations by assistant/project/state/text/cursor, materializes exact context eligibility once per distinct binding and applies LIMIT only after that join. Allowed-operation projections are server-derived; hidden contexts cannot consume visible page slots.
	// queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef фильтрует диалоги начавшего их actor по assistant/project/state/text/cursor, однократно материализует точную доступность каждого distinct context binding и применяет LIMIT лишь после соединения. Allowed operations выводятся сервером; скрытые контексты не занимают места видимой страницы.
	//go:embed sql/queries_listassistantconversations_select_assistant_conversations_organization_id_ref.sql
	queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef string
	// queryQueriesAttachconversationSelectSessionTurnsOrganizationIdSessionIdRef reads the already authorized conversation session turns in turn-number order with attachment/run refs and display actor names. User turn display state reflects cancelled runs or queued/running agent nodes without updating historical turn rows.
	// queryQueriesAttachconversationSelectSessionTurnsOrganizationIdSessionIdRef читает ходы сессии уже разрешённого диалога по номеру с attachment/run refs и отображаемыми именами actor. Состояние пользовательского хода отражает отмену запуска либо queued/running узел агента без изменения исторических строк.
	//go:embed sql/queries_attachconversation_select_session_turns_organization_id_session_id_ref.sql
	queryQueriesAttachconversationSelectSessionTurnsOrganizationIdSessionIdRef string
	// queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef reads plans of the exact organization conversation and each latest immutable receipt, including outcomes, operation receipts/conflicts/audit/resource refs. Caller conversation authorization precedes this chronological metadata attachment; receipt rows are not synthesized.
	// queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef читает планы точного диалога организации и последнюю неизменяемую квитанцию каждого с outcomes, operation receipts/conflicts/audit/resource refs. Авторизация диалога предшествует хронологическому добавлению метаданных; строки квитанций не подделываются.
	//go:embed sql/queries_attachconversation_select_assistant_plans_organization_id_ref.sql
	queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef string
	// queryQueriesListauditeventsSelectAuditEventsOrganizationIdRefAction lists organization audit entries with resource/actor display metadata, permission filtering and optional suppression of the closed technical-event set. Filters cover project/resource/action/outcome/text; keyset order is descending occurred-at with ascending ref ties and an explicit limit.
	// queryQueriesListauditeventsSelectAuditEventsOrganizationIdRefAction перечисляет аудит организации с отображаемыми resource/actor метаданными, фильтром полномочий и необязательным скрытием закрытого набора технических событий. Фильтры охватывают project/resource/action/outcome/text; keyset упорядочен по убыванию occurred-at и возрастанию ref при равенстве с явным лимитом.
	//go:embed sql/queries_listauditevents_select_audit_events_organization_id_ref_action.sql
	queryQueriesListauditeventsSelectAuditEventsOrganizationIdRefAction string
)
