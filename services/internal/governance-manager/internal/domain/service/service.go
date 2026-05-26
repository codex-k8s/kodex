// Package service contains governance-manager use-case skeletons.
package service

import (
	"context"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
)

// Service is the governance-manager application service boundary.
type Service struct {
	repository governancerepo.Repository
}

// New creates a governance-manager service with explicit dependencies.
func New(repository governancerepo.Repository) *Service {
	return &Service{repository: repository}
}

// Ready reports whether the minimal service dependencies are composed.
func (s *Service) Ready() bool {
	return s != nil && s.repository != nil && s.repository.Ready()
}

// BacklogOperation records that a stable contract operation reached the skeleton boundary.
func (s *Service) BacklogOperation(ctx context.Context, input BacklogOperationInput) error {
	if input.Operation == enum.Operation("") {
		return errs.ErrInvalidArgument
	}
	if s == nil || s.repository == nil {
		return fmt.Errorf("%w: governance repository is not configured", errs.ErrDependencyUnavailable)
	}
	if err := s.repository.RecordBacklogOperation(ctx, governancerepo.BacklogOperation{Operation: input.Operation}); err != nil {
		return fmt.Errorf("%w: %v", errs.ErrDependencyUnavailable, err)
	}
	return fmt.Errorf("%w: %s remains backlog in GOV-2 skeleton", errs.ErrNotImplemented, input.Operation)
}
