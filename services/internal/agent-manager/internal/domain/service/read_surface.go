package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func (s *Service) ListAgentSessionSummaries(ctx context.Context, filter query.AgentSessionFilter) ([]entity.AgentSessionListItem, value.PageResult, error) {
	return listAgentReadSurface(ctx, s, filter, validateAgentSessionSummaryFilter, s.repository.ListAgentSessionSummaries)
}

func (s *Service) ListAgentRunSummaries(ctx context.Context, filter query.AgentRunSummaryFilter) ([]entity.AgentRunListItem, value.PageResult, error) {
	return listAgentReadSurface(ctx, s, filter, validateAgentRunSummaryFilter, s.repository.ListAgentRunSummaries)
}

func listAgentReadSurface[T any, F any](
	ctx context.Context,
	service *Service,
	filter F,
	validate func(F) error,
	lister func(context.Context, F) ([]T, value.PageResult, error),
) ([]T, value.PageResult, error) {
	if err := validate(filter); err != nil {
		return nil, value.PageResult{}, err
	}
	return listFromRepository(ctx, service, filter, lister)
}

func validateAgentSessionSummaryFilter(filter query.AgentSessionFilter) error {
	if filter.CreatedAfter != nil && filter.CreatedBefore != nil && !filter.CreatedAfter.Before(*filter.CreatedBefore) {
		return errs.ErrInvalidArgument
	}
	if filter.Scope.Type != "" || filter.Scope.Ref != "" {
		if err := validateScope(filter.Scope); err != nil {
			return err
		}
	}
	if strings.TrimSpace(filter.Scope.Ref) != "" {
		return nil
	}
	if strings.TrimSpace(filter.ProviderWorkItemRef) == "" && strings.TrimSpace(filter.CreatedByActorRef) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateAgentRunSummaryFilter(filter query.AgentRunSummaryFilter) error {
	if filter.CreatedAfter != nil && filter.CreatedBefore != nil && !filter.CreatedAfter.Before(*filter.CreatedBefore) {
		return errs.ErrInvalidArgument
	}
	if filter.Scope.Type != "" || filter.Scope.Ref != "" {
		if err := validateScope(filter.Scope); err != nil {
			return err
		}
	}
	if filter.SessionID != uuid.Nil {
		return nil
	}
	if strings.TrimSpace(filter.Scope.Ref) != "" ||
		strings.TrimSpace(filter.ProviderWorkItemRef) != "" ||
		strings.TrimSpace(filter.ProviderPullRequestRef) != "" {
		return nil
	}
	return errs.ErrInvalidArgument
}
