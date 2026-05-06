// Package service implements project-catalog domain use cases.
package service

import (
	projectrepo "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/repository/project"
)

// Config contains optional domain service integrations.
type Config struct {
	Authorizer Authorizer
}

// Service coordinates project-catalog domain commands and reads.
type Service struct {
	repository projectrepo.Repository
	clock      projectrepo.Clock
	ids        projectrepo.IDGenerator
	authorizer Authorizer
}

// New creates a domain service with injected persistence, clock and id generator.
func New(repository projectrepo.Repository, clock projectrepo.Clock, ids projectrepo.IDGenerator) *Service {
	return NewWithConfig(repository, clock, ids, Config{})
}

// NewWithConfig creates a domain service with optional integrations.
func NewWithConfig(repository projectrepo.Repository, clock projectrepo.Clock, ids projectrepo.IDGenerator, cfg Config) *Service {
	authorizer := cfg.Authorizer
	if authorizer == nil {
		authorizer = AllowAllAuthorizer{}
	}
	return &Service{repository: repository, clock: clock, ids: ids, authorizer: authorizer}
}
