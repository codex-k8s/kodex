package service

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestProjectRunCardLinksAllParentRunsAndTrigger(t *testing.T) {
	store := chatRuntimeStore()
	store.sessionTurns = []entity.AgentSessionTurn{
		{ID: 1, RunID: "parent-1", MattermostRunsPostID: "run-card-parent-1"},
		{ID: 2, RunID: "parent-2", MattermostRunsPostID: "run-card-parent-2"},
		{ID: 3, RunID: "child", MattermostChannelID: "work-channel", MattermostRootPostID: "work-root", MattermostPostID: "trigger-post", ParentTurnIDs: []int64{1, 2}, TriggerPostIDs: []string{"trigger-post", "trigger-post-2"}, InitiatorUserNames: []string{"manager", "architect"}},
	}
	publisher := &fakeThreadPublisher{}
	ref, err := upsertProjectRunCard(context.Background(), projectRunCardInput{
		Localizer:         testLocalizer(t, texti18n.DefaultLocale),
		Store:             store,
		Publisher:         publisher,
		MattermostSiteURL: "https://mattermost.example",
		Project:           entity.Project{ID: 1, Slug: "platform", MattermostRunsChannelID: "runs-channel"},
		Session:           entity.AgentSession{ID: 1, ProjectID: 1},
		Turn:              store.sessionTurns[2],
		RoleName:          "developer",
		OpenAIAccountName: "main",
		Status:            agentSessionTurnQueued,
	})
	if err != nil {
		t.Fatalf("upsertProjectRunCard() error = %v", err)
	}
	if ref.PostID != "card-" || len(publisher.cards) != 1 {
		t.Fatalf("ref=%#v cards=%#v", ref, publisher.cards)
	}
	card := publisher.cards[0]
	joined := ""
	for _, field := range card.Fields {
		joined += "\n" + field.Value
	}
	for _, expected := range []string{"trigger-post", "trigger-post-2", "work-root", "run-card-parent-1", "run-card-parent-2", "@manager", "@architect"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("card fields miss %q: %#v", expected, card.Fields)
		}
	}
	turn, err := store.GetAgentSessionTurn(context.Background(), 3)
	if err != nil || turn.MattermostRunsPostID != "card-" {
		t.Fatalf("stored turn=%#v error=%v", turn, err)
	}
}
