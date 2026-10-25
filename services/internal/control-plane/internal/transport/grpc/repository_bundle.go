package grpc

import (
	"context"
	"io"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// bundlePin maps captured immutable metadata without exposing the artifact's private object locator or weakening its exact digest.
// bundlePin преобразует captured immutable metadata без private object locator и ослабления exact digest.
func bundlePin(pin *cp.RepositoryBundleArtifactPin) entity.RepositoryBundleArtifactPin {
	return entity.RepositoryBundleArtifactPin{Ref: pin.GetRef(), Revision: pin.GetRevision(), Version: pin.GetVersion(), SHA256: pin.GetSha256(), Size: pin.GetSizeBytes()}
}

// castBundlePin projects the compact source pin; the pin alone never grants permission to read source content.
// castBundlePin проецирует compact source pin; сама pin не разрешает чтение source content.
func castBundlePin(pin entity.RepositoryBundleArtifactPin) *cp.RepositoryBundleArtifactPin {
	return &cp.RepositoryBundleArtifactPin{Ref: pin.Ref, Revision: pin.Revision, Version: pin.Version, Sha256: pin.SHA256, SizeBytes: pin.Size}
}

// castRepositoryBundle maps the complete safe owner projection and closed state, excluding frozen bytes, credentials and internal actor identifiers.
// castRepositoryBundle преобразует полную safe owner projection и closed state, исключая frozen bytes, credentials и internal actor identifiers.
func castRepositoryBundle(p entity.RepositoryBundlePublication) *cp.RepositoryBundlePublication {
	r := &cp.RepositoryBundlePublication{Ref: p.Ref, Version: p.Version, ProjectRef: p.ProjectRef, OriginRunRef: p.OriginRunRef, OriginRuntimeRevisionRef: p.OriginRuntimeRevisionRef,
		InvocationRef: p.InvocationRef, GateRef: p.GateRef, ConnectionRef: p.ConnectionRef, ConnectionVersion: p.ConnectionVersion, GrantRef: p.GrantRef, GrantVersion: p.GrantVersion,
		RepositoryRef: p.RepositoryRef, BaseCommitSha: p.BaseCommitSHA, ProposalBranch: p.ProposalBranch, Archive: castBundlePin(p.Archive), Manifest: castBundlePin(p.Manifest), ApprovalDigest: p.ApprovalDigest,
		FileCount: p.FileCount, TotalBytes: p.TotalBytes, State: cp.RepositoryBundlePublicationState(cp.RepositoryBundlePublicationState_value["REPOSITORY_BUNDLE_PUBLICATION_STATE_"+p.State]),
		CandidateCommitSha: p.CandidateCommitSHA, CandidateTreeSha: p.CandidateTreeSHA, FailureCode: p.FailureCode, CreatedAt: timestamp(p.CreatedAt), ExpiresAt: timestamp(p.ExpiresAt),
		ContinuationState: p.ContinuationState, ContinuationCode: p.ContinuationCode, ContinuationNodeRef: p.ContinuationNodeRef}
	if p.ConfirmedAt != nil {
		r.ConfirmedAt = timestamp(*p.ConfirmedAt)
	}
	return r
}

// bundleLease decodes the complete work tuple while invalid/absent expiry stays zero and is closed by owner validation.
// bundleLease декодирует полный work tuple; неверный либо отсутствующий expiry остаётся zero и закрывается owner validation.
func bundleLease(l *cp.RepositoryBundleLease) entity.RepositoryBundleLease {
	return entity.RepositoryBundleLease{ProposalRef: l.GetProposalRef(), Attempt: l.GetAttempt(), Generation: l.GetGeneration(), Claimant: l.GetClaimant(), Fence: l.GetFence(), ExpiresAt: leaseExpiryOrZero(l.GetExpiresAt())}
}

// castBundleLease preserves exact owner-issued proposal, attempt, generation, claimant, fence and expiry without generating replacement authority.
// castBundleLease сохраняет exact owner-issued proposal, attempt, generation, claimant, fence и expiry без replacement authority.
func castBundleLease(l entity.RepositoryBundleLease) *cp.RepositoryBundleLease {
	return &cp.RepositoryBundleLease{ProposalRef: l.ProposalRef, Attempt: l.Attempt, Generation: l.Generation, Claimant: l.Claimant, Fence: l.Fence, ExpiresAt: timestamp(l.ExpiresAt)}
}

// bundleEffect maps only the closed candidate/source pin set; the request carries no URL, arbitrary Git command or caller-defined result text.
// bundleEffect преобразует лишь closed candidate/source pin set; URL, arbitrary Git command и caller-defined result text в request нет.
func bundleEffect(r *cp.RepositoryBundleEffectRequest) port.RepositoryBundleEffect {
	return port.RepositoryBundleEffect{Lease: bundleLease(r.GetLease()), CandidateCommitSHA: r.GetCandidateCommitSha(), CandidateTreeSHA: r.GetCandidateTreeSha(), BaseCommitSHA: r.GetBaseCommitSha(), ManifestSHA256: r.GetManifestSha256(), ArchiveSHA256: r.GetArchiveSha256()}
}

// PrepareRepositoryBundlePublication delegates compact exact origin/artifact pins to the owner, where full source eligibility and native gate creation share one transaction.
// PrepareRepositoryBundlePublication передаёт compact exact origin/artifact pins owner, где full source eligibility и native Gate creation объединены одной транзакцией.
func (server *Server) PrepareRepositoryBundlePublication(ctx context.Context, r *cp.PrepareRepositoryBundlePublicationRequest) (*cp.RepositoryBundlePublicationResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_PrepareRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareRepositoryBundle(ctx, p, port.RepositoryBundlePrepare{LeaseRef: r.GetLeaseRef(), Fence: r.GetFence(), Generation: r.GetGeneration(), GrantRef: r.GetGrantRef(), BaseCommitSHA: r.GetBaseCommitSha(), IdempotencyKey: r.GetIdempotencyKey(), Archive: bundlePin(r.GetArchive()), Manifest: bundlePin(r.GetManifest())})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RepositoryBundlePublicationResponse{Proposal: castRepositoryBundle(result)}, nil
}

// GetRepositoryBundlePublication returns one bounded version/digest-pinned manifest page and explicit completeness; it never truncates a full listing silently.
// GetRepositoryBundlePublication возвращает bounded version/digest-pinned manifest page с явной полнотой; full listing молча не усекается.
func (server *Server) GetRepositoryBundlePublication(ctx context.Context, r *cp.GetRepositoryBundlePublicationRequest) (*cp.GetRepositoryBundlePublicationResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_GetRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	view, err := server.service.GetRepositoryBundle(ctx, p, port.RepositoryBundleRead{ProposalRef: r.GetProposalRef(), GateRef: r.GetGateRef(), ExpectedVersion: r.GetExpectedVersion(), ExpectedManifestSHA256: r.GetExpectedManifestSha256(), Offset: r.GetOffset(), Limit: r.GetLimit()})
	if err != nil {
		return nil, transportError(err)
	}
	result := &cp.GetRepositoryBundlePublicationResponse{Proposal: castRepositoryBundle(view.Proposal), Offset: view.Offset, NextOffset: view.NextOffset, Complete: view.Complete, Files: []*cp.RepositoryBundleFile{}}
	for _, file := range view.Files {
		result.Files = append(result.Files, &cp.RepositoryBundleFile{Path: file.Path, Mode: file.Mode, Bytes: file.Bytes, Sha256: file.SHA256})
	}
	if proto.Size(result) > 24<<10 {
		return nil, transportError(errs.ErrUnavailable)
	}
	return result, nil
}

// ClaimRepositoryBundlePublications issues only specialized private work and projects the same frozen invocation schema for the existing provider-validation primitive.
// ClaimRepositoryBundlePublications выдаёт лишь specialized private work и проецирует frozen invocation schema для прежнего provider-validation primitive.
func (server *Server) ClaimRepositoryBundlePublications(ctx context.Context, r *cp.ClaimRepositoryBundlePublicationsRequest) (*cp.ClaimRepositoryBundlePublicationsResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_ClaimRepositoryBundlePublications_FullMethodName)
	if err != nil {
		return nil, err
	}
	work, err := server.service.ClaimRepositoryBundles(ctx, p, r.GetClaimant(), r.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	result := &cp.ClaimRepositoryBundlePublicationsResponse{Work: []*cp.RepositoryBundlePublicationWork{}}
	for _, w := range work {
		configuration, err := structpb.NewStruct(w.PublicConfiguration)
		if err != nil {
			return nil, transportError(errs.ErrUnavailable)
		}
		input, err := structpb.NewStruct(repositorybundle.InvocationInput(w.Proposal.Archive, w.Proposal.Manifest, w.Proposal.BaseCommitSHA))
		if err != nil {
			return nil, transportError(errs.ErrUnavailable)
		}
		mode := cp.IntegrationInvocationWorkMode(cp.IntegrationInvocationWorkMode_value["INTEGRATION_INVOCATION_WORK_MODE_"+w.Mode])
		invocation := &cp.IntegrationInvocationClaim{InvocationRef: w.Proposal.InvocationRef, ConnectionRef: w.Proposal.ConnectionRef, GrantRef: w.Proposal.GrantRef, GrantVersion: w.Proposal.GrantVersion,
			DefinitionKey: w.DefinitionKey, DefinitionVersion: w.DefinitionVersion, DefinitionDigest: w.DefinitionDigest, DefinitionPackage: w.DefinitionPackage,
			CapabilityKey: repositorybundle.Operation, Operation: repositorybundle.Operation, Risk: cp.IntegrationRisk_INTEGRATION_RISK_WRITE, ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_EACH_EFFECT,
			PublicConfiguration: configuration, BoundedInput: input, CredentialRevision: castIntegrationCredential(w.Credential), EffectKey: w.EffectKey, InputDigest: w.InputDigest, WorkMode: mode,
			ResourceScope: &cp.IntegrationResourceScope{Kind: integrationResourceKind("GITHUB_REPOSITORY"), Values: w.ResourceScope, Digest: w.ResourceScopeDigest}}
		item := &cp.RepositoryBundlePublicationWork{Lease: castBundleLease(w.Lease), Proposal: castRepositoryBundle(w.Proposal), Invocation: invocation, Mode: mode, Manifest: w.Manifest, CommitTime: timestamp(w.CommitTime), Deadline: timestamp(w.Deadline)}
		if w.EffectStartedAt != nil {
			item.EffectStartedAt = timestamp(*w.EffectStartedAt)
		}
		result.Work = append(result.Work, item)
	}
	return result, nil
}

// RenewRepositoryBundlePublication renews only the same exact owner-issued work tuple and cannot convert read-only recovery into execution.
// RenewRepositoryBundlePublication продлевает лишь прежний exact owner-issued work tuple, не превращая read-only recovery в execution.
func (server *Server) RenewRepositoryBundlePublication(ctx context.Context, r *cp.RepositoryBundleLeaseRequest) (*cp.RepositoryBundleLeaseResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_RenewRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	lease, err := server.service.RenewRepositoryBundle(ctx, p, bundleLease(r.GetLease()))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RepositoryBundleLeaseResponse{Lease: castBundleLease(lease)}, nil
}

// BeginRepositoryBundlePublication preserves the distinction between a new durable intent ACK and an already-started receipt, so replay is never push permission.
// BeginRepositoryBundlePublication различает новый durable intent ACK и already-started receipt, поэтому replay не разрешает push.
func (server *Server) BeginRepositoryBundlePublication(ctx context.Context, r *cp.RepositoryBundleEffectRequest) (*cp.BeginRepositoryBundlePublicationResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_BeginRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	proposal, started, err := server.service.BeginRepositoryBundle(ctx, p, bundleEffect(r))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.BeginRepositoryBundlePublicationResponse{Proposal: castRepositoryBundle(proposal), AlreadyStarted: started}, nil
}

// CompleteRepositoryBundlePublication reports exact provider readback without accepting caller-authored summaries or issuing another external write.
// CompleteRepositoryBundlePublication сообщает exact provider readback без caller-authored summary и нового external write.
func (server *Server) CompleteRepositoryBundlePublication(ctx context.Context, r *cp.RepositoryBundleEffectRequest) (*cp.RepositoryBundlePublicationResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_CompleteRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	proposal, err := server.service.CompleteRepositoryBundle(ctx, p, bundleEffect(r))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RepositoryBundlePublicationResponse{Proposal: castRepositoryBundle(proposal)}, nil
}

// FailRepositoryBundlePublication forwards a closed error class; the owner decides known no-effect terminal versus begun UNKNOWN atomically.
// FailRepositoryBundlePublication передаёт закрытый error class; owner атомарно выбирает terminal без эффекта либо begun UNKNOWN.
func (server *Server) FailRepositoryBundlePublication(ctx context.Context, r *cp.FailRepositoryBundlePublicationRequest) (*cp.RepositoryBundlePublicationResponse, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_FailRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	proposal, err := server.service.FailRepositoryBundle(ctx, p, bundleLease(r.GetLease()), r.GetFailureCode())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RepositoryBundlePublicationResponse{Proposal: castRepositoryBundle(proposal)}, nil
}

// ReadRepositoryBundlePublication transfers at most the fixed 1MiB frozen archive to an exact IG work epoch. Partial reads or digest mismatches fail before any response is returned.
// ReadRepositoryBundlePublication передаёт не более fixed1MiB frozen archive exact IG work epoch. Partial read либо digest mismatch отклоняются до выдачи ответа.
func (server *Server) ReadRepositoryBundlePublication(ctx context.Context, r *cp.RepositoryBundleLeaseRequest) (*cp.RepositoryBundleContent, error) {
	p, err := principal(ctx, cp.RepositoryBundlePublicationService_ReadRepositoryBundlePublication_FullMethodName)
	if err != nil {
		return nil, err
	}
	download, err := server.service.ReadRepositoryBundle(ctx, p, bundleLease(r.GetLease()))
	if err != nil {
		return nil, transportError(err)
	}
	if download.Reader == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	defer func() { _ = download.Reader.Close() }()
	if download.Artifact.SizeBytes < 1 || download.Artifact.SizeBytes > repositorybundle.MaximumArchiveBytes {
		return nil, transportError(errs.ErrUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(download.Reader, download.Artifact.SizeBytes+1))
	if err != nil || int64(len(raw)) != download.Artifact.SizeBytes || "sha256:"+repositorybundle.Digest(raw) != download.Artifact.Digest {
		clear(raw)
		return nil, transportError(errs.ErrUnavailable)
	}
	if err := download.Reader.Close(); err != nil {
		clear(raw)
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.RepositoryBundleContent{Metadata: &cp.RepositoryBundleArtifactPin{Ref: download.Artifact.Ref, Revision: download.Artifact.Revision, Version: download.Artifact.Version, Sha256: repositorybundle.Digest(raw), SizeBytes: int64(len(raw))}, Archive: raw}, nil
}
