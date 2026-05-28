package value

// WebhookPayloadStorage describes where the full provider webhook payload can be used.
type WebhookPayloadStorage string

const (
	WebhookPayloadStorageRetained WebhookPayloadStorage = "retained_for_retry"
	WebhookPayloadStorageRedacted WebhookPayloadStorage = "redacted_after_terminal_processing"
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
	RetainUntil          string `json:"retain_until,omitempty"`
}
