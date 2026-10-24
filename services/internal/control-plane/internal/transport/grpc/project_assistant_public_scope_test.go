package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestProjectAssistantGrantCandidatePublicScope checks the exact GitHub repository scope and
// digest reach typed output and rejects wrong kind, digest, absent/extra keys, private-key
// substitution or unrelated capability namespace.
// TestProjectAssistantGrantCandidatePublicScope проверяет передачу точных GitHub repository scope
// и digest в типизированный output и отказ для неверных kind, digest, отсутствующих либо лишних
// ключей, подмены ключа и чужого namespace capability.
func TestProjectAssistantGrantCandidatePublicScope(t *testing.T) {
	values := map[string]string{"owner": "fixture-owner", "repository": "repository"}
	encoded, _ := json.Marshal(values)
	digest := sha256.Sum256(encoded)
	item := entity.SystemAssistantIntegrationGrantCandidate{Reason: "READY", Grantable: true, Capability: entity.IntegrationCapability{Key: "github.repository.content.read", ResourceKind: "GITHUB_REPOSITORY"}, ResourceScope: values, ResourceScopeDigest: hex.EncodeToString(digest[:])}
	wire, err := castAssistantIntegrationGrantCandidate(item)
	if err != nil || wire.GetResourceScope().GetKind() != cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY || wire.GetResourceScope().GetValues()["owner"] != values["owner"] || wire.GetResourceScope().GetDigest() != item.ResourceScopeDigest {
		t.Fatal("typed wire lost public scope")
	}
	for _, scenario := range []string{"kind", "digest", "missing", "extra", "token", "namespace"} {
		t.Run(scenario, func(t *testing.T) {
			bad := item
			bad.ResourceScope = map[string]string{"owner": values["owner"], "repository": values["repository"]}
			switch scenario {
			case "kind":
				bad.Capability.ResourceKind = "UNKNOWN"
			case "digest":
				bad.ResourceScopeDigest = strings.Repeat("b", 64)
			case "missing":
				bad.ResourceScope = nil
			case "extra":
				bad.ResourceScope["token"] = "PRIVATE_SENTINEL"
			case "token":
				bad.ResourceScope = map[string]string{"owner": values["owner"], "token": "PRIVATE_SENTINEL"}
			case "namespace":
				bad.Capability.Key = "synthetic.read"
			}
			if _, err := castAssistantIntegrationGrantCandidate(bad); err == nil {
				t.Fatal("malformed domain scope became typed wire")
			}
		})
	}
}
