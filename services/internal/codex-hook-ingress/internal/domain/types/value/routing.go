package value

import (
	"github.com/google/uuid"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
)

const (
	RouteDiagnosticDelivered          = "route.delivered"
	RouteDiagnosticDisabled           = "route.disabled"
	RouteDiagnosticUnsupported        = "route.unsupported"
	RouteDiagnosticUnexpected         = "route.unexpected"
	RouteDiagnosticDownstreamFailed   = "route.downstream_failed"
	RouteDiagnosticFailurePolicyFired = "route.failure_policy_fired"
)

// SafeHookEvent is the owner-dispatch payload projected from schema-approved safe parts only.
type SafeHookEvent struct {
	EventID                uuid.UUID                `json:"event_id"`
	HookEventName          hookenum.HookEventName   `json:"hook_event_name"`
	Owner                  hookenum.DownstreamOwner `json:"owner"`
	DeliveryMode           hookenum.DeliveryMode    `json:"delivery_mode"`
	SafeParts              []string                 `json:"safe_parts"`
	SourceContext          *SourceContext           `json:"source_context,omitempty"`
	RunContext             *RunContext              `json:"run_context,omitempty"`
	ToolContext            *ToolContext             `json:"tool_context,omitempty"`
	CapabilityContext      *CapabilityContext       `json:"capability_context,omitempty"`
	SafeSummary            string                   `json:"safe_summary,omitempty"`
	PromptDigest           string                   `json:"prompt_digest,omitempty"`
	RiskClass              string                   `json:"risk_class,omitempty"`
	SanitizedReason        string                   `json:"sanitized_reason,omitempty"`
	BoundedError           *BoundedError            `json:"bounded_error,omitempty"`
	ProviderArtifactSignal *ProviderArtifactSignal  `json:"provider_artifact_signal,omitempty"`
	RateLimitHint          *RateLimitHint           `json:"rate_limit_hint,omitempty"`
	PendingActionRefs      []string                 `json:"pending_action_refs,omitempty"`
	CheckpointRef          *string                  `json:"checkpoint_ref,omitempty"`
	SanitizerReport        *SanitizerReport         `json:"sanitizer_report,omitempty"`
	PayloadDigest          string                   `json:"payload_digest,omitempty"`
	CorrelationID          string                   `json:"correlation_id,omitempty"`
}

// RouteDeliveryResult is a safe diagnostic result for one downstream owner route.
type RouteDeliveryResult struct {
	Owner             hookenum.DownstreamOwner     `json:"owner"`
	DeliveryMode      hookenum.DeliveryMode        `json:"delivery_mode"`
	Status            hookenum.RouteDeliveryStatus `json:"status"`
	DiagnosticCode    string                       `json:"diagnostic_code"`
	DiagnosticMessage string                       `json:"diagnostic_message,omitempty"`
	SafeParts         []string                     `json:"safe_parts,omitempty"`
	Retryable         bool                         `json:"retryable,omitempty"`
}

// Delivered reports whether the owner route accepted the safe dispatch payload.
func (result RouteDeliveryResult) Delivered() bool {
	return result.Status == hookenum.RouteDeliveryStatusDelivered
}
