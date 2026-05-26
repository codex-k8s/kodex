package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// DefaultEnvelopeValidator checks normalized envelope invariants mirrored from JSON Schema v1.
type DefaultEnvelopeValidator struct{}

var (
	sourceKindValues = enumSet(
		string(hookenum.SourceKindHookEmitter),
		string(hookenum.SourceKindLocalSidecar),
		string(hookenum.SourceKindManagedHook),
	)
	trustLevelValues = enumSet(
		string(hookenum.TrustLevelManaged),
		string(hookenum.TrustLevelTrusted),
		string(hookenum.TrustLevelUntrustedRejected),
	)
	toolCategoryValues = enumSet(
		string(hookenum.ToolCategoryShell),
		string(hookenum.ToolCategoryPatch),
		string(hookenum.ToolCategoryMCP),
		string(hookenum.ToolCategoryOther),
	)
	retentionClassValues = enumSet(
		string(hookenum.RetentionClassAudit),
		string(hookenum.RetentionClassOperational),
		string(hookenum.RetentionClassRealtime),
	)
	downstreamOwnerValues = enumSet(
		string(hookenum.DownstreamOwnerAgentManager),
		string(hookenum.DownstreamOwnerRuntimeManager),
		string(hookenum.DownstreamOwnerProviderHub),
		string(hookenum.DownstreamOwnerGovernanceManager),
		string(hookenum.DownstreamOwnerInteractionHub),
		string(hookenum.DownstreamOwnerOperationsFeed),
		string(hookenum.DownstreamOwnerAuditLog),
	)
	deliveryModeValues = enumSet(
		string(hookenum.DeliveryModeSync),
		string(hookenum.DeliveryModeAsync),
		string(hookenum.DeliveryModeRealtime),
		string(hookenum.DeliveryModeAudit),
	)
	sanitizerResultValues = enumSet(
		string(hookenum.SanitizerResultAccepted),
		string(hookenum.SanitizerResultRedacted),
		string(hookenum.SanitizerResultTruncated),
	)
	pathCategoryValues = enumSet("workspace", "docs", "generated", "config", "secrets_area", "unknown")
	scopeKindValues    = enumSet("platform", "organization", "project", "repository", "flow", "stage", "role")
	skillSourceValues  = enumSet("built_in", "repository", "package", "user")
	startSourceValues  = enumSet("startup", "resume", "clear")
	permissionModes    = enumSet("default", "acceptEdits", "plan", "dontAsk", "bypassPermissions")
	promptClassValues  = enumSet("user_instruction", "owner_feedback", "system_continuation", "unknown")
	riskClassValues    = enumSet("low", "medium", "high", "unknown")
	permissionClasses  = enumSet("shell_escalation", "managed_network", "file_write", "provider_action", "unknown")
	turnStatusValues   = enumSet("completed", "blocked", "failed", "waiting", "unknown")
	providerKinds      = enumSet("github", "gitlab", "unknown")
	artifactKinds      = enumSet("issue", "pull_request", "merge_request", "comment", "branch", "tag", "unknown")
	signalKinds        = enumSet("created", "updated", "mentioned", "rate_limit_hint", "unknown")
	rateLimitClasses   = enumSet("approaching_limit", "limited", "reset_observed", "unknown")
	safePartValues     = enumSet(
		"source_context",
		"run_context",
		"tool_context",
		"capability_context",
		"safe_summary",
		"prompt_digest",
		"risk_class",
		"sanitized_reason",
		"exit_status",
		"output_digest",
		"bounded_error",
		"provider_artifact_signal",
		"rate_limit_hint",
		"pending_action_refs",
		"checkpoint_ref",
		"sanitizer_report",
		"payload_digest",
		"correlation_id",
	)
)

