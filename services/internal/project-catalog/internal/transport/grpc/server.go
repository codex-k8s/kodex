package grpc

import (
	"context"

	"github.com/google/uuid"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
	grpccasters "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/transport/grpc/casters"
	grpcruntime "google.golang.org/grpc"
)

var _ projectsv1.ProjectCatalogServiceServer = (*Server)(nil)

type projectService interface {
	CreateProject(context.Context, projectservice.CreateProjectInput) (entity.Project, error)
	UpdateProject(context.Context, projectservice.UpdateProjectInput) (entity.Project, error)
	GetProject(context.Context, uuid.UUID, value.QueryMeta) (entity.Project, error)
	ListProjects(context.Context, projectservice.ListProjectsInput) (projectservice.ListProjectsResult, error)
	AttachRepository(context.Context, projectservice.AttachRepositoryInput) (entity.RepositoryBinding, error)
	CreateProviderRepository(context.Context, projectservice.CreateProviderRepositoryInput) (projectservice.RepositoryProviderCreateResult, error)
	CreateRepositoryBootstrapPullRequest(context.Context, projectservice.CreateRepositoryBootstrapPullRequestInput) (projectservice.RepositoryBootstrapPullRequestResult, error)
	UpdateRepository(context.Context, projectservice.UpdateRepositoryInput) (entity.RepositoryBinding, error)
	DetachRepository(context.Context, uuid.UUID, value.CommandMeta) (entity.RepositoryBinding, error)
	GetRepository(context.Context, uuid.UUID, value.QueryMeta) (entity.RepositoryBinding, error)
	ListRepositories(context.Context, projectservice.ListRepositoriesInput) (projectservice.ListRepositoriesResult, error)
	ImportServicesPolicy(context.Context, projectservice.ImportServicesPolicyInput) (entity.ServicesPolicy, error)
	ImportBootstrapServicesPolicy(context.Context, projectservice.ImportBootstrapServicesPolicyInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	ReconcileBootstrapMergeSignal(context.Context, projectservice.ReconcileBootstrapMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	ReconcileAdoptionMergeSignal(context.Context, projectservice.ReconcileAdoptionMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	GetSelfDeploySignal(context.Context, projectservice.GetSelfDeploySignalInput) (projectservice.SelfDeploySignalResult, error)
	GetSelfDeployBuildPlan(context.Context, projectservice.GetSelfDeployBuildPlanInput) (projectservice.SelfDeployBuildPlanResult, error)
	GetServicesPolicy(context.Context, projectservice.GetServicesPolicyInput) (entity.ServicesPolicy, error)
	ListServiceDescriptors(context.Context, projectservice.ListServiceDescriptorsInput) (projectservice.ListServiceDescriptorsResult, error)
	CreatePolicyEditProposal(context.Context, projectservice.CreatePolicyEditProposalInput) (entity.PolicyEditProposal, error)
	CreatePolicyOverride(context.Context, projectservice.CreatePolicyOverrideInput) (entity.PolicyOverride, error)
	CancelPolicyOverride(context.Context, projectservice.CancelPolicyOverrideInput) (entity.PolicyOverride, error)
	ListPolicyOverrides(context.Context, projectservice.ListPolicyOverridesInput) (projectservice.ListPolicyOverridesResult, error)
	PutDocumentationSource(context.Context, projectservice.PutDocumentationSourceInput) (entity.DocumentationSource, error)
	GetDocumentationSource(context.Context, uuid.UUID, value.QueryMeta) (entity.DocumentationSource, error)
	ListDocumentationSources(context.Context, projectservice.ListDocumentationSourcesInput) (projectservice.ListDocumentationSourcesResult, error)
	GetWorkspacePolicy(context.Context, projectservice.GetWorkspacePolicyInput) (entity.WorkspacePolicy, error)
	PutBranchRules(context.Context, projectservice.PutBranchRulesInput) (entity.BranchRules, error)
	GetBranchRules(context.Context, uuid.UUID, value.QueryMeta) (entity.BranchRules, error)
	ListBranchRules(context.Context, projectservice.ListBranchRulesInput) (projectservice.ListBranchRulesResult, error)
	PutReleasePolicy(context.Context, projectservice.PutReleasePolicyInput) (entity.ReleasePolicy, error)
	GetReleasePolicy(context.Context, uuid.UUID, value.QueryMeta) (entity.ReleasePolicy, error)
	ListReleasePolicies(context.Context, projectservice.ListReleasePoliciesInput) (projectservice.ListReleasePoliciesResult, error)
	PutReleaseLine(context.Context, projectservice.PutReleaseLineInput) (entity.ReleaseLine, error)
	GetReleaseLine(context.Context, uuid.UUID, value.QueryMeta) (entity.ReleaseLine, error)
	ListReleaseLines(context.Context, projectservice.ListReleaseLinesInput) (projectservice.ListReleaseLinesResult, error)
	PutPlacementPolicy(context.Context, projectservice.PutPlacementPolicyInput) (entity.PlacementPolicy, error)
	GetPlacementPolicy(context.Context, uuid.UUID, value.QueryMeta) (entity.PlacementPolicy, error)
	ListPlacementPolicies(context.Context, projectservice.ListPlacementPoliciesInput) (projectservice.ListPlacementPoliciesResult, error)
}

// Server implements the generated ProjectCatalogServiceServer contract.
type Server struct {
	projectsv1.UnimplementedProjectCatalogServiceServer
	service projectService
}

type unaryCaster[Request any, Input any] func(*Request) (Input, error)
type unaryCaller[Input any, Output any] func(context.Context, Input) (Output, error)
type unaryResponder[Output any, Response any] func(Output) *Response

// NewServer creates a project-catalog gRPC transport around domain use cases.
func NewServer(service projectService) *Server {
	if service == nil {
		panic("project-catalog grpc service is required")
	}
	return &Server{service: service}
}

// RegisterProjectCatalogService registers project-catalog gRPC handlers.
func RegisterProjectCatalogService(registrar grpcruntime.ServiceRegistrar, service projectService) {
	projectsv1.RegisterProjectCatalogServiceServer(registrar, NewServer(service))
}

// CreateProject creates a project in an organization.
func (s *Server) CreateProject(ctx context.Context, request *projectsv1.CreateProjectRequest) (*projectsv1.ProjectResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreateProjectInput, s.service.CreateProject, grpccasters.ProjectResponse)
}

// UpdateProject changes safe project fields and lifecycle status.
func (s *Server) UpdateProject(ctx context.Context, request *projectsv1.UpdateProjectRequest) (*projectsv1.ProjectResponse, error) {
	return handleUnary(ctx, request, grpccasters.UpdateProjectInput, s.service.UpdateProject, grpccasters.ProjectResponse)
}

// GetProject returns authoritative project state.
func (s *Server) GetProject(ctx context.Context, request *projectsv1.GetProjectRequest) (*projectsv1.ProjectResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetProjectInput, s.service.GetProject, grpccasters.ProjectResponse)
}

// ListProjects returns projects visible in an organization or filter scope.
func (s *Server) ListProjects(ctx context.Context, request *projectsv1.ListProjectsRequest) (*projectsv1.ListProjectsResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListProjectsInput, s.service.ListProjects, grpccasters.ListProjectsResponse)
}

