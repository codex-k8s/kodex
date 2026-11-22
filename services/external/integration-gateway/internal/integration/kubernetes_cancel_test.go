package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// kubernetesCancelOwner intercepts the single protected cancellation RPC without implementing native transport, credentials or a cancellation state machine.
// kubernetesCancelOwner перехватывает единственный protected cancellation RPC без native transport, credentials либо cancellation state machine.
type kubernetesCancelOwner struct {
	cp.ManagedKubernetesOwnerServiceClient
	call func(context.Context, *cp.CancelManagedKubernetesInvocationRequest) (*cp.CancelManagedKubernetesInvocationResponse, error)
}

// CancelManagedKubernetesInvocation forwards the original request to the controlled boundary for deadline, cardinality and binding assertions.
// CancelManagedKubernetesInvocation передаёт original request контролируемой boundary для проверки deadline, cardinality и binding.
func (owner *kubernetesCancelOwner) CancelManagedKubernetesInvocation(ctx context.Context, request *cp.CancelManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.CancelManagedKubernetesInvocationResponse, error) {
	return owner.call(ctx, request)
}

// TestKubernetesOwnerCancelRequiresExactTerminalMetadata covers lost ACK, revoked old execution, already-recorded truth and malformed/foreign responses without retry or invented remote join.
// TestKubernetesOwnerCancelRequiresExactTerminalMetadata проверяет lost ACK, revoked old execution, already-recorded truth и malformed/foreign responses без retry либо выдуманного remote join.
func TestKubernetesOwnerCancelRequiresExactTerminalMetadata(t *testing.T) {
	for _, variant := range []string{"cancelled", "unknown", "recorded", "running", "foreign", "unknown-wire", "lost-ack", "late-ack"} {
		t.Run(variant, func(t *testing.T) {
			binding, running, completed := kubernetesOwnerReceipts(t)
			response := &cp.CancelManagedKubernetesInvocationResponse{Receipt: proto.Clone(running).(*cp.ManagedKubernetesInvocationReceipt)}
			response.Receipt.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED
			switch variant {
			case "unknown":
				response.Receipt.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_UNKNOWN_OUTCOME
			case "recorded":
				response.Receipt = completed
			case "running":
				response.Receipt = running
			case "foreign":
				response.Receipt.Generation++
			case "unknown-wire":
				response.ProtoReflect().SetUnknown([]byte{0x10, 0x01})
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			owner := &kubernetesCancelOwner{call: func(work context.Context, request *cp.CancelManagedKubernetesInvocationRequest) (*cp.CancelManagedKubernetesInvocationResponse, error) {
				calls++
				wire, _ := kubernetesOwnerBinding(binding)
				deadline, ok := work.Deadline()
				if !proto.Equal(wire, request.Binding) || !ok || !deadline.After(time.Now()) || time.Until(deadline) > kubernetesOwnerTimeout {
					t.Fatal("cancellation changed original binding or lost its bound")
				}
				if variant == "lost-ack" {
					return nil, errors.New("synthetic lost cancellation acknowledgement")
				}
				if variant == "late-ack" {
					cancel()
				}
				return response, nil
			}}
			receipt, err := CancelKubernetesInvocation(ctx, binding, owner)
			valid := variant == "cancelled" || variant == "unknown" || variant == "recorded"
			if calls != 1 || (err == nil) != valid || valid && (receipt == response.Receipt || !proto.Equal(receipt, response.Receipt)) || !valid && receipt != nil {
				t.Fatal("cancellation replayed or accepted unproven terminal state")
			}
		})
	}
}
