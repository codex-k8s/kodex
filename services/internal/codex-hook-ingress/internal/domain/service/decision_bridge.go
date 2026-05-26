package service

import (
	"context"
	"errors"
	"time"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// OwnerDecisionBridge coordinates decision events with owner ports without owning decisions.
type OwnerDecisionBridge struct {
	ports map[hookenum.DownstreamOwner]DecisionOwnerPort
}

// NewOwnerDecisionBridge creates a decision bridge from explicit owner ports.
func NewOwnerDecisionBridge(ports map[hookenum.DownstreamOwner]DecisionOwnerPort) *OwnerDecisionBridge {
	return &OwnerDecisionBridge{ports: cloneDecisionOwnerPorts(ports)}
}

// NewDefaultDecisionOwnerPorts returns safe unavailable stubs until owner clients are wired.
func NewDefaultDecisionOwnerPorts() map[hookenum.DownstreamOwner]DecisionOwnerPort {
	return NewUnavailableDecisionOwnerPorts(
		hookenum.DownstreamOwnerGovernanceManager,
		hookenum.DownstreamOwnerAgentManager,
		hookenum.DownstreamOwnerInteractionHub,
	)
}

// NewUnavailableDecisionOwnerPorts creates explicit stubs that fail safely instead of reporting unsupported routes.
func NewUnavailableDecisionOwnerPorts(owners ...hookenum.DownstreamOwner) map[hookenum.DownstreamOwner]DecisionOwnerPort {
	ports := map[hookenum.DownstreamOwner]DecisionOwnerPort{}
	for _, owner := range owners {
		ports[owner] = UnavailableDecisionOwnerPort{Owner: owner}
	}
	return ports
}

// Ready reports whether the bridge is composed.
func (bridge *OwnerDecisionBridge) Ready() bool {
	return bridge != nil && bridge.ports != nil
}

// Evaluate sends decision-scoped events to owner ports and returns a safe handler result.
func (bridge *OwnerDecisionBridge) Evaluate(ctx context.Context, cfg Config, envelope value.HookEnvelope) (DecisionBridgeResult, bool, error) {
	owners := decisionOwners(cfg, envelope)
	if len(owners) == 0 {
		return DecisionBridgeResult{}, false, nil
	}
	timeout := decisionTimeout(cfg, envelope)
	decisionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := DecisionBridgeResult{
		HandlerResult: value.HookHandlerResult{
			Result:        defaultBridgeResult(envelope),
			HookEventName: envelope.HookEventName,
			CorrelationID: envelope.CorrelationID,
		},
		HandledOwners: cloneOwners(owners),
	}
	request := buildHookDecisionRequest(envelope, timeout)
	failed := false
	timeoutHit := false
	for _, owner := range owners {
		diagnostic := value.RouteDeliveryResult{
			Owner:        owner,
			DeliveryMode: hookenum.DeliveryModeSync,
			SafeParts:    decisionSafeParts(envelope.HookEventName, owner),
		}
		if !cfg.routeEnabled(owner) {
			failed = true
			diagnostic.Status = hookenum.RouteDeliveryStatusDisabled
			diagnostic.DiagnosticCode = value.RouteDiagnosticDisabled
			diagnostic.DiagnosticMessage = "decision owner route is disabled"
			result.RouteDiagnostics = append(result.RouteDiagnostics, diagnostic)
			continue
		}
		port, ok := bridge.ports[owner]
		if !ok || port == nil {
			failed = true
			diagnostic.Status = hookenum.RouteDeliveryStatusUnsupported
			diagnostic.DiagnosticCode = value.RouteDiagnosticUnsupported
			diagnostic.DiagnosticMessage = "decision owner port is not registered"
			result.RouteDiagnostics = append(result.RouteDiagnostics, diagnostic)
			continue
		}
		request.Owner = owner
		ownerDecision, err := port.RequestHookDecision(decisionCtx, request)
		if err != nil {
			failed = true
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, hookerrs.ErrDecisionTimeout) || decisionCtx.Err() != nil {
				timeoutHit = true
				diagnostic.DiagnosticCode = value.RouteDiagnosticDecisionTimeout
			} else {
				diagnostic.DiagnosticCode = value.RouteDiagnosticOwnerUnavailable
				diagnostic.Retryable = true
			}
			diagnostic.Status = hookenum.RouteDeliveryStatusFailed
			diagnostic.DiagnosticMessage = "decision owner port returned a safe failure"
			result.RouteDiagnostics = append(result.RouteDiagnostics, diagnostic)
			continue
		}
		diagnostic.Status = hookenum.RouteDeliveryStatusDelivered
		diagnostic.DiagnosticCode = decisionDiagnosticCode(ownerDecision.Result)
		diagnostic.Retryable = ownerDecision.Retryable
		result.RouteDiagnostics = append(result.RouteDiagnostics, diagnostic)
		if owner == hookenum.DownstreamOwnerGovernanceManager {
			result.HandlerResult.Result = normalizeOwnerDecision(ownerDecision.Result)
			result.HandlerResult.OwnerDecisionRef = ownerDecision.OwnerDecisionRef
			result.HandlerResult.DecisionReason = boundedDecisionReason(ownerDecision.DecisionReason)
		} else if result.HandlerResult.OwnerDecisionRef == "" {
			result.HandlerResult.OwnerDecisionRef = ownerDecision.OwnerDecisionRef
		}
	}
	if failed {
		result.HandlerResult = applyDecisionFailurePolicy(result.HandlerResult, cfg, envelope.HookEventName, timeoutHit)
	}
	return result, true, nil
}

