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

// resolvedPrincipalKey privately keys the domain principal installed by this package after
// admission and owner resolution.
// resolvedPrincipalKey закрыто адресует доменный principal, установленный пакетом после admission
// и разрешения владельцем.
type resolvedPrincipalKey struct{}

// resolvedPrincipal binds one resolved domain identity to its exact RPC method and admitted
// transport profile without exposing a caller-set context key.
// resolvedPrincipal связывает одну разрешённую доменную identity с точным RPC методом и допущенным
// transport profile без выдачи caller-set context key.
type resolvedPrincipal struct {
	method    string
	profile   string
	principal value.Principal
}

// AdmissionAuthorizer is supplied by the composition root for one explicit transport profile;
// admission and profile metadata do not replace domain owner resolution.
// AdmissionAuthorizer поставляется composition root для одного явного transport profile; admission
// и metadata профиля не заменяют разрешение доменного владельца.
type AdmissionAuthorizer interface {
	// Admit checks the configured peer/method admission and returns its server-owned operation/actor
	// mode for subsequent domain resolution.
	// Admit проверяет настроенный peer и method admission и возвращает server-owned operation и actor
	// mode для последующего доменного разрешения.
	Admit(context.Context, string) (serviceidentity.Admission, error)
	// RPCProfile returns the composition-selected profile that incoming metadata must match exactly,
	// rather than choosing a profile from the request.
	// RPCProfile возвращает выбранный composition profile, которому incoming metadata должны точно
	// соответствовать, вместо выбора профиля из запроса.
	RPCProfile() string
}

// ServiceIdentityUnary admits the peer/method, hashes the deterministic protobuf request and
// validates profile/project/credential metadata before owner resolution and installation of an
// exact-method principal; errors use closed gRPC mappings.
// ServiceIdentityUnary допускает peer и метод, хеширует deterministic protobuf request и проверяет
// profile, project и credential metadata до разрешения владельцем и установки principal точного
// метода; ошибки используют закрытые gRPC mappings.
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
		return handler(context.WithValue(ctx, resolvedPrincipalKey{}, resolvedPrincipal{method: info.FullMethod, profile: admission.RPCProfile, principal: principal}), request)
	}
}
