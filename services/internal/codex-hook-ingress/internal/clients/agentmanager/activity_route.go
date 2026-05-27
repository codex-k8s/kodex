// Package agentmanager contains codex-hook-ingress owner adapters for agent-manager.
package agentmanager

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
	"google.golang.org/grpc"
)

const (
	activityRouteActorType = "service"
	activityRouteActorID   = "codex-hook-ingress"
	activityRouteReason    = "codex-hook-ingress.safe-activity-route"
)

// ActivityRecorder is the generated agent-manager RecordAgentActivity call shape.
type ActivityRecorder interface {
	RecordAgentActivity(context.Context, *agentsv1.RecordAgentActivityRequest, ...grpc.CallOption) (*agentsv1.AgentActivityResponse, error)
}

// ActivityRoute projects safe tool hook events into agent-manager.RecordAgentActivity.
type ActivityRoute struct {
	recorder ActivityRecorder
}

var _ interface {
	DispatchSafeHookEvent(context.Context, value.SafeHookEvent) error
} = (*ActivityRoute)(nil)

// NewActivityRoute creates an owner route for safe agent activity timeline writes.
func NewActivityRoute(recorder ActivityRecorder) *ActivityRoute {
	return &ActivityRoute{recorder: recorder}
}

// DispatchSafeHookEvent records only agreed safe activity events; other agent-manager hooks stay placeholder-only.
func (route *ActivityRoute) DispatchSafeHookEvent(ctx context.Context, event value.SafeHookEvent) error {
	if !isAgentActivityEvent(event.HookEventName) {
		return nil
	}
	if route == nil || route.recorder == nil {
		return hookerrs.ErrOwnerUnavailable
	}
	request, err := recordAgentActivityRequest(event)
	if err != nil {
		return err
	}
	_, err = route.recorder.RecordAgentActivity(ctx, request)
	return err
}

// UnavailableRecorder is a safe process-composition stub until the real owner client is configured.
type UnavailableRecorder struct{}

// RecordAgentActivity reports that the agent-manager owner port is not wired yet.
func (UnavailableRecorder) RecordAgentActivity(context.Context, *agentsv1.RecordAgentActivityRequest, ...grpc.CallOption) (*agentsv1.AgentActivityResponse, error) {
	return nil, hookerrs.ErrOwnerUnavailable
}

func recordAgentActivityRequest(event value.SafeHookEvent) (*agentsv1.RecordAgentActivityRequest, error) {
	if event.RunContext == nil || event.ToolContext == nil || event.EventTime.IsZero() {
		return nil, hookerrs.ErrInvalidArgument
	}
	if event.HookEventName == hookenum.HookEventPostToolUse && (event.ExitStatus == nil || strings.TrimSpace(event.OutputDigest) == "") {
		return nil, hookerrs.ErrInvalidArgument
	}
	kind, status := activityKindAndStatus(event)
	if kind == agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_UNSPECIFIED {
		return nil, hookerrs.ErrInvalidArgument
	}
	refsJSON, err := marshalActivitySafeRefs(event)
	if err != nil {
		return nil, err
	}
	detailsJSON, err := marshalActivitySafeDetails(event)
	if err != nil {
		return nil, err
	}
	startedAt := event.EventTime.UTC().Format(time.RFC3339Nano)
	request := &agentsv1.RecordAgentActivityRequest{
		Meta:            activityCommandMeta(event),
		SessionId:       strings.TrimSpace(event.RunContext.SessionID),
		RunId:           optionalString(event.RunContext.RunID.String()),
		TurnId:          event.RunContext.TurnID,
		ToolUseId:       optionalString(event.ToolContext.ToolUseID),
		ActivityKind:    kind,
		ToolName:        optionalString(event.ToolContext.ToolName),
		ToolCategory:    optionalString(string(event.ToolContext.ToolCategory)),
		Status:          status,
		StartedAt:       optionalString(startedAt),
		SafeSummary:     optionalString(event.SafeSummary),
		PayloadDigest:   optionalString(event.PayloadDigest),
		BoundedError:    activityBoundedError(event.BoundedError),
		SafeRefsJson:    refsJSON,
		SafeDetailsJson: detailsJSON,
		CorrelationId:   optionalString(event.CorrelationID),
	}
	if event.HookEventName == hookenum.HookEventPostToolUse {
		request.FinishedAt = optionalString(startedAt)
		zero := int64(0)
		request.DurationMs = &zero
	}
	return request, nil
}

