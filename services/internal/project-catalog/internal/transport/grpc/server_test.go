package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateProjectCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	organizationID := uuid.New()
	commandID := uuid.New()
	service := &fakeProjectService{}
	service.createProject = func(_ context.Context, input projectservice.CreateProjectInput) (entity.Project, error) {
		if input.OrganizationID != organizationID || input.Slug != "payments" || input.DisplayName != "Платежи" {
			t.Fatalf("input = %+v, want project create fields", input)
		}
		if input.Meta.CommandID != commandID || input.Meta.Actor.Type != "user" || input.Meta.Actor.ID != "owner" {
			t.Fatalf("meta = %+v, want command id and actor", input.Meta)
		}
		return entity.Project{
			Base: entity.Base{
				ID:      projectID,
				Version: 1,
			},
			OrganizationID: organizationID,
			Slug:           input.Slug,
			DisplayName:    input.DisplayName,
			Status:         enum.ProjectStatusActive,
		}, nil
	}
	server := NewServer(service)

	response, err := server.CreateProject(context.Background(), &projectsv1.CreateProjectRequest{
		OrganizationId: organizationID.String(),
		Slug:           "payments",
		DisplayName:    "Платежи",
		Meta:           commandMeta(commandID.String()),
	})
	if err != nil {
		t.Fatalf("CreateProject(): %v", err)
	}
	project := response.GetProject()
	if project.GetProjectId() != projectID.String() || project.GetStatus() != projectsv1.ProjectStatus_PROJECT_STATUS_ACTIVE {
		t.Fatalf("project response = %+v, want created project", project)
	}
}

func TestCreateRepositoryBootstrapPullRequestCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	service := &fakeProjectService{}
	service.createBootstrap = func(_ context.Context, input projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error) {
		if input.ProjectID != projectID || input.RepositoryID != repositoryID || input.ExternalAccountID != externalAccountID {
			t.Fatalf("ids = %s/%s/%s, want request ids", input.ProjectID, input.RepositoryID, input.ExternalAccountID)
		}
		if input.BaseBranch != "main" || input.BootstrapBranch != "kodex/bootstrap" || len(input.Files) != 1 {
			t.Fatalf("bootstrap input = %+v, want branch and file fields", input)
		}
		if input.ServicesPolicy.SourcePath != "services.yaml" || input.ServicesPolicy.ContentHash != "sha256:policy" {
			t.Fatalf("services policy = %+v, want checked policy fields", input.ServicesPolicy)
		}
		return projectservice.RepositoryBootstrapPullRequestResult{
			Repository: entity.RepositoryBinding{
				Base:          entity.Base{ID: repositoryID, Version: 1},
				ProjectID:     projectID,
				Provider:      enum.RepositoryProviderGitHub,
				ProviderOwner: "codex-k8s",
				ProviderName:  "kodex",
				DefaultBranch: "main",
				Status:        enum.RepositoryStatusPending,
			},
			ProviderTarget: projectservice.RepositoryBootstrapProviderTarget{
				ProviderSlug:       "github",
				RepositoryFullName: "codex-k8s/kodex",
			},
			BaseBranch:      "main",
			BootstrapBranch: "kodex/bootstrap",
			ServicesPolicy: projectservice.RepositoryBootstrapServicesPolicy{
				SourcePath:  "services.yaml",
				ContentHash: "sha256:policy",
			},
			ProviderResult: projectservice.RepositoryBootstrapProviderResult{
				ProviderOperationID:          "operation-1",
				ProviderWorkItemProjectionID: "projection-1",
				ProviderWebURL:               "https://example.test/pull/1",
			},
		}, nil
	}
	server := NewServer(service)

	response, err := server.CreateRepositoryBootstrapPullRequest(context.Background(), &projectsv1.CreateRepositoryBootstrapPullRequestRequest{
		ProjectId:       projectID.String(),
		RepositoryId:    repositoryID.String(),
		BaseBranch:      "main",
		BootstrapBranch: "kodex/bootstrap",
		CommitMessage:   "Bootstrap repository",
		Title:           "Bootstrap repository",
		Files: []*projectsv1.RepositoryBootstrapFile{{
			Path:    "services.yaml",
			Content: "services:\n",
		}},
		WatermarkJson: `{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"test"}`,
		ServicesPolicy: &projectsv1.RepositoryBootstrapServicesPolicy{
			SourcePath:           "services.yaml",
			ContentHash:          "sha256:policy",
			ValidatedPayloadJson: `{"spec":{"services":[{"key":"api","rootPath":"services/api"}]}}`,
		},
		ExternalAccountId: externalAccountID.String(),
		Meta:              commandMeta(commandID.String()),
	})
	if err != nil {
		t.Fatalf("CreateRepositoryBootstrapPullRequest(): %v", err)
	}
	if response.GetProviderTarget().GetRepositoryFullName() != "codex-k8s/kodex" ||
		response.GetProviderOperationId() != "operation-1" ||
		response.GetProviderWebUrl() != "https://example.test/pull/1" {
		t.Fatalf("response = %+v, want provider refs", response)
	}
}

func TestCreateProviderRepositoryCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	externalAccountID := uuid.New()
	commandID := uuid.New()
	service := &fakeProjectService{}
	service.createProviderRepository = func(_ context.Context, input projectservice.CreateProviderRepositoryInput) (projectservice.RepositoryProviderCreateResult, error) {
		if input.ProjectID != projectID ||
			input.Provider != enum.RepositoryProviderGitHub ||
			input.OwnerKind != enum.RepositoryOwnerKindOrganization ||
			input.ProviderOwner != "codex-k8s" ||
			input.ProviderName != "new-service" ||
			input.Visibility != enum.RepositoryVisibilityPrivate ||
			input.ExternalAccountID != externalAccountID {
			t.Fatalf("input = %+v, want provider repository create fields", input)
		}
		if input.Meta.CommandID != commandID {
			t.Fatalf("meta = %+v, want command id", input.Meta)
		}
		return projectservice.RepositoryProviderCreateResult{
			Repository: entity.RepositoryBinding{
				Base:                 entity.Base{ID: repositoryID, Version: 2},
				ProjectID:            projectID,
				Provider:             enum.RepositoryProviderGitHub,
				ProviderOwner:        "codex-k8s",
				ProviderName:         "new-service",
				WebURL:               "https://github.com/codex-k8s/new-service",
				DefaultBranch:        "main",
				Status:               enum.RepositoryStatusPending,
				ProviderRepositoryID: "100500",
			},
			ProviderTarget: projectservice.RepositoryBootstrapProviderTarget{
				ProviderSlug:         "github",
				RepositoryFullName:   "codex-k8s/new-service",
				ProviderRepositoryID: "100500",
				WebURL:               "https://github.com/codex-k8s/new-service",
			},
			BaseBranch: "main",
			ProviderResult: projectservice.RepositoryProviderCreateProviderResult{
				ProviderOperationID:  "operation-1",
				ProviderRepositoryID: "100500",
				ProviderWebURL:       "https://github.com/codex-k8s/new-service",
				BaseBranch:           "main",
			},
		}, nil
	}
	server := NewServer(service)

	response, err := server.CreateProviderRepository(context.Background(), &projectsv1.CreateProviderRepositoryRequest{
		ProjectId:         projectID.String(),
		Provider:          projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB,
		OwnerKind:         projectsv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_ORGANIZATION,
		ProviderOwner:     "codex-k8s",
		ProviderName:      "new-service",
		Visibility:        projectsv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PRIVATE,
		ExternalAccountId: externalAccountID.String(),
		Meta:              commandMeta(commandID.String()),
	})
	if err != nil {
		t.Fatalf("CreateProviderRepository(): %v", err)
	}
	if response.GetRepository().GetRepositoryId() != repositoryID.String() ||
		response.GetProviderTarget().GetProviderRepositoryId() != "100500" ||
		response.GetBaseBranch() != "main" ||
		response.GetProviderOperationId() != "operation-1" {
		t.Fatalf("response = %+v, want provider repository refs", response)
	}
}

