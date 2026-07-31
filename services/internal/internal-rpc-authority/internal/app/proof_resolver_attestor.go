package app

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/snapshot"
)

const proofResolverAudience = "urn:mattercodex:internal-rpc-authority-issuer:control-api-gateway"

type proofResolverAttestor struct {
	primary          repository.SnapshotAttestor
	resolver         repository.SnapshotAttestor
	privateJWKFile   string
	proofTrustFile   string
	issuer           string
	signerGeneration uint64
	now              func() time.Time
}

func (attestor *proofResolverAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (string, error) {
	primaryReceipt, err := attestor.primary.Attest(ctx, state)
	if err != nil {
		return "", err
	}
	if err := snapshot.VerifyProofSigner(
		attestor.privateJWKFile,
		attestor.proofTrustFile,
		attestor.issuer,
		proofResolverAudience,
		state.SourceRevision,
		state.SourceDigestSHA256,
		attestor.signerGeneration,
		attestor.now().UTC(),
	); err != nil {
		return "", errors.New("served authority proof resolver key rejected")
	}
	if _, err := attestor.resolver.Attest(ctx, state); err != nil {
		return "", errors.New("obtain authority proof resolver readback receipt")
	}
	return primaryReceipt, nil
}

var _ repository.SnapshotAttestor = (*proofResolverAttestor)(nil)
