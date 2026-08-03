package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type claimDecision struct {
	result Result
	claim  *Claim
}

func (processor *Processor) claim(
	ctx context.Context,
	consumer Consumer,
	record eventRecord,
) (decision claimDecision, err error) {
	var decisionErr error
	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		cursor, lockErr := processor.ensureAndLockCursor(
			ctx,
			tx,
			consumer,
			record.OrderingKey,
		)
		if lockErr != nil {
			return lockErr
		}
		row, getErr := processor.getInboxByEvent(
			ctx,
			tx,
			consumer,
			record.Envelope.EventID,
		)
		if getErr != nil {
			return wrapSafe(errorTextDatabaseOperation, getErr)
		}
		if !sameDigest(record.Digest[:], row.EventDigest) ||
			!sameOrderingKey(record.OrderingKey, row.OrderingKey) ||
			record.Envelope.EventSequence != row.EventSequence {
			decision = claimDecision{result: Result{
				Outcome: OutcomeConflict,
				Action:  BrokerActionNACKTerminal,
				Durable: true,
			}}
			decisionErr = ErrEventConflict
			return nil
		}
		if row.State == stateCompleted {
			decision = claimDecision{result: Result{
				Outcome: OutcomeDuplicate,
				Action:  BrokerActionACK,
				Durable: true,
			}}
			return nil
		}
		if row.State == stateStale || row.EventSequence <= cursor.LastSequence {
			decision = claimDecision{result: Result{
				Outcome: OutcomeStale,
				Action:  BrokerActionACK,
				Durable: true,
			}}
			return nil
		}
		if row.State == stateDeadLetter {
			decision = claimDecision{result: Result{
				Outcome: OutcomeDeadLetter,
				Action:  BrokerActionNACKTerminal,
				Durable: true,
			}}
			decisionErr = ErrRetryExhausted
			return nil
		}
		if row.State != stateReceived && row.State != stateRetry &&
			row.State != stateProcessing {
			return ErrSchemaMismatch
		}
		if row.EventSequence > cursor.LastSequence+1 {
			decision = claimDecision{result: Result{
				Outcome: OutcomeGap,
				Action:  BrokerActionNACKRetry,
				Durable: true,
			}}
			decisionErr = ErrSequenceGap
			return nil
		}
		if row.State == stateProcessing && row.LeaseActive {
			decision = claimDecision{result: Result{
				Outcome: OutcomeBusy,
				Action:  BrokerActionNACKRetry,
				Durable: true,
			}}
			return nil
		}
		if !row.AvailableNow {
			decision = claimDecision{result: Result{
				Outcome: OutcomeRetry,
				Action:  BrokerActionNACKRetry,
				Durable: true,
			}}
			return nil
		}
		if row.Attempts >= row.MaxAttempts {
			tag, markErr := tx.Exec(
				ctx,
				processor.queries.inboxExpireToDeadLetter,
				pgx.StrictNamedArgs{
					"consumer_name":  consumer.Name,
					"consumer_scope": consumer.Scope,
					"event_id":       record.Envelope.EventID,
					"event_digest":   record.Digest[:],
					"error_code":     errorCodeRetryExhausted,
				},
			)
			if markErr != nil {
				return wrapSafe(errorTextDatabaseOperation, markErr)
			}
			if tag.RowsAffected() != 1 {
				return ErrStaleClaim
			}
			decision = claimDecision{result: Result{
				Outcome: OutcomeDeadLetter,
				Action:  BrokerActionNACKTerminal,
				Durable: true,
			}}
			decisionErr = ErrRetryExhausted
			return nil
		}

		var fence uint64
		err := tx.QueryRow(
			ctx,
			processor.queries.cursorTakeFence,
			pgx.StrictNamedArgs{
				"consumer_name":  consumer.Name,
				"consumer_scope": consumer.Scope,
				"ordering_key":   record.OrderingKey,
			},
		).Scan(&fence)
		if err != nil {
			return wrapSafe(errorTextDatabaseOperation, err)
		}
		leaseToken := uuid.NewString()
		claimed := Claim{
			Consumer:      consumer,
			EventID:       record.Envelope.EventID,
			EventDigest:   record.Digest,
			OrderingKey:   record.OrderingKey,
			EventSequence: record.Envelope.EventSequence,
			LeaseOwner:    processor.config.InstanceID,
			LeaseToken:    leaseToken,
			LeaseFence:    fence,
		}
		err = tx.QueryRow(
			ctx,
			processor.queries.inboxClaim,
			pgx.StrictNamedArgs{
				"consumer_name":  consumer.Name,
				"consumer_scope": consumer.Scope,
				"event_id":       record.Envelope.EventID,
				"lease_owner":    processor.config.InstanceID,
				"lease_token":    leaseToken,
				"lease_fence":    fence,
				"lease_seconds":  processor.config.LeaseDuration.Seconds(),
			},
		).Scan(
			&claimed.LeaseToken,
			&claimed.LeaseGeneration,
			&claimed.LeaseFence,
			&claimed.LeaseExpiresAt,
			&claimed.Attempts,
			&claimed.MaxAttempts,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrStaleClaim
			}
			return wrapSafe(errorTextDatabaseOperation, err)
		}
		decision = claimDecision{claim: &claimed}
		return nil
	})
	if err != nil {
		return claimDecision{}, err
	}
	return decision, decisionErr
}