// AttachRepository binds a provider repository to a project.
func (s *Server) AttachRepository(ctx context.Context, request *projectsv1.AttachRepositoryRequest) (*projectsv1.RepositoryResponse, error) {
	return handleUnary(ctx, request, grpccasters.AttachRepositoryInput, s.service.AttachRepository, grpccasters.RepositoryResponse)
}

// CreateProviderRepository asks provider-hub to create a repository and records project binding refs.
func (s *Server) CreateProviderRepository(ctx context.Context, request *projectsv1.CreateProviderRepositoryRequest) (*projectsv1.RepositoryProviderCreateResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreateProviderRepositoryInput, s.service.CreateProviderRepository, grpccasters.RepositoryProviderCreateResponse)
}

// CreateRepositoryBootstrapPullRequest asks provider-hub to open or update a bootstrap PR for an existing binding.
func (s *Server) CreateRepositoryBootstrapPullRequest(ctx context.Context, request *projectsv1.CreateRepositoryBootstrapPullRequestRequest) (*projectsv1.RepositoryBootstrapPullRequestResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreateRepositoryBootstrapPullRequestInput, s.service.CreateRepositoryBootstrapPullRequest, grpccasters.RepositoryBootstrapPullRequestResponse)
}

// UpdateRepository changes safe repository binding fields.
func (s *Server) UpdateRepository(ctx context.Context, request *projectsv1.UpdateRepositoryRequest) (*projectsv1.RepositoryResponse, error) {
	return handleUnary(ctx, request, grpccasters.UpdateRepositoryInput, s.service.UpdateRepository, grpccasters.RepositoryResponse)
}

