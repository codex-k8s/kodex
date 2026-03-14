package enum

// GitHubRateLimitProjectionSyncState describes whether dominant wait linkage was cleared, applied, or blocked.
type GitHubRateLimitProjectionSyncState string

const (
	GitHubRateLimitProjectionSyncStateCleared                   GitHubRateLimitProjectionSyncState = "cleared"
	GitHubRateLimitProjectionSyncStateApplied                   GitHubRateLimitProjectionSyncState = "applied"
	GitHubRateLimitProjectionSyncStateBlockedByRunWaitContext   GitHubRateLimitProjectionSyncState = "blocked_by_run_wait_context"
	GitHubRateLimitProjectionSyncStateBlockedBySessionWaitState GitHubRateLimitProjectionSyncState = "blocked_by_session_wait_state"
)
