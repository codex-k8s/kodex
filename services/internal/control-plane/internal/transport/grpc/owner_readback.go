package grpc

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ownerProjectionStatusToProto(status resource.OwnerProjectionStatus) controlplanev1.OwnerProjectionStatus {
	return map[resource.OwnerProjectionStatus]controlplanev1.OwnerProjectionStatus{
		resource.OwnerProjectionPresent:     controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_PRESENT,
		resource.OwnerProjectionUnavailable: controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_UNAVAILABLE,
		resource.OwnerProjectionStale:       controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_STALE,
		resource.OwnerProjectionIneligible:  controlplanev1.OwnerProjectionStatus_OWNER_PROJECTION_STATUS_INELIGIBLE,
	}[status]
}

func ownerSafeSelectionToProto(value resource.OwnerSafeSelection) *controlplanev1.OwnerSafeSelection {
	return &controlplanev1.OwnerSafeSelection{StableSelector: value.StableSelector,
		DisplayName: value.DisplayName, Status: ownerProjectionStatusToProto(value.Status),
		Version: value.Version, Sha256: value.SHA256, MaskedStatus: value.MaskedStatus}
}

func agentOwnerProjectionToProto(value resource.AgentOwnerProjection) *controlplanev1.AgentOwnerProjection {
	botStatus := map[string]controlplanev1.AgentBotIdentityStatus{
		"UNBOUND": controlplanev1.AgentBotIdentityStatus_AGENT_BOT_IDENTITY_STATUS_UNBOUND,
		"BOUND":   controlplanev1.AgentBotIdentityStatus_AGENT_BOT_IDENTITY_STATUS_BOUND,
		"REVOKED": controlplanev1.AgentBotIdentityStatus_AGENT_BOT_IDENTITY_STATUS_REVOKED,
	}[value.BotIdentity.Status]
	return &controlplanev1.AgentOwnerProjection{AgentRef: value.AgentRef, DisplayName: value.DisplayName,
		StableKey: value.StableKey, Version: value.Version, State: toProtoState(value.State), Enabled: value.Enabled,
		Capabilities: value.Capabilities, BotIdentity: &controlplanev1.AgentBotIdentityProjection{
			Status: botStatus, Username: value.BotIdentity.Username, MaskedStatus: value.BotIdentity.MaskedStatus,
			ProviderGeneration: value.BotIdentity.ProviderGeneration,
		}, RuntimeSelection: &controlplanev1.AgentRuntimeSelectionProjection{
			SelectionKey: value.RuntimeSelection.SelectionKey, DisplayName: value.RuntimeSelection.DisplayName,
			RoleDefinitionVersion: value.RuntimeSelection.RoleDefinitionVersion,
			RoleDefinitionSha256:  value.RuntimeSelection.RoleDefinitionSHA256,
			RuntimeProfileVersion: value.RuntimeSelection.RuntimeProfileVersion,
			RuntimeProfileSha256:  value.RuntimeSelection.RuntimeProfileSHA256,
			Status:                ownerProjectionStatusToProto(value.RuntimeSelection.Status),
		}, InstructionSelection: ownerSafeSelectionToProto(value.InstructionSelection),
		ProviderPoolSelection: ownerSafeSelectionToProto(value.ProviderPoolSelection)}
}

func ownerSchedulePresetToProto(value resource.OwnerSchedulePreset) *controlplanev1.OwnerSchedulePreset {
	return &controlplanev1.OwnerSchedulePreset{Key: value.Key, DisplayName: value.DisplayName,
		Description: value.Description, Revision: value.Revision, Sha256: value.SHA256, Cron: value.Cron}
}

