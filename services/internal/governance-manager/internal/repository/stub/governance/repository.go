// Package governance contains a storage stub for the GOV-2 service skeleton.
package governance

import (
	"context"
	"fmt"

	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
)

var _ governancerepo.Repository = (*Repository)(nil)

// Repository is a non-persistent repository stub used until GOV-3 storage lands.
type Repository struct{}

// NewRepository creates a storage stub.
func NewRepository() *Repository {
	return &Repository{}
}

// Ready reports that the stub repository is available.
func (repository *Repository) Ready() bool {
	return repository != nil
}

// RecordBacklogOperation accepts a safe operation marker without pretending to persist governance state.
func (repository *Repository) RecordBacklogOperation(ctx context.Context, operation governancerepo.BacklogOperation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if operation.Operation == enum.Operation("") {
		return fmt.Errorf("governance backlog operation is required")
	}
	return nil
}
