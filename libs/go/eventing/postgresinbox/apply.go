package postgresinbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// apply fences a leased event, runs its effect in a savepoint, and commits the inbox/cursor result with the effect.
// apply проверяет ограждение арендованного события, исполняет effect в savepoint и фиксирует inbox/cursor вместе с ним.
func (processor *Processor) apply(
	ctx context.Context,
	record eventRecord,
	claim Claim,
	handler Handler,
) (result Result, returnErr error) {
	if err := validateClaim(claim); err != nil || claim.LeaseOwner != processor.config.InstanceID {
		return Result{}, ErrStaleClaim
	}
	var durableErr error
	err := processor.transact(ctx, func(tx pgx.Tx) error {
		cursor, lockErr := processor.ensureAndLockCursor(
			ctx,
			tx,
			claim.Consumer,
			claim.OrderingKey,
		)
		if lockErr != nil {
			return lockErr
		}
		row, getErr := processor.getInboxByEvent(
			ctx,
			tx,
			claim.Consumer,
			claim.EventID,
		)
		if getErr != nil {
			return wrapSafe(errorTextDatabaseOperation, getErr)
		}
		if !claimMatchesRow(claim, row) || !row.LeaseActive ||
			claim.Attempts != row.Attempts || claim.MaxAttempts != row.MaxAttempts ||
			cursor.LastSequence+1 != claim.EventSequence ||
			!sameDigest(record.Digest[:], row.EventDigest) {
			return ErrStaleClaim
		}

		workTx, beginErr := tx.Begin(ctx)
		if beginErr != nil {
			return wrapSafe(errorTextSavepointBegin, beginErr)
		}
		workOpen := true
		rollbackWork := func() error {
			if !workOpen {
				return nil
			}
			rollbackErr := workTx.Rollback(ctx)
			workOpen = false
			if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return wrapSafe(errorTextSavepointRollback, rollbackErr)
			}
			return nil
		}

		effectCtx, cancelEffect := context.WithTimeout(ctx, processor.config.EffectTimeout)
		effectErr := handler(effectCtx, &effectTx{
			ctx:        effectCtx,
			tx:         workTx,
			operations: processor.effectOperations,
		}, EventSnapshot{envelope: record.Envelope})
		if effectErr == nil && effectCtx.Err() != nil {
			effectErr = effectCtx.Err()
		}
		cancelEffect()
		if effectErr != nil {
			if rollbackErr := rollbackWork(); rollbackErr != nil {
				return rollbackErr
			}
			failure := classifyEffectFailure(effectErr)
			if failure.Retryable() && claim.Attempts < claim.MaxAttempts {
				if err := processor.markRetry(ctx, tx, claim, failure.Code()); err != nil {
					return err
				}
				result = Result{
					Outcome: OutcomeRetry,
					Action:  BrokerActionNACKRetry,
					Durable: true,
				}
			} else {
				if err := processor.markDeadLetter(ctx, tx, claim, failure.Code()); err != nil {
					return err
				}
				result = Result{
					Outcome: OutcomeDeadLetter,
					Action:  BrokerActionNACKTerminal,
					Durable: true,
				}
			}
			durableErr = failure
			return nil
		}

		if err := processor.complete(ctx, workTx, claim); err != nil {
			return errors.Join(err, rollbackWork())
		}
		if err := processor.advanceCursor(ctx, workTx, claim); err != nil {
			return errors.Join(err, rollbackWork())
		}
		if commitErr := workTx.Commit(ctx); commitErr != nil {
			workOpen = false
			return wrapSafe(errorTextSavepointCommit, commitErr)
		}
		workOpen = false
		result = Result{
			Outcome: OutcomeProcessed,
			Action:  BrokerActionACK,
			Durable: true,
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return result, durableErr
}

// claimMatchesRow requires the locked inbox row to match every event, ordering and lease-fence coordinate of the claim.
// claimMatchesRow требует совпадения строки inbox со всеми координатами события, порядка и lease fence из claim.
func claimMatchesRow(claim Claim, row inboxRow) bool {
	return row.State == stateProcessing &&
		row.EventID == claim.EventID &&
		row.EventSequence == claim.EventSequence &&
		row.LeaseOwner != nil && *row.LeaseOwner == claim.LeaseOwner &&
		row.LeaseToken != nil && *row.LeaseToken == claim.LeaseToken &&
		row.LeaseGeneration == claim.LeaseGeneration &&
		row.LeaseFence == claim.LeaseFence &&
		sameOrderingKey(row.OrderingKey, claim.OrderingKey) &&
		sameDigest(row.EventDigest, claim.EventDigest[:])
}

// complete marks exactly one fenced inbox row complete and sets its retention horizon inside the effect savepoint.
// complete отмечает ровно одну защищённую строку inbox завершённой и задаёт срок хранения внутри savepoint effect.
func (processor *Processor) complete(ctx context.Context, tx pgx.Tx, claim Claim) error {
	arguments := claimMutationArguments(claim)
	arguments["retention_seconds"] = processor.config.RetentionHorizon.Seconds()
	tag, err := tx.Exec(
		ctx,
		processor.queries.inboxComplete,
		arguments,
	)
	return requireOneClaimMutation(tag, err)
}

// advanceCursor moves exactly one ordering cursor to the completed event within the same savepoint.
// advanceCursor продвигает ровно один cursor порядка к завершённому событию в том же savepoint.
func (processor *Processor) advanceCursor(ctx context.Context, tx pgx.Tx, claim Claim) error {
	tag, err := tx.Exec(
		ctx,
		processor.queries.cursorAdvance,
		pgx.StrictNamedArgs{
			"consumer_name":  claim.Consumer.Name,
			"consumer_scope": claim.Consumer.Scope,
			"ordering_key":   claim.OrderingKey,
			"event_sequence": claim.EventSequence,
			"event_id":       claim.EventID,
			"event_digest":   claim.EventDigest[:],
		},
	)
	return requireOneClaimMutation(tag, err)
}

// markRetry records a fenced retry with bounded backoff and a safe error code after the effect savepoint rolls back.
// markRetry фиксирует защищённый повтор с ограниченной задержкой и безопасным кодом ошибки после отката effect savepoint.
func (processor *Processor) markRetry(
	ctx context.Context,
	tx pgx.Tx,
	claim Claim,
	errorCode string,
) error {
	arguments := claimMutationArguments(claim)
	arguments["backoff_seconds"] = processor.backoff(claim.Attempts).Seconds()
	arguments["error_code"] = normalizeErrorCode(errorCode)
	tag, err := tx.Exec(
		ctx,
		processor.queries.inboxMarkRetry,
		arguments,
	)
	return requireOneClaimMutation(tag, err)
}

// markDeadLetter records a fenced terminal failure after the effect savepoint rolls back without advancing the cursor.
// markDeadLetter фиксирует защищённый конечный отказ после отката effect savepoint, не продвигая cursor.
func (processor *Processor) markDeadLetter(
	ctx context.Context,
	tx pgx.Tx,
	claim Claim,
	errorCode string,
) error {
	arguments := claimMutationArguments(claim)
	arguments["error_code"] = normalizeErrorCode(errorCode)
	tag, err := tx.Exec(
		ctx,
		processor.queries.inboxMarkDeadLetter,
		arguments,
	)
	return requireOneClaimMutation(tag, err)
}

// claimMutationArguments binds the exact consumer, event and lease fence to one inbox transition.
// claimMutationArguments привязывает точные consumer, event и lease fence к одному переходу inbox.
func claimMutationArguments(claim Claim) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"consumer_name":    claim.Consumer.Name,
		"consumer_scope":   claim.Consumer.Scope,
		"event_id":         claim.EventID,
		"event_digest":     claim.EventDigest[:],
		"lease_owner":      claim.LeaseOwner,
		"lease_token":      claim.LeaseToken,
		"lease_generation": claim.LeaseGeneration,
		"lease_fence":      claim.LeaseFence,
	}
}

