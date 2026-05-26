package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	opsrepo "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/repository/ops"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// Config contains schema-driven limits for the domain skeleton.
type Config struct {
	SchemaVersion                   string
	MaxEnvelopeBytes                int
	MaxTextPreviewBytes             int
	MaxBoundedErrorBytes            int
	SupportedEvents                 []hookenum.HookEventName
	DisabledRoutes                  []hookenum.DownstreamOwner
	RouteFailurePolicy              hookenum.RouteFailurePolicy
	OpsFeedRetention                time.Duration
	RateLimitWindow                 time.Duration
	RateLimitBurst                  int
	DecisionBridgeTimeout           time.Duration
	PreToolUseDecisionRiskClasses   []string
	PermissionDecisionFailurePolicy hookenum.DecisionFailurePolicy
	PreToolUseDecisionFailurePolicy hookenum.DecisionFailurePolicy
}

// Dependencies contains replaceable domain ports.
type Dependencies struct {
	Clock          Clock
	Validator      EnvelopeValidator
	SourceVerifier SourceVerifier
	Sanitizer      Sanitizer
	RouteRegistry  *RouteRegistry
	OpsFeed        opsrepo.Repository
	RateLimiter    RateLimiter
	DecisionBridge DecisionBridge
}

// Clock provides deterministic time for idempotency records and tests.
type Clock interface {
	Now() time.Time
}

// EnvelopeValidator validates normalized envelopes against schema-level invariants.
type EnvelopeValidator interface {
	ValidateEnvelope(ctx context.Context, cfg Config, envelope value.HookEnvelope) error
}

// SourceVerifier verifies source/run/session/slot binding at the ingress boundary.
type SourceVerifier interface {
	VerifySourceBinding(ctx context.Context, check SourceBindingCheck) (SourceBindingDecision, error)
}

// Sanitizer verifies the sanitizer boundary after emitter or sidecar normalization.
type Sanitizer interface {
	VerifyBoundary(ctx context.Context, cfg Config, envelope value.HookEnvelope) (SanitizerDecision, error)
}

// RateLimiter applies logical SubmitHookEvent admission limits before dispatch.
type RateLimiter interface {
	Ready() bool
	Allow(ctx context.Context, check RateLimitCheck) (RateLimitDecision, error)
}

// DecisionBridge coordinates policy-controlled hook decisions through owner ports.
type DecisionBridge interface {
	Ready() bool
	Evaluate(ctx context.Context, cfg Config, envelope value.HookEnvelope) (DecisionBridgeResult, bool, error)
}

// DecisionOwnerPort sends a safe hook decision request to one owner boundary.
type DecisionOwnerPort interface {
	RequestHookDecision(ctx context.Context, request HookDecisionRequest) (HookOwnerDecision, error)
}

// SourceBindingCheck contains safe context needed by source binding verification.
type SourceBindingCheck struct {
	SourceContext value.SourceContext
	RunContext    value.RunContext
}

// SourceBindingDecision describes the verified source placeholder result.
type SourceBindingDecision struct {
	BindingRef string
	Accepted   bool
}

// SanitizerDecision describes safe sanitizer boundary metadata.
type SanitizerDecision struct {
	Accepted      bool
	EnvelopeBytes int
}

// RateLimitCheck carries safe dimensions for admission control.
type RateLimitCheck struct {
	SourceRef      string
	RunID          string
	HookEventName  hookenum.HookEventName
	RetentionClass hookenum.RetentionClass
	At             time.Time
}

// RateLimitDecision describes a logical admission result.
type RateLimitDecision struct {
	Allowed    bool
	ReasonCode string
	RetryAfter time.Duration
}

// HookDecisionRequest contains only safe context for a policy owner decision.
type HookDecisionRequest struct {
	EventID           uuid.UUID
	HookEventName     hookenum.HookEventName
	Owner             hookenum.DownstreamOwner
	SourceContext     value.SourceContext
	RunContext        value.RunContext
	ToolContext       *value.ToolContext
	CapabilityContext *value.CapabilityContext
	SafeSummary       string
	RiskClass         string
	SanitizedReason   string
	PermissionClass   string
	PayloadDigest     string
	CorrelationID     string
	TimeoutBudget     time.Duration
}

// HookOwnerDecision is a safe owner response for a hook decision request.
type HookOwnerDecision struct {
	Owner            hookenum.DownstreamOwner
	Result           hookenum.HandlerResult
	OwnerDecisionRef string
	DecisionReason   string
	Retryable        bool
}

// DecisionBridgeResult carries the handler result and owner diagnostics.
type DecisionBridgeResult struct {
	HandlerResult    value.HookHandlerResult
	RouteDiagnostics []value.RouteDeliveryResult
	HandledOwners    []hookenum.DownstreamOwner
}

// SubmitHookEventInput is the in-process logical SubmitHookEvent request.
type SubmitHookEventInput struct {
	Envelope value.HookEnvelope
}

// SubmitHookEventResult is the in-process logical SubmitHookEvent response.
type SubmitHookEventResult struct {
	HandlerResult    value.HookHandlerResult
	Duplicate        bool
	RoutesAccepted   int
	RouteDiagnostics []value.RouteDeliveryResult
}
