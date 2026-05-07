package casters

import (
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
)

// SlotResponse maps a domain slot to a gRPC response.
func SlotResponse(slot entity.Slot) *runtimev1.SlotResponse {
	return &runtimev1.SlotResponse{Slot: SlotToProto(slot)}
}

// ListSlotsResponse maps a domain slot page to a gRPC response.
func ListSlotsResponse(result runtimeservice.ListSlotsResult) *runtimev1.ListSlotsResponse {
	slots := make([]*runtimev1.Slot, 0, len(result.Slots))
	for _, slot := range result.Slots {
		slots = append(slots, SlotToProto(slot))
	}
	return &runtimev1.ListSlotsResponse{Slots: slots, Page: pageResponseToProto(result.Page)}
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

func uuidStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
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
