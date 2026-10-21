package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Cleanup deletes one bounded batch of expired completed or stale rows under processor admission.
// Cleanup удаляет одну ограниченную порцию завершённых или устаревших строк после срока хранения.
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
		return tx.QueryRow(
			ctx,
			processor.queries.inboxCleanup,
			pgx.StrictNamedArgs{
				"retention_seconds": processor.config.RetentionHorizon.Seconds(),
				"batch_size":        processor.config.CleanupBatchSize,
			},
		).Scan(&deleted)
	})
	if err != nil {
		return 0, wrapSafe(errorTextDatabaseOperation, err)
	}
	return deleted, nil
}

// Repair requeues one exact dead-letter predecessor after owner authorization, CAS, and durable receipt checks.
// Repair повторно запускает одного точного dead-letter предшественника после авторизации, CAS и проверки устойчивой квитанции.
func (processor *Processor) Repair(
	ctx context.Context,
	request RepairRequest,
) (receipt RepairReceipt, err error) {
	if err := processor.enter(); err != nil {
		return RepairReceipt{}, err
	}
	defer processor.leave()
	ctx, span := processor.tracer.Start(ctx, OperationRepair)
	defer processor.observeOperator(span, OperationRepair, &err)
	if err := request.validate(); err != nil {
		return RepairReceipt{}, err
	}
	authority, err := processor.authorizeOperator(ctx, OperatorTarget{
		Action:             OperatorActionRepair,
		Consumer:           request.Consumer,
		IdempotencyKey:     request.IdempotencyKey,
		EventID:            request.EventID,
		EventDigest:        request.EventDigest,
		ExpectedGeneration: request.ExpectedGeneration,
		ExpectedFence:      request.ExpectedFence,
	}, true)
	if err != nil {
		return RepairReceipt{}, err
	}
	requestDigest := repairRequestDigest(request, authority)
	for attempt := 0; attempt < maximumTransactionRetries; attempt++ {
		receipt = RepairReceipt{}
		err = processor.transact(ctx, func(tx pgx.Tx) error {
			existing, found, readErr := processor.readOperatorReceipt(ctx, tx, authority)
			if readErr != nil {
				return readErr
			}
			if found {
				if !sameDigest(existing.requestDigest, requestDigest[:]) ||
					existing.action != operatorReceiptRequeue ||
					existing.directive != RecoveryReplayRequired {
					return ErrOperatorConflict
				}
				receipt = existing.repairReceipt(true)
				return nil
			}

			cursor, row, rowErr := processor.lockCursorThenInbox(
				ctx, tx, request.Consumer, request.EventID,
			)
			if errors.Is(rowErr, pgx.ErrNoRows) {
				return ErrRepairNotAllowed
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
			if row.State != stateDeadLetter ||
				row.EventSequence != cursor.LastSequence+1 ||
				row.RepairCount >= row.MaxRepairs {
				return ErrRepairNotAllowed
			}
			tag, requeueErr := tx.Exec(ctx, processor.queries.inboxRequeue, pgx.StrictNamedArgs{
				"consumer_name":       request.Consumer.Name,
				"consumer_scope":      request.Consumer.Scope,
				"event_id":            request.EventID,
				"event_digest":        request.EventDigest[:],
				"event_sequence":      row.EventSequence,
				"expected_generation": request.ExpectedGeneration,
				"expected_fence":      request.ExpectedFence,
			})
			if requeueErr != nil {
				return wrapSafe(errorTextDatabaseOperation, requeueErr)
			}
			if tag.RowsAffected() != 1 {
				return ErrStaleClaim
			}
			stored, insertErr := processor.insertOperatorReceipt(
				ctx, tx, authority, operatorReceiptInput{
					Consumer:           request.Consumer,
					RequestDigest:      requestDigest,
					EventID:            request.EventID,
					EventDigest:        request.EventDigest,
					ExpectedGeneration: request.ExpectedGeneration,
					ExpectedFence:      request.ExpectedFence,
					Action:             operatorReceiptRequeue,
					Reason:             request.Reason,
					EvidenceDigest:     request.EvidenceDigest,
					Directive:          RecoveryReplayRequired,
				},
			)
			if insertErr != nil {
				return insertErr
			}
			receipt = stored.repairReceipt(false)
			return nil
		})
		if err == nil {
			return receipt, nil
		}
		if (!isRetryableTransactionError(err) && !isUniqueViolation(err)) || ctx.Err() != nil {
			return RepairReceipt{}, err
		}
	}
	return RepairReceipt{}, err
}

// operatorReceiptInput binds an authorized operator action to immutable event and evidence coordinates.
// operatorReceiptInput связывает разрешённое действие оператора с неизменными координатами события и свидетельства.
type operatorReceiptInput struct {
	Consumer           Consumer
	RequestDigest      [sha256.Size]byte
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
	Action             string
	Reason             string
	EvidenceDigest     [sha256.Size]byte
	Directive          RecoveryDirective
}

// storedOperatorReceipt is the validated durable projection used for exact idempotent replay.
// storedOperatorReceipt — проверенная устойчивая проекция для точного идемпотентного повтора.
type storedOperatorReceipt struct {
	requestDigest []byte
	receiptID     string
	eventID       string
	eventDigest   [sha256.Size]byte
	generation    uint64
	fence         uint64
	action        string
	directive     RecoveryDirective
	createdAt     time.Time
}

const (
	// operatorReceiptRequeue records an authorized dead-letter transition back to replay.
	// operatorReceiptRequeue фиксирует разрешённый перевод dead-letter обратно к повтору.
	operatorReceiptRequeue = "REQUEUE"
	// operatorReceiptRejoin records a recovery path that rejoins broker redelivery.
	// operatorReceiptRejoin фиксирует восстановление с возвратом к доставке брокера.
	operatorReceiptRejoin = "REJOIN"
	// operatorReceiptTerminalize records a terminal recovery outcome without implicit ACK.
	// operatorReceiptTerminalize фиксирует завершённый исход восстановления без неявного ACK.
	operatorReceiptTerminalize = "TERMINALIZE"
	// operatorReceiptWait records a recovery decision to defer the next action.
	// operatorReceiptWait фиксирует решение восстановления отложить следующее действие.
	operatorReceiptWait = "WAIT"
)

// repairReceipt projects a stored receipt while marking whether this request was an exact replay.
// repairReceipt отображает сохранённую квитанцию с признаком точного повтора запроса.
func (stored storedOperatorReceipt) repairReceipt(repeated bool) RepairReceipt {
	return RepairReceipt{
		RepairID:        stored.receiptID,
		EventID:         stored.eventID,
		EventDigest:     stored.eventDigest,
		Generation:      stored.generation,
		Fence:           stored.fence,
		CreatedAt:       stored.createdAt,
		AlreadyRepaired: repeated,
	}
}

// recoveryReceipt projects a stored directive while marking exact idempotent replay.
// recoveryReceipt отображает сохранённую директиву с признаком точного идемпотентного повтора.
func (stored storedOperatorReceipt) recoveryReceipt(repeated bool) RecoveryReceipt {
	return RecoveryReceipt{
		RecoveryID:      stored.receiptID,
		EventID:         stored.eventID,
		EventDigest:     stored.eventDigest,
		Generation:      stored.generation,
		Fence:           stored.fence,
		Directive:       stored.directive,
		CreatedAt:       stored.createdAt,
		AlreadyRecorded: repeated,
	}
}

// readOperatorReceipt reads one server-scoped receipt and rejects malformed persisted evidence.
// readOperatorReceipt читает одну квитанцию в серверной области и отклоняет повреждённые устойчивые данные.
func (processor *Processor) readOperatorReceipt(
	ctx context.Context,
	tx pgx.Tx,
	authority OperatorAuthority,
) (storedOperatorReceipt, bool, error) {
	var stored storedOperatorReceipt
	var eventDigest []byte
	var expectedGeneration uint64
	var expectedFence uint64
	var directive string
	err := tx.QueryRow(ctx, processor.queries.operatorGetReceipt, authorityArguments(authority)).Scan(
		&stored.requestDigest,
		&stored.receiptID,
		&stored.eventID,
		&eventDigest,
		&expectedGeneration,
		&expectedFence,
		&stored.action,
		&stored.generation,
		&stored.fence,
		&directive,
		&stored.createdAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedOperatorReceipt{}, false, nil
	}
	if err != nil {
		return storedOperatorReceipt{}, false, wrapSafe(errorTextDatabaseOperation, err)
	}
	if len(eventDigest) != sha256.Size || !canonicalUUID(stored.eventID) ||
		expectedGeneration != stored.generation || expectedFence != stored.fence ||
		!validOperatorReceiptAction(stored.action) ||
		!validRecoveryDirective(directive) {
		return storedOperatorReceipt{}, true, ErrSchemaMismatch
	}
	copy(stored.eventDigest[:], eventDigest)
	stored.directive = RecoveryDirective(directive)
	return stored, true, nil
}

// validOperatorReceiptAction accepts only the four persisted operator transition codes.
// validOperatorReceiptAction допускает только четыре сохраняемых кода перехода оператора.
func validOperatorReceiptAction(action string) bool {
	return action == operatorReceiptRequeue || action == operatorReceiptRejoin ||
		action == operatorReceiptTerminalize || action == operatorReceiptWait
}

// validRecoveryDirective accepts only closed recovery decisions exposed to the broker adapter.
// validRecoveryDirective допускает только закрытые решения восстановления для адаптера брокера.
func validRecoveryDirective(directive string) bool {
	return directive == string(RecoveryReplayRequired) ||
		directive == string(RecoveryWaitPredecessor) ||
		directive == string(RecoveryWaitLease) ||
		directive == string(RecoveryWaitBackoff) ||
		directive == string(RecoveryRepairRequired) ||
		directive == string(RecoveryACKEligible)
}

// insertOperatorReceipt writes the exact authorized action and evidence within the caller transaction.
// insertOperatorReceipt записывает точное разрешённое действие и свидетельство в транзакции вызывающего.
func (processor *Processor) insertOperatorReceipt(
	ctx context.Context,
	tx pgx.Tx,
	authority OperatorAuthority,
	input operatorReceiptInput,
) (storedOperatorReceipt, error) {
	stored := storedOperatorReceipt{
		requestDigest: input.RequestDigest[:],
		receiptID:     uuid.NewString(),
		eventID:       input.EventID,
		eventDigest:   input.EventDigest,
		generation:    input.ExpectedGeneration,
		fence:         input.ExpectedFence,
		action:        input.Action,
		directive:     input.Directive,
	}
	arguments := authorityArguments(authority)
	arguments["consumer_name"] = input.Consumer.Name
	arguments["consumer_scope"] = input.Consumer.Scope
	arguments["request_digest"] = input.RequestDigest[:]
	arguments["receipt_id"] = stored.receiptID
	arguments["event_id"] = input.EventID
	arguments["event_digest"] = input.EventDigest[:]
	arguments["expected_generation"] = input.ExpectedGeneration
	arguments["expected_fence"] = input.ExpectedFence
	arguments["action"] = input.Action
	arguments["actor"] = authority.Actor
	arguments["reason"] = input.Reason
	arguments["evidence_digest"] = input.EvidenceDigest[:]
	arguments["result_generation"] = input.ExpectedGeneration
	arguments["result_fence"] = input.ExpectedFence
	arguments["result_directive"] = input.Directive
	if err := tx.QueryRow(ctx, processor.queries.operatorInsertReceipt, arguments).Scan(
		&stored.createdAt,
	); err != nil {
		return storedOperatorReceipt{}, wrapSafe(errorTextDatabaseOperation, err)
	}
	return stored, nil
}

// authorityArguments binds server-assigned organization, project, operation, and key hash to SQL.
// authorityArguments привязывает назначенные сервером организацию, проект, операцию и хеш ключа к SQL.
func authorityArguments(authority OperatorAuthority) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"organization_scope": authority.Organization,
		"project_scope":      authority.Project,
		"operation":          authority.Operation,
		"key_hash":           authority.KeyHash[:],
	}
}

