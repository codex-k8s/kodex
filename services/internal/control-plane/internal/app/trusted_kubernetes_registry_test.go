package app

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"google.golang.org/grpc/metadata"
)

// TestKubernetesTrustedAdmissionUsesActualClientRegistries consumes the generated embedded policy
// through the production authorizer and actual fixed client maps. A policy-generator-only PASS
// cannot hide an identifier rejected by local admission or a missing capture worker registration.
// This proves no signed worker grant, domain permission or installed RPC path.
// TestKubernetesTrustedAdmissionUsesActualClientRegistries потребляет generated embedded policy
// через production authorizer и actual fixed client maps. Успех одного policy generator не скрывает
// identifier, запрещённый local admission, либо missing capture worker registration. Signed worker
// grant, domain permission и installed RPC path эта проверка не доказывает.
func TestKubernetesTrustedAdmissionUsesActualClientRegistries(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer(transportprofile.TrustedCluster)
	if err != nil {
		t.Fatal("actual service identity policy rejected executable Kubernetes registries", err)
	}
	for _, caller := range []struct {
		name       string
		operations map[string]string
		mode       serviceidentity.ActorMode
	}{
		{"control-api-gateway", controlplaneclient.ControlAPIGatewayOperations(), serviceidentity.UserActor},
		{"secret-broker", controlplaneclient.SecretBrokerOperations(), serviceidentity.ServiceActor},
		{"integration-gateway", controlplaneclient.IntegrationGatewayOperations(), serviceidentity.ServiceActor},
		{"artifact-retention", controlplaneclient.ArtifactRetentionCaptureOperations(), serviceidentity.ServiceActor},
	} {
		matched := 0
		for operation, method := range caller.operations {
			if !strings.Contains(method, ".ManagedKubernetes") {
				continue
			}
			matched++
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller.name))
			admission, err := authorizer.Admit(ctx, method)
			if err != nil || admission.OperationID != operation || admission.Permission != operation || admission.ActorMode != caller.mode {
				t.Fatal("actual client method has no exact executable local admission", operation)
			}
			foreign := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/email-bridge"))
			if _, err := authorizer.Admit(foreign, method); err == nil {
				t.Fatal("Kubernetes permission leaked to an unrelated workload", operation)
			}
		}
		if matched == 0 {
			t.Fatal("executable Kubernetes caller registry was empty", caller.name)
		}
	}
}
