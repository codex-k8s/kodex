package httptransport

import (
	"errors"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/durationpb"
)

func castClosedEnum[T ~string](raw, prefix string, valid func(T) bool) (T, error) {
	value := T(strings.TrimPrefix(raw, prefix))
	if raw == "" || string(value) == raw || !valid(value) {
		return "", errors.New("authoritative closed enum is invalid")
	}
	return value, nil
}

func castLifecycleState(value controlplanev1.LifecycleState) (generated.LifecycleState, error) {
	return castClosedEnum(value.String(), "LIFECYCLE_STATE_", generated.LifecycleState.Valid)
}

func castOwnerProjectionStatus(value controlplanev1.OwnerProjectionStatus) (generated.OwnerProjectionStatus, error) {
	return castClosedEnum(value.String(), "OWNER_PROJECTION_STATUS_", generated.OwnerProjectionStatus.Valid)
}

func castOwnerDisplayValue(input *controlplanev1.OwnerDisplayValue) (generated.OwnerDisplayValue, error) {
	if input == nil || len(input.GetValue()) > 256 {
		return generated.OwnerDisplayValue{}, errors.New("owner display value is invalid")
	}
	status, err := castOwnerProjectionStatus(input.GetStatus())
	if err != nil {
		return generated.OwnerDisplayValue{}, err
	}
	return generated.OwnerDisplayValue{Status: status, Value: input.GetValue()}, nil
}

func castOwnerSafeSelection(input *controlplanev1.OwnerSafeSelection) (generated.OwnerSafeSelection, error) {
	if input == nil || len(input.GetStableSelector()) > 160 || len(input.GetDisplayName()) > 160 || len(input.GetMaskedStatus()) > 160 {
		return generated.OwnerSafeSelection{}, errors.New("owner safe selection is invalid")
	}
	status, err := castOwnerProjectionStatus(input.GetStatus())
	if err != nil {
		return generated.OwnerSafeSelection{}, err
	}
	result := generated.OwnerSafeSelection{Selector: input.GetStableSelector(), DisplayName: input.GetDisplayName(), Status: status, Version: int64(input.GetVersion())}
	if input.GetSha256() != "" {
		if !validSHA256(input.GetSha256()) {
			return generated.OwnerSafeSelection{}, errors.New("owner safe selection digest is invalid")
		}
		digest := generated.Sha256(strings.ToLower(input.GetSha256()))
		result.DigestSha256 = &digest
	}
	result.MaskedStatus = optionalString(input.GetMaskedStatus())
	return result, nil
}

func castAgentRuntimeSelection(input *controlplanev1.AgentRuntimeSelectionProjection) (generated.AgentRuntimeSelection, error) {
	if input == nil || input.GetSelectionKey() == "" || input.GetDisplayName() == "" || input.GetRoleDefinitionVersion() == 0 || input.GetRuntimeProfileVersion() == 0 ||
		!validSHA256(input.GetRoleDefinitionSha256()) || !validSHA256(input.GetRuntimeProfileSha256()) {
		return generated.AgentRuntimeSelection{}, errors.New("agent runtime selection is invalid")
	}
	status, err := castOwnerProjectionStatus(input.GetStatus())
	if err != nil {
		return generated.AgentRuntimeSelection{}, err
	}
	return generated.AgentRuntimeSelection{SelectionKey: input.GetSelectionKey(), DisplayName: input.GetDisplayName(),
		RoleDefinitionVersion: int64(input.GetRoleDefinitionVersion()), RoleDefinitionSha256: generated.Sha256(strings.ToLower(input.GetRoleDefinitionSha256())),
		RuntimeProfileVersion: int64(input.GetRuntimeProfileVersion()), RuntimeProfileSha256: generated.Sha256(strings.ToLower(input.GetRuntimeProfileSha256())), Status: status}, nil
}

