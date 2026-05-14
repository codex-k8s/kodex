// Package enum contains closed domain vocabularies for provider-hub.
package enum

// ProviderSlug identifies a provider adapter.
type ProviderSlug string

const (
	ProviderSlugGitHub ProviderSlug = "github"
	ProviderSlugGitLab ProviderSlug = "gitlab"
)

// WebhookProcessingStatus describes raw webhook processing state.
type WebhookProcessingStatus string

const (
	WebhookProcessingStatusPending   WebhookProcessingStatus = "pending"
	WebhookProcessingStatusProcessed WebhookProcessingStatus = "processed"
	WebhookProcessingStatusFailed    WebhookProcessingStatus = "failed"
	WebhookProcessingStatusIgnored   WebhookProcessingStatus = "ignored"
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

// WorkItemKind classifies provider-native work items.
type WorkItemKind string

const (
	WorkItemKindIssue        WorkItemKind = "issue"
	WorkItemKindPullRequest  WorkItemKind = "pull_request"
	WorkItemKindMergeRequest WorkItemKind = "merge_request"
)

// WorkItemWatermarkStatus describes parsed platform watermark state.
type WorkItemWatermarkStatus string

const (
	WorkItemWatermarkStatusMissing WorkItemWatermarkStatus = "missing"
	WorkItemWatermarkStatusValid   WorkItemWatermarkStatus = "valid"
	WorkItemWatermarkStatusInvalid WorkItemWatermarkStatus = "invalid"
	WorkItemWatermarkStatusStale   WorkItemWatermarkStatus = "stale"
)

// WorkItemDriftStatus describes projection freshness.
type WorkItemDriftStatus string

const (
	WorkItemDriftStatusFresh     WorkItemDriftStatus = "fresh"
	WorkItemDriftStatusSuspected WorkItemDriftStatus = "suspected"
	WorkItemDriftStatusStale     WorkItemDriftStatus = "stale"
	WorkItemDriftStatusFailed    WorkItemDriftStatus = "failed"
)

// CommentKind classifies comment-like provider records.
type CommentKind string

const (
	CommentKindComment CommentKind = "comment"
	CommentKindReview  CommentKind = "review"
	CommentKindMention CommentKind = "mention"
	CommentKindSystem  CommentKind = "system"
)

// ReviewState classifies provider-native review decisions.
type ReviewState string

const (
	ReviewStateApproved         ReviewState = "approved"
	ReviewStateChangesRequested ReviewState = "changes_requested"
	ReviewStateCommented        ReviewState = "commented"
	ReviewStateDismissed        ReviewState = "dismissed"
	ReviewStatePending          ReviewState = "pending"
)

// RelationshipSource classifies where a relationship was discovered.
type RelationshipSource string

const (
	RelationshipSourceProvider       RelationshipSource = "provider"
	RelationshipSourceWatermark      RelationshipSource = "watermark"
	RelationshipSourceComment        RelationshipSource = "comment"
	RelationshipSourceManual         RelationshipSource = "manual"
	RelationshipSourceReconciliation RelationshipSource = "reconciliation"
)

// RelationshipConfidence describes relationship reliability.
type RelationshipConfidence string

const (
	RelationshipConfidenceConfirmed RelationshipConfidence = "confirmed"
	RelationshipConfidenceInferred  RelationshipConfidence = "inferred"
	RelationshipConfidenceSuspected RelationshipConfidence = "suspected"
)

// SyncCursorScopeType classifies reconciliation scope.
type SyncCursorScopeType string

const (
	SyncCursorScopeRepository    SyncCursorScopeType = "repository"
	SyncCursorScopeOrganization  SyncCursorScopeType = "organization"
	SyncCursorScopeWorkItem      SyncCursorScopeType = "work_item"
	SyncCursorScopePackageSource SyncCursorScopeType = "package_source"
)

// SyncArtifactKind classifies artifacts reconciled by a cursor.
type SyncArtifactKind string

const (
	SyncArtifactIssue        SyncArtifactKind = "issue"
	SyncArtifactPullRequest  SyncArtifactKind = "pull_request"
	SyncArtifactMergeRequest SyncArtifactKind = "merge_request"
	SyncArtifactComment      SyncArtifactKind = "comment"
	SyncArtifactRelationship SyncArtifactKind = "relationship"
	SyncArtifactRepository   SyncArtifactKind = "repository"
)

// SyncCursorPriority classifies reconciliation urgency.
type SyncCursorPriority string

const (
	SyncCursorPriorityHot  SyncCursorPriority = "hot"
	SyncCursorPriorityWarm SyncCursorPriority = "warm"
	SyncCursorPriorityCold SyncCursorPriority = "cold"
)

// ProviderOperationType classifies commands executed through provider-hub.
type ProviderOperationType string

const (
	ProviderOperationCreateIssue                ProviderOperationType = "create_issue"
	ProviderOperationUpdateIssue                ProviderOperationType = "update_issue"
	ProviderOperationCreateComment              ProviderOperationType = "create_comment"
	ProviderOperationUpdateComment              ProviderOperationType = "update_comment"
	ProviderOperationCreatePullRequest          ProviderOperationType = "create_pull_request"
	ProviderOperationUpdatePullRequest          ProviderOperationType = "update_pull_request"
	ProviderOperationCreateBootstrapPullRequest ProviderOperationType = "create_bootstrap_pull_request"
	ProviderOperationCreateReviewSignal         ProviderOperationType = "create_review_signal"
	ProviderOperationUpdateRelationship         ProviderOperationType = "update_relationship"
)

// ProviderOperationStatus is the terminal or current operation state.
type ProviderOperationStatus string

const (
	ProviderOperationStatusInProgress      ProviderOperationStatus = "in_progress"
	ProviderOperationStatusSucceeded       ProviderOperationStatus = "succeeded"
	ProviderOperationStatusFailed          ProviderOperationStatus = "failed"
	ProviderOperationStatusRetryableFailed ProviderOperationStatus = "retryable_failed"
	ProviderOperationStatusDenied          ProviderOperationStatus = "denied"
)

// ReviewSignalKind classifies provider-native review actions requested by the platform.
type ReviewSignalKind string

const (
	ReviewSignalKindComment          ReviewSignalKind = "comment"
	ReviewSignalKindApproval         ReviewSignalKind = "approval"
	ReviewSignalKindChangesRequested ReviewSignalKind = "changes_requested"
)
