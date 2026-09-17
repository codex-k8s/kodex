package app

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/rpcprincipal"
	"google.golang.org/grpc"
)

// trustedClusterReader не создаёт proof service, verifier socket или TLS.
// Browser credential проверяется тем же OIDC verifier; principal разрешает CP.
func trustedClusterReader(ctx context.Context, config Config, owner rpcprincipal.Owner) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, *oidcverifier.Verifier, error) {
	authorizer, err := trustedClusterAuthorizer(config.RPCProfile)
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

func trustedClusterAuthorizer(profile string) (*serviceidentity.TrustedClusterAuthorizer, error) {
	const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	var policy serviceidentity.Policy
	if err := internalrpcauth.DecodeCanonicalJSON(serviceIdentityPolicy, &policy); err != nil || policy.Version != 1 || policy.TargetSPIFFEID != target {
		return nil, errors.New("trusted service policy is invalid")
	}
	credentialBinding := false
	for i := range policy.Bindings {
		binding := &policy.Bindings[i]
		if binding.OperationID != platformrepo.TrustedSTTCredentialOperation {
			continue
		}
		if credentialBinding || binding.CallerSPIFFEID != "spiffe://kodex.local/ns/kodex-system/sa/secret-broker" ||
			binding.FullMethod != cp.RuntimeSecretWorkService_ResolveTranscriptionCredentialProjection_FullMethodName ||
			binding.Permission != platformrepo.TrustedSTTCredentialOperation || binding.ProjectRequired || binding.ActorMode != serviceidentity.ServiceActor {
			return nil, errors.New("trusted STT credential route is invalid")
		}
		binding.ActorMode = serviceidentity.UserActor
		credentialBinding = true
	}
	if !credentialBinding {
		return nil, errors.New("trusted STT credential route is missing")
	}
	for operation, method := range map[string]string{
		platformrepo.TrustedSTTAuthorityOperation:        sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName,
		platformrepo.TrustedSTTCatalogAuthorityOperation: sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName,
		platformrepo.TrustedSTTPolicyOperation:           sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName,
	} {
		policy.Bindings = append(policy.Bindings, serviceidentity.Binding{
			CallerSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service",
			FullMethod:     method, OperationID: operation, Permission: operation, ActorMode: serviceidentity.UserActor,
		})
	}
	policy.Bindings = append(policy.Bindings, serviceidentity.Binding{
		CallerSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/image-admission-controller",
		FullMethod:     cp.RoleImageService_GetImageSupplyWorkAvailability_FullMethodName,
		OperationID:    "platform.role-images.supply-work.get",
		Permission:     "platform.role-images.supply-work.get",
		ActorMode:      serviceidentity.ServiceActor,
	})
	return serviceidentity.NewTrustedCluster(profile, target, policy.Bindings)
}
