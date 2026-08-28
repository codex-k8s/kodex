package grpc

import (
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maximumAssistantPlanOperationTitleRunes = 160

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}
func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamp(*value)
}
func structure(value map[string]any) *structpb.Struct {
	result, _ := structpb.NewStruct(value)
	if result == nil {
		result = &structpb.Struct{}
	}
	return result
}

func nextActions(values []string) []controlplanev1.NextAction {
	result := make([]controlplanev1.NextAction, 0, len(values))
	for _, value := range values {
		if action, ok := controlplanev1.NextAction_value["NEXT_ACTION_"+value]; ok {
			result = append(result, controlplanev1.NextAction(action))
		}
	}
	return result
}
func platformRole(value string) controlplanev1.PlatformRole {
	raw := controlplanev1.PlatformRole_value["PLATFORM_ROLE_"+value]
	return controlplanev1.PlatformRole(raw)
}
func projectPermissions(values []string) []controlplanev1.ProjectPermission {
	result := make([]controlplanev1.ProjectPermission, 0, len(values))
	for _, value := range values {
		if raw, ok := controlplanev1.ProjectPermission_value["PROJECT_PERMISSION_"+value]; ok {
			result = append(result, controlplanev1.ProjectPermission(raw))
		}
	}
	return result
}
func lifecycle(value string) controlplanev1.EntityLifecycle {
	raw := controlplanev1.EntityLifecycle_value["ENTITY_LIFECYCLE_"+value]
	return controlplanev1.EntityLifecycle(raw)
}
func agentState(value string) controlplanev1.AgentState {
	raw := controlplanev1.AgentState_value["AGENT_STATE_"+value]
	return controlplanev1.AgentState(raw)
}
func instructionState(value string) controlplanev1.InstructionState {
	raw := controlplanev1.InstructionState_value["INSTRUCTION_STATE_"+value]
	return controlplanev1.InstructionState(raw)
}
func workflowState(value string) controlplanev1.WorkflowState {
	raw := controlplanev1.WorkflowState_value["WORKFLOW_STATE_"+value]
	return controlplanev1.WorkflowState(raw)
}
func runState(value string) controlplanev1.RunState {
	raw := controlplanev1.RunState_value["RUN_STATE_"+value]
	return controlplanev1.RunState(raw)
}
func runSource(value string) controlplanev1.RunSource {
	raw := controlplanev1.RunSource_value["RUN_SOURCE_"+value]
	return controlplanev1.RunSource(raw)
}
func nodeType(value string) controlplanev1.RunNodeType {
	raw := controlplanev1.RunNodeType_value["RUN_NODE_TYPE_"+value]
	return controlplanev1.RunNodeType(raw)
}
func nodeState(value string) controlplanev1.RunNodeState {
	raw := controlplanev1.RunNodeState_value["RUN_NODE_STATE_"+value]
	return controlplanev1.RunNodeState(raw)
}
func edgeType(value string) controlplanev1.RunEdgeType {
	raw := controlplanev1.RunEdgeType_value["RUN_EDGE_TYPE_"+value]
	return controlplanev1.RunEdgeType(raw)
}
func eventType(value string) controlplanev1.RunEventType {
	raw := controlplanev1.RunEventType_value["RUN_EVENT_TYPE_"+value]
	return controlplanev1.RunEventType(raw)
}
func gateState(value string) controlplanev1.OwnerGateState {
	raw := controlplanev1.OwnerGateState_value["OWNER_GATE_STATE_"+value]
	return controlplanev1.OwnerGateState(raw)
}
func gateDecision(value string) controlplanev1.OwnerGateDecision {
	raw := controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+value]
	return controlplanev1.OwnerGateDecision(raw)
}
func gateDecisions(values []string) []controlplanev1.OwnerGateDecision {
	result := make([]controlplanev1.OwnerGateDecision, 0, len(values))
	for _, value := range values {
		result = append(result, gateDecision(value))
	}
	return result
}
func scanState(value string) controlplanev1.ArtifactScanState {
	raw := controlplanev1.ArtifactScanState_value["ARTIFACT_SCAN_STATE_"+value]
	return controlplanev1.ArtifactScanState(raw)
}

