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
