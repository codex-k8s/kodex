package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	model "github.com/mattermost/mattermost/server/public/model"
)

func TestCheckBotIdentityPermissionsUsesManifestManagementBot(t *testing.T) {
	const (
		primaryBotID = "provider-management-bot"
		ownerUserID  = "provider-human-owner"
		providerTeam = "provider-team"
	)
	principal := entity.TeamPrincipal{
		ActorID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", OrganizationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProjectID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	}
	authenticated := &model.User{Id: primaryBotID, IsBot: true, Roles: "system_user"}
	permissions := slices.Clone(requiredBotIdentityPermissions)
	enabled := true
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v4/users/me":
			_ = json.NewEncoder(writer).Encode(authenticated)
		case request.Method == http.MethodGet && request.URL.Path ==
			"/api/v4/teams/"+providerTeam+"/members/"+primaryBotID:
			_ = json.NewEncoder(writer).Encode(&model.TeamMember{
				TeamId: providerTeam, UserId: primaryBotID, Roles: "team_user",
			})
		case request.Method == http.MethodGet && request.URL.Path == "/api/v4/config":
			config := &model.Config{}
			config.ServiceSettings.EnableBotAccountCreation = &enabled
			config.ServiceSettings.EnableUserAccessTokens = &enabled
			_ = json.NewEncoder(writer).Encode(config)
		case request.Method == http.MethodPost && request.URL.Path == "/api/v4/roles/names":
			var names []string
			if json.NewDecoder(request.Body).Decode(&names) != nil {
				http.Error(writer, "invalid role request", http.StatusBadRequest)
				return
			}
			roles := make([]*model.Role, 0, len(names))
			for _, name := range names {
				role := &model.Role{Name: name}
				if name == "system_user" {
					role.Permissions = slices.Clone(permissions)
				}
				roles = append(roles, role)
			}
			_ = json.NewEncoder(writer).Encode(roles)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	api := model.NewAPIv4Client(server.URL)
	client := &Client{
		primary: &botClient{identity: BotIdentity{UserID: primaryBotID}, api: api},
		index: &index{actors: map[string]ActorBinding{
			ownerUserID: {
				MattermostUserID: ownerUserID, ActorID: principal.ActorID,
				OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
			},
		}},
	}

	if err := client.CheckBotIdentityPermissions(context.Background(), principal, providerTeam); err != nil {
		t.Fatalf("manifest management Bot distinct from human owner was rejected: %v", err)
	}
	authenticated = &model.User{Id: ownerUserID, IsBot: false, Roles: "system_user"}
	if err := client.CheckBotIdentityPermissions(context.Background(), principal, providerTeam); err == nil {
		t.Fatal("human owner authenticated by primary credential was accepted")
	}
	authenticated = &model.User{Id: primaryBotID, IsBot: true, Roles: "system_user"}
	permissions = slices.DeleteFunc(permissions, func(permission string) bool {
		return permission == model.PermissionManageOthersBots.Id
	})
	if err := client.CheckBotIdentityPermissions(context.Background(), principal, providerTeam); err == nil {
		t.Fatal("management Bot without manage_others_bots was accepted")
	}
}

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
