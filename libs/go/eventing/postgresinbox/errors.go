package postgresinbox

import (
	"context"
	"errors"
)

const (
	errorTextInvalidConfiguration = "postgres inbox configuration is invalid"
	errorTextInvalidConsumer      = "postgres inbox consumer is invalid"
	errorTextInvalidEvent         = "postgres inbox event is invalid"
	errorTextInvalidRepair        = "postgres inbox repair request is invalid"
	errorTextProcessorStopped     = "postgres inbox processor is stopped"
	errorTextSequenceGap          = "postgres inbox event sequence gap"
	errorTextEventConflict        = "postgres inbox event conflicts with durable evidence"
	errorTextStaleClaim           = "postgres inbox claim is stale"
	errorTextRetryExhausted       = "postgres inbox retry budget is exhausted"
	errorTextRepairConflict       = "postgres inbox repair conflicts with durable receipt"
	errorTextRepairNotAllowed     = "postgres inbox repair is not allowed"
	errorTextSchemaMismatch       = "postgres inbox schema contract mismatch"
	errorTextEffectFailed         = "postgres inbox effect failed"
	errorTextDatabaseOperation    = "postgres inbox database operation failed"
	errorTextTransactionBegin     = "begin postgres inbox transaction"
	errorTextTransactionCommit    = "commit postgres inbox transaction"
	errorTextTransactionRollback  = "rollback postgres inbox transaction"
	errorTextSavepointBegin       = "begin postgres inbox effect savepoint"
	errorTextSavepointCommit      = "commit postgres inbox effect savepoint"
	errorTextSavepointRollback    = "rollback postgres inbox effect savepoint"

	errorCodeEffectFailed   = "effect_failed"
	errorCodeRetryExhausted = "retry_exhausted"
	errorCodeSequenceGap    = "sequence_gap"
)

var (
	ErrInvalidConfiguration = errors.New(errorTextInvalidConfiguration)
	ErrInvalidConsumer      = errors.New(errorTextInvalidConsumer)
	ErrInvalidEvent         = errors.New(errorTextInvalidEvent)
	ErrInvalidRepair        = errors.New(errorTextInvalidRepair)
	ErrProcessorStopped     = errors.New(errorTextProcessorStopped)
	ErrSequenceGap          = errors.New(errorTextSequenceGap)
	ErrEventConflict        = errors.New(errorTextEventConflict)
	ErrStaleClaim           = errors.New(errorTextStaleClaim)
	ErrRetryExhausted       = errors.New(errorTextRetryExhausted)
	ErrRepairConflict       = errors.New(errorTextRepairConflict)
	ErrRepairNotAllowed     = errors.New(errorTextRepairNotAllowed)
	ErrSchemaMismatch       = errors.New(errorTextSchemaMismatch)
	ErrEffectFailed         = errors.New(errorTextEffectFailed)
)

type safeError struct {
	message string
	cause   error
}

func (err *safeError) Error() string { return err.message }
func (err *safeError) Unwrap() error { return err.cause }

func wrapSafe(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return &safeError{message: message, cause: cause}
}

func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
