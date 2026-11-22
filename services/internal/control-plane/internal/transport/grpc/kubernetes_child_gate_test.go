package grpc

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// kubernetesGateChildFixture contains only the persisted safe description of one ordinary effect.
// kubernetesGateChildFixture содержит только persisted safe description одного ordinary effect.
func kubernetesGateChildFixture() entity.KubernetesOwnerGateChild {
	return entity.KubernetesOwnerGateChild{PlanRef: "kpl_synthetic", ChildRef: "kch_synthetic", Operation: "kubernetes.patch", PlanDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), TargetRef: "ktg_synthetic", TargetRevision: 3,
		Preview: &managedkubernetes.GateEffectPreview{InputDigest: strings.Repeat("b", 64), Options: []managedkubernetes.GatePreviewOption{}, Changes: []managedkubernetes.GatePreviewChange{}, Limitations: []string{}}, Resource: managedkubernetes.ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "synthetic", Namespaced: true, Name: "example", UID: "30000000-0000-4000-8000-000000000001", ResourceVersion: "17"}}
}

// TestKubernetesChildGateProjectionPreservesExactTarget checks full address and create intent without fake incarnation.
// TestKubernetesChildGateProjectionPreservesExactTarget проверяет full address/create intent без fake incarnation.
func TestKubernetesChildGateProjectionPreservesExactTarget(t *testing.T) {
	for _, creating := range []bool{false, true} {
		child := kubernetesGateChildFixture()
		if creating {
			child.Operation = "kubernetes.create"
			child.Resource.Name = ""
			child.Resource.UID = ""
			child.Resource.ResourceVersion = ""
			child.IntendedGenerateName = "generated-"
		}
		result := castGate(entity.OwnerGate{KubernetesChild: &child})
		if result == nil || result.KubernetesChild == nil || result.IntegrationIntent != nil {
			t.Fatal("safe child projection missing")
		}
		projected := result.KubernetesChild
		if projected.PlanRef != child.PlanRef || projected.ChildRef != child.ChildRef || projected.TargetRef != child.TargetRef || projected.TargetRevision != 3 || projected.Resource.Group != "apps" || projected.Resource.Resource != "deployments" || projected.Resource.Namespace != "synthetic" || projected.Resource.Uid != child.Resource.UID || projected.Resource.ResourceVersion != child.Resource.ResourceVersion || projected.IntendedGenerateName != child.IntendedGenerateName {
			t.Fatal("child target or original pins changed")
		}
	}
}

// TestKubernetesRunGateKeepsNamedCollectionIntent rejects fabricated identities and unnamed or foreign resource creates.
// TestKubernetesRunGateKeepsNamedCollectionIntent отклоняет выдуманную identity, безымянное создание и подмену вида ресурса.
func TestKubernetesRunGateKeepsNamedCollectionIntent(t *testing.T) {
	for _, mode := range []string{"valid", "generated", "resource", "uid", "name"} {
		t.Run(mode, func(t *testing.T) {
			child := kubernetesGateChildFixture()
			child.Operation = "kubernetes.run"
			child.Resource = managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespace: "synthetic", Namespaced: true}
			child.IntendedName = "worker"
			switch mode {
			case "generated":
				child.IntendedName, child.IntendedGenerateName = "", "worker-"
			case "resource":
				child.Resource.Resource = "configmaps"
			case "uid":
				child.Resource.UID = "30000000-0000-4000-8000-000000000001"
			case "name":
				child.Resource.Name = "worker"
			}
			result := castGate(entity.OwnerGate{KubernetesChild: &child})
			if mode != "valid" {
				if result != nil {
					t.Fatal("invalid run intent became a resolvable Gate")
				}
				return
			}
			if result == nil || result.KubernetesChild == nil || result.KubernetesChild.IntendedName != "worker" || result.KubernetesChild.Resource.Name != "" || result.KubernetesChild.Resource.Uid != "" {
				t.Fatal("run Gate lost the original collection intent")
			}
		})
	}
}

// TestKubernetesChildGateProjectionRejectsInvalidOrMixedMetadata prevents generic fallback on malformed child metadata.
// TestKubernetesChildGateProjectionRejectsInvalidOrMixedMetadata запрещает generic fallback при malformed child metadata.
func TestKubernetesChildGateProjectionRejectsInvalidOrMixedMetadata(t *testing.T) {
	for _, mode := range []string{"target", "plan", "version", "operation", "namespace", "uid", "input", "intent", "rework", "create-name"} {
		t.Run(mode, func(t *testing.T) {
			child := kubernetesGateChildFixture()
			gate := entity.OwnerGate{KubernetesChild: &child}
			switch mode {
			case "target":
				child.TargetRef = ""
			case "plan":
				child.PlanDigest = "invalid"
			case "version":
				child.TargetRevision = 0
			case "operation":
				child.Operation = "kubernetes.not-registered"
			case "namespace":
				child.Resource.Namespace = "other/ns"
			case "uid":
				child.Resource.UID = ""
			case "input":
				child.InputDigest = "invalid"
			case "intent":
				gate.IntegrationIntent = &entity.IntegrationIntent{}
			case "rework":
				gate.ReworkPlan = &entity.WorkflowReworkPreview{}
			case "create-name":
				child.Operation = "kubernetes.create"
				child.IntendedName = "example"
			}
			if castGate(gate) != nil {
				t.Fatal("malformed child silently became generic Gate")
			}
		})
	}
}
