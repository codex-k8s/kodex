package grpc

import (
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
)

func TestRuntimeIncidentOwnerProjectionPreservesExecutionFence(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	projection := runtimeIncidentOwnerProjectionToProto(resource.RuntimeIncidentOwnerProjection{
		IncidentRef: "incident:owner-safe", Version: 9, ExecutionFence: 47,
		Kind: "HEARTBEAT_MISSED", State: "ACKNOWLEDGED", Severity: "ERROR",
		Impact: "Выполнение остановлено", OccurredAt: now, UpdatedAt: now,
		NextActions: []string{"RETRY", "CLOSE"},
	})
	if projection.GetVersion() != 9 || projection.GetExecutionFence() != 47 {
		t.Fatalf("incident version/fence was lost: %#v", projection)
	}
	if len(projection.GetNextActions()) != 2 {
		t.Fatalf("incident nextActions were lost: %#v", projection.GetNextActions())
	}
}
