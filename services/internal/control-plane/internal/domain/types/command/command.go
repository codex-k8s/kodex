// Package command defines the closed command vocabulary and typed inputs and results used by the
// control-plane owner. These declarations carry intent and evidence; authorization, validation, and
// state transitions belong to the command handlers and repository transaction.
// Package command определяет закрытый словарь команд и типизированные входы и результаты владельца
// control-plane. Эти объявления передают намерение и свидетельства; авторизация, проверка и переходы
// состояния принадлежат обработчикам команд и транзакции репозитория.
package command

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Kind is the named string selector for the closed owner-command dispatcher, not a Go-enforced enum.
// Known-command and exact payload/principal checks remain runtime obligations; arbitrary string
// conversion grants no authority.
// Kind — именованный строковый selector закрытого диспетчера команд владельца, не enum, обеспеченный
// Go. Допустимость команды и точные payload и principal проверяются runtime; преобразование строки
// полномочий не даёт.
type Kind string

const (
	// ConfigureRoleImageGitSource selects configuration of a recipe's exact Git
	// connection/repository/ref/path source, not a write to arbitrary repository content.
	// ConfigureRoleImageGitSource выбирает настройку точного Git-источника рецепта: подключение,
	// репозиторий, ref и путь, не запись произвольного содержимого репозитория.
	ConfigureRoleImageGitSource Kind = "CONFIGURE_ROLE_IMAGE_GIT_SOURCE"
	// ConfigureIntegrationDefinitionGitSource selects Git-source binding for an integration definition
	// while preserving its connection version and managed ownership boundary.
	// ConfigureIntegrationDefinitionGitSource выбирает Git-привязку определения интеграции с сохранением
	// версии подключения и границы управляемого владения.
	ConfigureIntegrationDefinitionGitSource Kind = "CONFIGURE_INTEGRATION_DEFINITION_GIT_SOURCE"
	// RefreshRoleImageGitSource selects owner refresh of the configured recipe source; refresh is distinct
	// from validating, building or admitting the image.
	// RefreshRoleImageGitSource выбирает обновление настроенного исходника рецепта владельцем отдельно от
	// проверки, сборки или допуска образа.
	RefreshRoleImageGitSource Kind = "REFRESH_ROLE_IMAGE_GIT_SOURCE"
	// RefreshIntegrationDefinitionGitSource selects rereading a configured integration Git source, not a
	// general remote-fetch command from model-supplied URLs.
	// RefreshIntegrationDefinitionGitSource выбирает перечитывание настроенного Git-исходника интеграции,
	// не общую загрузку URL от модели.
	RefreshIntegrationDefinitionGitSource Kind = "REFRESH_INTEGRATION_DEFINITION_GIT_SOURCE"
)

// ManagedConfigurationGitSourceInput pins a configuration to exact connection version,
// repository/ref/path and content format; these selectors do not authorize arbitrary Git access or
// overwrite source ownership.
// ManagedConfigurationGitSourceInput закрепляет конфигурацию с точными версией подключения,
// репозиторием, ref, путём и форматом; selectors не разрешают произвольный Git-доступ или перезапись
// владения.
type ManagedConfigurationGitSourceInput struct {
	ConfigurationRef, ConnectionRef, RepositoryRef, RefName, Path, ContentFormat string
	ExpectedConnectionVersion                                                    int64
}

// CreateProjectAssistantIntegrationConnection selects the internal project-assistant connection
// operation whose recipient/profile is owner-resolved, not a caller-selected arbitrary agent.
// CreateProjectAssistantIntegrationConnection выбирает внутреннюю операцию подключения помощника
// проекта с получателем и профилем владельца, не произвольным агентом вызывающего.
const CreateProjectAssistantIntegrationConnection Kind = "CREATE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"

// ChangeProjectAssistantIntegrationGrant selects a grant change for the persisted project assistant
// profile rather than accepting recipient authority from the grant payload.
// ChangeProjectAssistantIntegrationGrant выбирает изменение grant сохранённого профиля помощника
// проекта, не принимая полномочия получателя из grant payload.
const ChangeProjectAssistantIntegrationGrant Kind = "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT"

// ProjectAssistantIntegrationGrantInput carries the exact assistant reference and common grant
// settings; the recipient is resolved from the persisted project profile, not chosen by grant payload.
// ProjectAssistantIntegrationGrantInput несёт точную ссылку помощника и общие настройки grant;
// получатель разрешается из сохранённого профиля проекта, не выбирается grant payload.
type ProjectAssistantIntegrationGrantInput struct {
	AssistantRef string
	Grant        SystemAssistantIntegrationGrantInput
}

