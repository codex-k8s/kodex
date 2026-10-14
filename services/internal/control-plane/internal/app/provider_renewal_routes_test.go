package app

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"google.golang.org/grpc/metadata"
)

func TestProviderRenewalOwnerRoutesRequireExactBroker(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer(transportprofile.TrustedCluster)
	if err != nil {
		t.Fatal(err)
	}
	for operation, method := range map[string]string{
		"platform.provider-accounts.credential.renewal.work.check": cp.ProviderCredentialRenewalWorkService_CheckProviderCredentialRenewalWorkReadiness_FullMethodName,
		"platform.provider-accounts.credential.renewal.authorize":  cp.ProviderCredentialRenewalWorkService_AuthorizeProviderCredentialRenewal_FullMethodName,
		"platform.provider-accounts.credential.renewal.validate":   cp.ProviderCredentialRenewalWorkService_ValidateProviderCredentialRenewalObservation_FullMethodName,
	} {
		for _, caller := range []string{"secret-broker", "control-api-gateway", "runtime-controller"} {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
				serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller))
			admission, err := authorizer.Admit(ctx, method)
			if caller == "secret-broker" {
				if err != nil || admission.ActorMode != serviceidentity.ServiceActor || admission.ProjectRequired || admission.Permission != operation {
					t.Fatal("exact renewal owner route missing")
				}
			} else if err == nil {
				t.Fatal("foreign renewal caller admitted")
			}
		}
	}
}
