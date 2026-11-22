package grpc

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// sshExecutionRPCFixture returns controlled owner metadata only after transport admission; it performs no remote effect and does not emulate owner persistence.
// sshExecutionRPCFixture возвращает controlled owner metadata лишь после transport admission; remote effect и owner persistence не имитируются.
type sshExecutionRPCFixture struct {
	calls, reads int
	receipt      managedssh.ExecutionReceipt
}

// Execute records one finite entry without invoking metadata reconciliation implicitly.
// Execute учитывает один finite entry без implicit metadata reconciliation.
func (f *sshExecutionRPCFixture) Execute(context.Context, managedssh.Binding) (managedssh.ExecutionReceipt, error) {
	f.calls++
	return f.receipt, nil
}

// Get records one separate metadata call and never invokes Execute.
// Get учитывает отдельное metadata read и не вызывает Execute.
func (f *sshExecutionRPCFixture) Get(context.Context, managedssh.Binding) (managedssh.ExecutionReceipt, error) {
	f.reads++
	return f.receipt, nil
}

// TestSSHExecutionRPCUsesExactRegisteredMethods checks real registry names and method admission, malformed nested messages and closed receipt projection without asserting full mTLS or owner qualification.
// TestSSHExecutionRPCUsesExactRegisteredMethods проверяет real registry names и method admission, malformed nested messages и closed receipt projection без заявления full mTLS либо owner qualification.
func TestSSHExecutionRPCUsesExactRegisteredMethods(t *testing.T) {
	const caller = "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"
	for _, read := range []bool{false, true} {
		for _, scenario := range []string{"exact", "raw-metadata", "foreign-operation", "unknown-request", "unknown-binding", "unwired", "mixed-receipt"} {
			t.Run(map[bool]string{false: "execute", true: "reconcile"}[read]+"/"+scenario, func(t *testing.T) {
				method, operation := sb.ManagedSSHService_ExecuteSSHIdentity_FullMethodName, "ssh.execute"
				if read {
					method, operation = sb.ManagedSSHService_ReconcileSSHIdentity_FullMethodName, "ssh.reconcile"
				}
				if controlplaneclient.SSHIdentityBrokerOperations()[operation] != method {
					t.Fatal("actual method registry mismatch")
				}
				binding := &sb.ManagedSSHExecutionBinding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("a", 64)}
				executeRequest := &sb.ExecuteSSHIdentityRequest{Binding: binding}
				readRequest := &sb.ReconcileSSHIdentityRequest{Binding: binding}
				fixture := &sshExecutionRPCFixture{receipt: managedssh.ExecutionReceipt{AttemptRef: "sxe_fixture", SnapshotDigest: strings.Repeat("b", 64), State: "FAILED", OwnerState: "FAILED", Recorded: true, Applied: true, Observation: managedssh.ExecutionObservation{Outcome: managedssh.ExecutionFailed}}}
				server := &Server{}
				WithSSHIdentityExecution(fixture)(server)
				want := codes.OK
				calls := 1
				selected := operation
				switch scenario {
				case "raw-metadata", "foreign-operation":
					want = codes.PermissionDenied
					calls = 0
					selected = "ssh.health.probe"
				case "unknown-request":
					want = codes.InvalidArgument
					calls = 0
					executeRequest.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
					readRequest.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
				case "unknown-binding":
					want = codes.InvalidArgument
					calls = 0
					binding.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
				case "unwired":
					want = codes.Unavailable
					calls = 0
					server.sshExecution = nil
				case "mixed-receipt":
					want = codes.Unavailable
					fixture.receipt.Applied = false
				}
				ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, caller))
				invoke := func(ctx context.Context, _ any) (any, error) {
					if read {
						return server.ReconcileSSHIdentity(ctx, readRequest)
					}
					return server.ExecuteSSHIdentity(ctx, executeRequest)
				}
				var err error
				if scenario == "raw-metadata" {
					_, err = invoke(ctx, nil)
				} else {
					authorizer, createErr := serviceidentity.NewTrustedCluster(transportprofile.TrustedCluster, secretBrokerSPIFFEID, []serviceidentity.Binding{{CallerSPIFFEID: caller, FullMethod: method, OperationID: selected, Permission: selected, ActorMode: serviceidentity.ServiceActor}})
					if createErr != nil {
						t.Fatal(createErr)
					}
					_, err = authorizer.UnaryServerInterceptor()(ctx, executeRequest, &grpcgo.UnaryServerInfo{FullMethod: method}, invoke)
				}
				if status.Code(err) != want || fixture.calls+fixture.reads != calls || read && fixture.calls != 0 || !read && fixture.reads != 0 {
					t.Fatalf("closed boundary mismatch code=%s calls=%d reads=%d", status.Code(err), fixture.calls, fixture.reads)
				}
			})
		}
	}
}
