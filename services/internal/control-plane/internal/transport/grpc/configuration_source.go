package grpc

import (
	"context"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/structpb"
)

func sourceInput(ref string, source *cp.ManagedConfigurationGitSourceInput) command.ManagedConfigurationGitSourceInput {
	return command.ManagedConfigurationGitSourceInput{ConfigurationRef: ref, ConnectionRef: source.GetConnectionRef(), ExpectedConnectionVersion: source.GetExpectedConnectionVersion(),
		RepositoryRef: source.GetRepositoryRef(), RefName: source.GetRefName(), Path: source.GetPath(), ContentFormat: source.GetContentFormat()}
}
func (server *Server) sourceMutation(ctx context.Context, method string, kind command.Kind, mutation *cp.MutationContext, input command.ManagedConfigurationGitSourceInput) (*cp.ManagedConfigurationSet, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, input)
	if err != nil {
		return nil, err
	}
	return castManagedConfiguration(result.ManagedConfiguration), nil
}
func (server *Server) ConfigureRoleImageGitSource(ctx context.Context, r *cp.ConfigureRoleImageGitSourceRequest) (*cp.ConfigureRoleImageGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_ConfigureRoleImageGitSource_FullMethodName, command.ConfigureRoleImageGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), r.GetSource()))
	return &cp.ConfigureRoleImageGitSourceResponse{Configuration: result}, err
}
func (server *Server) ConfigureIntegrationDefinitionGitSource(ctx context.Context, r *cp.ConfigureIntegrationDefinitionGitSourceRequest) (*cp.ConfigureIntegrationDefinitionGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_ConfigureIntegrationDefinitionGitSource_FullMethodName, command.ConfigureIntegrationDefinitionGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), r.GetSource()))
	return &cp.ConfigureIntegrationDefinitionGitSourceResponse{Configuration: result}, err
}
func (server *Server) RefreshRoleImageGitSource(ctx context.Context, r *cp.RefreshRoleImageGitSourceRequest) (*cp.RefreshRoleImageGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_RefreshRoleImageGitSource_FullMethodName, command.RefreshRoleImageGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), nil))
	return &cp.RefreshRoleImageGitSourceResponse{Configuration: result}, err
}
func (server *Server) RefreshIntegrationDefinitionGitSource(ctx context.Context, r *cp.RefreshIntegrationDefinitionGitSourceRequest) (*cp.RefreshIntegrationDefinitionGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_RefreshIntegrationDefinitionGitSource_FullMethodName, command.RefreshIntegrationDefinitionGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), nil))
	return &cp.RefreshIntegrationDefinitionGitSourceResponse{Configuration: result}, err
}
func castConfigurationSource(s *entity.ManagedConfigurationGitSource) *cp.ManagedConfigurationGitSource {
	if s == nil {
		return nil
	}
	result := &cp.ManagedConfigurationGitSource{Ref: s.Ref, Version: s.Version, Generation: s.Generation, ConnectionRef: s.ConnectionRef, ProviderKey: s.ProviderKey, RepositoryRef: s.RepositoryRef, RefName: s.RefName, Path: s.Path,
		State:             cp.ManagedConfigurationSourceState(cp.ManagedConfigurationSourceState_value["MANAGED_CONFIGURATION_SOURCE_STATE_"+s.State]),
		AcceptedCommitSha: s.AcceptedCommitSHA, AcceptedContentSha256: s.AcceptedContentSHA256, AcceptedRevisionRef: s.AcceptedRevisionRef,
		FailureCode: cp.ManagedConfigurationSourceFailure(cp.ManagedConfigurationSourceFailure_value["MANAGED_CONFIGURATION_SOURCE_FAILURE_"+s.FailureCode])}
	if s.SyncedAt != nil {
		result.SyncedAt = timestamp(*s.SyncedAt)
	}
	return result
}
func castSourceLease(l entity.ManagedConfigurationSourceLease) *cp.ManagedConfigurationSourceLease {
	return &cp.ManagedConfigurationSourceLease{WorkRef: l.WorkRef, SourceGeneration: l.SourceGeneration, Attempt: l.Attempt, ClaimGeneration: l.ClaimGeneration, Claimant: l.Claimant, Fence: l.Fence, ExpiresAt: timestamp(l.ExpiresAt)}
}
func sourceLease(l *cp.ManagedConfigurationSourceLease) entity.ManagedConfigurationSourceLease {
	expires := time.Time{}
	if l.GetExpiresAt() != nil && l.GetExpiresAt().CheckValid() == nil {
		expires = l.GetExpiresAt().AsTime()
	}
	return entity.ManagedConfigurationSourceLease{WorkRef: l.GetWorkRef(), SourceGeneration: l.GetSourceGeneration(), Attempt: l.GetAttempt(), ClaimGeneration: l.GetClaimGeneration(), Claimant: l.GetClaimant(), Fence: l.GetFence(), ExpiresAt: expires}
}
func (server *Server) ClaimManagedConfigurationSourceWork(ctx context.Context, r *cp.ClaimManagedConfigurationSourceWorkRequest) (*cp.ClaimManagedConfigurationSourceWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationSourceWorkService_ClaimManagedConfigurationSourceWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	work, err := server.service.ClaimConfigurationSourceWork(ctx, p, r.GetClaimant(), r.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	result := &cp.ClaimManagedConfigurationSourceWorkResponse{}
	for _, w := range work {
		configuration, err := structpb.NewStruct(w.PublicConfiguration)
		if err != nil {
			return nil, transportError(err)
		}
		result.Work = append(result.Work, &cp.ManagedConfigurationSourceWork{Lease: castSourceLease(w.Lease), SourceRef: w.SourceRef, ConfigurationRef: w.ConfigurationRef,
			Kind: cp.ManagedConfigurationKind(cp.ManagedConfigurationKind_value["MANAGED_CONFIGURATION_KIND_"+w.Kind]), ConnectionRef: w.ConnectionRef, ConnectionVersion: w.ConnectionVersion,
			DefinitionKey: w.DefinitionKey, DefinitionVersion: w.DefinitionVersion, DefinitionDigest: w.DefinitionDigest, DefinitionPackage: w.DefinitionPackage, PublicConfiguration: configuration,
			CredentialRevision: castIntegrationCredential(w.CredentialRevision), RepositoryRef: w.RepositoryRef, RefName: w.RefName, Path: w.Path, PreviousCommitSha: w.PreviousCommitSHA,
			ContentFormat: w.ContentFormat, MaximumContentBytes: w.MaximumContentBytes, Deadline: timestamp(w.Deadline)})
	}
	return result, nil
}
func (server *Server) RenewManagedConfigurationSourceWork(ctx context.Context, r *cp.RenewManagedConfigurationSourceWorkRequest) (*cp.RenewManagedConfigurationSourceWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationSourceWorkService_RenewManagedConfigurationSourceWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.RenewConfigurationSourceWork(ctx, p, sourceLease(r.GetLease()))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RenewManagedConfigurationSourceWorkResponse{Lease: castSourceLease(result)}, nil
}
func (server *Server) CompleteManagedConfigurationSourceWork(ctx context.Context, r *cp.CompleteManagedConfigurationSourceWorkRequest) (*cp.CompleteManagedConfigurationSourceWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationSourceWorkService_CompleteManagedConfigurationSourceWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.CompleteConfigurationSourceWork(ctx, p, port.ConfigurationSourceCompletion{Lease: sourceLease(r.GetLease()), CommitSHA: r.GetCommitSha(), ContentSHA256: r.GetContentSha256(), Content: r.GetContent(), Ancestry: strings.TrimPrefix(r.GetAncestry().String(), "MANAGED_CONFIGURATION_SOURCE_ANCESTRY_")})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.CompleteManagedConfigurationSourceWorkResponse{Source: castConfigurationSource(&result)}, nil
}
func (server *Server) FailManagedConfigurationSourceWork(ctx context.Context, r *cp.FailManagedConfigurationSourceWorkRequest) (*cp.FailManagedConfigurationSourceWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationSourceWorkService_FailManagedConfigurationSourceWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FailConfigurationSourceWork(ctx, p, sourceLease(r.GetLease()), strings.TrimPrefix(r.GetFailureCode().String(), "MANAGED_CONFIGURATION_SOURCE_FAILURE_"))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.FailManagedConfigurationSourceWorkResponse{Source: castConfigurationSource(&result)}, nil
}
