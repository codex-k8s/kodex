package secretbroker

import (
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func dialTrustedCluster(config Config) (*Client, error) {
	if config.RPCProfile != "trusted-cluster" ||
		strings.TrimPrefix(config.Target, "dns:///") != "secret-broker.kodex-system.svc:8443" ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second {
		return nil, errors.New("trusted secret broker configuration rejected")
	}
	operations := controlplaneclient.SecretDraftGatewayOperations()
	for operation, method := range map[string]string{
		"secrets.create":    sb.SecretBrokerService_CreateSecret_FullMethodName,
		"secrets.rotate":    sb.SecretBrokerService_RotateSecret_FullMethodName,
		"secrets.reveal":    sb.SecretBrokerService_RevealSecret_FullMethodName,
		"secrets.revoke":    sb.SecretBrokerService_RevokeSecret_FullMethodName,
		"secrets.readiness": sb.SecretBrokerService_CheckReadiness_FullMethodName,
	} {
		operations[operation] = method
	}
	interceptor, err := controlplaneclient.TrustedUnaryClientInterceptor(config.RPCProfile, "control-api-gateway", operations)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(config.Target, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptor),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)))
	if err != nil {
		return nil, errors.New("create trusted secret broker connection")
	}
	return &Client{SecretBroker: sb.NewSecretBrokerServiceClient(connection), Drafts: sb.NewSecretBrokerServiceClient(connection),
		connection: connection, timeout: config.RequestTimeout}, nil
}
