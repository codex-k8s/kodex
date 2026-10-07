package admissioncontroller

import (
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestWorkspaceStoragePolicyRejectsChangedPVCBoundary(t *testing.T) {
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	var policy admissionPolicyDocument
	for _, candidate := range policies {
		if candidate.Metadata.Name == "kodex-image-admission-controller-workspaces" {
			policy = candidate
		}
	}
	ownerPolicy := completeTestPolicy()
	renderer, err := NewScriptRenderer(filepath.Join(repositoryRoot(), "tools", "render-image-admission-job.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runID := "v20260822120000-" + testOrchestrationRevision
	rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, "claim")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(rendered, "kodex-system", runID, "claim"); err != nil {
		t.Fatal(err)
	}
	fixture, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.PVC)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicyAccepts(t, policy, fixture, ownerPolicy)
	mutations := map[string]func(map[string]any){
		"larger storage":  func(s map[string]any) { s["resources"] = map[string]any{"requests": map[string]any{"storage": "4Gi"}} },
		"smaller storage": func(s map[string]any) { s["resources"] = map[string]any{"requests": map[string]any{"storage": "1Gi"}} },
		"numeric storage": func(s map[string]any) {
			s["resources"] = map[string]any{"requests": map[string]any{"storage": int64(2147483648)}}
		},
		"missing requests":    func(s map[string]any) { s["resources"] = map[string]any{} },
		"missing storage":     func(s map[string]any) { s["resources"] = map[string]any{"requests": map[string]any{}} },
		"missing resources":   func(s map[string]any) { delete(s, "resources") },
		"wrong resource type": func(s map[string]any) { s["resources"] = "2Gi" },
		"wrong requests type": func(s map[string]any) { s["resources"] = map[string]any{"requests": "2Gi"} },
		"shared access":       func(s map[string]any) { s["accessModes"] = []any{"ReadWriteMany"} },
		"extra access":        func(s map[string]any) { s["accessModes"] = []any{"ReadWriteOnce", "ReadOnlyMany"} },
		"prebound volume":     func(s map[string]any) { s["volumeName"] = "foreign-volume" },
		"volume selector": func(s map[string]any) {
			s["selector"] = map[string]any{"matchLabels": map[string]any{"foreign": "true"}}
		},
		"clone source": func(s map[string]any) {
			s["dataSource"] = map[string]any{"kind": "PersistentVolumeClaim", "name": "foreign"}
		},
		"external source": func(s map[string]any) {
			s["dataSourceRef"] = map[string]any{"kind": "PersistentVolumeClaim", "name": "foreign"}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			object := runtime.DeepCopyJSON(fixture)
			mutate(object["spec"].(map[string]any))
			accepted, err := evaluateAdmissionPolicy(policy, object, ownerPolicy)
			// Отсутствующее поле или неверный тип могут дать CEL error; failurePolicy
			// Fail закрывает этот путь точно так же, как boolean false.
			if accepted {
				t.Fatal("changed PVC was accepted")
			}
			if err != nil && name != "numeric storage" && name != "missing requests" && name != "missing storage" && name != "missing resources" && name != "wrong resource type" && name != "wrong requests type" {
				t.Fatal(err)
			}
		})
	}
}
