package resource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

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
	graph, err := service.resolveRunGraph(ctx, principal, processRunID)
	if err != nil {
		return RunLineageResult{}, err
	}
	result := RunLineageResult{RootSessionID: spec.RootSessionID, RootTurnID: spec.RootTurnID,
		ParentProcessRunID: spec.ParentProcessRunID, CurrentSessionID: tuple.SessionID,
		CurrentSessionVersion: tuple.SessionVersion, CurrentTurnID: tuple.TurnID,
		CurrentTurnVersion: tuple.TurnVersion, CurrentAttempt: tuple.Attempt,
		RuntimeRevisionID: tuple.RuntimeRevisionID, RuntimeRevisionVersion: tuple.RuntimeRevisionVersion,
		ImmutableInputSHA256: tuple.InputSHA256, Complete: true}
	for _, node := range graph {
		switch node.NodeType {
		case "PROCESS":
			result.Processes = append(result.Processes, node)
			if node.ParentProcessRunID == "" {
				result.RootProcessRunID = node.ID
			}
		case "ATTEMPT":
			result.Attempts = append(result.Attempts, node)
		}
	}
	linkRunLineage(&result)
	if result.RootProcessRunID == "" || len(result.Processes) == 0 {
		return RunLineageResult{}, errs.ErrStateConflict
	}
	return result, nil
}

func linkRunLineage(result *RunLineageResult) {
	processIndexes := make(map[string]int, len(result.Processes))
	for index := range result.Processes {
		processIndexes[result.Processes[index].ID] = index
	}
	for index := range result.Processes {
		parentID := result.Processes[index].ParentProcessRunID
		if parentIndex, ok := processIndexes[parentID]; ok {
			result.Processes[parentIndex].ChildIDs = append(
				result.Processes[parentIndex].ChildIDs,
				result.Processes[index].ID,
			)
		}
	}
	for index := range result.Processes {
		sort.Strings(result.Processes[index].ChildIDs)
	}
	attemptIndexes := make(map[string][]int, len(result.Attempts))
	for index := range result.Attempts {
		attemptIndexes[result.Attempts[index].TurnID] = append(attemptIndexes[result.Attempts[index].TurnID], index)
	}
	for _, indexes := range attemptIndexes {
		sort.Slice(indexes, func(left, right int) bool {
			leftAttempt, rightAttempt := result.Attempts[indexes[left]], result.Attempts[indexes[right]]
			if leftAttempt.Attempt == rightAttempt.Attempt {
				return leftAttempt.ID < rightAttempt.ID
			}
			return leftAttempt.Attempt < rightAttempt.Attempt
		})
		for position, index := range indexes {
			if position > 0 {
				result.Attempts[index].PredecessorID = result.Attempts[indexes[position-1]].ID
			}
			if position+1 < len(indexes) {
				result.Attempts[index].SuccessorID = result.Attempts[indexes[position+1]].ID
			}
		}
	}
}

func (service *Service) resolveRunGraph(ctx context.Context, principal value.Principal,
	processRunID string,
) ([]domainrepo.RunGraphNode, error) {
	if value.ValidateID(processRunID) != nil {
		return nil, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.RunGraphRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	nodes, err := repository.ListRunGraphNodes(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, processRunID)
	if err != nil {
		return nil, err
	}
	foundRequested := false
	for _, node := range nodes {
		if node.NodeType == "PROCESS" && node.ID == processRunID {
			foundRequested = true
		}
	}
	if !foundRequested {
		return nil, errs.ErrNotFound
	}
	return nodes, nil
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

type runTimelineCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

func (service *Service) ListRunTimeline(
	ctx context.Context,
	principal value.Principal,
	processRunID, pageToken string,
	limit int,
) ([]domainrepo.Audit, string, error) {
	if err := authorize(principal, permissionAuditRead); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		return nil, "", errs.ErrInvalidInput
	}
	cursor, err := decodeRunTimelineCursor(pageToken)
	if err != nil {
		return nil, "", err
	}
	_, _, err = service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return nil, "", err
	}
	graph, err := service.resolveRunGraph(ctx, principal, processRunID)
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(graph)*5)
	for _, node := range graph {
		ids = append(ids, node.ID)
		for _, id := range []string{node.ProcessRunID, node.SessionID, node.TurnID, node.RuntimeRevisionID} {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	repository, ok := service.repository.(domainrepo.RunTimelineRepository)
	if !ok {
		return nil, "", errs.ErrInternal
	}
	result, err := repository.ListRunTimelineAudit(
		ctx, principal.OrganizationID, principal.ProjectID, principal.ActorID,
		ids, cursor.OccurredAt, cursor.ID, limit,
	)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(result) == limit {
		next, err = encodeRunTimelineCursor(runTimelineCursor{
			OccurredAt: result[len(result)-1].OccurredAt,
			ID:         result[len(result)-1].ID,
		})
		if err != nil {
			return nil, "", err
		}
	}
	return result, next, nil
}

func decodeRunTimelineCursor(token string) (runTimelineCursor, error) {
	if token == "" {
		return runTimelineCursor{}, nil
	}
	if len(token) > 512 {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var cursor runTimelineCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.OccurredAt.IsZero() ||
		value.ValidateID(cursor.ID) != nil {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	return cursor, nil
}

func encodeRunTimelineCursor(cursor runTimelineCursor) (string, error) {
	if cursor.OccurredAt.IsZero() || value.ValidateID(cursor.ID) != nil {
		return "", errs.ErrInternal
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", errs.ErrInternal
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (service *Service) ListRunArtifacts(ctx context.Context, principal value.Principal, processRunID, pageToken string, limit int) ([]entity.Resource, string, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		return nil, "", errs.ErrInvalidInput
	}
	cursor, err := decodeRunTimelineCursor(pageToken)
	if err != nil {
		return nil, "", err
	}
	if _, _, err := service.resolveRun(ctx, principal, processRunID); err != nil {
		return nil, "", err
	}
	repository, ok := service.repository.(domainrepo.RunGraphRepository)
	if !ok {
		return nil, "", errs.ErrUnavailable
	}
	found, err := repository.ListRunGraphArtifacts(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, processRunID, cursor.OccurredAt, cursor.ID, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(found) > limit {
		found = found[:limit]
		last := found[len(found)-1]
		next, err = encodeRunTimelineCursor(runTimelineCursor{OccurredAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, "", err
		}
	}
	return filterOwnerBoundResources(found, principal.ActorID), next, nil
}
