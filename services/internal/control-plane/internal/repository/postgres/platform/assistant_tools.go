package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
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
	var assistantRef string
	var allowedOperations []string
	var conversationVersion int64
	actorScope := scope{correlationRef: machineScope.correlationRef}
	if err := tx.QueryRow(ctx, queryRuntimeProposeassistantplanSelectContext,
		machineScope.organizationID, lease["runID"],
	).Scan(&conversationID, &conversationRef, &conversationVersion, &projectID, &projectRef, &allowedOperations, &assistantRef,
		&actorScope.actorID, &actorScope.actorRef, &actorScope.actorName, &actorScope.role,
		&actorScope.organizationRef); err != nil {
		return commandOutcome{}, errs.ErrForbidden
	}
	actorScope.organizationID = machineScope.organizationID
	seen := make(map[string]struct{}, len(payload.Operations))
	normalizedOperations := make([]entity.AssistantPlanOperation, 0, len(payload.Operations))
	for _, operation := range payload.Operations {
		if operation.Key == "" || len(operation.Key) > 96 {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, duplicate := seen[operation.Key]; duplicate {
			return commandOutcome{}, errs.ErrInvalid
		}
		seen[operation.Key] = struct{}{}
		if !contains(allowedOperations, operation.Type) {
			return commandOutcome{}, errs.ErrForbidden
		}
		operation, err = repository.hydrateAssistantOperation(ctx, tx, machineScope, projectRef, operation)
		if err != nil {
			return commandOutcome{}, err
		}
		operation, err = normalizeAssistantOperation(operation)
		if err != nil {
			return commandOutcome{}, err
		}
		operation, err = bindAssistantOperationProject(operation, projectRef)
		if err != nil {
			return commandOutcome{}, err
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return commandOutcome{}, err
		}
		if err := repository.authorizeCommand(ctx, tx, actorScope, planned); err != nil {
			return commandOutcome{}, err
		}
		normalizedOperations = append(normalizedOperations, operation)
	}
	planRef, err := newRef("pln")
	if err != nil {
		return commandOutcome{}, err
	}
	rawOperations := asJSON(normalizedOperations)
	digest := assistantPlanDigest(strings.TrimSpace(payload.Summary), rawOperations)
	var planID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations,
		planRef, machineScope.organizationID, conversationRef, strings.TrimSpace(payload.Summary), rawOperations, digest,
	).Scan(&planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	revisionRef, err := newRef("prv")
	if err != nil {
		return commandOutcome{}, err
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryConfigurationInsertAssistantPlanRevision,
		revisionRef, machineScope.organizationID, planID, int64(1), strings.TrimSpace(payload.Summary),
		rawOperations, digest, "SYSTEM_ASSISTANT", assistantRef,
	).Scan(&createdAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt,
		conversationID, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := entity.AssistantPlan{Ref: planRef, ConversationRef: conversationRef, ProjectRef: projectRef,
		Summary: strings.TrimSpace(payload.Summary), State: "DRAFT", Version: 1, Revision: 1,
		ContentDigest: digest, Operations: normalizedOperations, CreatedAt: createdAt}
	conversation := entity.AssistantConversation{Ref: conversationRef, ProjectRef: projectRef, State: "ACTIVE",
		Version: conversationVersion + 1, LatestPlan: &plan, UpdatedAt: time.Now().UTC()}
	proposal := commandOutcome{projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_PLAN",
		resourceRef: planRef, summary: "i18n:ASSISTANT_PLAN_PROPOSED"}
	if _, err := repository.auditAssistantOperation(ctx, tx, actorScope, proposal, "PROPOSE_CONFIGURATION_PLAN"); err != nil {
		return commandOutcome{}, err
	}
	if err := repository.emitPlatformEvent(ctx, tx, actorScope, "SYSTEM_ASSISTANT_CHANGED", projectRef, planRef,
		"i18n:ASSISTANT_PLAN_PROPOSED"); err != nil {
		return commandOutcome{}, err
	}
	proposal.result = command.Result{Conversation: &conversation, Plan: &plan}
	return proposal, nil
}

func assistantPlanDigest(summary string, rawOperations []byte) string {
	digest := sha256.Sum256(append(append([]byte(strings.TrimSpace(summary)), '\n'), rawOperations...))
	return fmt.Sprintf("%x", digest[:])
}

func assistantOperationType(value string) bool {
	switch value {
	case "CREATE_PROJECT", "UPDATE_PROJECT", "CREATE_AGENT", "CREATE_WORKFLOW", "CHANGE_CAPABILITY",
		"CHANGE_INTEGRATION_GRANT", "CREATE_SCHEDULE", "LAUNCH_RUN",
		"CREATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION", "ARCHIVE_AGENT", "ARCHIVE_WORKFLOW":
		return true
	default:
		return false
	}
}

func (repository *Repository) hydrateAssistantOperation(
	ctx context.Context,
	tx pgx.Tx,
	machineScope scope,
	projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if operation.Parameters == nil {
		operation.Parameters = operation.Input
	}
	if operation.Parameters == nil || len(operation.Parameters) > 100 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}

	if targetKind, targetName, ok := assistantCreateTarget(operation.Type, operation.Parameters); ok {
		operation.Action = "CREATE"
		operation.Target = entity.AssistantPlanTarget{Kind: targetKind, Name: targetName}
		operation.Before = map[string]any{}
		operation.After = cloneAssistantFields(operation.Parameters)
		operation.Selected = true
		return operation, nil
	}
	if operation.Type != "UPDATE_PROJECT" {
		return operation, nil
	}
	if !onlyAssistantFields(operation.Parameters, "projectRef", "name", "purpose", "language") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	requestedProjectRef := assistantString(operation.Parameters, "projectRef")
	if projectRef == "" || requestedProjectRef != "" && requestedProjectRef != "current" && requestedProjectRef != projectRef {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}

	var name, purpose, language string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectProject,
		machineScope.organizationID, projectRef,
	).Scan(&name, &purpose, &language, &version); errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	} else if err != nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	return hydrateAssistantProjectOperation(projectRef, name, purpose, language, version, operation)
}

