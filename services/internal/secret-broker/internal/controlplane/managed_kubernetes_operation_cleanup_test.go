package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"google.golang.org/protobuf/proto"
)

// cleanupAdapterFixture provides closure metadata only; the real owner must prove closure in its persisted graph.
// cleanupAdapterFixture задаёт только closure metadata; настоящий owner обязан доказать closure в persisted graph.
func cleanupAdapterFixture(t *testing.T) (*Owner, *resolveClientFixture, managedkubernetes.Binding) {
	t.Helper()
	owner, client, binding := operationResolveFixture(t)
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		t.Fatal(err)
	}
	client.response = &cp.ResolveManagedKubernetesOperationResponse{CleanupReceipt: &cp.ManagedKubernetesCleanupReceipt{
		InvocationRef: binding.InvocationRef, StreamRef: "stream_synthetic", Generation: binding.Generation,
		BindingDigest: digest, SnapshotDigest: strings.Repeat("b", 64),
		StreamState: cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE, ExecutionClosed: true,
	}}
	return owner, client, binding
}

// TestCleanupAdapterRetainsOriginalClaim proves that a closed lease can clean an ACTIVE row without acquiring a fresh execution snapshot.
// TestCleanupAdapterRetainsOriginalClaim доказывает cleanup ACTIVE row после закрытия lease без получения fresh execution snapshot.
func TestCleanupAdapterRetainsOriginalClaim(t *testing.T) {
	owner, client, binding := cleanupAdapterFixture(t)
	result, err := owner.ResolveKubernetesStreamCleanup(t.Context(), binding, "stream_synthetic")
	if err != nil || !result.ExecutionClosed || result.StreamState != "ACTIVE" || client.calls != 1 || !client.request.CleanupOnly ||
		client.request.StreamRef != "stream_synthetic" || !proto.Equal(client.request.Binding, kubernetesHealthBindingToProto(binding)) {
		t.Fatal("cleanup changed original authority or retried")
	}
	client.response.CleanupReceipt.SnapshotDigest = "changed"
	if result.SnapshotDigest != strings.Repeat("b", 64) {
		t.Fatal("mutable RPC response changed closure proof")
	}
}

// TestCleanupAdapterRefusesAuthorityMixing exercises lost responses, foreign claims and snapshots masquerading as cleanup.
// TestCleanupAdapterRefusesAuthorityMixing проверяет lost responses, чужие claims и snapshots под видом cleanup.
func TestCleanupAdapterRefusesAuthorityMixing(t *testing.T) {
	for _, scenario := range []string{"lost", "nil", "missing", "open", "invocation", "stream", "generation", "binding", "snapshot", "unknown-state", "absent-state", "unknown-response", "unknown-proof", "normal-snapshot", "identity", "plan", "output-input"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding := cleanupAdapterFixture(t)
			proof := client.response.CleanupReceipt
			switch scenario {
			case "lost":
				client.err = errors.New("synthetic private owner failure")
			case "nil":
				client.response = nil
			case "missing":
				client.response.CleanupReceipt = nil
			case "open":
				proof.ExecutionClosed = false
			case "invocation":
				proof.InvocationRef = "inv_foreign"
			case "stream":
				proof.StreamRef = "stream_foreign"
			case "generation":
				proof.Generation++
			case "binding":
				proof.BindingDigest = strings.Repeat("c", 64)
			case "snapshot":
				proof.SnapshotDigest = ""
			case "unknown-state":
				proof.StreamState = cp.ManagedKubernetesOwnerStreamState(99)
			case "absent-state":
				proof.StreamState = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_UNSPECIFIED
			case "unknown-response":
				client.response.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "unknown-proof":
				proof.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 1})
			case "normal-snapshot":
				_, base, _ := operationResolveFixture(t)
				client.response = base.response
				client.response.CleanupReceipt = proof
			case "identity":
				client.response.IdentityLocator = &cp.ManagedKubernetesIdentityLocator{}
			case "plan":
				client.response.PlanRef = "plan_synthetic"
			case "output-input":
				client.response.NormalizedInputJson = []byte(`{}`)
			}
			result, err := owner.ResolveKubernetesStreamCleanup(t.Context(), binding, "stream_synthetic")
			if err == nil || result.ExecutionClosed || client.calls != 1 || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid cleanup yielded closure, retried or leaked private error")
			}
		})
	}
}

// TestCleanupAdapterRejectsInvalidRequestBeforeRPC refuses test bindings, invalid selectors and cancelled contexts before owner access.
// TestCleanupAdapterRejectsInvalidRequestBeforeRPC отклоняет test bindings, invalid selectors и cancelled contexts до owner access.
func TestCleanupAdapterRejectsInvalidRequestBeforeRPC(t *testing.T) {
	for _, scenario := range []string{"nil-context", "canceled", "test", "fence", "empty-stream", "oversized-stream"} {
		t.Run(scenario, func(t *testing.T) {
			owner, client, binding := cleanupAdapterFixture(t)
			ctx := t.Context()
			streamRef := "stream_synthetic"
			switch scenario {
			case "nil-context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "test":
				binding.InvocationRef, binding.ConnectionTestRef = "", "test_synthetic"
			case "fence":
				binding.Fence = ""
			case "empty-stream":
				streamRef = ""
			case "oversized-stream":
				streamRef = strings.Repeat("s", 97)
			}
			result, err := owner.ResolveKubernetesStreamCleanup(ctx, binding, streamRef)
			if err == nil || result.ExecutionClosed || client.calls != 0 {
				t.Fatal("invalid cleanup request reached owner")
			}
		})
	}
}

// TestOperationResolveCannotConsumeCleanup rejects both mixed and pure closure metadata on the execution read path.
// TestOperationResolveCannotConsumeCleanup отклоняет mixed и pure closure metadata на execution read path.
func TestOperationResolveCannotConsumeCleanup(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		owner, client, binding := cleanupAdapterFixture(t)
		if mixed {
			proof := client.response.CleanupReceipt
			_, base, _ := operationResolveFixture(t)
			client.response = base.response
			client.response.CleanupReceipt = proof
		}
		result, err := owner.ResolveKubernetesOperation(t.Context(), binding)
		if err == nil || result.Digest != "" || client.calls != 1 || client.request.CleanupOnly {
			t.Fatal("closure proof became execution authority")
		}
	}
}
