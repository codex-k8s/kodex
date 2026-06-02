package casters

import (
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
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

var syncCursorScopes = map[providersv1.SyncCursorScopeType]enum.SyncCursorScopeType{
	providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_REPOSITORY:     enum.SyncCursorScopeRepository,
	providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_ORGANIZATION:   enum.SyncCursorScopeOrganization,
	providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_WORK_ITEM:      enum.SyncCursorScopeWorkItem,
	providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_PACKAGE_SOURCE: enum.SyncCursorScopePackageSource,
}

var syncCursorPriorities = map[providersv1.SyncCursorPriority]enum.SyncCursorPriority{
	providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_HOT:  enum.SyncCursorPriorityHot,
	providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_WARM: enum.SyncCursorPriorityWarm,
	providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_COLD: enum.SyncCursorPriorityCold,
}

var operationTypes = providerOperationTypes()

func providerOperationTypes() map[providersv1.ProviderOperationType]enum.ProviderOperationType {
	result := make(map[providersv1.ProviderOperationType]enum.ProviderOperationType, 12)
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REPOSITORY] = enum.ProviderOperationCreateRepository
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE] = enum.ProviderOperationCreateIssue
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_ISSUE] = enum.ProviderOperationUpdateIssue
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_COMMENT] = enum.ProviderOperationCreateComment
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_COMMENT] = enum.ProviderOperationUpdateComment
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_PULL_REQUEST] = enum.ProviderOperationCreatePullRequest
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_PULL_REQUEST] = enum.ProviderOperationUpdatePullRequest
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_BOOTSTRAP_PULL_REQUEST] = enum.ProviderOperationCreateBootstrapPullRequest
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ADOPTION_PULL_REQUEST] = enum.ProviderOperationCreateAdoptionPullRequest
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_SCAN_REPOSITORY_FOR_ADOPTION] = enum.ProviderOperationScanRepositoryForAdoption
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REVIEW_SIGNAL] = enum.ProviderOperationCreateReviewSignal
	result[providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_RELATIONSHIP] = enum.ProviderOperationUpdateRelationship
	return result
}

var repositoryAdoptionScanStatuses = map[providersv1.RepositoryAdoptionScanStatus]enum.RepositoryAdoptionScanStatus{
	providersv1.RepositoryAdoptionScanStatus_REPOSITORY_ADOPTION_SCAN_STATUS_COMPLETED:    enum.RepositoryAdoptionScanStatusCompleted,
	providersv1.RepositoryAdoptionScanStatus_REPOSITORY_ADOPTION_SCAN_STATUS_LIMITED:      enum.RepositoryAdoptionScanStatusLimited,
	providersv1.RepositoryAdoptionScanStatus_REPOSITORY_ADOPTION_SCAN_STATUS_NEEDS_REVIEW: enum.RepositoryAdoptionScanStatusNeedsReview,
}

var providerOwnedDataStatuses = map[providersv1.ProviderOwnedDataStatus]enum.ProviderOwnedDataStatus{
	providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_READY:        enum.ProviderOwnedDataStatusReady,
	providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_NOT_FOUND:    enum.ProviderOwnedDataStatusNotFound,
	providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_NOT_VERIFIED: enum.ProviderOwnedDataStatusNotVerified,
	providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_STALE:        enum.ProviderOwnedDataStatusStale,
}

var repositoryMergeSignalKinds = map[providersv1.RepositoryMergeSignalKind]enum.RepositoryMergeSignalKind{
	providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_BOOTSTRAP: enum.RepositoryMergeSignalKindBootstrap,
	providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_ADOPTION:  enum.RepositoryMergeSignalKindAdoption,
}

var repositoryMergeSignalStatuses = map[providersv1.RepositoryMergeSignalStatus]enum.RepositoryMergeSignalStatus{
	providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_MERGED: enum.RepositoryMergeSignalStatusMerged,
}

