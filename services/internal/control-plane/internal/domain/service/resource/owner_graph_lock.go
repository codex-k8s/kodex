package resource

import (
	"context"
	"errors"
	"slices"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	graphLockRuntimeExecution = "runtime_execution"
	graphLockOccurrence       = "schedule_occurrence"
	graphLockSchedule         = "schedule"
	graphLockScheduledRun     = "scheduled_run"
	graphLockSession          = "session"
	graphLockTurn             = "turn"
	graphLockProcessRun       = "process_run"
	graphLockPinnedResource   = "pinned_resource"
	graphLockOwnerGate        = "owner_gate"
	graphLockContinuation     = "integration_continuation"
)

// lockedOwnerGraph — результат единственного production acquisition path для
// общих execution rows. Candidate discovery выполняется без row lock, после
// чего exact tuple повторно проверяется под каноническими locks.
type lockedOwnerGraph struct {
	Runtime    *RuntimeExecution
	Occurrence domainrepo.ScheduleOccurrence
	Schedule   entity.Resource
	Run        domainrepo.ScheduledRun
	Session    entity.Resource
	Turn       entity.Resource
	Process    entity.Resource
	Steps      []string
}

type runtimeGraphDisposition string

const (
	runtimeDispositionAbsent      runtimeGraphDisposition = "ABSENT"
	runtimeDispositionNonterminal runtimeGraphDisposition = "CURRENT_NONTERMINAL"
	runtimeDispositionTerminal    runtimeGraphDisposition = "TERMINAL"
)

func ownerGraphRuntimeDisposition(graph lockedOwnerGraph) runtimeGraphDisposition {
	if graph.Runtime == nil {
		return runtimeDispositionAbsent
	}
	if runtimeTerminal(graph.Runtime.State) {
		return runtimeDispositionTerminal
	}
	return runtimeDispositionNonterminal
}

// requireOwnerGraphRuntimeDisposition не позволяет caller незаметно отбросить
// graph.Runtime. Каждый переход общего графа обязан явно назвать допустимое
// состояние runtime либо закрыто отказаться до первого effect.
func requireOwnerGraphRuntimeDisposition(
	graph lockedOwnerGraph,
	allowed ...runtimeGraphDisposition,
) error {
	if slices.Contains(allowed, ownerGraphRuntimeDisposition(graph)) {
		return nil
	}
	return errs.ErrStateConflict
}

// requireClosedRuntimeConsistentWithTurn доказывает, что terminal runtime и
// текущий Turn описывают один закрытый outcome. Generic/scheduler path может
// продолжить только после этого доказательства; live runtime обслуживается
// исключительно специализированной runtime-командой.
func requireClosedRuntimeConsistentWithTurn(graph lockedOwnerGraph) error {
	if graph.Runtime == nil {
		return nil
	}
	if ownerGraphRuntimeDisposition(graph) != runtimeDispositionTerminal ||
		graph.Runtime.TerminalOutcome == "" {
		return errs.ErrStateConflict
	}
	expected := enum.State("")
	switch graph.Runtime.State {
	case "SUCCEEDED":
		expected = enum.StateSucceeded
	case "FAILED":
		expected = enum.StateFailed
	case "CANCELLED":
		expected = enum.StateCancelled
	case "EXPIRED":
		expected = enum.StateExpired
	case "SUSPENDED":
		if graph.Turn.State != enum.StateWaitingOwner &&
			graph.Turn.State != enum.StateWaitingExternal {
			return errs.ErrStateConflict
		}
		return nil
	default:
		// RETRIED не остаётся current: retry продвигает attempt Turn раньше,
		// чем generic graph path сможет её наблюдать.
		return errs.ErrStateConflict
	}
	if graph.Turn.State != expected {
		return errs.ErrStateConflict
	}
	return nil
}

func ownerGraphLockPlan(runtime, scheduled, process bool) []string {
	steps := make([]string, 0, 7)
	if runtime {
		steps = append(steps, graphLockRuntimeExecution)
	}
	if scheduled {
		steps = append(steps, graphLockOccurrence, graphLockSchedule, graphLockScheduledRun)
	}
	steps = append(steps, graphLockSession, graphLockTurn)
	if process {
		steps = append(steps, graphLockProcessRun)
	}
	return steps
}

