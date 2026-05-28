package value

// WebhookPayloadStorage describes where the full provider webhook payload can be used.
type WebhookPayloadStorage string

const (
	WebhookPayloadStorageRetained WebhookPayloadStorage = "retained_for_retry"
	WebhookPayloadStorageRedacted WebhookPayloadStorage = "redacted_after_terminal_processing"
	WebhookPayloadStorageExpired  WebhookPayloadStorage = "expired_after_retention"
)

// WebhookPayloadCleanupReason classifies why the full payload was removed.
type WebhookPayloadCleanupReason string

const (
	WebhookPayloadCleanupReasonExpired WebhookPayloadCleanupReason = "payload_expired"
)

// WebhookPayloadEnvelope is the only payload metadata safe for read APIs and diagnostics.
type WebhookPayloadEnvelope struct {
	ProviderSlug         string `json:"provider_slug,omitempty"`
	DeliveryID           string `json:"delivery_id,omitempty"`
	EventName            string `json:"event_name,omitempty"`
	RepositoryProviderID string `json:"repository_provider_id,omitempty"`
	PayloadSHA256        string `json:"payload_sha256,omitempty"`
	PayloadDigestSource  string `json:"payload_digest_source,omitempty"`
	PayloadStorage       string `json:"payload_storage"`
	PayloadCleanupReason string `json:"payload_cleanup_reason,omitempty"`
	PayloadExpiredAt     string `json:"payload_expired_at,omitempty"`
	RetainUntil          string `json:"retain_until,omitempty"`
}
