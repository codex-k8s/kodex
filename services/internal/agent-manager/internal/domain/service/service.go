package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentrepo "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/repository/agent"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

// Config contains dependencies required by the agent-manager service.
type Config struct {
	Repository       agentrepo.Repository
	Clock            agentrepo.Clock
	IDGenerator      agentrepo.IDGenerator
	GuidanceResolver GuidanceResolver
	// EventPublisher is a future outbox-backed publisher for agent domain events.
	EventPublisher EventPublisher
}

// Service is the agent-manager domain entry point.
type Service struct {
	repository       agentrepo.Repository
	clock            agentrepo.Clock
	idGenerator      agentrepo.IDGenerator
	guidanceResolver GuidanceResolver
	eventPublisher   EventPublisher
}

// GuidanceResolver resolves guidance package selections into safe frozen refs.
type GuidanceResolver interface {
	ResolveGuidanceRefs(context.Context, GuidanceResolutionInput) ([]value.GuidanceRef, error)
}

// GuidanceResolutionInput describes package guidance context needed for one run.
type GuidanceResolutionInput struct {
	Meta  value.CommandMeta
	Scope value.ScopeRef
	Hints []value.GuidanceSelectionHint
}

// DisabledGuidanceResolver keeps agent-manager runnable before package-hub is wired.
type DisabledGuidanceResolver struct{}

// ResolveGuidanceRefs rejects explicit hints when package-hub resolution is disabled.
func (DisabledGuidanceResolver) ResolveGuidanceRefs(_ context.Context, input GuidanceResolutionInput) ([]value.GuidanceRef, error) {
	if len(input.Hints) > 0 {
		return nil, errs.ErrPreconditionFailed
	}
	return nil, nil
}

// New creates an agent-manager service scaffold.
func New(cfg Config) *Service {
	if cfg.EventPublisher == nil {
		cfg.EventPublisher = DisabledEventPublisher{}
	}
	if cfg.GuidanceResolver == nil {
		cfg.GuidanceResolver = DisabledGuidanceResolver{}
	}
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	if cfg.IDGenerator == nil {
		cfg.IDGenerator = zeroIDGenerator{}
	}
	return &Service{
		repository:       cfg.Repository,
		clock:            cfg.Clock,
		idGenerator:      cfg.IDGenerator,
		guidanceResolver: cfg.GuidanceResolver,
		eventPublisher:   cfg.EventPublisher,
	}
}

// Ready reports whether the process has the minimal composed dependencies.
func (s *Service) Ready() bool {
	return s != nil && s.eventPublisher != nil
}

// EventPublisher returns the configured event publisher boundary.
func (s *Service) EventPublisher() EventPublisher {
	if s == nil {
		return DisabledEventPublisher{}
	}
	return s.eventPublisher
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type zeroIDGenerator struct{}

func (zeroIDGenerator) New() uuid.UUID {
	return uuid.New()
}
