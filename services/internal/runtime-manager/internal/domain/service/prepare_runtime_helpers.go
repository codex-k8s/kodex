package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

type prepareRuntimeResultPayload struct {
	WorkspaceMaterializationID uuid.UUID `json:"workspace_materialization_id"`
	PlacementFingerprint       string    `json:"placement_fingerprint,omitempty"`
}

func (s *Service) prepareRuntimeReplay(ctx context.Context, meta value.CommandMeta) (PrepareRuntimeResult, entity.CommandResult, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, operationPrepareRuntime, aggregateTypeSlot)
	if err != nil || !ok {
		return PrepareRuntimeResult{}, entity.CommandResult{}, ok, err
	}
	var payload prepareRuntimeResultPayload
	if err := json.Unmarshal(result.ResultPayload, &payload); err != nil || payload.WorkspaceMaterializationID == uuid.Nil {
		return PrepareRuntimeResult{}, result, true, errs.ErrConflict
	}
	slot, err := s.repository.GetSlot(ctx, result.AggregateID)
	if err != nil {
		return PrepareRuntimeResult{}, result, true, err
	}
	materialization, err := s.repository.GetWorkspaceMaterialization(ctx, payload.WorkspaceMaterializationID)
	if err != nil {
		return PrepareRuntimeResult{}, result, true, err
	}
	return PrepareRuntimeResult{Slot: slot, WorkspaceMaterialization: materialization, RuntimeContext: runtimeContext(slot, materialization)}, result, true, nil
}

func prepareRuntimeCommandPayload(workspaceMaterializationID uuid.UUID, placementFingerprint string) ([]byte, error) {
	return json.Marshal(prepareRuntimeResultPayload{
		WorkspaceMaterializationID: workspaceMaterializationID,
		PlacementFingerprint:       placementFingerprint,
	})
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
		WorkspacePVCRef:            workspacePVCRef(slot),
	}
}

func workspacePVCRef(slot entity.Slot) string {
	namespace := strings.TrimSpace(slot.NamespaceName)
	if namespace == "" {
		namespace = "kodex-rt-" + shortID(slot.ID)
	}
	return "pvc://" + namespace + "/runtime-workspace-" + shortID(slot.ID)
}
