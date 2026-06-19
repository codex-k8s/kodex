package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
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

func TestImportServicesPolicyValidatesDocumentationSources(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
	}})

	policy, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:          projectID,
		SourceRepositoryID: &repositoryID,
		SourcePath:         "services.yaml",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		ContentHash:        "sha256:policy",
		ValidatedPayload: []byte(`{
			"spec": {
				"services": [
					{"key":"api","rootPath":"services/api","dependsOn":["db"]},
					{"key":"db","rootPath":"services/db","documentationScopeId":"database"}
				],
				"documentationSources": [
					{"scopeType":"project","localPath":"docs/product","accessMode":"read"},
					{"scopeType":"service","scopeId":"api","localPath":"docs/api","accessMode":"write"},
					{"scopeType":"dependency","scopeId":"database","localPath":"docs/db","accessMode":"read"},
					{"scopeType":"guidance_ref","scopeId":"github.com/codex-k8s/kodex-guidelines-go-backend-ru","localPath":"docs/external/guidelines/go"}
				]
			}
		}`),
		Meta: commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("ImportServicesPolicy(): %v", err)
	}
	if len(store.policyDocumentationSources[policy.ID]) != 4 {
		t.Fatalf("policy documentation sources = %d, want 4", len(store.policyDocumentationSources[policy.ID]))
	}
	for _, source := range store.policyDocumentationSources[policy.ID] {
		if source.ProjectID != projectID || source.ID == uuid.Nil {
			t.Fatalf("policy documentation source = %+v, want project id and generated id", source)
		}
	}
}

func TestImportServicesPolicyRejectsUnknownDocumentationScope(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

	_, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:        uuid.New(),
		SourcePath:       "services.yaml",
		SourceCommitSHA:  "0123456789abcdef0123456789abcdef01234567",
		ContentHash:      "sha256:policy",
		ValidatedPayload: []byte(`{"spec":{"services":[{"key":"api","rootPath":"services/api"}],"documentationSources":[{"scopeType":"service","scopeId":"missing","localPath":"docs/missing"}]}}`),
		Meta:             commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ImportServicesPolicy() err = %v, want invalid argument", err)
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

func TestImportServicesPolicyRejectsPayloadWithoutServiceEntries(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

	_, err := svc.ImportServicesPolicy(ctx, ImportServicesPolicyInput{
		ProjectID:        uuid.New(),
		SourcePath:       "services.yaml",
		SourceCommitSHA:  "0123456789abcdef0123456789abcdef01234567",
		ContentHash:      "sha256:policy",
		ValidatedPayload: []byte(`{"spec":{}}`),
		ServiceDescriptors: []entity.ServiceDescriptor{
			{ServiceKey: "api", DisplayName: "API", Kind: enum.ServiceKindBackend, RootPath: "services/api"},
		},
		Meta: commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ImportServicesPolicy() err = %v, want invalid argument", err)
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

func TestCreateProviderRepositoryCreatesPendingBindingAndStoresSafeRefs(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	provider := &spyBootstrapProvider{
		createRepoResult: RepositoryProviderCreateProviderResult{
			ProviderOperationID:  "operation-1",
			ProviderResultRef:    "https://github.com/codex-k8s/new-service",
			ProviderRepositoryID: "100500",
			ProviderWebURL:       "https://github.com/codex-k8s/new-service",
			ProviderObjectID:     "100500",
			ProviderVersion:      "etag-1",
			BaseBranch:           "main",
			RepositoryFullName:   "codex-k8s/new-service",
		},
	}
	authorizer := &spyAuthorizer{}
	store := newMemoryRepository()
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{repositoryID, uuid.New(), uuid.New()}},
		Config{Authorizer: authorizer, BootstrapProvider: provider},
	)

	result, err := svc.CreateProviderRepository(ctx, CreateProviderRepositoryInput{
		ProjectID:         projectID,
		Provider:          enum.RepositoryProviderGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     "codex-k8s",
		ProviderName:      "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		Description:       "safe description",
		ExternalAccountID: externalAccountID,
		Meta:              commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("CreateProviderRepository(): %v", err)
	}
	if provider.createRepoCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.createRepoCalls)
	}
	if provider.createRepoInput.RepositoryID != repositoryID ||
		provider.createRepoInput.ProviderSlug != "github" ||
		provider.createRepoInput.ProviderOwner != "codex-k8s" ||
		provider.createRepoInput.RepositoryName != "new-service" ||
		provider.createRepoInput.Visibility != enum.RepositoryVisibilityPrivate ||
		provider.createRepoInput.ExternalAccountID != externalAccountID {
		t.Fatalf("provider input = %+v, want create repository request", provider.createRepoInput)
	}
	if result.Repository.ID != repositoryID ||
		result.Repository.Status != enum.RepositoryStatusPending ||
		result.Repository.DefaultBranch != "main" ||
		result.Repository.ProviderRepositoryID != "100500" ||
		result.Repository.WebURL != "https://github.com/codex-k8s/new-service" {
		t.Fatalf("repository result = %+v, want pending binding with provider refs", result.Repository)
	}
	if result.ProviderTarget.RepositoryFullName != "codex-k8s/new-service" ||
		result.ProviderTarget.ProviderRepositoryID != "100500" ||
		result.BaseBranch != "main" {
		t.Fatalf("provider target = %+v base=%q, want repository target and base branch", result.ProviderTarget, result.BaseBranch)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != projectActionRepositoryAttach {
		t.Fatalf("access requests = %+v, want repository attach check", authorizer.requests)
	}
	if len(store.events) != 2 ||
		store.events[0].EventType != projectEventRepositoryAttached ||
		store.events[1].EventType != projectEventRepositoryUpdated {
		t.Fatalf("events = %+v, want pending attach and provider refs update", store.events)
	}
	if len(store.commandResults) != 1 {
		t.Fatalf("command results = %d, want final idempotency record", len(store.commandResults))
	}
	for _, commandResult := range store.commandResults {
		if bytes.Contains(commandResult.ResultPayload, []byte("safe description")) ||
			bytes.Contains(commandResult.ResultPayload, []byte("raw_provider_payload")) ||
			bytes.Contains(commandResult.ResultPayload, []byte("files")) {
			t.Fatalf("command payload stores unsafe provider/bootstrap data: %s", commandResult.ResultPayload)
		}
	}
}

func TestCreateProviderRepositoryProviderFailureLeavesPendingBindingWithoutFinalResult(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	provider := &spyBootstrapProvider{createRepoErr: errs.ErrDependencyUnavailable}
	store := newMemoryRepository()
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{repositoryID, uuid.New()}},
		Config{BootstrapProvider: provider},
	)

	_, err := svc.CreateProviderRepository(ctx, CreateProviderRepositoryInput{
		ProjectID:         projectID,
		Provider:          enum.RepositoryProviderGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     "codex-k8s",
		ProviderName:      "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		ExternalAccountID: uuid.New(),
		Meta:              commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("CreateProviderRepository() err = %v, want dependency unavailable", err)
	}
	binding := store.repositories[repositoryID]
	if binding.ID != repositoryID ||
		binding.Status != enum.RepositoryStatusPending ||
		binding.DefaultBranch != "" ||
		binding.ProviderRepositoryID != "" ||
		binding.WebURL != "" {
		t.Fatalf("binding after provider failure = %+v, want only pending reservation", binding)
	}
	if len(store.commandResults) != 0 {
		t.Fatalf("command results = %d, want no final replay record after provider failure", len(store.commandResults))
	}
	if len(store.events) != 1 || store.events[0].EventType != projectEventRepositoryAttached {
		t.Fatalf("events = %+v, want only pending attach event", store.events)
	}
}

func TestCreateProviderRepositoryReplayDoesNotCallProviderAgain(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	commandID := uuid.New()
	provider := &spyBootstrapProvider{
		createRepoResult: RepositoryProviderCreateProviderResult{
			ProviderOperationID:  "operation-1",
			ProviderRepositoryID: "100500",
			ProviderWebURL:       "https://github.com/codex-k8s/new-service",
			BaseBranch:           "main",
			RepositoryFullName:   "codex-k8s/new-service",
		},
	}
	store := newMemoryRepository()
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}},
		Config{BootstrapProvider: provider},
	)
	input := CreateProviderRepositoryInput{
		ProjectID:         projectID,
		Provider:          enum.RepositoryProviderGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     "codex-k8s",
		ProviderName:      "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		ExternalAccountID: uuid.New(),
		Meta:              commandMeta(commandID),
	}

	first, err := svc.CreateProviderRepository(ctx, input)
	if err != nil {
		t.Fatalf("CreateProviderRepository() first: %v", err)
	}
	input.ProviderName = "changed"
	second, err := svc.CreateProviderRepository(ctx, input)
	if err != nil {
		t.Fatalf("CreateProviderRepository() replay: %v", err)
	}
	if provider.createRepoCalls != 1 {
		t.Fatalf("provider calls = %d, want one call after replay", provider.createRepoCalls)
	}
	if second.Repository.ID != first.Repository.ID ||
		second.ProviderResult.ProviderOperationID != "operation-1" ||
		second.ProviderTarget.RepositoryFullName != "codex-k8s/new-service" {
		t.Fatalf("replay result = %+v, want original provider repository result", second)
	}
	if len(store.events) != 2 {
		t.Fatalf("events = %d, want no new events after replay", len(store.events))
	}
}

func TestCreateProviderRepositoryRetriesPendingReservation(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	provider := &spyBootstrapProvider{createRepoErr: errs.ErrDependencyUnavailable}
	store := newMemoryRepository()
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{repositoryID, uuid.New(), uuid.New()}},
		Config{BootstrapProvider: provider},
	)
	input := CreateProviderRepositoryInput{
		ProjectID:         projectID,
		Provider:          enum.RepositoryProviderGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     "codex-k8s",
		ProviderName:      "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		ExternalAccountID: uuid.New(),
		Meta:              commandMeta(uuid.New()),
	}

	_, err := svc.CreateProviderRepository(ctx, input)
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("first err = %v, want dependency unavailable", err)
	}
	provider.createRepoErr = nil
	provider.createRepoResult = RepositoryProviderCreateProviderResult{
		ProviderRepositoryID: "100500",
		ProviderWebURL:       "https://github.com/codex-k8s/new-service",
		BaseBranch:           "main",
		RepositoryFullName:   "codex-k8s/new-service",
	}
	result, err := svc.CreateProviderRepository(ctx, input)
	if err != nil {
		t.Fatalf("retry CreateProviderRepository(): %v", err)
	}
	if result.Repository.ID != repositoryID || result.Repository.DefaultBranch != "main" {
		t.Fatalf("retry result = %+v, want same binding completed", result.Repository)
	}
	if provider.createRepoCalls != 2 {
		t.Fatalf("provider calls = %d, want retry to call provider again", provider.createRepoCalls)
	}
}

func TestCreateProviderRepositoryRejectsIncompleteProviderResult(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	provider := &spyBootstrapProvider{
		createRepoResult: RepositoryProviderCreateProviderResult{
			ProviderRepositoryID: "100500",
			ProviderWebURL:       "https://github.com/codex-k8s/new-service",
		},
	}
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		Config{BootstrapProvider: provider},
	)

	_, err := svc.CreateProviderRepository(ctx, CreateProviderRepositoryInput{
		ProjectID:         uuid.New(),
		Provider:          enum.RepositoryProviderGitHub,
		OwnerKind:         enum.RepositoryOwnerKindOrganization,
		ProviderOwner:     "codex-k8s",
		ProviderName:      "new-service",
		Visibility:        enum.RepositoryVisibilityPrivate,
		ExternalAccountID: uuid.New(),
		Meta:              commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("CreateProviderRepository() err = %v, want dependency unavailable for missing base branch", err)
	}
	if len(store.commandResults) != 0 {
		t.Fatalf("command results = %d, want no final replay record", len(store.commandResults))
	}
}

func TestCreateRepositoryBootstrapPullRequestDelegatesProviderWrite(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = entity.RepositoryBinding{
		Base:                 entity.Base{ID: repositoryID, Version: 1},
		ProjectID:            projectID,
		Provider:             enum.RepositoryProviderGitHub,
		ProviderOwner:        "codex-k8s",
		ProviderName:         "kodex",
		WebURL:               "https://github.com/codex-k8s/kodex",
		DefaultBranch:        "main",
		Status:               enum.RepositoryStatusPending,
		ProviderRepositoryID: "R_123",
	}
	authorizer := &spyAuthorizer{}
	provider := &spyBootstrapProvider{
		result: RepositoryBootstrapProviderResult{
			ProviderOperationID:          "operation-1",
			ProviderWorkItemProjectionID: "projection-1",
			ProviderWebURL:               "https://github.com/codex-k8s/kodex/pull/1",
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{Authorizer: authorizer, BootstrapProvider: provider})

	result, err := svc.CreateRepositoryBootstrapPullRequest(ctx, CreateRepositoryBootstrapPullRequestInput{
		ProjectID:       projectID,
		RepositoryID:    repositoryID,
		BaseBranch:      "main",
		BootstrapBranch: "kodex/bootstrap",
		CommitMessage:   "Bootstrap repository",
		Title:           "Bootstrap repository",
		Files: []RepositoryBootstrapFile{{
			Path:    "services.yaml",
			Content: bootstrapServicesPolicyFileContent,
		}},
		WatermarkJSON:     []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`),
		ServicesPolicy:    bootstrapServicesPolicy(),
		ExternalAccountID: externalAccountID,
		Meta:              commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("CreateRepositoryBootstrapPullRequest(): %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if provider.input.ProviderSlug != "github" || provider.input.RepositoryTarget.RepositoryFullName != "codex-k8s/kodex" || provider.input.RepositoryTarget.ProviderRepositoryID != "R_123" {
		t.Fatalf("provider target = %+v, want binding-derived target", provider.input.RepositoryTarget)
	}
	if provider.input.BaseBranch != "main" || provider.input.BootstrapBranch != "kodex/bootstrap" || provider.input.ExternalAccountID != externalAccountID {
		t.Fatalf("provider input = %+v, want branch and account fields", provider.input)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != projectActionRepositoryBootstrap {
		t.Fatalf("access requests = %+v, want repository bootstrap check", authorizer.requests)
	}
	if result.ProviderResult.ProviderOperationID != "operation-1" || result.ProviderTarget.RepositoryFullName != "codex-k8s/kodex" {
		t.Fatalf("result = %+v, want provider refs and target", result)
	}
	if len(store.events) != 0 {
		t.Fatalf("events = %d, want no project event before services policy import", len(store.events))
	}
}

func TestCreateRepositoryBootstrapPullRequestValidatesProjectPolicyLink(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = entity.RepositoryBinding{
		Base:          entity.Base{ID: repositoryID, Version: 1},
		ProjectID:     projectID,
		Provider:      enum.RepositoryProviderGitHub,
		ProviderOwner: "codex-k8s",
		ProviderName:  "kodex",
		DefaultBranch: "main",
		Status:        enum.RepositoryStatusPending,
	}
	provider := &spyBootstrapProvider{}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{BootstrapProvider: provider})

	_, err := svc.CreateRepositoryBootstrapPullRequest(ctx, CreateRepositoryBootstrapPullRequestInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		BaseBranch:        "main",
		BootstrapBranch:   "kodex/bootstrap",
		CommitMessage:     "Bootstrap repository",
		Title:             "Bootstrap repository",
		Files:             []RepositoryBootstrapFile{{Path: "README.md", Content: "# Project\n"}},
		WatermarkJSON:     []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`),
		ServicesPolicy:    bootstrapServicesPolicy(),
		ExternalAccountID: uuid.New(),
		Meta:              commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateRepositoryBootstrapPullRequest() err = %v, want invalid argument", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want validation to stop before provider", provider.calls)
	}
}

