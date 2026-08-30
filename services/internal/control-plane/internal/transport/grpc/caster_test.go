package grpc

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestCastScheduleUsesPublicLifecycleStates(t *testing.T) {
	t.Parallel()

	if got := castSchedule(entity.Schedule{State: "ACTIVE", Enabled: true}).GetState(); got != controlplanev1.ScheduleState_SCHEDULE_STATE_ACTIVE {
		t.Fatalf("enabled schedule state = %s", got)
	}
	if got := castSchedule(entity.Schedule{State: "ACTIVE", Enabled: false}).GetState(); got != controlplanev1.ScheduleState_SCHEDULE_STATE_PAUSED {
		t.Fatalf("paused schedule state = %s", got)
	}
	if got := castSchedule(entity.Schedule{State: "ARCHIVED", Enabled: false}).GetState(); got != controlplanev1.ScheduleState_SCHEDULE_STATE_ARCHIVED {
		t.Fatalf("archived schedule state = %s", got)
	}
	if got := castSchedule(entity.Schedule{State: "UNKNOWN", Enabled: true}).GetState(); got != controlplanev1.ScheduleState_SCHEDULE_STATE_UNSPECIFIED {
		t.Fatalf("unknown schedule state = %s", got)
	}
}

func TestCastBootstrapIncludesResolvedPlatformIdentity(t *testing.T) {
	t.Parallel()

	state := castBootstrap(platformrepo.BootstrapState{
		Actor:        entity.User{Ref: "usr_owner", DisplayName: "Владелец", EmailMasked: "o***@example.test"},
		PlatformRole: "OWNER",
	})

	if state.GetPlatformRole() != controlplanev1.PlatformRole_PLATFORM_ROLE_OWNER {
		t.Fatalf("platform role = %s", state.GetPlatformRole())
	}
	if state.GetCurrentUser().GetDisplayName() != "Владелец" || state.GetCurrentUser().GetEmailHint() != "o***@example.test" {
		t.Fatalf("current user was not cast as a minimized summary: %#v", state.GetCurrentUser())
	}
}

func TestCastRunIncludesTypedTokenUsage(t *testing.T) {
	t.Parallel()

	usage := entity.TokenUsage{
		TotalTokens: 120, InputTokens: 100, CachedInputTokens: 40,
		CacheWriteInputTokens: 10, OutputTokens: 20, ReasoningOutputTokens: 5,
		ModelContextWindow: 200000,
	}
	run := castRun(entity.Run{Usage: usage})
	delta := castRunDelta(&entity.RunDelta{Usage: usage})

	if run.GetUsage().GetTotalTokens() != usage.TotalTokens ||
		run.GetUsage().GetCacheWriteInputTokens() != usage.CacheWriteInputTokens ||
		run.GetUsage().GetModelContextWindow() != usage.ModelContextWindow {
		t.Fatalf("run token usage was not cast: %#v", run.GetUsage())
	}
	if delta.GetUsage().GetInputTokens() != usage.InputTokens ||
		delta.GetUsage().GetCachedInputTokens() != usage.CachedInputTokens ||
		delta.GetUsage().GetOutputTokens() != usage.OutputTokens ||
		delta.GetUsage().GetReasoningOutputTokens() != usage.ReasoningOutputTokens {
		t.Fatalf("run delta token usage was not cast: %#v", delta.GetUsage())
	}
}

func TestCastRunPreservesArtifactAndGateReadback(t *testing.T) {
	t.Parallel()

	run := castRun(entity.Run{
		InputAttachmentSetRef: "aset_abcdefgh",
		ArtifactRefs:          []string{"art_output"},
		GateRefs:              []string{"gat_review"},
		TitleSource:           "USER_EDITED",
	})

	if run.GetInputAttachmentSetRef() != "aset_abcdefgh" {
		t.Fatalf("run input attachment set was not cast: %q", run.GetInputAttachmentSetRef())
	}
	if len(run.GetArtifactRefs()) != 1 || run.GetArtifactRefs()[0] != "art_output" {
		t.Fatalf("run artifacts were not cast: %v", run.GetArtifactRefs())
	}
	if len(run.GetGateRefs()) != 1 || run.GetGateRefs()[0] != "gat_review" {
		t.Fatalf("run gates were not cast: %v", run.GetGateRefs())
	}
	if run.GetTitleSource() != "USER_EDITED" {
		t.Fatalf("run title source was not cast: %q", run.GetTitleSource())
	}
}

