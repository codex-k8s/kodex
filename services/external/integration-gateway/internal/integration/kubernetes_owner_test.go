package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// kubernetesOwnerFixture substitutes only owner RPC responses, retaining exact request bindings and the accepted completion after a lost acknowledgement.
// kubernetesOwnerFixture подменяет только ответы owner RPC, сохраняя точные request bindings и принятое completion после потерянного acknowledgement.
type kubernetesOwnerFixture struct {
	cp.ManagedKubernetesOwnerServiceClient
	read                                 *cp.GetManagedKubernetesInvocationResponse
	complete                             *cp.CompleteManagedKubernetesInvocationResponse
	renew                                *cp.RenewManagedKubernetesInvocationResponse
	readErr, completeErr, renewErr       error
	readCalls, completeCalls, renewCalls int
	readRequest                          *cp.GetManagedKubernetesInvocationRequest
	completeRequest                      *cp.CompleteManagedKubernetesInvocationRequest
	renewRequest                         *cp.RenewManagedKubernetesInvocationRequest
	afterCall                            func()
}

// GetManagedKubernetesInvocation records a metadata read without triggering any broker or native operation.
// GetManagedKubernetesInvocation фиксирует metadata read без вызова broker либо native operation.
func (fixture *kubernetesOwnerFixture) GetManagedKubernetesInvocation(_ context.Context, request *cp.GetManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.GetManagedKubernetesInvocationResponse, error) {
	fixture.readCalls++
	fixture.readRequest = request
	if fixture.afterCall != nil {
		fixture.afterCall()
	}
	return fixture.read, fixture.readErr
}

// CompleteManagedKubernetesInvocation models an owner commit that can survive a missing transport acknowledgement.
// CompleteManagedKubernetesInvocation моделирует owner commit, сохраняющийся при потерянном transport acknowledgement.
func (fixture *kubernetesOwnerFixture) CompleteManagedKubernetesInvocation(_ context.Context, request *cp.CompleteManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.CompleteManagedKubernetesInvocationResponse, error) {
	fixture.completeCalls++
	fixture.completeRequest = request
	if fixture.complete != nil {
		fixture.read = &cp.GetManagedKubernetesInvocationResponse{Receipt: fixture.complete.Receipt}
	}
	if fixture.afterCall != nil {
		fixture.afterCall()
	}
	return fixture.complete, fixture.completeErr
}

// RenewManagedKubernetesInvocation retains the original claim tuple and never creates an alternative lease locally.
// RenewManagedKubernetesInvocation сохраняет исходный claim tuple и не создаёт альтернативную lease локально.
func (fixture *kubernetesOwnerFixture) RenewManagedKubernetesInvocation(_ context.Context, request *cp.RenewManagedKubernetesInvocationRequest, _ ...grpc.CallOption) (*cp.RenewManagedKubernetesInvocationResponse, error) {
	fixture.renewCalls++
	fixture.renewRequest = request
	if fixture.afterCall != nil {
		fixture.afterCall()
	}
	return fixture.renew, fixture.renewErr
}

// kubernetesOwnerReceipts builds synthetic metadata bound to all original claim fields, including the private fence digest.
// kubernetesOwnerReceipts создаёт synthetic metadata, связанные со всеми исходными claim fields, включая private fence digest.
func kubernetesOwnerReceipts(t *testing.T) (*sb.ManagedKubernetesInvocationBinding, *cp.ManagedKubernetesInvocationReceipt, *cp.ManagedKubernetesInvocationReceipt) {
	t.Helper()
	binding := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_fixture", InvocationRef: "inv_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 7, InputDigest: strings.Repeat("a", 64)}
	digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	running := &cp.ManagedKubernetesInvocationReceipt{CompletionPendingReason: cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_NONE, InvocationRef: binding.InvocationRef, Generation: binding.Generation, InputDigest: binding.InputDigest, BindingDigest: digest, State: cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING, PlanRef: "plan_fixture", PlanRevision: 2, PlanDigest: strings.Repeat("b", 64), SnapshotDigest: strings.Repeat("c", 64)}
	completed := proto.Clone(running).(*cp.ManagedKubernetesInvocationReceipt)
	completed.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_SUCCEEDED
	completed.CompletionRecorded = true
	completed.ReceiptRef = "receipt_fixture"
	completed.ResultDigest = strings.Repeat("d", 64)
	return binding, running, completed
}

