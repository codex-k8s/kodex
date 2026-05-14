package service

// Config contains dependencies required by the agent-manager service.
type Config struct {
	// EventPublisher is a future outbox-backed publisher for agent domain events.
	EventPublisher EventPublisher
}

// Service is the agent-manager domain entry point.
type Service struct {
	eventPublisher EventPublisher
}

// New creates an agent-manager service scaffold.
func New(cfg Config) *Service {
	if cfg.EventPublisher == nil {
		cfg.EventPublisher = DisabledEventPublisher{}
	}
	return &Service{eventPublisher: cfg.EventPublisher}
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