// ValidateEnvelope validates the schema-shaped envelope before domain processing.
func (DefaultEnvelopeValidator) ValidateEnvelope(_ context.Context, cfg Config, envelope value.HookEnvelope) error {
	if envelope.EventID == uuid.Nil {
		return fmt.Errorf("%w: event_id is required", hookerrs.ErrInvalidArgument)
	}
	if strings.TrimSpace(envelope.SchemaVersion) != cfg.SchemaVersion {
		return fmt.Errorf("%w: unsupported schema_version", hookerrs.ErrInvalidArgument)
	}
	if !isConfiguredEvent(cfg, envelope.HookEventName) {
		if !hookenum.IsSupportedHookEvent(envelope.HookEventName) {
			return fmt.Errorf("%w: %s", hookerrs.ErrUnsupportedEvent, envelope.HookEventName)
		}
		return fmt.Errorf("%w: hook event is not enabled", hookerrs.ErrInvalidArgument)
	}
	if envelope.EventTime.IsZero() {
		return fmt.Errorf("%w: event_time is required", hookerrs.ErrInvalidArgument)
	}
	if err := validateSourceContext(envelope.SourceContext); err != nil {
		return err
	}
	if err := validateRunContext(envelope.RunContext); err != nil {
		return err
	}
	if envelope.ToolContext != nil {
		if err := validateToolContext(envelope.ToolContext, false); err != nil {
			return err
		}
	}
	if envelope.CapabilityContext != nil {
		if err := validateCapabilityContext(*envelope.CapabilityContext); err != nil {
			return err
		}
	}
	if err := validateSafePayload(envelope.SafePayload); err != nil {
		return err
	}
	if err := validateDigestField("payload_digest", envelope.PayloadDigest, true); err != nil {
		return err
	}
	if err := validateSanitizerReport(envelope.SanitizerReport); err != nil {
		return err
	}
	if err := validateStringField("correlation_id", envelope.CorrelationID, 1, 128); err != nil {
		return err
	}
	if !validCorrelationID(envelope.CorrelationID) {
		return fmt.Errorf("%w: correlation_id is invalid", hookerrs.ErrInvalidArgument)
	}
	if err := validateEnumField("retention_class", string(envelope.RetentionClass), retentionClassValues, true); err != nil {
		return err
	}
	if len(envelope.DownstreamRoutes) == 0 || len(envelope.DownstreamRoutes) > 8 {
		return fmt.Errorf("%w: downstream_routes is required", hookerrs.ErrInvalidArgument)
	}
	for _, route := range envelope.DownstreamRoutes {
		if err := validateDownstreamRoute(route); err != nil {
			return err
		}
	}
	if err := validateEventSpecificFields(envelope); err != nil {
		return err
	}
	return nil
}

// StaticSourceVerifier is a CHI-3 placeholder for future access/runtime binding checks.
type StaticSourceVerifier struct{}

// VerifySourceBinding accepts managed/trusted source refs and rejects untrusted refs.
func (StaticSourceVerifier) VerifySourceBinding(_ context.Context, check SourceBindingCheck) (SourceBindingDecision, error) {
	if check.SourceContext.TrustLevel == hookenum.TrustLevelUntrustedRejected {
		return SourceBindingDecision{}, hookerrs.ErrInvalidBinding
	}
	if strings.TrimSpace(check.SourceContext.SourceRef) == "" || strings.TrimSpace(check.SourceContext.ActorRef) == "" {
		return SourceBindingDecision{}, hookerrs.ErrInvalidBinding
	}
	if check.RunContext.RunID == uuid.Nil || check.RunContext.SlotID == uuid.Nil || strings.TrimSpace(check.RunContext.SessionID) == "" {
		return SourceBindingDecision{}, hookerrs.ErrInvalidBinding
	}
	return SourceBindingDecision{
		BindingRef: check.SourceContext.SourceRef + ":" + check.RunContext.RunID.String(),
		Accepted:   true,
	}, nil
}

// DefaultSanitizer verifies that an envelope stays inside sanitizer-contract.v1 limits.
type DefaultSanitizer struct{}

