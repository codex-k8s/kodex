package mattermost

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	model "github.com/mattermost/mattermost/server/public/model"
)

func TestCreatedTeamCausalityRejectsHijackAndRacedObject(t *testing.T) {
	intent := entity.MattermostTeamCreateIntent{
		DisplayName: "Owner Workspace", Slug: "owner-workspace-dddddddddddd",
		ProviderCorrelation: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
	}
	created := &model.Team{
		Id: "provider-team-one", Name: intent.Slug, DisplayName: intent.DisplayName,
		Description: providerOperationMarker(intent.ProviderCorrelation), Type: model.TeamOpen,
	}
	if !createdTeamMatches(created, intent) {
		t.Fatal("exact provider causality marker was rejected")
	}
	hijacked := *created
	hijacked.Name = "owner-workspace"
	hijacked.Description = ""
	if createdTeamMatches(&hijacked, intent) {
		t.Fatal("predictable slug/display Team without causality marker was adopted")
	}
	raced := *created
	raced.Description = providerOperationMarker("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	if createdTeamMatches(&raced, intent) {
		t.Fatal("raced Team owned by another operation was adopted")
	}
}
