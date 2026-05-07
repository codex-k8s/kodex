package service

import (
	"context"

	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
)

// Service is the domain entrypoint for provider-native work item workflows.
type Service struct {
	repository providerrepo.Repository
}

// New creates a provider-hub domain service.
func New(repository providerrepo.Repository) *Service {
	if repository == nil {
		panic("provider-hub repository is required")
	}
	return &Service{repository: repository}
}

// Ping checks whether the service can reach its owned storage.
func (s *Service) Ping(ctx context.Context) error {
	return s.repository.Ping(ctx)
}
