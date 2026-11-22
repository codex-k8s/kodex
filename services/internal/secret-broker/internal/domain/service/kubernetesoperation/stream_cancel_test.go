package kubernetesoperation

import (
	"errors"
	"strings"
	"testing"
	"time"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
)

// TestStreamCancelRequiresClosedOwnerProof rejects foreign or active owner graphs before touching native transport.
// TestStreamCancelRequiresClosedOwnerProof отвергает чужие либо active owner graphs до обращения к native transport.
func TestStreamCancelRequiresClosedOwnerProof(t *testing.T) {
	for _, variant := range []string{"active", "invocation", "stream", "generation", "binding", "snapshot", "state", "owner-error", "foreign-binding", "foreign-stream", "unwired"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, native, binding := streamOperationsFixture(t)
			opened, err := service.Execute(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			origin := native.transport.handshake.Origin
			owner.cleanup = &targetrepo.StreamCleanupReceipt{InvocationRef: binding.InvocationRef, StreamRef: origin.StreamRef, Generation: binding.Generation, BindingDigest: origin.BindingDigest, SnapshotDigest: origin.SnapshotDigest, StreamState: "ACTIVE", ExecutionClosed: true}
			ref := opened.Stream.Ref
			switch variant {
			case "active":
				owner.cleanup.ExecutionClosed = false
			case "invocation":
				owner.cleanup.InvocationRef = "inv_foreign"
			case "stream":
				owner.cleanup.StreamRef = "stream_foreign"
			case "generation":
				owner.cleanup.Generation++
			case "binding":
				owner.cleanup.BindingDigest = strings.Repeat("f", 64)
			case "snapshot":
				owner.cleanup.SnapshotDigest = strings.Repeat("f", 64)
			case "state":
				owner.cleanup.StreamState = "arbitrary"
			case "owner-error":
				owner.cleanupErr = errors.New("synthetic owner unavailable")
			case "foreign-binding":
				binding.Fence = "foreign-fence"
			case "foreign-stream":
				ref = "stream_foreign"
			case "unwired":
				service.owner = struct{ targetrepo.StreamPlanOwner }{owner}
			}
			result, joined, err := service.Cancel(t.Context(), binding, ref)
			if err == nil || joined || result.State != "" {
				t.Fatal("unproven closure manufactured successful cancellation")
			}
			select {
			case <-native.transport.closed:
				t.Fatal("unproven owner closure stopped native stream")
			default:
			}
			if (variant == "foreign-binding" || variant == "foreign-stream" || variant == "unwired") && owner.cleanupCalls != 0 {
				t.Fatal("unowned cleanup reached owner read")
			}
		})
	}
}

// TestStreamCancelAcceptsDiscardedLateReceipt preserves native join without reviving a terminal business invocation.
// TestStreamCancelAcceptsDiscardedLateReceipt сохраняет native join, не возрождая terminal business invocation.
func TestStreamCancelAcceptsDiscardedLateReceipt(t *testing.T) {
	for _, variant := range []string{"exact", "wrong-state", "wrong-result", "output-credit", "wrong-submission"} {
		t.Run(variant, func(t *testing.T) {
			service, owner, native, binding := streamOperationsFixture(t)
			owner.activeOwnerFixture.finalState = "CANCELED"
			owner.completionReceipt = func(receipt targetrepo.EffectReceipt) targetrepo.EffectReceipt {
				receipt.Applied, receipt.State, receipt.ResultDigest = false, "CANCELLED", resourceResultDigest([]byte("{}"))
				switch variant {
				case "wrong-state":
					receipt.State = "SUCCEEDED"
				case "wrong-result":
					receipt.ResultDigest = strings.Repeat("f", 64)
				case "output-credit":
					receipt.AuthorizedUntil = time.Now().Add(time.Second)
				case "wrong-submission":
					receipt.SubmissionDigest = strings.Repeat("f", 64)
				}
				return receipt
			}
			opened, err := service.Execute(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			origin := native.transport.handshake.Origin
			owner.cleanup = &targetrepo.StreamCleanupReceipt{InvocationRef: binding.InvocationRef, StreamRef: origin.StreamRef, Generation: binding.Generation, BindingDigest: origin.BindingDigest, SnapshotDigest: origin.SnapshotDigest, StreamState: "CANCELED", ExecutionClosed: true}
			result, joined, err := service.Cancel(t.Context(), binding, opened.Stream.Ref)
			if !joined || (err == nil) != (variant == "exact") {
				t.Fatal("late receipt acknowledgement or positive join changed", err)
			}
			if result.State != "CANCELED" || result.RemoteOutcome != "UNKNOWN" || len(result.Output) != 0 || len(owner.completions) != 1 || native.opens != 1 {
				t.Fatal("cleanup revived business state or fabricated remote result")
			}
			closing := <-owner.closed
			if !closing.TransportJoined || closing.TerminalReceipt.ExitCode != nil {
				t.Fatal("cleanup invented remote process exit")
			}
		})
	}
}
