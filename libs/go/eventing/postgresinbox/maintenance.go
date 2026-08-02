package postgresinbox

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Cleanup удаляет bounded batch только безопасных completed/stale rows.
func (processor *Processor) Cleanup(ctx context.Context) (deleted int, err error) {
	if err := processor.enter(); err != nil {
		return 0, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationCleanup)
	outcome := OutcomeError
	defer func() {
		if err == nil {
			outcome = OutcomeCleaned
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		}
		span.End(outcome)
		processor.observer.Observe(OperationCleanup, outcome)
	}()
	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(
			ctx,
			processor.queries.inboxCleanup,
			pgx.StrictNamedArgs{
				"retention_seconds": processor.config.RetentionHorizon.Seconds(),
				"batch_size":        processor.config.CleanupBatchSize,
			},
		).Scan(&deleted)
		if scanErr != nil {
			return wrapSafe(errorTextDatabaseOperation, scanErr)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// Repair выполняет bounded audited REQUEUE exact dead-letter predecessor.
func (processor *Processor) Repair(
	ctx context.Context,
	request RepairRequest,
) (receipt RepairReceipt, err error) {
	if err := processor.enter(); err != nil {
		return RepairReceipt{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationRepair)
	outcome := OutcomeError
	defer func() {
		if err == nil {
			outcome = OutcomeRepaired
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		} else if errors.Is(err, ErrRepairConflict) ||
			errors.Is(err, ErrEventConflict) ||
			errors.Is(err, ErrStaleClaim) {
			outcome = OutcomeConflict
		}
		span.End(outcome)
		processor.observer.Observe(OperationRepair, outcome)
	}()
	if err := request.validate(); err != nil {
		return RepairReceipt{}, err
	}
	authority, authorizationErr := processor.repairAuthorizer.AuthorizeRepair(
		ctx,
		RepairTarget{
			Consumer:           request.Consumer,
			EventID:            request.EventID,
			EventDigest:        request.EventDigest,
			ExpectedGeneration: request.ExpectedGeneration,
			ExpectedFence:      request.ExpectedFence,
		},
	)
	if authorizationErr != nil || len(authority.Actor) == 0 ||
		len(authority.Actor) > maximumActorLength ||
		strings.TrimSpace(authority.Actor) != authority.Actor {
		return RepairReceipt{}, wrapSafe(
			errorTextRepairNotAllowed,
			errors.Join(ErrRepairNotAllowed, authorizationErr),
		)
	}
	requestDigest := repairRequestDigest(request, authority.Actor)
	for attempt := 0; attempt < maximumTransactionRetries; attempt++ {
		receipt = RepairReceipt{}
		err = processor.transact(ctx, func(tx pgx.Tx) error {
			existing, found, readErr := processor.readRepairReceipt(
				ctx,
				tx,
				request,
			)
			if readErr != nil {
				return readErr
			}
			if found {
				if !sameDigest(existing.requestDigest, requestDigest[:]) {
					return ErrRepairConflict
				}
				receipt = existing.receipt
				receipt.AlreadyRepaired = true
				return nil
			}

			preRead, preReadErr := processor.readInboxByEvent(
				ctx,
				tx,
				request.Consumer,
				request.EventID,
			)
			if errors.Is(preReadErr, pgx.ErrNoRows) {
				return ErrRepairNotAllowed
			}
			if preReadErr != nil {
				return wrapSafe(errorTextDatabaseOperation, preReadErr)
			}
			cursor, lockErr := processor.ensureAndLockCursor(
				ctx,
				tx,
				request.Consumer,
				preRead.OrderingKey,
			)
			if lockErr != nil {
				return lockErr
			}
			row, rowErr := processor.getInboxByEvent(
				ctx,
				tx,
				request.Consumer,
				request.EventID,
			)
			if rowErr != nil {
				return wrapSafe(errorTextDatabaseOperation, rowErr)
			}
			if !sameDigest(row.EventDigest, request.EventDigest[:]) {
				return ErrEventConflict
			}
			if row.LeaseGeneration != request.ExpectedGeneration ||
				row.LeaseFence != request.ExpectedFence {
				return ErrStaleClaim
			}
			if row.State != stateDeadLetter ||
				row.EventSequence != cursor.LastSequence+1 ||
				row.RepairCount >= row.MaxRepairs {
				return ErrRepairNotAllowed
			}

			tag, requeueErr := tx.Exec(
				ctx,
				processor.queries.inboxRequeue,
				pgx.StrictNamedArgs{
					"consumer_name":       request.Consumer.Name,
					"consumer_scope":      request.Consumer.Scope,
					"event_id":            request.EventID,
					"event_digest":        request.EventDigest[:],
					"event_sequence":      row.EventSequence,
					"expected_generation": request.ExpectedGeneration,
					"expected_fence":      request.ExpectedFence,
				},
			)
			if requeueErr != nil {
				return wrapSafe(errorTextDatabaseOperation, requeueErr)
			}
			if tag.RowsAffected() != 1 {
				return ErrStaleClaim
			}

			repairID := uuid.NewString()
			var createdAt time.Time
			insertErr := tx.QueryRow(
				ctx,
				processor.queries.repairInsert,
				pgx.StrictNamedArgs{
					"consumer_name":       request.Consumer.Name,
					"consumer_scope":      request.Consumer.Scope,
					"idempotency_key":     request.IdempotencyKey,
					"request_digest":      requestDigest[:],
					"repair_id":           repairID,
					"event_id":            request.EventID,
					"event_digest":        request.EventDigest[:],
					"expected_generation": request.ExpectedGeneration,
					"expected_fence":      request.ExpectedFence,
					"actor":               authority.Actor,
					"reason":              request.Reason,
					"evidence_digest":     request.EvidenceDigest[:],
					"result_generation":   request.ExpectedGeneration,
					"result_fence":        request.ExpectedFence,
				},
			).Scan(&createdAt)
			if insertErr != nil {
				return wrapSafe(errorTextDatabaseOperation, insertErr)
			}
			receipt = RepairReceipt{
				RepairID:    repairID,
				EventID:     request.EventID,
				EventDigest: request.EventDigest,
				Generation:  request.ExpectedGeneration,
				Fence:       request.ExpectedFence,
				CreatedAt:   createdAt,
			}
			return nil
		})
		if err == nil {
			return receipt, nil
		}
		receipt = RepairReceipt{}
		if (!isRetryableTransactionError(err) && !isUniqueViolation(err)) ||
			ctx.Err() != nil {
			return RepairReceipt{}, err
		}
	}
	return RepairReceipt{}, err
}

type storedRepairReceipt struct {
	requestDigest []byte
	receipt       RepairReceipt
}

func (processor *Processor) readRepairReceipt(
	ctx context.Context,
	tx pgx.Tx,
	request RepairRequest,
) (storedRepairReceipt, bool, error) {
	var stored storedRepairReceipt
	var eventDigest []byte
	var expectedGeneration uint64
	var expectedFence uint64
	err := tx.QueryRow(
		ctx,
		processor.queries.repairGetByIdempotency,
		pgx.StrictNamedArgs{
			"consumer_name":   request.Consumer.Name,
			"consumer_scope":  request.Consumer.Scope,
			"idempotency_key": request.IdempotencyKey,
		},
	).Scan(
		&stored.requestDigest,
		&stored.receipt.RepairID,
		&stored.receipt.EventID,
		&eventDigest,
		&expectedGeneration,
		&expectedFence,
		&stored.receipt.Generation,
		&stored.receipt.Fence,
		&stored.receipt.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedRepairReceipt{}, false, nil
	}
	if err != nil {
		return storedRepairReceipt{}, false, wrapSafe(errorTextDatabaseOperation, err)
	}
	if !sameDigest(eventDigest, request.EventDigest[:]) ||
		expectedGeneration != request.ExpectedGeneration ||
		expectedFence != request.ExpectedFence || len(eventDigest) != len(stored.receipt.EventDigest) {
		return storedRepairReceipt{}, true, ErrRepairConflict
	}
	copy(stored.receipt.EventDigest[:], eventDigest)
	return stored, true, nil
}