// VerifyBoundary checks size limits and rejected sanitizer facts without inspecting raw payload.
func (DefaultSanitizer) VerifyBoundary(_ context.Context, cfg Config, envelope value.HookEnvelope) (SanitizerDecision, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return SanitizerDecision{}, fmt.Errorf("%w: encode normalized envelope: %v", hookerrs.ErrInvalidArgument, err)
	}
	if len(encoded) > cfg.MaxEnvelopeBytes {
		return SanitizerDecision{}, hookerrs.ErrPayloadTooLarge
	}
	if envelope.SanitizerReport.Result == "" || len(envelope.SanitizerReport.AppliedRules) == 0 {
		return SanitizerDecision{}, fmt.Errorf("%w: sanitizer_report is incomplete", hookerrs.ErrPayloadRejected)
	}
	if envelope.SanitizerReport.RedactionCount < 0 || len(envelope.SanitizerReport.RejectedFieldClasses) > 0 {
		return SanitizerDecision{}, hookerrs.ErrPayloadRejected
	}
	if len([]byte(envelope.SafePayload.SafeSummary)) > cfg.MaxTextPreviewBytes {
		return SanitizerDecision{}, hookerrs.ErrPayloadRejected
	}
	if len([]byte(envelope.SafePayload.SanitizedReason)) > cfg.MaxTextPreviewBytes {
		return SanitizerDecision{}, hookerrs.ErrPayloadRejected
	}
	if envelope.SafePayload.BoundedError != nil && len([]byte(envelope.SafePayload.BoundedError.SafeMessage)) > cfg.MaxBoundedErrorBytes {
		return SanitizerDecision{}, hookerrs.ErrPayloadRejected
	}
	return SanitizerDecision{Accepted: true, EnvelopeBytes: len(encoded)}, nil
}

func validateSourceContext(context value.SourceContext) error {
	if err := validateStringField("source_ref", context.SourceRef, 1, 128); err != nil {
		return err
	}
	if err := validateStringField("actor_ref", context.ActorRef, 1, 128); err != nil {
		return err
	}
	if context.OrganizationID == uuid.Nil || context.ProjectID == uuid.Nil {
		return fmt.Errorf("%w: organization_id and project_id are required", hookerrs.ErrInvalidArgument)
	}
	if err := validateStringField("emitter_version", context.EmitterVersion, 1, 64); err != nil {
		return err
	}
	if err := validateEnumField("source_kind", string(context.SourceKind), sourceKindValues, true); err != nil {
		return err
	}
	if err := validateEnumField("trust_level", string(context.TrustLevel), trustLevelValues, true); err != nil {
		return err
	}
	return nil
}

func validateRunContext(context value.RunContext) error {
	if context.RunID == uuid.Nil || context.SlotID == uuid.Nil {
		return fmt.Errorf("%w: run_id, session_id and slot_id are required", hookerrs.ErrInvalidArgument)
	}
	if err := validateStringField("session_id", context.SessionID, 1, 128); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("turn_id", context.TurnID, 128); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("role_ref", context.RoleRef, 128); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("stage_ref", context.StageRef, 128); err != nil {
		return err
	}
	return nil
}

func validateToolContext(context *value.ToolContext, requireUseID bool) error {
	if context == nil {
		return fmt.Errorf("%w: tool_context is required", hookerrs.ErrInvalidArgument)
	}
	if err := validateStringField("tool_name", context.ToolName, 1, 128); err != nil {
		return err
	}
	if err := validateEnumField("tool_category", string(context.ToolCategory), toolCategoryValues, true); err != nil {
		return err
	}
	if requireUseID {
		if err := validateStringField("tool_use_id", context.ToolUseID, 1, 128); err != nil {
			return err
		}
	} else if err := validateOptionalStringField("tool_use_id", context.ToolUseID, 128); err != nil {
		return err
	}
	if err := validateOptionalDigestPtr("command_digest", context.CommandDigest); err != nil {
		return err
	}
	if err := validateEnumField("path_category", context.PathCategory, pathCategoryValues, false); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("mcp_tool_name", context.MCPToolName, 160); err != nil {
		return err
	}
	return nil
}

