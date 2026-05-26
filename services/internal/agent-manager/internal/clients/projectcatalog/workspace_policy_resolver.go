// Package projectcatalog adapts project-catalog reads to agent-manager workspace policy inputs.
package projectcatalog

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
)

const (
	callerID           = "agent-manager"
	defaultReadTimeout = 3 * time.Second
)

// Config contains project-catalog client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type projectCatalogClient interface {
	GetWorkspacePolicy(context.Context, *projectsv1.GetWorkspacePolicyRequest, ...grpc.CallOption) (*projectsv1.WorkspacePolicyResponse, error)
}

// WorkspacePolicyResolver reads checked source policy from project-catalog.
type WorkspacePolicyResolver struct {
	client    projectCatalogClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.WorkspacePolicyResolver = (*WorkspacePolicyResolver)(nil)

// NewConnection creates a gRPC connection to project-catalog.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "project-catalog")
}

// NewWorkspacePolicyResolver creates a project-catalog workspace policy resolver.
func NewWorkspacePolicyResolver(client projectsv1.ProjectCatalogServiceClient, cfg Config) (*WorkspacePolicyResolver, error) {
	return newWorkspacePolicyResolver(client, cfg)
}

func newWorkspacePolicyResolver(client projectCatalogClient, cfg Config) (*WorkspacePolicyResolver, error) {
	settings, err := grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultReadTimeout, "project-catalog")
	if err != nil {
		return nil, err
	}
	resolver := WorkspacePolicyResolver{client: client}
	resolver.authToken = settings.AuthToken
	resolver.timeout = settings.Timeout
	return &resolver, nil
}

