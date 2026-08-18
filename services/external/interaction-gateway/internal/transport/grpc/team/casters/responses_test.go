package casters

import (
	"bytes"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestTeamViewDoesNotExposeProviderTeamID(t *testing.T) {
	view := TeamView(entity.MattermostTeam{
		ProviderTeamID: "private-provider-team-id", Selector: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		DisplayName: "Owner Workspace", Slug: "owner-workspace", Status: enum.MattermostTeamActive,
		ProviderSnapshotSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CreatedAt:              time.Now(), UpdatedAt: time.Now(), ObservedAt: time.Now(),
	})
	raw, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private-provider-team-id")) {
		t.Fatalf("provider Team ID leaked into response: %s", raw)
	}
}

func TestMappingOperationViewMasksProviderAndRawFailure(t *testing.T) {
	view := MappingOperationView(entity.WorkspaceMappingOperation{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Action: "bind",
		State: "REPAIR_REQUIRED", FailureCode: "private provider: team-id-secret",
		Team: entity.MattermostTeam{
			ProviderTeamID: "private-provider-team-id", Selector: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
			DisplayName: "Owner Workspace", Slug: "owner-workspace", Status: enum.MattermostTeamActive,
			ProviderSnapshotSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			CreatedAt:              time.Now(), UpdatedAt: time.Now(), ObservedAt: time.Now(),
		},
		Result: entity.WorkspaceMattermostMapping{
			ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Version: 1, Generation: 1, State: "BOUND",
			ProviderTeamID: "private-provider-team-id", ProviderEffectVersion: 1, ProviderEffectGeneration: 1,
			ProviderObservedAt: time.Now(), UpdatedAt: time.Now(),
		},
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	raw, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private-provider-team-id")) || bytes.Contains(raw, []byte("team-id-secret")) ||
		!bytes.Contains(raw, []byte("SAFE_FAILURE")) {
		t.Fatalf("unsafe mapping operation response: %s", raw)
	}
	if view.GetResult().GetTeam().GetSelector() != "cccccccc-cccc-4ccc-8ccc-cccccccccccc" {
		t.Fatalf("terminal mapping operation omitted safe Team result: %#v", view.GetResult())
	}
}