func hydrateAssistantProjectOperation(
	projectRef, name, purpose, language string,
	version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	before := map[string]any{"projectRef": projectRef, "name": name, "purpose": purpose, "language": language}
	after := cloneAssistantFields(before)
	changed := false
	for _, field := range []string{"name", "purpose", "language"} {
		value, exists := operation.Parameters[field]
		if !exists {
			continue
		}
		text, ok := value.(string)
		text = strings.TrimSpace(text)
		if !ok || text == "" {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after[field] = text
		changed = changed || text != before[field]
	}
	if !changed {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "PROJECT", Ref: projectRef, Name: name, Version: &version}
	operation.Parameters = after
	operation.Before = before
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

func assistantCreateTarget(operationType string, parameters map[string]any) (string, string, bool) {
	var kind string
	switch operationType {
	case "CREATE_PROJECT":
		kind = "PROJECT"
	case "CREATE_AGENT":
		kind = "AGENT"
	case "CREATE_WORKFLOW":
		kind = "WORKFLOW"
	case "CREATE_INTEGRATION_CONNECTION":
		kind = "INTEGRATION_CONNECTION"
	case "CREATE_SCHEDULE":
		kind = "SCHEDULE"
	default:
		return "", "", false
	}
	return kind, assistantString(parameters, "name"), true
}

func cloneAssistantFields(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func normalizeAssistantOperation(operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !assistantOperationType(operation.Type) || strings.TrimSpace(operation.Key) == "" ||
		strings.TrimSpace(operation.Title) == "" || len(operation.Title) > 200 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if operation.Parameters == nil {
		operation.Parameters = operation.Input
	}
	if operation.Parameters == nil || operation.Before == nil || operation.After == nil ||
		len(operation.Parameters) > 100 || len(operation.Before) > 100 || len(operation.After) > 100 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	expectedAction := "CREATE"
	switch operation.Type {
	case "UPDATE_PROJECT", "CHANGE_CAPABILITY", "CHANGE_INTEGRATION_GRANT":
		expectedAction = "UPDATE"
	case "ARCHIVE_AGENT", "ARCHIVE_WORKFLOW":
		expectedAction = "ARCHIVE"
	case "LAUNCH_RUN", "TEST_INTEGRATION_CONNECTION":
		expectedAction = "EXECUTE"
	}
	if operation.Action == "" {
		operation.Action = expectedAction
	}
	if operation.Action != expectedAction {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if expectedAction == "CREATE" && (len(operation.Before) != 0 || !reflect.DeepEqual(operation.Parameters, operation.After)) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if expectedAction == "UPDATE" || expectedAction == "ARCHIVE" {
		if operation.ExpectedVersion == nil || *operation.ExpectedVersion < 1 || operation.Target.Ref == "" || len(operation.Before) == 0 || len(operation.After) == 0 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	if expectedAction == "ARCHIVE" && (len(operation.Parameters) != 0 || assistantString(operation.After, "state") != "ARCHIVED") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	expectedTargetKind, expectedTargetRef := "", ""
	switch operation.Type {
	case "UPDATE_PROJECT":
		expectedTargetKind = "PROJECT"
		expectedTargetRef = assistantString(operation.Parameters, "projectRef")
	case "CHANGE_CAPABILITY", "ARCHIVE_AGENT":
		expectedTargetKind = "AGENT"
		expectedTargetRef = assistantString(operation.Parameters, "agentRef")
	case "CHANGE_INTEGRATION_GRANT", "TEST_INTEGRATION_CONNECTION":
		expectedTargetKind = "INTEGRATION_CONNECTION"
		expectedTargetRef = assistantString(operation.Parameters, "connectionRef")
	case "ARCHIVE_WORKFLOW":
		expectedTargetKind = "WORKFLOW"
	}
	if expectedTargetKind != "" && operation.Target.Kind != expectedTargetKind {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if expectedTargetRef != "" && operation.Target.Ref != expectedTargetRef {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	operation.Input = make(map[string]any, len(operation.Parameters)+1)
	for key, value := range operation.Parameters {
		operation.Input[key] = value
	}
	if operation.ExpectedVersion != nil {
		operation.Input["expectedVersion"] = *operation.ExpectedVersion
	}
	operation.TargetKind, operation.TargetRef = operation.Target.Kind, operation.Target.Ref
	operation.Permitted = true
	operation.ValidationProblems = []string{}
	return operation, nil
}

func bindAssistantOperationProject(operation entity.AssistantPlanOperation, projectRef string) (entity.AssistantPlanOperation, error) {
	switch operation.Type {
	case "UPDATE_PROJECT", "CREATE_AGENT", "CREATE_WORKFLOW", "CREATE_SCHEDULE", "LAUNCH_RUN":
	default:
		return operation, nil
	}
	if projectRef == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	requested := assistantString(operation.Input, "projectRef")
	if requested != "" && requested != "current" && requested != projectRef {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	boundInput := make(map[string]any, len(operation.Input))
	for key, value := range operation.Input {
		boundInput[key] = value
	}
	boundInput["projectRef"] = projectRef
	operation.Input = boundInput
	return operation, nil
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
	case "UPDATE_PROJECT":
		if !onlyAssistantFields(operation.Input, "projectRef", "name", "purpose", "language", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "projectRef", "name", "purpose", "language", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		payload := command.ProjectInput{Ref: assistantString(operation.Input, "projectRef"), Name: assistantString(operation.Input, "name"),
			Purpose: assistantString(operation.Input, "purpose"), Language: assistantString(operation.Input, "language")}
		if !expectedOK || expected < 1 || payload.Ref == "" || payload.Name == "" || len(payload.Name) > 160 ||
			payload.Purpose == "" || len(payload.Purpose) > 2000 || !contains([]string{"ru", "en"}, payload.Language) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.UpdateProject, payload
		result.Mutation.ExpectedVersion = &expected
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
		if !onlyAssistantFields(operation.Input, "connectionRef", "capabilityKey", "agentRef", "workflowRef", "enabled", "expectedVersion") || !hasAssistantFields(operation.Input, "connectionRef", "capabilityKey", "enabled", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		payload := command.IntegrationGrantInput{ConnectionRef: assistantString(operation.Input, "connectionRef"), CapabilityKey: assistantString(operation.Input, "capabilityKey"),
			AgentRef: assistantString(operation.Input, "agentRef"), WorkflowRef: assistantString(operation.Input, "workflowRef"), Enabled: enabled}
		if !enabledOK || !expectedOK || expected < 1 || payload.ConnectionRef == "" || !validCapabilityKey(payload.CapabilityKey) || (payload.AgentRef == "") == (payload.WorkflowRef == "") {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ChangeIntegrationGrant, payload
		result.Mutation.ExpectedVersion = &expected
	case "CREATE_INTEGRATION_CONNECTION":
		if !onlyAssistantFields(operation.Input, "definitionKey", "name", "publicConfiguration") ||
			!hasAssistantFields(operation.Input, "definitionKey", "name", "publicConfiguration") {
			return command.Command{}, errs.ErrInvalid
		}
		configuration, configurationOK := assistantObjectValue(operation.Input, "publicConfiguration")
		payload := command.ConnectionInput{DefinitionKey: assistantString(operation.Input, "definitionKey"), Name: assistantString(operation.Input, "name"), PublicConfiguration: configuration}
		if !configurationOK || !validCapabilityKey(payload.DefinitionKey) || payload.Name == "" || len(payload.Name) > 160 || len(configuration) > 100 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateConnection, payload
	case "TEST_INTEGRATION_CONNECTION":
		if !onlyAssistantFields(operation.Input, "connectionRef", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "connectionRef", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		connectionRef := assistantString(operation.Input, "connectionRef")
		if !expectedOK || expected < 1 || connectionRef == "" {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.TestConnection, command.ConnectionInput{Ref: connectionRef}
		result.Mutation.ExpectedVersion = &expected
	case "ARCHIVE_AGENT":
		if !onlyAssistantFields(operation.Input, "expectedVersion") || operation.Target.Ref == "" || operation.ExpectedVersion == nil {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ArchiveAgent, command.AgentInput{Ref: operation.Target.Ref}
		result.Mutation.ExpectedVersion = operation.ExpectedVersion
	case "ARCHIVE_WORKFLOW":
		if !onlyAssistantFields(operation.Input, "expectedVersion") || operation.Target.Ref == "" || operation.ExpectedVersion == nil {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ArchiveWorkflow, command.WorkflowInput{Ref: operation.Target.Ref}
		result.Mutation.ExpectedVersion = operation.ExpectedVersion
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
	parallelGroupLabels := map[string]int64{}
	for index, raw := range rawSteps {
		stepInput, ok := raw.(map[string]any)
		if !ok || !onlyAssistantFields(stepInput, "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") ||
			!hasAssistantFields(stepInput, "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") {
			return command.WorkflowInput{}, errs.ErrInvalid
		}
		parallel, parallelOK := assistantBoolValue(stepInput, "parallel")
		humanGate, humanGateOK := assistantBoolValue(stepInput, "humanGate")
		parallelGroup, parallelGroupOK := assistantParallelGroup(stepInput, "parallelGroup", parallel, parallelGroupLabels)
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
		payload.Task == "" || len(payload.Task) > 32768 || !validBoundedRunInput(payload.Input) {
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

func assistantParallelGroup(input map[string]any, key string, parallel bool, labels map[string]int64) (int64, bool) {
	if value, ok := assistantInt64(input, key); ok {
		return value, true
	}
	label, ok := input[key].(string)
	label = strings.TrimSpace(label)
	if !ok || label == "" || len(label) > 80 {
		return 0, false
	}
	if !parallel {
		return 0, true
	}
	if value, exists := labels[label]; exists {
		return value, true
	}
	if len(labels) >= 50 {
		return 0, false
	}
	value := int64(len(labels) + 1)
	labels[label] = value
	return value, true
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
