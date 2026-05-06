package project

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}

	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		query, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(query) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: "23505"}, want: errs.ErrAlreadyExists},
		{name: "foreign key", err: &pgconn.PgError{Code: "23503"}, want: errs.ErrPreconditionFailed},
		{name: "check", err: &pgconn.PgError{Code: "23514"}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
			var pgErr *pgconn.PgError
			if errors.As(tc.err, &pgErr) && !errors.As(wrapError("test operation", tc.err), &pgErr) {
				t.Fatalf("wrapError() lost postgres cause")
			}
		})
	}
}

func TestRepositoryIntegrationProjectsRepositoriesPoliciesAndOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	organizationID := uuid.New()
	projectA := testProject(organizationID, "billing", "Биллинг", now)
	projectB := testProject(organizationID, "portal", "Портал", now)

	commandID := uuid.New()
	if err := repository.CreateProject(
		ctx,
		projectA,
		testEvent("project.project.created", "project", projectA.ID, now),
		testCommandResult(commandID, operationCreateProject, "project", projectA.ID, now),
	); err != nil {
		t.Fatalf("create project A: %v", err)
	}
	if err := repository.CreateProject(
		ctx,
		projectB,
		testEvent("project.project.created", "project", projectB.ID, now),
		testCommandResult(uuid.New(), operationCreateProject, "project", projectB.ID, now),
	); err != nil {
		t.Fatalf("create project B: %v", err)
	}

	storedProject, err := repository.GetProject(ctx, projectA.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if storedProject.Slug != projectA.Slug || storedProject.IconObjectURI != projectA.IconObjectURI {
		t.Fatalf("stored project = %+v, want slug %s icon %s", storedProject, projectA.Slug, projectA.IconObjectURI)
	}
	projects, page, err := repository.ListProjects(ctx, query.ProjectFilter{
		OrganizationID: &organizationID,
		Statuses:       []enum.ProjectStatus{enum.ProjectStatusActive},
		Page:           value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || page.NextPageToken == "" {
		t.Fatalf("paged projects = %d token %q, want one item and next token", len(projects), page.NextPageToken)
	}
	result, err := repository.GetCommandResult(ctx, query.CommandIdentity{CommandID: commandID})
	if err != nil {
		t.Fatalf("get command result: %v", err)
	}
	if result.AggregateID != projectA.ID || !jsonEqual(result.ResultPayload, []byte(`{"ok":true}`)) {
		t.Fatalf("command result = %+v, want aggregate %s and payload", result, projectA.ID)
	}

	conflictingProject := testProject(organizationID, "billing-conflict", "Conflict", now)
	err = repository.CreateProject(
		ctx,
		conflictingProject,
		testEvent("project.project.created", "project", conflictingProject.ID, now),
		testCommandResult(commandID, operationCreateProject, "project", conflictingProject.ID, now),
	)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate command err = %v, want %v", err, errs.ErrConflict)
	}

	updatedProject := projectA
	updatedProject.DisplayName = "Биллинг и платежи"
	updatedProject.Version = 2
	updatedProject.UpdatedAt = now.Add(time.Minute)
	if err := repository.UpdateProject(ctx, updatedProject, projectA.Version, testEvent("project.project.updated", "project", projectA.ID, updatedProject.UpdatedAt), nil); err != nil {
		t.Fatalf("update project: %v", err)
	}
	if err := repository.UpdateProject(ctx, updatedProject, projectA.Version, testEvent("project.project.updated", "project", projectA.ID, updatedProject.UpdatedAt), nil); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale project update err = %v, want %v", err, errs.ErrConflict)
	}

	repositoryA := testRepository(projectA.ID, "billing-api", now)
	repositoryB := testRepository(projectA.ID, "billing-worker", now)
	if err := repository.AttachRepository(ctx, repositoryA, testEvent("project.repository.attached", "repository", repositoryA.ID, now), testCommandResult(uuid.New(), operationAttachRepository, "repository", repositoryA.ID, now)); err != nil {
		t.Fatalf("attach repository A: %v", err)
	}
	if err := repository.AttachRepository(ctx, repositoryB, testEvent("project.repository.attached", "repository", repositoryB.ID, now), testCommandResult(uuid.New(), operationAttachRepository, "repository", repositoryB.ID, now)); err != nil {
		t.Fatalf("attach repository B: %v", err)
	}
	repositories, _, err := repository.ListRepositories(ctx, query.RepositoryFilter{ProjectID: projectA.ID, Statuses: []enum.RepositoryStatus{enum.RepositoryStatusActive}})
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}
	if len(repositories) != 2 {
		t.Fatalf("repositories = %d, want 2", len(repositories))
	}

	policy := testServicesPolicy(projectA.ID, repositoryA.ID, now)
	descriptors := []entity.ServiceDescriptor{
		testServiceDescriptor(projectA.ID, policy.ID, &repositoryA.ID, "api", enum.ServiceKindBackend, now),
		testServiceDescriptor(projectA.ID, policy.ID, &repositoryB.ID, "worker", enum.ServiceKindWorker, now),
	}
	policy, err = repository.ImportServicesPolicy(ctx, policy, descriptors, testCommandResult(uuid.New(), operationImportServicesPolicy, "services_policy", policy.ID, now), testServicesPolicyEvent(now))
	if err != nil {
		t.Fatalf("import services policy: %v", err)
	}
	activePolicy, err := repository.GetServicesPolicy(ctx, projectA.ID, nil)
	if err != nil {
		t.Fatalf("get active services policy: %v", err)
	}
	if activePolicy.ID != policy.ID || activePolicy.SourceCommitSHA == "" {
		t.Fatalf("active policy = %+v, want %s", activePolicy, policy.ID)
	}
	services, _, err := repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID:    projectA.ID,
		RepositoryID: &repositoryA.ID,
		Statuses:     []enum.ServiceStatus{enum.ServiceStatusActive},
	})
	if err != nil {
		t.Fatalf("list service descriptors: %v", err)
	}
	if len(services) != 1 || services[0].ServiceKey != "api" {
		t.Fatalf("services = %+v, want api only", services)
	}

	docSource := testDocumentationSource(projectA.ID, &repositoryA.ID, "service", "api", now)
	if err := repository.PutDocumentationSource(ctx, docSource, nil, testEvent("project.documentation_source.created", "documentation_source", docSource.ID, now), nil); err != nil {
		t.Fatalf("put documentation source: %v", err)
	}
	docSourcePreviousVersion := docSource.Version
	docSource.Version = 2
	docSource.UpdatedAt = now.Add(time.Minute)
	docSource.LocalPath = "docs/api-v2"
	if err := repository.PutDocumentationSource(ctx, docSource, &docSourcePreviousVersion, testEvent("project.documentation_source.updated", "documentation_source", docSource.ID, docSource.UpdatedAt), nil); err != nil {
		t.Fatalf("update documentation source: %v", err)
	}
	docSource.Version = 3
	if err := repository.PutDocumentationSource(ctx, docSource, &docSourcePreviousVersion, testEvent("project.documentation_source.updated", "documentation_source", docSource.ID, docSource.UpdatedAt), nil); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale documentation source update error = %v, want conflict", err)
	}
	workspacePolicy, err := repository.GetWorkspacePolicy(ctx, query.WorkspacePolicyFilter{
		ProjectID:               projectA.ID,
		RepositoryIDs:           []uuid.UUID{repositoryA.ID},
		ServiceKeys:             []string{"api"},
		IncludeGuidancePackages: true,
	})
	if err != nil {
		t.Fatalf("get workspace policy: %v", err)
	}
	if len(workspacePolicy.CodeSources) != 1 || len(workspacePolicy.DocumentationSources) != 1 || workspacePolicy.PolicyVersion != policy.PolicyVersion {
		t.Fatalf("workspace policy = %+v, want one code source, one doc source and policy version %d", workspacePolicy, policy.PolicyVersion)
	}
	workspaceByServiceOnly, err := repository.GetWorkspacePolicy(ctx, query.WorkspacePolicyFilter{
		ProjectID:   projectA.ID,
		ServiceKeys: []string{"api"},
	})
	if err != nil {
		t.Fatalf("get workspace policy by service: %v", err)
	}
	if len(workspaceByServiceOnly.CodeSources) != 1 || workspaceByServiceOnly.CodeSources[0].RepositoryID != repositoryA.ID {
		t.Fatalf("workspace by service code sources = %+v, want repository %s", workspaceByServiceOnly.CodeSources, repositoryA.ID)
	}

	invalidPolicy := testServicesPolicy(projectA.ID, repositoryA.ID, now.Add(2*time.Minute))
	invalidPolicy.ID = uuid.New()
	invalidPolicy.ValidationStatus = enum.ServicesPolicyValidationInvalid
	invalidPolicy.ProjectionStatus = enum.ServicesPolicyProjectionFailed
	invalidPolicy, err = repository.ImportServicesPolicy(ctx, invalidPolicy, nil, testCommandResult(uuid.New(), operationImportServicesPolicy, "services_policy", invalidPolicy.ID, invalidPolicy.ImportedAt), testServicesPolicyEvent(invalidPolicy.ImportedAt))
	if err != nil {
		t.Fatalf("import invalid services policy: %v", err)
	}
	if invalidPolicy.PolicyVersion != policy.PolicyVersion+1 {
		t.Fatalf("invalid policy version = %d, want %d", invalidPolicy.PolicyVersion, policy.PolicyVersion+1)
	}
	activeAfterInvalid, err := repository.GetServicesPolicy(ctx, projectA.ID, nil)
	if err != nil {
		t.Fatalf("get active services policy after invalid import: %v", err)
	}
	if activeAfterInvalid.ID != policy.ID {
		t.Fatalf("active policy after invalid import = %s, want %s", activeAfterInvalid.ID, policy.ID)
	}
	activeDescriptorsAfterInvalid, _, err := repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{ProjectID: projectA.ID, Statuses: []enum.ServiceStatus{enum.ServiceStatusActive}})
	if err != nil {
		t.Fatalf("list descriptors after invalid import: %v", err)
	}
	if len(activeDescriptorsAfterInvalid) != 2 {
		t.Fatalf("active descriptors after invalid import = %d, want 2", len(activeDescriptorsAfterInvalid))
	}
	if got := countTableRows(t, ctx, pool, "project_catalog_outbox_events"); got < 7 {
		t.Fatalf("outbox events = %d, want at least 7", got)
	}
}

