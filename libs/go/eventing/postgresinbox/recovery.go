package postgresinbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Recover сохраняет provider-neutral решение после исчерпания broker redelivery.
func (processor *Processor) Recover(
	ctx context.Context,
	request RecoveryRequest,
) (receipt RecoveryReceipt, err error) {
	if err := processor.enter(); err != nil {
		return RecoveryReceipt{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationRecover)
	defer processor.observeOperator(span, OperationRecover, &err)
	if err := request.validate(); err != nil {
		return RecoveryReceipt{}, err
	}
	authority, err := processor.authorizeOperator(ctx, OperatorTarget{
		Action:             OperatorActionRecover,
		Consumer:           request.Consumer,
		IdempotencyKey:     request.IdempotencyKey,
		EventID:            request.EventID,
		EventDigest:        request.EventDigest,
		ExpectedGeneration: request.ExpectedGeneration,
		ExpectedFence:      request.ExpectedFence,
	}, true)
	if err != nil {
		return RecoveryReceipt{}, err
	}
	requestDigest := recoveryRequestDigest(request, authority)
	for attempt := 0; attempt < maximumTransactionRetries; attempt++ {
		receipt = RecoveryReceipt{}
		err = processor.transact(ctx, func(tx pgx.Tx) error {
			existing, found, readErr := processor.readOperatorReceipt(ctx, tx, authority)
			if readErr != nil {
				return readErr
			}
			if found {
				if !sameDigest(existing.requestDigest, requestDigest[:]) {
					return ErrOperatorConflict
				}
				receipt = existing.recoveryReceipt(true)
				return nil
			}

			cursor, row, rowErr := processor.lockCursorThenInbox(
				ctx, tx, request.Consumer, request.EventID,
			)
			if errors.Is(rowErr, pgx.ErrNoRows) {
				return ErrRecoveryNotAllowed
			}
			if rowErr != nil {
				return rowErr
			}
			if !sameDigest(row.EventDigest, request.EventDigest[:]) {
				return ErrEventConflict
			}
			if row.LeaseGeneration != request.ExpectedGeneration ||
				row.LeaseFence != request.ExpectedFence {
				return ErrStaleClaim
			}
			directive, action := recoveryDecision(row, cursor)
			switch action {
			case operatorReceiptRejoin:
				tag, updateErr := tx.Exec(ctx, processor.queries.inboxRecoverRejoin, pgx.StrictNamedArgs{
					"consumer_name":       request.Consumer.Name,
					"consumer_scope":      request.Consumer.Scope,
					"event_id":            request.EventID,
					"event_digest":        request.EventDigest[:],
					"expected_generation": request.ExpectedGeneration,
					"expected_fence":      request.ExpectedFence,
				})
				if updateErr != nil {
					return wrapSafe(errorTextDatabaseOperation, updateErr)
				}
				if tag.RowsAffected() != 1 {
					return ErrStaleClaim
				}
			case operatorReceiptTerminalize:
				tag, updateErr := tx.Exec(ctx, processor.queries.inboxExpireToDeadLetter, pgx.StrictNamedArgs{
					"consumer_name":  request.Consumer.Name,
					"consumer_scope": request.Consumer.Scope,
					"event_id":       request.EventID,
					"event_digest":   request.EventDigest[:],
					"error_code":     errorCodeRetryExhausted,
				})
				if updateErr != nil {
					return wrapSafe(errorTextDatabaseOperation, updateErr)
				}
				if tag.RowsAffected() != 1 {
					return ErrStaleClaim
				}
			}
			stored, insertErr := processor.insertOperatorReceipt(
				ctx, tx, authority, operatorReceiptInput{
					Consumer:           request.Consumer,
					RequestDigest:      requestDigest,
					EventID:            request.EventID,
					EventDigest:        request.EventDigest,
					ExpectedGeneration: request.ExpectedGeneration,
					ExpectedFence:      request.ExpectedFence,
					Action:             action,
					Reason:             request.Reason,
					EvidenceDigest:     request.EvidenceDigest,
					Directive:          directive,
				},
			)
			if insertErr != nil {
				return insertErr
			}
			receipt = stored.recoveryReceipt(false)
			return nil
		})
		if err == nil {
			return receipt, nil
		}
		if (!isRetryableTransactionError(err) && !isUniqueViolation(err)) || ctx.Err() != nil {
			return RecoveryReceipt{}, err
		}
	}
	return RecoveryReceipt{}, err
}

func recoveryDecision(row inboxRow, cursor cursorRow) (RecoveryDirective, string) {
	if row.State == stateCompleted || row.State == stateStale {
		return RecoveryACKEligible, operatorReceiptWait
	}
	if row.State == stateDeadLetter {
		return RecoveryRepairRequired, operatorReceiptWait
	}
	if hasSequenceGap(row.EventSequence, cursor.LastSequence) {
		return RecoveryWaitPredecessor, operatorReceiptWait
	}
	if row.State == stateProcessing && row.LeaseActive {
		return RecoveryWaitLease, operatorReceiptWait
	}
	if !row.AvailableNow {
		return RecoveryWaitBackoff, operatorReceiptWait
	}
	if row.Attempts >= row.MaxAttempts {
		return RecoveryRepairRequired, operatorReceiptTerminalize
	}
	return RecoveryReplayRequired, operatorReceiptRejoin
}