func TestCreateRepositoryBootstrapPullRequestRejectsPolicyContentDrift(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = entity.RepositoryBinding{
		Base:          entity.Base{ID: repositoryID, Version: 1},
		ProjectID:     projectID,
		Provider:      enum.RepositoryProviderGitHub,
		ProviderOwner: "codex-k8s",
		ProviderName:  "kodex",
		DefaultBranch: "main",
		Status:        enum.RepositoryStatusPending,
	}

	for _, tc := range []struct {
		name     string
		content  string
		policy   RepositoryBootstrapServicesPolicy
		provider *spyBootstrapProvider
	}{
		{
			name:     "hash mismatch",
			content:  "spec:\n  services:\n    - key: api\n      rootPath: services/other\n      kind: backend\n",
			policy:   bootstrapServicesPolicy(),
			provider: &spyBootstrapProvider{},
		},
		{
			name:    "payload mismatch",
			content: "spec:\n  services:\n    - key: api\n      rootPath: services/other\n      kind: backend\n",
			policy: bootstrapServicesPolicyForContent(
				"spec:\n  services:\n    - key: api\n      rootPath: services/other\n      kind: backend\n",
				[]byte(bootstrapServicesPolicyPayload),
			),
			provider: &spyBootstrapProvider{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{BootstrapProvider: tc.provider})
			_, err := svc.CreateRepositoryBootstrapPullRequest(ctx, CreateRepositoryBootstrapPullRequestInput{
				ProjectID:         projectID,
				RepositoryID:      repositoryID,
				BaseBranch:        "main",
				BootstrapBranch:   "kodex/bootstrap",
				CommitMessage:     "Bootstrap repository",
				Title:             "Bootstrap repository",
				Files:             []RepositoryBootstrapFile{{Path: "services.yaml", Content: tc.content}},
				WatermarkJSON:     []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`),
				ServicesPolicy:    tc.policy,
				ExternalAccountID: uuid.New(),
				Meta:              commandMeta(uuid.New()),
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("CreateRepositoryBootstrapPullRequest() err = %v, want invalid argument", err)
			}
			if tc.provider.calls != 0 {
				t.Fatalf("provider calls = %d, want validation to stop before provider", tc.provider.calls)
			}
		})
	}
}

func TestImportBootstrapServicesPolicyActivatesBindingAndStoresCheckedProjection(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	authorizer := &spyAuthorizer{}
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{policyID, uuid.New(), uuid.New(), uuid.New()}},
		Config{Authorizer: authorizer},
	)

	result, err := svc.ImportBootstrapServicesPolicy(ctx, importBootstrapServicesPolicyInput(projectID, repositoryID, commandMetaWithVersion(uuid.New(), 3)))
	if err != nil {
		t.Fatalf("ImportBootstrapServicesPolicy(): %v", err)
	}
	if result.Repository.Status != enum.RepositoryStatusActive || result.Repository.Version != 4 {
		t.Fatalf("repository result = %+v, want active version 4", result.Repository)
	}
	if result.ServicesPolicy.ID != policyID ||
		result.ServicesPolicy.SourceRepositoryID == nil ||
		*result.ServicesPolicy.SourceRepositoryID != repositoryID ||
		result.ServicesPolicy.SourceRef != "refs/heads/main" ||
		result.ServicesPolicy.SourceCommitSHA != bootstrapMergeCommitSHA ||
		result.ServicesPolicy.ContentHash != bootstrapContentHash(bootstrapServicesPolicyFileContent) ||
		!bytes.Equal(result.ServicesPolicy.ValidatedPayload, []byte(bootstrapServicesPolicyPayload)) {
		t.Fatalf("services policy = %+v, want checked bootstrap projection", result.ServicesPolicy)
	}
	if result.Summary == "" || !strings.Contains(result.Summary, bootstrapMergeCommitSHA[:12]) {
		t.Fatalf("summary = %q, want short commit summary", result.Summary)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != projectActionPolicyImport {
		t.Fatalf("access requests = %+v, want policy import check", authorizer.requests)
	}
	if len(store.events) != 2 ||
		store.events[0].EventType != projectEventRepositoryUpdated ||
		store.events[1].EventType != projectEventServicesPolicyImported {
		t.Fatalf("events = %+v, want repository update and policy import", store.events)
	}
	var payload value.ProjectEventPayload
	if err := json.Unmarshal(store.events[1].Payload, &payload); err != nil {
		t.Fatalf("unmarshal policy event: %v", err)
	}
	if payload.RepositoryID != repositoryID.String() ||
		payload.SourceRef != "refs/heads/main" ||
		payload.SourceCommitSHA != bootstrapMergeCommitSHA ||
		payload.ProviderWorkItemProjectionID != "projection-1" ||
		payload.ProviderWebURL != "https://github.com/codex-k8s/kodex/pull/7" ||
		payload.Summary == "" {
		t.Fatalf("policy event payload = %+v, want safe refs and summary", payload)
	}
	if len(store.commandResults) != 1 {
		t.Fatalf("command results = %d, want one import result", len(store.commandResults))
	}
	for _, commandResult := range store.commandResults {
		for _, forbidden := range [][]byte{
			[]byte("validated_payload_json"),
			[]byte(bootstrapServicesPolicyPayload),
			[]byte("watermark_json"),
			[]byte("raw_provider_payload"),
			[]byte("files"),
		} {
			if bytes.Contains(commandResult.ResultPayload, forbidden) {
				t.Fatalf("command payload stores unsafe import data %q in %s", forbidden, commandResult.ResultPayload)
			}
		}
	}
}

func TestImportBootstrapServicesPolicyCommandReplayDoesNotCreateDuplicates(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	commandID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := importBootstrapServicesPolicyInput(projectID, repositoryID, commandMetaWithVersion(commandID, 3))

	first, err := svc.ImportBootstrapServicesPolicy(ctx, input)
	if err != nil {
		t.Fatalf("first ImportBootstrapServicesPolicy(): %v", err)
	}
	input.SourceCommitSHA = strings.Repeat("a", 40)
	second, err := svc.ImportBootstrapServicesPolicy(ctx, input)
	if err != nil {
		t.Fatalf("replay ImportBootstrapServicesPolicy(): %v", err)
	}
	if first.ServicesPolicy.ID != second.ServicesPolicy.ID || second.SourceCommitSHA != bootstrapMergeCommitSHA {
		t.Fatalf("replay result = %+v, want original policy", second)
	}
	if len(store.policies) != 1 || len(store.events) != 2 {
		t.Fatalf("policies=%d events=%d, want no duplicate writes", len(store.policies), len(store.events))
	}
}

func TestImportBootstrapServicesPolicySourceReplayDoesNotCreateDuplicates(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)

	first, err := svc.ImportBootstrapServicesPolicy(ctx, importBootstrapServicesPolicyInput(projectID, repositoryID, commandMetaWithVersion(uuid.New(), 3)))
	if err != nil {
		t.Fatalf("first ImportBootstrapServicesPolicy(): %v", err)
	}
	second, err := svc.ImportBootstrapServicesPolicy(ctx, importBootstrapServicesPolicyInput(projectID, repositoryID, commandMeta(uuid.New())))
	if err != nil {
		t.Fatalf("source replay ImportBootstrapServicesPolicy(): %v", err)
	}
	if first.ServicesPolicy.ID != second.ServicesPolicy.ID || second.Repository.Status != enum.RepositoryStatusActive {
		t.Fatalf("source replay = %+v, want existing active import", second)
	}
	if len(store.policies) != 1 || len(store.commandResults) != 1 || len(store.events) != 2 {
		t.Fatalf("policies=%d commandResults=%d events=%d, want no duplicate source import", len(store.policies), len(store.commandResults), len(store.events))
	}
}

func TestImportBootstrapServicesPolicyConflictsOnDifferentSourceAfterActivation(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	if _, err := svc.ImportBootstrapServicesPolicy(ctx, importBootstrapServicesPolicyInput(projectID, repositoryID, commandMetaWithVersion(uuid.New(), 3))); err != nil {
		t.Fatalf("initial ImportBootstrapServicesPolicy(): %v", err)
	}

	changedCommit := importBootstrapServicesPolicyInput(projectID, repositoryID, commandMeta(uuid.New()))
	changedCommit.SourceCommitSHA = strings.Repeat("a", 40)
	if _, err := svc.ImportBootstrapServicesPolicy(ctx, changedCommit); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed commit err = %v, want conflict", err)
	}
	changedRef := importBootstrapServicesPolicyInput(projectID, repositoryID, commandMeta(uuid.New()))
	changedRef.SourceRef = "main"
	if _, err := svc.ImportBootstrapServicesPolicy(ctx, changedRef); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed source ref err = %v, want conflict", err)
	}
	if len(store.policies) != 1 || len(store.events) != 2 {
		t.Fatalf("policies=%d events=%d, want no writes on conflict", len(store.policies), len(store.events))
	}
}

func TestImportBootstrapServicesPolicyRejectsProviderTargetMismatch(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{})
	input := importBootstrapServicesPolicyInput(projectID, repositoryID, commandMetaWithVersion(uuid.New(), 3))
	input.ProviderTarget.RepositoryFullName = "codex-k8s/other"

	_, err := svc.ImportBootstrapServicesPolicy(ctx, input)
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ImportBootstrapServicesPolicy() err = %v, want precondition failed", err)
	}
	if len(store.policies) != 0 || len(store.events) != 0 || len(store.commandResults) != 0 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want no writes", len(store.policies), len(store.events), len(store.commandResults))
	}
}

func TestReconcileBootstrapMergeSignalImportsCheckedPolicy(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), policyID, uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))

	result, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("ReconcileBootstrapMergeSignal(): %v", err)
	}
	if result.Repository.Status != enum.RepositoryStatusActive || result.ServicesPolicy.ID != policyID {
		t.Fatalf("result = %+v, want active repository and checked policy", result)
	}
	if result.ServicesPolicy.SourceRef != "refs/heads/main" || result.SourceRef != "refs/heads/main" {
		t.Fatalf("source ref = policy:%q result:%q, want merged base branch", result.ServicesPolicy.SourceRef, result.SourceRef)
	}
	if len(store.policies) != 1 || len(store.events) != 2 || len(store.commandResults) != 1 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want single import", len(store.policies), len(store.events), len(store.commandResults))
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want one status record", len(store.onboardingSignals))
	}
	for _, signal := range store.onboardingSignals {
		if signal.Status != enum.OnboardingSignalStatusImported || signal.ServicesPolicyID == nil || *signal.ServicesPolicyID != policyID {
			t.Fatalf("onboarding signal = %+v, want imported policy status", signal)
		}
		if signal.ArtifactRef == "" || signal.ArtifactDigest == "" || signal.ContentHash == "" || string(signal.SignalKind) != "bootstrap_merge" {
			t.Fatalf("onboarding signal = %+v, want safe bootstrap refs and digests", signal)
		}
		for _, forbidden := range []string{bootstrapServicesPolicyPayload, "watermark_json", "raw_provider_payload", "diff"} {
			if strings.Contains(signal.Summary+signal.ErrorSummary+signal.ArtifactRef, forbidden) {
				t.Fatalf("onboarding signal stores unsafe data %q in %+v", forbidden, signal)
			}
		}
	}
	for key, commandResult := range store.commandResults {
		if !strings.Contains(key, bootstrapMergeIdempotencyPrefix+input.MergeSignal.SignalKey) {
			t.Fatalf("command key = %q, want derived signal idempotency key", key)
		}
		for _, forbidden := range [][]byte{
			[]byte("validated_payload_json"),
			[]byte(bootstrapServicesPolicyPayload),
			[]byte("watermark_json"),
			[]byte("raw_provider_payload"),
			[]byte("diff"),
		} {
			if bytes.Contains(commandResult.ResultPayload, forbidden) {
				t.Fatalf("command payload stores unsafe reconciliation data %q in %s", forbidden, commandResult.ResultPayload)
			}
		}
	}
}

func TestReconcileBootstrapMergeSignalReplayDoesNotDuplicateImport(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))

	first, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("first ReconcileBootstrapMergeSignal(): %v", err)
	}
	second, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("replay ReconcileBootstrapMergeSignal(): %v", err)
	}
	if first.ServicesPolicy.ID != second.ServicesPolicy.ID || second.Repository.Status != enum.RepositoryStatusActive {
		t.Fatalf("replay result = %+v, want existing import", second)
	}
	if len(store.policies) != 1 || len(store.commandResults) != 1 || len(store.events) != 2 {
		t.Fatalf("policies=%d commandResults=%d events=%d, want no duplicate writes", len(store.policies), len(store.commandResults), len(store.events))
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want one idempotent status record", len(store.onboardingSignals))
	}
	for _, signal := range store.onboardingSignals {
		if signal.Status != enum.OnboardingSignalStatusImported || signal.ServicesPolicyVersion != 1 {
			t.Fatalf("onboarding signal = %+v, want imported replay status", signal)
		}
	}
}

func TestRecordBootstrapMergeSignalDiagnosticIsIdempotent(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		Config{},
	)
	input := bootstrapMergeSignalDiagnosticInput(projectID, repositoryID, "sha256:"+strings.Repeat("1", 64))

	if err := svc.RecordBootstrapMergeSignalDiagnostic(ctx, input); err != nil {
		t.Fatalf("RecordBootstrapMergeSignalDiagnostic(): %v", err)
	}
	if err := svc.RecordBootstrapMergeSignalDiagnostic(ctx, input); err != nil {
		t.Fatalf("replay RecordBootstrapMergeSignalDiagnostic(): %v", err)
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want one idempotent diagnostic", len(store.onboardingSignals))
	}
	for _, signal := range store.onboardingSignals {
		if signal.Status != enum.OnboardingSignalStatusNeedsReview || signal.ErrorCode != "missing_checked_artifact" {
			t.Fatalf("signal = %+v, want needs_review missing_checked_artifact", signal)
		}
		if signal.ServicesPolicyID != nil || signal.ArtifactRef != "" || signal.ContentHash != "" {
			t.Fatalf("signal = %+v, want diagnostic without imported policy/artifact payload", signal)
		}
		for _, forbidden := range []string{bootstrapServicesPolicyPayload, "raw_provider_payload", "diff", "watermark_json"} {
			if strings.Contains(signal.Summary+signal.ErrorSummary+signal.ArtifactRef, forbidden) {
				t.Fatalf("diagnostic stores unsafe data %q in %+v", forbidden, signal)
			}
		}
	}
}

func TestRecordBootstrapMergeSignalDiagnosticRejectsConflictingReplay(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		Config{},
	)
	input := bootstrapMergeSignalDiagnosticInput(projectID, repositoryID, "sha256:"+strings.Repeat("1", 64))
	if err := svc.RecordBootstrapMergeSignalDiagnostic(ctx, input); err != nil {
		t.Fatalf("RecordBootstrapMergeSignalDiagnostic(): %v", err)
	}
	input.SignalFingerprint = "sha256:" + strings.Repeat("2", 64)

	if err := svc.RecordBootstrapMergeSignalDiagnostic(ctx, input); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting RecordBootstrapMergeSignalDiagnostic() err = %v, want conflict", err)
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want no duplicate diagnostic", len(store.onboardingSignals))
	}
}

func TestRecordBootstrapMergeSignalDiagnosticChecksRepositoryBinding(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}}, Config{})
	input := bootstrapMergeSignalDiagnosticInput(projectID, repositoryID, "sha256:"+strings.Repeat("1", 64))
	input.MergeSignal.ProviderTarget.RepositoryFullName = "codex-k8s/other"

	if err := svc.RecordBootstrapMergeSignalDiagnostic(ctx, input); !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("RecordBootstrapMergeSignalDiagnostic() err = %v, want precondition failed", err)
	}
	if len(store.onboardingSignals) != 0 {
		t.Fatalf("onboarding signals = %d, want no write for mismatched provider refs", len(store.onboardingSignals))
	}
}

func TestReconcileBootstrapMergeSignalReplayStillChecksAccess(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	authorizer := &spyAuthorizer{}
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{Authorizer: authorizer},
	)
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))

	if _, err := svc.ReconcileBootstrapMergeSignal(ctx, input); err != nil {
		t.Fatalf("initial ReconcileBootstrapMergeSignal(): %v", err)
	}
	var signalBefore entity.OnboardingSignalReconciliation
	for _, signal := range store.onboardingSignals {
		signalBefore = signal
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != projectActionPolicyImport {
		t.Fatalf("access requests = %+v, want initial policy import check", authorizer.requests)
	}

	authorizer.err = errs.ErrForbidden
	_, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("replay err = %v, want forbidden", err)
	}
	if len(authorizer.requests) != 2 {
		t.Fatalf("access checks = %d, want 2 including replay", len(authorizer.requests))
	}
	if len(store.policies) != 1 || len(store.commandResults) != 1 || len(store.events) != 2 {
		t.Fatalf("policies=%d commandResults=%d events=%d, want no replay writes after denied access", len(store.policies), len(store.commandResults), len(store.events))
	}
	for _, signal := range store.onboardingSignals {
		if signal.Version != signalBefore.Version || signal.Status != signalBefore.Status {
			t.Fatalf("onboarding signal changed after denied replay: before=%+v after=%+v", signalBefore, signal)
		}
	}
}

func TestReconcileBootstrapMergeSignalRejectsConflictingReplay(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))
	if _, err := svc.ReconcileBootstrapMergeSignal(ctx, input); err != nil {
		t.Fatalf("initial ReconcileBootstrapMergeSignal(): %v", err)
	}

	conflicting := input
	conflicting.MergeSignal.MergeCommitSHA = strings.Repeat("a", 40)
	conflicting.CheckedPolicy.ArtifactVersion = strings.Repeat("a", 40)
	_, err := svc.ReconcileBootstrapMergeSignal(ctx, conflicting)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting ReconcileBootstrapMergeSignal() err = %v, want conflict", err)
	}
	if len(store.policies) != 1 || len(store.commandResults) != 1 || len(store.events) != 2 {
		t.Fatalf("policies=%d commandResults=%d events=%d, want no conflicting replay writes", len(store.policies), len(store.commandResults), len(store.events))
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want existing status only", len(store.onboardingSignals))
	}
}

func TestReconcileBootstrapMergeSignalRejectsStaleArtifact(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{})
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))
	input.CheckedPolicy.ArtifactVersion = strings.Repeat("a", 40)

	_, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ReconcileBootstrapMergeSignal() err = %v, want precondition failed", err)
	}
	if len(store.policies) != 0 || len(store.events) != 0 || len(store.commandResults) != 0 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want no writes", len(store.policies), len(store.events), len(store.commandResults))
	}
}

func TestReconcileBootstrapMergeSignalStoresSafeFailureStatus(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))
	input.MergeSignal.ProviderTarget.RepositoryFullName = "codex-k8s/other"

	_, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ReconcileBootstrapMergeSignal() err = %v, want precondition failed", err)
	}
	if len(store.policies) != 0 || len(store.events) != 0 || len(store.commandResults) != 0 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want no import writes", len(store.policies), len(store.events), len(store.commandResults))
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want failed status record", len(store.onboardingSignals))
	}
	for _, signal := range store.onboardingSignals {
		if signal.Status != enum.OnboardingSignalStatusFailed || signal.ErrorCode != "failed_precondition" {
			t.Fatalf("onboarding signal = %+v, want safe failed status", signal)
		}
		for _, forbidden := range []string{bootstrapServicesPolicyPayload, "raw_provider_payload", "diff", "watermark_json"} {
			if strings.Contains(signal.ErrorSummary+signal.Summary, forbidden) {
				t.Fatalf("onboarding signal failure stores unsafe data %q in %+v", forbidden, signal)
			}
		}
	}
}

func TestReconcileBootstrapMergeSignalRejectsUnsafeJournalMetadata(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()

	for _, tc := range []struct {
		name   string
		mutate func(*ReconcileBootstrapMergeSignalInput)
	}{
		{
			name: "oversized signal key",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.MergeSignal.SignalKey = strings.Repeat("x", maxOnboardingSignalKeyLength+1)
			},
		},
		{
			name: "control char repository full name",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.MergeSignal.ProviderTarget.RepositoryFullName = "codex-k8s/kodex\nraw"
			},
		},
		{
			name: "oversized artifact ref",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.CheckedPolicy.ArtifactRef = "artifact://" + strings.Repeat("x", maxOnboardingArtifactRefLength+1)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryRepository()
			store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
			svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{})
			input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))
			tc.mutate(&input)

			_, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("ReconcileBootstrapMergeSignal() err = %v, want invalid argument", err)
			}
			if len(store.onboardingSignals) != 0 || len(store.policies) != 0 || len(store.events) != 0 || len(store.commandResults) != 0 {
				t.Fatalf(
					"onboardingSignals=%d policies=%d events=%d commandResults=%d, want no writes",
					len(store.onboardingSignals),
					len(store.policies),
					len(store.events),
					len(store.commandResults),
				)
			}
		})
	}
}

func TestNormalizeOnboardingSignalReconciliationRejectsUnsafeMetadata(t *testing.T) {
	valid := OnboardingSignalReconciliationInput{
		ProjectID:            uuid.New(),
		RepositoryID:         uuid.New(),
		SignalKind:           enum.OnboardingSignalKindBootstrapMerge,
		SignalKey:            "github/bootstrap/PR_123",
		SignalFingerprint:    "sha256:" + strings.Repeat("a", 64),
		ProviderSlug:         "github",
		RepositoryFullName:   "codex-k8s/kodex",
		ProviderRepositoryID: "R_123",
		BaseBranch:           "main",
		SourceRef:            "refs/heads/main",
		SourceCommitSHA:      bootstrapMergeCommitSHA,
		ArtifactRef:          "artifact://provider/bootstrap/PR_123/services.yaml",
		ArtifactDigest:       bootstrapContentHash(bootstrapServicesPolicyFileContent),
		ArtifactVersion:      bootstrapMergeCommitSHA,
		ContentHash:          bootstrapContentHash(bootstrapServicesPolicyFileContent),
	}

	for _, tc := range []struct {
		name   string
		mutate func(*OnboardingSignalReconciliationInput)
	}{
		{
			name: "control char signal key",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.SignalKey = "github/bootstrap/PR_123\nraw"
			},
		},
		{
			name: "invalid fingerprint",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.SignalFingerprint = "raw-fingerprint"
			},
		},
		{
			name: "control char provider repository id",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.ProviderRepositoryID = "R_123\nraw"
			},
		},
		{
			name: "unsafe base branch",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.BaseBranch = "main branch"
			},
		},
		{
			name: "control char artifact version",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.ArtifactVersion = bootstrapMergeCommitSHA + "\nraw"
			},
		},
		{
			name: "invalid artifact digest",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.ArtifactDigest = "sha256:" + strings.Repeat("z", 64)
			},
		},
		{
			name: "invalid content hash",
			mutate: func(input *OnboardingSignalReconciliationInput) {
				input.ContentHash = "raw-content-hash"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.mutate(&input)
			if _, err := normalizeOnboardingSignalReconciliationInput(input); !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("normalizeOnboardingSignalReconciliationInput() err = %v, want invalid argument", err)
			}
		})
	}
}

func TestReconcileBootstrapMergeSignalRejectsMismatchedSignal(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{})

	for _, tc := range []struct {
		name   string
		mutate func(*ReconcileBootstrapMergeSignalInput)
		err    error
	}{
		{
			name: "adoption signal",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.MergeSignal.SignalKind = "adoption"
			},
			err: errs.ErrInvalidArgument,
		},
		{
			name: "watermark digest mismatch",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.MergeSignal.WatermarkDigest = strings.Repeat("b", 64)
			},
			err: errs.ErrPreconditionFailed,
		},
		{
			name: "artifact digest mismatch",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.CheckedPolicy.ArtifactDigest = "sha256:" + strings.Repeat("c", 64)
			},
			err: errs.ErrPreconditionFailed,
		},
		{
			name: "missing provider source ref",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.MergeSignal.SourceRef = ""
			},
			err: errs.ErrInvalidArgument,
		},
		{
			name: "provider target mismatch",
			mutate: func(input *ReconcileBootstrapMergeSignalInput) {
				input.MergeSignal.ProviderTarget.RepositoryFullName = "codex-k8s/other"
			},
			err: errs.ErrPreconditionFailed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, commandMetaWithVersion(uuid.Nil, 3))
			tc.mutate(&input)
			_, err := svc.ReconcileBootstrapMergeSignal(ctx, input)
			if !errors.Is(err, tc.err) {
				t.Fatalf("ReconcileBootstrapMergeSignal() err = %v, want %v", err, tc.err)
			}
			if len(store.policies) != 0 || len(store.events) != 0 || len(store.commandResults) != 0 {
				t.Fatalf("policies=%d events=%d commandResults=%d, want no writes", len(store.policies), len(store.events), len(store.commandResults))
			}
		})
	}
}

func TestReconcileAdoptionMergeSignalImportsCheckedPolicyForPendingRepository(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), policyID, uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileAdoptionMergeSignalInput(projectID, repositoryID, commandMeta(uuid.Nil))

	result, err := svc.ReconcileAdoptionMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("ReconcileAdoptionMergeSignal(): %v", err)
	}
	if result.Repository.Status != enum.RepositoryStatusActive || result.ServicesPolicy.ID != policyID {
		t.Fatalf("result = %+v, want active repository and checked adoption policy", result)
	}
	if result.ServicesPolicy.SourceCommitSHA != bootstrapMergeCommitSHA || result.ServicesPolicy.ContentHash != bootstrapContentHash(bootstrapServicesPolicyFileContent) {
		t.Fatalf("services policy = %+v, want checked adoption projection", result.ServicesPolicy)
	}
	if len(store.policies) != 1 || len(store.events) != 2 || len(store.commandResults) != 1 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want single adoption import", len(store.policies), len(store.events), len(store.commandResults))
	}
	for _, signal := range store.onboardingSignals {
		if signal.Status != enum.OnboardingSignalStatusImported || signal.ServicesPolicyID == nil || *signal.ServicesPolicyID != policyID {
			t.Fatalf("onboarding signal = %+v, want imported adoption policy status", signal)
		}
		if signal.SignalKind != enum.OnboardingSignalKindAdoptionMerge || signal.ArtifactRef == "" || signal.ContentHash == "" {
			t.Fatalf("onboarding signal = %+v, want safe adoption refs and digests", signal)
		}
	}
	for key, commandResult := range store.commandResults {
		if !strings.Contains(key, adoptionMergeIdempotencyPrefix+input.MergeSignal.SignalKey) {
			t.Fatalf("command key = %q, want derived adoption idempotency key", key)
		}
		for _, forbidden := range [][]byte{
			[]byte("validated_payload_json"),
			[]byte(bootstrapServicesPolicyPayload),
			[]byte("watermark_json"),
			[]byte("raw_provider_payload"),
			[]byte("diff"),
		} {
			if bytes.Contains(commandResult.ResultPayload, forbidden) {
				t.Fatalf("command payload stores unsafe adoption reconciliation data %q in %s", forbidden, commandResult.ResultPayload)
			}
		}
	}
}

func TestReconcileAdoptionMergeSignalUpdatesActiveRepositoryPolicy(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{
			uuid.New(), uuid.New(), uuid.New(), uuid.New(),
			uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		}},
		Config{},
	)
	if _, err := svc.ImportBootstrapServicesPolicy(ctx, importBootstrapServicesPolicyInput(projectID, repositoryID, commandMetaWithVersion(uuid.New(), 3))); err != nil {
		t.Fatalf("initial ImportBootstrapServicesPolicy(): %v", err)
	}
	input := reconcileAdoptionMergeSignalInput(projectID, repositoryID, commandMeta(uuid.Nil))
	input.MergeSignal.MergeCommitSHA = strings.Repeat("a", 40)
	input.CheckedPolicy.ArtifactVersion = strings.Repeat("a", 40)

	result, err := svc.ReconcileAdoptionMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("ReconcileAdoptionMergeSignal(): %v", err)
	}
	if result.Repository.Status != enum.RepositoryStatusActive || result.Repository.Version != 5 {
		t.Fatalf("repository = %+v, want active version 5", result.Repository)
	}
	if result.ServicesPolicy.PolicyVersion != 2 || result.ServicesPolicy.SourceCommitSHA != strings.Repeat("a", 40) {
		t.Fatalf("services policy = %+v, want new checked adoption policy version", result.ServicesPolicy)
	}
	if len(store.policies) != 2 || len(store.events) != 4 || len(store.commandResults) != 2 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want bootstrap plus adoption import", len(store.policies), len(store.events), len(store.commandResults))
	}
}

func TestReconcileAdoptionMergeSignalReplayDoesNotDuplicateImport(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileAdoptionMergeSignalInput(projectID, repositoryID, commandMeta(uuid.Nil))

	first, err := svc.ReconcileAdoptionMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("first ReconcileAdoptionMergeSignal(): %v", err)
	}
	second, err := svc.ReconcileAdoptionMergeSignal(ctx, input)
	if err != nil {
		t.Fatalf("replay ReconcileAdoptionMergeSignal(): %v", err)
	}
	if first.ServicesPolicy.ID != second.ServicesPolicy.ID || second.Repository.Status != enum.RepositoryStatusActive {
		t.Fatalf("replay result = %+v, want existing adoption import", second)
	}
	if len(store.policies) != 1 || len(store.commandResults) != 1 || len(store.events) != 2 || len(store.onboardingSignals) != 1 {
		t.Fatalf("policies=%d commandResults=%d events=%d signals=%d, want no duplicate adoption writes", len(store.policies), len(store.commandResults), len(store.events), len(store.onboardingSignals))
	}
}

func TestRecordAdoptionMergeSignalDiagnosticIsIdempotent(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New()}}, Config{})
	input := adoptionMergeSignalDiagnosticInput(projectID, repositoryID, "sha256:"+strings.Repeat("1", 64))

	if err := svc.RecordAdoptionMergeSignalDiagnostic(ctx, input); err != nil {
		t.Fatalf("RecordAdoptionMergeSignalDiagnostic(): %v", err)
	}
	if err := svc.RecordAdoptionMergeSignalDiagnostic(ctx, input); err != nil {
		t.Fatalf("replay RecordAdoptionMergeSignalDiagnostic(): %v", err)
	}
	if len(store.onboardingSignals) != 1 {
		t.Fatalf("onboarding signals = %d, want one idempotent adoption diagnostic", len(store.onboardingSignals))
	}
	for _, signal := range store.onboardingSignals {
		if signal.SignalKind != enum.OnboardingSignalKindAdoptionMerge || signal.Status != enum.OnboardingSignalStatusNeedsReview || signal.ErrorCode != "missing_checked_artifact" {
			t.Fatalf("signal = %+v, want adoption needs_review missing_checked_artifact", signal)
		}
		if signal.ServicesPolicyID != nil || signal.ArtifactRef != "" || signal.ContentHash != "" {
			t.Fatalf("signal = %+v, want diagnostic without imported policy/artifact payload", signal)
		}
	}
}

func TestReconcileAdoptionMergeSignalRejectsConflictingReplay(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(
		store,
		fixedClock{},
		&sequenceIDs{ids: []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}},
		Config{},
	)
	input := reconcileAdoptionMergeSignalInput(projectID, repositoryID, commandMeta(uuid.Nil))
	if _, err := svc.ReconcileAdoptionMergeSignal(ctx, input); err != nil {
		t.Fatalf("initial ReconcileAdoptionMergeSignal(): %v", err)
	}

	conflicting := input
	conflicting.MergeSignal.MergeCommitSHA = strings.Repeat("a", 40)
	conflicting.CheckedPolicy.ArtifactVersion = strings.Repeat("a", 40)
	_, err := svc.ReconcileAdoptionMergeSignal(ctx, conflicting)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting ReconcileAdoptionMergeSignal() err = %v, want conflict", err)
	}
	if len(store.policies) != 1 || len(store.commandResults) != 1 || len(store.events) != 2 || len(store.onboardingSignals) != 1 {
		t.Fatalf("policies=%d commandResults=%d events=%d signals=%d, want no conflicting adoption writes", len(store.policies), len(store.commandResults), len(store.events), len(store.onboardingSignals))
	}
}

func TestReconcileAdoptionMergeSignalRejectsMismatchedWatermark(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = pendingBootstrapRepository(projectID, repositoryID)
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{})
	input := reconcileAdoptionMergeSignalInput(projectID, repositoryID, commandMeta(uuid.Nil))
	input.MergeSignal.WatermarkJSON = []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`)
	input.MergeSignal.WatermarkDigest = sha256HexDigest(input.MergeSignal.WatermarkJSON)

	_, err := svc.ReconcileAdoptionMergeSignal(ctx, input)
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ReconcileAdoptionMergeSignal() err = %v, want invalid argument", err)
	}
	if len(store.policies) != 0 || len(store.events) != 0 || len(store.commandResults) != 0 {
		t.Fatalf("policies=%d events=%d commandResults=%d, want no writes", len(store.policies), len(store.events), len(store.commandResults))
	}
}

func TestGetSelfDeploySignalReturnsReadyProjectSideInput(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	now := fixedClock{}.Now()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	store.policies[policyID] = activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, []byte(`{"secret":"must-not-leak"}`))
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "worker"),
	}
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{
			Status: ProviderOwnedDataStatusReady,
			Signal: RepositoryChangeSignal{
				SignalID:              "11111111-1111-1111-1111-111111111111",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA,
				Kind:                  "push",
				ProviderSlug:          "github",
				ProjectID:             projectID.String(),
				RepositoryID:          repositoryID.String(),
				RepositoryFullName:    "codex-k8s/kodex",
				ProviderRepositoryID:  "R_123",
				Ref:                   "refs/heads/main",
				BaseBranch:            "main",
				CommitSHA:             commitSHA,
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				PathDigest:            "sha256:path-digest-must-not-be-policy",
				PathCategories:        []RepositoryChangePathCategoryCount{{Category: enum.SelfDeployPathCategoryServiceSource, Count: 3}},
				ServicesPolicyChanged: true,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-change",
				ObservedAt:            now.Format(time.RFC3339Nano),
				Status:                "observed",
				Version:               2,
				ETag:                  "etag-1",
			},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(ctx, GetSelfDeploySignalInput{
		ProjectID:        projectID,
		RepositoryID:     &repositoryID,
		ProviderSignalID: "11111111-1111-1111-1111-111111111111",
		Meta:             queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusReady {
		t.Fatalf("status = %s, want ready, reason=%s", result.Status, result.SafeReason)
	}
	if result.Signal.ServicesYaml.ServicesYamlDigest != "sha256:services-policy" ||
		result.Signal.ServicesYaml.ServicesYamlDigest == "sha256:path-digest-must-not-be-policy" {
		t.Fatalf("services yaml digest = %q, want checked policy digest only", result.Signal.ServicesYaml.ServicesYamlDigest)
	}
	if got := strings.Join(result.Signal.AffectedServiceKeys, ","); got != "api,worker" {
		t.Fatalf("affected keys = %q, want api,worker", got)
	}
	if len(result.Signal.ExpectedRuntimeJobTypes) != 3 || !result.Signal.GovernanceRequirement.GateRequired {
		t.Fatalf("runtime/governance = %+v/%+v, want build deploy health_check and gate", result.Signal.ExpectedRuntimeJobTypes, result.Signal.GovernanceRequirement)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "validated_payload") {
		t.Fatalf("self-deploy signal leaked checked payload: %s", encoded)
	}
}

func TestGetSelfDeploySignalAllowsCodeOnlyPolicyCommitBehindSignal(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	policyCommit := "1111111111111111111111111111111111111111"
	codeCommit := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	store.policies[policyID] = activeSelfDeployPolicy(projectID, repositoryID, policyID, policyCommit, []byte(`{"spec":{"services":[{"key":"api"}]}}`))
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{
			Status: ProviderOwnedDataStatusReady,
			Signal: RepositoryChangeSignal{
				SignalID:              "signal-code-only",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:" + codeCommit,
				Kind:                  "push",
				ProviderSlug:          "github",
				RepositoryFullName:    "codex-k8s/kodex",
				ProviderRepositoryID:  "R_123",
				Ref:                   "refs/heads/main",
				BaseBranch:            "main",
				CommitSHA:             codeCommit,
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				PathCategories:        []RepositoryChangePathCategoryCount{{Category: enum.SelfDeployPathCategoryServiceSource, Count: 2}},
				ServicesPolicyChanged: false,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-code-only",
				Status:                "observed",
			},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(ctx, GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: "provider:github:repository_change:push:codex-k8s/kodex:main:" + codeCommit,
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusReady {
		t.Fatalf("status = %s, reason=%s, want ready for code-only change", result.Status, result.SafeReason)
	}
	if result.Signal.MergeCommitSHA != codeCommit ||
		result.Signal.ServicesYaml.SourceCommitSHA != policyCommit ||
		result.Signal.ServicesYamlChanged {
		t.Fatalf("signal/policy commits = %+v, want code commit with older checked policy commit", result.Signal)
	}
}

func TestGetSelfDeploySignalResolvesUnboundProviderSignalThroughExplicitBinding(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	signalKey := "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA
	store := newMemoryRepository()
	repository := activeSelfDeployRepository(projectID, repositoryID)
	repository.ProviderOwner = "Codex-K8S"
	repository.ProviderName = "Kodex"
	store.repositories[repositoryID] = repository
	store.policies[policyID] = activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, []byte(`{"spec":{"services":[{"key":"api"}]}}`))
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{Status: ProviderOwnedDataStatusNotFound},
		listResult: RepositoryChangeSignalListResult{
			Signals: []RepositoryChangeSignal{{
				SignalID:              "signal-live",
				SignalKey:             signalKey,
				Kind:                  "push",
				ProviderSlug:          "github",
				RepositoryFullName:    "codex-k8s/kodex",
				ProviderRepositoryID:  "R_123",
				Ref:                   "refs/heads/main",
				BaseBranch:            "main",
				CommitSHA:             commitSHA,
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				PathCategories:        []RepositoryChangePathCategoryCount{{Category: enum.SelfDeployPathCategoryServicesPolicy, Count: 1}},
				ServicesPolicyChanged: true,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-change",
				Status:                "observed",
				Version:               1,
				ETag:                  "etag-live",
			}},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(ctx, GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: signalKey,
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusReady {
		t.Fatalf("status = %s, reason=%s, want ready", result.Status, result.SafeReason)
	}
	if result.Signal.ProjectRef != projectID.String() || result.Signal.RepositoryRef != repositoryID.String() {
		t.Fatalf("signal refs = %s/%s, want explicit project/repository", result.Signal.ProjectRef, result.Signal.RepositoryRef)
	}
	if reader.listCalls != 1 {
		t.Fatalf("list calls = %d, want fallback list call", reader.listCalls)
	}
	if reader.listInput.ProviderSlug != "github" ||
		reader.listInput.RepositoryFullName != "codex-k8s/kodex" ||
		reader.listInput.ProviderRepositoryID != "R_123" ||
		reader.listInput.BaseBranch != "main" ||
		reader.listInput.CommitSHA != commitSHA ||
		reader.listInput.Page.PageSize != selfDeploySignalLookupLimit {
		t.Fatalf("list input = %+v, want canonical binding/commit lookup", reader.listInput)
	}
}

func TestGetSelfDeploySignalDoesNotResolveFallbackForOtherRepository(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	signalKey := "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{Status: ProviderOwnedDataStatusNotFound},
		listResult: RepositoryChangeSignalListResult{
			Signals: []RepositoryChangeSignal{{
				SignalID:              "signal-other",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/other:main:" + commitSHA,
				Kind:                  "push",
				ProviderSlug:          "github",
				RepositoryFullName:    "codex-k8s/other",
				ProviderRepositoryID:  "R_other",
				BaseBranch:            "main",
				CommitSHA:             commitSHA,
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:other",
				Status:                "observed",
			}},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(context.Background(), GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: signalKey,
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusProviderSignalNotFound || result.SafeReason != "provider_signal_not_found" {
		t.Fatalf("result = %+v, want provider signal not found for non-matching repo", result)
	}
}

func TestGetSelfDeploySignalReportsMissingProviderIdentityForFallback(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{Status: ProviderOwnedDataStatusNotFound},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(context.Background(), GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: "provider:github:repository_change:push",
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusRepositoryBindingNotFound || result.SafeReason != "provider_signal_identity_unavailable" {
		t.Fatalf("result = %+v, want missing provider identity safe reason", result)
	}
	if reader.listCalls != 0 {
		t.Fatalf("list calls = %d, want no list call without provider identity", reader.listCalls)
	}
}

func TestGetSelfDeploySignalBlocksProviderSourceBindingMismatch(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*RepositoryChangeSignal)
		reason string
	}{
		{
			name: "provider slug mismatch",
			mutate: func(signal *RepositoryChangeSignal) {
				signal.ProviderSlug = "gitlab"
			},
			reason: "provider_signal_provider_mismatch",
		},
		{
			name: "repository full name missing",
			mutate: func(signal *RepositoryChangeSignal) {
				signal.RepositoryFullName = ""
			},
			reason: "provider_signal_repository_ref_missing",
		},
		{
			name: "repository full name mismatch",
			mutate: func(signal *RepositoryChangeSignal) {
				signal.RepositoryFullName = "codex-k8s/other"
			},
			reason: "provider_signal_repository_ref_mismatch",
		},
		{
			name: "provider repository id mismatch",
			mutate: func(signal *RepositoryChangeSignal) {
				signal.ProviderRepositoryID = "R_other"
			},
			reason: "provider_signal_provider_repository_mismatch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectID := uuid.New()
			repositoryID := uuid.New()
			policyID := uuid.New()
			commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
			store := newMemoryRepository()
			store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
			store.policies[policyID] = activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, []byte(`{"spec":{"services":[{"key":"api"}]}}`))
			store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
				activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
			}
			signal := RepositoryChangeSignal{
				SignalID:              "signal-1",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA,
				Kind:                  "push",
				ProviderSlug:          "github",
				RepositoryFullName:    "codex-k8s/kodex",
				ProviderRepositoryID:  "R_123",
				Ref:                   "refs/heads/main",
				BaseBranch:            "main",
				CommitSHA:             commitSHA,
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				PathCategories:        []RepositoryChangePathCategoryCount{{Category: enum.SelfDeployPathCategoryServiceSource, Count: 1}},
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-change",
			}
			tc.mutate(&signal)
			reader := &fakeRepositoryChangeSignalReader{
				result: RepositoryChangeSignalReadResult{
					Status: ProviderOwnedDataStatusReady,
					Signal: signal,
				},
			}
			svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

			result, err := svc.GetSelfDeploySignal(ctx, GetSelfDeploySignalInput{
				ProjectID:         projectID,
				RepositoryID:      &repositoryID,
				ProviderSignalKey: signal.SignalKey,
				Meta:              queryMeta(),
			})
			if err != nil {
				t.Fatalf("GetSelfDeploySignal(): %v", err)
			}
			if result.Status != enum.SelfDeploySignalStatusRepositoryBindingNotFound || result.SafeReason != tc.reason {
				t.Fatalf("result = %+v, want repository binding blocked with reason %q", result, tc.reason)
			}
		})
	}
}

func TestGetSelfDeploySignalReturnsSafeStatusWhenServicesPolicyMissing(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.wrapNotFound = true
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{
			Status: ProviderOwnedDataStatusReady,
			Signal: RepositoryChangeSignal{
				SignalID:              "signal-1",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
				Kind:                  "push",
				ProviderSlug:          "github",
				RepositoryFullName:    "codex-k8s/kodex",
				ProviderRepositoryID:  "R_123",
				BaseBranch:            "main",
				CommitSHA:             "abcdef0123456789abcdef0123456789abcdef01",
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-change",
			},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(context.Background(), GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusServicesPolicyNotFound || result.SafeReason != "services_policy_not_found" {
		t.Fatalf("result = %+v, want wrapped services policy not found safe status", result)
	}
}

func TestGetSelfDeploySignalReturnsSafeStatusWhenProviderIdentityMissing(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{
			Status: ProviderOwnedDataStatusReady,
			Signal: RepositoryChangeSignal{
				SignalID:              "signal-1",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
				Kind:                  "push",
				ProviderSlug:          "github",
				BaseBranch:            "main",
				CommitSHA:             "abcdef0123456789abcdef0123456789abcdef01",
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-change",
			},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(context.Background(), GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusRepositoryBindingNotFound || result.SafeReason != "provider_signal_repository_ref_missing" {
		t.Fatalf("result = %+v, want missing provider identity safe reason", result)
	}
}

func TestGetSelfDeploySignalDoesNotMaskPermissionDeniedAsNotFound(t *testing.T) {
	projectID := uuid.New()
	reader := &fakeRepositoryChangeSignalReader{}
	svc := NewWithConfig(newMemoryRepository(), fixedClock{}, &sequenceIDs{}, Config{
		Authorizer:              &spyAuthorizer{err: errs.ErrForbidden},
		RepositoryChangeSignals: reader,
	})

	_, err := svc.GetSelfDeploySignal(context.Background(), GetSelfDeploySignalInput{
		ProjectID:         projectID,
		ProviderSignalKey: "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
		Meta:              queryMeta(),
	})
	if err != errs.ErrForbidden {
		t.Fatalf("error = %v, want permission denied", err)
	}
	if reader.calls != 0 || reader.listCalls != 0 {
		t.Fatalf("provider reader was called before authorization: get=%d list=%d", reader.calls, reader.listCalls)
	}
}

func TestGetSelfDeploySignalRequiresServicesPolicyReconcileWhenPolicyCommitIsStale(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	store.policies[policyID] = activeSelfDeployPolicy(projectID, repositoryID, policyID, "oldcommit0123456789abcdef0123456789abc", []byte(`{"spec":{"services":[{"key":"api"}]}}`))
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{
			Status: ProviderOwnedDataStatusReady,
			Signal: RepositoryChangeSignal{
				SignalID:              "signal-1",
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:new",
				Kind:                  "push",
				ProviderSlug:          "github",
				RepositoryFullName:    "codex-k8s/kodex",
				Ref:                   "refs/heads/main",
				BaseBranch:            "main",
				CommitSHA:             "newcommit0123456789abcdef0123456789abc",
				PathSummaryStatus:     RepositoryChangePathSummaryStatusReady,
				ServicesPolicyChanged: true,
				DeployRelevantChanged: true,
				ChangeFingerprint:     "sha256:provider-change",
			},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(ctx, GetSelfDeploySignalInput{
		ProjectID:         projectID,
		ProviderSignalKey: "provider:github:repository_change:push:codex-k8s/kodex:main:new",
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusNeedsServicesPolicyReconcile || result.SafeReason != "services_policy_commit_not_reconciled" {
		t.Fatalf("result = %+v, want services policy reconcile status", result)
	}
	if len(result.Signal.AffectedServiceKeys) != 0 {
		t.Fatalf("affected keys = %+v, want none before checked policy reconcile", result.Signal.AffectedServiceKeys)
	}
}

func TestGetSelfDeploySignalBlocksUnavailablePathSummary(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	reader := &fakeRepositoryChangeSignalReader{
		result: RepositoryChangeSignalReadResult{
			Status: ProviderOwnedDataStatusReady,
			Signal: RepositoryChangeSignal{
				SignalID:           "signal-1",
				SignalKey:          "provider:github:repository_change:pull_request_merged:codex-k8s/kodex:main:1",
				Kind:               "pull_request_merged",
				ProviderSlug:       "github",
				RepositoryFullName: "codex-k8s/kodex",
				BaseBranch:         "main",
				CommitSHA:          bootstrapMergeCommitSHA,
				SourceRef:          "feature/self-deploy",
				PathSummaryStatus:  RepositoryChangePathSummaryStatusUnavailable,
			},
		},
	}
	svc := NewWithConfig(store, fixedClock{}, &sequenceIDs{}, Config{RepositoryChangeSignals: reader})

	result, err := svc.GetSelfDeploySignal(context.Background(), GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: "provider:github:repository_change:pull_request_merged:codex-k8s/kodex:main:1",
		Meta:              queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != enum.SelfDeploySignalStatusNeedsRepositoryChangeSummary || result.SafeReason != "path_summary_unavailable" {
		t.Fatalf("result = %+v, want path summary unavailable status", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsReadyRuntimeCompatibleSpecs(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api", "api"})
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{
		selfDeployMaterializedBuildContext("api"),
	}

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusReady {
		t.Fatalf("status = %s, want ready, reason=%s", result.Status, result.SafeReason)
	}
	if got := strings.Join(result.Plan.AffectedServiceKeys, ","); got != "api" {
		t.Fatalf("affected keys = %q, want api", got)
	}
	if len(result.Plan.BuildItems) != 1 {
		t.Fatalf("build items = %d, want 1", len(result.Plan.BuildItems))
	}
	item := result.Plan.BuildItems[0]
	if item.Status != SelfDeployBuildPlanItemStatusReady || item.BuildRecipe.RecipeFingerprint == "" {
		t.Fatalf("item = %+v, want ready item with recipe fingerprint", item)
	}
	spec := item.BuildExecutionSpec
	if spec.ServiceKey != "api" ||
		spec.ImageRef != "registry.example/kodex/api" ||
		spec.ImageTag != "abcdef0" ||
		spec.ImageDigest != selfDeployBuildImageDigest ||
		spec.BuildContextRef != "pvc://runtime/build-context-api" ||
		spec.BuildContextDigest != selfDeployBuildContextDigest ||
		spec.DockerfileRef != "context://services/api/Dockerfile" ||
		spec.DockerfileDigest != selfDeployBuildDockerfileDigest ||
		spec.DockerfileTarget != "prod" ||
		spec.BuilderImageRef != "gcr.io/kaniko-project/executor:v1.23.2" {
		t.Fatalf("build spec = %+v, want checked runtime-compatible fields", spec)
	}
	if len(spec.AllowedSecretRefs) != 1 ||
		spec.AllowedSecretRefs[0].SecretRef != "secret://runtime/registry" ||
		spec.AllowedSecretRefs[0].Purpose != "registry_docker_config" {
		t.Fatalf("secret refs = %+v, want safe ref without values", spec.AllowedSecretRefs)
	}
	if result.Plan.PlanFingerprint == "" ||
		spec.BuildPlanFingerprint != result.Plan.PlanFingerprint ||
		item.PlanItemFingerprint == "" {
		t.Fatalf("fingerprints = plan %q spec %q item %q, want stable fingerprints", result.Plan.PlanFingerprint, spec.BuildPlanFingerprint, item.PlanItemFingerprint)
	}
	replayed, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("replayed GetSelfDeployBuildPlan(): %v", err)
	}
	if replayed.Plan.PlanFingerprint != result.Plan.PlanFingerprint ||
		replayed.Plan.BuildItems[0].PlanItemFingerprint != item.PlanItemFingerprint {
		t.Fatalf("replayed fingerprints = %+v, want stable %+v", replayed.Plan, result.Plan)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), "secret_value") ||
		strings.Contains(string(encoded), "validated_payload") ||
		strings.Contains(string(encoded), "must-not-leak") {
		t.Fatalf("build plan leaked unsafe payload: %s", encoded)
	}
}

func TestGetSelfDeployBuildPlanAllowsCodeOnlySignalCommitAfterPolicyCommit(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	policyCommit := "1111111111111111111111111111111111111111"
	codeCommit := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, policyCommit, selfDeployBuildPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.MergeCommitSHA = codeCommit
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{
		selfDeployMaterializedBuildContext("api"),
	}

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusReady {
		t.Fatalf("status = %s, reason=%s, want ready for code-only change", result.Status, result.SafeReason)
	}
	if result.SafeReason != "" {
		t.Fatalf("safe reason = %q, want empty ready diagnostic", result.SafeReason)
	}
	item := result.Plan.BuildItems[0]
	spec := result.Plan.BuildItems[0].BuildExecutionSpec
	if item.Status != SelfDeployBuildPlanItemStatusReady || item.SafeReason != "" {
		t.Fatalf("item status/reason = %s/%q, want ready without stale policy reason", item.Status, item.SafeReason)
	}
	if spec.SourceCommitSHA != codeCommit ||
		spec.SourceRef != input.SourceRef ||
		result.Plan.MergeCommitSHA != codeCommit ||
		result.Plan.ServicesYaml.SourceCommitSHA != policyCommit {
		t.Fatalf("refs = spec commit %q spec source %q plan commit %q policy commit %q, want code commit/ref and older policy commit", spec.SourceCommitSHA, spec.SourceRef, result.Plan.MergeCommitSHA, result.Plan.ServicesYaml.SourceCommitSHA)
	}
}

func TestGetSelfDeployBuildPlanDerivesSpecsFromStackInventoryImages(t *testing.T) {
	t.Setenv("KODEX_INTERNAL_REGISTRY_HOST", "private.registry.example")
	t.Setenv("KODEX_API_IMAGE", "private.registry.example/kodex/api:must-not-leak")
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployStackInventoryBuildPolicyPayload(true))
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{
		selfDeployMaterializedBuildContext("api"),
	}

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusReady {
		t.Fatalf("status = %s, want ready, reason=%s", result.Status, result.SafeReason)
	}
	spec := result.Plan.BuildItems[0].BuildExecutionSpec
	if spec.ImageRef != "127.0.0.1:5000/kodex/api" ||
		spec.ImageTag != "0.1.0" ||
		spec.BuildContextRef != "pvc://runtime/build-context-api" ||
		spec.BuildContextDigest != selfDeployBuildContextDigest ||
		spec.DockerfileRef != "context://services/api/Dockerfile" ||
		spec.DockerfileTarget != "prod" ||
		spec.BuilderImageRef != "gcr.io/kaniko-project/executor:v1.24.0-debug" {
		t.Fatalf("build spec = %+v, want derived stack inventory build spec", spec)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), "private.registry.example") ||
		strings.Contains(string(encoded), "must-not-leak") {
		t.Fatalf("build plan used process env values: %s", encoded)
	}
}

func TestGetSelfDeployDeployPlanUsesRuntimeManifestBundleDigest(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildDeployPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.SourceRef = "main"
	input.BuildOutputs = []SelfDeployBuildOutput{selfDeployBuildOutput("api")}
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusReady {
		t.Fatalf("status = %s, want ready, reason=%s", result.Status, result.SafeReason)
	}
	if len(result.Plan.DeployItems) != 1 {
		t.Fatalf("deploy items = %d, want 1", len(result.Plan.DeployItems))
	}
	spec := result.Plan.DeployItems[0].DeployExecutionSpec
	if spec.ManifestBundleRef != "pvc://runtime/build-context-api/deploy/base/api" ||
		spec.ManifestBundleDigest != selfDeployManifestBundleDigest {
		t.Fatalf("manifest bundle = %q/%q, want runtime materialized digest", spec.ManifestBundleRef, spec.ManifestBundleDigest)
	}
	if len(spec.RolloutTargets) != 1 ||
		spec.RolloutTargets[0].Ref != "kubernetes://deployment/api" ||
		spec.RolloutTargets[0].Namespace != "kodex" ||
		spec.RolloutTargets[0].Name != "api" {
		t.Fatalf("rollout targets = %+v, want checked deployment target", spec.RolloutTargets)
	}
	if len(spec.ExpectedImageRefs) != 1 ||
		spec.ExpectedImageRefs[0].ImageRef != "registry.example/kodex/api:abcdef0" ||
		spec.ExpectedImageRefs[0].ImageDigest != selfDeployBuildImageDigest {
		t.Fatalf("expected image refs = %+v, want successful build output image ref", spec.ExpectedImageRefs)
	}
	if synthetic := checkedDeployDigest("bundle", spec.ManifestBundleRef, selfDeployBuildContextDigest); synthetic == spec.ManifestBundleDigest {
		t.Fatalf("manifest bundle digest = %q, want actual runtime digest not synthetic checked digest", spec.ManifestBundleDigest)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), "validated_payload") ||
		strings.Contains(string(encoded), "raw_manifest") ||
		strings.Contains(string(encoded), "secret_value") {
		t.Fatalf("deploy plan leaked unsafe payload: %s", encoded)
	}
}

func TestGetSelfDeployDeployPlanAllowsCodeOnlySignalCommitAfterPolicyCommit(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	policyCommit := "1111111111111111111111111111111111111111"
	codeCommit := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, policyCommit, selfDeployBuildDeployPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.MergeCommitSHA = codeCommit
	input.SourceRef = "main"
	input.BuildOutputs = []SelfDeployBuildOutput{selfDeployBuildOutput("api")}
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusReady {
		t.Fatalf("status = %s, reason=%s, want ready for code-only change", result.Status, result.SafeReason)
	}
	spec := result.Plan.DeployItems[0].DeployExecutionSpec
	if spec.SourceCommitSHA != codeCommit ||
		result.Plan.MergeCommitSHA != codeCommit ||
		result.Plan.ServicesYaml.SourceCommitSHA != policyCommit {
		t.Fatalf("commits = spec %q plan %q policy %q, want code commit and older policy commit", spec.SourceCommitSHA, result.Plan.MergeCommitSHA, result.Plan.ServicesYaml.SourceCommitSHA)
	}
}

func TestGetSelfDeployDeployPlanReturnsPolicyStaleOnDigestMismatch(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildDeployPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.ExpectedServicesPolicyDigest = "sha256:old"
	input.BuildOutputs = []SelfDeployBuildOutput{selfDeployBuildOutput("api")}
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusPolicyStale || result.SafeReason != "services_policy_digest_mismatch" {
		t.Fatalf("result = %+v, want policy stale digest mismatch", result)
	}
}

func TestGetSelfDeployDeployPlanReturnsBuildNotReadyWhenBuildOutputMissing(t *testing.T) {
	projectID, repositoryID, policy, svc := newSelfDeployBuildDeployPlanFixture()
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusBuildNotReady || result.SafeReason != "build_not_ready:api" {
		t.Fatalf("result = %+v, want build_not_ready", result)
	}
	if len(result.Plan.DeployItems) != 1 ||
		result.Plan.DeployItems[0].Status != SelfDeployDeployPlanItemStatusBuildNotReady ||
		result.Plan.DeployItems[0].DeployExecutionSpec.ManifestBundleRef != "" {
		t.Fatalf("deploy items = %+v, want no deploy spec before successful build output", result.Plan.DeployItems)
	}
}

func TestGetSelfDeployDeployPlanReturnsUnavailableWhenManifestBundleMissing(t *testing.T) {
	projectID, repositoryID, policy, svc := newSelfDeployBuildDeployPlanFixture()
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.BuildOutputs = []SelfDeployBuildOutput{selfDeployBuildOutput("api")}
	materializedContext := selfDeployMaterializedBuildContext("api")
	materializedContext.ManifestBundleDigest = ""
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{materializedContext}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusDeployPlanUnavailable || result.SafeReason != "deploy_plan_unavailable:api" {
		t.Fatalf("result = %+v, want deploy_plan_unavailable", result)
	}
	if len(result.Plan.DeployItems) != 1 ||
		result.Plan.DeployItems[0].Status != SelfDeployDeployPlanItemStatusDeployPlanUnavailable ||
		result.Plan.DeployItems[0].DeployExecutionSpec.ManifestBundleDigest != "" {
		t.Fatalf("deploy items = %+v, want no deploy spec without checked manifest bundle digest", result.Plan.DeployItems)
	}
}

func TestGetSelfDeployDeployPlanRejectsBuildOutputFromDifferentBuildPlan(t *testing.T) {
	projectID, repositoryID, policy, svc := newSelfDeployBuildDeployPlanFixture()
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	output := selfDeployBuildOutput("api")
	output.BuildPlanFingerprint = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	input.BuildOutputs = []SelfDeployBuildOutput{output}
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusBuildOutputInvalid || result.SafeReason != "build_output_build_plan_fingerprint_mismatch:api" {
		t.Fatalf("result = %+v, want build output fingerprint mismatch", result)
	}
}

func TestGetSelfDeployDeployPlanRejectsBuildOutputFromDifferentContext(t *testing.T) {
	projectID, repositoryID, policy, svc := newSelfDeployBuildDeployPlanFixture()
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	output := selfDeployBuildOutput("api")
	output.BuildContextDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	input.BuildOutputs = []SelfDeployBuildOutput{output}
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusBuildOutputInvalid || result.SafeReason != "build_output_context_mismatch:api" {
		t.Fatalf("result = %+v, want build output context mismatch", result)
	}
}

func TestGetSelfDeployDeployPlanReturnsInvalidInputWithoutExpectedBuildPlanFingerprint(t *testing.T) {
	projectID, repositoryID, policy, svc := newSelfDeployBuildDeployPlanFixture()
	input := selfDeployDeployPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.ExpectedBuildPlanFingerprint = ""
	input.BuildOutputs = []SelfDeployBuildOutput{selfDeployBuildOutput("api")}
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployDeployPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployDeployPlanStatusInvalidInput || result.SafeReason != "invalid_input" {
		t.Fatalf("result = %+v, want invalid input without expected build plan fingerprint", result)
	}
}

func TestCheckedDeployManifestBundleRejectsDifferentBundlePath(t *testing.T) {
	t.Parallel()

	_, _, err := checkedDeployManifestBundle(value.ServicesPolicyDeploySpec{
		Kustomization: "deploy/overlays/prod/api/kustomization.yaml",
	}, selfDeployMaterializedBuildContext("api"))

	if !errors.Is(err, errDeployPlanUnavailable) {
		t.Fatalf("checkedDeployManifestBundle() err = %v, want deploy plan unavailable", err)
	}
}

func TestGetSelfDeployBuildPlanReturnsBuildContextRequiredForStackInventoryWithoutRuntimeContext(t *testing.T) {
	t.Setenv("KODEX_INTERNAL_REGISTRY_HOST", "private.registry.example")
	t.Setenv("KODEX_API_IMAGE", "private.registry.example/kodex/api:must-not-leak")
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployStackInventoryBuildPolicyPayload(true))
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusBuildContextRequired || result.SafeReason != "build_context_required:api" {
		t.Fatalf("result = %+v, want build_context_required", result)
	}
	if len(result.Plan.BuildItems) != 1 {
		t.Fatalf("build items = %d, want recipe item before runtime context", len(result.Plan.BuildItems))
	}
	item := result.Plan.BuildItems[0]
	if item.Status != SelfDeployBuildPlanItemStatusBuildContextRequired ||
		item.BuildRecipe.ImageRef != "127.0.0.1:5000/kodex/api" ||
		item.BuildRecipe.RecipeFingerprint == "" ||
		item.BuildExecutionSpec.BuildContextRef != "" {
		t.Fatalf("item = %+v, want recipe-only context-required item", item)
	}
}

func TestGetSelfDeployBuildPlanReturnsBuildContextInvalidForBadRuntimeContext(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
	materializedContext := selfDeployMaterializedBuildContext("api")
	materializedContext.BuildContextDigest = "sha256:bad"
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{materializedContext}

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusBuildContextInvalid || result.SafeReason != "build_context_invalid:api" {
		t.Fatalf("result = %+v, want build_context_invalid", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsPolicyStaleOnDigestMismatch(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.ExpectedServicesPolicyDigest = "sha256:old"

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusPolicyStale || result.SafeReason != "services_policy_digest_mismatch" {
		t.Fatalf("result = %+v, want policy stale digest mismatch", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsPolicyStaleOnSourceMismatch(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	for _, tt := range []struct {
		name       string
		mutate     func(*GetSelfDeployBuildPlanInput)
		wantReason string
	}{
		{
			name: "source ref",
			mutate: func(input *GetSelfDeployBuildPlanInput) {
				input.SourceRef = "refs/heads/release"
			},
			wantReason: "services_policy_source_ref_mismatch",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newMemoryRepository()
			store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
			policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
			store.policies[policyID] = policy
			store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
				activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
			}
			svc := New(store, fixedClock{}, &sequenceIDs{})
			input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
			tt.mutate(&input)

			result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
			if err != nil {
				t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
			}
			if result.Status != enum.SelfDeployBuildPlanStatusPolicyStale || result.SafeReason != tt.wantReason {
				t.Fatalf("result = %+v, want policy stale %s", result, tt.wantReason)
			}
		})
	}
}

func TestGetSelfDeployBuildPlanAcceptsEquivalentBranchSourceRefs(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	policy.SourceRef = "refs/heads/main"
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.SourceRef = "main"
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusReady {
		t.Fatalf("result = %+v, want ready for equivalent branch source refs", result)
	}
}

func TestGetSelfDeployBuildPlanRejectsCommitLikeSourceRefNormalization(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	policy.SourceRef = "refs/heads/" + commitSHA
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})
	input.SourceRef = commitSHA
	input.MaterializedBuildContexts = []SelfDeployMaterializedBuildContext{selfDeployMaterializedBuildContext("api")}

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusPolicyStale || result.SafeReason != "services_policy_source_ref_mismatch" {
		t.Fatalf("result = %+v, want policy stale source ref mismatch", result)
	}
}

func TestSelfDeploySourceRefsEquivalentOnlyNormalizesSafeBranches(t *testing.T) {
	sha40 := "abcdef0123456789abcdef0123456789abcdef01"
	sha64 := strings.Repeat("a", 64)
	for _, tt := range []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{
			name:  "bare branch",
			left:  "main",
			right: "refs/heads/main",
			want:  true,
		},
		{
			name:  "nested branch",
			left:  "feature/self-deploy_1.2",
			right: "refs/heads/feature/self-deploy_1.2",
			want:  true,
		},
		{
			name:  "raw forty hex sha",
			left:  sha40,
			right: "refs/heads/" + sha40,
			want:  false,
		},
		{
			name:  "raw sixty four hex sha",
			left:  sha64,
			right: "refs/heads/" + sha64,
			want:  false,
		},
		{
			name:  "tag shorthand",
			left:  "v1.2.3",
			right: "refs/tags/v1.2.3",
			want:  false,
		},
		{
			name:  "provider ref",
			left:  "github:codex-k8s/kodex@main",
			right: "refs/heads/main",
			want:  false,
		},
		{
			name:  "digest ref",
			left:  "sha256:" + sha64,
			right: "refs/heads/main",
			want:  false,
		},
		{
			name:  "unsafe whitespace",
			left:  "main branch",
			right: "refs/heads/main branch",
			want:  false,
		},
		{
			name:  "unsafe traversal",
			left:  "feature/../main",
			right: "refs/heads/feature/../main",
			want:  false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := selfDeploySourceRefsEquivalent(tt.left, tt.right)
			if got != tt.want {
				t.Fatalf("selfDeploySourceRefsEquivalent(%q, %q) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestGetSelfDeployBuildPlanReturnsServiceNotFoundForUnknownAffectedService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"missing"})

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusServiceNotFound || !strings.Contains(result.SafeReason, "service_not_found") {
		t.Fatalf("result = %+v, want service_not_found", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsUnavailableWhenServicesPolicyMissing(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.wrapNotFound = true
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildPolicyPayload())
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusBuildPlanUnavailable || result.SafeReason != "services_policy_not_found" {
		t.Fatalf("result = %+v, want wrapped services policy not found safe status", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsUnavailableWhenServiceHasNoBuildSpec(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, []byte(`{"spec":{"services":[{"key":"api","rootPath":"services/api"}]}}`))
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusBuildPlanUnavailable {
		t.Fatalf("result = %+v, want build_plan_unavailable", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsUnavailableForRuntimeIncompatibleBuildSpec(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	payload := []byte(strings.Replace(string(selfDeployBuildPolicyPayload()), `"imageTag": "abcdef0"`, `"imageTag": "bad tag"`, 1))
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, payload)
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	svc := New(store, fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, []string{"api"})

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusBuildPlanUnavailable || !strings.Contains(result.SafeReason, "service_build_spec_unavailable") {
		t.Fatalf("result = %+v, want unavailable runtime-incompatible build spec", result)
	}
}

func TestGetSelfDeployBuildPlanReturnsInvalidInputForEmptyAffectedServices(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policy := activeSelfDeployPolicy(projectID, repositoryID, uuid.New(), "abcdef0123456789abcdef0123456789abcdef01", selfDeployBuildPolicyPayload())
	svc := New(newMemoryRepository(), fixedClock{}, &sequenceIDs{})
	input := selfDeployBuildPlanInput(projectID, repositoryID, policy, nil)

	result, err := svc.GetSelfDeployBuildPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != enum.SelfDeployBuildPlanStatusInvalidInput {
		t.Fatalf("result = %+v, want invalid_input", result)
	}
}

func TestPutDocumentationSourceNormalizesAndPublishesEvent(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	sourceID := uuid.New()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{sourceID, uuid.New()}})

	source, err := svc.PutDocumentationSource(ctx, PutDocumentationSourceInput{
		ProjectID:  uuid.New(),
		ScopeType:  enum.DocumentationScopeService,
		ScopeID:    "api",
		LocalPath:  "docs/api/../api",
		AccessMode: enum.DocumentationAccessWrite,
		Meta:       commandMeta(uuid.New()),
	})
	if err != nil {
		t.Fatalf("PutDocumentationSource(): %v", err)
	}
	if source.ID != sourceID || source.LocalPath != "docs/api" || source.Status != enum.DocumentationSourceStatusActive {
		t.Fatalf("source = %+v, want normalized active source", source)
	}
	if len(store.events) != 1 || store.events[0].EventType != projectEventDocumentationCreated {
		t.Fatalf("events = %+v, want documentation source created", store.events)
	}
}

func TestPutDocumentationSourceRejectsUnsafePath(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

	_, err := svc.PutDocumentationSource(ctx, PutDocumentationSourceInput{
		ProjectID: uuid.New(),
		ScopeType: enum.DocumentationScopeProject,
		LocalPath: "../docs",
		Meta:      commandMeta(uuid.New()),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("PutDocumentationSource() err = %v, want invalid argument", err)
	}
}

func TestCancelPolicyOverrideMarksOverrideCancelled(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	overrideID := uuid.New()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	store := newMemoryRepository()
	store.policyOverrides[overrideID] = entity.PolicyOverride{
		Base:              entity.Base{ID: overrideID, Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:         projectID,
		TargetType:        enum.PolicyOverrideTargetServicesPolicy,
		Payload:           []byte(`{"reason":"test"}`),
		Reason:            "test",
		Status:            enum.PolicyOverrideStatusActive,
		ExpiresAt:         now.Add(time.Hour),
		CreatedByActorRef: "user:owner",
	}
	svc := New(store, fixedClock{}, &sequenceIDs{ids: []uuid.UUID{uuid.New()}})

	override, err := svc.CancelPolicyOverride(ctx, CancelPolicyOverrideInput{
		PolicyOverrideID: overrideID,
		Meta:             commandMetaWithVersion(uuid.New(), 1),
	})
	if err != nil {
		t.Fatalf("CancelPolicyOverride(): %v", err)
	}
	if override.Status != enum.PolicyOverrideStatusCancelled || override.Version != 2 {
		t.Fatalf("override = %+v, want cancelled version 2", override)
	}
	if len(store.events) != 1 || store.events[0].EventType != projectEventPolicyOverrideCancelled {
		t.Fatalf("events = %+v, want policy override cancelled", store.events)
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

type spyBootstrapProvider struct {
	calls            int
	input            ProviderBootstrapPullRequestInput
	result           RepositoryBootstrapProviderResult
	err              error
	createRepoCalls  int
	createRepoInput  ProviderRepositoryCreateInput
	createRepoResult RepositoryProviderCreateProviderResult
	createRepoErr    error
}

func (p *spyBootstrapProvider) CreateProviderRepository(_ context.Context, input ProviderRepositoryCreateInput) (RepositoryProviderCreateProviderResult, error) {
	p.createRepoCalls++
	p.createRepoInput = input
	return p.createRepoResult, p.createRepoErr
}

func (p *spyBootstrapProvider) CreateRepositoryBootstrapPullRequest(_ context.Context, input ProviderBootstrapPullRequestInput) (RepositoryBootstrapProviderResult, error) {
	p.calls++
	p.input = input
	return p.result, p.err
}

type fakeRepositoryChangeSignalReader struct {
	calls      int
	input      RepositoryChangeSignalReadInput
	result     RepositoryChangeSignalReadResult
	err        error
	listCalls  int
	listInput  RepositoryChangeSignalListInput
	listResult RepositoryChangeSignalListResult
	listErr    error
}

func (r *fakeRepositoryChangeSignalReader) GetRepositoryChangeSignal(_ context.Context, input RepositoryChangeSignalReadInput) (RepositoryChangeSignalReadResult, error) {
	r.calls++
	r.input = input
	return r.result, r.err
}

func (r *fakeRepositoryChangeSignalReader) ListRepositoryChangeSignals(_ context.Context, input RepositoryChangeSignalListInput) (RepositoryChangeSignalListResult, error) {
	r.listCalls++
	r.listInput = input
	return r.listResult, r.listErr
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

func queryMeta() value.QueryMeta {
	return value.QueryMeta{
		Actor: value.Actor{
			Type: "service",
			ID:   "agent-manager",
		},
		RequestID: "read-1",
		RequestContext: value.RequestContext{
			Source: "test",
		},
	}
}

func commandMetaWithVersion(commandID uuid.UUID, version int64) value.CommandMeta {
	meta := commandMeta(commandID)
	meta.ExpectedVersion = &version
	return meta
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

const bootstrapServicesPolicyFileContent = "spec:\n  services:\n    - key: api\n      rootPath: services/api\n      kind: backend\n"

const bootstrapServicesPolicyPayload = `{"spec":{"services":[{"key":"api","rootPath":"services/api","kind":"backend"}]}}`

const bootstrapMergeCommitSHA = "0123456789abcdef0123456789abcdef01234567"

const selfDeployBuildImageDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

const selfDeployBuildContextDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

const selfDeployBuildDockerfileDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

const selfDeployManifestBundleDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func bootstrapServicesPolicy() RepositoryBootstrapServicesPolicy {
	return bootstrapServicesPolicyForContent(bootstrapServicesPolicyFileContent, []byte(bootstrapServicesPolicyPayload))
}

func pendingBootstrapRepository(projectID uuid.UUID, repositoryID uuid.UUID) entity.RepositoryBinding {
	now := fixedClock{}.Now()
	return entity.RepositoryBinding{
		Base: entity.Base{
			ID:        repositoryID,
			Version:   3,
			CreatedAt: now,
			UpdatedAt: now,
		},
		ProjectID:            projectID,
		Provider:             enum.RepositoryProviderGitHub,
		ProviderOwner:        "codex-k8s",
		ProviderName:         "kodex",
		WebURL:               "https://github.com/codex-k8s/kodex",
		DefaultBranch:        "main",
		Status:               enum.RepositoryStatusPending,
		ProviderRepositoryID: "R_123",
	}
}

func activeSelfDeployRepository(projectID uuid.UUID, repositoryID uuid.UUID) entity.RepositoryBinding {
	repository := pendingBootstrapRepository(projectID, repositoryID)
	repository.Status = enum.RepositoryStatusActive
	return repository
}

func activeSelfDeployPolicy(projectID uuid.UUID, repositoryID uuid.UUID, policyID uuid.UUID, commitSHA string, payload []byte) entity.ServicesPolicy {
	now := fixedClock{}.Now()
	return entity.ServicesPolicy{
		Base:               entity.Base{ID: policyID, Version: 7, CreatedAt: now, UpdatedAt: now},
		ProjectID:          projectID,
		SourceRepositoryID: &repositoryID,
		SourcePath:         "services.yaml",
		SourceRef:          "refs/heads/main",
		SourceCommitSHA:    commitSHA,
		ContentHash:        "sha256:services-policy",
		ValidatedPayload:   payload,
		ValidationStatus:   enum.ServicesPolicyValidationValid,
		ProjectionStatus:   enum.ServicesPolicyProjectionSynced,
		PolicyVersion:      7,
		ImportedAt:         now,
	}
}

func activeSelfDeployDescriptor(projectID uuid.UUID, repositoryID uuid.UUID, policyID uuid.UUID, key string) entity.ServiceDescriptor {
	now := fixedClock{}.Now()
	return entity.ServiceDescriptor{
		Base:             entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:        projectID,
		ServicesPolicyID: policyID,
		RepositoryID:     &repositoryID,
		ServiceKey:       key,
		DisplayName:      key,
		Kind:             enum.ServiceKindBackend,
		RootPath:         "services/" + key,
		Status:           enum.ServiceStatusActive,
	}
}

func selfDeployBuildPolicyPayload() []byte {
	return []byte(`{
		"spec": {
			"services": [
				{
					"key": "api",
					"rootPath": "services/api",
						"build": {
							"imageRef": "registry.example/kodex/api",
							"imageTag": "abcdef0",
							"imageDigest": "` + selfDeployBuildImageDigest + `",
							"buildContextRef": "pvc://runtime/build-context-api",
							"buildContextDigest": "` + selfDeployBuildContextDigest + `",
							"dockerfileRef": "context://services/api/Dockerfile",
							"dockerfileDigest": "` + selfDeployBuildDockerfileDigest + `",
							"dockerfileTarget": "prod",
							"builderImageRef": "gcr.io/kaniko-project/executor:v1.23.2",
						"allowedSecretRefs": [
							{
								"secretRef": "secret://runtime/registry",
								"purpose": "registry_docker_config",
								"value": "secret_value"
							}
						],
						"outputRefs": [
							{
								"kind": "image",
								"ref": "runtime:image:api"
							}
						],
						"unsafePayload": "must-not-leak"
					}
				}
			]
		}
	}`)
}

func selfDeployBuildDeployPolicyPayload() []byte {
	return []byte(`{
		"spec": {
			"services": [
				{
					"key": "api",
					"rootPath": "services/api",
					"build": {
						"imageRef": "registry.example/kodex/api",
						"imageTag": "abcdef0",
						"imageDigest": "` + selfDeployBuildImageDigest + `",
						"dockerfileRef": "context://services/api/Dockerfile",
						"dockerfileDigest": "` + selfDeployBuildDockerfileDigest + `",
						"dockerfileTarget": "prod",
						"builderImageRef": "gcr.io/kaniko-project/executor:v1.23.2"
					},
					"deploy": {
						"serviceManifest": "deploy/base/api/deployment.yaml",
						"kustomization": "deploy/base/api/kustomization.yaml",
						"targetNamespace": "kodex",
						"targetClusterRef": "fleet://cluster/default",
						"rolloutTargets": [
							{
								"kind": "deployment",
								"ref": "kubernetes://deployment/api",
								"namespace": "kodex",
								"name": "api"
							}
						]
					}
				}
			]
		}
	}`)
}

func selfDeployStackInventoryBuildPolicyPayload(withContext bool) []byte {
	contextFields := ""
	if withContext {
		contextFields = `,
				"buildContextRef": "pvc://runtime-jobs/runtime-build-context-api",
				"buildContextDigest": "` + selfDeployBuildContextDigest + `",
				"dockerfileDigest": "` + selfDeployBuildDockerfileDigest + `",
				"allowedSecretRefs": [
					{
						"secretRef": "secret://runtime/registry-push",
						"purpose": "registry"
					}
				],
				"outputRefs": [
					{
						"kind": "image_ref",
						"ref": "runtime://artifacts/images/api"
					}
				]`
	}
	return []byte(`{
		"spec": {
			"versions": {
				"api": {
					"value": "0.1.0"
				},
				"kaniko-executor": {
					"value": "v1.24.0-debug"
				}
			},
			"images": {
				"kaniko-executor": {
					"type": "external",
					"from": "gcr.io/kaniko-project/executor:{{ version \"kaniko-executor\" }}"
				},
				"api": {
					"type": "build",
					"repository": "{{ envOr \"KODEX_INTERNAL_REGISTRY_HOST\" \"127.0.0.1:5000\" }}/kodex/api",
					"tagTemplate": "{{ version \"api\" }}",
					"dockerfile": "services/api/Dockerfile",
					"target": "prod",
					"context": ".",
					"imageEnv": "KODEX_API_IMAGE"` + contextFields + `
				}
			},
			"deployableServices": [
				{
					"name": "api",
					"status": "foundation",
					"path": "services/api",
					"dockerfile": "services/api/Dockerfile",
					"imageEnv": "KODEX_API_IMAGE"
				}
			]
		}
	}`)
}

func newSelfDeployBuildDeployPlanFixture() (uuid.UUID, uuid.UUID, entity.ServicesPolicy, *Service) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commitSHA := "abcdef0123456789abcdef0123456789abcdef01"
	store := newMemoryRepository()
	store.repositories[repositoryID] = activeSelfDeployRepository(projectID, repositoryID)
	policy := activeSelfDeployPolicy(projectID, repositoryID, policyID, commitSHA, selfDeployBuildDeployPolicyPayload())
	store.policies[policyID] = policy
	store.serviceDescriptors[policyID] = []entity.ServiceDescriptor{
		activeSelfDeployDescriptor(projectID, repositoryID, policyID, "api"),
	}
	return projectID, repositoryID, policy, New(store, fixedClock{}, &sequenceIDs{})
}

func selfDeployBuildPlanInput(projectID uuid.UUID, repositoryID uuid.UUID, policy entity.ServicesPolicy, serviceKeys []string) GetSelfDeployBuildPlanInput {
	version := policy.PolicyVersion
	return GetSelfDeployBuildPlanInput{
		ProjectID:                         projectID,
		RepositoryID:                      repositoryID,
		SourceRef:                         "refs/heads/main",
		MergeCommitSHA:                    "abcdef0123456789abcdef0123456789abcdef01",
		ProviderSignalRef:                 "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
		AffectedServiceKeys:               serviceKeys,
		ExpectedServicesPolicyDigest:      policy.ContentHash,
		ExpectedServicesPolicyFingerprint: selfDeployServicesYamlFingerprint(policy),
		ExpectedServicesPolicyVersion:     &version,
		Meta:                              queryMeta(),
	}
}

func selfDeployDeployPlanInput(projectID uuid.UUID, repositoryID uuid.UUID, policy entity.ServicesPolicy, serviceKeys []string) GetSelfDeployDeployPlanInput {
	version := policy.PolicyVersion
	return GetSelfDeployDeployPlanInput{
		ProjectID:                         projectID,
		RepositoryID:                      repositoryID,
		SourceRef:                         "refs/heads/main",
		MergeCommitSHA:                    "abcdef0123456789abcdef0123456789abcdef01",
		ProviderSignalRef:                 "provider:github:repository_change:push:codex-k8s/kodex:main:abcdef",
		AffectedServiceKeys:               serviceKeys,
		ExpectedServicesPolicyDigest:      policy.ContentHash,
		ExpectedServicesPolicyFingerprint: selfDeployServicesYamlFingerprint(policy),
		ExpectedServicesPolicyVersion:     &version,
		ExpectedBuildPlanFingerprint:      "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Meta:                              queryMeta(),
	}
}

func selfDeployBuildOutput(serviceKey string) SelfDeployBuildOutput {
	return SelfDeployBuildOutput{
		ServiceKey:               serviceKey,
		RuntimeJobRef:            "runtime://job/build-" + serviceKey,
		ImageRef:                 "registry.example/kodex/" + serviceKey,
		ImageTag:                 "abcdef0",
		ImageDigest:              selfDeployBuildImageDigest,
		BuildPlanItemFingerprint: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		BuildPlanFingerprint:     "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		BuildContextRef:          "pvc://runtime/build-context-" + serviceKey,
		BuildContextDigest:       selfDeployBuildContextDigest,
	}
}

func selfDeployMaterializedBuildContext(serviceKey string) SelfDeployMaterializedBuildContext {
	return SelfDeployMaterializedBuildContext{
		ServiceKey:           serviceKey,
		BuildContextRef:      "pvc://runtime/build-context-" + serviceKey,
		BuildContextDigest:   selfDeployBuildContextDigest,
		DockerfileDigest:     selfDeployBuildDockerfileDigest,
		MaterializationRef:   "runtime://workspace-materialization/" + serviceKey,
		ManifestBundleDigest: selfDeployManifestBundleDigest,
	}
}

func importBootstrapServicesPolicyInput(projectID uuid.UUID, repositoryID uuid.UUID, meta value.CommandMeta) ImportBootstrapServicesPolicyInput {
	return ImportBootstrapServicesPolicyInput{
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		ProviderTarget: RepositoryBootstrapProviderTarget{
			ProviderSlug:         "github",
			RepositoryFullName:   "codex-k8s/kodex",
			ProviderRepositoryID: "R_123",
			WebURL:               "https://github.com/codex-k8s/kodex",
		},
		BaseBranch:                   "main",
		SourceRef:                    "refs/heads/main",
		SourceCommitSHA:              bootstrapMergeCommitSHA,
		SourceBlobSHA:                "blob-1",
		SourcePath:                   "services.yaml",
		ContentHash:                  bootstrapContentHash(bootstrapServicesPolicyFileContent),
		ValidatedPayload:             []byte(bootstrapServicesPolicyPayload),
		WatermarkJSON:                []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`),
		ProviderWorkItemProjectionID: "projection-1",
		ProviderWebURL:               "https://github.com/codex-k8s/kodex/pull/7",
		ProviderObjectID:             "PR_123",
		MergeObservedAt:              fixedClock{}.Now().Format(time.RFC3339Nano),
		Meta:                         meta,
	}
}

func reconcileBootstrapMergeSignalInput(projectID uuid.UUID, repositoryID uuid.UUID, meta value.CommandMeta) ReconcileBootstrapMergeSignalInput {
	watermarkJSON := []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`)
	return ReconcileBootstrapMergeSignalInput{
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		MergeSignal: BootstrapRepositoryMergeSignal{
			SignalID:   uuid.New().String(),
			SignalKey:  "github/bootstrap/PR_123",
			SignalKind: "bootstrap",
			ProviderTarget: RepositoryBootstrapProviderTarget{
				ProviderSlug:         "github",
				RepositoryFullName:   "codex-k8s/kodex",
				ProviderRepositoryID: "R_123",
				WebURL:               "https://github.com/codex-k8s/kodex",
			},
			BaseBranch:                   "main",
			SourceRef:                    "kodex/bootstrap",
			MergeCommitSHA:               bootstrapMergeCommitSHA,
			SourceBlobSHA:                "blob-1",
			WatermarkDigest:              sha256HexDigest(watermarkJSON),
			WatermarkJSON:                watermarkJSON,
			ProviderWorkItemProjectionID: "projection-1",
			ProviderWebURL:               "https://github.com/codex-k8s/kodex/pull/7",
			ProviderObjectID:             "PR_123",
			MergeObservedAt:              fixedClock{}.Now().Format(time.RFC3339Nano),
			MergedAt:                     fixedClock{}.Now().Format(time.RFC3339Nano),
		},
		CheckedPolicy: CheckedBootstrapServicesPolicyArtifact{
			ArtifactRef:      "artifact://provider/bootstrap/PR_123/services.yaml",
			ArtifactDigest:   bootstrapContentHash(bootstrapServicesPolicyFileContent),
			ArtifactVersion:  bootstrapMergeCommitSHA,
			SourcePath:       "services.yaml",
			ContentHash:      bootstrapContentHash(bootstrapServicesPolicyFileContent),
			ValidatedPayload: []byte(bootstrapServicesPolicyPayload),
		},
		Meta: meta,
	}
}

func reconcileAdoptionMergeSignalInput(projectID uuid.UUID, repositoryID uuid.UUID, meta value.CommandMeta) ReconcileAdoptionMergeSignalInput {
	watermarkJSON := []byte(`{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_adoption","source_ref":"services.yaml"}`)
	return ReconcileAdoptionMergeSignalInput{
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		MergeSignal: RepositoryAdoptionMergeSignal{
			SignalID:   uuid.New().String(),
			SignalKey:  "github/adoption/PR_123",
			SignalKind: "adoption",
			ProviderTarget: RepositoryBootstrapProviderTarget{
				ProviderSlug:         "github",
				RepositoryFullName:   "codex-k8s/kodex",
				ProviderRepositoryID: "R_123",
				WebURL:               "https://github.com/codex-k8s/kodex",
			},
			BaseBranch:                   "main",
			SourceRef:                    "kodex/adoption",
			MergeCommitSHA:               bootstrapMergeCommitSHA,
			SourceBlobSHA:                "blob-1",
			WatermarkDigest:              sha256HexDigest(watermarkJSON),
			WatermarkJSON:                watermarkJSON,
			ProviderWorkItemProjectionID: "projection-adoption-1",
			ProviderWebURL:               "https://github.com/codex-k8s/kodex/pull/8",
			ProviderObjectID:             "PR_124",
			MergeObservedAt:              fixedClock{}.Now().Format(time.RFC3339Nano),
			MergedAt:                     fixedClock{}.Now().Format(time.RFC3339Nano),
		},
		CheckedPolicy: CheckedAdoptionServicesPolicyArtifact{
			ArtifactRef:      "artifact://provider/adoption/PR_123/services.yaml",
			ArtifactDigest:   bootstrapContentHash(bootstrapServicesPolicyFileContent),
			ArtifactVersion:  bootstrapMergeCommitSHA,
			SourcePath:       "services.yaml",
			ContentHash:      bootstrapContentHash(bootstrapServicesPolicyFileContent),
			ValidatedPayload: []byte(bootstrapServicesPolicyPayload),
		},
		Meta: meta,
	}
}

func bootstrapMergeSignalDiagnosticInput(projectID uuid.UUID, repositoryID uuid.UUID, fingerprint string) BootstrapMergeSignalDiagnosticInput {
	input := reconcileBootstrapMergeSignalInput(projectID, repositoryID, value.CommandMeta{})
	return BootstrapMergeSignalDiagnosticInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		MergeSignal:       input.MergeSignal,
		SignalFingerprint: fingerprint,
		ErrorCode:         "missing_checked_artifact",
		ErrorSummary:      "bootstrap merge signal needs checked artifact input",
		Summary:           "bootstrap merge signal received",
	}
}

func adoptionMergeSignalDiagnosticInput(projectID uuid.UUID, repositoryID uuid.UUID, fingerprint string) AdoptionMergeSignalDiagnosticInput {
	input := reconcileAdoptionMergeSignalInput(projectID, repositoryID, commandMeta(uuid.Nil))
	return AdoptionMergeSignalDiagnosticInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		MergeSignal:       input.MergeSignal,
		SignalFingerprint: fingerprint,
		ErrorCode:         "missing_checked_artifact",
		ErrorSummary:      "adoption merge signal needs checked artifact input",
		Summary:           "adoption merge signal received",
	}
}

func bootstrapServicesPolicyForContent(content string, payload []byte) RepositoryBootstrapServicesPolicy {
	return RepositoryBootstrapServicesPolicy{
		SourcePath:       "services.yaml",
		ContentHash:      bootstrapContentHash(content),
		ValidatedPayload: payload,
	}
}

func bootstrapContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sha256HexDigest(payload []byte) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(payload))))
	return hex.EncodeToString(sum[:])
}

type memoryRepository struct {
	projects                   map[uuid.UUID]entity.Project
	repositories               map[uuid.UUID]entity.RepositoryBinding
	policies                   map[uuid.UUID]entity.ServicesPolicy
	serviceDescriptors         map[uuid.UUID][]entity.ServiceDescriptor
	policyDocumentationSources map[uuid.UUID][]entity.DocumentationSource
	branchRules                map[uuid.UUID]entity.BranchRules
	documentationSources       map[uuid.UUID]entity.DocumentationSource
	policyEditProposals        map[uuid.UUID]entity.PolicyEditProposal
	policyOverrides            map[uuid.UUID]entity.PolicyOverride
	onboardingSignals          map[string]entity.OnboardingSignalReconciliation
	policyVersions             map[uuid.UUID]int64
	commandResults             map[string]entity.CommandResult
	events                     []entity.OutboxEvent
	wrapNotFound               bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		projects:                   map[uuid.UUID]entity.Project{},
		repositories:               map[uuid.UUID]entity.RepositoryBinding{},
		policies:                   map[uuid.UUID]entity.ServicesPolicy{},
		serviceDescriptors:         map[uuid.UUID][]entity.ServiceDescriptor{},
		policyDocumentationSources: map[uuid.UUID][]entity.DocumentationSource{},
		branchRules:                map[uuid.UUID]entity.BranchRules{},
		documentationSources:       map[uuid.UUID]entity.DocumentationSource{},
		policyEditProposals:        map[uuid.UUID]entity.PolicyEditProposal{},
		policyOverrides:            map[uuid.UUID]entity.PolicyOverride{},
		onboardingSignals:          map[string]entity.OnboardingSignalReconciliation{},
		policyVersions:             map[uuid.UUID]int64{},
		commandResults:             map[string]entity.CommandResult{},
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
		return entity.Project{}, r.notFound()
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

func (r *memoryRepository) ReserveRepositoryBinding(_ context.Context, repository entity.RepositoryBinding, event entity.OutboxEvent) error {
	for _, existing := range r.repositories {
		if existing.Status != enum.RepositoryStatusArchived &&
			existing.Provider == repository.Provider &&
			existing.ProviderOwner == repository.ProviderOwner &&
			existing.ProviderName == repository.ProviderName {
			return errs.ErrConflict
		}
	}
	r.repositories[repository.ID] = repository
	r.events = append(r.events, event)
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
		return entity.RepositoryBinding{}, r.notFound()
	}
	return repository, nil
}

func (r *memoryRepository) GetRepositoryByProviderRef(_ context.Context, provider enum.RepositoryProvider, owner string, name string) (entity.RepositoryBinding, error) {
	for _, repository := range r.repositories {
		if repository.Status != enum.RepositoryStatusArchived &&
			repository.Provider == provider &&
			repository.ProviderOwner == owner &&
			repository.ProviderName == name {
			return repository, nil
		}
	}
	return entity.RepositoryBinding{}, r.notFound()
}

func (r *memoryRepository) ListRepositories(context.Context, query.RepositoryFilter) ([]entity.RepositoryBinding, query.PageResult, error) {
	return nil, query.PageResult{}, nil
}

func (r *memoryRepository) RecordOnboardingSignalReconciliation(_ context.Context, signal entity.OnboardingSignalReconciliation) (entity.OnboardingSignalReconciliation, error) {
	if _, ok := r.repositories[signal.RepositoryID]; !ok {
		return entity.OnboardingSignalReconciliation{}, errs.ErrNotFound
	}
	key := signal.ProjectID.String() + ":" + string(signal.SignalKind) + ":" + signal.SignalKey
	if existing, ok := r.onboardingSignals[key]; ok {
		if existing.SignalFingerprint != signal.SignalFingerprint {
			return entity.OnboardingSignalReconciliation{}, errs.ErrConflict
		}
		signal.ID = existing.ID
		signal.CreatedAt = existing.CreatedAt
		signal.Version = existing.Version + 1
		if signal.ServicesPolicyID == nil {
			signal.ServicesPolicyID = existing.ServicesPolicyID
		}
		if signal.ServicesPolicyVersion == 0 {
			signal.ServicesPolicyVersion = existing.ServicesPolicyVersion
		}
		if signal.CompletedAt == nil {
			signal.CompletedAt = existing.CompletedAt
		}
	}
	r.onboardingSignals[key] = signal
	return signal, nil
}

func (r *memoryRepository) ImportServicesPolicy(_ context.Context, policy entity.ServicesPolicy, descriptors []entity.ServiceDescriptor, documentationSources []entity.DocumentationSource, result entity.CommandResult, buildEvent projectrepo.ServicesPolicyEventBuilder) (entity.ServicesPolicy, error) {
	r.policyVersions[policy.ProjectID]++
	policy.PolicyVersion = r.policyVersions[policy.ProjectID]
	event, err := buildEvent(policy)
	if err != nil {
		return entity.ServicesPolicy{}, err
	}
	r.policies[policy.ID] = policy
	r.serviceDescriptors[policy.ID] = descriptors
	r.policyDocumentationSources[policy.ID] = documentationSources
	r.events = append(r.events, event)
	r.commandResults[result.Key] = result
	return policy, nil
}

func (r *memoryRepository) ImportBootstrapServicesPolicy(
	_ context.Context,
	repository entity.RepositoryBinding,
	previousVersion int64,
	policy entity.ServicesPolicy,
	descriptors []entity.ServiceDescriptor,
	documentationSources []entity.DocumentationSource,
	repositoryEvent entity.OutboxEvent,
	result entity.CommandResult,
	buildPolicyEvent projectrepo.ServicesPolicyEventBuilder,
) (entity.ServicesPolicy, entity.RepositoryBinding, error) {
	current, ok := r.repositories[repository.ID]
	if !ok {
		return entity.ServicesPolicy{}, entity.RepositoryBinding{}, errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return entity.ServicesPolicy{}, entity.RepositoryBinding{}, errs.ErrConflict
	}
	r.policyVersions[policy.ProjectID]++
	policy.PolicyVersion = r.policyVersions[policy.ProjectID]
	policyEvent, err := buildPolicyEvent(policy)
	if err != nil {
		return entity.ServicesPolicy{}, entity.RepositoryBinding{}, err
	}
	r.repositories[repository.ID] = repository
	r.policies[policy.ID] = policy
	r.serviceDescriptors[policy.ID] = descriptors
	r.policyDocumentationSources[policy.ID] = documentationSources
	r.events = append(r.events, repositoryEvent, policyEvent)
	r.commandResults[result.Key] = result
	return policy, repository, nil
}

func (r *memoryRepository) GetServicesPolicy(_ context.Context, projectID uuid.UUID, policyID *uuid.UUID) (entity.ServicesPolicy, error) {
	if policyID != nil {
		policy, ok := r.policies[*policyID]
		if !ok {
			return entity.ServicesPolicy{}, r.notFound()
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
		return entity.ServicesPolicy{}, r.notFound()
	}
	return latest, nil
}

func (r *memoryRepository) GetServicesPolicyBySource(_ context.Context, projectID uuid.UUID, sourceRepositoryID uuid.UUID, sourcePath string, sourceCommitSHA string) (entity.ServicesPolicy, error) {
	var latest entity.ServicesPolicy
	for _, policy := range r.policies {
		if policy.ProjectID != projectID ||
			policy.SourceRepositoryID == nil ||
			*policy.SourceRepositoryID != sourceRepositoryID ||
			policy.SourcePath != sourcePath ||
			policy.SourceCommitSHA != sourceCommitSHA {
			continue
		}
		if policy.PolicyVersion > latest.PolicyVersion {
			latest = policy
		}
	}
	if latest.ID == uuid.Nil {
		return entity.ServicesPolicy{}, r.notFound()
	}
	return latest, nil
}

func (r *memoryRepository) notFound() error {
	if r.wrapNotFound {
		return errors.Join(errs.ErrNotFound, errors.New("wrapped not found"))
	}
	return errs.ErrNotFound
}

func (r *memoryRepository) ListServiceDescriptors(_ context.Context, filter query.ServiceDescriptorFilter) ([]entity.ServiceDescriptor, query.PageResult, error) {
	policy, err := r.GetServicesPolicy(context.Background(), filter.ProjectID, nil)
	if err != nil {
		return nil, query.PageResult{}, err
	}
	descriptors := r.serviceDescriptors[policy.ID]
	result := make([]entity.ServiceDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if filter.RepositoryID != nil && (descriptor.RepositoryID == nil || *descriptor.RepositoryID != *filter.RepositoryID) {
			continue
		}
		if len(filter.ServiceKeys) > 0 && !containsString(filter.ServiceKeys, descriptor.ServiceKey) {
			continue
		}
		if len(filter.Statuses) > 0 && !containsServiceStatus(filter.Statuses, descriptor.Status) {
			continue
		}
		result = append(result, descriptor)
	}
	return result, query.PageResult{}, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsServiceStatus(values []enum.ServiceStatus, target enum.ServiceStatus) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func (r *memoryRepository) CancelPolicyOverride(_ context.Context, override entity.PolicyOverride, _ int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	r.policyOverrides[override.ID] = override
	r.events = append(r.events, event)
	if result != nil {
		r.commandResults[result.Key] = *result
	}
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

func (r *memoryRepository) PutDocumentationSource(_ context.Context, source entity.DocumentationSource, _ *int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	r.documentationSources[source.ID] = source
	r.events = append(r.events, event)
	if result != nil {
		r.commandResults[result.Key] = *result
	}
	return nil
}

func (r *memoryRepository) GetDocumentationSource(_ context.Context, id uuid.UUID) (entity.DocumentationSource, error) {
	source, ok := r.documentationSources[id]
	if !ok {
		return entity.DocumentationSource{}, errs.ErrNotFound
	}
	return source, nil
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
