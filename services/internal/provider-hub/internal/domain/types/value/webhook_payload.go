package value

// WebhookPayloadStorage describes the safe storage state of a provider webhook payload.
type WebhookPayloadStorage string

const (
	WebhookPayloadStorageSafeEnvelope WebhookPayloadStorage = "safe_envelope_only"
	WebhookPayloadStorageRetained     WebhookPayloadStorage = "retained_for_retry"                 // legacy safe marker
	WebhookPayloadStorageRedacted     WebhookPayloadStorage = "redacted_after_terminal_processing" // legacy safe marker
	WebhookPayloadStorageExpired      WebhookPayloadStorage = "expired_after_retention"
)

// WebhookPayloadCleanupReason classifies why the full payload was removed.
type WebhookPayloadCleanupReason string

const (
	WebhookPayloadCleanupReasonRemoved WebhookPayloadCleanupReason = "raw_payload_removed"
	WebhookPayloadCleanupReasonExpired WebhookPayloadCleanupReason = "payload_expired"
)

// WebhookPayloadEnvelope is the only payload metadata safe for read APIs and diagnostics.
type WebhookPayloadEnvelope struct {
	ProviderSlug          string `json:"provider_slug,omitempty"`
	DeliveryID            string `json:"delivery_id,omitempty"`
	EventName             string `json:"event_name,omitempty"`
	RepositoryProviderID  string `json:"repository_provider_id,omitempty"`
	RepositoryFullName    string `json:"repository_full_name,omitempty"`
	ProviderWorkItemID    string `json:"provider_work_item_id,omitempty"`
	ProviderCommentID     string `json:"provider_comment_id,omitempty"`
	FactKind              string `json:"fact_kind,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	Number                int64  `json:"number,omitempty"`
	PullRequestProviderID string `json:"pull_request_provider_id,omitempty"`
	PullRequestURL        string `json:"pull_request_url,omitempty"`
	BaseBranch            string `json:"base_branch,omitempty"`
	HeadBranch            string `json:"head_branch,omitempty"`
	MergeCommitSHA        string `json:"merge_commit_sha,omitempty"`
	SourceRef             string `json:"source_ref,omitempty"`
	MergedAt              string `json:"merged_at,omitempty"`
	PayloadSHA256         string `json:"payload_sha256,omitempty"`
	PayloadDigestSource   string `json:"payload_digest_source,omitempty"`
	PayloadStorage        string `json:"payload_storage"`
	PayloadCleanupReason  string `json:"payload_cleanup_reason,omitempty"`
	PayloadExpiredAt      string `json:"payload_expired_at,omitempty"`
	RetainUntil           string `json:"retain_until,omitempty"`
}