func castAgentView(input *controlplanev1.AgentOwnerProjection) (generated.AgentView, error) {
	if input == nil || input.GetAgentRef() == "" || input.GetDisplayName() == "" || input.GetStableKey() == "" || input.GetVersion() == 0 || len(input.GetCapabilities()) > 64 || input.GetBotIdentity() == nil {
		return generated.AgentView{}, errors.New("agent owner projection is incomplete")
	}
	state, stateErr := castLifecycleState(input.GetState())
	runtimeSelection, runtimeErr := castAgentRuntimeSelection(input.GetRuntimeSelection())
	instruction, instructionErr := castOwnerSafeSelection(input.GetInstructionSelection())
	pool, poolErr := castOwnerSafeSelection(input.GetProviderPoolSelection())
	botStatus, botErr := castClosedEnum(input.GetBotIdentity().GetStatus().String(), "AGENT_BOT_IDENTITY_STATUS_", generated.AgentBindingStatus.Valid)
	if stateErr != nil || runtimeErr != nil || instructionErr != nil || poolErr != nil || botErr != nil {
		return generated.AgentView{}, errors.New("agent owner projection values are invalid")
	}
	return generated.AgentView{AgentRef: input.GetAgentRef(), DisplayName: input.GetDisplayName(), StableKey: input.GetStableKey(), Version: int64(input.GetVersion()), State: state,
		Enabled: input.GetEnabled(), Capabilities: append([]string(nil), input.GetCapabilities()...), RuntimeSelection: runtimeSelection, InstructionSelection: instruction, ProviderPoolSelection: pool,
		BotIdentity: generated.AgentBotIdentitySummary{Status: botStatus, Username: input.GetBotIdentity().GetUsername(), MaskedStatus: input.GetBotIdentity().GetMaskedStatus(), ProviderGeneration: int64(input.GetBotIdentity().GetProviderGeneration())}}, nil
}

func castRuntimeSelectionCatalogEntry(input *controlplanev1.OwnerRuntimeSelectionCatalogEntry) (generated.RuntimeSelectionCatalogEntry, error) {
	if input == nil || input.GetSelectionKey() == "" || input.GetDisplayName() == "" || input.GetRoleDefinitionVersion() == 0 || input.GetRuntimeProfileVersion() == 0 ||
		!validSHA256(input.GetRoleDefinitionSha256()) || !validSHA256(input.GetRuntimeProfileSha256()) || len(input.GetCapabilities()) > 64 {
		return generated.RuntimeSelectionCatalogEntry{}, errors.New("runtime selection catalog entry is invalid")
	}
	status, err := castOwnerProjectionStatus(input.GetStatus())
	if err != nil {
		return generated.RuntimeSelectionCatalogEntry{}, err
	}
	return generated.RuntimeSelectionCatalogEntry{SelectionKey: input.GetSelectionKey(), DisplayName: input.GetDisplayName(), Description: input.GetDescription(),
		RoleDefinitionVersion: int64(input.GetRoleDefinitionVersion()), RoleDefinitionSha256: generated.Sha256(strings.ToLower(input.GetRoleDefinitionSha256())),
		RuntimeProfileVersion: int64(input.GetRuntimeProfileVersion()), RuntimeProfileSha256: generated.Sha256(strings.ToLower(input.GetRuntimeProfileSha256())), Capabilities: append([]string(nil), input.GetCapabilities()...), Status: status}, nil
}

