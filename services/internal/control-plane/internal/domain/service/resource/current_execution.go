package resource

import (
	"context"
	"reflect"
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
	graph lockedOwnerGraph,
	spec entity.TurnSpec,
	restore *domainrepo.RuntimeRestoreOperation,
	now time.Time,
) (entity.Resource, entity.TurnSpec, error) {
	turn := graph.Turn
	if spec.Attempt >= 100 {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	session := graph.Session
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.ID != spec.SessionID || session.Kind != enum.KindSession ||
		session.State != enum.StateActive ||
		session.OwnerActorID != turn.OwnerActorID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	prompt, err := service.requireCleanArtifact(ctx, tx, principal, spec.PromptArtifactID)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	revision, err := service.createRuntimeRevision(
		ctx, tx, principal, session, sessionSpec, spec.ScheduledResultContract,
	)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrInternal
	}
	spec, err = prepareRetryTurnSpec(
		spec, revision.ID, prompt.SHA256, revisionSpec.ManifestSHA256,
	)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
	if restore != nil {
		spec.RestoreOperationID = restore.ID
		spec.RestoreSourceExecutionID = restore.BackupID
		spec.RestoreSourceVersion = restore.SourceVersion
		spec.RestoreSourceArchiveSHA256 = restore.ArchiveSHA256
		spec.RestoreSourceProvenanceSHA256 = restore.ProvenanceSHA256
		spec.RestoreOperationGeneration = restore.Generation
		spec.RestoreSourceAuthoritySHA256 = restore.SourceAuthoritySHA256
		spec.EffectiveInputSHA256, err = canonicalHash(struct {
			BaseSHA256, RestoreOperationID, RestoreSourceAuthoritySHA256 string
			RestoreOperationGeneration                                   uint64
		}{spec.EffectiveInputSHA256, restore.ID, restore.SourceAuthoritySHA256, restore.Generation})
		if err != nil {
			return entity.Resource{}, entity.TurnSpec{}, errs.ErrInternal
		}
		if spec.Validate() != nil {
			return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
		}
	}
	previousAttempt := spec.Attempt - 1
	// Interaction-gateway уже создал server-owned Session/Turn. Retry не
	// наследует и не создаёт bot-service binding: новую attempt/revision
	// авторитетно связывает только control-plane owner transaction.
	spec.AgentSessionTurnID = 0
	spec.AgentRunID = ""
	spec.AgentTurnBindingVersion = 0
	spec.AgentTurnBindingSHA256 = ""
	retried, err := turn.ReplaceAndTransition(spec, enum.StateQueued, now)
	if err != nil {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if spec.ProcessRunID == "" {
		return service.rebindStandaloneScheduledRetry(
			ctx, tx, principal, graph, retried, spec, previousAttempt,
			revision, now,
		)
	}

	process := graph.Process
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.ID != spec.ProcessRunID ||
		process.Kind != enum.KindProcessRun ||
		(process.State.Terminal() && process.State != enum.StateFailed &&
			process.State != enum.StateCancelled && process.State != enum.StateExpired) || current.TurnID != turn.ID ||
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
	if process.State == enum.StateFailed || process.State == enum.StateCancelled || process.State == enum.StateExpired ||
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
	if graph.Occurrence.ID == "" {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	occurrence := graph.Occurrence
	schedule := graph.Schedule
	run := graph.Run
	if occurrence.ID != processSpec.OccurrenceID ||
		schedule.ID != processSpec.ScheduleID || schedule.Kind != enum.KindSchedule ||
		schedule.OwnerActorID != process.OwnerActorID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.ExecutionTurnID != turn.ID ||
		occurrence.ExecutionProcessRunID != process.ID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	targetOccurrenceState := occurrence.State
	if occurrence.State == "WAITING_OWNER" || occurrence.State == "FAILED" {
		targetOccurrenceState = "CONTINUATION"
	}
	if err := rebindScheduledOccurrence(
		&occurrence,
		targetOccurrenceState,
		scheduledOccurrenceExecutionBinding{
			SessionID: session.ID, SessionVersion: session.Version,
			TurnID: retried.ID, TurnVersion: retried.Version,
			ProcessRunID: updatedProcess.ID, ProcessVersion: updatedProcess.Version,
			RuntimeRevisionID:      revision.ID,
			RuntimeRevisionVersion: revision.Version,
			InputSHA256:            spec.EffectiveInputSHA256,
		},
		"",
		now,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
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
	if err := appendScheduleOccurrenceAudit(
		ctx, tx, principal, "rebind_schedule_retry", occurrence,
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
	spec.ResultArtifactVersion = 0
	spec.ResultArtifactSHA256 = ""
	spec.RestoreOperationID = ""
	spec.RestoreSourceExecutionID = ""
	spec.RestoreSourceVersion = 0
	spec.RestoreSourceArchiveSHA256 = ""
	spec.RestoreSourceProvenanceSHA256 = ""
	spec.RestoreOperationGeneration = 0
	spec.RestoreSourceAuthoritySHA256 = ""
	return spec, nil
}

func (service *Service) rebindStandaloneScheduledRetry(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graph lockedOwnerGraph,
	retried entity.Resource,
	spec entity.TurnSpec,
	previousAttempt uint32,
	revision entity.Resource,
	now time.Time,
) (entity.Resource, entity.TurnSpec, error) {
	if graph.Occurrence.ID == "" {
		return retried, spec, nil
	}
	previous := graph.Turn
	run := graph.Run
	previousSpec, previousOK := previous.Spec.(entity.TurnSpec)
	if !previousOK || run.CurrentTurnID != previous.ID ||
		run.CurrentTurnAttempt != previousAttempt ||
		run.CurrentInputSHA256 != previousSpec.EffectiveInputSHA256 ||
		run.CurrentProcessRunID != "" {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	occurrence := graph.Occurrence
	if validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.State != "CLAIMED" {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if revision.ID != spec.RuntimeRevisionID || revision.Kind != enum.KindRuntimeRevision ||
		revision.State != enum.StateActive {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if err := rebindScheduledOccurrence(
		&occurrence,
		"CLAIMED",
		scheduledOccurrenceExecutionBinding{
			SessionID: spec.SessionID, SessionVersion: run.CurrentSessionVersion,
			TurnID: retried.ID, TurnVersion: retried.Version,
			RuntimeRevisionID:      spec.RuntimeRevisionID,
			RuntimeRevisionVersion: revision.Version,
			InputSHA256:            spec.EffectiveInputSHA256,
		},
		"",
		now,
	); err != nil {
		return entity.Resource{}, entity.TurnSpec{}, err
	}
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
	schedule := graph.Schedule
	if schedule.ID != occurrence.ScheduleID || schedule.Kind != enum.KindSchedule ||
		schedule.OwnerActorID != retried.OwnerActorID {
		return entity.Resource{}, entity.TurnSpec{}, errs.ErrStateConflict
	}
	if err := appendScheduleOccurrenceAudit(
		ctx, tx, principal, "rebind_schedule_retry", occurrence,
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

// requireCurrentTurnBinding доказывает, что все уже заблокированные строки
// owner graph указывают на одну и ту же текущую попытку. Проверка выполняется
// до receipt replay и до любого version bump: сохранённый receipt не может
// скрыть stale ProcessRun либо scheduled binding.
func requireCurrentTurnBinding(graph lockedOwnerGraph) error {
	turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
	if !ok || graph.Turn.Kind != enum.KindTurn ||
		graph.Session.Kind != enum.KindSession ||
		turnSpec.SessionID != graph.Session.ID {
		return errs.ErrStateConflict
	}
	var processCurrent executionTuple
	if turnSpec.ProcessRunID == "" {
		if graph.Process.ID != "" {
			return errs.ErrStateConflict
		}
	} else {
		processSpec, processOK := graph.Process.Spec.(entity.ProcessRunSpec)
		current, currentErr := currentExecution(processSpec)
		if !processOK || currentErr != nil || graph.Process.Kind != enum.KindProcessRun ||
			graph.Process.ID != turnSpec.ProcessRunID ||
			current.SessionID != graph.Session.ID ||
			current.SessionVersion != graph.Session.Version ||
			current.TurnID != graph.Turn.ID ||
			current.TurnVersion != graph.Turn.Version ||
			current.Attempt != turnSpec.Attempt ||
			current.RuntimeRevisionID != turnSpec.RuntimeRevisionID ||
			current.InputSHA256 != turnSpec.EffectiveInputSHA256 {
			return errs.ErrStateConflict
		}
		processCurrent = current
	}
	if graph.Occurrence.ID == "" {
		if graph.Run.OccurrenceID != "" {
			return errs.ErrStateConflict
		}
		return nil
	}
	occurrence, run := graph.Occurrence, graph.Run
	if graph.Schedule.Kind != enum.KindSchedule ||
		graph.Schedule.ID != occurrence.ScheduleID ||
		validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.ExecutionSessionID != graph.Session.ID ||
		occurrence.ExecutionSessionVersion != graph.Session.Version ||
		occurrence.ExecutionTurnID != graph.Turn.ID ||
		occurrence.ExecutionTurnVersion != graph.Turn.Version ||
		run.CurrentTurnAttempt != turnSpec.Attempt ||
		run.CurrentRuntimeRevisionID != turnSpec.RuntimeRevisionID ||
		run.CurrentInputSHA256 != turnSpec.EffectiveInputSHA256 {
		return errs.ErrStateConflict
	}
	if turnSpec.ProcessRunID == "" {
		if occurrence.ExecutionProcessRunID != "" ||
			occurrence.ExecutionProcessVersion != 0 ||
			run.CurrentProcessRunID != "" || run.CurrentProcessVersion != 0 {
			return errs.ErrStateConflict
		}
		return nil
	}
	if occurrence.ExecutionProcessRunID != graph.Process.ID ||
		occurrence.ExecutionProcessVersion != graph.Process.Version ||
		run.CurrentProcessRunID != graph.Process.ID ||
		run.CurrentProcessVersion != graph.Process.Version ||
		occurrence.ExecutionRuntimeRevisionVersion != processCurrent.RuntimeRevisionVersion ||
		run.CurrentRuntimeRevisionVersion != processCurrent.RuntimeRevisionVersion {
		return errs.ErrStateConflict
	}
	return nil
}

// propagateCurrentTurnTransition переносит server-owned current tuple после
// нетерминального version bump Turn. Все строки уже получены через
// lockOwnerGraphByTurn; helper не открывает поздние locks и либо сохраняет
// Turn/ProcessRun/scheduled binding целиком, либо откатывает owner transaction.
func (service *Service) propagateCurrentTurnTransition(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graph lockedOwnerGraph,
	updatedTurn entity.Resource,
	now time.Time,
) (entity.Resource, error) {
	if err := requireCurrentTurnBinding(graph); err != nil {
		return entity.Resource{}, err
	}
	previousSpec, previousOK := graph.Turn.Spec.(entity.TurnSpec)
	updatedSpec, updatedOK := updatedTurn.Spec.(entity.TurnSpec)
	if !previousOK || !updatedOK || updatedTurn.ID != graph.Turn.ID ||
		updatedTurn.Version != graph.Turn.Version+1 || !reflect.DeepEqual(updatedSpec, previousSpec) ||
		updatedTurn.State.Terminal() {
		return entity.Resource{}, errs.ErrStateConflict
	}

	updatedProcess := graph.Process
	if updatedSpec.ProcessRunID != "" {
		processSpec, ok := graph.Process.Spec.(entity.ProcessRunSpec)
		if !ok {
			return entity.Resource{}, errs.ErrStateConflict
		}
		current, err := currentExecution(processSpec)
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		current.TurnVersion = updatedTurn.Version
		setCurrentExecution(&processSpec, current)
		updatedProcess, err = graph.Process.Update(graph.Process.Name, processSpec, now)
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := tx.Update(ctx, updatedProcess, graph.Process.Version); err != nil {
			return entity.Resource{}, err
		}
		if err := service.appendMutationRecords(
			ctx, tx, principal, "propagate_current_turn_process", updatedProcess,
		); err != nil {
			return entity.Resource{}, err
		}
	}

	if graph.Occurrence.ID == "" {
		return updatedProcess, nil
	}
	occurrence := graph.Occurrence
	occurrence.ExecutionTurnVersion = updatedTurn.Version
	if updatedProcess.ID != "" {
		occurrence.ExecutionProcessVersion = updatedProcess.Version
	}
	occurrence.UpdatedAt = now
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, graph.Occurrence.Attempt, graph.Occurrence.TokenHash,
	); err != nil {
		return entity.Resource{}, err
	}
	run := graph.Run
	run.CurrentTurnVersion = updatedTurn.Version
	if updatedProcess.ID != "" {
		run.CurrentProcessVersion = updatedProcess.Version
	}
	if err := tx.RebindScheduledRun(
		ctx, run, graph.Turn.ID, previousSpec.Attempt,
	); err != nil {
		return entity.Resource{}, err
	}
	if err := appendScheduleOccurrenceAudit(
		ctx, tx, principal, "propagate_current_turn_schedule", occurrence,
	); err != nil {
		return entity.Resource{}, err
	}
	return updatedProcess, nil
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