// TestKubernetesOwnerLostCompletionReadsWithoutReplay proves exact plan submission and original-binding readback; completion transport failure is never accepted as terminal success or retried by the adapter.
// TestKubernetesOwnerLostCompletionReadsWithoutReplay доказывает exact plan submission и readback по исходному binding; transport failure не считается terminal success и не повторяется adapter.
func TestKubernetesOwnerLostCompletionReadsWithoutReplay(t *testing.T) {
	binding, running, completed := kubernetesOwnerReceipts(t)
	fixture := &kubernetesOwnerFixture{read: &cp.GetManagedKubernetesInvocationResponse{Receipt: running}, complete: &cp.CompleteManagedKubernetesInvocationResponse{Receipt: completed}, completeErr: errors.New("synthetic lost acknowledgement")}
	observed, err := ReadKubernetesInvocation(t.Context(), binding, fixture)
	if err != nil || observed == running || !proto.Equal(observed, running) {
		t.Fatal("exact independent metadata read failed")
	}
	if result, err := CompleteKubernetesInvocation(t.Context(), binding, observed, fixture); err == nil || result != nil {
		t.Fatal("uncertain completion became success")
	}
	result, err := ReadKubernetesInvocation(t.Context(), binding, fixture)
	if err != nil || !result.CompletionRecorded || !proto.Equal(result, completed) || fixture.completeCalls != 1 || fixture.readCalls != 2 {
		t.Fatal("lost acknowledgement did not reconcile without replay")
	}
	wire, _ := kubernetesOwnerBinding(binding)
	if !proto.Equal(fixture.completeRequest.Binding, wire) || !proto.Equal(fixture.readRequest.Binding, wire) || fixture.completeRequest.PlanRef != running.PlanRef || fixture.completeRequest.PlanRevision != running.PlanRevision || fixture.completeRequest.PlanDigest != running.PlanDigest {
		t.Fatal("original claim or plan pins changed")
	}
	if _, err := CompleteKubernetesInvocation(t.Context(), binding, result, fixture); err == nil || fixture.completeCalls != 1 {
		t.Fatal("recorded completion was resubmitted")
	}
}

// TestKubernetesOwnerRejectsMalformedReceipt keeps unknown, foreign, partial and unrecorded-success responses outside semantic use.
// TestKubernetesOwnerRejectsMalformedReceipt исключает семантическое использование unknown, foreign, partial и unrecorded-success ответов.
func TestKubernetesOwnerRejectsMalformedReceipt(t *testing.T) {
	binding, _, completed := kubernetesOwnerReceipts(t)
	for _, mutate := range []func(*cp.ManagedKubernetesInvocationReceipt){
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.InvocationRef = "foreign" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.Generation++ },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.InputDigest = strings.Repeat("e", 64) },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.BindingDigest = strings.Repeat("e", 64) },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.State = cp.ManagedKubernetesInvocationState(99) },
		func(r *cp.ManagedKubernetesInvocationReceipt) {
			r.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING
		},
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.PlanRef = "" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.PlanRevision = 0 },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.PlanDigest = "" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.SnapshotDigest = "" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.ReceiptRef = "" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.ResultDigest = "" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.CompletionRecorded = false },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.ProtoReflect().SetUnknown([]byte{0x78, 1}) },
	} {
		r := proto.Clone(completed).(*cp.ManagedKubernetesInvocationReceipt)
		mutate(r)
		fixture := &kubernetesOwnerFixture{read: &cp.GetManagedKubernetesInvocationResponse{Receipt: r}}
		if result, err := ReadKubernetesInvocation(t.Context(), binding, fixture); err == nil || result != nil {
			t.Fatal("malformed receipt accepted")
		}
	}
}

// TestKubernetesOwnerCompletionRequiresSamePlan rejects structurally valid acknowledgements for another plan or snapshot and accepts owner-recorded business failure without inventing success.
// TestKubernetesOwnerCompletionRequiresSamePlan отклоняет структурно верные acknowledgements другого plan/snapshot и принимает owner-recorded business failure без выдуманного успеха.
func TestKubernetesOwnerCompletionRequiresSamePlan(t *testing.T) {
	binding, running, completed := kubernetesOwnerReceipts(t)
	for _, mutate := range []func(*cp.ManagedKubernetesInvocationReceipt){
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.PlanRef = "another_plan" },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.PlanRevision++ },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.PlanDigest = strings.Repeat("e", 64) },
		func(r *cp.ManagedKubernetesInvocationReceipt) { r.SnapshotDigest = strings.Repeat("e", 64) },
	} {
		r := proto.Clone(completed).(*cp.ManagedKubernetesInvocationReceipt)
		mutate(r)
		fixture := &kubernetesOwnerFixture{complete: &cp.CompleteManagedKubernetesInvocationResponse{Receipt: r}}
		if result, err := CompleteKubernetesInvocation(t.Context(), binding, running, fixture); err == nil || result != nil {
			t.Fatal("another completion plan accepted")
		}
	}
	running.PlanRef = ""
	running.PlanRevision = 0
	running.PlanDigest = ""
	running.SnapshotDigest = ""
	completed = proto.Clone(running).(*cp.ManagedKubernetesInvocationReceipt)
	completed.CompletionRecorded = true
	completed.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED
	completed.ReceiptRef = "receipt_fixture"
	completed.ResultDigest = strings.Repeat("d", 64)
	fixture := &kubernetesOwnerFixture{complete: &cp.CompleteManagedKubernetesInvocationResponse{Receipt: completed}}
	if result, err := CompleteKubernetesInvocation(t.Context(), binding, running, fixture); err != nil || result.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED {
		t.Fatal("recorded no-plan rejection lost")
	}
}