func TestRepositoryIntegrationImportServicesPolicyBatchesDescriptors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	project := testProject(uuid.New(), "batch-policy", "Batch policy", now)
	if err := repository.CreateProject(ctx, project, testEvent("project.project.created", "project", project.ID, now), testCommandResult(uuid.New(), operationCreateProject, "project", project.ID, now)); err != nil {
		t.Fatalf("create project: %v", err)
	}

	repositoryAPI := testRepository(project.ID, "batch-api", now)
	repositoryWorker := testRepository(project.ID, "batch-worker", now)
	repositoryFrontend := testRepository(project.ID, "batch-frontend", now)
	for _, binding := range []entity.RepositoryBinding{repositoryAPI, repositoryWorker, repositoryFrontend} {
		if err := repository.AttachRepository(ctx, binding, testEvent("project.repository.attached", "repository", binding.ID, now), testCommandResult(uuid.New(), operationAttachRepository, "repository", binding.ID, now)); err != nil {
			t.Fatalf("attach repository %s: %v", binding.ProviderName, err)
		}
	}

	policy := testServicesPolicy(project.ID, repositoryAPI.ID, now)
	descriptors := []entity.ServiceDescriptor{
		testServiceDescriptor(project.ID, policy.ID, &repositoryAPI.ID, "api", enum.ServiceKindBackend, now),
		testServiceDescriptor(project.ID, policy.ID, &repositoryWorker.ID, "worker", enum.ServiceKindWorker, now),
		testServiceDescriptor(project.ID, policy.ID, &repositoryFrontend.ID, "frontend", enum.ServiceKindFrontend, now),
	}
	if _, err := repository.ImportServicesPolicy(ctx, policy, descriptors, testCommandResult(uuid.New(), operationImportServicesPolicy, "services_policy", policy.ID, now), testServicesPolicyEvent(now)); err != nil {
		t.Fatalf("import services policy: %v", err)
	}

	services, _, err := repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID: project.ID,
		Statuses:  []enum.ServiceStatus{enum.ServiceStatusActive},
	})
	if err != nil {
		t.Fatalf("list service descriptors: %v", err)
	}
	if len(services) != len(descriptors) {
		t.Fatalf("service descriptors = %d, want %d", len(services), len(descriptors))
	}
	servicesByKey := make(map[string]entity.ServiceDescriptor, len(services))
	for _, service := range services {
		servicesByKey[service.ServiceKey] = service
	}
	for _, descriptor := range descriptors {
		service, ok := servicesByKey[descriptor.ServiceKey]
		if !ok {
			t.Fatalf("descriptor %s was not persisted", descriptor.ServiceKey)
		}
		if service.ID != descriptor.ID || service.RepositoryID == nil || *service.RepositoryID != *descriptor.RepositoryID {
			t.Fatalf("descriptor %s = %+v, want id %s repository %s", descriptor.ServiceKey, service, descriptor.ID, *descriptor.RepositoryID)
		}
	}
	if got := countTableRows(t, ctx, pool, "project_catalog_service_descriptors"); got != len(descriptors) {
		t.Fatalf("project_catalog_service_descriptors rows = %d, want %d", got, len(descriptors))
	}
}

