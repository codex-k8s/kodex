package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/structpb"
)

// castWriteBack maps proposal/source/connection/content/approval pins, state, optional
// effect-confirmation times and action availability without executing Git effects or validating
// owner transitions.
// castWriteBack преобразует proposal, source, connection, content и approval pins, state,
// необязательные effect-confirmation times и action availability без Git effects либо проверки
// owner transitions.
func castWriteBack(p entity.ConfigurationWriteBack) *cp.ManagedConfigurationGitWriteBack {
	r := &cp.ManagedConfigurationGitWriteBack{Ref: p.Ref, Version: p.Version, ConfigurationRef: p.ConfigurationRef,
		Kind:                 cp.ManagedConfigurationKind(cp.ManagedConfigurationKind_value["MANAGED_CONFIGURATION_KIND_"+p.Kind]),
		ConfigurationVersion: p.ConfigurationVersion, SourceRef: p.SourceRef, SourceVersion: p.SourceVersion, ConnectionRef: p.ConnectionRef, ConnectionVersion: p.ConnectionVersion,
		RepositoryRef: p.RepositoryRef, SourceRefName: p.SourceRefName, Path: p.Path, BaseCommitSha: p.BaseCommitSHA, BaseContentSha256: p.BaseContentSHA256,
		ProposedContentSha256: p.ProposedContentSHA256, ContentFormat: p.ContentFormat, ProposalBranch: p.ProposalBranch, ApprovalDigest: p.ApprovalDigest,
		State:              cp.ManagedConfigurationGitWriteBackState(cp.ManagedConfigurationGitWriteBackState_value["MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_"+p.State]),
		FailureCode:        cp.ManagedConfigurationGitWriteBackFailure(cp.ManagedConfigurationGitWriteBackFailure_value["MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_"+p.FailureCode]),
		CandidateCommitSha: p.CandidateCommitSHA, PullRequestRef: p.PullRequestRef, PullRequestUrl: p.PullRequestURL, CreatedAt: timestamp(p.CreatedAt), ExpiresAt: timestamp(p.ExpiresAt)}
	if p.ApprovedAt != nil {
		r.ApprovedAt = timestamp(*p.ApprovedAt)
	}
	if p.CompletedAt != nil {
		r.CompletedAt = timestamp(*p.CompletedAt)
	}
	if p.BranchConfirmedAt != nil {
		r.BranchConfirmedAt = timestamp(*p.BranchConfirmedAt)
	}
	if p.PullRequestConfirmedAt != nil {
		r.PullRequestConfirmedAt = timestamp(*p.PullRequestConfirmedAt)
	}
	for _, a := range p.NextActions {
		r.NextActions = append(r.NextActions, &cp.ManagedConfigurationGitWriteBackActionAvailability{
			Action: cp.ManagedConfigurationGitWriteBackAction(cp.ManagedConfigurationGitWriteBackAction_value["MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_"+a.Action]), Enabled: a.Enabled,
			Reason: cp.ManagedConfigurationGitWriteBackActionReason(cp.ManagedConfigurationGitWriteBackActionReason_value["MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_REASON_"+a.Reason])})
	}
	return r
}

// writeBackMutation dispatches a specialized write-back command and rejects an absent proposal
// result as unavailable before serializing the owner projection.
// writeBackMutation вызывает специализированную write-back command и отклоняет отсутствующий
// proposal result как unavailable до сериализации owner projection.
func (server *Server) writeBackMutation(ctx context.Context, method string, kind command.Kind, mutation *cp.MutationContext, input command.ConfigurationWriteBackInput) (*cp.ManagedConfigurationGitWriteBack, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, input)
	if err != nil {
		return nil, err
	}
	if result.ConfigurationWriteBack == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return castWriteBack(*result.ConfigurationWriteBack), nil
}

