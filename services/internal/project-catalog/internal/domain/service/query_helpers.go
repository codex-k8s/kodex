package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

func readProjectScopedAggregate[Aggregate any](
	s *Service,
	ctx context.Context,
	id uuid.UUID,
	meta value.QueryMeta,
	actionKey string,
	aggregateType string,
	load func(context.Context, uuid.UUID) (Aggregate, error),
	projectID func(Aggregate) uuid.UUID,
) (Aggregate, error) {
	var zero Aggregate
	if err := requireProjectID(id); err != nil {
		return zero, err
	}
	aggregate, err := load(ctx, id)
	if err != nil {
		return zero, err
	}
	if err := s.authorizeQuery(ctx, meta, actionKey, projectScopedResource(aggregateType, projectID(aggregate))); err != nil {
		return zero, err
	}
	return aggregate, nil
}

func (s *Service) authorizeProjectQuery(ctx context.Context, projectID uuid.UUID, meta value.QueryMeta, actionKey string, aggregateType string) error {
	if err := requireProjectID(projectID); err != nil {
		return err
	}
	return s.authorizeQuery(ctx, meta, actionKey, projectScopedResource(aggregateType, projectID))
}

func listProjectScoped[Item any, Result any](
	s *Service,
	ctx context.Context,
	projectID uuid.UUID,
	meta value.QueryMeta,
	actionKey string,
	aggregateType string,
	load func(context.Context) ([]Item, value.PageResult, error),
	build func([]Item, value.PageResult) Result,
) (Result, error) {
	var zero Result
	if err := s.authorizeProjectQuery(ctx, projectID, meta, actionKey, aggregateType); err != nil {
		return zero, err
	}
	items, page, err := load(ctx)
	if err != nil {
		return zero, err
	}
	return build(items, page), nil
}

func projectID(project entity.Project) uuid.UUID {
	return project.ID
}

func projectOrganizationID(project entity.Project) uuid.UUID {
	return project.OrganizationID
}

func repositoryProjectID(repository entity.RepositoryBinding) uuid.UUID {
	return repository.ProjectID
}

func documentationSourceProjectID(source entity.DocumentationSource) uuid.UUID {
	return source.ProjectID
}

func branchRulesProjectID(rules entity.BranchRules) uuid.UUID {
	return rules.ProjectID
}

func releasePolicyProjectID(policy entity.ReleasePolicy) uuid.UUID {
	return policy.ProjectID
}

func releaseLineProjectID(line entity.ReleaseLine) uuid.UUID {
	return line.ProjectID
}

func placementPolicyProjectID(policy entity.PlacementPolicy) uuid.UUID {
	return policy.ProjectID
}
