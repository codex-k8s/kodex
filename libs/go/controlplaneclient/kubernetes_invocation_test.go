package controlplaneclient

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// TestManagedKubernetesOwnerConnectionKeepsUnaryMethodBoundary observes pre-network dispatch only.
// A synthetic interceptor proves request identity and unrelated-method refusal, not TLS/proof validity.
// TestManagedKubernetesOwnerConnectionKeepsUnaryMethodBoundary проверяет лишь pre-network dispatch.
// Synthetic interceptor доказывает request identity/refusal, не TLS/proof validity.
func TestManagedKubernetesOwnerConnectionKeepsUnaryMethodBoundary(t *testing.T) {
	calls := 0
	original := &cp.GetManagedKubernetesInvocationRequest{Binding: &cp.ManagedKubernetesOwnerBinding{InvocationRef: "inv_original"}}
	protected, err := grpc.NewClient("passthrough:///synthetic-owner", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(func(_ context.Context, _ string, input, output any, _ *grpc.ClientConn, _ grpc.UnaryInvoker, _ ...grpc.CallOption) error {
		calls++
		if input != original {
			t.Fatal("owner wrapper replaced original request")
		}
		return nil
	}))
	if err != nil {
		t.Fatal("construct synthetic intercepted connection")
	}
	defer protected.Close()
	connection := (&Client{protected: protected}).ManagedKubernetesOwnerConnection()
	for _, method := range []string{cp.ManagedKubernetesOwnerService_RenewManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_CancelManagedKubernetesInvocation_FullMethodName, cp.ManagedKubernetesOwnerService_AcceptManagedKubernetesOutputFrame_FullMethodName, cp.ManagedKubernetesOwnerService_GetManagedKubernetesOutputFrame_FullMethodName, cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInputFrame_FullMethodName, cp.ManagedKubernetesOwnerService_CheckManagedKubernetesOwnerReadiness_FullMethodName} {
		if err := connection.Invoke(t.Context(), method, original, &cp.GetManagedKubernetesInvocationResponse{}); err != nil {
			t.Fatal("fixed owner unary method refused", err)
		}
	}
	for _, method := range []string{cp.ManagedKubernetesOwnerService_BeginManagedKubernetesEffect_FullMethodName, cp.ManagedKubernetesOwnerService_ResolveManagedKubernetesOperation_FullMethodName, cp.ManagedKubernetesOwnerService_ClaimManagedKubernetesInvocations_FullMethodName, cp.PlatformQueryService_GetBootstrapState_FullMethodName, "/caller.chosen/Arbitrary"} {
		if err := connection.Invoke(t.Context(), method, original, nil); status.Code(err) != codes.PermissionDenied {
			t.Fatal("unrelated owner method reached protected channel")
		}
	}
	if _, err := connection.NewStream(t.Context(), &grpc.StreamDesc{}, cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName); status.Code(err) != codes.PermissionDenied {
		t.Fatal("streaming escaped unary owner port")
	}
	if calls != 8 {
		t.Fatal("rejected methods reached protected channel")
	}
	var absent *Client
	if err := absent.ManagedKubernetesOwnerConnection().Invoke(t.Context(), cp.ManagedKubernetesOwnerService_GetManagedKubernetesInvocation_FullMethodName, original, nil); status.Code(err) != codes.PermissionDenied {
		t.Fatal("missing protected client became a usable port")
	}
}
