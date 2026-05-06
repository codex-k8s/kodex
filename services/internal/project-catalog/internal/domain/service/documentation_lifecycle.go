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

// PutDocumentationSource creates or updates a documentation source.
func (s *Service) PutDocumentationSource(ctx context.Context, input PutDocumentationSourceInput) (entity.DocumentationSource, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.DocumentationSource{}, err
	}
	sourceID := s.ids.New()
	previousVersion, err := previousVersion(input.Meta)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	if input.DocumentationSourceID != nil {
		sourceID = *input.DocumentationSourceID
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionDocsUpdate, projectScopedResource(projectAggregateDocumentationSource, input.ProjectID)); err != nil {
		return entity.DocumentationSource{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationPutDocumentation, projectAggregateDocumentationSource, input.ProjectID, s.repository.GetDocumentationSource, documentationSourceProjectID); ok || err != nil {
		if err != nil {
			return entity.DocumentationSource{}, err
		}
		return replay, nil
	}
	now := s.clock.Now()
	localPath, err := normalizeWorkspacePath(input.LocalPath)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	source := entity.DocumentationSource{
		Base:         newBase(sourceID, now),
		ProjectID:    input.ProjectID,
		RepositoryID: input.RepositoryID,
		ScopeType:    input.ScopeType,
		ScopeID:      strings.TrimSpace(input.ScopeID),
		LocalPath:    localPath,
		AccessMode:   defaultDocumentationAccessMode(input.AccessMode),
		Status:       defaultDocumentationStatus(input.Status),
	}
	if previousVersion != nil {
		source.Version = *previousVersion + 1
	}
	if err := validateDocumentationSource(source); err != nil {
		return entity.DocumentationSource{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationPutDocumentation, projectAggregateDocumentationSource, source.ID, now)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	event, err := s.documentationEvent(documentationEventType(previousVersion == nil, source.Status), source)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	if err := s.repository.PutDocumentationSource(ctx, source, previousVersion, event, result); err != nil {
		return entity.DocumentationSource{}, err
	}
	return source, nil
}

// GetDocumentationSource returns a documentation source.
func (s *Service) GetDocumentationSource(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.DocumentationSource, error) {
	return readProjectScopedAggregate(s, ctx, id, meta, projectActionDocsRead, projectAggregateDocumentationSource, s.repository.GetDocumentationSource, documentationSourceProjectID)
}

// ListDocumentationSources returns documentation sources matching filter.
func (s *Service) ListDocumentationSources(ctx context.Context, input ListDocumentationSourcesInput) (ListDocumentationSourcesResult, error) {
	return listProjectScoped(s, ctx, input.ProjectID, input.Meta, projectActionDocsRead, projectAggregateDocumentationSource,
		func(ctx context.Context) ([]entity.DocumentationSource, value.PageResult, error) {
			return s.repository.ListDocumentationSources(ctx, query.DocumentationSourceFilter{
				ProjectID:    input.ProjectID,
				RepositoryID: input.RepositoryID,
				ScopeType:    input.ScopeType,
				ScopeID:      input.ScopeID,
				Statuses:     input.Statuses,
				Page:         input.Page,
			})
		},
		func(sources []entity.DocumentationSource, page value.PageResult) ListDocumentationSourcesResult {
			return ListDocumentationSourcesResult{DocumentationSources: sources, Page: page}
		},
	)
}

// GetWorkspacePolicy returns the source set allowed for an agent workspace.
func (s *Service) GetWorkspacePolicy(ctx context.Context, input GetWorkspacePolicyInput) (entity.WorkspacePolicy, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.WorkspacePolicy{}, err
	}
	if err := validateWorkspacePolicyFilter(input); err != nil {
		return entity.WorkspacePolicy{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, projectActionWorkspaceRead, projectScopedResource(projectAggregateProject, input.ProjectID)); err != nil {
		return entity.WorkspacePolicy{}, err
	}
	return s.repository.GetWorkspacePolicy(ctx, query.WorkspacePolicyFilter{
		ProjectID:               input.ProjectID,
		RepositoryIDs:           input.RepositoryIDs,
		ServiceKeys:             input.ServiceKeys,
		IncludeGuidancePackages: input.IncludeGuidancePackages,
	})
}

func validateWorkspacePolicyFilter(input GetWorkspacePolicyInput) error {
	for _, repositoryID := range input.RepositoryIDs {
		if repositoryID == uuid.Nil {
			return errs.ErrInvalidArgument
		}
	}
	for _, serviceKey := range input.ServiceKeys {
		if !serviceKeyPattern.MatchString(strings.TrimSpace(serviceKey)) {
			return errs.ErrInvalidArgument
		}
	}
	return nil
}

func (s *Service) documentationEvent(eventType string, source entity.DocumentationSource) (entity.OutboxEvent, error) {
	return s.aggregateEvent(
		eventType,
		projectAggregateDocumentationSource,
		source.ID,
		source.UpdatedAt,
		payloadProjectID(source.ProjectID),
		payloadField(projectPayloadSourceID, source.ID.String()),
		payloadField(projectPayloadScopeType, string(source.ScopeType)),
		payloadField(projectPayloadAccessMode, string(source.AccessMode)),
		payloadField(projectPayloadStatus, string(source.Status)),
		payloadVersion(source.Version),
	)
}

func documentationEventType(created bool, status enum.DocumentationSourceStatus) string {
	if status == enum.DocumentationSourceStatusDisabled {
		return projectEventDocumentationDisabled
	}
	if created {
		return projectEventDocumentationCreated
	}
	return projectEventDocumentationUpdated
}
