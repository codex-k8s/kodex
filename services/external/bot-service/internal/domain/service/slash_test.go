package service

import (
	"context"
	"strings"
	"testing"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

func TestSlashTokenCheck(t *testing.T) {
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})
	text := svc.Handle(context.Background(), SlashCommand{Text: "token check"})
	for _, want := range []string{
		"mattermost bot token: configured",
		"mattermost slash token: configured",
		"database dsn: configured",
		"storage: ready",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Handle(token check) missing %q in %q", want, text)
		}
	}
}

func TestSlashRepoAddCreatesChannelAndStoresRepository(t *testing.T) {
	store := &fakeAdminStore{}
	channels := &fakeChannelManager{}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService:         testStatusService(),
		Store:                 store,
		ChannelManager:        channels,
		DefaultTeamName:       "agents",
		BotTokenConfigured:    true,
		SlashTokenConfigured:  true,
		DatabaseConfigured:    true,
		StorageReady:          true,
		ChannelManagerEnabled: true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "repo add github codex-k8s/matter-codex main", UserName: "owner"})

	if !strings.Contains(text, "github:codex-k8s/matter-codex") {
		t.Fatalf("Handle(repo add) text = %q", text)
	}
	if store.upsert.Provider != "github" || store.upsert.Owner != "codex-k8s" || store.upsert.Name != "matter-codex" {
		t.Fatalf("unexpected upsert input: %#v", store.upsert)
	}
	if channels.channelName != "repo-codex-k8s-matter-codex" {
		t.Fatalf("channelName = %q", channels.channelName)
	}
	if !store.auditRecorded {
		t.Fatal("audit event was not recorded")
	}
}

func TestSlashProfileList(t *testing.T) {
	store := &fakeAdminStore{
		profiles: []entity.AgentProfile{{Name: "developer", Role: "developer", Description: "dev", Enabled: true}},
	}
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		StatusService: testStatusService(),
		Store:         store,
		StorageReady:  true,
	})

	text := svc.Handle(context.Background(), SlashCommand{Text: "profile list"})

	if !strings.Contains(text, "`developer` role `developer` enabled") {
		t.Fatalf("Handle(profile list) text = %q", text)
	}
}

func testStatusService() *StatusService {
	return NewStatusService(Config{
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DatabaseConfigured:   true,
		StorageReady:         true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control"},
	})
}

type fakeAdminStore struct {
	upsert        adminrepo.UpsertRepositoryInput
	auditRecorded bool
	profiles      []entity.AgentProfile
}

func (store *fakeAdminStore) UpsertRepository(_ context.Context, input adminrepo.UpsertRepositoryInput) (entity.Repository, bool, error) {
	store.upsert = input
	return entity.Repository{
		ID:                1,
		Provider:          input.Provider,
		Owner:             input.Owner,
		Name:              input.Name,
		DefaultBranch:     input.DefaultBranch,
		Status:            "active",
		MattermostChannel: input.MattermostChannel,
	}, true, nil
}

func (store *fakeAdminStore) ListRepositories(context.Context, int) ([]entity.Repository, error) {
	return []entity.Repository{}, nil
}

func (store *fakeAdminStore) ListAgentProfiles(context.Context) ([]entity.AgentProfile, error) {
	return store.profiles, nil
}

func (store *fakeAdminStore) RecordAuditEvent(context.Context, adminrepo.AuditEventInput) error {
	store.auditRecorded = true
	return nil
}

type fakeChannelManager struct {
	channelName string
}

func (manager *fakeChannelManager) EnsureRepositoryChannel(_ context.Context, _ string, channelName string, _ string) (bool, error) {
	manager.channelName = channelName
	return true, nil
}