const (
	// CreateEmailMailboxDraft selects creation of editable mailbox configuration source, distinct from
	// publishing it or allowing a mail effect.
	// CreateEmailMailboxDraft выбирает создание редактируемого исходника настройки ящика отдельно от
	// публикации или разрешения почтового эффекта.
	CreateEmailMailboxDraft Kind = "CREATE_EMAIL_MAILBOX_DRAFT"
	// SaveEmailMailboxDraft selects saving a mailbox draft revision under owner mutation checks, without
	// activating its delivery configuration.
	// SaveEmailMailboxDraft выбирает сохранение ревизии черновика ящика под проверками изменения владельца
	// без активации доставки.
	SaveEmailMailboxDraft Kind = "SAVE_EMAIL_MAILBOX_DRAFT"
	// ValidateEmailMailboxDraft selects explicit mailbox-draft validation and diagnostics, not publication
	// or successful SMTP/MCP execution.
	// ValidateEmailMailboxDraft выбирает явную проверку черновика ящика и диагностику, не публикацию или
	// успешное SMTP/MCP выполнение.
	ValidateEmailMailboxDraft Kind = "VALIDATE_EMAIL_MAILBOX_DRAFT"
	// PublishEmailMailboxDraft selects publication of the exact checked mailbox revision; delivery
	// materialization/readback and effect approvals remain separate boundaries.
	// PublishEmailMailboxDraft выбирает публикацию точной проверенной ревизии ящика; материализация,
	// readback доставки и подтверждения эффектов остаются отдельными.
	PublishEmailMailboxDraft Kind = "PUBLISH_EMAIL_MAILBOX_DRAFT"
	// DiscardEmailMailboxDraft selects discarding the addressed draft, not deleting an active mailbox or
	// its stored messages.
	// DiscardEmailMailboxDraft выбирает отклонение заданного черновика, не удаление активного ящика или
	// хранящихся писем.
	DiscardEmailMailboxDraft Kind = "DISCARD_EMAIL_MAILBOX_DRAFT"
	// BindEmailMailboxConfiguration selects binding a connection to an exact mailbox configuration
	// revision; the identifier does not substitute for owner/version checks.
	// BindEmailMailboxConfiguration выбирает связь подключения с точной ревизией конфигурации ящика; ID
	// проверки владельца и версии не заменяет.
	BindEmailMailboxConfiguration Kind = "BIND_EMAIL_MAILBOX_CONFIGURATION"
	// UnbindEmailMailboxConfiguration selects removal of a managed mailbox configuration binding, not a
	// remote mailbox-management operation.
	// UnbindEmailMailboxConfiguration выбирает снятие управляемой связи конфигурации ящика, не удалённое
	// управление почтовым ящиком.
	UnbindEmailMailboxConfiguration Kind = "UNBIND_EMAIL_MAILBOX_CONFIGURATION"
	// ReconcileEmailEffect selects owner reconciliation of an exact effect receipt/outcome; it is not
	// permission to blindly repeat an uncertain send.
	// ReconcileEmailEffect выбирает согласование точных receipt и исхода эффекта владельцем, не разрешение
	// слепого повтора неопределённой отправки.
	ReconcileEmailEffect Kind = "RECONCILE_EMAIL_EFFECT"
	// ReportEmailEffect selects the worker's exact email-effect report to the owner, distinct from a human
	// approval or a newly requested send.
	// ReportEmailEffect выбирает точный отчёт worker о почтовом эффекте владельцу отдельно от
	// подтверждения человеком или новой отправки.
	ReportEmailEffect Kind = "REPORT_EMAIL_EFFECT"
	// CreateSkillBundleDraft selects creation of versioned skill source in a draft, not immediate skill
	// execution or admission.
	// CreateSkillBundleDraft выбирает создание версионного исходника навыка в черновике, не немедленное
	// исполнение или допуск.
	CreateSkillBundleDraft Kind = "CREATE_SKILL_BUNDLE_DRAFT"
	// SaveSkillBundleDraft selects revision of editable skill source before separate
	// validation/review/publication steps.
	// SaveSkillBundleDraft выбирает изменение редактируемого исходника навыка до отдельных проверки,
	// рассмотрения и публикации.
	SaveSkillBundleDraft Kind = "SAVE_SKILL_BUNDLE_DRAFT"
	// ValidateSkillBundleDraft selects draft checks and diagnostics, not a replacement for skill review or
	// executable admission.
	// ValidateSkillBundleDraft выбирает проверки черновика и диагностику, не заменяя рассмотрение или
	// допуск навыка.
	ValidateSkillBundleDraft Kind = "VALIDATE_SKILL_BUNDLE_DRAFT"
	// ReviewSkillBundleDraft selects recording the addressed skill-review decision against its pinned
	// draft, distinct from publication.
	// ReviewSkillBundleDraft выбирает фиксацию решения рассмотрения точного черновика навыка отдельно от
	// публикации.
	ReviewSkillBundleDraft Kind = "REVIEW_SKILL_BUNDLE_DRAFT"
	// PublishSkillBundleDraft selects owner publication of a checked/reviewed skill revision, not bypass
	// of source or runtime admission.
	// PublishSkillBundleDraft выбирает публикацию проверенной и рассмотренной ревизии навыка владельцем
	// без обхода допуска исходника и runtime.
	PublishSkillBundleDraft Kind = "PUBLISH_SKILL_BUNDLE_DRAFT"
	// DiscardSkillBundleDraft selects discarding the draft revision while leaving other published/history
	// resources to their own lifecycle commands.
	// DiscardSkillBundleDraft выбирает отклонение ревизии черновика, оставляя опубликованные и
	// исторические ресурсы их отдельным lifecycle-командам.
	DiscardSkillBundleDraft Kind = "DISCARD_SKILL_BUNDLE_DRAFT"
	// ArchiveSkillBundle selects moving a skill bundle out of active use without claiming that its exact
	// stored revisions were permanently deleted.
	// ArchiveSkillBundle выбирает вывод пакета навыка из активного использования без утверждения о
	// безвозвратном удалении ревизий.
	ArchiveSkillBundle Kind = "ARCHIVE_SKILL_BUNDLE"
	// RestoreSkillBundle selects restoration of an eligible archived bundle, not reapproval of arbitrary
	// newly supplied executable content.
	// RestoreSkillBundle выбирает восстановление допустимого архивного пакета, не повторное одобрение
	// произвольного нового исполняемого содержимого.
	RestoreSkillBundle Kind = "RESTORE_SKILL_BUNDLE"
	// PurgeSkillBundle selects irreversible skill-bundle cleanup under owner lifecycle checks, distinct
	// from archive or unbind.
	// PurgeSkillBundle выбирает безвозвратную очистку пакета навыка под lifecycle-проверками владельца
	// отдельно от архивации или снятия связи.
	PurgeSkillBundle Kind = "PURGE_SKILL_BUNDLE"
	// CreateMemoryRecord selects creation of a scoped memory record; its content remains data rather than
	// platform instructions or permissions.
	// CreateMemoryRecord выбирает создание scoped-записи памяти; содержимое остаётся данными, не
	// платформенными инструкциями или правами.
	CreateMemoryRecord Kind = "CREATE_MEMORY_RECORD"
	// ReviseMemoryRecord selects a new revision of an existing memory record instead of silently replacing
	// pinned historical content.
	// ReviseMemoryRecord выбирает новую ревизию существующей записи памяти вместо незаметной замены
	// закреплённого исторического содержимого.
	ReviseMemoryRecord Kind = "REVISE_MEMORY_RECORD"
	// ArchiveMemoryRecord selects archival of a memory record while preserving the distinction from
	// permanent purge.
	// ArchiveMemoryRecord выбирает архивацию записи памяти с сохранением отличия от безвозвратной очистки.
	ArchiveMemoryRecord Kind = "ARCHIVE_MEMORY_RECORD"
	// RestoreMemoryRecord selects owner restoration of an eligible memory record without automatically
	// binding it to an agent.
	// RestoreMemoryRecord выбирает восстановление допустимой записи памяти владельцем без автоматической
	// связи с агентом.
	RestoreMemoryRecord Kind = "RESTORE_MEMORY_RECORD"
	// PurgeMemoryRecord selects permanent memory-record removal through its dedicated lifecycle path, not
	// an unrestricted deletion by prompt text.
	// PurgeMemoryRecord выбирает безвозвратное удаление записи памяти через отдельный lifecycle-путь, не
	// неограниченное удаление по тексту промпта.
	PurgeMemoryRecord Kind = "PURGE_MEMORY_RECORD"
	// BindAgentMemoryRecord selects an exact agent-to-memory revision binding; resource names alone do not
	// establish eligibility.
	// BindAgentMemoryRecord выбирает точную связь агента с ревизией памяти; одни имена ресурсов
	// допустимость не доказывают.
	BindAgentMemoryRecord Kind = "BIND_AGENT_MEMORY_RECORD"
	// UnbindAgentMemoryRecord selects removal of the addressed memory binding without deleting the
	// underlying memory record.
	// UnbindAgentMemoryRecord выбирает снятие заданной связи памяти без удаления исходной записи.
	UnbindAgentMemoryRecord Kind = "UNBIND_AGENT_MEMORY_RECORD"
	// BindAgentSkillBundle selects binding an agent to an eligible exact skill revision, not arbitrary
	// filesystem injection.
	// BindAgentSkillBundle выбирает связь агента с допустимой точной ревизией навыка, не произвольное
	// внедрение файлов.
	BindAgentSkillBundle Kind = "BIND_AGENT_SKILL_BUNDLE"
	// UnbindAgentSkillBundle selects removal of an agent skill binding, distinct from bundle archive or
	// purge.
	// UnbindAgentSkillBundle выбирает снятие связи навыка агента отдельно от архивации или очистки пакета.
	UnbindAgentSkillBundle Kind = "UNBIND_AGENT_SKILL_BUNDLE"
	// CompleteOnboarding selects recording installation onboarding completion; it is not proof that every
	// optional dependency or user scenario has been verified.
	// CompleteOnboarding выбирает фиксацию завершения onboarding установки, не доказательство проверки
	// всех optional-зависимостей или сценариев.
	CompleteOnboarding Kind = "COMPLETE_ONBOARDING"
	// CreateProject selects project creation with owner-derived identity and mutation handling, not caller
	// assignment of internal tenant/owner fields.
	// CreateProject выбирает создание проекта с личностью и обработкой изменения владельцем, не назначение
	// внутренних tenant и owner вызывающим.
	CreateProject Kind = "CREATE_PROJECT"
	// CreateProjectFile selects the dedicated prepared/scanned project-file creation path rather than
	// generic content insertion into any project.
	// CreateProjectFile выбирает отдельный путь создания подготовленного и проверенного файла проекта, не
	// общую вставку содержимого в любой проект.
	CreateProjectFile Kind = "CREATE_PROJECT_FILE"
	// CreateProjectFileRevision selects a new exact revision of an existing artifact while keeping
	// source/prepared-upload provenance separate from a filename.
	// CreateProjectFileRevision выбирает новую точную ревизию артефакта, отделяя происхождение исходника и
	// подготовленной загрузки от имени файла.
	CreateProjectFileRevision Kind = "CREATE_PROJECT_FILE_REVISION"
	// UpdateProject selects a versioned metadata update inside the project's owner boundary, not a change
	// of tenant inferred from payload.
	// UpdateProject выбирает версионное изменение метаданных внутри владельца проекта, не смену tenant из
	// payload.
	UpdateProject Kind = "UPDATE_PROJECT"
	// TrashProject selects the recoverable project-trash transition, distinct from immediate irreversible
	// cleanup.
	// TrashProject выбирает восстанавливаемое перемещение проекта в корзину отдельно от немедленной
	// безвозвратной очистки.
	TrashProject Kind = "TRASH_PROJECT"
	// RestoreProject selects recovery of an eligible trashed project under current owner/lifecycle checks.
	// RestoreProject выбирает восстановление допустимого проекта корзины под текущими проверками владельца
	// и жизненного цикла.
	RestoreProject Kind = "RESTORE_PROJECT"
	// PurgeProject selects the project's dedicated irreversible purge lifecycle, not deletion of arbitrary
	// resources through a universal command.
	// PurgeProject выбирает отдельный безвозвратный lifecycle очистки проекта, не удаление любых ресурсов
	// универсальной командой.
	PurgeProject Kind = "PURGE_PROJECT"
	// AddPlatformMembership selects adding organization/platform membership through its role-specific
	// owner operation.
	// AddPlatformMembership выбирает добавление организационного или платформенного членства через
	// отдельную операцию роли владельца.
	AddPlatformMembership Kind = "ADD_PLATFORM_MEMBERSHIP"
	// ChangePlatformMembership selects changing an existing platform membership's role/activity under its
	// version/authority boundary.
	// ChangePlatformMembership выбирает изменение роли или активности существующего платформенного
	// членства под границей версии и полномочий.
	ChangePlatformMembership Kind = "CHANGE_PLATFORM_MEMBERSHIP"
	// RemovePlatformMembership selects revoking a platform membership, not deleting the user's identity or
	// unrelated project membership.
	// RemovePlatformMembership выбирает отзыв платформенного членства, не удаление личности пользователя
	// или несвязанного членства проекта.
	RemovePlatformMembership Kind = "REMOVE_PLATFORM_MEMBERSHIP"
	// AddMembership selects adding a user to one project with explicit permission inputs subject to owner
	// checks.
	// AddMembership выбирает добавление пользователя в проект с явным вводом прав под проверками
	// владельца.
	AddMembership Kind = "ADD_MEMBERSHIP"
	// ChangeMembership selects an existing project's membership permission/activity update, not authority
	// inferred from a user reference.
	// ChangeMembership выбирает изменение прав или активности существующего членства проекта, не
	// полномочия из ссылки пользователя.
	ChangeMembership Kind = "CHANGE_MEMBERSHIP"
	// RemoveMembership selects removing the addressed project membership while preserving separate
	// platform identity and access lifecycles.
	// RemoveMembership выбирает снятие заданного членства проекта с сохранением отдельных жизненных циклов
	// платформенной личности и доступа.
	RemoveMembership Kind = "REMOVE_MEMBERSHIP"
	// CreateAgent selects creation of a project agent from its typed role/runtime/instruction inputs,
	// without granting capabilities merely because names were supplied.
	// CreateAgent выбирает создание агента проекта из типизированных роли, runtime и инструкций без выдачи
	// возможностей по переданным именам.
	CreateAgent Kind = "CREATE_AGENT"
	// CreateProjectAssistant selects creating the project's independent assistant profile/agent rather
	// than a mutable copy of another project's assistant.
	// CreateProjectAssistant выбирает создание независимого профиля и агента помощника проекта, не
	// изменяемой копии чужого помощника.
	CreateProjectAssistant Kind = "CREATE_PROJECT_ASSISTANT"
	// UpdateAgent selects the agent metadata/configuration update path with optional impact-plan
	// selections, not a bypass of approved runtime dependencies.
	// UpdateAgent выбирает обновление агента с optional выбором плана влияния, не обход одобренных
	// runtime-зависимостей.
	UpdateAgent Kind = "UPDATE_AGENT"
	// SetAgentEnabled selects explicit agent availability change; protected system-assistant restrictions
	// remain domain checks.
	// SetAgentEnabled выбирает явное изменение доступности агента; ограничения защищённого системного
	// помощника остаются доменными проверками.
	SetAgentEnabled Kind = "SET_AGENT_ENABLED"
	// ArchiveAgent selects archival of an eligible agent, not unrestricted removal of the protected system
	// assistant.
	// ArchiveAgent выбирает архивацию допустимого агента, не неограниченное удаление защищённого
	// системного помощника.
	ArchiveAgent Kind = "ARCHIVE_AGENT"
	// SetAgentAvatar selects binding an eligible uploaded artifact as the exact agent's avatar, not
	// arbitrary host-path assignment.
	// SetAgentAvatar выбирает привязку допустимого загруженного артефакта как аватара точного агента, не
	// назначение произвольного пути хоста.
	SetAgentAvatar Kind = "SET_AGENT_AVATAR"
	// RemoveAgentAvatar selects removing the avatar binding without implying immediate purge of every
	// referenced artifact revision.
	// RemoveAgentAvatar выбирает снятие связи аватара без немедленной очистки всех связанных ревизий
	// артефакта.
	RemoveAgentAvatar Kind = "REMOVE_AGENT_AVATAR"
	// CreateInstructions selects instruction draft creation; the persisted command label deliberately
	// remains CREATE_INSTRUCTION_DRAFT, not publication.
	// CreateInstructions выбирает создание черновика инструкций; сохранённая метка намеренно остаётся
	// CREATE_INSTRUCTION_DRAFT, не публикацией.
	CreateInstructions Kind = "CREATE_INSTRUCTION_DRAFT"
	// PrepareInstructionsImpact selects preparation of the exact instruction-change impact, separate from
	// applying consumer updates.
	// PrepareInstructionsImpact выбирает подготовку влияния точного изменения инструкций отдельно от
	// применения обновлений потребителей.
	PrepareInstructionsImpact Kind = "PREPARE_INSTRUCTIONS_IMPACT"
	// PreparePromptTemplateImpact selects a version-pinned prompt-template impact proposal, not automatic
	// rebinding or execution.
	// PreparePromptTemplateImpact выбирает версионное предложение влияния шаблона промпта, не
	// автоматическую перепривязку или исполнение.
	PreparePromptTemplateImpact Kind = "PREPARE_PROMPT_TEMPLATE_IMPACT"
	// ValidateInstructions selects explicit instruction-draft validation and diagnostics before any
	// publication transition.
	// ValidateInstructions выбирает явную проверку черновика инструкций и диагностику до перехода
	// публикации.
	ValidateInstructions Kind = "VALIDATE_INSTRUCTION_DRAFT"
	// PublishInstructions selects publishing the exact validated instruction revision, not treating
	// arbitrary chat content as system authority.
	// PublishInstructions выбирает публикацию точной проверенной ревизии инструкций, не принятие
	// произвольного чата как системных полномочий.
	PublishInstructions Kind = "PUBLISH_INSTRUCTION_DRAFT"
	// RollbackInstructions selects owner-controlled selection of an earlier instruction version, not
	// rewriting applied version history.
	// RollbackInstructions выбирает управляемый владельцем выбор прежней версии инструкций, не
	// переписывание применённой истории.
	RollbackInstructions Kind = "ROLLBACK_INSTRUCTIONS"
	// PublishAgentRuntimeConfig selects publication of a staff agent's exact runtime/model/account
	// configuration; assistant-specific owner plans use a distinct command.
	// PublishAgentRuntimeConfig выбирает публикацию точных runtime, модели и аккаунтов сотрудника;
	// owner-планы помощника используют отдельную команду.
	PublishAgentRuntimeConfig Kind = "PUBLISH_AGENT_RUNTIME_CONFIGURATION"
	// PublishAssistantRuntimeConfig selects assistant runtime publication through its owner-versioned plan
	// boundary, not the ordinary staff-agent publication endpoint.
	// PublishAssistantRuntimeConfig выбирает публикацию runtime помощника через границу версионного
	// owner-плана, не обычный endpoint сотрудника.
	PublishAssistantRuntimeConfig Kind = "PUBLISH_ASSISTANT_RUNTIME_CONFIGURATION"
	// CreateSystemAssistantRoleImageRecipe selects creation of the organization assistant's role-image
	// recipe inside its explicit system/organization scope.
	// CreateSystemAssistantRoleImageRecipe выбирает создание рецепта образа общесистемного помощника
	// внутри явной системной и организационной области.
	CreateSystemAssistantRoleImageRecipe Kind = "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
	// UpdateSystemAssistantRoleImageRecipe selects an exact system-assistant recipe/spec update, not
	// project-owned recipe mutation by a forged scope field.
	// UpdateSystemAssistantRoleImageRecipe выбирает изменение точных рецепта и спецификации системного
	// помощника, не изменение рецепта проекта по поддельной области.
	UpdateSystemAssistantRoleImageRecipe Kind = "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
	// CreateConfigOverlayDraft selects creation of editable agent runtime overlay source, not application
	// of unvalidated configuration.
	// CreateConfigOverlayDraft выбирает создание редактируемого runtime overlay агента, не применение
	// непроверенной конфигурации.
	CreateConfigOverlayDraft Kind = "CREATE_CONFIG_OVERLAY_DRAFT"
	// ValidateConfigOverlayDraft selects schema-bound overlay checks and diagnostics without changing
	// effective runtime configuration.
	// ValidateConfigOverlayDraft выбирает привязанные к схеме проверки overlay и диагностику без изменения
	// действующего runtime.
	ValidateConfigOverlayDraft Kind = "VALIDATE_CONFIG_OVERLAY_DRAFT"
	// PublishConfigOverlayDraft selects publication of the exact validated overlay draft through owner
	// mutation handling.
	// PublishConfigOverlayDraft выбирает публикацию точного проверенного черновика overlay через обработку
	// изменения владельцем.
	PublishConfigOverlayDraft Kind = "PUBLISH_CONFIG_OVERLAY_DRAFT"
	// RollbackConfigOverlay selects a prior overlay revision through the explicit lifecycle path, not
	// silent modification of immutable history.
	// RollbackConfigOverlay выбирает прежнюю ревизию overlay через явный lifecycle-путь, не незаметное
	// изменение истории.
	RollbackConfigOverlay Kind = "ROLLBACK_CONFIG_OVERLAY"
	// CreateRuntimeEnvironment selects environment-set creation; its persisted label
	// CREATE_RUNTIME_ENVIRONMENT_SET is distinct from creating a draft or published version.
	// CreateRuntimeEnvironment выбирает создание набора окружения; метка CREATE_RUNTIME_ENVIRONMENT_SET
	// отличается от создания черновика или опубликованной версии.
	CreateRuntimeEnvironment Kind = "CREATE_RUNTIME_ENVIRONMENT_SET"
	// CreateRuntimeEnvironmentDraft selects creation of project environment draft source with exact
	// environment/version inputs.
	// CreateRuntimeEnvironmentDraft выбирает создание исходника черновика окружения проекта с точными
	// окружением и версией.
	CreateRuntimeEnvironmentDraft Kind = "CREATE_RUNTIME_ENVIRONMENT_DRAFT"
	// CreateOrganizationRuntimeEnvironmentDraft selects the distinct organization-owned environment draft
	// path rather than taking project scope from payload.
	// CreateOrganizationRuntimeEnvironmentDraft выбирает отдельный организационный путь черновика
	// окружения, не область проекта из payload.
	CreateOrganizationRuntimeEnvironmentDraft Kind = "CREATE_ORGANIZATION_RUNTIME_ENVIRONMENT_DRAFT"
	// SaveRuntimeEnvironmentDraft selects saving the addressed environment draft revision while leaving
	// the published runtime version unchanged.
	// SaveRuntimeEnvironmentDraft выбирает сохранение заданной ревизии черновика окружения без изменения
	// опубликованной runtime-версии.
	SaveRuntimeEnvironmentDraft Kind = "SAVE_RUNTIME_ENVIRONMENT_DRAFT"
	// ValidateRuntimeEnvironmentDraft selects environment draft validation/readiness diagnostics, not
	// admission of an arbitrary image or tool.
	// ValidateRuntimeEnvironmentDraft выбирает проверку черновика окружения и диагностику готовности, не
	// допуск произвольного образа или инструмента.
	ValidateRuntimeEnvironmentDraft Kind = "VALIDATE_RUNTIME_ENVIRONMENT_DRAFT"
	// PublishRuntimeEnvironmentDraft selects publication of the exact checked draft, retaining
	// image/secret/policy dependency checks in the owner.
	// PublishRuntimeEnvironmentDraft выбирает публикацию точного проверенного черновика с сохранением
	// проверок зависимостей образа, секретов и политики у владельца.
	PublishRuntimeEnvironmentDraft Kind = "PUBLISH_RUNTIME_ENVIRONMENT_DRAFT"
	// DiscardRuntimeEnvironmentDraft selects discarding an editable draft without deleting the
	// environment's published version.
	// DiscardRuntimeEnvironmentDraft выбирает отклонение редактируемого черновика без удаления
	// опубликованной версии окружения.
	DiscardRuntimeEnvironmentDraft Kind = "DISCARD_RUNTIME_ENVIRONMENT_DRAFT"
	// RebindRuntimeEnvironment selects exact environment-version consumer rebinding, not broad
	// reassignment of every agent by a loose environment name.
	// RebindRuntimeEnvironment выбирает точную перепривязку потребителей версии окружения, не
	// переназначение всех агентов по неточному имени.
	RebindRuntimeEnvironment Kind = "REBIND_RUNTIME_ENVIRONMENT"
	// RebindRuntimeSecret selects explicit consumer selections for a pinned logical secret revision rather
	// than automatically switching all credentials.
	// RebindRuntimeSecret выбирает явные потребители закреплённой логической ревизии секрета вместо
	// автоматического переключения всех credentials.
	RebindRuntimeSecret Kind = "REBIND_RUNTIME_SECRET"
	// BindInteractionIdentity selects owner binding of an external source/user identity to an internal
	// subject; external IDs do not authenticate that subject by themselves.
	// BindInteractionIdentity выбирает owner-привязку внешнего источника и пользователя к внутреннему
	// субъекту; внешние ID сами субъект не аутентифицируют.
	BindInteractionIdentity Kind = "BIND_INTERACTION_IDENTITY"
	// RevokeInteractionIdentity selects revocation of the exact external-to-internal identity mapping
	// without deleting unrelated platform identities.
	// RevokeInteractionIdentity выбирает отзыв точного соответствия внешней и внутренней личности без
	// удаления несвязанных платформенных личностей.
	RevokeInteractionIdentity Kind = "REVOKE_INTERACTION_IDENTITY"
	// PublishRuntimeEnvironment selects direct publication of a pinned environment version; its command
	// label names VERSION, not a generic set update.
	// PublishRuntimeEnvironment выбирает прямую публикацию закреплённой версии окружения; метка команды
	// называет VERSION, не общее обновление набора.
	PublishRuntimeEnvironment Kind = "PUBLISH_RUNTIME_ENVIRONMENT_VERSION"
	// RollbackRuntimeEnvironment selects the explicit environment version rollback path while preserving
	// version/dependency provenance.
	// RollbackRuntimeEnvironment выбирает явный путь отката версии окружения с сохранением происхождения
	// версии и зависимостей.
	RollbackRuntimeEnvironment Kind = "ROLLBACK_RUNTIME_ENVIRONMENT"
	// SetRuntimeEnvironmentEnabled selects changing environment availability, not bypassing
	// published-version readiness or admission.
	// SetRuntimeEnvironmentEnabled выбирает изменение доступности окружения без обхода готовности или
	// допуска опубликованной версии.
	SetRuntimeEnvironmentEnabled Kind = "SET_RUNTIME_ENVIRONMENT_ENABLED"
	// DeleteRuntimeEnvironment selects owner-controlled environment deletion after lifecycle/consumer
	// checks, not arbitrary runtime-resource removal.
	// DeleteRuntimeEnvironment выбирает удаление окружения владельцем после lifecycle и consumer проверок,
	// не произвольное удаление runtime-ресурсов.
	DeleteRuntimeEnvironment Kind = "DELETE_RUNTIME_ENVIRONMENT"
	// BindAgentRuntimeEnvironment selects an exact agent/environment/version binding rather than mutable
	// latest-image selection.
	// BindAgentRuntimeEnvironment выбирает точную связь агента, окружения и версии вместо изменяемого
	// выбора latest image.
	BindAgentRuntimeEnvironment Kind = "BIND_AGENT_RUNTIME_ENVIRONMENT"
	// PromoteRoleImage selects the dedicated owner promotion request with exact recipe/artifact/provenance
	// pins, not direct registry push authorization.
	// PromoteRoleImage выбирает отдельный owner-запрос продвижения с точными рецептом, артефактом и
	// происхождением, не прямое разрешение registry push.
	PromoteRoleImage Kind = "PROMOTE_ROLE_IMAGE"
	// ChangeAgentCapability selects an agent capability configuration change; a capability name does not
	// itself grant an external effect.
	// ChangeAgentCapability выбирает изменение настройки возможности агента; имя возможности само внешний
	// эффект не разрешает.
	ChangeAgentCapability Kind = "CHANGE_AGENT_CAPABILITY"
	// ChangeAgentGrant selects updating an agent's exact integration grant binding under owner policy
	// rather than free-form model tool authority.
	// ChangeAgentGrant выбирает изменение точного grant интеграции агента под политикой владельца вместо
	// произвольных tool-полномочий модели.
	ChangeAgentGrant Kind = "CHANGE_AGENT_GRANT"
	// CreateWorkflow selects a project workflow definition draft, not immediate launch of its coordinator
	// or stages.
	// CreateWorkflow выбирает черновик определения процесса проекта, не немедленный запуск координатора
	// или этапов.
	CreateWorkflow Kind = "CREATE_WORKFLOW"
	// UpdateWorkflow selects editing workflow draft metadata/stages; the persisted UPDATE_WORKFLOW_DRAFT
	// label does not change a running revision.
	// UpdateWorkflow выбирает редактирование метаданных и этапов черновика; метка UPDATE_WORKFLOW_DRAFT
	// исполняемую ревизию не меняет.
	UpdateWorkflow Kind = "UPDATE_WORKFLOW_DRAFT"
	// ValidateWorkflow selects explicit draft graph/input/capability checks before publication.
	// ValidateWorkflow выбирает явную проверку графа, входов и возможностей черновика до публикации.
	ValidateWorkflow Kind = "VALIDATE_WORKFLOW_DRAFT"
	// PublishWorkflow selects publication of the exact validated workflow draft for subsequent
	// version-pinned launches.
	// PublishWorkflow выбирает публикацию точного проверенного черновика для последующих версионных
	// запусков.
	PublishWorkflow Kind = "PUBLISH_WORKFLOW_DRAFT"
	// ArchiveWorkflow selects workflow archival, distinct from cancelling already owned run attempts.
	// ArchiveWorkflow выбирает архивацию процесса отдельно от отмены уже принадлежащих владельцу попыток
	// запуска.
	ArchiveWorkflow Kind = "ARCHIVE_WORKFLOW"
	// LaunchRun selects owner creation of an execution graph from the exact target/session/input context,
	// not a worker self-issued lease.
	// LaunchRun выбирает создание графа исполнения владельцем из точных цели, сессии и ввода, не lease,
	// выданный worker самому себе.
	LaunchRun Kind = "LAUNCH_RUN"
	// AddSessionTurn selects a new user turn for the addressed session/run/node with prompt-context pins
	// rather than editing an existing turn's history.
	// AddSessionTurn выбирает новый пользовательский ход заданных сессии, запуска и узла с контекстными
	// привязками, не редактирование истории хода.
	AddSessionTurn Kind = "ADD_SESSION_TURN"
	// CancelRun selects owner cancellation of the exact run graph; task/grant/lease termination belongs to
	// that lifecycle, not client UI state.
	// CancelRun выбирает отмену точного графа владельцем; завершение task, grant и lease принадлежит этому
	// lifecycle, не состоянию UI.
	CancelRun Kind = "CANCEL_RUN"
	// RetryRun selects a new owner-managed attempt for an eligible failed run rather than reusing stale
	// lease/grant identity.
	// RetryRun выбирает новую управляемую владельцем попытку допустимого сбойного запуска вместо старых
	// lease и grant.
	RetryRun Kind = "RETRY_RUN"
	// ResolveOwnerGate selects recording a human decision on the exact gate/version, not interpreting
	// ordinary message text as approval.
	// ResolveOwnerGate выбирает фиксацию решения человека по точным gate и версии, не подтверждение по
	// обычному тексту сообщения.
	ResolveOwnerGate Kind = "RESOLVE_OWNER_GATE"
	// ChangeArtifactBinding selects changing an eligible artifact-to-agent binding without granting
	// arbitrary file reads.
	// ChangeArtifactBinding выбирает изменение допустимой связи артефакта и агента без разрешения
	// произвольного чтения файлов.
	ChangeArtifactBinding Kind = "CHANGE_ARTIFACT_BINDING"
	// DeleteArtifact selects the artifact trash/lifecycle transition, distinct from irreversible
	// object-store purge.
	// DeleteArtifact выбирает перемещение артефакта по lifecycle корзины отдельно от безвозвратной очистки
	// object store.
	DeleteArtifact Kind = "DELETE_ARTIFACT"
	// RestoreArtifact selects restoration of an eligible deleted artifact while retaining revision/content
	// provenance.
	// RestoreArtifact выбирает восстановление допустимого удалённого артефакта с сохранением происхождения
	// ревизии и содержимого.
	RestoreArtifact Kind = "RESTORE_ARTIFACT"
	// PurgeArtifact identifies permanent artifact cleanup; the dedicated owner/object lifecycle remains
	// separate from ordinary metadata deletion.
	// PurgeArtifact обозначает безвозвратную очистку артефакта; отдельный owner/object lifecycle остаётся
	// отличным от обычного удаления метаданных.
	PurgeArtifact Kind = "PURGE_ARTIFACT"
	// CreateAttachmentSetDraft selects an editable attachment manifest for an exact project/purpose before
	// finalization.
	// CreateAttachmentSetDraft выбирает редактируемый manifest вложений точных проекта и назначения до
	// фиксации.
	CreateAttachmentSetDraft Kind = "CREATE_ATTACHMENT_SET_DRAFT"
	// AddAttachmentSetItems selects ordered additions to an existing draft, preserving exact artifact
	// revision/digest selection.
	// AddAttachmentSetItems выбирает упорядоченное добавление в черновик с сохранением точных ревизий и
	// хешей артефактов.
	AddAttachmentSetItems Kind = "ADD_ATTACHMENT_SET_ITEMS"
	// RemoveAttachmentSetItems selects removal from a draft manifest, not deletion of the underlying
	// stored files.
	// RemoveAttachmentSetItems выбирает снятие элементов чернового manifest, не удаление исходных файлов.
	RemoveAttachmentSetItems Kind = "REMOVE_ATTACHMENT_SET_ITEMS"
	// FinalizeAttachmentSet selects sealing the exact checked manifest for use as pinned execution input.
	// FinalizeAttachmentSet выбирает фиксацию точного проверенного manifest как закреплённого ввода
	// исполнения.
	FinalizeAttachmentSet Kind = "FINALIZE_ATTACHMENT_SET"
	// CreateSchedule selects creation of a versioned automation definition with explicit
	// target/timing/session policies, not immediate due-work materialization.
	// CreateSchedule выбирает версионное определение автоматизации с явными целью, временем и политиками
	// сессии, не немедленную материализацию события.
	CreateSchedule Kind = "CREATE_SCHEDULE"
	// UpdateSchedule selects a new automation-definition revision rather than rewriting the provenance of
	// already claimed occurrences.
	// UpdateSchedule выбирает новую ревизию автоматизации вместо перезаписи происхождения уже захваченных
	// событий.
	UpdateSchedule Kind = "UPDATE_SCHEDULE"
	// SetScheduleEnabled selects explicit scheduler availability while leaving exact occurrence claims to
	// their worker lifecycle.
	// SetScheduleEnabled выбирает явную доступность расписания, оставляя захват событий их worker
	// lifecycle.
	SetScheduleEnabled Kind = "SET_SCHEDULE_ENABLED"
	// ArchiveSchedule selects archival of the schedule definition, not a fabricated completion of active
	// scheduled runs.
	// ArchiveSchedule выбирает архивацию расписания, не вымышленное завершение активных запланированных
	// запусков.
	ArchiveSchedule Kind = "ARCHIVE_SCHEDULE"
	// DeleteSchedule selects owner deletion of an eligible schedule through its explicit lifecycle and
	// receipt path.
	// DeleteSchedule выбирает удаление допустимого расписания владельцем через явные lifecycle и receipt.
	DeleteSchedule Kind = "DELETE_SCHEDULE"
	// CreateProviderAccount selects creation of managed provider-account metadata without embedding a raw
	// token in this command kind.
	// CreateProviderAccount выбирает создание метаданных управляемого аккаунта без включения сырого токена
	// в kind команды.
	CreateProviderAccount Kind = "CREATE_PROVIDER_ACCOUNT"
	// StartProviderDeviceAuth selects starting a managed device-code authorization flow, distinct from
	// successful owner login or provider readiness.
	// StartProviderDeviceAuth выбирает начало управляемой device-code авторизации отдельно от успешного
	// входа владельца или готовности провайдера.
	StartProviderDeviceAuth Kind = "START_PROVIDER_DEVICE_AUTHORIZATION"
	// AuthorizeProviderAPIKey selects the managed API-key authorization path; raw credential
	// storage/materialization is not represented by the constant itself.
	// AuthorizeProviderAPIKey выбирает управляемый путь авторизации API key; константа сама хранение или
	// материализацию credentials не представляет.
	AuthorizeProviderAPIKey Kind = "AUTHORIZE_PROVIDER_API_KEY"
	// RefreshProviderAuthorization selects owner-controlled authorization refresh, not an unrestricted
	// direct provider call from an agent.
	// RefreshProviderAuthorization выбирает обновление авторизации владельцем, не неограниченный прямой
	// вызов провайдера агентом.
	RefreshProviderAuthorization Kind = "REFRESH_PROVIDER_AUTHORIZATION"
	// VerifyProviderAuthorization selects an explicit verification attempt for the current account
	// authorization rather than trusting a stored Ready flag alone.
	// VerifyProviderAuthorization выбирает явную проверку текущей авторизации вместо доверия лишь
	// сохранённому Ready-флагу.
	VerifyProviderAuthorization Kind = "VERIFY_PROVIDER_AUTHORIZATION"
	// CancelProviderAccountQueuedWork selects cancellation of the exact chosen queued runs under account
	// blocker/version pins, not unrelated active executions.
	// CancelProviderAccountQueuedWork выбирает отмену точных выбранных queued-запусков под привязками
	// препятствий и версии аккаунта, не несвязанных активных исполнений.
	CancelProviderAccountQueuedWork Kind = "CANCEL_PROVIDER_ACCOUNT_QUEUED_WORK"
	// RevokeProviderAccount selects managed authorization revocation with its owner cleanup lifecycle, not
	// a cosmetic account-state change.
	// RevokeProviderAccount выбирает управляемый отзыв авторизации с owner cleanup lifecycle, не
	// косметическое изменение состояния аккаунта.
	RevokeProviderAccount Kind = "REVOKE_PROVIDER_ACCOUNT"
	// DeleteProviderAccount selects deletion of an eligible account after dependency/credential cleanup
	// handling, distinct from disabling selection.
	// DeleteProviderAccount выбирает удаление допустимого аккаунта после обработки зависимостей и
	// credentials отдельно от отключения выбора.
	DeleteProviderAccount Kind = "DELETE_PROVIDER_ACCOUNT"
	// SetProviderAccountEnabled selects account routing availability, not provider login or a new
	// execution reservation.
	// SetProviderAccountEnabled выбирает доступность аккаунта для маршрутизации, не вход провайдера или
	// новое резервирование исполнения.
	SetProviderAccountEnabled Kind = "SET_PROVIDER_ACCOUNT_ENABLED"
	// SetProviderAccountConcurrency selects owner configuration of the account's simultaneous-execution
	// limit, distinct from claiming capacity.
	// SetProviderAccountConcurrency выбирает настройку владельцем лимита одновременных исполнений отдельно
	// от захвата ёмкости.
	SetProviderAccountConcurrency Kind = "SET_PROVIDER_ACCOUNT_CONCURRENCY"
	// CreateConnection selects a scoped managed integration connection with definition/configuration
	// provenance, not an arbitrary remote endpoint.
	// CreateConnection выбирает scoped-управляемое подключение интеграции с происхождением определения и
	// настроек, не произвольный remote endpoint.
	CreateConnection Kind = "CREATE_INTEGRATION_CONNECTION"
	// UpdateConnection selects a versioned integration-connection metadata/configuration update,
	// preserving credential revision as a separate input.
	// UpdateConnection выбирает версионное обновление метаданных и настроек подключения с отдельным вводом
	// ревизии credentials.
	UpdateConnection Kind = "UPDATE_INTEGRATION_CONNECTION"
	// DeleteConnection selects owner deletion of an eligible connection and its managed lifecycle, not a
	// remote provider-management command.
	// DeleteConnection выбирает удаление допустимого подключения и его управляемого lifecycle владельцем,
	// не удалённое управление провайдером.
	DeleteConnection Kind = "DELETE_INTEGRATION_CONNECTION"
	// ConfigureConnectionCredential selects committing exact materialized connection-credential
	// provenance, not copying plaintext credentials from public configuration.
	// ConfigureConnectionCredential выбирает фиксацию точного материализованного происхождения
	// credentials, не копирование plaintext из публичных настроек.
	ConfigureConnectionCredential Kind = "CONFIGURE_INTEGRATION_CONNECTION_CREDENTIAL"
	// ConfigureEmailCredential selects binding exact mailbox credential metadata/digest under its
	// connection, separate from a send approval.
	// ConfigureEmailCredential выбирает связь точных метаданных и хеша credentials ящика под подключением
	// отдельно от подтверждения отправки.
	ConfigureEmailCredential Kind = "CONFIGURE_EMAIL_MAILBOX_CREDENTIAL"
	// TestConnection selects managed connection-test preparation/work, retaining any required
	// dangerous-effect gate.
	// TestConnection выбирает подготовку и работу управляемой проверки подключения с сохранением
	// обязательного gate опасного эффекта.
	TestConnection Kind = "TEST_INTEGRATION_CONNECTION"
	// SetConnectionEnabled selects connection availability; it does not mint capability grants or bypass
	// readiness.
	// SetConnectionEnabled выбирает доступность подключения; grants возможностей не выпускает и готовность
	// не обходит.
	SetConnectionEnabled Kind = "SET_INTEGRATION_CONNECTION_ENABLED"
	// ChangeIntegrationGrant selects an exact agent/workflow capability/approval-scope binding change, not
	// generic adapter authority.
	// ChangeIntegrationGrant выбирает изменение точной связи агента или процесса с capability и approval
	// scope, не общие полномочия адаптера.
	ChangeIntegrationGrant Kind = "CHANGE_INTEGRATION_GRANT"
	// ChangeSystemAssistantIntegrationGrant selects the organization assistant's explicit grant change,
	// separate from project-assistant recipient resolution.
	// ChangeSystemAssistantIntegrationGrant выбирает явное изменение grant общесистемного помощника
	// отдельно от разрешения получателя проекта.
	ChangeSystemAssistantIntegrationGrant Kind = "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"
	// CreateAssistantConversation selects creation of an owner-scoped assistant session/dialog, not a
	// model turn or a shared global draft.
	// CreateAssistantConversation выбирает создание scoped-сессии и диалога помощника, не ход модели или
	// общий глобальный черновик.
	CreateAssistantConversation Kind = "CREATE_ASSISTANT_CONVERSATION"
	// UpdateAssistantConversation selects a conversation title update; its persisted label explicitly
	// names TITLE, not arbitrary history editing.
	// UpdateAssistantConversation выбирает обновление названия диалога; метка явно называет TITLE, не
	// произвольное редактирование истории.
	UpdateAssistantConversation Kind = "UPDATE_ASSISTANT_CONVERSATION_TITLE"
	// ArchiveAssistantConversation selects moving the addressed dialog out of active use while preserving
	// a separate restoration/purge lifecycle.
	// ArchiveAssistantConversation выбирает вывод заданного диалога из активных с отдельными
	// восстановлением и очисткой.
	ArchiveAssistantConversation Kind = "ARCHIVE_ASSISTANT_CONVERSATION"
	// RestoreAssistantConversation selects owner restoration of an eligible archived dialog without
	// copying another session's history.
	// RestoreAssistantConversation выбирает восстановление допустимого архивного диалога владельцем без
	// копирования истории чужой сессии.
	RestoreAssistantConversation Kind = "RESTORE_ASSISTANT_CONVERSATION"
	// PurgeAssistantConversation selects irreversible cleanup of the exact dialog under owner/session
	// lifecycle checks.
	// PurgeAssistantConversation выбирает безвозвратную очистку точного диалога под проверками владельца и
	// сессии.
	PurgeAssistantConversation Kind = "PURGE_ASSISTANT_CONVERSATION"
	// MoveAssistantConversationToProject selects the explicit dialog project-move operation, not a scope
	// switch inferred from UI context.
	// MoveAssistantConversationToProject выбирает явное перемещение диалога в проект, не смену области из
	// UI-контекста.
	MoveAssistantConversationToProject Kind = "MOVE_ASSISTANT_CONVERSATION_TO_PROJECT"
	// AddAssistantTurn selects adding bounded user content with attachment/delivery-mode pins to one
	// conversation, separate from other concurrent dialogs.
	// AddAssistantTurn выбирает добавление ограниченного пользовательского текста с вложениями и delivery
	// mode в диалог отдельно от других параллельных диалогов.
	AddAssistantTurn Kind = "ADD_ASSISTANT_TURN"
	// CancelAssistantTurn selects owner cancellation of the conversation's exact active turn rather than
	// globally stopping every assistant session.
	// CancelAssistantTurn выбирает отмену владельцем точного активного хода диалога вместо глобальной
	// остановки всех сессий.
	CancelAssistantTurn Kind = "CANCEL_ASSISTANT_TURN"
	// UpdateAssistantPlan selects editing an assistant plan draft, not applying its proposed effects.
	// UpdateAssistantPlan выбирает редактирование черновика плана помощника без применения предлагаемых
	// эффектов.
	UpdateAssistantPlan Kind = "UPDATE_ASSISTANT_PLAN_DRAFT"
	// ValidateAssistantPlan selects exact plan-revision validation and conflict diagnostics before
	// application.
	// ValidateAssistantPlan выбирает проверку точной ревизии плана и диагностику конфликтов до применения.
	ValidateAssistantPlan Kind = "VALIDATE_ASSISTANT_PLAN"
	// ApplyAssistantPlan selects the specialized application of an eligible validated plan revision;
	// proposal text alone is not approval or authority.
	// ApplyAssistantPlan выбирает специализированное применение допустимой проверенной ревизии; текст
	// предложения сам не является подтверждением или полномочиями.
	ApplyAssistantPlan Kind = "APPLY_ASSISTANT_PLAN"
	// RejectAssistantPlan selects recording rejection of the addressed plan revision without silently
	// executing its selected operations.
	// RejectAssistantPlan выбирает фиксацию отклонения заданной ревизии без незаметного исполнения
	// выбранных операций.
	RejectAssistantPlan Kind = "REJECT_ASSISTANT_PLAN"
	// UpdateAssistantInstructions selects updating owner-controlled assistant instructions, not replacing
	// the separately owned platform core prompt.
	// UpdateAssistantInstructions выбирает обновление owner-инструкций помощника, не замену отдельно
	// принадлежащего платформе ядра.
	UpdateAssistantInstructions Kind = "UPDATE_ASSISTANT_OWNER_INSTRUCTIONS"
	// RecoverAssistant selects the explicit system-assistant recovery path, not a grant to mutate
	// arbitrary runtime workloads.
	// RecoverAssistant выбирает явное восстановление системного помощника, не право изменять произвольные
	// runtime-нагрузки.
	RecoverAssistant Kind = "RECOVER_SYSTEM_ASSISTANT"
	// ClaimExecution selects owner allocation of bounded executable work to a worker instance, not
	// caller-chosen actor or session authority.
	// ClaimExecution выбирает выделение владельцем ограниченной работы экземпляру worker, не выбранные
	// вызывающим полномочия исполнителя или сессии.
	ClaimExecution Kind = "CLAIM_EXECUTION"
	// RenewExecution selects renewal of the exact execution lease/fence/generation tuple, not extension of
	// another worker's lease by reference alone.
	// RenewExecution выбирает продление точного набора lease, fence и generation, не чужого lease по одной
	// ссылке.
	RenewExecution Kind = "RENEW_EXECUTION"
	// ReportExecutionProgress selects progress/message/usage reporting under the exact execution lease,
	// without making intermediary text a terminal result.
	// ReportExecutionProgress выбирает отчёт прогресса, сообщения и использования под точным lease без
	// превращения промежуточного текста в terminal-результат.
	ReportExecutionProgress Kind = "REPORT_EXECUTION_PROGRESS"
	// CompleteExecution selects owner finalization of exact execution outcome, usage, artifacts and
	// archive pins; a late worker result cannot create authority by its success flag.
	// CompleteExecution выбирает фиксацию владельцем точных исхода, использования, артефактов и архива;
	// поздний результат worker не создаёт полномочий флагом успеха.
	CompleteExecution Kind = "COMPLETE_EXECUTION"
	// DelegateExecution selects server-owned child-work delegation from the exact parent lease and target
	// context, not a child spawned by payload IDs alone.
	// DelegateExecution выбирает серверное делегирование дочерней работы из точного родительского lease и
	// цели, не дочерний запуск лишь по ID payload.
	DelegateExecution Kind = "DELEGATE_EXECUTION"
	// LaunchWorkflowExecution selects runtime-controller workflow materialization through its exact
	// specialized permission, not the ordinary user launch path.
	// LaunchWorkflowExecution выбирает материализацию процесса runtime-controller с точным
	// специализированным правом, не обычный пользовательский запуск.
	LaunchWorkflowExecution Kind = "LAUNCH_WORKFLOW_EXECUTION"
	// ProposeAssistantPlan selects an execution-bound plan proposal for later owner
	// validation/application, not direct execution of model-suggested mutations.
	// ProposeAssistantPlan выбирает предложение плана, связанное с исполнением, для последующей
	// owner-проверки и применения, не прямые изменения по предложению модели.
	ProposeAssistantPlan Kind = "PROPOSE_ASSISTANT_PLAN"
	// ProposeAssistantMetadata selects a lease-bound assistant title/metadata proposal, preserving owner
	// validation rather than arbitrary session rewriting.
	// ProposeAssistantMetadata выбирает связанное с lease предложение названия или метаданных с проверкой
	// владельца вместо произвольной перезаписи сессии.
	ProposeAssistantMetadata Kind = "PROPOSE_ASSISTANT_METADATA"
	// ProposeRunMetadata selects an exact run title/activity proposal under the current lease, not a new
	// user task or terminal event.
	// ProposeRunMetadata выбирает предложение названия и активности точного запуска под текущим lease, не
	// новую задачу пользователя или terminal event.
	ProposeRunMetadata Kind = "PROPOSE_RUN_METADATA"
	// RecordRunToolCall selects a revisioned, lease-bound safe tool-event projection; recording an event
	// is not permission to invoke that tool.
	// RecordRunToolCall выбирает версионную безопасную проекцию tool event, связанную с lease; запись
	// события инструмент не разрешает.
	RecordRunToolCall Kind = "RECORD_RUN_TOOL_CALL"
	// CompleteSessionSnapshot selects exact archive-task snapshot completion with immutable object/source
	// pins, not arbitrary session export.
	// CompleteSessionSnapshot выбирает завершение снимка точной archive task с неизменяемыми object и
	// source pins, не произвольный экспорт сессии.
	CompleteSessionSnapshot Kind = "COMPLETE_SESSION_SNAPSHOT"
	// CompleteSessionRestore selects archive-task restore readback completion, keeping restored source
	// digest and task fence explicit.
	// CompleteSessionRestore выбирает завершение readback восстановления archive task с явными хешем
	// восстановленного исходника и task fence.
	CompleteSessionRestore Kind = "COMPLETE_SESSION_RESTORE"
	// CompleteSessionPVCDeletion selects reporting exact task-owned PVC deletion, not permission to delete
	// any volume by a supplied name.
	// CompleteSessionPVCDeletion выбирает отчёт удаления PVC точной задачи, не право удалить любой том по
	// переданному имени.
	CompleteSessionPVCDeletion Kind = "COMPLETE_SESSION_PVC_DELETION"
	// CompleteSessionObjectDeletion selects exact archived-object deletion completion under the archive
	// lease and object version pins.
	// CompleteSessionObjectDeletion выбирает завершение удаления точного архивного объекта под archive
	// lease и object version pins.
	CompleteSessionObjectDeletion Kind = "COMPLETE_SESSION_OBJECT_DELETION"
	// FailSessionArchiveTask selects safe failure handling of the exact archive task lease rather than
	// silently marking partial archive work complete.
	// FailSessionArchiveTask выбирает безопасную обработку отказа точного lease archive task без
	// незаметного завершения частичной работы.
	FailSessionArchiveTask Kind = "FAIL_SESSION_ARCHIVE_TASK"
	// MaterializeOccurrence selects due-occurrence materialization by automation-scheduler's exact
	// permission, not arbitrary client schedule launch.
	// MaterializeOccurrence выбирает материализацию события по точному праву automation-scheduler, не
	// произвольный запуск расписания клиентом.
	MaterializeOccurrence Kind = "MATERIALIZE_SCHEDULE_OCCURRENCE"
	// FailScheduleOccurrence selects scheduler-owned failure/retry handling of the exact occurrence
	// lease/generation.
	// FailScheduleOccurrence выбирает scheduler-обработку отказа и повтора точных lease и generation
	// события.
	FailScheduleOccurrence Kind = "FAIL_SCHEDULE_OCCURRENCE"
	// CompleteConnectionTest selects reporting exact managed connection-test outcome under its claim,
	// separate from provider-effect approval.
	// CompleteConnectionTest выбирает отчёт исхода точной управляемой проверки подключения под claim
	// отдельно от подтверждения эффекта провайдера.
	CompleteConnectionTest Kind = "COMPLETE_INTEGRATION_CONNECTION_TEST"
	// CompleteIntegrationInvocation selects the exact invocation's outcome/receipt/effect report,
	// retaining unknown outcome rather than guessing successful or safe-to-retry status.
	// CompleteIntegrationInvocation выбирает отчёт точных исхода, receipt и эффекта вызова с сохранением
	// неизвестного исхода вместо догадки об успехе или безопасности повтора.
	CompleteIntegrationInvocation Kind = "COMPLETE_INTEGRATION_INVOCATION"
	// CompleteInteractionDelivery selects delivery completion with exact external post/thread and
	// unknown/no-effect status, not an unbound outbound message.
	// CompleteInteractionDelivery выбирает завершение доставки с точными внешними post и thread и
	// состояниями unknown или no-effect, не несвязное исходящее сообщение.
	CompleteInteractionDelivery Kind = "COMPLETE_INTERACTION_DELIVERY"
	// AcceptInteractionMessage selects ingestion of an exact bound external event/identity context;
	// message content cannot itself choose internal actor or gate authority.
	// AcceptInteractionMessage выбирает приём точного связанного внешнего события и личности; содержимое
	// само внутреннего исполнителя или gate authority не выбирает.
	AcceptInteractionMessage Kind = "ACCEPT_INTERACTION_MESSAGE"
	// CreateAccessRole selects creation of an access-role definition, not assignment of the role to its
	// creator.
	// CreateAccessRole выбирает создание определения роли доступа без назначения её создателю.
	CreateAccessRole Kind = "CREATE_ACCESS_ROLE"
	// CreateAccessRoleVersion selects a new policy version under an existing role rather than rewriting
	// historical policy in place.
	// CreateAccessRoleVersion выбирает новую версию политики роли вместо перезаписи исторической на месте.
	CreateAccessRoleVersion Kind = "CREATE_ACCESS_ROLE_VERSION"
	// ArchiveAccessRole selects role archival through its dependency/lifecycle checks, distinct from
	// arbitrary deletion of audit history.
	// ArchiveAccessRole выбирает архивацию роли через проверки зависимостей и lifecycle отдельно от
	// произвольного удаления audit history.
	ArchiveAccessRole Kind = "ARCHIVE_ACCESS_ROLE"
	// CreateAccessBinding selects a scoped subject-to-role binding with explicit owner authorization, not
	// permission inferred from a role name.
	// CreateAccessBinding выбирает scoped-связь субъекта и роли с явной owner-авторизацией, не право по
	// имени роли.
	CreateAccessBinding Kind = "CREATE_ACCESS_BINDING"
	// ChangeAccessBinding selects a versioned update of the exact access binding and its scope/role pins.
	// ChangeAccessBinding выбирает версионное изменение точной связи доступа и её scope и role pins.
	ChangeAccessBinding Kind = "CHANGE_ACCESS_BINDING"
	// RevokeAccessBinding selects owner revocation of the addressed access binding, not deletion of the
	// subject's identity.
	// RevokeAccessBinding выбирает отзыв заданной связи доступа владельцем, не удаление личности субъекта.
	RevokeAccessBinding Kind = "REVOKE_ACCESS_BINDING"
	// CreatePromptTemplateDraft selects editable scoped prompt-template creation while retaining separate
	// platform-core ownership.
	// CreatePromptTemplateDraft выбирает создание редактируемого scoped-шаблона с сохранением отдельного
	// владения платформенным ядром.
	CreatePromptTemplateDraft Kind = "CREATE_PROMPT_TEMPLATE_DRAFT"
	// SavePromptTemplateDraft selects a new saved child draft with exact target/context pins, not in-place
	// replacement of published instructions.
	// SavePromptTemplateDraft выбирает новый дочерний сохранённый черновик с точными target и context
	// pins, не замену опубликованных инструкций на месте.
	SavePromptTemplateDraft Kind = "SAVE_PROMPT_TEMPLATE_DRAFT"
	// DiscardPromptTemplateDraft selects discarding the exact prompt draft without modifying the published
	// head.
	// DiscardPromptTemplateDraft выбирает отклонение точного черновика промпта без изменения
	// опубликованной головы.
	DiscardPromptTemplateDraft Kind = "DISCARD_PROMPT_TEMPLATE_DRAFT"
	// SaveRoleImageRevisionDraft selects saving recipe source as a child managed draft, not building or
	// promoting that image.
	// SaveRoleImageRevisionDraft выбирает сохранение исходника рецепта дочерним управляемым черновиком, не
	// сборку или продвижение образа.
	SaveRoleImageRevisionDraft Kind = "SAVE_ROLE_IMAGE_REVISION_DRAFT"
	// DiscardRoleImageRevisionDraft selects draft discard, distinct from deleting a built artifact or
	// revoking an admitted image.
	// DiscardRoleImageRevisionDraft выбирает отклонение черновика отдельно от удаления собранного
	// артефакта или отзыва допущенного образа.
	DiscardRoleImageRevisionDraft Kind = "DISCARD_ROLE_IMAGE_REVISION_DRAFT"
	// SaveIntegrationDefinitionDraft selects saving normalized/unfinished definition source as a draft,
	// not activating connection capabilities.
	// SaveIntegrationDefinitionDraft выбирает сохранение нормализованного или незавершённого исходника
	// определения черновиком без активации возможностей подключения.
	SaveIntegrationDefinitionDraft Kind = "SAVE_INTEGRATION_DEFINITION_DRAFT"
	// DiscardIntegrationDefinitionDraft selects discarding the addressed definition draft while preserving
	// separately published connection state.
	// DiscardIntegrationDefinitionDraft выбирает отклонение заданного черновика определения с сохранением
	// отдельно опубликованного состояния подключения.
	DiscardIntegrationDefinitionDraft Kind = "DISCARD_INTEGRATION_DEFINITION_DRAFT"
	// SaveSystemSTTConfigurationDraft selects saving STT source to a new draft revision, not sending audio
	// or replacing active policy immediately.
	// SaveSystemSTTConfigurationDraft выбирает сохранение STT-исходника новой черновой ревизией без
	// отправки аудио или немедленной замены политики.
	SaveSystemSTTConfigurationDraft Kind = "SAVE_SYSTEM_STT_CONFIGURATION_DRAFT"
	// DiscardSystemSTTConfigurationDraft selects discarding only the addressed STT draft, not revoking
	// unrelated provider accounts.
	// DiscardSystemSTTConfigurationDraft выбирает отклонение лишь заданного STT-черновика без отзыва
	// несвязанных аккаунтов.
	DiscardSystemSTTConfigurationDraft Kind = "DISCARD_SYSTEM_STT_CONFIGURATION_DRAFT"
	// ValidatePromptTemplateDraft selects exact template/context validation and diagnostics before
	// publication.
	// ValidatePromptTemplateDraft выбирает проверку точных шаблона и контекста и диагностику до
	// публикации.
	ValidatePromptTemplateDraft Kind = "VALIDATE_PROMPT_TEMPLATE_DRAFT"
	// PublishPromptTemplateDraft selects publishing the eligible checked prompt revision, not elevating
	// user template content into platform authority.
	// PublishPromptTemplateDraft выбирает публикацию допустимой проверенной ревизии без повышения
	// пользовательского текста до платформенных полномочий.
	PublishPromptTemplateDraft Kind = "PUBLISH_PROMPT_TEMPLATE_DRAFT"
	// RebindPromptTemplate selects explicit pinned consumer rebinding for a published template revision,
	// not a global implicit latest-template switch.
	// RebindPromptTemplate выбирает явную закреплённую перепривязку потребителей опубликованной ревизии
	// вместо общего неявного latest-переключения.
	RebindPromptTemplate Kind = "REBIND_PROMPT_TEMPLATE_CONSUMERS"
	// CreateRoleImageRevisionDraft selects creation of managed role-image recipe source before separate
	// validation/build/admission steps.
	// CreateRoleImageRevisionDraft выбирает создание управляемого исходника рецепта до отдельных проверки,
	// сборки и допуска.
	CreateRoleImageRevisionDraft Kind = "CREATE_ROLE_IMAGE_REVISION_DRAFT"
	// CreateAssistantRoleImageRecipe selects a project assistant's recipe creation with exact
	// agent/version and environment selection.
	// CreateAssistantRoleImageRecipe выбирает создание рецепта помощника проекта с точными агентом,
	// версией и окружением.
	CreateAssistantRoleImageRecipe Kind = "CREATE_ASSISTANT_ROLE_IMAGE_RECIPE"
	// UpdateAssistantRoleImageRecipe selects project-assistant recipe/spec revision without using a
	// generic employee-image update to cross assistant scope.
	// UpdateAssistantRoleImageRecipe выбирает изменение рецепта и спецификации помощника проекта без
	// пересечения области общим обновлением образа сотрудника.
	UpdateAssistantRoleImageRecipe Kind = "UPDATE_ASSISTANT_ROLE_IMAGE_RECIPE"
	// ValidateRoleImageRevision selects recipe-source checks before publication; it is not a successful
	// OCI build or security admission receipt.
	// ValidateRoleImageRevision выбирает проверки исходника рецепта до публикации, не успешную OCI-сборку
	// или security admission receipt.
	ValidateRoleImageRevision Kind = "VALIDATE_ROLE_IMAGE_REVISION_DRAFT"
	// PublishRoleImageRevision selects publication of the checked managed recipe revision, leaving binary
	// build/admission/promotion separate.
	// PublishRoleImageRevision выбирает публикацию проверенной управляемой ревизии рецепта с отдельными
	// сборкой, допуском и продвижением бинарного образа.
	PublishRoleImageRevision Kind = "PUBLISH_ROLE_IMAGE_REVISION_DRAFT"
	// RebindRoleImage selects exact role-image consumer rebinding under impact/version pins rather than
	// replacing every environment by tag.
	// RebindRoleImage выбирает точную перепривязку потребителей образа под impact и version pins вместо
	// замены всех окружений по тегу.
	RebindRoleImage Kind = "REBIND_ROLE_IMAGE_CONSUMERS"
	// PrepareRoleImageImpactPlan selects preparation of an exact image-consumer impact plan without
	// applying its outcomes.
	// PrepareRoleImageImpactPlan выбирает подготовку точного плана влияния на потребителей образа без
	// применения результатов.
	PrepareRoleImageImpactPlan Kind = "PREPARE_ROLE_IMAGE_IMPACT_PLAN"
	// PrepareEnvironmentDraftImpact selects computing a versioned consumer-impact proposal for the
	// addressed environment draft.
	// PrepareEnvironmentDraftImpact выбирает подготовку версионного предложения влияния на потребителей
	// заданного черновика окружения.
	PrepareEnvironmentDraftImpact Kind = "PREPARE_ENVIRONMENT_DRAFT_IMPACT"
	// CreateIntegrationDefinition selects definition draft creation; the command label intentionally ends
	// in DRAFT, not immediate adapter installation.
	// CreateIntegrationDefinition выбирает создание черновика определения; метка намеренно заканчивается
	// DRAFT, не немедленной установкой адаптера.
	CreateIntegrationDefinition Kind = "CREATE_INTEGRATION_DEFINITION_DRAFT"
	// ValidateIntegrationDefinition selects schema/policy validation of definition source, not provider
	// authorization or an external side effect.
	// ValidateIntegrationDefinition выбирает проверку схемы и политики исходника определения, не
	// авторизацию провайдера или внешний эффект.
	ValidateIntegrationDefinition Kind = "VALIDATE_INTEGRATION_DEFINITION_DRAFT"
	// PublishIntegrationDefinition selects publishing an exact checked definition revision through managed
	// ownership.
	// PublishIntegrationDefinition выбирает публикацию точной проверенной ревизии определения через
	// управляемое владение.
	PublishIntegrationDefinition Kind = "PUBLISH_INTEGRATION_DEFINITION_DRAFT"
	// RebindIntegrationDefinition selects explicit version-pinned definition consumer changes, preserving
	// connection/grant provenance checks.
	// RebindIntegrationDefinition выбирает явные версионные изменения потребителей определения с
	// проверками происхождения подключений и grants.
	RebindIntegrationDefinition Kind = "REBIND_INTEGRATION_DEFINITION_CONSUMERS"
	// CreateSystemSTTDraft selects managed STT-policy draft creation without capturing microphone input or
	// issuing model credentials.
	// CreateSystemSTTDraft выбирает создание управляемого STT-черновика без записи микрофона или выдачи
	// credentials модели.
	CreateSystemSTTDraft Kind = "CREATE_SYSTEM_STT_CONFIGURATION_DRAFT"
	// ValidateSystemSTTDraft selects exact STT configuration/schema/capability checks before publication.
	// ValidateSystemSTTDraft выбирает точные проверки STT-конфигурации, схемы и возможностей до
	// публикации.
	ValidateSystemSTTDraft Kind = "VALIDATE_SYSTEM_STT_CONFIGURATION_DRAFT"
	// PublishSystemSTTDraft selects publishing the checked STT configuration revision under the system
	// owner boundary.
	// PublishSystemSTTDraft выбирает публикацию проверенной STT-ревизии внутри границы системного
	// владельца.
	PublishSystemSTTDraft Kind = "PUBLISH_SYSTEM_STT_CONFIGURATION_DRAFT"
	// RebindSystemSTT selects explicit consumer binding to an exact STT revision rather than silent
	// mutation of an active projection.
	// RebindSystemSTT выбирает явную связь потребителя с точной STT-ревизией вместо незаметного изменения
	// активной проекции.
	RebindSystemSTT Kind = "REBIND_SYSTEM_STT_CONSUMERS"
	// DetachGitManagedConfiguration selects the explicit ownership-detach transition required before UI
	// mutation of Git-managed source.
	// DetachGitManagedConfiguration выбирает явный переход снятия Git-владения перед изменением исходника
	// через UI.
	DetachGitManagedConfiguration Kind = "DETACH_GIT_MANAGED_CONFIGURATION"
	// CopyGitManagedConfiguration selects a provenance-preserving UI copy of Git-managed source without
	// writing back to its original owner.
	// CopyGitManagedConfiguration выбирает UI-копию Git-исходника с сохранением происхождения без записи
	// исходному владельцу.
	CopyGitManagedConfiguration Kind = "COPY_GIT_MANAGED_CONFIGURATION"
)