// DetachRepository archives a repository binding and removes it from active project policy.
func (s *Server) DetachRepository(ctx context.Context, request *projectsv1.DetachRepositoryRequest) (*projectsv1.RepositoryResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.DetachRepositoryInput, s.service.DetachRepository, grpccasters.RepositoryResponse)
}

// GetRepository returns authoritative repository binding state.
func (s *Server) GetRepository(ctx context.Context, request *projectsv1.GetRepositoryRequest) (*projectsv1.RepositoryResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetRepositoryInput, s.service.GetRepository, grpccasters.RepositoryResponse)
}

// ListRepositories returns repository bindings for a project.
func (s *Server) ListRepositories(ctx context.Context, request *projectsv1.ListRepositoriesRequest) (*projectsv1.ListRepositoriesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListRepositoriesInput, s.service.ListRepositories, grpccasters.ListRepositoriesResponse)
}

// ImportServicesPolicy stores a checked services.yaml projection.
func (s *Server) ImportServicesPolicy(ctx context.Context, request *projectsv1.ImportServicesPolicyRequest) (*projectsv1.ServicesPolicyResponse, error) {
	return handleUnary(ctx, request, grpccasters.ImportServicesPolicyInput, s.service.ImportServicesPolicy, grpccasters.ServicesPolicyResponse)
}

// ImportBootstrapServicesPolicy imports checked services.yaml after bootstrap merge and activates the repository binding.
func (s *Server) ImportBootstrapServicesPolicy(ctx context.Context, request *projectsv1.ImportBootstrapServicesPolicyRequest) (*projectsv1.BootstrapServicesPolicyImportResponse, error) {
	return handleUnary(ctx, request, grpccasters.ImportBootstrapServicesPolicyInput, s.service.ImportBootstrapServicesPolicy, grpccasters.BootstrapServicesPolicyImportResponse)
}

// ReconcileBootstrapMergeSignal imports checked services.yaml from a safe provider bootstrap merge signal.
func (s *Server) ReconcileBootstrapMergeSignal(ctx context.Context, request *projectsv1.ReconcileBootstrapMergeSignalRequest) (*projectsv1.BootstrapServicesPolicyImportResponse, error) {
	return handleUnary(ctx, request, grpccasters.ReconcileBootstrapMergeSignalInput, s.service.ReconcileBootstrapMergeSignal, grpccasters.BootstrapServicesPolicyImportResponse)
}

// ReconcileAdoptionMergeSignal imports checked services.yaml from a safe provider adoption merge signal.
func (s *Server) ReconcileAdoptionMergeSignal(ctx context.Context, request *projectsv1.ReconcileAdoptionMergeSignalRequest) (*projectsv1.BootstrapServicesPolicyImportResponse, error) {
	return handleUnary(ctx, request, grpccasters.ReconcileAdoptionMergeSignalInput, s.service.ReconcileAdoptionMergeSignal, grpccasters.BootstrapServicesPolicyImportResponse)
}

// GetSelfDeploySignal возвращает project-side enrichment для safe provider repository change signal.
func (s *Server) GetSelfDeploySignal(ctx context.Context, request *projectsv1.GetSelfDeploySignalRequest) (*projectsv1.SelfDeploySignalResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetSelfDeploySignalInput, s.service.GetSelfDeploySignal, grpccasters.SelfDeploySignalResponse)
}

// GetSelfDeployBuildPlan возвращает checked build plan для self-deploy runtime jobs.
func (s *Server) GetSelfDeployBuildPlan(ctx context.Context, request *projectsv1.GetSelfDeployBuildPlanRequest) (*projectsv1.SelfDeployBuildPlanResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetSelfDeployBuildPlanInput, s.service.GetSelfDeployBuildPlan, grpccasters.SelfDeployBuildPlanResponse)
}

// GetServicesPolicy returns a checked services.yaml projection.
func (s *Server) GetServicesPolicy(ctx context.Context, request *projectsv1.GetServicesPolicyRequest) (*projectsv1.ServicesPolicyResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetServicesPolicyInput, s.service.GetServicesPolicy, grpccasters.ServicesPolicyResponse)
}

