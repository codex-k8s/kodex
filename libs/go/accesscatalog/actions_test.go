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

	action, ok := SystemActionByKey(ActionProviderReconciliationRun)
	if !ok {
		t.Fatalf("SystemActionByKey(%q) ok = false, want true", ActionProviderReconciliationRun)
	}
	if action.ResourceType != ResourceProviderReconciliation {
		t.Fatalf("provider reconciliation resource = %q, want %q", action.ResourceType, ResourceProviderReconciliation)
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
