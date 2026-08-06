package resource

import (
	"context"
	"errors"
	"slices"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

// ManageRun маршрутизирует закрытые cancel/retry действия в уже авторитетные
// owner graph команды, не вводя второй lifecycle path.
func (service *Service) ManageRun(ctx context.Context, input ManageRunInput) (ManageRunResult, error) {
	if err := authorize(input.Principal, permissionRunManage); err != nil {
		return ManageRunResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProcessRunID) != nil || input.ExpectedVersion == 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil ||
		(input.Action != "cancel" && input.Action != "retry") {
		return ManageRunResult{}, errs.ErrInvalidInput
	}
	process, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.ProcessRunID, enum.KindProcessRun)
	if err != nil {
		return ManageRunResult{}, err
	}
	if process.OwnerActorID != input.Principal.ActorID {
		return ManageRunResult{}, errs.ErrNotFound
	}
	if input.Action == "cancel" {
		if process.Version != input.ExpectedVersion {
			return ManageRunResult{}, errs.ErrVersionMismatch
		}
		principal := input.Principal
		principal.Permission = permissionCancelProcess
		cancelled, err := service.CancelProcess(ctx, CancelProcessInput{Principal: principal,
			IdempotencyKey: input.IdempotencyKey, ProcessRunID: input.ProcessRunID,
			ExpectedVersion: input.ExpectedVersion, ReasonCode: input.ReasonCode})
		return ManageRunResult{ProcessRun: cancelled}, err
	}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok {
		return ManageRunResult{}, errs.ErrStateConflict
	}
	current, err := currentExecution(spec)
	if err != nil || current.TurnID == "" {
		return ManageRunResult{}, errs.ErrStateConflict
	}
	turn, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		current.TurnID, enum.KindTurn)
	if err != nil || turn.OwnerActorID != input.Principal.ActorID || turn.Version != current.TurnVersion {
		if err != nil {
			return ManageRunResult{}, err
		}
		return ManageRunResult{}, errs.ErrStateConflict
	}
	if turn.Version != input.ExpectedVersion {
		return ManageRunResult{}, errs.ErrVersionMismatch
	}
	principal := input.Principal
	principal.Permission = permissionRetryTurn
	retried, err := service.RetryTurn(ctx, RetryTurnInput{Principal: principal,
		IdempotencyKey: input.IdempotencyKey, TurnID: turn.ID, ExpectedVersion: input.ExpectedVersion,
		ReasonCode: input.ReasonCode})
	if err != nil {
		return ManageRunResult{}, err
	}
	updated, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.ProcessRunID, enum.KindProcessRun)
	if err != nil {
		return ManageRunResult{}, err
	}
	return ManageRunResult{ProcessRun: updated, SuccessorTurn: retried}, nil
}

func (service *Service) GetRunDetail(
	ctx context.Context,
	principal value.Principal,
	processRunID string,
) (RunDetailResult, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return RunDetailResult{}, err
	}
	process, tuple, err := service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return RunDetailResult{}, err
	}
	result := RunDetailResult{ProcessRun: process}
	for _, target := range []struct {
		id   string
		kind enum.Kind
		set  func(entity.Resource)
	}{
		{tuple.SessionID, enum.KindSession, func(item entity.Resource) { result.Session = item }},
		{tuple.TurnID, enum.KindTurn, func(item entity.Resource) { result.Turn = item }},
		{tuple.RuntimeRevisionID, enum.KindRuntimeRevision, func(item entity.Resource) { result.RuntimeRevision = item }},
	} {
		if target.id == "" {
			continue
		}
		item, getErr := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID, target.id, target.kind)
		if getErr != nil || item.OwnerActorID != principal.ActorID {
			if getErr != nil {
				return RunDetailResult{}, getErr
			}
			return RunDetailResult{}, errs.ErrNotFound
		}
		target.set(item)
	}
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		execution, executionErr := tx.GetRuntimeExecutionByTurn(ctx, tuple.TurnID, tuple.Attempt)
		if executionErr == nil {
			if execution.ProcessID != process.ID || execution.SessionID != tuple.SessionID ||
				execution.RuntimeRevisionID != tuple.RuntimeRevisionID || execution.ImmutableInputSHA256 != tuple.InputSHA256 {
				return errs.ErrStateConflict
			}
			result.Runtime = &execution
			return nil
		}
		if errors.Is(executionErr, errs.ErrNotFound) {
			return nil
		}
		return executionErr
	})
	if err != nil {
		return RunDetailResult{}, err
	}
	incidents, err := service.repository.ListRuntimeIncidents(ctx, query.RuntimeIncidentFilter{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID, Limit: 100,
	})
	if err != nil {
		return RunDetailResult{}, err
	}
	if result.Runtime != nil {
		for _, incident := range incidents {
			if incident.ExecutionID == result.Runtime.ID {
				result.Incidents = append(result.Incidents, incident)
			}
		}
	}
	return result, nil
}