const (
	// CopyRoleImageConfiguration selects a new recipe configuration copy with lineage, not shared mutable
	// ownership of the original set.
	// CopyRoleImageConfiguration выбирает новую копию конфигурации рецепта с происхождением, не общее
	// изменяемое владение исходным набором.
	CopyRoleImageConfiguration Kind = "COPY_ROLE_IMAGE_CONFIGURATION"
	// CopyIntegrationDefinitionConfiguration selects an independent definition configuration copy while
	// preserving exact source lineage.
	// CopyIntegrationDefinitionConfiguration выбирает независимую копию конфигурации определения с
	// сохранением точного происхождения.
	CopyIntegrationDefinitionConfiguration Kind = "COPY_INTEGRATION_DEFINITION_CONFIGURATION"
	// ArchiveRoleImageConfiguration selects managed recipe-configuration archival, not deletion of every
	// built/promoted artifact.
	// ArchiveRoleImageConfiguration выбирает архивацию управляемой конфигурации рецепта без удаления всех
	// собранных и продвинутых артефактов.
	ArchiveRoleImageConfiguration Kind = "ARCHIVE_ROLE_IMAGE_CONFIGURATION"
	// ArchiveIntegrationDefinitionConfiguration selects definition-configuration archival under dependency
	// checks, distinct from remote provider deletion.
	// ArchiveIntegrationDefinitionConfiguration выбирает архивацию конфигурации определения под проверками
	// зависимостей отдельно от удаления у провайдера.
	ArchiveIntegrationDefinitionConfiguration Kind = "ARCHIVE_INTEGRATION_DEFINITION_CONFIGURATION"
)

