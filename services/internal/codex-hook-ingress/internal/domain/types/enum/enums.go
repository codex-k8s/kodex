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

// DeliveryMode identifies future delivery behavior without selecting a transport.
type DeliveryMode string

const (
	DeliveryModeSync     DeliveryMode = "sync"
	DeliveryModeAsync    DeliveryMode = "async"
	DeliveryModeRealtime DeliveryMode = "realtime"
	DeliveryModeAudit    DeliveryMode = "audit"
)

// HandlerResult is the normalized hook handler outcome returned to an emitter.
type HandlerResult string

const (
	HandlerResultContinue   HandlerResult = "continue"
	HandlerResultAllow      HandlerResult = "allow"
	HandlerResultDeny       HandlerResult = "deny"
	HandlerResultNoDecision HandlerResult = "no_decision"
	HandlerResultRetry      HandlerResult = "retry"
	HandlerResultFailClosed HandlerResult = "fail_closed"
	HandlerResultIgnored    HandlerResult = "ignored"
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
