package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestKubernetesGrantCandidateProjection carries only exact safe target/namespace metadata from
// the owner and refuses missing/foreign kinds, unknown fields, scope widening and hash mismatch.
// TestKubernetesGrantCandidateProjection передаёт только exact safe target/namespace metadata
// owner и отклоняет missing/foreign kinds, unknown fields, scope widening и hash mismatch.
func TestKubernetesGrantCandidateProjection(t *testing.T) {
	values := map[string]string{"target_ref": "target_example", "namespace": "example"}
	raw, _ := json.Marshal(values)
	digest := sha256.Sum256(raw)
	base := &cp.GetSystemAssistantIntegrationGrantCandidatesResponse{
		ScopeKind:       cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION,
		OrganizationRef: "org_fixture01", AssistantRef: "agt_fixture01", AssistantVersion: 2,
		ConnectionRef: "icon_fixture01", ConnectionVersion: 3, DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64),
		Items: []*cp.SystemAssistantIntegrationGrantCandidate{{Capability: &cp.IntegrationCapability{
			Key: "kubernetes.get", Operation: "kubernetes.get", TypedRisk: cp.IntegrationRisk_INTEGRATION_RISK_READ,
			ResourceKind:            cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_KUBERNETES_TARGET,
			ApprovalPolicy:          cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
			AllowedApprovalPolicies: []cp.IntegrationApprovalPolicy{cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE}},
			ResourceScope: &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_KUBERNETES_TARGET, Values: values, Digest: hex.EncodeToString(digest[:])},
			Grantable:     true, Reason: cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY}}, Total: 1, Page: &cp.PageInfo{},
	}
	view, ok := systemAssistantGrantCandidatePage(base, base.ConnectionRef, 50)
	if !ok {
		t.Fatal("safe Kubernetes scope was lost")
	}
	scope, present := view["items"].([]map[string]any)[0]["resourceScope"].(map[string]any)
	if !present || scope["kind"] != "KUBERNETES_TARGET" || scope["digest"] != hex.EncodeToString(digest[:]) {
		t.Fatal("safe Kubernetes scope pins were not preserved")
	}
	for _, mutation := range []string{"missing", "kind", "operation", "namespace", "private", "digest", "unknown"} {
		invalid := proto.Clone(base).(*cp.GetSystemAssistantIntegrationGrantCandidatesResponse)
		item := invalid.Items[0]
		switch mutation {
		case "missing":
			item.ResourceScope = nil
		case "kind":
			item.Capability.ResourceKind = cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_HTTPS_RESOURCE
		case "operation":
			item.Capability.Operation = "kubernetes.delete"
		case "namespace":
			item.ResourceScope.Values["namespace"] = "@all-namespaces"
		case "private":
			item.ResourceScope.Values["credential"] = "private-sentinel"
		case "digest":
			item.ResourceScope.Digest = strings.Repeat("0", 64)
		case "unknown":
			item.ResourceScope.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
		}
		if _, ok := systemAssistantGrantCandidatePage(invalid, base.ConnectionRef, 50); ok {
			t.Fatal("invalid Kubernetes scope reached public output")
		}
	}
}
