package resource

import (
	"context"
	"errors"
	"reflect"
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

type lockedOwnerGraphSet struct {
	ByTurn   map[string]lockedOwnerGraph
	Sessions map[string]entity.Resource
}

type ownerGraphCandidate struct {
	Turn       entity.Resource
	TurnSpec   entity.TurnSpec
	Occurrence domainrepo.ScheduleOccurrence
	Scheduled  bool
}

type lockedSessionLifecycle struct {
	Session entity.Resource
	Graphs  []lockedOwnerGraph
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
	locked, err := service.lockOwnerGraphSet(ctx, tx, principal, []string{turnID}, nil)
	if err != nil {
		return lockedOwnerGraph{}, err
	}
	graph, ok := locked.ByTurn[turnID]
	if !ok {
		return lockedOwnerGraph{}, errs.ErrInternal
	}
	return graph, nil
}

// lockOwnerGraphSet получает любое число пересекающихся execution graph одним
// глобальным порядком. Это необходимо для cross-session delegation и session
// lifecycle: последовательный вызов single-graph resolver мог бы удерживать
// Session первого графа и ждать RuntimeExecution второго.
func (service *Service) lockOwnerGraphSet(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turnIDs []string,
	extraSessionIDs []string,
) (lockedOwnerGraphSet, error) {
	turnIDs = append([]string(nil), turnIDs...)
	slices.Sort(turnIDs)
	turnIDs = slices.Compact(turnIDs)
	extraSessionIDs = append([]string(nil), extraSessionIDs...)
	slices.Sort(extraSessionIDs)
	extraSessionIDs = slices.Compact(extraSessionIDs)

	candidates := make(map[string]ownerGraphCandidate, len(turnIDs))
	for _, turnID := range turnIDs {
		candidateTurn, err := tx.Get(
			ctx, principal.OrganizationID, principal.ProjectID, turnID,
		)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		candidateSpec, ok := candidateTurn.Spec.(entity.TurnSpec)
		if !ok || candidateTurn.Kind != enum.KindTurn ||
			value.ValidateID(candidateSpec.SessionID) != nil {
			return lockedOwnerGraphSet{}, errs.ErrStateConflict
		}
		candidate := ownerGraphCandidate{Turn: candidateTurn, TurnSpec: candidateSpec}
		occurrence, occurrenceErr := tx.GetScheduleOccurrenceByCurrentTurn(
			ctx, principal.OrganizationID, principal.ProjectID, turnID,
		)
		if occurrenceErr == nil {
			candidate.Occurrence = occurrence
			candidate.Scheduled = true
		} else if !errors.Is(occurrenceErr, errs.ErrNotFound) {
			return lockedOwnerGraphSet{}, occurrenceErr
		}
		candidates[turnID] = candidate
	}

	result := lockedOwnerGraphSet{
		ByTurn:   make(map[string]lockedOwnerGraph, len(candidates)),
		Sessions: make(map[string]entity.Resource, len(candidates)+len(extraSessionIDs)),
	}
	for _, turnID := range turnIDs {
		candidate := candidates[turnID]
		graph := lockedOwnerGraph{Steps: make([]string, 0, 7)}
		execution, err := tx.GetRuntimeExecutionByTurnForUpdate(
			ctx, turnID, candidate.TurnSpec.Attempt,
		)
		if err == nil {
			if execution.OrganizationID != principal.OrganizationID ||
				execution.ProjectID != principal.ProjectID || execution.TurnID != turnID ||
				execution.Attempt != candidate.TurnSpec.Attempt ||
				execution.ImmutableInputSHA256 != candidate.TurnSpec.EffectiveInputSHA256 {
				return lockedOwnerGraphSet{}, errs.ErrStateConflict
			}
			graph.Runtime = &execution
			graph.Steps = append(graph.Steps, graphLockRuntimeExecution)
		} else if !errors.Is(err, errs.ErrNotFound) {
			return lockedOwnerGraphSet{}, err
		}
		result.ByTurn[turnID] = graph
	}

	occurrenceIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Scheduled {
			occurrenceIDs = append(occurrenceIDs, candidate.Occurrence.ID)
		}
	}
	slices.Sort(occurrenceIDs)
	lockedOccurrences := make(map[string]domainrepo.ScheduleOccurrence, len(occurrenceIDs))
	for _, occurrenceID := range slices.Compact(occurrenceIDs) {
		occurrence, err := tx.GetScheduleOccurrenceForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, occurrenceID,
		)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		lockedOccurrences[occurrenceID] = occurrence
	}

	scheduleIDs := make([]string, 0, len(lockedOccurrences))
	for _, occurrence := range lockedOccurrences {
		scheduleIDs = append(scheduleIDs, occurrence.ScheduleID)
	}
	slices.Sort(scheduleIDs)
	lockedSchedules := make(map[string]entity.Resource, len(scheduleIDs))
	for _, scheduleID := range slices.Compact(scheduleIDs) {
		schedule, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, scheduleID,
		)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		lockedSchedules[scheduleID] = schedule
	}
	lockedRuns := make(map[string]domainrepo.ScheduledRun, len(lockedOccurrences))
	for _, occurrenceID := range slices.Compact(occurrenceIDs) {
		occurrence := lockedOccurrences[occurrenceID]
		run, err := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		lockedRuns[occurrenceID] = run
	}

	sessionIDs := append([]string(nil), extraSessionIDs...)
	for _, candidate := range candidates {
		sessionIDs = append(sessionIDs, candidate.TurnSpec.SessionID)
	}
	slices.Sort(sessionIDs)
	for _, sessionID := range slices.Compact(sessionIDs) {
		session, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, sessionID,
		)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		result.Sessions[sessionID] = session
	}

	lockedTurns := make(map[string]entity.Resource, len(turnIDs))
	for _, turnID := range turnIDs {
		turn, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, turnID,
		)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		lockedTurns[turnID] = turn
	}

	processIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.TurnSpec.ProcessRunID != "" {
			processIDs = append(processIDs, candidate.TurnSpec.ProcessRunID)
		}
	}
	slices.Sort(processIDs)
	lockedProcesses := make(map[string]entity.Resource, len(processIDs))
	for _, processID := range slices.Compact(processIDs) {
		process, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, processID,
		)
		if err != nil {
			return lockedOwnerGraphSet{}, err
		}
		lockedProcesses[processID] = process
	}

	for _, turnID := range turnIDs {
		candidate := candidates[turnID]
		graph := result.ByTurn[turnID]
		graph.Session = result.Sessions[candidate.TurnSpec.SessionID]
		graph.Turn = lockedTurns[turnID]
		lockedSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
		if !ok || graph.Turn.Kind != enum.KindTurn ||
			graph.Turn.Version != candidate.Turn.Version || !reflect.DeepEqual(lockedSpec, candidate.TurnSpec) {
			return lockedOwnerGraphSet{}, errs.ErrStateConflict
		}
		if candidate.Scheduled {
			graph.Occurrence = lockedOccurrences[candidate.Occurrence.ID]
			graph.Schedule = lockedSchedules[graph.Occurrence.ScheduleID]
			graph.Run = lockedRuns[graph.Occurrence.ID]
			if graph.Occurrence.ExecutionTurnID != turnID ||
				graph.Occurrence.ScheduleID != candidate.Occurrence.ScheduleID ||
				graph.Occurrence.Attempt != candidate.Occurrence.Attempt ||
				graph.Occurrence.State != candidate.Occurrence.State ||
				validateScheduledRunBinding(graph.Occurrence, graph.Run) != nil ||
				graph.Run.CurrentTurnID != turnID || graph.Run.State != graph.Occurrence.State ||
				graph.Occurrence.ExecutionSessionID != graph.Session.ID {
				return lockedOwnerGraphSet{}, errs.ErrStateConflict
			}
		}
		if lockedSpec.ProcessRunID != "" {
			graph.Process = lockedProcesses[lockedSpec.ProcessRunID]
			processSpec, ok := graph.Process.Spec.(entity.ProcessRunSpec)
			if !ok || graph.Process.Kind != enum.KindProcessRun ||
				(candidate.Scheduled && (processSpec.OccurrenceID != graph.Occurrence.ID ||
					processSpec.ScheduleID != graph.Schedule.ID)) ||
				(!candidate.Scheduled && (processSpec.OccurrenceID != "" ||
					processSpec.ScheduleID != "")) {
				return lockedOwnerGraphSet{}, errs.ErrStateConflict
			}
		}
		if graph.Runtime != nil && (graph.Runtime.SessionID != graph.Session.ID ||
			(lockedSpec.ProcessRunID != "" && graph.Runtime.ProcessID != graph.Process.ID)) {
			return lockedOwnerGraphSet{}, errs.ErrStateConflict
		}
		if graph.Runtime == nil {
			// Отсутствие строки не создаёт PostgreSQL gap lock. После Session/Turn
			// locks выполняется read-only recheck: concurrent creator либо уже
			// виден и команда закрыто отклоняется, либо ждёт Session и не сможет
			// materialize runtime до завершения текущего graph transition.
			_, runtimeErr := tx.GetRuntimeExecutionByTurn(
				ctx, turnID, lockedSpec.Attempt,
			)
			if runtimeErr == nil {
				return lockedOwnerGraphSet{}, errs.ErrStateConflict
			}
			if !errors.Is(runtimeErr, errs.ErrNotFound) {
				return lockedOwnerGraphSet{}, runtimeErr
			}
		}
		graph.Steps = ownerGraphLockPlan(
			graph.Runtime != nil, candidate.Scheduled, lockedSpec.ProcessRunID != "",
		)
		result.ByTurn[turnID] = graph
	}
	return result, nil
}

