// Package enum contains codex-hook-ingress local enum-like value types.
package enum

// HookEventName identifies one normalized Codex hook event in the MVP set.
type HookEventName string

const (
	HookEventSessionStart      HookEventName = "SessionStart"
	HookEventUserPromptSubmit  HookEventName = "UserPromptSubmit"
	HookEventPreToolUse        HookEventName = "PreToolUse"
	HookEventPermissionRequest HookEventName = "PermissionRequest"
	HookEventPostToolUse       HookEventName = "PostToolUse"
	HookEventStop              HookEventName = "Stop"
)

// SourceKind identifies the local hook sender class.
type SourceKind string

const (
	SourceKindHookEmitter  SourceKind = "hook_emitter"
	SourceKindLocalSidecar SourceKind = "local_sidecar"
	SourceKindManagedHook  SourceKind = "managed_hook"
)

// TrustLevel describes how source binding was prepared for the sender.
type TrustLevel string

const (
	TrustLevelManaged           TrustLevel = "managed"
	TrustLevelTrusted           TrustLevel = "trusted"
	TrustLevelUntrustedRejected TrustLevel = "untrusted_rejected"
)

// ToolCategory classifies tool-scoped hook events without carrying raw input.
type ToolCategory string

const (
	ToolCategoryShell ToolCategory = "shell"
	ToolCategoryPatch ToolCategory = "patch"
	ToolCategoryMCP   ToolCategory = "mcp"
	ToolCategoryOther ToolCategory = "other"
)

// RetentionClass describes the service-local retention intent.
type RetentionClass string

const (
	RetentionClassAudit       RetentionClass = "audit"
	RetentionClassOperational RetentionClass = "operational"
	RetentionClassRealtime    RetentionClass = "realtime"
)

// DownstreamOwner identifies a future downstream owner route.
type DownstreamOwner string

const (
	DownstreamOwnerAgentManager      DownstreamOwner = "agent-manager"
	DownstreamOwnerRuntimeManager    DownstreamOwner = "runtime-manager"
	DownstreamOwnerProviderHub       DownstreamOwner = "provider-hub"
	DownstreamOwnerGovernanceManager DownstreamOwner = "governance-manager"
	DownstreamOwnerInteractionHub    DownstreamOwner = "interaction-hub"
	DownstreamOwnerOperationsFeed    DownstreamOwner = "operations-feed"
	DownstreamOwnerAuditLog          DownstreamOwner = "audit-log"
)

// DownstreamOwners returns all schema-defined downstream route owners.
func DownstreamOwners() []DownstreamOwner {
	return []DownstreamOwner{
		DownstreamOwnerAgentManager,
		DownstreamOwnerRuntimeManager,
		DownstreamOwnerProviderHub,
		DownstreamOwnerGovernanceManager,
		DownstreamOwnerInteractionHub,
		DownstreamOwnerOperationsFeed,
		DownstreamOwnerAuditLog,
	}
}

// IsDownstreamOwner reports whether owner belongs to the normalized envelope route set.
func IsDownstreamOwner(owner DownstreamOwner) bool {
	for _, known := range DownstreamOwners() {
		if known == owner {
			return true
		}
	}
	return false
}

// DeliveryMode identifies future delivery behavior without selecting a transport.
type DeliveryMode string

const (
	DeliveryModeSync     DeliveryMode = "sync"
	DeliveryModeAsync    DeliveryMode = "async"
	DeliveryModeRealtime DeliveryMode = "realtime"
	DeliveryModeAudit    DeliveryMode = "audit"
)

// SafeEventPart identifies one schema-approved payload part for owner dispatch.
type SafeEventPart string

const (
	SafeEventPartSourceContext          SafeEventPart = "source_context"
	SafeEventPartRunContext             SafeEventPart = "run_context"
	SafeEventPartToolContext            SafeEventPart = "tool_context"
	SafeEventPartCapabilityContext      SafeEventPart = "capability_context"
	SafeEventPartSafeSummary            SafeEventPart = "safe_summary"
	SafeEventPartPromptDigest           SafeEventPart = "prompt_digest"
	SafeEventPartRiskClass              SafeEventPart = "risk_class"
	SafeEventPartSanitizedReason        SafeEventPart = "sanitized_reason"
	SafeEventPartExitStatus             SafeEventPart = "exit_status"
	SafeEventPartOutputDigest           SafeEventPart = "output_digest"
	SafeEventPartBoundedError           SafeEventPart = "bounded_error"
	SafeEventPartProviderArtifactSignal SafeEventPart = "provider_artifact_signal"
	SafeEventPartRateLimitHint          SafeEventPart = "rate_limit_hint"
	SafeEventPartPendingActionRefs      SafeEventPart = "pending_action_refs"
	SafeEventPartCheckpointRef          SafeEventPart = "checkpoint_ref"
	SafeEventPartSanitizerReport        SafeEventPart = "sanitizer_report"
	SafeEventPartPayloadDigest          SafeEventPart = "payload_digest"
	SafeEventPartCorrelationID          SafeEventPart = "correlation_id"
)

