package postgresinbox

import (
	"context"
	"errors"
)

const (
	// errorTextInvalidConfiguration hides the specific rejected configuration field from callers.
	// errorTextInvalidConfiguration скрывает от вызывающего конкретное отвергнутое поле конфигурации.
	errorTextInvalidConfiguration = "postgres inbox configuration is invalid"
	// errorTextInvalidConsumer reports a consumer identity outside the accepted domain.
	// errorTextInvalidConsumer сообщает об идентичности потребителя вне допустимой области.
	errorTextInvalidConsumer = "postgres inbox consumer is invalid"
	// errorTextInvalidEvent reports an event that cannot enter the durable inbox.
	// errorTextInvalidEvent сообщает о событии, которое нельзя принять в устойчивый inbox.
	errorTextInvalidEvent = "postgres inbox event is invalid"
	// errorTextInvalidRepair reports malformed bounded repair input before operator authorization.
	// errorTextInvalidRepair сообщает о неверном ограниченном запросе исправления до авторизации оператора.
	errorTextInvalidRepair = "postgres inbox repair request is invalid"
	// errorTextInvalidRecovery reports malformed recovery input before inspecting durable state.
	// errorTextInvalidRecovery сообщает о неверном запросе восстановления до чтения устойчивого состояния.
	errorTextInvalidRecovery = "postgres inbox recovery request is invalid"
	// errorTextInvalidBlockageRead reports an invalid target or cursor for blockage inspection.
	// errorTextInvalidBlockageRead сообщает о неверной цели или курсоре чтения блокировок.
	errorTextInvalidBlockageRead = "postgres inbox blockage read is invalid"
	// errorTextInvalidDeliveryRead reports invalid exact-event evidence requested for delivery readback.
	// errorTextInvalidDeliveryRead сообщает о неверном точном свидетельстве события для чтения доставки.
	errorTextInvalidDeliveryRead = "postgres inbox delivery outcome read is invalid"
	// errorTextInvalidEffectOperation identifies a malformed operation in the closed effect registry.
	// errorTextInvalidEffectOperation обозначает неверную операцию закрытого реестра эффектов.
	errorTextInvalidEffectOperation = "postgres inbox effect operation is invalid"
	// errorTextEffectOperationNotAllowed rejects an operation absent from the consumer's grant.
	// errorTextEffectOperationNotAllowed отклоняет операцию вне полномочий потребителя.
	errorTextEffectOperationNotAllowed = "postgres inbox effect operation is not allowed"
	// errorTextInvalidEffectInput reports a payload that fails its operation-specific contract.
	// errorTextInvalidEffectInput сообщает о содержимом, нарушающем контракт конкретной операции.
	errorTextInvalidEffectInput = "postgres inbox effect input is invalid"
	// errorTextEffectOperation masks private effect failure details from the outer result.
	// errorTextEffectOperation скрывает закрытые детали ошибки эффекта во внешнем результате.
	errorTextEffectOperation = "postgres inbox effect operation failed"
	// errorTextProcessorStopped reports rejection after the processor shutdown barrier.
	// errorTextProcessorStopped сообщает об отказе после барьера остановки процессора.
	errorTextProcessorStopped = "postgres inbox processor is stopped"
	// errorTextSequenceGap identifies a missing predecessor before broker acknowledgement.
	// errorTextSequenceGap обозначает отсутствующего предшественника до подтверждения брокеру.
	errorTextSequenceGap = "postgres inbox event sequence gap"
	// errorTextEventConflict rejects reused event identity with different durable evidence.
	// errorTextEventConflict отклоняет повторное использование ID события с другими устойчивыми данными.
	errorTextEventConflict = "postgres inbox event conflicts with durable evidence"
	// errorTextStaleClaim reports a lease whose exact token, generation, or fence no longer matches.
	// errorTextStaleClaim сообщает об аренде с устаревшим токеном, поколением или fence.
	errorTextStaleClaim = "postgres inbox claim is stale"
	// errorTextRetryExhausted marks delivery that reached its bounded attempt budget.
	// errorTextRetryExhausted обозначает доставку с исчерпанным лимитом попыток.
	errorTextRetryExhausted = "postgres inbox retry budget is exhausted"
	// errorTextOperatorConflict rejects reuse of an operator idempotency key for another action.
	// errorTextOperatorConflict отклоняет повторное применение ключа идемпотентности к другому действию.
	errorTextOperatorConflict = "postgres inbox operator request conflicts with durable receipt"
	// errorTextRepairNotAllowed denies a dead-letter transition outside its exact preconditions.
	// errorTextRepairNotAllowed запрещает переход dead-letter вне точных предусловий.
	errorTextRepairNotAllowed = "postgres inbox repair is not allowed"
	// errorTextOperatorNotAllowed denotes failed operator authorization, not an input parse failure.
	// errorTextOperatorNotAllowed обозначает отказ авторизации оператора, а не ошибку разбора запроса.
	errorTextOperatorNotAllowed = "postgres inbox operator action is not allowed"
	// errorTextRecoveryNotAllowed denies recovery when durable state forbids the requested directive.
	// errorTextRecoveryNotAllowed запрещает восстановление, если устойчивое состояние не допускает директиву.
	errorTextRecoveryNotAllowed = "postgres inbox recovery is not allowed"
	// errorTextBlockageNotFound distinguishes an absent exact blocker from a schema failure.
	// errorTextBlockageNotFound отличает отсутствие точной блокировки от ошибки схемы.
	errorTextBlockageNotFound = "postgres inbox blockage was not found"
	// errorTextDeliveryOutcomeNotFound distinguishes absent delivery evidence from a stale event.
	// errorTextDeliveryOutcomeNotFound отличает отсутствие данных доставки от устаревшего события.
	errorTextDeliveryOutcomeNotFound = "postgres inbox delivery outcome was not found"
	// errorTextSchemaMismatch rejects unexpected database shape before semantic interpretation.
	// errorTextSchemaMismatch отклоняет неожиданную форму данных базы до смысловой обработки.
	errorTextSchemaMismatch = "postgres inbox schema contract mismatch"
	// errorTextEffectFailed masks an effect failure while retaining a stable outer classification.
	// errorTextEffectFailed скрывает детали ошибки эффекта, сохраняя устойчивую внешнюю классификацию.
	errorTextEffectFailed = "postgres inbox effect failed"
	// errorTextDatabaseOperation masks a storage failure without exposing its SQL or private values.
	// errorTextDatabaseOperation скрывает ошибку хранилища без раскрытия SQL и закрытых значений.
	errorTextDatabaseOperation = "postgres inbox database operation failed"
	// errorTextTransactionBegin labels failure to open the inbox's state-changing transaction.
	// errorTextTransactionBegin обозначает ошибку открытия транзакции изменения состояния inbox.
	errorTextTransactionBegin = "begin postgres inbox transaction"
	// errorTextTransactionCommit labels failure to durably commit inbox and effect outcomes.
	// errorTextTransactionCommit обозначает ошибку устойчивой фиксации inbox и результата эффекта.
	errorTextTransactionCommit = "commit postgres inbox transaction"
	// errorTextTransactionRollback labels a failed rollback after a transaction error.
	// errorTextTransactionRollback обозначает ошибку отката после сбоя транзакции.
	errorTextTransactionRollback = "rollback postgres inbox transaction"
	// errorTextSavepointBegin labels failure to isolate an effect within its transaction.
	// errorTextSavepointBegin обозначает ошибку изоляции эффекта внутри транзакции.
	errorTextSavepointBegin = "begin postgres inbox effect savepoint"
	// errorTextSavepointCommit labels failure to release a successful effect savepoint.
	// errorTextSavepointCommit обозначает ошибку освобождения успешной контрольной точки эффекта.
	errorTextSavepointCommit = "commit postgres inbox effect savepoint"
	// errorTextSavepointRollback labels failure to discard a failed effect's partial writes.
	// errorTextSavepointRollback обозначает ошибку отмены частичных записей неуспешного эффекта.
	errorTextSavepointRollback = "rollback postgres inbox effect savepoint"

	// errorCodeEffectFailed persists a bounded failure category for operator inspection.
	// errorCodeEffectFailed сохраняет ограниченную категорию ошибки для оператора.
	errorCodeEffectFailed = "effect_failed"
	// errorCodeRetryExhausted persists terminal delivery after all allowed attempts.
	// errorCodeRetryExhausted сохраняет завершённую доставку после всех допустимых попыток.
	errorCodeRetryExhausted = "retry_exhausted"
	// errorCodeSequenceGap persists a missing-predecessor category without event payload.
	// errorCodeSequenceGap сохраняет категорию отсутствующего предшественника без содержимого события.
	errorCodeSequenceGap = "sequence_gap"
)