// ListServiceDescriptors returns typed service descriptors from active policy.
func (s *Server) ListServiceDescriptors(ctx context.Context, request *projectsv1.ListServiceDescriptorsRequest) (*projectsv1.ListServiceDescriptorsResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListServiceDescriptorsInput, s.service.ListServiceDescriptors, grpccasters.ListServiceDescriptorsResponse)
}

// CreatePolicyEditProposal creates a request to change services.yaml through PR.
func (s *Server) CreatePolicyEditProposal(ctx context.Context, request *projectsv1.CreatePolicyEditProposalRequest) (*projectsv1.PolicyEditProposalResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreatePolicyEditProposalInput, s.service.CreatePolicyEditProposal, grpccasters.PolicyEditProposalResponse)
}

// CreatePolicyOverride creates a time-bound operator override.
func (s *Server) CreatePolicyOverride(ctx context.Context, request *projectsv1.CreatePolicyOverrideRequest) (*projectsv1.PolicyOverrideResponse, error) {
	return handleUnary(ctx, request, grpccasters.CreatePolicyOverrideInput, s.service.CreatePolicyOverride, grpccasters.PolicyOverrideResponse)
}

// CancelPolicyOverride cancels an active operator override.
func (s *Server) CancelPolicyOverride(ctx context.Context, request *projectsv1.CancelPolicyOverrideRequest) (*projectsv1.PolicyOverrideResponse, error) {
	return handleUnary(ctx, request, grpccasters.CancelPolicyOverrideInput, s.service.CancelPolicyOverride, grpccasters.PolicyOverrideResponse)
}

// ListPolicyOverrides returns visible operator overrides for a project.
func (s *Server) ListPolicyOverrides(ctx context.Context, request *projectsv1.ListPolicyOverridesRequest) (*projectsv1.ListPolicyOverridesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListPolicyOverridesInput, s.service.ListPolicyOverrides, grpccasters.ListPolicyOverridesResponse)
}

// PutDocumentationSource creates or updates a documentation source.
func (s *Server) PutDocumentationSource(ctx context.Context, request *projectsv1.PutDocumentationSourceRequest) (*projectsv1.DocumentationSourceResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutDocumentationSourceInput, s.service.PutDocumentationSource, grpccasters.DocumentationSourceResponse)
}

// GetDocumentationSource returns a documentation source.
func (s *Server) GetDocumentationSource(ctx context.Context, request *projectsv1.GetDocumentationSourceRequest) (*projectsv1.DocumentationSourceResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetDocumentationSourceInput, s.service.GetDocumentationSource, grpccasters.DocumentationSourceResponse)
}

// ListDocumentationSources returns documentation sources for a project scope.
func (s *Server) ListDocumentationSources(ctx context.Context, request *projectsv1.ListDocumentationSourcesRequest) (*projectsv1.ListDocumentationSourcesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListDocumentationSourcesInput, s.service.ListDocumentationSources, grpccasters.ListDocumentationSourcesResponse)
}

// GetWorkspacePolicy returns source checkout policy for an agent workspace.
func (s *Server) GetWorkspacePolicy(ctx context.Context, request *projectsv1.GetWorkspacePolicyRequest) (*projectsv1.WorkspacePolicyResponse, error) {
	return handleUnary(ctx, request, grpccasters.GetWorkspacePolicyInput, s.service.GetWorkspacePolicy, grpccasters.WorkspacePolicyResponse)
}

// PutBranchRules creates or updates branch rules.
func (s *Server) PutBranchRules(ctx context.Context, request *projectsv1.PutBranchRulesRequest) (*projectsv1.BranchRulesResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutBranchRulesInput, s.service.PutBranchRules, grpccasters.BranchRulesResponse)
}

// GetBranchRules returns branch rules.
func (s *Server) GetBranchRules(ctx context.Context, request *projectsv1.GetBranchRulesRequest) (*projectsv1.BranchRulesResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetBranchRulesInput, s.service.GetBranchRules, grpccasters.BranchRulesResponse)
}

// ListBranchRules returns active branch rules for a project or repository.
func (s *Server) ListBranchRules(ctx context.Context, request *projectsv1.ListBranchRulesRequest) (*projectsv1.ListBranchRulesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListBranchRulesInput, s.service.ListBranchRules, grpccasters.ListBranchRulesResponse)
}