// ResolveWorkspacePolicy returns code/documentation source refs without policy payload text.
func (r *WorkspacePolicyResolver) ResolveWorkspacePolicy(ctx context.Context, input agentservice.WorkspacePolicyResolutionInput) (agentservice.WorkspacePolicySnapshot, error) {
	if r == nil || r.client == nil {
		return agentservice.WorkspacePolicySnapshot{}, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(r.outgoingContext(ctx), r.timeout)
	defer cancel()
	response, err := r.client.GetWorkspacePolicy(callCtx, &projectsv1.GetWorkspacePolicyRequest{
		ProjectId:               input.ProjectID.String(),
		RepositoryIds:           uuidStrings(input.RepositoryIDs),
		ServiceKeys:             trimmedStrings(input.ServiceKeys),
		IncludeGuidancePackages: input.IncludeGuidancePackages,
		Meta:                    queryMeta(input.Meta),
	})
	if err != nil {
		return agentservice.WorkspacePolicySnapshot{}, mapProjectCatalogError(err)
	}
	return workspacePolicySnapshot(response.GetWorkspacePolicy())
}

func (r *WorkspacePolicyResolver) outgoingContext(ctx context.Context) context.Context {
	return grpcclient.OutgoingContext(ctx, r.authToken, callerID)
}

func workspacePolicySnapshot(policy *projectsv1.WorkspacePolicy) (agentservice.WorkspacePolicySnapshot, error) {
	if policy == nil {
		return agentservice.WorkspacePolicySnapshot{}, errs.ErrDependencyUnavailable
	}
	projectID, err := uuid.Parse(strings.TrimSpace(policy.GetProjectId()))
	if err != nil || projectID == uuid.Nil {
		return agentservice.WorkspacePolicySnapshot{}, errs.ErrDependencyUnavailable
	}
	codeSources, err := codeSourcesFromProto(policy.GetCodeSources())
	if err != nil {
		return agentservice.WorkspacePolicySnapshot{}, err
	}
	documentationSources, err := documentationSourcesFromProto(policy.GetDocumentationSources())
	if err != nil {
		return agentservice.WorkspacePolicySnapshot{}, err
	}
	overrides := make([]agentservice.PolicyOverrideRef, 0, len(policy.GetActivePolicyOverrides()))
	for _, override := range policy.GetActivePolicyOverrides() {
		id := strings.TrimSpace(override.GetPolicyOverrideId())
		if id != "" {
			overrides = append(overrides, agentservice.PolicyOverrideRef{ID: id})
		}
	}
	return agentservice.WorkspacePolicySnapshot{
		ProjectID:             projectID,
		CodeSources:           codeSources,
		DocumentationSources:  documentationSources,
		GuidancePackageRefs:   trimmedStrings(policy.GetGuidancePackageRefs()),
		ActivePolicyOverrides: overrides,
		PolicyVersion:         policy.GetPolicyVersion(),
	}, nil
}

func codeSourcesFromProto(sources []*projectsv1.WorkspaceCodeSource) ([]agentservice.WorkspaceCodeSource, error) {
	result := make([]agentservice.WorkspaceCodeSource, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		repositoryID, err := uuid.Parse(strings.TrimSpace(source.GetRepositoryId()))
		if err != nil || repositoryID == uuid.Nil {
			return nil, errs.ErrDependencyUnavailable
		}
		result = append(result, agentservice.WorkspaceCodeSource{
			RepositoryID:  repositoryID,
			Provider:      repositoryProvider(source.GetProvider()),
			ProviderOwner: strings.TrimSpace(source.GetProviderOwner()),
			ProviderName:  strings.TrimSpace(source.GetProviderName()),
			DefaultBranch: strings.TrimSpace(source.GetDefaultBranch()),
			LocalPath:     strings.TrimSpace(source.GetLocalPath()),
			AccessMode:    sourceAccess(source.GetAccessMode()),
		})
	}
	return result, nil
}

func documentationSourcesFromProto(sources []*projectsv1.WorkspaceDocumentationSource) ([]agentservice.WorkspaceDocumentationSource, error) {
	result := make([]agentservice.WorkspaceDocumentationSource, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		documentationSourceID, err := uuid.Parse(strings.TrimSpace(source.GetDocumentationSourceId()))
		if err != nil || documentationSourceID == uuid.Nil {
			return nil, errs.ErrDependencyUnavailable
		}
		repositoryID, err := optionalUUID(source.GetRepositoryId())
		if err != nil {
			return nil, errs.ErrDependencyUnavailable
		}
		result = append(result, agentservice.WorkspaceDocumentationSource{
			DocumentationSourceID: documentationSourceID,
			RepositoryID:          repositoryID,
			ScopeType:             strings.TrimSpace(source.GetScopeType().String()),
			ScopeID:               strings.TrimSpace(source.GetScopeId()),
			LocalPath:             strings.TrimSpace(source.GetLocalPath()),
			AccessMode:            sourceAccess(source.GetAccessMode()),
		})
	}
	return result, nil
}

func repositoryProvider(provider projectsv1.RepositoryProvider) string {
	switch provider {
	case projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITHUB:
		return "github"
	case projectsv1.RepositoryProvider_REPOSITORY_PROVIDER_GITLAB:
		return "gitlab"
	default:
		return ""
	}
}

func sourceAccess(mode projectsv1.SourceAccessMode) string {
	switch mode {
	case projectsv1.SourceAccessMode_SOURCE_ACCESS_MODE_WRITE:
		return agentservice.WorkspaceSourceAccessWrite
	default:
		return agentservice.WorkspaceSourceAccessRead
	}
}

func queryMeta(meta value.CommandMeta) *projectsv1.QueryMeta {
	return &projectsv1.QueryMeta{
		Actor: &projectsv1.Actor{
			Type: strings.TrimSpace(meta.Actor.Type),
			Id:   strings.TrimSpace(meta.Actor.ID),
		},
		RequestContext: &projectsv1.RequestContext{Source: callerID},
	}
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			result = append(result, id.String())
		}
	}
	return result
}

func trimmedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func optionalUUID(text string) (*uuid.UUID, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	id, err := uuid.Parse(trimmed)
	if err != nil || id == uuid.Nil {
		return nil, errs.ErrDependencyUnavailable
	}
	return &id, nil
}

func mapProjectCatalogError(err error) error {
	return grpcclient.MapReadError(err, "project-catalog read failed")
}
