package mattermost

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	model "github.com/mattermost/mattermost/server/public/model"
)

func TestCreatedBotMatchRequiresExactServerMarkerAndOwner(t *testing.T) {
	t.Parallel()
	intent := entity.AgentMattermostBotCreateIntent{
		Username: "agent-primary", DisplayName: "Agent primary",
		ProviderCorrelation: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	}
	bot := &model.Bot{
		UserId: "provider-user", Username: intent.Username, DisplayName: intent.DisplayName,
		Description: botOperationMarker(intent.ProviderCorrelation), OwnerId: "provider-owner",
	}
	if !createdBotMatches(bot, intent, "provider-owner") {
		t.Fatal("exact operation bot was rejected")
	}
	bot.Description = botOperationMarker("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if createdBotMatches(bot, intent, "provider-owner") {
		t.Fatal("foreign provider bot marker was accepted")
	}
	bot.Description = botOperationMarker(intent.ProviderCorrelation)
	bot.OwnerId = "foreign-owner"
	if createdBotMatches(bot, intent, "provider-owner") {
		t.Fatal("foreign provider bot owner was accepted")
	}
}

func TestBotEffectPredecessorRejectsOwnerTransferRace(t *testing.T) {
	t.Parallel()
	before := entity.AgentMattermostBotIdentity{
		ProviderUserID: "provider-user", ProviderTeamID: "provider-team",
		ProviderSnapshotSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:                 "AVAILABLE",
	}
	afterTransfer := before
	afterTransfer.ProviderSnapshotSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if !sameBotPredecessor(before, before) {
		t.Fatal("exact fresh predecessor was rejected")
	}
	if sameBotPredecessor(before, afterTransfer) {
		t.Fatal("owner-transfer snapshot race was accepted before/after provider effect")
	}
	afterTransfer.ProviderSnapshotSHA256 = before.ProviderSnapshotSHA256
	afterTransfer.Status = "REVOKED"
	if sameBotPredecessor(before, afterTransfer) {
		t.Fatal("revoked provider predecessor was accepted")
	}
}

func TestProviderErrorsAreMappedToClosedTypedSet(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 401, 403, 409} {
		if err := botProviderMutationError(&model.Response{StatusCode: status}, assertError{}); err == nil {
			t.Fatalf("provider mutation status %d was not mapped", status)
		}
	}
	if err := botProviderMutationError(nil, assertError{}); err == nil {
		t.Fatal("ambiguous provider mutation was not mapped")
	}
}

type assertError struct{}

func (assertError) Error() string { return "private provider error" }
