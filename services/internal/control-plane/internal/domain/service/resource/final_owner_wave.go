package resource

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func (service *Service) continueOwnerGateGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	gate entity.Resource,
	gateSpec entity.OwnerGateSpec,
	process entity.Resource,
	processSpec entity.ProcessRunSpec,
	turn entity.Resource,
	turnSpec entity.TurnSpec,
	reason string,
	occurrence domainrepo.ScheduleOccurrence,
	schedule entity.Resource,
	now time.Time,
) (OwnerGateResult, error) {
	open, err := tx.ProcessHasOpenWork(
		ctx, process.OrganizationID, process.ProjectID, process.ID, turn.ID, gate.ID,
	)
	if err != nil {
		return OwnerGateResult{}, err
	}
	if open {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	session, err := tx.GetForUpdate(
		ctx, process.OrganizationID, process.ProjectID, gateSpec.SessionID,
	)
	if err != nil {
		return OwnerGateResult{}, err
	}
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.Kind != enum.KindSession || session.State != enum.StateActive ||
		session.OwnerActorID != process.OwnerActorID ||
		sessionSpec.LastTurnSequence == ^uint64(0) {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	promptArtifact, err := service.requireCleanArtifact(
		ctx, tx, principal, turnSpec.PromptArtifactID,
	)
	if err != nil {
		return OwnerGateResult{}, err
	}
	revision, err := service.createRuntimeRevision(ctx, tx, principal, session, sessionSpec)
	if err != nil {
		return OwnerGateResult{}, err
	}
	revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok {
		return OwnerGateResult{}, errs.ErrInternal
	}
	feedbackSHA256, err := canonicalHash(struct {
		Version      uint32
		GateID       string
		GateVersion  uint64
		ProcessID    string
		TurnID       string
		Attempt      uint32
		ResultSHA256 string
		Reason       string
	}{
		1, gate.ID, gate.Version, process.ID, turn.ID, turnSpec.Attempt,
		gateSpec.ResultSHA256, reason,
	})
	if err != nil {
		return OwnerGateResult{}, errs.ErrInternal
	}
	feedbackContentSHA256 := hashString(reason)
	sourceRef := "owner-gate-continuation:" + gate.ID + ":" + feedbackSHA256
	sessionSpec.LastTurnSequence++
	updatedSession, err := session.Update(session.Name, sessionSpec, now)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updatedSession, session.Version); err != nil {
		return OwnerGateResult{}, err
	}
	continuation, err := entity.New(
		uuid.NewString(), process.OrganizationID, process.ProjectID, session.ID,
		process.OwnerActorID, enum.KindTurn, "Owner continuation "+process.ID,
		entity.TurnSpec{
			SessionID:         session.ID,
			Sequence:          sessionSpec.LastTurnSequence,
			SourceRef:         sourceRef,
			PromptArtifactID:  turnSpec.PromptArtifactID,
			RuntimeRevisionID: revision.ID,
			ProcessRunID:      process.ID,
			Attempt:           1,
			EffectiveInputSHA256: hashRuntimeInput(
				sourceRef, promptArtifact.SHA256, revisionSpec.ManifestSHA256, process.ID,
			),
			PredecessorTurnID:    turn.ID,
			OwnerFeedback:        reason,
			OwnerFeedbackGateID:  gate.ID,
			OwnerFeedbackVersion: gate.Version,
			OwnerFeedbackSHA256:  feedbackContentSHA256,
		},
		now,
	)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := service.validateReferences(ctx, tx, continuation); err != nil {
		return OwnerGateResult{}, err
	}
	if err := tx.Insert(ctx, continuation); err != nil {
		return OwnerGateResult{}, err
	}
	continuationSpec := continuation.Spec.(entity.TurnSpec)
	if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
		TurnID:              continuation.ID,
		Attempt:             continuationSpec.Attempt,
		WorkloadID:          "unassigned",
		AuthorityGeneration: principal.AuthorityGeneration,
		State:               "QUEUED",
		InputSHA256:         continuationSpec.EffectiveInputSHA256,
		LeaseFence:          continuation.Version,
		StartedAt:           now,
	}); err != nil {
		return OwnerGateResult{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, turnSpec.Attempt)
	if err != nil || attempt.State != "WAITING_OWNER" ||
		attempt.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
		attempt.FinishedAt.IsZero() || attempt.Outcome != "owner_gate_pending" {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	turnSpec.Outcome = "owner_gate_changes_requested"
	cancelledTurn, err := turn.ReplaceAndTransition(turnSpec, enum.StateCancelled, now)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, cancelledTurn, turn.Version); err != nil {
		return OwnerGateResult{}, err
	}
	if err := service.revokeExecutionClaimsForOwner(
		ctx, tx, principal, process.OwnerActorID, process.ID, turn.ID,
		"owner_gate_changes_requested", now,
	); err != nil {
		return OwnerGateResult{}, err
	}
	if err := processSpec.SetOwnerGateContinuation(entity.ProcessContinuationBinding{
		TurnID: continuation.ID, TurnVersion: continuation.Version,
		Attempt: continuationSpec.Attempt, RuntimeRevisionID: revision.ID,
		RuntimeRevisionVersion: revision.Version,
		InputSHA256:            continuationSpec.EffectiveInputSHA256,
	}, gate.ID, feedbackContentSHA256); err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	setCurrentExecution(&processSpec, executionTuple{
		SessionID:              session.ID,
		SessionVersion:         updatedSession.Version,
		TurnID:                 continuation.ID,
		TurnVersion:            continuation.Version,
		Attempt:                continuationSpec.Attempt,
		RuntimeRevisionID:      revision.ID,
		RuntimeRevisionVersion: revision.Version,
		InputSHA256:            continuationSpec.EffectiveInputSHA256,
	})
	processSpec.Outcome = ""
	processSpec.ResultArtifactID = ""
	continuedProcess, err := process.ReplaceAndTransition(
		processSpec, enum.StateRunning, now,
	)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, continuedProcess, process.Version); err != nil {
		return OwnerGateResult{}, err
	}
	gateSpec.Decision = "CHANGES_REQUESTED"
	gateSpec.DecisionReason = reason
	gateSpec.DecisionReceiptSHA256 = feedbackSHA256
	gateSpec.ContinuationTurnID = continuation.ID
	gateSpec.ContinuationTurnVersion = continuation.Version
	gateSpec.ContinuationInputSHA256 = continuationSpec.EffectiveInputSHA256
	resolvedGate, err := gate.ReplaceAndTransition(gateSpec, enum.StateSucceeded, now)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, resolvedGate, gate.Version); err != nil {
		return OwnerGateResult{}, err
	}
	if occurrence.ID != "" {
		run, err := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
		if err != nil || validateScheduledRunBinding(occurrence, run) != nil ||
			run.State != "WAITING_OWNER" || schedule.Kind != enum.KindSchedule ||
			schedule.OwnerActorID != process.OwnerActorID {
			return OwnerGateResult{}, errs.ErrStateConflict
		}
		if err := rebindScheduledOccurrence(
			&occurrence,
			"CONTINUATION",
			scheduledOccurrenceExecutionBinding{
				SessionID: session.ID, SessionVersion: updatedSession.Version,
				TurnID: continuation.ID, TurnVersion: continuation.Version,
				ProcessRunID: continuedProcess.ID, ProcessVersion: continuedProcess.Version,
				RuntimeRevisionID:      revision.ID,
				RuntimeRevisionVersion: revision.Version,
				InputSHA256:            continuationSpec.EffectiveInputSHA256,
			},
			"owner_gate_changes_requested",
			now,
		); err != nil {
			return OwnerGateResult{}, err
		}
		if err := tx.UpdateScheduleOccurrence(
			ctx, occurrence, occurrence.Attempt, occurrence.TokenHash,
		); err != nil {
			return OwnerGateResult{}, err
		}
		if err := tx.ContinueScheduledRun(ctx, domainrepo.ScheduledRun{
			OccurrenceID:                       occurrence.ID,
			Attempt:                            occurrence.Attempt,
			ContinuationTurnID:                 continuation.ID,
			ContinuationTurnVersion:            continuation.Version,
			ContinuationRuntimeRevisionID:      revision.ID,
			ContinuationRuntimeRevisionVersion: revision.Version,
			ContinuationInputSHA256:            continuationSpec.EffectiveInputSHA256,
			OwnerFeedbackSHA256:                feedbackContentSHA256,
			CurrentSessionID:                   session.ID,
			CurrentSessionVersion:              updatedSession.Version,
			CurrentTurnID:                      continuation.ID,
			CurrentTurnVersion:                 continuation.Version,
			CurrentTurnAttempt:                 continuationSpec.Attempt,
			CurrentProcessRunID:                continuedProcess.ID,
			CurrentProcessVersion:              continuedProcess.Version,
			CurrentRuntimeRevisionID:           revision.ID,
			CurrentRuntimeRevisionVersion:      revision.Version,
			CurrentInputSHA256:                 continuationSpec.EffectiveInputSHA256,
		}); err != nil {
			return OwnerGateResult{}, err
		}
	}
	for _, record := range []struct {
		action   string
		resource entity.Resource
	}{
		{"owner_gate_continue_session", updatedSession},
		{"owner_gate_terminalize_previous_turn", cancelledTurn},
		{"owner_gate_enqueue_continuation", continuation},
		{"owner_gate_continue_process", continuedProcess},
		{"resolve_owner_gate_changes_requested", resolvedGate},
	} {
		if err := service.appendMutationRecords(
			ctx, tx, principal, record.action, record.resource,
		); err != nil {
			return OwnerGateResult{}, err
		}
	}
	if occurrence.ID != "" {
		if err := appendScheduleOccurrenceAudit(
			ctx, tx, principal, "owner_gate_continue_occurrence", occurrence,
		); err != nil {
			return OwnerGateResult{}, err
		}
	}
	return OwnerGateResult{OwnerGate: resolvedGate, Process: continuedProcess}, nil
}