func activityKindAndStatus(event value.SafeHookEvent) (agentsv1.AgentActivityKind, agentsv1.AgentActivityStatus) {
	switch event.HookEventName {
	case hookenum.HookEventPreToolUse:
		return agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_USE, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_STARTED
	case hookenum.HookEventPostToolUse:
		if postToolUseFailed(event) {
			return agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_RESULT, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_FAILED
		}
		return agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_RESULT, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED
	default:
		return agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_UNSPECIFIED, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_UNSPECIFIED
	}
}

func postToolUseFailed(event value.SafeHookEvent) bool {
	return event.BoundedError != nil || (event.ExitStatus != nil && *event.ExitStatus != 0)
}

func activityCommandMeta(event value.SafeHookEvent) *agentsv1.CommandMeta {
	idempotencyKey := "codex-hook-ingress:" + event.EventID.String() + ":" + string(event.HookEventName) + ":agent-activity"
	requestID := event.CorrelationID
	if strings.TrimSpace(requestID) == "" {
		requestID = event.EventID.String()
	}
	meta := &agentsv1.CommandMeta{
		IdempotencyKey: optionalString(idempotencyKey),
		Actor: &agentsv1.Actor{
			Type: activityRouteActorType,
			Id:   activityRouteActorID,
		},
		Reason:    activityRouteReason,
		RequestId: requestID,
		RequestContext: &agentsv1.RequestContext{
			Source: activityRouteActorID,
		},
	}
	if event.CorrelationID != "" {
		meta.RequestContext.TraceId = optionalString(event.CorrelationID)
	}
	if event.RunContext != nil && event.RunContext.SessionID != "" {
		meta.RequestContext.SessionId = optionalString(event.RunContext.SessionID)
	}
	return meta
}

type activitySafeRefs struct {
	EventRef                     string             `json:"event_ref,omitempty"`
	SourceRef                    string             `json:"source_ref,omitempty"`
	SessionRef                   string             `json:"session_ref,omitempty"`
	RunRef                       string             `json:"run_ref,omitempty"`
	SlotRef                      string             `json:"slot_ref,omitempty"`
	TurnRef                      string             `json:"turn_ref,omitempty"`
	ToolUseRef                   string             `json:"tool_use_ref,omitempty"`
	CapabilityContextRef         string             `json:"capability_context_ref,omitempty"`
	CapabilityDigestRef          string             `json:"capability_digest_ref,omitempty"`
	CapabilitySelectionRef       string             `json:"capability_selection_ref,omitempty"`
	CapabilityMaterializationRef string             `json:"capability_materialization_ref,omitempty"`
	SkillRefs                    []activitySkillRef `json:"skill_refs,omitempty"`
	ProviderArtifactRef          string             `json:"provider_artifact_ref,omitempty"`
	RateLimitResetRef            string             `json:"rate_limit_reset_ref,omitempty"`
	CheckpointRef                string             `json:"checkpoint_ref,omitempty"`
	CorrelationRef               string             `json:"correlation_ref,omitempty"`
}

type activitySkillRef struct {
	SkillRef               string `json:"skill_ref,omitempty"`
	SourceRef              string `json:"source_ref,omitempty"`
	VersionRef             string `json:"version_ref,omitempty"`
	PackageRef             string `json:"package_ref,omitempty"`
	PackageInstallationRef string `json:"package_installation_ref,omitempty"`
	PackageVersionRef      string `json:"package_version_ref,omitempty"`
	ManifestDigestRef      string `json:"manifest_digest_ref,omitempty"`
	CapabilityRef          string `json:"capability_ref,omitempty"`
	InvocationPolicyRef    string `json:"invocation_policy_ref,omitempty"`
	PolicySummaryDigestRef string `json:"policy_summary_digest_ref,omitempty"`
	DigestRef              string `json:"digest_ref,omitempty"`
}

type activitySkillDetails struct {
	SkillRef            string `json:"skill_ref,omitempty"`
	SourceKind          string `json:"source_kind,omitempty"`
	CapabilityKind      string `json:"capability_kind,omitempty"`
	PackageSlug         string `json:"package_slug,omitempty"`
	PackageVersionLabel string `json:"package_version_label,omitempty"`
}

