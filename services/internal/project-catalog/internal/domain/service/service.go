// Package service implements project-catalog domain use cases.
package service

import (
	projectrepo "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/repository/project"
)

// Service coordinates project-catalog domain commands and reads.
type Service struct {
	repository projectrepo.Repository
	clock      projectrepo.Clock
	ids        projectrepo.IDGenerator
}

// New creates a domain service with injected persistence, clock and id generator.
func New(repository projectrepo.Repository, clock projectrepo.Clock, ids projectrepo.IDGenerator) *Service {
	return &Service{repository: repository, clock: clock, ids: ids}
}
