package grpc

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

// sshHealthRPCFixture returns only a finite owner receipt and counts entry after actual method admission.
// sshHealthRPCFixture возвращает лишь finite owner receipt и считает entry после actual method admission.
type sshHealthRPCFixture struct {
	calls   int
	receipt managedssh.HealthReceipt
}

// Probe observes the original metadata binding without reading a key, choosing a target or running a second Claim.
// Probe наблюдает original metadata binding без key read, выбора target либо второго Claim.
func (f *sshHealthRPCFixture) Probe(_ context.Context, b managedssh.Binding) (managedssh.HealthReceipt, error) {
	f.calls++
	return f.receipt, nil
}

// TestSSHHealthRPCRegistryBoundary exercises the real registered operation, exact method policy and malformed nested bindings before domain I/O; raw metadata is never authority.
// TestSSHHealthRPCRegistryBoundary проверяет real registered operation, exact method policy и malformed nested bindings до domain I/O; raw metadata не является authority.
func TestSSHHealthRPCRegistryBoundary(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"
	method := sb.ManagedSSHService_ProbeSSHIdentity_FullMethodName
	operation := ""
	for key, value := range controlplaneclient.SSHIdentityBrokerOperations() {
		if value == method {
			if operation != "" {
				t.Fatal("duplicate method registration")
			}
			operation = key
		}
	}
	if operation == "" {
		t.Fatal("health method unregistered")
	}
	for _, scenario := range []string{"exact", "raw-metadata", "foreign-operation", "unknown-request", "unknown-binding", "foreign-receipt", "mixed-receipt", "unwired"} {
		t.Run(scenario, func(t *testing.T) {
			request := &sb.ProbeSSHIdentityRequest{Binding: &sb.ManagedSSHHealthBinding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: "test_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}}
			fixture := &sshHealthRPCFixture{receipt: managedssh.HealthReceipt{TestRef: "test_fixture", State: "SUCCEEDED", Applied: true}}
			server := &Server{}
			WithSSHIdentityHealth(fixture)(server)
			selected := operation
			want := codes.OK
			calls := 1
			switch scenario {
			case "raw-metadata":
				want = codes.PermissionDenied
				calls = 0
			case "foreign-operation":
				selected = "ssh.execute"
				want = codes.PermissionDenied
				calls = 0
			case "unknown-request":
				request.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
				want = codes.InvalidArgument
				calls = 0
			case "unknown-binding":
				request.Binding.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
				want = codes.InvalidArgument
				calls = 0
			case "foreign-receipt":
				fixture.receipt.TestRef = "foreign_test"
				want = codes.Unavailable
			case "mixed-receipt":
				fixture.receipt.State = "CANCELLED"
				want = codes.Unavailable
			case "unwired":
				server.sshHealth = nil
				want = codes.Unavailable
				calls = 0
			}
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller))
			var err error
			if scenario == "raw-metadata" {
				_, err = server.ProbeSSHIdentity(ctx, request)
			} else {
				authorizer, createErr := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: selected, Permission: selected, ActorMode: serviceidentity.ServiceActor}})
				if createErr != nil {
					t.Fatal(createErr)
				}
				_, err = authorizer.UnaryServerInterceptor()(ctx, request, &grpcgo.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) { return server.ProbeSSHIdentity(ctx, request) })
			}
			if status.Code(err) != want || fixture.calls != calls {
				t.Fatalf("boundary mismatch code=%s calls=%d", status.Code(err), fixture.calls)
			}
		})
	}
}