func castScheduleDefaults(input *controlplanev1.OwnerScheduleDefaults) (generated.ScheduleDefaults, error) {
	if input == nil || input.GetRevision() == 0 || !validSHA256(input.GetSha256()) {
		return generated.ScheduleDefaults{}, errors.New("schedule defaults are invalid")
	}
	overlap, err1 := castClosedEnum(input.GetOverlapPolicy().String(), "SCHEDULE_OVERLAP_POLICY_", generated.ScheduleOverlapPolicy.Valid)
	misfire, err2 := castClosedEnum(input.GetMisfirePolicy().String(), "SCHEDULE_MISFIRE_POLICY_", generated.ScheduleMisfirePolicy.Valid)
	session, err3 := castClosedEnum(input.GetSessionPolicy().String(), "SCHEDULE_SESSION_POLICY_", generated.ScheduleSessionPolicy.Valid)
	notification, err4 := castClosedEnum(input.GetNotificationPolicy().String(), "SCHEDULE_NOTIFICATION_POLICY_", generated.ScheduleNotificationPolicy.Valid)
	calendar := generated.ScheduleDefaultsCalendar(input.GetCalendar())
	delivery := generated.ScheduleDefaultsDeliveryPolicy(input.GetDeliveryPolicy())
	initial, err5 := durationSeconds(input.GetInitialBackoff())
	maximum, err6 := durationSeconds(input.GetMaximumBackoff())
	deadLetter, err7 := durationSeconds(input.GetDeadLetterAfter())
	execution, err8 := durationSeconds(input.GetMaximumExecutionDuration())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil || err8 != nil || !calendar.Valid() || !delivery.Valid() || input.GetMaximumAttempts() == 0 {
		return generated.ScheduleDefaults{}, errors.New("schedule defaults values are invalid")
	}
	return generated.ScheduleDefaults{Revision: int64(input.GetRevision()), DigestSha256: generated.Sha256(strings.ToLower(input.GetSha256())), Calendar: calendar,
		OverlapPolicy: overlap, MisfirePolicy: misfire, DeliveryPolicy: delivery, MaximumAttempts: int(input.GetMaximumAttempts()), InitialBackoffSeconds: initial,
		MaximumBackoffSeconds: maximum, DeadLetterAfterSeconds: deadLetter, SessionPolicy: session, NotificationPolicy: notification, MaximumExecutionSeconds: execution, Coalesce: input.GetCoalesce()}, nil
}

func durationSeconds(value *durationpb.Duration) (int64, error) {
	if value == nil || value.CheckValid() != nil || value.AsDuration() < 0 || value.AsDuration()%time.Second != 0 {
		return 0, errors.New("duration is invalid")
	}
	return int64(value.AsDuration() / time.Second), nil
}

func optionalDurationSeconds(value *durationpb.Duration) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	seconds, err := durationSeconds(value)
	return &seconds, err
}

func castSchedulePreset(input *controlplanev1.OwnerSchedulePreset) (generated.SchedulePreset, error) {
	if input == nil || input.GetKey() == "" || input.GetDisplayName() == "" || input.GetRevision() == 0 || !validSHA256(input.GetSha256()) || input.GetCron() == "" {
		return generated.SchedulePreset{}, errors.New("schedule preset is invalid")
	}
	return generated.SchedulePreset{Key: input.GetKey(), DisplayName: input.GetDisplayName(), Description: input.GetDescription(), Revision: int64(input.GetRevision()), DigestSha256: generated.Sha256(strings.ToLower(input.GetSha256())), Cron: input.GetCron()}, nil
}