func saveOwnerGateResultReceipt(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	keyHash, requestHash string,
	result OwnerGateResult,
	now time.Time,
) error {
	payload, err := json.Marshal(ownerGateReceipt{Process: result.Process})
	if err != nil {
		return errs.ErrInternal
	}
	return tx.SaveReceipt(ctx, domainrepo.Receipt{
		OrganizationID: principal.OrganizationID,
		ProjectID:      principal.ProjectID,
		Scope:          "resolve_owner_gate",
		KeyHash:        keyHash,
		RequestHash:    requestHash,
		Result:         result.OwnerGate,
		Payload:        payload,
		CreatedAt:      now,
	})
}

func (service *Service) finishContinuationOccurrence(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	process entity.Resource,
	processSpec entity.ProcessRunSpec,
	turn entity.Resource,
	turnSpec entity.TurnSpec,
) error {
	if processSpec.OccurrenceID == "" || processSpec.ContinuationTurnID == "" {
		return nil
	}
	occurrence, err := tx.GetScheduleOccurrenceForUpdate(
		ctx, process.OrganizationID, process.ProjectID, processSpec.OccurrenceID,
	)
	if err != nil {
		return err
	}
	schedule, err := tx.GetForUpdate(
		ctx, process.OrganizationID, process.ProjectID, occurrence.ScheduleID,
	)
	if err != nil {
		return err
	}
	if schedule.Kind != enum.KindSchedule || schedule.OwnerActorID != process.OwnerActorID {
		return errs.ErrStateConflict
	}
	run, err := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
	if err != nil || validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.State != "CONTINUATION" || run.State != "CONTINUATION" ||
		occurrence.ScheduleID != processSpec.ScheduleID ||
		occurrence.ExecutionProcessRunID != process.ID ||
		occurrence.ExecutionTurnID != turn.ID ||
		turn.ID != processSpec.ContinuationTurnID ||
		turnSpec.Attempt != processSpec.ContinuationAttempt ||
		turnSpec.EffectiveInputSHA256 != processSpec.ContinuationInputSHA256 ||
		!turn.State.Terminal() {
		return errs.ErrStateConflict
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	occurrence.State = scheduledTerminalState(turn.State)
	occurrence.Outcome = turnSpec.Outcome
	occurrence.ResultArtifactID = turnSpec.ResultArtifactID
	occurrence.UpdatedAt = now
	if err := tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
		OccurrenceID:     occurrence.ID,
		Attempt:          occurrence.Attempt,
		State:            occurrence.State,
		Outcome:          turnSpec.Outcome,
		ResultArtifactID: turnSpec.ResultArtifactID,
		FinishedAt:       now,
	}); err != nil {
		return err
	}
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, occurrence.Attempt, occurrence.TokenHash,
	); err != nil {
		return err
	}
	return appendScheduleOccurrenceAudit(
		ctx, tx, principal, "complete_owner_continuation_occurrence", occurrence,
	)
}

