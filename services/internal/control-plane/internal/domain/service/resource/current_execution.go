package resource

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

// executionTuple — единая назначенная сервером координата текущей попытки.
// Root/target/continuation сохраняются как история, а lifecycle-команды читают
// только эту связку.
type executionTuple struct {
	SessionID              string
	SessionVersion         uint64
	TurnID                 string
	TurnVersion            uint64
	Attempt                uint32
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	InputSHA256            string
}

func (service *Service) prepareRetriedExecution(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turn entity.Resource,
	spec entity.TurnSpec,
	now time.Time,
) (entity.Resource, entity.TurnSpec, error) {
	if spec.Attempt >= 100 {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	session, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.SessionID,
	)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.Kind != enum.KindSession || session.State != enum.StateActive ||
		session.OwnerActorID != turn.OwnerActorID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	prompt, err := service.requireCleanArtifact(ctx, tx, principal, spec.PromptArtifactID)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	revision, err := service.createRuntimeRevision(ctx, tx, principal, session, sessionSpec)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrInternal
	}
	previousAttempt := spec.Attempt
	spec, err = prepareRetryTurnSpec(
		spec, revision.ID, prompt.SHA256, revisionSpec.ManifestSHA256,
	)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	retried, err := turn.ReplaceAndTransition(spec, enum.StateQueued, now)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if spec.ProcessRunID == "" {
		return service.rebindStandaloneScheduledRetry(
			ctx, tx, principal, turn, retried, spec, previousAttempt, now,
		)
	}

	process, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.ProcessRunID,
	)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		(process.State.Terminal() && process.State != enum.StateFailed &&
			process.State != enum.StateExpired) || current.TurnID != turn.ID ||
		current.Attempt != previousAttempt {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	wasWaitingOwner := process.State == enum.StateWaitingOwner
	if wasWaitingOwner {
		gate, gateErr := tx.ActiveOwnerGateForProcess(
			ctx, process.OrganizationID, process.ProjectID, process.ID,
		)
		if gateErr != nil || gate.OwnerActorID != process.OwnerActorID {
			return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
		}
		cancelledGate, transitionErr := gate.Transition(enum.StateCancelled, now)
		if transitionErr != nil {
			return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelledGate, gate.Version); err != nil {
			return entity.Resource{}, entity.TurnSpec{}, err
		}
		if err := service.appendMutationRecords(
			ctx, tx, principal, "retry_turn_cancel_owner_gate", cancelledGate,
		); err != nil {
			return entity.Resource{}, entity.TurnSpec{}, err
		}
		// Незавершённый gate не имеет owner feedback и поэтому не является
		// OWNER_GATE continuation arm. Retry закрывает gate и начинает обычную
		// свежую попытку без фиктивных gate/digest provenance.
		processSpec.ClearContinuation()
	}
	tuple := executionTuple{
		SessionID:              session.ID,
		SessionVersion:         session.Version,
		TurnID:                 retried.ID,
		TurnVersion:            retried.Version,
		Attempt:                spec.Attempt,
		RuntimeRevisionID:      revision.ID,
		RuntimeRevisionVersion: revision.Version,
		InputSHA256:            spec.EffectiveInputSHA256,
	}
	setCurrentExecution(&processSpec, tuple)
	processSpec.Outcome = ""
	processSpec.ResultArtifactID = ""
	continuationBinding := entity.ProcessContinuationBinding{
		TurnID: retried.ID, TurnVersion: retried.Version, Attempt: spec.Attempt,
		RuntimeRevisionID: revision.ID, RuntimeRevisionVersion: revision.Version,
		InputSHA256: spec.EffectiveInputSHA256,
	}
	switch processSpec.ContinuationKind {
	case enum.ProcessContinuationNone:
		processSpec.ClearContinuation()
	case enum.ProcessContinuationOwnerGate:
		if err := processSpec.SetOwnerGateContinuation(
			continuationBinding, processSpec.ContinuationGateID,
			processSpec.OwnerFeedbackSHA256,
		); err != nil {
			return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
		}
	case enum.ProcessContinuationIntegration:
		if err := processSpec.SetIntegrationContinuation(
			continuationBinding, processSpec.ContinuationIntegrationID,
			processSpec.ContinuationOutcomeSHA256,
		); err != nil {
			return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
		}
	default:
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	var updatedProcess entity.Resource
	if process.State == enum.StateFailed || process.State == enum.StateExpired ||
		process.State == enum.StateWaitingOwner ||
		process.State == enum.StateWaitingExternal || process.State == enum.StateBlocked {
		updatedProcess, err = process.ReplaceAndTransition(processSpec, enum.StateRunning, now)
	} else {
		updatedProcess, err = process.Update(process.Name, processSpec, now)
	}
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updatedProcess, process.Version); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "rebind_process_retry", updatedProcess,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	if processSpec.OccurrenceID == "" {
		return retried, spec, nil
	}
	occurrence, err := tx.GetScheduleOccurrenceForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, processSpec.OccurrenceID,
	)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	schedule, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, processSpec.ScheduleID,
	)
	if err != nil || schedule.Kind != enum.KindSchedule ||
		schedule.OwnerActorID != process.OwnerActorID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	run, err := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
	if err != nil || validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.ExecutionTurnID != turn.ID ||
		occurrence.ExecutionProcessRunID != process.ID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	occurrence.ExecutionSessionID = session.ID
	occurrence.ExecutionSessionVersion = session.Version
	occurrence.ExecutionTurnID = retried.ID
	occurrence.ExecutionTurnVersion = retried.Version
	occurrence.ExecutionProcessVersion = updatedProcess.Version
	occurrence.ExecutionRuntimeRevisionID = revision.ID
	occurrence.ExecutionRuntimeRevisionVersion = revision.Version
	occurrence.EffectiveInputSHA256 = spec.EffectiveInputSHA256
	if occurrence.State == "WAITING_OWNER" || occurrence.State == "FAILED" {
		occurrence.State = "CONTINUATION"
	}
	occurrence.Outcome = ""
	occurrence.ResultArtifactID = ""
	occurrence.UpdatedAt = now
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, occurrence.Attempt, occurrence.TokenHash,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	run.CurrentSessionID = session.ID
	run.CurrentSessionVersion = session.Version
	run.CurrentTurnID = retried.ID
	run.CurrentTurnVersion = retried.Version
	run.CurrentTurnAttempt = spec.Attempt
	run.CurrentProcessRunID = updatedProcess.ID
	run.CurrentProcessVersion = updatedProcess.Version
	run.CurrentRuntimeRevisionID = revision.ID
	run.CurrentRuntimeRevisionVersion = revision.Version
	run.CurrentInputSHA256 = spec.EffectiveInputSHA256
	// Поля ScheduledRun.Continuation* сохраняют owner-feedback provenance и
	// применимы только к OwnerGate. Integration continuation использует тот же
	// current tuple, но её immutable outcome хранится в отдельном owner aggregate.
	if processSpec.ContinuationTurnID != "" &&
		strings.HasPrefix(spec.SourceRef, "owner-gate-continuation:") {
		run.ContinuationTurnID = retried.ID
		run.ContinuationTurnVersion = retried.Version
		run.ContinuationRuntimeRevisionID = revision.ID
		run.ContinuationRuntimeRevisionVersion = revision.Version
		run.ContinuationInputSHA256 = spec.EffectiveInputSHA256
		run.OwnerFeedbackSHA256 = processSpec.OwnerFeedbackSHA256
	}
	if err := tx.RebindScheduledRun(ctx, run, turn.ID, previousAttempt); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "rebind_schedule_retry", schedule,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	return retried, spec, nil
}

