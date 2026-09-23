package authorization

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor выполняется после workload admission и до handler.
// Технические методы не получают пользовательскую identity этим interceptor.
func (resolver *TrustedResolver) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		bound, err := resolver.Resolve(ctx, info.FullMethod)
		if err != nil {
			return nil, trustedResolutionError(ctx, err)
		}
		return handler(bound, request)
	}
}

// StreamServerInterceptor разрешает owner identity до первого audio Recv.
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

type trustedPrincipalStream struct {
	grpc.ServerStream
	principal trustedPrincipal
}

func (stream *trustedPrincipalStream) Context() context.Context {
	return context.WithValue(stream.ServerStream.Context(), trustedPrincipalKey{}, stream.principal)
}

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