func decisionOwners(cfg Config, envelope value.HookEnvelope) []hookenum.DownstreamOwner {
	switch envelope.HookEventName {
	case hookenum.HookEventPermissionRequest:
		return []hookenum.DownstreamOwner{
			hookenum.DownstreamOwnerGovernanceManager,
			hookenum.DownstreamOwnerAgentManager,
			hookenum.DownstreamOwnerInteractionHub,
		}
	case hookenum.HookEventPreToolUse:
		if preToolUseNeedsDecision(cfg, envelope.SafePayload.RiskClass) {
			return []hookenum.DownstreamOwner{hookenum.DownstreamOwnerGovernanceManager}
		}
	}
	return nil
}

func preToolUseNeedsDecision(cfg Config, riskClass string) bool {
	for _, configured := range cfg.PreToolUseDecisionRiskClasses {
		if configured == riskClass {
			return true
		}
	}
	return false
}

func decisionTimeout(cfg Config, envelope value.HookEnvelope) time.Duration {
	timeout := cfg.DecisionBridgeTimeout
	if envelope.SafePayload.TimeoutBudgetMS != nil {
		envelopeTimeout := time.Duration(*envelope.SafePayload.TimeoutBudgetMS) * time.Millisecond
		if envelopeTimeout > 0 && envelopeTimeout < timeout {
			timeout = envelopeTimeout
		}
	}
	return timeout
}

func buildHookDecisionRequest(envelope value.HookEnvelope, timeout time.Duration) HookDecisionRequest {
	request := HookDecisionRequest{
		EventID:         envelope.EventID,
		HookEventName:   envelope.HookEventName,
		SourceContext:   envelope.SourceContext,
		RunContext:      envelope.RunContext,
		SafeSummary:     envelope.SafePayload.SafeSummary,
		RiskClass:       envelope.SafePayload.RiskClass,
		SanitizedReason: envelope.SafePayload.SanitizedReason,
		PermissionClass: envelope.SafePayload.PermissionClass,
		PayloadDigest:   envelope.PayloadDigest,
		CorrelationID:   envelope.CorrelationID,
		TimeoutBudget:   timeout,
	}
	if envelope.ToolContext != nil {
		toolContext := *envelope.ToolContext
		request.ToolContext = &toolContext
	}
	if envelope.CapabilityContext != nil {
		capabilityContext := cloneCapabilityContext(*envelope.CapabilityContext)
		request.CapabilityContext = &capabilityContext
	}
	return request
}

func decisionSafeParts(event hookenum.HookEventName, owner hookenum.DownstreamOwner) []string {
	plan := canonicalRoutePlan(event)
	if route, ok := canonicalRouteForOwner(plan, owner); ok {
		return cloneStrings(route.SafeParts)
	}
	return nil
}