func prepareRetryTurnSpec(
	spec entity.TurnSpec,
	runtimeRevisionID, promptSHA256, manifestSHA256 string,
) (entity.TurnSpec, error) {
	if spec.Attempt >= 100 || value.ValidateID(runtimeRevisionID) != nil ||
		!validSHA256Text(promptSHA256) || !validSHA256Text(manifestSHA256) {
		return entity.TurnSpec{}, errs.ErrStateConflict
	}
	spec.Attempt++
	spec.RuntimeRevisionID = runtimeRevisionID
	// SourceRef — неизменяемая server-bound identity. Номер попытки уже входит
	// в current tuple и не должен безгранично раздувать допустимую ссылку.
	spec.EffectiveInputSHA256 = hashRuntimeInput(
		spec.SourceRef, promptSHA256, manifestSHA256, spec.ProcessRunID,
	)
	spec.Outcome = ""
	spec.ResultArtifactID = ""
	return spec, nil
}

func (service *Service) rebindStandaloneScheduledRetry(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	previous, retried entity.Resource,
	spec entity.TurnSpec,
	previousAttempt uint32,
	now time.Time,
) (entity.Resource, entity.TurnSpec, error) {
	run, err := tx.GetScheduledRunByCurrentTurnForUpdate(ctx, previous.ID)
	if errors.Is(err, errs.ErrNotFound) {
		return retried, spec, nil
	}
	previousSpec, previousOK := previous.Spec.(entity.TurnSpec)
	if err != nil || !previousOK || run.CurrentTurnAttempt != previousAttempt ||
		run.CurrentInputSHA256 != previousSpec.EffectiveInputSHA256 ||
		run.CurrentProcessRunID != "" {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	occurrence, err := tx.GetScheduleOccurrenceForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, run.OccurrenceID,
	)
	if err != nil || validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.State != "CLAIMED" {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	revision, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.RuntimeRevisionID,
	)
	if err != nil || revision.Kind != enum.KindRuntimeRevision ||
		revision.State != enum.StateActive {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	occurrence.ExecutionSessionID = spec.SessionID
	occurrence.ExecutionSessionVersion = run.CurrentSessionVersion
	occurrence.ExecutionTurnID = retried.ID
	occurrence.ExecutionTurnVersion = retried.Version
	occurrence.ExecutionRuntimeRevisionID = spec.RuntimeRevisionID
	occurrence.ExecutionRuntimeRevisionVersion = revision.Version
	occurrence.EffectiveInputSHA256 = spec.EffectiveInputSHA256
	occurrence.UpdatedAt = now
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, occurrence.Attempt, occurrence.TokenHash,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	run.CurrentTurnID = retried.ID
	run.CurrentTurnVersion = retried.Version
	run.CurrentTurnAttempt = spec.Attempt
	run.CurrentRuntimeRevisionID = spec.RuntimeRevisionID
	run.CurrentRuntimeRevisionVersion = revision.Version
	run.CurrentInputSHA256 = spec.EffectiveInputSHA256
	if err := tx.RebindScheduledRun(ctx, run, previous.ID, previousAttempt); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	schedule, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, occurrence.ScheduleID,
	)
	if err != nil || schedule.Kind != enum.KindSchedule ||
		schedule.OwnerActorID != retried.OwnerActorID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "rebind_schedule_retry", schedule,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	return retried, spec, nil
}

