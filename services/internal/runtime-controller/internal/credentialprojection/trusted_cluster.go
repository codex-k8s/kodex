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

// dialTrustedCluster accepts only the explicit trusted-cluster profile, exact internal broker target and bounded timeout.
// It constructs a plaintext, size-bounded gRPC channel with the trusted operation interceptor and no local issuer or new proof.
// Construction performs no network I/O; lease/attempt/input pins remain unchanged and materialization authorization belongs to the receiver.
// dialTrustedCluster принимает только явный профиль доверенного кластера, точный внутренний адрес брокера и ограниченный таймаут.
// Создаётся незашифрованный ограниченный по размеру канал gRPC с перехватчиком доверенных операций, без локального издателя и нового доказательства.
// Создание не обращается к сети; привязки аренды/попытки/входа неизменны, а авторизация размещения остаётся у принимающей стороны.
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
