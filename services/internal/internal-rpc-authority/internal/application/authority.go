package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

type IssueCommand struct {
	OperationID  string
	ProofCompact string
}

type IssueResult struct {
	Compact string
	Claims  model.AuthorizationClaims
}

type VerifyCommand struct {
	Compact            string
	ObservedFullMethod string
	DownstreamSPIFFEID string
}

type Authority struct {
	domain *service.Authority
}

func NewAuthority(domain *service.Authority) *Authority {
	return &Authority{domain: domain}
}

func (application *Authority) Issue(
	ctx context.Context,
	command IssueCommand,
) (IssueResult, error) {
	compact, claims, err := application.domain.Issue(
		ctx,
		command.OperationID,
		command.ProofCompact,
	)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Compact: compact, Claims: claims}, nil
}

func (application *Authority) Verify(
	ctx context.Context,
	command VerifyCommand,
) (model.AuthorizationClaims, error) {
	return application.domain.Verify(
		ctx,
		command.Compact,
		command.ObservedFullMethod,
		command.DownstreamSPIFFEID,
	)
}

func (application *Authority) ActivateSnapshot(ctx context.Context) error {
	return application.domain.ActivateSnapshot(ctx)
}

func (application *Authority) Ready(ctx context.Context) error {
	return application.domain.Ready(ctx)
}

func (application *Authority) SnapshotState() model.SnapshotState {
	state := application.domain.SnapshotState()
	return model.SnapshotState{
		SourceRevision:          state.SourceRevision,
		SourceDigestSHA256:      state.SourceDigestSHA256,
		PredecessorRevision:     state.PredecessorRevision,
		PredecessorDigestSHA256: state.PredecessorDigestSHA256,
		KeySetRevision:          state.KeySetRevision,
		PolicyRevision:          state.PolicyRevision,
		SignerGeneration:        state.SignerGeneration,
	}
}
