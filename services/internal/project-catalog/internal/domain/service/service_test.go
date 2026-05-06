package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

func TestCreateProjectStoresOutboxAndChecksAccess(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	eventID := uuid.New()
	organizationID := uuid.New()
	authorizer := &spyAuthorizer{}
	store := newMemoryRepository()
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{projectID, eventID}}, Config{Authorizer: authorizer})

	project, err := svc.CreateProject(ctx, CreateProjectInput{
		OrganizationID: organizationID,
		Slug:           "payments",
		DisplayName:    "Платежи",
		IconObjectURI:  "s3://icons/payments.png",
		Meta:           commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	if project.ID != projectID || project.Status != enum.ProjectStatusActive {
		t.Fatalf("project = %+v, want id %s and active status", project, projectID)
	}
	if len(authorizer.requests) != 1 {
		t.Fatalf("access checks = %d, want 1", len(authorizer.requests))
	}
	request := authorizer.requests[0]
	if request.ActionKey != projectActionCreate || request.ScopeType != "organization" || request.ScopeID != organizationID.String() {
		t.Fatalf("access request = %+v, want create in organization scope", request)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1", len(store.events))
	}
	event := store.events[0]
	if event.ID != eventID || event.EventType != projectEventProjectCreated || event.AggregateID != projectID {
		t.Fatalf("event = %+v, want created event for project %s", event, projectID)
	}
	if len(store.commandResults) != 1 {
		t.Fatalf("command results = %d, want 1", len(store.commandResults))
	}
}

func TestCreateProjectReplaysCommandResultWithoutNewEvent(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	eventID := uuid.New()
	organizationID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{projectID, eventID}})

	created, err := svc.CreateProject(ctx, CreateProjectInput{
		OrganizationID: organizationID,
		Slug:           "platform",
		DisplayName:    "Платформа",
		Meta:           commandMeta(commandID),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	replayed, err := svc.CreateProject(ctx, CreateProjectInput{
		OrganizationID: organizationID,
		Slug:           "different",
		DisplayName:    "Другое имя",
		Meta:           commandMeta(commandID),
	})
	if err != nil {
		t.Fatalf("replay project command: %v", err)
	}
	if replayed.ID != created.ID || replayed.DisplayName != created.DisplayName {
		t.Fatalf("replay changed result: got %+v want %+v", replayed, created)
	}
	if len(store.events) != 1 {
		t.Fatalf("events = %d, want 1 after idempotent replay", len(store.events))
	}
}

func TestCreateProjectStopsWhenAccessDenied(t *testing.T) {
	store := newMemoryRepository()
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		Config{Authorizer: &spyAuthorizer{err: errs.ErrForbidden}},
	)

	_, err := svc.CreateProject(context.Background(), CreateProjectInput{
		OrganizationID: uuid.New(),
		Slug:           "blocked",
		DisplayName:    "Недоступный проект",
		Meta:           commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("err = %v, want %v", err, errs.ErrForbidden)
	}
	if len(store.projects) != 0 || len(store.events) != 0 {
		t.Fatalf("store was mutated after denied access: projects=%d events=%d", len(store.projects), len(store.events))
	}
}

type spyAuthorizer struct {
	requests []AuthorizationRequest
	err      error
}

func (a *spyAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) error {
	a.requests = append(a.requests, request)
	return a.err
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
}

type sequenceIDs struct {
	ids []uuid.UUID
}

func (s *sequenceIDs) New() uuid.UUID {
	if len(s.ids) == 0 {
		return uuid.New()
	}
	id := s.ids[0]
	s.ids = s.ids[1:]
	return id
}

func commandMeta(commandID uuid.UUID) value.CommandMeta {
	return value.CommandMeta{
		CommandID: commandID,
		Actor: value.Actor{
			Type: "user",
			ID:   "owner",
		},
		RequestID: "req-1",
		RequestContext: value.RequestContext{
			Source: "test",
		},
	}
}

type memoryRepository struct {
	projects       map[uuid.UUID]entity.Project
	commandResults map[string]entity.CommandResult
	events         []entity.OutboxEvent
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		projects:       map[uuid.UUID]entity.Project{},
		commandResults: map[string]entity.CommandResult{},
	}
}

