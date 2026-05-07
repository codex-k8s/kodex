// Package enum contains closed domain vocabularies for provider-hub.
package enum

// ProviderSlug identifies a provider adapter.
type ProviderSlug string

const (
	ProviderSlugGitHub ProviderSlug = "github"
	ProviderSlugGitLab ProviderSlug = "gitlab"
)

// ProviderAccountRuntimeStatus describes account health at provider side.
type ProviderAccountRuntimeStatus string

const (
	ProviderAccountRuntimeStatusActive                  ProviderAccountRuntimeStatus = "active"
	ProviderAccountRuntimeStatusReauthorizationRequired ProviderAccountRuntimeStatus = "reauthorization_required"
	ProviderAccountRuntimeStatusLimited                 ProviderAccountRuntimeStatus = "limited"
	ProviderAccountRuntimeStatusDisabled                ProviderAccountRuntimeStatus = "disabled"
	ProviderAccountRuntimeStatusError                   ProviderAccountRuntimeStatus = "error"
)

// ProviderLimitSource identifies who observed a provider limit state.
type ProviderLimitSource string

const (
	ProviderLimitSourceProviderHub     ProviderLimitSource = "provider_hub"
	ProviderLimitSourceSlotAgentBefore ProviderLimitSource = "slot_agent_before"
	ProviderLimitSourceSlotAgentAfter  ProviderLimitSource = "slot_agent_after"
	ProviderLimitSourceSlotAgentSignal ProviderLimitSource = "slot_agent_signal"
)

// ProviderOperationType classifies commands executed through provider-hub.
type ProviderOperationType string

const (
	ProviderOperationCreateIssue        ProviderOperationType = "create_issue"
	ProviderOperationUpdateIssue        ProviderOperationType = "update_issue"
	ProviderOperationCreateComment      ProviderOperationType = "create_comment"
	ProviderOperationUpdateComment      ProviderOperationType = "update_comment"
	ProviderOperationCreatePullRequest  ProviderOperationType = "create_pull_request"
	ProviderOperationCreateReviewSignal ProviderOperationType = "create_review_signal"
	ProviderOperationUpdateRelationship ProviderOperationType = "update_relationship"
)

// ProviderOperationStatus is the terminal or current operation state.
type ProviderOperationStatus string

const (
	ProviderOperationStatusSucceeded       ProviderOperationStatus = "succeeded"
	ProviderOperationStatusFailed          ProviderOperationStatus = "failed"
	ProviderOperationStatusRetryableFailed ProviderOperationStatus = "retryable_failed"
	ProviderOperationStatusDenied          ProviderOperationStatus = "denied"
)