type activitySafeDetails struct {
	HookEventName         string                 `json:"hook_event_name,omitempty"`
	DeliveryMode          string                 `json:"delivery_mode,omitempty"`
	ToolCategory          string                 `json:"tool_category,omitempty"`
	PathCategory          string                 `json:"path_category,omitempty"`
	MCPToolName           string                 `json:"mcp_tool_name,omitempty"`
	CommandDigest         string                 `json:"command_digest,omitempty"`
	RiskClass             string                 `json:"risk_class,omitempty"`
	ProviderKind          string                 `json:"provider_kind,omitempty"`
	ProviderArtifactKind  string                 `json:"provider_artifact_kind,omitempty"`
	ProviderSignalKind    string                 `json:"provider_signal_kind,omitempty"`
	RateLimitScope        string                 `json:"rate_limit_scope,omitempty"`
	RateLimitHintClass    string                 `json:"rate_limit_hint_class,omitempty"`
	ExitStatus            *int                   `json:"exit_status,omitempty"`
	OutputDigest          string                 `json:"output_digest,omitempty"`
	BoundedErrorClass     string                 `json:"bounded_error_class,omitempty"`
	BoundedErrorDigest    string                 `json:"bounded_error_digest,omitempty"`
	BoundedErrorTruncated bool                   `json:"bounded_error_truncated,omitempty"`
	CapabilityScopeKind   string                 `json:"capability_scope_kind,omitempty"`
	SkillDetails          []activitySkillDetails `json:"skill_details,omitempty"`
}

func marshalActivitySafeRefs(event value.SafeHookEvent) (string, error) {
	refs := activitySafeRefs{
		EventRef:       prefixedRef("hook-event", event.EventID.String()),
		CorrelationRef: prefixedRef("correlation", event.CorrelationID),
	}
	if event.SourceContext != nil {
		refs.SourceRef = prefixedRef("source", event.SourceContext.SourceRef)
	}
	if event.RunContext != nil {
		refs.SessionRef = prefixedRef("session", event.RunContext.SessionID)
		refs.RunRef = prefixedRef("run", event.RunContext.RunID.String())
		refs.SlotRef = prefixedRef("slot", event.RunContext.SlotID.String())
		if event.RunContext.TurnID != nil {
			refs.TurnRef = prefixedRef("turn", *event.RunContext.TurnID)
		}
	}
	if event.ToolContext != nil {
		refs.ToolUseRef = prefixedRef("tool-use", event.ToolContext.ToolUseID)
	}
	if event.CapabilityContext != nil {
		refs.CapabilityContextRef = capabilityContextRef(*event.CapabilityContext)
		refs.CapabilityDigestRef = prefixedRef("digest", event.CapabilityContext.CapabilityDigest)
		refs.CapabilitySelectionRef = prefixedRef("capability-selection", event.CapabilityContext.SelectedByRef)
		refs.CapabilityMaterializationRef = prefixedRef("capability-materialization", event.CapabilityContext.MaterializedByRef)
		refs.SkillRefs = activitySkillRefs(event.CapabilityContext.SkillRefs)
	}
	if event.ProviderArtifactSignal != nil {
		refs.ProviderArtifactRef = prefixedRef("provider-artifact", event.ProviderArtifactSignal.ArtifactRef)
	}
	if event.RateLimitHint != nil && event.RateLimitHint.ResetRef != nil {
		refs.RateLimitResetRef = prefixedRef("rate-limit-reset", *event.RateLimitHint.ResetRef)
	}
	if event.CheckpointRef != nil {
		refs.CheckpointRef = prefixedRef("checkpoint", *event.CheckpointRef)
	}
	return compactActivityJSON(refs)
}

func activitySkillRefs(skillRefs []value.SkillRef) []activitySkillRef {
	if len(skillRefs) == 0 {
		return nil
	}
	refs := make([]activitySkillRef, 0, len(skillRefs))
	for _, skill := range skillRefs {
		refs = append(refs, activitySkillRef{
			SkillRef:               prefixedRef("skill", skill.SkillRef),
			SourceRef:              prefixedRef("source", stringPtrValue(skill.SourceRef)),
			VersionRef:             prefixedRef("skill-version", stringPtrValue(skill.VersionRef)),
			PackageRef:             prefixedRef("package", stringPtrValue(skill.PackageRef)),
			PackageInstallationRef: prefixedRef("package-installation", stringPtrValue(skill.PackageInstallationRef)),
			PackageVersionRef:      prefixedRef("package-version", stringPtrValue(skill.PackageVersionRef)),
			ManifestDigestRef:      prefixedRef("digest", stringPtrValue(skill.ManifestDigest)),
			CapabilityRef:          prefixedRef("capability", stringPtrValue(skill.CapabilityRef)),
			InvocationPolicyRef:    prefixedRef("policy", stringPtrValue(skill.InvocationPolicyRef)),
			PolicySummaryDigestRef: prefixedRef("digest", stringPtrValue(skill.PolicySummaryDigest)),
			DigestRef:              prefixedRef("digest", skill.Digest),
		})
	}
	return refs
}