func validateCapabilityContext(context value.CapabilityContext) error {
	if context.CapabilityContextID == uuid.Nil {
		return fmt.Errorf("%w: capability_context_id is required", hookerrs.ErrInvalidArgument)
	}
	if err := validateStringField("selected_by_ref", context.SelectedByRef, 1, 160); err != nil {
		return err
	}
	if err := validateStringField("materialized_by_ref", context.MaterializedByRef, 1, 160); err != nil {
		return err
	}
	if err := validateEnumField("scope_kind", context.ScopeKind, scopeKindValues, true); err != nil {
		return err
	}
	if len(context.SkillRefs) > 32 {
		return fmt.Errorf("%w: skill_refs exceeds schema limit", hookerrs.ErrInvalidArgument)
	}
	for _, skillRef := range context.SkillRefs {
		if err := validateSkillRef(skillRef); err != nil {
			return err
		}
	}
	return nil
}

func validateSkillRef(skillRef value.SkillRef) error {
	if err := validateEnumField("skill_ref.source_kind", skillRef.SourceKind, skillSourceValues, true); err != nil {
		return err
	}
	if err := validateStringField("skill_ref", skillRef.SkillRef, 1, 160); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("skill_ref.version_ref", skillRef.VersionRef, 128); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("skill_ref.package_installation_ref", skillRef.PackageInstallationRef, 160); err != nil {
		return err
	}
	return validateDigestField("skill_ref.digest", skillRef.Digest, true)
}

func validateSafePayload(payload value.SafePayload) error {
	if err := validateOptionalStringField("safe_summary", payload.SafeSummary, 4096); err != nil {
		return err
	}
	if err := validateEnumField("start_source", payload.StartSource, startSourceValues, false); err != nil {
		return err
	}
	if err := validateOptionalStringField("model", payload.Model, 128); err != nil {
		return err
	}
	if err := validateEnumField("permission_mode", payload.PermissionMode, permissionModes, false); err != nil {
		return err
	}
	if err := validateOptionalStringField("workspace_ref", payload.WorkspaceRef, 160); err != nil {
		return err
	}
	if err := validateDigestField("prompt_digest", payload.PromptDigest, false); err != nil {
		return err
	}
	if err := validateEnumField("prompt_class", payload.PromptClass, promptClassValues, false); err != nil {
		return err
	}
	if err := validateEnumField("risk_class", payload.RiskClass, riskClassValues, false); err != nil {
		return err
	}
	if err := validateOptionalStringField("sanitized_reason", payload.SanitizedReason, 4096); err != nil {
		return err
	}
	if err := validateEnumField("permission_class", payload.PermissionClass, permissionClasses, false); err != nil {
		return err
	}
	if err := validateOptionalIntRange("timeout_budget_ms", payload.TimeoutBudgetMS, 1, 600000); err != nil {
		return err
	}
	if err := validateOptionalIntRange("exit_status", payload.ExitStatus, -1, 255); err != nil {
		return err
	}
	if err := validateDigestField("output_digest", payload.OutputDigest, false); err != nil {
		return err
	}
	if payload.BoundedError != nil {
		if err := validateBoundedError(*payload.BoundedError); err != nil {
			return err
		}
	}
	if payload.ProviderArtifactSignal != nil {
		if err := validateProviderArtifactSignal(*payload.ProviderArtifactSignal); err != nil {
			return err
		}
	}
	if payload.RateLimitHint != nil {
		if err := validateRateLimitHint(*payload.RateLimitHint); err != nil {
			return err
		}
	}
	if err := validateStringList("pending_action_refs", payload.PendingActionRefs, 0, 32, 160, nil); err != nil {
		return err
	}
	if err := validateOptionalStringPtr("checkpoint_ref", payload.CheckpointRef, 160); err != nil {
		return err
	}
	return validateEnumField("turn_status", payload.TurnStatus, turnStatusValues, false)
}

func validateBoundedError(boundedError value.BoundedError) error {
	if err := validateStringField("error_class", boundedError.ErrorClass, 1, 80); err != nil {
		return err
	}
	if err := validateStringField("safe_message", boundedError.SafeMessage, 0, 8192); err != nil {
		return err
	}
	return validateOptionalDigestPtr("error_digest", boundedError.ErrorDigest)
}

func validateProviderArtifactSignal(signal value.ProviderArtifactSignal) error {
	if err := validateEnumField("provider_kind", signal.ProviderKind, providerKinds, true); err != nil {
		return err
	}
	if err := validateEnumField("artifact_kind", signal.ArtifactKind, artifactKinds, true); err != nil {
		return err
	}
	if err := validateStringField("artifact_ref", signal.ArtifactRef, 1, 160); err != nil {
		return err
	}
	return validateEnumField("signal_kind", signal.SignalKind, signalKinds, true)
}