func artifactSource(value string) controlplanev1.ArtifactSource {
	raw := controlplanev1.ArtifactSource_value["ARTIFACT_SOURCE_"+value]
	return controlplanev1.ArtifactSource(raw)
}
func scheduleState(schedule entity.Schedule) controlplanev1.ScheduleState {
	switch schedule.State {
	case "ARCHIVED":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_ARCHIVED
	case "NEEDS_ATTENTION":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_NEEDS_ATTENTION
	case "", "ACTIVE":
	default:
		return controlplanev1.ScheduleState_SCHEDULE_STATE_UNSPECIFIED
	}
	if schedule.Enabled {
		return controlplanev1.ScheduleState_SCHEDULE_STATE_ACTIVE
	}
	return controlplanev1.ScheduleState_SCHEDULE_STATE_PAUSED
}
func connectionState(value string) controlplanev1.ConnectionState {
	if value == "TESTING" {
		value = "CONNECTING"
	}
	if value == "DEGRADED" {
		value = "UNAVAILABLE"
	}
	raw := controlplanev1.ConnectionState_value["CONNECTION_STATE_"+value]
	return controlplanev1.ConnectionState(raw)
}
func assistantState(value string) controlplanev1.AssistantRuntimeState {
	mapping := map[string]string{"STARTING": "PROVISIONING", "READY": "READY", "BUSY": "BUSY", "RECOVERING": "RECOVERING", "UNAVAILABLE": "FAILED"}
	raw := controlplanev1.AssistantRuntimeState_value["ASSISTANT_RUNTIME_STATE_"+mapping[value]]
	return controlplanev1.AssistantRuntimeState(raw)
}

