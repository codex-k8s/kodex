package platform

import _ "embed"

var (
	// queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation locks one unexpired upload idempotency receipt for the exact organization, actor, operation and key, returning its intent and recorded response.
	// queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation блокирует одну неистёкшую квитанцию идемпотентности загрузки по точным организации, автору, операции и ключу, возвращая замысел и сохранённый ответ.
	//go:embed sql/artifacts_uploadartifact_select_idempotency_receipts_organization_id_actor_id_operation.sql
	queryArtifactsUploadartifactSelectIdempotencyReceiptsOrganizationIdActorIdOperation string
	// queryArtifactsUploadartifactSelectRunsOrganizationIdProjectIdRef resolves an upload run under exact organization, project and ref with its session ref; it does not grant the caller access by itself.
	// queryArtifactsUploadartifactSelectRunsOrganizationIdProjectIdRef разрешает запуск загрузки по точным организации, проекту и ссылке вместе со ссылкой сессии; сам по себе доступ вызывающему не выдаёт.
	//go:embed sql/artifacts_uploadartifact_select_runs_organization_id_project_id_ref.sql
	queryArtifactsUploadartifactSelectRunsOrganizationIdProjectIdRef string
	// queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName inserts an artifact with the next stored filename revision in project or creator scope and returns initial metadata; the surrounding transaction owns concurrency and authorization.
	// queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName создаёт артефакт со следующим сохранённым номером ревизии имени файла в области проекта либо автора и возвращает метаданные; транзакция вызывающего кода владеет конкурентностью и доступом.
	//go:embed sql/artifacts_uploadartifact_insert_artifacts_ref_project_id_file_name.sql
	queryArtifactsUploadartifactInsertArtifactsRefProjectIdFileName string
	// queryArtifactsUploadartifactInsertArtifactContentArtifactId records object locator, digest, size and antivirus report for the artifact selected by ref; caller must supply a previously validated artifact and object receipt.
	// queryArtifactsUploadartifactInsertArtifactContentArtifactId сохраняет адрес объекта, дайджест, размер и отчёт антивируса для артефакта по ссылке; вызывающий код должен передать уже проверенные артефакт и квитанцию объекта.
	//go:embed sql/artifacts_uploadartifact_insert_artifact_content_artifact_id.sql
	queryArtifactsUploadartifactInsertArtifactContentArtifactId string
	// queryArtifactsUploadartifactInsertAuditEventsRefProjectIdAction appends a successful artifact-upload audit row with organization, project, actor and correlation refs; it does not publish an event.
	// queryArtifactsUploadartifactInsertAuditEventsRefProjectIdAction добавляет запись аудита успешной загрузки артефакта с организацией, проектом, автором и корреляцией; событие не публикует.
	//go:embed sql/artifacts_uploadartifact_insert_audit_events_ref_project_id_action.sql
	queryArtifactsUploadartifactInsertAuditEventsRefProjectIdAction string
	// queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest stores the exact upload intent digest and response as a 24-hour idempotency receipt for organization, actor, operation and key.
	// queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest сохраняет точный дайджест замысла загрузки и ответ в квитанции идемпотентности на 24 часа по организации, автору, операции и ключу.
	//go:embed sql/artifacts_uploadartifact_insert_idempotency_receipts_organization_id_operation_intent_digest.sql
	queryArtifactsUploadartifactInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest string
	// queryArtifactsDownloadartifactSelectArtifactForGrant locks the head of an ACTIVE artifact revision under organization, optional project and both view/download catalog permissions; this selection does not require CLEAN scan state.
	// queryArtifactsDownloadartifactSelectArtifactForGrant блокирует голову ACTIVE-ревизии артефакта в области организации, необязательного проекта и обоих разрешений каталога view/download; эта выборка не требует CLEAN состояния сканирования.
	//go:embed sql/artifacts_downloadartifact_select_artifact_for_grant.sql
	queryArtifactsDownloadartifactSelectArtifactForGrant string
	// queryArtifactsDownloadartifactInsertDownloadGrant creates a one-minute download grant pinned to exact artifact revision, version, subject, purpose and project scope.
	// queryArtifactsDownloadartifactInsertDownloadGrant создаёт минутный допуск на скачивание, закреплённый за точными ревизией и версией артефакта, субъектом, целью и областью проекта.
	//go:embed sql/artifacts_downloadartifact_insert_download_grant.sql
	queryArtifactsDownloadartifactInsertDownloadGrant string
	// queryArtifactsDownloadartifactConsumeDownloadGrant consumes an unexpired exact download grant once only if its revision remains CLEAN and ACTIVE and the subject still has view and download permission.
	// queryArtifactsDownloadartifactConsumeDownloadGrant одноразово погашает неистёкший точный допуск на скачивание лишь пока ревизия CLEAN и ACTIVE и у субъекта сохраняются права view и download.
	//go:embed sql/artifacts_downloadartifact_consume_download_grant.sql
	queryArtifactsDownloadartifactConsumeDownloadGrant string
	// queryArtifactsDownloadartifactSelectArtifactContent reads object locator and digest for an exact CLEAN ACTIVE artifact revision; caller must already have consumed the authorized download grant.
	// queryArtifactsDownloadartifactSelectArtifactContent читает адрес объекта и дайджест точной CLEAN ACTIVE ревизии артефакта; вызывающий код должен прежде погасить разрешённый допуск на скачивание.
	//go:embed sql/artifacts_downloadartifact_select_artifact_content.sql
	queryArtifactsDownloadartifactSelectArtifactContent string
	// queryArtifactsDownloadartifactInsertAuditEvent appends a successful artifact download audit row for the supplied subject, action and correlation without deciding grant eligibility.
	// queryArtifactsDownloadartifactInsertAuditEvent добавляет запись аудита успешного скачивания артефакта с заданными субъектом, действием и корреляцией, не определяя допустимость допуска.
	//go:embed sql/artifacts_downloadartifact_insert_audit_event.sql
	queryArtifactsDownloadartifactInsertAuditEvent string
	// queryArtifactsChangeartifactbindingSelectArtifactsOrganizationIdRefScanState locks the ACTIVE artifact head by organization and ref, optionally requiring CLEAN scan state, and returns project and version for binding changes.
	// queryArtifactsChangeartifactbindingSelectArtifactsOrganizationIdRefScanState блокирует голову ACTIVE артефакта по организации и ссылке, при необходимости требуя CLEAN сканирование, и возвращает проект и версию для изменения привязки.
	//go:embed sql/artifacts_changeartifactbinding_select_artifacts_organization_id_ref_scan_state.sql
	queryArtifactsChangeartifactbindingSelectArtifactsOrganizationIdRefScanState string
	// queryArtifactsChangeartifactbindingSelectAgentsOrganizationIdProjectIdRef locks the exact project agent, returning its capability flag; an archived agent is eligible only for removal of its existing knowledge binding.
	// queryArtifactsChangeartifactbindingSelectAgentsOrganizationIdProjectIdRef блокирует точного агента проекта и возвращает признак возможности; архивный агент допустим только для снятия уже существующей привязки знаний.
	//go:embed sql/artifacts_changeartifactbinding_select_agents_organization_id_project_id_ref.sql
	queryArtifactsChangeartifactbindingSelectAgentsOrganizationIdProjectIdRef string
	// queryArtifactsChangeartifactbindingInsertArtifactBindingsArtifactIdTargetRef adds a KNOWLEDGE artifact binding for an agent in the exact organization/project if present; a duplicate binding is left unchanged.
	// queryArtifactsChangeartifactbindingInsertArtifactBindingsArtifactIdTargetRef добавляет привязку артефакта KNOWLEDGE к агенту точных организации и проекта, если он существует; повторную привязку не меняет.
	//go:embed sql/artifacts_changeartifactbinding_insert_artifact_bindings_artifact_id_target_ref.sql
	queryArtifactsChangeartifactbindingInsertArtifactBindingsArtifactIdTargetRef string
	// queryArtifactsChangeartifactbindingDeleteArtifactBindingsArtifactIdTargetKindTargetRef deletes one KNOWLEDGE binding by artifact ID and target ref without changing unrelated binding kinds.
	// queryArtifactsChangeartifactbindingDeleteArtifactBindingsArtifactIdTargetKindTargetRef удаляет одну привязку KNOWLEDGE по ID артефакта и ссылке цели, не затрагивая другие виды привязок.
	//go:embed sql/artifacts_changeartifactbinding_delete_artifact_bindings_artifact_id_target_kind_target_ref.sql
	queryArtifactsChangeartifactbindingDeleteArtifactBindingsArtifactIdTargetKindTargetRef string
	// queryArtifactsChangeartifactbindingUpdateArtifactsVersion advances the artifact-head version after a binding change; exact version checks belong to the enclosing command.
	// queryArtifactsChangeartifactbindingUpdateArtifactsVersion увеличивает версию головы артефакта после изменения привязки; проверка ожидаемой версии принадлежит внешней команде.
	//go:embed sql/artifacts_changeartifactbinding_update_artifacts_version.sql
	queryArtifactsChangeartifactbindingUpdateArtifactsVersion string
	// queryArtifactsChangeartifactbindingUpdateAgentsVersion advances the locked agent version and update time after its knowledge binding changes.
	// queryArtifactsChangeartifactbindingUpdateAgentsVersion увеличивает версию заблокированного агента и время обновления после изменения его привязки знаний.
	//go:embed sql/artifacts_changeartifactbinding_update_agents_version.sql
	queryArtifactsChangeartifactbindingUpdateAgentsVersion string
	// queryArtifactsLifecycleSelectForUpdate locks one artifact head by organization and ref and returns lifecycle state, project and version for owner transitions.
	// queryArtifactsLifecycleSelectForUpdate блокирует одну голову артефакта по организации и ссылке и возвращает состояние жизненного цикла, проект и версию для переходов владельца.
	//go:embed sql/artifacts_lifecycle_select_for_update.sql
	queryArtifactsLifecycleSelectForUpdate string
	// queryArtifactsLifecycleSelectHasQueuedDependencies reports queued agent-execution dependencies through run or turn attachment sets for one artifact; it does not cover every possible active run usage.
	// queryArtifactsLifecycleSelectHasQueuedDependencies показывает поставленные в очередь зависимости выполнения агента через наборы вложений запуска или хода для одного артефакта; все виды активного использования запуска не охватывает.
	//go:embed sql/artifacts_lifecycle_select_has_queued_dependencies.sql
	queryArtifactsLifecycleSelectHasQueuedDependencies string
	// queryArtifactsLifecycleSoftDelete marks an ACTIVE artifact DELETED at expected version, starts the 30-day purge window and advances version.
	// queryArtifactsLifecycleSoftDelete помечает ACTIVE артефакт DELETED при ожидаемой версии, начинает 30-дневное окно очистки и увеличивает версию.
	//go:embed sql/artifacts_lifecycle_soft_delete.sql
	queryArtifactsLifecycleSoftDelete string
	// queryArtifactsLifecycleRestore restores a DELETED artifact at expected version only before purge expiry, clearing deletion timestamps and advancing version.
	// queryArtifactsLifecycleRestore восстанавливает DELETED артефакт при ожидаемой версии только до истечения срока очистки, сбрасывая отметки удаления и увеличивая версию.
	//go:embed sql/artifacts_lifecycle_restore.sql
	queryArtifactsLifecycleRestore string
	// queryArtifactsImpact projects artifact impact with counts of bindings, attachments, active run uses and skill references plus at most 21 recent runs; it is a read model, not a deletion decision.
	// queryArtifactsImpact проецирует влияние артефакта с числами привязок, вложений, активных запусков и ссылок навыков и максимум 21 недавним запуском; это модель чтения, не решение об удалении.
	//go:embed sql/artifacts_impact.sql
	queryArtifactsImpact string
	// queryArtifactsPurgeSelectArtifactContentForUpdate locks an artifact head and lists object locators of all stored revision content for an exact organization and ref before purge.
	// queryArtifactsPurgeSelectArtifactContentForUpdate блокирует голову артефакта и перечисляет адреса объектов содержимого всех сохранённых ревизий по точным организации и ссылке перед очисткой.
	//go:embed sql/artifacts_purge_select_artifact_content_for_update.sql
	queryArtifactsPurgeSelectArtifactContentForUpdate string
	// queryArtifactsPurgeMarkPending moves a DELETED artifact at expected version to PURGE_PENDING and advances version without deleting content yet.
	// queryArtifactsPurgeMarkPending переводит DELETED артефакт при ожидаемой версии в PURGE_PENDING и увеличивает версию, ещё не удаляя содержимое.
	//go:embed sql/artifacts_purge_mark_pending.sql
	queryArtifactsPurgeMarkPending string
	// queryArtifactsPurgeFinalize marks a PURGE_PENDING artifact PURGED only after retained revisions and revision content are absent; this statement has no expected-version predicate.
	// queryArtifactsPurgeFinalize помечает PURGE_PENDING артефакт PURGED лишь при отсутствии удерживаемых ревизий и содержимого; в этом запросе нет предиката ожидаемой версии.
	//go:embed sql/artifacts_purge_finalize.sql
	queryArtifactsPurgeFinalize string
	// queryArtifactsPurgeDeleteBindings deletes all bindings of the exact artifact ID during owner-controlled purge.
	// queryArtifactsPurgeDeleteBindings удаляет все привязки точного ID артефакта во время контролируемой владельцем очистки.
	//go:embed sql/artifacts_purge_delete_bindings.sql
	queryArtifactsPurgeDeleteBindings string
	// queryArtifactsPurgeDeleteDownloadGrants deletes all download grants for the exact artifact ID during owner-controlled purge.
	// queryArtifactsPurgeDeleteDownloadGrants удаляет все допуски на скачивание точного ID артефакта во время контролируемой владельцем очистки.
	//go:embed sql/artifacts_purge_delete_download_grants.sql
	queryArtifactsPurgeDeleteDownloadGrants string
	// queryArtifactsPurgeDeleteContent deletes stored content rows of every revision of the exact artifact ID during owner-controlled purge.
	// queryArtifactsPurgeDeleteContent удаляет записи сохранённого содержимого всех ревизий точного ID артефакта во время контролируемой владельцем очистки.
	//go:embed sql/artifacts_purge_delete_content.sql
	queryArtifactsPurgeDeleteContent string
	// queryArtifactsPurgeDeleteRevisions deletes revision metadata for the exact artifact ID after dependent content has been removed.
	// queryArtifactsPurgeDeleteRevisions удаляет метаданные ревизий точного ID артефакта после удаления зависимого содержимого.
	//go:embed sql/artifacts_purge_delete_revisions.sql
	queryArtifactsPurgeDeleteRevisions string
	// queryArtifactsPurgeUpdateIdempotencyReceipt replaces the stored artifact idempotency response only for the exact organization, actor, operation, key and original intent digest.
	// queryArtifactsPurgeUpdateIdempotencyReceipt заменяет сохранённый ответ идемпотентной операции над артефактом лишь при совпадении организации, автора, операции, ключа и исходного дайджеста замысла.
	//go:embed sql/artifacts_purge_update_idempotency_receipt.sql
	queryArtifactsPurgeUpdateIdempotencyReceipt string
)
