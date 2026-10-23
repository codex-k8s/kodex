package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"
)

// deliveryOutcomeRow holds exact durable inbox and cursor evidence before it is classified.
// deliveryOutcomeRow хранит точные устойчивые данные inbox и курсора до классификации.
type deliveryOutcomeRow struct {
	EventDigest    []byte
	State          string
	EventSequence  uint64
	CursorSequence uint64
	Attempts       uint32
	MaxAttempts    uint32
	AvailableNow   bool
	LeaseActive    bool
}

// ReadDeliveryOutcome authorizes and reads durable evidence for one exact event and digest.
// ReadDeliveryOutcome авторизует и читает устойчивые данные одного точного события и дайджеста.
func (processor *Processor) ReadDeliveryOutcome(
	ctx context.Context,
	request DeliveryOutcomeRequest,
) (decision DeliveryDecision, err error) {
	if err := processor.enter(); err != nil {
		return DeliveryDecision{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationDelivery)
	defer processor.observeOperator(span, OperationDelivery, &err)
	if err := request.validate(); err != nil {
		return DeliveryDecision{}, err
	}
	if _, err := processor.authorizeOperator(ctx, OperatorTarget{
		Action:      OperatorActionDeliveryOutcome,
		Consumer:    request.Consumer,
		EventID:     request.EventID,
		EventDigest: request.EventDigest,
	}, false); err != nil {
		return DeliveryDecision{}, err
	}

	var row deliveryOutcomeRow
	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, processor.queries.deliveryReadOutcome, pgx.StrictNamedArgs{
			"consumer_name":  request.Consumer.Name,
			"consumer_scope": request.Consumer.Scope,
			"event_id":       request.EventID,
		}).Scan(
			&row.EventDigest,
			&row.State,
			&row.EventSequence,
			&row.CursorSequence,
			&row.Attempts,
			&row.MaxAttempts,
			&row.AvailableNow,
			&row.LeaseActive,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryDecision{}, ErrDeliveryOutcomeNotFound
	}
	if err != nil {
		return DeliveryDecision{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	return classifyDeliveryOutcome(request.EventDigest, row)
}

// classifyDeliveryOutcome chooses a broker action only after validating row shape and event digest.
// classifyDeliveryOutcome выбирает действие брокера только после проверки строки и дайджеста события.
func classifyDeliveryOutcome(
	expectedDigest [sha256.Size]byte,
	row deliveryOutcomeRow,
) (DeliveryDecision, error) {
	if !validDeliveryOutcomeRow(row) {
		return DeliveryDecision{}, ErrSchemaMismatch
	}
	if !sameDigest(expectedDigest[:], row.EventDigest) {
		return DeliveryDecision{}, ErrEventConflict
	}
	decision := DeliveryDecision{
		State:   DeliveryState(row.State),
		Action:  BrokerActionNACKRetry,
		Durable: true,
	}
	switch row.State {
	case stateCompleted, stateStale:
		if row.EventSequence > row.CursorSequence {
			return DeliveryDecision{}, ErrSchemaMismatch
		}
		decision.Directive = RecoveryACKEligible
		decision.Action = BrokerActionACK
	case stateDeadLetter:
		if row.EventSequence <= row.CursorSequence {
			return DeliveryDecision{}, ErrSchemaMismatch
		}
		decision.Directive = RecoveryRepairRequired
		decision.Action = BrokerActionNACKTerminal
	case stateReceived, stateProcessing, stateRetry:
		if row.EventSequence <= row.CursorSequence {
			return DeliveryDecision{}, ErrSchemaMismatch
		}
		switch {
		case hasSequenceGap(row.EventSequence, row.CursorSequence):
			decision.Directive = RecoveryWaitPredecessor
		case row.State == stateProcessing && row.LeaseActive:
			decision.Directive = RecoveryWaitLease
		case !row.AvailableNow:
			decision.Directive = RecoveryWaitBackoff
		case row.Attempts >= row.MaxAttempts:
			decision.Directive = RecoveryRepairRequired
			decision.Action = BrokerActionNACKTerminal
		default:
			decision.Directive = RecoveryReplayRequired
		}
	default:
		return DeliveryDecision{}, ErrSchemaMismatch
	}
	return decision, nil
}

// validDeliveryOutcomeRow enforces the bounded inbox state, sequence, and attempt domain.
// validDeliveryOutcomeRow проверяет ограниченные состояния inbox, номера и число попыток.
func validDeliveryOutcomeRow(row deliveryOutcomeRow) bool {
	return len(row.EventDigest) == sha256.Size &&
		row.EventSequence > 0 && row.MaxAttempts > 0 &&
		row.MaxAttempts <= 100 && row.Attempts <= row.MaxAttempts &&
		(row.State == stateReceived || row.State == stateProcessing ||
			row.State == stateRetry || row.State == stateCompleted ||
			row.State == stateStale || row.State == stateDeadLetter)
}

// hasSequenceGap detects a missing predecessor without overflowing the unsigned sequence range.
// hasSequenceGap находит отсутствующего предшественника без переполнения беззнакового номера.
func hasSequenceGap(sequence, cursor uint64) bool {
	return sequence > cursor && sequence-cursor > 1
}
