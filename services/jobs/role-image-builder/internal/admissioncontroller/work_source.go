package admissioncontroller

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

type WorkSourceConfig struct {
	RPCProfile                                                                 string
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout, RPCDeadline                                                   time.Duration
}

type ControlPlaneWorkSource struct {
	client      *sharedclient.Client
	rpcDeadline time.Duration
}

type LegacyPollingWorkSource struct{}

func (LegacyPollingWorkSource) GetAvailability(context.Context) (WorkAvailability, error) {
	return WorkAvailability{AdmissionAvailable: true, PromotionAvailable: true}, nil
}

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

func (source *ControlPlaneWorkSource) Close() error {
	return source.client.Close()
}

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
