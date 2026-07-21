package service

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestAutomationCreateDialogUsesNamedSelectsAndStableCommandKey(t *testing.T) {
	store := &fakeAdminStore{
		projects: map[int64]entity.Project{
			1: {ID: 1, Name: "MatterCodex"},
		},
		agentRoles: map[int64]entity.AgentRole{
			2: {ID: 2, ProjectID: 1, Name: "developer", Enabled: true},
		},
		chats: map[int64]entity.Chat{
			3: {ID: 3, ProjectID: 1, Name: "Разработка", MattermostChannelID: "channel-1"},
		},
		chatParticipants: map[int64][]entity.ChatParticipant{
			3: {{ChatID: 3, RoleID: 2, Enabled: true}},
		},
	}
	repository := &fakeAutomationRepository{schedule: entity.AutomationSchedule{
		PublicID: "schedule-11111111111111111111111111111111", ProjectID: 1, ProjectName: "MatterCodex",
		TargetAgentRoleID: 2, TargetAgentRoleName: "developer", TargetChatID: 3, TargetChatName: "Разработка",
		Name: "Ежедневная проверка", LocalTime: "09:00", TimeZone: "UTC", Enabled: true,
	}}
	automations := NewAutomationService(AutomationServiceConfig{
		Repository: repository, Catalog: store,
		OwnerMattermostUsername: "owner", StorageReady: true,
	})
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:       testLocalizer(t, "ru"),
		Store:           store,
		Automations:     automations,
		MenuActionURL:   "https://mattermost.example/actions",
		DialogSubmitURL: "https://mattermost.example/dialogs",
	})

	dialog, failure := svc.automationCreateDialog(context.Background(), MenuActionCommand{
		View: menuViewAutomations, IdempotencyKey: "command-stable",
	})
	if failure != "" || dialog == nil {
		t.Fatalf("dialog=%#v failure=%q", dialog, failure)
	}
	if len(dialog.Title) > mattermostmodel.DialogTitleMaxLength {
		t.Fatalf("заголовок диалога длиннее лимита Mattermost: %q", dialog.Title)
	}
	for _, element := range dialog.Elements {
		if len(element.DisplayName) > mattermostmodel.DialogElementDisplayNameMaxLength || len(element.HelpText) > mattermostmodel.DialogElementHelpTextMaxLength {
			t.Fatalf("поле диалога выходит за лимиты Mattermost: %#v", element)
		}
	}
	state, err := decodeDialogState(dialog.State)
	if err != nil || state.IdempotencyKey != "command-stable" {
		t.Fatalf("dialog state=%#v error=%v", state, err)
	}
	for _, field := range []struct {
		name string
		text string
	}{
		{dialogFieldAutomationProject, "MatterCodex"},
		{dialogFieldAutomationRole, "MatterCodex — developer"},
		{dialogFieldAutomationChat, "MatterCodex — Разработка"},
		{dialogFieldAutomationPreset, "Ежедневно"},
		{dialogFieldAutomationTimeZone, "UTC"},
		{dialogFieldAutomationPlaybook, "проверка состояния проекта"},
	} {
		element, ok := dialogElementByName(dialog.Elements, field.name)
		if !ok || element.Type != "select" || len(element.Options) == 0 || !strings.Contains(strings.ToLower(element.Options[0].Text), strings.ToLower(field.text)) {
			t.Fatalf("поле %s не является понятным select: %#v", field.name, element)
		}
	}

	card := svc.automationScheduleCard(context.Background(), MenuActionCommand{
		ID: automationResourceID(1, repository.schedule.PublicID), UserID: "owner-id", UserName: "owner",
	})
	if len(card.Actions) == 0 || card.Actions[0].Context["action"] != menuActionAutomationRunNow || strings.TrimSpace(contextStringValue(card.Actions[0].Context, "idempotency_key")) == "" {
		t.Fatalf("карточка не содержит идемпотентное действие Run Now: %#v", card.Actions)
	}
	resourceType, resourceID := interactionResource(card.Actions[0].Context)
	if !typedInteractionOperationAllowed(InteractionAdmissionRequest{
		ActionKey: "mattermost.callback.action", Operation: actionCallbackOperation(card.Actions[0].Context),
		ResourceType: resourceType, ResourceID: resourceID,
	}) {
		t.Fatalf("типизированный admission отклонил Run Now: %#v", card.Actions[0].Context)
	}
	if !typedInteractionOperationAllowed(InteractionAdmissionRequest{
		ActionKey: "mattermost.callback.dialog", Operation: dialogCallbackOperation(dialog.CallbackID),
	}) {
		t.Fatalf("типизированный admission отклонил диалог создания: %s", dialog.CallbackID)
	}
}

func TestAutomationRunCardsExposeEveryStateWithoutColorOnly(t *testing.T) {
	svc := NewSlashCommandService(SlashCommandServiceConfig{
		Localizer:     testLocalizer(t, "ru"),
		MenuActionURL: "https://mattermost.example/actions",
	})
	tests := []struct {
		status  string
		outcome string
		label   string
		color   string
	}{
		{string(value.AutomationRunStatusQueued), "", "В очереди", "#5b667a"},
		{string(value.AutomationRunStatusRunning), "", "Выполняется", "#1c58d9"},
		{string(value.AutomationRunStatusSucceeded), string(value.AutomationRunOutcomeNoAction), "Завершено успешно", "#227a55"},
		{string(value.AutomationRunStatusFailed), string(value.AutomationRunOutcomeFailed), "Завершено с ошибкой", "#c4314b"},
	}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			card := svc.automationRunCard(context.Background(), entity.ScheduledRun{
				PublicID:            "scheduled-run-11111111111111111111111111111111",
				SchedulePublicID:    "schedule-11111111111111111111111111111111",
				ScheduleName:        "Daily check",
				ProjectID:           1,
				ProjectName:         "MatterCodex",
				TargetAgentRoleName: "developer",
				TargetChatName:      "Development",
				Status:              test.status,
				Outcome:             test.outcome,
			})
			if card.Color != test.color {
				t.Fatalf("color=%q want=%q", card.Color, test.color)
			}
			visible := card.Title + "\n" + card.Text
			for _, field := range card.Fields {
				visible += "\n" + field.Title + "\n" + field.Value
			}
			if !strings.Contains(visible, test.label) {
				t.Fatalf("карточка %s не содержит явное состояние %q: %s", test.status, test.label, visible)
			}
			if len(card.Actions) == 0 {
				t.Fatalf("карточка %s не содержит навигации", test.status)
			}
		})
	}
}