// ExpireOwnerGate — bounded reconciliation-команда существующего
// interaction-gateway. PostgreSQL сам выбирает одну просроченную строку и
// удерживает её до атомарного закрытия всего связанного графа.
func (service *Service) ExpireOwnerGate(
	ctx context.Context,
	input ExpireOwnerGateInput,
) (OwnerGateResult, error) {
	if err := authorize(input.Principal, permissionExpireGate); err != nil {
		return OwnerGateResult{}, err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID {
		return OwnerGateResult{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return OwnerGateResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
	}{identity(input.Principal)})
	if err != nil {
		return OwnerGateResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result OwnerGateResult
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			gateCandidate, err := tx.NextExpiredOwnerGateCandidate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
			)
			if err != nil {
				return err
			}
			candidateSpec, ok := gateCandidate.Spec.(entity.OwnerGateSpec)
			if !ok || gateCandidate.Kind != enum.KindOwnerGate {
				return errs.ErrStateConflict
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, candidateSpec.TurnID,
			)
			if err != nil {
				return err
			}
			if graph.Runtime != nil &&
				(graph.Runtime.State != "SUSPENDED" ||
					graph.Runtime.TerminalReference != gateCandidate.ID) {
				return errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent, runtimeDispositionTerminal,
			); err != nil {
				return err
			}
			gate, err := tx.GetForUpdate(
				ctx, gateCandidate.OrganizationID, gateCandidate.ProjectID,
				gateCandidate.ID,
			)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			lockedSpec, ok := gate.Spec.(entity.OwnerGateSpec)
			if !ok || gate.Version != gateCandidate.Version ||
				gate.State != enum.StateWaitingOwner || lockedSpec.ExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx, input.Principal.OrganizationID, "expire_owner_gate", keyHash,
			)
			if receiptErr == nil {
				// Ограниченный поиск кандидата не доказывает, что receipt прежнего
				// terminal Gate относится к current candidate. Сохранённый result
				// не раскрывается, а повторное использование ключа закрыто.
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				return errs.ErrStateConflict
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			result, err = service.expireOwnerGateGraph(
				ctx, tx, input.Principal, gate, graph,
				now.UTC().Truncate(time.Microsecond),
			)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(ownerGateReceipt{Process: result.Process})
			if err != nil {
				return errs.ErrInternal
			}
			return tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "expire_owner_gate",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         result.OwnerGate,
				Payload:        payload,
				CreatedAt:      result.OwnerGate.UpdatedAt,
			})
		},
	)
	return result, err
}

