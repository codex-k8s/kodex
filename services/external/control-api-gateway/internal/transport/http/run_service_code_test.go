package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// TestRunCancellationServiceCodeProjection checks each canonical cancellation summary gains its
// closed machine code, survives localization and leaves the original protobuf event unchanged.
// TestRunCancellationServiceCodeProjection проверяет назначение закрытого машинного кода каждой
// канонической сводке отмены, его сохранение при локализации и неизменность исходного protobuf
// события.
func TestRunCancellationServiceCodeProjection(t *testing.T) {
	for _, code := range []string{"RUN_CANCELLED", "RUN_NODE_CANCELLED", "ASSISTANT_TURN_CANCELLED"} {
		t.Run(code, func(t *testing.T) {
			event := cancellationProjectionEvent(code)
			got, err := messageMap(event)
			if err != nil || got["serviceCode"] != code {
				t.Fatal("closed cancellation service code is missing", err)
			}
			LocalizeSafeErrors(got, func(string) string { return "Запуск отменён" })
			if got["serviceCode"] != code || got["summary"] != "Запуск отменён" {
				t.Fatal("localization changed the machine code or lost the summary")
			}
			if event.Summary != "i18n:"+code {
				t.Fatal("projection changed the source event")
			}
		})
	}
}

// cancellationProjectionEvent builds a minimal synthetic cancelled turn-progress intermediate
// event with a selected canonical i18n summary, without content-bearing fields.
// cancellationProjectionEvent собирает минимальное синтетическое cancelled turn-progress
// intermediate событие с выбранной канонической i18n сводкой без полей содержимого.
func cancellationProjectionEvent(code string) *cp.RunEvent {
	return &cp.RunEvent{
		Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, MessageKind: cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_INTERMEDIATE_MESSAGE,
		Summary: "i18n:" + code, RunState: cp.RunState_RUN_STATE_CANCELLED,
	}
}

// TestRootCompletionServiceCodeProjection checks all terminal root states receive the canonical
// completion code and rejects mismatched node/type/state, content-bearing and noncanonical
// completion events.
// TestRootCompletionServiceCodeProjection проверяет канонический код завершения для всех terminal
// root состояний и отказ для несовпадающих node/type/state, содержательных и неканонических
// событий завершения.
func TestRootCompletionServiceCodeProjection(t *testing.T) {
	for _, state := range []cp.RunNodeState{cp.RunNodeState_RUN_NODE_STATE_FAILED, cp.RunNodeState_RUN_NODE_STATE_SUCCEEDED, cp.RunNodeState_RUN_NODE_STATE_CANCELLED} {
		event := rootCompletionProjectionEvent()
		event.NodeState, event.Node.State = state, state
		got, err := messageMap(event)
		if err != nil || got["serviceCode"] != "ROOT_PROCESS_COMPLETED" {
			t.Fatal("closed root completion code is missing", err)
		}
		LocalizeSafeErrors(got, func(string) string { return "Корневой процесс завершён" })
		if got["serviceCode"] != "ROOT_PROCESS_COMPLETED" || got["summary"] != "Корневой процесс завершён" || event.Summary != "i18n:ROOT_PROCESS_COMPLETED" {
			t.Fatal("localization lost the discriminator or changed the source")
		}
	}
	for _, mutate := range []func(*cp.RunEvent){
		func(e *cp.RunEvent) { e.Summary = "Корневой процесс завершён" },
		func(e *cp.RunEvent) { e.Summary = "i18n:RUNTIME_WORKFLOW_INCOMPLETE" },
		func(e *cp.RunEvent) { e.Node.Type = cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION },
		func(e *cp.RunEvent) { e.Node = nil },
		func(e *cp.RunEvent) { e.Node.Ref = "nod_other" },
		func(e *cp.RunEvent) { e.Node.State = cp.RunNodeState_RUN_NODE_STATE_RUNNING },
		func(e *cp.RunEvent) { e.Type = cp.RunEventType_RUN_EVENT_TYPE_TURN_COMPLETED },
		func(e *cp.RunEvent) { e.MessageKind = cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_FINAL_MESSAGE },
		func(e *cp.RunEvent) {
			e.Message = &cp.RunMessage{Text: "Full final", Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_ORDINARY}}
		},
		func(e *cp.RunEvent) { e.Progress = "Different outcome" },
		func(e *cp.RunEvent) { e.GateRef = "gat_fixture" },
		func(e *cp.RunEvent) { e.Incident = &cp.Incident{} },
		func(e *cp.RunEvent) { e.ToolCall = &cp.RunToolCall{} },
		func(e *cp.RunEvent) { e.ArtifactRef = "art_fixture" },
	} {
		event := rootCompletionProjectionEvent()
		mutate(event)
		assertNoCancellationServiceCode(t, event)
	}
}

// rootCompletionProjectionEvent builds one matching synthetic failed root-process state event with
// identical outer/nested node coordinates and the canonical completion summary.
// rootCompletionProjectionEvent собирает согласованное синтетическое событие failed root-process с
// одинаковыми внешними и вложенными координатами узла и канонической сводкой завершения.
func rootCompletionProjectionEvent() *cp.RunEvent {
	return &cp.RunEvent{
		Type:        cp.RunEventType_RUN_EVENT_TYPE_NODE_STATE_CHANGED,
		MessageKind: cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_STATE,
		Summary:     "i18n:ROOT_PROCESS_COMPLETED",
		NodeRef:     "nod_rootfixture", NodeState: cp.RunNodeState_RUN_NODE_STATE_FAILED,
		Node: &cp.RunNode{Ref: "nod_rootfixture", Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS, State: cp.RunNodeState_RUN_NODE_STATE_FAILED},
	}
}