func currentExecution(spec entity.ProcessRunSpec) (executionTuple, error) {
	tuple := executionTuple{
		SessionID:              spec.CurrentSessionID,
		SessionVersion:         spec.CurrentSessionVersion,
		TurnID:                 spec.CurrentTurnID,
		TurnVersion:            spec.CurrentTurnVersion,
		Attempt:                spec.CurrentAttempt,
		RuntimeRevisionID:      spec.CurrentRuntimeRevisionID,
		RuntimeRevisionVersion: spec.CurrentRuntimeRevisionVersion,
		InputSHA256:            spec.CurrentInputSHA256,
	}
	if tuple.SessionID != "" {
		if tuple.SessionVersion == 0 || tuple.TurnID == "" || tuple.TurnVersion == 0 ||
			tuple.Attempt == 0 || tuple.RuntimeRevisionID == "" ||
			tuple.RuntimeRevisionVersion == 0 || !validSHA256Text(tuple.InputSHA256) {
			return executionTuple{}, errs.ErrStateConflict
		}
		return tuple, nil
	}

	// Совместимое чтение строк, созданных до появления явного current tuple.
	if spec.ContinuationTurnID != "" {
		return executionTuple{
			SessionID:              currentSessionID(spec),
			SessionVersion:         currentSessionVersion(spec),
			TurnID:                 spec.ContinuationTurnID,
			TurnVersion:            spec.ContinuationTurnVersion,
			Attempt:                spec.ContinuationAttempt,
			RuntimeRevisionID:      spec.ContinuationRuntimeRevisionID,
			RuntimeRevisionVersion: spec.ContinuationRuntimeRevisionVersion,
			InputSHA256:            spec.ContinuationInputSHA256,
		}, nil
	}
	if spec.ParentProcessRunID != "" {
		return executionTuple{
			SessionID:      spec.TargetSessionID,
			SessionVersion: spec.TargetSessionVersion,
			TurnID:         spec.TargetTurnID,
			TurnVersion:    spec.TargetTurnVersion,
			Attempt:        spec.TargetAttempt,
		}, nil
	}
	return executionTuple{
		SessionID:      spec.RootSessionID,
		SessionVersion: spec.RootSessionVersion,
		TurnID:         spec.RootTurnID,
		TurnVersion:    spec.RootTurnVersion,
		Attempt:        spec.RootAttempt,
	}, nil
}

