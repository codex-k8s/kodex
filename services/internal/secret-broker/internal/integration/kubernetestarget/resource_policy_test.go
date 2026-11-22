package kubernetestarget

import (
	"encoding/json"
	"testing"
)

// resourcePolicyFixture constructs a safe built-in Deployment with an explicitly noncredential
// Pod template. Every test gets private maps so a rejected reconstruction cannot poison another.
// resourcePolicyFixture создаёт safe built-in Deployment с явно noncredential Pod template.
// Каждый тест получает private maps; отклонённая reconstruction не меняет соседний сценарий.
func resourcePolicyFixture(t *testing.T) (ResourceAddress, map[string]any, map[string]any) {
	t.Helper()
	var object map[string]any
	if json.Unmarshal([]byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"selected","namespace":"example"},"spec":{"template":{"spec":{"automountServiceAccountToken":false,"containers":[{"name":"app","image":"example.test/image:fixture"}],"volumes":[{"name":"scratch","emptyDir":{}}]}}}}`), &object) != nil {
		t.Fatal("invalid owned resource fixture")
	}
	spec := object["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	return ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Name: "selected", Namespaced: true, Namespace: "example"}, object, spec
}

// TestResourceObjectPolicyDeniesEscalatingReconstructedTemplates proves that known host and
// credential paths are checked on the complete post-patch object, not only a submitted root key.
// TestResourceObjectPolicyDeniesEscalatingReconstructedTemplates доказывает проверку известных
// host/credential paths в полном post-patch object, не только в присланном root key.
func TestResourceObjectPolicyDeniesEscalatingReconstructedTemplates(t *testing.T) {
	address, object, _ := resourcePolicyFixture(t)
	if ValidateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, []string{"kodex-system"}) != nil {
		t.Fatal("ordinary explicitly noncredential workload was rejected")
	}
	for _, testCase := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"implicit automount", func(spec map[string]any) { delete(spec, "automountServiceAccountToken") }},
		{"token automount", func(spec map[string]any) { spec["automountServiceAccountToken"] = true }},
		{"host network", func(spec map[string]any) { spec["hostNetwork"] = true }},
		{"host PID", func(spec map[string]any) { spec["hostPID"] = true }},
		{"host IPC", func(spec map[string]any) { spec["hostIPC"] = true }},
		{"shared PID", func(spec map[string]any) { spec["shareProcessNamespace"] = true }},
		{"service account", func(spec map[string]any) { spec["serviceAccountName"] = "operator" }},
		{"service account alias", func(spec map[string]any) { spec["serviceAccount"] = "operator" }},
		{"image pull secret", func(spec map[string]any) { spec["imagePullSecrets"] = []any{map[string]any{"name": "registry"}} }},
		{"secret volume", func(spec map[string]any) {
			spec["volumes"] = []any{map[string]any{"name": "private", "secret": map[string]any{"secretName": "selected"}}}
		}},
		{"CSI secret", func(spec map[string]any) {
			spec["volumes"] = []any{map[string]any{"name": "private", "csi": map[string]any{"driver": "example.test", "nodePublishSecretRef": map[string]any{"name": "selected"}}}}
		}},
		{"host path", func(spec map[string]any) {
			spec["volumes"] = []any{map[string]any{"name": "host", "hostPath": map[string]any{"path": "/"}}}
		}},
		{"projected token", func(spec map[string]any) {
			spec["volumes"] = []any{map[string]any{"name": "token", "projected": map[string]any{"sources": []any{map[string]any{"serviceAccountToken": map[string]any{"path": "token"}}}}}}
		}},
		{"projected mixed", func(spec map[string]any) {
			spec["volumes"] = []any{map[string]any{"name": "mixed", "projected": map[string]any{"sources": []any{map[string]any{"configMap": map[string]any{"name": "public"}, "secret": map[string]any{"name": "private"}}}}}}
		}},
		{"mixed volume", func(spec map[string]any) {
			spec["volumes"] = []any{map[string]any{"name": "mixed", "emptyDir": map[string]any{}, "secret": map[string]any{"secretName": "private"}}}
		}},
		{"privileged", func(spec map[string]any) {
			spec["containers"].([]any)[0].(map[string]any)["securityContext"] = map[string]any{"privileged": true}
		}},
		{"dangerous capability", func(spec map[string]any) {
			spec["containers"].([]any)[0].(map[string]any)["securityContext"] = map[string]any{"capabilities": map[string]any{"add": []any{"SYS_ADMIN"}}}
		}},
		{"secret env", func(spec map[string]any) {
			spec["containers"].([]any)[0].(map[string]any)["env"] = []any{map[string]any{"name": "PRIVATE", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "private", "key": "token"}}}}
		}},
		{"init secret envFrom", func(spec map[string]any) {
			spec["initContainers"] = []any{map[string]any{"name": "init", "image": "example.test/image:fixture", "envFrom": []any{map[string]any{"secretRef": map[string]any{"name": "private"}}}}}
		}},
		{"unknown Pod field", func(spec map[string]any) { spec["unexpected"] = true }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			address, object, spec := resourcePolicyFixture(t)
			testCase.edit(spec)
			if ValidateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil) == nil {
				t.Fatal("unsafe reconstructed Pod template was admitted")
			}
			if ValidateResourceObjectPolicy("kubernetes.get", address, "Deployment", object, nil) != nil {
				t.Fatal("write policy incorrectly removed ordinary resource read eligibility")
			}
		})
	}
}

// TestResourceObjectPolicyBindsScopeAndRegisteredObject prevents a safe-looking template from
// being admitted for another discovery kind, protected namespace or changed resource name.
// TestResourceObjectPolicyBindsScopeAndRegisteredObject не позволяет допустить внешне safe
// template для другого discovery kind, protected namespace либо изменённого resource name.
func TestResourceObjectPolicyBindsScopeAndRegisteredObject(t *testing.T) {
	address, object, _ := resourcePolicyFixture(t)
	if ValidateResourceObjectPolicy("kubernetes.get", address, "Deployment", object, []string{"example"}) == nil ||
		ValidateResourceObjectPolicy("kubernetes.patch", address, "Pod", object, nil) == nil ||
		ValidateResourceObjectPolicy("unknown", address, "Deployment", object, nil) == nil {
		t.Fatal("foreign or protected resource was admitted")
	}
	object["metadata"].(map[string]any)["name"] = "changed"
	if ValidateResourceObjectPolicy("kubernetes.patch", address, "Deployment", object, nil) == nil {
		t.Fatal("changed name bypassed resource ownership")
	}
}
