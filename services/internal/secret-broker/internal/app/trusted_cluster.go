package app

import (
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
)

// Реестр остаётся закрытым. Domain operation grants в запросах Secrets и
// owner-resolved runtime projection проверяются обработчиками отдельно.
func trustedClusterAdmission(profile string) (grpc.UnaryServerInterceptor, error) {
	bindings := []serviceidentity.Binding{}
	add := func(caller string, operations map[string]string) {
		for operation, method := range operations {
			bindings = append(bindings, serviceidentity.Binding{
				CallerSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/" + caller,
				FullMethod:     method, OperationID: operation, Permission: operation,
				ActorMode: serviceidentity.ServiceActor,
			})
		}
	}
	add("control-plane", controlplaneclient.ProviderCredentialMaterializerOperations())
	add("control-api-gateway", controlplaneclient.SecretDraftGatewayOperations())
	add("control-api-gateway", map[string]string{
		"secrets.create":    sb.SecretBrokerService_CreateSecret_FullMethodName,
		"secrets.rotate":    sb.SecretBrokerService_RotateSecret_FullMethodName,
		"secrets.reveal":    sb.SecretBrokerService_RevealSecret_FullMethodName,
		"secrets.revoke":    sb.SecretBrokerService_RevokeSecret_FullMethodName,
		"secrets.readiness": sb.SecretBrokerService_CheckReadiness_FullMethodName,
	})
	add("runtime-controller", controlplaneclient.RuntimeCredentialProjectionOperations())
	add("stt-tts-service", controlplaneclient.STTCredentialProjectionOperations())
	authorizer, err := serviceidentity.NewTrustedCluster(profile,
		"spiffe://kodex.local/ns/kodex-system/sa/secret-broker", bindings)
	if err != nil {
		return nil, err
	}
	return authorizer.UnaryServerInterceptor(), nil
}
