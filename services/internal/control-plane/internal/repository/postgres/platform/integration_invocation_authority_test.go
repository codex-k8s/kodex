package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

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

func TestIntegrationGateContextSummary(t *testing.T) {
	if got := integrationGateContextSummary("Тестовое подключение", "Изменить запись", "write"); got != "Тестовое подключение · Изменить запись" {
		t.Fatalf("unexpected gate context summary: %q", got)
	}
	if got := integrationGateContextSummary("Тестовое подключение", "", "write"); got != "Тестовое подключение · write" {
		t.Fatalf("unexpected fallback gate context summary: %q", got)
	}
}