func ownerScheduleDefaultsToProto(value resource.OwnerScheduleDefaults) *controlplanev1.OwnerScheduleDefaults {
	return &controlplanev1.OwnerScheduleDefaults{Revision: value.Revision, Sha256: value.SHA256,
		Calendar: value.Calendar, OverlapPolicy: overlapPolicy(value.OverlapPolicy),
		MisfirePolicy: misfirePolicy(value.MisfirePolicy), DeliveryPolicy: value.DeliveryPolicy,
		MaximumAttempts: value.MaximumAttempts, InitialBackoff: durationpb.New(value.InitialBackoff),
		MaximumBackoff: durationpb.New(value.MaximumBackoff), DeadLetterAfter: durationpb.New(value.DeadLetterAfter),
		SessionPolicy:            scheduleSessionPolicy(value.SessionPolicy),
		NotificationPolicy:       scheduleNotificationPolicy(value.NotificationPolicy),
		MaximumExecutionDuration: durationpb.New(value.MaximumExecutionDuration), Coalesce: value.Coalesce}
}

func (server *Server) GetOwnerConfigurationCatalog(
	ctx context.Context,
	request *controlplanev1.GetOwnerConfigurationCatalogRequest,
) (*controlplanev1.GetOwnerConfigurationCatalogResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetOwnerConfigurationCatalog_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	catalog, err := server.service.GetOwnerConfigurationCatalog(ctx, principal, request.GetPageToken(), pageSize(request.GetPageSize()))
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.GetOwnerConfigurationCatalogResponse{ScheduleDefaults: ownerScheduleDefaultsToProto(catalog.ScheduleDefaults),
		NextPageToken: catalog.NextPageToken}
	for _, item := range catalog.RuntimeSelections {
		response.RuntimeSelections = append(response.RuntimeSelections, &controlplanev1.OwnerRuntimeSelectionCatalogEntry{
			SelectionKey: item.SelectionKey, DisplayName: item.DisplayName, Description: item.Description,
			RoleDefinitionVersion: item.RoleDefinitionVersion, RoleDefinitionSha256: item.RoleDefinitionSHA256,
			RuntimeProfileVersion: item.RuntimeProfileVersion, RuntimeProfileSha256: item.RuntimeProfileSHA256,
			Capabilities: item.Capabilities, Status: ownerProjectionStatusToProto(item.Status),
		})
	}
	for _, item := range catalog.SchedulePresets {
		response.SchedulePresets = append(response.SchedulePresets, ownerSchedulePresetToProto(item))
	}
	return response, nil
}

