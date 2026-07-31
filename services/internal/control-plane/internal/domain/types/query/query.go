// Package query задаёт доменные фильтры и pagination.
package query

import (
	"errors"
	"slices"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const MaximumPageSize = 100

// ResourceFilter ограничивает tenant-scoped stable list.
type ResourceFilter struct {
	OrganizationID string
	ProjectID      string
	ParentID       string
	Kind           enum.Kind
	States         []enum.State
	AfterID        string
	Limit          int
}

// Validate проверяет bounded filtering.
func (filter ResourceFilter) Validate() error {
	if !filter.Kind.Valid() || filter.Limit < 1 || filter.Limit > MaximumPageSize {
		return errors.New("resource filter is invalid")
	}
	if len(filter.States) > 8 {
		return errors.New("resource state filter is invalid")
	}
	if filter.ParentID != "" && value.ValidateID(filter.ParentID) != nil {
		return errors.New("resource parent filter is invalid")
	}
	if filter.AfterID != "" && value.ValidateID(filter.AfterID) != nil {
		return errors.New("resource cursor is invalid")
	}
	copyStates := slices.Clone(filter.States)
	slices.Sort(copyStates)
	for index, state := range copyStates {
		if !stateValid(state) || (index > 0 && state == copyStates[index-1]) {
			return errors.New("resource state filter is invalid")
		}
	}
	return nil
}

func stateValid(state enum.State) bool {
	switch state {
	case enum.StateActive, enum.StatePaused, enum.StateArchived,
		enum.StateDeletionPending, enum.StateDeleted, enum.StateQueued,
		enum.StateClaimed, enum.StateRunning, enum.StateWaitingOwner,
		enum.StateWaitingExternal, enum.StateSucceeded, enum.StateFailed,
		enum.StateCancelled, enum.StateExpired, enum.StateBlocked:
		return true
	default:
		return false
	}
}
