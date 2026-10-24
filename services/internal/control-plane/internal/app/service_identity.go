package app

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// serviceIdentityPolicy embeds the target-owned compiled route policy; declarations alone do not
// authenticate a peer or turn payload identifiers into authority.
// serviceIdentityPolicy встраивает compiled route policy владельца target; одни declarations не
// аутентифицируют peer и не превращают payload identifiers в полномочия.
//
//go:embed service-identity-policy.json
var serviceIdentityPolicy []byte

// serviceIdentityReader builds the opt-in service-v1 policy/principal interceptors and routes only
// absent profile metadata to legacy; duplicate/unknown profiles and service-v1 failures never
// trigger legacy retry.
// serviceIdentityReader создаёт opt-in service-v1 interceptors policy и principal и направляет в
// legacy только отсутствие profile metadata; дубли, неизвестные profiles и service-v1 отказы не
// запускают legacy retry.
func serviceIdentityReader(owner rpcprincipal.Owner, credentials rpcprincipal.CredentialVerifier, legacy grpc.UnaryServerInterceptor, legacyStream grpc.StreamServerInterceptor) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, error) {
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	authorizer, _, err := serviceidentity.FromPolicy(serviceIdentityPolicy, target, serviceidentity.CertificateLifetimeBoundary{})
	if err != nil {
		return nil, nil, err
	}
	resolver, err := rpcprincipal.New(owner, credentials)
	if err != nil {
		return nil, nil, err
	}
	next := authorization.ServiceIdentityUnary(authorizer, resolver)
	unary := func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		profile := md.Get("x-kodex-rpc-profile")
		if len(profile) == 0 {
			return legacy(ctx, request, info, handler)
		}
		if len(profile) != 1 || profile[0] != "service-v1" {
			return nil, status.Error(codes.Unauthenticated, "RPC service profile rejected")
		}
		return next(ctx, request, info, handler)
	}
	nextStream := authorization.ServiceIdentityStream(authorizer, resolver)
	stream := func(server any, incoming grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, _ := metadata.FromIncomingContext(incoming.Context())
		profile := md.Get("x-kodex-rpc-profile")
		if len(profile) == 0 {
			return legacyStream(server, incoming, info, handler)
		}
		if len(profile) != 1 || profile[0] != "service-v1" {
			return status.Error(codes.Unauthenticated, "RPC service profile rejected")
		}
		return nextStream(server, incoming, info, handler)
	}
	return unary, stream, nil
}
