package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"strings"
	"testing"
)

// TestKubernetesChildGateSnapshotPreservesOriginalClaim verifies the fixed original snapshot hash
// without fabricating a raw fence from its stored digest. Every selected source pin remains bound.
// TestKubernetesChildGateSnapshotPreservesOriginalClaim проверяет исходный фиксированный hash
// без выдуманного raw fence из stored digest. Все selected source pins остаются связанными.
func TestKubernetesChildGateSnapshotPreservesOriginalClaim(t *testing.T) {
	var owner kubernetesOperationOwner
	owner.candidate.ref = "ivi_synthetic"
	owner.candidate.inputDigest = strings.Repeat("a", 64)
	owner.candidate.definitionDigest = strings.Repeat("c", 64)
	owner.candidate.resourceScopeDigest = strings.Repeat("e", 64)
	owner.selected.identity.Ref = "kid_synthetic"
	owner.selected.identity.Revision = 2
	owner.selected.subjectDigest = strings.Repeat("b", 64)
	owner.selected.configurationDigest = strings.Repeat("d", 64)
	owner.selected.bindingGeneration = 4
	owner.selected.source.registry.Revision = 5
	owner.selected.target.Revision = 6
	binding := value.ManagedKubernetesBinding{Fence: "synthetic-original-fence", Generation: 3}
	const original = "33ff7efaa0505def504286c5a3b8e35a17a74f8302354585ea9bf843c9c67dcf"
	raw, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil || raw != original {
		t.Fatal("original snapshot hash bytes changed")
	}
	stored, err := kubernetesOperationSnapshotDigestFromClaim(owner, kubernetesHealthFenceDigest(binding.Fence), binding.Generation)
	if err != nil || stored != original {
		t.Fatal("stored original claim failed snapshot verification")
	}
	for _, changed := range []string{"identity", "subject", "configuration", "binding", "registry", "target", "scope", "definition", "invocation", "input"} {
		current := owner
		switch changed {
		case "identity":
			current.selected.identity.Revision++
		case "subject":
			current.selected.subjectDigest = strings.Repeat("f", 64)
		case "configuration":
			current.selected.configurationDigest = strings.Repeat("f", 64)
		case "binding":
			current.selected.bindingGeneration++
		case "registry":
			current.selected.source.registry.Revision++
		case "target":
			current.selected.target.Revision++
		case "scope":
			current.candidate.resourceScopeDigest = strings.Repeat("f", 64)
		case "definition":
			current.candidate.definitionDigest = strings.Repeat("f", 64)
		case "invocation":
			current.candidate.ref = "ivi_foreign"
		case "input":
			current.candidate.inputDigest = strings.Repeat("f", 64)
		}
		observed, err := kubernetesOperationSnapshotDigestFromClaim(current, kubernetesHealthFenceDigest(binding.Fence), 3)
		if err != nil || observed == original {
			t.Fatalf("changed %s source retained original snapshot", changed)
		}
	}
	for _, generation := range []int64{0, 4} {
		observed, err := kubernetesOperationSnapshotDigestFromClaim(owner, kubernetesHealthFenceDigest(binding.Fence), generation)
		if err == nil && observed == original {
			t.Fatal("another generation retained original snapshot")
		}
	}
	if _, err := kubernetesOperationSnapshotDigestFromClaim(owner, "invalid", 3); err == nil {
		t.Fatal("invalid stored fence digest accepted")
	}
}