func validateRateLimitHint(hint value.RateLimitHint) error {
	if err := validateStringField("rate_limit.scope", hint.Scope, 1, 128); err != nil {
		return err
	}
	if err := validateEnumField("rate_limit.hint_class", hint.HintClass, rateLimitClasses, true); err != nil {
		return err
	}
	return validateOptionalStringPtr("rate_limit.reset_ref", hint.ResetRef, 128)
}

func validateSanitizerReport(report value.SanitizerReport) error {
	if err := validateEnumField("sanitizer_report.result", string(report.Result), sanitizerResultValues, true); err != nil {
		return err
	}
	if err := validateStringList("sanitizer_report.applied_rules", report.AppliedRules, 1, 64, 128, nil); err != nil {
		return err
	}
	if report.RedactionCount < 0 || report.RedactionCount > 100000 {
		return fmt.Errorf("%w: redaction_count is out of range", hookerrs.ErrInvalidArgument)
	}
	if err := validateStringList("sanitizer_report.truncated_fields", report.TruncatedFields, 0, 32, 160, nil); err != nil {
		return err
	}
	return validateStringList("sanitizer_report.rejected_field_classes", report.RejectedFieldClasses, 0, 32, 160, nil)
}

func validateDownstreamRoute(route value.DownstreamRoute) error {
	if err := validateEnumField("downstream_routes.owner", string(route.Owner), downstreamOwnerValues, true); err != nil {
		return err
	}
	if err := validateEnumField("downstream_routes.delivery_mode", string(route.DeliveryMode), deliveryModeValues, true); err != nil {
		return err
	}
	return validateStringList("downstream_routes.safe_parts", route.SafeParts, 1, 32, 160, safePartValues)
}

func validateStringField(name string, value string, minLength int, maxLength int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid utf-8", hookerrs.ErrInvalidArgument, name)
	}
	length := utf8.RuneCountInString(value)
	if length < minLength || length > maxLength {
		return fmt.Errorf("%w: %s length is outside schema limit", hookerrs.ErrInvalidArgument, name)
	}
	if minLength > 0 && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is required", hookerrs.ErrInvalidArgument, name)
	}
	return nil
}

func validateOptionalStringField(name string, value string, maxLength int) error {
	if value == "" {
		return nil
	}
	return validateStringField(name, value, 1, maxLength)
}

func validateOptionalStringPtr(name string, value *string, maxLength int) error {
	if value == nil {
		return nil
	}
	return validateStringField(name, *value, 1, maxLength)
}

func validateEnumField(name string, value string, allowed map[string]struct{}, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%w: %s is required", hookerrs.ErrInvalidArgument, name)
		}
		return nil
	}
	if _, ok := allowed[value]; !ok {
		return fmt.Errorf("%w: %s is invalid", hookerrs.ErrInvalidArgument, name)
	}
	return nil
}

func validateDigestField(name string, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%w: %s is required", hookerrs.ErrInvalidArgument, name)
		}
		return nil
	}
	if !validDigest(value) {
		return fmt.Errorf("%w: %s is invalid", hookerrs.ErrInvalidArgument, name)
	}
	return nil
}

func validateOptionalDigestPtr(name string, value *string) error {
	if value == nil {
		return nil
	}
	return validateDigestField(name, *value, true)
}

func validateOptionalIntRange(name string, value *int, minValue int, maxValue int) error {
	if value == nil {
		return nil
	}
	if *value < minValue || *value > maxValue {
		return fmt.Errorf("%w: %s is out of range", hookerrs.ErrInvalidArgument, name)
	}
	return nil
}

