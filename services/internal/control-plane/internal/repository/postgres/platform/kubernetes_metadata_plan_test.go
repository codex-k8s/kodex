package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestKubernetesMetadataChildRetainsOriginalReadScope proves addressless commands retain the original READ/NONE cluster grant and canonical input without fabricated object evidence.
// TestKubernetesMetadataChildRetainsOriginalReadScope доказывает сохранность исходного READ/NONE cluster grant и canonical input без выдуманного object evidence.
func TestKubernetesMetadataChildRetainsOriginalReadScope(t *testing.T) {
	for _, operation := range []string{"kubernetes.version", "kubernetes.identity", "kubernetes.help", "kubernetes.config.metadata"} {
		t.Run(operation, func(t *testing.T) {
			input, canonical, err := managedkubernetes.DecodeOperationInput(operation, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			owner := kubernetesOperationOwner{input: input, normalized: canonical, candidate: integrationInvocationCandidate{operation: operation, risk: "READ", approvalPolicy: "NONE", resourceScope: []byte(`{"target_ref":"target_fixture","namespace":"@cluster"}`)}}
			child, err := kubernetesMetadataChild(owner)
			if err != nil || child.Operation != operation || child.Resource != (managedkubernetes.ResourceAddress{}) || child.Observation != nil || child.NodeTransport != nil || string(child.NormalizedInputJSON) != string(canonical) || !managedkubernetes.ValidDigest(child.InputDigest) {
				t.Fatal("metadata child lost original input or invented resource evidence", err)
			}
			for _, mode := range []string{"namespace", "risk", "gate", "unknown", "input"} {
				bad := owner
				switch mode {
				case "namespace":
					bad.candidate.resourceScope = []byte(`{"target_ref":"target_fixture","namespace":"example"}`)
				case "risk":
					bad.candidate.risk = "WRITE"
				case "gate":
					bad.candidate.approvalPolicy = "HUMAN_EACH_EFFECT"
				case "unknown":
					bad.candidate.operation = "kubernetes.discovery"
				case "input":
					bad.normalized = []byte(`{"name":"foreign"}`)
				}
				if _, err := kubernetesMetadataChild(bad); err == nil {
					t.Fatal("metadata child widened authority", mode)
				}
			}
		})
	}
}
