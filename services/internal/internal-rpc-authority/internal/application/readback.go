package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

type ReadbackAttestor struct {
	service *service.ReadbackAttestor
}

func NewReadbackAttestor(serviceValue *service.ReadbackAttestor) *ReadbackAttestor {
	return &ReadbackAttestor{service: serviceValue}
}

func (application *ReadbackAttestor) IssueChallenge(
	ctx context.Context,
	peerSPIFFEID string,
	intentID string,
	credentialCompact string,
	idempotencyKey string,
) (service.ReadbackChallengeResult, error) {
	return application.service.IssueChallenge(
		ctx,
		peerSPIFFEID,
		intentID,
		credentialCompact,
		idempotencyKey,
	)
}

func (application *ReadbackAttestor) Attest(
	ctx context.Context,
	peerSPIFFEID string,
	intentID string,
	challengeID string,
	credentialCompact string,
	evidenceCompact string,
	idempotencyKey string,
) (service.ReadbackAttestationResult, error) {
	return application.service.Attest(
		ctx,
		peerSPIFFEID,
		intentID,
		challengeID,
		credentialCompact,
		evidenceCompact,
		idempotencyKey,
	)
}

func (application *ReadbackAttestor) Ready(ctx context.Context) error {
	return application.service.Ready(ctx)
}