func currentSessionID(spec entity.ProcessRunSpec) string {
	if spec.CurrentSessionID != "" {
		return spec.CurrentSessionID
	}
	if spec.ParentProcessRunID != "" {
		return spec.TargetSessionID
	}
	return spec.RootSessionID
}

func currentSessionVersion(spec entity.ProcessRunSpec) uint64 {
	if spec.CurrentSessionVersion != 0 {
		return spec.CurrentSessionVersion
	}
	if spec.ParentProcessRunID != "" {
		return spec.TargetSessionVersion
	}
	return spec.RootSessionVersion
}

func setCurrentExecution(spec *entity.ProcessRunSpec, tuple executionTuple) {
	spec.CurrentSessionID = tuple.SessionID
	spec.CurrentSessionVersion = tuple.SessionVersion
	spec.CurrentTurnID = tuple.TurnID
	spec.CurrentTurnVersion = tuple.TurnVersion
	spec.CurrentAttempt = tuple.Attempt
	spec.CurrentRuntimeRevisionID = tuple.RuntimeRevisionID
	spec.CurrentRuntimeRevisionVersion = tuple.RuntimeRevisionVersion
	spec.CurrentInputSHA256 = tuple.InputSHA256
}

func executionMatchesTurn(
	tuple executionTuple,
	turn entity.Resource,
	spec entity.TurnSpec,
) bool {
	return tuple.TurnID == turn.ID && tuple.Attempt == spec.Attempt &&
		tuple.SessionID == spec.SessionID &&
		tuple.RuntimeRevisionID == spec.RuntimeRevisionID &&
		tuple.InputSHA256 == spec.EffectiveInputSHA256
}

func (service *Service) resolveCurrentExecution(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	process entity.Resource,
	spec entity.ProcessRunSpec,
) (executionTuple, error) {
	tuple, err := currentExecution(spec)
	if err != nil {
		return executionTuple{}, err
	}
	session, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, tuple.SessionID,
	)
	if err != nil {
		return executionTuple{}, err
	}
	turn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, tuple.TurnID,
	)
	if err != nil {
		return executionTuple{}, err
	}
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if tuple.RuntimeRevisionID == "" {
		tuple.RuntimeRevisionID = turnSpec.RuntimeRevisionID
		tuple.InputSHA256 = turnSpec.EffectiveInputSHA256
	}
	if !ok || session.Kind != enum.KindSession || session.State != enum.StateActive ||
		turn.Kind != enum.KindTurn || turn.State.Terminal() ||
		session.OwnerActorID != process.OwnerActorID ||
		turn.OwnerActorID != process.OwnerActorID ||
		turnSpec.ProcessRunID != process.ID ||
		!executionMatchesTurn(tuple, turn, turnSpec) {
		return executionTuple{}, errs.ErrStateConflict
	}
	if tuple.SessionVersion == 0 {
		tuple.SessionVersion = session.Version
	}
	if tuple.TurnVersion == 0 {
		tuple.TurnVersion = turn.Version
	}
	if tuple.RuntimeRevisionVersion == 0 {
		revision, getErr := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID,
			turnSpec.RuntimeRevisionID,
		)
		if getErr != nil || revision.Kind != enum.KindRuntimeRevision ||
			revision.State != enum.StateActive {
			return executionTuple{}, errs.ErrStateConflict
		}
		tuple.RuntimeRevisionID = revision.ID
		tuple.RuntimeRevisionVersion = revision.Version
		tuple.InputSHA256 = turnSpec.EffectiveInputSHA256
	}
	return tuple, nil
}
