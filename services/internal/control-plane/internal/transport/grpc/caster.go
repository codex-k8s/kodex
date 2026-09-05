package grpc

import (
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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

func artifactLifecycleState(value string) controlplanev1.ArtifactLifecycleState {
	if value == "" {
		value = "ACTIVE"
	}
	raw := controlplanev1.ArtifactLifecycleState_value["ARTIFACT_LIFECYCLE_STATE_"+value]
	return controlplanev1.ArtifactLifecycleState(raw)
}

func attachmentSetState(value string) controlplanev1.AttachmentSetState {
	return controlplanev1.AttachmentSetState(controlplanev1.AttachmentSetState_value["ATTACHMENT_SET_STATE_"+value])
}

func attachmentSetPurpose(value string) controlplanev1.AttachmentSetPurpose {
	return controlplanev1.AttachmentSetPurpose(controlplanev1.AttachmentSetPurpose_value["ATTACHMENT_SET_PURPOSE_"+value])
}
func scheduleState(schedule entity.Schedule) controlplanev1.ScheduleState {
	switch schedule.State {
	case "ARCHIVED":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_ARCHIVED
	case "NEEDS_ATTENTION":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_NEEDS_ATTENTION
	case "DELETED":
		return controlplanev1.ScheduleState_SCHEDULE_STATE_DELETED
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
	raw, exists := controlplanev1.ConnectionState_value["CONNECTION_STATE_"+value]
	if !exists {
		return controlplanev1.ConnectionState_CONNECTION_STATE_UNSPECIFIED
	}
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
	return &controlplanev1.Membership{Ref: value.Ref, Version: value.Version, User: castUser(value.User), PlatformRole: platformRole(value.Role), ProjectPermissions: projectPermissions(value.Permissions), Active: value.Active, NextActions: nextActions(value.NextActions), ProjectRef: value.ProjectRef}
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
func castProviderPolicy(value entity.ProviderAccountPolicyVersion) *controlplanev1.ProviderAccountPolicyVersion {
	result := &controlplanev1.ProviderAccountPolicyVersion{Ref: value.Ref, Version: value.Version, Mode: value.Mode,
		Digest: value.Digest, CreatedAt: timestamp(value.CreatedAt)}
	for _, candidate := range value.AccountCandidates {
		result.AccountCandidates = append(result.AccountCandidates, &controlplanev1.ProviderAccountCandidate{AccountRef: candidate.AccountRef, Weight: candidate.Weight})
	}
	return result
}
func castAgentRuntimeConfiguration(value entity.AgentRuntimeConfiguration) *controlplanev1.AgentRuntimeConfiguration {
	return &controlplanev1.AgentRuntimeConfiguration{Ref: value.Ref, Version: value.Version, AgentRef: value.AgentRef,
		RuntimeProfileRef: value.RuntimeProfileRef, Provider: value.Provider, Model: value.Model,
		ProviderPolicy: castProviderPolicy(value.ProviderPolicy), Digest: value.Digest, CreatedAt: timestamp(value.CreatedAt)}
}
func castConfigOverlay(value *entity.ConfigOverlayVersion) *controlplanev1.ConfigOverlayVersion {
	if value == nil {
		return nil
	}
	return &controlplanev1.ConfigOverlayVersion{Ref: value.Ref, Version: value.Version, Revision: value.Revision,
		State: value.State, Content: value.Content, Digest: value.Digest, ValidationMessages: value.ValidationMessages,
		CreatedAt: timestamp(value.CreatedAt), PublishedAt: optionalTimestamp(value.PublishedAt)}
}
func castRuntimeEnvironmentVersion(value entity.RuntimeEnvironmentVersion) *controlplanev1.RuntimeEnvironmentVersion {
	result := &controlplanev1.RuntimeEnvironmentVersion{Ref: value.Ref, Version: value.Version, Revision: value.Revision,
		Digest: value.Digest, CreatedAt: timestamp(value.CreatedAt), Image: &controlplanev1.RuntimeEnvironmentImage{
			ArtifactRef: value.Image.ArtifactRef, RecipeRef: value.Image.RecipeRef, RecipeGeneration: value.Image.RecipeGeneration,
			Reference: value.Image.Reference, Digest: value.Image.Digest,
		}, Policy: castRuntimeEnvironmentPolicy(value.Policy)}
	for _, item := range value.Values {
		result.Values = append(result.Values, &controlplanev1.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	for _, item := range value.SecretDescriptors {
		result.SecretDescriptors = append(result.SecretDescriptors, &controlplanev1.RuntimeSecretDescriptor{Name: item.Name,
			SecretRef: item.SecretRef, Namespace: item.Namespace, Revision: item.Revision,
			SecretName: item.SecretName, SecretKey: item.SecretKey, SecretUid: item.SecretUID,
			SecretResourceVersion: item.SecretResourceVersion, ContentSha256: item.ContentSHA256})
	}
	for _, item := range value.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RuntimeEnvironmentTool{Name: item.Name, Command: item.Command, Description: item.Description, UsageHint: item.UsageHint})
	}
	return result
}

func castRuntimeEnvironmentPolicy(value runtimecontract.RuntimeEnvironmentPolicy) *controlplanev1.RuntimeEnvironmentPolicy {
	result := &controlplanev1.RuntimeEnvironmentPolicy{
		Resources: &controlplanev1.RuntimeResourcePolicy{
			CpuRequestMilli: value.Resources.CPURequestMilli, CpuLimitMilli: value.Resources.CPULimitMilli,
			MemoryRequestMib: value.Resources.MemoryRequestMiB, MemoryLimitMib: value.Resources.MemoryLimitMiB,
			EphemeralStorageRequestMib: value.Resources.EphemeralStorageRequestMiB,
			EphemeralStorageLimitMib:   value.Resources.EphemeralStorageLimitMiB,
		}, Network: &controlplanev1.RuntimeNetworkPolicy{DenyByDefault: value.Network.DenyByDefault},
		KubernetesAccess: &controlplanev1.RuntimeKubernetesAccessProfile{
			Kind: castRuntimeKubernetesAccessKind(value.KubernetesAccess.Kind), Namespace: value.KubernetesAccess.Namespace,
		}, ResourcesDigest: value.ResourcesDigest, VolumesDigest: value.VolumesDigest,
		NetworkDigest: value.NetworkDigest, RbacDigest: value.RBACDigest,
	}
	for _, volume := range value.Volumes {
		result.Volumes = append(result.Volumes, &controlplanev1.RuntimeVolume{
			Name: volume.Name, Kind: castRuntimeVolumeKind(volume.Kind), SizeMib: volume.SizeMiB, MountPath: volume.MountPath,
		})
	}
	for _, egress := range value.Network.Egress {
		result.Network.Egress = append(result.Network.Egress, &controlplanev1.RuntimeNetworkEgress{
			Destination: castRuntimeNetworkDestination(egress.Destination), Protocol: castRuntimeNetworkProtocol(egress.Protocol), Port: egress.Port,
		})
	}
	return result
}

func castRuntimeVolumeKind(value string) controlplanev1.RuntimeVolumeKind {
	if value == runtimecontract.RuntimeVolumeEphemeralMemory {
		return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY
	}
	return controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK
}

func castRuntimeNetworkDestination(value string) controlplanev1.RuntimeNetworkDestination {
	switch value {
	case runtimecontract.RuntimeEgressDNS:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS
	case runtimecontract.RuntimeEgressRuntimeCallback:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK
	case runtimecontract.RuntimeEgressProviderProxy:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY
	case runtimecontract.RuntimeEgressKubernetesAPI:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_KUBERNETES_API
	default:
		return controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_UNSPECIFIED
	}
}

func castRuntimeNetworkProtocol(value string) controlplanev1.RuntimeNetworkProtocol {
	if value == runtimecontract.RuntimeProtocolUDP {
		return controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_UDP
	}
	return controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_TCP
}

func castRuntimeKubernetesAccessKind(value string) controlplanev1.RuntimeKubernetesAccessKind {
	if value == runtimecontract.RuntimeKubernetesAccessReadOwnExecution {
		return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION
	}
	return controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE
}
func castRuntimeEnvironment(value entity.RuntimeEnvironmentSet) *controlplanev1.RuntimeEnvironmentSet {
	return &controlplanev1.RuntimeEnvironmentSet{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef,
		Name: value.Name, Description: value.Description, State: value.State,
		CurrentVersion: castRuntimeEnvironmentVersion(value.CurrentVersion), UpdatedAt: timestamp(value.UpdatedAt),
		Ready: value.Ready, ReadinessBlockers: value.ReadinessBlockers, NextActions: nextActions(value.NextActions)}
}
func castRuntimeConfigurationView(value entity.AgentRuntimeConfigurationView) *controlplanev1.AgentRuntimeConfigurationView {
	result := &controlplanev1.AgentRuntimeConfigurationView{Configuration: castAgentRuntimeConfiguration(value.Configuration),
		PublishedOverlay: castConfigOverlay(&value.PublishedOverlay), DraftOverlay: castConfigOverlay(value.DraftOverlay),
		EnvironmentBinding: &controlplanev1.AgentRuntimeEnvironmentBinding{Ref: value.EnvironmentBinding.Ref,
			Version: value.EnvironmentBinding.Version, AgentRef: value.EnvironmentBinding.AgentRef,
			EnvironmentRef: value.EnvironmentBinding.EnvironmentRef, Digest: value.EnvironmentBinding.Digest, VersionRef: value.EnvironmentBinding.VersionRef},
		Environment: castRuntimeEnvironment(value.Environment), SafeEffectiveConfig: value.SafeEffectiveConfig,
		AgentVersion: value.AgentVersion}
	for _, binding := range value.SkillBindings {
		result.SkillBindings = append(result.SkillBindings, castContextBinding(binding))
	}
	for _, binding := range value.MemoryBindings {
		result.MemoryBindings = append(result.MemoryBindings, castContextBinding(binding))
	}
	return result
}
func castIntegrationCapability(value entity.IntegrationCapability) *controlplanev1.IntegrationCapability {
	result := &controlplanev1.IntegrationCapability{
		Key: value.Key, Name: value.Name, Description: value.Description, Operation: value.Operation,
		Risk: value.Risk, TypedRisk: integrationRisk(value.Risk), ApprovalRequired: value.ApprovalPolicy != "NONE",
		ApprovalPolicy: integrationApprovalPolicy(value.ApprovalPolicy), ResourceKind: integrationResourceKind(value.ResourceKind),
		InputSchema: value.InputSchema, InputSchemaSha256: value.InputSchemaSHA256,
	}
	for _, field := range value.InputFields {
		result.InputFields = append(result.InputFields, castIntegrationField(field))
	}
	return result
}

func integrationRisk(value string) controlplanev1.IntegrationRisk {
	return controlplanev1.IntegrationRisk(controlplanev1.IntegrationRisk_value["INTEGRATION_RISK_"+value])
}

func integrationApprovalPolicy(value string) controlplanev1.IntegrationApprovalPolicy {
	return controlplanev1.IntegrationApprovalPolicy(controlplanev1.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+value])
}

func integrationResourceKind(value string) controlplanev1.IntegrationResourceKind {
	return controlplanev1.IntegrationResourceKind(controlplanev1.IntegrationResourceKind_value["INTEGRATION_RESOURCE_KIND_"+value])
}

func castIntegrationField(value entity.IntegrationConfigurationField) *controlplanev1.IntegrationConfigurationField {
	return &controlplanev1.IntegrationConfigurationField{Key: value.Key, Label: value.Label, Help: value.Help, ValueType: value.ValueType, Required: value.Required, Placeholder: value.Placeholder}
}

func castIntegrationCredential(value entity.IntegrationCredentialRevision) *controlplanev1.IntegrationCredentialRevision {
	return &controlplanev1.IntegrationCredentialRevision{
		Ref: value.Ref, Revision: value.Revision, SecretRef: value.SecretRef, SecretUid: value.SecretUID,
		SecretResourceVersion: value.SecretResourceVersion, ContentSha256: value.ContentSHA256, CreatedAt: timestamp(value.CreatedAt),
	}
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
	avatar := &controlplanev1.AgentAvatar{Source: controlplanev1.AgentAvatar_SOURCE_FALLBACK}
	if value.Avatar.ArtifactRef != "" {
		avatar.Source = controlplanev1.AgentAvatar_SOURCE_ARTIFACT
		avatar.ArtifactRef = value.Avatar.ArtifactRef
		avatar.ArtifactRevision = value.Avatar.ArtifactRevision
		avatar.ContentPath = value.Avatar.ContentPath
	}
	return &controlplanev1.Agent{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RoleDefinitionRef: value.RoleDefinitionRef, RoleDefinitionName: value.RoleDefinitionName, Name: value.Name, Purpose: value.Purpose, RoleDescription: value.RoleDescription, AvatarUrl: value.AvatarURL, Avatar: avatar, State: agentState(value.State), Enabled: value.Enabled, System: value.System, Runtime: &controlplanev1.RuntimeSelection{Ref: value.RuntimeKey, Name: value.RuntimeName, Revision: value.RuntimeRevision, Ready: value.State == "READY", Provider: value.Provider, Model: value.Model}, PublishedInstructions: castInstruction(value.PublishedInstructions), DraftInstructions: castInstruction(value.DraftInstructions), Capabilities: capabilities, IntegrationGrantRefs: value.IntegrationGrantRefs, KnowledgeArtifactRefs: value.KnowledgeArtifactRefs, UpdatedAt: timestamp(value.UpdatedAt), NextActions: nextActions(value.NextActions)}
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
	result := &controlplanev1.Run{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, SessionRef: value.SessionRef, RootRunRef: value.RootRunRef, ParentRunRef: value.ParentRunRef, RetryOfRunRef: value.RetryOfRunRef, Target: castRunTarget(value.Target), Title: value.Title, TitleSource: value.TitleSource, ActivitySummary: value.ActivitySummary, InputSummary: value.Task, State: runState(value.State), Source: runSource(value.Source), Initiator: &controlplanev1.UserSummary{DisplayName: value.InitiatorName}, Attempt: value.Attempt, GraphRevision: value.GraphRevision, LastEventSequence: value.EventSequence, ResultSummary: value.ResultSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, Usage: castTokenUsage(value.Usage), InputAttachmentSetRef: value.InputAttachmentSetRef, ArtifactRefs: value.ArtifactRefs, GateRefs: value.GateRefs, CreatedAt: timestamp(value.CreatedAt), StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions)}
	for _, incident := range value.Incidents {
		result.Incidents = append(result.Incidents, castIncident(incident))
	}
	return result
}
func castNode(value entity.RunNode) *controlplanev1.RunNode {
	return &controlplanev1.RunNode{Ref: value.Ref, RunRef: value.RunRef, ParentNodeRef: value.ParentNodeRef, Type: nodeType(value.Type), State: nodeState(value.State), DisplayName: value.DisplayName, Role: value.Role, AgentRef: value.AgentRef, TurnRef: value.TurnRef, Attempt: value.Attempt, InputSummary: value.InputSummary, ProgressSummary: value.ProgressSummary, IntegrationNames: value.IntegrationNames, ArtifactRefs: value.ArtifactRefs, ChildRunRefs: value.ChildRunRefs, CallbackSummary: value.CallbackSummary, SafeErrorCode: value.SafeErrorCode, SafeErrorMessage: value.SafeErrorMessage, CreatedAt: timestamp(value.CreatedAt), StartedAt: optionalTimestamp(value.StartedAt), FinishedAt: optionalTimestamp(value.FinishedAt), NextActions: nextActions(value.NextActions), Planned: value.MaterializationState == "PLANNED"}
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
	event := &controlplanev1.RunEvent{Ref: value.Ref, RunRef: value.RunRef, Sequence: value.Sequence, Type: eventType(value.Type), NodeRef: value.NodeRef, EdgeRef: value.EdgeRef, GateRef: value.GateRef, ArtifactRef: value.ArtifactRef, Summary: value.Summary, Progress: value.Progress, RunState: runState(value.RunState), NodeState: nodeState(value.NodeState), OccurredAt: timestamp(value.OccurredAt), GraphRevision: value.GraphRevision, Run: castRunDelta(value.Delta.Run), Actor: &controlplanev1.RunEventActor{Kind: controlplanev1.RunEventActorKind(controlplanev1.RunEventActorKind_value["RUN_EVENT_ACTOR_KIND_"+value.Actor.Kind]), Ref: value.Actor.Ref, Name: value.Actor.Name}, MessageKind: controlplanev1.RunEventMessageKind(controlplanev1.RunEventMessageKind_value["RUN_EVENT_MESSAGE_KIND_"+value.MessageKind])}
	if value.ToolCall != nil {
		event.ToolCall = &controlplanev1.RunToolCall{Ref: value.ToolCall.Ref, Tool: value.ToolCall.Tool, SafeParameters: structure(value.ToolCall.SafeParameters), CapabilityRef: value.ToolCall.CapabilityRef, GrantRef: value.ToolCall.GrantRef, State: controlplanev1.RunToolCallState(controlplanev1.RunToolCallState_value["RUN_TOOL_CALL_STATE_"+value.ToolCall.State]), DurationMs: value.ToolCall.DurationMS, SafeResult: value.ToolCall.SafeResult, AuditRef: value.ToolCall.AuditRef}
	}
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
	gate := &controlplanev1.OwnerGate{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RunRef: value.RunRef, NodeRef: value.NodeRef, Title: value.Title, ContextSummary: value.ContextSummary, ConsequencesSummary: value.Prompt, RequestedBy: &controlplanev1.UserSummary{Ref: value.RequestedByRef, DisplayName: value.RequestedByName}, State: gateState(value.State), AllowedDecisions: gateDecisions(value.AllowedDecisions), Decision: gateDecision(value.Decision), DecisionComment: value.DecisionComment, OpenedAt: timestamp(value.CreatedAt), DecidedAt: optionalTimestamp(value.ResolvedAt), ResolutionAttachmentSetRef: value.ResolutionAttachmentSetRef, NextActions: nextActions(value.NextActions)}
	if value.ResolvedByName != "" {
		gate.DecidedBy = &controlplanev1.UserSummary{DisplayName: value.ResolvedByName}
	}
	return gate
}
func castArtifact(value entity.Artifact) *controlplanev1.Artifact {
	return &controlplanev1.Artifact{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, RunRef: value.RunRef, SessionRef: value.SessionRef, FileName: value.FileName, MediaType: value.MediaType, SizeBytes: value.SizeBytes, ScanState: scanState(value.ScanState), Source: artifactSource(value.Source), Revision: int32(value.Revision), AgentBindings: value.Bindings, PreviewAvailable: value.PreviewState == "AVAILABLE", CreatedAt: timestamp(value.CreatedAt), NextActions: nextActions(value.NextActions), Digest: value.Digest, LifecycleState: artifactLifecycleState(value.LifecycleState), DeletedAt: optionalTimestamp(value.DeletedAt), PurgeAfter: optionalTimestamp(value.PurgeAfter)}
}
func castAttachmentSet(value entity.AttachmentSet) *controlplanev1.AttachmentSet {
	result := &controlplanev1.AttachmentSet{Ref: value.Ref, FamilyRef: value.FamilyRef, Revision: value.Revision,
		Version: value.Version, ProjectRef: value.ProjectRef, State: attachmentSetState(value.State),
		Purpose: attachmentSetPurpose(value.Purpose), Source: value.Source, ItemCount: value.ItemCount,
		TotalSizeBytes: value.TotalSizeBytes, ManifestDigest: value.ManifestDigest, CreatedAt: timestamp(value.CreatedAt),
		FinalizedAt: optionalTimestamp(value.FinalizedAt), Superseded: value.Superseded}
	for _, item := range value.Items {
		result.Items = append(result.Items, &controlplanev1.AttachmentSetItem{ArtifactRef: item.ArtifactRef,
			ArtifactRevision: item.ArtifactRevision, ArtifactVersion: item.ArtifactVersion,
			DisplayName: item.DisplayName, MediaType: item.MediaType, SizeBytes: item.SizeBytes,
			Digest: item.Digest, Source: artifactSource(item.Source), Position: item.Position})
	}
	return result
}
func castSchedule(value entity.Schedule) *controlplanev1.Schedule {
	return &controlplanev1.Schedule{
		Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name,
		Target: castRunTarget(value.Target), State: scheduleState(value), Preset: value.Preset,
		CronExpression: value.CronExpression, Timezone: value.Timezone, Input: structure(value.Input),
		SessionPolicy: value.SessionPolicy, NotificationPolicy: value.NotificationPolicy,
		NextRunAt: optionalTimestamp(value.NextRunAt), NextActions: nextActions(value.NextActions),
		TimeOfDay: value.TimeOfDay, DayOfWeek: value.DayOfWeek,
		CurrentRevision:    castScheduleRevision(value.CurrentRevision),
		ContinueSessionRef: value.ContinueSessionRef,
		LastOutcome:        value.LastOutcome,
		DstGapPolicy:       value.DSTGapPolicy, DstFoldPolicy: value.DSTFoldPolicy,
		MisfirePolicy: value.MisfirePolicy, OverlapPolicy: value.OverlapPolicy,
		TargetVersion: value.TargetVersion, TargetDigest: value.TargetDigest,
		AutomationText: value.AutomationText, PromptInputs: structure(value.PromptInputs),
	}
}
func castDefinition(value entity.IntegrationDefinition) *controlplanev1.IntegrationDefinition {
	result := &controlplanev1.IntegrationDefinition{
		Key: value.Key, Name: value.Name, Description: value.Description, Category: value.Category, BuiltIn: true, Available: value.Enabled,
		SchemaVersion: value.SchemaVersion, DefinitionVersion: value.DefinitionVersion,
		Origin: controlplanev1.IntegrationDefinitionOrigin_INTEGRATION_DEFINITION_ORIGIN_SHIPPED,
		Digest: value.Digest, Adapter: value.Adapter, CredentialSecretKey: value.CredentialSecretKey,
		AdapterOwner: value.AdapterOwner, ExecutionRoute: value.ExecutionRoute, AdapterReadiness: value.AdapterReadiness,
	}
	for _, capability := range value.Capabilities {
		result.Capabilities = append(result.Capabilities, castIntegrationCapability(capability))
	}
	for _, field := range value.ConfigurationFields {
		result.ConfigurationFields = append(result.ConfigurationFields, castIntegrationField(field))
	}
	return result
}
func castGrant(value entity.IntegrationGrant) *controlplanev1.IntegrationGrant {
	grant := &controlplanev1.IntegrationGrant{
		Ref: value.Ref, Version: value.Version, CapabilityKey: value.CapabilityKey, TargetName: value.TargetName, Enabled: value.Enabled,
		TypedRisk: integrationRisk(value.Risk), ApprovalPolicy: integrationApprovalPolicy(value.ApprovalPolicy),
		ResourceScope: &controlplanev1.IntegrationResourceScope{Kind: integrationResourceKind(value.ResourceKind), Values: value.ResourceScope, Digest: value.ResourceScopeDigest},
	}
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
	result := &controlplanev1.IntegrationConnection{
		Ref: value.Ref, Version: value.Version, DefinitionKey: value.DefinitionKey, Name: value.Name,
		DefinitionVersion: value.DefinitionVersion, DefinitionDigest: value.DefinitionDigest,
		State: connectionState(value.State), CredentialsConfigured: value.MaskedCredentialsState == "CONFIGURED",
		CredentialsHint: credentialsHint, LastTestedAt: optionalTimestamp(value.LastTestedAt), LastTestOutcome: value.LastTestSummary,
		PublicConfiguration: structure(value.PublicConfiguration), NextActions: nextActions(value.NextActions),
		CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt),
	}
	if value.CredentialRevision != nil {
		result.CredentialRevision = castIntegrationCredential(*value.CredentialRevision)
	}
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
	rawState := controlplanev1.AssistantPlanState_value["ASSISTANT_PLAN_STATE_"+value.State]
	result := &controlplanev1.AssistantPlan{Ref: value.Ref, Version: value.Version, ConversationRef: value.ConversationRef,
		ProjectRef: value.ProjectRef, AuditSummary: value.Summary, Applied: value.State == "APPLIED",
		State: controlplanev1.AssistantPlanState(rawState), Revision: value.Revision, ValidatedRevision: value.ValidatedRevision,
		ContentDigest: value.ContentDigest, ValidationProblems: append([]string(nil), value.ValidationProblems...),
		ValidatedAt: optionalTimestamp(value.ValidatedAt), AppliedAt: optionalTimestamp(value.AppliedAt)}
	for _, operation := range value.Operations {
		raw := controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+operation.Type]
		rawAction := controlplanev1.AssistantPlanOperation_Action_value["ACTION_"+operation.Action]
		parameters := operation.Parameters
		if parameters == nil {
			parameters = operation.Input
		}
		result.Operations = append(result.Operations, &controlplanev1.AssistantPlanOperation{
			Ref: operation.Key, Type: controlplanev1.AssistantPlanOperation_Type(raw), Action: controlplanev1.AssistantPlanOperation_Action(rawAction),
			Title: assistantPlanOperationTitle(operation), Summary: operation.Summary, TargetKind: operation.Target.Kind,
			TargetRef: operation.Target.Ref, TargetName: operation.Target.Name, ExpectedVersion: operation.ExpectedVersion,
			Parameters: structure(parameters), Before: structure(operation.Before), After: structure(operation.After), Selected: operation.Selected,
			Permitted: operation.Permitted, UnavailableReason: operation.UnavailableReason,
			ValidationProblems: append([]string(nil), operation.ValidationProblems...),
		})
	}
	if value.State == "VALID" {
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
		parameters := operation.Parameters
		if parameters == nil {
			parameters = operation.Input
		}
		title, _ = parameters[field].(string)
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
	context := &controlplanev1.AssistantContextDescriptor{Route: value.Context.Route, EntityKind: value.Context.EntityKind,
		EntityRef: value.Context.EntityRef, EntityName: value.Context.EntityName, EntityVersion: value.Context.EntityVersion}
	for _, operation := range value.Context.AllowedOperations {
		context.AllowedOperations = append(context.AllowedOperations, controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+operation]))
	}
	result := &controlplanev1.AssistantConversation{Ref: value.Ref, Version: value.Version, Title: value.Title,
		TitleSource: value.TitleSource, TitleRevision: value.TitleRevision, ProjectRef: value.ProjectRef,
		Context: context, UpdatedAt: timestamp(value.UpdatedAt)}
	nextSequence := int64(1)
	for _, turn := range value.Turns {
		result.Turns = append(result.Turns, &controlplanev1.AssistantTurn{Ref: turn.Ref, Sequence: turn.Sequence, Role: publicAssistantTurnRole(turn.Actor), Content: turn.Content, State: turn.State, AttachmentSetRef: turn.AttachmentSetRef, CreatedAt: timestamp(turn.CreatedAt)})
		if turn.Sequence >= nextSequence {
			nextSequence = turn.Sequence + 1
		}
	}
	if value.LatestPlan != nil {
		result.Turns = append(result.Turns, &controlplanev1.AssistantTurn{Ref: value.LatestPlan.Ref, Sequence: nextSequence, Role: "ASSISTANT", Content: value.LatestPlan.Summary, State: "COMPLETED", Plan: castPlan(value.LatestPlan), CreatedAt: timestamp(value.LatestPlan.CreatedAt)})
	}
	return result
}

