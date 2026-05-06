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
			if got := status.Code(errorToStatus(tc.err)); got != tc.code {
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
	createProject func(context.Context, projectservice.CreateProjectInput) (entity.Project, error)
}

func (s *fakeProjectService) CreateProject(ctx context.Context, input projectservice.CreateProjectInput) (entity.Project, error) {
	if s.createProject == nil {
		return entity.Project{}, errs.ErrInvalidArgument
	}
	return s.createProject(ctx, input)
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
