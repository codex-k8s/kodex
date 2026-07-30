package application

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

// ReadbackAttestor предоставляет варианты использования независимой проверки.
type ReadbackAttestor struct {
	service *service.ReadbackAttestor
}

// NewReadbackAttestor создаёт прикладную границу attestor.
func NewReadbackAttestor(serviceValue *service.ReadbackAttestor) *ReadbackAttestor {
	return &ReadbackAttestor{service: serviceValue}
}

// IssueChallenge выпускает устойчивый одноразовый challenge.
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

// Attest проверяет evidence и сохраняет неизменяемый receipt.
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

// Ready проверяет полный путь attestor.
func (application *ReadbackAttestor) Ready(ctx context.Context) error {
	return application.service.Ready(ctx)
}
