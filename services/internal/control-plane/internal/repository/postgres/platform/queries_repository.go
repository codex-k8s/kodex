package platform

import _ "embed"

var (
	// queryRepositoryReadySelectInstallationSingleton reads the singleton installation schema version for readiness without locking or proving that bootstrap has completed.
	// queryRepositoryReadySelectInstallationSingleton читает версию схемы единственной установки для readiness без блокировки и без доказательства завершённого bootstrap.
	//go:embed sql/repository_ready_select_installation_singleton.sql
	queryRepositoryReadySelectInstallationSingleton string
	// queryRepositoryBootstrapSelectInstallationSingleton locks the singleton installation row and reads its bootstrap timestamp to serialize initialization.
	// queryRepositoryBootstrapSelectInstallationSingleton блокирует единственную строку установки и читает время bootstrap для сериализации инициализации.
	//go:embed sql/repository_bootstrap_select_installation_singleton.sql
	queryRepositoryBootstrapSelectInstallationSingleton string
	// queryRepositoryBootstrapInsertOrganizationsRefName creates the initial organization with fixed display name Kodex and caller-supplied public ref, returning its internal ID.
	// queryRepositoryBootstrapInsertOrganizationsRefName создаёт исходную организацию с фиксированным именем Kodex и переданной публичной ссылкой, возвращая внутренний ID.
	//go:embed sql/repository_bootstrap_insert_organizations_ref_name.sql
	queryRepositoryBootstrapInsertOrganizationsRefName string
	// queryRepositoryBootstrapInsertOwnerClaimContractsOrganizationIdStableKeyState creates a PENDING_CLAIM installation-owner contract for the organization; it does not assign a human owner.
	// queryRepositoryBootstrapInsertOwnerClaimContractsOrganizationIdStableKeyState создаёт контракт владельца установки PENDING_CLAIM для организации; человека-владельца не назначает.
	//go:embed sql/repository_bootstrap_insert_owner_claim_contracts_organization_id_stable_key_state.sql
	queryRepositoryBootstrapInsertOwnerClaimContractsOrganizationIdStableKeyState string
	// queryRepositoryBootstrapInsertSubjectsRefIssuerDisplayName creates the fixed sys_platform SERVICE subject with kodex-system issuer and supplied external-subject digest; it is not a human identity.
	// queryRepositoryBootstrapInsertSubjectsRefIssuerDisplayName создаёт фиксированный служебный субъект sys_platform с issuer kodex-system и заданным дайджестом внешнего субъекта; это не человеческая личность.
	//go:embed sql/repository_bootstrap_insert_subjects_ref_issuer_display_name.sql
	queryRepositoryBootstrapInsertSubjectsRefIssuerDisplayName string
	// queryRepositoryBootstrapInsertPlatformCapabilitiesStableKeyNameDescription inserts a platform capability's stable key, display metadata and risk class during bootstrap without granting it to an actor.
	// queryRepositoryBootstrapInsertPlatformCapabilitiesStableKeyNameDescription создаёт возможность платформы со стабильным ключом, отображаемыми данными и классом риска при bootstrap без выдачи её субъекту.
	//go:embed sql/repository_bootstrap_insert_platform_capabilities_stable_key_name_description.sql
	queryRepositoryBootstrapInsertPlatformCapabilitiesStableKeyNameDescription string
	// queryRepositoryBootstrapInsertRuntimeProfilesStableKeyProviderRuntimeRevision inserts a runtime profile with pinned provider/model/resource limits and runtime-v1 revision; this is configuration, not a running workload.
	// queryRepositoryBootstrapInsertRuntimeProfilesStableKeyProviderRuntimeRevision создаёт профиль исполнения с закреплёнными провайдером, моделью, ресурсами и ревизией runtime-v1; это конфигурация, не запущенный исполнитель.
	//go:embed sql/repository_bootstrap_insert_runtime_profiles_stable_key_provider_runtime_revision.sql
	queryRepositoryBootstrapInsertRuntimeProfilesStableKeyProviderRuntimeRevision string
	// queryRepositoryBootstrapInsertProviderDefinition inserts the fixed OpenAI Codex provider definition and capability flags during bootstrap; it does not authenticate an account.
	// queryRepositoryBootstrapInsertProviderDefinition создаёт фиксированное определение провайдера OpenAI Codex и признаки возможностей при bootstrap; аккаунт не авторизует.
	//go:embed sql/repository_bootstrap_insert_provider_definition.sql
	queryRepositoryBootstrapInsertProviderDefinition string
	// queryRepositoryBootstrapInsertProviderAccount inserts the default OpenAI Codex provider account as AUTHORIZED/enabled with supplied creator; credential material is not stored in this row.
	// queryRepositoryBootstrapInsertProviderAccount создаёт исходный включённый аккаунт OpenAI Codex в состоянии AUTHORIZED с заданным создателем; сами реквизиты в строке не хранятся.
	//go:embed sql/repository_bootstrap_insert_provider_account.sql
	queryRepositoryBootstrapInsertProviderAccount string
	// queryRepositoryBootstrapInsertProviderCredentialRevision inserts credential revision one with exact Secret name, UID, resource version and content digest, returning its ID without reading Secret bytes.
	// queryRepositoryBootstrapInsertProviderCredentialRevision создаёт первую ревизию реквизитов с точными именем Secret, UID, resource version и дайджестом содержимого, возвращая ID без чтения байтов Secret.
	//go:embed sql/repository_bootstrap_insert_provider_credential_revision.sql
	queryRepositoryBootstrapInsertProviderCredentialRevision string
	// queryRepositoryBootstrapActivateProviderCredentialRevision activates an initial credential revision only while the account pointer is NULL, setting AUTHORIZED and advancing version; caller must own the account transaction.
	// queryRepositoryBootstrapActivateProviderCredentialRevision активирует исходную ревизию реквизитов лишь при NULL-указателе аккаунта, устанавливает AUTHORIZED и увеличивает версию; транзакцией аккаунта владеет вызывающий код.
	//go:embed sql/repository_bootstrap_activate_provider_credential_revision.sql
	queryRepositoryBootstrapActivateProviderCredentialRevision string
	// queryProviderCredentialGetCurrentForReconcile locks the default provider account through the installation owner contract and reads the current credential's Secret identity/digest for reconciliation.
	// queryProviderCredentialGetCurrentForReconcile блокирует исходный аккаунт провайдера через контракт владельца установки и читает идентичность и дайджест Secret текущих реквизитов для сверки.
	//go:embed sql/provider_credential__get_current_for_reconcile.sql
	queryProviderCredentialGetCurrentForReconcile string
	// queryProviderCredentialHistoricalBootstrapPin checks whether the exact historical Secret name, UID, resource version and digest already have a revision for the tenant account.
	// queryProviderCredentialHistoricalBootstrapPin проверяет существование исторической ревизии с точными именем Secret, UID, resource version и дайджестом для аккаунта организации.
	//go:embed sql/provider_credential__historical_bootstrap_pin.sql
	queryProviderCredentialHistoricalBootstrapPin string
	// queryProviderCredentialInsertReconciledRevision inserts the next reconciled credential revision with supplied generation and exact Secret identity/digest; caller determines revision number under lock.
	// queryProviderCredentialInsertReconciledRevision создаёт следующую сверенную ревизию реквизитов с заданным номером и точными идентичностью и дайджестом Secret; номер определяет вызывающий код под блокировкой.
	//go:embed sql/provider_credential__insert_reconciled_revision.sql
	queryProviderCredentialInsertReconciledRevision string
	// queryProviderAccountActivateReconciledCredential switches the account's current credential pointer only if its previous revision still matches, then sets AUTHORIZED and advances version.
	// queryProviderAccountActivateReconciledCredential переключает указатель текущих реквизитов аккаунта лишь при совпадении прежней ревизии, затем устанавливает AUTHORIZED и увеличивает версию.
	//go:embed sql/provider_account__activate_reconciled_credential.sql
	queryProviderAccountActivateReconciledCredential string
	// queryRepositoryBootstrapInsertRoleDefinitionsRefStableKeyName creates the fixed system-assistant role definition with empty default policies and supplied creator; it does not grant this role to anyone.
	// queryRepositoryBootstrapInsertRoleDefinitionsRefStableKeyName создаёт фиксированное определение роли системного помощника с пустыми политиками по умолчанию и заданным создателем; роль никому не выдаёт.
	//go:embed sql/repository_bootstrap_insert_role_definitions_ref_stable_key_name.sql
	queryRepositoryBootstrapInsertRoleDefinitionsRefStableKeyName string
	// queryRepositoryBootstrapInsertIntegrationDefinitionsStableKeyDescriptionCapabilities upserts shipped integration definition metadata by stable key, enabling it and advancing version on conflict; no connection grant or adapter readiness is established.
	// queryRepositoryBootstrapInsertIntegrationDefinitionsStableKeyDescriptionCapabilities вставляет либо обновляет метаданные поставляемой интеграции по стабильному ключу, включая её и увеличивая версию при конфликте; подключение, grant и готовность адаптера не создаёт.
	//go:embed sql/repository_bootstrap_insert_integration_definitions_stable_key_description_capabilities.sql
	queryRepositoryBootstrapInsertIntegrationDefinitionsStableKeyDescriptionCapabilities string
	// queryRepositoryReconcileIntegrationDefinition locks one integration definition by stable key and returns definition version/digest for reconciliation.
	// queryRepositoryReconcileIntegrationDefinition блокирует одно определение интеграции по стабильному ключу и возвращает версию и дайджест для сверки.
	//go:embed sql/repository_reconcile_integration_definition.sql
	queryRepositoryReconcileIntegrationDefinition string
	// queryRepositoryDisableUnshippedIntegrationDefinitions disables still-enabled integration definitions whose stable keys are absent from the shipped set, advancing their version.
	// queryRepositoryDisableUnshippedIntegrationDefinitions отключает ещё включённые определения интеграций, стабильных ключей которых нет в поставляемом наборе, увеличивая их версию.
	//go:embed sql/repository_disable_unshipped_integration_definitions.sql
	queryRepositoryDisableUnshippedIntegrationDefinitions string
	// queryRepositoryBootstrapInsertAgentsRefSystemKeyPurpose creates the READY system-assistant agent with fixed role, system key and initial capability, bound to the supplied runtime profile.
	// queryRepositoryBootstrapInsertAgentsRefSystemKeyPurpose создаёт READY сотрудника системного помощника с фиксированными ролью, системным ключом и исходной возможностью, привязывая заданный профиль исполнения.
	//go:embed sql/repository_bootstrap_insert_agents_ref_system_key_purpose.sql
	queryRepositoryBootstrapInsertAgentsRefSystemKeyPurpose string
	// queryRepositoryBootstrapInsertInstructionVersionsRefAgentIdState publishes instruction version one for the bootstrap assistant with supplied content/digest and core flag; active binding is a separate operation.
	// queryRepositoryBootstrapInsertInstructionVersionsRefAgentIdState публикует первую версию инструкции помощника bootstrap с заданными содержимым и дайджестом и признаком core; активная привязка выполняется отдельно.
	//go:embed sql/repository_bootstrap_insert_instruction_versions_ref_agent_id_state.sql
	queryRepositoryBootstrapInsertInstructionVersionsRefAgentIdState string
	// queryRepositoryBootstrapInsertSessionsRefTargetTypeState creates an ACTIVE system-assistant session on the default provider account and fixed system-assistant target; it is not a conversation turn.
	// queryRepositoryBootstrapInsertSessionsRefTargetTypeState создаёт ACTIVE сессию системного помощника на исходном аккаунте провайдера и фиксированной цели system-assistant; это не ход диалога.
	//go:embed sql/repository_bootstrap_insert_sessions_ref_target_type_state.sql
	queryRepositoryBootstrapInsertSessionsRefTargetTypeState string
	// queryRepositoryBootstrapInsertAssistantRuntimeOrganizationIdStableKeyCorePromptRevision creates the system assistant runtime with pinned core revision and resource limits, starting only when a system session ref exists; it does not launch a workload.
	// queryRepositoryBootstrapInsertAssistantRuntimeOrganizationIdStableKeyCorePromptRevision создаёт runtime системного помощника с закреплённой core-ревизией и ресурсами, выбирая STARTING лишь при наличии ссылки системной сессии; исполнителя не запускает.
	//go:embed sql/repository_bootstrap_insert_assistant_runtime_organization_id_stable_key_core_prompt_revision.sql
	queryRepositoryBootstrapInsertAssistantRuntimeOrganizationIdStableKeyCorePromptRevision string
	// queryRepositoryBootstrapUpdateInstallationBootstrappedAt sets the singleton installation bootstrap timestamp; it is a recorded milestone, not an external dependency readiness proof.
	// queryRepositoryBootstrapUpdateInstallationBootstrappedAt записывает время bootstrap единственной установки; это зафиксированный этап, не доказательство готовности внешних зависимостей.
	//go:embed sql/repository_bootstrap_update_installation_bootstrapped_at.sql
	queryRepositoryBootstrapUpdateInstallationBootstrappedAt string
	// queryRepositoryBootstrapSelectAssistantCorePrompt locks the system assistant runtime and reads its currently bound PUBLISHED core instruction's digest/content; absent binding yields no row.
	// queryRepositoryBootstrapSelectAssistantCorePrompt блокирует runtime системного помощника и читает дайджест и содержимое привязанной PUBLISHED core-инструкции; без привязки строка отсутствует.
	//go:embed sql/repository_bootstrap_select_assistant_core_prompt.sql
	queryRepositoryBootstrapSelectAssistantCorePrompt string
	// queryRepositoryBootstrapInsertAssistantCorePromptVersion creates the next PUBLISHED core instruction revision using max-plus-one for the tenant agent; caller serializes the version calculation.
	// queryRepositoryBootstrapInsertAssistantCorePromptVersion создаёт следующую PUBLISHED core-ревизию инструкции через max-plus-one для сотрудника организации; расчёт номера сериализует вызывающий код.
	//go:embed sql/repository_bootstrap_insert_assistant_core_prompt_version.sql
	queryRepositoryBootstrapInsertAssistantCorePromptVersion string
	// queryRepositoryBootstrapUpdateAssistantCorePrompt replaces the core prompt pointer only when the previous revision matches, resets warm runtime to RECOVERING and advances version.
	// queryRepositoryBootstrapUpdateAssistantCorePrompt заменяет указатель core-промпта лишь при совпадении прежней ревизии, сбрасывает тёплый runtime в RECOVERING и увеличивает версию.
	//go:embed sql/repository_bootstrap_update_assistant_core_prompt.sql
	queryRepositoryBootstrapUpdateAssistantCorePrompt string
	// queryRepositoryBootstrapInsertAssistantCorePromptAudit inserts a core-prompt upgrade audit event attributed to the active kodex-system service subject for the resolved system runtime.
	// queryRepositoryBootstrapInsertAssistantCorePromptAudit записывает событие аудита обновления core-промпта от активного служебного субъекта kodex-system для разрешённого системного runtime.
	//go:embed sql/repository_bootstrap_insert_assistant_core_prompt_audit.sql
	queryRepositoryBootstrapInsertAssistantCorePromptAudit string
	// queryRepositoryBootstrapSelectAssistantRuntimeEnvironment locks the system runtime and ACTIVE organization environment, reading current version metadata and whether its image has the platform bootstrap signature; no Secret bytes are returned.
	// queryRepositoryBootstrapSelectAssistantRuntimeEnvironment блокирует системный runtime и ACTIVE окружение организации, читает метаданные текущей версии и признак платформенной bootstrap-подписи образа; байты Secret не возвращаются.
	//go:embed sql/repository_bootstrap_select_assistant_runtime_environment.sql
	queryRepositoryBootstrapSelectAssistantRuntimeEnvironment string
	// queryRepositoryBootstrapReconcileAssistantRuntimeEnvironment creates a new system environment version from the locked current version only when exact version/pointer and platform-bootstrap image conditions match, switches pointer and marks runtime RECOVERING; an absent final row must not be committed as success.
	// queryRepositoryBootstrapReconcileAssistantRuntimeEnvironment создаёт новую версию системного окружения из заблокированной текущей лишь при точном совпадении версии и указателя и условии платформенного bootstrap-образа, переключает указатель и переводит runtime в RECOVERING; отсутствие итоговой строки нельзя фиксировать как успех.
	//go:embed sql/repository_bootstrap_reconcile_assistant_runtime_environment.sql
	queryRepositoryBootstrapReconcileAssistantRuntimeEnvironment string
	// queryRepositoryBootstrapInsertAssistantRuntimeEnvironmentAudit inserts environment-reconciliation audit under the active kodex-system service subject and exact tenant/runtime/environment IDs.
	// queryRepositoryBootstrapInsertAssistantRuntimeEnvironmentAudit записывает аудит сверки окружения от активного служебного субъекта kodex-system при точных ID организации, runtime и окружения.
	//go:embed sql/repository_bootstrap_insert_assistant_runtime_environment_audit.sql
	queryRepositoryBootstrapInsertAssistantRuntimeEnvironmentAudit string
	// queryResolveVerifiedPrincipal resolves active subject and organization refs from IDs already bound to a verified transport identity; SQL does not verify an unsigned caller payload.
	// queryResolveVerifiedPrincipal разрешает ссылки активного субъекта и организации по ID, уже привязанным к проверенной transport identity; SQL не подтверждает неподписанный payload вызывающего.
	//go:embed sql/principal_resolve_verified_identity.sql
	queryResolveVerifiedPrincipal string
	// queryLockInstallationOwnerClaim locks one organization and owner-claim contract for installation ownership and returns claim state and tenant-ref metadata; LIMIT 1 has no stable-key filter or ordering here.
	// queryLockInstallationOwnerClaim блокирует одну организацию и контракт притязания на владельца установки и возвращает состояние и ссылку организации; LIMIT 1 здесь не фильтрует стабильный ключ и не задаёт порядок.
	//go:embed sql/proof_owner_lock_installation.sql
	queryLockInstallationOwnerClaim string
	// queryResolveClaimedOwnerSubject resolves an active subject only when the installation owner contract is CLAIMED and tenant ref plus external-subject digest match.
	// queryResolveClaimedOwnerSubject разрешает активного субъекта только при CLAIMED контракте владельца установки и совпадении ссылки организации и дайджеста внешнего субъекта.
	//go:embed sql/proof_owner_resolve_claimed_subject.sql
	queryResolveClaimedOwnerSubject string
	// queryFindInstallationOwnerSubject finds an existing active subject by tenant and external-subject digest before a possible owner claim.
	// queryFindInstallationOwnerSubject находит существующего активного субъекта по организации и дайджесту внешнего субъекта перед возможным притязанием на владение.
	//go:embed sql/proof_owner_find_subject.sql
	queryFindInstallationOwnerSubject string
	// queryCreateInstallationOwnerSubject creates an installation owner subject with verified-oidc-subject issuer and supplied masked profile; it does not claim ownership by insertion alone.
	// queryCreateInstallationOwnerSubject создаёт субъекта владельца установки с issuer verified-oidc-subject и заданным маскированным профилем; одна вставка владение не присваивает.
	//go:embed sql/proof_owner_create_subject.sql
	queryCreateInstallationOwnerSubject string
	// queryUpdateOIDCSubjectProfile updates a verified OIDC subject's display and masked email only when changed, excluding other issuers and preserving authority bindings.
	// queryUpdateOIDCSubjectProfile обновляет отображаемое имя и маскированную почту проверенного OIDC-субъекта лишь при изменении, исключая другие issuer и сохраняя привязки полномочий.
	//go:embed sql/proof_owner_update_subject_profile.sql
	queryUpdateOIDCSubjectProfile string
	// queryClaimInstallationOwnership atomically moves a PENDING_CLAIM owner contract to CLAIMED with subject and updates organization tenant ref only if the claim row changed.
	// queryClaimInstallationOwnership атомарно переводит контракт владельца из PENDING_CLAIM в CLAIMED с субъектом и обновляет ссылку организации только при изменении строки притязания.
	//go:embed sql/proof_owner_claim_installation.sql
	queryClaimInstallationOwnership string
	// queryProofSubjectHasActiveBinding checks existence of a currently time-valid ACTIVE binding to an ACTIVE role for an exact subject or its OIDC groups; it does not test a particular permission.
	// queryProofSubjectHasActiveBinding проверяет наличие действующей по времени ACTIVE привязки к ACTIVE роли точного субъекта либо его OIDC-групп; конкретное разрешение не проверяет.
	//go:embed sql/proof_subject_has_active_binding.sql
	queryProofSubjectHasActiveBinding string
	// queryResolveSystemWorkloadIdentity resolves the fixed active system subject only when an ACTIVE organization-scoped SYSTEM OWNER binding exists; this query checks neither subject kind nor binding time.
	// queryResolveSystemWorkloadIdentity разрешает фиксированного активного системного субъекта только при наличии ACTIVE привязки к SYSTEM OWNER роли на уровне организации; этот запрос не проверяет вид субъекта и срок действия привязки.
	//go:embed sql/proof_system_resolve_identity.sql
	queryResolveSystemWorkloadIdentity string
	// queryAuthorizeProjectMembership returns an ACTIVE project/version only when the subject or OIDC group has a time-valid ACTIVE non-owner binding whose role version permits project.view in the matching scope.
	// queryAuthorizeProjectMembership возвращает ACTIVE проект и версию лишь при действующей по времени ACTIVE привязке субъекта либо OIDC-группы без require_owner, версия роли которой разрешает project.view в подходящей области.
	//go:embed sql/proof_project_authorize_membership.sql
	queryAuthorizeProjectMembership string
	// queryNextAuthorityProofRevision advances the singleton authority-proof sequence and returns its new revision; creating a sequence number does not sign or grant a proof.
	// queryNextAuthorityProofRevision увеличивает последовательность доказательств полномочий единственной установки и возвращает новую ревизию; номер сам не подписывает доказательство и не выдаёт grant.
	//go:embed sql/proof_next_revision.sql
	queryNextAuthorityProofRevision string
	// queryAcceptWorkerGrantHighWatermark persists a worker grant high watermark, advancing only by credential generation/revision or accepting exact same issued/expiry replay; lower revisions are rejected.
	// queryAcceptWorkerGrantHighWatermark сохраняет верхнюю отметку worker grant, продвигая её только по поколению реквизитов и ревизии либо принимая точный повтор времени выдачи и срока; меньшие ревизии отвергает.
	//go:embed sql/proof_worker_grant_accept_high_watermark.sql
	queryAcceptWorkerGrantHighWatermark string
	// queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive resolves an active subject in the requested organization and displays the highest-priority SYSTEM role from an ACTIVE organization binding or MEMBER fallback; it does not check role lifecycle or prove permissions.
	// queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive разрешает активного субъекта запрошенной организации и выводит приоритетную SYSTEM роль из ACTIVE привязки организации либо MEMBER; жизненный цикл роли и разрешения не проверяет.
	//go:embed sql/repository_resolvescope_select_memberships_organization_id_subject_id_active.sql
	queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive string
)
