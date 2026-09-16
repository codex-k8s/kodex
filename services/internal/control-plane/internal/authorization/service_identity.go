package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type resolvedPrincipalKey struct{}
type resolvedPrincipal struct {
	method    string
	principal value.Principal
}

// AdmissionAuthorizer выбирается только composition root выбранного профиля.
type AdmissionAuthorizer interface {
	Admit(context.Context, string) (serviceidentity.Admission, error)
	RPCProfile() string
}

func ServiceIdentityUnary(authorizer AdmissionAuthorizer, resolver *rpcprincipal.Service) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if authorizer == nil || resolver == nil {
			return nil, status.Error(codes.Unavailable, "service authorization is unavailable")
		}
		admission, err := authorizer.Admit(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		message, ok := request.(proto.Message)
		if !ok || message == nil || !message.ProtoReflect().IsValid() {
			return nil, status.Error(codes.InvalidArgument, "protobuf request is required")
		}
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "protobuf request encoding failed")
		}
		digest := sha256.Sum256(raw)
		md, _ := metadata.FromIncomingContext(ctx)
		profiles := md.Get("x-kodex-rpc-profile")
		if len(profiles) != 1 || profiles[0] != authorizer.RPCProfile() || len(md.Get("x-kodex-authorization")) != 0 {
			return nil, status.Error(codes.Unauthenticated, "RPC service profile rejected")
		}
		projects := md.Get("x-kodex-project-ref")
		if len(projects) > 1 {
			return nil, status.Error(codes.InvalidArgument, "multiple RPC project locators rejected")
		}
		project := ""
		if len(projects) == 1 {
			project = projects[0]
			if len(project) == 0 || len(project) > 96 {
				return nil, status.Error(codes.InvalidArgument, "RPC project locator rejected")
			}
		}
		credentials := md.Get("authorization")
		if len(credentials) > 1 {
			return nil, status.Error(codes.Unauthenticated, "multiple application credentials rejected")
		}
		credential := ""
		if len(credentials) == 1 {
			credential = credentials[0]
		}
		principal, err := resolver.Resolve(ctx, rpcprincipal.Input{Admission: admission, Authorization: credential, ProjectRef: project, RequestDigestSHA256: hex.EncodeToString(digest[:])})
		if err != nil {
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			if errors.Is(err, errs.ErrUnavailable) {
				return nil, status.Error(codes.Unavailable, "RPC domain authority unavailable")
			}
			if errors.Is(err, errs.ErrUnauthorized) {
				return nil, status.Error(codes.Unauthenticated, "RPC user credential rejected")
			}
			return nil, status.Error(codes.PermissionDenied, "RPC domain authority rejected")
		}
		return handler(context.WithValue(ctx, resolvedPrincipalKey{}, resolvedPrincipal{method: info.FullMethod, principal: principal}), request)
	}
}