// CommitProviderCredentialRefresh selects owner commit of exact refreshed credential descriptors under
// the current lease and predecessor pins, not raw token transport.
// CommitProviderCredentialRefresh выбирает owner-фиксацию точных обновлённых дескрипторов credentials
// под текущими lease и predecessor pins, не передачу сырого токена.
const CommitProviderCredentialRefresh Kind = "COMMIT_PROVIDER_CREDENTIAL_REFRESH"

// Command combines a specialized kind with checked principal, semantic mutation and typed payload
// carried as any. The struct itself does not validate kind/payload agreement, ownership, idempotency
// or JSON-safe disclosure.
// Command соединяет специализированный kind с проверенным principal, смысловым изменением и
// типизированным payload через any. Структура сама соответствие kind и payload, владение,
// идемпотентность и безопасность JSON-выдачи не проверяет.
type Command struct {
	Kind      Kind
	Principal value.Principal
	Mutation  value.Mutation
	Payload   any
}

// ProjectInput carries the project reference and editable name/purpose/language; internal
// organization/owner identity is deliberately outside this payload.
// ProjectInput несёт ссылку проекта и редактируемые имя, цель и язык; внутренняя личность организации
// и владельца намеренно вне payload.
type ProjectInput struct{ Ref, Name, Purpose, Language string }

