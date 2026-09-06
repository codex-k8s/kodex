package httptransport

import (
	"net/http"
	"slices"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/structpb"
)

func schedulePromptContext(value *generated.SchedulePromptPreviewContext) (*cp.SchedulePromptPreviewContext, bool) {
	if value == nil {
		return nil, true
	}
	if !fileTargetRef(value.ProjectRef) || !fileTargetRef(value.TargetRef) || !value.TargetType.Valid() || !validSearchText(value.Name, 1, 160) || !promptText(value.Task, 32768) || value.Task == "" ||
		!value.SessionPolicy.Valid() || !value.NotificationPolicy.Valid() || len(value.Input) > 100 || len(value.PromptInputs) > 100 {
		return nil, false
	}
	schedule := stringValue(value.ScheduleRef)
	if (value.ScheduleRef != nil) != (value.ExpectedScheduleVersion != nil) || value.ScheduleRef != nil && !fileTargetRef(schedule) || value.ExpectedScheduleVersion != nil && !validManagedVersion(*value.ExpectedScheduleVersion) ||
		value.ExpectedContextDigest != nil && !validManagedDigest(*value.ExpectedContextDigest) {
		return nil, false
	}
	mode := cp.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_DRAFT
	if value.Mode != nil {
		if !value.Mode.Valid() {
			return nil, false
		}
		mode = cp.SchedulePromptPreviewMode(cp.SchedulePromptPreviewMode_value["SCHEDULE_PROMPT_PREVIEW_MODE_"+string(*value.Mode)])
	}
	if mode == cp.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_CURRENT_REVISION && schedule == "" {
		return nil, false
	}
	input, err := structpb.NewStruct(value.Input)
	if err != nil {
		return nil, false
	}
	promptInput, err := structpb.NewStruct(value.PromptInputs)
	if err != nil {
		return nil, false
	}
	result := &cp.SchedulePromptPreviewContext{ProjectRef: value.ProjectRef, Target: targetProto(string(value.TargetType), value.TargetRef), Name: value.Name, Task: value.Task, Input: input, PromptInputs: promptInput,
		SessionPolicy: string(value.SessionPolicy), NotificationPolicy: string(value.NotificationPolicy), ScheduleRef: schedule, ExpectedContextDigest: stringValue(value.ExpectedContextDigest), IncludeFullMaterialization: value.IncludeFullMaterialization != nil && *value.IncludeFullMaterialization, Mode: mode}
	if value.ExpectedScheduleVersion != nil {
		result.ExpectedScheduleVersion = *value.ExpectedScheduleVersion
	}
	return result, true
}

