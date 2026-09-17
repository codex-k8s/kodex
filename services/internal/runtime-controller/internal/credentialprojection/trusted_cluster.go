package credentialprojection

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Транспорт не создаёт proof и не изменяет lease/attempt/input в запросе.
// Владельческая проверка materialization остаётся обязанностью принимающей стороны.
func dialTrustedCluster(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || ctx.Err() != nil || config.RPCProfile != transportprofile.TrustedCluster ||
		config.Target != "dns:///secret-broker.kodex-system.svc:8443" ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second {
		return nil, errors.New("trusted runtime projection configuration rejected")
	}
	interceptor, err := controlplaneclient.TrustedUnaryClientInterceptor(config.RPCProfile,
		"runtime-controller", controlplaneclient.RuntimeCredentialProjectionOperations())
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(config.Target, grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptor),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1<<20), grpc.MaxCallSendMsgSize(1<<20)))
	if err != nil {
		return nil, errors.New("create trusted runtime projection connection")
	}
	return &Client{api: secretbrokerv1.NewRuntimeCredentialProjectionServiceClient(connection), connection: connection}, nil
}
