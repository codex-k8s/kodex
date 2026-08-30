package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/structpb"
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
		ValueType: generated.WorkflowInputFieldInputValueTypeSELECT, Required: true, Options: []string{"Обычный", "Высокий"},
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

func TestNormalizeArtifactLifecycleState(t *testing.T) {
	t.Parallel()
	value := map[string]any{"lifecycleState": "ARTIFACT_LIFECYCLE_STATE_DELETED"}
	normalize(value)
	if value["lifecycleState"] != "DELETED" {
		t.Fatalf("lifecycle artifact не нормализован: %#v", value)
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

func TestMessageMapNormalizesAccessEnumsToOpenAPIValues(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.PermissionDefinition{
		Key:           "agent.launch",
		Risk:          controlplanev1.PermissionRisk_PERMISSION_RISK_WRITE,
		AllowedScopes: []controlplanev1.AccessScopeKind{controlplanev1.AccessScopeKind_ACCESS_SCOPE_KIND_RESOURCE_INSTANCE},
		ResourceKinds: []controlplanev1.AccessResourceKind{controlplanev1.AccessResourceKind_ACCESS_RESOURCE_KIND_AGENT},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	if value["risk"] != "WRITE" {
		t.Fatalf("permission risk is not public: %#v", value)
	}
	if !reflect.DeepEqual(value["allowedScopes"], []any{"RESOURCE_INSTANCE"}) {
		t.Fatalf("permission scopes are not public: %#v", value)
	}
	if !reflect.DeepEqual(value["resourceKinds"], []any{"AGENT"}) {
		t.Fatalf("permission resources are not public: %#v", value)
	}
}

func TestMessageMapConvertsProtoInt64ToOpenAPIJSONNumber(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.AssistantPlan{Ref: "pln-example", Version: 7})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	if value["version"] != float64(7) {
		t.Fatalf("version не преобразован в JSON number: %#v", value)
	}
	for _, key := range []string{"operations", "validationProblems", "nextActions"} {
		if items, ok := value[key].([]any); !ok || len(items) != 0 {
			t.Fatalf("пустая обязательная коллекция %s не материализована: %#v", key, value)
		}
	}
	if _, exists := value["projectRef"]; exists {
		t.Fatalf("отсутствующая optional ссылка ошибочно материализована: %#v", value)
	}
}

func TestMessageMapMaterializesZeroTokenUsage(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.Run{
		Ref:   "run-example",
		Usage: &controlplanev1.TokenUsage{},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	usage, ok := value["usage"].(map[string]any)
	if !ok {
		t.Fatalf("нулевой TokenUsage не материализован: %#v", value)
	}
	for _, key := range []string{
		"totalTokens",
		"inputTokens",
		"cachedInputTokens",
		"cacheWriteInputTokens",
		"outputTokens",
		"reasoningOutputTokens",
		"modelContextWindow",
	} {
		if usage[key] != float64(0) {
			t.Fatalf("нулевое поле %s не материализовано как JSON number: %#v", key, usage)
		}
	}
}

func TestMessageMapMaterializesRequiredEmptyRuntimeConfigurationStrings(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.AgentRuntimeConfigurationView{
		PublishedOverlay: &controlplanev1.ConfigOverlayVersion{
			Ref: "cov-example", Version: 1, Revision: 1, State: "PUBLISHED",
		},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	if value["safeEffectiveConfig"] != "" {
		t.Fatalf("safeEffectiveConfig не материализован: %#v", value)
	}
	overlay, ok := value["publishedOverlay"].(map[string]any)
	if !ok || overlay["content"] != "" {
		t.Fatalf("пустой published overlay не материализован: %#v", value)
	}
}

func TestMessageMapDoesNotMaterializeAbsentOptionalScalar(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.MutationContext{IdempotencyKey: "idem-example"})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	if _, exists := value["expectedVersion"]; exists {
		t.Fatalf("отсутствующее optional поле ошибочно материализовано: %#v", value)
	}
}

func TestMessageMapMaterializesNestedEmptyCollections(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.ListAssistantConversationsResponse{
		Conversations: []*controlplanev1.AssistantConversation{{Ref: "cnv-example", Version: 1}},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	conversations, ok := value["conversations"].([]any)
	if !ok || len(conversations) != 1 {
		t.Fatalf("conversation list искажён: %#v", value)
	}
	conversation := conversations[0].(map[string]any)
	if turns, ok := conversation["turns"].([]any); !ok || len(turns) != 0 {
		t.Fatalf("пустой обязательный turns не материализован: %#v", conversation)
	}
}

func TestMessageMapNormalizesAssistantConversationToOpenAPIShape(t *testing.T) {
	t.Parallel()

	parameters, err := structpb.NewStruct(map[string]any{"name": "Продажи"})
	if err != nil {
		t.Fatalf("create parameters: %v", err)
	}
	value, err := messageMap(&controlplanev1.AssistantConversation{
		Ref: "cnv-example", Version: 3, Title: "Диалог", TitleSource: "SERVER_DEFAULT", TitleRevision: 1,
		Context: &controlplanev1.AssistantContextDescriptor{Route: "/onboarding"},
		Turns: []*controlplanev1.AssistantTurn{{
			Ref: "pln-example", Sequence: 2, Role: "ASSISTANT", State: "COMPLETED",
			Plan: &controlplanev1.AssistantPlan{
				Ref: "pln-example", Version: 1, Revision: 1, ConversationRef: "cnv-example",
				State: controlplanev1.AssistantPlanState_ASSISTANT_PLAN_STATE_DRAFT, AuditSummary: "Создать проект",
				ContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Operations: []*controlplanev1.AssistantPlanOperation{{
					Ref: "operation-001", Type: controlplanev1.AssistantPlanOperation_TYPE_CREATE_PROJECT,
					Action: controlplanev1.AssistantPlanOperation_ACTION_CREATE, TargetKind: "PROJECT", TargetName: "Продажи",
					Parameters: parameters,
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	context := value["context"].(map[string]any)
	for _, key := range []string{"entityKind", "entityRef", "entityName"} {
		if context[key] != "" {
			t.Fatalf("required assistant context field %s = %#v", key, context[key])
		}
	}
	turn := value["turns"].([]any)[0].(map[string]any)
	plan := turn["plan"].(map[string]any)
	if plan["state"] != "DRAFT" || plan["applied"] != false {
		t.Fatalf("assistant plan state is not public: %#v", plan)
	}
	operation := plan["operations"].([]any)[0].(map[string]any)
	if operation["type"] != "CREATE_PROJECT" || operation["action"] != "CREATE" {
		t.Fatalf("assistant operation enums are not public: %#v", operation)
	}
	target := operation["target"].(map[string]any)
	if target["kind"] != "PROJECT" || target["name"] != "Продажи" {
		t.Fatalf("assistant operation target is invalid: %#v", target)
	}
	if !reflect.DeepEqual(operation["parameters"], map[string]any{"name": "Продажи"}) {
		t.Fatalf("protobuf Struct was corrupted: %#v", operation["parameters"])
	}
	if problems, ok := operation["validationProblems"].([]any); !ok || len(problems) != 0 {
		t.Fatalf("assistant operation validation problems are invalid: %#v", operation)
	}
}

func TestMessageMapMaterializesEmptyAssistantPlanReceiptCollections(t *testing.T) {
	t.Parallel()

	value, err := messageMap(&controlplanev1.AssistantPlanReceipt{
		Ref: "rcp-example", PlanRef: "pln-example", PlanRevision: 1,
		Outcome: "APPLIED", Operations: []*controlplanev1.AssistantPlanOperationReceipt{{
			OperationRef: "operation-001", ResourceRef: "prj-example", Outcome: "APPLIED", AuditRef: "aud-example",
		}},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	operationReceipts, ok := value["operationReceipts"].([]any)
	if !ok || len(operationReceipts) != 1 {
		t.Fatalf("assistant receipt operations are not normalized: %#v", value)
	}
	if _, exists := value["operations"]; exists {
		t.Fatalf("internal receipt operations leaked into the public response: %#v", value)
	}
	for _, key := range []string{"conflicts", "auditRefs", "createdResourceRefs"} {
		if items, ok := value[key].([]any); !ok || len(items) != 0 {
			t.Fatalf("empty assistant receipt collection %s is not materialized: %#v", key, value)
		}
	}
}

func TestMessageMapPreservesRunNodeIdentityWhileNormalizingRunTarget(t *testing.T) {
	t.Parallel()
	value, err := messageMap(&controlplanev1.GetRunGraphResponse{
		Run: &controlplanev1.Run{
			Ref:    "run_example001",
			Target: targetProto("AGENT", "agt_example001"),
		},
		Graph: &controlplanev1.RunGraph{
			RunRef: "run_example001",
			Nodes: []*controlplanev1.RunNode{{
				Ref: "nod_example001", RunRef: "run_example001", AgentRef: "agt_example001",
			}},
		},
	})
	if err != nil {
		t.Fatalf("messageMap() error = %v", err)
	}
	run := value["run"].(map[string]any)
	target := run["target"].(map[string]any)
	if target["type"] != "AGENT" || target["ref"] != "agt_example001" {
		t.Fatalf("run target не нормализован: %#v", target)
	}
	nodes := value["graph"].(map[string]any)["nodes"].([]any)
	node := nodes[0].(map[string]any)
	if node["ref"] != "nod_example001" || node["agentRef"] != "agt_example001" {
		t.Fatalf("identity agent node искажена: %#v", node)
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
