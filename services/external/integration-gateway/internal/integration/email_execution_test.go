package integration

import (
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEmailExecutionClaimMapping(t *testing.T) {
	lease := &cp.WorkLease{Ref: "lease_fixture01", Fence: "fixture-fence", Generation: 7, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
	invocation := RequestFromInvocation(&cp.IntegrationInvocationClaim{InvocationRef: "inv_fixture01", Lease: lease}).EmailExecution
	probe := RequestFromTest(&cp.IntegrationConnectionTestClaim{TestRef: "test_fixture01", Lease: lease}).EmailExecution
	if !api.ValidExecutionBinding(invocation) || invocation.InvocationRef == nil || *invocation.InvocationRef != "inv_fixture01" || invocation.ConnectionTestRef != nil || !api.ValidExecutionBinding(probe) || probe.ConnectionTestRef == nil || *probe.ConnectionTestRef != "test_fixture01" || probe.InvocationRef != nil {
		t.Fatal("claim source mapping mismatch")
	}
	for _, b := range []*api.ExecutionBinding{invocation, probe} {
		if b.Lease.Ref != lease.Ref || b.Lease.Fence != lease.Fence || b.Lease.Generation != lease.Generation || !b.Lease.ExpiresAt.Equal(lease.ExpiresAt.AsTime()) {
			t.Fatal("claim lease mapping mismatch")
		}
	}
	if RequestFromInvocation(nil).EmailExecution != nil || RequestFromTest(nil).EmailExecution != nil {
		t.Fatal("absent claim acquired authority")
	}
	invalid := emailExecutionBinding("inv_fixture01", "test_fixture01", lease)
	if api.ValidExecutionBinding(invalid) {
		t.Fatal("ambiguous source accepted")
	}
}
