package app

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
)

// trustedClusterReader не создаёт proof service, verifier socket или TLS.
// Browser credential проверяется тем же OIDC verifier; principal разрешает CP.
func trustedClusterReader(ctx context.Context, config Config, owner rpcprincipal.Owner) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, *oidcverifier.Verifier, error) {
	authorizer, err := serviceidentity.TrustedClusterFromPolicy(config.RPCProfile, serviceIdentityPolicy,
		"spiffe://kodex.local/ns/kodex-system/sa/control-plane")
	if err != nil {
		return nil, nil, nil, err
	}
	verifier, err := oidcverifier.New(ctx, oidcverifier.Config{
		Issuer: config.OIDCIssuer, Audience: config.OIDCAudience, JWKSURL: config.OIDCJWKSURL,
		ConnectAddress: config.OIDCConnectAddress, TLSServerName: config.OIDCTLSServerName,
		CAFile: config.OIDCCAFile, Timeout: config.ReadinessTimeout,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	resolver, err := rpcprincipal.New(owner, verifier)
	if err != nil {
		verifier.Close()
		return nil, nil, nil, err
	}
	return authorization.ServiceIdentityUnary(authorizer, resolver), authorization.ServiceIdentityStream(authorizer, resolver), verifier, nil
}
