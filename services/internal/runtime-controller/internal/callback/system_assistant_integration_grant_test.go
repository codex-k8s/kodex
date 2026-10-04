package callback

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestSystemAssistantIntegrationGrantClosedPlanShape(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	input.AssistantContext.AllowedOperations = []string{"CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"}
	valid := map[string]any{"connectionRef": "icn_fixture123", "capabilityKey": "github.issue.comment.create", "enabled": true, "approvalPolicy": "NONE"}
	if !assistantConfigurationParametersAllowed(input, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", valid) {
		t.Fatal("closed SYSTEM grant locator rejected")
	}
	if assistantServerAction("CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") != "UPDATE" || !assistantServerHydratedOperation("CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT") {
		t.Fatal("system grant was not server-hydrated update")
	}
	target := assistantServerTarget("CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", valid, nil)
	if target["kind"] != "INTEGRATION_CONNECTION" || target["name"] != valid["connectionRef"] {
		t.Fatal("grant target is not exact connection")
	}
	found := false
	for _, schema := range assistantPlanOperationSchemas(input) {
		if assistantSchemaType(schema) == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			found = true
		}
	}
	if !found {
		t.Fatal("SYSTEM grant schema unreachable")
	}
	project := assistantConfigurationFixture(runtimecontract.AssistantScopeProject)
	if assistantConfigurationParametersAllowed(project, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", valid) {
		t.Fatal("PROJECT source admitted SYSTEM grant")
	}
	for _, field := range []string{"agentRef", "projectRef", "organizationRef", "systemAssistantRef", "scopeKind"} {
		bad := map[string]any{}
		for key, value := range valid {
			bad[key] = value
		}
		bad[field] = ""
		if assistantConfigurationParametersAllowed(input, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", bad) {
			t.Fatalf("caller owner field %s accepted", field)
		}
	}
	for _, paths := range []any{nil, []any{"/issue_number", "/issue_number"}, []any{float64(1)}, []any{""}} {
		bad := map[string]any{}
		for key, value := range valid {
			bad[key] = value
		}
		bad["approvalPolicy"] = "HUMAN_SCOPED"
		bad["approvalScopePaths"] = paths
		if assistantConfigurationParametersAllowed(input, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", bad) {
			t.Fatal("malformed scoped approval admitted")
		}
	}
	valid["approvalPolicy"] = "HUMAN_SCOPED"
	valid["approvalScopePaths"] = []any{"/issue_number"}
	if !assistantConfigurationParametersAllowed(input, "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", valid) {
		t.Fatal("explicit scoped paths rejected")
	}
}