func publicAssistantTurnRole(role string) string {
	switch role {
	case "USER", "ASSISTANT", "SYSTEM_RECEIPT":
		return role
	case "SYSTEM_ASSISTANT":
		return "ASSISTANT"
	default:
		return "SYSTEM_RECEIPT"
	}
}

func castPlanReceipt(value *entity.AssistantPlanReceipt) *controlplanev1.AssistantPlanReceipt {
	if value == nil {
		return nil
	}
	result := &controlplanev1.AssistantPlanReceipt{Ref: value.Ref, PlanRef: value.PlanRef, PlanRevision: value.PlanRevision,
		Outcome: value.Outcome, AuditRefs: append([]string(nil), value.AuditRefs...), CreatedResourceRefs: append([]string(nil), value.CreatedResourceRefs...), CreatedAt: timestamp(value.CreatedAt)}
	for _, operation := range value.Operations {
		result.Operations = append(result.Operations, &controlplanev1.AssistantPlanOperationReceipt{OperationRef: operation.OperationRef,
			ResourceRef: operation.ResourceRef, Outcome: operation.Outcome, AuditRef: operation.AuditRef})
	}
	for _, conflict := range value.Conflicts {
		expected, _ := structpb.NewValue(conflict.Expected)
		actual, _ := structpb.NewValue(conflict.Actual)
		result.Conflicts = append(result.Conflicts, &controlplanev1.AssistantPlanConflict{OperationRef: conflict.OperationRef,
			TargetRef: conflict.TargetRef, Field: conflict.Field, Expected: expected, Actual: actual})
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
	return &controlplanev1.BootstrapState{Initialized: value.Bootstrapped, OnboardingComplete: value.OnboardingCompleted, WebOnlyReady: value.Assistant.Ready, Assistant: castAssistant(value.Assistant), CurrentUser: castUser(value.Actor), PlatformRole: platformRole(value.PlatformRole), NextActions: nextActions(value.NextActions),
		SpeechTranscription: &controlplanev1.SpeechTranscriptionAvailability{Eligible: value.SpeechTranscription.Eligible, Available: false, Reason: value.SpeechTranscription.Reason}}
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