var repositoryChangeSignalKinds = map[providersv1.RepositoryChangeSignalKind]enum.RepositoryChangeSignalKind{
	providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_PUSH:                enum.RepositoryChangeSignalKindPush,
	providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_PULL_REQUEST_MERGED: enum.RepositoryChangeSignalKindPullRequestMerged,
}

var repositoryChangeSignalStatuses = map[providersv1.RepositoryChangeSignalStatus]enum.RepositoryChangeSignalStatus{
	providersv1.RepositoryChangeSignalStatus_REPOSITORY_CHANGE_SIGNAL_STATUS_OBSERVED: enum.RepositoryChangeSignalStatusObserved,
}

var repositoryChangePathSummaryStatuses = map[providersv1.RepositoryChangePathSummaryStatus]enum.RepositoryChangePathSummaryStatus{
	providersv1.RepositoryChangePathSummaryStatus_REPOSITORY_CHANGE_PATH_SUMMARY_STATUS_READY:       enum.RepositoryChangePathSummaryStatusReady,
	providersv1.RepositoryChangePathSummaryStatus_REPOSITORY_CHANGE_PATH_SUMMARY_STATUS_UNAVAILABLE: enum.RepositoryChangePathSummaryStatusUnavailable,
	providersv1.RepositoryChangePathSummaryStatus_REPOSITORY_CHANGE_PATH_SUMMARY_STATUS_TRUNCATED:   enum.RepositoryChangePathSummaryStatusTruncated,
}

var repositoryChangePathCategories = map[providersv1.RepositoryChangePathCategory]enum.RepositoryChangePathCategory{
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_SERVICES_POLICY: enum.RepositoryChangePathCategoryServicesPolicy,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_SERVICE_SOURCE:  enum.RepositoryChangePathCategoryServiceSource,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_SERVICE_CONFIG:  enum.RepositoryChangePathCategoryServiceConfig,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_DEPLOY_MANIFEST: enum.RepositoryChangePathCategoryDeployManifest,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_RUNTIME_CONFIG:  enum.RepositoryChangePathCategoryRuntimeConfig,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_DOCUMENTATION:   enum.RepositoryChangePathCategoryDocumentation,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_TEST:            enum.RepositoryChangePathCategoryTest,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_PLATFORM_POLICY: enum.RepositoryChangePathCategoryPlatformPolicy,
	providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_OTHER:           enum.RepositoryChangePathCategoryOther,
}

var repositoryAdoptionMarkerKinds = map[providersv1.RepositoryAdoptionMarkerKind]enum.RepositoryAdoptionMarkerKind{
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_SERVICE_DESCRIPTOR: enum.RepositoryAdoptionMarkerServiceDescriptor,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_GITMODULES:         enum.RepositoryAdoptionMarkerGitmodules,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_README:             enum.RepositoryAdoptionMarkerReadme,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_AGENTS:             enum.RepositoryAdoptionMarkerAgents,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_DOCS:               enum.RepositoryAdoptionMarkerDocs,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_WORKFLOW:           enum.RepositoryAdoptionMarkerWorkflow,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_MODULE:             enum.RepositoryAdoptionMarkerModule,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_PACKAGE:            enum.RepositoryAdoptionMarkerPackage,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_DEPLOY:             enum.RepositoryAdoptionMarkerDeploy,
	providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_OTHER:              enum.RepositoryAdoptionMarkerOther,
}

var repositoryOwnerKinds = map[providersv1.RepositoryOwnerKind]enum.RepositoryOwnerKind{
	providersv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_ORGANIZATION:       enum.RepositoryOwnerKindOrganization,
	providersv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_AUTHENTICATED_USER: enum.RepositoryOwnerKindAuthenticatedUser,
}

var repositoryVisibilities = map[providersv1.RepositoryVisibility]enum.RepositoryVisibility{
	providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PUBLIC:   enum.RepositoryVisibilityPublic,
	providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PRIVATE:  enum.RepositoryVisibilityPrivate,
	providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_INTERNAL: enum.RepositoryVisibilityInternal,
}

