package platform

import _ "embed"

var (
	// queryManagedConfigurationCASConsumer inserts an absent consumer binding or updates an exact expected version and revision, returning only the changed row; caller validates owner and target before this CAS.
	// queryManagedConfigurationCASConsumer создаёт отсутствующую привязку потребителя либо обновляет точные ожидаемые версию и ревизию, возвращая только изменённую строку; владелец и цель проверяются вызывающим кодом до CAS.
	//go:embed sql/managed_configuration_cas_consumer.sql
	queryManagedConfigurationCASConsumer string
	// queryManagedConfigurationDiscardRevision marks a DRAFT, INVALID or VALID managed revision DISCARDED within exact organization and set, returning its stored content and diagnostics.
	// queryManagedConfigurationDiscardRevision помечает DRAFT, INVALID или VALID управляемую ревизию DISCARDED в точных организации и наборе, возвращая сохранённые содержимое и диагностику.
	//go:embed sql/managed_configuration_discard_revision.sql
	queryManagedConfigurationDiscardRevision string
	// querySTTRuntimeActor resolves the active initiating actor of a run root under organization and run ref for STT; this read alone does not authorize transcription.
	// querySTTRuntimeActor разрешает активного инициатора корневого запуска по организации и ссылке для STT; одно это чтение не разрешает транскрибацию.
	//go:embed sql/stt_runtime_actor.sql
	querySTTRuntimeActor string
	// queryManagedConfigurationCurrentRevision reads one managed revision by organization, set and internal revision ID, including content, digest, parent and validation metadata.
	// queryManagedConfigurationCurrentRevision читает одну управляемую ревизию по организации, набору и внутреннему ID с содержимым, дайджестом, родителем и данными проверки.
	//go:embed sql/managed_configuration_current_revision.sql
	queryManagedConfigurationCurrentRevision string
	// queryManagedConfigurationList pages nonarchived managed sets visible to the actor, enforcing project scope and extra integration.manage visibility for email mailbox sets.
	// queryManagedConfigurationList выдаёт страницы неархивных управляемых наборов, видимых автору, соблюдая область проекта и дополнительное право integration.manage для почтовых наборов.
	//go:embed sql/managed_configuration_list.sql
	queryManagedConfigurationList string
	// queryManagedConfigurationInsertSet creates a managed set only in an ACTIVE project for project kinds or at organization scope for closed system kinds, retaining source ownership metadata.
	// queryManagedConfigurationInsertSet создаёт управляемый набор только в ACTIVE проекте для проектных видов либо на уровне организации для закрытых системных видов, сохраняя происхождение управления.
	//go:embed sql/managed_configuration_insert_set.sql
	queryManagedConfigurationInsertSet string
	// queryManagedConfigurationLockSet locks one managed set by organization and ref and returns its source, current revision, lifecycle and version for owner mutation.
	// queryManagedConfigurationLockSet блокирует один управляемый набор по организации и ссылке и возвращает источник, текущую ревизию, состояние и версию для изменения владельцем.
	//go:embed sql/managed_configuration_lock_set.sql
	queryManagedConfigurationLockSet string
	// queryManagedConfigurationInsertRevision appends a DRAFT revision with next stored number, exact content digest and optional parent for a set; transaction controls concurrent revisions.
	// queryManagedConfigurationInsertRevision добавляет DRAFT ревизию со следующим сохранённым номером, точным дайджестом содержимого и необязательным родителем набора; конкурентность ревизий контролирует транзакция.
	//go:embed sql/managed_configuration_insert_revision.sql
	queryManagedConfigurationInsertRevision string
	// queryManagedConfigurationTouchSet advances a managed set version at expected version and records update time without changing its current revision.
	// queryManagedConfigurationTouchSet увеличивает версию управляемого набора при ожидаемой версии и записывает время изменения без смены текущей ревизии.
	//go:embed sql/managed_configuration_touch_set.sql
	queryManagedConfigurationTouchSet string
	// queryManagedConfigurationLockRevision locks one revision by organization, set and ref with stored content, digest and parent for exact owner lifecycle checks.
	// queryManagedConfigurationLockRevision блокирует одну ревизию по организации, набору и ссылке с сохранёнными содержимым, дайджестом и родителем для точных проверок жизненного цикла владельцем.
	//go:embed sql/managed_configuration_lock_revision.sql
	queryManagedConfigurationLockRevision string
	// queryManagedConfigurationValidateRevision records validation state, diagnostics and time only for a DRAFT or INVALID revision; the validation decision is supplied by the caller.
	// queryManagedConfigurationValidateRevision сохраняет состояние проверки, диагностику и время только для DRAFT или INVALID ревизии; решение проверки задаёт вызывающий код.
	//go:embed sql/managed_configuration_validate_revision.sql
	queryManagedConfigurationValidateRevision string
	// queryManagedConfigurationPublishRevision supersedes the current PUBLISHED revision, publishes a VALID revision and advances the set at expected version, returning a row only when publication and set update succeed.
	// queryManagedConfigurationPublishRevision переводит текущую PUBLISHED ревизию в SUPERSEDED, публикует VALID ревизию и увеличивает версию набора при ожидаемой версии, возвращая строку лишь при успешной публикации и обновлении набора.
	//go:embed sql/managed_configuration_publish_revision.sql
	queryManagedConfigurationPublishRevision string
	// queryManagedConfigurationValidateConsumer checks existence of the requested closed consumer kind within the organization and optional project; it does not assign permission to bind that consumer.
	// queryManagedConfigurationValidateConsumer проверяет существование потребителя запрошенного закрытого вида в организации и необязательном проекте; право привязать потребителя не назначает.
	//go:embed sql/managed_configuration_validate_consumer.sql
	queryManagedConfigurationValidateConsumer string
	// queryManagedConfigurationRebindConsumer upserts a consumer binding to one revision and increments version on conflict without an expected-version predicate; caller must choose when this path is authorized.
	// queryManagedConfigurationRebindConsumer создаёт или заменяет привязку потребителя к одной ревизии и увеличивает версию при конфликте без предиката ожидаемой версии; вызывающий код выбирает допустимость этого пути.
	//go:embed sql/managed_configuration_rebind_consumer.sql
	queryManagedConfigurationRebindConsumer string
	// queryManagedConfigurationListHistory lists revisions of one managed set in descending order before an optional revision cursor, with total revision count per row.
	// queryManagedConfigurationListHistory выдаёт ревизии одного управляемого набора по убыванию до необязательного курсора номера с общим числом ревизий в каждой строке.
	//go:embed sql/managed_configuration_list_history.sql
	queryManagedConfigurationListHistory string
	// queryManagedConfigurationListBindings pages eligible consumer bindings with actor visibility and computes a digest of the full binding snapshot, not merely the visible page.
	// queryManagedConfigurationListBindings выдаёт страницы допустимых привязок потребителей с проверкой видимости автора и вычисляет дайджест полного снимка привязок, а не одной видимой страницы.
	//go:embed sql/managed_configuration_list_bindings.sql
	queryManagedConfigurationListBindings string
	// queryManagedConfigurationDetach detaches a GIT-managed set at expected version into UI ownership, clearing its source revision; content remains in stored revisions.
	// queryManagedConfigurationDetach отделяет управляемый из Git набор при ожидаемой версии под управление UI, очищая ревизию источника; содержимое остаётся в сохранённых ревизиях.
	//go:embed sql/managed_configuration_detach.sql
	queryManagedConfigurationDetach string
	// queryManagedConfigurationCopy copies a GIT-managed set at expected version into a new UI-owned set with a DRAFT child revision and source lineage.
	// queryManagedConfigurationCopy копирует управляемый из Git набор при ожидаемой версии в новый набор под управлением UI с дочерней DRAFT ревизией и происхождением источника.
	//go:embed sql/managed_configuration_copy.sql
	queryManagedConfigurationCopy string
	// queryManagedConfigurationGetSTT reads the bound SYSTEM_STT revision and provider account/credential readiness fields for the STT service; it does not perform a provider request.
	// queryManagedConfigurationGetSTT читает привязанную SYSTEM_STT ревизию и поля готовности аккаунта и реквизитов провайдера для STT-сервиса; запрос к провайдеру не выполняет.
	//go:embed sql/managed_configuration_get_stt.sql
	queryManagedConfigurationGetSTT string
	// queryManagedConfigurationAccessTarget resolves a managed set project ref and kind by organization/ref for the caller to apply a kind-specific access rule.
	// queryManagedConfigurationAccessTarget разрешает проект и вид управляемого набора по организации и ссылке для применения вызывающим кодом правила доступа этого вида.
	//go:embed sql/managed_configuration_access_target.sql
	queryManagedConfigurationAccessTarget string
	// queryManagedConfigurationEffectivePrompt selects the highest-priority effective prompt revision for one agent from a bound template or published instruction, preferring the template.
	// queryManagedConfigurationEffectivePrompt выбирает приоритетную действующую ревизию prompt одного агента из привязанного шаблона либо опубликованной инструкции, предпочитая шаблон.
	//go:embed sql/managed_configuration_effective_prompt.sql
	queryManagedConfigurationEffectivePrompt string
	// queryManagedConfigurationGetConsumerBinding reads a published or superseded binding for an eligible runtime environment or active integration connection; other consumer kinds return no row.
	// queryManagedConfigurationGetConsumerBinding читает PUBLISHED либо SUPERSEDED привязку допустимого runtime-окружения или активного подключения интеграции; другие виды потребителей не дают строку.
	//go:embed sql/managed_configuration_get_consumer_binding.sql
	queryManagedConfigurationGetConsumerBinding string
	// queryManagedConfigurationConsumerBindingExists reports whether any binding exists for the exact organization, configuration kind, consumer kind and ref without checking the bound revision state.
	// queryManagedConfigurationConsumerBindingExists сообщает, существует ли привязка по точным организации, виду конфигурации, виду и ссылке потребителя, не проверяя состояние связанной ревизии.
	//go:embed sql/managed_configuration_consumer_binding_exists.sql
	queryManagedConfigurationConsumerBindingExists string
)