func TestGetSelfDeploySignalMapsDomainResult(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	service := &fakeProjectService{}
	service.getSelfDeploySignal = func(_ context.Context, input projectservice.GetSelfDeploySignalInput) (projectservice.SelfDeploySignalResult, error) {
		if input.ProjectID != projectID || input.RepositoryID == nil || *input.RepositoryID != repositoryID || input.ProviderSignalID != "signal-1" {
			t.Fatalf("input = %+v, want project/repository/provider signal ids", input)
		}
		return projectservice.SelfDeploySignalResult{
			Status: enum.SelfDeploySignalStatusReady,
			Signal: projectservice.SelfDeploySignal{
				ProviderSignalRef:    "provider:github:repository_change:push:codex-k8s/kodex:main:abc",
				ProviderSignalID:     "signal-1",
				ProviderSignalKey:    "signal-key-1",
				ProjectRef:           projectID.String(),
				RepositoryRef:        repositoryID.String(),
				ProviderSlug:         "github",
				RepositoryFullName:   "codex-k8s/kodex",
				ProviderRepositoryID: "R_123",
				SourceRef:            "refs/heads/main",
				MergeCommitSHA:       "abcdef0123456789abcdef0123456789abcdef01",
				ServicesYaml: projectservice.SelfDeployServicesYamlProjection{
					ServicesYamlRef:         "project-catalog:services-policy:policy-1:services.yaml",
					ServicesYamlDigest:      "sha256:services-policy",
					ServicesYamlFingerprint: "sha256:projection",
					ServicesPolicyID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
					SourceRepositoryID:      &repositoryID,
					SourcePath:              "services.yaml",
					SourceRef:               "refs/heads/main",
					SourceCommitSHA:         "abcdef0123456789abcdef0123456789abcdef01",
					PolicyVersion:           3,
					ValidationStatus:        enum.ServicesPolicyValidationValid,
					ProjectionStatus:        enum.ServicesPolicyProjectionSynced,
					ImportedAt:              "2026-05-06T12:00:00Z",
				},
				AffectedServiceKeys: []string{"api"},
				PathCategories: []projectservice.RepositoryChangePathCategoryCount{
					{Category: enum.SelfDeployPathCategoryServiceSource, Count: 2},
				},
				ServicesYamlChanged:       true,
				DeployRelevantChanged:     true,
				ExpectedRuntimeJobTypes:   []enum.SelfDeployExpectedRuntimeJobType{enum.SelfDeployExpectedRuntimeJobTypeBuild, enum.SelfDeployExpectedRuntimeJobTypeDeploy},
				GovernanceRequirement:     projectservice.SelfDeployGovernanceRequirement{GateRequired: true, GatePolicyRef: "self_deploy.owner_gate"},
				ProviderChangeFingerprint: "sha256:provider",
				ProjectSignalFingerprint:  "sha256:project",
				ProviderETag:              "etag-1",
				SafeSummary:               "self-deploy signal ready",
				ObservedAt:                "2026-05-06T12:00:00Z",
				Version:                   1,
			},
		}, nil
	}
	server := NewServer(service)

	response, err := server.GetSelfDeploySignal(context.Background(), &projectsv1.GetSelfDeploySignalRequest{
		ProjectId:        projectID.String(),
		RepositoryId:     stringPtr(repositoryID.String()),
		ProviderSignalId: stringPtr("signal-1"),
		Meta:             queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	signal := response.GetSignal()
	if response.GetStatus() != projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY ||
		signal.GetServicesYaml().GetServicesYamlDigest() != "sha256:services-policy" ||
		signal.GetServicesYaml().GetServicesYamlDigest() == "sha256:provider" ||
		signal.GetAffectedServiceKeys()[0] != "api" ||
		!signal.GetGovernanceRequirement().GetGateRequired() {
		t.Fatalf("response = %+v, want safe self-deploy signal", response)
	}
}

func TestGetSelfDeployBuildPlanMapsDomainResult(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	policyVersion := int64(3)
	service := &fakeProjectService{}
	service.getSelfDeployBuildPlan = func(_ context.Context, input projectservice.GetSelfDeployBuildPlanInput) (projectservice.SelfDeployBuildPlanResult, error) {
		if input.ProjectID != projectID ||
			input.RepositoryID != repositoryID ||
			input.ProviderSignalRef != "provider-signal-ref" ||
			input.ExpectedServicesPolicyDigest != "sha256:services-policy" ||
			input.ExpectedServicesPolicyVersion == nil ||
			*input.ExpectedServicesPolicyVersion != policyVersion ||
			input.ExpectedBuildPlanFingerprint != "sha256:recipe-plan" ||
			len(input.MaterializedBuildContexts) != 1 ||
			input.MaterializedBuildContexts[0].BuildContextRef != "pvc://runtime/build-context-api" {
			t.Fatalf("input = %+v, want build plan request refs", input)
		}
		return projectservice.SelfDeployBuildPlanResult{
			Status: enum.SelfDeployBuildPlanStatusReady,
			Plan: projectservice.SelfDeployBuildPlan{
				ProjectRef:        projectID.String(),
				RepositoryRef:     repositoryID.String(),
				ProviderSignalRef: "provider-signal-ref",
				SourceRef:         "refs/heads/main",
				MergeCommitSHA:    "abcdef0123456789abcdef0123456789abcdef01",
				ServicesYaml: projectservice.SelfDeployServicesYamlProjection{
					ServicesYamlRef:         "project-catalog:services-policy:policy-1:services.yaml",
					ServicesYamlDigest:      "sha256:services-policy",
					ServicesYamlFingerprint: "sha256:projection",
					ServicesPolicyID:        policyID,
					SourceRepositoryID:      &repositoryID,
					SourcePath:              "services.yaml",
					SourceRef:               "refs/heads/main",
					SourceCommitSHA:         "abcdef0123456789abcdef0123456789abcdef01",
					PolicyVersion:           policyVersion,
					ValidationStatus:        enum.ServicesPolicyValidationValid,
					ProjectionStatus:        enum.ServicesPolicyProjectionSynced,
					ImportedAt:              "2026-05-06T12:00:00Z",
				},
				AffectedServiceKeys: []string{"api"},
				BuildItems: []projectservice.SelfDeployBuildPlanItem{
					{
						ServiceKey: "api",
						ServiceRef: "project-catalog:service-descriptor:service-1:api",
						Status:     projectservice.SelfDeployBuildPlanItemStatusReady,
						BuildRecipe: projectservice.SelfDeployBuildRecipe{
							ImageRef:          "registry.example/kodex/api",
							ImageTag:          "abcdef0",
							DockerfileRef:     "context://services/api/Dockerfile",
							DockerfileTarget:  "prod",
							BuilderImageRef:   "gcr.io/kaniko-project/executor:v1.23.2",
							RecipeFingerprint: "sha256:recipe",
							AllowedSecretRefs: []projectservice.RuntimeJobAllowedSecretRef{
								{SecretRef: "secret://runtime/registry", Purpose: "registry_docker_config"},
							},
							OutputRefs: []projectservice.RuntimeJobOutputRef{
								{Kind: "image", Ref: "runtime:image:api"},
							},
						},
						BuildExecutionSpec: projectservice.SelfDeployBuildExecutionSpec{
							SourceRef:            "refs/heads/main",
							SourceCommitSHA:      "abcdef0123456789abcdef0123456789abcdef01",
							ServiceKey:           "api",
							ImageRef:             "registry.example/kodex/api",
							ImageTag:             "abcdef0",
							BuildContextRef:      "pvc://runtime/build-context-api",
							BuildContextDigest:   "sha256:context",
							DockerfileRef:        "context://services/api/Dockerfile",
							DockerfileTarget:     "prod",
							BuilderImageRef:      "gcr.io/kaniko-project/executor:v1.23.2",
							BuildPlanFingerprint: "sha256:build-plan",
							AllowedSecretRefs: []projectservice.RuntimeJobAllowedSecretRef{
								{SecretRef: "secret://runtime/registry", Purpose: "registry_docker_config"},
							},
							OutputRefs: []projectservice.RuntimeJobOutputRef{
								{Kind: "image", Ref: "runtime:image:api"},
							},
						},
						PlanItemFingerprint: "sha256:item",
					},
				},
				PlanFingerprint: "sha256:build-plan",
				SafeSummary:     "self-deploy build plan ready",
				Version:         1,
			},
		}, nil
	}
	server := NewServer(service)

	response, err := server.GetSelfDeployBuildPlan(context.Background(), &projectsv1.GetSelfDeployBuildPlanRequest{
		ProjectId:                         projectID.String(),
		RepositoryId:                      repositoryID.String(),
		SourceRef:                         "refs/heads/main",
		MergeCommitSha:                    "abcdef0123456789abcdef0123456789abcdef01",
		ProviderSignalRef:                 stringPtr("provider-signal-ref"),
		AffectedServiceKeys:               []string{"api"},
		ExpectedServicesPolicyDigest:      "sha256:services-policy",
		ExpectedServicesPolicyFingerprint: stringPtr("sha256:projection"),
		ExpectedServicesPolicyVersion:     &policyVersion,
		ExpectedBuildPlanFingerprint:      stringPtr("sha256:recipe-plan"),
		MaterializedBuildContexts: []*projectsv1.SelfDeployMaterializedBuildContext{
			{
				ServiceKey:         "api",
				BuildContextRef:    "pvc://runtime/build-context-api",
				BuildContextDigest: "sha256:context",
				DockerfileDigest:   stringPtr("sha256:dockerfile"),
				MaterializationRef: stringPtr("runtime://workspace-materialization/api"),
			},
		},
		Meta: queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	item := response.GetPlan().GetBuildItems()[0]
	spec := item.GetBuildExecutionSpec()
	if response.GetStatus() != projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_READY ||
		item.GetStatus() != projectsv1.SelfDeployBuildPlanItemStatus_SELF_DEPLOY_BUILD_PLAN_ITEM_STATUS_READY ||
		item.GetBuildRecipe().GetRecipeFingerprint() != "sha256:recipe" ||
		spec.GetServiceKey() != "api" ||
		spec.GetBuildPlanFingerprint() != "sha256:build-plan" ||
		spec.GetAllowedSecretRefs()[0].GetSecretRef() != "secret://runtime/registry" ||
		spec.GetAllowedSecretRefs()[0].GetPurpose() != "registry_docker_config" {
		t.Fatalf("response = %+v, want safe build plan", response)
	}
	if strings.Contains(response.String(), "secret_value") {
		t.Fatalf("response leaked secret value: %s", response.String())
	}
}

func TestImportBootstrapServicesPolicyCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commandID := uuid.New()
	projectionID := "projection-1"
	expectedVersion := int64(3)
	service := &fakeProjectService{}
	service.importBootstrapPolicy = func(_ context.Context, input projectservice.ImportBootstrapServicesPolicyInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
		if input.ProjectID != projectID ||
			input.RepositoryID != repositoryID ||
			input.ProviderTarget.ProviderSlug != "github" ||
			input.ProviderTarget.RepositoryFullName != "codex-k8s/kodex" ||
			input.BaseBranch != "main" ||
			input.SourceRef != "refs/heads/main" ||
			input.SourceCommitSHA != "0123456789abcdef0123456789abcdef01234567" ||
			input.SourcePath != "services.yaml" ||
			input.ProviderWorkItemProjectionID != "projection-1" {
			t.Fatalf("input = %+v, want bootstrap policy import fields", input)
		}
		if input.Meta.CommandID != commandID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
			t.Fatalf("meta = %+v, want command id and expected version", input.Meta)
		}
		return projectservice.BootstrapServicesPolicyImportResult{
			Repository: entity.RepositoryBinding{
				Base:          entity.Base{ID: repositoryID, Version: 4},
				ProjectID:     projectID,
				Provider:      enum.RepositoryProviderGitHub,
				ProviderOwner: "codex-k8s",
				ProviderName:  "kodex",
				DefaultBranch: "main",
				Status:        enum.RepositoryStatusActive,
			},
			ServicesPolicy: entity.ServicesPolicy{
				Base:            entity.Base{ID: policyID, Version: 1},
				ProjectID:       projectID,
				SourcePath:      "services.yaml",
				SourceRef:       "refs/heads/main",
				SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				PolicyVersion:   1,
				ContentHash:     "sha256:policy",
				ImportedAt:      time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
			},
			SourceRef:       "refs/heads/main",
			SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			Summary:         "services.yaml imported from refs/heads/main at 0123456789ab",
		}, nil
	}
	server := NewServer(service)
	meta := commandMeta(commandID.String())
	meta.ExpectedVersion = &expectedVersion

	response, err := server.ImportBootstrapServicesPolicy(context.Background(), &projectsv1.ImportBootstrapServicesPolicyRequest{
		ProjectId:    projectID.String(),
		RepositoryId: repositoryID.String(),
		ProviderTarget: &projectsv1.RepositoryBootstrapProviderTarget{
			ProviderSlug:       "github",
			RepositoryFullName: "codex-k8s/kodex",
		},
		BaseBranch:                   "main",
		SourceRef:                    "refs/heads/main",
		SourceCommitSha:              "0123456789abcdef0123456789abcdef01234567",
		SourcePath:                   "services.yaml",
		ContentHash:                  "sha256:policy",
		ValidatedPayloadJson:         `{"spec":{"services":[{"key":"api","rootPath":"services/api"}]}}`,
		WatermarkJson:                `{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`,
		ProviderWorkItemProjectionId: &projectionID,
		Meta:                         meta,
	})
	if err != nil {
		t.Fatalf("ImportBootstrapServicesPolicy(): %v", err)
	}
	if response.GetRepository().GetStatus() != projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE ||
		response.GetServicesPolicy().GetServicesPolicyId() != policyID.String() ||
		response.GetSourceCommitSha() != "0123456789abcdef0123456789abcdef01234567" ||
		response.GetSummary() == "" {
		t.Fatalf("response = %+v, want active repository and imported policy", response)
	}
}

func TestReconcileBootstrapMergeSignalCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commandID := uuid.New()
	projectionID := uuid.New().String()
	signalID := uuid.New().String()
	expectedVersion := int64(3)
	service := &fakeProjectService{}
	service.reconcileBootstrapMerge = func(_ context.Context, input projectservice.ReconcileBootstrapMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
		if input.ProjectID != projectID ||
			input.RepositoryID != repositoryID ||
			input.MergeSignal.SignalID != signalID ||
			input.MergeSignal.SignalKey != "github/bootstrap/PR_123" ||
			input.MergeSignal.SignalKind != "bootstrap" ||
			input.MergeSignal.ProviderTarget.ProviderSlug != "github" ||
			input.MergeSignal.ProviderTarget.RepositoryFullName != "codex-k8s/kodex" ||
			input.MergeSignal.BaseBranch != "main" ||
			input.MergeSignal.SourceRef != "kodex/bootstrap" ||
			input.MergeSignal.MergeCommitSHA != "0123456789abcdef0123456789abcdef01234567" ||
			input.MergeSignal.WatermarkDigest != "watermark-digest" ||
			input.MergeSignal.ProviderWorkItemProjectionID != projectionID {
			t.Fatalf("merge signal input = %+v, want safe signal fields", input.MergeSignal)
		}
		if input.CheckedPolicy.ArtifactRef != "artifact://provider/bootstrap/1" ||
			input.CheckedPolicy.ArtifactDigest != "sha256:policy" ||
			input.CheckedPolicy.ArtifactVersion != "0123456789abcdef0123456789abcdef01234567" ||
			input.CheckedPolicy.SourcePath != "services.yaml" {
			t.Fatalf("checked policy = %+v, want checked artifact fields", input.CheckedPolicy)
		}
		if input.Meta.CommandID != commandID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
			t.Fatalf("meta = %+v, want command id and expected version", input.Meta)
		}
		return projectservice.BootstrapServicesPolicyImportResult{
			Repository: entity.RepositoryBinding{
				Base:          entity.Base{ID: repositoryID, Version: 4},
				ProjectID:     projectID,
				Provider:      enum.RepositoryProviderGitHub,
				ProviderOwner: "codex-k8s",
				ProviderName:  "kodex",
				DefaultBranch: "main",
				Status:        enum.RepositoryStatusActive,
			},
			ServicesPolicy: entity.ServicesPolicy{
				Base:            entity.Base{ID: policyID, Version: 1},
				ProjectID:       projectID,
				SourcePath:      "services.yaml",
				SourceRef:       "refs/heads/main",
				SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
				PolicyVersion:   1,
				ContentHash:     "sha256:policy",
				ImportedAt:      time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
			},
			SourceRef:       "refs/heads/main",
			SourceCommitSHA: "0123456789abcdef0123456789abcdef01234567",
			Summary:         "services.yaml imported from refs/heads/main at 0123456789ab",
		}, nil
	}
	server := NewServer(service)
	meta := commandMeta(commandID.String())
	meta.ExpectedVersion = &expectedVersion

	response, err := server.ReconcileBootstrapMergeSignal(context.Background(), &projectsv1.ReconcileBootstrapMergeSignalRequest{
		ProjectId:    projectID.String(),
		RepositoryId: repositoryID.String(),
		MergeSignal: &projectsv1.BootstrapRepositoryMergeSignal{
			SignalId:   &signalID,
			SignalKey:  "github/bootstrap/PR_123",
			SignalKind: "bootstrap",
			ProviderTarget: &projectsv1.RepositoryBootstrapProviderTarget{
				ProviderSlug:       "github",
				RepositoryFullName: "codex-k8s/kodex",
			},
			BaseBranch:                   "main",
			SourceRef:                    "kodex/bootstrap",
			MergeCommitSha:               "0123456789abcdef0123456789abcdef01234567",
			WatermarkDigest:              "watermark-digest",
			WatermarkJson:                `{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_bootstrap","source_ref":"services.yaml"}`,
			ProviderWorkItemProjectionId: &projectionID,
		},
		CheckedPolicy: &projectsv1.CheckedBootstrapServicesPolicyArtifact{
			ArtifactRef:          "artifact://provider/bootstrap/1",
			ArtifactDigest:       "sha256:policy",
			ArtifactVersion:      "0123456789abcdef0123456789abcdef01234567",
			SourcePath:           "services.yaml",
			ContentHash:          "sha256:policy",
			ValidatedPayloadJson: `{"spec":{"services":[{"key":"api","rootPath":"services/api"}]}}`,
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("ReconcileBootstrapMergeSignal(): %v", err)
	}
	if response.GetRepository().GetStatus() != projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE ||
		response.GetServicesPolicy().GetServicesPolicyId() != policyID.String() ||
		response.GetSummary() == "" {
		t.Fatalf("response = %+v, want active repository and imported policy", response)
	}
}

func TestReconcileAdoptionMergeSignalCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	commandID := uuid.New()
	projectionID := uuid.New().String()
	signalID := uuid.New().String()
	expectedVersion := int64(5)
	service := &fakeProjectService{}
	service.reconcileAdoptionMerge = func(_ context.Context, input projectservice.ReconcileAdoptionMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
		if input.ProjectID != projectID ||
			input.RepositoryID != repositoryID ||
			input.MergeSignal.SignalID != signalID ||
			input.MergeSignal.SignalKey != "github/adoption/PR_456" ||
			input.MergeSignal.SignalKind != "adoption" ||
			input.MergeSignal.ProviderTarget.ProviderSlug != "github" ||
			input.MergeSignal.ProviderTarget.RepositoryFullName != "codex-k8s/existing" ||
			input.MergeSignal.BaseBranch != "main" ||
			input.MergeSignal.SourceRef != "kodex/adoption" ||
			input.MergeSignal.MergeCommitSHA != "89abcdef0123456789abcdef0123456789abcdef" ||
			input.MergeSignal.WatermarkDigest != "adoption-watermark-digest" ||
			input.MergeSignal.ProviderWorkItemProjectionID != projectionID {
			t.Fatalf("adoption merge signal input = %+v, want safe signal fields", input.MergeSignal)
		}
		if input.CheckedPolicy.ArtifactRef != "artifact://provider/adoption/1" ||
			input.CheckedPolicy.ArtifactDigest != "sha256:adoption-policy" ||
			input.CheckedPolicy.ArtifactVersion != "89abcdef0123456789abcdef0123456789abcdef" ||
			input.CheckedPolicy.SourcePath != "services.yaml" {
			t.Fatalf("checked policy = %+v, want checked artifact fields", input.CheckedPolicy)
		}
		if input.Meta.CommandID != commandID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
			t.Fatalf("meta = %+v, want command id and expected version", input.Meta)
		}
		return projectservice.BootstrapServicesPolicyImportResult{
			Repository: entity.RepositoryBinding{
				Base:          entity.Base{ID: repositoryID, Version: 6},
				ProjectID:     projectID,
				Provider:      enum.RepositoryProviderGitHub,
				ProviderOwner: "codex-k8s",
				ProviderName:  "existing",
				DefaultBranch: "main",
				Status:        enum.RepositoryStatusActive,
			},
			ServicesPolicy: entity.ServicesPolicy{
				Base:            entity.Base{ID: policyID, Version: 2},
				ProjectID:       projectID,
				SourcePath:      "services.yaml",
				SourceRef:       "refs/heads/main",
				SourceCommitSHA: "89abcdef0123456789abcdef0123456789abcdef",
				PolicyVersion:   2,
				ContentHash:     "sha256:adoption-policy",
				ImportedAt:      time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
			},
			SourceRef:       "refs/heads/main",
			SourceCommitSHA: "89abcdef0123456789abcdef0123456789abcdef",
			Summary:         "services.yaml imported from refs/heads/main at 89abcdef0123",
		}, nil
	}
	server := NewServer(service)
	meta := commandMeta(commandID.String())
	meta.ExpectedVersion = &expectedVersion

	response, err := server.ReconcileAdoptionMergeSignal(context.Background(), &projectsv1.ReconcileAdoptionMergeSignalRequest{
		ProjectId:    projectID.String(),
		RepositoryId: repositoryID.String(),
		MergeSignal: &projectsv1.RepositoryAdoptionMergeSignal{
			SignalId:   &signalID,
			SignalKey:  "github/adoption/PR_456",
			SignalKind: "adoption",
			ProviderTarget: &projectsv1.RepositoryBootstrapProviderTarget{
				ProviderSlug:       "github",
				RepositoryFullName: "codex-k8s/existing",
			},
			BaseBranch:                   "main",
			SourceRef:                    "kodex/adoption",
			MergeCommitSha:               "89abcdef0123456789abcdef0123456789abcdef",
			WatermarkDigest:              "adoption-watermark-digest",
			WatermarkJson:                `{"kind":"provider_pr","managed_by":"kodex","work_type":"repository_adoption","source_ref":"services.yaml"}`,
			ProviderWorkItemProjectionId: &projectionID,
		},
		CheckedPolicy: &projectsv1.CheckedAdoptionServicesPolicyArtifact{
			ArtifactRef:          "artifact://provider/adoption/1",
			ArtifactDigest:       "sha256:adoption-policy",
			ArtifactVersion:      "89abcdef0123456789abcdef0123456789abcdef",
			SourcePath:           "services.yaml",
			ContentHash:          "sha256:adoption-policy",
			ValidatedPayloadJson: `{"spec":{"services":[{"key":"api","rootPath":"services/api"}]}}`,
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("ReconcileAdoptionMergeSignal(): %v", err)
	}
	if response.GetRepository().GetStatus() != projectsv1.RepositoryStatus_REPOSITORY_STATUS_ACTIVE ||
		response.GetServicesPolicy().GetServicesPolicyId() != policyID.String() ||
		response.GetSummary() == "" {
		t.Fatalf("response = %+v, want active repository and imported policy", response)
	}
}

func TestGetProjectOnboardingStatusCallsDomainService(t *testing.T) {
	projectID := uuid.New()
	repositoryID := uuid.New()
	policyID := uuid.New()
	expectedVersion := int64(3)
	service := &fakeProjectService{}
	service.getOnboardingStatus = func(_ context.Context, input projectservice.GetProjectOnboardingStatusInput) (projectservice.ProjectOnboardingStatusResult, error) {
		if input.ProjectID != projectID ||
			input.RepositoryID == nil ||
			*input.RepositoryID != repositoryID ||
			input.ExpectedSourceRef != "refs/heads/main" ||
			input.ExpectedSourceCommitSHA != "0123456789abcdef0123456789abcdef01234567" ||
			input.ExpectedContentHash != "sha256:policy" ||
			input.ExpectedServicesPolicyVersion == nil ||
			*input.ExpectedServicesPolicyVersion != expectedVersion ||
			len(input.ServiceKeys) != 1 ||
			input.ServiceKeys[0] != "api" {
			t.Fatalf("input = %+v, want onboarding status read input", input)
		}
		return projectservice.ProjectOnboardingStatusResult{
			Status: enum.ProjectOnboardingStatusReady,
			Project: &entity.Project{
				Base:           entity.Base{ID: projectID, Version: 1},
				OrganizationID: uuid.New(),
				Slug:           "dogfood",
				DisplayName:    "Dogfood",
				Status:         enum.ProjectStatusActive,
			},
			Repository: &entity.RepositoryBinding{
				Base:          entity.Base{ID: repositoryID, Version: 1},
				ProjectID:     projectID,
				Provider:      enum.RepositoryProviderGitHub,
				ProviderOwner: "codex-k8s",
				ProviderName:  "dogfood",
				DefaultBranch: "main",
				Status:        enum.RepositoryStatusActive,
			},
			ServicesPolicy: &entity.ServicesPolicy{
				Base:             entity.Base{ID: policyID, Version: 1},
				ProjectID:        projectID,
				SourcePath:       "services.yaml",
				SourceCommitSHA:  "0123456789abcdef0123456789abcdef01234567",
				ContentHash:      "sha256:policy",
				ValidationStatus: enum.ServicesPolicyValidationValid,
				ProjectionStatus: enum.ServicesPolicyProjectionSynced,
				PolicyVersion:    3,
				ImportedAt:       time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
			},
			ServiceDescriptors: []entity.ServiceDescriptor{{
				Base:             entity.Base{ID: uuid.New(), Version: 1},
				ProjectID:        projectID,
				ServicesPolicyID: policyID,
				RepositoryID:     &repositoryID,
				ServiceKey:       "api",
				DisplayName:      "API",
				Kind:             enum.ServiceKindBackend,
				RootPath:         "services/api",
				Status:           enum.ServiceStatusActive,
			}},
			Summary: "project onboarding ready policy_version=3 descriptors=1",
		}, nil
	}
	server := NewServer(service)

	response, err := server.GetProjectOnboardingStatus(context.Background(), &projectsv1.GetProjectOnboardingStatusRequest{
		ProjectId:                     projectID.String(),
		RepositoryId:                  stringPtr(repositoryID.String()),
		ServiceKeys:                   []string{"api"},
		ExpectedSourceRef:             stringPtr("refs/heads/main"),
		ExpectedSourceCommitSha:       stringPtr("0123456789abcdef0123456789abcdef01234567"),
		ExpectedContentHash:           stringPtr("sha256:policy"),
		ExpectedServicesPolicyVersion: &expectedVersion,
		Meta:                          queryMeta(),
	})
	if err != nil {
		t.Fatalf("GetProjectOnboardingStatus(): %v", err)
	}
	if response.GetStatus() != projectsv1.ProjectOnboardingStatus_PROJECT_ONBOARDING_STATUS_READY ||
		response.GetRepository().GetRepositoryId() != repositoryID.String() ||
		response.GetServicesPolicy().GetServicesPolicyId() != policyID.String() ||
		len(response.GetServiceDescriptors()) != 1 ||
		response.GetSummary() == "" {
		t.Fatalf("response = %+v, want ready onboarding state", response)
	}
}

func TestErrorToStatusMapsDomainErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "invalid argument", err: errs.ErrInvalidArgument, code: codes.InvalidArgument},
		{name: "forbidden", err: errs.ErrForbidden, code: codes.PermissionDenied},
		{name: "not found", err: errs.ErrNotFound, code: codes.NotFound},
		{name: "already exists", err: errs.ErrAlreadyExists, code: codes.AlreadyExists},
		{name: "conflict", err: errs.ErrConflict, code: codes.Aborted},
		{name: "precondition", err: errs.ErrPreconditionFailed, code: codes.FailedPrecondition},
		{name: "dependency", err: errs.ErrDependencyUnavailable, code: codes.Unavailable},
		{name: "unknown", err: errors.New("boom"), code: codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			interceptor := UnaryErrorInterceptor(nil)
			info := &grpcruntime.UnaryServerInfo{FullMethod: "/kodex.projects.v1.ProjectCatalogService/Test"}
			_, err := interceptor(context.Background(), nil, info, func(context.Context, any) (any, error) {
				return nil, tc.err
			})
			if got := status.Code(err); got != tc.code {
				t.Fatalf("code = %s, want %s", got, tc.code)
			}
		})
	}
}

