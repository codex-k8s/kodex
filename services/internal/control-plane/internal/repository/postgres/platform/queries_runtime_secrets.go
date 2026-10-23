package platform

import _ "embed"

var (
	// queryOrganizationRuntimeSecretsList pages non-PROVISIONING organization-scoped secret metadata by ref without returning values; caller must establish organization authority before this query.
	// queryOrganizationRuntimeSecretsList выдаёт страницы метаданных секретов уровня организации вне PROVISIONING по ссылке без значений; вызывающий код должен подтвердить полномочия на организацию до запроса.
	//go:embed sql/organization_runtime_secrets_list.sql
	queryOrganizationRuntimeSecretsList string
	// queryRuntimeSecretsList pages project-scoped non-PROVISIONING secret metadata under exact organization, optional project and catalog secret.view visibility.
	// queryRuntimeSecretsList выдаёт страницы метаданных проектных секретов вне PROVISIONING по организации, необязательному проекту и видимости каталога secret.view.
	//go:embed sql/runtime_secrets_list.sql
	queryRuntimeSecretsList string
	// queryRuntimeSecretGet reads metadata and current revision of one non-PROVISIONING secret by organization/ref; SQL does not check caller visibility or reveal the secret value.
	// queryRuntimeSecretGet читает метаданные и текущую ревизию одного секрета вне PROVISIONING по организации и ссылке; SQL не проверяет видимость для вызывающего и не раскрывает значение.
	//go:embed sql/runtime_secret_get.sql
	queryRuntimeSecretGet string
	// queryRuntimeSecretLockProject locks one ACTIVE project by organization and ref for a secret mutation without granting permission to mutate the project.
	// queryRuntimeSecretLockProject блокирует один ACTIVE проект по организации и ссылке для изменения секрета, не выдавая права менять проект.
	//go:embed sql/runtime_secret_lock_project.sql
	queryRuntimeSecretLockProject string
	// queryRuntimeSecretLock locks one secret by organization/ref and returns its project, lifecycle, version and scope for owner-side mutation checks.
	// queryRuntimeSecretLock блокирует один секрет по организации и ссылке и возвращает проект, состояние, версию и область для проверок изменения владельцем.
	//go:embed sql/runtime_secret_lock.sql
	queryRuntimeSecretLock string
	// queryRuntimeSecretInsert creates a PROVISIONING secret with organization/project scope and actor metadata, returning initial version; no value or Kubernetes material is stored here.
	// queryRuntimeSecretInsert создаёт PROVISIONING секрет с областью организации или проекта и автором, возвращая начальную версию; значение и материал Kubernetes здесь не сохраняются.
	//go:embed sql/runtime_secret_insert.sql
	queryRuntimeSecretInsert string
	// queryRuntimeSecretLockByName locks a secret by exact organization, nullable project and name for uniqueness and lifecycle inspection.
	// queryRuntimeSecretLockByName блокирует секрет по точным организации, необязательному проекту и имени для проверки уникальности и жизненного цикла.
	//go:embed sql/runtime_secret_lock_by_name.sql
	queryRuntimeSecretLockByName string
	// queryRuntimeSecretHasActiveOperation locks a PREPARED or CLAIMED CREATE/ROTATE/REVOKE operation for one secret; its presence is a concurrency signal, not proof of completion.
	// queryRuntimeSecretHasActiveOperation блокирует операцию CREATE/ROTATE/REVOKE в PREPARED или CLAIMED для одного секрета; её наличие сигнализирует о конкурентной работе, а не о завершении.
	//go:embed sql/runtime_secret_has_active_operation.sql
	queryRuntimeSecretHasActiveOperation string
	// queryRuntimeSecretMaximumRevision computes the highest stored current secret revision, create/rotate target and draft target so the caller can allocate a later revision.
	// queryRuntimeSecretMaximumRevision вычисляет наибольшую сохранённую текущую ревизию секрета и цели операций создания, ротации и черновиков для назначения следующей ревизии вызывающим кодом.
	//go:embed sql/runtime_secret_maximum_revision.sql
	queryRuntimeSecretMaximumRevision string
	// queryRuntimeSecretOperationInsert inserts a PREPARED secret operation with exact owner, intent, token digest, expected versions, target revision and grant expiry.
	// queryRuntimeSecretOperationInsert создаёт PREPARED операцию над секретом с точными владельцем, замыслом, дайджестом токена, ожидаемыми версиями, целевой ревизией и сроком допуска.
	//go:embed sql/runtime_secret_operation_insert.sql
	queryRuntimeSecretOperationInsert string
	// queryRuntimeSecretOperationLockByDigest locks an operation and secret by organization and token digest; caller must still verify actor, kind, scope and grant state.
	// queryRuntimeSecretOperationLockByDigest блокирует операцию и секрет по организации и дайджесту токена; вызывающий код ещё обязан проверить автора, вид, область и состояние допуска.
	//go:embed sql/runtime_secret_operation_lock_by_digest.sql
	queryRuntimeSecretOperationLockByDigest string
	// queryRuntimeSecretOperationLockIdempotency locks an operation and secret by exact organization, actor, kind and idempotency key for safe retry inspection.
	// queryRuntimeSecretOperationLockIdempotency блокирует операцию и секрет по точным организации, автору, виду и ключу идемпотентности для проверки безопасного повтора.
	//go:embed sql/runtime_secret_operation_lock_idempotency.sql
	queryRuntimeSecretOperationLockIdempotency string
	// queryRuntimeSecretOperationReissue reissues a token digest and expiry only for an operation still PREPARED, clearing stale claimant and lease fields.
	// queryRuntimeSecretOperationReissue перевыпускает дайджест токена и срок только у операции в PREPARED, очищая прежнего исполнителя и поля аренды.
	//go:embed sql/runtime_secret_operation_reissue.sql
	queryRuntimeSecretOperationReissue string
	// queryRuntimeSecretOperationConsume claims an unexpired PREPARED grant, increments claim generation and sets claimant and lease deadline; token and actor validation belong to the caller.
	// queryRuntimeSecretOperationConsume захватывает неистёкший PREPARED допуск, увеличивает поколение и записывает исполнителя со сроком аренды; токен и автора проверяет вызывающий код.
	//go:embed sql/runtime_secret_operation_consume.sql
	queryRuntimeSecretOperationConsume string
	// queryRuntimeSecretOperationLock locks an operation and secret by organization and operation ref for an exact lifecycle transition.
	// queryRuntimeSecretOperationLock блокирует операцию и секрет по организации и ссылке операции для точного перехода жизненного цикла.
	//go:embed sql/runtime_secret_operation_lock.sql
	queryRuntimeSecretOperationLock string
	// queryRuntimeSecretRecoveryWorkList pages CLAIMED operations with expired leases by deadline/ref cursor for bounded recovery; it does not itself reclaim them.
	// queryRuntimeSecretRecoveryWorkList выдаёт страницы CLAIMED операций с истёкшей арендой по курсору времени и ссылки для ограниченного восстановления; сам по себе захват не повторяет.
	//go:embed sql/runtime_secret_recovery_work_list.sql
	queryRuntimeSecretRecoveryWorkList string
	// queryRuntimeSecretRevisionInsert records a secret revision with exact Kubernetes name, UID, resource version and content digest without storing plaintext.
	// queryRuntimeSecretRevisionInsert сохраняет ревизию секрета с точными именем Kubernetes, UID, версией ресурса и дайджестом содержимого без открытого значения.
	//go:embed sql/runtime_secret_revision_insert.sql
	queryRuntimeSecretRevisionInsert string
	// queryRuntimeSecretActivate activates a secret at expected aggregate version, current revision and state, advancing version and public display hints; external material must already be verified.
	// queryRuntimeSecretActivate активирует секрет при ожидаемых версии агрегата, текущей ревизии и состоянии, увеличивая версию и публичные подсказки; внешний материал должен быть ранее проверен.
	//go:embed sql/runtime_secret_activate.sql
	queryRuntimeSecretActivate string
	// queryRuntimeSecretRevoke revokes an ACTIVE secret at expected version/revision and clears display hints; it does not delete Kubernetes material by itself.
	// queryRuntimeSecretRevoke отзывает ACTIVE секрет при ожидаемых версии и ревизии и очищает подсказки; материал Kubernetes самостоятельно не удаляет.
	//go:embed sql/runtime_secret_revoke.sql
	queryRuntimeSecretRevoke string
	// queryRuntimeSecretOperationComplete marks only the matching CLAIMED operation COMPLETED for exact claimant and generation and stores its terminal secret snapshot.
	// queryRuntimeSecretOperationComplete помечает только совпадающую CLAIMED операцию COMPLETED по точным исполнителю и поколению и сохраняет итоговый снимок секрета.
	//go:embed sql/runtime_secret_operation_complete.sql
	queryRuntimeSecretOperationComplete string
	// queryRuntimeSecretOperationFail marks a matching CLAIMED operation FAILED by claimant/generation for other failure codes, or an unclaimed expired PREPARED grant FAILED only with GRANT_EXPIRED.
	// queryRuntimeSecretOperationFail помечает совпадающую CLAIMED операцию FAILED по исполнителю и поколению для других ошибок либо незахваченный истёкший PREPARED допуск FAILED только с GRANT_EXPIRED.
	//go:embed sql/runtime_secret_operation_fail.sql
	queryRuntimeSecretOperationFail string
	// queryRuntimeSecretAudit inserts an audit event and links it to the exact secret operation in one SQL statement; the supplied outcome and safe summary remain caller-owned.
	// queryRuntimeSecretAudit создаёт запись аудита и связывает её с точной операцией над секретом одним SQL-запросом; исход и безопасную сводку задаёт вызывающий код.
	//go:embed sql/runtime_secret_audit.sql
	queryRuntimeSecretAudit string
	// queryRuntimeSecretResolveBinding resolves a shared ACTIVE secret revision in exact organization, nullable project and scope with Kubernetes identity/digest pins; it does not check the caller's permission.
	// queryRuntimeSecretResolveBinding разрешает ACTIVE ревизию секрета по точным организации, необязательному проекту и области с реквизитами Kubernetes и дайджестом; разрешение вызывающего не проверяет.
	//go:embed sql/runtime_secret_resolve_binding.sql
	queryRuntimeSecretResolveBinding string
	// queryRuntimeSecretIsReferenced checks secret references in current environment versions, agent bindings and active-run runtime revisions; this is a bounded reference query, not every possible dependency.
	// queryRuntimeSecretIsReferenced проверяет ссылки секрета в текущих версиях окружений, привязках агентов и ревизиях активных запусков; это ограниченная выборка, а не все возможные зависимости.
	//go:embed sql/runtime_secret_is_referenced.sql
	queryRuntimeSecretIsReferenced string
	// queryRuntimeSecretRevisionList lists revision metadata and Kubernetes identity pins for one secret in ascending revision order without returning values.
	// queryRuntimeSecretRevisionList возвращает метаданные ревизий и реквизиты Kubernetes одного секрета по возрастанию номера без значений.
	//go:embed sql/runtime_secret_revision_list.sql
	queryRuntimeSecretRevisionList string
	// queryRuntimeSecretRevisionGet reads one numbered secret revision metadata row and its Kubernetes identity pins without returning the value.
	// queryRuntimeSecretRevisionGet читает метаданные одной нумерованной ревизии секрета и её реквизиты Kubernetes без значения.
	//go:embed sql/runtime_secret_revision_get.sql
	queryRuntimeSecretRevisionGet string
)
