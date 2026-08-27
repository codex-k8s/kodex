package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
)

const proofResolverAudience = "urn:kodex:internal-rpc-authority-issuer:control-api-gateway"

type proofResolverAttestor struct {
	primary              repository.SnapshotAttestor
	resolver             repository.SnapshotAttestor
	privateJWKFile       string
	signerGenerationFile string
	proofTrustFile       string
	issuer               string
	now                  func() time.Time
}

func (attestor *proofResolverAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	primaryReceipt, err := attestor.primary.Attest(ctx, state)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	signerGeneration, err := readProofSignerGeneration(attestor.signerGenerationFile)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	if err := snapshot.VerifyProofSigner(
		attestor.privateJWKFile,
		attestor.proofTrustFile,
		attestor.issuer,
		proofResolverAudience,
		state.SourceRevision,
		state.SourceDigestSHA256,
		signerGeneration,
		attestor.now().UTC(),
	); err != nil {
		return repository.SnapshotAttestationReceipt{}, errors.New("served authority proof resolver key rejected")
	}
	if _, err := attestor.resolver.Attest(ctx, state); err != nil {
		return repository.SnapshotAttestationReceipt{}, errors.New("obtain authority proof resolver readback receipt")
	}
	return primaryReceipt, nil
}

func readProofSignerGeneration(path string) (uint64, error) {
	raw, err := securefile.Read(path, 32)
	if err != nil {
		return 0, errors.New("read authority proof signer generation")
	}
	generation, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || generation == 0 || generation > 9_007_199_254_740_991 {
		return 0, errors.New("authority proof signer generation rejected")
	}
	return generation, nil
}

var _ repository.SnapshotAttestor = (*proofResolverAttestor)(nil)