func castUser(value entity.User) *controlplanev1.UserSummary {
	return &controlplanev1.UserSummary{Ref: value.Ref, DisplayName: value.DisplayName, EmailHint: value.EmailMasked}
}
func castMembership(value entity.Membership) *controlplanev1.Membership {
	return &controlplanev1.Membership{Ref: value.Ref, Version: value.Version, User: castUser(value.User), PlatformRole: platformRole(value.Role), ProjectPermissions: projectPermissions(value.Permissions), Active: value.Active, NextActions: nextActions(value.NextActions)}
}
func castProject(value entity.Project) *controlplanev1.Project {
	return &controlplanev1.Project{Ref: value.Ref, Version: value.Version, Name: value.Name, Purpose: value.Purpose, Language: value.Language, Lifecycle: lifecycle(value.Lifecycle), AgentCount: value.AgentCount, WorkflowCount: value.WorkflowCount, ActiveRunCount: value.ActiveRunCount, PendingGateCount: value.PendingGateCount, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions)}
}
func castSearchResult(value entity.SearchResult) *controlplanev1.SearchResult {
	kind := controlplanev1.SearchResultKind(controlplanev1.SearchResultKind_value["SEARCH_RESULT_KIND_"+value.Kind])
	return &controlplanev1.SearchResult{Kind: kind, Ref: value.Ref, ProjectRef: value.ProjectRef, Title: value.Title, Subtitle: value.Subtitle, State: value.State, UpdatedAt: timestamp(value.UpdatedAt)}
}
func castCapability(value entity.IntegrationCapability) *controlplanev1.PlatformCapability {
	return &controlplanev1.PlatformCapability{Key: value.Key, Name: value.Name, Description: value.Description, Category: value.Risk, AvailableWithoutIntegration: strings.HasPrefix(value.Key, "platform.")}
}
func castRuntime(value entity.RuntimeSelection) *controlplanev1.RuntimeSelection {
	return &controlplanev1.RuntimeSelection{Ref: value.Ref, Name: value.Name, Revision: value.RuntimeRevision, Ready: value.Ready, Provider: value.Provider, Model: value.Model}
}
func castIntegrationCapability(value entity.IntegrationCapability) *controlplanev1.IntegrationCapability {
	return &controlplanev1.IntegrationCapability{Key: value.Key, Name: value.Name, Description: value.Description, Risk: value.Risk, ApprovalRequired: value.Risk == "SENSITIVE" || value.Risk == "DESTRUCTIVE"}
}
func castInstruction(value *entity.InstructionVersion) *controlplanev1.InstructionVersion {
	if value == nil {
		return nil
	}
	return &controlplanev1.InstructionVersion{Ref: value.Ref, Version: int64(value.VersionNumber), Revision: value.VersionNumber, State: instructionState(value.State), Content: value.Content, ValidationMessages: value.ValidationProblems, CreatedAt: timestamp(value.CreatedAt), PublishedAt: optionalTimestamp(value.PublishedAt)}
}
func castAgent(value entity.Agent) *controlplanev1.Agent {
	capabilities := make([]*controlplanev1.PlatformCapability, 0, len(value.Capabilities))
	for _, key := range value.Capabilities {
		capabilities = append(capabilities, &controlplanev1.PlatformCapability{Key: key, Name: key, AvailableWithoutIntegration: strings.HasPrefix(key, "platform.")})
	}
	return &controlplanev1.Agent{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RoleDefinitionRef: value.RoleDefinitionRef, RoleDefinitionName: value.RoleDefinitionName, Name: value.Name, Purpose: value.Purpose, RoleDescription: value.RoleDescription, AvatarUrl: value.AvatarURL, State: agentState(value.State), Enabled: value.Enabled, System: value.System, Runtime: &controlplanev1.RuntimeSelection{Ref: value.RuntimeKey, Name: value.RuntimeName, Revision: value.RuntimeRevision, Ready: value.State == "READY", Provider: value.Provider, Model: value.Model}, PublishedInstructions: castInstruction(value.PublishedInstructions), DraftInstructions: castInstruction(value.DraftInstructions), Capabilities: capabilities, IntegrationGrantRefs: value.IntegrationGrantRefs, KnowledgeArtifactRefs: value.KnowledgeArtifactRefs, UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions)}
}
func castWorkflowVersion(value *entity.WorkflowVersion, state, coordinatorAgentRef string) *controlplanev1.WorkflowVersion {
	if value == nil {
		return nil
	}
	inputs := make([]*controlplanev1.WorkflowInputField, 0, len(value.Inputs))
	for _, input := range value.Inputs {
		inputs = append(inputs, &controlplanev1.WorkflowInputField{Key: input.Key, Label: input.Label, Description: input.Help, ValueType: input.Type, Required: input.Required, Options: append([]string(nil), input.Options...)})
	}
	steps := make([]*controlplanev1.WorkflowStep, 0, len(value.Steps))
	for index, step := range value.Steps {
		position := step.Position
		if position == 0 {
			position = int32(index + 1)
		}
		timeoutSeconds := step.TimeoutSeconds
		if timeoutSeconds == 0 {
			timeoutSeconds = int32(value.TimeoutSeconds)
		}
		steps = append(steps, &controlplanev1.WorkflowStep{
			Ref: step.Key, Position: position, Name: step.Name, Purpose: step.Instructions, AgentRef: step.AgentRef,
			Parallel: step.Parallel, ParallelGroup: step.ParallelGroup, TimeoutSeconds: timeoutSeconds,
			ExpectedResult: step.ExpectedResult, HumanGate: step.HumanGateAfter,
			GateDecisions: gateDecisions(step.GateDecisions), RequiredCapabilityKeys: append([]string(nil), step.RequiredCapabilityKeys...),
		})
	}
	return &controlplanev1.WorkflowVersion{Ref: value.Ref, Version: int64(value.VersionNumber), Revision: value.VersionNumber, State: workflowState(state), CoordinatorAgentRef: coordinatorAgentRef, InputFields: inputs, Steps: steps, MaxConcurrency: value.Concurrency, TimeoutSeconds: int32(value.TimeoutSeconds), CompletionCriteria: value.CompletionCriteria}
}
func castWorkflow(value entity.Workflow) *controlplanev1.Workflow {
	return &controlplanev1.Workflow{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name, Purpose: value.Purpose, State: workflowState(value.State), PublishedVersion: castWorkflowVersion(value.Published, "PUBLISHED", value.CoordinatorAgentRef), DraftVersion: castWorkflowVersion(value.Draft, value.State, value.CoordinatorAgentRef), UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions)}
}
func castRunTarget(value entity.RunTarget) *controlplanev1.RunTarget {
	target := &controlplanev1.RunTarget{DisplayName: value.Name}
	if value.Type == "WORKFLOW" {
		target.Target = &controlplanev1.RunTarget_WorkflowRef{WorkflowRef: value.Ref}
	} else {
		target.Target = &controlplanev1.RunTarget_AgentRef{AgentRef: value.Ref}
	}
	return target
}
func castTokenUsage(value entity.TokenUsage) *controlplanev1.TokenUsage {
	return &controlplanev1.TokenUsage{TotalTokens: value.TotalTokens, InputTokens: value.InputTokens, CachedInputTokens: value.CachedInputTokens, CacheWriteInputTokens: value.CacheWriteInputTokens, OutputTokens: value.OutputTokens, ReasoningOutputTokens: value.ReasoningOutputTokens, ModelContextWindow: value.ModelContextWindow}
}
func usageFromProto(value *controlplanev1.TokenUsage) entity.TokenUsage {
	if value == nil {
		return entity.TokenUsage{}
	}
	return entity.TokenUsage{TotalTokens: value.GetTotalTokens(), InputTokens: value.GetInputTokens(), CachedInputTokens: value.GetCachedInputTokens(), CacheWriteInputTokens: value.GetCacheWriteInputTokens(), OutputTokens: value.GetOutputTokens(), ReasoningOutputTokens: value.GetReasoningOutputTokens(), ModelContextWindow: value.GetModelContextWindow()}
}
func castRun(value entity.Run) *controlplanev1.Run {
	result := &controlplanev1.Run{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, SessionRef: value.SessionRef, RootRunRef: value.RootRunRef, ParentRunRef: value.ParentRunRef, RetryOfRunRef: value.RetryOfRunRef, Target: castRunTarget(value.Target), Title: value.Title, InputSummary: value.Task, State: runState(value.State), Source: runSource(value.Source), Initiator: &controlplanev1.UserSummary{DisplayName: value.InitiatorName}, Attempt: value.Attempt, GraphRevision: value.GraphRevision, LastEventSequence: value.EventSequence, ResultSummary: value.ResultSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, Usage: castTokenUsage(value.Usage), InputArtifactRefs: value.InputArtifactRefs, ArtifactRefs: value.ArtifactRefs, GateRefs: value.GateRefs, CreatedAt: timestamp(value.CreatedAt), StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions)}
	for _, incident := range value.Incidents {
		result.Incidents = append(result.Incidents, castIncident(incident))
	}
	return result
}
func castNode(value entity.RunNode) *controlplanev1.RunNode {
	return &controlplanev1.RunNode{Ref: value.Ref, RunRef: value.RunRef, ParentNodeRef: value.ParentNodeRef, Type: nodeType(value.Type), State: nodeState(value.State), DisplayName: value.DisplayName, Role: value.Role, AgentRef: value.AgentRef, TurnRef: value.TurnRef, Attempt: value.Attempt, InputSummary: value.InputSummary, ProgressSummary: value.ProgressSummary, IntegrationNames: value.IntegrationNames, ArtifactRefs: value.ArtifactRefs, ChildRunRefs: value.ChildRunRefs, CallbackSummary: value.CallbackSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, CreatedAt: timestamp(value.CreatedAt), StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions)}
}
func castEdge(value entity.RunEdge) *controlplanev1.RunEdge {
	return &controlplanev1.RunEdge{Ref: value.Ref, RunRef: value.RunRef, SourceNodeRef: value.SourceNodeRef, TargetNodeRef: value.TargetNodeRef, Type: edgeType(value.Type), Label: value.Label}
}
func castRunDelta(value *entity.RunDelta) *controlplanev1.RunDelta {
	if value == nil {
		return nil
	}
	return &controlplanev1.RunDelta{Ref: value.Ref, Version: value.Version, State: runState(value.State), GraphRevision: value.GraphRevision, LastEventSequence: value.EventSequence, ResultSummary: value.ResultSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, Usage: castTokenUsage(value.Usage), ArtifactRefs: value.ArtifactRefs, GateRefs: value.GateRefs, StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions)}
}
func castEvent(value entity.RunEvent) *controlplanev1.RunEvent {
	event := &controlplanev1.RunEvent{Ref: value.Ref, RunRef: value.RunRef, Sequence: value.Sequence, Type: eventType(value.Type), NodeRef: value.NodeRef, EdgeRef: value.EdgeRef, GateRef: value.GateRef, ArtifactRef: value.ArtifactRef, Summary: value.Summary, Progress: value.Progress, RunState: runState(value.RunState), NodeState: nodeState(value.NodeState), OccurredAt: timestamp(value.OccurredAt), GraphRevision: value.GraphRevision, Run: castRunDelta(value.Delta.Run)}
	if value.Delta.Node != nil {
		event.Node = castNode(*value.Delta.Node)
	}
	if value.Delta.Edge != nil {
		event.Edge = castEdge(*value.Delta.Edge)
	}
	if value.Delta.Gate != nil {
		event.Gate = castGate(*value.Delta.Gate)
	}
	if value.Delta.Artifact != nil {
		event.Artifact = castArtifact(*value.Delta.Artifact)
	}
	if value.Delta.Incident != nil {
		event.Incident = castIncident(*value.Delta.Incident)
	}
	return event
}
func castGraph(value entity.RunGraph) *controlplanev1.RunGraph {
	result := &controlplanev1.RunGraph{RunRef: value.RunRef, Revision: value.Revision, Sequence: value.Sequence}
	for _, node := range value.Nodes {
		result.Nodes = append(result.Nodes, castNode(node))
	}
	for _, edge := range value.Edges {
		result.Edges = append(result.Edges, castEdge(edge))
	}
	return result
}
func castGate(value entity.OwnerGate) *controlplanev1.OwnerGate {
	gate := &controlplanev1.OwnerGate{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RunRef: value.RunRef, NodeRef: value.NodeRef, Title: value.Title, ContextSummary: value.ContextSummary, ConsequencesSummary: value.Prompt, RequestedBy: &controlplanev1.UserSummary{Ref: value.RequestedByRef, DisplayName: value.RequestedByName}, State: gateState(value.State), AllowedDecisions: gateDecisions(value.AllowedDecisions), Decision: gateDecision(value.Decision), DecisionComment: value.DecisionComment, OpenedAt: timestamp(value.CreatedAt), DecidedAt: optionalTimestamp(value.ResolvedAt), ArtifactRefs: value.ArtifactRefs, NextActions: nextActions(value.NextActions)}
	if value.ResolvedByName != "" {
		gate.DecidedBy = &controlplanev1.UserSummary{DisplayName: value.ResolvedByName}
	}
	return gate
}
func castArtifact(value entity.Artifact) *controlplanev1.Artifact {
	return &controlplanev1.Artifact{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RunRef: value.RunRef, SessionRef: value.SessionRef, FileName: value.FileName, MediaType: value.MediaType, SizeBytes: value.SizeBytes, ScanState: scanState(value.ScanState), Source: artifactSource(value.Source), Revision: int32(value.Revision), AgentBindings: value.Bindings, PreviewAvailable: value.PreviewState == "AVAILABLE", CreatedAt: timestamp(value.CreatedAt), NextActions: nextActions(value.NextActions), Digest: value.Digest}
}
func castSchedule(value entity.Schedule) *controlplanev1.Schedule {
	return &controlplanev1.Schedule{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name, Target: castRunTarget(value.Target), State: scheduleState(value), Preset: value.Preset, CronExpression: value.CronExpression, Timezone: value.Timezone, Input: structure(value.Input), SessionPolicy: value.SessionPolicy, NotificationPolicy: value.NotificationPolicy, NextRunAt: optionalTimestamp(value.NextRunAt), NextActions: nextActions(value.NextActions), TimeOfDay: value.TimeOfDay, DayOfWeek: value.DayOfWeek}
}
func castDefinition(value entity.IntegrationDefinition) *controlplanev1.IntegrationDefinition {
	result := &controlplanev1.IntegrationDefinition{Key: value.Key, Name: value.Name, Description: value.Description, Category: value.Category, BuiltIn: true, Available: value.Enabled}
	for _, capability := range value.Capabilities {
		result.Capabilities = append(result.Capabilities, castIntegrationCapability(capability))
	}
	for _, field := range value.ConfigurationFields {
		result.ConfigurationFields = append(result.ConfigurationFields, &controlplanev1.IntegrationConfigurationField{Key: field.Key, Label: field.Label, Help: field.Help, ValueType: field.ValueType, Required: field.Required, Placeholder: field.Placeholder})
	}
	return result
}
func castGrant(value entity.IntegrationGrant) *controlplanev1.IntegrationGrant {
	grant := &controlplanev1.IntegrationGrant{Ref: value.Ref, Version: value.Version, CapabilityKey: value.CapabilityKey, TargetName: value.TargetName, Enabled: value.Enabled}
	if value.TargetType == "AGENT" {
		grant.AgentRef = value.TargetRef
	} else {
		grant.WorkflowRef = value.TargetRef
	}
	return grant
}
func castConnection(value entity.IntegrationConnection) *controlplanev1.IntegrationConnection {
	credentialsHint := "i18n:INTEGRATION_CREDENTIAL_NOT_CONFIGURED"
	if value.MaskedCredentialsState == "CONFIGURED" {
		credentialsHint = "i18n:INTEGRATION_CREDENTIAL_CONFIGURED"
	} else if value.MaskedCredentialsState == "INVALID" {
		credentialsHint = "i18n:INTEGRATION_CREDENTIAL_INVALID"
	}
	result := &controlplanev1.IntegrationConnection{Ref: value.Ref, Version: value.Version, DefinitionKey: value.DefinitionKey, Name: value.Name, State: connectionState(value.State), CredentialsConfigured: value.MaskedCredentialsState == "CONFIGURED", CredentialsHint: credentialsHint, LastTestedAt: optionalTimestamp(value.LastTestedAt), LastTestOutcome: value.LastTestSummary, NextActions: nextActions(value.NextActions)}
	for _, capability := range value.Capabilities {
		result.Capabilities = append(result.Capabilities, castIntegrationCapability(capability))
	}
	for _, grant := range value.Grants {
		result.Grants = append(result.Grants, castGrant(grant))
	}
	return result
}
func castPlan(value *entity.AssistantPlan) *controlplanev1.AssistantPlan {
	if value == nil {
		return nil
	}
	result := &controlplanev1.AssistantPlan{Ref: value.Ref, Version: value.Version, AuditSummary: value.Summary, Applied: value.State == "APPLIED"}
	for _, operation := range value.Operations {
		raw := controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+operation.Type]
		result.Operations = append(result.Operations, &controlplanev1.AssistantPlanOperation{Ref: operation.Key, Type: controlplanev1.AssistantPlanOperation_Type(raw), Title: assistantPlanOperationTitle(operation), Summary: operation.Summary, BoundedInput: structure(operation.Input), Permitted: true})
	}
	if value.State == "PROPOSED" {
		result.NextActions = []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_APPLY_PLAN}
	}
	return result
}

