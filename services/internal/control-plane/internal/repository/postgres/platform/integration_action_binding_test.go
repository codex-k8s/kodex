package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestIntegrationActionBinding allows only terminal integration outcome messages on TURN_PROGRESS
// with a bounded well-formed invocation reference.
// TestIntegrationActionBinding разрешает только сообщения завершённых исходов интеграции в
// TURN_PROGRESS с ограниченным корректным ref вызова.
func TestIntegrationActionBinding(t *testing.T) {
	for _, state := range []string{"SUCCEEDED", "FAILED", "UNKNOWN_OUTCOME"} {
		if !validIntegrationActionBinding("TURN_PROGRESS", integrationActionOutcomeMessage(state), "inv_fixture01") {
			t.Fatal("owner completion binding rejected")
		}
	}
	for _, fixture := range []struct{ kind, summary, ref string }{
		{"TOOL_CALL_RECORDED", "i18n:INTEGRATION_ACTION_SUCCEEDED", "inv_fixture01"},
		{"TURN_PROGRESS", "i18n:INTEGRATION_ACTION_STARTED", "inv_fixture01"},
		{"TURN_PROGRESS", "i18n:INTEGRATION_ACTION_SUCCEEDED", "evt_fixture01"},
		{"TURN_PROGRESS", "i18n:INTEGRATION_ACTION_SUCCEEDED", "inv_"},
		{"TURN_PROGRESS", "i18n:INTEGRATION_ACTION_SUCCEEDED", "inv_private/value"},
		{"TURN_PROGRESS", "i18n:INTEGRATION_ACTION_SUCCEEDED", "inv_" + strings.Repeat("a", 93)},
	} {
		if validIntegrationActionBinding(fixture.kind, fixture.summary, fixture.ref) {
			t.Fatal("invalid integration binding accepted")
		}
	}
}

// TestIntegrationActionBindingPersistedDelta checks empty and present invocation references
// survive event-delta JSON round-trips without inventing the historical field marker.
// TestIntegrationActionBindingPersistedDelta проверяет сохранение отсутствующего и присутствующего
// ref вызова при JSON-кодировании и декодировании event delta без добавления исторического маркера
// поля.
func TestIntegrationActionBindingPersistedDelta(t *testing.T) {
	for _, ref := range []string{"", "inv_fixture01"} {
		encoded, err := json.Marshal(entity.RunEventDelta{IntegrationInvocationRef: ref})
		if err != nil {
			t.Fatal(err)
		}
		var restored entity.RunEventDelta
		if err := json.Unmarshal(encoded, &restored); err != nil || restored.IntegrationInvocationRef != ref {
			t.Fatal("persisted binding changed")
		}
		if ref == "" && strings.Contains(string(encoded), "IntegrationInvocationRef") {
			t.Fatal("historical absence must remain absent")
		}
	}
}
