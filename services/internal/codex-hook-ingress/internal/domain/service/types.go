package service

import (
	"context"
	"time"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// Config contains schema-driven limits for the domain skeleton.
type Config struct {
	SchemaVersion        string
	MaxEnvelopeBytes     int
	MaxTextPreviewBytes  int
	MaxBoundedErrorBytes int
	SupportedEvents      []hookenum.HookEventName
	DisabledRoutes       []hookenum.DownstreamOwner
	RouteFailurePolicy   hookenum.RouteFailurePolicy
}

// Dependencies contains replaceable domain ports.
type Dependencies struct {
	Clock          Clock
	Validator      EnvelopeValidator
	SourceVerifier SourceVerifier
	Sanitizer      Sanitizer
	RouteRegistry  *RouteRegistry
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
