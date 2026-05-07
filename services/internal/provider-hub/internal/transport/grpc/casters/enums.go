package casters

import (
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

type domainEnum interface {
	~string
}

var runtimeStatuses = map[providersv1.ProviderAccountRuntimeStatus]enum.ProviderAccountRuntimeStatus{
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_ACTIVE:                   enum.ProviderAccountRuntimeStatusActive,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_REAUTHORIZATION_REQUIRED: enum.ProviderAccountRuntimeStatusReauthorizationRequired,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_LIMITED:                  enum.ProviderAccountRuntimeStatusLimited,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_DISABLED:                 enum.ProviderAccountRuntimeStatusDisabled,
	providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_ERROR:                    enum.ProviderAccountRuntimeStatusError,
}

var webhookStatuses = map[providersv1.WebhookProcessingStatus]enum.WebhookProcessingStatus{
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_PENDING:   enum.WebhookProcessingStatusPending,
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_PROCESSED: enum.WebhookProcessingStatusProcessed,
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_FAILED:    enum.WebhookProcessingStatusFailed,
	providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_IGNORED:   enum.WebhookProcessingStatusIgnored,
}

var workItemKinds = map[providersv1.WorkItemKind]enum.WorkItemKind{
	providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE:         enum.WorkItemKindIssue,
	providersv1.WorkItemKind_WORK_ITEM_KIND_PULL_REQUEST:  enum.WorkItemKindPullRequest,
	providersv1.WorkItemKind_WORK_ITEM_KIND_MERGE_REQUEST: enum.WorkItemKindMergeRequest,
}

var watermarkStatuses = map[providersv1.WorkItemWatermarkStatus]enum.WorkItemWatermarkStatus{
	providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_MISSING: enum.WorkItemWatermarkStatusMissing,
	providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_VALID:   enum.WorkItemWatermarkStatusValid,
	providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_INVALID: enum.WorkItemWatermarkStatusInvalid,
	providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_STALE:   enum.WorkItemWatermarkStatusStale,
}

var driftStatuses = map[providersv1.WorkItemDriftStatus]enum.WorkItemDriftStatus{
	providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FRESH:     enum.WorkItemDriftStatusFresh,
	providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_SUSPECTED: enum.WorkItemDriftStatusSuspected,
	providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_STALE:     enum.WorkItemDriftStatusStale,
	providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FAILED:    enum.WorkItemDriftStatusFailed,
}

var commentKinds = map[providersv1.CommentKind]enum.CommentKind{
	providersv1.CommentKind_COMMENT_KIND_COMMENT: enum.CommentKindComment,
	providersv1.CommentKind_COMMENT_KIND_REVIEW:  enum.CommentKindReview,
	providersv1.CommentKind_COMMENT_KIND_MENTION: enum.CommentKindMention,
	providersv1.CommentKind_COMMENT_KIND_SYSTEM:  enum.CommentKindSystem,
}

var reviewStates = map[providersv1.ReviewState]enum.ReviewState{
	providersv1.ReviewState_REVIEW_STATE_APPROVED:          enum.ReviewStateApproved,
	providersv1.ReviewState_REVIEW_STATE_CHANGES_REQUESTED: enum.ReviewStateChangesRequested,
	providersv1.ReviewState_REVIEW_STATE_COMMENTED:         enum.ReviewStateCommented,
	providersv1.ReviewState_REVIEW_STATE_DISMISSED:         enum.ReviewStateDismissed,
	providersv1.ReviewState_REVIEW_STATE_PENDING:           enum.ReviewStatePending,
}

var relationshipSources = map[providersv1.RelationshipSource]enum.RelationshipSource{
	providersv1.RelationshipSource_RELATIONSHIP_SOURCE_PROVIDER:       enum.RelationshipSourceProvider,
	providersv1.RelationshipSource_RELATIONSHIP_SOURCE_WATERMARK:      enum.RelationshipSourceWatermark,
	providersv1.RelationshipSource_RELATIONSHIP_SOURCE_COMMENT:        enum.RelationshipSourceComment,
	providersv1.RelationshipSource_RELATIONSHIP_SOURCE_MANUAL:         enum.RelationshipSourceManual,
	providersv1.RelationshipSource_RELATIONSHIP_SOURCE_RECONCILIATION: enum.RelationshipSourceReconciliation,
}

var relationshipConfidenceLevels = map[providersv1.RelationshipConfidence]enum.RelationshipConfidence{
	providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_CONFIRMED: enum.RelationshipConfidenceConfirmed,
	providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_INFERRED:  enum.RelationshipConfidenceInferred,
	providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_SUSPECTED: enum.RelationshipConfidenceSuspected,
}

var operationTypes = map[providersv1.ProviderOperationType]enum.ProviderOperationType{
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE:         enum.ProviderOperationCreateIssue,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_ISSUE:         enum.ProviderOperationUpdateIssue,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_COMMENT:       enum.ProviderOperationCreateComment,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_COMMENT:       enum.ProviderOperationUpdateComment,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_PULL_REQUEST:  enum.ProviderOperationCreatePullRequest,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REVIEW_SIGNAL: enum.ProviderOperationCreateReviewSignal,
	providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_RELATIONSHIP:  enum.ProviderOperationUpdateRelationship,
}

var operationStatuses = map[providersv1.ProviderOperationStatus]enum.ProviderOperationStatus{
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED:        enum.ProviderOperationStatusSucceeded,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_FAILED:           enum.ProviderOperationStatusFailed,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_RETRYABLE_FAILED: enum.ProviderOperationStatusRetryableFailed,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_DENIED:           enum.ProviderOperationStatusDenied,
}

func runtimeStatusesFromProto(statuses []providersv1.ProviderAccountRuntimeStatus) ([]enum.ProviderAccountRuntimeStatus, error) {
	return enumsFromProto(statuses, providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_UNSPECIFIED, runtimeStatuses)
}

func RuntimeStatusToProto(status enum.ProviderAccountRuntimeStatus) providersv1.ProviderAccountRuntimeStatus {
	return enumToProto(status, providersv1.ProviderAccountRuntimeStatus_PROVIDER_ACCOUNT_RUNTIME_STATUS_UNSPECIFIED, invertEnum(runtimeStatuses))
}

func webhookStatusesFromProto(statuses []providersv1.WebhookProcessingStatus) ([]enum.WebhookProcessingStatus, error) {
	return enumsFromProto(statuses, providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_UNSPECIFIED, webhookStatuses)
}

func WebhookStatusToProto(status enum.WebhookProcessingStatus) providersv1.WebhookProcessingStatus {
	return enumToProto(status, providersv1.WebhookProcessingStatus_WEBHOOK_PROCESSING_STATUS_UNSPECIFIED, invertEnum(webhookStatuses))
}

func workItemKindsFromProto(kinds []providersv1.WorkItemKind) ([]enum.WorkItemKind, error) {
	return enumsFromProto(kinds, providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED, workItemKinds)
}

func workItemKindFromProto(kind providersv1.WorkItemKind) (enum.WorkItemKind, error) {
	if kind == providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED {
		return "", nil
	}
	mapped, ok := workItemKinds[kind]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func WorkItemKindToProto(kind enum.WorkItemKind) providersv1.WorkItemKind {
	return enumToProto(kind, providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED, invertEnum(workItemKinds))
}

func WatermarkStatusToProto(status enum.WorkItemWatermarkStatus) providersv1.WorkItemWatermarkStatus {
	return enumToProto(status, providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_UNSPECIFIED, invertEnum(watermarkStatuses))
}

func driftStatusesFromProto(statuses []providersv1.WorkItemDriftStatus) ([]enum.WorkItemDriftStatus, error) {
	return enumsFromProto(statuses, providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_UNSPECIFIED, driftStatuses)
}

func DriftStatusToProto(status enum.WorkItemDriftStatus) providersv1.WorkItemDriftStatus {
	return enumToProto(status, providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_UNSPECIFIED, invertEnum(driftStatuses))
}

func commentKindsFromProto(kinds []providersv1.CommentKind) ([]enum.CommentKind, error) {
	return enumsFromProto(kinds, providersv1.CommentKind_COMMENT_KIND_UNSPECIFIED, commentKinds)
}

func CommentKindToProto(kind enum.CommentKind) providersv1.CommentKind {
	return enumToProto(kind, providersv1.CommentKind_COMMENT_KIND_UNSPECIFIED, invertEnum(commentKinds))
}

func ReviewStateToProto(state enum.ReviewState) providersv1.ReviewState {
	return enumToProto(state, providersv1.ReviewState_REVIEW_STATE_UNSPECIFIED, invertEnum(reviewStates))
}

func relationshipSourcesFromProto(sources []providersv1.RelationshipSource) ([]enum.RelationshipSource, error) {
	return enumsFromProto(sources, providersv1.RelationshipSource_RELATIONSHIP_SOURCE_UNSPECIFIED, relationshipSources)
}

func RelationshipSourceToProto(source enum.RelationshipSource) providersv1.RelationshipSource {
	return enumToProto(source, providersv1.RelationshipSource_RELATIONSHIP_SOURCE_UNSPECIFIED, invertEnum(relationshipSources))
}

func relationshipConfidenceLevelsFromProto(levels []providersv1.RelationshipConfidence) ([]enum.RelationshipConfidence, error) {
	return enumsFromProto(levels, providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_UNSPECIFIED, relationshipConfidenceLevels)
}

func RelationshipConfidenceToProto(level enum.RelationshipConfidence) providersv1.RelationshipConfidence {
	return enumToProto(level, providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_UNSPECIFIED, invertEnum(relationshipConfidenceLevels))
}

func operationTypesFromProto(types []providersv1.ProviderOperationType) ([]enum.ProviderOperationType, error) {
	return enumsFromProto(types, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, operationTypes)
}

func OperationTypeToProto(operationType enum.ProviderOperationType) providersv1.ProviderOperationType {
	return enumToProto(operationType, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, invertEnum(operationTypes))
}

func operationStatusesFromProto(statuses []providersv1.ProviderOperationStatus) ([]enum.ProviderOperationStatus, error) {
	return enumsFromProto(statuses, providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_UNSPECIFIED, operationStatuses)
}

func OperationStatusToProto(status enum.ProviderOperationStatus) providersv1.ProviderOperationStatus {
	return enumToProto(status, providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_UNSPECIFIED, invertEnum(operationStatuses))
}

func enumsFromProto[Proto comparable, Domain domainEnum](values []Proto, unspecified Proto, mapping map[Proto]Domain) ([]Domain, error) {
	result := make([]Domain, 0, len(values))
	for _, value := range values {
		if value == unspecified {
			return nil, errs.ErrInvalidArgument
		}
		mapped, ok := mapping[value]
		if !ok {
			return nil, errs.ErrInvalidArgument
		}
		result = append(result, mapped)
	}
	return result, nil
}

func enumToProto[Domain comparable, Proto any](value Domain, unspecified Proto, mapping map[Domain]Proto) Proto {
	mapped, ok := mapping[value]
	if !ok {
		return unspecified
	}
	return mapped
}

func invertEnum[Proto comparable, Domain comparable](mapping map[Proto]Domain) map[Domain]Proto {
	inverted := make(map[Domain]Proto, len(mapping))
	for protoValue, domainValue := range mapping {
		inverted[domainValue] = protoValue
	}
	return inverted
}
