package authorization

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServiceIdentityStream поддерживает только передачу execution artifact с
// одним начальным запросом. Lease/fence/generation проверяет прежний owner path.
func ServiceIdentityStream(authorizer *serviceidentity.Authorizer, resolver *rpcprincipal.Service) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if authorizer == nil || resolver == nil {
			return status.Error(codes.Unavailable, "service stream authorization unavailable")
		}
		if info.FullMethod != cp.RuntimeWorkService_StreamExecutionArtifact_FullMethodName || info.IsClientStream || !info.IsServerStream {
			return status.Error(codes.PermissionDenied, "service stream method rejected")
		}
		admission, err := authorizer.Admit(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		if admission.ActorMode != serviceidentity.ServiceActor {
			return status.Error(codes.PermissionDenied, "service stream actor rejected")
		}
		return handler(server, &servicePrincipalStream{ServerStream: stream, ctx: stream.Context(), method: info.FullMethod, authorizer: authorizer, resolve: ServiceIdentityUnary(authorizer, resolver)})
	}
}

type servicePrincipalStream struct {
	grpc.ServerStream
	ctx        context.Context
	method     string
	authorizer *serviceidentity.Authorizer
	resolve    grpc.UnaryServerInterceptor
	received   bool
}

func (stream *servicePrincipalStream) Context() context.Context { return stream.ctx }
func (stream *servicePrincipalStream) RecvMsg(message any) error {
	if stream.received {
		return status.Error(codes.InvalidArgument, "service stream accepts one request")
	}
	stream.received = true
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	_, err := stream.resolve(stream.ctx, message, &grpc.UnaryServerInfo{FullMethod: stream.method}, func(ctx context.Context, _ any) (any, error) { stream.ctx = ctx; return nil, nil })
	return err
}
func (stream *servicePrincipalStream) SendMsg(message any) error {
	if !stream.received {
		return status.Error(codes.PermissionDenied, "service stream request required")
	}
	if _, err := Principal(stream.ctx, stream.method); err != nil {
		return status.Error(codes.PermissionDenied, "service stream principal required")
	}
	if _, err := stream.authorizer.Admit(stream.ctx, stream.method); err != nil {
		return err
	}
	return stream.ServerStream.SendMsg(message)
}
