package application

import (
	"context"
	"sync/atomic"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
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
	domain    atomic.Pointer[service.Authority]
	available atomic.Bool
}

func NewAuthority(domain *service.Authority) *Authority {
	application := &Authority{}
	application.domain.Store(domain)
	return application
}

func (application *Authority) Issue(
	ctx context.Context,
	command IssueCommand,
) (IssueResult, error) {
	domain, err := application.current()
	if err != nil {
		return IssueResult{}, err
	}
	compact, claims, err := domain.Issue(
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
	domain, err := application.current()
	if err != nil {
		return model.AuthorizationClaims{}, err
	}
	return domain.Verify(
		ctx,
		command.Compact,
		command.ObservedFullMethod,
		command.DownstreamSPIFFEID,
	)
}

func (application *Authority) ActivateSnapshot(ctx context.Context) error {
	domain := application.domain.Load()
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	if err := domain.ActivateSnapshot(ctx); err != nil {
		return err
	}
	application.available.Store(true)
	return nil
}

func (application *Authority) ReplaceActivatedSnapshot(domain *service.Authority) error {
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	application.domain.Store(domain)
	application.available.Store(true)
	return nil
}

func (application *Authority) SetAvailable(available bool) {
	application.available.Store(available)
}

func (application *Authority) Ready(ctx context.Context) error {
	domain, err := application.current()
	if err != nil {
		return err
	}
	return domain.Ready(ctx)
}

func (application *Authority) ServedStateReady(ctx context.Context) error {
	domain := application.domain.Load()
	if domain == nil {
		return failure.New(failure.SnapshotRejected, "authority snapshot is unavailable")
	}
	return domain.Ready(ctx)
}

func (application *Authority) SnapshotState() model.SnapshotState {
	domain := application.domain.Load()
	if domain == nil {
		return model.SnapshotState{}
	}
	state := domain.SnapshotState()
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

func (application *Authority) current() (*service.Authority, error) {
	if !application.available.Load() {
		return nil, failure.New(
			failure.PersistenceUnavailable,
			"authority snapshot is not ready",
		)
	}
	domain := application.domain.Load()
	if domain == nil {
		return nil, failure.New(
			failure.SnapshotRejected,
			"authority snapshot is unavailable",
		)
	}
	return domain, nil
}