func castOwnerScheduleView(input *controlplanev1.OwnerScheduleProjection) (generated.OwnerScheduleView, error) {
	if input == nil || input.GetScheduleRef() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 || input.GetPresetKey() == "" || input.GetPresetRevision() == 0 || input.GetDefaultsRevision() == 0 ||
		!validSHA256(input.GetPresetSha256()) || !validSHA256(input.GetDefaultsSha256()) || input.GetPrompt() == nil {
		return generated.OwnerScheduleView{}, errors.New("owner schedule projection is incomplete")
	}
	state, err1 := castLifecycleState(input.GetState())
	overlap, err2 := castClosedEnum(input.GetOverlapPolicy().String(), "SCHEDULE_OVERLAP_POLICY_", generated.ScheduleOverlapPolicy.Valid)
	misfire, err3 := castClosedEnum(input.GetMisfirePolicy().String(), "SCHEDULE_MISFIRE_POLICY_", generated.ScheduleMisfirePolicy.Valid)
	session, err4 := castClosedEnum(input.GetSessionPolicy().String(), "SCHEDULE_SESSION_POLICY_", generated.ScheduleSessionPolicy.Valid)
	notification, err5 := castClosedEnum(input.GetNotificationPolicy().String(), "SCHEDULE_NOTIFICATION_POLICY_", generated.ScheduleNotificationPolicy.Valid)
	calendar := generated.OwnerScheduleViewCalendar(input.GetCalendar())
	delivery := generated.OwnerScheduleViewDeliveryPolicy(input.GetDeliveryPolicy())
	interval, err6 := optionalDurationSeconds(input.GetInterval())
	misfireGrace, err7 := durationSeconds(input.GetMisfireGrace())
	initial, err8 := durationSeconds(input.GetInitialBackoff())
	maximum, err9 := durationSeconds(input.GetMaximumBackoff())
	deadLetter, err10 := durationSeconds(input.GetDeadLetterAfter())
	execution, err11 := durationSeconds(input.GetMaximumExecutionDuration())
	agent, err12 := castOwnerSafeSelection(input.GetAgentSelection())
	instruction, err13 := castOwnerSafeSelection(input.GetInstructionSelection())
	pool, err14 := castOwnerSafeSelection(input.GetProviderPoolSelection())
	room, err15 := castOwnerSafeSelection(input.GetRoomSelection())
	prompt, err16 := castOwnerSchedulePrompt(input.GetPrompt())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil || err8 != nil || err9 != nil || err10 != nil || err11 != nil || err12 != nil || err13 != nil || err14 != nil || err15 != nil || err16 != nil || !calendar.Valid() || !delivery.Valid() || input.GetMaximumAttempts() == 0 {
		return generated.OwnerScheduleView{}, errors.New("owner schedule projection values are invalid")
	}
	result := generated.OwnerScheduleView{ScheduleRef: input.GetScheduleRef(), DisplayName: input.GetDisplayName(), Version: int64(input.GetVersion()), State: state,
		PresetKey: input.GetPresetKey(), PresetRevision: int64(input.GetPresetRevision()), PresetSha256: generated.Sha256(strings.ToLower(input.GetPresetSha256())), DefaultsRevision: int64(input.GetDefaultsRevision()), DefaultsSha256: generated.Sha256(strings.ToLower(input.GetDefaultsSha256())),
		Timezone: input.GetTimezone(), Cron: optionalString(input.GetCron()), IntervalSeconds: interval, AdvancedOverrides: append([]string(nil), input.GetAdvancedOverrides()...), Calendar: calendar,
		OverlapPolicy: overlap, MisfirePolicy: misfire, MisfireGraceSeconds: misfireGrace, DeliveryPolicy: delivery, MaximumAttempts: int(input.GetMaximumAttempts()), InitialBackoffSeconds: initial, MaximumBackoffSeconds: maximum,
		DeadLetterAfterSeconds: deadLetter, SessionPolicy: session, NotificationPolicy: notification, MaximumExecutionSeconds: execution, Coalesce: input.GetCoalesce(), AgentSelection: agent, InstructionSelection: instruction, ProviderPoolSelection: pool, RoomSelection: room, Prompt: prompt}
	if input.GetNextRunAt() != nil {
		value, err := requiredTimestamp(input.GetNextRunAt())
		if err != nil {
			return generated.OwnerScheduleView{}, err
		}
		result.NextRunAt = &value
	}
	return result, nil
}

func castOwnerSchedulePrompt(input *controlplanev1.OwnerSchedulePromptProjection) (generated.OwnerSchedulePromptView, error) {
	status, err := castOwnerProjectionStatus(input.GetStatus())
	kind := generated.OwnerSchedulePromptViewKind(input.GetKind())
	if err != nil || !kind.Valid() || input.GetDisplayName() == "" || (input.GetSha256() != "" && !validSHA256(input.GetSha256())) {
		return generated.OwnerSchedulePromptView{}, errors.New("schedule prompt projection is invalid")
	}
	result := generated.OwnerSchedulePromptView{Kind: kind, DisplayName: input.GetDisplayName(), Status: status, Version: int64(input.GetVersion())}
	if value := input.GetInlineMarkdown(); value != "" {
		result.InlineMarkdown = &value
	}
	if value := input.GetArtifactSelector(); value != "" {
		result.ArtifactSelector = &value
	}
	if (result.InlineMarkdown == nil) == (result.ArtifactSelector == nil) {
		return generated.OwnerSchedulePromptView{}, errors.New("schedule prompt source is invalid")
	}
	if input.GetSha256() != "" {
		digest := generated.Sha256(strings.ToLower(input.GetSha256()))
		result.DigestSha256 = &digest
	}
	return result, nil
}

