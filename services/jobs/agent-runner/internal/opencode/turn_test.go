package opencode

import (
	"encoding/json"
	"fmt"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"testing"
)

// nativeObservation serializes only synthetic native properties for observer contract tests.
// nativeObservation сериализует только synthetic native properties для observer contract tests.
func nativeObservation(kind string, properties any) Event {
	raw, _ := json.Marshal(properties)
	return Event{Type: kind, Properties: raw}
}

// TestObserverSeparatesSummaryAndDisjointUsage checks the event consumer without replacing native qualification.
// TestObserverSeparatesSummaryAndDisjointUsage проверяет event consumer, не заменяя native qualification.
func TestObserverSeparatesSummaryAndDisjointUsage(t *testing.T) {
	comments := 0
	var measurements []runtimecontract.ModelMeasurement
	observer := turnObserver{measurement: func(value runtimecontract.ModelMeasurement) error {
		measurements = append(measurements, value)
		return nil
	}, sessionID: "ses_native", active: map[string]*observedMessage{}, allowTool: func(name string) bool { return name == "kodex_counter" }, commentary: func(_, text string) error { comments++; return nil }}
	for _, event := range []Event{
		nativeObservation("message.updated", map[string]any{"info": map[string]any{"id": "msg_summary", "sessionID": "ses_native", "role": "assistant", "summary": true}}),
		nativeObservation("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_text", "messageID": "msg_summary", "sessionID": "ses_native", "type": "text", "text": "not final", "time": map[string]int{"end": 1}}}),
		nativeObservation("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_usage", "messageID": "msg_summary", "sessionID": "ses_native", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 5, "reasoning": 2, "cache": map[string]int{"read": 20, "write": 3}}}}),
		nativeObservation("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_usage", "messageID": "msg_summary", "sessionID": "ses_native", "type": "step-finish", "tokens": map[string]any{"input": 10, "output": 5, "reasoning": 2, "cache": map[string]int{"read": 20, "write": 3}}}}),
		nativeObservation("message.updated", map[string]any{"info": map[string]any{"id": "msg_summary", "sessionID": "ses_native", "role": "assistant", "summary": true, "time": map[string]int{"completed": 1}}}),
	} {
		if err := observer.consume(event); err != nil {
			t.Fatal(err)
		}
	}
	if comments != 0 || len(observer.active) != 0 || observer.result.Usage.InputTokens != 33 || observer.result.Usage.OutputTokens != 7 || observer.result.Usage.TotalTokens != 40 {
		t.Fatal("summary publication, event retention or native accounting is invalid")
	}
	if len(measurements) != 1 || measurements[0].Validate() != nil || measurements[0].Phase != "COMPACTION" || measurements[0].Usage.CacheWriteInputTokens != 3 || measurements[0].TTFTMS != nil {
		t.Fatal("native observation duplicated or invented transport timing")
	}
	observer.active["msg_tool"] = &observedMessage{parts: map[string]bool{}}
	if err := observer.consume(nativeObservation("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_shell", "messageID": "msg_tool", "sessionID": "ses_native", "type": "tool", "tool": "bash"}})); err == nil {
		t.Fatal("unmanaged native tool accepted")
	}
}

// TestFinalTextRejectsSummaryErrorsAndIncompleteOutput closes false success at the native POST boundary.
// TestFinalTextRejectsSummaryErrorsAndIncompleteOutput запрещает ложный успех на границе native POST.
func TestFinalTextRejectsSummaryErrorsAndIncompleteOutput(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"summary", func(info map[string]any) { info["summary"] = true }},
		{"error", func(info map[string]any) { info["error"] = map[string]string{"name": "synthetic"} }},
		{"incomplete", func(info map[string]any) { delete(info, "time") }},
		{"length", func(info map[string]any) { info["finish"] = "length" }},
		{"foreign", func(info map[string]any) { info["sessionID"] = "ses_foreign" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := map[string]any{"sessionID": "ses_native", "role": "assistant", "finish": "stop", "time": map[string]int{"completed": 1}}
			test.change(info)
			raw, _ := json.Marshal(info)
			_, err := finalText(Message{Info: raw, Parts: []json.RawMessage{json.RawMessage(`{"type":"text","text":"synthetic final"}`)}}, "ses_native")
			if err == nil {
				t.Fatal("false native success accepted")
			}
		})
	}
}

// TestObserverJoinsEarlyCompletionWithUsage reproduces the pinned server's mutable event snapshot.
// It requires each final signal to follow usage, while duplicate events cannot count a second step.
// TestObserverJoinsEarlyCompletionWithUsage воспроизводит изменяемый снимок события закреплённого сервера.
// Итоговый сигнал следует после usage; повторные события не учитывают шаг повторно.
func TestObserverJoinsEarlyCompletionWithUsage(t *testing.T) {
	completed := 0
	observer := turnObserver{sessionID: "ses_native", active: map[string]*observedMessage{}, allowTool: func(string) bool { return true }, commentary: func(string, string) error { return nil }, completed: func(string) error { completed++; return nil }}
	message := nativeObservation("message.updated", map[string]any{"info": map[string]any{"id": "msg_final", "sessionID": "ses_native", "role": "assistant", "time": map[string]int{"completed": 1}}})
	part := nativeObservation("message.part.updated", map[string]any{"part": map[string]any{"id": "prt_finish", "messageID": "msg_final", "sessionID": "ses_native", "type": "step-finish", "tokens": map[string]int{"input": 10, "output": 24}}})
	for _, event := range []Event{message, message} {
		if err := observer.consume(event); err != nil {
			t.Fatal(err)
		}
	}
	if completed != 0 {
		t.Fatal("completion preceded measured step")
	}
	for _, event := range []Event{part, message, part, message} {
		if err := observer.consume(event); err != nil {
			t.Fatal(err)
		}
	}
	if completed != 1 || observer.result.Usage.OutputTokens != 24 || len(observer.active) != 0 {
		t.Fatal("completion and usage were not joined exactly once")
	}
}

// TestObserverBoundsCompletionMemory crosses the old event lifetime limit with constant retained state.
// TestObserverBoundsCompletionMemory проходит прежний предел событий с постоянным объёмом состояния.
func TestObserverBoundsCompletionMemory(t *testing.T) {
	observer := turnObserver{sessionID: "ses_native", active: map[string]*observedMessage{}, allowTool: func(string) bool { return true }, commentary: func(string, string) error { return nil }}
	for i := 0; i < 4097; i++ {
		id := fmt.Sprintf("msg_%d", i)
		message := nativeObservation("message.updated", map[string]any{"info": map[string]any{"id": id, "sessionID": "ses_native", "role": "assistant", "summary": true, "time": map[string]int{"completed": 1}}})
		part := nativeObservation("message.part.updated", map[string]any{"part": map[string]any{"id": fmt.Sprintf("prt_%d", i), "messageID": id, "sessionID": "ses_native", "type": "step-finish", "tokens": map[string]int{"output": 1}}})
		for _, event := range []Event{message, part, message, part} {
			if err := observer.consume(event); err != nil {
				t.Fatal(err)
			}
		}
		if len(observer.active) != 0 || len(observer.retired) > 64 {
			t.Fatal("completed message state grew beyond its bound")
		}
	}
	if observer.result.Usage.OutputTokens != 4097 {
		t.Fatal("long event stream lost measured steps")
	}
}
