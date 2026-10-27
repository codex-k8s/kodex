package platform

import _ "embed"

var (
	// queryProviderCredentialCleanupExpireTerminalClaims embeds terminalization of expired CLAIMED
	// cleanup tasks at their attempt limit, clearing lease fields and recording a deterministic
	// dead-letter receipt. The query is global; trusted worker admission and the enclosing transaction
	// belong to its caller.
	// queryProviderCredentialCleanupExpireTerminalClaims встраивает завершение просроченных CLAIMED
	// задач очистки при исчерпании попыток, очищая аренду и записывая детерминированную квитанцию
	// dead-letter. Запрос глобальный; допуск доверенного worker и общая транзакция принадлежат
	// вызывающему коду.
	//go:embed sql/provider_credential_cleanup_expire_terminal_claims.sql
	queryProviderCredentialCleanupExpireTerminalClaims string
	// queryProviderCredentialCleanupLockClaimableAccounts embeds a bounded account lock selection
	// using eligible pending or expired cleanup tasks, attempt limits and the database eligibility
	// predicate. SKIP LOCKED supports concurrent workers; selecting an account does not itself claim
	// its tasks.
	// queryProviderCredentialCleanupLockClaimableAccounts встраивает ограниченный выбор блокируемых
	// аккаунтов по допустимым ожидающим или просроченным задачам, лимиту попыток и условию
	// допустимости БД. SKIP LOCKED поддерживает конкурентных worker; выбор аккаунта сам не захватывает
	// задачи.
	//go:embed sql/provider_credential_cleanup_lock_claimable_accounts.sql
	queryProviderCredentialCleanupLockClaimableAccounts string
	// queryProviderCredentialCleanupSelectClaimableTasks embeds bounded, ordered SKIP LOCKED task
	// selection for one locked account, including exact secret/materializer and recovery metadata. It
	// enforces pending/expired eligibility and attempt limits, but worker authorization and subsequent
	// claim generation remain separate.
	// queryProviderCredentialCleanupSelectClaimableTasks встраивает ограниченный упорядоченный выбор
	// задач SKIP LOCKED одного заблокированного аккаунта с точными метаданными секрета, материализации
	// и восстановления. Проверяет ожидание либо истёкшую аренду и лимит попыток, но авторизация worker
	// и следующее поколение захвата выполняются отдельно.
	//go:embed sql/provider_credential_cleanup_select_claimable_tasks.sql
	queryProviderCredentialCleanupSelectClaimableTasks string
	// queryProviderCredentialCleanupClaimTask embeds a conditional claim of a pending or expired task,
	// incrementing lease generation and attempts and storing the supplied owner/deadline. It returns
	// the new claim tuple; this update assumes the caller already locked and authorized the eligible
	// task.
	// queryProviderCredentialCleanupClaimTask встраивает условный захват ожидающей или просроченной
	// задачи с увеличением поколения аренды и попыток и сохранением переданных владельца и срока.
	// Возвращает новый кортеж захвата; предполагает, что вызывающий код уже заблокировал допустимую
	// задачу и проверил полномочия.
	//go:embed sql/provider_credential_cleanup_claim_task.sql
	queryProviderCredentialCleanupClaimTask string
	// queryProviderCredentialCleanupLockTask embeds an exact task-ref row lock with lease, attempts,
	// terminal receipt and completion descriptor plus authorization/materializer references. The query
	// is not actor scoped; the trusted caller must validate the returned claim and lifecycle.
	// queryProviderCredentialCleanupLockTask встраивает блокировку по точной ссылке задачи с арендой,
	// попытками, конечной квитанцией и дескриптором завершения и ссылками авторизации и
	// материализации. Запрос не ограничен субъектом; доверенный вызывающий код обязан проверить захват
	// и жизненный цикл.
	//go:embed sql/provider_credential_cleanup_lock_task.sql
	queryProviderCredentialCleanupLockTask string
	// queryProviderCredentialCleanupCompleteTask embeds completion of an exact live CLAIMED task
	// owner/generation, clearing lease and error fields and storing a terminal receipt and descriptor.
	// The caller must check affected rows and independently validate evidence of external cleanup.
	// queryProviderCredentialCleanupCompleteTask встраивает завершение точных владельца и поколения
	// действующей CLAIMED задачи, очищая аренду и ошибку и сохраняя конечные квитанцию и дескриптор.
	// Вызывающий код обязан проверить число изменённых строк и отдельно проверить доказательства
	// внешней очистки.
	//go:embed sql/provider_credential_cleanup_complete_task.sql
	queryProviderCredentialCleanupCompleteTask string
	// queryProviderCredentialCleanupFailTask embeds the caller-selected failure/retry state, next
	// eligibility and safe error/receipt for an exact live task owner/generation, releasing its lease.
	// State/code validation, attempt policy and affected-row handling belong to the caller.
	// queryProviderCredentialCleanupFailTask встраивает выбранные вызывающим кодом состояние ошибки
	// или повтора, следующий срок допустимости и безопасные ошибку и квитанцию для точных владельца и
	// поколения действующей задачи, освобождая аренду. Проверка состояния и кода, политика попыток и
	// число изменённых строк остаются у вызывающего кода.
	//go:embed sql/provider_credential_cleanup_fail_task.sql
	queryProviderCredentialCleanupFailTask string
)
