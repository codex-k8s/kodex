package admissioncontroller

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

// WorkSourceConfig supplies the shared transport/profile/identity paths and the
// controller's per-read deadline. It carries configuration references, not loaded
// private credential bytes or an admission/promotion claim.
// WorkSourceConfig задаёт transport/profile/identity пути общего клиента и срок
// чтения контроллера. Значение содержит ссылки конфигурации, не загруженные
// закрытые реквизиты и не admission/promotion claim.
type WorkSourceConfig struct {
	RPCProfile                                                                 string
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout, RPCDeadline                                                   time.Duration
}

// ControlPlaneWorkSource owns the registered controller client for availability
// and recovery-terminal reads, keeping those reads separate from phase Job commands.
// ControlPlaneWorkSource владеет зарегистрированным клиентом чтения availability
// и recovery terminal, отдельно от команд phase Jobs.
type ControlPlaneWorkSource struct {
	client      *sharedclient.Client
	rpcDeadline time.Duration
}

// LegacyPollingWorkSource preserves the existing unconditional polling trigger;
// it does not read authoritative availability or provide recovery-terminal proof.
// Phase Jobs still need their independent owner claim/authorization path.
// LegacyPollingWorkSource сохраняет существующий безусловный polling trigger;
// он не читает authoritative availability и не выдаёт recovery-terminal proof.
// Phase Jobs по-прежнему требуют отдельного owner claim/authorization.
type LegacyPollingWorkSource struct{}

// GetAvailability advertises both polling phases without I/O or actual work lookup;
// true here is a scheduling hint, not an owner-issued lease or image admission.
// GetAvailability объявляет обе polling-фазы без I/O и поиска настоящей работы;
// true здесь — повод планирования, не lease владельца и не допуск образа.
func (LegacyPollingWorkSource) GetAvailability(context.Context) (WorkAvailability, error) {
	return WorkAvailability{AdmissionAvailable: true, PromotionAvailable: true}, nil
}

// DialWorkSource selects the fixed controller operation registration and service
// identity path through the shared client. After construction it requires a positive
// read deadline and closes the acquired client if that check fails; it claims no work.
// DialWorkSource выбирает фиксированную регистрацию операций контроллера и service
// identity через общий клиент. После создания требуется положительный срок чтения;
// при его ошибке полученный клиент закрывается. Работа здесь не забирается.
func DialWorkSource(ctx context.Context, config WorkSourceConfig) (*ControlPlaneWorkSource, error) {
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		ServiceIdentity: true, RPCProfile: config.RPCProfile, CallerWorkload: "image-admission-controller",
		Target: config.Target, TLSServerName: config.TLSServerName, CAFile: config.CAFile,
		ClientCertificateFile: config.ClientCertificateFile, ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: config.ExpectedIssuerUID,
		ExpectedIssuerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout,
		Operations: sharedclient.ImageAdmissionControllerOperations(),
	})
	if err != nil {
		return nil, err
	}
	if config.RPCDeadline <= 0 {
		_ = client.Close()
		return nil, errors.New("image supply work source deadline is invalid")
	}
	return &ControlPlaneWorkSource{client: client, rpcDeadline: config.RPCDeadline}, nil
}

// GetAvailability reads the owner's two availability booleans within the configured
// deadline, returning an empty result with RPC errors instead of inventing work.
// A successful availability hint still does not reserve an admission or promotion.
// GetAvailability читает два owner availability-флага с заданным сроком, возвращая
// пустой результат при RPC-ошибке, а не вымышленную работу. Успешная подсказка
// доступности также не резервирует admission либо promotion.
func (source *ControlPlaneWorkSource) GetAvailability(ctx context.Context) (WorkAvailability, error) {
	callCtx, cancel := context.WithTimeout(ctx, source.rpcDeadline)
	defer cancel()
	response, err := source.client.RoleImages.GetImageSupplyWorkAvailability(callCtx,
		&controlplanev1.GetImageSupplyWorkAvailabilityRequest{})
	if err != nil {
		return WorkAvailability{}, err
	}
	return WorkAvailability{
		AdmissionAvailable: response.GetAdmissionAvailable(),
		PromotionAvailable: response.GetPromotionAvailable(),
	}, nil
}

// Close releases the owned shared connection; the caller remains responsible for
// stopping concurrent controller reads before closing the source.
// Close освобождает соединение общего клиента; вызывающий код должен предварительно
// остановить конкурентные чтения контроллера.
func (source *ControlPlaneWorkSource) Close() error {
	return source.client.Close()
}

// GetRecoveryTerminal reads the owner proof for the requested run, rejects absent
// artifacts/unknown terminal states and projects the version/fence/attempt fields
// required by local recovery validation. This subset is not the full owner claim,
// tenant/policy/risk proof and does not itself authorize Kubernetes deletion.
// GetRecoveryTerminal читает owner proof запрошенного run, отклоняет отсутствие
// артефакта/неизвестное terminal-состояние и переносит version/fence/attempt
// для локальной проверки recovery. Эта проекция не является полным owner claim,
// tenant/policy/risk proof и сама не разрешает удаление Kubernetes-ресурсов.
func (source *ControlPlaneWorkSource) GetRecoveryTerminal(ctx context.Context, runID string) (RecoveryTerminalProof, error) {
	callCtx, cancel := context.WithTimeout(ctx, source.rpcDeadline)
	defer cancel()
	result, err := source.client.RoleImages.GetImageAdmissionRecoveryTerminal(callCtx, &controlplanev1.GetImageAdmissionRecoveryTerminalRequest{AdmissionRunId: runID})
	if err != nil {
		return RecoveryTerminalProof{}, err
	}
	response := result.GetTerminalProof()
	a := response.GetClaimedArtifact()
	if a == nil {
		return RecoveryTerminalProof{}, errors.New("image admission recovery terminal artifact is absent")
	}
	var state string
	switch response.GetTerminalState() {
	case controlplanev1.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_ACCEPTED:
		state = "ACCEPTED"
	case controlplanev1.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_REJECTED:
		state = "REJECTED"
	case controlplanev1.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_FAILED:
		state = "FAILED"
	case controlplanev1.ImageAdmissionTerminalState_IMAGE_ADMISSION_TERMINAL_STATE_CANCELLED:
		state = "CANCELLED"
	default:
		return RecoveryTerminalProof{}, errors.New("image admission recovery terminal state is invalid")
	}
	proof := RecoveryTerminalProof{RunID: runID, State: state, ArtifactRef: a.GetRef(), BuildRef: a.GetBuildRef(), AttemptRef: response.GetAdmissionAttemptRef(), Attempt: response.GetAdmissionAttempt(), ClaimVersion: a.GetVersion(), ClaimFence: response.GetClaimFence(), ClaimGeneration: response.GetClaimAuthorityGeneration(), TerminalVersion: response.GetTerminalArtifactVersion(), TerminalFence: response.GetTerminalFence(), TerminalAttemptVersion: response.GetTerminalAttemptVersion()}
	if !validRecoveryTerminal(proof, runID) {
		return RecoveryTerminalProof{}, errors.New("image admission recovery terminal proof is incomplete")
	}
	return proof, nil
}
