// Package service contains package-hub use cases.
package service

import catalogrepo "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/repository/catalog"

// Service is the package-hub application service boundary.
type Service struct {
	repository catalogrepo.Repository
	clock      catalogrepo.Clock
	ids        catalogrepo.IDGenerator
}

// New creates a package-hub service with explicit dependencies.
func New(repository catalogrepo.Repository, clock catalogrepo.Clock, ids catalogrepo.IDGenerator) *Service {
	return &Service{repository: repository, clock: clock, ids: ids}
}
