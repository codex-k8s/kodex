// Package service contains package-hub use cases.
package service

import catalogrepo "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/repository/catalog"

// Config contains optional package-hub service integrations.
type Config struct {
	Authorizer Authorizer
}

// Service is the package-hub application service boundary.
type Service struct {
	repository catalogrepo.Repository
	clock      catalogrepo.Clock
	ids        catalogrepo.IDGenerator
	authorizer Authorizer
}

// New creates a package-hub service with explicit dependencies.
func New(repository catalogrepo.Repository, clock catalogrepo.Clock, ids catalogrepo.IDGenerator) *Service {
	service := &Service{repository: repository, clock: clock, ids: ids}
	service.setAuthorizer(nil)
	return service
}

// NewWithConfig creates a package-hub service with optional integrations.
func NewWithConfig(repository catalogrepo.Repository, clock catalogrepo.Clock, ids catalogrepo.IDGenerator, cfg Config) *Service {
	service := &Service{repository: repository, clock: clock, ids: ids}
	service.setAuthorizer(cfg.Authorizer)
	return service
}

func (s *Service) setAuthorizer(authorizer Authorizer) {
	if authorizer == nil {
		s.authorizer = AllowAllAuthorizer{}
		return
	}
	s.authorizer = authorizer
}
