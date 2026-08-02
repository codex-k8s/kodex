package postgresinbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

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
		effectErr := handler(effectCtx, workTx, record.Envelope)
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

func (processor *Processor) complete(ctx context.Context, tx pgx.Tx, claim Claim) error {
	tag, err := tx.Exec(
		ctx,
		processor.queries.inboxComplete,
		pgx.StrictNamedArgs{
			"consumer_name":     claim.Consumer.Name,
			"consumer_scope":    claim.Consumer.Scope,
			"event_id":          claim.EventID,
			"event_digest":      claim.EventDigest[:],
			"lease_owner":       claim.LeaseOwner,
			"lease_token":       claim.LeaseToken,
			"lease_generation":  claim.LeaseGeneration,
			"lease_fence":       claim.LeaseFence,
			"retention_seconds": processor.config.RetentionHorizon.Seconds(),
		},
	)
	if err != nil {
		return wrapSafe(errorTextDatabaseOperation, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleClaim
	}
	return nil
}

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
	if err != nil {
		return wrapSafe(errorTextDatabaseOperation, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleClaim
	}
	return nil
}

func (processor *Processor) markRetry(
	ctx context.Context,
	tx pgx.Tx,
	claim Claim,
	errorCode string,
) error {
	tag, err := tx.Exec(
		ctx,
		processor.queries.inboxMarkRetry,
		pgx.StrictNamedArgs{
			"consumer_name":    claim.Consumer.Name,
			"consumer_scope":   claim.Consumer.Scope,
			"event_id":         claim.EventID,
			"event_digest":     claim.EventDigest[:],
			"lease_owner":      claim.LeaseOwner,
			"lease_token":      claim.LeaseToken,
			"lease_generation": claim.LeaseGeneration,
			"lease_fence":      claim.LeaseFence,
			"backoff_seconds":  processor.backoff(claim.Attempts).Seconds(),
			"error_code":       normalizeErrorCode(errorCode),
		},
	)
	if err != nil {
		return wrapSafe(errorTextDatabaseOperation, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleClaim
	}
	return nil
}

func (processor *Processor) markDeadLetter(
	ctx context.Context,
	tx pgx.Tx,
	claim Claim,
	errorCode string,
) error {
	tag, err := tx.Exec(
		ctx,
		processor.queries.inboxMarkDeadLetter,
		pgx.StrictNamedArgs{
			"consumer_name":    claim.Consumer.Name,
			"consumer_scope":   claim.Consumer.Scope,
			"event_id":         claim.EventID,
			"event_digest":     claim.EventDigest[:],
			"lease_owner":      claim.LeaseOwner,
			"lease_token":      claim.LeaseToken,
			"lease_generation": claim.LeaseGeneration,
			"lease_fence":      claim.LeaseFence,
			"error_code":       normalizeErrorCode(errorCode),
		},
	)
	if err != nil {
		return wrapSafe(errorTextDatabaseOperation, err)
	}
	if tag.RowsAffected() != 1 {
		return ErrStaleClaim
	}
	return nil
}

func (processor *Processor) backoff(attempt uint32) time.Duration {
	backoff := processor.config.InitialBackoff
	if attempt <= 1 {
		return backoff
	}
	for current := uint32(1); current < attempt; current++ {
		if backoff >= processor.config.MaximumBackoff/2 {
			return processor.config.MaximumBackoff
		}
		backoff *= 2
	}
	if backoff > processor.config.MaximumBackoff {
		return processor.config.MaximumBackoff
	}
	return backoff
}

func normalizeErrorCode(code string) string {
	if len(code) == 0 || len(code) > maximumErrorCodeLength ||
		!errorCodePattern.MatchString(code) {
		return errorCodeEffectFailed
	}
	return code
}
