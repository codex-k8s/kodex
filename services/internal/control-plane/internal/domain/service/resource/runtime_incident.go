package resource

import (
	"context"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

var runtimeIncidentTransitions = map[string]map[string]string{
	"OPEN":         {"acknowledge": "ACKNOWLEDGED", "retry": "RETRYING"},
	"ACKNOWLEDGED": {"retry": "RETRYING", "release": "RELEASED", "close": "CLOSED"},
	"RETRYING":     {"close": "CLOSED"},
	"RELEASED":     {"retry": "RETRYING", "close": "CLOSED"},
}

// ManageRuntimeIncident применяет закрытый action registry после owner graph
// resolution. Retry материализует fresh RuntimeRevision/attempt в той же tx.
func (service *Service) ManageRuntimeIncident(
	ctx context.Context,
	input ManageRuntimeIncidentInput,
) (ManageRuntimeIncidentResult, error) {
	if err := authorize(input.Principal, permissionRuntimeIncidentManage); err != nil {
		return ManageRuntimeIncidentResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.IncidentID) != nil ||
		input.ExpectedVersion == 0 || value.ValidateStableKey(input.ReasonCode) != nil ||
		!slices.Contains([]string{"acknowledge", "retry", "release", "close"}, input.Action) {
		return ManageRuntimeIncidentResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		IncidentID      string
		ExpectedVersion uint64
		Action          string
		ReasonCode      string
	}{identity(input.Principal), input.IncidentID, input.ExpectedVersion, input.Action, input.ReasonCode})
	if err != nil {
		return ManageRuntimeIncidentResult{}, errs.ErrInvalidInput
	}
	var result ManageRuntimeIncidentResult
	var lockedIncident domainrepo.RuntimeIncident
	var lockedExecution RuntimeExecution
	var lockedOwnerActorID string
	var targetState string
	err = service.withLifecycleReceipt(ctx, input.Principal, input.IdempotencyKey,
		"manage_runtime_incident_"+input.Action, requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			protected, ok := tx.(domainrepo.ProtectedTransaction)
			if !ok {
				return 0, errs.ErrInternal
			}
			incident, err := protected.GetRuntimeIncidentForUpdate(ctx, input.IncidentID)
			if err != nil {
				return 0, err
			}
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, incident.ExecutionID)
			if err != nil {
				return 0, err
			}
			graph, err := service.lockOwnerGraphByTurn(ctx, tx, input.Principal, execution.TurnID)
			if err != nil || graph.Process.OwnerActorID == "" ||
				graph.Process.ID != execution.ProcessID || graph.Turn.ID != execution.TurnID ||
				incident.ExecutionFence > execution.Fence {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrNotFound
			}
			lockedIncident, lockedExecution = incident, execution
			lockedOwnerActorID = graph.Process.OwnerActorID
			replayState := map[string]string{
				"acknowledge": "ACKNOWLEDGED", "retry": "RETRYING",
				"release": "RELEASED", "close": "CLOSED",
			}[input.Action]
			if incident.Version == input.ExpectedVersion+1 {
				if incident.State == replayState && incident.ReasonCode == input.ReasonCode {
					targetState = replayState
					return lifecycleReceiptReplay, nil
				}
				return 0, errs.ErrVersionMismatch
			}
			if incident.Version != input.ExpectedVersion {
				return 0, errs.ErrVersionMismatch
			}
			currentExecution := graph.Runtime != nil && graph.Runtime.ID == execution.ID
			switch input.Action {
			case "retry":
				if !currentExecution || incident.ExecutionFence != execution.Fence {
					return 0, errs.ErrStateConflict
				}
			case "release":
				if !currentExecution || runtimeTerminal(execution.State) {
					return 0, errs.ErrStateConflict
				}
			case "acknowledge":
				if !currentExecution && !runtimeTerminal(execution.State) {
					return 0, errs.ErrStateConflict
				}
			case "close":
				if !runtimeTerminal(execution.State) {
					return 0, errs.ErrStateConflict
				}
			}
			targetState = runtimeIncidentTransitions[incident.State][input.Action]
			if targetState == "" {
				return 0, errs.ErrStateConflict
			}
			return lifecycleReceiptApply, nil
		},
		func() error {
			if result.Incident.ID != lockedIncident.ID || result.Incident.Version != lockedIncident.Version ||
				result.Incident.State != lockedIncident.State || result.Incident.ReasonCode != lockedIncident.ReasonCode {
				return errs.ErrStateConflict
			}
			if input.Action == "retry" && result.SuccessorTurn.ID == "" {
				return errs.ErrStateConflict
			}
			if input.Action == "release" && (result.ReleasedExecution == nil ||
				result.ReleasedExecution.State != "CANCELLED" ||
				result.ReleasedExecution.TerminalReference != input.ReasonCode) {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			protected, ok := tx.(domainrepo.ProtectedTransaction)
			if !ok {
				return errs.ErrInternal
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			if input.Action == "retry" {
				ownerPrincipal := input.Principal
				ownerPrincipal.ActorID = lockedOwnerActorID
				retried, err := service.retryWorkspaceExecution(ctx, tx, ownerPrincipal, lockedExecution, nil, now)
				if err != nil {
					return err
				}
				result.SuccessorTurn = retried.Turn
			}
			if input.Action == "release" {
				released, err := service.terminateRuntimeGraphForOwner(
					ctx, tx, input.Principal, lockedOwnerActorID, lockedExecution, input.ReasonCode,
					"release_runtime_incident_graph", now,
				)
				if err != nil {
					return err
				}
				result.ReleasedExecution = &released
			}
			updated := lockedIncident
			updated.Version++
			updated.State, updated.ReasonCode, updated.UpdatedAt = targetState, input.ReasonCode, now
			if err := protected.UpdateRuntimeIncident(ctx, updated, lockedIncident.Version); err != nil {
				return err
			}
			if err := protected.AppendRuntimeIncidentHistory(ctx, domainrepo.RuntimeIncidentHistory{
				IncidentID: updated.ID, Version: updated.Version, State: updated.State,
				Action: input.Action, ReasonCode: input.ReasonCode, OccurredAt: now,
				OwnerActorID: lockedOwnerActorID,
			}); err != nil {
				return err
			}
			if err := appendOwnerStateAudit(ctx, tx, input.Principal, "manage_runtime_incident_"+input.Action,
				updated.OrganizationID, updated.ProjectID, updated.ID, "RUNTIME_INCIDENT", updated.Version, now); err != nil {
				return err
			}
			result.Incident = updated
			return nil
		})
	return result, err
}

