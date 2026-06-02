package casters

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
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
		PayloadJson:          webhookSafePayloadJSON(event),
		LastError:            optionalStringPtr(event.LastError),
		RetainUntil:          formatTime(event.RetainUntil),
		PayloadSha256:        event.PayloadDigest,
	}
}

// CleanupExpiredWebhookPayloadsResponse maps cleanup result to safe gRPC diagnostics.
func CleanupExpiredWebhookPayloadsResponse(result providerservice.CleanupExpiredWebhookPayloadsResult) *providersv1.CleanupExpiredWebhookPayloadsResponse {
	return &providersv1.CleanupExpiredWebhookPayloadsResponse{
		CleanedCount:  result.CleanedCount,
		CleanedAt:     formatTime(result.CleanedAt),
		WebhookEvents: mapSlice(result.WebhookEvents, WebhookEventToProto),
	}
}

func webhookSafePayloadJSON(event entity.WebhookEvent) string {
	stored, ok := webhookStoredSafeEnvelope(event)
	if ok {
		return webhookSafeEnvelopeJSON(stored, event)
	}
	storage := value.WebhookPayloadStorageSafeEnvelope
	if event.ProcessingStatus == enum.WebhookProcessingStatusProcessed ||
		event.ProcessingStatus == enum.WebhookProcessingStatusIgnored {
		storage = value.WebhookPayloadStorageSafeEnvelope
	}
	return webhookSafeEnvelopeJSON(value.WebhookPayloadEnvelope{PayloadStorage: string(storage)}, event)
}

func webhookStoredSafeEnvelope(event entity.WebhookEvent) (value.WebhookPayloadEnvelope, bool) {
	var envelope value.WebhookPayloadEnvelope
	if err := json.Unmarshal(event.PayloadJSON, &envelope); err != nil {
		return value.WebhookPayloadEnvelope{}, false
	}
	switch envelope.PayloadStorage {
	case string(value.WebhookPayloadStorageSafeEnvelope),
		string(value.WebhookPayloadStorageRetained),
		string(value.WebhookPayloadStorageRedacted),
		string(value.WebhookPayloadStorageExpired):
		return envelope, true
	default:
		return value.WebhookPayloadEnvelope{}, false
	}
}

func webhookSafeEnvelopeJSON(envelope value.WebhookPayloadEnvelope, event entity.WebhookEvent) string {
	envelope.ProviderSlug = string(event.ProviderSlug)
	envelope.DeliveryID = event.DeliveryID
	envelope.EventName = event.EventName
	envelope.RepositoryProviderID = event.RepositoryProviderID
	envelope.PayloadSHA256 = event.PayloadDigest
	envelope.RetainUntil = formatTime(event.RetainUntil)
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "{}"
	}
	return string(payload)
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

// RepositoryMergeSignalResponse maps one provider-owned merge signal read result to gRPC.
func RepositoryMergeSignalResponse(result providerservice.RepositoryMergeSignalResult) *providersv1.RepositoryMergeSignalResponse {
	response := &providersv1.RepositoryMergeSignalResponse{
		ReadStatus: ProviderOwnedDataStatusToProto(result.Status),
	}
	if result.MergeSignal != nil {
		response.MergeSignal = RepositoryMergeSignalToProto(*result.MergeSignal)
	}
	return response
}

func RepositoryMergeSignalToProto(signal entity.RepositoryMergeSignal) *providersv1.RepositoryMergeSignal {
	return &providersv1.RepositoryMergeSignal{
		SignalId:                    signal.ID.String(),
		SignalKey:                   signal.SignalKey,
		Kind:                        RepositoryMergeSignalKindToProto(signal.Kind),
		ProviderSlug:                string(signal.ProviderSlug),
		ProjectId:                   uuidPtrValue(signal.ProjectID),
		RepositoryId:                uuidPtrValue(signal.RepositoryID),
		RepositoryFullName:          signal.RepositoryFullName,
		ProviderRepositoryId:        signal.ProviderRepositoryID,
		WorkItemProjectionId:        signal.WorkItemProjectionID.String(),
		ProviderWorkItemId:          signal.ProviderWorkItemID,
		PullRequestNumber:           signal.PullRequestNumber,
		PullRequestProviderId:       signal.PullRequestProviderID,
		PullRequestUrl:              signal.PullRequestURL,
		BaseBranch:                  signal.BaseBranch,
		HeadBranch:                  signal.HeadBranch,
		MergeCommitSha:              signal.MergeCommitSHA,
		SourceRef:                   signal.SourceRef,
		RelatedProviderOperationRef: signal.RelatedProviderOperationRef,
		WatermarkDigest:             signal.WatermarkDigest,
		ObservedAt:                  formatTime(signal.ObservedAt),
		MergedAt:                    formatTime(signal.MergedAt),
		Status:                      RepositoryMergeSignalStatusToProto(signal.Status),
		Version:                     signal.Version,
		Etag:                        repositoryMergeSignalETag(signal),
		CreatedAt:                   formatTime(signal.CreatedAt),
		UpdatedAt:                   formatTime(signal.UpdatedAt),
	}
}

// ListRepositoryMergeSignalsResponse maps provider-owned merge signals to gRPC.
func ListRepositoryMergeSignalsResponse(result providerservice.ListRepositoryMergeSignalsResult) *providersv1.ListRepositoryMergeSignalsResponse {
	return &providersv1.ListRepositoryMergeSignalsResponse{
		MergeSignals: mapSlice(result.MergeSignals, RepositoryMergeSignalToProto),
		Page:         pageResponseToProto(result.Page),
	}
}