func TestCastPlanUsesBoundedHumanReadableOperationTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		operationType string
		input         map[string]any
		wantTitle     string
	}{
		{name: "project name", operationType: "CREATE_PROJECT", input: map[string]any{"name": "Проект продаж", "projectRef": "prj_secret"}, wantTitle: "Проект продаж"},
		{name: "agent name", operationType: "CREATE_AGENT", input: map[string]any{"name": "Менеджер", "runtimeRef": "runtime_secret"}, wantTitle: "Менеджер"},
		{name: "workflow name", operationType: "CREATE_WORKFLOW", input: map[string]any{"name": "Обработка заявок", "coordinatorAgentRef": "agt_secret"}, wantTitle: "Обработка заявок"},
		{name: "schedule name", operationType: "CREATE_SCHEDULE", input: map[string]any{"name": "Ежедневная проверка", "targetRef": "wfl_secret"}, wantTitle: "Ежедневная проверка"},
		{name: "connection name", operationType: "CREATE_INTEGRATION_CONNECTION", input: map[string]any{"name": "Рабочая CRM", "definitionKey": "secret.provider"}, wantTitle: "Рабочая CRM"},
		{name: "run title", operationType: "LAUNCH_RUN", input: map[string]any{"title": "Разобрать новые заявки", "sessionRef": "ses_secret"}, wantTitle: "Разобрать новые заявки"},
		{name: "fallback for unsupported operation", operationType: "CHANGE_CAPABILITY", input: map[string]any{"name": "Не показывать", "capabilityKey": "secret.capability"}, wantTitle: "Безопасное описание"},
		{name: "fallback for absent display field", operationType: "CREATE_WORKFLOW", input: map[string]any{"name": 42, "workflowRef": "wfl_secret"}, wantTitle: "Безопасное описание"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			plan := castPlan(&entity.AssistantPlan{Operations: []entity.AssistantPlanOperation{{
				Key: "operation-001", Type: test.operationType, Summary: "Безопасное описание", Input: test.input,
			}}})
			operation := plan.GetOperations()[0]
			if operation.GetTitle() != test.wantTitle {
				t.Fatalf("operation title = %q, want %q", operation.GetTitle(), test.wantTitle)
			}
			if operation.GetSummary() != "Безопасное описание" {
				t.Fatalf("operation summary = %q", operation.GetSummary())
			}
		})
	}
}

func TestCastPlanBoundsOperationTitleWithoutChangingSummary(t *testing.T) {
	t.Parallel()

	longTitle := strings.Repeat("я", maximumAssistantPlanOperationTitleRunes+20)
	plan := castPlan(&entity.AssistantPlan{Operations: []entity.AssistantPlanOperation{{
		Key: "operation-001", Type: "LAUNCH_RUN", Summary: "Полное безопасное описание", Input: map[string]any{"title": longTitle},
	}}})
	operation := plan.GetOperations()[0]

	if got := len([]rune(operation.GetTitle())); got != maximumAssistantPlanOperationTitleRunes {
		t.Fatalf("operation title length = %d, want %d", got, maximumAssistantPlanOperationTitleRunes)
	}
	if !strings.HasSuffix(operation.GetTitle(), "…") {
		t.Fatalf("bounded operation title = %q, want ellipsis suffix", operation.GetTitle())
	}
	if operation.GetSummary() != "Полное безопасное описание" {
		t.Fatalf("operation summary = %q", operation.GetSummary())
	}
}

func TestCastConversationUsesPublicAssistantTurnShape(t *testing.T) {
	t.Parallel()

	conversation := castConversation(entity.AssistantConversation{
		Ref: "cnv-example", ProjectRef: "prj-example",
		LatestPlan: &entity.AssistantPlan{
			Ref: "pln-example", ConversationRef: "cnv-example", ProjectRef: "prj-example",
			State: "DRAFT", Summary: "План готов", Version: 1, Revision: 1,
		},
	})
	if len(conversation.GetTurns()) != 1 {
		t.Fatalf("assistant plan turn count = %d, want 1", len(conversation.GetTurns()))
	}
	turn := conversation.GetTurns()[0]
	if turn.GetRole() != "ASSISTANT" || turn.GetState() != "COMPLETED" {
		t.Fatalf("assistant plan turn = role %q state %q", turn.GetRole(), turn.GetState())
	}
	if turn.GetPlan().GetConversationRef() != "cnv-example" || turn.GetPlan().GetProjectRef() != "prj-example" {
		t.Fatalf("assistant plan lineage was lost: %#v", turn.GetPlan())
	}
}
