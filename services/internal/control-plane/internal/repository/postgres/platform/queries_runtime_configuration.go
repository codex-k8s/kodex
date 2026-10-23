package platform

import _ "embed"

var (
	// queryRuntimeConfigurationGetAgentView joins the exact current runtime config/policy, published/latest mutable overlay and agent environment binding with image/tool/policy pins. Only a projectless system-assistant NULL revision binding follows the environment current version; row hints do not replace the caller exact-agent authorization.
	// queryRuntimeConfigurationGetAgentView связывает точные текущие runtime config/policy, опубликованный/последний изменяемый overlay и привязку окружения с image/tool/policy pins. Лишь NULL revision binding системного помощника без проекта следует текущей версии окружения; подсказки строки не заменяют авторизацию точного агента вызывающим.
	//go:embed sql/runtime_configuration__get_agent_view.sql
	queryRuntimeConfigurationGetAgentView string
	// queryRuntimeConfigurationListAgentVersions lists immutable runtime config/policy revisions for an organization agent below the optional version cursor in descending order. The SQL retains its system-assistant/owner/admin or active VIEW membership row condition; the owner query path checks the requested agent independently.
	// queryRuntimeConfigurationListAgentVersions перечисляет неизменяемые runtime config/policy ревизии агента организации ниже необязательного курсора версии по убыванию. SQL сохраняет условие строки system-assistant/owner/admin либо активный VIEW membership; точного агента независимо проверяет owner query path.
	//go:embed sql/runtime_configuration__list_agent_versions.sql
	queryRuntimeConfigurationListAgentVersions string
	// queryRuntimeConfigurationListEnvironments lists nondeleted project environments with their current revision and exact image/tool/resource/network descriptors, applying text/ref/authority-project and catalog project.view checks before ref-keyset pagination. Organization environments use a separate catalog path.
	// queryRuntimeConfigurationListEnvironments перечисляет неудалённые проектные окружения с текущей ревизией и точными image/tool/resource/network дескрипторами, применяя text/ref/authority-project и catalog project.view до ref-keyset страницы. Общие окружения используют отдельный путь каталога.
	//go:embed sql/runtime_configuration__list_environments.sql
	queryRuntimeConfigurationListEnvironments string
	// queryRuntimeConfigurationGetEnvironment reads the current nondeleted organization-scoped environment projection, allowing a nullable project join but requiring an ENVIRONMENT assistant-context projection and a nonempty role parameter. Descriptor metadata is returned without secret bytes; additional owner checks remain in the caller.
	// queryRuntimeConfigurationGetEnvironment читает текущую проекцию неудалённого окружения организации с nullable project join, требуя ENVIRONMENT assistant-context projection и непустой параметр роли. Возвращает метаданные дескрипторов без байтов секретов; дополнительные проверки владельца остаются у вызывающего.
	//go:embed sql/runtime_configuration__get_environment.sql
	queryRuntimeConfigurationGetEnvironment string
	// queryRuntimeConfigurationListEnvironmentVersions lists immutable versions of one exact organization environment below the optional version cursor, with image/tools/policy digests and descriptor metadata. It uses the existing owner/admin or active project VIEW membership row rule and descending-version page limit.
	// queryRuntimeConfigurationListEnvironmentVersions перечисляет неизменяемые версии точного окружения организации ниже необязательного курсора с image/tools/policy digest и метаданными дескрипторов. Использует прежнее условие owner/admin либо активного проектного VIEW membership и лимит страницы по убыванию версии.
	//go:embed sql/runtime_configuration__list_environment_versions.sql
	queryRuntimeConfigurationListEnvironmentVersions string
	// queryRuntimeConfigurationLockAgent locks the exact organization agent row and returns agent/config/overlay/binding versions plus runtime profile and project/system identity. The lock serializes later owner changes but does not authorize a caller by itself.
	// queryRuntimeConfigurationLockAgent блокирует точную строку агента организации и возвращает agent/config/overlay/binding версии, runtime profile и project/system identity. Блокировка согласует последующие изменения владельца, но сама не авторизует вызывающего.
	//go:embed sql/runtime_configuration__lock_agent.sql
	queryRuntimeConfigurationLockAgent string
	// queryRuntimeConfigurationValidateAccounts resolves an enabled runtime profile and counts distinct matching enabled AUTHORIZED organization accounts with a current credential revision. This verifies stored account availability, not fresh model-catalog capability or network/provider health.
	// queryRuntimeConfigurationValidateAccounts разрешает включённый runtime profile и считает различные совпадающие включённые AUTHORIZED аккаунты организации с текущей credential revision. Проверяет сохранённую доступность аккаунта, а не свежие возможности каталога модели и здоровье сети/провайдера.
	//go:embed sql/runtime_configuration__validate_accounts.sql
	queryRuntimeConfigurationValidateAccounts string
	// queryRuntimeConfigurationPublish inserts a provider policy and runtime config at the supplied new version, then points the locked agent at that config while advancing agent version. Actor authority, expected version, catalog checks and transaction commit are prerequisites owned by the command.
	// queryRuntimeConfigurationPublish вставляет policy провайдера и runtime config с переданной новой версией, затем переводит заблокированного агента на config и продвигает версию агента. Полномочия actor, ожидаемая версия, каталог и commit являются предварительными обязанностями команды.
	//go:embed sql/runtime_configuration__publish.sql
	queryRuntimeConfigurationPublish string
	// queryRuntimeConfigurationCreateOverlayDraft creates a DRAFT overlay at max existing version plus one with exact parent/content/digest metadata. The enclosing locked-agent transaction serializes version allocation; this SQL does not validate TOML or authorize the actor.
	// queryRuntimeConfigurationCreateOverlayDraft создаёт DRAFT overlay с max существующей версии плюс один и точными parent/content/digest метаданными. Внешняя транзакция с блокировкой агента согласует выделение версии; SQL не проверяет TOML и не авторизует actor.
	//go:embed sql/runtime_configuration__create_overlay_draft.sql
	queryRuntimeConfigurationCreateOverlayDraft string
	// queryRuntimeConfigurationSupersedeMutableOverlays supersedes only DRAFT/VALID/INVALID overlays for an already resolved agent, leaving published history unchanged. Caller transaction and agent lock determine when this transition is allowed.
	// queryRuntimeConfigurationSupersedeMutableOverlays замещает лишь DRAFT/VALID/INVALID overlays уже разрешённого агента без изменения опубликованной истории. Допустимость перехода определяют транзакция и блокировка агента вызывающим.
	//go:embed sql/runtime_configuration__supersede_mutable_overlays.sql
	queryRuntimeConfigurationSupersedeMutableOverlays string
	// queryRuntimeConfigurationSupersedePublishedOverlay marks the agent published overlay SUPERSEDED before a new publication in the same owner transaction; content/digest/history remain intact and this statement does not switch the agent pointer itself.
	// queryRuntimeConfigurationSupersedePublishedOverlay помечает опубликованный overlay агента SUPERSEDED перед новой публикацией в той же транзакции владельца; content/digest/history сохраняются, указатель агента этот запрос сам не переключает.
	//go:embed sql/runtime_configuration__supersede_published_overlay.sql
	queryRuntimeConfigurationSupersedePublishedOverlay string
	// queryRuntimeConfigurationSupersedeOverlayDraft transitions only the exact agent-owned VALID draft to SUPERSEDED, allowing its affected-row count to fence the subsequent publication. Other drafts or invalid states are untouched.
	// queryRuntimeConfigurationSupersedeOverlayDraft переводит лишь точный VALID-черновик агента в SUPERSEDED, позволяя числом затронутых строк оградить последующую публикацию. Другие черновики и неверные состояния не меняются.
	//go:embed sql/runtime_configuration__supersede_overlay_draft.sql
	queryRuntimeConfigurationSupersedeOverlayDraft string
	// queryRuntimeConfigurationGetOverlayDraft selects and locks the highest-version mutable overlay for the exact organization/agent, returning identity/version/state/content. The enclosing owner path separately locks/authorizes the agent and handles absence.
	// queryRuntimeConfigurationGetOverlayDraft выбирает и блокирует изменяемый overlay максимальной версии точных organization/agent, возвращая identity/version/state/content. Внешний путь владельца отдельно блокирует/авторизует агента и обрабатывает отсутствие.
	//go:embed sql/runtime_configuration__get_overlay_draft.sql
	queryRuntimeConfigurationGetOverlayDraft string
	// queryRuntimeConfigurationValidateOverlay stores caller-computed draft validation state/errors and timestamp only while the row is DRAFT/VALID/INVALID. It records diagnostics rather than parsing or proving the content safe itself.
	// queryRuntimeConfigurationValidateOverlay сохраняет вычисленные вызывающим validation state/errors и время лишь для DRAFT/VALID/INVALID строки. Фиксирует диагностику, а не самостоятельно разбирает содержимое и доказывает его безопасность.
	//go:embed sql/runtime_configuration__validate_overlay.sql
	queryRuntimeConfigurationValidateOverlay string
	// queryRuntimeConfigurationPublishOverlay creates a new PUBLISHED overlay only from the exact agent draft already fenced to SUPERSEDED, storing canonical content/digest and current schema pins before updating the agent pointer. Prior VALID/candidate checks and surrounding atomic commit belong to the command.
	// queryRuntimeConfigurationPublishOverlay создаёт новый PUBLISHED overlay лишь из точного черновика агента, уже ограждённого состоянием SUPERSEDED, сохраняя канонические content/digest и текущие schema pins до обновления указателя агента. Прежние VALID/candidate проверки и общий атомарный commit принадлежат команде.
	//go:embed sql/runtime_configuration__publish_overlay.sql
	queryRuntimeConfigurationPublishOverlay string
	// queryRuntimeConfigurationRollbackOverlay copies an agent-owned previously published or superseded published overlay into a new PUBLISHED revision linked to that source, with fresh schema pins and a new agent pointer. It never rewrites the historical source; current eligibility must be rechecked by the command.
	// queryRuntimeConfigurationRollbackOverlay копирует принадлежащий агенту ранее опубликованный либо superseded published overlay в новую PUBLISHED-ревизию с parent источника, свежими schema pins и новым указателем агента. Исторический источник не переписывает; текущую доступность повторно проверяет команда.
	//go:embed sql/runtime_configuration__rollback_overlay.sql
	queryRuntimeConfigurationRollbackOverlay string
	// queryRuntimeConfigurationCreateEnvironment inserts a project/organization environment and its immutable first version containing non-secret values, secret descriptors, image/tools/policy and digests, returning both IDs and the ref. Activation is a separate step in the same owner transaction; plaintext secrets are absent.
	// queryRuntimeConfigurationCreateEnvironment вставляет проектное/общее окружение и неизменяемую первую версию с несекретными значениями, дескрипторами секретов, image/tools/policy и digest, возвращая оба ID и ref. Активация — отдельный шаг той же транзакции владельца; открытых секретов нет.
	//go:embed sql/runtime_configuration__create_environment.sql
	queryRuntimeConfigurationCreateEnvironment string
	// queryRuntimeConfigurationResolveImageArtifact resolves an exact artifact with matching organization/project/scope on an ACTIVE recipe, requiring ACCEPTED admission, PROMOTED state and a nonempty promoted reference. It returns the stored recipe generation and image pins; tool inventory checks occur separately.
	// queryRuntimeConfigurationResolveImageArtifact разрешает точный артефакт с совпадающими organization/project/scope активного рецепта, требуя ACCEPTED admission, PROMOTED и непустой promoted reference. Возвращает сохранённое поколение рецепта и image pins; inventory инструментов проверяется отдельно.
	//go:embed sql/runtime_configuration__resolve_image_artifact.sql
	queryRuntimeConfigurationResolveImageArtifact string
	// queryRuntimeConfigurationLockEnvironment locks one ACTIVE organization environment and its current revision counters, preserving a nullable project for organization scope. The caller uses the returned version for its exact publication/rollback checks.
	// queryRuntimeConfigurationLockEnvironment блокирует ACTIVE-окружение организации и счётчики текущей ревизии, сохраняя nullable проект для общей области. Вызывающий использует версию для точных проверок публикации/rollback.
	//go:embed sql/runtime_configuration__lock_environment.sql
	queryRuntimeConfigurationLockEnvironment string
	// queryRuntimeConfigurationLockEnvironmentLifecycle locks one nondeleted project environment under the organization/ref and returns its state/version for enable/disable/delete decisions. This query joins a project and is not the organization-environment lifecycle path.
	// queryRuntimeConfigurationLockEnvironmentLifecycle блокирует неудалённое проектное окружение по organization/ref и возвращает state/version для решений enable/disable/delete. Запрос требует project join и не является путём жизненного цикла общего окружения.
	//go:embed sql/runtime_configuration__lock_environment_lifecycle.sql
	queryRuntimeConfigurationLockEnvironmentLifecycle string
	// queryRuntimeConfigurationGetEnvironmentLifecycleSnapshot loads the complete current revision/image/tool/policy snapshot of a nondeleted project environment for its already authorized lifecycle command, including scope and organization ref. It does not mutate lifecycle or independently grant read access.
	// queryRuntimeConfigurationGetEnvironmentLifecycleSnapshot загружает полный текущий revision/image/tool/policy snapshot неудалённого проектного окружения для уже авторизованной lifecycle-команды, включая scope и organization ref. Состояние не меняет и самостоятельного права чтения не предоставляет.
	//go:embed sql/runtime_configuration__get_environment_lifecycle_snapshot.sql
	queryRuntimeConfigurationGetEnvironmentLifecycleSnapshot string
	// queryRuntimeConfigurationUpdateEnvironmentLifecycle updates the already resolved environment only at the exact expected version while not DELETED, increments its version and returns state/version/time. The command selects the allowed target state and owns access/in-use checks.
	// queryRuntimeConfigurationUpdateEnvironmentLifecycle обновляет уже разрешённое окружение лишь при точной ожидаемой версии и состоянии не DELETED, увеличивает версию и возвращает state/version/time. Допустимое целевое состояние, доступ и проверку использования определяет команда.
	//go:embed sql/runtime_configuration__update_environment_lifecycle.sql
	queryRuntimeConfigurationUpdateEnvironmentLifecycle string
	// queryRuntimeConfigurationCountEnvironmentBindings counts every agent binding to an already resolved environment ID, without filtering enabled or active agents, so any retained binding can prevent lifecycle deletion.
	// queryRuntimeConfigurationCountEnvironmentBindings считает все привязки агентов уже разрешённого ID окружения без фильтра включённых и активных агентов, чтобы любая удержанная привязка могла запретить удаление.
	//go:embed sql/runtime_configuration__count_environment_bindings.sql
	queryRuntimeConfigurationCountEnvironmentBindings string
	// queryRuntimeConfigurationPublishEnvironment inserts a new immutable environment revision from validated content/policy descriptors and advances current pointer, metadata and version together. Expected-version and owner checks have already run under the environment lock; this SQL does not issue network or secret grants.
	// queryRuntimeConfigurationPublishEnvironment вставляет новую неизменяемую ревизию окружения из проверенных content/policy дескрипторов и совместно продвигает current pointer, метаданные и версию. Ожидаемая версия и владение уже проверены под блокировкой окружения; SQL не выдаёт сетевых и секретных grants.
	//go:embed sql/runtime_configuration__publish_environment.sql
	queryRuntimeConfigurationPublishEnvironment string
	// queryRuntimeConfigurationRollbackEnvironment copies an exact version of the same environment into a new revision parented to that historical source, preserves its image/tools/secret descriptors and digests, then advances current pointer/version. Environment name/description and historical source remain unchanged.
	// queryRuntimeConfigurationRollbackEnvironment копирует точную версию того же окружения в новую ревизию с parent исторического источника, сохраняет его image/tools/secret descriptors и digest, затем продвигает current pointer/version. Имя/описание окружения и исторический источник не меняются.
	//go:embed sql/runtime_configuration__rollback_environment.sql
	queryRuntimeConfigurationRollbackEnvironment string
	// queryRuntimeConfigurationBindEnvironment resolves an ACTIVE environment with exact same project or both-null organization scope and either the selected version ref or current revision. It updates only the exact agent binding version, pinning the revision and advancing binding digest/actor/time; authorization is owned by the locked-agent command.
	// queryRuntimeConfigurationBindEnvironment разрешает ACTIVE-окружение того же проекта либо both-null общей области и выбранный version ref или текущую ревизию. Обновляет лишь точную версию привязки агента, фиксируя ревизию и продвигая binding digest/actor/time; авторизацией владеет команда с блокировкой агента.
	//go:embed sql/runtime_configuration__bind_environment.sql
	queryRuntimeConfigurationBindEnvironment string
	// queryRuntimeConfigurationBootstrapAgent creates first policy/config/published overlay, reuses or creates the default scoped environment and inserts an environment revision only if no current one exists. Project roles pin that revision while a projectless system-assistant binding follows current by NULL; returned IDs let the caller activate a newly initialized environment without replacing an existing publication.
	// queryRuntimeConfigurationBootstrapAgent создаёт первые policy/config/published overlay, повторно использует либо создаёт scoped окружение по умолчанию и вставляет ревизию лишь при отсутствии текущей. Проектная роль фиксирует ревизию, системный помощник без проекта следует current через NULL; возвращённые ID позволяют активировать новое окружение без замены существующей публикации.
	//go:embed sql/runtime_configuration__bootstrap_agent.sql
	queryRuntimeConfigurationBootstrapAgent string
	// queryRuntimeConfigurationResolveBootstrapImage reuses the current ACTIVE default environment image only when recipe/artifact organization/project identities match and the configured reference/digest is already ACCEPTED/PROMOTED on an ACTIVE recipe. The lookup does not run a fresh build or admission scan.
	// queryRuntimeConfigurationResolveBootstrapImage повторно использует образ текущего ACTIVE-окружения по умолчанию лишь при совпадении organization/project recipe/artifact и уже ACCEPTED/PROMOTED настроенных reference/digest активного рецепта. Новую сборку и admission scan поиск не выполняет.
	//go:embed sql/runtime_configuration__resolve_bootstrap_image.sql
	queryRuntimeConfigurationResolveBootstrapImage string
	// queryRuntimeConfigurationMaterializeSystemImage records the configured platform-owned bootstrap recipe/build/artifact metadata, reusing an exact existing accepted/promoted artifact when available. New metadata carries COMPLETED/ACCEPTED/PROMOTED and platform-owned-bootstrap identity from trusted release inputs; this statement performs no external build, scan or signature verification itself.
	// queryRuntimeConfigurationMaterializeSystemImage сохраняет метаданные настроенных платформенных bootstrap recipe/build/artifact, повторно используя точный существующий accepted/promoted артефакт при наличии. Новые метаданные получают COMPLETED/ACCEPTED/PROMOTED и platform-owned-bootstrap identity из доверенных release inputs; внешнюю сборку, сканирование и проверку подписи запрос сам не выполняет.
	//go:embed sql/runtime_configuration__materialize_system_image.sql
	queryRuntimeConfigurationMaterializeSystemImage string
	// queryRuntimeConfigurationActivateSystemImage sets an ACTIVE recipe current artifact only when exact organization/project/recipe/artifact/generation/spec/reference/digest and accepted/promoted state match. Recipe version/time change only if the pointer changes, preserving idempotent replay.
	// queryRuntimeConfigurationActivateSystemImage назначает текущий артефакт ACTIVE-рецепта лишь при точном совпадении organization/project/recipe/artifact/generation/spec/reference/digest и accepted/promoted. Версия/время рецепта меняются только при изменении указателя, сохраняя идемпотентный повтор.
	//go:embed sql/runtime_configuration__activate_system_image.sql
	queryRuntimeConfigurationActivateSystemImage string
	// queryRuntimeConfigurationActivateEnvironment initializes an environment current-version pointer or accepts the same pointer idempotently; it cannot overwrite a different current revision and does not increment the environment version.
	// queryRuntimeConfigurationActivateEnvironment инициализирует current-version pointer окружения либо идемпотентно принимает прежний; не перезаписывает другую текущую ревизию и не увеличивает версию окружения.
	//go:embed sql/runtime_configuration__activate_environment.sql
	queryRuntimeConfigurationActivateEnvironment string
	// queryRuntimeConfigurationAdvanceBootstrapEnvironment initializes only a still-null environment current revision with an exact null expected-current comparison, preventing role bootstrap from advancing an already published shared environment.
	// queryRuntimeConfigurationAdvanceBootstrapEnvironment инициализирует лишь по-прежнему null текущую ревизию окружения с точным null expected-current сравнением, не позволяя bootstrap роли продвинуть уже опубликованное общее окружение.
	//go:embed sql/runtime_configuration__advance_bootstrap_environment.sql
	queryRuntimeConfigurationAdvanceBootstrapEnvironment string
	// queryRuntimeConfigurationSelectProviderAccount locks enabled AUTHORIZED accounts with current credentials for a READY enabled agent and its exact provider policy candidates. Ordering implements FIXED, active-session LEAST_USED or weight-adjusted usage with stable account-ref ties; fresh model-catalog validation and capacity claiming are separate steps.
	// queryRuntimeConfigurationSelectProviderAccount блокирует включённые AUTHORIZED аккаунты с текущими credentials READY-агента и его точных кандидатов policy провайдера. Порядок реализует FIXED, LEAST_USED по активным сессиям либо использование с учётом веса и стабильным ref при равенстве; свежая проверка каталога модели и захват ёмкости выполняются отдельно.
	//go:embed sql/runtime_configuration__select_provider_account.sql
	queryRuntimeConfigurationSelectProviderAccount string
)