func ownerScheduleSelectionFromProto(intent *controlplanev1.OwnerScheduleIntent) (resource.OwnerScheduleSelection, error) {
	if intent == nil || intent.GetPresetKey() == "" || intent.GetTimezone() == "" || intent.GetPrompt() == nil {
		return resource.OwnerScheduleSelection{}, errs.ErrInvalidInput
	}
	selection := resource.OwnerScheduleSelection{PresetKey: intent.GetPresetKey(), Timezone: intent.GetTimezone(),
		RoomStableKey: intent.GetRoomStableKey(), Overrides: resource.OwnerScheduleOverrides{Present: map[string]bool{}}}
	switch source := intent.GetPrompt().GetSource().(type) {
	case *controlplanev1.OwnerSchedulePromptIntent_InlineMarkdown:
		selection.Prompt = resource.OwnerSchedulePromptInput{Kind: "INLINE", InlineMarkdown: source.InlineMarkdown}
	case *controlplanev1.OwnerSchedulePromptIntent_ArtifactName:
		selection.Prompt = resource.OwnerSchedulePromptInput{Kind: "SELECTOR", ArtifactName: source.ArtifactName}
	default:
		return resource.OwnerScheduleSelection{}, errs.ErrInvalidInput
	}
	overrides := intent.GetAdvancedOverrides()
	if overrides == nil {
		return selection, nil
	}
	setString := func(key string, present bool, target *string, value string) {
		if present {
			selection.Overrides.Present[key] = true
			*target = value
		}
	}
	setDuration := func(key string, source *durationpb.Duration, target *time.Duration) error {
		if source == nil {
			return nil
		}
		value, err := optionalDuration(source)
		if err != nil {
			return err
		}
		selection.Overrides.Present[key] = true
		*target = value
		return nil
	}
	setString("cron", overrides.Cron != nil, &selection.Overrides.Cron, overrides.GetCron())
	setString("calendar", overrides.Calendar != nil, &selection.Overrides.Calendar, overrides.GetCalendar())
	setString("overlap_policy", overrides.OverlapPolicy != nil, &selection.Overrides.OverlapPolicy,
		trimEnum(overrides.GetOverlapPolicy().String(), "SCHEDULE_OVERLAP_POLICY_"))
	setString("misfire_policy", overrides.MisfirePolicy != nil, &selection.Overrides.MisfirePolicy,
		trimEnum(overrides.GetMisfirePolicy().String(), "SCHEDULE_MISFIRE_POLICY_"))
	setString("delivery_policy", overrides.DeliveryPolicy != nil, &selection.Overrides.DeliveryPolicy, overrides.GetDeliveryPolicy())
	setString("session_policy", overrides.SessionPolicy != nil, &selection.Overrides.SessionPolicy,
		trimEnum(overrides.GetSessionPolicy().String(), "SCHEDULE_SESSION_POLICY_"))
	setString("notification_policy", overrides.NotificationPolicy != nil, &selection.Overrides.NotificationPolicy,
		trimEnum(overrides.GetNotificationPolicy().String(), "SCHEDULE_NOTIFICATION_POLICY_"))
	for _, item := range []struct {
		key    string
		source *durationpb.Duration
		target *time.Duration
	}{{"interval", overrides.GetInterval(), &selection.Overrides.Interval},
		{"misfire_grace", overrides.GetMisfireGrace(), &selection.Overrides.MisfireGrace},
		{"initial_backoff", overrides.GetInitialBackoff(), &selection.Overrides.InitialBackoff},
		{"maximum_backoff", overrides.GetMaximumBackoff(), &selection.Overrides.MaximumBackoff},
		{"dead_letter_after", overrides.GetDeadLetterAfter(), &selection.Overrides.DeadLetterAfter},
		{"maximum_execution_duration", overrides.GetMaximumExecutionDuration(), &selection.Overrides.MaximumExecutionDuration}} {
		if err := setDuration(item.key, item.source, item.target); err != nil {
			return resource.OwnerScheduleSelection{}, errs.ErrInvalidInput
		}
	}
	if overrides.MaximumAttempts != nil {
		selection.Overrides.Present["maximum_attempts"] = true
		selection.Overrides.MaximumAttempts = overrides.GetMaximumAttempts()
	}
	if overrides.Coalesce != nil {
		selection.Overrides.Present["coalesce"] = true
		selection.Overrides.Coalesce = overrides.GetCoalesce()
	}
	return selection, nil
}