func castRunView(input *controlplanev1.RunOwnerProjection) (generated.RunView, error) {
	if input == nil || input.GetRunRef() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 || input.GetUpdatedAt() == nil {
		return generated.RunView{}, errors.New("run owner projection is incomplete")
	}
	state, err1 := castLifecycleState(input.GetState())
	workspace, err2 := castOwnerDisplayValue(input.GetWorkspace())
	trigger, err3 := castOwnerDisplayValue(input.GetTrigger())
	runtimeStatus, err4 := castOwnerDisplayValue(input.GetRuntimeStatus())
	initiator, err5 := castOwnerDisplayValue(input.GetInitiator())
	agent, err6 := castOwnerDisplayValue(input.GetAgent())
	role, err7 := castOwnerDisplayValue(input.GetRole())
	model, err8 := castOwnerDisplayValue(input.GetModel())
	provider, err9 := castOwnerDisplayValue(input.GetProvider())
	updated, err10 := requiredTimestamp(input.GetUpdatedAt())
	duration, err11 := durationSeconds(input.GetDuration())
	next, err12 := castRunNextActions(input.GetNextActions())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil || err8 != nil || err9 != nil || err10 != nil || err11 != nil || err12 != nil {
		return generated.RunView{}, errors.New("run owner projection values are invalid")
	}
	result := generated.RunView{RunRef: input.GetRunRef(), DisplayName: input.GetDisplayName(), Version: int64(input.GetVersion()), State: state, Workspace: workspace, Trigger: trigger,
		RuntimeStatus: runtimeStatus, Attempt: int(input.GetAttempt()), UpdatedAt: updated, DurationSeconds: duration, NextActions: next, Initiator: initiator, Agent: agent, Role: role, Model: model, Provider: provider}
	if input.GetStartedAt() != nil {
		started, err := requiredTimestamp(input.GetStartedAt())
		if err != nil {
			return generated.RunView{}, err
		}
		result.StartedAt = &started
	}
	return result, nil
}

