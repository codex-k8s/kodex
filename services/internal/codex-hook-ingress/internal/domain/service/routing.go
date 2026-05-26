package service

import (
	"context"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// OwnerRoute dispatches one projected safe hook event to a service owner port.
type OwnerRoute interface {
	DispatchSafeHookEvent(ctx context.Context, event value.SafeHookEvent) error
}

// RouteRegistry selects enabled owner routes and dispatches projected safe parts.
type RouteRegistry struct {
	dispatchers map[hookenum.DownstreamOwner]OwnerRoute
}

// NewRouteRegistry creates a registry from explicit owner route ports.
func NewRouteRegistry(dispatchers map[hookenum.DownstreamOwner]OwnerRoute) *RouteRegistry {
	copied := make(map[hookenum.DownstreamOwner]OwnerRoute, len(dispatchers))
	for owner, dispatcher := range dispatchers {
		if dispatcher != nil {
			copied[owner] = dispatcher
		}
	}
	return &RouteRegistry{dispatchers: copied}
}

// NewDefaultRouteRegistry creates no-op ports for all schema-defined owner routes.
func NewDefaultRouteRegistry() *RouteRegistry {
	dispatchers := make(map[hookenum.DownstreamOwner]OwnerRoute, len(hookenum.DownstreamOwners()))
	for _, owner := range hookenum.DownstreamOwners() {
		dispatchers[owner] = NoopOwnerRoute{Owner: owner}
	}
	return NewRouteRegistry(dispatchers)
}

// Ready reports whether the registry is composed.
func (registry *RouteRegistry) Ready() bool {
	return registry != nil && registry.dispatchers != nil
}

// DispatchRoutes sends canonical safe event projections to enabled owner route ports.
func (registry *RouteRegistry) DispatchRoutes(ctx context.Context, cfg Config, envelope value.HookEnvelope) []value.RouteDeliveryResult {
	plan := canonicalRoutePlan(envelope.HookEventName)
	results := make([]value.RouteDeliveryResult, 0, len(plan)+len(envelope.DownstreamRoutes))
	results = append(results, unexpectedRouteDiagnostics(envelope, plan)...)
	for _, route := range plan {
		result := value.RouteDeliveryResult{
			Owner:        route.Owner,
			DeliveryMode: route.DeliveryMode,
			SafeParts:    cloneStrings(route.SafeParts),
		}
		if !cfg.routeEnabled(route.Owner) {
			result.Status = hookenum.RouteDeliveryStatusDisabled
			result.DiagnosticCode = value.RouteDiagnosticDisabled
			result.DiagnosticMessage = "route disabled by codex-hook-ingress config"
			results = append(results, result)
			continue
		}
		dispatcher, ok := registry.dispatchers[route.Owner]
		if !ok || dispatcher == nil {
			result.Status = hookenum.RouteDeliveryStatusUnsupported
			result.DiagnosticCode = value.RouteDiagnosticUnsupported
			result.DiagnosticMessage = "route owner port is not registered"
			results = append(results, result)
			continue
		}
		event := projectSafeHookEvent(envelope, route)
		if err := dispatcher.DispatchSafeHookEvent(ctx, event); err != nil {
			result.Status = hookenum.RouteDeliveryStatusFailed
			result.DiagnosticCode = value.RouteDiagnosticDownstreamFailed
			result.DiagnosticMessage = "route owner port returned a safe failure"
			result.Retryable = true
			results = append(results, result)
			continue
		}
		result.Status = hookenum.RouteDeliveryStatusDelivered
		result.DiagnosticCode = value.RouteDiagnosticDelivered
		results = append(results, result)
	}
	return results
}

func unexpectedRouteDiagnostics(envelope value.HookEnvelope, plan []value.DownstreamRoute) []value.RouteDeliveryResult {
	var diagnostics []value.RouteDeliveryResult
	for _, route := range envelope.DownstreamRoutes {
		canonical, ok := canonicalRouteForOwner(plan, route.Owner)
		if !ok {
			diagnostics = append(diagnostics, value.RouteDeliveryResult{
				Owner:             route.Owner,
				DeliveryMode:      route.DeliveryMode,
				Status:            hookenum.RouteDeliveryStatusUnsupported,
				DiagnosticCode:    value.RouteDiagnosticUnexpected,
				DiagnosticMessage: "route owner is not allowed for hook event",
				SafeParts:         cloneStrings(route.SafeParts),
			})
			continue
		}
		if canonical.DeliveryMode != route.DeliveryMode || !safePartsAllowed(route.SafeParts, canonical.SafeParts) {
			diagnostics = append(diagnostics, value.RouteDeliveryResult{
				Owner:             route.Owner,
				DeliveryMode:      route.DeliveryMode,
				Status:            hookenum.RouteDeliveryStatusUnsupported,
				DiagnosticCode:    value.RouteDiagnosticUnexpected,
				DiagnosticMessage: "route safe parts are not allowed for hook event",
				SafeParts:         cloneStrings(route.SafeParts),
			})
		}
	}
	return diagnostics
}

// NoopOwnerRoute is a placeholder port for owners whose physical transport is not selected yet.
type NoopOwnerRoute struct {
	Owner hookenum.DownstreamOwner
}

// DispatchSafeHookEvent accepts safe hook event projections without side effects.
func (NoopOwnerRoute) DispatchSafeHookEvent(_ context.Context, _ value.SafeHookEvent) error {
	return nil
}

func projectSafeHookEvent(envelope value.HookEnvelope, route value.DownstreamRoute) value.SafeHookEvent {
	event := value.SafeHookEvent{
		EventID:       envelope.EventID,
		HookEventName: envelope.HookEventName,
		Owner:         route.Owner,
		DeliveryMode:  route.DeliveryMode,
		SafeParts:     cloneStrings(route.SafeParts),
	}
	for _, rawPart := range route.SafeParts {
		switch hookenum.SafeEventPart(rawPart) {
		case hookenum.SafeEventPartSourceContext:
			sourceContext := envelope.SourceContext
			event.SourceContext = &sourceContext
		case hookenum.SafeEventPartRunContext:
			runContext := envelope.RunContext
			event.RunContext = &runContext
		case hookenum.SafeEventPartToolContext:
			if envelope.ToolContext != nil {
				toolContext := *envelope.ToolContext
				event.ToolContext = &toolContext
			}
		case hookenum.SafeEventPartCapabilityContext:
			if envelope.CapabilityContext != nil {
				capabilityContext := cloneCapabilityContext(*envelope.CapabilityContext)
				event.CapabilityContext = &capabilityContext
			}
		case hookenum.SafeEventPartSafeSummary:
			event.SafeSummary = envelope.SafePayload.SafeSummary
		case hookenum.SafeEventPartPromptDigest:
			event.PromptDigest = envelope.SafePayload.PromptDigest
		case hookenum.SafeEventPartRiskClass:
			event.RiskClass = envelope.SafePayload.RiskClass
		case hookenum.SafeEventPartSanitizedReason:
			event.SanitizedReason = envelope.SafePayload.SanitizedReason
		case hookenum.SafeEventPartBoundedError:
			if envelope.SafePayload.BoundedError != nil {
				boundedError := *envelope.SafePayload.BoundedError
				event.BoundedError = &boundedError
			}
		case hookenum.SafeEventPartProviderArtifactSignal:
			if envelope.SafePayload.ProviderArtifactSignal != nil {
				providerArtifactSignal := *envelope.SafePayload.ProviderArtifactSignal
				event.ProviderArtifactSignal = &providerArtifactSignal
			}
		case hookenum.SafeEventPartRateLimitHint:
			if envelope.SafePayload.RateLimitHint != nil {
				rateLimitHint := *envelope.SafePayload.RateLimitHint
				event.RateLimitHint = &rateLimitHint
			}
		case hookenum.SafeEventPartPendingActionRefs:
			event.PendingActionRefs = cloneStrings(envelope.SafePayload.PendingActionRefs)
		case hookenum.SafeEventPartCheckpointRef:
			if envelope.SafePayload.CheckpointRef != nil {
				checkpointRef := *envelope.SafePayload.CheckpointRef
				event.CheckpointRef = &checkpointRef
			}
		case hookenum.SafeEventPartSanitizerReport:
			sanitizerReport := cloneSanitizerReport(envelope.SanitizerReport)
			event.SanitizerReport = &sanitizerReport
		case hookenum.SafeEventPartPayloadDigest:
			event.PayloadDigest = envelope.PayloadDigest
		case hookenum.SafeEventPartCorrelationID:
			event.CorrelationID = envelope.CorrelationID
		}
	}
	return event
}

func canonicalRoutePlan(event hookenum.HookEventName) []value.DownstreamRoute {
	switch event {
	case hookenum.HookEventSessionStart:
		return []value.DownstreamRoute{
			canonicalRoute(hookenum.DownstreamOwnerAgentManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerRuntimeManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "correlation_id"),
		}
	case hookenum.HookEventUserPromptSubmit:
		return []value.DownstreamRoute{
			canonicalRoute(hookenum.DownstreamOwnerAgentManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "prompt_digest", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerInteractionHub, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "prompt_digest", "correlation_id"),
		}
	case hookenum.HookEventPreToolUse:
		return []value.DownstreamRoute{
			canonicalRoute(hookenum.DownstreamOwnerAgentManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "capability_context", "safe_summary", "risk_class", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerGovernanceManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "capability_context", "safe_summary", "risk_class", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerRuntimeManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "safe_summary", "risk_class", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerOperationsFeed, hookenum.DeliveryModeRealtime, "run_context", "tool_context", "safe_summary", "risk_class", "correlation_id"),
		}
	case hookenum.HookEventPermissionRequest:
		return []value.DownstreamRoute{
			canonicalRoute(hookenum.DownstreamOwnerGovernanceManager, hookenum.DeliveryModeSync, "source_context", "run_context", "tool_context", "capability_context", "risk_class", "sanitized_reason", "payload_digest", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerAgentManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "capability_context", "risk_class", "sanitized_reason", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerInteractionHub, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "risk_class", "sanitized_reason", "correlation_id"),
		}
	case hookenum.HookEventPostToolUse:
		return []value.DownstreamRoute{
			canonicalRoute(hookenum.DownstreamOwnerAgentManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "bounded_error", "provider_artifact_signal", "rate_limit_hint", "payload_digest", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerRuntimeManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "tool_context", "bounded_error", "payload_digest", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerProviderHub, hookenum.DeliveryModeAsync, "source_context", "run_context", "provider_artifact_signal", "rate_limit_hint", "payload_digest", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerOperationsFeed, hookenum.DeliveryModeRealtime, "run_context", "tool_context", "bounded_error", "provider_artifact_signal", "rate_limit_hint", "correlation_id"),
		}
	case hookenum.HookEventStop:
		return []value.DownstreamRoute{
			canonicalRoute(hookenum.DownstreamOwnerAgentManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "pending_action_refs", "checkpoint_ref", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerRuntimeManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "checkpoint_ref", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerProviderHub, hookenum.DeliveryModeAsync, "source_context", "run_context", "provider_artifact_signal", "rate_limit_hint", "pending_action_refs", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerGovernanceManager, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "pending_action_refs", "correlation_id"),
			canonicalRoute(hookenum.DownstreamOwnerInteractionHub, hookenum.DeliveryModeAsync, "source_context", "run_context", "safe_summary", "pending_action_refs", "correlation_id"),
		}
	default:
		return nil
	}
}

func canonicalRoute(owner hookenum.DownstreamOwner, deliveryMode hookenum.DeliveryMode, safeParts ...string) value.DownstreamRoute {
	return value.DownstreamRoute{
		Owner:        owner,
		DeliveryMode: deliveryMode,
		SafeParts:    cloneStrings(safeParts),
	}
}

func canonicalRouteForOwner(plan []value.DownstreamRoute, owner hookenum.DownstreamOwner) (value.DownstreamRoute, bool) {
	for _, route := range plan {
		if route.Owner == owner {
			return route, true
		}
	}
	return value.DownstreamRoute{}, false
}

func safePartsAllowed(requested []string, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, part := range allowed {
		allowedSet[part] = struct{}{}
	}
	for _, part := range requested {
		if _, ok := allowedSet[part]; !ok {
			return false
		}
	}
	return true
}

func cloneCapabilityContext(context value.CapabilityContext) value.CapabilityContext {
	context.SkillRefs = append([]value.SkillRef(nil), context.SkillRefs...)
	return context
}

func cloneSanitizerReport(report value.SanitizerReport) value.SanitizerReport {
	report.AppliedRules = cloneStrings(report.AppliedRules)
	report.TruncatedFields = cloneStrings(report.TruncatedFields)
	report.RejectedFieldClasses = cloneStrings(report.RejectedFieldClasses)
	return report
}

func cloneRouteDeliveryResults(results []value.RouteDeliveryResult) []value.RouteDeliveryResult {
	if len(results) == 0 {
		return nil
	}
	copied := make([]value.RouteDeliveryResult, 0, len(results))
	for _, result := range results {
		result.SafeParts = cloneStrings(result.SafeParts)
		copied = append(copied, result)
	}
	return copied
}

func countDeliveredRoutes(results []value.RouteDeliveryResult) int {
	delivered := 0
	for _, result := range results {
		if result.Delivered() {
			delivered++
		}
	}
	return delivered
}

func hasUndeliveredRoute(results []value.RouteDeliveryResult) bool {
	for _, result := range results {
		if !result.Delivered() {
			return true
		}
	}
	return false
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}
