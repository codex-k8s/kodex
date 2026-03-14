package enum

// GitHubRateLimitRecoveryHintSource identifies which provider evidence produced the current hint.
type GitHubRateLimitRecoveryHintSource string

const (
	GitHubRateLimitRecoveryHintSourceResetAt           GitHubRateLimitRecoveryHintSource = "reset_at"
	GitHubRateLimitRecoveryHintSourceRetryAfter        GitHubRateLimitRecoveryHintSource = "retry_after"
	GitHubRateLimitRecoveryHintSourceProviderUncertain GitHubRateLimitRecoveryHintSource = "provider_uncertain"
)

// GitHubRateLimitNextStepKind describes whether the platform scheduled auto-resume or requires operator action.
type GitHubRateLimitNextStepKind string

const (
	GitHubRateLimitNextStepKindAutoResumeScheduled  GitHubRateLimitNextStepKind = "auto_resume_scheduled"
	GitHubRateLimitNextStepKindManualActionRequired GitHubRateLimitNextStepKind = "manual_action_required"
)

// GitHubRateLimitCommentMirrorState tracks best-effort GitHub service-comment health for one run projection.
type GitHubRateLimitCommentMirrorState string

const (
	GitHubRateLimitCommentMirrorStateSynced       GitHubRateLimitCommentMirrorState = "synced"
	GitHubRateLimitCommentMirrorStatePendingRetry GitHubRateLimitCommentMirrorState = "pending_retry"
	GitHubRateLimitCommentMirrorStateNotAttempted GitHubRateLimitCommentMirrorState = "not_attempted"
)

// GitHubRateLimitResolutionKind describes how one wait aggregate was finally resolved.
type GitHubRateLimitResolutionKind string

const (
	GitHubRateLimitResolutionKindAutoResumed      GitHubRateLimitResolutionKind = "auto_resumed"
	GitHubRateLimitResolutionKindManuallyResolved GitHubRateLimitResolutionKind = "manually_resolved"
	GitHubRateLimitResolutionKindCancelled        GitHubRateLimitResolutionKind = "cancelled"
)