func castRunNextActions(input []controlplanev1.RunNextAction) ([]generated.RunNextAction, error) {
	result := make([]generated.RunNextAction, 0, len(input))
	seen := make(map[generated.RunNextAction]struct{}, len(input))
	for _, item := range input {
		value, err := castClosedEnum(item.String(), "RUN_NEXT_ACTION_", generated.RunNextAction.Valid)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, errors.New("run next action is duplicated")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func castIncidentNextActions(input []controlplanev1.RuntimeIncidentNextAction) ([]generated.IncidentNextAction, error) {
	result := make([]generated.IncidentNextAction, 0, len(input))
	seen := make(map[generated.IncidentNextAction]struct{}, len(input))
	for _, item := range input {
		value, err := castClosedEnum(item.String(), "RUNTIME_INCIDENT_NEXT_ACTION_", generated.IncidentNextAction.Valid)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, errors.New("incident next action is duplicated")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func castIncidentView(input *controlplanev1.RuntimeIncidentOwnerProjection) (generated.IncidentView, error) {
	if input == nil || input.GetIncidentRef() == "" || input.GetVersion() == 0 || input.GetOccurredAt() == nil || input.GetUpdatedAt() == nil || input.GetExecutionFence() == 0 {
		return generated.IncidentView{}, errors.New("incident owner projection is incomplete")
	}
	kind, err1 := castClosedEnum(input.GetKind().String(), "RUNTIME_INCIDENT_KIND_", generated.RuntimeIncidentKind.Valid)
	state, err2 := castClosedEnum(input.GetState().String(), "RUNTIME_INCIDENT_STATE_", generated.IncidentState.Valid)
	severity, err3 := castClosedEnum(input.GetSeverity().String(), "RUNTIME_INCIDENT_SEVERITY_", generated.IncidentSeverity.Valid)
	workspace, err4 := castOwnerDisplayValue(input.GetWorkspace())
	run, err5 := castOwnerDisplayValue(input.GetRun())
	occurred, err6 := requiredTimestamp(input.GetOccurredAt())
	updated, err7 := requiredTimestamp(input.GetUpdatedAt())
	next, err8 := castIncidentNextActions(input.GetNextActions())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil || err8 != nil {
		return generated.IncidentView{}, errors.New("incident owner projection values are invalid")
	}
	return generated.IncidentView{IncidentRef: input.GetIncidentRef(), Version: int64(input.GetVersion()), Kind: kind, State: state, Severity: severity, Impact: input.GetImpact(), Workspace: workspace, Run: run,
		SafeCorrelation: input.GetSafeCorrelation(), DiagnosticSummary: input.GetDiagnosticSummary(), RunbookUrl: input.GetRunbookUrl(), OccurredAt: occurred, UpdatedAt: updated, NextActions: next, ExecutionFence: int64(input.GetExecutionFence())}, nil
}

func castRunSession(input *controlplanev1.Resource) (generated.RunSessionView, error) {
	if input == nil || input.GetKind() != controlplanev1.ResourceKind_RESOURCE_KIND_SESSION || input.GetId() == "" || input.GetName() == "" || input.GetVersion() == 0 {
		return generated.RunSessionView{}, errors.New("run session projection is invalid")
	}
	state, err1 := castLifecycleState(input.GetState())
	updated, err2 := requiredTimestamp(input.GetUpdatedAt())
	if err1 != nil || err2 != nil {
		return generated.RunSessionView{}, errors.New("run session projection values are invalid")
	}
	return generated.RunSessionView{SessionRef: input.GetId(), DisplayName: input.GetName(), State: state, Version: int64(input.GetVersion()), UpdatedAt: updated}, nil
}

func castRunTurn(input *controlplanev1.Resource) (generated.RunTurnView, error) {
	if input == nil || input.GetKind() != controlplanev1.ResourceKind_RESOURCE_KIND_TURN || input.GetId() == "" || input.GetName() == "" || input.GetVersion() == 0 {
		return generated.RunTurnView{}, errors.New("run turn projection is invalid")
	}
	state, err1 := castLifecycleState(input.GetState())
	updated, err2 := requiredTimestamp(input.GetUpdatedAt())
	if err1 != nil || err2 != nil {
		return generated.RunTurnView{}, errors.New("run turn projection values are invalid")
	}
	return generated.RunTurnView{TurnRef: input.GetId(), DisplayName: input.GetName(), State: state, Version: int64(input.GetVersion()), UpdatedAt: updated}, nil
}

func castRunTimelineEntry(input *controlplanev1.RunTimelineEntry) (generated.RunTimelineEntry, error) {
	if input == nil || input.GetEventRef() == "" || input.GetDisplay() == "" || input.GetVersion() == 0 {
		return generated.RunTimelineEntry{}, errors.New("run timeline projection is incomplete")
	}
	kind, err1 := castClosedEnum(input.GetKind().String(), "RUN_TIMELINE_KIND_", generated.RunTimelineKind.Valid)
	outcome, err2 := castClosedEnum(input.GetOutcome().String(), "RUN_TIMELINE_OUTCOME_", generated.RunTimelineOutcome.Valid)
	occurred, err3 := requiredTimestamp(input.GetOccurredAt())
	next, err4 := castRunNextActions(input.GetNextActions())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return generated.RunTimelineEntry{}, errors.New("run timeline projection values are invalid")
	}
	return generated.RunTimelineEntry{EventRef: input.GetEventRef(), Kind: kind, Display: input.GetDisplay(), Outcome: outcome, Version: int64(input.GetVersion()), OccurredAt: occurred, NextActions: next}, nil
}

func castRunLineageNode(input *controlplanev1.RunLineageNode) (generated.RunLineageNode, error) {
	if input == nil || input.GetNodeRef() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 {
		return generated.RunLineageNode{}, errors.New("run lineage projection is incomplete")
	}
	kind, err1 := castClosedEnum(input.GetKind().String(), "RUN_LINEAGE_KIND_", generated.RunLineageKind.Valid)
	state, err2 := castClosedEnum(input.GetState().String(), "RUN_LINEAGE_STATE_", generated.RunLineageState.Valid)
	created, err3 := requiredTimestamp(input.GetCreatedAt())
	updated, err4 := requiredTimestamp(input.GetUpdatedAt())
	agent, err5 := castOwnerDisplayValue(input.GetAgent())
	role, err6 := castOwnerDisplayValue(input.GetRole())
	model, err7 := castOwnerDisplayValue(input.GetModel())
	provider, err8 := castOwnerDisplayValue(input.GetProvider())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || err7 != nil || err8 != nil {
		return generated.RunLineageNode{}, errors.New("run lineage projection values are invalid")
	}
	return generated.RunLineageNode{NodeRef: input.GetNodeRef(), ParentRef: optionalString(input.GetParentRef()), Kind: kind, State: state, Version: int64(input.GetVersion()), Attempt: int(input.GetAttempt()), CreatedAt: created, UpdatedAt: updated,
		DisplayName: input.GetDisplayName(), Agent: agent, Role: role, Model: model, Provider: provider}, nil
}

func castRunArtifact(input *controlplanev1.RunArtifactProjection) (generated.RunArtifactView, error) {
	if input == nil || input.GetArtifactRef() == "" || input.GetDisplayName() == "" || input.GetKind() == "" || input.GetMediaType() == "" || !validSHA256(input.GetSha256()) {
		return generated.RunArtifactView{}, errors.New("run artifact projection is incomplete")
	}
	status, err1 := castClosedEnum(input.GetStatus().String(), "RUN_ARTIFACT_STATUS_", generated.RunArtifactStatus.Valid)
	created, err2 := requiredTimestamp(input.GetCreatedAt())
	if err1 != nil || err2 != nil {
		return generated.RunArtifactView{}, errors.New("run artifact projection values are invalid")
	}
	return generated.RunArtifactView{ArtifactRef: input.GetArtifactRef(), DisplayName: input.GetDisplayName(), Kind: input.GetKind(), MediaType: input.GetMediaType(), SizeBytes: int64(input.GetSizeBytes()), Sha256: generated.Sha256(strings.ToLower(input.GetSha256())), Status: status, CreatedAt: created}, nil
}

func castWorkspaceRestoreView(input *controlplanev1.WorkspaceRestoreOwnerProjection) (generated.WorkspaceRestoreView, error) {
	if input == nil || input.GetRestoreRef() == "" || input.GetDisplayName() == "" || input.GetVersion() == 0 || input.GetAttempt() == 0 || input.GetGeneration() == 0 {
		return generated.WorkspaceRestoreView{}, errors.New("workspace restore projection is incomplete")
	}
	state, err1 := castClosedEnum(input.GetState().String(), "WORKSPACE_RESTORE_STATE_", generated.WorkspaceRestoreState.Valid)
	created, err2 := requiredTimestamp(input.GetCreatedAt())
	updated, err3 := requiredTimestamp(input.GetUpdatedAt())
	next := make([]generated.WorkspaceRestoreNextAction, 0, len(input.GetNextActions()))
	for _, item := range input.GetNextActions() {
		value, err := castClosedEnum(item.String(), "WORKSPACE_RESTORE_NEXT_ACTION_", generated.WorkspaceRestoreNextAction.Valid)
		if err != nil {
			return generated.WorkspaceRestoreView{}, err
		}
		next = append(next, value)
	}
	if err1 != nil || err2 != nil || err3 != nil {
		return generated.WorkspaceRestoreView{}, errors.New("workspace restore projection values are invalid")
	}
	return generated.WorkspaceRestoreView{RestoreRef: input.GetRestoreRef(), DisplayName: input.GetDisplayName(), Version: int64(input.GetVersion()), State: state, Attempt: int(input.GetAttempt()), Generation: int64(input.GetGeneration()), MemberCount: int(input.GetMemberCount()), TerminalReasonCode: input.GetTerminalReasonCode(), CreatedAt: created, UpdatedAt: updated, NextActions: next}, nil
}

func castBotIdentity(input *interactiongatewayv1.AgentMattermostBotIdentityView) (generated.AgentBotIdentity, error) {
	if input == nil || input.GetSelector() == "" || input.GetUsername() == "" || input.GetDisplayName() == "" || input.GetProviderVersion() == 0 || input.GetProviderGeneration() == 0 || !validSHA256(input.GetProviderSnapshotSha256()) {
		return generated.AgentBotIdentity{}, errors.New("agent bot identity projection is incomplete")
	}
	status, err1 := castClosedEnum(input.GetStatus().String(), "AGENT_MATTERMOST_BOT_IDENTITY_STATUS_", generated.AgentBotIdentityStatus.Valid)
	observed, err2 := requiredTimestamp(input.GetObservedAt())
	updated, err3 := requiredTimestamp(input.GetUpdatedAt())
	if err1 != nil || err2 != nil || err3 != nil {
		return generated.AgentBotIdentity{}, errors.New("agent bot identity projection values are invalid")
	}
	return generated.AgentBotIdentity{Selector: input.GetSelector(), Username: input.GetUsername(), DisplayName: input.GetDisplayName(), Status: status, ProviderVersion: int64(input.GetProviderVersion()), ProviderGeneration: int64(input.GetProviderGeneration()), ProviderSnapshotSha256: generated.Sha256(strings.ToLower(input.GetProviderSnapshotSha256())), ObservedAt: observed, UpdatedAt: updated}, nil
}

func castBotBinding(input *interactiongatewayv1.AgentMattermostBotIdentityBindingView) (generated.AgentBotIdentityBinding, error) {
	if input == nil || input.GetAgentRef() == "" || input.GetAgentVersion() == 0 || !validSHA256(input.GetReceiptSha256()) {
		return generated.AgentBotIdentityBinding{}, errors.New("agent bot identity binding is incomplete")
	}
	identity, err1 := castBotIdentity(input.GetIdentity())
	updated, err2 := requiredTimestamp(input.GetUpdatedAt())
	if err1 != nil || err2 != nil {
		return generated.AgentBotIdentityBinding{}, errors.New("agent bot identity binding values are invalid")
	}
	return generated.AgentBotIdentityBinding{AgentRef: input.GetAgentRef(), AgentVersion: int64(input.GetAgentVersion()), Identity: identity, ReceiptSha256: generated.Sha256(strings.ToLower(input.GetReceiptSha256())), UpdatedAt: updated}, nil
}

func castBotOperation(input *interactiongatewayv1.AgentMattermostBotIdentityOperationView) (generated.AgentBotIdentityOperation, error) {
	if input == nil || input.GetOperationId() == "" || input.GetAgentRef() == "" || input.GetExpectedAgentVersion() == 0 {
		return generated.AgentBotIdentityOperation{}, errors.New("agent bot identity operation is incomplete")
	}
	action, err1 := castClosedEnum(input.GetAction().String(), "AGENT_MATTERMOST_BOT_IDENTITY_ACTION_", generated.AgentBotIdentityAction.Valid)
	state, err2 := castClosedEnum(input.GetState().String(), "AGENT_MATTERMOST_BOT_IDENTITY_OPERATION_STATE_", generated.AgentBotIdentityOperationState.Valid)
	created, err3 := requiredTimestamp(input.GetCreatedAt())
	updated, err4 := requiredTimestamp(input.GetUpdatedAt())
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return generated.AgentBotIdentityOperation{}, errors.New("agent bot identity operation values are invalid")
	}
	result := generated.AgentBotIdentityOperation{OperationRef: input.GetOperationId(), Action: action, State: state, AgentRef: input.GetAgentRef(), ExpectedAgentVersion: int64(input.GetExpectedAgentVersion()), PredecessorGeneration: int64(input.GetPredecessorGeneration()), FailureCode: optionalString(input.GetFailureCode()), CreatedAt: created, UpdatedAt: updated}
	if input.GetRetryNotBefore() != nil {
		value, err := requiredTimestamp(input.GetRetryNotBefore())
		if err != nil {
			return generated.AgentBotIdentityOperation{}, err
		}
		result.RetryNotBefore = &value
	}
	if input.GetRecoveryDeadline() != nil {
		value, err := requiredTimestamp(input.GetRecoveryDeadline())
		if err != nil {
			return generated.AgentBotIdentityOperation{}, err
		}
		result.RecoveryDeadline = &value
	}
	if input.GetResult() != nil {
		value, err := castBotBinding(input.GetResult())
		if err != nil {
			return generated.AgentBotIdentityOperation{}, err
		}
		result.Result = &value
	}
	return result, nil
}