func assistantPlanOperationTitle(operation entity.AssistantPlanOperation) string {
	field := ""
	switch operation.Type {
	case "CREATE_PROJECT", "CREATE_AGENT", "CREATE_WORKFLOW", "CREATE_SCHEDULE", "CREATE_INTEGRATION_CONNECTION":
		field = "name"
	case "LAUNCH_RUN":
		field = "title"
	}

	title := ""
	if field != "" {
		title, _ = operation.Input[field].(string)
	}
	if strings.TrimSpace(title) == "" {
		title = operation.Summary
	}
	return boundedAssistantPlanOperationTitle(title)
}

func boundedAssistantPlanOperationTitle(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximumAssistantPlanOperationTitleRunes {
		return string(runes)
	}
	return string(runes[:maximumAssistantPlanOperationTitleRunes-1]) + "…"
}
func castConversation(value entity.AssistantConversation) *controlplanev1.AssistantConversation {
	result := &controlplanev1.AssistantConversation{Ref: value.Ref, Version: value.Version, Title: value.Title, ProjectRef: value.ProjectRef, UpdatedAt: timestamp(value.UpdatedAt)}
	for index, turn := range value.Turns {
		result.Turns = append(result.Turns, &controlplanev1.AssistantTurn{Ref: turn.Ref, Sequence: int64(index + 1), Role: turn.Actor, Content: turn.Content, State: turn.State, CreatedAt: timestamp(turn.CreatedAt)})
	}
	if value.LatestPlan != nil {
		result.Turns = append(result.Turns, &controlplanev1.AssistantTurn{Ref: value.LatestPlan.Ref, Sequence: int64(len(result.Turns) + 1), Role: "SYSTEM_ASSISTANT", Content: value.LatestPlan.Summary, State: value.LatestPlan.State, Plan: castPlan(value.LatestPlan), CreatedAt: timestamp(value.LatestPlan.CreatedAt)})
	}
	return result
}
func castAssistant(value entity.SystemAssistant) *controlplanev1.SystemAssistant {
	summary := "i18n:ASSISTANT_RECOVERING"
	if value.RuntimeState == "BUSY" {
		summary = "i18n:ASSISTANT_EXECUTING"
	} else if value.Ready {
		summary = "i18n:ASSISTANT_READY"
	}
	return &controlplanev1.SystemAssistant{Ref: value.Ref, Version: value.Version, Name: value.Name, SystemLabel: "i18n:SYSTEM_ASSISTANT_LABEL", CorePromptRevision: value.CorePromptRevision, OwnerInstructions: value.OwnerInstructions, OwnerInstructionsRevision: int32(value.Version), RuntimeState: assistantState(value.RuntimeState), RuntimeRevision: value.RuntimeRevision, WarmSessionRef: value.WarmSessionRef, ReadinessSummary: summary, LastHeartbeatAt: optionalTimestamp(value.LastHeartbeatAt), NextActions: nextActions(value.NextActions)}
}
func castAudit(value entity.AuditEvent) *controlplanev1.AuditEvent {
	return &controlplanev1.AuditEvent{Ref: value.Ref, ProjectRef: value.ProjectRef, Initiator: &controlplanev1.UserSummary{Ref: value.ActorRef, DisplayName: value.ActorName}, Executor: value.Executor, Source: value.Source, Action: value.Action, ResourceType: value.ResourceKind, ResourceRef: value.ResourceRef, ResourceName: value.ResourceName, Outcome: value.Outcome, SafeSummary: value.Summary, OccurredAt: timestamp(value.OccurredAt)}
}
func castIncident(value entity.Incident) *controlplanev1.Incident {
	return &controlplanev1.Incident{Ref: value.Ref, ProjectRef: value.ProjectRef, RunRef: value.RunRef, Category: value.Category, Severity: value.Severity, State: value.State, SafeSummary: value.SafeSummary, SafeNextStep: value.SafeNextStep, CoreAffected: value.CoreAffected, CreatedAt: timestamp(value.CreatedAt)}
}

