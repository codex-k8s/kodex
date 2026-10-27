package platform

import _ "embed"

var (
	// querySecretDraftPublishingActive checks whether a resolved internal secret has any PUBLISHING
	// draft; this boolean has no actor/expiry predicate and callers own tenant/secret eligibility and
	// lifecycle locking.
	// querySecretDraftPublishingActive проверяет наличие черновика PUBLISHING у разрешённого
	// внутреннего секрета. Условия по субъекту и сроку отсутствуют; границу организации, доступность
	// секрета и блокировки жизненного цикла проверяет вызывающий.
	//go:embed sql/secret_draft_publishing_active.sql
	querySecretDraftPublishingActive string
	// querySecretDraftCreatorMatches compares the persisted creator of a resolved internal secret ID
	// with the supplied actor ID. It is a creator predicate rather than an independent
	// organization/resource authorization query.
	// querySecretDraftCreatorMatches сравнивает сохранённого создателя секрета с переданным ID
	// субъекта по внутреннему ID секрета. Это проверка создателя, а не самостоятельная авторизация по
	// организации и ресурсу.
	//go:embed sql/secret_draft_creator_matches.sql
	querySecretDraftCreatorMatches string
	// querySecretDraftExpireOperations marks every PREPARED/CLAIMED operation of the resolved draft
	// FAILED with GRANT_EXPIRED, shortening grant/lease deadlines to at most database time. The caller
	// decides why invalidation is required; the query does not select only already expired grants.
	// querySecretDraftExpireOperations переводит все операции PREPARED/CLAIMED черновика в FAILED с
	// GRANT_EXPIRED, сокращая сроки разрешения и аренды до текущего времени БД. Причину отзыва
	// определяет вызывающий; запрос охватывает и ещё не истёкшие разрешения.
	//go:embed sql/secret_draft_expire_operations.sql
	querySecretDraftExpireOperations string
	// querySecretDraftReadiness checks current database-role table/sequence privileges and existence
	// of the active-operation index. It proves this storage prerequisite only, not encryption, secret
	// publisher connectivity or successful materialization.
	// querySecretDraftReadiness проверяет права текущей роли БД на таблицы и последовательности,
	// наличие индекса активной операции. Подтверждает лишь предпосылки хранилища; шифрование,
	// доступность издателя секретов и материализацию не проверяет.
	//go:embed sql/secret_draft_readiness.sql
	querySecretDraftReadiness string
	// querySecretDraftGet reads exact organization/ref draft and joined secret/owner/scope metadata
	// including the encrypted descriptor and staged namespace. It does not filter state/expiry or
	// grant plaintext access; callers validate lifecycle, owner and disclosure boundaries.
	// querySecretDraftGet читает черновик по точным организации и ссылке, связанные сведения секрета,
	// владельца и области, зашифрованный дескриптор и подготовленный namespace. Состояние и срок не
	// фильтрует; жизненный цикл и границу раскрытия проверяет вызывающий.
	//go:embed sql/secret_draft_get.sql
	querySecretDraftGet string
	// querySecretDraftLock reads the same organization/ref draft projection as Get while locking both
	// secret and draft rows. Locking serializes owner decisions but does not itself validate actor
	// authority, expiry or allowed transitions.
	// querySecretDraftLock читает проекцию Get по организации и ссылке, блокируя строки секрета и
	// черновика. Блокировки упорядочивают решения владельца; полномочия субъекта, срок и допустимость
	// перехода проверяются отдельно.
	//go:embed sql/secret_draft_lock.sql
	querySecretDraftLock string
	// querySecretDraftInsert inserts a PREPARING draft with server-resolved organization/secret/owner,
	// expected content hash and staged namespace. Database defaults own other fields; authorization
	// and surrounding atomic effects remain with the caller.
	// querySecretDraftInsert вставляет черновик PREPARING с разрешёнными сервером организацией,
	// секретом и владельцем, ожидаемым хешем содержимого и подготовленным namespace. Остальные поля
	// получают значения БД; авторизация и атомарные эффекты принадлежат вызывающему.
	//go:embed sql/secret_draft_insert.sql
	querySecretDraftInsert string
	// querySecretDraftUpdate updates a resolved draft only at the supplied version, increments
	// version/time and retains the old encrypted descriptor when the new JSON value is SQL NULL.
	// State/published revision are caller inputs; legal transition and row-count handling are
	// external.
	// querySecretDraftUpdate обновляет черновик при точном совпадении версии, увеличивает версию и
	// время, сохраняет прежний зашифрованный дескриптор при SQL NULL нового JSON. Состояние и
	// опубликованную ревизию задаёт вызывающий; он же проверяет переход и число изменённых строк.
	//go:embed sql/secret_draft_update.sql
	querySecretDraftUpdate string
	// querySecretDraftOperationLookup locates an organization operation by explicit ref, token digest
	// or actor/kind/idempotency tuple and returns operation/draft refs. Only the idempotency branch
	// includes actor/kind predicates; finding a row through another locator does not grant authority
	// to consume it.
	// querySecretDraftOperationLookup находит операцию организации по ссылке, хешу токена либо
	// сочетанию субъекта, вида и ключа идемпотентности. Только последняя ветвь ограничена субъектом и
	// видом; найденные ссылки операции и черновика сами не разрешают потребление.
	//go:embed sql/secret_draft_operation_lookup.sql
	querySecretDraftOperationLookup string
	// querySecretDraftOperationLock locks the exact organization/operation row and returns expected
	// owner versions/revisions, intent, claimant/generation/deadlines, terminal snapshot and cleanup
	// descriptors. Callers still verify actor, kind, fence/lifecycle and disclosure rules; these are
	// internal metadata, not a public secret response.
	// querySecretDraftOperationLock блокирует точную операцию организации, читает ожидаемые версии и
	// ревизии владельца, намерение, заявителя, поколение, сроки, итоговый снимок и дескрипторы
	// очистки. Субъекта, вид, поколение захвата, жизненный цикл и допустимость раскрытия проверяет
	// вызывающий.
	//go:embed sql/secret_draft_operation_lock.sql
	querySecretDraftOperationLock string
	// querySecretDraftOperationInsert stores a prepared operation identity with
	// owner/version/revision/intent pins, hashed token, idempotency key, grant expiry and correlation
	// metadata. It stores no raw grant token and relies on caller authorization/locking plus database
	// constraints for a valid operation.
	// querySecretDraftOperationInsert сохраняет подготовленную операцию с привязками владельца,
	// версий, ревизий и намерения, хешем токена, ключом идемпотентности, сроком разрешения и
	// корреляцией. Исходный токен не хранится; авторизация, блокировки и ограничения БД обеспечиваются
	// отдельно.
	//go:embed sql/secret_draft_operation_insert.sql
	querySecretDraftOperationInsert string
	// querySecretDraftOperationReissue replaces only token digest and grant expiry of a resolved
	// PREPARED operation, returning the new deadline. It does not advance expected owner pins or
	// itself authorize a new grant lifetime.
	// querySecretDraftOperationReissue заменяет хеш токена и срок разрешения только у разрешённой
	// операции PREPARED, возвращая новый срок. Ожидаемые версии владельца не меняются; право продления
	// проверяет вызывающий.
	//go:embed sql/secret_draft_operation_reissue.sql
	querySecretDraftOperationReissue string
	// querySecretDraftOperationConsume atomically moves a nonexpired PREPARED operation to CLAIMED,
	// records claimant, increments claim generation and caps the 30-second lease by grant expiry.
	// Token/actor/owner checks belong to the caller; returning a generation is not a fresh resource
	// grant by itself.
	// querySecretDraftOperationConsume атомарно переводит неистёкшую PREPARED операцию в CLAIMED,
	// сохраняет заявителя, увеличивает поколение захвата и ограничивает 30-секундную аренду сроком
	// разрешения. Проверки токена, субъекта и владельца остаются у вызывающего.
	//go:embed sql/secret_draft_operation_consume.sql
	querySecretDraftOperationConsume string
	// querySecretDraftOperationFinish writes state, safe failure code and terminal JSON snapshot for a
	// resolved operation ID. This SQL has no independent version/claim/state guard; the surrounding
	// locked owner path must establish the legal terminal transition.
	// querySecretDraftOperationFinish пишет состояние, безопасный код ошибки и итоговый JSON-снимок по
	// разрешённому ID операции. Собственного условия по версии, захвату и прежнему состоянию нет;
	// допустимый переход устанавливает вызывающий под блокировкой владельца.
	//go:embed sql/secret_draft_operation_finish.sql
	querySecretDraftOperationFinish string
	// querySecretDraftOperationActive checks existence of any PREPARED/CLAIMED operation for a
	// resolved draft ID without evaluating deadline freshness or actor authority; callers decide how
	// this blocks another operation.
	// querySecretDraftOperationActive проверяет наличие операции PREPARED/CLAIMED по разрешённому ID
	// черновика без проверки срока и полномочий субъекта. Влияние этой операции на следующую
	// определяет вызывающий.
	//go:embed sql/secret_draft_operation_active.sql
	querySecretDraftOperationActive string
	// querySecretDraftTargetRevision chooses one greater than the maximum
	// published/draft-operation/other-operation revision for a resolved secret, preserving reserved
	// historical numbers. It computes a candidate rather than reserving it; the caller owns locking
	// and atomic insertion.
	// querySecretDraftTargetRevision вычисляет номер больше максимума опубликованных ревизий, операций
	// черновиков и остальных операций секрета, учитывая исторически занятые номера. Номер не
	// резервируется; блокировка и атомарная вставка принадлежат вызывающему.
	//go:embed sql/secret_draft_target_revision.sql
	querySecretDraftTargetRevision string
	// querySecretDraftAudit inserts a SECRET audit event with resolved actor/org/optional project,
	// outcome, secret reference and a fixed safe summary. It records metadata rather than secret
	// values; caller transaction ownership binds it to the associated state change.
	// querySecretDraftAudit записывает аудит SECRET с разрешёнными субъектом, организацией,
	// необязательным проектом, исходом, ссылкой секрета и фиксированным безопасным описанием. Значения
	// секретов не записываются; транзакция вызывающего связывает аудит с изменением.
	//go:embed sql/secret_draft_audit.sql
	querySecretDraftAudit string
	// querySecretDraftRecoveryList pages organization operations by ref requiring both
	// recovery/cleanup interest and an expired/failed/terminal condition, including completed
	// published active-revision readback. The result identifies reconciliation candidates, not
	// permission to repeat a secret effect or skip current owner checks.
	// querySecretDraftRecoveryList выдаёт страницу операций организации по ссылке при одновременной
	// необходимости восстановления или очистки и условии истечения, ошибки либо завершения. Включает
	// сверку опубликованной активной ревизии; кандидаты не разрешают повтор эффекта или обход свежих
	// проверок владельца.
	//go:embed sql/secret_draft_recovery_list.sql
	querySecretDraftRecoveryList string
	// querySecretDraftCleanupIntent stores pending encrypted/materialization cleanup descriptors
	// without overwriting unfinished ones, reopening cleanup when new work appears after completion.
	// It mutates intent metadata only; external deletion and exact ownership verification remain with
	// the cleanup consumer.
	// querySecretDraftCleanupIntent сохраняет дескрипторы очистки шифрованных и материализованных
	// данных без перезаписи незавершённых, повторно открывая очистку при новых данных после
	// завершения. Меняет только намерение; удаление и проверка принадлежности выполняются
	// потребителем.
	//go:embed sql/secret_draft_cleanup_intent.sql
	querySecretDraftCleanupIntent string
	// querySecretDraftCleanupComplete marks cleanup complete for a resolved operation ID and advances
	// update time after the caller has verified the required external cleanup. The flag update itself
	// does not delete staged or materialized secret data.
	// querySecretDraftCleanupComplete помечает очистку операции завершённой и обновляет время после
	// внешней проверки вызывающим. Сам флаг не удаляет подготовленные или материализованные данные
	// секрета.
	//go:embed sql/secret_draft_cleanup_complete.sql
	querySecretDraftCleanupComplete string
	// querySecretDraftOwnerScope resolves public actor/organization refs only for an active subject in
	// the exact organization/internal actor tuple. It does not evaluate a secret-specific capability
	// or return credential values.
	// querySecretDraftOwnerScope разрешает публичные ссылки субъекта и организации только для
	// активного субъекта точной пары внутренних ID. Не проверяет право на конкретный секрет и не
	// возвращает учётные данные.
	//go:embed sql/secret_draft_owner_scope.sql
	querySecretDraftOwnerScope string
	// querySecretDraftClock reads PostgreSQL clock_timestamp for owner deadline decisions; unlike
	// transaction time it observes the current database clock on each call.
	// querySecretDraftClock читает clock_timestamp PostgreSQL для решений владельца по срокам. В
	// отличие от времени начала транзакции получает текущее время БД при каждом обращении.
	//go:embed sql/secret_draft_clock.sql
	querySecretDraftClock string
)