func (service *Service) expireOwnerGateGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	gate entity.Resource,
	graph lockedOwnerGraph,
	now time.Time,
) (OwnerGateResult, error) {
	gateSpec, ok := gate.Spec.(entity.OwnerGateSpec)
	if !ok || gate.Kind != enum.KindOwnerGate || gate.State != enum.StateWaitingOwner {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	process := graph.Process
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok || process.Kind != enum.KindProcessRun ||
		process.State != enum.StateWaitingOwner ||
		process.OwnerActorID != gate.OwnerActorID ||
		processSpec.RootInitiatorActorID != gateSpec.RootInitiatorActorID ||
		processSpec.ImmutableInputSHA256 != gateSpec.ImmutableInputSHA256 {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	turn := graph.Turn
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.State != enum.StateWaitingOwner ||
		turn.OwnerActorID != gate.OwnerActorID ||
		turnSpec.SessionID != gateSpec.SessionID ||
		turnSpec.ProcessRunID != process.ID || turnSpec.Attempt != gateSpec.Attempt ||
		turnSpec.EffectiveInputSHA256 != gateSpec.ImmutableInputSHA256 {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	open, err := tx.ProcessHasOpenWork(
		ctx, process.OrganizationID, process.ProjectID, process.ID, turn.ID, gate.ID,
	)
	if err != nil {
		return OwnerGateResult{}, err
	}
	if open {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, turnSpec.Attempt)
	if err != nil || attempt.State != "WAITING_OWNER" ||
		attempt.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
		attempt.FinishedAt.IsZero() || attempt.Outcome != "owner_gate_pending" {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	turnSpec.Outcome = "owner_gate_expired"
	turnSpec.ResultArtifactID = ""
	turnSpec.ResultArtifactVersion = 0
	turnSpec.ResultArtifactSHA256 = ""
	failedTurn, err := turn.ReplaceAndTransition(turnSpec, enum.StateFailed, now)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, failedTurn, turn.Version); err != nil {
		return OwnerGateResult{}, err
	}
	if err := service.revokeExecutionClaimsForOwner(
		ctx, tx, principal, gate.OwnerActorID, process.ID, turn.ID,
		"owner_gate_expired", now,
	); err != nil {
		return OwnerGateResult{}, err
	}
	processSpec.Outcome = "owner_gate_expired"
	processSpec.ResultArtifactID = ""
	failedProcess, err := process.ReplaceAndTransition(
		processSpec, enum.StateFailed, now,
	)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, failedProcess, process.Version); err != nil {
		return OwnerGateResult{}, err
	}
	gateSpec.Decision = ""
	gateSpec.DecisionReason = ""
	expiredGate, err := gate.ReplaceAndTransition(gateSpec, enum.StateExpired, now)
	if err != nil {
		return OwnerGateResult{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, expiredGate, gate.Version); err != nil {
		return OwnerGateResult{}, err
	}
	if gateSpec.OccurrenceID != "" {
		occurrence := graph.Occurrence
		if occurrence.ScheduleID != gateSpec.ScheduleID ||
			occurrence.ID != gateSpec.OccurrenceID ||
			occurrence.State != "WAITING_OWNER" ||
			occurrence.ExecutionTurnID != turn.ID ||
			occurrence.ExecutionProcessRunID != process.ID {
			return OwnerGateResult{}, errs.ErrStateConflict
		}
		occurrence.State = "FAILED"
		occurrence.Outcome = "owner_gate_expired"
		occurrence.ResultArtifactID = ""
		occurrence.UpdatedAt = now
		if err := tx.UpdateScheduleOccurrence(
			ctx, occurrence, occurrence.Attempt, occurrence.TokenHash,
		); err != nil {
			return OwnerGateResult{}, err
		}
		if err := tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
			OccurrenceID: occurrence.ID,
			Attempt:      occurrence.Attempt,
			State:        "FAILED",
			Outcome:      "owner_gate_expired",
			FinishedAt:   now,
		}); err != nil {
			return OwnerGateResult{}, err
		}
	}
	for _, record := range []struct {
		action   string
		resource entity.Resource
	}{
		{"expire_owner_gate_turn", failedTurn},
		{"expire_owner_gate_process", failedProcess},
		{"expire_owner_gate", expiredGate},
	} {
		if err := service.appendMutationRecords(
			ctx, tx, principal, record.action, record.resource,
		); err != nil {
			return OwnerGateResult{}, err
		}
	}
	return OwnerGateResult{OwnerGate: expiredGate, Process: failedProcess}, nil
}

// recoverExpiredScheduleOccurrence сначала закрывает весь граф прежней
// scheduler-аренды и только затем делает новую попытку доступной. Строка
// scheduled_runs остаётся неизменяемым readback прежнего исполнения.
func (service *Service) recoverExpiredScheduleOccurrence(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graph lockedOwnerGraph,
	now time.Time,
) error {
	schedule := graph.Schedule
	occurrence := graph.Occurrence
	_, ok := schedule.Spec.(entity.ScheduleSpec)
	if !ok || schedule.Kind != enum.KindSchedule ||
		!scheduleAllowsQueuedOccurrence(schedule.State) ||
		occurrence.ScheduleID != schedule.ID || occurrence.State != "CLAIMED" ||
		occurrence.TokenHash == "" || occurrence.ClaimantWorkloadID == "" ||
		occurrence.AuthorityGeneration == 0 || occurrence.LeaseExpiresAt.After(now) {
		return errs.ErrStateConflict
	}
	run := graph.Run
	if validateScheduledRunBinding(occurrence, run) != nil ||
		run.State != "CLAIMED" {
		return errs.ErrStateConflict
	}
	session := graph.Session
	turn := graph.Turn
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || session.Kind != enum.KindSession ||
		session.OwnerActorID != schedule.OwnerActorID ||
		turn.Kind != enum.KindTurn || turn.OwnerActorID != schedule.OwnerActorID ||
		turnSpec.SessionID != session.ID || turnSpec.Attempt == 0 ||
		turnSpec.RuntimeRevisionID != run.RuntimeRevisionID ||
		turnSpec.EffectiveInputSHA256 != run.CurrentInputSHA256 {
		return errs.ErrStateConflict
	}
	if err := requireClosedRuntimeConsistentWithTurn(graph); err != nil {
		return err
	}
	if graph.Runtime != nil && !turn.State.Terminal() {
		// Scheduler recovery не является runtime authority. SUSPENDED runtime
		// сохраняет WAITING_OWNER/WAITING_EXTERNAL до специализированного path.
		return errs.ErrStateConflict
	}

	// Если terminal-команда runner уже победила до row lock recovery, повтор
	// запрещён: фиксируется именно авторитетный terminal outcome.
	if turn.State.Terminal() {
		attempt, attemptErr := tx.GetTurnAttemptForUpdate(ctx, turn.ID, turnSpec.Attempt)
		if attemptErr != nil || attempt.State != string(turn.State) ||
			attempt.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
			attempt.FinishedAt.IsZero() || turnSpec.Outcome == "" {
			return errs.ErrStateConflict
		}
		if run.ProcessRunID != "" {
			process := graph.Process
			processSpec, processOK := process.Spec.(entity.ProcessRunSpec)
			if !processOK || process.Kind != enum.KindProcessRun ||
				!process.State.Terminal() || process.State != turn.State ||
				processSpec.ScheduleID != schedule.ID ||
				processSpec.OccurrenceID != occurrence.ID ||
				processSpec.Outcome != turnSpec.Outcome {
				return errs.ErrStateConflict
			}
		}
		_, err := service.applyScheduledTerminalDisposition(
			ctx, tx, principal, schedule, occurrence, run,
			scheduledTerminalState(turn.State), turnSpec.Outcome,
			turnSpec.ResultArtifactID, now, "recover_schedule_occurrence",
		)
		return err
	}

	if _, err := service.cancelTurnExecutionForOwner(
		ctx, tx, principal, schedule.OwnerActorID, turn, "scheduler_lease_expired", now,
	); err != nil {
		return err
	}
	if run.ProcessRunID != "" {
		process := graph.Process
		processSpec, processOK := process.Spec.(entity.ProcessRunSpec)
		if !processOK || process.Kind != enum.KindProcessRun ||
			process.State.Terminal() || process.OwnerActorID != schedule.OwnerActorID ||
			processSpec.ScheduleID != schedule.ID ||
			processSpec.OccurrenceID != occurrence.ID ||
			processSpec.RootSessionID != session.ID ||
			processSpec.RootTurnID != turn.ID {
			return errs.ErrStateConflict
		}
		children, childErr := tx.HasActiveChildProcesses(
			ctx, process.OrganizationID, process.ProjectID, process.ID,
		)
		if childErr != nil || children {
			return errs.ErrStateConflict
		}
		gate, gateErr := tx.ActiveOwnerGateForProcess(
			ctx, process.OrganizationID, process.ProjectID, process.ID,
		)
		if gateErr == nil {
			if gate.OwnerActorID != schedule.OwnerActorID {
				return errs.ErrNotFound
			}
			cancelledGate, transitionErr := gate.Transition(enum.StateCancelled, now)
			if transitionErr != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Update(ctx, cancelledGate, gate.Version); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx, tx, principal, "scheduler_expiry_owner_gate", cancelledGate,
			); err != nil {
				return err
			}
		} else if !errors.Is(gateErr, errs.ErrNotFound) {
			return gateErr
		}
		cancelledProcess, transitionErr := process.Transition(enum.StateCancelled, now)
		if transitionErr != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelledProcess, process.Version); err != nil {
			return err
		}
		if err := service.revokeExecutionClaimsForOwner(
			ctx, tx, principal, schedule.OwnerActorID, process.ID, turn.ID,
			"scheduler_lease_expired", now,
		); err != nil {
			return err
		}
		if err := service.appendMutationRecords(
			ctx, tx, principal, "scheduler_expiry_process", cancelledProcess,
		); err != nil {
			return err
		}
	}
	if session.ParentID == schedule.ID && !session.State.Terminal() {
		cancelledSession, transitionErr := session.Transition(enum.StateCancelled, now)
		if transitionErr != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelledSession, session.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(
			ctx, tx, principal, "scheduler_expiry_session", cancelledSession,
		); err != nil {
			return err
		}
	}
	_, err := service.applyScheduledTerminalDisposition(
		ctx, tx, principal, schedule, occurrence, run,
		"FAILED", "scheduler_lease_expired", "", now,
		"recover_schedule_occurrence",
	)
	return err
}
