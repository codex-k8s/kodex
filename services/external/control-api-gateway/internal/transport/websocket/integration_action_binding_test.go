package websockettransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestIntegrationActionBindingRealtimeProjection preserves a valid invocation ref and localized summary, keeps a historical absent binding absent and rejects a ref from the wrong resource namespace.
// TestIntegrationActionBindingRealtimeProjection сохраняет допустимую ссылку вызова и локализованную сводку, не выдумывает отсутствующую историческую привязку и отклоняет ссылку другого пространства ресурсов.
func TestIntegrationActionBindingRealtimeProjection(t *testing.T) {
	event := &cp.RunEvent{Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, Summary: "i18n:INTEGRATION_ACTION_SUCCEEDED", IntegrationInvocationRef: proto.String("inv_fixture01")}
	got, err := projectRunEvent(event, func(string) string { return "Действие выполнено" })
	if err != nil || got.IntegrationInvocationRef == nil || *got.IntegrationInvocationRef != "inv_fixture01" || got.Summary != "Действие выполнено" {
		t.Fatal("realtime binding or localization lost", err)
	}
	event.IntegrationInvocationRef = nil
	got, err = projectRunEvent(event, func(s string) string { return s })
	if err != nil || got.IntegrationInvocationRef != nil {
		t.Fatal("historical binding invented", err)
	}
	event.IntegrationInvocationRef = proto.String("evt_fixture01")
	if _, err := projectRunEvent(event, func(s string) string { return s }); err == nil {
		t.Fatal("wrong binding passed realtime projection")
	}
}
