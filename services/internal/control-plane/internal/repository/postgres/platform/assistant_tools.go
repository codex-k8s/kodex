package platform

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const maximumAssistantPlanOperations = 32

func (repository *Repository) proposeAssistantPlan(ctx context.Context, tx pgx.Tx, machineScope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProposeAssistantPlanInput)
	if !ok || strings.TrimSpace(payload.Summary) == "" || len(payload.Summary) > 2000 ||
		len(payload.Operations) == 0 || len(payload.Operations) > maximumAssistantPlanOperations {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, machineScope, command.LeaseInput{
		LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation,
	}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var conversationID, conversationRef, projectID, projectRef string
	var conversationVersion int64
	actorScope := scope{correlationRef: machineScope.correlationRef}
	if err := tx.QueryRow(ctx, queryRuntimeProposeassistantplanSelectContext,
		machineScope.organizationID, lease["runID"],
	).Scan(&conversationID, &conversationRef, &conversationVersion, &projectID, &projectRef,
		&actorScope.actorID, &actorScope.actorRef, &actorScope.actorName, &actorScope.role,
		&actorScope.organizationRef); err != nil {
		return commandOutcome{}, errs.ErrForbidden
	}
	actorScope.organizationID = machineScope.organizationID
	seen := make(map[string]struct{}, len(payload.Operations))
	for _, operation := range payload.Operations {
		if operation.Key == "" || len(operation.Key) > 96 {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, duplicate := seen[operation.Key]; duplicate {
			return commandOutcome{}, errs.ErrInvalid
		}
		seen[operation.Key] = struct{}{}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return commandOutcome{}, err
		}
		if err := repository.authorizeCommand(ctx, tx, actorScope, planned); err != nil {
			return commandOutcome{}, err
		}
	}
	planRef, err := newRef("pln")
	if err != nil {
		return commandOutcome{}, err
	}
	var planID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations,
		planRef, machineScope.organizationID, conversationRef, strings.TrimSpace(payload.Summary), asJSON(payload.Operations),
	).Scan(&planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt,
		conversationID, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := entity.AssistantPlan{Ref: planRef, Summary: strings.TrimSpace(payload.Summary), State: "PROPOSED", Version: 1,
		Operations: payload.Operations, CreatedAt: time.Now().UTC()}
	conversation := entity.AssistantConversation{Ref: conversationRef, ProjectRef: projectRef, State: "ACTIVE",
		Version: conversationVersion + 1, LatestPlan: &plan, UpdatedAt: time.Now().UTC()}
	proposal := commandOutcome{projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_PLAN",
		resourceRef: planRef, summary: "i18n:ASSISTANT_PLAN_PROPOSED"}
	if err := repository.auditAssistantOperation(ctx, tx, actorScope, proposal, "PROPOSE_CONFIGURATION_PLAN"); err != nil {
		return commandOutcome{}, err
	}
	if err := repository.emitPlatformEvent(ctx, tx, actorScope, "SYSTEM_ASSISTANT_CHANGED", projectRef, planRef,
		"i18n:ASSISTANT_PLAN_PROPOSED"); err != nil {
		return commandOutcome{}, err
	}
	proposal.result = command.Result{Conversation: &conversation, Plan: &plan}
	return proposal, nil
}

func assistantOperationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if strings.TrimSpace(operation.Summary) == "" || len(operation.Summary) > 500 || operation.Input == nil {
		return command.Command{}, errs.ErrInvalid
	}
	result := command.Command{}
	switch operation.Type {
	case "CREATE_PROJECT":
		if !onlyAssistantFields(operation.Input, "name", "purpose", "language") || !hasAssistantFields(operation.Input, "name", "purpose", "language") {
			return command.Command{}, errs.ErrInvalid
		}
		name, purpose, language := assistantString(operation.Input, "name"), assistantString(operation.Input, "purpose"), assistantString(operation.Input, "language")
		if name == "" || len(name) > 160 || purpose == "" || len(purpose) > 2000 || !contains([]string{"ru", "en"}, language) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateProject, command.ProjectInput{Name: name, Purpose: purpose, Language: language}
	case "CREATE_AGENT":
		if !onlyAssistantFields(operation.Input, "projectRef", "roleDefinitionRef", "name", "purpose", "roleDescription", "avatarUrl", "runtimeRef", "instructions") ||
			!hasAssistantFields(operation.Input, "projectRef", "name", "purpose", "roleDescription", "instructions") {
			return command.Command{}, errs.ErrInvalid
		}
		payload := command.AgentInput{ProjectRef: assistantString(operation.Input, "projectRef"), RoleDefinitionRef: assistantString(operation.Input, "roleDefinitionRef"),
			Name: assistantString(operation.Input, "name"), Purpose: assistantString(operation.Input, "purpose"),
			RoleDescription: assistantString(operation.Input, "roleDescription"), AvatarURL: assistantString(operation.Input, "avatarUrl"),
			RuntimeRef: assistantString(operation.Input, "runtimeRef"), Instructions: assistantString(operation.Input, "instructions")}
		if payload.ProjectRef == "" || payload.Name == "" || len(payload.Name) > 160 || payload.Purpose == "" || len(payload.Purpose) > 2000 ||
			payload.RoleDescription == "" || len(payload.RoleDescription) > 2000 || len(payload.AvatarURL) > 500 || len(payload.Instructions) < 20 || len(payload.Instructions) > 65536 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateAgent, payload
	case "CREATE_WORKFLOW":
		workflow, err := assistantWorkflow(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.CreateWorkflow, workflow
	case "CHANGE_CAPABILITY":
		if !onlyAssistantFields(operation.Input, "agentRef", "capabilityKey", "enabled", "expectedVersion") || !hasAssistantFields(operation.Input, "agentRef", "capabilityKey", "enabled", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, ok := assistantInt64(operation.Input, "expectedVersion")
		enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
		if !ok || expected < 1 || !enabledOK || assistantString(operation.Input, "agentRef") == "" || !validCapabilityKey(assistantString(operation.Input, "capabilityKey")) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind = command.ChangeAgentCapability
		result.Mutation.ExpectedVersion = &expected
		result.Payload = command.AgentBindingInput{AgentRef: assistantString(operation.Input, "agentRef"), BindingRef: assistantString(operation.Input, "capabilityKey"), Enabled: enabled}
	case "CHANGE_INTEGRATION_GRANT":
		if !onlyAssistantFields(operation.Input, "connectionRef", "capabilityKey", "agentRef", "workflowRef", "enabled") || !hasAssistantFields(operation.Input, "connectionRef", "capabilityKey", "enabled") {
			return command.Command{}, errs.ErrInvalid
		}
		enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
		payload := command.IntegrationGrantInput{ConnectionRef: assistantString(operation.Input, "connectionRef"), CapabilityKey: assistantString(operation.Input, "capabilityKey"),
			AgentRef: assistantString(operation.Input, "agentRef"), WorkflowRef: assistantString(operation.Input, "workflowRef"), Enabled: enabled}
		if !enabledOK || payload.ConnectionRef == "" || !validCapabilityKey(payload.CapabilityKey) || (payload.AgentRef == "") == (payload.WorkflowRef == "") {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ChangeIntegrationGrant, payload
	case "CREATE_SCHEDULE":
		schedule, err := assistantSchedule(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.CreateSchedule, schedule
	case "LAUNCH_RUN":
		run, err := assistantRun(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.LaunchRun, run
	default:
		return command.Command{}, errs.ErrInvalid
	}
	return result, nil
}

func assistantWorkflow(input map[string]any) (command.WorkflowInput, error) {
	if !onlyAssistantFields(input, "projectRef", "name", "purpose", "coordinatorAgentRef", "inputFields", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria") ||
		!hasAssistantFields(input, "projectRef", "name", "purpose", "coordinatorAgentRef", "steps") {
		return command.WorkflowInput{}, errs.ErrInvalid
	}
	projectRef, name := assistantString(input, "projectRef"), assistantString(input, "name")
	coordinator := assistantString(input, "coordinatorAgentRef")
	concurrency, concurrencyOK := assistantInt64(input, "maxConcurrency")
	timeout, timeoutOK := assistantInt64(input, "timeoutSeconds")
	if !concurrencyOK {
		concurrency = 1
	}
	if !timeoutOK {
		timeout = 7200
	}
	rawSteps, ok := input["steps"].([]any)
	if !ok || len(rawSteps) == 0 || len(rawSteps) > 200 {
		return command.WorkflowInput{}, errs.ErrInvalid
	}
	draft := entity.WorkflowVersion{Ref: "draft", Name: name, Purpose: assistantString(input, "purpose"), CoordinatorAgentRef: coordinator,
		VersionNumber: 1, Concurrency: int32(concurrency), TimeoutSeconds: timeout, CompletionCriteria: assistantString(input, "completionCriteria"), ResultSchema: map[string]any{}}
	if rawFields, exists := input["inputFields"]; exists {
		fields, ok := rawFields.([]any)
		if !ok || len(fields) > 100 {
			return command.WorkflowInput{}, errs.ErrInvalid
		}
		for index, raw := range fields {
			field, ok := raw.(map[string]any)
			if !ok || !onlyAssistantFields(field, "label", "description", "valueType", "required", "options") ||
				!hasAssistantFields(field, "label", "valueType", "required", "options") {
				return command.WorkflowInput{}, errs.ErrInvalid
			}
			required, requiredOK := assistantBoolValue(field, "required")
			options, optionsOK := assistantStringsValue(field, "options")
			if !requiredOK || !optionsOK {
				return command.WorkflowInput{}, errs.ErrInvalid
			}
			draft.Inputs = append(draft.Inputs, entity.WorkflowInputField{
				Key: "field-" + leftPad(index+1, 3), Label: assistantString(field, "label"),
				Help: assistantString(field, "description"), Type: assistantString(field, "valueType"),
				Required: required, Options: options,
			})
		}
	}
	frontier := []string{}
	parallelGroups := map[int32][]string{}
	for index, raw := range rawSteps {
		stepInput, ok := raw.(map[string]any)
		if !ok || !onlyAssistantFields(stepInput, "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") ||
			!hasAssistantFields(stepInput, "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") {
			return command.WorkflowInput{}, errs.ErrInvalid
		}
		parallel, parallelOK := assistantBoolValue(stepInput, "parallel")
		humanGate, humanGateOK := assistantBoolValue(stepInput, "humanGate")
		parallelGroup, parallelGroupOK := assistantInt64(stepInput, "parallelGroup")
		stepTimeout, timeoutOK := assistantInt64(stepInput, "timeoutSeconds")
		gateDecisions, gateDecisionsOK := assistantStringsValue(stepInput, "gateDecisions")
		requiredCapabilities, requiredCapabilitiesOK := assistantStringsValue(stepInput, "requiredCapabilityKeys")
		if !parallelOK || !humanGateOK || !parallelGroupOK || !timeoutOK || !gateDecisionsOK || !requiredCapabilitiesOK {
			return command.WorkflowInput{}, errs.ErrInvalid
		}
		key := "step-" + leftPad(index+1, 3)
		dependencies := append([]string(nil), frontier...)
		if parallel {
			group := int32(parallelGroup)
			if known, exists := parallelGroups[group]; exists {
				dependencies = append([]string(nil), known...)
			} else {
				parallelGroups[group] = append([]string(nil), frontier...)
				frontier = nil
			}
			frontier = append(frontier, key)
		} else {
			parallelGroups = map[int32][]string{}
			frontier = []string{key}
		}
		draft.Steps = append(draft.Steps, entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: assistantString(stepInput, "name"),
			AgentRef: assistantString(stepInput, "agentRef"), Instructions: assistantString(stepInput, "purpose"), Parallel: parallel,
			ParallelGroup: int32(parallelGroup), TimeoutSeconds: int32(stepTimeout), ExpectedResult: assistantString(stepInput, "expectedResult"),
			HumanGateAfter: humanGate, DependsOn: dependencies,
			GateDecisions: gateDecisions, RequiredCapabilityKeys: requiredCapabilities})
	}
	if projectRef == "" || !validWorkflowVersion(draft) {
		return command.WorkflowInput{}, errs.ErrInvalid
	}
	return command.WorkflowInput{ProjectRef: projectRef, Name: name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator, Draft: &draft}, nil
}

func assistantSchedule(input map[string]any) (command.ScheduleInput, error) {
	if !onlyAssistantFields(input, "projectRef", "name", "targetType", "targetRef", "preset", "timeOfDay", "dayOfWeek", "timezone", "input", "sessionPolicy", "notificationPolicy") ||
		!hasAssistantFields(input, "projectRef", "name", "targetType", "targetRef", "preset", "timeOfDay", "timezone", "input", "sessionPolicy", "notificationPolicy") {
		return command.ScheduleInput{}, errs.ErrInvalid
	}
	boundedInput, boundedInputOK := assistantObjectValue(input, "input")
	payload := command.ScheduleInput{ProjectRef: assistantString(input, "projectRef"), Name: assistantString(input, "name"),
		Preset: assistantString(input, "preset"), TimeOfDay: assistantString(input, "timeOfDay"), DayOfWeek: assistantString(input, "dayOfWeek"), Timezone: assistantString(input, "timezone"),
		SessionPolicy: assistantString(input, "sessionPolicy"), NotificationPolicy: assistantString(input, "notificationPolicy"),
		Target: entity.RunTarget{Type: assistantString(input, "targetType"), Ref: assistantString(input, "targetRef")}, Input: boundedInput}
	if !boundedInputOK || payload.ProjectRef == "" || payload.Name == "" || len(payload.Name) > 160 || !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) || payload.Target.Ref == "" ||
		payload.Preset == "" || len(payload.Preset) > 120 || len(payload.TimeOfDay) > 5 || len(payload.DayOfWeek) > 9 || payload.Timezone == "" || len(payload.Timezone) > 80 ||
		!contains([]string{"NEW_EACH_RUN", "CONTINUE_ONE"}, payload.SessionPolicy) || !contains([]string{"CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"}, payload.NotificationPolicy) || !validBoundedRunInput(payload.Input) {
		return command.ScheduleInput{}, errs.ErrInvalid
	}
	return payload, nil
}

func assistantRun(input map[string]any) (command.LaunchRunInput, error) {
	if !onlyAssistantFields(input, "projectRef", "targetType", "targetRef", "title", "task", "input", "artifactRefs", "sessionRef") ||
		!hasAssistantFields(input, "projectRef", "targetType", "targetRef", "title", "task", "input") {
		return command.LaunchRunInput{}, errs.ErrInvalid
	}
	boundedInput, boundedInputOK := assistantObjectValue(input, "input")
	payload := command.LaunchRunInput{ProjectRef: assistantString(input, "projectRef"), Title: assistantString(input, "title"), Task: assistantString(input, "task"),
		SessionRef: assistantString(input, "sessionRef"), Source: "SYSTEM_ASSISTANT", Input: boundedInput, ArtifactRefs: assistantStrings(input, "artifactRefs"),
		Target: entity.RunTarget{Type: assistantString(input, "targetType"), Ref: assistantString(input, "targetRef")}}
	if !boundedInputOK || payload.ProjectRef == "" || !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) || payload.Target.Ref == "" || payload.Title == "" || len(payload.Title) > 240 ||
		payload.Task == "" || len(payload.Task) > 32768 || !validBoundedRunInput(payload.Input) || len(payload.ArtifactRefs) > 50 {
		return command.LaunchRunInput{}, errs.ErrInvalid
	}
	return payload, nil
}

func onlyAssistantFields(input map[string]any, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range input {
		if _, ok := known[key]; !ok {
			return false
		}
	}
	return true
}

func hasAssistantFields(input map[string]any, required ...string) bool {
	for _, key := range required {
		if _, ok := input[key]; !ok {
			return false
		}
	}
	return true
}

func assistantString(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func assistantBoolValue(input map[string]any, key string) (bool, bool) {
	value, ok := input[key].(bool)
	return value, ok
}

func assistantInt64(input map[string]any, key string) (int64, bool) {
	switch value := input[key].(type) {
	case float64:
		integer := int64(value)
		return integer, float64(integer) == value
	case int64:
		return value, true
	case int:
		return int64(value), true
	case json.Number:
		integer, err := value.Int64()
		return integer, err == nil
	default:
		return 0, false
	}
}

func assistantObjectValue(input map[string]any, key string) (map[string]any, bool) {
	value, ok := input[key].(map[string]any)
	return value, ok
}

func assistantStrings(input map[string]any, key string) []string {
	result, _ := assistantStringsValue(input, key)
	return result
}

func assistantStringsValue(input map[string]any, key string) ([]string, bool) {
	raw, ok := input[key].([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, false
		}
		result = append(result, strings.TrimSpace(value))
	}
	return result, true
}

func leftPad(value, width int) string {
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	for len(result) < width {
		result = "0" + result
	}
	return result
}
