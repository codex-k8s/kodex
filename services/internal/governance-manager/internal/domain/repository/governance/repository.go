// Package governance contains governance-manager repository contracts.
package governance

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
)

// Repository is the storage boundary for governance state.
type Repository interface {
	Ready() bool
	RecordBacklogOperation(context.Context, BacklogOperation) error
}

// BacklogOperation records that a contract operation reached the GOV-2 skeleton.
type BacklogOperation struct {
	Operation enum.Operation
}