// TestRunCancellationServiceCodeDoesNotClassifyContent proves cancellation-looking text, unknown
// summaries, ordinary messages, tool/artifact/progress data and incompatible event/state kinds
// never acquire a service cancellation code.
// TestRunCancellationServiceCodeDoesNotClassifyContent доказывает, что похожий на отмену текст,
// неизвестные сводки, обычные сообщения, tool/artifact/progress и несовместимые event/state виды
// не получают служебный код отмены.
func TestRunCancellationServiceCodeDoesNotClassifyContent(t *testing.T) {
	for _, summary := range []string{"RUN_CANCELLED", "Запуск отменён", "Run cancelled", "i18n:RUN_CANCELLED extra", "i18n:FUTURE_CANCELLATION"} {
		event := cancellationProjectionEvent("RUN_CANCELLED")
		event.Summary = summary
		assertNoCancellationServiceCode(t, event)
	}
	for _, mutate := range []func(*cp.RunEvent){
		func(event *cp.RunEvent) {
			event.Message = &cp.RunMessage{Text: "i18n:RUN_CANCELLED", Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_ORDINARY}}
		},
		func(event *cp.RunEvent) { event.ToolCall = &cp.RunToolCall{} },
		func(event *cp.RunEvent) { event.ArtifactRef = "art_fixture001" },
		func(event *cp.RunEvent) { event.Progress = "Additional outcome" },
		func(event *cp.RunEvent) {
			event.MessageKind = cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_ASSISTANT_MESSAGE
		},
		func(event *cp.RunEvent) { event.Type = cp.RunEventType_RUN_EVENT_TYPE_TOOL_CALL_RECORDED },
		func(event *cp.RunEvent) { event.RunState = cp.RunState_RUN_STATE_RUNNING },
	} {
		event := cancellationProjectionEvent("RUN_CANCELLED")
		mutate(event)
		assertNoCancellationServiceCode(t, event)
	}
	got, err := messageMap(&cp.RunMessage{Text: "i18n:RUN_CANCELLED", Source: &cp.MessageSource{Origin: cp.MessageOrigin_MESSAGE_ORIGIN_ORDINARY}})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["serviceCode"]; present {
		t.Fatal("service code escaped the RunEvent descriptor")
	}
}

// TestRunCancellationServiceCodeNestedHTTPProjection checks nested owner event-list projection and
// localization preserve canonical cancellation codes for run/node state and turn-progress event
// shapes.
// TestRunCancellationServiceCodeNestedHTTPProjection проверяет сохранение канонических кодов
// отмены при вложенной проекции списка событий владельца и локализации для run/node state и
// turn-progress форм.
func TestRunCancellationServiceCodeNestedHTTPProjection(t *testing.T) {
	for _, shape := range []struct {
		kind        cp.RunEventType
		messageKind cp.RunEventMessageKind
		code        string
	}{
		{cp.RunEventType_RUN_EVENT_TYPE_RUN_STATE_CHANGED, cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_STATE, "RUN_CANCELLED"},
		{cp.RunEventType_RUN_EVENT_TYPE_NODE_STATE_CHANGED, cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_STATE, "RUN_NODE_CANCELLED"},
		{cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_INTERMEDIATE_MESSAGE, "RUN_CANCELLED"},
	} {
		event := cancellationProjectionEvent(shape.code)
		event.Type, event.MessageKind = shape.kind, shape.messageKind
		got, err := messageMap(&cp.ListRunEventsResponse{Events: []*cp.RunEvent{event}})
		if err != nil {
			t.Fatal(err)
		}
		LocalizeSafeErrors(got, func(string) string { return "Отменено" })
		projected := got["events"].([]any)[0].(map[string]any)
		if projected["serviceCode"] != shape.code || projected["summary"] != "Отменено" {
			t.Fatal("nested HTTP projection lost the cancellation code")
		}
	}
}

// TestRunCancellationServiceCodeHTTPOutput writes a localized event through the real HTTP message
// writer and decodes the generated DTO to verify status, localized summary and valid typed
// serviceCode.
// TestRunCancellationServiceCodeHTTPOutput выдаёт локализованное событие настоящим HTTP writer и
// декодирует generated DTO для проверки статуса, переведённой сводки и корректного typed
// serviceCode.
func TestRunCancellationServiceCodeHTTPOutput(t *testing.T) {
	writer := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
	writeMessage(writer, http.StatusOK, cancellationProjectionEvent("RUN_CANCELLED"), "", "")
	var event generated.RunEvent
	if err := json.Unmarshal(writer.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if writer.Code != http.StatusOK || event.ServiceCode == nil || !event.ServiceCode.Valid() || string(*event.ServiceCode) != "RUN_CANCELLED" || event.Summary != "localized:RUN_CANCELLED" {
		t.Fatal("HTTP wire output lost the closed service code or localized summary")
	}
}

// assertNoCancellationServiceCode uses the real message projection and fails if any serviceCode
// appears on the supplied content/unknown event. Helper attribution keeps failures at the caller
// scenario.
// assertNoCancellationServiceCode использует настоящую проекцию сообщения и отклоняет появление
// любого serviceCode у переданного содержательного или неизвестного события. Отметка Helper
// относит ошибку к сценарию caller.
func assertNoCancellationServiceCode(t *testing.T, event *cp.RunEvent) {
	t.Helper()
	got, err := messageMap(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["serviceCode"]; present {
		t.Fatal("content or an unknown event was classified as service cancellation")
	}
}
