package interaction

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
)

// Repository is the domain persistence port for interaction-hub.
type Repository interface {
	Ready() bool
	RecordBacklogOperation(context.Context, enum.Operation) error
}
