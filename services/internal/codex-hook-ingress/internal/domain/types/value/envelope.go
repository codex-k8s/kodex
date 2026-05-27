// Package value contains codex-hook-ingress domain value objects.
package value

import (
	"time"

	"github.com/google/uuid"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
)

// HookEnvelope is the normalized-hook-envelope.v1 shape accepted by the domain skeleton.
type HookEnvelope struct {
	EventID           uuid.UUID               `json:"event_id"`
	SchemaVersion     string                  `json:"schema_version"`
	HookEventName     hookenum.HookEventName  `json:"hook_event_name"`
	EventTime         time.Time               `json:"event_time"`
	SourceContext     SourceContext           `json:"source_context"`
	RunContext        RunContext              `json:"run_context"`
	ToolContext       *ToolContext            `json:"tool_context,omitempty"`
	CapabilityContext *CapabilityContext      `json:"capability_context,omitempty"`
	SafePayload       SafePayload             `json:"safe_payload"`
	PayloadDigest     string                  `json:"payload_digest"`
	SanitizerReport   SanitizerReport         `json:"sanitizer_report"`
	DownstreamRoutes  []DownstreamRoute       `json:"downstream_routes"`
	CorrelationID     string                  `json:"correlation_id"`
	RetentionClass    hookenum.RetentionClass `json:"retention_class"`
}

// SourceContext carries actor, source and scope refs without secret material.
type SourceContext struct {
	SourceRef      string              `json:"source_ref"`
	SourceKind     hookenum.SourceKind `json:"source_kind"`
	ActorRef       string              `json:"actor_ref"`
	OrganizationID uuid.UUID           `json:"organization_id"`
	ProjectID      uuid.UUID           `json:"project_id"`
	RepositoryID   *uuid.UUID          `json:"repository_id,omitempty"`
	EmitterVersion string              `json:"emitter_version"`
	TrustLevel     hookenum.TrustLevel `json:"trust_level"`
}

// RunContext links the hook event to agent runtime refs owned by other domains.
type RunContext struct {
	RunID     uuid.UUID `json:"run_id"`
	SessionID string    `json:"session_id"`
	SlotID    uuid.UUID `json:"slot_id"`
	TurnID    *string   `json:"turn_id,omitempty"`
	RoleRef   *string   `json:"role_ref,omitempty"`
	StageRef  *string   `json:"stage_ref,omitempty"`
}

// ToolContext contains safe tool metadata for tool-scoped hook events.
type ToolContext struct {
	ToolName      string                `json:"tool_name"`
	ToolCategory  hookenum.ToolCategory `json:"tool_category"`
	ToolUseID     string                `json:"tool_use_id,omitempty"`
	CommandDigest *string               `json:"command_digest,omitempty"`
	PathCategory  string                `json:"path_category,omitempty"`
	MCPToolName   *string               `json:"mcp_tool_name,omitempty"`
}

// CapabilityContext contains refs to selected and materialized skills without content.
type CapabilityContext struct {
	CapabilityContextID  uuid.UUID  `json:"capability_context_id"`
	CapabilityContextRef string     `json:"capability_context_ref,omitempty"`
	CapabilityDigest     string     `json:"capability_digest,omitempty"`
	SelectedByRef        string     `json:"selected_by_ref"`
	MaterializedByRef    string     `json:"materialized_by_ref"`
	ScopeKind            string     `json:"scope_kind"`
	SkillRefs            []SkillRef `json:"skill_refs"`
}

// SkillRef identifies a selected skill by refs and digest only.
type SkillRef struct {
	SourceKind             string  `json:"source_kind"`
	SkillRef               string  `json:"skill_ref"`
	VersionRef             *string `json:"version_ref,omitempty"`
	SourceRef              *string `json:"source_ref,omitempty"`
	PackageRef             *string `json:"package_ref,omitempty"`
	PackageInstallationRef *string `json:"package_installation_ref,omitempty"`
	PackageVersionRef      *string `json:"package_version_ref,omitempty"`
	ManifestDigest         *string `json:"manifest_digest,omitempty"`
	CapabilityRef          *string `json:"capability_ref,omitempty"`
	CapabilityKind         *string `json:"capability_kind,omitempty"`
	PackageSlug            *string `json:"package_slug,omitempty"`
	PackageVersionLabel    *string `json:"package_version_label,omitempty"`
	InvocationPolicyRef    *string `json:"invocation_policy_ref,omitempty"`
	PolicySummaryDigest    *string `json:"policy_summary_digest,omitempty"`
	Digest                 string  `json:"digest"`
}

