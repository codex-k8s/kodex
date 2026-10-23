package platform

import _ "embed"

var (
	// queryContextBindingsList lists at most 129 enabled memory or published skill bindings for an agent after actor visibility and source-run checks; each resource type uses its own eligibility predicate.
	// queryContextBindingsList возвращает не более 129 включённых привязок памяти или опубликованных навыков сотрудника после проверок видимости для субъекта и исходного запуска; у каждого вида свой предикат допустимости.
	//go:embed sql/context_bindings_list.sql
	queryContextBindingsList string
	// queryContextBindingSkillFiles reads a skill revision's file manifest by tenant, bundle and revision refs; it does not fetch or authorize file bytes by itself.
	// queryContextBindingSkillFiles читает манифест файлов ревизии навыка по организации и ссылкам комплекта и ревизии; сами байты файлов не получает и не разрешает.
	//go:embed sql/context_binding_skill_files.sql
	queryContextBindingSkillFiles string
	// queryContextBindingGet locks one agent context binding for the resolved memory or skill resource and returns version, enabled state and revision ref.
	// queryContextBindingGet блокирует одну контекстную привязку сотрудника для разрешённой памяти или навыка и возвращает версию, состояние включения и ссылку ревизии.
	//go:embed sql/context_binding_get.sql
	queryContextBindingGet string
	// queryContextBindingCount counts enabled context bindings for an exact tenant and agent; the count is not an authorization decision for a new binding.
	// queryContextBindingCount считает включённые контекстные привязки точных организации и сотрудника; число не является решением о разрешении новой привязки.
	//go:embed sql/context_binding_count.sql
	queryContextBindingCount string
	// queryContextBindingInsert inserts one binding with pinned memory or skill revision and supplied tenant/project/agent/actor IDs, returning its initial version.
	// queryContextBindingInsert создаёт одну привязку с закреплённой ревизией памяти или навыка и заданными ID организации, проекта, сотрудника и субъекта, возвращая исходную версию.
	//go:embed sql/context_binding_insert.sql
	queryContextBindingInsert string
	// queryContextBindingUpdate changes enabled state and pinned revision of one tenant binding only at its expected version, advancing version and update time.
	// queryContextBindingUpdate меняет включение и закреплённую ревизию одной привязки организации только при ожидаемой версии, увеличивая версию и время обновления.
	//go:embed sql/context_binding_update.sql
	queryContextBindingUpdate string
	// queryContextBindingMemoryTarget shares a memory record and chosen revision while returning an eligibility flag requiring ACTIVE state and unexpired chosen/current retention; caller must inspect the flag.
	// queryContextBindingMemoryTarget разделяемо блокирует запись памяти и выбранную ревизию и возвращает признак допустимости: ACTIVE и неистёкшее хранение выбранной и текущей ревизий; признак проверяет вызывающий код.
	//go:embed sql/context_binding_memory_target.sql
	queryContextBindingMemoryTarget string
	// queryContextBindingSkillTarget shares a skill bundle and chosen revision while returning an eligibility flag requiring ACTIVE, PUBLISHED and CLEAN; caller must inspect the flag.
	// queryContextBindingSkillTarget разделяемо блокирует комплект навыка и выбранную ревизию и возвращает признак допустимости ACTIVE, PUBLISHED и CLEAN; признак проверяет вызывающий код.
	//go:embed sql/context_binding_skill_target.sql
	queryContextBindingSkillTarget string
	// querySkillBundleList pages visible skill bundle projections by ref with actor/project/agent filters and independent current/draft revision visibility, returning page and total.
	// querySkillBundleList выдаёт страницы видимых проекций комплектов навыков по ссылке с фильтрами субъекта, проекта и сотрудника и отдельной видимостью текущей и черновой ревизий, возвращая страницу и итог.
	//go:embed sql/skill_bundle_list.sql
	querySkillBundleList string
	// querySkillRevisionList pages skill revisions of one tenant bundle in descending revision order only when each revision is visible to the actor.
	// querySkillRevisionList выдаёт ревизии одного комплекта организации страницами по убыванию номера лишь при видимости каждой ревизии для субъекта.
	//go:embed sql/skill_revision_list.sql
	querySkillRevisionList string
	// querySkillRevisionTransition transitions a revision only from the expected state and digest, optionally recording reviewer, time and comment; it does not move the bundle pointer.
	// querySkillRevisionTransition переводит ревизию лишь из ожидаемого состояния и при совпадении дайджеста, при необходимости записывает рецензента, время и комментарий; указатель комплекта не меняет.
	//go:embed sql/skill_revision_transition.sql
	querySkillRevisionTransition string
	// querySkillBundleTransition transitions a bundle at its expected version and optionally publishes its draft or clears the draft pointer, advancing version.
	// querySkillBundleTransition переводит комплект при ожидаемой версии и при необходимости публикует черновик либо очищает указатель на него, увеличивая версию.
	//go:embed sql/skill_bundle_transition.sql
	querySkillBundleTransition string
	// querySkillBundleDisableBindings disables all still-enabled bindings to a tenant skill bundle and advances each binding version; historical rows remain.
	// querySkillBundleDisableBindings отключает все ещё включённые привязки к комплекту навыков организации и увеличивает версию каждой; исторические строки остаются.
	//go:embed sql/skill_bundle_disable_bindings.sql
	querySkillBundleDisableBindings string
	// querySkillBundlePurge clears file manifests of all nonempty revisions in a tenant bundle during purge; it does not delete external artifact bytes.
	// querySkillBundlePurge очищает непустые манифесты файлов всех ревизий комплекта организации при удалении; внешние байты артефактов не удаляет.
	//go:embed sql/skill_bundle_purge.sql
	querySkillBundlePurge string
	// querySkillArtifactContent reads exact object coordinates for an ACTIVE and CLEAN artifact revision while matching content digest/size and sharing the head lock; bytes remain in object storage.
	// querySkillArtifactContent читает точные координаты объекта для ACTIVE и CLEAN ревизии артефакта при совпадении дайджеста и размера содержимого с разделяемой блокировкой заголовка; байты остаются в объектном хранилище.
	//go:embed sql/skill_artifact_content.sql
	querySkillArtifactContent string
	// querySkillRevisionValidate records validation state, scanner metadata and diagnostics for a mutable draft-like revision with matching digest, resetting prior review metadata.
	// querySkillRevisionValidate записывает состояние проверки, данные сканера и диагностику изменяемой черновой ревизии при совпадении дайджеста, сбрасывая прежние данные рецензирования.
	//go:embed sql/skill_revision_validate.sql
	querySkillRevisionValidate string
	// querySkillBundleGet reads one tenant skill bundle projection and its current/draft revision IDs by ref; caller performs actor eligibility separately.
	// querySkillBundleGet читает проекцию одного комплекта навыков организации и ID текущей и черновой ревизий по ссылке; доступ субъекта вызывающий код проверяет отдельно.
	//go:embed sql/skill_bundle_get.sql
	querySkillBundleGet string
	// querySkillBundleInsert creates a skill bundle only inside an ACTIVE project of the tenant, pinning creator and returning its ID.
	// querySkillBundleInsert создаёт комплект навыков только в ACTIVE проекте организации, закрепляет создателя и возвращает ID.
	//go:embed sql/skill_bundle_insert.sql
	querySkillBundleInsert string
	// querySkillBundleLock locks one tenant skill bundle by ref and reads version/state for a subsequent owner transaction.
	// querySkillBundleLock блокирует один комплект навыков организации по ссылке и читает версию и состояние для последующей транзакции владельца данных.
	//go:embed sql/skill_bundle_lock.sql
	querySkillBundleLock string
	// querySkillRevisionInsert creates the next DRAFT skill revision from an ACTIVE bundle without an existing draft, pinning current parent and supplied manifest/digest; caller serializes max-plus-one.
	// querySkillRevisionInsert создаёт следующую DRAFT ревизию навыка в ACTIVE комплекте без действующего черновика, закрепляет текущего родителя и заданные манифест и дайджест; порядок max-plus-one сериализует вызывающий код.
	//go:embed sql/skill_revision_insert.sql
	querySkillRevisionInsert string
	// querySkillRevisionSave replaces fields of a nonpublished, nondiscarded skill revision and resets scan/review results to pending draft state; caller authorizes the edit.
	// querySkillRevisionSave заменяет поля неопубликованной и неотброшенной ревизии навыка и сбрасывает результаты сканирования и рецензирования к ожидающему черновику; изменение разрешает вызывающий код.
	//go:embed sql/skill_revision_save.sql
	querySkillRevisionSave string
	// querySkillBundleSetDraft sets a tenant bundle's draft revision pointer and increments version by the supplied amount; SQL has no expected-version guard.
	// querySkillBundleSetDraft устанавливает указатель черновой ревизии комплекта организации и увеличивает версию на заданную величину; SQL не содержит условия ожидаемой версии.
	//go:embed sql/skill_bundle_set_draft.sql
	querySkillBundleSetDraft string
	// querySkillArtifactGet reads digest, size, scan and lifecycle state of one exact artifact revision under a shared head lock; content eligibility is checked by caller.
	// querySkillArtifactGet читает дайджест, размер, результат сканирования и жизненное состояние точной ревизии артефакта под разделяемой блокировкой заголовка; допустимость содержимого проверяет вызывающий код.
	//go:embed sql/skill_artifact_get.sql
	querySkillArtifactGet string
	// queryMemoryRevisionGet reads one memory revision projection by tenant record and revision refs without independently checking actor or source-run visibility.
	// queryMemoryRevisionGet читает проекцию одной ревизии памяти по организации и ссылкам записи и ревизии без самостоятельной проверки видимости для субъекта и исходного запуска.
	//go:embed sql/memory_revision_get.sql
	queryMemoryRevisionGet string
	// queryMemoryRecordList pages actor-visible memory records by ref after project/agent filters, memory visibility and source-run visibility, returning page and total.
	// queryMemoryRecordList выдаёт страницы видимых субъекту записей памяти по ссылке после фильтров проекта и сотрудника и проверок видимости памяти и исходного запуска, возвращая страницу и итог.
	//go:embed sql/memory_record_list.sql
	queryMemoryRecordList string
	// queryMemoryRevisionList pages revisions of one tenant memory record in descending order, hiding revisions whose source run is not visible to the actor.
	// queryMemoryRevisionList выдаёт ревизии одной записи памяти организации страницами по убыванию номера, скрывая ревизии с невидимым субъекту исходным запуском.
	//go:embed sql/memory_revision_list.sql
	queryMemoryRevisionList string
	// queryMemoryRecordGet reads one tenant memory record projection and optional agent/current-revision IDs by ref; actor eligibility remains with the caller.
	// queryMemoryRecordGet читает проекцию одной записи памяти организации и необязательные ID сотрудника и текущей ревизии по ссылке; допустимость для субъекта остаётся у вызывающего кода.
	//go:embed sql/memory_record_get.sql
	queryMemoryRecordGet string
	// queryMemoryRecordLock locks one tenant memory record by ref and reads version/state for a subsequent owner transaction.
	// queryMemoryRecordLock блокирует одну запись памяти организации по ссылке и читает версию и состояние для последующей транзакции владельца данных.
	//go:embed sql/memory_record_lock.sql
	queryMemoryRecordLock string
	// queryMemoryRecordInsert creates a memory record in an ACTIVE tenant project, optionally bound to a nonarchived agent in that project, and pins creator.
	// queryMemoryRecordInsert создаёт запись памяти в ACTIVE проекте организации с необязательной привязкой к неархивному сотруднику того же проекта и закрепляет создателя.
	//go:embed sql/memory_record_insert.sql
	queryMemoryRecordInsert string
	// queryMemoryRevisionInsert creates the next USER_SUMMARY memory revision, pinning prior current revision and an optional same-project source run/version; caller serializes max-plus-one.
	// queryMemoryRevisionInsert создаёт следующую USER_SUMMARY ревизию памяти, закрепляя прежнюю текущую ревизию и необязательный исходный запуск того же проекта с версией; порядок max-plus-one сериализует вызывающий код.
	//go:embed sql/memory_revision_insert.sql
	queryMemoryRevisionInsert string
	// queryMemoryRecordSetRevision sets current revision on a non-PURGED tenant memory record and advances version by the supplied increment; expected-version gating is outside SQL.
	// queryMemoryRecordSetRevision устанавливает текущую ревизию записи памяти организации не в PURGED и увеличивает версию на заданный шаг; проверка ожидаемой версии находится вне SQL.
	//go:embed sql/memory_record_set_revision.sql
	queryMemoryRecordSetRevision string
	// queryMemoryRecordSetState changes state of a non-PURGED tenant memory record and advances version; allowed transitions are decided before this statement.
	// queryMemoryRecordSetState меняет состояние записи памяти организации не в PURGED и увеличивает версию; допустимые переходы определяются до этого запроса.
	//go:embed sql/memory_record_set_state.sql
	queryMemoryRecordSetState string
	// queryMemoryRecordPurge clears summaries of all revisions in a tenant memory record during purge without deleting revision rows.
	// queryMemoryRecordPurge очищает содержимое сводок всех ревизий записи памяти организации при удалении, сохраняя строки ревизий.
	//go:embed sql/memory_record_purge.sql
	queryMemoryRecordPurge string
	// queryMemoryRecordDisableBindings disables all still-enabled bindings to a tenant memory record and advances each binding version; it does not delete agent history.
	// queryMemoryRecordDisableBindings отключает все ещё включённые привязки к записи памяти организации и увеличивает версию каждой; историю сотрудника не удаляет.
	//go:embed sql/memory_record_disable_bindings.sql
	queryMemoryRecordDisableBindings string
)