// PrepareRoleImageGitWriteBack passes role-image configuration, expected source version and
// proposed content to the owner proposal command without writing a repository.
// PrepareRoleImageGitWriteBack передаёт role-image configuration, expected source version и
// proposed content owner proposal command без записи в repository.
func (server *Server) PrepareRoleImageGitWriteBack(ctx context.Context, r *cp.PrepareRoleImageGitWriteBackRequest) (*cp.PrepareRoleImageGitWriteBackResponse, error) {
	p, err := server.writeBackMutation(ctx, cp.PlatformCommandService_PrepareRoleImageGitWriteBack_FullMethodName, command.PrepareRoleImageGitWriteBack, r.GetMutation(), command.ConfigurationWriteBackInput{ConfigurationRef: r.GetConfigurationRef(), ExpectedSourceVersion: r.GetExpectedSourceVersion(), Content: r.GetContent()})
	return &cp.PrepareRoleImageGitWriteBackResponse{Proposal: p}, err
}

// PrepareIntegrationDefinitionGitWriteBack creates the owner write-back proposal for an
// integration definition using exact configuration/source version pins and content.
// PrepareIntegrationDefinitionGitWriteBack создаёт owner write-back proposal integration
// definition по точным configuration и source version pins и content.
func (server *Server) PrepareIntegrationDefinitionGitWriteBack(ctx context.Context, r *cp.PrepareIntegrationDefinitionGitWriteBackRequest) (*cp.PrepareIntegrationDefinitionGitWriteBackResponse, error) {
	p, err := server.writeBackMutation(ctx, cp.PlatformCommandService_PrepareIntegrationDefinitionGitWriteBack_FullMethodName, command.PrepareIntegrationDefinitionGitWriteBack, r.GetMutation(), command.ConfigurationWriteBackInput{ConfigurationRef: r.GetConfigurationRef(), ExpectedSourceVersion: r.GetExpectedSourceVersion(), Content: r.GetContent()})
	return &cp.PrepareIntegrationDefinitionGitWriteBackResponse{Proposal: p}, err
}

// ApproveManagedConfigurationGitWriteBack passes proposal locator and approval digest through the
// dedicated approve command with mutation/version and exact-method principal.
// ApproveManagedConfigurationGitWriteBack передаёт proposal locator и approval digest отдельной
// approve command с mutation, version и principal точного метода.
func (server *Server) ApproveManagedConfigurationGitWriteBack(ctx context.Context, r *cp.ApproveManagedConfigurationGitWriteBackRequest) (*cp.ApproveManagedConfigurationGitWriteBackResponse, error) {
	p, err := server.writeBackMutation(ctx, cp.PlatformCommandService_ApproveManagedConfigurationGitWriteBack_FullMethodName, command.ApproveManagedConfigurationGitWriteBack, r.GetMutation(), command.ConfigurationWriteBackInput{ProposalRef: r.GetProposalRef(), ApprovalDigest: r.GetApprovalDigest()})
	return &cp.ApproveManagedConfigurationGitWriteBackResponse{Proposal: p}, err
}

// RejectManagedConfigurationGitWriteBack passes proposal and approval pins through the owner
// rejection command without deleting or rewriting an external branch.
// RejectManagedConfigurationGitWriteBack передаёт proposal и approval pins owner rejection command
// без удаления либо перезаписи внешней branch.
func (server *Server) RejectManagedConfigurationGitWriteBack(ctx context.Context, r *cp.RejectManagedConfigurationGitWriteBackRequest) (*cp.RejectManagedConfigurationGitWriteBackResponse, error) {
	p, err := server.writeBackMutation(ctx, cp.PlatformCommandService_RejectManagedConfigurationGitWriteBack_FullMethodName, command.RejectManagedConfigurationGitWriteBack, r.GetMutation(), command.ConfigurationWriteBackInput{ProposalRef: r.GetProposalRef(), ApprovalDigest: r.GetApprovalDigest()})
	return &cp.RejectManagedConfigurationGitWriteBackResponse{Proposal: p}, err
}

