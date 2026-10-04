package grpc

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestCastIntegrationActionBinding(t *testing.T) {
	for _, ref := range []string{"", "inv_fixture01"} {
		got := castEvent(entity.RunEvent{Type: "TURN_PROGRESS", Summary: "i18n:INTEGRATION_ACTION_SUCCEEDED", Delta: entity.RunEventDelta{IntegrationInvocationRef: ref}})
		if got.GetIntegrationInvocationRef() != ref || (got.IntegrationInvocationRef != nil) != (ref != "") {
			t.Fatal("typed binding presence changed")
		}
	}
}