// authorizeOperator validates trusted actor identity and requires full durable scope for mutation.
// authorizeOperator проверяет доверенного actor и требует полную устойчивую область для изменения.
func (processor *Processor) authorizeOperator(
	ctx context.Context,
	target OperatorTarget,
	mutation bool,
) (OperatorAuthority, error) {
	authority, err := processor.operatorAuthorizer.AuthorizeOperator(ctx, target)
	invalid := err != nil || len(authority.Actor) == 0 ||
		len(authority.Actor) > maximumActorLength ||
		strings.TrimSpace(authority.Actor) != authority.Actor
	if mutation {
		invalid = invalid || len(authority.Organization) == 0 ||
			len(authority.Organization) > maximumConsumerLength ||
			len(authority.Project) == 0 || len(authority.Project) > maximumConsumerLength ||
			len(authority.Operation) == 0 || len(authority.Operation) > maximumConsumerLength ||
			!consumerScopePattern.MatchString(authority.Organization) ||
			!consumerScopePattern.MatchString(authority.Project) ||
			!consumerNamePattern.MatchString(authority.Operation) ||
			authority.KeyHash == ([sha256.Size]byte{})
	}
	if invalid {
		return OperatorAuthority{}, wrapSafe(
			errorTextOperatorNotAllowed,
			errors.Join(ErrOperatorNotAllowed, err),
		)
	}
	return authority, nil
}

// observeOperator maps an authorized operation's error to closed trace and metric outcomes.
// observeOperator сопоставляет ошибку авторизованной операции закрытым исходам трассировки и метрик.
func (processor *Processor) observeOperator(span Span, operation Operation, err *error) {
	outcome := OutcomeError
	if *err == nil {
		if operation == OperationRepair {
			outcome = OutcomeRepaired
		} else if operation == OperationRecover {
			outcome = OutcomeRecovered
		} else {
			outcome = OutcomeListed
		}
	} else if isContextDone(*err) {
		outcome = OutcomeCanceled
	} else if errors.Is(*err, ErrOperatorConflict) ||
		errors.Is(*err, ErrEventConflict) || errors.Is(*err, ErrStaleClaim) {
		outcome = OutcomeConflict
	}
	span.End(outcome)
	processor.observer.Observe(operation, outcome)
}
