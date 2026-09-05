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
