package platform

import _ "embed"

var (
	// queryProviderAccountsSetConcurrency Updates concurrency only for the exact
	// organization/account/expected version outside DELETING/DELETED, increasing version/time. Callers
	// own authorization and affected-row handling.
	// queryProviderAccountsSetConcurrency Меняет concurrency только по точным организации, аккаунту и
	// ожидаемой версии вне DELETING/DELETED, увеличивает версию и время. Авторизацию и число строк
	// проверяют вызывающие.
	//go:embed sql/provider_accounts__set_concurrency.sql
	queryProviderAccountsSetConcurrency string
	// queryProviderAccountsCreate Inserts a disabled PENDING_AUTHORIZATION account only for an enabled
	// definition, assigning a deterministic internal stable key from the generated public ref. It does
	// not materialize a credential or authorize a provider request.
	// queryProviderAccountsCreate Вставляет выключенный PENDING_AUTHORIZATION аккаунт только для
	// включённого определения, назначая внутренний stable key из новой публичной ссылки. Credential не
	// материализует и запрос провайдеру не разрешает.
	//go:embed sql/provider_accounts_create.sql
	queryProviderAccountsCreate string
	// queryProviderAccountsLock Locks exact organization/account and reads lifecycle/version/current
	// credential ID before owner mutation. The lock serializes changes but does not replace actor
	// authorization or expected-version checks.
	// queryProviderAccountsLock Блокирует точный аккаунт организации и читает жизненный цикл, версию и
	// текущую credential перед изменением владельца. Блокировка упорядочивает записи, не заменяя
	// авторизацию или ожидаемую версию.
	//go:embed sql/provider_accounts_lock.sql
	queryProviderAccountsLock string
	// queryProviderAccountsInsertAuthorization Inserts an authorization attempt or fills only a
	// matching unexpired RESERVED owner/account/actor/method reservation at its pinned pending account
	// version. Stores safe materializer/verification metadata; it does not itself perform
	// authentication or a remote effect.
	// queryProviderAccountsInsertAuthorization Вставляет попытку авторизации либо заполняет только
	// совпавшую неистёкшую RESERVED запись владельца, аккаунта, субъекта и метода с закреплённой
	// pending-версией аккаунта. Хранит метаданные materializer и подтверждения; аутентификацию или
	// внешний эффект не выполняет.
	//go:embed sql/provider_accounts_insert_authorization.sql
	queryProviderAccountsInsertAuthorization string
	// queryProviderAccountsCompleteAuthorization Transitions only a matching pending
	// organization/account/authorization/materializer tuple, clears verification fields and advances
	// version/time. The caller chooses a legal terminal state and safe code.
	// queryProviderAccountsCompleteAuthorization Меняет только совпавшую pending-попытку точных
	// организации, аккаунта, authorization и materializer, очищает поля подтверждения, увеличивает
	// версию и время. Допустимый итог и безопасный код выбирает вызывающий.
	//go:embed sql/provider_accounts_complete_authorization.sql
	queryProviderAccountsCompleteAuthorization string
	// queryProviderAccountsInsertCredentialRevision Inserts the next immutable revision descriptor
	// under an already resolved account, returning its internal ID. Numbering depends on caller
	// account locking; this SQL does not independently validate the supplied organization/secret
	// ownership or read secret bytes.
	// queryProviderAccountsInsertCredentialRevision Вставляет следующий неизменяемый дескриптор
	// ревизии разрешённого аккаунта, возвращает внутренний ID. Нумерация требует блокировки аккаунта
	// вызывающим; SQL отдельно не проверяет переданную организацию или владение секретом и не читает
	// значения.
	//go:embed sql/provider_accounts_insert_credential_revision.sql
	queryProviderAccountsInsertCredentialRevision string
	// queryProviderAccountsActivateCredential Sets the resolved internal account credential pointer
	// and masked external identity, activates/enables it and increments version/time. Tenant, actor,
	// revision ownership and OCC must be established by the surrounding locked path.
	// queryProviderAccountsActivateCredential Назначает разрешённому внутреннему аккаунту credential и
	// маскированную внешнюю личность, активирует его и увеличивает версию с временем. Организация,
	// субъект, принадлежность ревизии и OCC заранее проверяются окружающим заблокированным путём.
	//go:embed sql/provider_accounts_activate_credential.sql
	queryProviderAccountsActivateCredential string
	// queryProviderAccountsUpdateLifecycle Updates the caller-selected lifecycle/enabled flags of a
	// resolved internal account, optionally clearing its credential pointer and advancing
	// version/time. It does not itself decide allowed transitions or revoke runtime consumers.
	// queryProviderAccountsUpdateLifecycle Меняет заданные вызывающим жизненный цикл и enabled
	// разрешённого внутреннего аккаунта, при необходимости очищает credential и увеличивает версию с
	// временем. Сам не определяет переходы и не отзывает runtime-потребителей.
	//go:embed sql/provider_accounts_update_lifecycle.sql
	queryProviderAccountsUpdateLifecycle string
	// queryProviderAccountsFailPendingAuthorizations Marks all pending attempts of a resolved account
	// failed with the safe caller code, clears verification fields and increases versions.
	// Account/tenant eligibility and atomic surrounding effects belong to the caller.
	// queryProviderAccountsFailPendingAuthorizations Помечает pending-попытки разрешённого аккаунта
	// FAILED с безопасным кодом, очищает подтверждение и увеличивает версии. Пригодность аккаунта и
	// организации, атомарные соседние эффекты принадлежат вызывающему.
	//go:embed sql/provider_accounts_fail_pending_authorizations.sql
	queryProviderAccountsFailPendingAuthorizations string
	// queryProviderAccountsMaterializationReferenced Checks whether exact organization/account still
	// references a reserved authorization, matching materializer attempt or complete secret
	// object-identity/digest descriptor. It is an owner-reference guard rather than permission to
	// retain arbitrary or mismatched objects.
	// queryProviderAccountsMaterializationReferenced Проверяет ссылки точного аккаунта организации на
	// RESERVED authorization, совпавшую materializer attempt либо полный дескриптор идентичности и
	// хеша секрета. Это проверка ссылок владельца, а не разрешение сохранять произвольный или иной
	// объект.
	//go:embed sql/provider_accounts_materialization_referenced.sql
	queryProviderAccountsMaterializationReferenced string
	// queryProviderAccountsMaterializationGuard Locks the exact organization/account row before
	// materialization reference reconciliation. It reads no credential bytes and does not
	// independently authorize materialization.
	// queryProviderAccountsMaterializationGuard Блокирует точный аккаунт организации до сверки ссылок
	// материализации. Значений credential не читает и самостоятельно материализацию не разрешает.
	//go:embed sql/provider_accounts_materialization_guard.sql
	queryProviderAccountsMaterializationGuard string
	// queryProviderAccountsCleanupGuard Projects active CLAIMED runtime leases and warm assistant
	// consumers for the exact organization/account. The booleans let the owner defer cleanup; they do
	// not execute cleanup or refresh credentials.
	// queryProviderAccountsCleanupGuard Проецирует активные CLAIMED runtime-аренды и тёплых
	// assistant-потребителей точного аккаунта организации. Boolean позволяют владельцу отложить
	// очистку; удаления и обновления credential нет.
	//go:embed sql/provider_accounts_cleanup_guard.sql
	queryProviderAccountsCleanupGuard string
	// queryProviderAccountsIsAPIKey Checks the latest authorized method of the exact
	// organization/account and returns false when absent or not API_KEY. It reads authorization
	// metadata without revealing the key.
	// queryProviderAccountsIsAPIKey Проверяет последний авторизованный метод точного аккаунта
	// организации; отсутствие или иной метод даёт false. Читает метаданные авторизации без раскрытия
	// ключа.
	//go:embed sql/provider_accounts_is_api_key.sql
	queryProviderAccountsIsAPIKey string
	// queryProviderCredentialCleanupScheduleRevision Schedules an exact organization/account/revision
	// descriptor and only moves an existing root PENDING task earlier on conflict. Terminal/claimed
	// tasks are not reset and no external deletion is performed.
	// queryProviderCredentialCleanupScheduleRevision Планирует точный дескриптор организации, аккаунта
	// и ревизии, при конфликте лишь приближает срок корневой PENDING задачи. Terminal и захваченные
	// задачи не сбрасываются, внешнего удаления нет.
	//go:embed sql/provider_credential_cleanup_schedule_revision.sql
	queryProviderCredentialCleanupScheduleRevision string
	// queryProviderCredentialCleanupScheduleAccount Schedules descriptors of all exact
	// organization/account revisions, preserving root-task uniqueness and only advancing PENDING
	// eligibility. Runtime reference guards remain required before claims/effects.
	// queryProviderCredentialCleanupScheduleAccount Планирует дескрипторы всех ревизий точного
	// аккаунта организации, сохраняет уникальность корневой задачи и лишь приближает PENDING
	// eligibility. До захвата или эффекта остаются проверки runtime-ссылок.
	//go:embed sql/provider_credential_cleanup_schedule_account.sql
	queryProviderCredentialCleanupScheduleAccount string
)
