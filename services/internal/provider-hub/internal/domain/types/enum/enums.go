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
	ProviderOperationCreateRepository           ProviderOperationType = "create_repository"
	ProviderOperationCreateIssue                ProviderOperationType = "create_issue"
	ProviderOperationUpdateIssue                ProviderOperationType = "update_issue"
	ProviderOperationCreateComment              ProviderOperationType = "create_comment"
	ProviderOperationUpdateComment              ProviderOperationType = "update_comment"
	ProviderOperationCreatePullRequest          ProviderOperationType = "create_pull_request"
	ProviderOperationUpdatePullRequest          ProviderOperationType = "update_pull_request"
	ProviderOperationCreateBootstrapPullRequest ProviderOperationType = "create_bootstrap_pull_request"
	ProviderOperationCreateAdoptionPullRequest  ProviderOperationType = "create_adoption_pull_request"
	ProviderOperationScanRepositoryForAdoption  ProviderOperationType = "scan_repository_for_adoption"
	ProviderOperationCreateReviewSignal         ProviderOperationType = "create_review_signal"
	ProviderOperationUpdateRelationship         ProviderOperationType = "update_relationship"
)

// RepositoryOwnerKind selects provider-side repository owner semantics.
type RepositoryOwnerKind string

const (
	RepositoryOwnerKindOrganization      RepositoryOwnerKind = "organization"
	RepositoryOwnerKindAuthenticatedUser RepositoryOwnerKind = "authenticated_user"
)

// RepositoryVisibility selects provider-side repository visibility.
type RepositoryVisibility string

const (
	RepositoryVisibilityPublic   RepositoryVisibility = "public"
	RepositoryVisibilityPrivate  RepositoryVisibility = "private"
	RepositoryVisibilityInternal RepositoryVisibility = "internal"
)

// RepositoryAdoptionScanStatus describes the safe result status of a lightweight adoption scan.
type RepositoryAdoptionScanStatus string

const (
	RepositoryAdoptionScanStatusCompleted   RepositoryAdoptionScanStatus = "completed"
	RepositoryAdoptionScanStatusLimited     RepositoryAdoptionScanStatus = "limited"
	RepositoryAdoptionScanStatusNeedsReview RepositoryAdoptionScanStatus = "needs_review"
)

// RepositoryAdoptionMarkerKind classifies provider-side adoption markers without file contents.
type RepositoryAdoptionMarkerKind string

const (
	RepositoryAdoptionMarkerServiceDescriptor RepositoryAdoptionMarkerKind = "service_descriptor"
	RepositoryAdoptionMarkerGitmodules        RepositoryAdoptionMarkerKind = "gitmodules"
	RepositoryAdoptionMarkerReadme            RepositoryAdoptionMarkerKind = "readme"
	RepositoryAdoptionMarkerAgents            RepositoryAdoptionMarkerKind = "agents"
	RepositoryAdoptionMarkerDocs              RepositoryAdoptionMarkerKind = "docs"
	RepositoryAdoptionMarkerWorkflow          RepositoryAdoptionMarkerKind = "workflow"
	RepositoryAdoptionMarkerModule            RepositoryAdoptionMarkerKind = "module"
	RepositoryAdoptionMarkerPackage           RepositoryAdoptionMarkerKind = "package"
	RepositoryAdoptionMarkerDeploy            RepositoryAdoptionMarkerKind = "deploy"
	RepositoryAdoptionMarkerOther             RepositoryAdoptionMarkerKind = "other"
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

// RepositoryMergeSignalKind classifies provider-side onboarding merge signals.
type RepositoryMergeSignalKind string

const (
	RepositoryMergeSignalKindBootstrap RepositoryMergeSignalKind = "bootstrap"
	RepositoryMergeSignalKindAdoption  RepositoryMergeSignalKind = "adoption"
)

// RepositoryMergeSignalStatus describes the merge signal lifecycle at provider edge.
type RepositoryMergeSignalStatus string

const (
	RepositoryMergeSignalStatusMerged RepositoryMergeSignalStatus = "merged"
)

// ProviderOwnedDataStatus describes readiness of provider-owned data for service reads.
type ProviderOwnedDataStatus string

const (
	ProviderOwnedDataStatusReady       ProviderOwnedDataStatus = "ready"
	ProviderOwnedDataStatusNotFound    ProviderOwnedDataStatus = "not_found"
	ProviderOwnedDataStatusNotVerified ProviderOwnedDataStatus = "not_verified"
	ProviderOwnedDataStatusStale       ProviderOwnedDataStatus = "stale"
)

// ReviewSignalKind classifies provider-native review actions requested by the platform.
type ReviewSignalKind string

const (
	ReviewSignalKindComment          ReviewSignalKind = "comment"
	ReviewSignalKindApproval         ReviewSignalKind = "approval"
	ReviewSignalKindChangesRequested ReviewSignalKind = "changes_requested"
)
