package callback

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestWorkflowTextBudgetCatalogDisclosesTheExactSum checks both published operation schemas explain the same aggregate bytes and retained UPDATE values without changing individual limits.
// TestWorkflowTextBudgetCatalogDisclosesTheExactSum проверяет раскрытие одинаковой суммы байт и сохраняемых UPDATE значений в обеих опубликованных схемах без изменения отдельных ограничений.
func TestWorkflowTextBudgetCatalogDisclosesTheExactSum(t *testing.T) {
	for _, operation := range []string{"CREATE_WORKFLOW", "UPDATE_WORKFLOW"} {
		input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AgentRef: "agt_helper123", ProjectRef: "prj_fixture123"}
		if operation == "UPDATE_WORKFLOW" {
			version := int64(1)
			input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "WORKFLOW", EntityRef: "wfl_fixture123", EntityVersion: &version, AllowedOperations: []string{operation}}
		}
		catalog, err := configurationCatalog(input, map[string]any{"operation_types": []any{operation}})
		if err != nil {
			t.Fatal("discover actual workflow operation catalog", err)
		}
		schemas := catalog.(map[string]any)["operation_schemas"].([]map[string]any)
		if len(schemas) != 1 {
			t.Fatal("workflow catalog did not publish one exact operation")
		}
		schema := schemas[0]["properties"].(map[string]any)["parameters"].(map[string]any)
		description, _ := schema["description"].(string)
		for _, fragment := range []string{"SERVER_DAG_V1", "8192 UTF-8 bytes", "name + purpose + instructions + completionCriteria", "steps[].name + steps[].purpose + steps[].expectedResult", "not Unicode characters", "MANAGER_DRIVEN"} {
			if !strings.Contains(description, fragment) {
				t.Fatal("workflow catalog hides its exact aggregate text budget", fragment)
			}
		}
		raw, err := json.Marshal(schema["properties"])
		if err != nil || !strings.Contains(string(raw), `"maxLength":1000`) || !strings.Contains(string(raw), `"maxLength":160`) {
			t.Fatal("budget disclosure changed individual field limits")
		}
	}
	update := workflowUpdateInputSchema("wfl_fixture123")
	if !strings.Contains(update["description"].(string), "including retained fields") {
		t.Fatal("sparse update hides the retained text budget")
	}
}
