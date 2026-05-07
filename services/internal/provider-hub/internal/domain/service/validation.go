package service

import (
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
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

func validOperationTypes(types []enum.ProviderOperationType) bool {
	for _, operationType := range types {
		switch operationType {
		case enum.ProviderOperationCreateIssue,
			enum.ProviderOperationUpdateIssue,
			enum.ProviderOperationCreateComment,
			enum.ProviderOperationUpdateComment,
			enum.ProviderOperationCreatePullRequest,
			enum.ProviderOperationCreateReviewSignal,
			enum.ProviderOperationUpdateRelationship:
		default:
			return false
		}
	}
	return true
}

func validOperationStatuses(statuses []enum.ProviderOperationStatus) bool {
	for _, status := range statuses {
		switch status {
		case enum.ProviderOperationStatusSucceeded,
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
