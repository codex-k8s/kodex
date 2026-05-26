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
	if strings.TrimSpace(envelope.PayloadDigest) == "" || !validDigest(envelope.PayloadDigest) {
		return fmt.Errorf("%w: payload_digest is invalid", hookerrs.ErrInvalidArgument)
	}
	if strings.TrimSpace(envelope.CorrelationID) == "" {
		return fmt.Errorf("%w: correlation_id is required", hookerrs.ErrInvalidArgument)
	}
	if envelope.RetentionClass == "" {
		return fmt.Errorf("%w: retention_class is required", hookerrs.ErrInvalidArgument)
	}
	if len(envelope.DownstreamRoutes) == 0 {
		return fmt.Errorf("%w: downstream_routes is required", hookerrs.ErrInvalidArgument)
	}
	if err := validateEventSpecificFields(envelope); err != nil {
		return err
	}
	for _, route := range envelope.DownstreamRoutes {
		if route.Owner == "" || route.DeliveryMode == "" || len(route.SafeParts) == 0 {
			return fmt.Errorf("%w: downstream route is incomplete", hookerrs.ErrInvalidArgument)
		}
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
	if strings.TrimSpace(context.SourceRef) == "" || strings.TrimSpace(context.ActorRef) == "" {
		return fmt.Errorf("%w: source_ref and actor_ref are required", hookerrs.ErrInvalidArgument)
	}
	if context.OrganizationID == uuid.Nil || context.ProjectID == uuid.Nil {
		return fmt.Errorf("%w: organization_id and project_id are required", hookerrs.ErrInvalidArgument)
	}
	if strings.TrimSpace(context.EmitterVersion) == "" {
		return fmt.Errorf("%w: emitter_version is required", hookerrs.ErrInvalidArgument)
	}
	switch context.SourceKind {
	case hookenum.SourceKindHookEmitter, hookenum.SourceKindLocalSidecar, hookenum.SourceKindManagedHook:
	default:
		return fmt.Errorf("%w: source_kind is invalid", hookerrs.ErrInvalidArgument)
	}
	switch context.TrustLevel {
	case hookenum.TrustLevelManaged, hookenum.TrustLevelTrusted, hookenum.TrustLevelUntrustedRejected:
	default:
		return fmt.Errorf("%w: trust_level is invalid", hookerrs.ErrInvalidArgument)
	}
	return nil
}

func validateRunContext(context value.RunContext) error {
	if context.RunID == uuid.Nil || context.SlotID == uuid.Nil || strings.TrimSpace(context.SessionID) == "" {
		return fmt.Errorf("%w: run_id, session_id and slot_id are required", hookerrs.ErrInvalidArgument)
	}
	return nil
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
