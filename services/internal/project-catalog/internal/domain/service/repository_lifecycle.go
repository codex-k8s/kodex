package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// AttachRepository binds a provider repository to a project.
func (s *Service) AttachRepository(ctx context.Context, input AttachRepositoryInput) (entity.RepositoryBinding, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.RepositoryBinding{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionRepositoryAttach, projectScopedResource(projectAggregateRepository, input.ProjectID)); err != nil {
		return entity.RepositoryBinding{}, err
	}
	if result, ok, err := s.findCommandResult(ctx, input.Meta, projectOperationAttachRepository, projectAggregateRepository); ok || err != nil {
		if err != nil {
			return entity.RepositoryBinding{}, err
		}
		return s.repository.GetRepository(ctx, result.AggregateID)
	}
	now := s.clock.Now()
	binding := entity.RepositoryBinding{
		Base:                 newBase(s.ids.New(), now),
		ProjectID:            input.ProjectID,
		Provider:             input.Provider,
		ProviderOwner:        strings.TrimSpace(input.ProviderOwner),
		ProviderName:         strings.TrimSpace(input.ProviderName),
		WebURL:               strings.TrimSpace(input.WebURL),
		DefaultBranch:        strings.TrimSpace(input.DefaultBranch),
		Status:               defaultRepositoryStatus(input.Status),
		ProviderRepositoryID: strings.TrimSpace(input.ProviderRepositoryID),
		IconObjectURI:        strings.TrimSpace(input.IconObjectURI),
	}
	if binding.Provider == "" || binding.ProviderOwner == "" || binding.ProviderName == "" || binding.DefaultBranch == "" {
		return entity.RepositoryBinding{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationAttachRepository, projectAggregateRepository, binding.ID, now)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	event, err := s.repositoryEvent(projectEventRepositoryAttached, binding)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	if err := s.repository.AttachRepository(ctx, binding, event, *result); err != nil {
		return entity.RepositoryBinding{}, err
	}
	return binding, nil
}

// UpdateRepository changes safe repository binding fields.
func (s *Service) UpdateRepository(ctx context.Context, input UpdateRepositoryInput) (entity.RepositoryBinding, error) {
	return s.updateRepository(ctx, input.RepositoryID, input.Meta, projectActionRepositoryUpdate, projectOperationUpdateRepository, input.DefaultBranch, input.IconObjectURI, input.Status, projectEventRepositoryUpdated)
}

// DetachRepository archives a repository binding without physical deletion.
func (s *Service) DetachRepository(ctx context.Context, repositoryID uuid.UUID, meta value.CommandMeta) (entity.RepositoryBinding, error) {
	return s.updateRepository(ctx, repositoryID, meta, projectActionRepositoryDetach, projectOperationDetachRepository, nil, nil, enum.RepositoryStatusArchived, projectEventRepositoryDetached)
}

func (s *Service) updateRepository(
	ctx context.Context,
	repositoryID uuid.UUID,
	meta value.CommandMeta,
	actionKey string,
	operation string,
	defaultBranch *string,
	iconObjectURI *string,
	status enum.RepositoryStatus,
	eventType string,
) (entity.RepositoryBinding, error) {
	if err := requireProjectID(repositoryID); err != nil {
		return entity.RepositoryBinding{}, err
	}
	if result, ok, err := s.findCommandResult(ctx, meta, operation, projectAggregateRepository); ok || err != nil {
		if err != nil {
			return entity.RepositoryBinding{}, err
		}
		return s.repository.GetRepository(ctx, result.AggregateID)
	}
	previousVersion, err := expectedVersion(meta)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	current, err := s.repository.GetRepository(ctx, repositoryID)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	if err := s.authorizeCommand(ctx, meta, actionKey, repositoryScopedResource(repositoryID, current.ProjectID)); err != nil {
		return entity.RepositoryBinding{}, err
	}
	now := s.clock.Now()
	updated := current
	updated.Base = updatedBase(current.Base, now)
	updated.DefaultBranch = applyString(current.DefaultBranch, defaultBranch)
	updated.IconObjectURI = applyString(current.IconObjectURI, iconObjectURI)
	if status != "" {
		updated.Status = status
	}
	if updated.DefaultBranch == "" {
		return entity.RepositoryBinding{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(meta, operation, projectAggregateRepository, updated.ID, now)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	event, err := s.repositoryEvent(eventType, updated)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	if err := s.repository.UpdateRepository(ctx, updated, previousVersion, event, result); err != nil {
		return entity.RepositoryBinding{}, err
	}
	return updated, nil
}

// GetRepository returns authoritative repository binding state.
func (s *Service) GetRepository(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.RepositoryBinding, error) {
	if err := requireProjectID(id); err != nil {
		return entity.RepositoryBinding{}, err
	}
	repository, err := s.repository.GetRepository(ctx, id)
	if err != nil {
		return entity.RepositoryBinding{}, err
	}
	if err := s.authorizeQuery(ctx, meta, projectActionRepositoryRead, repositoryScopedResource(id, repository.ProjectID)); err != nil {
		return entity.RepositoryBinding{}, err
	}
	return repository, nil
}

// ListRepositories returns repository bindings in a project.
func (s *Service) ListRepositories(ctx context.Context, input ListRepositoriesInput) (ListRepositoriesResult, error) {
	return listProjectScoped(s, ctx, input.ProjectID, input.Meta, projectActionRepositoryList, projectAggregateRepository,
		func(ctx context.Context) ([]entity.RepositoryBinding, value.PageResult, error) {
			return s.repository.ListRepositories(ctx, query.RepositoryFilter{
				ProjectID: input.ProjectID,
				Statuses:  input.Statuses,
				Page:      input.Page,
			})
		},
		func(repositories []entity.RepositoryBinding, page value.PageResult) ListRepositoriesResult {
			return ListRepositoriesResult{Repositories: repositories, Page: page}
		},
	)
}

func (s *Service) repositoryEvent(eventType string, binding entity.RepositoryBinding) (entity.OutboxEvent, error) {
	options := []projectEventPayloadOption{
		payloadProjectID(binding.ProjectID),
		payloadRepositoryID(binding.ID),
		payloadField(projectPayloadProvider, string(binding.Provider)),
		payloadField(projectPayloadProviderOwner, binding.ProviderOwner),
		payloadField(projectPayloadProviderName, binding.ProviderName),
		payloadField(projectPayloadStatus, string(binding.Status)),
		payloadVersion(binding.Version),
	}
	if binding.IconObjectURI != "" {
		options = append(options, payloadField(projectPayloadIconObjectURI, binding.IconObjectURI))
	}
	return s.aggregateEvent(eventType, projectAggregateRepository, binding.ID, binding.UpdatedAt, options...)
}

func repositoryScopedResource(repositoryID uuid.UUID, projectID uuid.UUID) resourceRef {
	return resourceRef{
		Type:      projectAggregateRepository,
		ID:        repositoryID.String(),
		ScopeType: "project",
		ScopeID:   projectID.String(),
	}
}
