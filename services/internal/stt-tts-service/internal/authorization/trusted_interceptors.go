package authorization

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor resolves owner identity after workload admission and before the handler, returning a safe error if resolution fails.
// RU: UnaryServerInterceptor разрешает идентичность владельца после workload admission и до handler, возвращая безопасную ошибку при отказе разрешения.
// Only methods supported by Resolve can acquire user identity; technical methods are not implicitly authorized.
// RU: Пользовательскую идентичность получают только поддержанные Resolve методы; технические методы не разрешаются неявно.
func (resolver *TrustedResolver) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		bound, err := resolver.Resolve(ctx, info.FullMethod)
		if err != nil {
			return nil, trustedResolutionError(ctx, err)
		}
		return handler(bound, request)
	}
}

// StreamServerInterceptor resolves the owner principal before the first audio receive, then wraps the live stream without replacing its cancellation source.
// RU: StreamServerInterceptor разрешает субъекта владельца до первого приёма аудио, затем оборачивает живой поток без замены источника его отмены.
func (resolver *TrustedResolver) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		bound, err := resolver.Resolve(stream.Context(), info.FullMethod)
		if err != nil {
			return trustedResolutionError(stream.Context(), err)
		}
		principal := bound.Value(trustedPrincipalKey{}).(trustedPrincipal)
		return handler(server, &trustedPrincipalStream{ServerStream: stream, principal: principal})
	}
}

// trustedPrincipalStream retains the original ServerStream and reattaches the resolved method-bound principal whenever its context is read.
// RU: trustedPrincipalStream сохраняет исходный ServerStream и возвращает разрешённого субъекта с привязкой к методу при каждом чтении контекста.
type trustedPrincipalStream struct {
	grpc.ServerStream
	principal trustedPrincipal
}

// Context preserves the current underlying stream deadline and cancellation while adding only the private principal value.
// RU: Context сохраняет текущие deadline и отмену нижележащего потока, добавляя только приватное значение субъекта.
func (stream *trustedPrincipalStream) Context() context.Context {
	return context.WithValue(stream.ServerStream.Context(), trustedPrincipalKey{}, stream.principal)
}

// trustedResolutionError preserves request cancellation and transient owner status codes, mapping all other resolution failures to a safe permission denial.
// RU: trustedResolutionError сохраняет отмену запроса и временные коды владельца, отображая остальные ошибки разрешения в безопасный отказ доступа.
func trustedResolutionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	code := status.Code(err)
	switch code {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return status.Error(code, "STT owner authority unavailable")
	default:
		return status.Error(codes.PermissionDenied, "STT owner authority rejected")
	}
}