// lockOwnerGraphByTurn реализует общий порядок RuntimeExecution (если строка
// уже существует) -> occurrence -> schedule -> scheduled run -> session ->
// turn -> process run. Pinned resources/gate/continuation берутся после него.
func (service *Service) lockOwnerGraphByTurn(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turnID string,
) (lockedOwnerGraph, error) {
	candidateTurn, err := tx.Get(
		ctx, principal.OrganizationID, principal.ProjectID, turnID,
	)
	if err != nil {
		return lockedOwnerGraph{}, err
	}
	candidateSpec, ok := candidateTurn.Spec.(entity.TurnSpec)
	if !ok || candidateTurn.Kind != enum.KindTurn ||
		value.ValidateID(candidateSpec.SessionID) != nil {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}

	candidateOccurrence, occurrenceErr := tx.GetScheduleOccurrenceByCurrentTurn(
		ctx, principal.OrganizationID, principal.ProjectID, turnID,
	)
	scheduled := occurrenceErr == nil
	if occurrenceErr != nil && !errors.Is(occurrenceErr, errs.ErrNotFound) {
		return lockedOwnerGraph{}, occurrenceErr
	}
	hasProcess := candidateSpec.ProcessRunID != ""
	graph := lockedOwnerGraph{Steps: make([]string, 0, 7)}
	runtimeExecution, runtimeErr := tx.GetRuntimeExecutionByTurnForUpdate(
		ctx, turnID, candidateSpec.Attempt,
	)
	if runtimeErr == nil {
		if runtimeExecution.OrganizationID != principal.OrganizationID ||
			runtimeExecution.ProjectID != principal.ProjectID ||
			runtimeExecution.TurnID != turnID ||
			runtimeExecution.Attempt != candidateSpec.Attempt ||
			runtimeExecution.ImmutableInputSHA256 != candidateSpec.EffectiveInputSHA256 {
			return lockedOwnerGraph{}, errs.ErrStateConflict
		}
		graph.Steps = append(graph.Steps, graphLockRuntimeExecution)
		graph.Runtime = &runtimeExecution
	} else if !errors.Is(runtimeErr, errs.ErrNotFound) {
		return lockedOwnerGraph{}, runtimeErr
	}
	if scheduled {
		graph.Steps = append(graph.Steps, graphLockOccurrence)
		graph.Occurrence, err = tx.GetScheduleOccurrenceForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, candidateOccurrence.ID,
		)
		if err != nil {
			return lockedOwnerGraph{}, err
		}
		if graph.Occurrence.ExecutionTurnID != turnID ||
			graph.Occurrence.ScheduleID != candidateOccurrence.ScheduleID ||
			graph.Occurrence.Attempt != candidateOccurrence.Attempt ||
			graph.Occurrence.State != candidateOccurrence.State {
			return lockedOwnerGraph{}, errs.ErrStateConflict
		}
		graph.Steps = append(graph.Steps, graphLockSchedule)
		graph.Schedule, err = tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID,
			graph.Occurrence.ScheduleID,
		)
		if err != nil {
			return lockedOwnerGraph{}, err
		}
		graph.Steps = append(graph.Steps, graphLockScheduledRun)
		graph.Run, err = tx.GetScheduledRunForUpdate(
			ctx, graph.Occurrence.ID, graph.Occurrence.Attempt,
		)
		if err != nil || validateScheduledRunBinding(graph.Occurrence, graph.Run) != nil ||
			graph.Run.CurrentTurnID != turnID || graph.Run.State != graph.Occurrence.State {
			return lockedOwnerGraph{}, errs.ErrStateConflict
		}
	}

	graph.Steps = append(graph.Steps, graphLockSession)
	graph.Session, err = tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, candidateSpec.SessionID,
	)
	if err != nil {
		return lockedOwnerGraph{}, err
	}
	graph.Steps = append(graph.Steps, graphLockTurn)
	graph.Turn, err = tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, turnID,
	)
	if err != nil {
		return lockedOwnerGraph{}, err
	}
	lockedSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
	if !ok || graph.Turn.Kind != enum.KindTurn ||
		graph.Turn.Version != candidateTurn.Version ||
		lockedSpec.SessionID != candidateSpec.SessionID ||
		lockedSpec.ProcessRunID != candidateSpec.ProcessRunID ||
		lockedSpec.Attempt != candidateSpec.Attempt ||
		lockedSpec.EffectiveInputSHA256 != candidateSpec.EffectiveInputSHA256 {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}
	if hasProcess {
		graph.Steps = append(graph.Steps, graphLockProcessRun)
		graph.Process, err = tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID,
			candidateSpec.ProcessRunID,
		)
		if err != nil {
			return lockedOwnerGraph{}, err
		}
		processSpec, ok := graph.Process.Spec.(entity.ProcessRunSpec)
		if !ok || graph.Process.Kind != enum.KindProcessRun ||
			(scheduled && (processSpec.OccurrenceID != graph.Occurrence.ID ||
				processSpec.ScheduleID != graph.Schedule.ID)) ||
			(!scheduled && (processSpec.OccurrenceID != "" || processSpec.ScheduleID != "")) {
			return lockedOwnerGraph{}, errs.ErrStateConflict
		}
	}
	if scheduled && graph.Occurrence.ExecutionSessionID != graph.Session.ID {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}
	if graph.Runtime != nil &&
		(graph.Runtime.SessionID != graph.Session.ID ||
			(hasProcess && graph.Runtime.ProcessID != graph.Process.ID)) {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}
	if !slices.Equal(
		graph.Steps, ownerGraphLockPlan(graph.Runtime != nil, scheduled, hasProcess),
	) {
		return lockedOwnerGraph{}, errs.ErrInternal
	}
	return graph, nil
}

// lockOwnerGraphByProcess выводит current Turn только из server-owned
// ProcessRunSpec, а затем использует тот же единственный acquisition path.
// Caller-selected process ID после locks повторно сверяется с exact lineage.
func (service *Service) lockOwnerGraphByProcess(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	processID string,
) (lockedOwnerGraph, error) {
	candidate, err := tx.Get(
		ctx, principal.OrganizationID, principal.ProjectID, processID,
	)
	if err != nil {
		return lockedOwnerGraph{}, err
	}
	spec, ok := candidate.Spec.(entity.ProcessRunSpec)
	if !ok || candidate.Kind != enum.KindProcessRun {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}
	current, err := currentExecution(spec)
	if err != nil || value.ValidateID(current.TurnID) != nil {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, current.TurnID)
	if err != nil {
		return lockedOwnerGraph{}, err
	}
	lockedSpec, ok := graph.Process.Spec.(entity.ProcessRunSpec)
	lockedCurrent, currentErr := currentExecution(lockedSpec)
	if !ok || currentErr != nil || graph.Process.ID != processID ||
		graph.Process.Version != candidate.Version ||
		lockedCurrent != current || graph.Turn.ID != current.TurnID ||
		graph.Session.ID != current.SessionID {
		return lockedOwnerGraph{}, errs.ErrStateConflict
	}
	return graph, nil
}
