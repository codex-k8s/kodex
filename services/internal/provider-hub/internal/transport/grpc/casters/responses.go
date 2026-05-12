package casters

import (
	"encoding/json"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
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

// WorkItemProjectionResponse maps a work item projection to gRPC.
func WorkItemProjectionResponse(projection entity.ProviderWorkItemProjection) *providersv1.WorkItemProjectionResponse {
	return &providersv1.WorkItemProjectionResponse{WorkItemProjection: WorkItemProjectionToProto(projection)}
}

func WorkItemProjectionToProto(projection entity.ProviderWorkItemProjection) *providersv1.WorkItemProjection {
	return &providersv1.WorkItemProjection{
		WorkItemProjectionId:   projection.ID.String(),
		ProviderSlug:           string(projection.ProviderSlug),
		ProviderWorkItemId:     projection.ProviderWorkItemID,
		ProjectId:              uuidPtrString(projection.ProjectID),
		RepositoryId:           uuidPtrString(projection.RepositoryID),
		RepositoryFullName:     projection.RepositoryFullName,
		Kind:                   WorkItemKindToProto(projection.Kind),
		Number:                 projection.Number,
		WebUrl:                 projection.URL,
		Title:                  projection.Title,
		State:                  projection.State,
		WorkItemType:           optionalStringPtr(projection.WorkItemType),
		Labels:                 stringArrayFromJSON(projection.LabelsJSON),
		AssigneeProviderLogins: stringArrayFromJSON(projection.AssigneesJSON),
		Milestone:              optionalStringPtr(projection.Milestone),
		ProjectFieldsJson:      jsonObjectString(projection.ProjectFieldsJSON),
		WatermarkStatus:        WatermarkStatusToProto(projection.WatermarkStatus),
		WatermarkJson:          jsonObjectString(projection.WatermarkJSON),
		BodyDigest:             projection.BodyDigest,
		ProviderUpdatedAt:      timePtrString(projection.ProviderUpdatedAt),
		SyncedAt:               formatTime(projection.SyncedAt),
		DriftStatus:            DriftStatusToProto(projection.DriftStatus),
		Version:                projection.Version,
	}
}

// ListWorkItemProjectionsResponse maps work item projection lists to gRPC.
func ListWorkItemProjectionsResponse(result providerservice.ListWorkItemProjectionsResult) *providersv1.ListWorkItemProjectionsResponse {
	return &providersv1.ListWorkItemProjectionsResponse{
		WorkItemProjections: mapSlice(result.WorkItemProjections, WorkItemProjectionToProto),
		Page:                pageResponseToProto(result.Page),
	}
}

func CommentProjectionToProto(comment entity.ProviderCommentProjection) *providersv1.CommentProjection {
	return &providersv1.CommentProjection{
		CommentProjectionId:  comment.ID.String(),
		WorkItemProjectionId: comment.WorkItemProjectionID.String(),
		ProviderCommentId:    comment.ProviderCommentID,
		Kind:                 CommentKindToProto(comment.Kind),
		ReviewState:          ReviewStateToProto(comment.ReviewState),
		AuthorProviderLogin:  comment.AuthorProviderLogin,
		BodyDigest:           comment.BodyDigest,
		Summary:              comment.Summary,
		ProviderCreatedAt:    timePtrString(comment.ProviderCreatedAt),
		ProviderUpdatedAt:    timePtrString(comment.ProviderUpdatedAt),
	}
}

// ListCommentsResponse maps comment projections to gRPC.
func ListCommentsResponse(result providerservice.ListCommentsResult) *providersv1.ListCommentsResponse {
	return &providersv1.ListCommentsResponse{
		Comments: mapSlice(result.Comments, CommentProjectionToProto),
		Page:     pageResponseToProto(result.Page),
	}
}

func RelationshipToProto(relationship entity.ProviderRelationship) *providersv1.ProviderRelationship {
	return &providersv1.ProviderRelationship{
		RelationshipId:             relationship.ID.String(),
		SourceWorkItemProjectionId: relationship.SourceWorkItemID.String(),
		TargetWorkItemProjectionId: uuidPtrString(relationship.TargetWorkItemID),
		TargetProviderRef:          optionalStringPtr(relationship.TargetProviderRef),
		RelationshipType:           relationship.RelationshipType,
		Source:                     RelationshipSourceToProto(relationship.Source),
		Confidence:                 RelationshipConfidenceToProto(relationship.Confidence),
		CreatedAt:                  formatTime(relationship.CreatedAt),
		Version:                    relationship.Version,
	}
}

// ListRelationshipsResponse maps provider relationships to gRPC.
func ListRelationshipsResponse(result providerservice.ListRelationshipsResult) *providersv1.ListRelationshipsResponse {
	return &providersv1.ListRelationshipsResponse{
		Relationships: mapSlice(result.Relationships, RelationshipToProto),
		Page:          pageResponseToProto(result.Page),
	}
}

// ProviderArtifactSignalResponse maps accepted signal state to gRPC.
func ProviderArtifactSignalResponse(result providerservice.ProviderArtifactSignalResult) *providersv1.ProviderArtifactSignalResponse {
	return &providersv1.ProviderArtifactSignalResponse{
		SignalId: result.SignalID,
		Status:   result.Status,
		Target:   ProviderArtifactTargetToProto(result.Target),
	}
}

func ProviderArtifactTargetToProto(target providerservice.ProviderArtifactTarget) *providersv1.ProviderTarget {
	return providerTargetMessage(
		target.ProviderSlug,
		target.RepositoryFullName,
		target.ProviderRepositoryID,
		target.WorkItemKind,
		target.Number,
		target.ProviderObjectID,
		target.WebURL,
	)
}

func providerTargetMessage(
	providerSlug enum.ProviderSlug,
	repositoryFullName string,
	providerRepositoryID string,
	workItemKind enum.WorkItemKind,
	number int64,
	providerObjectID string,
	webURL string,
) *providersv1.ProviderTarget {
	mappedWorkItemKind := WorkItemKindToProto(workItemKind)
	return &providersv1.ProviderTarget{
		ProviderSlug:         string(providerSlug),
		RepositoryFullName:   optionalStringPtr(repositoryFullName),
		ProviderRepositoryId: optionalStringPtr(providerRepositoryID),
		WorkItemKind:         optionalWorkItemKindPtr(mappedWorkItemKind),
		Number:               optionalPositiveInt64Ptr(number),
		ProviderObjectId:     optionalStringPtr(providerObjectID),
		WebUrl:               optionalStringPtr(webURL),
	}
}

func SyncCursorToProto(cursor entity.SyncCursor) *providersv1.SyncCursor {
	return &providersv1.SyncCursor{
		SyncCursorId:        cursor.ID.String(),
		ProviderSlug:        string(cursor.ProviderSlug),
		ExternalAccountId:   cursor.ExternalAccountID.String(),
		ScopeType:           SyncCursorScopeToProto(cursor.ScopeType),
		ScopeRef:            cursor.ScopeRef,
		ArtifactKind:        SyncArtifactKindToProto(cursor.ArtifactKind),
		CursorValue:         optionalStringPtr(cursor.CursorValue),
		OverlapSince:        timePtrString(cursor.OverlapSince),
		Priority:            SyncCursorPriorityToProto(cursor.Priority),
		LastSuccessAt:       timePtrString(cursor.LastSuccessAt),
		LastCheckedAt:       timePtrString(cursor.LastCheckedAt),
		LastError:           optionalStringPtr(cursor.LastError),
		RateBudgetStateJson: jsonObjectString(cursor.RateBudgetStateJSON),
		LeaseOwner:          optionalStringPtr(cursor.LeaseOwner),
		LeaseUntil:          timePtrString(cursor.LeaseUntil),
	}
}

// ReconciliationRequestResponse maps affected sync cursors to gRPC.
func ReconciliationRequestResponse(result providerservice.EnqueueReconciliationResult) *providersv1.ReconciliationRequestResponse {
	return &providersv1.ReconciliationRequestResponse{
		SyncCursors: mapSlice(result.SyncCursors, SyncCursorToProto),
	}
}

// RunReconciliationBatchResponse maps a leased reconciliation batch to gRPC.
func RunReconciliationBatchResponse(result providerservice.RunReconciliationBatchResult) *providersv1.RunReconciliationBatchResponse {
	return &providersv1.RunReconciliationBatchResponse{
		SyncCursor:      SyncCursorToProto(result.SyncCursor),
		ItemsProcessed:  result.ItemsProcessed,
		EventsPublished: result.EventsPublished,
		RetryAfter:      optionalStringPtr(result.RetryAfter),
	}
}

// SyncCursorResponse maps a sync cursor to gRPC.
func SyncCursorResponse(cursor entity.SyncCursor) *providersv1.SyncCursorResponse {
	return &providersv1.SyncCursorResponse{SyncCursor: SyncCursorToProto(cursor)}
}

// ListSyncCursorsResponse maps sync cursors to gRPC.
func ListSyncCursorsResponse(result providerservice.ListSyncCursorsResult) *providersv1.ListSyncCursorsResponse {
	return &providersv1.ListSyncCursorsResponse{
		SyncCursors: mapSlice(result.SyncCursors, SyncCursorToProto),
		Page:        pageResponseToProto(result.Page),
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
		ProviderOperationId:    operation.ID.String(),
		CommandId:              operation.CommandID,
		ActorId:                uuidPtrString(operation.ActorID),
		ExternalAccountId:      operation.ExternalAccountID.String(),
		ProviderSlug:           string(operation.ProviderSlug),
		OperationType:          OperationTypeToProto(operation.OperationType),
		TargetRef:              operation.TargetRef,
		Status:                 OperationStatusToProto(operation.Status),
		ResultRef:              optionalStringPtr(operation.ResultRef),
		ErrorCode:              optionalStringPtr(operation.ErrorCode),
		ErrorMessage:           optionalStringPtr(operation.ErrorMessage),
		RateLimitSnapshotId:    uuidPtrString(operation.RateLimitSnapshotID),
		StartedAt:              formatTime(operation.StartedAt),
		FinishedAt:             timePtrString(operation.FinishedAt),
		OperationPolicyContext: PolicyContextToProto(operation.OperationPolicyContext),
		ApprovalGateRef:        ApprovalGateRefToProto(operation.ApprovalGateRef),
		ProviderVersion:        optionalStringPtr(operation.ProviderVersion),
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

func stringArrayFromJSON(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func jsonObjectString(raw []byte) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
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
