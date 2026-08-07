package controlplaneapi

import (
	"strings"
	"testing"
)

func TestWorkspaceMattermostMappingIntentIsStableAndBounded(t *testing.T) {
	input := WorkspaceMattermostMappingIntent{
		ActorID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", OrganizationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProjectID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", WorkspaceID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		Action: "relink", MappingID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		ExpectedVersion: 3, ExpectedGeneration: 2, ProviderTeamRef: "mattermost-team-two",
		ProviderObjectRef: "mattermost-team-two", EffectGeneration: 8, EffectSHA256: strings.Repeat("a", 64),
	}
	first, err := WorkspaceMattermostMappingIntentSHA256(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := WorkspaceMattermostMappingIntentSHA256(input)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("canonical digest mismatch: first=%q second=%q err=%v", first, second, err)
	}
	input.ProviderTeamRef = "mattermost-team-three"
	changed, err := WorkspaceMattermostMappingIntentSHA256(input)
	if err != nil || changed == first {
		t.Fatalf("provider target was not bound: changed=%q err=%v", changed, err)
	}
}