// TestKubernetesOwnerRenewRetainsExactLease rejects changed or malformed lease acknowledgements, including expired nested timestamps and unknown fields.
// TestKubernetesOwnerRenewRetainsExactLease отклоняет изменённые либо malformed lease acknowledgements, включая истёкшие nested timestamps и unknown fields.
func TestKubernetesOwnerRenewRetainsExactLease(t *testing.T) {
	binding, _, _ := kubernetesOwnerReceipts(t)
	current := time.Now().Add(15 * time.Second)
	for index, mutate := range []func(*cp.RenewManagedKubernetesInvocationResponse){
		func(*cp.RenewManagedKubernetesInvocationResponse) {},
		func(r *cp.RenewManagedKubernetesInvocationResponse) { r.LeaseRef = "foreign" },
		func(r *cp.RenewManagedKubernetesInvocationResponse) { r.Generation++ },
		func(r *cp.RenewManagedKubernetesInvocationResponse) { r.ExpiresAt = nil },
		func(r *cp.RenewManagedKubernetesInvocationResponse) {
			r.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		},
		func(r *cp.RenewManagedKubernetesInvocationResponse) {
			r.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
		},
		func(r *cp.RenewManagedKubernetesInvocationResponse) { r.ProtoReflect().SetUnknown([]byte{0x78, 1}) },
		func(r *cp.RenewManagedKubernetesInvocationResponse) {
			r.ExpiresAt.ProtoReflect().SetUnknown([]byte{0x78, 1})
		},
	} {
		response := &cp.RenewManagedKubernetesInvocationResponse{LeaseRef: binding.LeaseRef, Generation: binding.Generation, ExpiresAt: timestamppb.New(time.Now().Add(25 * time.Second))}
		mutate(response)
		fixture := &kubernetesOwnerFixture{renew: response}
		until, err := RenewKubernetesInvocation(t.Context(), binding, current, fixture)
		if index == 0 {
			if err != nil || !until.Equal(response.ExpiresAt.AsTime()) || fixture.renewRequest.Binding.Fence != binding.Fence {
				t.Fatal("valid exact renewal failed")
			}
		} else if err == nil || !until.IsZero() {
			t.Fatal("invalid renewal created local credit")
		}
	}
}

// TestKubernetesOwnerCanceledAndInvalidBindingNeverGainCredit rejects pre-dispatch invalid inputs and successful-looking replies delivered after cancellation.
// TestKubernetesOwnerCanceledAndInvalidBindingNeverGainCredit отклоняет неверные inputs до отправки и успешные на вид ответы после отмены.
func TestKubernetesOwnerCanceledAndInvalidBindingNeverGainCredit(t *testing.T) {
	binding, running, completed := kubernetesOwnerReceipts(t)
	ctx, cancel := context.WithCancel(t.Context())
	fixture := &kubernetesOwnerFixture{read: &cp.GetManagedKubernetesInvocationResponse{Receipt: completed}, afterCall: cancel}
	if result, err := ReadKubernetesInvocation(ctx, binding, fixture); err == nil || result != nil {
		t.Fatal("cancelled response gained credit")
	}
	fixture = &kubernetesOwnerFixture{}
	if _, err := ReadKubernetesInvocation(ctx, binding, fixture); err == nil || fixture.readCalls != 0 {
		t.Fatal("cancelled call dispatched")
	}
	for _, mutate := range []func(*sb.ManagedKubernetesInvocationBinding){func(b *sb.ManagedKubernetesInvocationBinding) { b.Fence = "" }, func(b *sb.ManagedKubernetesInvocationBinding) { b.ConnectionTestRef = "test_fixture" }, func(b *sb.ManagedKubernetesInvocationBinding) { b.ProtoReflect().SetUnknown([]byte{0x78, 1}) }} {
		b := proto.Clone(binding).(*sb.ManagedKubernetesInvocationBinding)
		mutate(b)
		if _, err := ReadKubernetesInvocation(t.Context(), b, fixture); err == nil {
			t.Fatal("invalid binding read")
		}
		if _, err := CompleteKubernetesInvocation(t.Context(), b, running, fixture); err == nil {
			t.Fatal("invalid binding completed")
		}
		if _, err := RenewKubernetesInvocation(t.Context(), b, time.Now().Add(time.Second), fixture); err == nil {
			t.Fatal("invalid binding renewed")
		}
	}
	if fixture.readCalls+fixture.completeCalls+fixture.renewCalls != 0 {
		t.Fatal("rejected inputs reached owner")
	}
}

