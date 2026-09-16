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

// ServiceIdentityStream поддерживает закрытый набор streaming RPC. Для upload
// principal связывается с первой metadata-частью, а размер и digest всего тела
// проверяет handler до устойчивой записи.
func ServiceIdentityStream(authorizer AdmissionAuthorizer, resolver *rpcprincipal.Service) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if authorizer == nil || resolver == nil {
			return status.Error(codes.Unavailable, "service stream authorization unavailable")
		}
		runtimeServerStream := info.FullMethod == cp.RuntimeWorkService_StreamExecutionArtifact_FullMethodName && !info.IsClientStream && info.IsServerStream
		userServerStream := info.FullMethod == cp.PlatformCommandService_DownloadArtifact_FullMethodName && !info.IsClientStream && info.IsServerStream
		serverStream := runtimeServerStream || userServerStream
		clientStream := (info.FullMethod == cp.PlatformCommandService_UploadArtifact_FullMethodName || info.FullMethod == cp.PlatformCommandService_UploadOrganizationArtifact_FullMethodName) && info.IsClientStream && !info.IsServerStream
		if !serverStream && !clientStream {
			return status.Error(codes.PermissionDenied, "service stream method rejected")
		}
		admission, err := authorizer.Admit(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		if runtimeServerStream && admission.ActorMode != serviceidentity.ServiceActor ||
			(userServerStream || clientStream) && admission.ActorMode != serviceidentity.UserActor {
			return status.Error(codes.PermissionDenied, "service stream actor rejected")
		}
		return handler(server, &servicePrincipalStream{ServerStream: stream, ctx: stream.Context(), method: info.FullMethod, authorizer: authorizer, resolve: ServiceIdentityUnary(authorizer, resolver), singleRequest: serverStream})
	}
}

type servicePrincipalStream struct {
	grpc.ServerStream
	ctx           context.Context
	method        string
	authorizer    AdmissionAuthorizer
	resolve       grpc.UnaryServerInterceptor
	received      bool
	singleRequest bool
}

func (stream *servicePrincipalStream) Context() context.Context { return stream.ctx }
func (stream *servicePrincipalStream) RecvMsg(message any) error {
	if stream.received && stream.singleRequest {
		return status.Error(codes.InvalidArgument, "service stream accepts one request")
	}
	if _, err := stream.authorizer.Admit(stream.ctx, stream.method); err != nil {
		return err
	}
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	if _, err := stream.authorizer.Admit(stream.ctx, stream.method); err != nil {
		return err
	}
	if stream.received {
		return nil
	}
	stream.received = true
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
