package app

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// kubernetesCompletionOwner models only protected owner RPC replies, with separate call counts to detect accidental completion replay; it does not emulate PostgreSQL authorization or target execution.
// kubernetesCompletionOwner моделирует лишь ответы protected owner RPC, считая calls для обнаружения повтора completion; PostgreSQL authorization и target execution не имитируются.
type kubernetesCompletionOwner struct {
	cp.ManagedKubernetesOwnerServiceClient
	initial, after, response *cp.ManagedKubernetesInvocationReceipt
	completeErr              error
	reads, completes         int
	binding                  *sb.ManagedKubernetesInvocationBinding
	t                        *testing.T
}

// GetManagedKubernetesInvocation checks that even post-failure readback retains the original private binding before returning an owned metadata copy.
// GetManagedKubernetesInvocation проверяет original private binding даже при post-failure readback и возвращает отдельную metadata copy.
func (owner *kubernetesCompletionOwner) GetManagedKubernetesInvocation(_ context.Context, request *cp.GetManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.GetManagedKubernetesInvocationResponse, error) {
	if request.Binding.Fence != owner.binding.Fence || request.Binding.Generation != owner.binding.Generation || request.Binding.InvocationRef != owner.binding.InvocationRef || request.Binding.WorkloadInstance != owner.binding.WorkloadInstance {
		owner.t.Fatal("completion read changed original binding")
	}
	owner.reads++
	receipt := owner.initial
	if owner.reads > 1 {
		receipt = owner.after
	}
	return &cp.GetManagedKubernetesInvocationResponse{Receipt: proto.Clone(receipt).(*cp.ManagedKubernetesInvocationReceipt)}, nil
}

// CompleteManagedKubernetesInvocation records exactly one original-plan submission and can lose the acknowledgement after storing the terminal state.
// CompleteManagedKubernetesInvocation фиксирует ровно одну original-plan submission и может потерять acknowledgement после сохранения terminal state.
func (owner *kubernetesCompletionOwner) CompleteManagedKubernetesInvocation(_ context.Context, request *cp.CompleteManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.CompleteManagedKubernetesInvocationResponse, error) {
	owner.completes++
	if request.Binding.Fence != owner.binding.Fence || request.PlanRef != owner.initial.PlanRef || request.PlanRevision != owner.initial.PlanRevision || request.PlanDigest != owner.initial.PlanDigest {
		owner.t.Fatal("completion changed original claim or plan")
	}
	if owner.completeErr != nil {
		return nil, owner.completeErr
	}
	return &cp.CompleteManagedKubernetesInvocationResponse{Receipt: proto.Clone(owner.response).(*cp.ManagedKubernetesInvocationReceipt)}, nil
}

// kubernetesCompletionFixture creates complete synthetic plan/binding pins for the real strict owner adapters rather than skipping their receipt validation.
// kubernetesCompletionFixture создаёт полные synthetic plan/binding pins для настоящих strict owner adapters, не пропуская проверку квитанций.
func kubernetesCompletionFixture(t *testing.T) *kubernetesCompletionOwner {
	t.Helper()
	input := kubernetesRenewalInput(t, 20*time.Second)
	b := input.binding
	digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: b.WorkloadInstance, InvocationRef: b.InvocationRef, LeaseRef: b.LeaseRef, Fence: b.Fence, Generation: b.Generation, InputDigest: b.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	initial := &cp.ManagedKubernetesInvocationReceipt{CompletionPendingReason: cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_NONE, InvocationRef: b.InvocationRef, Generation: b.Generation, InputDigest: b.InputDigest, BindingDigest: digest, PlanRef: "plan_fixture", PlanRevision: 1, PlanDigest: strings.Repeat("b", 64), SnapshotDigest: strings.Repeat("c", 64), State: cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING}
	terminal := proto.Clone(initial).(*cp.ManagedKubernetesInvocationReceipt)
	terminal.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_SUCCEEDED
	terminal.CompletionRecorded, terminal.ReceiptRef, terminal.ResultDigest = true, "receipt_fixture", strings.Repeat("d", 64)
	return &kubernetesCompletionOwner{initial: initial, after: terminal, response: terminal, binding: b, t: t}
}

// TestKubernetesCompletionLostAcknowledgementReadsWithoutReplay distinguishes recorded success, closed cancellation, unknown live completion and foreign plan readback after one uncertain RPC.
// TestKubernetesCompletionLostAcknowledgementReadsWithoutReplay различает recorded success, closed cancellation, unknown live completion и foreign plan readback после одного uncertain RPC.
func TestKubernetesCompletionLostAcknowledgementReadsWithoutReplay(t *testing.T) {
	for _, variant := range []string{"recorded", "cancelled", "still-running", "foreign-plan", "foreign-binding"} {
		t.Run(variant, func(t *testing.T) {
			owner := kubernetesCompletionFixture(t)
			owner.completeErr = status.Error(codes.Unavailable, "synthetic lost acknowledgement")
			switch variant {
			case "cancelled":
				owner.after.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED
				owner.after.CompletionRecorded, owner.after.ReceiptRef, owner.after.ResultDigest = false, "", ""
			case "still-running":
				owner.after = proto.Clone(owner.initial).(*cp.ManagedKubernetesInvocationReceipt)
			case "foreign-plan":
				owner.after.PlanRef = "plan_foreign"
			case "foreign-binding":
				owner.after.Generation++
			}
			receipt, err := settleKubernetesActivity(t.Context(), owner.binding, owner)
			wantClosed := variant == "recorded" || variant == "cancelled"
			if (err == nil) != wantClosed || wantClosed && !proto.Equal(receipt, owner.after) || !wantClosed && receipt != nil || owner.reads != 2 || owner.completes != 1 {
				t.Fatal("uncertain completion was replayed or incorrectly resolved")
			}
		})
	}
}

// TestKubernetesCompletionRequiresPositivePendingReply permits a known remaining plan only from validated Complete, while malformed responses and terminal initial reads never initiate another completion.
// TestKubernetesCompletionRequiresPositivePendingReply допускает known remaining plan лишь из validated Complete; malformed responses и terminal initial reads не запускают ещё один completion.
func TestKubernetesCompletionRequiresPositivePendingReply(t *testing.T) {
	for _, variant := range []string{"pending", "malformed", "already-closed"} {
		t.Run(variant, func(t *testing.T) {
			owner := kubernetesCompletionFixture(t)
			switch variant {
			case "pending":
				owner.response = proto.Clone(owner.initial).(*cp.ManagedKubernetesInvocationReceipt)
			case "malformed":
				owner.response = proto.Clone(owner.after).(*cp.ManagedKubernetesInvocationReceipt)
				owner.response.PlanDigest = strings.Repeat("e", 64)
			case "already-closed":
				owner.initial = owner.after
			}
			receipt, err := settleKubernetesActivity(t.Context(), owner.binding, owner)
			wantCompletions := 1
			if variant == "already-closed" {
				wantCompletions = 0
			}
			if owner.reads != 1 || owner.completes != wantCompletions || (err != nil) != (variant == "malformed") || err == nil && receipt == nil {
				t.Fatal("completion failed to preserve terminal or protocol boundaries")
			}
		})
	}
}