func capabilityContextRef(context value.CapabilityContext) string {
	if strings.TrimSpace(context.CapabilityContextRef) != "" {
		return prefixedRef("capability-context", context.CapabilityContextRef)
	}
	return prefixedRef("capability-context", context.CapabilityContextID.String())
}

func marshalActivitySafeDetails(event value.SafeHookEvent) (string, error) {
	details := activitySafeDetails{
		HookEventName: string(event.HookEventName),
		DeliveryMode:  string(event.DeliveryMode),
		RiskClass:     event.RiskClass,
	}
	if event.ToolContext != nil {
		details.ToolCategory = string(event.ToolContext.ToolCategory)
		details.PathCategory = event.ToolContext.PathCategory
		details.MCPToolName = stringPtrValue(event.ToolContext.MCPToolName)
		details.CommandDigest = stringPtrValue(event.ToolContext.CommandDigest)
	}
	if event.ExitStatus != nil {
		exitStatus := *event.ExitStatus
		details.ExitStatus = &exitStatus
	}
	details.OutputDigest = strings.TrimSpace(event.OutputDigest)
	if event.ProviderArtifactSignal != nil {
		details.ProviderKind = event.ProviderArtifactSignal.ProviderKind
		details.ProviderArtifactKind = event.ProviderArtifactSignal.ArtifactKind
		details.ProviderSignalKind = event.ProviderArtifactSignal.SignalKind
	}
	if event.RateLimitHint != nil {
		details.RateLimitScope = event.RateLimitHint.Scope
		details.RateLimitHintClass = event.RateLimitHint.HintClass
	}
	if event.BoundedError != nil {
		details.BoundedErrorClass = event.BoundedError.ErrorClass
		details.BoundedErrorDigest = stringPtrValue(event.BoundedError.ErrorDigest)
		details.BoundedErrorTruncated = event.BoundedError.Truncated
	}
	if event.CapabilityContext != nil {
		details.CapabilityScopeKind = event.CapabilityContext.ScopeKind
		details.SkillDetails = activitySkillDetailsList(event.CapabilityContext.SkillRefs)
	}
	return compactActivityJSON(details)
}

func activitySkillDetailsList(skillRefs []value.SkillRef) []activitySkillDetails {
	if len(skillRefs) == 0 {
		return nil
	}
	details := make([]activitySkillDetails, 0, len(skillRefs))
	for _, skill := range skillRefs {
		details = append(details, activitySkillDetails{
			SkillRef:            prefixedRef("skill", skill.SkillRef),
			SourceKind:          strings.TrimSpace(skill.SourceKind),
			CapabilityKind:      strings.TrimSpace(stringPtrValue(skill.CapabilityKind)),
			PackageSlug:         strings.TrimSpace(stringPtrValue(skill.PackageSlug)),
			PackageVersionLabel: strings.TrimSpace(stringPtrValue(skill.PackageVersionLabel)),
		})
	}
	return details
}

func compactActivityJSON(payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", hookerrs.ErrInvalidArgument
	}
	if len(data) == 0 || string(data) == "null" {
		return "{}", nil
	}
	return string(data), nil
}

func activityBoundedError(err *value.BoundedError) *string {
	if err == nil {
		return nil
	}
	parts := make([]string, 0, 3)
	if strings.TrimSpace(err.ErrorClass) != "" {
		parts = append(parts, "class="+strings.TrimSpace(err.ErrorClass))
	}
	if err.ErrorDigest != nil && strings.TrimSpace(*err.ErrorDigest) != "" {
		parts = append(parts, "digest="+strings.TrimSpace(*err.ErrorDigest))
	}
	if err.Truncated {
		parts = append(parts, "truncated=true")
	}
	return optionalString(strings.Join(parts, " "))
}

func isAgentActivityEvent(event hookenum.HookEventName) bool {
	return event == hookenum.HookEventPreToolUse || event == hookenum.HookEventPostToolUse
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func prefixedRef(prefix string, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, ":") {
		return trimmed
	}
	return prefix + ":" + trimmed
}
