package websockettransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestCancellationServiceCodeRealtimeProjection(t *testing.T) {
	for _, locale := range []struct{ name, summary string }{{"ru", "Запуск отменён"}, {"en", "Run cancelled"}} {
		t.Run(locale.name, func(t *testing.T) {
			event := &cp.RunEvent{
				Type:        cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS,
				MessageKind: cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_INTERMEDIATE_MESSAGE,
				RunState:    cp.RunState_RUN_STATE_CANCELLED, NodeState: cp.RunNodeState_RUN_NODE_STATE_CANCELLED,
				Summary:   "i18n:RUN_CANCELLED",
				Execution: &cp.RunEventExecution{RunRef: "run_fixture01", NodeRef: "nod_fixture01", SessionRef: "ses_fixture01", TurnRef: "trn_fixture01", TurnNumber: 11, Attempt: 1},
			}
			got, err := projectRunEvent(event, func(string) string { return locale.summary })
			if err != nil || got.ServiceCode == nil || string(*got.ServiceCode) != "RUN_CANCELLED" || got.Summary != locale.summary {
				t.Fatal("realtime cancellation code or localized summary lost", err)
			}
			if got.Execution == nil || got.Execution.RunRef != event.Execution.RunRef || got.Execution.NodeRef != event.Execution.NodeRef || got.Execution.SessionRef != event.Execution.SessionRef || got.Execution.TurnRef != event.Execution.TurnRef || got.Execution.TurnNumber != 11 || got.Execution.Attempt != 1 {
				t.Fatal("realtime projection changed the exact execution")
			}
			event.Summary = "i18n:FUTURE_CANCELLATION"
			got, err = projectRunEvent(event, func(string) string { return locale.summary })
			if err != nil || got.ServiceCode != nil {
				t.Fatal("unknown cancellation acquired a service code", err)
			}
		})
	}
}

func TestRootCompletionServiceCodeRealtimeProjection(t *testing.T) {
	for _, summary := range []string{"Корневой процесс завершён", "Root workflow completed"} {
		event := &cp.RunEvent{
			Type:        cp.RunEventType_RUN_EVENT_TYPE_NODE_STATE_CHANGED,
			MessageKind: cp.RunEventMessageKind_RUN_EVENT_MESSAGE_KIND_STATE,
			Summary:     "i18n:ROOT_PROCESS_COMPLETED", NodeRef: "nod_fixture01",
			NodeState: cp.RunNodeState_RUN_NODE_STATE_FAILED,
			Node:      &cp.RunNode{Ref: "nod_fixture01", Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS, State: cp.RunNodeState_RUN_NODE_STATE_FAILED},
		}
		got, err := projectRunEvent(event, func(string) string { return summary })
		if err != nil || got.ServiceCode == nil || string(*got.ServiceCode) != "ROOT_PROCESS_COMPLETED" || got.Summary != summary {
			t.Fatal("realtime root completion discriminator or localized summary lost", err)
		}
		event.Summary = "i18n:RUNTIME_WORKFLOW_INCOMPLETE"
		got, err = projectRunEvent(event, func(string) string { return summary })
		if err != nil || got.ServiceCode != nil {
			t.Fatal("different root reason acquired a service code", err)
		}
	}
}