// ProjectLifecycleInput addresses one project for dedicated trash/restore/purge handling; a reference
// is not permission to transition its whole lifecycle.
// ProjectLifecycleInput задаёт проект для отдельных trash, restore и purge; ссылка не разрешает весь
// его жизненный цикл.
type ProjectLifecycleInput struct{ Ref string }

// ResolveProjectPurgeRecovery selects explicit purge-recovery resolution against the exact snapshot
// digest and unknown-outcome acknowledgement.
// ResolveProjectPurgeRecovery выбирает явное решение восстановления очистки по точному хешу снимка и
// подтверждению неизвестного исхода.
const ResolveProjectPurgeRecovery Kind = "RESOLVE_PROJECT_PURGE_RECOVERY"

// ProjectPurgeRecoveryInput pins recovery to an exact project/snapshot digest and an explicit
// unknown-outcome acknowledgement; the boolean is not proof that deletion actually completed.
// ProjectPurgeRecoveryInput закрепляет восстановление с точными проектом, хешем снимка и явным
// признанием неизвестного исхода; флаг не доказывает завершённое удаление.
type ProjectPurgeRecoveryInput struct {
	Ref, SnapshotDigest       string
	AcknowledgeUnknownOutcome bool
}

// PlatformMembershipInput selects exact membership/user and proposed platform role/activity; role
// strings are expected owner-checked configuration, not caller authority.
// PlatformMembershipInput выбирает точные членство, пользователя и предлагаемые платформенные роль и
// активность; строки роли проверяются владельцем, не дают полномочий вызывающему.
type PlatformMembershipInput struct {
	MembershipRef, UserRef, Role string
	Active                       bool
}

