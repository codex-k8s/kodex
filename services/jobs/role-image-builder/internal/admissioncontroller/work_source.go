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