func defaultBridgeResult(envelope value.HookEnvelope) hookenum.HandlerResult {
	if envelope.HookEventName == hookenum.HookEventPreToolUse {
		return hookenum.HandlerResultNoDecision
	}
	return hookenum.HandlerResultNoDecision
}

func normalizeOwnerDecision(result hookenum.HandlerResult) hookenum.HandlerResult {
	switch result {
	case hookenum.HandlerResultAllow,
		hookenum.HandlerResultDeny,
		hookenum.HandlerResultNoDecision,
		hookenum.HandlerResultTimeout,
		hookenum.HandlerResultRetryableError,
		hookenum.HandlerResultFailClosed:
		return result
	default:
		return hookenum.HandlerResultNoDecision
	}
}

func decisionDiagnosticCode(result hookenum.HandlerResult) string {
	switch normalizeOwnerDecision(result) {
	case hookenum.HandlerResultAllow:
		return value.RouteDiagnosticDecisionAllowed
	case hookenum.HandlerResultDeny:
		return value.RouteDiagnosticDecisionDenied
	case hookenum.HandlerResultTimeout:
		return value.RouteDiagnosticDecisionTimeout
	case hookenum.HandlerResultRetryableError:
		return value.RouteDiagnosticRetryableError
	case hookenum.HandlerResultFailClosed:
		return value.RouteDiagnosticFailClosed
	default:
		return value.RouteDiagnosticNoDecision
	}
}

func applyDecisionFailurePolicy(result value.HookHandlerResult, cfg Config, event hookenum.HookEventName, timeoutHit bool) value.HookHandlerResult {
	policy := cfg.PreToolUseDecisionFailurePolicy
	if event == hookenum.HookEventPermissionRequest {
		policy = cfg.PermissionDecisionFailurePolicy
	}
	switch policy {
	case hookenum.DecisionFailurePolicyFailClosed:
		result.Result = hookenum.HandlerResultFailClosed
		result.DecisionReason = value.RouteDiagnosticFailClosed
	case hookenum.DecisionFailurePolicyRetryableError:
		result.Result = hookenum.HandlerResultRetryableError
		result.DecisionReason = value.RouteDiagnosticRetryableError
	case hookenum.DecisionFailurePolicyTimeout:
		result.Result = hookenum.HandlerResultTimeout
		result.DecisionReason = value.RouteDiagnosticDecisionTimeout
	default:
		result.Result = hookenum.HandlerResultNoDecision
		if timeoutHit {
			result.DecisionReason = value.RouteDiagnosticDecisionTimeout
		} else {
			result.DecisionReason = value.RouteDiagnosticNoDecision
		}
	}
	return result
}

func boundedDecisionReason(reason string) string {
	if len([]byte(reason)) <= 4096 {
		return reason
	}
	limit := 0
	for idx := range reason {
		if idx > 4096 {
			break
		}
		limit = idx
	}
	return reason[:limit]
}

func cloneOwners(owners []hookenum.DownstreamOwner) []hookenum.DownstreamOwner {
	if len(owners) == 0 {
		return nil
	}
	return append([]hookenum.DownstreamOwner(nil), owners...)
}

func cloneDecisionOwnerPorts(ports map[hookenum.DownstreamOwner]DecisionOwnerPort) map[hookenum.DownstreamOwner]DecisionOwnerPort {
	cloned := map[hookenum.DownstreamOwner]DecisionOwnerPort{}
	for owner, port := range ports {
		if port == nil {
			continue
		}
		cloned[owner] = port
	}
	return cloned
}

// UnavailableDecisionOwnerPort is an explicit process-level placeholder for owners without a selected transport.
type UnavailableDecisionOwnerPort struct {
	Owner hookenum.DownstreamOwner
}

// RequestHookDecision reports a safe unavailable decision owner without accepting delivery.
func (port UnavailableDecisionOwnerPort) RequestHookDecision(_ context.Context, request HookDecisionRequest) (HookOwnerDecision, error) {
	owner := port.Owner
	if owner == "" {
		owner = request.Owner
	}
	return HookOwnerDecision{
		Owner:     owner,
		Result:    hookenum.HandlerResultNoDecision,
		Retryable: true,
	}, hookerrs.ErrOwnerUnavailable
}