// MembershipInput pins project/membership/user and proposed permission/activity changes without
// allowing project membership to assign platform identity.
// MembershipInput закрепляет проект, членство, пользователя и предлагаемые права и активность без
// назначения платформенной личности проектным членством.
type MembershipInput struct {
	ProjectRef, MembershipRef, UserRef string
	Permissions                        []string
	Active                             bool
}

// AgentInput groups editable agent metadata, role/runtime/instructions, initial capabilities and
// optional impact-plan selections. References and SelectedItemRefs remain inputs for owner resolution,
// not approvals or executable grants.
// AgentInput объединяет редактируемые метаданные, роль, runtime, инструкции, исходные возможности и
// optional выбор плана влияния. Ссылки и SelectedItemRefs остаются вводом для разрешения владельцем,
// не подтверждениями или grants.
type AgentInput struct {
	PlanRef                                                                                                 string
	SelectedItemRefs                                                                                        []string
	InitialCapabilities                                                                                     []string
	Ref, ProjectRef, RoleDefinitionRef, Name, Purpose, RoleDescription, AvatarURL, RuntimeRef, Instructions string
	Enabled                                                                                                 bool
}

// AssistantRoleImageRecipeInput pins assistant image creation to project/agent version, recipe name
// and environment selection; image source eligibility remains an owner check.
// AssistantRoleImageRecipeInput закрепляет создание образа помощника с проектом, версией агента,
// именем рецепта и выбором окружения; допустимость исходника проверяет владелец.
type AssistantRoleImageRecipeInput struct {
	ProjectRef, AgentRef, Name string
	AgentVersion               int64
	Environment                entity.RoleEnvironmentSelection
}

// AssistantRoleImageUpdateInput targets an exact project recipe/spec digest with proposed
// name/environment, preventing a loose name from replacing the immutable build provenance by itself.
// AssistantRoleImageUpdateInput задаёт точные рецепт проекта и хеш спецификации с предлагаемыми именем
// и окружением; неточное имя само происхождение сборки не заменяет.
type AssistantRoleImageUpdateInput struct {
	ProjectRef, RecipeRef, Name string
	SpecSHA256                  string
	Environment                 entity.RoleEnvironmentSelection
}

// AgentBindingInput selects one agent/binding and desired enablement; the identifiers do not prove
// either resource is eligible or owned by the caller.
// AgentBindingInput выбирает агента, связь и желаемую активность; идентификаторы допустимость или
// владение вызывающего не доказывают.
type AgentBindingInput struct {
	AgentRef, BindingRef string
	Enabled              bool
}

// AgentAvatarInput links an exact agent to an artifact reference rather than a caller-supplied host
// filename or arbitrary avatar byte path.
// AgentAvatarInput связывает точного агента со ссылкой артефакта вместо имени файла хоста или
// произвольного пути байтов аватара.
type AgentAvatarInput struct{ AgentRef, ArtifactRef string }

// ProjectAssistantConnectionInput is internal confirmed-specialty-plan input pinning
// assistant/organization/project/profile, agent/profile versions and definition provenance around a
// connection proposal; those fields are not public self-assigned ownership.
// ProjectAssistantConnectionInput — внутренний ввод подтверждённого specialty plan с точными
// помощником, организацией, проектом, профилем, версиями агента и профиля и происхождением определения
// вокруг подключения; поля не являются публичным самоназначением владения.
type ProjectAssistantConnectionInput struct {
	AssistantRef, OrganizationRef, ProjectRef, ProfileRef, DefinitionVersion, DefinitionDigest string
	AgentVersion, ProfileVersion                                                               int64
	Connection                                                                                 ConnectionInput
}

// AgentRuntimeConfigurationInput proposes exact agent/runtime-profile/model and provider-policy
// mode/candidates, keeping account catalog provenance separate from raw credentials.
// AgentRuntimeConfigurationInput предлагает точные агента, runtime profile, модель, режим политики и
// кандидатов аккаунтов, отделяя происхождение каталога от сырых credentials.
type AgentRuntimeConfigurationInput struct {
	AgentRef, RuntimeProfileRef, Model, ProviderPolicyMode string
	ProviderAccounts                                       []entity.ProviderAccountCandidate
}

// AssistantRuntimeConfigurationInput is assigned by an owner-versioned assistant plan and adds exact
// runtime-profile provenance, reasoning/web-search settings and assistant scope lineage; it is not the
// staff publication endpoint's general payload.
// AssistantRuntimeConfigurationInput назначается версионным owner-планом помощника и добавляет точное
// происхождение runtime profile, reasoning, web search и scope lineage; общим payload публикации
// сотрудника не является.
type AssistantRuntimeConfigurationInput struct {
	RuntimeProfilePin                                           entity.AssistantRuntimeProfilePin
	Configuration                                               AgentRuntimeConfigurationInput
	ReasoningEffort                                             string
	WebSearchMode                                               string
	ScopeKind, OrganizationRef, ProjectRef, AssistantProfileRef string
}

// SystemAssistantRoleImageInput pins the organization assistant's exact identity/agent version and
// recipe/spec/environment proposal, separate from project assistant image input.
// SystemAssistantRoleImageInput закрепляет точные личность и версию агента общесистемного помощника и
// предложение рецепта, спецификации и окружения отдельно от ввода проекта.
type SystemAssistantRoleImageInput struct {
	SystemAssistantRef, OrganizationRef, RecipeRef, Name string
	SpecSHA256                                           string
	AgentVersion                                         int64
	Environment                                          entity.RoleEnvironmentSelection
}

// ConfigOverlayInput supplies agent overlay source and an optional published-overlay reference for
// validation/publication/rollback selection; text is not an already approved runtime configuration.
// ConfigOverlayInput передаёт исходник overlay агента и optional ссылку опубликованного overlay для
// проверки, публикации или отката; текст ещё не одобренная runtime-конфигурация.
type ConfigOverlayInput struct {
	AgentRef, Content, PublishedOverlayRef string
}

// RuntimeEnvironmentInput proposes scoped environment metadata and an exact image artifact with
// tool/value/secret-binding/policy data; declaration does not admit binaries, expose secret bytes or
// enforce network rules.
// RuntimeEnvironmentInput предлагает scoped-метаданные окружения и точный image artifact с
// инструментами, значениями, связями секретов и политикой; декларация бинарные файлы не допускает,
// секреты не раскрывает и сеть не ограничивает.
type RuntimeEnvironmentInput struct {
	ScopeKind                                                                 string
	Ref, ProjectRef, Name, Description, PublishedVersionRef, ImageArtifactRef string
	Values                                                                    []entity.RuntimeEnvironmentValue
	SecretBindings                                                            []entity.RuntimeSecretBinding
	Tools                                                                     []entity.RuntimeEnvironmentTool
	Policy                                                                    runtimecontract.RuntimeEnvironmentPolicy
}

// RuntimeEnvironmentDraftInput pins draft/environment/project and expected environment version with
// typed specification and optional impact-plan selections; selecting items does not apply them by
// itself.
// RuntimeEnvironmentDraftInput закрепляет draft, окружение, проект и ожидаемую версию с типизированной
// спецификацией и optional выбором impact plan; выбор сам применение не выполняет.
type RuntimeEnvironmentDraftInput struct {
	ScopeKind                            string
	DraftRef, ProjectRef, EnvironmentRef string
	ExpectedEnvironmentVersion           int64
	Specification                        entity.RuntimeEnvironmentDraftSpecification
	PlanRef                              string
	SelectedItemRefs                     []string
}

// RuntimeSecretRebindInput pairs one logical secret/revision with explicit consumer selections so
// rebinding is not an implicit switch of every runtime using the name.
// RuntimeSecretRebindInput связывает логические секрет и ревизию с явными потребителями, не выполняя
// неявное переключение всех runtime по имени.
type RuntimeSecretRebindInput struct {
	SecretRef  string
	Revision   int64
	Selections []entity.RuntimeSecretRebindSelection
}

// MemoryRecordInput carries exact record/project/agent references and typed memory specification;
// record content does not become system instructions or cross-project access.
// MemoryRecordInput несёт точные record, project и agent references и типизированную память;
// содержимое не становится системной инструкцией или межпроектным доступом.
type MemoryRecordInput struct {
	RecordRef, ProjectRef, AgentRef string
	Specification                   entity.MemoryRecordSpecification
}

// SkillBundleInput binds the proposed skill specification or review/lifecycle decision to exact
// project/bundle/revision/digest and comment data; decision text alone is not admission.
// SkillBundleInput связывает спецификацию навыка или review/lifecycle решение с точными проектом,
// пакетом, ревизией, хешем и комментарием; текст решения сам допуск не выдаёт.
type SkillBundleInput struct {
	ProjectRef, BundleRef, RevisionRef, ExpectedDigest, Decision, Comment string
	Specification                                                         entity.SkillBundleSpecification
}

// AgentContextBindingInput pins agent/resource/revision and expected binding version for context
// attachment changes, rather than resolving mutable latest content silently.
// AgentContextBindingInput закрепляет агента, ресурс, ревизию и ожидаемую версию context binding без
// незаметного выбора изменяемого latest content.
type AgentContextBindingInput struct {
	AgentRef, ResourceRef, RevisionRef string
	ExpectedBindingVersion             int64
}

// RuntimeEnvironmentBindingInput selects exact agent/environment/version references; current
// eligibility and owner scope must still be resolved.
// RuntimeEnvironmentBindingInput выбирает точные ссылки агента, окружения и версии; текущая
// допустимость и область владельца ещё требуют разрешения.
type RuntimeEnvironmentBindingInput struct {
	AgentRef, EnvironmentRef, VersionRef string
}

// RuntimeEnvironmentRebindInput combines an exact environment/version with explicit versioned
// consumers, not a request to rewrite all matching names globally.
// RuntimeEnvironmentRebindInput соединяет точные окружение и версию с явными версионными
// потребителями, не запрашивая глобальную перезапись совпавших имён.
type RuntimeEnvironmentRebindInput struct {
	EnvironmentRef, VersionRef string
	Consumers                  []entity.RuntimeEnvironmentConsumer
}

// RuntimeEnvironmentLifecycleInput addresses one environment and proposed availability for its
// dedicated lifecycle command; Enabled does not imply image admission.
// RuntimeEnvironmentLifecycleInput задаёт окружение и предлагаемую доступность отдельной
// lifecycle-команде; Enabled допуска образа не означает.
type RuntimeEnvironmentLifecycleInput struct {
	EnvironmentRef string
	Enabled        bool
}

// RoleImagePromotionInput pins exact recipe/artifact and expected provenance digest for a promotion
// request, not direct permission to push or retag registry content.
// RoleImagePromotionInput закрепляет точные рецепт, артефакт и ожидаемый хеш происхождения запроса
// продвижения, не даёт прямого права push или retag в registry.
type RoleImagePromotionInput struct {
	RecipeRef, ImageArtifactRef, ExpectedProvenanceSHA256 string
}

// WorkflowInput carries project/workflow identity, editable metadata/coordinator and optional typed
// draft definition; a supplied agent reference is not delegation authority.
// WorkflowInput несёт проект, процесс, редактируемые метаданные, координатора и optional
// типизированный черновик; ссылка агента не является полномочием делегирования.
type WorkflowInput struct {
	Ref, ProjectRef, Name, Purpose, CoordinatorAgentRef string
	Draft                                               *entity.WorkflowVersion
}

// LaunchRunInput proposes exact project/target/session plus task/title/source, bounded attachment
// context and structured input; actor/root ownership is assigned outside this payload.
// LaunchRunInput предлагает точные проект, цель, сессию, задачу, название, источник, контекст вложений
// и структурированный ввод; actor и root ownership назначаются вне payload.
type LaunchRunInput struct {
	ProjectRef, Title, TitleSource, Task, SessionRef, Source, AttachmentSetRef, AttachmentPurpose string
	Target                                                                                        entity.RunTarget
	Input                                                                                         map[string]any
}

// SessionTurnInput pins a new task/attachment set to exact session/run/node and expected
// prompt-context digest; it does not edit a completed turn in place.
// SessionTurnInput закрепляет новую задачу и вложения с точными сессией, запуском, узлом и ожидаемым
// хешем контекста; завершённый ход на месте не редактирует.
type SessionTurnInput struct {
	SessionRef, RunRef, NodeRef, Task, AttachmentSetRef string
	ExpectedPromptContextDigest                         string
}

// RunCommandInput supplies an exact run reference and human reason for specialized cancellation/retry
// handling, without carrying an actor or reusable lease.
// RunCommandInput передаёт точную ссылку запуска и причину человека для специализированных отмены или
// повтора без actor или многоразового lease.
type RunCommandInput struct{ RunRef, Reason string }