// PutReleasePolicy creates or updates a release policy.
func (s *Server) PutReleasePolicy(ctx context.Context, request *projectsv1.PutReleasePolicyRequest) (*projectsv1.ReleasePolicyResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutReleasePolicyInput, s.service.PutReleasePolicy, grpccasters.ReleasePolicyResponse)
}

// GetReleasePolicy returns a release policy.
func (s *Server) GetReleasePolicy(ctx context.Context, request *projectsv1.GetReleasePolicyRequest) (*projectsv1.ReleasePolicyResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetReleasePolicyInput, s.service.GetReleasePolicy, grpccasters.ReleasePolicyResponse)
}

// ListReleasePolicies returns release policies for a project.
func (s *Server) ListReleasePolicies(ctx context.Context, request *projectsv1.ListReleasePoliciesRequest) (*projectsv1.ListReleasePoliciesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListReleasePoliciesInput, s.service.ListReleasePolicies, grpccasters.ListReleasePoliciesResponse)
}

// PutReleaseLine creates or updates a concrete release line.
func (s *Server) PutReleaseLine(ctx context.Context, request *projectsv1.PutReleaseLineRequest) (*projectsv1.ReleaseLineResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutReleaseLineInput, s.service.PutReleaseLine, grpccasters.ReleaseLineResponse)
}

// GetReleaseLine returns a concrete release line.
func (s *Server) GetReleaseLine(ctx context.Context, request *projectsv1.GetReleaseLineRequest) (*projectsv1.ReleaseLineResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetReleaseLineInput, s.service.GetReleaseLine, grpccasters.ReleaseLineResponse)
}

// ListReleaseLines returns release lines for a project or release policy.
func (s *Server) ListReleaseLines(ctx context.Context, request *projectsv1.ListReleaseLinesRequest) (*projectsv1.ListReleaseLinesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListReleaseLinesInput, s.service.ListReleaseLines, grpccasters.ListReleaseLinesResponse)
}

// PutPlacementPolicy creates or updates placement policy.
func (s *Server) PutPlacementPolicy(ctx context.Context, request *projectsv1.PutPlacementPolicyRequest) (*projectsv1.PlacementPolicyResponse, error) {
	return handleUnary(ctx, request, grpccasters.PutPlacementPolicyInput, s.service.PutPlacementPolicy, grpccasters.PlacementPolicyResponse)
}

// GetPlacementPolicy returns placement policy.
func (s *Server) GetPlacementPolicy(ctx context.Context, request *projectsv1.GetPlacementPolicyRequest) (*projectsv1.PlacementPolicyResponse, error) {
	return handleUnaryPair(ctx, request, grpccasters.GetPlacementPolicyInput, s.service.GetPlacementPolicy, grpccasters.PlacementPolicyResponse)
}

// ListPlacementPolicies returns placement policies for a project scope.
func (s *Server) ListPlacementPolicies(ctx context.Context, request *projectsv1.ListPlacementPoliciesRequest) (*projectsv1.ListPlacementPoliciesResponse, error) {
	return handleUnary(ctx, request, grpccasters.ListPlacementPoliciesInput, s.service.ListPlacementPolicies, grpccasters.ListPlacementPoliciesResponse)
}

func handleUnary[Request any, Input any, Output any, Response any](
	ctx context.Context,
	request *Request,
	cast unaryCaster[Request, Input],
	call unaryCaller[Input, Output],
	respond unaryResponder[Output, Response],
) (*Response, error) {
	return finishUnary(func() (Output, error) {
		domainInput, err := cast(request)
		if err != nil {
			var zero Output
			return zero, err
		}
		return call(ctx, domainInput)
	}, respond)
}

func finishUnary[Output any, Response any](
	run func() (Output, error),
	respond func(Output) *Response,
) (*Response, error) {
	domainOutput, err := run()
	if err != nil {
		return nil, err
	}
	return respond(domainOutput), nil
}

func handleUnaryPair[Request any, First any, Second any, Output any, Response any](
	ctx context.Context,
	request *Request,
	cast func(*Request) (First, Second, error),
	call func(context.Context, First, Second) (Output, error),
	respond func(Output) *Response,
) (*Response, error) {
	first, second, err := cast(request)
	if err != nil {
		return nil, err
	}
	output, err := call(ctx, first, second)
	if err != nil {
		return nil, err
	}
	return respond(output), nil
}
