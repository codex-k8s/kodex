package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

type localizingRecorder struct{ *httptest.ResponseRecorder }

func (recorder *localizingRecorder) Localize(messageID string) string {
	return "localized:" + messageID
}

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

func TestWorkflowDraftPreservesBoundedInputFields(t *testing.T) {
	t.Parallel()

	key := "priority"
	fields := []generated.WorkflowInputFieldInput{{
		Key: &key, Label: "Приоритет", Description: "Выберите срочность",
		ValueType: generated.SELECT, Required: true, Options: []string{"Обычный", "Высокий"},
	}}
	draft := workflowDraft(generated.WorkflowInput{
		Name: "Обработка обращения", Purpose: "Подготовить ответ", CoordinatorAgentRef: "agt-coordinator",
		InputFields: &fields,
	})
	if len(draft.GetInputFields()) != 1 {
		t.Fatalf("поля входа потеряны: %#v", draft)
	}
	field := draft.GetInputFields()[0]
	if field.GetKey() != key || field.GetValueType() != "SELECT" || !field.GetRequired() || !reflect.DeepEqual(field.GetOptions(), []string{"Обычный", "Высокий"}) {
		t.Fatalf("поле входа искажено: %#v", field)
	}
}

func TestNormalizeWorkflowInputFieldAddsEmptyOptions(t *testing.T) {
	t.Parallel()

	value := map[string]any{"key": "company", "label": "Компания", "valueType": "TEXT"}
	normalize(value)
	if options, ok := value["options"].([]any); !ok || len(options) != 0 {
		t.Fatalf("обязательная коллекция options отсутствует: %#v", value)
	}
}

func TestNormalizeArtifactSource(t *testing.T) {
	t.Parallel()
	value := map[string]any{"source": "ARTIFACT_SOURCE_AGENT_RESULT"}
	normalize(value)
	if value["source"] != "AGENT_RESULT" {
		t.Fatalf("источник artifact не нормализован: %#v", value)
	}
}

func TestNormalizeEnumCollections(t *testing.T) {
	t.Parallel()
	value := map[string]any{"nextActions": []any{"NEXT_ACTION_OPEN", "NEXT_ACTION_CREATE_PROJECT"}}
	normalize(value)
	if !reflect.DeepEqual(value["nextActions"], []any{"OPEN", "CREATE_PROJECT"}) {
		t.Fatalf("enum collection не нормализована: %#v", value)
	}
}

func TestNormalizeFlattensAgentRuntimeWithExplicitReadiness(t *testing.T) {
	t.Parallel()
	value := map[string]any{"runtime": map[string]any{
		"ref": "builtin-safe-runtime", "name": "Runtime", "revision": "runtime-v1",
		"provider": "provider", "model": "model",
	}}
	normalize(value)
	if _, exists := value["runtime"]; exists {
		t.Fatalf("вложенный runtime не удалён: %#v", value)
	}
	if value["runtimeRef"] != "builtin-safe-runtime" || value["runtimeReady"] != false {
		t.Fatalf("runtime агента нормализован неверно: %#v", value)
	}
}

func TestSafeAttachmentFileNameRemovesHeaderAndPathControls(t *testing.T) {
	t.Parallel()

	if actual := safeAttachmentFileName(" ../отчёт\r\nX-Test: value\\.pdf "); actual != "..отчётX-Test: value.pdf" {
		t.Fatalf("небезопасное имя файла нормализовано неверно: %q", actual)
	}
	if actual := safeAttachmentFileName("\r\n/\\"); actual != "artifact" {
		t.Fatalf("пустое имя файла не заменено: %q", actual)
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

func TestWriteMessagePreservesCollectionAuthorityAndLocalizesCatalog(t *testing.T) {
	t.Parallel()

	writer := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
	writeMessage(writer, http.StatusOK, &controlplanev1.ListIntegrationDefinitionsResponse{
		Definitions: []*controlplanev1.IntegrationDefinition{{
			Key: "example", Name: "i18n:INTEGRATION_EXAMPLE_NAME", Available: true,
		}},
		NextActions: []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_CREATE_CONNECTION},
		CoreReady:   true,
	}, "", "definitions")

	var value map[string]any
	if err := json.Unmarshal(writer.Body.Bytes(), &value); err != nil {
		t.Fatalf("декодировать response: %v", err)
	}
	items, _ := value["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "localized:INTEGRATION_EXAMPLE_NAME" {
		t.Fatalf("каталог не локализован: %#v", value)
	}
	if ready, _ := value["coreReady"].(bool); !ready {
		t.Fatalf("core readiness потерян: %#v", value)
	}
	actions, _ := value["nextActions"].([]any)
	if len(actions) != 1 || actions[0] != "CREATE_CONNECTION" {
		t.Fatalf("авторитетные действия потеряны: %#v", value)
	}
}