// CancelManagedConfigurationGitWriteBack dispatches the owner cancellation transition for the
// proposal, leaving effect/lease closure and recovery semantics with the owner.
// CancelManagedConfigurationGitWriteBack вызывает owner cancellation переход proposal, оставляя
// закрытие effects и leases и recovery semantics владельцу.
func (server *Server) CancelManagedConfigurationGitWriteBack(ctx context.Context, r *cp.CancelManagedConfigurationGitWriteBackRequest) (*cp.CancelManagedConfigurationGitWriteBackResponse, error) {
	p, err := server.writeBackMutation(ctx, cp.PlatformCommandService_CancelManagedConfigurationGitWriteBack_FullMethodName, command.CancelManagedConfigurationGitWriteBack, r.GetMutation(), command.ConfigurationWriteBackInput{ProposalRef: r.GetProposalRef()})
	return &cp.CancelManagedConfigurationGitWriteBackResponse{Proposal: p}, err
}

// GetManagedConfigurationGitWriteBack resolves the exact query principal before reading
// proposal/base/proposed content through the domain owner.
// GetManagedConfigurationGitWriteBack разрешает principal точного query до чтения proposal, base и
// proposed content через доменного владельца.
func (server *Server) GetManagedConfigurationGitWriteBack(ctx context.Context, r *cp.GetManagedConfigurationGitWriteBackRequest) (*cp.GetManagedConfigurationGitWriteBackResponse, error) {
	return queryResponse(ctx, cp.PlatformQueryService_GetManagedConfigurationGitWriteBack_FullMethodName, func(p value.Principal) (*cp.GetManagedConfigurationGitWriteBackResponse, error) {
		view, err := server.service.GetConfigurationWriteBack(ctx, p, r.GetProposalRef())
		if err != nil {
			return nil, err
		}
		return &cp.GetManagedConfigurationGitWriteBackResponse{Proposal: castWriteBack(view.Proposal), BaseContent: view.BaseContent, ProposedContent: view.ProposedContent}, nil

	})
}

// ListManagedConfigurationGitWriteBacks reads an owner-authorized proposal page for the
// configuration, preserving total/cursor and a nonnil empty response slice.
// ListManagedConfigurationGitWriteBacks читает разрешённую владельцем страницу proposals
// configuration, сохраняя total, cursor и nonnil пустой response slice.
func (server *Server) ListManagedConfigurationGitWriteBacks(ctx context.Context, r *cp.ListManagedConfigurationGitWriteBacksRequest) (*cp.ListManagedConfigurationGitWriteBacksResponse, error) {
	return queryResponse(ctx, cp.PlatformQueryService_ListManagedConfigurationGitWriteBacks_FullMethodName, func(p value.Principal) (*cp.ListManagedConfigurationGitWriteBacksResponse, error) {
		items, next, total, err := server.service.ListConfigurationWriteBacks(ctx, p, r.GetConfigurationRef(), query.Filter{Page: page(r.GetPage())})
		if err != nil {
			return nil, err
		}
		result := &cp.ListManagedConfigurationGitWriteBacksResponse{Proposals: []*cp.ManagedConfigurationGitWriteBack{}, Page: &cp.PageInfo{NextPageToken: next}, Total: total}
		for _, item := range items {
			result.Proposals = append(result.Proposals, castWriteBack(item))
		}
		return result, nil

	})
}

// castWriteBackLease copies the owner proposal/attempt/claimant/generation/fence/expiry tuple to
// protobuf without creating authority.
// castWriteBackLease копирует owner proposal, attempt, claimant, generation, fence и expiry tuple
// в protobuf без создания полномочий.
func castWriteBackLease(l entity.ConfigurationWriteBackLease) *cp.ManagedConfigurationGitWriteBackLease {
	return &cp.ManagedConfigurationGitWriteBackLease{ProposalRef: l.ProposalRef, Attempt: l.Attempt, ClaimGeneration: l.ClaimGeneration, Claimant: l.Claimant, Fence: l.Fence, ExpiresAt: timestamp(l.ExpiresAt)}
}