func castBootstrap(value repository.BootstrapState) *controlplanev1.BootstrapState {
	return &controlplanev1.BootstrapState{Initialized: value.Bootstrapped, OnboardingComplete: value.OnboardingCompleted, WebOnlyReady: value.Assistant.Ready, Assistant: castAssistant(value.Assistant), CurrentUser: castUser(value.Actor), PlatformRole: platformRole(value.PlatformRole), NextActions: nextActions(value.NextActions)}
}

func castOverview(value repository.Overview) *controlplanev1.Overview {
	result := &controlplanev1.Overview{ProjectCount: value.ProjectCount, AgentCount: value.AgentCount, ActiveRunCount: value.ActiveRunCount, PendingGateCount: value.PendingGateCount}
	for _, item := range value.ActiveRuns {
		result.ActiveRuns = append(result.ActiveRuns, castRun(item))
	}
	for _, item := range value.PendingGates {
		result.PendingGates = append(result.PendingGates, castGate(item))
	}
	for _, item := range value.RecentArtifacts {
		result.RecentArtifacts = append(result.RecentArtifacts, castArtifact(item))
	}
	return result
}

func castAdministration(value repository.Administration) *controlplanev1.AdministrationState {
	result := &controlplanev1.AdministrationState{Profile: value.Profile, CoreReady: value.CoreReady, CoreSummary: value.CoreSummary, Assistant: castAssistant(value.Assistant), ObservedAt: timestamp(value.ObservedAt)}
	for _, item := range value.OptionalAdapters {
		result.OptionalAdapters = append(result.OptionalAdapters, castDefinition(item))
	}
	for _, item := range value.Incidents {
		result.Incidents = append(result.Incidents, castIncident(item))
	}
	return result
}