// GateResolutionInput addresses one gate with decision/comment and optional resolution attachments;
// the enclosing checked principal and versioned mutation supply authorization context, not the
// decision string.
// GateResolutionInput задаёт gate, решение, комментарий и optional вложения ответа; контекст
// авторизации дают проверенный principal и версионное изменение, не строка решения.
type GateResolutionInput struct {
	GateRef, Decision, Comment, AttachmentSetRef string
}

// ArtifactBindingInput selects an exact artifact/agent binding and proposed enablement, leaving
// revision/lifecycle/read eligibility checks to the owner.
// ArtifactBindingInput выбирает точные связь артефакта и агента и активность, оставляя ревизию,
// lifecycle и допустимость чтения владельцу.
type ArtifactBindingInput struct {
	ArtifactRef, AgentRef string
	Enabled               bool
}

// ArtifactLifecycleInput binds an artifact lifecycle request to the exact impact digest rather than
// treating a resource reference as permission for destructive cleanup.
// ArtifactLifecycleInput связывает lifecycle-запрос артефакта с точным хешем влияния, не считая ссылку
// разрешением разрушительной очистки.
type ArtifactLifecycleInput struct{ ArtifactRef, ImpactDigest string }

// AttachmentSetDraftInput supplies the draft's project/purpose/reference, ordered artifact selections
// and insertion position; the owner resolves exact eligible revisions and manifest state.
// AttachmentSetDraftInput передаёт проект, назначение и ссылку черновика, упорядоченные артефакты и
// позицию вставки; точные допустимые ревизии и manifest state разрешает владелец.
type AttachmentSetDraftInput struct {
	ProjectRef, Purpose, AttachmentSetRef string
	ArtifactRefs                          []string
	InsertAfterPosition                   int64
}

// ScheduleInput proposes version-pinned target,
// timing/timezone/DST/misfire/overlap/session/notification policies, automation text and structured
// prompt inputs. It does not compute or claim occurrences, and maps are ordinary mutable data.
// ScheduleInput предлагает закреплённую цель, время, часовой пояс, политики DST, пропусков,
// перекрытия, сессии и уведомлений, текст и prompt inputs. События не вычисляет и не захватывает; maps
// остаются изменяемыми.
type ScheduleInput struct {
	Ref, ProjectRef, Name, Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone, SessionPolicy, NotificationPolicy string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy, AutomationText                                        string
	Target                                                                                                           entity.RunTarget
	TargetVersion                                                                                                    int64
	TargetDigest                                                                                                     string
	Input, PromptInputs                                                                                              map[string]any
	Enabled                                                                                                          bool
}

// ProviderAccountInput carries account metadata/concurrency and exact managed
// authorization/materializer/credential descriptors, with optional blocker/queued-run selections.
// UserCode/verification metadata is sensitive; the shape neither stores a raw API key nor performs
// authentication.
// ProviderAccountInput несёт метаданные, параллельность и точные управляемые дескрипторы авторизации,
// materializer и credentials с optional препятствиями и выбранными queued runs. UserCode и
// verification metadata чувствительны; форма сырой API key не хранит и аутентификацию не выполняет.
type ProviderAccountInput struct {
	MaximumConcurrentExecutions                                            int32
	SelectedRunRefs                                                        []string
	BlockersDigest                                                         string
	AccountRef, DefinitionKey, Name, AuthorizationRef, AuthorizationMethod string
	AuthorizationState, MaterializerAttemptRef, VerificationURI, UserCode  string
	MaterializerAttemptUID, MaterializerAttemptResourceVersion             string
	ExternalAccountMasked, SafeFailureCode                                 string
	AuthorizationExpiresAt                                                 *time.Time
	Credential                                                             *entity.ProviderCredentialDescriptor
	Enabled                                                                bool
}

// ConnectionInput describes a managed integration connection and its credential-revision descriptor.
// PublicConfiguration is arbitrary configuration data, not a guarantee of redaction; the owner
// validates the definition and resolves credential access.
// ConnectionInput описывает управляемое подключение интеграции и дескриптор ревизии реквизитов.
// PublicConfiguration содержит произвольные данные конфигурации и не гарантирует сокрытие секретов;
// владелец проверяет определение и разрешает доступ к реквизитам.
type ConnectionInput struct {
	Ref, DefinitionKey, Name, MaterializationRef string
	PublicConfiguration                          map[string]any
	CredentialRevision                           *entity.IntegrationCredentialRevision
	Enabled                                      bool
}

// IntegrationGrantInput proposes a capability binding for a connection and an agent or workflow.
// Approval policy and scope paths constrain the binding; naming a resource does not establish
// ownership or issue a grant.
// IntegrationGrantInput задаёт предлагаемую привязку возможности подключения к сотруднику или
// процессу. Политика подтверждения и пути области действия ограничивают привязку; указание ресурса не
// доказывает владение и не выдаёт разрешение.
type IntegrationGrantInput struct {
	ConnectionRef, CapabilityKey, AgentRef, WorkflowRef string
	ApprovalPolicy                                      string
	ApprovalScopePaths                                  []string
	Enabled                                             bool
}

// SystemAssistantIntegrationGrantInput proposes an integration capability for the system assistant.
// The owner resolves the recipient and checks connection eligibility and approval scope instead of
// accepting an actor from this payload.
// SystemAssistantIntegrationGrantInput задаёт предлагаемую возможность интеграции для системного
// помощника. Владелец определяет получателя, проверяет допустимость подключения и область
// подтверждения, а не принимает полномочия действующего лица из этих данных.
type SystemAssistantIntegrationGrantInput struct {
	ConnectionRef, CapabilityKey, ApprovalPolicy string
	ApprovalScopePaths                           []string
	Enabled                                      bool
}

// AssistantConversationInput selects the project, assistant scope, and initial screen context of a
// conversation. The owner derives its profile and session within the authenticated boundary; these
// fields cannot select another user's history.
// AssistantConversationInput выбирает проект, область помощника и исходный контекст экрана диалога.
// Владелец определяет профиль и сессию в проверенной области доступа; эти поля не позволяют выбрать
// историю другого пользователя.
type AssistantConversationInput struct {
	ProjectRef, AssistantScope string
	Context                    entity.AssistantContextDescriptor
}

// ProjectAssistantInput describes a project's assistant name, purpose, and instructions. The
// referenced project must be resolved by the owner; no internal actor or ownership assignment is
// supplied here.
// ProjectAssistantInput описывает имя, назначение и инструкции проектного помощника. Указанный проект
// разрешает владелец; внутренний действующий субъект и назначение владения здесь не передаются.
type ProjectAssistantInput struct{ ProjectRef, Name, Purpose, Instructions string }

// AssistantConversationTitleInput requests a title change for one conversation. Resolving that
// conversation and checking its version and ownership remain responsibilities of the command owner.
// AssistantConversationTitleInput запрашивает изменение названия одного диалога. Разрешение диалога,
// проверка его версии и владения остаются обязанностями владельца команды.
type AssistantConversationTitleInput struct{ ConversationRef, Title string }

// AssistantConversationArchiveInput identifies the conversation affected by an archive lifecycle
// command. The outer command kind selects the transition; the identifier alone grants no lifecycle
// permission.
// AssistantConversationArchiveInput указывает диалог для команды жизненного цикла архива. Переход
// выбирает внешний вид команды; сам идентификатор не предоставляет права менять жизненный цикл.
type AssistantConversationArchiveInput struct{ ConversationRef string }

// AssistantConversationProjectInput requests moving a conversation into a project. The owner checks
// access to both the existing conversation and the destination before changing its scope.
// AssistantConversationProjectInput запрашивает перенос диалога в проект. Перед изменением области
// владелец проверяет доступ к существующему диалогу и целевому проекту.
type AssistantConversationProjectInput struct{ ConversationRef, ProjectRef string }

// EmailCredentialInput carries a managed credential descriptor and replay intent for one connection.
// ReplayOnly does not authorize creation of a receipt or replacement of credential bytes; the owner
// verifies the existing operation.
// EmailCredentialInput передаёт дескриптор управляемых реквизитов и намерение повтора для одного
// подключения. ReplayOnly не разрешает создать квитанцию или заменить содержимое реквизитов; владелец
// проверяет существующую операцию.
type EmailCredentialInput struct {
	ConnectionRef string
	Credential    entity.EmailMailboxCredential
	ReplayOnly    bool
}

// AssistantTurnInput supplies conversation content, an attachment-set reference, delivery mode, and
// optional screen context. Queueing or interrupting work requires owner-side session checks; this
// input is not a shared draft between conversations.
// AssistantTurnInput передаёт содержимое сообщения, ссылку на набор вложений, режим доставки и
// необязательный контекст экрана. Очередь или прерывание требуют проверок сессии у владельца; эти
// данные не являются общим черновиком нескольких диалогов.
type AssistantTurnInput struct {
	ConversationRef, Content, AttachmentSetRef, DeliveryMode string
	Context                                                  *entity.AssistantContextDescriptor
}

// AssistantTurnCancellationInput identifies the conversation whose active turn should be cancelled.
// The owner resolves the exact active work and closes its associated claims rather than accepting
// arbitrary run identifiers.
// AssistantTurnCancellationInput указывает диалог, активный ход которого требуется отменить. Владелец
// определяет точную активную работу и закрывает связанные права исполнения, а не принимает
// произвольные идентификаторы запусков.
type AssistantTurnCancellationInput struct{ ConversationRef string }

// AssistantPlanInput pins a plan and its revision for validation or application. The command must use
// that exact revision rather than silently substituting the latest proposal.
// AssistantPlanInput закрепляет план и его ревизию для проверки или применения. Команда должна
// использовать именно эту ревизию, а не незаметно подменять её последним предложением.
type AssistantPlanInput struct {
	PlanRef  string
	Revision int64
}

// ProjectFileInput supplies file metadata and either content or an owner-prepared artifact receipt.
// FileName is not a filesystem destination, and prepared references and digests require validation
// before publication; raw content may contain private data.
// ProjectFileInput передаёт метаданные файла и содержимое либо подготовленную владельцем квитанцию
// материала. FileName не является путём назначения; подготовленные ссылки и дайджесты проверяются до
// публикации, а исходное содержимое может включать персональные данные.
type ProjectFileInput struct {
	ProjectRef, FileName, MediaType, SHA256, ContentRef string
	PreparedLedgerID                                    string
	SizeBytes                                           int64
	Content                                             []byte
	Prepared                                            *PreparedArtifact
}

// ProjectFileRevisionInput describes a new revision of an existing artifact with source, content, and
// preparation pins. The owner validates the artifact boundary and receipt consistency; supplying a
// locator or digest does not establish admission.
// ProjectFileRevisionInput описывает новую ревизию существующего материала с привязками к источнику,
// содержимому и подготовке. Владелец проверяет область материала и согласованность квитанции; указание
// адреса или дайджеста не означает допуска.
type ProjectFileRevisionInput struct {
	ArtifactRef, MediaType, Digest, ContentRef, SourceRevisionRef string
	SizeBytes                                                     int64
	Content                                                       []byte
	Prepared                                                      *PreparedArtifact
	PreparedLedgerID, PreparedRevisionRef                         string
}

// AssistantPlanDraftInput supplies a proposed plan summary, operations, and prepared content. These
// values stage a proposal rather than approving or executing mutations; the owner pins prepared
// content to the resulting revision.
// AssistantPlanDraftInput передаёт предлагаемые описание плана, операции и подготовленное содержимое.
// Эти данные формируют предложение, но не подтверждают и не исполняют изменения; владелец закрепляет
// подготовленное содержимое за итоговой ревизией.
type AssistantPlanDraftInput struct {
	PlanRef, Summary string
	Operations       []entity.AssistantPlanOperation
	PreparedContent  map[string]string
}

// AssistantInstructionsInput carries editable assistant instructions. Applying this overlay does not
// replace platform-owned core instructions or waive the owner's configuration checks.
// AssistantInstructionsInput передаёт редактируемые инструкции помощника. Применение этого слоя не
// заменяет принадлежащие платформе основные инструкции и не отменяет проверки конфигурации владельцем.
type AssistantInstructionsInput struct{ Instructions string }

// LeaseInput carries the workload, lease, fence, and generation pins used by worker lifecycle
// operations, with optional progress and financial observations. Reported observations are not
// authority to claim work, approve spending, or bypass an expired lease.
// LeaseInput передаёт привязки нагрузки, аренды, барьера и поколения для операций исполнителя, а также
// необязательные наблюдения о ходе и финансах. Эти наблюдения не дают права захватить работу,
// согласовать расходы или обойти истёкшую аренду.
type LeaseInput struct {
	Financial                         *entity.ProviderFinancialSummary
	WorkloadInstance, LeaseRef, Fence string
	Generation                        int64
	Limit                             int32
	Progress                          string
	Message                           *entity.RunMessage
}

// ProviderCredentialRefreshInput links the current lease and previous credential revision to the
// proposed replacement Secret descriptor. The owner checks predecessor and generation consistency;
// this structure carries identifiers and digests, not raw provider tokens.
// ProviderCredentialRefreshInput связывает текущую аренду и предыдущую ревизию реквизитов с
// предлагаемым дескриптором нового Secret. Владелец проверяет предшественника и поколение; структура
// передаёт идентификаторы и дайджесты, а не исходные токены провайдера.
type ProviderCredentialRefreshInput struct {
	LeaseRef, Fence, PreviousCredentialRevisionRef, PreviousContentSHA256 string
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256           string
	Generation                                                            int64
}

// CompleteExecutionInput reports a fenced execution outcome, usage, archive metadata, and produced
// artifacts. Worker-reported success and a relative archive path do not establish a terminal
// transition or host filesystem authority; the owner validates and commits the outcome.
// CompleteExecutionInput сообщает ограждённый результат исполнения, использование, метаданные архива и
// созданные материалы. Заявленный исполнителем успех и относительный путь архива не устанавливают
// конечный переход и не дают доступа к файловой системе узла; владелец проверяет и фиксирует
// результат.
type CompleteExecutionInput struct {
	LeaseRef, Fence, ResultSummary, SafeErrorCode string
	CodexSessionID, ArchiveRelativePath           string
	ArchiveSHA256                                 string
	ArchiveSizeBytes                              int64
	Generation                                    int64
	Success                                       bool
	Usage                                         entity.TokenUsage
	Artifacts                                     []CompletedArtifact
}

// CompletedArtifact describes bytes or a prepared receipt produced by an execution. The owner must
// bind the artifact to the execution and verify its preparation and scan evidence before treating it
// as admitted content.
// CompletedArtifact описывает содержимое или подготовленную квитанцию материала, созданного
// исполнением. Владелец связывает материал с исполнением и проверяет подготовку и свидетельства
// сканирования, прежде чем считать содержимое допущенным.
type CompletedArtifact struct {
	FileName, MediaType, SHA256 string
	SizeBytes                   int64
	Content                     []byte
	Prepared                    *PreparedArtifact
}

