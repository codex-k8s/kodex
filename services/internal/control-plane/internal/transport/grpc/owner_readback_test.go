package grpc

import (
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
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

func TestResourceNextActionsComeFromClosedLifecycle(t *testing.T) {
	active := entity.Resource{Kind: enum.KindChat, State: enum.StateActive,
		Spec: entity.ChatSpec{Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"}}}
	got := resourceNextActionsToProto(active)
	want := []string{"RESOURCE_NEXT_ACTION_UPDATE", "RESOURCE_NEXT_ACTION_PAUSE", "RESOURCE_NEXT_ACTION_ARCHIVE", "RESOURCE_NEXT_ACTION_DELETE"}
	if len(got) != len(want) {
		t.Fatalf("active chat actions: got %v want %v", got, want)
	}
	for index := range want {
		if got[index].String() != want[index] {
			t.Fatalf("active chat action %d: got %s want %s", index, got[index], want[index])
		}
	}

	gitOwned := active
	gitOwned.Kind = enum.KindTeam
	gitOwned.Spec = entity.TeamSpec{Ownership: entity.ConfigurationOwnership{
		ManagedBy: "GIT", SourceRef: "git://owner/team", SourceRevision: 7,
	}}
	gitActions := resourceNextActionsToProto(gitOwned)
	if len(gitActions) != 2 || gitActions[0].String() != "RESOURCE_NEXT_ACTION_DETACH" ||
		gitActions[1].String() != "RESOURCE_NEXT_ACTION_COPY" {
		t.Fatalf("git-owned access actions: %v", gitActions)
	}
}
