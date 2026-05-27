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
		return entity.RepositoryBinding{}, errs.ErrNotFound
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
	return entity.RepositoryBinding{}, errs.ErrNotFound
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
