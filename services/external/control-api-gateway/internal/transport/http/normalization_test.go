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

func TestLocalizeSafeErrorsResolvesOnlyExplicitMessageReferences(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"name":             "i18n:SYSTEM_ASSISTANT_NAME",
		"ownerContent":     "SYSTEM_ASSISTANT_NAME",
		"safeErrorCode":    "RUNTIME_UNAVAILABLE",
		"safeErrorMessage": "stale",
		"nested":           map[string]any{"title": "i18n:OWNER_GATE_REVIEW_TITLE"},
	}
	LocalizeSafeErrors(value, func(messageID string) string { return "localized:" + messageID })

	if value["name"] != "localized:SYSTEM_ASSISTANT_NAME" {
		t.Fatalf("явная ссылка на сообщение не локализована: %#v", value)
	}
	if value["ownerContent"] != "SYSTEM_ASSISTANT_NAME" {
		t.Fatalf("пользовательский текст ошибочно локализован: %#v", value)
	}
	if value["safeErrorMessage"] != "localized:RUNTIME_UNAVAILABLE" {
		t.Fatalf("безопасная ошибка не локализована: %#v", value)
	}
	nested := value["nested"].(map[string]any)
	if nested["title"] != "localized:OWNER_GATE_REVIEW_TITLE" {
		t.Fatalf("вложенная ссылка на сообщение не локализована: %#v", value)
	}
}
