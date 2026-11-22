package controlplaneclient

import (
	"context"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
)

// kubernetesPublicProofResolver records exact production-map operation requests without minting a usable authority proof.
// kubernetesPublicProofResolver фиксирует exact production-map operation requests без выдачи пригодного authority proof.
type kubernetesPublicProofResolver struct {
	api.AuthorityProofResolverServiceClient
	operation string
	calls     int
}

// ResolveAuthorityProof deliberately returns an incomplete proof after capturing the operation selected by the real client registry.
// ResolveAuthorityProof намеренно возвращает неполный proof после фиксации operation, выбранной настоящим client registry.
func (r *kubernetesPublicProofResolver) ResolveAuthorityProof(_ context.Context, in *api.ResolveAuthorityProofRequest, _ ...grpc.CallOption) (*api.ResolveAuthorityProofResponse, error) {
	r.calls++
	r.operation = in.OperationId
	return &api.ResolveAuthorityProofResponse{}, nil
}

// TestKubernetesPublicOperationsReachExactProofPath uses the actual production map, including Material commands previously missing there, without claiming live policy or signature acceptance.
// TestKubernetesPublicOperationsReachExactProofPath использует actual production map, включая прежде отсутствовавшие Material commands, не заявляя live policy либо signature acceptance.
func TestKubernetesPublicOperationsReachExactProofPath(t *testing.T) {
	operations, err := validateOperations(ControlAPIGatewayOperations())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ operation, method string }{
		{"platform.integrations.kubernetes.capture.cleanup.repair", cp.ManagedKubernetesCaptureService_RepairManagedKubernetesStagingCleanup_FullMethodName},
		{"platform.integrations.kubernetes.capture.get", cp.ManagedKubernetesCaptureService_GetManagedKubernetesCaptureStatus_FullMethodName},
		{"platform.integrations.kubernetes.capture.list", cp.ManagedKubernetesCaptureService_ListManagedKubernetesCaptures_FullMethodName},
		{"platform.integrations.kubernetes.capture.repair", cp.ManagedKubernetesCaptureService_RepairManagedKubernetesCapture_FullMethodName},
		{"platform.integrations.kubernetes.output.read", cp.ManagedKubernetesStreamOutputService_ReadManagedKubernetesStreamOutput_FullMethodName},
		{"platform.integrations.kubernetes.port-forward.open", cp.ManagedKubernetesPortForwardConnectionService_OpenManagedKubernetesPortForwardConnection_FullMethodName},
		{"platform.integrations.kubernetes.port-forward.get", cp.ManagedKubernetesPortForwardConnectionService_GetManagedKubernetesPortForwardConnection_FullMethodName},
		{"platform.integrations.kubernetes.port-forward.list", cp.ManagedKubernetesPortForwardConnectionService_ListManagedKubernetesPortForwardConnections_FullMethodName},
		{"platform.integrations.kubernetes.port-forward.close", cp.ManagedKubernetesPortForwardConnectionService_CloseManagedKubernetesPortForwardConnection_FullMethodName},
		{"platform.integrations.kubernetes.material.begin", cp.ManagedKubernetesResourceMaterialService_BeginManagedKubernetesResourceMaterial_FullMethodName},
		{"platform.integrations.kubernetes.material.get", cp.ManagedKubernetesResourceMaterialService_GetManagedKubernetesResourceMaterial_FullMethodName},
		{"platform.integrations.kubernetes.material.cancel", cp.ManagedKubernetesResourceMaterialService_CancelManagedKubernetesResourceMaterial_FullMethodName},
		{"platform.integrations.kubernetes.tool.list", cp.ManagedKubernetesToolService_ListManagedKubernetesTools_FullMethodName},
		{"platform.integrations.kubernetes.tool.get", cp.ManagedKubernetesToolService_GetManagedKubernetesTool_FullMethodName},
		{"platform.integrations.kubernetes.tool.stop", cp.ManagedKubernetesToolService_StopManagedKubernetesTool_FullMethodName},
		{"platform.integrations.kubernetes.input.list", cp.ManagedKubernetesStreamInputService_ListManagedKubernetesStreamInputs_FullMethodName},
		{"platform.integrations.kubernetes.input.stop", cp.ManagedKubernetesStreamInputService_StopManagedKubernetesStream_FullMethodName},
		{"platform.integrations.kubernetes.input.get", cp.ManagedKubernetesStreamInputService_GetManagedKubernetesStreamInput_FullMethodName},
		{"platform.integrations.kubernetes.input.submit", cp.ManagedKubernetesStreamInputService_SubmitManagedKubernetesStreamInput_FullMethodName},
	} {
		t.Run(item.operation, func(t *testing.T) {
			resolver := &kubernetesPublicProofResolver{}
			client := &Client{proofOperations: operations, resolver: resolver}
			ctx, err := WithApplicationGrant(t.Context(), "synthetic-private-application-grant")
			if err != nil {
				t.Fatal(err)
			}
			proof, _, err := client.AuthorityProof(ctx, item.operation, item.method)
			if err == nil || proof != "" || !strings.Contains(err.Error(), "authority_stage="+string(authorityclient.StageProofResponse)) || resolver.calls != 1 || resolver.operation != item.operation {
				t.Fatal("production method did not reach its exact registered proof route")
			}
			if _, _, err := client.AuthorityProof(ctx, item.operation+".foreign", item.method); err == nil || resolver.calls != 1 {
				t.Fatal("foreign operation reached resolver")
			}
		})
	}
}