func ownerScheduleProjectionToProto(value resource.OwnerScheduleProjection) *controlplanev1.OwnerScheduleProjection {
	result := &controlplanev1.OwnerScheduleProjection{ScheduleRef: ownerOpaqueResponseRef(value.ScheduleRef),
		DisplayName: value.DisplayName, Version: value.Version, State: toProtoState(value.State), PresetKey: value.PresetKey,
		PresetRevision: value.PresetRevision, PresetSha256: value.PresetSHA256,
		DefaultsRevision: value.DefaultsRevision, DefaultsSha256: value.DefaultsSHA256,
		Timezone: value.Timezone, Cron: value.Cron, Interval: optionalProtoDuration(value.Interval),
		PromptKind: value.PromptKind, PromptDisplay: value.PromptDisplay, PromptVersion: value.PromptVersion,
		PromptSha256: value.PromptSHA256, AdvancedOverrides: value.AdvancedOverrides,
		OverlapPolicy: overlapPolicy(value.OverlapPolicy), MisfirePolicy: misfirePolicy(value.MisfirePolicy),
		SessionPolicy:      scheduleSessionPolicy(value.SessionPolicy),
		NotificationPolicy: scheduleNotificationPolicy(value.NotificationPolicy), Coalesce: value.Coalesce,
		NextRunAt: timestamppb.New(value.NextRunAt), Calendar: value.Calendar,
		MisfireGrace: optionalProtoDuration(value.MisfireGrace), DeliveryPolicy: value.DeliveryPolicy,
		MaximumAttempts: value.MaximumAttempts, InitialBackoff: optionalProtoDuration(value.InitialBackoff),
		MaximumBackoff: optionalProtoDuration(value.MaximumBackoff), DeadLetterAfter: optionalProtoDuration(value.DeadLetterAfter),
		MaximumExecutionDuration: optionalProtoDuration(value.MaximumExecutionDuration),
		AgentSelection:           ownerSafeSelectionToProto(value.AgentSelection),
		InstructionSelection:     ownerSafeSelectionToProto(value.InstructionSelection),
		ProviderPoolSelection:    ownerSafeSelectionToProto(value.ProviderPoolSelection),
		RoomSelection:            ownerSafeSelectionToProto(value.RoomSelection)}
	for _, action := range value.NextActions {
		if enumValue, ok := controlplanev1.OwnerScheduleNextAction_value["OWNER_SCHEDULE_NEXT_ACTION_"+action]; ok {
			result.NextActions = append(result.NextActions, controlplanev1.OwnerScheduleNextAction(enumValue))
		}
	}
	result.Prompt = &controlplanev1.OwnerSchedulePromptProjection{Kind: value.Prompt.Kind,
		DisplayName: value.Prompt.DisplayName, Status: ownerProjectionStatusToProto(value.Prompt.Status),
		Version: value.Prompt.Version, Sha256: value.Prompt.SHA256}
	if value.Prompt.Kind == "INLINE" {
		result.Prompt.Source = &controlplanev1.OwnerSchedulePromptProjection_InlineMarkdown{
			InlineMarkdown: value.Prompt.InlineMarkdown}
	} else if value.Prompt.Kind == "SELECTOR" {
		result.Prompt.Source = &controlplanev1.OwnerSchedulePromptProjection_ArtifactSelector{
			ArtifactSelector: value.Prompt.ArtifactSelector}
	}
	return result
}

func ownerOpaqueResponseRef(value string) string { return value }

func (server *Server) ManageOwnerSchedule(ctx context.Context, request *controlplanev1.ManageOwnerScheduleRequest) (*controlplanev1.ManageOwnerScheduleResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageOwnerSchedule_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	selection, err := ownerScheduleSelectionFromProto(request.GetIntent())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	projection, err := server.service.ManageOwnerSchedule(ctx, resource.ManageOwnerScheduleInput{Principal: principal,
		IdempotencyKey: request.GetIdempotencyKey(), Action: trimEnum(request.GetAction().String(), "OWNER_SCHEDULE_COMMAND_ACTION_"),
		ScheduleID: request.GetScheduleId(), ExpectedVersion: request.GetExpectedVersion(), Name: request.GetName(),
		AgentStableKey: request.GetAgentStableKey(), InstructionSetStableKey: request.GetInstructionSetStableKey(),
		ProviderPoolStableKey: request.GetProviderPoolStableKey(), Selection: selection})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ManageOwnerScheduleResponse{Schedule: ownerScheduleProjectionToProto(projection)}, nil
}

func (server *Server) GetOwnerSchedule(ctx context.Context, request *controlplanev1.GetOwnerScheduleRequest) (*controlplanev1.GetOwnerScheduleResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetOwnerSchedule_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	projection, err := server.service.GetOwnerSchedule(ctx, principal, request.GetScheduleId())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetOwnerScheduleResponse{Schedule: ownerScheduleProjectionToProto(projection)}, nil
}

func (server *Server) ListOwnerSchedules(ctx context.Context, request *controlplanev1.ListOwnerSchedulesRequest) (*controlplanev1.ListOwnerSchedulesResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListOwnerSchedules_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, fromProtoState(state))
	}
	items, next, err := server.service.ListOwnerSchedules(ctx, principal, states, request.GetPageToken(), pageSize(request.GetPageSize()))
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListOwnerSchedulesResponse{NextPageToken: next}
	for _, item := range items {
		response.Schedules = append(response.Schedules, ownerScheduleProjectionToProto(item))
	}
	return response, nil
}

