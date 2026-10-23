package application

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// ReadbackAttestor forwards typed independent readback use cases to the domain, leaving credential
// verification and durable receipt ownership there.
// ReadbackAttestor передаёт типизированные сценарии независимой проверки доставки домену, оставляя ему
// проверку учётных данных и владение устойчивыми квитанциями.
type ReadbackAttestor struct {
	service *service.ReadbackAttestor
}

// NewReadbackAttestor wraps the supplied domain service without validation, trust activation or network
// activity; initialization remains composition-owned.
// NewReadbackAttestor оборачивает переданный доменный сервис без проверки, активации доверия или сети;
// инициализация принадлежит композиции.
func NewReadbackAttestor(serviceValue *service.ReadbackAttestor) *ReadbackAttestor {
	return &ReadbackAttestor{service: serviceValue}
}

// IssueChallenge forwards the observed peer, intent, credential and idempotency binding to domain
// challenge issuance; peer text alone is not newly authenticated here.
// IssueChallenge передаёт наблюдаемую сторону, намерение, учётные данные и привязку идемпотентности
// доменному выпуску запроса проверки; строка стороны сама по себе здесь не удостоверяется.
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

// Attest delegates exact peer/intent/challenge/credential/evidence verification and receipt persistence,
// returning domain errors without manufacturing acceptance.
// Attest передаёт точную проверку стороны, намерения, запроса проверки, учётных данных и свидетельства и
// сохранение квитанции, возвращая доменные ошибки без фиктивного принятия.
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

// Ready delegates the independent attestor's complete domain readiness path rather than testing only
// whether the wrapper exists.
// Ready передаёт полную доменную проверку готовности независимой аттестации вместо проверки
// существования обёртки.
func (application *ReadbackAttestor) Ready(ctx context.Context) error {
	return application.service.Ready(ctx)
}

// ActivateTrust passes verified trust records and pinned served-state metadata to domain durable
// activation; this wrapper neither clones the map nor verifies its signatures.
// ActivateTrust передаёт проверенные записи доверия и закреплённые метаданные обслуживаемого состояния
// устойчивой доменной активации; обёртка не копирует карту и не проверяет подписи.
func (application *ReadbackAttestor) ActivateTrust(
	ctx context.Context,
	trust map[string]service.VerificationKeyRecord,
	state model.ReadbackTrustState,
) error {
	return application.service.ActivateTrust(ctx, trust, state)
}

// TrustState returns the domain's currently served readback trust metadata without computing a new
// watermark or performing independent persistence reads.
// TrustState возвращает текущие обслуживаемые метаданные доверия домена без вычисления новой границы
// версий или самостоятельного чтения хранилища.
func (application *ReadbackAttestor) TrustState() model.ReadbackTrustState {
	return application.service.TrustState()
}