func schedulePromptPreviewView(w http.ResponseWriter, request *cp.PreviewScheduleRequest, response *cp.PreviewScheduleResponse) (map[string]any, bool) {
	if response == nil || len(response.Occurrences) < 1 || len(response.Occurrences) > 100 {
		return nil, false
	}
	base, err := messageMap(&cp.PreviewScheduleResponse{NormalizedCronExpression: response.NormalizedCronExpression, Occurrences: response.Occurrences, DstGapPolicy: response.DstGapPolicy, DstFoldPolicy: response.DstFoldPolicy, MisfirePolicy: response.MisfirePolicy, OverlapPolicy: response.OverlapPolicy})
	if err != nil {
		return nil, false
	}
	context := request.Materialization
	if context == nil {
		return base, response.MaterializedPrompt == nil && response.MaterializationPin == nil && len(response.AutomationVariables) == 0
	}
	pin := response.MaterializationPin
	if pin == nil || pin.Mode != context.Mode || !fileTargetRef(pin.ExecutionActorRef) || pin.ScheduleRef != context.ScheduleRef || pin.ScheduleVersion != context.ExpectedScheduleVersion ||
		pin.ScheduledFor == nil || pin.ScheduledFor.CheckValid() != nil || response.Occurrences[0] == nil || !pin.ScheduledFor.AsTime().Equal(response.Occurrences[0].AsTime()) || pin.Timezone != request.Timezone ||
		(pin.ScheduleRef == "") != (pin.ScheduleVersion == 0) || (pin.SessionRef != "") != pin.Continuation || pin.SessionRef != "" && !fileTargetRef(pin.SessionRef) {
		return nil, false
	}
	if pin.ScheduleRef == "" {
		if pin.BaseRevisionRef != "" || pin.BaseRevisionDigest != "" {
			return nil, false
		}
	} else if !fileTargetRef(pin.BaseRevisionRef) || !validManagedDigest(pin.BaseRevisionDigest) {
		return nil, false
	}
	if pin.Mode == cp.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_DRAFT {
		if pin.RevisionAvailable || pin.RevisionRef != "" || pin.RevisionDigest != "" {
			return nil, false
		}
	} else if pin.Mode != cp.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_CURRENT_REVISION || !pin.RevisionAvailable || pin.ScheduleRef == "" || pin.RevisionRef != pin.BaseRevisionRef || pin.RevisionDigest != pin.BaseRevisionDigest {
		return nil, false
	}
	preview, ok := promptPreviewView(response.MaterializedPrompt, context.IncludeFullMaterialization)
	if !ok || response.MaterializedPrompt.ContextPin == nil || context.ExpectedContextDigest != "" && response.MaterializedPrompt.ContextPin.Digest != context.ExpectedContextDigest {
		return nil, false
	}
	contextPin := response.MaterializedPrompt.ContextPin
	if context.Target.GetAgentRef() != "" && (contextPin.AgentRef != context.Target.GetAgentRef() || contextPin.WorkflowRef != "") || context.Target.GetWorkflowRef() != "" && (contextPin.WorkflowRef != context.Target.GetWorkflowRef() || !fileTargetRef(contextPin.WorkflowRevisionRef) || contextPin.WorkflowStageKey != "workflow.coordinator.initial") {
		return nil, false
	}
	if pin.Continuation {
		if response.MaterializedPrompt.RuntimeDiff == nil || response.MaterializedPrompt.RuntimeDiff.SessionRef != pin.SessionRef || !fileTargetRef(contextPin.PreviousRuntimeRevisionRef) || response.MaterializedPrompt.RuntimeDiff.PreviousRevisionRef != contextPin.PreviousRuntimeRevisionRef {
			return nil, false
		}
	} else if response.MaterializedPrompt.RuntimeDiff != nil {
		return nil, false
	}
	if len(response.AutomationVariables) != 6 {
		return nil, false
	}
	variables := make([]generated.TemplateVariable, 0, 6)
	seen := map[string]bool{}
	for _, variable := range response.AutomationVariables {
		v, ok := templateVariableView(variable)
		if !ok || seen[v.Name] || !slices.Contains([]string{"automation.ref", "automation.name", "automation.task", "automation.scheduled_at", "automation.timezone", "automation.revision"}, v.Name) {
			return nil, false
		}
		if v.Name == "automation.revision" && ((!pin.RevisionAvailable && (v.Available || v.Reason != "REVISION_NOT_SAVED")) || pin.RevisionAvailable && !v.Available) {
			return nil, false
		}
		seen[v.Name] = true
		localizeTemplateVariable(w, &v)
		variables = append(variables, v)
	}
	publicPin := map[string]any{"scheduleVersion": pin.ScheduleVersion, "scheduledFor": pin.ScheduledFor.AsTime(), "timezone": pin.Timezone, "continuation": pin.Continuation, "revisionAvailable": pin.RevisionAvailable, "mode": context.Mode.String()[len("SCHEDULE_PROMPT_PREVIEW_MODE_"):], "executionActorRef": pin.ExecutionActorRef}
	for key, value := range map[string]string{"scheduleRef": pin.ScheduleRef, "revisionRef": pin.RevisionRef, "revisionDigest": pin.RevisionDigest, "sessionRef": pin.SessionRef, "baseRevisionRef": pin.BaseRevisionRef, "baseRevisionDigest": pin.BaseRevisionDigest} {
		if value != "" {
			publicPin[key] = value
		}
	}
	base["materializedPrompt"], base["automationVariables"], base["materializationPin"] = preview, variables, publicPin
	return base, true
}
