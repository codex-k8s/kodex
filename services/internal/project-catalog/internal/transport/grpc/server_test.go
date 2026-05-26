package grpc

import (
	"context"
	"errors"
	"testing"

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

type fakeProjectService struct {
	unimplementedProjectService
	createProject   func(context.Context, projectservice.CreateProjectInput) (entity.Project, error)
	createBootstrap func(context.Context, projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error)
}

func (s *fakeProjectService) CreateProject(ctx context.Context, input projectservice.CreateProjectInput) (entity.Project, error) {
	if s.createProject == nil {
		return entity.Project{}, errs.ErrInvalidArgument
	}
	return s.createProject(ctx, input)
}

func (s *fakeProjectService) CreateRepositoryBootstrapPullRequest(ctx context.Context, input projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error) {
	if s.createBootstrap == nil {
		return projectservice.RepositoryBootstrapPullRequestResult{}, errs.ErrInvalidArgument
	}
	return s.createBootstrap(ctx, input)
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

func (unimplementedProjectService) GetServicesPolicy(context.Context, projectservice.GetServicesPolicyInput) (entity.ServicesPolicy, error) {
	return entity.ServicesPolicy{}, errs.ErrInvalidArgument
}

func (unimplementedProjectService) ListServiceDescriptors(context.Context, projectservice.ListServiceDescriptorsInput) (projectservice.ListServiceDescriptorsResult, error) {
	return projectservice.ListServiceDescriptorsResult{}, errs.ErrInvalidArgument
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
