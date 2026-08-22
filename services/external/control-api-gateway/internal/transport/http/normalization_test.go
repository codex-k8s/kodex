package httptransport

import (
	"reflect"
	"testing"
)

func TestNormalizePreservesRequiredWorkflowDefaults(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"ref": "wfl-example",
		"draftVersion": map[string]any{
			"steps": []any{map[string]any{
				"ref": "step-001", "position": float64(1), "name": "Шаг", "purpose": "Выполнить", "timeoutSeconds": float64(60),
			}},
		},
	}

	normalize(value)
	steps, ok := value["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("шаги workflow потеряны: %#v", value)
	}
	step := steps[0].(map[string]any)
	for key, expected := range map[string]any{"parallel": false, "parallelGroup": float64(0), "expectedResult": "", "humanGate": false} {
		if !reflect.DeepEqual(step[key], expected) {
			t.Fatalf("обязательное поле %s: получено %#v, ожидалось %#v", key, step[key], expected)
		}
	}
	for _, key := range []string{"gateDecisions", "requiredCapabilityKeys"} {
		if items, ok := step[key].([]any); !ok || len(items) != 0 {
			t.Fatalf("обязательная коллекция %s отсутствует: %#v", key, step[key])
		}
	}
}
