package service

import (
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

func validProviderSlug(slug enum.ProviderSlug) bool {
	switch slug {
	case enum.ProviderSlugGitHub, enum.ProviderSlugGitLab:
		return true
	default:
		return false
	}
}

func validRuntimeStatuses(statuses []enum.ProviderAccountRuntimeStatus) bool {
	for _, status := range statuses {
		switch status {
		case enum.ProviderAccountRuntimeStatusActive,
			enum.ProviderAccountRuntimeStatusReauthorizationRequired,
			enum.ProviderAccountRuntimeStatusLimited,
			enum.ProviderAccountRuntimeStatusDisabled,
			enum.ProviderAccountRuntimeStatusError:
		default:
			return false
		}
	}
	return true
}

func validWebhookStatuses(statuses []enum.WebhookProcessingStatus) bool {
	for _, status := range statuses {
		switch status {
		case enum.WebhookProcessingStatusPending,
			enum.WebhookProcessingStatusProcessed,
			enum.WebhookProcessingStatusFailed,
			enum.WebhookProcessingStatusIgnored:
		default:
			return false
		}
	}
	return true
}

func validWorkItemKinds(kinds []enum.WorkItemKind) bool {
	for _, kind := range kinds {
		if !validWorkItemKind(kind) {
			return false
		}
	}
	return true
}

func validWorkItemKind(kind enum.WorkItemKind) bool {
	switch kind {
	case enum.WorkItemKindIssue,
		enum.WorkItemKindPullRequest,
		enum.WorkItemKindMergeRequest:
		return true
	default:
		return false
	}
}

func validWorkItemDriftStatuses(statuses []enum.WorkItemDriftStatus) bool {
	for _, status := range statuses {
		switch status {
		case enum.WorkItemDriftStatusFresh,
			enum.WorkItemDriftStatusSuspected,
			enum.WorkItemDriftStatusStale,
			enum.WorkItemDriftStatusFailed:
		default:
			return false
		}
	}
	return true
}

func validCommentKinds(kinds []enum.CommentKind) bool {
	for _, kind := range kinds {
		switch kind {
		case enum.CommentKindComment,
			enum.CommentKindReview,
			enum.CommentKindMention,
			enum.CommentKindSystem:
		default:
			return false
		}
	}
	return true
}

func validRelationshipSources(sources []enum.RelationshipSource) bool {
	for _, source := range sources {
		switch source {
		case enum.RelationshipSourceProvider,
			enum.RelationshipSourceWatermark,
			enum.RelationshipSourceComment,
			enum.RelationshipSourceManual,
			enum.RelationshipSourceReconciliation:
		default:
			return false
		}
	}
	return true
}

func validRelationshipConfidenceLevels(levels []enum.RelationshipConfidence) bool {
	for _, level := range levels {
		switch level {
		case enum.RelationshipConfidenceConfirmed,
			enum.RelationshipConfidenceInferred,
			enum.RelationshipConfidenceSuspected:
		default:
			return false
		}
	}
	return true
}

func validSyncCursorScope(scope enum.SyncCursorScopeType) bool {
	switch scope {
	case enum.SyncCursorScopeRepository,
		enum.SyncCursorScopeOrganization,
		enum.SyncCursorScopeWorkItem,
		enum.SyncCursorScopePackageSource:
		return true
	default:
		return false
	}
}

func validSyncArtifactKind(kind enum.SyncArtifactKind) bool {
	switch kind {
	case enum.SyncArtifactIssue,
		enum.SyncArtifactPullRequest,
		enum.SyncArtifactMergeRequest,
		enum.SyncArtifactComment,
		enum.SyncArtifactRelationship,
		enum.SyncArtifactRepository:
		return true
	default:
		return false
	}
}

func validSyncArtifactKinds(kinds []enum.SyncArtifactKind) bool {
	for _, kind := range kinds {
		if !validSyncArtifactKind(kind) {
			return false
		}
	}
	return true
}

func validSyncCursorPriority(priority enum.SyncCursorPriority) bool {
	switch priority {
	case enum.SyncCursorPriorityHot,
		enum.SyncCursorPriorityWarm,
		enum.SyncCursorPriorityCold:
		return true
	default:
		return false
	}
}

func validSyncCursorPriorities(priorities []enum.SyncCursorPriority) bool {
	for _, priority := range priorities {
		if !validSyncCursorPriority(priority) {
			return false
		}
	}
	return true
}

func validLimitSource(source enum.ProviderLimitSource) bool {
	switch source {
	case enum.ProviderLimitSourceProviderHub,
		enum.ProviderLimitSourceSlotAgentBefore,
		enum.ProviderLimitSourceSlotAgentAfter,
		enum.ProviderLimitSourceSlotAgentSignal:
		return true
	default:
		return false
	}
}

func validCommandIdentity(meta value.CommandMeta) bool {
	return meta.CommandID != uuid.Nil || strings.TrimSpace(meta.IdempotencyKey) != ""
}

func validOperationTypes(types []enum.ProviderOperationType) bool {
	for _, operationType := range types {
		switch operationType {
		case enum.ProviderOperationCreateRepository,
			enum.ProviderOperationCreateIssue,
			enum.ProviderOperationUpdateIssue,
			enum.ProviderOperationCreateComment,
			enum.ProviderOperationUpdateComment,
			enum.ProviderOperationCreatePullRequest,
			enum.ProviderOperationUpdatePullRequest,
			enum.ProviderOperationCreateBootstrapPullRequest,
			enum.ProviderOperationCreateAdoptionPullRequest,
			enum.ProviderOperationCreateReviewSignal,
			enum.ProviderOperationUpdateRelationship:
		default:
			return false
		}
	}
	return true
}

func validRepositoryOwnerKind(kind enum.RepositoryOwnerKind) bool {
	switch kind {
	case enum.RepositoryOwnerKindOrganization,
		enum.RepositoryOwnerKindAuthenticatedUser:
		return true
	default:
		return false
	}
}

func validRepositoryVisibility(visibility enum.RepositoryVisibility) bool {
	switch visibility {
	case enum.RepositoryVisibilityPublic,
		enum.RepositoryVisibilityPrivate,
		enum.RepositoryVisibilityInternal:
		return true
	default:
		return false
	}
}

func validOperationStatuses(statuses []enum.ProviderOperationStatus) bool {
	for _, status := range statuses {
		switch status {
		case enum.ProviderOperationStatusInProgress,
			enum.ProviderOperationStatusSucceeded,
			enum.ProviderOperationStatusFailed,
			enum.ProviderOperationStatusRetryableFailed,
			enum.ProviderOperationStatusDenied:
		default:
			return false
		}
	}
	return true
}

func runtimeStatusFromLimit(remaining *int64) enum.ProviderAccountRuntimeStatus {
	if remaining != nil && *remaining == 0 {
		return enum.ProviderAccountRuntimeStatusLimited
	}
	return enum.ProviderAccountRuntimeStatusActive
}

func hasNilUUID(ids []uuid.UUID) bool {
	for _, id := range ids {
		if id == uuid.Nil {
			return true
		}
	}
	return false
}

func hasBlankStrings(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	return false
}

func trimStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func uniqueSyncArtifactKinds(values []enum.SyncArtifactKind) []enum.SyncArtifactKind {
	result := make([]enum.SyncArtifactKind, 0, len(values))
	seen := make(map[enum.SyncArtifactKind]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
