package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetBlockage возвращает самый ранний blocking predecessor exact event.
func (processor *Processor) GetBlockage(
	ctx context.Context,
	consumer Consumer,
	eventID string,
) (blockage Blockage, err error) {
	if err := processor.enter(); err != nil {
		return Blockage{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationList)
	defer processor.observeOperator(span, OperationList, &err)
	if err := consumer.validate(); err != nil || !canonicalUUID(eventID) {
		return Blockage{}, ErrInvalidBlockageRead
	}
	if _, err := processor.authorizeOperator(ctx, OperatorTarget{
		Action: OperatorActionRead, Consumer: consumer, EventID: eventID,
	}, false); err != nil {
		return Blockage{}, err
	}
	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		return scanBlockage(tx.QueryRow(ctx, processor.queries.blockageGet, pgx.StrictNamedArgs{
			"consumer_name":  consumer.Name,
			"consumer_scope": consumer.Scope,
			"event_id":       eventID,
		}), &blockage)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Blockage{}, ErrBlockageNotFound
	}
	if err != nil && !errors.Is(err, ErrSchemaMismatch) {
		return Blockage{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	return blockage, err
}

// ListBlockages возвращает bounded страницу самых ранних predecessors каждого key.
func (processor *Processor) ListBlockages(
	ctx context.Context,
	consumer Consumer,
	request BlockageListRequest,
) (page BlockagePage, err error) {
	if err := processor.enter(); err != nil {
		return BlockagePage{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationList)
	defer processor.observeOperator(span, OperationList, &err)
	if err := consumer.validate(); err != nil {
		return BlockagePage{}, err
	}
	request, err = request.validate()
	if err != nil {
		if errors.Is(err, ErrSchemaMismatch) {
			return BlockagePage{}, err
		}
		return BlockagePage{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	if _, err := processor.authorizeOperator(ctx, OperatorTarget{
		Action: OperatorActionRead, Consumer: consumer,
	}, false); err != nil {
		return BlockagePage{}, err
	}
	arguments := pgx.StrictNamedArgs{
		"consumer_name":     consumer.Name,
		"consumer_scope":    consumer.Scope,
		"after_received_at": nil,
		"after_event_id":    nil,
		"page_limit":        request.Limit + 1,
	}
	if request.After != nil {
		arguments["after_received_at"] = request.After.ReceivedAt
		arguments["after_event_id"] = request.After.EventID
	}
	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		page.Items = nil
		rows, queryErr := tx.Query(ctx, processor.queries.blockageList, arguments)
		if queryErr != nil {
			return wrapSafe(errorTextDatabaseOperation, queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var item Blockage
			if scanErr := scanBlockage(rows, &item); scanErr != nil {
				return scanErr
			}
			page.Items = append(page.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return BlockagePage{}, err
	}
	if len(page.Items) > request.Limit {
		page.Items = page.Items[:request.Limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &BlockageCursor{ReceivedAt: last.ReceivedAt, EventID: last.EventID}
	}
	return page, nil
}

type blockageScanner interface {
	Scan(...any) error
}

func scanBlockage(scanner blockageScanner, blockage *Blockage) error {
	var eventDigest []byte
	var orderingKey string
	var state string
	var availableNow bool
	var leaseActive bool
	if err := scanner.Scan(
		&blockage.EventID,
		&eventDigest,
		&orderingKey,
		&blockage.EventSequence,
		&blockage.CursorSequence,
		&state,
		&blockage.Attempts,
		&blockage.MaxAttempts,
		&blockage.RepairCount,
		&blockage.MaxRepairs,
		&blockage.LeaseGeneration,
		&blockage.LeaseFence,
		&blockage.AvailableAt,
		&blockage.LeaseExpiresAt,
		&blockage.TerminalAt,
		&blockage.FailureCode,
		&blockage.ReceivedAt,
		&availableNow,
		&leaseActive,
	); err != nil {
		return err
	}
	if !canonicalUUID(blockage.EventID) || len(eventDigest) != sha256.Size ||
		!validOrderingKey(orderingKey) || blockage.EventSequence == 0 ||
		!validBlockageState(state) ||
		(blockage.FailureCode != "" && !errorCodePattern.MatchString(blockage.FailureCode)) {
		return ErrSchemaMismatch
	}
	copy(blockage.EventDigest[:], eventDigest)
	blockage.State = BlockageState(state)
	blockage.OrderingKeyDigest = sha256.Sum256([]byte(orderingKey))
	blockage.Eligibility = blockageEligibility(*blockage, availableNow, leaseActive)
	return nil
}

func blockageEligibility(
	blockage Blockage,
	availableNow bool,
	leaseActive bool,
) BlockageEligibility {
	if blockage.State == BlockageStateDeadLetter {
		return BlockageRepairRequired
	}
	if hasSequenceGap(blockage.EventSequence, blockage.CursorSequence) {
		return BlockageWaitPredecessor
	}
	if blockage.State == BlockageStateProcessing && leaseActive {
		return BlockageWaitLease
	}
	if !availableNow {
		return BlockageWaitBackoff
	}
	return BlockageReplayRequired
}

func validBlockageState(state string) bool {
	return state == stateReceived || state == stateProcessing ||
		state == stateRetry || state == stateDeadLetter
}
