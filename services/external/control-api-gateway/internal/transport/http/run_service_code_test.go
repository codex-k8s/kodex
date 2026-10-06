package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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

func cancellationProjectionEvent(code string) *cp.RunEvent {
	return &cp.RunEvent{
		Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, MessageKind: cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_INTERMEDIATE_MESSAGE,
		Summary: "i18n:" + code, RunState: cp.RunState_RUN_STATE_CANCELLED,
	}
}

func TestRunCancellationServiceCodeDoesNotClassifyContent(t *testing.T) {
	for _, summary := range []string{"RUN_CANCELLED", "Запуск отменён", "Run cancelled", "i18n:RUN_CANCELLED extra", "i18n:FUTURE_CANCELLATION"} {
		event := cancellationProjectionEvent("RUN_CANCELLED")
		event.Summary = summary
		assertNoCancellationServiceCode(t, event)
	}
	for _, mutate := range []func(*cp.RunEvent){
		func(event *cp.RunEvent) { event.Message = &cp.RunMessage{Text: "i18n:RUN_CANCELLED"} },
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
	got, err := messageMap(&cp.RunMessage{Text: "i18n:RUN_CANCELLED"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["serviceCode"]; present {
		t.Fatal("service code escaped the RunEvent descriptor")
	}
}

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