// terminateRuntimeGraphForOwner закрывает Turn,
// Process, lease/grants/claims и сам RuntimeExecution в одной owner-транзакции.
// actionPrincipal остаётся оператором для audit, а ownerActorID берётся только
// из уже заблокированного Process graph и не доверяется caller payload.
func (service *Service) terminateRuntimeGraphForOwner(
	ctx context.Context,
	tx domainrepo.Transaction,
	actionPrincipal value.Principal,
	ownerActorID string,
	execution RuntimeExecution,
	reason string,
	auditAction string,
	now time.Time,
) (RuntimeExecution, error) {
	if ownerActorID == "" || runtimeTerminal(execution.State) ||
		(execution.State != "ADMITTED" && execution.State != "RUNNING") {
		return RuntimeExecution{}, errs.ErrStateConflict
	}
	ownerPrincipal := actionPrincipal
	ownerPrincipal.ActorID = ownerActorID
	closedTurn, err := service.closeRuntimeGraph(
		ctx, tx, ownerPrincipal, execution, enum.StateCancelled, reason, now, nil,
	)
	if err != nil {
		return RuntimeExecution{}, err
	}
	if err := service.completeRuntimeProcessFromTurn(ctx, tx, ownerPrincipal, closedTurn); err != nil {
		return RuntimeExecution{}, err
	}
	expectedVersion, expectedFence := execution.Version, execution.Fence
	execution.Version++
	execution.Fence++
	execution.State = "CANCELLED"
	if err := pinRuntimeRetention(&execution, now); err != nil {
		return RuntimeExecution{}, err
	}
	execution.TerminalOutcome = "CANCELLED"
	execution.TerminalReference = reason
	execution.TerminalSHA256 = hashString(reason)
	execution.LeaseID = ""
	execution.LeaseTokenSHA256 = ""
	execution.LeaseExpiresAt = time.Time{}
	execution.UpdatedAt = now
	if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
		return RuntimeExecution{}, err
	}
	if err := service.appendLifecycleAudit(
		ctx, tx, actionPrincipal, auditAction, execution.ID,
		"RUNTIME_EXECUTION", execution.Version, now,
	); err != nil {
		return RuntimeExecution{}, err
	}
	return execution, nil
}

func (service *Service) GetRuntimeIncident(
	ctx context.Context,
	input GetRuntimeIncidentInput,
) (domainrepo.RuntimeIncident, error) {
	if err := authorize(input.Principal, permissionRuntimeIncidentRead); err != nil {
		return domainrepo.RuntimeIncident{}, err
	}
	if value.ValidateID(input.IncidentID) != nil || input.Principal.ProjectID == "" {
		return domainrepo.RuntimeIncident{}, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.ProtectedRepository)
	if !ok {
		return domainrepo.RuntimeIncident{}, errs.ErrInternal
	}
	return repository.GetRuntimeIncident(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Principal.ActorID, input.IncidentID)
}

func (service *Service) ListRuntimeIncidentHistory(
	ctx context.Context,
	input ListRuntimeIncidentHistoryInput,
) ([]domainrepo.RuntimeIncidentHistory, error) {
	if err := authorize(input.Principal, permissionRuntimeIncidentRead); err != nil {
		return nil, err
	}
	if value.ValidateID(input.IncidentID) != nil || input.Principal.ProjectID == "" ||
		input.Limit < 1 || input.Limit > 100 {
		return nil, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.ProtectedRepository)
	if !ok {
		return nil, errs.ErrInternal
	}
	return repository.ListRuntimeIncidentHistory(ctx, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.Principal.ActorID, input.IncidentID, input.BeforeVersion, input.Limit)
}
