package casters

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// PrepareRuntimeResponse maps a domain prepare result to a gRPC response.
func PrepareRuntimeResponse(result runtimeservice.PrepareRuntimeResult) *runtimev1.PrepareRuntimeResponse {
	return &runtimev1.PrepareRuntimeResponse{
		Slot:                     SlotToProto(result.Slot),
		WorkspaceMaterialization: WorkspaceMaterializationToProto(result.WorkspaceMaterialization),
		RuntimeContext:           RuntimeContextToProto(result.RuntimeContext),
	}
}

// SlotResponse maps a domain slot to a gRPC response.
func SlotResponse(slot entity.Slot) *runtimev1.SlotResponse {
	return &runtimev1.SlotResponse{Slot: SlotToProto(slot)}
}

// WorkspaceMaterializationResponse maps a domain materialization to a gRPC response.
func WorkspaceMaterializationResponse(materialization entity.WorkspaceMaterialization) *runtimev1.WorkspaceMaterializationResponse {
	return &runtimev1.WorkspaceMaterializationResponse{WorkspaceMaterialization: WorkspaceMaterializationToProto(materialization)}
}

// ListSlotsResponse maps a domain slot page to a gRPC response.
func ListSlotsResponse(result runtimeservice.ListSlotsResult) *runtimev1.ListSlotsResponse {
	return &runtimev1.ListSlotsResponse{Slots: slotsToProto(result.Slots), Page: pageResponseToProto(result.Page)}
}

// ListWorkspaceMaterializationsResponse maps a domain materialization page to a gRPC response.
func ListWorkspaceMaterializationsResponse(result runtimeservice.ListWorkspaceMaterializationsResult) *runtimev1.ListWorkspaceMaterializationsResponse {
	return &runtimev1.ListWorkspaceMaterializationsResponse{
		WorkspaceMaterializations: workspaceMaterializationsToProto(result.WorkspaceMaterializations),
		Page:                      pageResponseToProto(result.Page),
	}
}

// SlotToProto maps one domain slot.
func SlotToProto(slot entity.Slot) *runtimev1.Slot {
	return &runtimev1.Slot{
		SlotId:           slot.ID.String(),
		SlotKey:          slot.SlotKey,
		Status:           SlotStatusToProto(slot.Status),
		RuntimeMode:      RuntimeModeToProto(slot.RuntimeMode),
		IsPrewarmed:      slot.IsPrewarmed,
		FleetScopeId:     uuidStringPtr(slot.FleetScopeID),
		ClusterId:        uuidStringPtr(slot.ClusterID),
		NamespaceName:    slot.NamespaceName,
		AgentRunId:       uuidStringPtr(slot.AgentRunID),
		ProjectId:        uuidStringPtr(slot.ProjectID),
		RepositoryIds:    uuidStrings(slot.RepositoryIDs),
		RuntimeProfile:   slot.RuntimeProfile,
		Fingerprint:      slot.Fingerprint,
		LeaseOwner:       slot.LeaseOwner,
		LeaseUntil:       timeStringPtr(slot.LeaseUntil),
		LastErrorCode:    slot.LastErrorCode,
		LastErrorMessage: slot.LastErrorMessage,
		CreatedAt:        slot.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:        slot.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Version:          slot.Version,
	}
}

// WorkspaceMaterializationToProto maps one domain materialization attempt.
func WorkspaceMaterializationToProto(materialization entity.WorkspaceMaterialization) *runtimev1.WorkspaceMaterialization {
	return &runtimev1.WorkspaceMaterialization{
		WorkspaceMaterializationId: materialization.ID.String(),
		SlotId:                     materialization.SlotID.String(),
		Status:                     WorkspaceMaterializationStatusToProto(materialization.Status),
		PolicyDigest:               materialization.PolicyDigest,
		Sources:                    WorkspaceSourcesToProto(materialization.Sources),
		Fingerprint:                materialization.Fingerprint,
		StartedAt:                  timeStringPtr(materialization.StartedAt),
		FinishedAt:                 timeStringPtr(materialization.FinishedAt),
		LastErrorCode:              materialization.LastErrorCode,
		LastErrorMessage:           materialization.LastErrorMessage,
		Version:                    materialization.Version,
	}
}

// RuntimeContextToProto maps a prepared runtime context.
func RuntimeContextToProto(context runtimeservice.RuntimeContext) *runtimev1.RuntimeContext {
	return &runtimev1.RuntimeContext{
		SlotId:                     context.SlotID.String(),
		AgentRunId:                 uuidStringPtr(context.AgentRunID),
		FleetScopeId:               uuidStringPtr(context.FleetScopeID),
		ClusterId:                  uuidStringPtr(context.ClusterID),
		NamespaceName:              context.NamespaceName,
		RuntimeProfile:             context.RuntimeProfile,
		WorkspaceRoot:              context.WorkspaceRoot,
		MaterializationFingerprint: context.MaterializationFingerprint,
	}
}

// WorkspaceSourcesToProto maps normalized workspace sources.
func WorkspaceSourcesToProto(sources []value.WorkspaceSource) []*runtimev1.WorkspaceSource {
	if len(sources) == 0 {
		return nil
	}
	result := make([]*runtimev1.WorkspaceSource, len(sources))
	for index := range sources {
		result[index] = WorkspaceSourceToProto(sources[index])
	}
	return result
}

// WorkspaceSourceToProto maps one normalized workspace source.
func WorkspaceSourceToProto(source value.WorkspaceSource) *runtimev1.WorkspaceSource {
	return &runtimev1.WorkspaceSource{
		SourceId:      source.SourceID,
		Kind:          WorkspaceSourceKindToProto(source.Kind),
		RepositoryId:  uuidStringPtr(source.RepositoryID),
		Provider:      optionalStringPtr(source.Provider),
		ProviderOwner: optionalStringPtr(source.ProviderOwner),
		ProviderName:  optionalStringPtr(source.ProviderName),
		SourceRef:     optionalStringPtr(source.SourceRef),
		CommitSha:     optionalStringPtr(source.CommitSHA),
		LocalPath:     source.LocalPath,
		AccessMode:    WorkspaceSourceAccessModeToProto(source.AccessMode),
		Digest:        optionalStringPtr(source.Digest),
		MetadataJson:  workspaceSourceMetadata(source.Metadata),
	}
}

func uuidStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func workspaceSourceMetadata(metadata json.RawMessage) string {
	if len(metadata) == 0 {
		return "{}"
	}
	return string(metadata)
}

func slotsToProto(slots []entity.Slot) []*runtimev1.Slot {
	result := []*runtimev1.Slot{}
	for index := range slots {
		result = append(result, SlotToProto(slots[index]))
	}
	return result
}

func workspaceMaterializationsToProto(materializations []entity.WorkspaceMaterialization) []*runtimev1.WorkspaceMaterialization {
	result := make([]*runtimev1.WorkspaceMaterialization, 0, len(materializations))
	for _, materialization := range materializations {
		result = append(result, WorkspaceMaterializationToProto(materialization))
	}
	return result
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, id.String())
	}
	return result
}

func timeStringPtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	text := value.UTC().Format(time.RFC3339Nano)
	return &text
}
