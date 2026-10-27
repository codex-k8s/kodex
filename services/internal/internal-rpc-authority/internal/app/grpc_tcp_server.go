package app

import (
	"context"
	"crypto/tls"
	"log/slog"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
)

// newObservedTCPGRPCServer constructs the shared TLS gRPC server with strict codec and ordered
// metrics, tracing and error interceptors. The caller supplies its closed method normalizer,
// registers exact services and owns TLS configuration, listener and shutdown.
// newObservedTCPGRPCServer создаёт общий TLS gRPC server со строгим codec и упорядоченными
// metrics, tracing и error interceptors. Caller передаёт закрытый normalizer методов,
// регистрирует точные сервисы и владеет TLS-конфигурацией, listener и завершением.
func newObservedTCPGRPCServer(serverTLS *tls.Config, metrics *observability.Metrics, telemetry *appTelemetry, methods map[string]string, logger *slog.Logger, normalize func(string) string) *grpc.Server {
	return grpc.NewServer(
		grpc.Creds(credentials.NewTLS(serverTLS)),
		grpc.ForceServerCodec(grpcserver.StrictProtoCodec()),
		grpc.ChainUnaryInterceptor(
			metrics.UnaryServerInterceptor(),
			telemetry.UnaryServerInterceptor(methods),
			grpcserver.ErrorBoundary(grpcserver.ErrorObserverFunc(func(
				_ context.Context, method string, code codes.Code, _ error,
			) {
				logger.Error(
					"unexpected gRPC failure",
					"method", normalize(method),
					"code", code.String(),
				)
			})),
		),
	)
}
