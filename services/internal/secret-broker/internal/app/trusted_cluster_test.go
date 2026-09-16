package app

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestTrustedBrokerExactCallerMethods(t *testing.T) {
	interceptor, err := trustedClusterAdmission("trusted-cluster")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		caller, method string
		allowed        bool
	}{
		{"control-api-gateway", sb.SecretBrokerService_CheckReadiness_FullMethodName, true},
		{"control-api-gateway", sb.SecretBrokerService_SaveSecretDraft_FullMethodName, true},
		{"control-plane", cp.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName, true},
		{"control-api-gateway", cp.ProviderCredentialMaterializerService_MaterializeAPIKey_FullMethodName, false},
		{"unknown", sb.SecretBrokerService_CheckReadiness_FullMethodName, false},
		{"control-plane", "/unknown.Service/Command", false},
	} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-kodex-rpc-profile", "trusted-cluster",
			"x-kodex-trusted-caller", "spiffe://kodex.local/ns/kodex-system/sa/"+fixture.caller))
		called := false
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: fixture.method}, func(ctx context.Context, _ any) (any, error) {
			called = true
			admission, ok := serviceidentity.FromContext(ctx)
			if !ok || admission.FullMethod != fixture.method {
				t.Fatal("server admission absent")
			}
			return nil, nil
		})
		if (err == nil) != fixture.allowed || called != fixture.allowed {
			t.Fatal("caller method boundary mismatch")
		}
	}
	if _, err := trustedClusterAdmission(""); err == nil {
		t.Fatal("implicit trusted profile accepted")
	}
}