// TestKubernetesOwnerCompletionDistinguishesPendingAndClosed preserves owner cancellation racing with completion without promoting unrecorded metadata to success.
// TestKubernetesOwnerCompletionDistinguishesPendingAndClosed сохраняет owner cancellation одновременно с completion, не превращая unrecorded metadata в успех.
func TestKubernetesOwnerCompletionDistinguishesPendingAndClosed(t *testing.T) {
	binding, observed, _ := kubernetesOwnerReceipts(t)
	for _, state := range []cp.ManagedKubernetesInvocationState{
		cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING,
		cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED,
		cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_FAILED,
		cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_UNKNOWN_OUTCOME,
	} {
		receipt := proto.Clone(observed).(*cp.ManagedKubernetesInvocationReceipt)
		receipt.State = state
		fixture := &kubernetesOwnerFixture{complete: &cp.CompleteManagedKubernetesInvocationResponse{Receipt: receipt}}
		result, err := CompleteKubernetesInvocation(t.Context(), binding, observed, fixture)
		if err != nil || result.State != state || result.CompletionRecorded || result.ReceiptRef != "" || result.ResultDigest != "" {
			t.Fatal("known pending or closed metadata was lost or promoted to success")
		}
	}
	noPlan := proto.Clone(observed).(*cp.ManagedKubernetesInvocationReceipt)
	noPlan.PlanRef, noPlan.PlanDigest, noPlan.SnapshotDigest, noPlan.PlanRevision = "", "", "", 0
	fixture := &kubernetesOwnerFixture{complete: &cp.CompleteManagedKubernetesInvocationResponse{Receipt: noPlan}}
	if result, err := CompleteKubernetesInvocation(t.Context(), binding, noPlan, fixture); err == nil || result != nil {
		t.Fatal("absent plan became permission to advance a child")
	}
}

// TestKubernetesOwnerCapturePendingIsClosedVariant refuses absent/future reasons and any attempt
// to combine capture wait with a terminal state, receipt or unbound plan. Only the exact live
// metadata variant may reach the capture-only completion poller.
// TestKubernetesOwnerCapturePendingIsClosedVariant отклоняет absent/future reasons и сочетание
// capture wait с terminal state, receipt либо unbound plan. Только exact live metadata variant
// передаётся capture-only completion poller.
func TestKubernetesOwnerCapturePendingIsClosedVariant(t *testing.T) {
	binding, running, _ := kubernetesOwnerReceipts(t)
	for _, variant := range []string{"capture pending", "absent", "future", "terminal", "recorded", "no plan", "receipt", "digest"} {
		t.Run(variant, func(t *testing.T) {
			receipt := proto.Clone(running).(*cp.ManagedKubernetesInvocationReceipt)
			receipt.CompletionPendingReason = cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_CAPTURE_PENDING
			switch variant {
			case "absent":
				receipt.CompletionPendingReason = cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_UNSPECIFIED
			case "future":
				receipt.CompletionPendingReason = cp.ManagedKubernetesCompletionPendingReason(99)
			case "terminal":
				receipt.State = cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_CANCELLED
			case "recorded":
				receipt.CompletionRecorded = true
			case "no plan":
				receipt.PlanRef, receipt.PlanRevision, receipt.PlanDigest, receipt.SnapshotDigest = "", 0, "", ""
			case "receipt":
				receipt.ReceiptRef = "receipt_fixture"
			case "digest":
				receipt.ResultDigest = strings.Repeat("f", 64)
			}
			fixture := &kubernetesOwnerFixture{read: &cp.GetManagedKubernetesInvocationResponse{Receipt: receipt}}
			got, err := ReadKubernetesInvocation(t.Context(), binding, fixture)
			if (err == nil && got != nil) != (variant == "capture pending") {
				t.Fatal("invalid capture completion metadata reached semantic use")
			}
		})
	}
}