// writeBackLease copies request lease pins and converts only valid protobuf expiry, leaving
// absent/invalid expiry as zero for the owner lease checks.
// writeBackLease копирует request lease pins и преобразует только корректный protobuf expiry,
// оставляя отсутствующий либо неверный expiry нулевым для owner lease checks.
func writeBackLease(l *cp.ManagedConfigurationGitWriteBackLease) entity.ConfigurationWriteBackLease {
	return entity.ConfigurationWriteBackLease{ProposalRef: l.GetProposalRef(), Attempt: l.GetAttempt(), ClaimGeneration: l.GetClaimGeneration(), Claimant: l.GetClaimant(), Fence: l.GetFence(), ExpiresAt: leaseExpiryOrZero(l.GetExpiresAt())}
}

// ClaimManagedConfigurationGitWriteBackWork claims through the exact worker principal and
// serializes full owner effect/package/content pins with public configuration; failed Struct
// conversion discards the response.
// ClaimManagedConfigurationGitWriteBackWork получает claim через principal точного worker и
// сериализует полные owner effect, package и content pins с public configuration; отказ Struct
// conversion отбрасывает ответ.
func (server *Server) ClaimManagedConfigurationGitWriteBackWork(ctx context.Context, r *cp.ClaimManagedConfigurationGitWriteBackWorkRequest) (*cp.ClaimManagedConfigurationGitWriteBackWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationGitWriteBackWorkService_ClaimManagedConfigurationGitWriteBackWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	work, err := server.service.ClaimConfigurationWriteBackWork(ctx, p, r.GetClaimant(), r.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	result := &cp.ClaimManagedConfigurationGitWriteBackWorkResponse{Work: []*cp.ManagedConfigurationGitWriteBackWork{}}
	for _, w := range work {
		configuration, err := structpb.NewStruct(w.PublicConfiguration)
		if err != nil {
			return nil, transportError(err)
		}
		item := &cp.ManagedConfigurationGitWriteBackWork{Lease: castWriteBackLease(w.Lease), Proposal: castWriteBack(w.Proposal),
			Mode:          cp.ManagedConfigurationGitWriteBackWorkMode(cp.ManagedConfigurationGitWriteBackWorkMode_value["MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_"+w.Mode]),
			Effect:        cp.ManagedConfigurationGitWriteBackEffect(cp.ManagedConfigurationGitWriteBackEffect_value["MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_"+w.Effect]),
			DefinitionKey: w.DefinitionKey, DefinitionVersion: w.DefinitionVersion, DefinitionDigest: w.DefinitionDigest, DefinitionPackage: w.DefinitionPackage,
			PublicConfiguration: configuration, CredentialRevision: castIntegrationCredential(w.CredentialRevision), ProposedContent: w.ProposedContent, EffectMarker: w.EffectMarker,
			CommitMessage: w.CommitMessage, CommitAuthorName: w.CommitAuthorName, CommitAuthorEmail: w.CommitAuthorEmail, CommitTime: timestamp(w.CommitTime),
			CandidateTreeSha: w.CandidateTreeSHA, CandidateBlobSha: w.CandidateBlobSHA, Deadline: timestamp(w.Deadline)}
		if w.EffectStartedAt != nil {
			item.EffectStartedAt = timestamp(*w.EffectStartedAt)
		}
		result.Work = append(result.Work, item)
	}
	return result, nil
}

// RenewManagedConfigurationGitWriteBackWork requests owner renewal using the supplied exact lease
// tuple under the verified worker method principal.
// RenewManagedConfigurationGitWriteBackWork запрашивает owner renewal по переданному точному lease
// tuple под проверенным principal worker метода.
func (server *Server) RenewManagedConfigurationGitWriteBackWork(ctx context.Context, r *cp.RenewManagedConfigurationGitWriteBackWorkRequest) (*cp.RenewManagedConfigurationGitWriteBackWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationGitWriteBackWorkService_RenewManagedConfigurationGitWriteBackWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	l, err := server.service.RenewConfigurationWriteBackWork(ctx, p, writeBackLease(r.GetLease()))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RenewManagedConfigurationGitWriteBackWorkResponse{Lease: castWriteBackLease(l)}, nil
}

// BeginManagedConfigurationGitWriteBackEffect passes lease/effect and immutable
// commit/tree/blob/parent/content pins to the owner begin boundary and preserves the
// already-started result without issuing the external write.
// BeginManagedConfigurationGitWriteBackEffect передаёт lease, effect и immutable commit, tree,
// blob, parent и content pins owner begin boundary и сохраняет already-started result без внешней
// записи.
func (server *Server) BeginManagedConfigurationGitWriteBackEffect(ctx context.Context, r *cp.BeginManagedConfigurationGitWriteBackEffectRequest) (*cp.BeginManagedConfigurationGitWriteBackEffectResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationGitWriteBackWorkService_BeginManagedConfigurationGitWriteBackEffect_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, started, err := server.service.BeginConfigurationWriteBackEffect(ctx, p, port.ConfigurationWriteBackEffectInput{Lease: writeBackLease(r.GetLease()), Effect: strings.TrimPrefix(r.GetEffect().String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_"), CandidateCommitSHA: r.GetCandidateCommitSha(), CandidateTreeSHA: r.GetCandidateTreeSha(), CandidateBlobSHA: r.GetCandidateBlobSha(), ParentCommitSHA: r.GetParentCommitSha(), ContentSHA256: r.GetContentSha256(), BaseBlobSHA: r.GetBaseBlobSha()})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.BeginManagedConfigurationGitWriteBackEffectResponse{Proposal: castWriteBack(result), AlreadyStarted: started}, nil
}

// CompleteManagedConfigurationGitWriteBackEffect reports exact effect/commit/content and
// pull-request locators to owner completion rather than proving an external effect from a URL
// alone.
// CompleteManagedConfigurationGitWriteBackEffect передаёт точные effect, commit, content и
// pull-request locators owner completion без доказательства внешнего effect одним URL.
func (server *Server) CompleteManagedConfigurationGitWriteBackEffect(ctx context.Context, r *cp.CompleteManagedConfigurationGitWriteBackEffectRequest) (*cp.CompleteManagedConfigurationGitWriteBackEffectResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationGitWriteBackWorkService_CompleteManagedConfigurationGitWriteBackEffect_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.CompleteConfigurationWriteBackEffect(ctx, p, port.ConfigurationWriteBackEffectInput{Lease: writeBackLease(r.GetLease()), Effect: strings.TrimPrefix(r.GetEffect().String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_"), CandidateCommitSHA: r.GetCandidateCommitSha(), ContentSHA256: r.GetContentSha256(), PullRequestRef: r.GetPullRequestRef(), PullRequestURL: r.GetPullRequestUrl()})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.CompleteManagedConfigurationGitWriteBackEffectResponse{Proposal: castWriteBack(result)}, nil
}

// FailManagedConfigurationGitWriteBackWork passes lease and failure suffix to the specialized
// owner work-failure transition with safe transport error mapping.
// FailManagedConfigurationGitWriteBackWork передаёт lease и failure suffix специализированному
// owner work-failure переходу с безопасным transport error mapping.
func (server *Server) FailManagedConfigurationGitWriteBackWork(ctx context.Context, r *cp.FailManagedConfigurationGitWriteBackWorkRequest) (*cp.FailManagedConfigurationGitWriteBackWorkResponse, error) {
	p, err := principal(ctx, cp.ManagedConfigurationGitWriteBackWorkService_FailManagedConfigurationGitWriteBackWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FailConfigurationWriteBackWork(ctx, p, writeBackLease(r.GetLease()), strings.TrimPrefix(r.GetFailureCode().String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_"))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.FailManagedConfigurationGitWriteBackWorkResponse{Proposal: castWriteBack(result)}, nil
}