// lockSessionLifecycleGraph выполняет unlocked discovery полного набора
// открытых Turn, затем получает все их graph и Session одним глобальным
// порядком. Повторный discovery под Session lock запрещает phantom EnqueueTurn.
func (service *Service) lockSessionLifecycleGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	sessionID string,
) (lockedSessionLifecycle, error) {
	candidates, err := tx.OpenSessionTurns(
		ctx, principal.OrganizationID, principal.ProjectID, sessionID,
	)
	if err != nil {
		return lockedSessionLifecycle{}, err
	}
	turnIDs := make([]string, 0, len(candidates))
	candidateVersions := make(map[string]uint64, len(candidates))
	for _, candidate := range candidates {
		turnIDs = append(turnIDs, candidate.Turn.ID)
		candidateVersions[candidate.Turn.ID] = candidate.Turn.Version
	}
	slices.Sort(turnIDs)
	locked, err := service.lockOwnerGraphSet(
		ctx, tx, principal, turnIDs, []string{sessionID},
	)
	if err != nil {
		return lockedSessionLifecycle{}, err
	}
	session, ok := locked.Sessions[sessionID]
	if !ok || session.Kind != enum.KindSession {
		return lockedSessionLifecycle{}, errs.ErrStateConflict
	}
	rechecked, err := tx.OpenSessionTurns(
		ctx, principal.OrganizationID, principal.ProjectID, sessionID,
	)
	if err != nil {
		return lockedSessionLifecycle{}, err
	}
	if len(rechecked) != len(candidates) {
		return lockedSessionLifecycle{}, errs.ErrStateConflict
	}
	recheckedIDs := make([]string, 0, len(rechecked))
	for _, item := range rechecked {
		graph, exists := locked.ByTurn[item.Turn.ID]
		if !exists || graph.Session.ID != sessionID || graph.Turn.Version != item.Turn.Version ||
			candidateVersions[item.Turn.ID] != item.Turn.Version {
			return lockedSessionLifecycle{}, errs.ErrStateConflict
		}
		recheckedIDs = append(recheckedIDs, item.Turn.ID)
	}
	slices.Sort(recheckedIDs)
	if !slices.Equal(turnIDs, recheckedIDs) {
		return lockedSessionLifecycle{}, errs.ErrStateConflict
	}
	graphs := make([]lockedOwnerGraph, 0, len(turnIDs))
	for _, turnID := range turnIDs {
		graphs = append(graphs, locked.ByTurn[turnID])
	}
	return lockedSessionLifecycle{Session: session, Graphs: graphs}, nil
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