// SafeEventParts returns all schema-approved safe parts.
func SafeEventParts() []SafeEventPart {
	return []SafeEventPart{
		SafeEventPartSourceContext,
		SafeEventPartRunContext,
		SafeEventPartToolContext,
		SafeEventPartCapabilityContext,
		SafeEventPartSafeSummary,
		SafeEventPartPromptDigest,
		SafeEventPartRiskClass,
		SafeEventPartSanitizedReason,
		SafeEventPartExitStatus,
		SafeEventPartOutputDigest,
		SafeEventPartBoundedError,
		SafeEventPartProviderArtifactSignal,
		SafeEventPartRateLimitHint,
		SafeEventPartPendingActionRefs,
		SafeEventPartCheckpointRef,
		SafeEventPartSanitizerReport,
		SafeEventPartPayloadDigest,
		SafeEventPartCorrelationID,
	}
}

// IsSafeEventPart reports whether part belongs to the schema-approved safe part set.
func IsSafeEventPart(part SafeEventPart) bool {
	for _, known := range SafeEventParts() {
		if known == part {
			return true
		}
	}
	return false
}

// RouteDeliveryStatus describes a safe owner dispatch outcome.
type RouteDeliveryStatus string

const (
	RouteDeliveryStatusDelivered   RouteDeliveryStatus = "delivered"
	RouteDeliveryStatusDisabled    RouteDeliveryStatus = "disabled"
	RouteDeliveryStatusUnsupported RouteDeliveryStatus = "unsupported"
	RouteDeliveryStatusFailed      RouteDeliveryStatus = "failed"
)

// OpsFeedStatus describes the safe lifecycle state stored in the short diagnostics feed.
type OpsFeedStatus string

const (
	OpsFeedStatusAccepted OpsFeedStatus = "accepted"
	OpsFeedStatusRejected OpsFeedStatus = "rejected"
	OpsFeedStatusDropped  OpsFeedStatus = "dropped"
)

// RouteFailurePolicy describes how SubmitHookEvent reacts to failed owner dispatch.
type RouteFailurePolicy string

const (
	RouteFailurePolicyDiagnostic RouteFailurePolicy = "diagnostic"
	RouteFailurePolicyFailClosed RouteFailurePolicy = "fail_closed"
)

// IsRouteFailurePolicy reports whether policy is supported by CHI-4.
func IsRouteFailurePolicy(policy RouteFailurePolicy) bool {
	return policy == RouteFailurePolicyDiagnostic || policy == RouteFailurePolicyFailClosed
}

// DecisionFailurePolicy describes safe fallback behavior for decision bridge failures.
type DecisionFailurePolicy string

const (
	DecisionFailurePolicyFailClosed     DecisionFailurePolicy = "fail_closed"
	DecisionFailurePolicyNoDecision     DecisionFailurePolicy = "no_decision"
	DecisionFailurePolicyTimeout        DecisionFailurePolicy = "timeout"
	DecisionFailurePolicyRetryableError DecisionFailurePolicy = "retryable_error"
)

// IsDecisionFailurePolicy reports whether policy is supported by CHI-5.
func IsDecisionFailurePolicy(policy DecisionFailurePolicy) bool {
	switch policy {
	case DecisionFailurePolicyFailClosed,
		DecisionFailurePolicyNoDecision,
		DecisionFailurePolicyTimeout,
		DecisionFailurePolicyRetryableError:
		return true
	default:
		return false
	}
}

// HandlerResult is the normalized hook handler outcome returned to an emitter.
type HandlerResult string

const (
	HandlerResultContinue       HandlerResult = "continue"
	HandlerResultAllow          HandlerResult = "allow"
	HandlerResultDeny           HandlerResult = "deny"
	HandlerResultNoDecision     HandlerResult = "no_decision"
	HandlerResultTimeout        HandlerResult = "timeout"
	HandlerResultRetry          HandlerResult = "retry"
	HandlerResultRetryableError HandlerResult = "retryable_error"
	HandlerResultFailClosed     HandlerResult = "fail_closed"
	HandlerResultIgnored        HandlerResult = "ignored"
)

// SanitizerResult describes the result of the pre-ingress sanitizer.
type SanitizerResult string

const (
	SanitizerResultAccepted  SanitizerResult = "accepted"
	SanitizerResultRedacted  SanitizerResult = "redacted"
	SanitizerResultTruncated SanitizerResult = "truncated"
)

// SupportedHookEvents returns the schema-defined MVP hook set.
func SupportedHookEvents() []HookEventName {
	return []HookEventName{
		HookEventSessionStart,
		HookEventUserPromptSubmit,
		HookEventPreToolUse,
		HookEventPermissionRequest,
		HookEventPostToolUse,
		HookEventStop,
	}
}

// IsSupportedHookEvent reports whether event belongs to the MVP hook set.
func IsSupportedHookEvent(event HookEventName) bool {
	for _, supported := range SupportedHookEvents() {
		if event == supported {
			return true
		}
	}
	return false
}