var operationStatuses = map[providersv1.ProviderOperationStatus]enum.ProviderOperationStatus{
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_IN_PROGRESS:      enum.ProviderOperationStatusInProgress,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED:        enum.ProviderOperationStatusSucceeded,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_FAILED:           enum.ProviderOperationStatusFailed,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_RETRYABLE_FAILED: enum.ProviderOperationStatusRetryableFailed,
	providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_DENIED:           enum.ProviderOperationStatusDenied,
}

var reviewSignalKinds = map[providersv1.ReviewSignalKind]enum.ReviewSignalKind{
	providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_COMMENT:           enum.ReviewSignalKindComment,
	providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_APPROVAL:          enum.ReviewSignalKindApproval,
	providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_CHANGES_REQUESTED: enum.ReviewSignalKindChangesRequested,
}

var operationRiskLevels = map[providersv1.ProviderOperationRiskLevel]value.ProviderOperationRiskLevel{
	providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_LOW:      value.ProviderOperationRiskLevelLow,
	providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_MEDIUM:   value.ProviderOperationRiskLevelMedium,
	providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_HIGH:     value.ProviderOperationRiskLevelHigh,
	providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_CRITICAL: value.ProviderOperationRiskLevelCritical,
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

func syncCursorScopeFromProto(scope providersv1.SyncCursorScopeType) (enum.SyncCursorScopeType, error) {
	if scope == providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := syncCursorScopes[scope]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func optionalSyncCursorScopeFromProto(scope providersv1.SyncCursorScopeType) (enum.SyncCursorScopeType, error) {
	if scope == providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_UNSPECIFIED {
		return "", nil
	}
	return syncCursorScopeFromProto(scope)
}

func SyncCursorScopeToProto(scope enum.SyncCursorScopeType) providersv1.SyncCursorScopeType {
	return enumToProto(scope, providersv1.SyncCursorScopeType_SYNC_CURSOR_SCOPE_TYPE_UNSPECIFIED, invertEnum(syncCursorScopes))
}

func syncArtifactKindsFromProto(kinds []providersv1.SyncArtifactKind) ([]enum.SyncArtifactKind, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	result := make([]enum.SyncArtifactKind, 0, len(kinds))
	for index := range kinds {
		mapped, err := syncArtifactKindFromProto(kinds[index])
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}

func syncArtifactKindFromProto(kind providersv1.SyncArtifactKind) (enum.SyncArtifactKind, error) {
	switch kind {
	case providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_ISSUE:
		return enum.SyncArtifactIssue, nil
	case providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_PULL_REQUEST:
		return enum.SyncArtifactPullRequest, nil
	case providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_MERGE_REQUEST:
		return enum.SyncArtifactMergeRequest, nil
	case providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_COMMENT:
		return enum.SyncArtifactComment, nil
	case providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_RELATIONSHIP:
		return enum.SyncArtifactRelationship, nil
	case providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_REPOSITORY:
		return enum.SyncArtifactRepository, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func SyncArtifactKindToProto(kind enum.SyncArtifactKind) providersv1.SyncArtifactKind {
	switch kind {
	case enum.SyncArtifactIssue:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_ISSUE
	case enum.SyncArtifactPullRequest:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_PULL_REQUEST
	case enum.SyncArtifactMergeRequest:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_MERGE_REQUEST
	case enum.SyncArtifactComment:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_COMMENT
	case enum.SyncArtifactRelationship:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_RELATIONSHIP
	case enum.SyncArtifactRepository:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_REPOSITORY
	default:
		return providersv1.SyncArtifactKind_SYNC_ARTIFACT_KIND_UNSPECIFIED
	}
}

func syncCursorPriorityFromProto(priority providersv1.SyncCursorPriority) (enum.SyncCursorPriority, error) {
	if priority == providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := syncCursorPriorities[priority]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func syncCursorPrioritiesFromProto(priorities []providersv1.SyncCursorPriority) ([]enum.SyncCursorPriority, error) {
	return enumsFromProto(priorities, providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_UNSPECIFIED, syncCursorPriorities)
}

func SyncCursorPriorityToProto(priority enum.SyncCursorPriority) providersv1.SyncCursorPriority {
	return enumToProto(priority, providersv1.SyncCursorPriority_SYNC_CURSOR_PRIORITY_UNSPECIFIED, invertEnum(syncCursorPriorities))
}

func operationTypesFromProto(types []providersv1.ProviderOperationType) ([]enum.ProviderOperationType, error) {
	return enumsFromProto(types, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, operationTypes)
}

func OperationTypeToProto(operationType enum.ProviderOperationType) providersv1.ProviderOperationType {
	return enumToProto(operationType, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, invertEnum(operationTypes))
}

func RepositoryAdoptionScanStatusToProto(status enum.RepositoryAdoptionScanStatus) providersv1.RepositoryAdoptionScanStatus {
	return enumToProto(status, providersv1.RepositoryAdoptionScanStatus_REPOSITORY_ADOPTION_SCAN_STATUS_UNSPECIFIED, invertEnum(repositoryAdoptionScanStatuses))
}

func repositoryAdoptionScanStatusesFromProto(statuses []providersv1.RepositoryAdoptionScanStatus) ([]enum.RepositoryAdoptionScanStatus, error) {
	return enumsFromProto(statuses, providersv1.RepositoryAdoptionScanStatus_REPOSITORY_ADOPTION_SCAN_STATUS_UNSPECIFIED, repositoryAdoptionScanStatuses)
}

func ProviderOwnedDataStatusToProto(status enum.ProviderOwnedDataStatus) providersv1.ProviderOwnedDataStatus {
	return enumToProto(status, providersv1.ProviderOwnedDataStatus_PROVIDER_OWNED_DATA_STATUS_UNSPECIFIED, invertEnum(providerOwnedDataStatuses))
}

func repositoryMergeSignalKindsFromProto(kinds []providersv1.RepositoryMergeSignalKind) ([]enum.RepositoryMergeSignalKind, error) {
	return enumsFromProto(kinds, providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_UNSPECIFIED, repositoryMergeSignalKinds)
}

func RepositoryMergeSignalKindToProto(kind enum.RepositoryMergeSignalKind) providersv1.RepositoryMergeSignalKind {
	return enumToProto(kind, providersv1.RepositoryMergeSignalKind_REPOSITORY_MERGE_SIGNAL_KIND_UNSPECIFIED, invertEnum(repositoryMergeSignalKinds))
}

func repositoryMergeSignalStatusesFromProto(statuses []providersv1.RepositoryMergeSignalStatus) ([]enum.RepositoryMergeSignalStatus, error) {
	return enumsFromProto(statuses, providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_UNSPECIFIED, repositoryMergeSignalStatuses)
}

func RepositoryMergeSignalStatusToProto(status enum.RepositoryMergeSignalStatus) providersv1.RepositoryMergeSignalStatus {
	return enumToProto(status, providersv1.RepositoryMergeSignalStatus_REPOSITORY_MERGE_SIGNAL_STATUS_UNSPECIFIED, invertEnum(repositoryMergeSignalStatuses))
}

func repositoryChangeSignalKindsFromProto(kinds []providersv1.RepositoryChangeSignalKind) ([]enum.RepositoryChangeSignalKind, error) {
	return enumsFromProto(kinds, providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_UNSPECIFIED, repositoryChangeSignalKinds)
}

func RepositoryChangeSignalKindToProto(kind enum.RepositoryChangeSignalKind) providersv1.RepositoryChangeSignalKind {
	return enumToProto(kind, providersv1.RepositoryChangeSignalKind_REPOSITORY_CHANGE_SIGNAL_KIND_UNSPECIFIED, invertEnum(repositoryChangeSignalKinds))
}

func repositoryChangeSignalStatusesFromProto(statuses []providersv1.RepositoryChangeSignalStatus) ([]enum.RepositoryChangeSignalStatus, error) {
	return enumsFromProto(statuses, providersv1.RepositoryChangeSignalStatus_REPOSITORY_CHANGE_SIGNAL_STATUS_UNSPECIFIED, repositoryChangeSignalStatuses)
}

func RepositoryChangeSignalStatusToProto(status enum.RepositoryChangeSignalStatus) providersv1.RepositoryChangeSignalStatus {
	return enumToProto(status, providersv1.RepositoryChangeSignalStatus_REPOSITORY_CHANGE_SIGNAL_STATUS_UNSPECIFIED, invertEnum(repositoryChangeSignalStatuses))
}

func RepositoryChangePathSummaryStatusToProto(status enum.RepositoryChangePathSummaryStatus) providersv1.RepositoryChangePathSummaryStatus {
	return enumToProto(status, providersv1.RepositoryChangePathSummaryStatus_REPOSITORY_CHANGE_PATH_SUMMARY_STATUS_UNSPECIFIED, invertEnum(repositoryChangePathSummaryStatuses))
}

func RepositoryChangePathCategoryToProto(category enum.RepositoryChangePathCategory) providersv1.RepositoryChangePathCategory {
	return enumToProto(category, providersv1.RepositoryChangePathCategory_REPOSITORY_CHANGE_PATH_CATEGORY_UNSPECIFIED, invertEnum(repositoryChangePathCategories))
}

func RepositoryAdoptionMarkerKindToProto(kind enum.RepositoryAdoptionMarkerKind) providersv1.RepositoryAdoptionMarkerKind {
	return enumToProto(kind, providersv1.RepositoryAdoptionMarkerKind_REPOSITORY_ADOPTION_MARKER_KIND_UNSPECIFIED, invertEnum(repositoryAdoptionMarkerKinds))
}

func repositoryOwnerKindFromProto(kind providersv1.RepositoryOwnerKind) (enum.RepositoryOwnerKind, error) {
	if kind == providersv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := repositoryOwnerKinds[kind]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func repositoryVisibilityFromProto(visibility providersv1.RepositoryVisibility) (enum.RepositoryVisibility, error) {
	if visibility == providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := repositoryVisibilities[visibility]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func operationStatusesFromProto(statuses []providersv1.ProviderOperationStatus) ([]enum.ProviderOperationStatus, error) {
	return enumsFromProto(statuses, providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_UNSPECIFIED, operationStatuses)
}

func OperationStatusToProto(status enum.ProviderOperationStatus) providersv1.ProviderOperationStatus {
	return enumToProto(status, providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_UNSPECIFIED, invertEnum(operationStatuses))
}

func reviewSignalKindFromProto(kind providersv1.ReviewSignalKind) (enum.ReviewSignalKind, error) {
	if kind == providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := reviewSignalKinds[kind]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func ReviewSignalKindToProto(kind enum.ReviewSignalKind) providersv1.ReviewSignalKind {
	return enumToProto(kind, providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_UNSPECIFIED, invertEnum(reviewSignalKinds))
}

func operationRiskLevelFromProto(level providersv1.ProviderOperationRiskLevel) (value.ProviderOperationRiskLevel, error) {
	if level == providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_UNSPECIFIED {
		return "", errs.ErrInvalidArgument
	}
	mapped, ok := operationRiskLevels[level]
	if !ok {
		return "", errs.ErrInvalidArgument
	}
	return mapped, nil
}

func OperationRiskLevelToProto(level value.ProviderOperationRiskLevel) providersv1.ProviderOperationRiskLevel {
	return enumToProto(level, providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_UNSPECIFIED, invertEnum(operationRiskLevels))
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
