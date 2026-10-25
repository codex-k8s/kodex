package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestIntegrationExecutionRoute accepts only the explicit integration/interaction workload routes
// and denies empty, unrelated or route-name callers.
// TestIntegrationExecutionRoute принимает только явные маршруты workload integration и interaction
// и отклоняет пустых, посторонних вызывающих и имя маршрута вместо workload.
func TestIntegrationExecutionRoute(t *testing.T) {
	for workload, want := range map[string]string{"integration-gateway": "MANAGED_MCP", "interaction-gateway": "INTERACTION"} {
		got, err := integrationExecutionRoute(workload)
		if err != nil || got != want {
			t.Fatalf("route %s: %q %v", workload, got, err)
		}
	}
	for _, workload := range []string{"", "runtime-controller", "control-api-gateway", "INTERACTION"} {
		if _, err := integrationExecutionRoute(workload); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("unexpected workload %q: %v", workload, err)
		}
	}
}

// TestIntegrationActionOutcomeMessage checks exact localization keys for successful, failed and
// unknown integration outcomes.
// TestIntegrationActionOutcomeMessage проверяет точные ключи локализации успешного, ошибочного и
// неизвестного результата интеграции.
func TestIntegrationActionOutcomeMessage(t *testing.T) {
	for state, want := range map[string]string{
		"SUCCEEDED":       "i18n:INTEGRATION_ACTION_SUCCEEDED",
		"FAILED":          "i18n:INTEGRATION_ACTION_FAILED",
		"UNKNOWN_OUTCOME": "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN",
	} {
		if got := integrationActionOutcomeMessage(state); got != want {
			t.Errorf("state %s: got %q, want %q", state, got, want)
		}
	}
}

// TestIntegrationGateContextSummary checks connection and operation names form the approval
// summary, falling back to the operation key when its display name is absent.
// TestIntegrationGateContextSummary проверяет составление сводки подтверждения из имён соединения
// и операции с возвратом к ключу операции при отсутствии её отображаемого имени.
func TestIntegrationGateContextSummary(t *testing.T) {
	if got := integrationGateContextSummary("Тестовое подключение", "Изменить запись", "write"); got != "Тестовое подключение · Изменить запись" {
		t.Fatalf("unexpected gate context summary: %q", got)
	}
	if got := integrationGateContextSummary("Тестовое подключение", "", "write"); got != "Тестовое подключение · write" {
		t.Fatalf("unexpected fallback gate context summary: %q", got)
	}
}
