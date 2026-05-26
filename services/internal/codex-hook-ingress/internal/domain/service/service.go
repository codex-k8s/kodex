// Package service implements codex-hook-ingress domain skeleton use cases.
package service

import (
	"context"
	"fmt"
	"time"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	hookrepo "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/repository/hook"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// Service coordinates normalized hook event acceptance without downstream routing.
type Service struct {
	repository     hookrepo.Repository
	config         Config
	clock          Clock
	validator      EnvelopeValidator
	sourceVerifier SourceVerifier
	sanitizer      Sanitizer
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
	return &Service{
		repository:     repository,
		config:         cfg,
		clock:          deps.Clock,
		validator:      deps.Validator,
		sourceVerifier: deps.SourceVerifier,
		sanitizer:      deps.Sanitizer,
	}
}

// Ready reports whether the domain skeleton and repository are composed.
func (s *Service) Ready() bool {
	return s != nil && s.repository != nil && s.repository.Ready() && s.validator != nil && s.sourceVerifier != nil && s.sanitizer != nil
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
	existing, found, err := s.repository.GetAcceptedEvent(ctx, envelope.EventID)
	if err != nil {
		return SubmitHookEventResult{}, fmt.Errorf("%w: read hook idempotency record: %v", hookerrs.ErrDependencyUnavailable, err)
	}
	if found {
		if existing.PayloadDigest != envelope.PayloadDigest {
			return SubmitHookEventResult{}, hookerrs.ErrDuplicateConflict
		}
		return SubmitHookEventResult{
			HandlerResult:  existing.Result,
			Duplicate:      true,
			RoutesAccepted: len(envelope.DownstreamRoutes),
		}, nil
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
	if err := s.repository.RecordAcceptedEvent(ctx, record); err != nil {
		return SubmitHookEventResult{}, fmt.Errorf("%w: store hook idempotency record: %v", hookerrs.ErrDependencyUnavailable, err)
	}
	return SubmitHookEventResult{
		HandlerResult:  result,
		RoutesAccepted: len(envelope.DownstreamRoutes),
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
	return cfg
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