func (service *Service) GetRunLineage(ctx context.Context, principal value.Principal, processRunID string) (RunLineageResult, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return RunLineageResult{}, err
	}
	process, tuple, err := service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return RunLineageResult{}, err
	}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok {
		return RunLineageResult{}, errs.ErrStateConflict
	}
	return RunLineageResult{RootSessionID: spec.RootSessionID, RootTurnID: spec.RootTurnID,
		ParentProcessRunID: spec.ParentProcessRunID, CurrentSessionID: tuple.SessionID,
		CurrentSessionVersion: tuple.SessionVersion, CurrentTurnID: tuple.TurnID,
		CurrentTurnVersion: tuple.TurnVersion, CurrentAttempt: tuple.Attempt,
		RuntimeRevisionID: tuple.RuntimeRevisionID, RuntimeRevisionVersion: tuple.RuntimeRevisionVersion,
		ImmutableInputSHA256: tuple.InputSHA256}, nil
}

func (service *Service) resolveRun(ctx context.Context, principal value.Principal, processRunID string) (entity.Resource, executionTuple, error) {
	if value.ValidateID(processRunID) != nil {
		return entity.Resource{}, executionTuple{}, errs.ErrInvalidInput
	}
	process, err := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID, processRunID, enum.KindProcessRun)
	if err != nil {
		return entity.Resource{}, executionTuple{}, err
	}
	if process.OwnerActorID != principal.ActorID {
		return entity.Resource{}, executionTuple{}, errs.ErrNotFound
	}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok {
		return entity.Resource{}, executionTuple{}, errs.ErrStateConflict
	}
	tuple, err := currentExecution(spec)
	if err != nil || tuple.SessionID == "" || tuple.TurnID == "" {
		return entity.Resource{}, executionTuple{}, errs.ErrStateConflict
	}
	return process, tuple, nil
}

func (service *Service) ListRunTimeline(ctx context.Context, principal value.Principal, processRunID, afterID string, limit int) ([]domainrepo.Audit, error) {
	if err := authorize(principal, permissionAuditRead); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || afterID != "" && value.ValidateID(afterID) != nil {
		return nil, errs.ErrInvalidInput
	}
	process, tuple, err := service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return nil, err
	}
	ids := []string{process.ID, tuple.SessionID, tuple.TurnID, tuple.RuntimeRevisionID}
	result := make([]domainrepo.Audit, 0, limit)
	for _, id := range ids {
		if id == "" {
			continue
		}
		entries, listErr := service.repository.ListAudit(ctx, query.AuditFilter{OrganizationID: principal.OrganizationID,
			ProjectID: principal.ProjectID, ResourceID: id, AfterID: afterID, Limit: limit})
		if listErr != nil {
			return nil, listErr
		}
		result = append(result, entries...)
	}
	slices.SortFunc(result, func(left, right domainrepo.Audit) int {
		return compareText(left.ID, right.ID)
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (service *Service) ListRunArtifacts(ctx context.Context, principal value.Principal, processRunID, afterID string, limit int) ([]entity.Resource, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || afterID != "" && value.ValidateID(afterID) != nil {
		return nil, errs.ErrInvalidInput
	}
	process, tuple, err := service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return nil, err
	}
	found := make([]entity.Resource, 0, limit)
	for _, parentID := range []string{process.ID, tuple.SessionID, tuple.TurnID} {
		items, listErr := service.repository.List(ctx, query.ResourceFilter{OrganizationID: principal.OrganizationID,
			ProjectID: principal.ProjectID, ActorID: principal.ActorID, ParentID: parentID,
			Kind: enum.KindArtifact, AfterID: afterID, Limit: limit})
		if listErr != nil {
			return nil, listErr
		}
		found = append(found, items...)
	}
	slices.SortFunc(found, func(left, right entity.Resource) int { return compareText(left.ID, right.ID) })
	if len(found) > limit {
		found = found[:limit]
	}
	return filterOwnerBoundResources(found, principal.ActorID), nil
}
