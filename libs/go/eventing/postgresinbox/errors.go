package postgresinbox

import (
	"context"
	"errors"
)

const (
	errorTextInvalidConfiguration      = "postgres inbox configuration is invalid"
	errorTextInvalidConsumer           = "postgres inbox consumer is invalid"
	errorTextInvalidEvent              = "postgres inbox event is invalid"
	errorTextInvalidRepair             = "postgres inbox repair request is invalid"
	errorTextInvalidRecovery           = "postgres inbox recovery request is invalid"
	errorTextInvalidBlockageRead       = "postgres inbox blockage read is invalid"
	errorTextInvalidDeliveryRead       = "postgres inbox delivery outcome read is invalid"
	errorTextInvalidEffectOperation    = "postgres inbox effect operation is invalid"
	errorTextEffectOperationNotAllowed = "postgres inbox effect operation is not allowed"
	errorTextInvalidEffectInput        = "postgres inbox effect input is invalid"
	errorTextEffectOperation           = "postgres inbox effect operation failed"
	errorTextProcessorStopped          = "postgres inbox processor is stopped"
	errorTextSequenceGap               = "postgres inbox event sequence gap"
	errorTextEventConflict             = "postgres inbox event conflicts with durable evidence"
	errorTextStaleClaim                = "postgres inbox claim is stale"
	errorTextRetryExhausted            = "postgres inbox retry budget is exhausted"
	errorTextOperatorConflict          = "postgres inbox operator request conflicts with durable receipt"
	errorTextRepairNotAllowed          = "postgres inbox repair is not allowed"
	errorTextOperatorNotAllowed        = "postgres inbox operator action is not allowed"
	errorTextRecoveryNotAllowed        = "postgres inbox recovery is not allowed"
	errorTextBlockageNotFound          = "postgres inbox blockage was not found"
	errorTextDeliveryOutcomeNotFound   = "postgres inbox delivery outcome was not found"
	errorTextSchemaMismatch            = "postgres inbox schema contract mismatch"
	errorTextEffectFailed              = "postgres inbox effect failed"
	errorTextDatabaseOperation         = "postgres inbox database operation failed"
	errorTextTransactionBegin          = "begin postgres inbox transaction"
	errorTextTransactionCommit         = "commit postgres inbox transaction"
	errorTextTransactionRollback       = "rollback postgres inbox transaction"
	errorTextSavepointBegin            = "begin postgres inbox effect savepoint"
	errorTextSavepointCommit           = "commit postgres inbox effect savepoint"
	errorTextSavepointRollback         = "rollback postgres inbox effect savepoint"

	errorCodeEffectFailed   = "effect_failed"
	errorCodeRetryExhausted = "retry_exhausted"
	errorCodeSequenceGap    = "sequence_gap"
)

var (
	ErrInvalidConfiguration       = errors.New(errorTextInvalidConfiguration)
	ErrInvalidConsumer            = errors.New(errorTextInvalidConsumer)
	ErrInvalidEvent               = errors.New(errorTextInvalidEvent)
	ErrInvalidRepair              = errors.New(errorTextInvalidRepair)
	ErrInvalidRecovery            = errors.New(errorTextInvalidRecovery)
	ErrInvalidBlockageRead        = errors.New(errorTextInvalidBlockageRead)
	ErrInvalidDeliveryOutcomeRead = errors.New(errorTextInvalidDeliveryRead)
	ErrInvalidEffectOperation     = errors.New(errorTextInvalidEffectOperation)
	ErrEffectOperationNotAllowed  = errors.New(errorTextEffectOperationNotAllowed)
	ErrInvalidEffectInput         = errors.New(errorTextInvalidEffectInput)
	ErrProcessorStopped           = errors.New(errorTextProcessorStopped)
	ErrSequenceGap                = errors.New(errorTextSequenceGap)
	ErrEventConflict              = errors.New(errorTextEventConflict)
	ErrStaleClaim                 = errors.New(errorTextStaleClaim)
	ErrRetryExhausted             = errors.New(errorTextRetryExhausted)
	ErrOperatorConflict           = errors.New(errorTextOperatorConflict)
	ErrRepairNotAllowed           = errors.New(errorTextRepairNotAllowed)
	ErrOperatorNotAllowed         = errors.New(errorTextOperatorNotAllowed)
	ErrRecoveryNotAllowed         = errors.New(errorTextRecoveryNotAllowed)
	ErrBlockageNotFound           = errors.New(errorTextBlockageNotFound)
	ErrDeliveryOutcomeNotFound    = errors.New(errorTextDeliveryOutcomeNotFound)
	ErrSchemaMismatch             = errors.New(errorTextSchemaMismatch)
	ErrEffectFailed               = errors.New(errorTextEffectFailed)
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