var (
	// ErrInvalidConfiguration is the stable sentinel for rejected processor configuration.
	// ErrInvalidConfiguration — устойчивый маркер отклонённой конфигурации процессора.
	ErrInvalidConfiguration = errors.New(errorTextInvalidConfiguration)
	// ErrInvalidConsumer is returned before a consumer identity reaches storage.
	// ErrInvalidConsumer возвращается до передачи неверной идентичности потребителя в хранилище.
	ErrInvalidConsumer = errors.New(errorTextInvalidConsumer)
	// ErrInvalidEvent keeps malformed envelopes outside the durable inbox.
	// ErrInvalidEvent не допускает неверные конверты в устойчивый inbox.
	ErrInvalidEvent = errors.New(errorTextInvalidEvent)
	// ErrInvalidRepair rejects malformed repair requests before operator action.
	// ErrInvalidRepair отклоняет неверные запросы исправления до действия оператора.
	ErrInvalidRepair = errors.New(errorTextInvalidRepair)
	// ErrInvalidRecovery rejects malformed recovery requests before operator action.
	// ErrInvalidRecovery отклоняет неверные запросы восстановления до действия оператора.
	ErrInvalidRecovery = errors.New(errorTextInvalidRecovery)
	// ErrInvalidBlockageRead rejects malformed blocker targets or list cursors.
	// ErrInvalidBlockageRead отклоняет неверные цели блокировок или курсоры списка.
	ErrInvalidBlockageRead = errors.New(errorTextInvalidBlockageRead)
	// ErrInvalidDeliveryOutcomeRead rejects malformed exact delivery readback requests.
	// ErrInvalidDeliveryOutcomeRead отклоняет неверные запросы точного чтения доставки.
	ErrInvalidDeliveryOutcomeRead = errors.New(errorTextInvalidDeliveryRead)
	// ErrInvalidEffectOperation rejects an operation outside the closed effect grammar.
	// ErrInvalidEffectOperation отклоняет операцию вне закрытой грамматики эффектов.
	ErrInvalidEffectOperation = errors.New(errorTextInvalidEffectOperation)
	// ErrEffectOperationNotAllowed denies a syntactically valid but ungranted effect.
	// ErrEffectOperationNotAllowed запрещает синтаксически верный, но неразрешённый эффект.
	ErrEffectOperationNotAllowed = errors.New(errorTextEffectOperationNotAllowed)
	// ErrInvalidEffectInput rejects data that fails the selected effect's schema.
	// ErrInvalidEffectInput отклоняет данные, не соответствующие схеме выбранного эффекта.
	ErrInvalidEffectInput = errors.New(errorTextInvalidEffectInput)
	// ErrProcessorStopped prevents new work after shutdown begins.
	// ErrProcessorStopped запрещает новую работу после начала остановки.
	ErrProcessorStopped = errors.New(errorTextProcessorStopped)
	// ErrSequenceGap requests broker retry while an ordering predecessor is missing.
	// ErrSequenceGap требует повтора брокера, пока отсутствует предшественник порядка.
	ErrSequenceGap = errors.New(errorTextSequenceGap)
	// ErrEventConflict protects durable identity against a changed digest or sequence.
	// ErrEventConflict защищает устойчивую идентичность от смены дайджеста или номера.
	ErrEventConflict = errors.New(errorTextEventConflict)
	// ErrStaleClaim fences a lease whose exact identity is no longer current.
	// ErrStaleClaim ограждает аренду, точная идентичность которой уже не актуальна.
	ErrStaleClaim = errors.New(errorTextStaleClaim)
	// ErrRetryExhausted marks delivery beyond its bounded attempt budget.
	// ErrRetryExhausted обозначает доставку после исчерпания ограниченного числа попыток.
	ErrRetryExhausted = errors.New(errorTextRetryExhausted)
	// ErrOperatorConflict protects an existing receipt from a conflicting request digest.
	// ErrOperatorConflict защищает существующий receipt от иного дайджеста запроса.
	ErrOperatorConflict = errors.New(errorTextOperatorConflict)
	// ErrRepairNotAllowed rejects repair when exact dead-letter preconditions are absent.
	// ErrRepairNotAllowed отклоняет исправление без точных предусловий dead-letter.
	ErrRepairNotAllowed = errors.New(errorTextRepairNotAllowed)
	// ErrOperatorNotAllowed reports an explicit authorization denial for the operator target.
	// ErrOperatorNotAllowed сообщает об отказе полномочия для цели оператора.
	ErrOperatorNotAllowed = errors.New(errorTextOperatorNotAllowed)
	// ErrRecoveryNotAllowed rejects recovery incompatible with durable event state.
	// ErrRecoveryNotAllowed отклоняет восстановление, несовместимое с устойчивым состоянием события.
	ErrRecoveryNotAllowed = errors.New(errorTextRecoveryNotAllowed)
	// ErrBlockageNotFound reports an absent exact blocker after an authorized read.
	// ErrBlockageNotFound сообщает об отсутствии точной блокировки после авторизованного чтения.
	ErrBlockageNotFound = errors.New(errorTextBlockageNotFound)
	// ErrDeliveryOutcomeNotFound reports absent durable delivery evidence after authorization.
	// ErrDeliveryOutcomeNotFound сообщает об отсутствии устойчивых данных доставки после авторизации.
	ErrDeliveryOutcomeNotFound = errors.New(errorTextDeliveryOutcomeNotFound)
	// ErrSchemaMismatch stops interpretation when a stored row violates its contract.
	// ErrSchemaMismatch останавливает обработку строки хранилища, нарушающей контракт.
	ErrSchemaMismatch = errors.New(errorTextSchemaMismatch)
	// ErrEffectFailed classifies a failed effect without exposing private error details.
	// ErrEffectFailed классифицирует ошибку эффекта без раскрытия закрытых деталей.
	ErrEffectFailed = errors.New(errorTextEffectFailed)
)

// safeError exposes a bounded diagnostic while retaining its private cause for errors.Is.
// safeError выдаёт ограниченную диагностику, сохраняя закрытую причину для errors.Is.
type safeError struct {
	message string
	cause   error
}

// Error returns only the safe external diagnostic, never the wrapped cause text.
// Error возвращает только безопасную внешнюю диагностику, но не текст вложенной причины.
func (err *safeError) Error() string { return err.message }

// Unwrap retains programmatic cause matching without widening the display message.
// Unwrap сохраняет программное сопоставление причины, не расширяя отображаемое сообщение.
func (err *safeError) Unwrap() error { return err.cause }

// wrapSafe masks a non-nil private cause and preserves nil when no failure occurred.
// wrapSafe скрывает непустую закрытую причину и сохраняет nil при отсутствии ошибки.
func wrapSafe(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return &safeError{message: message, cause: cause}
}

// isContextDone groups cancellation and deadline expiry for lifecycle outcome accounting.
// isContextDone объединяет отмену и истечение срока для учёта исхода жизненного цикла.
func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
