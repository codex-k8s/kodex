package mattermost

import (
	"slices"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	model "github.com/mattermost/mattermost/server/public/model"
)

func TestBotIdentityPermissionProfileRequiresExactLifecycleSet(t *testing.T) {
	t.Parallel()
	roleNames := []string{"system-agent-bot-lifecycle", "team-agent-bot-lifecycle"}
	roles := []*model.Role{
		{Name: roleNames[0], Permissions: slices.Clone(requiredBotIdentityPermissions)},
		{Name: roleNames[1]},
	}
	if !botIdentityPermissionProfile(roleNames, roles) {
		t.Fatal("exact Agent bot lifecycle permission profile was rejected")
	}
	for index, permission := range requiredBotIdentityPermissions {
		permissions := slices.Clone(requiredBotIdentityPermissions)
		permissions = append(permissions[:index], permissions[index+1:]...)
		candidate := []*model.Role{{Name: roleNames[0], Permissions: permissions}, {Name: roleNames[1]}}
		if botIdentityPermissionProfile(roleNames, candidate) {
			t.Fatalf("missing provider permission %q was accepted", permission)
		}
	}
	if botIdentityPermissionProfile(roleNames, roles[:1]) {
		t.Fatal("incomplete effective role readback was accepted")
	}
	duplicated := []*model.Role{roles[0], roles[0]}
	if botIdentityPermissionProfile(roleNames, duplicated) {
		t.Fatal("duplicated role readback was accepted")
	}
}

func TestBotIdentityPermissionProfileRequiresProviderFeatures(t *testing.T) {
	t.Parallel()
	enabled, disabled := true, false
	config := &model.Config{}
	config.ServiceSettings.EnableBotAccountCreation = &enabled
	config.ServiceSettings.EnableUserAccessTokens = &enabled
	if !botIdentityFeaturesReady(config) {
		t.Fatal("enabled bot and token provider features were rejected")
	}
	config.ServiceSettings.EnableUserAccessTokens = &disabled
	if botIdentityFeaturesReady(config) {
		t.Fatal("disabled user access tokens were accepted")
	}
	config.ServiceSettings.EnableUserAccessTokens = &enabled
	config.ServiceSettings.EnableBotAccountCreation = &disabled
	if botIdentityFeaturesReady(config) {
		t.Fatal("disabled bot creation was accepted")
	}
}

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