// PreparedArtifact records an internal storage object version, content digest, size, and scan and
// preview evidence. It is a receipt-shaped value, not a verifier or an automatically safe public
// projection; the owner validates its provenance and exact object binding.
// PreparedArtifact хранит версию внутреннего объекта хранилища, дайджест, размер и свидетельства
// сканирования и предпросмотра. Это данные квитанции, а не проверяющий механизм и не автоматически
// безопасное публичное представление; владелец проверяет происхождение и точную привязку к объекту.
type PreparedArtifact struct {
	Ref, ObjectKey, ObjectVersion, ObjectETag, MediaType, Digest, ScanState, PreviewState string
	SizeBytes                                                                             int64
	Antivirus                                                                             value.ArtifactScan
}

// DelegateInput proposes child work under the current lease, fence, and generation for a target agent
// or workflow step. The owner creates the delegation edge and preserves root actor and policy; payload
// references do not prove that lineage.
// DelegateInput предлагает дочернюю работу для сотрудника или этапа процесса в рамках текущих аренды,
// барьера и поколения. Владелец создаёт ребро делегирования и сохраняет корневого субъекта и политику;
// ссылки в данных не доказывают это происхождение.
type DelegateInput struct {
	LeaseRef, Fence, TargetAgentRef, WorkflowStepKey, Task string
	Generation                                             int64
	Input                                                  map[string]any
}

// ProposeAssistantPlanInput reports a fenced assistant proposal with operations and prepared content.
// It creates a versioned proposal through the owner rather than applying the proposed mutations or
// supplying approval.
// ProposeAssistantPlanInput сообщает ограждённое предложение помощника с операциями и подготовленным
// содержимым. Оно создаёт версионированное предложение через владельца, а не применяет предлагаемые
// изменения и не передаёт подтверждение.
type ProposeAssistantPlanInput struct {
	LeaseRef, Fence, Summary string
	Generation               int64
	Operations               []entity.AssistantPlanOperation
	PreparedContent          map[string]string
}

// ProposeAssistantMetadataInput reports a conversation title proposal from a fenced execution. The
// owner validates the active attempt and decides whether the metadata can be applied; a title is not a
// new user instruction.
// ProposeAssistantMetadataInput сообщает предложение названия диалога от ограждённого исполнения.
// Владелец проверяет активную попытку и допустимость применения метаданных; название не является новой
// инструкцией пользователя.
type ProposeAssistantMetadataInput struct {
	LeaseRef, Fence, Title string
	Generation             int64
}

// ProposeRunMetadataInput reports a run title and activity summary under a lease generation. These
// descriptive values do not change execution state or authorize a continuation without the owner's
// lifecycle checks.
// ProposeRunMetadataInput сообщает название запуска и описание деятельности в рамках поколения аренды.
// Эти описательные данные не меняют состояние исполнения и не разрешают продолжение без проверок
// жизненного цикла владельцем.
type ProposeRunMetadataInput struct {
	LeaseRef, Fence, Title, ActivitySummary string
	Generation                              int64
}

// RunToolCallInput records a fenced tool-call observation with capability, grant, state, timing, and
// revision pins. Recording the observation does not authorize tool execution; names such as
// SafeParameters and SafeResult do not themselves redact their values.
// RunToolCallInput фиксирует ограждённое наблюдение вызова инструмента с привязками возможности,
// разрешения, состояния, времени и ревизии. Запись наблюдения не разрешает исполнение инструмента;
// названия SafeParameters и SafeResult сами по себе не скрывают содержимое значений.
type RunToolCallInput struct {
	LeaseRef, Fence, CallRef, Tool, CapabilityRef, GrantRef, State, SafeResult string
	Generation, DurationMS, Revision                                           int64
	SafeParameters                                                             map[string]any
}

// SessionArchiveTaskInput reports archive-task fencing and exact storage-object and restored-source
// metadata. Object and restored-source hashes describe different bytes; neither a PVC name nor an
// object key grants arbitrary restore or deletion authority.
// SessionArchiveTaskInput сообщает ограждение задачи архива и точные метаданные объекта хранилища и
// восстановленного источника. Хеши объекта и восстановленного источника относятся к разным байтам; имя
// PVC или ключ объекта не дают произвольных прав восстановления и удаления.
type SessionArchiveTaskInput struct {
	TaskRef, LeaseRef, Fence, SafeErrorCode            string
	ObjectKey, ObjectVersion, ObjectETag, ObjectDigest string
	RestoredSourceSHA256, PVCName                      string
	Generation, ObjectSizeBytes, SourceSizeBytes       int64
	FormatVersion                                      uint32
}

// WarmRuntimeInput reports the state of a workload instance for one runtime revision. The owner
// resolves the corresponding warm runtime and validates the transition rather than accepting an actor
// or project scope from the report.
// WarmRuntimeInput сообщает состояние экземпляра нагрузки для одной ревизии исполнения. Владелец
// разрешает соответствующее прогретое исполнение и проверяет переход, а не принимает действующего
// субъекта или область проекта из отчёта.
type WarmRuntimeInput struct{ WorkloadInstance, RuntimeRevision, State, SafeErrorCode string }

// OccurrenceInput reports a scheduled occurrence outcome under its lease and generation. Retryable is
// an observation for the owner's retry policy, not permission to reopen a terminal occurrence or
// bypass its attempt checks.
// OccurrenceInput сообщает результат планового срабатывания в рамках аренды и поколения. Retryable
// является наблюдением для политики повторов владельца, а не разрешением открыть конечное срабатывание
// заново или обойти проверки попытки.
type OccurrenceInput struct {
	OccurrenceRef, LeaseRef, Fence, SafeErrorCode string
	Generation                                    int64
	Retryable                                     bool
}

// IntegrationInvocationInput reports an integration effect with lease pins, receipt, effect key, input
// and response digests, and outcome. UnknownOutcome must remain distinguishable from confirmed
// failure; the report does not justify a blind retry or promise exactly-once delivery.
// IntegrationInvocationInput сообщает внешний эффект интеграции с привязками аренды, квитанции, ключа
// эффекта, дайджестов входа и ответа и результата. UnknownOutcome должен отличаться от подтверждённого
// отказа; отчёт не оправдывает слепой повтор и не гарантирует доставку ровно один раз.
type IntegrationInvocationInput struct {
	InvocationRef, LeaseRef, Fence, ResultSummary, SafeErrorCode          string
	ReceiptRef, EffectKey, InputDigest, ProviderEffectRef, ResponseDigest string
	Generation                                                            int64
	Success                                                               bool
	UnknownOutcome                                                        bool
}

// IntegrationConnectionTestInput reports the fenced result of testing a managed connection. A
// successful probe describes connectivity and does not approve a separate human-gated integration
// operation.
// IntegrationConnectionTestInput сообщает ограждённый результат проверки управляемого подключения.
// Успешная проба характеризует связь, но не подтверждает отдельную операцию интеграции, требующую
// решения человека.
type IntegrationConnectionTestInput struct {
	TestRef, LeaseRef, Fence, ResultSummary, SafeErrorCode string
	Generation                                             int64
	Success                                                bool
}

// InteractionDeliveryInput reports a fenced external message delivery, including unknown outcome or
// confirmed absence of effect. The structure does not enforce mutually exclusive flags; the owner
// classifies the evidence before deciding whether another attempt is safe.
// InteractionDeliveryInput сообщает ограждённую доставку внешнего сообщения, включая неизвестный исход
// или подтверждённое отсутствие эффекта. Структура не обеспечивает взаимоисключение признаков;
// владелец классифицирует свидетельства перед решением о безопасности новой попытки.
type InteractionDeliveryInput struct {
	ExternalTeamRef, ExternalChannelRef                                             string
	DeliveryRef, LeaseRef, Fence, ExternalPostRef, ExternalThreadRef, SafeErrorCode string
	Generation                                                                      int64
	Success                                                                         bool
	UnknownOutcome, ConfirmedNoEffect                                               bool
}

// InteractionMessageInput carries an external interaction and its routing and expected-gate-version
// references. Message text and identifiers are not authenticated actor or approval authority; the
// owner verifies the connection and identity binding before applying a decision.
// InteractionMessageInput передаёт внешнее сообщение со ссылками маршрутизации и ожидаемой версией
// шлюза подтверждения. Текст и идентификаторы не удостоверяют субъекта и право подтверждения; владелец
// проверяет подключение и привязку личности до применения решения.
type InteractionMessageInput struct {
	ConnectionRef, ExternalEventRef, ExternalPostRef, ExternalRootPostRef string
	ExternalChannelRef, ExternalUserDigest, Message, Decision             string
	ExternalTeamRef, GateRef, RunRef                                      string
	ExpectedGateVersion                                                   int64
}

// InteractionIdentityInput describes a proposed mapping from an external integration identity to a
// platform subject. The owner checks connection scope and authorization; a supplied subject reference
// is not self-authentication.
// InteractionIdentityInput описывает предлагаемую привязку внешней личности интеграции к субъекту
// платформы. Владелец проверяет область подключения и полномочия; переданная ссылка на субъекта не
// является самостоятельной аутентификацией.
type InteractionIdentityInput struct {
	IdentityRef, ConnectionRef, ExternalTeamRef, ExternalChannelRef, ExternalUserDigest, SubjectRef string
}

// ManagedConfigurationInput supplies configuration content, definition and revision pins, affected
// consumers, and optional prompt scope for a managed change. Its Kind describes the configuration, not
// the outer command; ownership, lineage, and approval are resolved by the owner.
// ManagedConfigurationInput передаёт содержимое конфигурации, привязки определения и ревизии,
// затронутых потребителей и необязательную область промпта для управляемого изменения. Kind описывает
// конфигурацию, а не внешнюю команду; владение, происхождение и подтверждение определяет владелец.
type ManagedConfigurationInput struct {
	RecipeRef, DefinitionKey, DefinitionVersion, DefinitionDigest                               string
	PromptScope                                                                                 *PromptTemplateScopeInput
	PlanRef                                                                                     string
	SelectedItemRefs                                                                            []string
	ConfigurationRef, ProjectRef, Name, Kind, ContentFormat, Content, RevisionRef, ImpactDigest string
	Consumers                                                                                   []entity.ManagedConfigurationConsumer
}

// PromptTemplateScopeInput pins the target, agent, workflow revision and stage, template kind, and
// expected context digest of a prompt change. These dependencies identify the intended snapshot but do
// not confer authority to edit it.
// PromptTemplateScopeInput закрепляет цель, сотрудника, ревизию и этап процесса, вид шаблона и
// ожидаемый дайджест контекста изменения промпта. Эти зависимости определяют предполагаемый снимок, но
// не предоставляют права его редактирования.
type PromptTemplateScopeInput struct {
	TargetKind, TargetRef, AgentRef, WorkflowRevisionRef, WorkflowStageKey, ExpectedContextDigest, TemplateKind string
}

// EmailMailboxInput requests managed-mailbox configuration against an expected connection version.
// This configuration transition is separate from, and cannot substitute for, approval of an exact
// outgoing email.
// EmailMailboxInput запрашивает настройку управляемого ящика относительно ожидаемой версии
// подключения. Этот переход конфигурации отделён от подтверждения точного исходящего письма и не может
// его заменить.
type EmailMailboxInput struct {
	ConnectionRef             string
	ExpectedConnectionVersion int64
	Managed                   ManagedConfigurationInput
}

// Result aggregates optional owner-command outputs, including replay receipts and internal runtime
// values. It is not an exclusive union or a universally safe transport response: each boundary must
// select its typed projection, and Duplicate does not authorize repeating an effect.
// Result объединяет необязательные результаты команд владельца, включая квитанции повторов и
// внутренние данные исполнения. Это не исключающее объединение и не универсально безопасный
// транспортный ответ: каждая граница выбирает типизированное представление, а Duplicate не разрешает
// повторить эффект.
type Result struct {
	ProjectPurgeRecovery *entity.ProjectPurgeRecovery
	// ExecutionDeadlineExpired reports that the owner committed expiry; late effects remain forbidden.
	// ExecutionDeadlineExpired сообщает о зафиксированном владельцем истечении; поздние эффекты запрещены.
	ExecutionDeadlineExpired  bool `json:"executionDeadlineExpired,omitempty"`
	ProviderQueuedWorkResults []entity.ProviderQueuedWorkCancellationResult
	EmailMailbox              *entity.EmailMailboxConfigurationView
	EmailPublication          *entity.EmailMailboxPublication
	EmailConnectionVersion    int64
	EmailReceipt              *entity.EmailEffectReceipt
	EmailDecision             *entity.EmailReconciliationDecision
	SkillBundle               *entity.SkillBundle
	MemoryRecord              *entity.KodexMemoryRecord
	ContextBinding            *entity.AgentContextBinding
	InteractionIdentity       *entity.InteractionIdentity
	EnvironmentBindings       []entity.AgentRuntimeEnvironmentBinding
	RuntimeEnvironmentDraft   *entity.RuntimeEnvironmentDraft
	Project                   *entity.Project
	Membership                *entity.Membership
	Agent                     *entity.Agent
	RuntimeConfiguration      *entity.AgentRuntimeConfigurationView
	RuntimeEnvironment        *entity.RuntimeEnvironmentSet
	RuntimeEnvironments       []entity.RuntimeEnvironmentSet
	Workflow                  *entity.Workflow
	Run                       *entity.Run
	Graph                     *entity.RunGraph
	Gate                      *entity.OwnerGate
	Artifact                  *entity.Artifact
	AttachmentSet             *entity.AttachmentSet
	Schedule                  *entity.Schedule
	Connection                *entity.IntegrationConnection
	Conversation              *entity.AssistantConversation
	EmailCredential           *entity.EmailMailboxCredential
	Plan                      *entity.AssistantPlan
	PlanReceipt               *entity.AssistantPlanReceipt
	Assistant                 *entity.SystemAssistant
	ProjectAssistant          *entity.ProjectAssistantProfile
	Event                     *entity.RunEvent
	CreatedRefs               []string
	Duplicate                 bool
	Runtime                   map[string]any
	RuntimeItems              []map[string]any
	AccessRole                *entity.AccessRole
	AccessBinding             *entity.AccessBinding
	ProviderAccount           *entity.ProviderAccount
	PromotionReceipt          *entity.RoleImagePromotionReceipt
	ManagedConfiguration      *entity.ManagedConfigurationSet
	ConfigurationWriteBack    *entity.ConfigurationWriteBack
	ManagedRevision           *entity.ManagedConfigurationRevision
	RoleImageImpactPlan       *entity.RoleImageImpactPlan
	RevisionImpactPlan        *entity.RevisionImpactPlan
}

// EmailReconciliationInput records a reconciliation outcome and note for one receipt pinned by its
// expected digest. The owner verifies the exact receipt and evidence; this report is neither textual
// send approval nor permission to repeat an uncertain send.
// EmailReconciliationInput фиксирует исход сверки и примечание для одной квитанции, закреплённой
// ожидаемым дайджестом. Владелец проверяет точную квитанцию и свидетельства; этот отчёт не является
// текстовым подтверждением отправки или разрешением повторить отправку с неизвестным исходом.
type EmailReconciliationInput struct {
	ReceiptRef, ExpectedReceiptDigest, Outcome, Note string
}
