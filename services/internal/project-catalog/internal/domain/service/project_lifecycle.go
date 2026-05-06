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

// CreateProject creates a project aggregate and records the matching domain event.
func (s *Service) CreateProject(ctx context.Context, input CreateProjectInput) (entity.Project, error) {
	if err := requireProjectID(input.OrganizationID); err != nil {
		return entity.Project{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionCreate, organizationScopedResource(projectAggregateProject, "", input.OrganizationID)); err != nil {
		return entity.Project{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationCreateProject, projectAggregateProject, input.OrganizationID, s.repository.GetProject, projectOrganizationID); ok || err != nil {
		if err != nil {
			return entity.Project{}, err
		}
		return replay, nil
	}
	now := s.clock.Now()
	project := entity.Project{
		Base:           newBase(s.ids.New(), now),
		OrganizationID: input.OrganizationID,
		Slug:           strings.TrimSpace(input.Slug),
		DisplayName:    strings.TrimSpace(input.DisplayName),
		Description:    strings.TrimSpace(input.Description),
		IconObjectURI:  strings.TrimSpace(input.IconObjectURI),
		Status:         defaultProjectStatus(input.Status),
	}
	if project.Slug == "" || project.DisplayName == "" {
		return entity.Project{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationCreateProject, projectAggregateProject, project.ID, now)
	if err != nil {
		return entity.Project{}, err
	}
	event, err := s.projectEvent(projectEventProjectCreated, project)
	if err != nil {
		return entity.Project{}, err
	}
	if err := s.repository.CreateProject(ctx, project, event, *result); err != nil {
		return entity.Project{}, err
	}
	return project, nil
}

// UpdateProject changes safe project fields using optimistic concurrency.
func (s *Service) UpdateProject(ctx context.Context, input UpdateProjectInput) (entity.Project, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return entity.Project{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, projectActionUpdate, projectScopedResource(projectAggregateProject, input.ProjectID)); err != nil {
		return entity.Project{}, err
	}
	if replay, ok, err := findScopedCommandReplay(s, ctx, input.Meta, projectOperationUpdateProject, projectAggregateProject, input.ProjectID, s.repository.GetProject, projectID); ok || err != nil {
		if err != nil {
			return entity.Project{}, err
		}
		return replay, nil
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Project{}, err
	}
	current, err := s.repository.GetProject(ctx, input.ProjectID)
	if err != nil {
		return entity.Project{}, err
	}
	now := s.clock.Now()
	updated := current
	updated.Base = updatedBase(current.Base, now)
	updated.Slug = applyString(current.Slug, input.Slug)
	updated.DisplayName = applyString(current.DisplayName, input.DisplayName)
	updated.Description = applyString(current.Description, input.Description)
	updated.IconObjectURI = applyString(current.IconObjectURI, input.IconObjectURI)
	if input.Status != "" {
		updated.Status = input.Status
	}
	if updated.Slug == "" || updated.DisplayName == "" {
		return entity.Project{}, errs.ErrInvalidArgument
	}
	result, err := commandResult(input.Meta, projectOperationUpdateProject, projectAggregateProject, updated.ID, now)
	if err != nil {
		return entity.Project{}, err
	}
	event, err := s.projectEvent(projectUpdateEventType(updated.Status), updated)
	if err != nil {
		return entity.Project{}, err
	}
	if err := s.repository.UpdateProject(ctx, updated, previousVersion, event, result); err != nil {
		return entity.Project{}, err
	}
	return updated, nil
}

// GetProject returns authoritative project state.
func (s *Service) GetProject(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.Project, error) {
	if err := requireProjectID(id); err != nil {
		return entity.Project{}, err
	}
	if err := s.authorizeQuery(ctx, meta, projectActionRead, projectScopedResource(projectAggregateProject, id)); err != nil {
		return entity.Project{}, err
	}
	return s.repository.GetProject(ctx, id)
}

// ListProjects returns projects matching the caller-visible filter.
func (s *Service) ListProjects(ctx context.Context, input ListProjectsInput) (ListProjectsResult, error) {
	if input.OrganizationID != nil && *input.OrganizationID == uuid.Nil {
		return ListProjectsResult{}, errs.ErrInvalidArgument
	}
	resource := resourceRef{Type: projectAggregateProject, ScopeType: "global"}
	if input.OrganizationID != nil {
		resource.ScopeType = "organization"
		resource.ScopeID = input.OrganizationID.String()
	}
	if err := s.authorizeQuery(ctx, input.Meta, projectActionList, resource); err != nil {
		return ListProjectsResult{}, err
	}
	projects, page, err := s.repository.ListProjects(ctx, query.ProjectFilter{
		OrganizationID: input.OrganizationID,
		Statuses:       input.Statuses,
		Page:           input.Page,
	})
	if err != nil {
		return ListProjectsResult{}, err
	}
	return ListProjectsResult{Projects: projects, Page: page}, nil
}

func (s *Service) projectEvent(eventType string, project entity.Project) (entity.OutboxEvent, error) {
	options := []projectEventPayloadOption{
		payloadProjectID(project.ID),
		payloadField(projectPayloadOrganizationID, project.OrganizationID.String()),
		payloadField(projectPayloadSlug, project.Slug),
		payloadField(projectPayloadStatus, string(project.Status)),
		payloadVersion(project.Version),
	}
	if project.IconObjectURI != "" {
		options = append(options, payloadField(projectPayloadIconObjectURI, project.IconObjectURI))
	}
	return s.aggregateEvent(eventType, projectAggregateProject, project.ID, project.UpdatedAt, options...)
}

func projectUpdateEventType(status enum.ProjectStatus) string {
	switch status {
	case enum.ProjectStatusArchived:
		return projectEventProjectArchived
	case enum.ProjectStatusDisabled:
		return projectEventProjectDisabled
	default:
		return projectEventProjectUpdated
	}
}

func projectScopedResource(resourceType string, id uuid.UUID) resourceRef {
	return resourceRef{Type: resourceType, ID: id.String(), ScopeType: "project", ScopeID: id.String()}
}

func organizationScopedResource(resourceType string, id string, organizationID uuid.UUID) resourceRef {
	return resourceRef{Type: resourceType, ID: id, ScopeType: "organization", ScopeID: organizationID.String()}
}