// SafePayload contains schema-approved event fields after local sanitizer processing.
type SafePayload struct {
	SafeSummary            string                  `json:"safe_summary,omitempty"`
	StartSource            string                  `json:"start_source,omitempty"`
	Model                  string                  `json:"model,omitempty"`
	PermissionMode         string                  `json:"permission_mode,omitempty"`
	WorkspaceRef           string                  `json:"workspace_ref,omitempty"`
	PromptDigest           string                  `json:"prompt_digest,omitempty"`
	PromptClass            string                  `json:"prompt_class,omitempty"`
	RiskClass              string                  `json:"risk_class,omitempty"`
	SanitizedReason        string                  `json:"sanitized_reason,omitempty"`
	PermissionClass        string                  `json:"permission_class,omitempty"`
	TimeoutBudgetMS        *int                    `json:"timeout_budget_ms,omitempty"`
	ExitStatus             *int                    `json:"exit_status,omitempty"`
	OutputDigest           string                  `json:"output_digest,omitempty"`
	BoundedError           *BoundedError           `json:"bounded_error,omitempty"`
	ProviderArtifactSignal *ProviderArtifactSignal `json:"provider_artifact_signal,omitempty"`
	RateLimitHint          *RateLimitHint          `json:"rate_limit_hint,omitempty"`
	PendingActionRefs      []string                `json:"pending_action_refs,omitempty"`
	CheckpointRef          *string                 `json:"checkpoint_ref,omitempty"`
	TurnStatus             string                  `json:"turn_status,omitempty"`
}

// BoundedError contains a safe, bounded error preview.
type BoundedError struct {
	ErrorClass  string  `json:"error_class"`
	SafeMessage string  `json:"safe_message"`
	Truncated   bool    `json:"truncated"`
	ErrorDigest *string `json:"error_digest,omitempty"`
}

// ProviderArtifactSignal contains a safe provider artifact hint.
type ProviderArtifactSignal struct {
	ProviderKind string `json:"provider_kind"`
	ArtifactKind string `json:"artifact_kind"`
	ArtifactRef  string `json:"artifact_ref"`
	SignalKind   string `json:"signal_kind"`
}

// RateLimitHint contains a safe provider limit signal.
type RateLimitHint struct {
	Scope     string  `json:"scope"`
	HintClass string  `json:"hint_class"`
	ResetRef  *string `json:"reset_ref,omitempty"`
}

// SanitizerReport records audit-safe sanitizer facts without rejected values.
type SanitizerReport struct {
	Result               hookenum.SanitizerResult `json:"result"`
	AppliedRules         []string                 `json:"applied_rules"`
	RedactionCount       int                      `json:"redaction_count"`
	TruncatedFields      []string                 `json:"truncated_fields"`
	RejectedFieldClasses []string                 `json:"rejected_field_classes"`
}

// DownstreamRoute describes a future owner route and safe parts without making the call.
type DownstreamRoute struct {
	Owner        hookenum.DownstreamOwner `json:"owner"`
	DeliveryMode hookenum.DeliveryMode    `json:"delivery_mode"`
	SafeParts    []string                 `json:"safe_parts"`
}

// HookHandlerResult is the normalized platform result for SubmitHookEvent.
type HookHandlerResult struct {
	Result            hookenum.HandlerResult `json:"result"`
	HookEventName     hookenum.HookEventName `json:"hook_event_name"`
	SystemMessage     string                 `json:"system_message,omitempty"`
	AdditionalContext string                 `json:"additional_context,omitempty"`
	DecisionReason    string                 `json:"decision_reason,omitempty"`
	StopReason        string                 `json:"stop_reason,omitempty"`
	OwnerDecisionRef  string                 `json:"owner_decision_ref,omitempty"`
	CorrelationID     string                 `json:"correlation_id"`
}
