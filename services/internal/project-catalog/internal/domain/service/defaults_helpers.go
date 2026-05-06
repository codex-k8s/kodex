package service

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

func newBase(id uuid.UUID, now time.Time) entity.Base {
	return entity.Base{ID: id, Version: 1, CreatedAt: now, UpdatedAt: now}
}

func updatedBase(current entity.Base, now time.Time) entity.Base {
	return entity.Base{ID: current.ID, Version: current.Version + 1, CreatedAt: current.CreatedAt, UpdatedAt: now}
}

func applyString(current string, next *string) string {
	if next == nil {
		return current
	}
	return strings.TrimSpace(*next)
}

func defaultProjectStatus(status enum.ProjectStatus) enum.ProjectStatus {
	if status == "" {
		return enum.ProjectStatusActive
	}
	return status
}

func defaultRepositoryStatus(status enum.RepositoryStatus) enum.RepositoryStatus {
	if status == "" {
		return enum.RepositoryStatusActive
	}
	return status
}

func defaultValidationStatus(status enum.ServicesPolicyValidationStatus) enum.ServicesPolicyValidationStatus {
	if status == "" {
		return enum.ServicesPolicyValidationValid
	}
	return status
}

func projectionStatusForValidation(status enum.ServicesPolicyValidationStatus) enum.ServicesPolicyProjectionStatus {
	if status == enum.ServicesPolicyValidationValid {
		return enum.ServicesPolicyProjectionSynced
	}
	return enum.ServicesPolicyProjectionFailed
}

func defaultDocumentationStatus(status enum.DocumentationSourceStatus) enum.DocumentationSourceStatus {
	if status == "" {
		return enum.DocumentationSourceStatusActive
	}
	return status
}

func defaultDocumentationAccessMode(mode enum.DocumentationAccessMode) enum.DocumentationAccessMode {
	if mode == "" {
		return enum.DocumentationAccessRead
	}
	return mode
}

func defaultBranchRulesStatus(status enum.BranchRulesStatus) enum.BranchRulesStatus {
	if status == "" {
		return enum.BranchRulesStatusActive
	}
	return status
}

func defaultMergePolicy(policy enum.MergePolicy) enum.MergePolicy {
	if policy == "" {
		return enum.MergePolicySquash
	}
	return policy
}

func defaultReleasePolicyStatus(status enum.ReleasePolicyStatus) enum.ReleasePolicyStatus {
	if status == "" {
		return enum.ReleasePolicyStatusActive
	}
	return status
}

func defaultRolloutStrategy(strategy enum.RolloutStrategy) enum.RolloutStrategy {
	if strategy == "" {
		return enum.RolloutStrategyDirect
	}
	return strategy
}

func defaultRollbackPolicy(policy enum.RollbackPolicy) enum.RollbackPolicy {
	if policy == "" {
		return enum.RollbackPolicyManual
	}
	return policy
}

func defaultPlacementPolicyStatus(status enum.PlacementPolicyStatus) enum.PlacementPolicyStatus {
	if status == "" {
		return enum.PlacementPolicyStatusActive
	}
	return status
}

func requireProjectID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func actorRef(actor value.Actor) string {
	actorType := strings.TrimSpace(actor.Type)
	actorID := strings.TrimSpace(actor.ID)
	if actorType == "" || actorID == "" {
		return ""
	}
	return actorType + ":" + actorID
}

func parseRFC3339(text string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
	if err != nil || parsed.IsZero() {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return parsed.UTC(), nil
}
