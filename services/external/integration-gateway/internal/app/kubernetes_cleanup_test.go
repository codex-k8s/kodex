package app

import (
	"context"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesCleanupOwner extends the metadata fixture with the actual cancellation RPC, preserving original binding and a lost acknowledgement after commit.
// kubernetesCleanupOwner дополняет metadata fixture фактическим cancellation RPC, сохраняя original binding и lost acknowledgement после commit.
type kubernetesCleanupOwner struct {
	*kubernetesCompletionOwner
	cancels  int
	variant  string
	deadline time.Time
}

// CancelManagedKubernetesInvocation returns only controlled owner facts and verifies that business cancellation cannot abandon the bounded cleanup request.
// CancelManagedKubernetesInvocation возвращает только контролируемые owner facts и проверяет, что business cancellation не бросает bounded cleanup request.
func (owner *kubernetesCleanupOwner) CancelManagedKubernetesInvocation(ctx context.Context, request *cp.CancelManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.CancelManagedKubernetesInvocationResponse, error) {
	owner.cancels++
	deadline, bounded := ctx.Deadline()
	if ctx.Err() != nil || !bounded || request.Binding.Fence != owner.binding.Fence || request.Binding.Generation != owner.binding.Generation {
		owner.t.Fatal("owner cleanup lost its original binding or independent budget")
	}
	owner.deadline = deadline
	if owner.variant == "timeout" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	owner.after.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED
	owner.after.CompletionRecorded, owner.after.ReceiptRef, owner.after.ResultDigest = false, "", ""
	if owner.variant != "still-running" {
		owner.initial = owner.after
	}
	if owner.variant == "lost-ack" || owner.variant == "still-running" {
		return nil, status.Error(codes.Unavailable, "synthetic lost cancel acknowledgement")
	}
	return &cp.CancelManagedKubernetesInvocationResponse{Receipt: proto.Clone(owner.after).(*cp.ManagedKubernetesInvocationReceipt)}, nil
}

// kubernetesCleanupBroker models only the final bounded cancellation/join RPC; no effect, output or credential route is implemented by this fixture.
// kubernetesCleanupBroker моделирует только final bounded cancellation/join RPC; effect, output и credential routes в fixture отсутствуют.
type kubernetesCleanupBroker struct {
	sb.ManagedKubernetesServiceClient
	owner *kubernetesCleanupOwner
	calls int
}

// CancelManagedKubernetesOperation observes owner closure before accepting broker cleanup and cannot expand the shared remaining budget.
// CancelManagedKubernetesOperation проверяет owner closure до broker cleanup и не расширяет общий оставшийся бюджет.
func (broker *kubernetesCleanupBroker) CancelManagedKubernetesOperation(ctx context.Context, request *sb.CancelManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.CancelManagedKubernetesOperationResponse, error) {
	broker.calls++
	deadline, ok := ctx.Deadline()
	if !ok || deadline.After(broker.owner.deadline) || ctx.Err() != nil || broker.owner.cancels != 1 || broker.owner.initial.State == cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING || !proto.Equal(request.Binding, broker.owner.binding) {
		broker.owner.t.Fatal("broker cleanup preceded owner closure or reset the deadline")
	}
	return &sb.CancelManagedKubernetesOperationResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_CANCELED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_UNKNOWN, TransportJoined: broker.owner.variant != "unjoined"}, nil
}

// TestKubernetesActivityCleanupComposesExactOwnerAndBroker exercises actual owner adapters and stream consumer after business cancellation, including lost ACK, expired total budget and an explicitly unjoined transport.
// TestKubernetesActivityCleanupComposesExactOwnerAndBroker исполняет actual owner adapters и stream consumer после business cancellation, включая lost ACK, expired total budget и явно unjoined transport.
func TestKubernetesActivityCleanupComposesExactOwnerAndBroker(t *testing.T) {
	for _, variant := range []string{"closed", "lost-ack", "still-running", "timeout", "unjoined"} {
		t.Run(variant, func(t *testing.T) {
			owner := &kubernetesCleanupOwner{kubernetesCompletionOwner: kubernetesCompletionFixture(t), variant: variant}
			broker := &kubernetesCleanupBroker{owner: owner}
			descriptor := &sb.ManagedKubernetesStreamDescriptor{InvocationRef: owner.binding.InvocationRef, StreamRef: "stream_fixture", Generation: owner.binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), MaximumFrameBytes: 1024, MaximumTotalBytes: 4096, OutputChannels: []sb.ManagedKubernetesChannel{sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_STDOUT}}
			stream, err := integration.NewKubernetesStreamConsumer(owner.binding, descriptor, "kubernetes.logs", broker, func(context.Context, *sb.ManagedKubernetesInvocationBinding, *sb.ManagedKubernetesStreamDescriptor, integration.KubernetesStreamOutput) error {
				t.Fatal("cleanup unexpectedly delivered output")
				return nil
			}, func(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding, ref string) error {
				if ref != descriptor.StreamRef {
					t.Fatal("cleanup selected a foreign stream")
				}
				return closeKubernetesActivity(ctx, binding, owner)
			})
			if err != nil {
				t.Fatal(err)
			}
			business, stop := context.WithCancel(t.Context())
			stop()
			err = cleanupKubernetesActivity(business, owner.binding, owner, stream, 100*time.Millisecond)
			wantSuccess := variant == "closed" || variant == "lost-ack"
			wantBroker := variant == "closed" || variant == "lost-ack" || variant == "unjoined"
			if (err == nil) != wantSuccess || owner.cancels != 1 || (broker.calls == 1) != wantBroker {
				t.Fatal("cleanup replayed cancellation or claimed an unproven join")
			}
			wantReads := 0
			if variant == "lost-ack" || variant == "still-running" {
				wantReads = 1
			}
			if owner.reads != wantReads {
				t.Fatal("cancellation recovery performed an unexpected read or retry")
			}
		})
	}
}