func TestRepositoryIntegrationPoliciesAndRules(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	project := testProject(uuid.New(), "catalog", "Каталог", now)
	if err := repository.CreateProject(ctx, project, testEvent("project.project.created", "project", project.ID, now), testCommandResult(uuid.New(), operationCreateProject, "project", project.ID, now)); err != nil {
		t.Fatalf("create project: %v", err)
	}
	repositoryBinding := testRepository(project.ID, "catalog-api", now)
	if err := repository.AttachRepository(ctx, repositoryBinding, testEvent("project.repository.attached", "repository", repositoryBinding.ID, now), testCommandResult(uuid.New(), operationAttachRepository, "repository", repositoryBinding.ID, now)); err != nil {
		t.Fatalf("attach repository: %v", err)
	}

	branchRules := entity.BranchRules{
		Base:           entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:      project.ID,
		RepositoryID:   &repositoryBinding.ID,
		Pattern:        "release/*",
		RequiredChecks: []string{"unit", "lint"},
		MergePolicy:    enum.MergePolicySquash,
		Status:         enum.BranchRulesStatusActive,
	}
	if err := repository.PutBranchRules(ctx, branchRules, nil, testEvent("project.branch_rules.created", "branch_rules", branchRules.ID, now), nil); err != nil {
		t.Fatalf("put branch rules: %v", err)
	}
	releasePolicy := entity.ReleasePolicy{
		Base:            entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:       project.ID,
		Name:            "stable",
		BranchPattern:   "release/*",
		RolloutStrategy: enum.RolloutStrategyCanary,
		RollbackPolicy:  enum.RollbackPolicyAutomaticOnAlert,
		RiskProfileRef:  "standard",
		Status:          enum.ReleasePolicyStatusActive,
	}
	if err := repository.PutReleasePolicy(ctx, releasePolicy, nil, testEvent("project.release_policy.created", "release_policy", releasePolicy.ID, now), nil); err != nil {
		t.Fatalf("put release policy: %v", err)
	}
	releaseLine := entity.ReleaseLine{
		Base:            entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:       project.ID,
		ReleasePolicyID: releasePolicy.ID,
		Name:            "2026.05",
		BranchPattern:   "release/2026.05",
		Status:          enum.ReleasePolicyStatusActive,
	}
	if err := repository.PutReleaseLine(ctx, releaseLine, nil, testEvent("project.release_line.created", "release_line", releaseLine.ID, now), nil); err != nil {
		t.Fatalf("put release line: %v", err)
	}
	placement := entity.PlacementPolicy{
		Base:               entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:          project.ID,
		RepositoryID:       &repositoryBinding.ID,
		ServiceKey:         "api",
		AllowedClusterRefs: []string{"prod-eu"},
		Status:             enum.PlacementPolicyStatusActive,
	}
	if err := repository.PutPlacementPolicy(ctx, placement, nil, testEvent("project.placement_policy.created", "placement_policy", placement.ID, now), nil); err != nil {
		t.Fatalf("put placement policy: %v", err)
	}
	branchPreviousVersion := branchRules.Version
	branchRules.Version = 2
	branchRules.UpdatedAt = now.Add(time.Minute)
	branchRules.RequiredChecks = []string{"unit", "lint", "integration"}
	if err := repository.PutBranchRules(ctx, branchRules, &branchPreviousVersion, testEvent("project.branch_rules.updated", "branch_rules", branchRules.ID, branchRules.UpdatedAt), nil); err != nil {
		t.Fatalf("update branch rules: %v", err)
	}
	releasePolicyPreviousVersion := releasePolicy.Version
	releasePolicy.Version = 2
	releasePolicy.UpdatedAt = now.Add(time.Minute)
	releasePolicy.RolloutStrategy = enum.RolloutStrategyStaged
	if err := repository.PutReleasePolicy(ctx, releasePolicy, &releasePolicyPreviousVersion, testEvent("project.release_policy.updated", "release_policy", releasePolicy.ID, releasePolicy.UpdatedAt), nil); err != nil {
		t.Fatalf("update release policy: %v", err)
	}
	releaseLinePreviousVersion := releaseLine.Version
	releaseLine.Version = 2
	releaseLine.UpdatedAt = now.Add(time.Minute)
	releaseLine.Status = enum.ReleasePolicyStatusArchived
	if err := repository.PutReleaseLine(ctx, releaseLine, &releaseLinePreviousVersion, testEvent("project.release_line.archived", "release_line", releaseLine.ID, releaseLine.UpdatedAt), nil); err != nil {
		t.Fatalf("update release line: %v", err)
	}
	placementPreviousVersion := placement.Version
	placement.Version = 2
	placement.UpdatedAt = now.Add(time.Minute)
	placement.AllowedClusterRefs = []string{"prod-eu", "prod-us"}
	if err := repository.PutPlacementPolicy(ctx, placement, &placementPreviousVersion, testEvent("project.placement_policy.updated", "placement_policy", placement.ID, placement.UpdatedAt), nil); err != nil {
		t.Fatalf("update placement policy: %v", err)
	}
	placement.Version = 3
	if err := repository.PutPlacementPolicy(ctx, placement, &placementPreviousVersion, testEvent("project.placement_policy.updated", "placement_policy", placement.ID, placement.UpdatedAt), nil); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale placement policy update error = %v, want conflict", err)
	}
	activeOverrideAt := time.Now().UTC()
	override := entity.PolicyOverride{
		Base:              entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:         project.ID,
		TargetType:        enum.PolicyOverrideTargetPlacementPolicy,
		TargetID:          &placement.ID,
		Payload:           []byte(`{"allowed_cluster_refs":["hotfix"]}`),
		Reason:            "hotfix",
		Status:            enum.PolicyOverrideStatusActive,
		ExpiresAt:         activeOverrideAt.Add(time.Hour),
		CreatedByActorRef: "user:owner",
	}
	if err := repository.CreatePolicyOverride(ctx, override, testEvent("project.policy_override.created", "policy_override", override.ID, now), testCommandResult(uuid.New(), operationCreatePolicyOverride, "policy_override", override.ID, now)); err != nil {
		t.Fatalf("create policy override: %v", err)
	}
	activeOverrides, _, err := repository.ListPolicyOverrides(ctx, query.PolicyOverrideFilter{
		ProjectID:   project.ID,
		TargetTypes: []enum.PolicyOverrideTargetType{enum.PolicyOverrideTargetPlacementPolicy},
		TargetID:    &placement.ID,
		ActiveOnly:  true,
		ActiveAt:    &activeOverrideAt,
	})
	if err != nil {
		t.Fatalf("list active policy overrides: %v", err)
	}
	if len(activeOverrides) != 1 || activeOverrides[0].ID != override.ID || !jsonEqual(activeOverrides[0].Payload, override.Payload) {
		t.Fatalf("active policy overrides = %+v, want override %s", activeOverrides, override.ID)
	}
	workspace, err := repository.GetWorkspacePolicy(ctx, query.WorkspacePolicyFilter{ProjectID: project.ID})
	if err != nil {
		t.Fatalf("get workspace policy with overrides: %v", err)
	}
	if len(workspace.ActivePolicyOverrides) != 1 || workspace.ActivePolicyOverrides[0].ID != override.ID {
		t.Fatalf("workspace active policy overrides = %+v, want override %s", workspace.ActivePolicyOverrides, override.ID)
	}
	proposal := entity.PolicyEditProposal{
		ID:           uuid.New(),
		ProjectID:    project.ID,
		RepositoryID: repositoryBinding.ID,
		SourcePath:   "services.yaml",
		RequestedChanges: value.PolicyEditProposalRequestedChanges{
			Summary: "Добавить сервис",
			Changes: []value.PolicyEditProposalChange{
				{
					Type:        value.PolicyEditProposalChangeAddService,
					Target:      "services.billing-api",
					Description: "Добавить backend-сервис биллинга.",
				},
			},
		},
		Status:    "pending",
		CreatedAt: now,
	}
	if err := repository.CreatePolicyEditProposal(ctx, proposal, testCommandResult(uuid.New(), operationCreatePolicyEditProposal, "policy_edit_proposal", proposal.ID, now)); err != nil {
		t.Fatalf("create policy edit proposal: %v", err)
	}

	branchRulesItems, _, err := repository.ListBranchRules(ctx, query.BranchRulesFilter{ProjectID: project.ID, RepositoryID: &repositoryBinding.ID, Statuses: []enum.BranchRulesStatus{enum.BranchRulesStatusActive}})
	if err != nil {
		t.Fatalf("list branch rules: %v", err)
	}
	releasePolicies, _, err := repository.ListReleasePolicies(ctx, query.ReleasePolicyFilter{ProjectID: project.ID, Statuses: []enum.ReleasePolicyStatus{enum.ReleasePolicyStatusActive}})
	if err != nil {
		t.Fatalf("list release policies: %v", err)
	}
	releaseLines, _, err := repository.ListReleaseLines(ctx, query.ReleaseLineFilter{ProjectID: project.ID, ReleasePolicyID: &releasePolicy.ID, Statuses: []enum.ReleasePolicyStatus{enum.ReleasePolicyStatusArchived}})
	if err != nil {
		t.Fatalf("list release lines: %v", err)
	}
	placements, _, err := repository.ListPlacementPolicies(ctx, query.PlacementPolicyFilter{ProjectID: project.ID, RepositoryID: &repositoryBinding.ID, ServiceKey: "api", Statuses: []enum.PlacementPolicyStatus{enum.PlacementPolicyStatusActive}})
	if err != nil {
		t.Fatalf("list placements: %v", err)
	}
	if len(branchRulesItems) != 1 || len(releasePolicies) != 1 || len(releaseLines) != 1 || len(placements) != 1 {
		t.Fatalf("lists sizes = branch %d release policy %d release line %d placement %d, want all one", len(branchRulesItems), len(releasePolicies), len(releaseLines), len(placements))
	}
}