// Renew продлевает только действующий exact claim по PostgreSQL time.
func (processor *Processor) Renew(
	ctx context.Context,
	claim Claim,
) (expiresAt time.Time, err error) {
	if err := processor.enter(); err != nil {
		return time.Time{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationRenew)
	outcome := OutcomeError
	defer func() {
		if err == nil {
			outcome = OutcomeRenewed
		} else if isContextDone(err) {
			outcome = OutcomeCanceled
		} else if errors.Is(err, ErrStaleClaim) {
			outcome = OutcomeConflict
		}
		span.End(outcome)
		processor.observer.Observe(OperationRenew, outcome)
	}()
	if err := validateClaim(claim); err != nil {
		return time.Time{}, err
	}
	if claim.LeaseOwner != processor.config.InstanceID {
		return time.Time{}, ErrStaleClaim
	}
	err = processor.retryTransaction(ctx, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(
			ctx,
			processor.queries.inboxRenew,
			claimArguments(claim, processor.config.LeaseDuration),
		).Scan(&expiresAt)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrStaleClaim
		}
		if scanErr != nil {
			return wrapSafe(errorTextDatabaseOperation, scanErr)
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return expiresAt, nil
}

func validateClaim(claim Claim) error {
	if err := claim.Consumer.validate(); err != nil {
		return err
	}
	eventID, eventErr := uuid.Parse(claim.EventID)
	leaseToken, tokenErr := uuid.Parse(claim.LeaseToken)
	if eventErr != nil || eventID == uuid.Nil || eventID.String() != claim.EventID ||
		tokenErr != nil || leaseToken == uuid.Nil || leaseToken.String() != claim.LeaseToken ||
		claim.EventDigest == ([sha256.Size]byte{}) ||
		!validOrderingKey(claim.OrderingKey) ||
		claim.EventSequence == 0 || claim.EventSequence > math.MaxInt64 ||
		claim.LeaseGeneration > math.MaxInt64 || claim.LeaseFence > math.MaxInt64 ||
		claim.LeaseOwner == "" ||
		len(claim.LeaseOwner) > maximumInstanceLength ||
		!instanceIDPattern.MatchString(claim.LeaseOwner) ||
		claim.LeaseToken == "" || claim.LeaseGeneration == 0 ||
		claim.LeaseFence == 0 || claim.Attempts == 0 ||
		claim.MaxAttempts == 0 || claim.MaxAttempts > 100 ||
		claim.Attempts > claim.MaxAttempts {
		return ErrStaleClaim
	}
	return nil
}

func claimArguments(claim Claim, leaseDuration time.Duration) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"consumer_name":    claim.Consumer.Name,
		"consumer_scope":   claim.Consumer.Scope,
		"event_id":         claim.EventID,
		"event_digest":     claim.EventDigest[:],
		"lease_owner":      claim.LeaseOwner,
		"lease_token":      claim.LeaseToken,
		"lease_generation": claim.LeaseGeneration,
		"lease_fence":      claim.LeaseFence,
		"lease_seconds":    leaseDuration.Seconds(),
	}
}
