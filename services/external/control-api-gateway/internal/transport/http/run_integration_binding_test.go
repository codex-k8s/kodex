package httptransport

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func TestRunIntegrationBindingProjection(t *testing.T) {
	for _, summary := range []string{"i18n:INTEGRATION_ACTION_SUCCEEDED", "i18n:INTEGRATION_ACTION_FAILED", "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN"} {
		event := &cp.RunEvent{Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, Summary: summary, IntegrationInvocationRef: proto.String("inv_fixture01")}
		got, err := messageMap(event)
		if err != nil || got["integrationInvocationRef"] != "inv_fixture01" {
			t.Fatal("valid binding lost", err)
		}
	}
	historical, err := messageMap(&cp.RunEvent{Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, Summary: "i18n:INTEGRATION_ACTION_SUCCEEDED"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := historical["integrationInvocationRef"]; present {
		t.Fatal("historical binding invented")
	}
	for _, ref := range []string{"", "evt_fixture01", "inv_", "inv_private/value", "inv_" + strings.Repeat("a", 93)} {
		if _, err := messageMap(&cp.RunEvent{Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, Summary: "i18n:INTEGRATION_ACTION_SUCCEEDED", IntegrationInvocationRef: &ref}); err == nil {
			t.Fatal("invalid ref accepted")
		}
	}
	for _, event := range []*cp.RunEvent{
		{Type: cp.RunEventType_RUN_EVENT_TYPE_TOOL_CALL_RECORDED, Summary: "i18n:INTEGRATION_ACTION_SUCCEEDED", IntegrationInvocationRef: proto.String("inv_fixture01")},
		{Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, Summary: "i18n:INTEGRATION_ACTION_STARTED", IntegrationInvocationRef: proto.String("inv_fixture01")},
	} {
		if _, err := messageMap(event); err == nil {
			t.Fatal("non-completion binding accepted")
		}
	}
}