func commandMeta(commandID string) *projectsv1.CommandMeta {
	return &projectsv1.CommandMeta{
		CommandId: &commandID,
		Actor: &projectsv1.Actor{
			Type: "user",
			Id:   "owner",
		},
		RequestId: "req-1",
		RequestContext: &projectsv1.RequestContext{
			Source: "test",
		},
	}
}

func queryMeta() *projectsv1.QueryMeta {
	return &projectsv1.QueryMeta{
		Actor: &projectsv1.Actor{
			Type: "service",
			Id:   "agent-manager",
		},
		RequestId: "read-1",
		RequestContext: &projectsv1.RequestContext{
			Source: "test",
		},
	}
}

func stringPtr(value string) *string {
	return &value
}

type fakeProjectService struct {
	unimplementedProjectService
	createProject            func(context.Context, projectservice.CreateProjectInput) (entity.Project, error)
	createProviderRepository func(context.Context, projectservice.CreateProviderRepositoryInput) (projectservice.RepositoryProviderCreateResult, error)
	createBootstrap          func(context.Context, projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error)
	importBootstrapPolicy    func(context.Context, projectservice.ImportBootstrapServicesPolicyInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	reconcileBootstrapMerge  func(context.Context, projectservice.ReconcileBootstrapMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	reconcileAdoptionMerge   func(context.Context, projectservice.ReconcileAdoptionMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	getSelfDeploySignal      func(context.Context, projectservice.GetSelfDeploySignalInput) (projectservice.SelfDeploySignalResult, error)
	getSelfDeployBuildPlan   func(context.Context, projectservice.GetSelfDeployBuildPlanInput) (projectservice.SelfDeployBuildPlanResult, error)
	getSelfDeployDeployPlan  func(context.Context, projectservice.GetSelfDeployDeployPlanInput) (projectservice.SelfDeployDeployPlanResult, error)
	getOnboardingStatus      func(context.Context, projectservice.GetProjectOnboardingStatusInput) (projectservice.ProjectOnboardingStatusResult, error)
}

func (s *fakeProjectService) CreateProject(ctx context.Context, input projectservice.CreateProjectInput) (entity.Project, error) {
	if s.createProject == nil {
		return entity.Project{}, errs.ErrInvalidArgument
	}
	return s.createProject(ctx, input)
}

func (s *fakeProjectService) CreateProviderRepository(ctx context.Context, input projectservice.CreateProviderRepositoryInput) (projectservice.RepositoryProviderCreateResult, error) {
	if s.createProviderRepository == nil {
		return projectservice.RepositoryProviderCreateResult{}, errs.ErrInvalidArgument
	}
	return s.createProviderRepository(ctx, input)
}

func (s *fakeProjectService) CreateRepositoryBootstrapPullRequest(ctx context.Context, input projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error) {
	if s.createBootstrap == nil {
		return projectservice.RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
	}
	return s.createBootstrap(ctx, input)
}

func (s *fakeProjectService) ImportBootstrapServicesPolicy(ctx context.Context, input projectservice.ImportBootstrapServicesPolicyInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
	if s.importBootstrapPolicy == nil {
		return projectservice.BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
	}
	return s.importBootstrapPolicy(ctx, input)
}

func (s *fakeProjectService) ReconcileBootstrapMergeSignal(ctx context.Context, input projectservice.ReconcileBootstrapMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
	if s.reconcileBootstrapMerge == nil {
		return projectservice.BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
	}
	return s.reconcileBootstrapMerge(ctx, input)
}

func (s *fakeProjectService) ReconcileAdoptionMergeSignal(ctx context.Context, input projectservice.ReconcileAdoptionMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
	if s.reconcileAdoptionMerge == nil {
		return projectservice.BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
	}
	return s.reconcileAdoptionMerge(ctx, input)
}

func (s *fakeProjectService) GetSelfDeploySignal(ctx context.Context, input projectservice.GetSelfDeploySignalInput) (projectservice.SelfDeploySignalResult, error) {
	if s.getSelfDeploySignal == nil {
		return projectservice.SelfDeploySignalResult{}, errs.ErrInvalidArgument
	}
	return s.getSelfDeploySignal(ctx, input)
}

func (s *fakeProjectService) GetSelfDeployBuildPlan(ctx context.Context, input projectservice.GetSelfDeployBuildPlanInput) (projectservice.SelfDeployBuildPlanResult, error) {
	if s.getSelfDeployBuildPlan == nil {
		return projectservice.SelfDeployBuildPlanResult{}, errs.ErrInvalidArgument
	}
	return s.getSelfDeployBuildPlan(ctx, input)
}

func (s *fakeProjectService) GetSelfDeployDeployPlan(ctx context.Context, input projectservice.GetSelfDeployDeployPlanInput) (projectservice.SelfDeployDeployPlanResult, error) {
	if s.getSelfDeployDeployPlan == nil {
		return projectservice.SelfDeployDeployPlanResult{}, errs.ErrInvalidArgument
	}
	return s.getSelfDeployDeployPlan(ctx, input)
}

func (s *fakeProjectService) GetProjectOnboardingStatus(ctx context.Context, input projectservice.GetProjectOnboardingStatusInput) (projectservice.ProjectOnboardingStatusResult, error) {
	if s.getOnboardingStatus == nil {
		return projectservice.ProjectOnboardingStatusResult{}, errs.ErrInvalidArgument
	}
	return s.getOnboardingStatus(ctx, input)
}

type unimplementedProjectService struct{}

func (unimplementedProjectService) CreateProject(context.Context, projectservice.CreateProjectInput) (entity.Project, error) {
	return entity.Project{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) UpdateProject(context.Context, projectservice.UpdateProjectInput) (entity.Project, error) {
	return entity.Project{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetProject(context.Context, uuid.UUID, value.QueryMeta) (entity.Project, error) {
	return entity.Project{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListProjects(context.Context, projectservice.ListProjectsInput) (projectservice.ListProjectsResult, error) {
	return projectservice.ListProjectsResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) AttachRepository(context.Context, projectservice.AttachRepositoryInput) (entity.RepositoryBinding, error) {
	return entity.RepositoryBinding{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) CreateProviderRepository(context.Context, projectservice.CreateProviderRepositoryInput) (projectservice.RepositoryProviderCreateResult, error) {
	return projectservice.RepositoryProviderCreateResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) CreateRepositoryBootstrapPullRequest(context.Context, projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error) {
	return projectservice.RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) UpdateRepository(context.Context, projectservice.UpdateRepositoryInput) (entity.RepositoryBinding, error) {
	return entity.RepositoryBinding{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) DetachRepository(context.Context, uuid.UUID, value.CommandMeta) (entity.RepositoryBinding, error) {
	return entity.RepositoryBinding{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetRepository(context.Context, uuid.UUID, value.QueryMeta) (entity.RepositoryBinding, error) {
	return entity.RepositoryBinding{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListRepositories(context.Context, projectservice.ListRepositoriesInput) (projectservice.ListRepositoriesResult, error) {
	return projectservice.ListRepositoriesResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ImportServicesPolicy(context.Context, projectservice.ImportServicesPolicyInput) (entity.ServicesPolicy, error) {
	return entity.ServicesPolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ImportBootstrapServicesPolicy(context.Context, projectservice.ImportBootstrapServicesPolicyInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
	return projectservice.BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ReconcileBootstrapMergeSignal(context.Context, projectservice.ReconcileBootstrapMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
	return projectservice.BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ReconcileAdoptionMergeSignal(context.Context, projectservice.ReconcileAdoptionMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error) {
	return projectservice.BootstrapServicesPolicyImportResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetSelfDeploySignal(context.Context, projectservice.GetSelfDeploySignalInput) (projectservice.SelfDeploySignalResult, error) {
	return projectservice.SelfDeploySignalResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetSelfDeployBuildPlan(context.Context, projectservice.GetSelfDeployBuildPlanInput) (projectservice.SelfDeployBuildPlanResult, error) {
	return projectservice.SelfDeployBuildPlanResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetSelfDeployDeployPlan(context.Context, projectservice.GetSelfDeployDeployPlanInput) (projectservice.SelfDeployDeployPlanResult, error) {
	return projectservice.SelfDeployDeployPlanResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetServicesPolicy(context.Context, projectservice.GetServicesPolicyInput) (entity.ServicesPolicy, error) {
	return entity.ServicesPolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListServiceDescriptors(context.Context, projectservice.ListServiceDescriptorsInput) (projectservice.ListServiceDescriptorsResult, error) {
	return projectservice.ListServiceDescriptorsResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetProjectOnboardingStatus(context.Context, projectservice.GetProjectOnboardingStatusInput) (projectservice.ProjectOnboardingStatusResult, error) {
	return projectservice.ProjectOnboardingStatusResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) CreatePolicyEditProposal(context.Context, projectservice.CreatePolicyEditProposalInput) (entity.PolicyEditProposal, error) {
	return entity.PolicyEditProposal{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) CreatePolicyOverride(context.Context, projectservice.CreatePolicyOverrideInput) (entity.PolicyOverride, error) {
	return entity.PolicyOverride{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) CancelPolicyOverride(context.Context, projectservice.CancelPolicyOverrideInput) (entity.PolicyOverride, error) {
	return entity.PolicyOverride{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListPolicyOverrides(context.Context, projectservice.ListPolicyOverridesInput) (projectservice.ListPolicyOverridesResult, error) {
	return projectservice.ListPolicyOverridesResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) PutDocumentationSource(context.Context, projectservice.PutDocumentationSourceInput) (entity.DocumentationSource, error) {
	return entity.DocumentationSource{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetDocumentationSource(context.Context, uuid.UUID, value.QueryMeta) (entity.DocumentationSource, error) {
	return entity.DocumentationSource{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListDocumentationSources(context.Context, projectservice.ListDocumentationSourcesInput) (projectservice.ListDocumentationSourcesResult, error) {
	return projectservice.ListDocumentationSourcesResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetWorkspacePolicy(context.Context, projectservice.GetWorkspacePolicyInput) (entity.WorkspacePolicy, error) {
	return entity.WorkspacePolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) PutBranchRules(context.Context, projectservice.PutBranchRulesInput) (entity.BranchRules, error) {
	return entity.BranchRules{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetBranchRules(context.Context, uuid.UUID, value.QueryMeta) (entity.BranchRules, error) {
	return entity.BranchRules{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListBranchRules(context.Context, projectservice.ListBranchRulesInput) (projectservice.ListBranchRulesResult, error) {
	return projectservice.ListBranchRulesResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) PutReleasePolicy(context.Context, projectservice.PutReleasePolicyInput) (entity.ReleasePolicy, error) {
	return entity.ReleasePolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetReleasePolicy(context.Context, uuid.UUID, value.QueryMeta) (entity.ReleasePolicy, error) {
	return entity.ReleasePolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListReleasePolicies(context.Context, projectservice.ListReleasePoliciesInput) (projectservice.ListReleasePoliciesResult, error) {
	return projectservice.ListReleasePoliciesResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) PutReleaseLine(context.Context, projectservice.PutReleaseLineInput) (entity.ReleaseLine, error) {
	return entity.ReleaseLine{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetReleaseLine(context.Context, uuid.UUID, value.QueryMeta) (entity.ReleaseLine, error) {
	return entity.ReleaseLine{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListReleaseLines(context.Context, projectservice.ListReleaseLinesInput) (projectservice.ListReleaseLinesResult, error) {
	return projectservice.ListReleaseLinesResult{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) PutPlacementPolicy(context.Context, projectservice.PutPlacementPolicyInput) (entity.PlacementPolicy, error) {
	return entity.PlacementPolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) GetPlacementPolicy(context.Context, uuid.UUID, value.QueryMeta) (entity.PlacementPolicy, error) {
	return entity.PlacementPolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListPlacementPolicies(context.Context, projectservice.ListPlacementPoliciesInput) (projectservice.ListPlacementPoliciesResult, error) {
	return projectservice.ListPlacementPoliciesResult{}, errs.ErrInvalidArgument
}

var _ projectService = (*fakeProjectService)(nil)
