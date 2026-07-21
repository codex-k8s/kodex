package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
)

func TestAutomationRunNowDispatchesSavedPlaybookAndBindsRuntime(t *testing.T) {
	prompt := mustAutomationPlaybook()
	promptHash := sha256.Sum256([]byte(prompt))
	schedule := entity.AutomationSchedule{
		ID: 1, PublicID: "schedule-11111111111111111111111111111111", ProjectID: 1, ProjectName: "MatterCodex",
		TargetAgentRoleID: 2, TargetAgentRoleName: "developer", TargetChatID: 3, TargetChatName: "Development",
		Name: "Daily check", OwnerMattermostUserID: "owner-id", OwnerMattermostUserName: "owner", Enabled: true,
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1,
		PromptSnapshot: prompt, PromptSHA256: promptHash[:], CallbackContractVersion: value.AutomationCallbackContractV1,
	}
	run := entity.ScheduledRun{
		ID: 4, PublicID: "scheduled-run-11111111111111111111111111111111", OccurrenceID: 5,
		ScheduleID: schedule.ID, SchedulePublicID: schedule.PublicID, ScheduleName: schedule.Name,
		ProjectID: 1, ProjectName: schedule.ProjectName, TargetAgentRoleID: 2, TargetAgentRoleName: "developer",
		TargetChatID: 3, TargetChatName: "Development", OwnerMattermostUserID: "owner-id", Status: string(value.AutomationRunStatusQueued),
	}
	repository := &fakeAutomationRepository{schedule: schedule, run: run, createRunCreated: true}
	catalog := &fakeAutomationCatalog{
		project:      entity.Project{ID: 1, Name: "MatterCodex"},
		role:         entity.AgentRole{ID: 2, ProjectID: 1, Name: "developer", Enabled: true},
		chat:         entity.Chat{ID: 3, ProjectID: 1, Name: "Development", MattermostChannelID: "channel-1"},
		participants: []entity.ChatParticipant{{ChatID: 3, RoleID: 2, Enabled: true}},
		session:      entity.AgentSession{ID: 6, SessionKey: "session-1", ProjectID: 1, ChatID: 3, RoleID: 2, ActiveTurnID: 7, ActiveRunID: "runtime-run-1"},
	}
	dispatcher := &fakeAutomationDispatcher{queued: AgentTurnQueued{SessionKey: "session-1", TurnID: 7, RunID: "runtime-run-1"}}
	publisher := &fakeAutomationPublisher{ref: MattermostPostRef{ChannelID: "channel-1", PostID: "root-1"}}
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	svc := NewAutomationService(AutomationServiceConfig{
		Repository: repository, Catalog: catalog, Dispatcher: dispatcher, Publisher: publisher,
		OwnerMattermostUsername: "owner", StorageReady: true, RuntimeReady: true, Now: func() time.Time { return now },
	})

	result, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "owner-id", UserName: "@Owner"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "command-1",
	})
	if err != nil {
		t.Fatalf("RunNow() error=%v", err)
	}
	if result.Run.Status != string(value.AutomationRunStatusRunning) || dispatcher.calls != 1 || publisher.calls != 1 || repository.bindCalls != 1 {
		t.Fatalf("result=%#v dispatcher=%d publisher=%d binds=%d", result, dispatcher.calls, publisher.calls, repository.bindCalls)
	}
	if !strings.Contains(dispatcher.request.PreparedPrompt, "mattermost_complete_automation") || !strings.Contains(dispatcher.request.PreparedPrompt, run.PublicID) || strings.Contains(dispatcher.request.PreparedPrompt, "session-token") {
		t.Fatalf("prepared prompt не содержит безопасный callback-контракт: %q", dispatcher.request.PreparedPrompt)
	}
	if repository.bindInput.ProjectID != 1 || repository.bindInput.RuntimeSessionID != 6 || repository.bindInput.RuntimeTurnID != 7 || repository.bindInput.RuntimeRunID != "runtime-run-1" || repository.bindInput.MattermostRootPostID != "root-1" {
		t.Fatalf("runtime binding=%#v", repository.bindInput)
	}

	repository.createRunCreated = false
	duplicate, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "owner-id", UserName: "owner"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "command-1",
	})
	if err != nil || !duplicate.Duplicate || dispatcher.calls != 1 || publisher.calls != 1 {
		t.Fatalf("duplicate=%#v error=%v dispatcher=%d publisher=%d", duplicate, err, dispatcher.calls, publisher.calls)
	}
	if _, err := svc.RunNow(context.Background(), RunAutomationNowCommand{
		Actor: AuthenticatedActor{UserID: "other-id", UserName: "developer"}, ProjectID: 1,
		ScheduleID: schedule.PublicID, IdempotencyKey: "spoofed-run",
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("ручной запуск от не-владельца error=%v", err)
	}
	if dispatcher.calls != 1 || publisher.calls != 1 {
		t.Fatalf("неавторизованный запуск вызвал побочный эффект: dispatcher=%d publisher=%d", dispatcher.calls, publisher.calls)
	}

	if _, _, err := svc.CreateSchedule(context.Background(), CreateAutomationScheduleCommand{
		Actor: AuthenticatedActor{UserID: "other-id", UserName: "developer"}, ProjectID: 1, TargetAgentRoleID: 2, TargetChatID: 3,
		Name: "Spoofed", Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC",
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, IdempotencyKey: "spoofed",
	}); !errors.Is(err, automationsrepo.ErrForbidden) {
		t.Fatalf("создание от не-владельца error=%v", err)
	}
}

