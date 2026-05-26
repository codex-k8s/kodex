package interaction

import (
	"context"

	interactionrepo "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/repository/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
)

var _ interactionrepo.Repository = (*Repository)(nil)

// Repository is an IH-2 persistence stub that records no domain state.
type Repository struct{}

// NewRepository creates the scaffold repository used until IH-3 adds PostgreSQL.
func NewRepository() *Repository {
	return &Repository{}
}

// Ready reports that the scaffold repository is composed.
func (r *Repository) Ready() bool {
	return r != nil
}

// RecordBacklogOperation accepts a stable operation without persisting state.
func (r *Repository) RecordBacklogOperation(context.Context, enum.Operation) error {
	return nil
}
