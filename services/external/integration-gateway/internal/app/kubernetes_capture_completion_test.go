package app

import (
	"context"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// kubernetesCapturePendingOwner scripts only capture publication metadata. Native execution,
// provider credentials and child selection are intentionally absent from this owner port.
// kubernetesCapturePendingOwner задаёт только capture publication metadata. Native execution,
// provider credentials и child selection в этом owner port отсутствуют.
type kubernetesCapturePendingOwner struct {
	*kubernetesCompletionOwner
	terminal                          *cp.ManagedKubernetesInvocationReceipt
	uncertain, neverReady, changePlan bool
}

// CompleteManagedKubernetesInvocation returns a sealed capture wait before the canonical artifact
// appears, optionally losing that acknowledgement. It never returns an effect or next-child grant.
// CompleteManagedKubernetesInvocation возвращает sealed capture wait до canonical artifact,
// при необходимости теряя этот acknowledgement. Effect либо next-child grant не выдаёт.
func (owner *kubernetesCapturePendingOwner) CompleteManagedKubernetesInvocation(ctx context.Context, request *cp.CompleteManagedKubernetesInvocationRequest, options ...grpc.CallOption) (*cp.CompleteManagedKubernetesInvocationResponse, error) {
	owner.completeErr = nil
	owner.response = proto.Clone(owner.terminal).(*cp.ManagedKubernetesInvocationReceipt)
	if owner.completes == 0 || owner.neverReady {
		owner.response = proto.Clone(owner.initial).(*cp.ManagedKubernetesInvocationReceipt)
		owner.response.CompletionPendingReason = cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_CAPTURE_PENDING
	}
	owner.after = proto.Clone(owner.response).(*cp.ManagedKubernetesInvocationReceipt)
	if owner.changePlan {
		owner.after.PlanRef = "plan_foreign"
	}
	if owner.uncertain && owner.completes == 0 {
		owner.completeErr = status.Error(codes.Unavailable, "synthetic capture completion acknowledgement lost")
	}
	return owner.kubernetesCompletionOwner.CompleteManagedKubernetesInvocation(ctx, request, options...)
}

// TestKubernetesExecutionWaitsOnlyForTypedCapture drives the actual finish/strict owner adapter
// through normal and lost-ACK publication, cancellation and changed-plan refusal. A nil adapter and
// broker make any accidental Execute or stream replay fail immediately instead of simulating success.
// TestKubernetesExecutionWaitsOnlyForTypedCapture проводит actual finish/strict owner adapter через
// обычную и lost-ACK publication, cancellation и отказ changed plan. Nil adapter и broker немедленно
// выявляют случайный Execute либо stream replay вместо имитации успеха.
func TestKubernetesExecutionWaitsOnlyForTypedCapture(t *testing.T) {
	for _, variant := range []string{"published", "lost acknowledgement", "cancelled wait", "foreign plan"} {
		t.Run(variant, func(t *testing.T) {
			base := kubernetesCompletionFixture(t)
			owner := &kubernetesCapturePendingOwner{kubernetesCompletionOwner: base, terminal: proto.Clone(base.after).(*cp.ManagedKubernetesInvocationReceipt)}
			budget := 3 * time.Second
			switch variant {
			case "lost acknowledgement":
				owner.uncertain = true
			case "cancelled wait":
				owner.neverReady = true
				budget = 25 * time.Millisecond
			case "foreign plan":
				owner.changePlan = true
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			err := (&kubernetesExecution{owner: owner}).finish(ctx, owner.binding)
			wantSuccess := variant == "published" || variant == "lost acknowledgement"
			if (err == nil) != wantSuccess {
				t.Fatal("capture metadata became false success or lost publication", err)
			}
			wantCompletes := 1
			if wantSuccess {
				wantCompletes = 2
			}
			if owner.completes != wantCompletes {
				t.Fatal("capture polling replayed an unknown effect or changed original plan")
			}
		})
	}
}