func TestRepositoryIntegrationOutboxClaimRetryAndPublish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	project := testProject(uuid.New(), "outbox", "Outbox", now)
	if err := repository.CreateProject(ctx, project, testEvent("project.project.created", "project", project.ID, now), testCommandResult(uuid.New(), operationCreateProject, "project", project.ID, now)); err != nil {
		t.Fatalf("create project: %v", err)
	}

	claimed, err := repository.ClaimOutboxEvents(ctx, 10, now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed events = %d, want 1", len(claimed))
	}
	event := claimed[0]
	if event.AttemptCount != 1 || event.LockedUntil == nil || !event.LockedUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("claimed event lock = attempt %d until %v, want attempt 1 until %s", event.AttemptCount, event.LockedUntil, now.Add(time.Minute))
	}
	reclaimed, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(time.Second), now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("claim locked outbox events: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("locked events claimed = %d, want 0", len(reclaimed))
	}

	nextAttemptAt := now.Add(5 * time.Minute)
	if err := repository.MarkOutboxEventFailed(ctx, event.ID, event.AttemptCount, nextAttemptAt, "temporary failure"); err != nil {
		t.Fatalf("mark outbox event failed: %v", err)
	}
	retried, err := repository.ClaimOutboxEvents(ctx, 10, nextAttemptAt, nextAttemptAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim retry outbox events: %v", err)
	}
	if len(retried) != 1 || retried[0].AttemptCount != 2 {
		t.Fatalf("retried events = %+v, want one attempt 2", retried)
	}
	if err := repository.MarkOutboxEventPublished(ctx, retried[0].ID, retried[0].AttemptCount, nextAttemptAt.Add(time.Second)); err != nil {
		t.Fatalf("mark outbox event published: %v", err)
	}
	afterPublish, err := repository.ClaimOutboxEvents(ctx, 10, nextAttemptAt.Add(2*time.Minute), nextAttemptAt.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("claim after publish: %v", err)
	}
	if len(afterPublish) != 0 {
		t.Fatalf("published events claimed = %d, want 0", len(afterPublish))
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set KODEX_PROJECT_CATALOG_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "project_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	applyMigrations(t, ctx, pool)
	return pool
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	files, err := filepath.Glob("../../../../cmd/cli/migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		for _, statement := range splitSQLStatements(upMigrationSQL(t, string(content), file)) {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("apply migration %s statement %q: %v", file, statement, err)
			}
		}
	}
}