func ownerDisplayValueToProto(value resource.OwnerDisplayValue) *controlplanev1.OwnerDisplayValue {
	return &controlplanev1.OwnerDisplayValue{Status: ownerProjectionStatusToProto(value.Status), Value: value.Value}
}

func runNextActionToProto(action string) controlplanev1.RunNextAction {
	return map[string]controlplanev1.RunNextAction{"CANCEL": controlplanev1.RunNextAction_RUN_NEXT_ACTION_CANCEL,
		"RETRY": controlplanev1.RunNextAction_RUN_NEXT_ACTION_RETRY}[action]
}

func runTimelineKindToProto(kind string) controlplanev1.RunTimelineKind {
	return map[string]controlplanev1.RunTimelineKind{
		"STATE_CHANGE": controlplanev1.RunTimelineKind_RUN_TIMELINE_KIND_STATE_CHANGE,
	}[kind]
}

func runTimelineOutcomeToProto(outcome string) controlplanev1.RunTimelineOutcome {
	return map[string]controlplanev1.RunTimelineOutcome{
		"SUCCEEDED":      controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_SUCCEEDED,
		"FAILED":         controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_FAILED,
		"CANCELLED":      controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_CANCELLED,
		"EXPIRED":        controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_EXPIRED,
		"RETRIED":        controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_RETRIED,
		"REQUIRES_OWNER": controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_REQUIRES_OWNER,
		"OTHER":          controlplanev1.RunTimelineOutcome_RUN_TIMELINE_OUTCOME_OTHER,
	}[outcome]
}

func runLineageKindToProto(kind string) controlplanev1.RunLineageKind {
	return map[string]controlplanev1.RunLineageKind{
		"PROCESS": controlplanev1.RunLineageKind_RUN_LINEAGE_KIND_PROCESS,
		"ATTEMPT": controlplanev1.RunLineageKind_RUN_LINEAGE_KIND_ATTEMPT,
	}[kind]
}

func runLineageStateToProto(state string) controlplanev1.RunLineageState {
	return controlplanev1.RunLineageState(controlplanev1.RunLineageState_value["RUN_LINEAGE_STATE_"+state])
}

func runArtifactStatusToProto(status string) controlplanev1.RunArtifactStatus {
	return controlplanev1.RunArtifactStatus(controlplanev1.RunArtifactStatus_value["RUN_ARTIFACT_STATUS_"+status])
}

func runOwnerProjectionToProto(value resource.RunOwnerProjection) *controlplanev1.RunOwnerProjection {
	result := &controlplanev1.RunOwnerProjection{RunRef: value.RunRef, DisplayName: value.DisplayName,
		Version: value.Version, State: toProtoState(value.State), Workspace: ownerDisplayValueToProto(value.Workspace),
		Trigger: ownerDisplayValueToProto(value.Trigger), RuntimeStatus: ownerDisplayValueToProto(value.RuntimeStatus),
		Attempt: value.Attempt, StartedAt: timestamppb.New(value.StartedAt), UpdatedAt: timestamppb.New(value.UpdatedAt),
		Duration: optionalProtoDuration(value.Duration), Initiator: ownerDisplayValueToProto(value.Initiator),
		Agent: ownerDisplayValueToProto(value.Agent), Role: ownerDisplayValueToProto(value.Role),
		Model: ownerDisplayValueToProto(value.Model), Provider: ownerDisplayValueToProto(value.Provider)}
	for _, action := range value.NextActions {
		result.NextActions = append(result.NextActions, runNextActionToProto(action))
	}
	return result
}

func (server *Server) ListOwnerRuns(ctx context.Context, request *controlplanev1.ListOwnerRunsRequest) (*controlplanev1.ListOwnerRunsResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListOwnerRuns_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, fromProtoState(state))
	}
	items, next, err := server.service.ListOwnerRuns(ctx, principal, states, request.GetPageToken(), pageSize(request.GetPageSize()))
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListOwnerRunsResponse{NextPageToken: next}
	for _, item := range items {
		response.Runs = append(response.Runs, runOwnerProjectionToProto(item))
	}
	return response, nil
}

