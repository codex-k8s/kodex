package platform

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantWorkspaceLimitsPrepareAndPersistExactPolicy(t *testing.T) {
	policyInput := assistantTestEnvironmentPolicy()
	resources := policyInput["resources"].(map[string]any)
	resources["workspaceLimits"] = map[string]any{"maxBytes": float64(4096), "maxFiles": float64(10)}
	policy, valid := assistantEnvironmentPolicy(map[string]any{"policy": policyInput})
	if !valid || policy.Resources.WorkspaceLimits == nil {
		t.Fatal("typed workspace budget rejected")
	}
	defaultPolicy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	spec := entity.RuntimeEnvironmentDraftSpecification{Name: "Original", Policy: defaultPolicy}
	raw, _ := json.Marshal(spec)
	var safe map[string]any
	_ = json.Unmarshal(raw, &safe)
	before := map[string]any{"environmentRef": "renv_exact", "projectRef": "", "systemAssistantRef": "agt_exact",
		"name": "Original", "description": "", "imageArtifactRef": "", "specification": safe}
	operation, err := hydrateAssistantEnvironmentFields(before, 7, entity.AssistantPlanOperation{
		Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Key: "workspace", Title: "Настроить workspace", Summary: "Ограничить ресурсы workspace", Parameters: map[string]any{"policy": policyInput}})
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAssistantOperation(operation)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := assistantOperationCommand(normalized)
	if err != nil || mapped.Kind != command.CreateOrganizationRuntimeEnvironmentDraft {
		t.Fatal("own configuration command boundary changed")
	}
	payload := mapped.Payload.(command.RuntimeEnvironmentDraftInput)
	if payload.EnvironmentRef != "renv_exact" || payload.ProjectRef != "" || payload.ExpectedEnvironmentVersion != 7 ||
		!reflect.DeepEqual(payload.Specification.Policy.Resources.WorkspaceLimits, policy.Resources.WorkspaceLimits) {
		t.Fatal("workspace plan lost own target, version or typed budget")
	}
	resourceJSON, _ := json.Marshal(policy.Resources)
	volumeJSON, _ := json.Marshal(policy.Volumes)
	networkJSON, _ := json.Marshal(policy.Network)
	accessJSON, _ := json.Marshal(policy.KubernetesAccess)
	restored, err := decodeRuntimeEnvironmentPolicy(resourceJSON, volumeJSON, networkJSON, accessJSON,
		policy.ResourcesDigest, policy.VolumesDigest, policy.NetworkDigest, policy.RBACDigest)
	if err != nil || !reflect.DeepEqual(restored.Resources, policy.Resources) {
		t.Fatal("immutable persisted resources lost workspace quota")
	}
	projection := assistantEnvironmentPolicyInput(restored)
	projectionJSON, _ := json.Marshal(projection)
	var projected map[string]any
	_ = json.Unmarshal(projectionJSON, &projected)
	if roundtrip, valid := assistantEnvironmentPolicy(map[string]any{"policy": projected}); !valid || !reflect.DeepEqual(roundtrip, restored) {
		t.Fatal("fresh own read lost typed workspace budget")
	}
	warm, err := runtimeWorkspacePolicyWithLimits(restored.Resources.WorkspaceLimits)
	turn, turnErr := runtimeWorkspacePolicyWithLimits(restored.Resources.WorkspaceLimits)
	if err != nil || turnErr != nil || !reflect.DeepEqual(warm, turn) || warm.Root != "/workspace" || warm.MaximumFileCount != 10 {
		t.Fatal("warm and turn quota projection disagree")
	}
}

func TestAssistantWorkspaceLimitsRejectUnknownAndExcessiveFields(t *testing.T) {
	for _, limits := range []map[string]any{
		{"maxBytes": float64(0), "maxFiles": float64(10)},
		{"maxBytes": float64(1<<30) + 1, "maxFiles": float64(10)},
		{"maxBytes": float64(4096), "maxFiles": float64(10001)},
		{"maxBytes": float64(1.5), "maxFiles": float64(10)},
		{"maxBytes": float64(4096)},
		{"maxBytes": float64(4096), "maxFiles": float64(10), "root": "/outside"},
		{"maxBytes": "PRIVATE_SENTINEL", "maxFiles": float64(10)},
	} {
		input := assistantTestEnvironmentPolicy()
		input["resources"].(map[string]any)["workspaceLimits"] = limits
		if _, valid := assistantEnvironmentPolicy(map[string]any{"policy": input}); valid {
			t.Fatal("unknown, incomplete or excessive quota accepted")
		}
	}
	input := assistantTestEnvironmentPolicy()
	input["workspaceLimits"] = map[string]any{"maxBytes": float64(4096), "maxFiles": float64(10)}
	if _, valid := assistantEnvironmentPolicy(map[string]any{"policy": input}); valid {
		t.Fatal("top-level alias accepted")
	}
}
