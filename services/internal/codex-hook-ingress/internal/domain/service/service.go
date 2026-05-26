// Package service implements codex-hook-ingress domain skeleton use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookrepo "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/repository/hook"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// Service coordinates normalized hook event acceptance and safe owner routing.
type Service struct {
	repository     hookrepo.Repository
	config         Config
	clock          Clock
	validator      EnvelopeValidator
	sourceVerifier SourceVerifier
	sanitizer      Sanitizer
	routeRegistry  *RouteRegistry
}

// New creates a codex-hook-ingress domain service with explicit skeleton ports.
func New(repository hookrepo.Repository, cfg Config, deps Dependencies) *Service {
	cfg = normalizedConfig(cfg)
	if deps.Clock == nil {
		deps.Clock = systemClock{}
	}
	if deps.Validator == nil {
		deps.Validator = DefaultEnvelopeValidator{}
	}
	if deps.SourceVerifier == nil {
		deps.SourceVerifier = StaticSourceVerifier{}
	}
	if deps.Sanitizer == nil {
		deps.Sanitizer = DefaultSanitizer{}
	}
	if deps.RouteRegistry == nil {
		deps.RouteRegistry = NewDefaultRouteRegistry()
	}
	return &Service{
		repository:     repository,
		config:         cfg,
		clock:          deps.Clock,
		validator:      deps.Validator,
		sourceVerifier: deps.SourceVerifier,
		sanitizer:      deps.Sanitizer,
		routeRegistry:  deps.RouteRegistry,
	}
}

// Ready reports whether the domain skeleton and repository are composed.
func (s *Service) Ready() bool {
	return s != nil &&
		s.repository != nil &&
		s.repository.Ready() &&
		s.validator != nil &&
		s.sourceVerifier != nil &&
		s.sanitizer != nil &&
		s.routeRegistry != nil &&
		s.routeRegistry.Ready()
}

// SubmitHookEvent accepts a normalized hook event through the logical command boundary.
func (s *Service) SubmitHookEvent(ctx context.Context, input SubmitHookEventInput) (SubmitHookEventResult, error) {
	if !s.Ready() {
		return SubmitHookEventResult{}, fmt.Errorf("%w: hook service is not ready", hookerrs.ErrDependencyUnavailable)
	}
	envelope := input.Envelope
	if err := s.validator.ValidateEnvelope(ctx, s.config, envelope); err != nil {
		return SubmitHookEventResult{}, err
	}
	if _, err := s.sanitizer.VerifyBoundary(ctx, s.config, envelope); err != nil {
		return SubmitHookEventResult{}, err
	}
	decision, err := s.sourceVerifier.VerifySourceBinding(ctx, SourceBindingCheck{
		SourceContext: envelope.SourceContext,
		RunContext:    envelope.RunContext,
	})
	if err != nil {
		return SubmitHookEventResult{}, err
	}
	if !decision.Accepted {
		return SubmitHookEventResult{}, hookerrs.ErrInvalidBinding
	}
	result := s.handlerResult(envelope)
	record := entity.AcceptedEvent{
		EventID:        envelope.EventID,
		PayloadDigest:  envelope.PayloadDigest,
		HookEventName:  envelope.HookEventName,
		CorrelationID:  envelope.CorrelationID,
		RetentionClass: envelope.RetentionClass,
		Result:         result,
		RecordedAt:     s.clock.Now(),
	}
	accepted, duplicate, err := s.repository.RegisterAcceptedEvent(ctx, record)
	if err != nil {
		if errors.Is(err, hookerrs.ErrDuplicateConflict) {
			return SubmitHookEventResult{}, err
		}
		return SubmitHookEventResult{}, fmt.Errorf("%w: store hook idempotency record: %v", hookerrs.ErrDependencyUnavailable, err)
	}
	if duplicate {
		routeDiagnostics := cloneRouteDeliveryResults(accepted.RouteDiagnostics)
		return SubmitHookEventResult{
			HandlerResult:    accepted.Result,
			Duplicate:        true,
			RoutesAccepted:   countDeliveredRoutes(routeDiagnostics),
			RouteDiagnostics: routeDiagnostics,
		}, nil
	}
	routeDiagnostics := s.routeRegistry.DispatchRoutes(ctx, s.config, envelope)
	result = s.applyRouteFailurePolicy(result, routeDiagnostics)
	accepted, err = s.repository.RecordDeliveryResults(ctx, entity.DeliveryUpdate{
		EventID:          envelope.EventID,
		PayloadDigest:    envelope.PayloadDigest,
		Result:           result,
		RouteDiagnostics: routeDiagnostics,
	})
	if err != nil {
		return SubmitHookEventResult{}, fmt.Errorf("%w: store hook route diagnostics: %v", hookerrs.ErrDependencyUnavailable, err)
	}
	routeDiagnostics = cloneRouteDeliveryResults(accepted.RouteDiagnostics)
	return SubmitHookEventResult{
		HandlerResult:    accepted.Result,
		RoutesAccepted:   countDeliveredRoutes(routeDiagnostics),
		RouteDiagnostics: routeDiagnostics,
	}, nil
}

func (s *Service) handlerResult(envelope value.HookEnvelope) value.HookHandlerResult {
	result := hookenum.HandlerResultContinue
	if envelope.HookEventName == hookenum.HookEventPermissionRequest {
		result = hookenum.HandlerResultNoDecision
	}
	return value.HookHandlerResult{
		Result:        result,
		HookEventName: envelope.HookEventName,
		CorrelationID: envelope.CorrelationID,
	}
}

func (s *Service) applyRouteFailurePolicy(result value.HookHandlerResult, diagnostics []value.RouteDeliveryResult) value.HookHandlerResult {
	if s.config.RouteFailurePolicy != hookenum.RouteFailurePolicyFailClosed || !hasUndeliveredRoute(diagnostics) {
		return result
	}
	result.Result = hookenum.HandlerResultFailClosed
	result.SystemMessage = "hook route delivery failed safely"
	result.DecisionReason = value.RouteDiagnosticFailurePolicyFired
	return result
}

func normalizedConfig(cfg Config) Config {
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = "codex-hook-ingress.normalized-hook-envelope.v1"
	}
	if cfg.MaxEnvelopeBytes <= 0 {
		cfg.MaxEnvelopeBytes = 65536
	}
	if cfg.MaxTextPreviewBytes <= 0 {
		cfg.MaxTextPreviewBytes = 4096
	}
	if cfg.MaxBoundedErrorBytes <= 0 {
		cfg.MaxBoundedErrorBytes = 8192
	}
	if len(cfg.SupportedEvents) == 0 {
		cfg.SupportedEvents = hookenum.SupportedHookEvents()
	}
	if cfg.RouteFailurePolicy == "" {
		cfg.RouteFailurePolicy = hookenum.RouteFailurePolicyDiagnostic
	}
	return cfg
}

func (cfg Config) routeEnabled(owner hookenum.DownstreamOwner) bool {
	for _, disabled := range cfg.DisabledRoutes {
		if disabled == owner {
			return false
		}
	}
	return true
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