func runtimeIncidentNextActionToProto(action string) controlplanev1.RuntimeIncidentNextAction {
	return map[string]controlplanev1.RuntimeIncidentNextAction{
		"ACKNOWLEDGE": controlplanev1.RuntimeIncidentNextAction_RUNTIME_INCIDENT_NEXT_ACTION_ACKNOWLEDGE,
		"RETRY":       controlplanev1.RuntimeIncidentNextAction_RUNTIME_INCIDENT_NEXT_ACTION_RETRY,
		"RELEASE":     controlplanev1.RuntimeIncidentNextAction_RUNTIME_INCIDENT_NEXT_ACTION_RELEASE,
		"CLOSE":       controlplanev1.RuntimeIncidentNextAction_RUNTIME_INCIDENT_NEXT_ACTION_CLOSE,
	}[action]
}

func runtimeIncidentNextActionsToProto(actions []string) []controlplanev1.RuntimeIncidentNextAction {
	result := make([]controlplanev1.RuntimeIncidentNextAction, 0, len(actions))
	for _, action := range actions {
		result = append(result, runtimeIncidentNextActionToProto(action))
	}
	return result
}

func runtimeIncidentOwnerProjectionToProto(value resource.RuntimeIncidentOwnerProjection) *controlplanev1.RuntimeIncidentOwnerProjection {
	result := &controlplanev1.RuntimeIncidentOwnerProjection{IncidentRef: value.IncidentRef, Version: value.Version,
		Kind: toProtoRuntimeIncidentKind(value.Kind), State: runtimeIncidentStateToProto(value.State),
		Severity: map[string]controlplanev1.RuntimeIncidentSeverity{"WARNING": controlplanev1.RuntimeIncidentSeverity_RUNTIME_INCIDENT_SEVERITY_WARNING,
			"ERROR":    controlplanev1.RuntimeIncidentSeverity_RUNTIME_INCIDENT_SEVERITY_ERROR,
			"CRITICAL": controlplanev1.RuntimeIncidentSeverity_RUNTIME_INCIDENT_SEVERITY_CRITICAL}[value.Severity],
		Impact: value.Impact, Workspace: ownerDisplayValueToProto(value.Workspace), Run: ownerDisplayValueToProto(value.Run),
		SafeCorrelation: value.SafeCorrelation, DiagnosticSummary: value.DiagnosticSummary, RunbookUrl: value.RunbookURL,
		OccurredAt: timestamppb.New(value.OccurredAt), UpdatedAt: timestamppb.New(value.UpdatedAt),
		ExecutionFence: value.ExecutionFence}
	for _, action := range value.NextActions {
		result.NextActions = append(result.NextActions, runtimeIncidentNextActionToProto(action))
	}
	return result
}

func workspaceRestoreNextActionToProto(action string) controlplanev1.WorkspaceRestoreNextAction {
	return map[string]controlplanev1.WorkspaceRestoreNextAction{
		"CANCEL": controlplanev1.WorkspaceRestoreNextAction_WORKSPACE_RESTORE_NEXT_ACTION_CANCEL,
		"RETRY":  controlplanev1.WorkspaceRestoreNextAction_WORKSPACE_RESTORE_NEXT_ACTION_RETRY,
	}[action]
}

func workspaceRestoreOwnerProjectionToProto(value resource.WorkspaceRestoreOwnerProjection) *controlplanev1.WorkspaceRestoreOwnerProjection {
	result := &controlplanev1.WorkspaceRestoreOwnerProjection{RestoreRef: value.RestoreRef,
		DisplayName: value.DisplayName, Version: value.Version,
		State: workspaceRestoreStateToProto(value.State), Attempt: value.Attempt, Generation: value.Generation,
		MemberCount: value.MemberCount, TerminalReasonCode: value.TerminalReasonCode,
		CreatedAt: timestamppb.New(value.CreatedAt), UpdatedAt: timestamppb.New(value.UpdatedAt)}
	for _, action := range value.NextActions {
		result.NextActions = append(result.NextActions, workspaceRestoreNextActionToProto(action))
	}
	return result
}