func (r *memoryRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	key := identity.CommandID.String()
	if identity.CommandID == uuid.Nil {
		key = identity.Operation + ":" + identity.IdempotencyKey
	}
	result, ok := r.commandResults[key]
	if !ok {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	return result, nil
}

func (r *memoryRepository) CreateProject(_ context.Context, project entity.Project, event entity.OutboxEvent, result entity.CommandResult) error {
	r.projects[project.ID] = project
	r.events = append(r.events, event)
	r.commandResults[result.Key] = result
	return nil
}

func (r *memoryRepository) UpdateProject(_ context.Context, project entity.Project, _ int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	r.projects[project.ID] = project
	r.events = append(r.events, event)
	if result != nil {
		r.commandResults[result.Key] = *result
	}
	return nil
}

func (r *memoryRepository) GetProject(_ context.Context, id uuid.UUID) (entity.Project, error) {
	project, ok := r.projects[id]
	if !ok {
		return entity.Project{}, errs.ErrNotFound
	}
	return project, nil
}

func (r *memoryRepository) ListProjects(context.Context, query.ProjectFilter) ([]entity.Project, query.PageResult, error) {
	projects := make([]entity.Project, 0, len(r.projects))
	for _, project := range r.projects {
		projects = append(projects, project)
	}
	return projects, query.PageResult{}, nil
}

func (r *memoryRepository) AttachRepository(context.Context, entity.RepositoryBinding, entity.OutboxEvent, entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) UpdateRepository(context.Context, entity.RepositoryBinding, int64, entity.OutboxEvent, *entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetRepository(context.Context, uuid.UUID) (entity.RepositoryBinding, error) {
	return entity.RepositoryBinding{}, errs.ErrNotFound
}

func (r *memoryRepository) ListRepositories(context.Context, query.RepositoryFilter) ([]entity.RepositoryBinding, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) ImportServicesPolicy(context.Context, entity.ServicesPolicy, []entity.ServiceDescriptor, entity.OutboxEvent, entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetServicesPolicy(context.Context, uuid.UUID, *uuid.UUID) (entity.ServicesPolicy, error) {
	return entity.ServicesPolicy{}, errs.ErrNotFound
}

func (r *memoryRepository) ListServiceDescriptors(context.Context, query.ServiceDescriptorFilter) ([]entity.ServiceDescriptor, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) CreatePolicyEditProposal(context.Context, entity.PolicyEditProposal, entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) CreatePolicyOverride(context.Context, entity.PolicyOverride, entity.OutboxEvent, entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) ListPolicyOverrides(context.Context, query.PolicyOverrideFilter) ([]entity.PolicyOverride, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) PutDocumentationSource(context.Context, entity.DocumentationSource, *int64, entity.OutboxEvent, *entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetDocumentationSource(context.Context, uuid.UUID) (entity.DocumentationSource, error) {
	return entity.DocumentationSource{}, errs.ErrNotFound
}

func (r *memoryRepository) ListDocumentationSources(context.Context, query.DocumentationSourceFilter) ([]entity.DocumentationSource, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) GetWorkspacePolicy(context.Context, query.WorkspacePolicyFilter) (entity.WorkspacePolicy, error) {
	return entity.WorkspacePolicy{}, errs.ErrNotFound
}

func (r *memoryRepository) PutBranchRules(context.Context, entity.BranchRules, *int64, entity.OutboxEvent, *entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetBranchRules(context.Context, uuid.UUID) (entity.BranchRules, error) {
	return entity.BranchRules{}, errs.ErrNotFound
}

func (r *memoryRepository) ListBranchRules(context.Context, query.BranchRulesFilter) ([]entity.BranchRules, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) PutReleasePolicy(context.Context, entity.ReleasePolicy, *int64, entity.OutboxEvent, *entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetReleasePolicy(context.Context, uuid.UUID) (entity.ReleasePolicy, error) {
	return entity.ReleasePolicy{}, errs.ErrNotFound
}

func (r *memoryRepository) ListReleasePolicies(context.Context, query.ReleasePolicyFilter) ([]entity.ReleasePolicy, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) PutReleaseLine(context.Context, entity.ReleaseLine, *int64, entity.OutboxEvent, *entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetReleaseLine(context.Context, uuid.UUID) (entity.ReleaseLine, error) {
	return entity.ReleaseLine{}, errs.ErrNotFound
}

func (r *memoryRepository) ListReleaseLines(context.Context, query.ReleaseLineFilter) ([]entity.ReleaseLine, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) PutPlacementPolicy(context.Context, entity.PlacementPolicy, *int64, entity.OutboxEvent, *entity.CommandResult) error {
	return errs.ErrInvalidArgument
}

func (r *memoryRepository) GetPlacementPolicy(context.Context, uuid.UUID) (entity.PlacementPolicy, error) {
	return entity.PlacementPolicy{}, errs.ErrNotFound
}

func (r *memoryRepository) ListPlacementPolicies(context.Context, query.PlacementPolicyFilter) ([]entity.PlacementPolicy, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, nil
}

func (r *memoryRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return nil
}

func (r *memoryRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return nil
}

func (r *memoryRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return nil
}
