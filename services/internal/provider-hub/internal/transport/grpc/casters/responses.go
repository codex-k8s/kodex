package casters

import (
	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
)

// WebhookEventResponse maps a stored webhook to gRPC.
func WebhookEventResponse(event entity.WebhookEvent) *providersv1.WebhookEventResponse {
	return &providersv1.WebhookEventResponse{WebhookEvent: WebhookEventToProto(event)}
}

func WebhookEventToProto(event entity.WebhookEvent) *providersv1.WebhookEvent {
	return &providersv1.WebhookEvent{
		WebhookEventId:       event.ID.String(),
		ProviderSlug:         string(event.ProviderSlug),
		DeliveryId:           event.DeliveryID,
		EventName:            event.EventName,
		RepositoryProviderId: optionalStringPtr(event.RepositoryProviderID),
		ReceivedAt:           formatTime(event.ReceivedAt),
		ProcessingStatus:     WebhookStatusToProto(event.ProcessingStatus),
		PayloadJson:          string(event.PayloadJSON),
		LastError:            optionalStringPtr(event.LastError),
		RetainUntil:          formatTime(event.RetainUntil),
	}
}

// ListWebhookEventsResponse maps stored webhooks to gRPC.
func ListWebhookEventsResponse(result providerservice.ListWebhookEventsResult) *providersv1.ListWebhookEventsResponse {
	return &providersv1.ListWebhookEventsResponse{
		WebhookEvents: mapSlice(result.WebhookEvents, WebhookEventToProto),
		Page:          pageResponseToProto(result.Page),
	}
}

// ProviderAccountRuntimeStateResponse maps runtime state to gRPC.
func ProviderAccountRuntimeStateResponse(state entity.ProviderAccountRuntimeState) *providersv1.ProviderAccountRuntimeStateResponse {
	return &providersv1.ProviderAccountRuntimeStateResponse{RuntimeState: ProviderAccountRuntimeStateToProto(state)}
}

func ProviderAccountRuntimeStateToProto(state entity.ProviderAccountRuntimeState) *providersv1.ProviderAccountRuntimeState {
	return &providersv1.ProviderAccountRuntimeState{
		ProviderAccountRuntimeStateId: state.ID.String(),
		ExternalAccountId:             state.ExternalAccountID.String(),
		ProviderSlug:                  string(state.ProviderSlug),
		Status:                        RuntimeStatusToProto(state.Status),
		LastCheckedAt:                 timePtrString(state.LastCheckedAt),
		LastSuccessAt:                 timePtrString(state.LastSuccessAt),
		LastErrorCode:                 optionalStringPtr(state.LastErrorCode),
		LastErrorMessage:              optionalStringPtr(state.LastErrorMessage),
		Version:                       state.Version,
	}
}

// ListProviderAccountRuntimeStatesResponse maps runtime states to gRPC.
func ListProviderAccountRuntimeStatesResponse(result providerservice.ListProviderAccountRuntimeStatesResult) *providersv1.ListProviderAccountRuntimeStatesResponse {
	return &providersv1.ListProviderAccountRuntimeStatesResponse{
		RuntimeStates: mapSlice(result.RuntimeStates, ProviderAccountRuntimeStateToProto),
		Page:          pageResponseToProto(result.Page),
	}
}

// ProviderLimitSnapshotResponse maps a limit snapshot to gRPC.
func ProviderLimitSnapshotResponse(snapshot entity.ProviderLimitSnapshot) *providersv1.ProviderLimitSnapshotResponse {
	return &providersv1.ProviderLimitSnapshotResponse{LimitSnapshot: ProviderLimitSnapshotToProto(snapshot)}
}

func ProviderLimitSnapshotToProto(snapshot entity.ProviderLimitSnapshot) *providersv1.ProviderLimitSnapshot {
	return &providersv1.ProviderLimitSnapshot{
		ProviderLimitSnapshotId: snapshot.ID.String(),
		ExternalAccountId:       snapshot.ExternalAccountID.String(),
		ProviderSlug:            string(snapshot.ProviderSlug),
		LimitClass:              snapshot.LimitClass,
		Remaining:               optionalInt64Ptr(snapshot.Remaining),
		LimitValue:              optionalInt64Ptr(snapshot.LimitValue),
		ResetAt:                 timePtrString(snapshot.ResetAt),
		CapturedAt:              formatTime(snapshot.CapturedAt),
		Source:                  string(snapshot.Source),
	}
}

// ListProviderLimitSnapshotsResponse maps limit snapshots to gRPC.
func ListProviderLimitSnapshotsResponse(result providerservice.ListProviderLimitSnapshotsResult) *providersv1.ListProviderLimitSnapshotsResponse {
	return &providersv1.ListProviderLimitSnapshotsResponse{
		LimitSnapshots: mapSlice(result.LimitSnapshots, ProviderLimitSnapshotToProto),
		Page:           pageResponseToProto(result.Page),
	}
}

func ProviderOperationToProto(operation entity.ProviderOperation) *providersv1.ProviderOperation {
	return &providersv1.ProviderOperation{
		ProviderOperationId: operation.ID.String(),
		CommandId:           operation.CommandID,
		ActorId:             uuidPtrString(operation.ActorID),
		ExternalAccountId:   operation.ExternalAccountID.String(),
		ProviderSlug:        string(operation.ProviderSlug),
		OperationType:       OperationTypeToProto(operation.OperationType),
		TargetRef:           operation.TargetRef,
		Status:              OperationStatusToProto(operation.Status),
		ResultRef:           optionalStringPtr(operation.ResultRef),
		ErrorCode:           optionalStringPtr(operation.ErrorCode),
		ErrorMessage:        optionalStringPtr(operation.ErrorMessage),
		RateLimitSnapshotId: uuidPtrString(operation.RateLimitSnapshotID),
		StartedAt:           formatTime(operation.StartedAt),
		FinishedAt:          timePtrString(operation.FinishedAt),
	}
}

// ListProviderOperationsResponse maps provider operations to gRPC.
func ListProviderOperationsResponse(result providerservice.ListProviderOperationsResult) *providersv1.ListProviderOperationsResponse {
	return &providersv1.ListProviderOperationsResponse{
		ProviderOperations: mapSlice(result.ProviderOperations, ProviderOperationToProto),
		Page:               pageResponseToProto(result.Page),
	}
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func mapSlice[Input any, Output any](items []Input, mapper func(Input) *Output) []*Output {
	if len(items) == 0 {
		return nil
	}
	result := make([]*Output, 0, len(items))
	for _, item := range items {
		result = append(result, mapper(item))
	}
	return result
}
