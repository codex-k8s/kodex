package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/structpb"
)

// sourceInput copies configuration/Git/connection locators and the expected connection version
// from a possibly absent source message without resolving ownership.
// sourceInput копирует configuration, Git и connection locators и expected connection version из
// необязательного source message без разрешения ownership.
func sourceInput(ref string, source *cp.ManagedConfigurationGitSourceInput) command.ManagedConfigurationGitSourceInput {
	return command.ManagedConfigurationGitSourceInput{ConfigurationRef: ref, ConnectionRef: source.GetConnectionRef(), ExpectedConnectionVersion: source.GetExpectedConnectionVersion(),
		RepositoryRef: source.GetRepositoryRef(), RefName: source.GetRefName(), Path: source.GetPath(), ContentFormat: source.GetContentFormat()}
}

// sourceMutation runs the specialized configuration-source command through exact-method execute
// and casts its managed configuration result.
// sourceMutation исполняет специализированную configuration-source command через execute точного
// метода и преобразует managed configuration result.
func (server *Server) sourceMutation(ctx context.Context, method string, kind command.Kind, mutation *cp.MutationContext, input command.ManagedConfigurationGitSourceInput) (*cp.ManagedConfigurationSet, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, input)
	if err != nil {
		return nil, err
	}
	return castManagedConfiguration(result.ManagedConfiguration), nil
}

// ConfigureRoleImageGitSource maps the role-image configuration locator and Git source pins into
// the dedicated owner source command; transport fields do not grant repository access.
// ConfigureRoleImageGitSource передаёт role-image configuration locator и Git source pins
// отдельной owner source command; transport fields не дают repository access.
func (server *Server) ConfigureRoleImageGitSource(ctx context.Context, r *cp.ConfigureRoleImageGitSourceRequest) (*cp.ConfigureRoleImageGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_ConfigureRoleImageGitSource_FullMethodName, command.ConfigureRoleImageGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), r.GetSource()))
	return &cp.ConfigureRoleImageGitSourceResponse{Configuration: result}, err
}

// ConfigureIntegrationDefinitionGitSource maps integration-definition Git source fields into the
// dedicated configure command with the caller mutation and verified method principal.
// ConfigureIntegrationDefinitionGitSource передаёт integration-definition Git source fields
// отдельной configure command с caller mutation и проверенным principal метода.
func (server *Server) ConfigureIntegrationDefinitionGitSource(ctx context.Context, r *cp.ConfigureIntegrationDefinitionGitSourceRequest) (*cp.ConfigureIntegrationDefinitionGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_ConfigureIntegrationDefinitionGitSource_FullMethodName, command.ConfigureIntegrationDefinitionGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), r.GetSource()))
	return &cp.ConfigureIntegrationDefinitionGitSourceResponse{Configuration: result}, err
}

// RefreshRoleImageGitSource requests the owner refresh transition for the role-image configuration
// using an otherwise empty source input, without performing a Git fetch here.
// RefreshRoleImageGitSource запрашивает owner refresh переход role-image configuration с остальным
// пустым source input без выполнения Git fetch в transport.
func (server *Server) RefreshRoleImageGitSource(ctx context.Context, r *cp.RefreshRoleImageGitSourceRequest) (*cp.RefreshRoleImageGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_RefreshRoleImageGitSource_FullMethodName, command.RefreshRoleImageGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), nil))
	return &cp.RefreshRoleImageGitSourceResponse{Configuration: result}, err
}

// RefreshIntegrationDefinitionGitSource requests the dedicated integration-definition source
// refresh while the owner schedules and fences subsequent work.
// RefreshIntegrationDefinitionGitSource запрашивает отдельное integration-definition source
// refresh, оставляя планирование и fencing последующей работы владельцу.
func (server *Server) RefreshIntegrationDefinitionGitSource(ctx context.Context, r *cp.RefreshIntegrationDefinitionGitSourceRequest) (*cp.RefreshIntegrationDefinitionGitSourceResponse, error) {
	result, err := server.sourceMutation(ctx, cp.PlatformCommandService_RefreshIntegrationDefinitionGitSource_FullMethodName, command.RefreshIntegrationDefinitionGitSource, r.GetMutation(), sourceInput(r.GetConfigurationRef(), nil))
	return &cp.RefreshIntegrationDefinitionGitSourceResponse{Configuration: result}, err
}

// castConfigurationSource preserves absent source presence and maps accepted
// commit/content/revision pins, state/failure enums and optional sync time from the owner
// projection.
// castConfigurationSource сохраняет presence отсутствующего source и преобразует accepted commit,
// content и revision pins, state и failure enums и необязательное sync time owner projection.
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

// castSourceLease copies the owner work/source/attempt/claim/fence/expiry tuple to the worker
// response without minting a lease.
// castSourceLease копирует owner work, source, attempt, claim, fence и expiry tuple в worker
// response без выдачи новой lease.
func castSourceLease(l entity.ManagedConfigurationSourceLease) *cp.ManagedConfigurationSourceLease {
	return &cp.ManagedConfigurationSourceLease{WorkRef: l.WorkRef, SourceGeneration: l.SourceGeneration, Attempt: l.Attempt, ClaimGeneration: l.ClaimGeneration, Claimant: l.Claimant, Fence: l.Fence, ExpiresAt: timestamp(l.ExpiresAt)}
}

// sourceLease copies the supplied lease tuple and converts only a CheckValid expiry, mapping
// absent/invalid time to zero for owner validation rather than proving lease authority.
// sourceLease копирует переданный lease tuple и преобразует только CheckValid expiry, отображая
// отсутствующее либо неверное время в zero для owner validation без доказательства lease
// authority.
func sourceLease(l *cp.ManagedConfigurationSourceLease) entity.ManagedConfigurationSourceLease {
	return entity.ManagedConfigurationSourceLease{WorkRef: l.GetWorkRef(), SourceGeneration: l.GetSourceGeneration(), Attempt: l.GetAttempt(), ClaimGeneration: l.GetClaimGeneration(), Claimant: l.GetClaimant(), Fence: l.GetFence(), ExpiresAt: leaseExpiryOrZero(l.GetExpiresAt())}
}

// ClaimManagedConfigurationSourceWork resolves the exact worker principal, delegates
// claimant/limit admission and projects owner work with package/public configuration and
// credential descriptors; Struct encoding failure returns no partial response.
// ClaimManagedConfigurationSourceWork разрешает principal точного worker, передаёт claimant и
// limit admission и проецирует owner work с package, public configuration и credential
// descriptors; ошибка Struct encoding не выдаёт частичный ответ.
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

// RenewManagedConfigurationSourceWork passes the exact-method principal and supplied lease tuple
// to owner renewal, returning the freshly granted owner lease only on success.
// RenewManagedConfigurationSourceWork передаёт principal точного метода и lease tuple owner
// renewal и возвращает свежую owner lease только при успехе.
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

// CompleteManagedConfigurationSourceWork passes lease, commit/content hashes, content and ancestry
// to owner completion; a worker-reported snapshot is not accepted as authority by the transport
// itself.
// CompleteManagedConfigurationSourceWork передаёт lease, commit и content hashes, content и
// ancestry owner completion; transport самостоятельно не признаёт worker snapshot полномочием.
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

// FailManagedConfigurationSourceWork passes the lease and generated failure suffix to the owner
// failure transition and returns the resulting source projection.
// FailManagedConfigurationSourceWork передаёт lease и generated failure suffix owner failure
// переходу и возвращает итоговую source projection.
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
