package accesscatalog

import "testing"

func TestSystemActionsHaveUniqueKeys(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for _, action := range SystemActions() {
		if action.Key == "" || action.ResourceType == "" {
			t.Fatalf("system action = %+v, want non-empty key and resource type", action)
		}
		if _, exists := seen[action.Key]; exists {
			t.Fatalf("duplicate system action key %q", action.Key)
		}
		seen[action.Key] = struct{}{}
	}
}

func TestSystemActionByKey(t *testing.T) {
	t.Parallel()

	action, ok := SystemActionByKey(" " + ActionRuntimeJobCreate + " ")
	if !ok {
		t.Fatalf("SystemActionByKey() ok = false, want true")
	}
	if action.Key != ActionRuntimeJobCreate || action.ResourceType != ResourceRuntimeJob {
		t.Fatalf("SystemActionByKey() = %+v, want runtime job create", action)
	}
	if _, ok := SystemActionByKey("package.installation.reed"); ok {
		t.Fatalf("SystemActionByKey() ok = true for unknown action, want false")
	}
}

func TestProviderHubActionsAreSystemActions(t *testing.T) {
	t.Parallel()

	action, ok := SystemActionByKey(ActionProviderRepositoryWrite)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionProviderRepositoryWrite)
	}
	if action.ResourceType != ResourceProviderRepository {
		t.Fatalf("provider repository resource = %q, want %q", action.ResourceType, ResourceProviderRepository)
	}
}

func TestFleetManagerActionsAreSystemActions(t *testing.T) {
	t.Parallel()

	action, ok := SystemActionByKey(ActionFleetClusterEnable)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionFleetClusterEnable)
	}
	if action.ResourceType != ResourceFleetCluster {
		t.Fatalf("fleet cluster resource = %q, want %q", action.ResourceType, ResourceFleetCluster)
	}
}

func TestAgentManagerActionsAreSystemActions(t *testing.T) {
	t.Parallel()

	action, ok := SystemActionByKey(ActionAgentAcceptanceRun)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionAgentAcceptanceRun)
	}
	if action.ResourceType != ResourceAgentAcceptance {
		t.Fatalf("agent acceptance resource = %q, want %q", action.ResourceType, ResourceAgentAcceptance)
	}
}

func TestGovernanceManagerActionsAreSystemActions(t *testing.T) {
	t.Parallel()

	action, ok := SystemActionByKey(ActionGovernanceGateDecide)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionGovernanceGateDecide)
	}
	if action.ResourceType != ResourceGovernanceGate {
		t.Fatalf("governance gate resource = %q, want %q", action.ResourceType, ResourceGovernanceGate)
	}
}

func TestInteractionHubActionsAreSystemActions(t *testing.T) {
	t.Parallel()

	action, ok := SystemActionByKey(ActionInteractionCallbackRecord)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionInteractionCallbackRecord)
	}
	if action.ResourceType != ResourceInteractionCallback {
		t.Fatalf("interaction callback resource = %q, want %q", action.ResourceType, ResourceInteractionCallback)
	}

	responseAction, ok := SystemActionByKey(ActionInteractionRequestRespond)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionInteractionRequestRespond)
	}
	if responseAction.ResourceType != ResourceInteractionResponse {
		t.Fatalf("interaction response resource = %q, want %q", responseAction.ResourceType, ResourceInteractionResponse)
	}
}
