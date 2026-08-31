package grpc

import (
	"reflect"
	"strings"
	"testing"
	"time"

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
	if got := castSchedule(entity.Schedule{State: "DELETED", Enabled: false}).GetState(); got != controlplanev1.ScheduleState_SCHEDULE_STATE_DELETED {
		t.Fatalf("deleted schedule state = %s", got)
	}
	if got := castSchedule(entity.Schedule{State: "UNKNOWN", Enabled: true}).GetState(); got != controlplanev1.ScheduleState_SCHEDULE_STATE_UNSPECIFIED {
		t.Fatalf("unknown schedule state = %s", got)
	}
}

func TestCastScheduleMaterializesRevisionAndContinuationContract(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.August, 31, 1, 2, 3, 0, time.UTC)
	input := map[string]any{"task": "Prepare a bounded report.", "limit": float64(10)}
	schedule := castSchedule(entity.Schedule{
		Ref: "sch_contract", Version: 4, ProjectRef: "prj_contract", Name: "Daily report",
		Target: entity.RunTarget{Type: "AGENT", Ref: "agt_contract"}, State: "ACTIVE", Enabled: true,
		Preset: "DAILY", CronExpression: "0 9 * * *", Timezone: "UTC", Input: input,
		SessionPolicy: "CONTINUE_ONE", NotificationPolicy: "CONTROL_CENTER_ONLY",
		CurrentRevision: entity.ScheduleRevision{
			Ref: "srev_contract", Revision: 3, Digest: strings.Repeat("a", 64), Name: "Daily report",
			Target: entity.RunTarget{Type: "AGENT", Ref: "agt_contract"}, Preset: "DAILY",
			CronExpression: "0 9 * * *", Timezone: "UTC", Input: input,
			SessionPolicy: "CONTINUE_ONE", NotificationPolicy: "CONTROL_CENTER_ONLY", CreatedAt: createdAt,
		},
		ContinueSessionRef: "ses_contract",
	})

	revision := schedule.GetCurrentRevision()
	if revision == nil || revision.GetRef() != "srev_contract" || revision.GetRevision() != 3 ||
		revision.GetDigest() != strings.Repeat("a", 64) || revision.GetTarget().GetAgentRef() != "agt_contract" ||
		revision.GetCreatedAt().AsTime() != createdAt || !reflect.DeepEqual(revision.GetInput().AsMap(), input) {
		t.Fatalf("schedule current revision contract is incomplete: %#v", revision)
	}
	if schedule.GetContinueSessionRef() != "ses_contract" {
		t.Fatalf("schedule continuation session = %q", schedule.GetContinueSessionRef())
	}
	fields := schedule.ProtoReflect().Descriptor().Fields()
	if fields.ByJSONName("currentRevision") == nil || fields.ByJSONName("continueSessionRef") == nil {
		t.Fatal("generated Schedule contract does not expose currentRevision and continueSessionRef")
	}
}

func TestCastConnectionUsesCompletePublicStatesAndSnapshot(t *testing.T) {
	t.Parallel()

	for state, want := range map[string]controlplanev1.ConnectionState{
		"TESTING":  controlplanev1.ConnectionState_CONNECTION_STATE_TESTING,
		"DEGRADED": controlplanev1.ConnectionState_CONNECTION_STATE_DEGRADED,
		"DELETED":  controlplanev1.ConnectionState_CONNECTION_STATE_DELETED,
		"UNKNOWN":  controlplanev1.ConnectionState_CONNECTION_STATE_UNSPECIFIED,
	} {
		if got := connectionState(state); got != want {
			t.Fatalf("connection state %q = %s, want %s", state, got, want)
		}
	}
	now := time.Now().UTC()
	connection := castConnection(entity.IntegrationConnection{
		State: "DELETED", CreatedAt: now, UpdatedAt: now,
		CredentialRevision: &entity.IntegrationCredentialRevision{Ref: "icr_example", CreatedAt: now},
	})
	if connection.GetState() != controlplanev1.ConnectionState_CONNECTION_STATE_DELETED ||
		connection.GetCredentialRevision().GetRef() != "icr_example" ||
		connection.GetCreatedAt() == nil || connection.GetUpdatedAt() == nil {
		t.Fatalf("connection terminal snapshot is incomplete: %#v", connection)
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

func TestNextActionsPreserveAllDomainLifecycleCommands(t *testing.T) {
	t.Parallel()

	actions := nextActions([]string{"UPDATE", "RESTORE", "REQUEST_BUILD", "PURGE"})
	want := []controlplanev1.NextAction{
		controlplanev1.NextAction_NEXT_ACTION_UPDATE,
		controlplanev1.NextAction_NEXT_ACTION_RESTORE,
		controlplanev1.NextAction_NEXT_ACTION_REQUEST_BUILD,
		controlplanev1.NextAction_NEXT_ACTION_PURGE,
	}
	if len(actions) != len(want) {
		t.Fatalf("next actions length = %d, want %d: %v", len(actions), len(want), actions)
	}
	for index := range want {
		if actions[index] != want[index] {
			t.Fatalf("next action %d = %s, want %s", index, actions[index], want[index])
		}
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
		Turns: []entity.AssistantTurn{{
			Ref: "trn-example", Sequence: 7, Actor: "SYSTEM_ASSISTANT", Content: "План подготовлен", State: "COMPLETED",
		}},
		LatestPlan: &entity.AssistantPlan{
			Ref: "pln-example", ConversationRef: "cnv-example", ProjectRef: "prj-example",
			State: "DRAFT", Summary: "План готов", Version: 1, Revision: 1,
		},
	})
	if len(conversation.GetTurns()) != 2 {
		t.Fatalf("assistant turn count = %d, want 2", len(conversation.GetTurns()))
	}
	if turn := conversation.GetTurns()[0]; turn.GetRole() != "ASSISTANT" || turn.GetContent() != "План подготовлен" || turn.GetSequence() != 7 {
		t.Fatalf("system assistant turn leaked internal role: %#v", turn)
	}
	turn := conversation.GetTurns()[1]
	if turn.GetRole() != "ASSISTANT" || turn.GetState() != "COMPLETED" || turn.GetSequence() != 8 {
		t.Fatalf("assistant plan turn = role %q state %q", turn.GetRole(), turn.GetState())
	}
	if turn.GetPlan().GetConversationRef() != "cnv-example" || turn.GetPlan().GetProjectRef() != "prj-example" {
		t.Fatalf("assistant plan lineage was lost: %#v", turn.GetPlan())
	}
}
