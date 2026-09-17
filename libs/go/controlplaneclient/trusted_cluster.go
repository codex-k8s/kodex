package controlplaneclient

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var trustedWorkloadPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// TrustedUnaryClientInterceptor переносит только явно установленный credential,
// а caller/profile назначает adapter. Target проверяется самим adapter до dial.
func TrustedUnaryClientInterceptor(profile, caller string, operations map[string]string) (grpc.UnaryClientInterceptor, error) {
	if profile != transportprofile.TrustedCluster || !trustedWorkloadPattern.MatchString(caller) || len(operations) == 0 {
		return nil, errors.New("trusted cluster interceptor configuration rejected")
	}
	registered, err := validateOperations(operations)
	if err != nil {
		return nil, err
	}
	return serviceUnary(registered, nil, profile, "spiffe://kodex.local/ns/kodex-system/sa/"+caller), nil
}

func TrustedStreamClientInterceptor(profile, caller string, operations map[string]string) (grpc.StreamClientInterceptor, error) {
	unary, err := TrustedUnaryClientInterceptor(profile, caller, operations)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, desc *grpc.StreamDesc, conn *grpc.ClientConn, method string, next grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		var stream grpc.ClientStream
		err := unary(ctx, method, nil, nil, conn, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
			var err error
			stream, err = next(ctx, desc, conn, method, options...)
			return err
		})
		return stream, err
	}, nil
}

// dialTrustedCluster не создаёт legacy connection, issuer или proof resolver.
// Plaintext допускается только для точного внутреннего Service; внешний
// endpoint нельзя включить одной настройкой insecure.
func dialTrustedCluster(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil || ctx.Err() != nil || config.RPCProfile != transportprofile.TrustedCluster ||
		!trustedWorkloadPattern.MatchString(config.CallerWorkload) ||
		config.DialTimeout < 100*time.Millisecond || config.DialTimeout > 5*time.Second || len(config.Operations) == 0 {
		return nil, errors.New("trusted cluster client configuration rejected")
	}
	switch strings.TrimPrefix(config.Target, "dns:///") {
	case "control-plane.kodex-system.svc:8443", "control-plane.kodex-system.svc.cluster.local:8443":
	case "secret-broker.kodex-system.svc:8443", "secret-broker.kodex-system.svc.cluster.local:8443":
		if config.CallerWorkload != "control-plane" {
			return nil, errors.New("trusted materializer caller rejected")
		}
		allowed := ProviderCredentialMaterializerOperations()
		for operation, method := range config.Operations {
			if allowed[operation] != method {
				return nil, errors.New("trusted materializer operation rejected")
			}
		}
	default:
		return nil, errors.New("trusted cluster target must be an approved internal service")
	}
	operations, err := validateOperations(config.Operations)
	if err != nil {
		return nil, err
	}
	projects, err := serviceProjectOperations(operations, config.ProofOperations, config.ProjectRequiredOperations)
	if err != nil {
		return nil, err
	}
	caller := "spiffe://kodex.local/ns/kodex-system/sa/" + config.CallerWorkload
	interceptors := []grpc.UnaryClientInterceptor{serviceUnary(operations, projects, config.RPCProfile, caller)}
	if config.UnaryClientInterceptor != nil {
		interceptors = append(interceptors, config.UnaryClientInterceptor)
	}
	connection, err := grpc.NewClient(config.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(interceptors...),
		grpc.WithStreamInterceptor(serviceStream(operations, projects, config.RPCProfile, caller)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maximumProtectedResponseBytes), grpc.MaxCallSendMsgSize(17<<20)))
	if err != nil {
		return nil, errors.New("create trusted cluster control-plane connection")
	}
	client := &Client{trustedCluster: true, serviceIdentity: true}
	client.bindServices(connection)
	return client, nil
}
