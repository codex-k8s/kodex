package service

import (
	"time"

	"github.com/google/uuid"

	agentrepo "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/repository/agent"
)

// Config contains dependencies required by the agent-manager service.
type Config struct {
	Repository  agentrepo.Repository
	Clock       agentrepo.Clock
	IDGenerator agentrepo.IDGenerator
	// EventPublisher is a future outbox-backed publisher for agent domain events.
	EventPublisher EventPublisher
}

// Service is the agent-manager domain entry point.
type Service struct {
	repository     agentrepo.Repository
	clock          agentrepo.Clock
	idGenerator    agentrepo.IDGenerator
	eventPublisher EventPublisher
}

// New creates an agent-manager service scaffold.
func New(cfg Config) *Service {
	if cfg.EventPublisher == nil {
		cfg.EventPublisher = DisabledEventPublisher{}
	}
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	if cfg.IDGenerator == nil {
		cfg.IDGenerator = zeroIDGenerator{}
	}
	return &Service{
		repository:     cfg.Repository,
		clock:          cfg.Clock,
		idGenerator:    cfg.IDGenerator,
		eventPublisher: cfg.EventPublisher,
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