func upMigrationSQL(t *testing.T, content string, file string) string {
	t.Helper()

	upIndex := strings.Index(content, "-- +goose Up")
	downIndex := strings.Index(content, "-- +goose Down")
	if upIndex < 0 || downIndex < 0 || downIndex < upIndex {
		t.Fatalf("invalid goose migration markers in %s", file)
	}
	return content[upIndex+len("-- +goose Up") : downIndex]
}

func splitSQLStatements(content string) []string {
	parts := strings.Split(content, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func countTableRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func testProject(organizationID uuid.UUID, slug string, displayName string, now time.Time) entity.Project {
	return entity.Project{
		Base:           entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		OrganizationID: organizationID,
		Slug:           slug,
		DisplayName:    displayName,
		Description:    "Тестовый проект",
		IconObjectURI:  "s3://kodex-icons/" + slug + ".png",
		Status:         enum.ProjectStatusActive,
	}
}

func testRepository(projectID uuid.UUID, name string, now time.Time) entity.RepositoryBinding {
	return entity.RepositoryBinding{
		Base:                 entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:            projectID,
		Provider:             enum.RepositoryProviderGitHub,
		ProviderOwner:        "codex-k8s",
		ProviderName:         name,
		WebURL:               "https://github.com/codex-k8s/" + name,
		DefaultBranch:        "main",
		Status:               enum.RepositoryStatusActive,
		ProviderRepositoryID: uuid.NewString(),
		IconObjectURI:        "s3://kodex-icons/" + name + ".png",
	}
}

func testServicesPolicy(projectID uuid.UUID, repositoryID uuid.UUID, now time.Time) entity.ServicesPolicy {
	return entity.ServicesPolicy{
		Base:               entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:          projectID,
		SourceRepositoryID: &repositoryID,
		SourcePath:         "services.yaml",
		SourceRef:          "main",
		SourceCommitSHA:    "0123456789abcdef0123456789abcdef01234567",
		SourceBlobSHA:      "abcdef0123456789abcdef0123456789abcdef01",
		PolicyVersion:      1,
		ContentHash:        "sha256:test",
		ValidatedPayload:   []byte(`{"services":["api","worker"]}`),
		ValidationStatus:   enum.ServicesPolicyValidationValid,
		ProjectionStatus:   enum.ServicesPolicyProjectionSynced,
		ImportedAt:         now,
	}
}

func testServiceDescriptor(projectID uuid.UUID, policyID uuid.UUID, repositoryID *uuid.UUID, key string, kind enum.ServiceKind, now time.Time) entity.ServiceDescriptor {
	return entity.ServiceDescriptor{
		Base:                 entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:            projectID,
		ServicesPolicyID:     policyID,
		RepositoryID:         repositoryID,
		ServiceKey:           key,
		DisplayName:          key,
		Kind:                 kind,
		RootPath:             "services/" + key,
		DocumentationScopeID: key,
		DependsOnServiceKeys: []string{},
		Status:               enum.ServiceStatusActive,
	}
}

func testDocumentationSource(projectID uuid.UUID, repositoryID *uuid.UUID, scopeType string, scopeID string, now time.Time) entity.DocumentationSource {
	return entity.DocumentationSource{
		Base:         entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		ScopeType:    enum.DocumentationScopeType(scopeType),
		ScopeID:      scopeID,
		LocalPath:    "docs/" + scopeID,
		AccessMode:   enum.DocumentationAccessWrite,
		Status:       enum.DocumentationSourceStatusActive,
	}
}

func testEvent(eventType string, aggregateType string, aggregateID uuid.UUID, occurredAt time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{
		ID:            uuid.New(),
		EventType:     eventType,
		SchemaVersion: 1,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       []byte(`{"ok":true}`),
		OccurredAt:    occurredAt,
	}
}

func testServicesPolicyEvent(occurredAt time.Time) func(entity.ServicesPolicy) (entity.OutboxEvent, error) {
	return func(policy entity.ServicesPolicy) (entity.OutboxEvent, error) {
		payload, err := json.Marshal(value.ProjectEventPayload{
			ProjectID:     policy.ProjectID.String(),
			PolicyID:      policy.ID.String(),
			PolicyVersion: policy.PolicyVersion,
		})
		if err != nil {
			return entity.OutboxEvent{}, err
		}
		return entity.OutboxEvent{
			ID:            uuid.New(),
			EventType:     "project.services_policy.imported",
			SchemaVersion: 1,
			AggregateType: "services_policy",
			AggregateID:   policy.ID,
			Payload:       payload,
			OccurredAt:    occurredAt,
		}, nil
	}
}

func testCommandResult(commandID uuid.UUID, operation string, aggregateType string, aggregateID uuid.UUID, createdAt time.Time) entity.CommandResult {
	return entity.CommandResult{
		Key:           operation + ":" + commandID.String(),
		CommandID:     commandID,
		Operation:     operation,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		ResultPayload: []byte(`{"ok":true}`),
		CreatedAt:     createdAt,
	}
}

func jsonEqual(left []byte, right []byte) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
