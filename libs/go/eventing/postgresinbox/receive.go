package postgresinbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const (
	stateReceived   = "RECEIVED"
	stateProcessing = "PROCESSING"
	stateRetry      = "RETRY"
	stateCompleted  = "COMPLETED"
	stateStale      = "STALE"
	stateDeadLetter = "DEAD_LETTER"
)

type receiveDecision struct {
	result    Result
	claimable bool
}

func (processor *Processor) receive(
	ctx context.Context,
	consumer Consumer,
	record eventRecord,
) (decision receiveDecision, err error) {
	var decisionErr error
	operation := func(tx pgx.Tx) error {
		cursor, lockErr := processor.ensureAndLockCursor(
			ctx,
			tx,
			consumer,
			record.OrderingKey,
		)
		if lockErr != nil {
			return lockErr
		}

		existing, getErr := processor.getInboxByEvent(
			ctx,
			tx,
			consumer,
			record.Envelope.EventID,
		)
		if getErr == nil {
			decision, decisionErr = classifyExisting(record, cursor, existing)
			return nil
		}
		if !errors.Is(getErr, pgx.ErrNoRows) {
			return wrapSafe(errorTextDatabaseOperation, getErr)
		}

		sequenceRow, sequenceErr := processor.getInboxBySequence(
			ctx,
			tx,
			consumer,
			record.OrderingKey,
			record.Envelope.EventSequence,
		)
		if sequenceErr == nil {
			_ = sequenceRow
			decision = receiveDecision{
				result: Result{
					Outcome: OutcomeConflict,
					Action:  BrokerActionNACKTerminal,
					Durable: true,
				},
			}
			decisionErr = ErrEventConflict
			return nil
		}
		if !errors.Is(sequenceErr, pgx.ErrNoRows) {
			return wrapSafe(errorTextDatabaseOperation, sequenceErr)
		}

		if record.Envelope.EventSequence <= cursor.LastSequence {
			var storedOrderingKey string
			insertErr := tx.QueryRow(
				ctx,
				processor.queries.inboxInsertStale,
				processor.eventArguments(consumer, record, true),
			).Scan(&storedOrderingKey)
			if insertErr != nil {
				return wrapSafe(errorTextDatabaseOperation, insertErr)
			}
			if !sameOrderingKey(record.OrderingKey, storedOrderingKey) {
				return ErrEventConflict
			}
			decision = receiveDecision{result: Result{
				Outcome: OutcomeStale,
				Action:  BrokerActionACK,
				Durable: true,
			}}
			return nil
		}

		var storedOrderingKey string
		receivedArguments := processor.eventArguments(consumer, record, false)
		receivedArguments["error_code"] = nil
		if record.Envelope.EventSequence > cursor.LastSequence+1 {
			receivedArguments["error_code"] = errorCodeSequenceGap
		}
		insertErr := tx.QueryRow(
			ctx,
			processor.queries.inboxInsertReceived,
			receivedArguments,
		).Scan(&storedOrderingKey)
		if insertErr != nil {
			return wrapSafe(errorTextDatabaseOperation, insertErr)
		}
		if !sameOrderingKey(record.OrderingKey, storedOrderingKey) {
			return ErrEventConflict
		}
		if record.Envelope.EventSequence > cursor.LastSequence+1 {
			decision = receiveDecision{result: Result{
				Outcome: OutcomeGap,
				Action:  BrokerActionNACKRetry,
				Durable: true,
			}}
			decisionErr = ErrSequenceGap
			return nil
		}
		decision = receiveDecision{claimable: true}
		return nil
	}
	for attempt := 0; attempt < maximumTransactionRetries; attempt++ {
		decision = receiveDecision{}
		decisionErr = nil
		err = processor.transact(ctx, operation)
		if err == nil {
			return decision, decisionErr
		}
		if (!isRetryableTransactionError(err) && !isUniqueViolation(err)) ||
			ctx.Err() != nil {
			return receiveDecision{}, err
		}
	}
	return receiveDecision{}, err
}

func classifyExisting(
	record eventRecord,
	cursor cursorRow,
	existing inboxRow,
) (receiveDecision, error) {
	if !sameDigest(record.Digest[:], existing.EventDigest) ||
		!sameOrderingKey(record.OrderingKey, existing.OrderingKey) ||
		record.Envelope.EventSequence != existing.EventSequence {
		return receiveDecision{result: Result{
			Outcome: OutcomeConflict,
			Action:  BrokerActionNACKTerminal,
			Durable: true,
		}}, ErrEventConflict
	}
	switch existing.State {
	case stateCompleted:
		return receiveDecision{result: Result{
			Outcome: OutcomeDuplicate,
			Action:  BrokerActionACK,
			Durable: true,
		}}, nil
	case stateStale:
		return receiveDecision{result: Result{
			Outcome: OutcomeStale,
			Action:  BrokerActionACK,
			Durable: true,
		}}, nil
	case stateDeadLetter:
		return receiveDecision{result: Result{
			Outcome: OutcomeDeadLetter,
			Action:  BrokerActionNACKTerminal,
			Durable: true,
		}}, ErrRetryExhausted
	case stateProcessing:
		if existing.LeaseActive {
			return receiveDecision{result: Result{
				Outcome: OutcomeBusy,
				Action:  BrokerActionNACKRetry,
				Durable: true,
			}}, nil
		}
	case stateRetry:
		if !existing.AvailableNow {
			return receiveDecision{result: Result{
				Outcome: OutcomeRetry,
				Action:  BrokerActionNACKRetry,
				Durable: true,
			}}, nil
		}
	case stateReceived:
	default:
		return receiveDecision{}, ErrSchemaMismatch
	}
	if existing.EventSequence > cursor.LastSequence+1 {
		return receiveDecision{result: Result{
			Outcome: OutcomeGap,
			Action:  BrokerActionNACKRetry,
			Durable: true,
		}}, ErrSequenceGap
	}
	if existing.EventSequence <= cursor.LastSequence {
		return receiveDecision{}, ErrSchemaMismatch
	}
	return receiveDecision{claimable: true}, nil
}

func (processor *Processor) eventArguments(
	consumer Consumer,
	record eventRecord,
	includeRetention bool,
) pgx.StrictNamedArgs {
	organizationID := any(nil)
	if record.Envelope.OrganizationID != "" {
		organizationID = record.Envelope.OrganizationID
	}
	arguments := pgx.StrictNamedArgs{
		"consumer_name":     consumer.Name,
		"consumer_scope":    consumer.Scope,
		"event_id":          record.Envelope.EventID,
		"event_digest":      record.Digest[:],
		"event_name":        record.Envelope.EventName,
		"event_version":     record.Envelope.EventVersion,
		"schema_version":    record.Envelope.SchemaVersion,
		"occurred_at":       record.Envelope.OccurredAt,
		"organization_id":   organizationID,
		"aggregate_type":    record.Envelope.AggregateType,
		"aggregate_id":      record.Envelope.AggregateID,
		"aggregate_version": record.Envelope.AggregateVersion,
		"event_sequence":    record.Envelope.EventSequence,
		"max_attempts":      processor.config.MaxAttempts,
		"max_repairs":       processor.config.MaxRepairs,
	}
	if includeRetention {
		arguments["retention_seconds"] = processor.config.RetentionHorizon.Seconds()
	}
	return arguments
}