// RepositoryChangeSignalResponse maps one provider-owned repository change signal read result to gRPC.
func RepositoryChangeSignalResponse(result providerservice.RepositoryChangeSignalResult) *providersv1.RepositoryChangeSignalResponse {
	response := &providersv1.RepositoryChangeSignalResponse{
		ReadStatus: ProviderOwnedDataStatusToProto(result.Status),
	}
	if result.ChangeSignal != nil {
		response.ChangeSignal = RepositoryChangeSignalToProto(*result.ChangeSignal)
	}
	return response
}

func RepositoryChangeSignalToProto(signal entity.RepositoryChangeSignal) *providersv1.RepositoryChangeSignal {
	return &providersv1.RepositoryChangeSignal{
		SignalId:              signal.ID.String(),
		SignalKey:             signal.SignalKey,
		Kind:                  RepositoryChangeSignalKindToProto(signal.Kind),
		ProviderSlug:          string(signal.ProviderSlug),
		ProjectId:             uuidPtrValue(signal.ProjectID),
		RepositoryId:          uuidPtrValue(signal.RepositoryID),
		RepositoryFullName:    signal.RepositoryFullName,
		ProviderRepositoryId:  signal.ProviderRepositoryID,
		Ref:                   signal.Ref,
		BaseBranch:            signal.BaseBranch,
		CommitSha:             signal.CommitSHA,
		BeforeSha:             signal.BeforeSHA,
		SourceRef:             signal.SourceRef,
		PullRequestNumber:     signal.PullRequestNumber,
		PullRequestProviderId: signal.PullRequestProviderID,
		PullRequestUrl:        signal.PullRequestURL,
		PathSummaryStatus:     RepositoryChangePathSummaryStatusToProto(signal.PathSummaryStatus),
		ChangedPathCount:      signal.ChangedPathCount,
		PathDigest:            signal.PathDigest,
		PathCategories:        mapSlice(signal.PathCategories, RepositoryChangePathCategoryCountToProto),
		ServicesPolicyChanged: signal.ServicesPolicyChanged,
		DeployRelevantChanged: signal.DeployRelevantChanged,
		ChangeFingerprint:     signal.ChangeFingerprint,
		ObservedAt:            formatTime(signal.ObservedAt),
		Status:                RepositoryChangeSignalStatusToProto(signal.Status),
		Version:               signal.Version,
		Etag:                  repositoryChangeSignalETag(signal),
		CreatedAt:             formatTime(signal.CreatedAt),
		UpdatedAt:             formatTime(signal.UpdatedAt),
	}
}

func RepositoryChangePathCategoryCountToProto(category entity.RepositoryChangePathCategoryCount) *providersv1.RepositoryChangePathCategoryCount {
	return &providersv1.RepositoryChangePathCategoryCount{
		Category: RepositoryChangePathCategoryToProto(category.Category),
		Count:    category.Count,
	}
}

// ListRepositoryChangeSignalsResponse maps provider-owned repository change signals to gRPC.
func ListRepositoryChangeSignalsResponse(result providerservice.ListRepositoryChangeSignalsResult) *providersv1.ListRepositoryChangeSignalsResponse {
	return &providersv1.ListRepositoryChangeSignalsResponse{
		ChangeSignals: mapSlice(result.ChangeSignals, RepositoryChangeSignalToProto),
		Page:          pageResponseToProto(result.Page),
	}
}

// RepositoryAdoptionScanSnapshotResponse maps one provider-owned scan snapshot read result to gRPC.
func RepositoryAdoptionScanSnapshotResponse(result providerservice.RepositoryAdoptionScanSnapshotResult) *providersv1.RepositoryAdoptionScanSnapshotResponse {
	response := &providersv1.RepositoryAdoptionScanSnapshotResponse{
		ReadStatus: ProviderOwnedDataStatusToProto(result.Status),
	}
	if result.Snapshot != nil {
		response.AdoptionScanSnapshot = RepositoryAdoptionScanSnapshotToProto(*result.Snapshot)
	}
	return response
}

// ListRepositoryAdoptionScanSnapshotsResponse maps provider-owned scan snapshots to gRPC.
func ListRepositoryAdoptionScanSnapshotsResponse(result providerservice.ListRepositoryAdoptionScanSnapshotsResult) *providersv1.ListRepositoryAdoptionScanSnapshotsResponse {
	return &providersv1.ListRepositoryAdoptionScanSnapshotsResponse{
		AdoptionScanSnapshots: mapSlice(result.Snapshots, RepositoryAdoptionScanSnapshotToProto),
		Page:                  pageResponseToProto(result.Page),
	}
}

func repositoryMergeSignalETag(signal entity.RepositoryMergeSignal) string {
	return fmt.Sprintf("repository_merge_signal:%s:%d:%s:%s", signal.ID.String(), signal.Version, signal.MergeCommitSHA, signal.WatermarkDigest)
}

func repositoryChangeSignalETag(signal entity.RepositoryChangeSignal) string {
	return fmt.Sprintf("repository_change_signal:%s:%d:%s:%s", signal.ID.String(), signal.Version, signal.CommitSHA, signal.ChangeFingerprint)
}

func repositoryAdoptionScanETag(snapshot entity.RepositoryAdoptionScanSnapshot) string {
	return fmt.Sprintf("repository_adoption_scan:%s:%d:%s:%s", snapshot.ID.String(), snapshot.Version, snapshot.HeadSHA, snapshot.SnapshotDigest)
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

func uuidPtrValue(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
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