// requireOneClaimMutation preserves the database error or rejects a nonunique fenced transition as stale.
// requireOneClaimMutation сохраняет ошибку БД либо отвергает неединственный защищённый переход как устаревший.
func requireOneClaimMutation(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return wrapSafe(errorTextDatabaseOperation, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleClaim
	}
	return nil
}

// backoff doubles the retry delay for later attempts but saturates at the configured maximum without overflow.
// backoff удваивает задержку следующих попыток, но останавливается на заданном максимуме без переполнения.
func (processor *Processor) backoff(attempt uint32) time.Duration {
	backoff := processor.config.InitialBackoff
	if attempt <= 1 {
		return backoff
	}
	for current := uint32(1); current < attempt; current++ {
		if backoff >= processor.config.MaximumBackoff ||
			backoff > processor.config.MaximumBackoff-backoff {
			return processor.config.MaximumBackoff
		}
		backoff += backoff
	}
	if backoff > processor.config.MaximumBackoff {
		return processor.config.MaximumBackoff
	}
	return backoff
}

// normalizeErrorCode accepts only a bounded safe code and replaces invalid input with the fixed failure code.
// normalizeErrorCode принимает только ограниченный безопасный код и заменяет неверный ввод постоянным кодом отказа.
func normalizeErrorCode(code string) string {
	if len(code) == 0 || len(code) > maximumErrorCodeLength ||
		!errorCodePattern.MatchString(code) {
		return errorCodeEffectFailed
	}
	return code
}
