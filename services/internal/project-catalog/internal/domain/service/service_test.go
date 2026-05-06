package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectrepo "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/repository/project"
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

func TestCreateProjectReplayRejectsDifferentOrganizationScope(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	organizationA := uuid.New()
	organizationB := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.projects[projectID] = entity.Project{
		Base:           entity.Base{ID: projectID, Version: 1},
		OrganizationID: organizationA,
		Slug:           "private",
		DisplayName:    "Private",
		Status:         enum.ProjectStatusActive,
	}
	store.commandResults[commandID.String()] = entity.CommandResult{
		Key:           commandID.String(),
		CommandID:     commandID,
		Operation:     projectOperationCreateProject,
		AggregateType: projectAggregateProject,
		AggregateID:   projectID,
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})

	_, err := svc.CreateProject(ctx, CreateProjectInput{
		OrganizationID: organizationB,
		Slug:           "other",
		DisplayName:    "Other",
		Meta:           commandMeta(commandID),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want %v", err, errs.ErrConflict)
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

func TestUpdateProjectReplayRejectsDifferentProjectScope(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.projects[projectA] = entity.Project{
		Base:           entity.Base{ID: projectA, Version: 1},
		OrganizationID: uuid.New(),
		Slug:           "project-a",
		DisplayName:    "Project A",
		Status:         enum.ProjectStatusActive,
	}
	store.projects[projectB] = entity.Project{
		Base:           entity.Base{ID: projectB, Version: 1},
		OrganizationID: uuid.New(),
		Slug:           "project-b",
		DisplayName:    "Project B",
		Status:         enum.ProjectStatusActive,
	}
	store.commandResults[commandID.String()] = entity.CommandResult{
		Key:           commandID.String(),
		CommandID:     commandID,
		Operation:     projectOperationUpdateProject,
		AggregateType: projectAggregateProject,
		AggregateID:   projectA,
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})

	_, err := svc.UpdateProject(ctx, UpdateProjectInput{
		ProjectID:   projectB,
		DisplayName: stringPtr("ignored"),
		Meta:        commandMeta(commandID),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestUpdateRepositoryReplayStillChecksAccess(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = entity.RepositoryBinding{
		Base:          entity.Base{ID: repositoryID, Version: 1},
		ProjectID:     projectID,
		Provider:      enum.RepositoryProviderGitHub,
		ProviderOwner: "codex-k8s",
		ProviderName:  "kodex",
		DefaultBranch: "main",
		Status:        enum.RepositoryStatusActive,
	}
	authorizer := &spyAuthorizer{}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}}, Config{Authorizer: authorizer})

	meta := commandMeta(commandID)
	meta.ExpectedVersion = int64Ptr(1)
	updated, err := svc.UpdateRepository(ctx, UpdateRepositoryInput{
		RepositoryID:  repositoryID,
		DefaultBranch: stringPtr("develop"),
		Meta:          meta,
	})
	if err != nil {
		t.Fatalf("UpdateRepository(): %v", err)
	}
	if updated.DefaultBranch != "develop" {
		t.Fatalf("updated default branch = %q, want develop", updated.DefaultBranch)
	}

	authorizer.err = errs.ErrForbidden
	_, err = svc.UpdateRepository(ctx, UpdateRepositoryInput{
		RepositoryID:  repositoryID,
		DefaultBranch: stringPtr("ignored"),
		Meta:          meta,
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("replay err = %v, want %v", err, errs.ErrForbidden)
	}
	if len(authorizer.requests) != 2 {
		t.Fatalf("access checks = %d, want 2 including replay", len(authorizer.requests))
	}
}

func TestPutBranchRulesReplayRejectsDifferentProjectScope(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	rulesID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.branchRules[rulesID] = entity.BranchRules{
		Base:      entity.Base{ID: rulesID, Version: 1},
		ProjectID: projectA,
		Pattern:   "main",
		Status:    enum.BranchRulesStatusActive,
	}
	store.commandResults[commandID.String()] = entity.CommandResult{
		Key:           commandID.String(),
		CommandID:     commandID,
		Operation:     projectOperationPutBranchRules,
		AggregateType: projectAggregateBranchRules,
		AggregateID:   rulesID,
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})

	_, err := svc.PutBranchRules(ctx, PutBranchRulesInput{
		ProjectID: projectB,
		Pattern:   "main",
		Meta:      commandMeta(commandID),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestCreatePolicyEditProposalReplayRejectsDifferentProjectScope(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	proposalID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.policyEditProposals[proposalID] = entity.PolicyEditProposal{
		ID:           proposalID,
		ProjectID:    projectA,
		RepositoryID: uuid.New(),
		SourcePath:   "services.yaml",
		RequestedChanges: value.PolicyEditProposalRequestedChanges{
			Summary: "Изменить политику проекта A",
		},
		Status:    projectProposalStatusPending,
		CreatedAt: fixedClock{}.Now(),
	}
	store.commandResults[commandID.String()] = entity.CommandResult{
		Key:           commandID.String(),
		CommandID:     commandID,
		Operation:     projectOperationPolicyEditProposal,
		AggregateType: projectAggregatePolicyEditProposal,
		AggregateID:   proposalID,
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})

	_, err := svc.CreatePolicyEditProposal(ctx, CreatePolicyEditProposalInput{
		ProjectID:    projectB,
		RepositoryID: uuid.New(),
		SourcePath:   "services.yaml",
		Meta:         commandMeta(commandID),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestCreatePolicyOverrideReplayRejectsDifferentProjectScope(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	overrideID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.policyOverrides[overrideID] = entity.PolicyOverride{
		Base:              entity.Base{ID: overrideID, Version: 1, CreatedAt: fixedClock{}.Now(), UpdatedAt: fixedClock{}.Now()},
		ProjectID:         projectA,
		TargetType:        enum.PolicyOverrideTargetServicesPolicy,
		Payload:           []byte(`{"reason":"test"}`),
		Reason:            "test",
		Status:            enum.PolicyOverrideStatusActive,
		ExpiresAt:         fixedClock{}.Now().Add(time.Hour),
		CreatedByActorRef: "user:owner",
	}
	store.commandResults[commandID.String()] = entity.CommandResult{
		Key:           commandID.String(),
		CommandID:     commandID,
		Operation:     projectOperationPolicyOverride,
		AggregateType: projectAggregatePolicyOverride,
		AggregateID:   overrideID,
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})

	_, err := svc.CreatePolicyOverride(ctx, CreatePolicyOverrideInput{
		ProjectID: projectB,
		Meta:      commandMeta(commandID),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestCreatePolicyOverrideReplayReturnsStoredInactiveOverride(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	overrideID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	saved := entity.PolicyOverride{
		Base:              entity.Base{ID: overrideID, Version: 1, CreatedAt: fixedClock{}.Now(), UpdatedAt: fixedClock{}.Now()},
		ProjectID:         projectID,
		TargetType:        enum.PolicyOverrideTargetServicesPolicy,
		Payload:           []byte(`{"reason":"expired"}`),
		Reason:            "expired",
		Status:            enum.PolicyOverrideStatusExpired,
		ExpiresAt:         fixedClock{}.Now().Add(-time.Hour),
		CreatedByActorRef: "user:owner",
	}
	store.policyOverrides[overrideID] = saved
	store.commandResults[commandID.String()] = entity.CommandResult{
		Key:           commandID.String(),
		CommandID:     commandID,
		Operation:     projectOperationPolicyOverride,
		AggregateType: projectAggregatePolicyOverride,
		AggregateID:   overrideID,
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})

	replayed, err := svc.CreatePolicyOverride(ctx, CreatePolicyOverrideInput{
		ProjectID: projectID,
		Meta:      commandMeta(commandID),
	})
	if err != nil {
		t.Fatalf("replay policy override: %v", err)
	}
	if replayed.ID != saved.ID || replayed.Status != enum.PolicyOverrideStatusExpired {
		t.Fatalf("replayed override = %+v, want saved inactive override %+v", replayed, saved)
	}
}

func TestImportServicesPolicyAssignsMonotonicVersions(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{
		uuid.New(), uuid.New(),
		uuid.New(), uuid.New(),
	}})

	first, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:          projectID,
		SourceRepositoryID: &repositoryID,
		SourcePath:         "services.yaml",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		ContentHash:        "sha256:first",
		ValidatedPayload:   []byte(servicesPolicyPayload("api", "services/internal/api", "backend")),
		Meta:               commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("first ImportServicesPolicy(): %v", err)
	}
	second, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:          projectID,
		SourceRepositoryID: &repositoryID,
		SourcePath:         "services.yaml",
		SourceCommitSHA:    "abcdef0123456789abcdef0123456789abcdef01",
		ContentHash:        "sha256:second",
		ValidatedPayload:   []byte(servicesPolicyPayload("worker", "services/internal/worker", "worker")),
		Meta:               commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("second ImportServicesPolicy(): %v", err)
	}
	if first.PolicyVersion != 1 || second.PolicyVersion != 2 {
		t.Fatalf("policy versions = %d, %d; want 1, 2", first.PolicyVersion, second.PolicyVersion)
	}
	var payload value.ProjectEventPayload
	if err := json.Unmarshal(store.events[len(store.events)-1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal event payload: %v", err)
	}
	if payload.PolicyVersion != second.PolicyVersion {
		t.Fatalf("event policy version = %d, want %d", payload.PolicyVersion, second.PolicyVersion)
	}
}

func TestImportServicesPolicyBuildsDescriptorsFromPayload(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{
		uuid.New(), uuid.New(), uuid.New(),
	}})

	policy, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:          projectID,
		SourceRepositoryID: &repositoryID,
		SourcePath:         "services.yaml",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		ContentHash:        "sha256:policy",
		ValidatedPayload: []byte(`{
			"apiVersion": "kodex.works/v1alpha1",
			"kind": "ServiceStackDraft",
			"spec": {
				"services": [
					{"key":"api","displayName":"Public API","kind":"backend","rootPath":"services/api","documentationScopeId":"api"},
					{"key":"worker","kind":"worker","rootPath":"services/worker","dependsOn":["api"]}
				]
			}
		}`),
		ServiceDescriptors: []entity.ServiceDescriptor{
			{ServiceKey: "ignored", DisplayName: "Ignored", Kind: enum.ServiceKindOther, RootPath: "ignored"},
		},
		Meta: commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("ImportServicesPolicy(): %v", err)
	}
	descriptors := store.serviceDescriptors[policy.ID]
	if len(descriptors) != 2 {
		t.Fatalf("descriptors = %d, want 2", len(descriptors))
	}
	if descriptors[0].ServiceKey != "api" || descriptors[0].RepositoryID == nil || *descriptors[0].RepositoryID != repositoryID {
		t.Fatalf("first descriptor = %+v, want api with source repository", descriptors[0])
	}
	if descriptors[1].DisplayName != "worker" || len(descriptors[1].DependsOnServiceKeys) != 1 || descriptors[1].DependsOnServiceKeys[0] != "api" {
		t.Fatalf("second descriptor = %+v, want worker depending on api", descriptors[1])
	}
}

func TestImportServicesPolicyAcceptsDeployableServicesDraftShape(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}})

	policy, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:        projectID,
		SourcePath:       "services.yaml",
		SourceCommitSHA:  "0123456789abcdef0123456789abcdef01234567",
		ContentHash:      "sha256:policy",
		ValidatedPayload: []byte(`{"apiVersion":"kodex.works/v1alpha1","kind":"ServiceStackDraft","spec":{"deployableServices":[{"name":"project-catalog","status":"foundation","path":"services/internal/project-catalog"}]}}`),
		Meta:             commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("ImportServicesPolicy(): %v", err)
	}
	descriptors := store.serviceDescriptors[policy.ID]
	if len(descriptors) != 1 {
		t.Fatalf("descriptors = %d, want 1", len(descriptors))
	}
	descriptor := descriptors[0]
	if descriptor.ServiceKey != "project-catalog" || descriptor.RootPath != "services/internal/project-catalog" || descriptor.Status != enum.ServiceStatusActive {
		t.Fatalf("descriptor = %+v, want active project-catalog descriptor", descriptor)
	}
}