func TestAutomationSummaryRejectsSecretsAndRawPlaybook(t *testing.T) {
	tests := []string{
		"token=secret-value",
		"authorization: bearer-value",
		"sk-1234567890abcdefghijklmnop",
		"-----BEGIN PRIVATE KEY-----",
		"Ты выполняешь минимальный playbook автоматизации MatterCodex",
		"callback_contract: automation.callback.v1",
	}
	for _, summary := range tests {
		if automationSummaryIsSafe(summary) {
			t.Fatalf("чувствительное резюме принято: %q", summary)
		}
	}
	if !automationSummaryIsSafe("Проверка завершена; изменений не требуется") {
		t.Fatal("безопасное резюме отклонено")
	}
}

func TestNextDailyAutomationRunUsesIANAZone(t *testing.T) {
	now := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	next, err := nextDailyAutomationRun(now, "09:00", "Europe/Moscow")
	if err != nil {
		t.Fatalf("nextDailyAutomationRun() error=%v", err)
	}
	want := time.Date(2026, time.July, 22, 6, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
	if _, err := nextDailyAutomationRun(now, "09:00", "Local"); err == nil {
		t.Fatal("не-IANA зона Local принята")
	}
}

type fakeAutomationRepository struct {
	automationsrepo.Repository
	schedule         entity.AutomationSchedule
	run              entity.ScheduledRun
	createRunCreated bool
	bindInput        automationsrepo.BindRunInput
	bindCalls        int
}

func (repository *fakeAutomationRepository) CreateManualRun(_ context.Context, _ automationsrepo.CreateManualRunInput) (entity.ScheduledRun, bool, error) {
	return repository.run, repository.createRunCreated, nil
}

func (repository *fakeAutomationRepository) GetSchedule(_ context.Context, _ string, _ int64, _ string) (entity.AutomationSchedule, error) {
	return repository.schedule, nil
}

func (repository *fakeAutomationRepository) BindRun(_ context.Context, input automationsrepo.BindRunInput) (entity.ScheduledRun, error) {
	repository.bindCalls++
	repository.bindInput = input
	bound := repository.run
	bound.Status = string(value.AutomationRunStatusRunning)
	bound.RuntimeSessionID = input.RuntimeSessionID
	bound.RuntimeTurnID = input.RuntimeTurnID
	bound.RuntimeRunID = input.RuntimeRunID
	return bound, nil
}

func (repository *fakeAutomationRepository) FailRun(_ context.Context, _ automationsrepo.FailRunInput) (entity.ScheduledRun, error) {
	failed := repository.run
	failed.Status = string(value.AutomationRunStatusFailed)
	failed.Outcome = string(value.AutomationRunOutcomeFailed)
	return failed, nil
}

type fakeAutomationCatalog struct {
	project      entity.Project
	role         entity.AgentRole
	chat         entity.Chat
	participants []entity.ChatParticipant
	repositories []entity.ProjectRepository
	session      entity.AgentSession
}

func (catalog *fakeAutomationCatalog) GetProject(context.Context, int64) (entity.Project, error) {
	return catalog.project, nil
}

func (catalog *fakeAutomationCatalog) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return catalog.role, nil
}

func (catalog *fakeAutomationCatalog) GetChat(context.Context, int64) (entity.Chat, error) {
	return catalog.chat, nil
}

func (catalog *fakeAutomationCatalog) ListChatParticipants(context.Context, int64) ([]entity.ChatParticipant, error) {
	return catalog.participants, nil
}

func (catalog *fakeAutomationCatalog) ListProjectRepositories(context.Context, int64) ([]entity.ProjectRepository, error) {
	return catalog.repositories, nil
}

func (catalog *fakeAutomationCatalog) GetAgentSession(context.Context, string) (entity.AgentSession, error) {
	return catalog.session, nil
}

type fakeAutomationDispatcher struct {
	request AgentTurnRequest
	queued  AgentTurnQueued
	calls   int
}

func (dispatcher *fakeAutomationDispatcher) EnqueueAgentTurn(_ context.Context, request AgentTurnRequest) (AgentTurnQueued, error) {
	dispatcher.calls++
	dispatcher.request = request
	return dispatcher.queued, nil
}

type fakeAutomationPublisher struct {
	input MattermostThreadPostInput
	ref   MattermostPostRef
	calls int
}

func (publisher *fakeAutomationPublisher) PostThreadMessage(_ context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.calls++
	publisher.input = input
	return publisher.ref, nil
}
