// Package query задаёт доменные фильтры и pagination.
package query

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const MaximumPageSize = 100

var auditActionPattern = regexp.MustCompile(`^[a-z][a-z0-9_:]{0,95}$`)

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

// ResourceSearch ограничивает tenant-scoped normalized search.
type ResourceSearch struct {
	OrganizationID string
	ProjectID      string
	Kind           enum.Kind
	States         []enum.State
	Query          string
	AfterID        string
	Limit          int
}

func (filter ResourceSearch) Validate() error {
	if filter.Query != strings.TrimSpace(filter.Query) ||
		len(filter.Query) < 2 || len(filter.Query) > 128 {
		return errors.New("resource search query is invalid")
	}
	return ResourceFilter{
		Kind:    filter.Kind,
		States:  filter.States,
		AfterID: filter.AfterID,
		Limit:   filter.Limit,
	}.Validate()
}

// AuditFilter задаёт безопасный cursor по immutable audit records.
type AuditFilter struct {
	OrganizationID string
	ProjectID      string
	ResourceKind   enum.Kind
	ResourceID     string
	Action         string
	AfterID        string
	Limit          int
}

func (filter AuditFilter) Validate() error {
	if filter.Limit < 1 || filter.Limit > MaximumPageSize ||
		(filter.ResourceKind != "" && !filter.ResourceKind.Valid()) ||
		(filter.ResourceID != "" && value.ValidateID(filter.ResourceID) != nil) ||
		(filter.AfterID != "" && value.ValidateID(filter.AfterID) != nil) ||
		(filter.Action != "" && !auditActionPattern.MatchString(filter.Action)) {
		return errors.New("audit filter is invalid")
	}
	return nil
}

type TombstoneFilter struct {
	OrganizationID string
	ProjectID      string
	Kind           enum.Kind
	AfterID        string
	Limit          int
}

func (filter TombstoneFilter) Validate() error {
	if !filter.Kind.Valid() || filter.Limit < 1 || filter.Limit > MaximumPageSize ||
		(filter.AfterID != "" && value.ValidateID(filter.AfterID) != nil) {
		return errors.New("tombstone filter is invalid")
	}
	return nil
}

type ScheduleOccurrenceFilter struct {
	OrganizationID string
	ProjectID      string
	ScheduleID     string
	States         []string
	AfterID        string
	Limit          int
}

func (filter ScheduleOccurrenceFilter) Validate() error {
	if value.ValidateID(filter.ScheduleID) != nil ||
		filter.Limit < 1 || filter.Limit > MaximumPageSize ||
		(filter.AfterID != "" && value.ValidateID(filter.AfterID) != nil) ||
		len(filter.States) > 7 {
		return errors.New("schedule occurrence filter is invalid")
	}
	seen := make(map[string]struct{}, len(filter.States))
	for _, state := range filter.States {
		switch state {
		case "QUEUED", "CLAIMED", "SUCCEEDED", "FAILED",
			"CANCELLED", "SKIPPED", "DEAD_LETTER":
		default:
			return errors.New("schedule occurrence state is invalid")
		}
		if _, duplicate := seen[state]; duplicate {
			return errors.New("schedule occurrence state is duplicated")
		}
		seen[state] = struct{}{}
	}
	return nil
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
