package mattermost

import (
	"slices"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

func TestExplicitMentionsClosedResolution(t *testing.T) {
	mentions := explicitMentions("@agent-b inspect https://example.test/@ignored, mail@ignored.test and @Agent-A, then @agent-b")
	if !slices.Equal(mentions, []string{"agent-a", "agent-b"}) {
		t.Fatalf("mentions = %v", mentions)
	}
}

func TestResolveAssignmentDefersProviderIdentityToRuntimeAdmission(t *testing.T) {
	current := &index{assignments: map[string]AgentAssignment{
		"team\x00channel\x00agent-b": {
			MentionUsername: "agent-b", MattermostUserID: "user-b", RoleID: "role-b", BotStableKey: "bot-b",
		},
	}}
	assignment, err := current.resolveAssignment("team", "channel", "agent-b", "user-current")
	if err != nil || assignment.RoleID != "role-b" || assignment.BotStableKey != "bot-b" {
		t.Fatal("server-owned stable key assignment was not resolved")
	}
	if _, err := current.resolveAssignment("team", "channel", "agent-b", ""); err == nil {
		t.Fatal("assignment accepted missing provider readback identity")
	}
}

func TestOnlyAssignedMentionIsAgentSelector(t *testing.T) {
	current := &index{assignments: map[string]AgentAssignment{
		"team\x00channel\x00agent-b": {MentionUsername: "agent-b", MattermostUserID: "user-b"},
	}}
	if _, assigned := current.assigned("team", "channel", "human-a"); assigned {
		t.Fatal("human mention was treated as agent selector")
	}
	assignment, assigned := current.assigned("team", "channel", "Agent-B")
	if !assigned || assignment.MattermostUserID != "user-b" {
		t.Fatal("server-owned agent selector was not found")
	}
}

func TestChannelBoundariesPreserveProviderAndDomainIDs(t *testing.T) {
	current := &index{channels: map[string]ChannelBinding{
		"team\x00channel": {
			TeamID: "team", ChannelID: "channel", OrganizationID: "organization",
			ProjectID: "project", ChatID: "chat",
		},
	}}
	boundaries := current.channelBoundaries()
	if len(boundaries) != 1 || boundaries[0].TeamID != "team" || boundaries[0].ChannelID != "channel" ||
		boundaries[0].ChatID != "chat" || boundaries[0].OrganizationID != "organization" ||
		boundaries[0].ProjectID != "project" {
		t.Fatalf("channel boundary projection mismatch: %+v", boundaries)
	}
}

func TestIgnoredBotKeepsCursorAuthorityBoundary(t *testing.T) {
	current := &index{templates: map[string]ChannelBinding{
		"template": {
			TeamID: "team", ChannelID: "channel", OrganizationID: "organization",
			ProjectID: "project", ChatID: "chat", RoleID: "role", BotStableKey: "bot", Locale: "ru",
			LifecycleActorID: "owner",
		},
	}, botUsers: map[string]struct{}{"bot-user": {}}}
	client := &Client{index: current}
	boundary, _, err := client.resolveRuntimeBoundary(entity.MattermostRuntimeRoute{
		TemplateKey: "template", Boundary: entity.Boundary{
			TeamID: "team", ChannelID: "channel", OrganizationID: "organization", ProjectID: "project",
			ChatID: "chat", RoleID: "role", BotStableKey: "bot", Locale: "ru", MappingOwnerActorID: "owner",
		},
	}, "bot-user", true)
	if err != nil || !boundary.IgnoredBot || boundary.OrganizationID != "organization" ||
		boundary.ProjectID != "project" || boundary.ChannelID != "channel" {
		t.Fatalf("ignored bot lost server-owned boundary: %+v, %v", boundary, err)
	}
}

func TestRuntimeAdmissionAllowsOnlyDefaultOrAssignedStableKey(t *testing.T) {
	current := &index{templates: map[string]ChannelBinding{
		"template": {Assignments: []AgentAssignment{{BotStableKey: "assigned-bot"}}},
	}}
	client := &Client{index: current}
	route := entity.MattermostRuntimeRoute{TemplateKey: "template",
		Boundary: entity.Boundary{BotStableKey: "default-bot"}}
	if !client.runtimeStableKeyAllowed(route, "default-bot") ||
		!client.runtimeStableKeyAllowed(route, "assigned-bot") ||
		client.runtimeStableKeyAllowed(route, "foreign-bot") {
		t.Fatal("runtime stable key escaped the server-owned route template")
	}
}
