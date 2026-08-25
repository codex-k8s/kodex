package application

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
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

// ActivateTrust сохраняет и атомарно переключает обслуживаемое доверие.
func (application *ReadbackAttestor) ActivateTrust(
	ctx context.Context,
	trust map[string]service.VerificationKeyRecord,
	state model.ReadbackTrustState,
) error {
	return application.service.ActivateTrust(ctx, trust, state)
}

// TrustState возвращает фактически обслуживаемый durable watermark.
func (application *ReadbackAttestor) TrustState() model.ReadbackTrustState {
	return application.service.TrustState()
}
