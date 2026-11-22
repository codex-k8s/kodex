package grpc

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPortForwardUnsupportedProfileRPCPreservesOnlyClosedReason tests the actual protected handler without fabricating a completed native effect or exposing upstream text.
// TestPortForwardUnsupportedProfileRPCPreservesOnlyClosedReason проверяет actual protected handler без fabricated completed native effect либо раскрытия upstream text.
func TestPortForwardUnsupportedProfileRPCPreservesOnlyClosedReason(t *testing.T) {
	request, fixture := kubernetesOperationRPCFixture()
	fixture.err = targetrepo.ErrPortForwardProfileUnsupported
	server := &Server{}
	WithKubernetesOperations(fixture)(server)
	response, err := invokeKubernetesOperation(t, server, request, false, "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway", "kubernetes.execute")
	result, ok := status.FromError(err)
	if response != nil || !ok || result.Code() != codes.FailedPrecondition || len(result.Details()) != 1 || fixture.calls != 1 {
		t.Fatal("preflight error became completed output or generic unavailable")
	}
	detail, ok := result.Details()[0].(*errdetails.ErrorInfo)
	if !ok || detail.Domain != "kodex.secret-broker" || detail.Reason != managedkubernetes.ReasonPortForwardProfileUnsupported || len(detail.Metadata) != 0 {
		t.Fatal("preflight detail changed its closed contract")
	}
}