func TestImportServicesPolicyRejectsDuplicateServiceKeys(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

	_, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:        uuid.New(),
		SourcePath:       "services.yaml",
		SourceCommitSHA:  "0123456789abcdef0123456789abcdef01234567",
		ContentHash:      "sha256:policy",
		ValidatedPayload: []byte(`{"spec":{"services":[{"key":"api","rootPath":"services/api"},{"key":"api","rootPath":"services/api-copy"}]}}`),
		Meta:             commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ImportServicesPolicy() err = %v, want invalid argument", err)
	}
}

func TestImportServicesPolicyRejectsUnknownDependency(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

	_, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:        uuid.New(),
		SourcePath:       "services.yaml",
		SourceCommitSHA:  "0123456789abcdef0123456789abcdef01234567",
		ContentHash:      "sha256:policy",
		ValidatedPayload: []byte(`{"spec":{"services":[{"key":"worker","rootPath":"services/worker","dependsOn":["api"]}]}}`),
		Meta:             commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ImportServicesPolicy() err = %v, want invalid argument", err)
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

func int64Ptr(value int64) *int64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func servicesPolicyPayload(key string, rootPath string, kind string) string {
	return `{"spec":{"services":[{"key":"` + key + `","rootPath":"` + rootPath + `","kind":"` + kind + `"}]}}`
}

type memoryRepository struct {
	projects            map[uuid.UUID]entity.Project
	repositories        map[uuid.UUID]entity.RepositoryBinding
	policies            map[uuid.UUID]entity.ServicesPolicy
	serviceDescriptors  map[uuid.UUID][]entity.ServiceDescriptor
	branchRules         map[uuid.UUID]entity.BranchRules
	policyEditProposals map[uuid.UUID]entity.PolicyEditProposal
	policyOverrides     map[uuid.UUID]entity.PolicyOverride
	policyVersions      map[uuid.UUID]int64
	commandResults      map[string]entity.CommandResult
	events              []entity.OutboxEvent
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		projects:            map[uuid.UUID]entity.Project{},
		repositories:        map[uuid.UUID]entity.RepositoryBinding{},
		policies:            map[uuid.UUID]entity.ServicesPolicy{},
		serviceDescriptors:  map[uuid.UUID][]entity.ServiceDescriptor{},
		branchRules:         map[uuid.UUID]entity.BranchRules{},
		policyEditProposals: map[uuid.UUID]entity.PolicyEditProposal{},
		policyOverrides:     map[uuid.UUID]entity.PolicyOverride{},
		policyVersions:      map[uuid.UUID]int64{},
		commandResults:      map[string]entity.CommandResult{},
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

func (r *memoryRepository) AttachRepository(_ context.Context, repository entity.RepositoryBinding, event entity.OutboxEvent, result entity.CommandResult) error {
	r.repositories[repository.ID] = repository
	r.events = append(r.events, event)
	r.commandResults[result.Key] = result
	return nil
}

func (r *memoryRepository) UpdateRepository(_ context.Context, repository entity.RepositoryBinding, _ int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	r.repositories[repository.ID] = repository
	r.events = append(r.events, event)
	if result != nil {
		r.commandResults[result.Key] = *result
	}
	return nil
}

func (r *memoryRepository) GetRepository(_ context.Context, id uuid.UUID) (entity.RepositoryBinding, error) {
	repository, ok := r.repositories[id]
	if !ok {
		return entity.RepositoryBinding{}, errs.ErrNotFound
	}
	return repository, nil
}

func (r *memoryRepository) ListRepositories(context.Context, query.RepositoryFilter) ([]entity.RepositoryBinding, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) ImportServicesPolicy(_ context.Context, policy entity.ServicesPolicy, descriptors []entity.ServiceDescriptor, result entity.CommandResult, buildEvent projectrepo.ServicesPolicyEventBuilder) (entity.ServicesPolicy, error) {
	r.policyVersions[policy.ProjectID]++
	policy.PolicyVersion = r.policyVersions[policy.ProjectID]
	event, err := buildEvent(policy)
	if err != nil {
		return entity.ServicesPolicy{}, err
	}
	r.policies[policy.ID] = policy
	r.serviceDescriptors[policy.ID] = descriptors
	r.events = append(r.events, event)
	r.commandResults[result.Key] = result
	return policy, nil
}

func (r *memoryRepository) GetServicesPolicy(_ context.Context, projectID uuid.UUID, policyID *uuid.UUID) (entity.ServicesPolicy, error) {
	if policyID != nil {
		policy, ok := r.policies[*policyID]
		if !ok {
			return entity.ServicesPolicy{}, errs.ErrNotFound
		}
		return policy, nil
	}
	var latest entity.ServicesPolicy
	for _, policy := range r.policies {
		if policy.ProjectID == projectID && policy.PolicyVersion > latest.PolicyVersion {
			latest = policy
		}
	}
	if latest.ID == uuid.Nil {
		return entity.ServicesPolicy{}, errs.ErrNotFound
	}
	return latest, nil
}

func (r *memoryRepository) ListServiceDescriptors(context.Context, query.ServiceDescriptorFilter) ([]entity.ServiceDescriptor, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) CreatePolicyEditProposal(_ context.Context, proposal entity.PolicyEditProposal, result entity.CommandResult) error {
	r.policyEditProposals[proposal.ID] = proposal
	r.commandResults[result.Key] = result
	return nil
}

func (r *memoryRepository) GetPolicyEditProposal(_ context.Context, id uuid.UUID) (entity.PolicyEditProposal, error) {
	proposal, ok := r.policyEditProposals[id]
	if !ok {
		return entity.PolicyEditProposal{}, errs.ErrNotFound
	}
	return proposal, nil
}

func (r *memoryRepository) CreatePolicyOverride(_ context.Context, override entity.PolicyOverride, event entity.OutboxEvent, result entity.CommandResult) error {
	r.policyOverrides[override.ID] = override
	r.events = append(r.events, event)
	r.commandResults[result.Key] = result
	return nil
}

func (r *memoryRepository) GetPolicyOverride(_ context.Context, id uuid.UUID) (entity.PolicyOverride, error) {
	override, ok := r.policyOverrides[id]
	if !ok {
		return entity.PolicyOverride{}, errs.ErrNotFound
	}
	return override, nil
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

func (r *memoryRepository) GetBranchRules(_ context.Context, id uuid.UUID) (entity.BranchRules, error) {
	rules, ok := r.branchRules[id]
	if !ok {
		return entity.BranchRules{}, errs.ErrNotFound
	}
	return rules, nil
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
