package service

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

type prepareRuntimeResultPayload struct {
	WorkspaceMaterializationID uuid.UUID `json:"workspace_materialization_id"`
}

func (s *Service) prepareRuntimeReplay(ctx context.Context, meta value.CommandMeta) (PrepareRuntimeResult, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, operationPrepareRuntime, aggregateTypeSlot)
	if err != nil || !ok {
		return PrepareRuntimeResult{}, ok, err
	}
	var payload prepareRuntimeResultPayload
	if err := json.Unmarshal(result.ResultPayload, &payload); err != nil || payload.WorkspaceMaterializationID == uuid.Nil {
		return PrepareRuntimeResult{}, true, errs.ErrConflict
	}
	slot, err := s.repository.GetSlot(ctx, result.AggregateID)
	if err != nil {
		return PrepareRuntimeResult{}, true, err
	}
	materialization, err := s.repository.GetWorkspaceMaterialization(ctx, payload.WorkspaceMaterializationID)
	if err != nil {
		return PrepareRuntimeResult{}, true, err
	}
	return PrepareRuntimeResult{Slot: slot, WorkspaceMaterialization: materialization, RuntimeContext: runtimeContext(slot, materialization)}, true, nil
}

func prepareRuntimeCommandPayload(workspaceMaterializationID uuid.UUID) ([]byte, error) {
	return json.Marshal(prepareRuntimeResultPayload{WorkspaceMaterializationID: workspaceMaterializationID})
}

func runtimeContext(slot entity.Slot, materialization entity.WorkspaceMaterialization) RuntimeContext {
	return RuntimeContext{
		SlotID:                     slot.ID,
		AgentRunID:                 slot.AgentRunID,
		FleetScopeID:               slot.FleetScopeID,
		ClusterID:                  slot.ClusterID,
		NamespaceName:              slot.NamespaceName,
		RuntimeProfile:             slot.RuntimeProfile,
		WorkspaceRoot:              "/workspace",
		MaterializationFingerprint: materialization.Fingerprint,
	}
}