func validateStringList(name string, values []string, minItems int, maxItems int, maxLength int, allowed map[string]struct{}) error {
	if len(values) < minItems || len(values) > maxItems {
		return fmt.Errorf("%w: %s item count is outside schema limit", hookerrs.ErrInvalidArgument, name)
	}
	for _, value := range values {
		if err := validateStringField(name, value, 1, maxLength); err != nil {
			return err
		}
		if allowed != nil {
			if _, ok := allowed[value]; !ok {
				return fmt.Errorf("%w: %s contains unsupported value", hookerrs.ErrInvalidArgument, name)
			}
		}
	}
	return nil
}

func enumSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func validCorrelationID(value string) bool {
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '_', '.', ':', '-':
			continue
		default:
			return false
		}
	}
	return true
}

func validateEventSpecificFields(envelope value.HookEnvelope) error {
	switch envelope.HookEventName {
	case hookenum.HookEventSessionStart:
		if envelope.SafePayload.StartSource == "" || envelope.SafePayload.Model == "" || envelope.SafePayload.WorkspaceRef == "" {
			return fmt.Errorf("%w: SessionStart safe payload is incomplete", hookerrs.ErrInvalidArgument)
		}
	case hookenum.HookEventUserPromptSubmit:
		if envelope.SafePayload.PromptDigest == "" || envelope.SafePayload.PromptClass == "" || envelope.SafePayload.SafeSummary == "" {
			return fmt.Errorf("%w: UserPromptSubmit safe payload is incomplete", hookerrs.ErrInvalidArgument)
		}
	case hookenum.HookEventPreToolUse:
		if envelope.ToolContext == nil || envelope.ToolContext.ToolName == "" || envelope.ToolContext.ToolCategory == "" || envelope.ToolContext.ToolUseID == "" {
			return fmt.Errorf("%w: PreToolUse tool context is incomplete", hookerrs.ErrInvalidArgument)
		}
		if envelope.SafePayload.SafeSummary == "" || envelope.SafePayload.RiskClass == "" {
			return fmt.Errorf("%w: PreToolUse safe payload is incomplete", hookerrs.ErrInvalidArgument)
		}
	case hookenum.HookEventPermissionRequest:
		if envelope.ToolContext == nil || envelope.ToolContext.ToolName == "" || envelope.ToolContext.ToolCategory == "" {
			return fmt.Errorf("%w: PermissionRequest tool context is incomplete", hookerrs.ErrInvalidArgument)
		}
		if envelope.SafePayload.SanitizedReason == "" || envelope.SafePayload.RiskClass == "" || envelope.SafePayload.PermissionClass == "" || envelope.SafePayload.TimeoutBudgetMS == nil {
			return fmt.Errorf("%w: PermissionRequest safe payload is incomplete", hookerrs.ErrInvalidArgument)
		}
		if envelope.RetentionClass != hookenum.RetentionClassAudit {
			return fmt.Errorf("%w: PermissionRequest must use audit retention", hookerrs.ErrInvalidArgument)
		}
	case hookenum.HookEventPostToolUse:
		if envelope.ToolContext == nil || envelope.ToolContext.ToolName == "" || envelope.ToolContext.ToolCategory == "" || envelope.ToolContext.ToolUseID == "" {
			return fmt.Errorf("%w: PostToolUse tool context is incomplete", hookerrs.ErrInvalidArgument)
		}
		if envelope.SafePayload.ExitStatus == nil || envelope.SafePayload.OutputDigest == "" {
			return fmt.Errorf("%w: PostToolUse safe payload is incomplete", hookerrs.ErrInvalidArgument)
		}
	case hookenum.HookEventStop:
		if envelope.SafePayload.TurnStatus == "" || envelope.SafePayload.SafeSummary == "" {
			return fmt.Errorf("%w: Stop safe payload is incomplete", hookerrs.ErrInvalidArgument)
		}
	default:
		return fmt.Errorf("%w: %s", hookerrs.ErrUnsupportedEvent, envelope.HookEventName)
	}
	return nil
}

func isConfiguredEvent(cfg Config, event hookenum.HookEventName) bool {
	for _, supported := range cfg.SupportedEvents {
		if supported == event {
			return true
		}
	}
	return false
}

func validDigest(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	if len(value) != len("sha256:0000000000000000000000000000000000000000000000000000000000000000") {
		return false
	}
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range strings.TrimPrefix(value, "sha256:") {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
