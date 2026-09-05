package grpc

import (
	"context"
	"io"
	"math"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maximumExecutionArtifactBytes = runtimecontract.MaximumSkillFileBytes

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
func mapInt64(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int64:
		return value
	case int32:
		return int64(value)
	case int:
		return int64(value)
	case uint64:
		if value > math.MaxInt64 {
			return 0
		}
		return int64(value)
	case uint32:
		return int64(value)
	case uint:
		if uint64(value) > math.MaxInt64 {
			return 0
		}
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}
func mapTime(values map[string]any, key string) *timestamppb.Timestamp {
	value, _ := values[key].(time.Time)
	return timestamp(value)
}
func mapStrings(values map[string]any, key string) []string {
	if value, ok := values[key].([]string); ok {
		return value
	}
	if value, ok := values[key].([]any); ok {
		result := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	}
	return nil
}

func castLease(values map[string]any) *controlplanev1.WorkLease {
	return &controlplanev1.WorkLease{Ref: mapString(values, "leaseRef"), Fence: mapString(values, "fence"), Generation: mapInt64(values, "generation"), ExpiresAt: mapTime(values, "expiresAt")}
}

func castRuntimeRevision(values map[string]any) *controlplanev1.RuntimeRevisionSnapshot {
	instructions := mapString(values, "instructions")
	if instructions == "" {
		instructions = mapString(values, "corePrompt")
	}
	agentRef := mapString(values, "agentRef")
	if agentRef == "" {
		agentRef = mapString(values, "assistantRef")
	}
	result := &controlplanev1.RuntimeRevisionSnapshot{Ref: mapString(values, "runtimeRevisionRef"), Version: mapInt64(values, "runtimeRevisionVersion"), OrganizationRef: mapString(values, "organizationRef"), RunRef: mapString(values, "runRef"), NodeRef: mapString(values, "nodeRef"), SessionRef: mapString(values, "sessionRef"), TurnRef: mapString(values, "turnRef"), Attempt: int32(mapInt64(values, "attempt")), AgentRef: agentRef, Instructions: instructions, InputDigest: mapString(values, "inputDigest"), RevisionDigest: mapString(values, "revisionDigest"), SystemAssistant: mapString(values, "stableKey") == "system-assistant"}
	result.RoleDefinitionRef = mapString(values, "roleDefinitionRef")
	if !castRuntimeContext(result, values["contextSnapshot"], mapString(values, "projectRef")) {
		return nil
	}
	result.InstructionRef = mapString(values, "instructionRef")
	result.InstructionDigest = mapString(values, "instructionDigest")
	result.PromptTemplateRef = mapString(values, "promptTemplateRef")
	result.PromptTemplateDigest = mapString(values, "promptTemplateDigest")
	result.PromptMaterializationDigest = mapString(values, "promptMaterializationDigest")
	result.SystemSttConfigurationRef = mapString(values, "systemSTTConfigurationRef")
	result.SystemSttConfigurationRevisionRef = mapString(values, "systemSTTConfigurationRevisionRef")
	result.SystemSttConfigurationVersion = mapInt64(values, "systemSTTConfigurationVersion")
	result.SystemSttConfigurationDigest = mapString(values, "systemSTTConfigurationDigest")
	result.RoleImageRecipeRef = mapString(values, "roleImageRecipeRef")
	result.RoleImageArtifactRef = mapString(values, "roleImageArtifactRef")
	result.RoleImageRecipeGeneration = mapInt64(values, "roleImageRecipeGeneration")
	result.ImageReference = mapString(values, "imageReference")
	result.ImageManifestDigest = mapString(values, "imageManifestDigest")
	result.RoleRuntimeContractRevision = uint64(mapInt64(values, "roleRuntimeContractRevision"))
	result.RoleRuntimeContractSha256 = mapString(values, "roleRuntimeContractSHA256")
	result.ProviderCredential = &controlplanev1.ProviderCredentialBinding{
		AccountRef:            mapString(values, "providerAccountRef"),
		CredentialRevisionRef: mapString(values, "providerCredentialRevisionRef"),
		CredentialRevision:    mapInt64(values, "providerCredentialRevisionNumber"),
		SecretName:            mapString(values, "providerSecretName"),
		SecretUid:             mapString(values, "providerSecretUID"),
		SecretResourceVersion: mapString(values, "providerSecretResourceVersion"),
		ContentSha256:         mapString(values, "providerCredentialSHA256"),
	}
	result.RuntimeConfigRef = mapString(values, "runtimeConfigRef")
	result.RuntimeConfigVersion = mapInt64(values, "runtimeConfigVersion")
	result.RuntimeConfigDigest = mapString(values, "runtimeConfigDigest")
	result.ProviderPolicyRef = mapString(values, "providerPolicyRef")
	result.ProviderPolicyVersion = mapInt64(values, "providerPolicyVersion")
	result.ProviderPolicyDigest = mapString(values, "providerPolicyDigest")
	result.ConfigOverlayRef = mapString(values, "configOverlayRef")
	result.ConfigOverlayVersion = mapInt64(values, "configOverlayVersion")
	result.ConfigOverlayDigest = mapString(values, "configOverlayDigest")
	result.ConfigOverlay = mapString(values, "configOverlay")
	result.RuntimeEnvironmentRef = mapString(values, "runtimeEnvironmentRef")
	result.RuntimeEnvironmentVersion = mapInt64(values, "runtimeEnvironmentVersion")
	result.RuntimeEnvironmentDigest = mapString(values, "runtimeEnvironmentDigest")
	result.EnvironmentBindingRef = mapString(values, "environmentBindingRef")
	result.EnvironmentBindingVersion = mapInt64(values, "environmentBindingVersion")
	result.EnvironmentBindingDigest = mapString(values, "environmentBindingDigest")
	result.AttachmentSetRef = mapString(values, "attachmentSetRef")
	result.AttachmentSetManifestDigest = mapString(values, "attachmentSetManifestDigest")
	result.AttachmentContext = mapString(values, "attachmentContext")
	if sets, ok := values["attachmentSets"].([]map[string]string); ok {
		for _, set := range sets {
			result.AttachmentSets = append(result.AttachmentSets, &controlplanev1.RuntimeAttachmentSet{
				Ref: set["ref"], ManifestDigest: set["manifestDigest"], Purpose: set["purpose"],
				Scope: set["scope"], Provenance: set["provenance"], TurnRef: set["turnRef"],
			})
		}
	}
	if environmentValues, ok := values["environmentValues"].([]runtimecontract.RuntimeEnvironmentValue); ok {
		for _, item := range environmentValues {
			result.EnvironmentValues = append(result.EnvironmentValues, &controlplanev1.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
		}
	}
	if secretProjections, ok := values["secretProjections"].([]runtimecontract.RuntimeSecretProjection); ok {
		for _, item := range secretProjections {
			result.SecretProjections = append(result.SecretProjections, &controlplanev1.RuntimeSecretDescriptor{
				Name: item.Name, SecretName: item.SecretName, SecretKey: item.SecretKey,
				SecretUid: item.SecretUID, SecretResourceVersion: item.SecretResourceVersion,
				ContentSha256: item.ContentSHA256,
			})
		}
	}
	if tools, ok := values["environmentTools"].([]runtimecontract.RuntimeEnvironmentTool); ok {
		for _, item := range tools {
			result.EnvironmentTools = append(result.EnvironmentTools, &controlplanev1.RuntimeEnvironmentTool{
				Name: item.Name, Command: item.Command, Description: item.Description, UsageHint: item.UsageHint,
			})
		}
	}
	if policy, ok := values["environmentPolicy"].(runtimecontract.RuntimeEnvironmentPolicy); ok {
		result.EnvironmentPolicy = castRuntimeEnvironmentPolicy(policy)
	}
	if access, ok := values["effectiveKubernetesAccess"].(runtimecontract.RuntimeKubernetesAccess); ok {
		result.EffectiveKubernetesAccess = &controlplanev1.RuntimeKubernetesAccess{
			Profile: &controlplanev1.RuntimeKubernetesAccessProfile{
				Kind: castRuntimeKubernetesAccessKind(access.Profile.Kind), Namespace: access.Profile.Namespace,
			}, ServiceAccountName: access.ServiceAccountName, Digest: access.Digest,
		}
		for _, rule := range access.Rules {
			result.EffectiveKubernetesAccess.Rules = append(result.EffectiveKubernetesAccess.Rules,
				&controlplanev1.RuntimeKubernetesRule{ApiGroup: rule.APIGroup, Resource: rule.Resource,
					Verbs: append([]string(nil), rule.Verbs...), ResourceNames: append([]string(nil), rule.ResourceNames...)})
		}
	}
	if policy, ok := values["workspacePolicy"].(entity.RuntimeWorkspacePolicy); ok {
		result.WorkspacePolicy = &controlplanev1.RuntimeWorkspacePolicy{
			Revision: policy.Revision, Root: policy.Root, MaximumWritableBytes: policy.MaximumWritableBytes,
			MaximumFileCount: policy.MaximumFileCount, Digest: policy.Digest,
		}
		for _, rule := range policy.Rules {
			result.WorkspacePolicy.Rules = append(result.WorkspacePolicy.Rules, &controlplanev1.RuntimeWorkspacePathRule{
				Path:   rule.Path,
				Access: controlplanev1.RuntimeWorkspaceAccess(controlplanev1.RuntimeWorkspaceAccess_value["RUNTIME_WORKSPACE_ACCESS_"+rule.Access]),
			})
		}
		for _, reason := range policy.DenialReasons {
			result.WorkspacePolicy.DenialReasons = append(result.WorkspacePolicy.DenialReasons,
				controlplanev1.RuntimeWorkspaceDenialReason(controlplanev1.RuntimeWorkspaceDenialReason_value["RUNTIME_WORKSPACE_DENIAL_REASON_"+reason]))
		}
	}
	profileRevision := mapString(values, "profileRevision")
	if profileRevision == "" {
		profileRevision = mapString(values, "runtimeRevision")
	}
	result.Runtime = &controlplanev1.RuntimeSelection{Ref: mapString(values, "runtimeKey"), Revision: profileRevision, Provider: mapString(values, "runtimeProvider"), Model: mapString(values, "runtimeModel"), Ready: true}
	for _, key := range mapStrings(values, "capabilities") {
		result.Capabilities = append(result.Capabilities, &controlplanev1.PlatformCapability{Key: key, Name: key})
	}
	if grants, ok := values["integrationGrants"].([]map[string]string); ok {
		for _, grant := range grants {
			result.IntegrationGrants = append(result.IntegrationGrants, &controlplanev1.IntegrationGrant{
				Ref: grant["ref"], ConnectionRef: grant["connectionRef"], DefinitionKey: grant["definitionKey"],
				DefinitionVersion: grant["definitionVersion"], DefinitionDigest: grant["definitionDigest"],
				ConnectionName: grant["connectionName"], CapabilityKey: grant["capabilityKey"],
				CapabilityName: grant["capabilityName"], CapabilityDescription: grant["capabilityDescription"],
				Operation: grant["operation"], InputSchema: grant["inputSchema"], InputSchemaSha256: grant["inputSchemaSha256"],
				Risk: grant["risk"], Enabled: true,
			})
		}
	}
	if artifacts, ok := values["artifacts"].([]map[string]any); ok {
		for _, artifact := range artifacts {
			item := &controlplanev1.Artifact{
				Ref:       mapString(artifact, "ref"),
				FileName:  mapString(artifact, "fileName"),
				MediaType: mapString(artifact, "mediaType"),
				SizeBytes: mapInt64(artifact, "sizeBytes"),
				Digest:    mapString(artifact, "digest"),
				Revision:  int32(mapInt64(artifact, "revision")),
				Version:   mapInt64(artifact, "version"),
				ScanState: controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_CLEAN,
				Source:    artifactSource(mapString(artifact, "source")),
			}
			result.Artifacts = append(result.Artifacts, item)
			result.InputArtifacts = append(result.InputArtifacts, &controlplanev1.RuntimeInputArtifact{
				Artifact: item, Scope: mapString(artifact, "scope"), Position: mapInt64(artifact, "position"),
				AttachmentSetRef:  mapString(artifact, "attachmentSetRef"),
				AttachmentPurpose: mapString(artifact, "attachmentPurpose"),
				Provenance:        mapString(artifact, "provenance"),
			})
		}
	}
	if targets, ok := values["delegationTargets"].([]map[string]string); ok {
		for _, target := range targets {
			result.DelegationTargets = append(result.DelegationTargets, &controlplanev1.DelegationTarget{Ref: target["ref"], Name: target["name"], Purpose: target["purpose"], RoleDescription: target["roleDescription"], WorkflowStepKey: target["workflowStepKey"], WorkflowStepName: target["workflowStepName"], Instructions: target["instructions"], ExpectedResult: target["expectedResult"]})
		}
	}
	if messages, ok := values["sessionContext"].([]map[string]string); ok {
		for _, message := range messages {
			result.SessionContext = append(result.SessionContext, &controlplanev1.SessionContextMessage{Role: message["role"], Content: message["content"]})
		}
	}
	result.CallbackEdgeRef = mapString(values, "callbackEdgeRef")
	if boundedInput, ok := values["input"].(map[string]any); ok {
		result.BoundedInput = structure(boundedInput)
	}
	if context, ok := values["assistantContext"].(map[string]any); ok {
		result.AssistantContext = &controlplanev1.AssistantContextDescriptor{Route: mapString(context, "route"),
			EntityKind: mapString(context, "entityKind"), EntityRef: mapString(context, "entityRef"),
			EntityName: mapString(context, "entityName"), AllowedOperations: []controlplanev1.AssistantPlanOperation_Type{}}
		if version := mapInt64(context, "entityVersion"); version > 0 {
			result.AssistantContext.EntityVersion = &version
		}
		for _, operation := range mapStrings(context, "allowedOperations") {
			result.AssistantContext.AllowedOperations = append(result.AssistantContext.AllowedOperations,
				controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+operation]))
		}
	}
	return result
}

func castClaim(values map[string]any) *controlplanev1.ClaimedExecution {
	run := &controlplanev1.Run{Ref: mapString(values, "runRef"), ProjectRef: mapString(values, "projectRef"), SessionRef: mapString(values, "sessionRef"), State: controlplanev1.RunState_RUN_STATE_RUNNING}
	node := &controlplanev1.RunNode{Ref: mapString(values, "nodeRef"), RunRef: mapString(values, "runRef"), AgentRef: mapString(values, "agentRef"), TurnRef: mapString(values, "turnRef"), Type: controlplanev1.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION, State: controlplanev1.RunNodeState_RUN_NODE_STATE_RUNNING}
	return &controlplanev1.ClaimedExecution{Run: run, Node: node, Revision: castRuntimeRevision(values), Lease: castLease(values), Task: mapString(values, "task")}
}

func assistantRuntimeState(input controlplanev1.AssistantRuntimeState) string {
	switch input {
	case controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_PROVISIONING:
		return "STARTING"
	case controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY:
		return "READY"
	case controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_BUSY:
		return "BUSY"
	case controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_RECOVERING:
		return "RECOVERING"
	case controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_FAILED:
		return "UNAVAILABLE"
	default:
		return ""
	}
}

func (server *Server) ClaimExecution(ctx context.Context, request *controlplanev1.ClaimExecutionRequest) (*controlplanev1.ClaimExecutionResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ClaimExecution_FullMethodName, command.ClaimExecution, nil, command.LeaseInput{WorkloadInstance: request.GetWorkloadInstance(), Limit: request.GetLimit()})
	if err != nil {
		return nil, err
	}
	response := &controlplanev1.ClaimExecutionResponse{}
	for _, item := range result.RuntimeItems {
		response.Executions = append(response.Executions, castClaim(item))
	}
	return response, nil
}

func (server *Server) ReadExecutionArtifact(ctx context.Context, request *controlplanev1.ReadExecutionArtifactRequest) (*controlplanev1.ReadExecutionArtifactResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ReadExecutionArtifact_FullMethodName)
	if err != nil {
		return nil, err
	}
	download, err := server.service.ReadExecutionArtifact(ctx, p, request.GetLeaseRef(), request.GetFence(), request.GetGeneration(), request.GetArtifactRef())
	if err != nil {
		return nil, transportError(err)
	}
	defer download.Reader.Close()
	content, err := io.ReadAll(io.LimitReader(download.Reader, maximumExecutionArtifactBytes+1))
	if err != nil || int64(len(content)) > maximumExecutionArtifactBytes || int64(len(content)) != download.Artifact.SizeBytes {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &controlplanev1.ReadExecutionArtifactResponse{Artifact: castArtifact(download.Artifact), Content: content}, nil
}

func (server *Server) RenewExecution(ctx context.Context, request *controlplanev1.RenewExecutionRequest) (*controlplanev1.RenewExecutionResponse, error) {
	payload := command.LeaseInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_RenewExecution_FullMethodName, command.RenewExecution, nil, payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RenewExecutionResponse{Lease: castLease(result.Runtime)}, nil
}

func (server *Server) ReportExecutionProgress(ctx context.Context, request *controlplanev1.ReportExecutionProgressRequest) (*controlplanev1.ReportExecutionProgressResponse, error) {
	payload := command.LeaseInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Progress: request.GetProgress()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ReportExecutionProgress_FullMethodName, command.ReportExecutionProgress, nil, payload)
	if err != nil {
		return nil, err
	}
	response := &controlplanev1.ReportExecutionProgressResponse{Run: castRun(*result.Run), Event: castEvent(*result.Event)}
	for _, node := range result.Graph.Nodes {
		if node.Ref == result.Event.NodeRef {
			response.Node = castNode(node)
			break
		}
	}
	return response, nil
}

func (server *Server) CommitProviderCredentialRefresh(ctx context.Context, request *controlplanev1.CommitProviderCredentialRefreshRequest) (*controlplanev1.CommitProviderCredentialRefreshResponse, error) {
	payload := providerCredentialRefreshInput(request)
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_CommitProviderCredentialRefresh_FullMethodName,
		command.CommitProviderCredentialRefresh, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CommitProviderCredentialRefreshResponse{ProviderCredential: castProviderCredential(result.Runtime)}, nil
}

func providerCredentialRefreshInput(request *controlplanev1.CommitProviderCredentialRefreshRequest) command.ProviderCredentialRefreshInput {
	return command.ProviderCredentialRefreshInput{
		LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
		PreviousCredentialRevisionRef: request.GetPreviousCredentialRevisionRef(),
		PreviousContentSHA256:         request.GetPreviousContentSha256(), SecretName: request.GetSecretName(),
		SecretUID: request.GetSecretUid(), SecretResourceVersion: request.GetSecretResourceVersion(),
		ContentSHA256: request.GetContentSha256(),
	}
}

func castProviderCredential(values map[string]any) *controlplanev1.ProviderCredentialBinding {
	return &controlplanev1.ProviderCredentialBinding{
		AccountRef:            mapString(values, "providerAccountRef"),
		CredentialRevisionRef: mapString(values, "providerCredentialRevisionRef"),
		CredentialRevision:    mapInt64(values, "providerCredentialRevisionNumber"),
		SecretName:            mapString(values, "providerSecretName"), SecretUid: mapString(values, "providerSecretUID"),
		SecretResourceVersion: mapString(values, "providerSecretResourceVersion"),
		ContentSha256:         mapString(values, "providerCredentialSHA256"),
	}
}

func (server *Server) CompleteExecution(ctx context.Context, request *controlplanev1.CompleteExecutionRequest) (*controlplanev1.CompleteExecutionResponse, error) {
	payload := command.CompleteExecutionInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Success: request.GetSuccess(), ResultSummary: request.GetResultSummary(), SafeErrorCode: request.GetSafeErrorCode(), Usage: usageFromProto(request.GetUsage()), CodexSessionID: request.GetCodexSessionId(), ArchiveRelativePath: request.GetCodexArchiveRelativePath(), ArchiveSHA256: request.GetCodexArchiveSha256(), ArchiveSizeBytes: request.GetCodexArchiveSizeBytes()}
	for _, item := range request.GetArtifacts() {
		payload.Artifacts = append(payload.Artifacts, command.CompletedArtifact{FileName: item.GetFileName(), MediaType: item.GetMediaType(), SizeBytes: item.GetSizeBytes(), Content: item.GetContent(), SHA256: item.GetSha256()})
	}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_CompleteExecution_FullMethodName, command.CompleteExecution, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteExecutionResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}

func (server *Server) DelegateExecution(ctx context.Context, request *controlplanev1.DelegateExecutionRequest) (*controlplanev1.DelegateExecutionResponse, error) {
	payload := command.DelegateInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), TargetAgentRef: request.GetTargetAgentRef(), WorkflowStepKey: request.GetWorkflowStepKey(), Task: request.GetTask(), Input: asMap(request.GetInput())}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_DelegateExecution_FullMethodName, command.DelegateExecution, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DelegateExecutionResponse{ChildRun: castRun(*result.Run), RootGraph: castGraph(*result.Graph), CallbackEdgeRef: mapString(result.Runtime, "callbackEdgeRef")}, nil
}

func (server *Server) ProposeAssistantPlan(ctx context.Context, request *controlplanev1.ProposeAssistantPlanRequest) (*controlplanev1.ProposeAssistantPlanResponse, error) {
	operations := make([]entity.AssistantPlanOperation, 0, len(request.GetOperations()))
	for _, item := range request.GetOperations() {
		if item == nil || item.GetType() == controlplanev1.AssistantPlanOperation_TYPE_UNSPECIFIED {
			return nil, transportError(errs.ErrInvalid)
		}
		operations = append(operations, assistantOperation(item))
	}
	payload := command.ProposeAssistantPlanInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Summary: request.GetSummary(), Operations: operations}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ProposeAssistantPlan_FullMethodName, command.ProposeAssistantPlan, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ProposeAssistantPlanResponse{Plan: castPlan(result.Plan), Conversation: castConversation(*result.Conversation)}, nil
}

func (server *Server) ProposeAssistantMetadata(ctx context.Context, request *controlplanev1.ProposeAssistantMetadataRequest) (*controlplanev1.ProposeAssistantMetadataResponse, error) {
	payload := command.ProposeAssistantMetadataInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Title: request.GetTitle()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ProposeAssistantMetadata_FullMethodName, command.ProposeAssistantMetadata, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ProposeAssistantMetadataResponse{Conversation: castConversation(*result.Conversation)}, nil
}

func (server *Server) ProposeRunMetadata(ctx context.Context, request *controlplanev1.ProposeRunMetadataRequest) (*controlplanev1.ProposeRunMetadataResponse, error) {
	payload := command.ProposeRunMetadataInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Title: request.GetTitle(), ActivitySummary: request.GetActivitySummary()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ProposeRunMetadata_FullMethodName, command.ProposeRunMetadata, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ProposeRunMetadataResponse{Run: castRun(*result.Run), Event: castEvent(*result.Event)}, nil
}

func (server *Server) RecordRunToolCall(ctx context.Context, request *controlplanev1.RecordRunToolCallRequest) (*controlplanev1.RecordRunToolCallResponse, error) {
	payload := command.RunToolCallInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
		CallRef: request.GetCallRef(), Tool: request.GetTool(), SafeParameters: asMap(request.GetSafeParameters()),
		CapabilityRef: request.GetCapabilityRef(), GrantRef: request.GetGrantRef(), State: enumSuffix(request.GetState(), "RUN_TOOL_CALL_STATE_"),
		DurationMS: request.GetDurationMs(), SafeResult: request.GetSafeResult()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_RecordRunToolCall_FullMethodName, command.RecordRunToolCall, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RecordRunToolCallResponse{Event: castEvent(*result.Event)}, nil
}

func assistantOperations(items []*controlplanev1.AssistantPlanOperation) []entity.AssistantPlanOperation {
	result := make([]entity.AssistantPlanOperation, 0, len(items))
	for _, item := range items {
		if item != nil {
			result = append(result, assistantOperation(item))
		}
	}
	return result
}

func assistantOperation(item *controlplanev1.AssistantPlanOperation) entity.AssistantPlanOperation {
	parameters := asMap(item.GetParameters())
	if item.GetParameters() == nil {
		parameters = asMap(item.GetBoundedInput())
	}
	return entity.AssistantPlanOperation{
		Key: item.GetRef(), Type: enumSuffix(item.GetType(), "TYPE_"), Action: enumSuffix(item.GetAction(), "ACTION_"),
		Title: item.GetTitle(), Summary: item.GetSummary(), Parameters: parameters, Before: asMap(item.GetBefore()), After: asMap(item.GetAfter()),
		Target:          entity.AssistantPlanTarget{Kind: item.GetTargetKind(), Ref: item.GetTargetRef(), Name: item.GetTargetName(), Version: item.ExpectedVersion},
		ExpectedVersion: item.ExpectedVersion, Selected: item.GetSelected(),
	}
}

func (server *Server) ReconcileWarmRuntime(ctx context.Context, request *controlplanev1.ReconcileWarmRuntimeRequest) (*controlplanev1.ReconcileWarmRuntimeResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ReconcileWarmRuntime_FullMethodName)
	if err != nil {
		return nil, err
	}
	assistant, desired, required, err := server.service.ReconcileWarmRuntime(ctx, p, request.GetWorkloadInstance())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ReconcileWarmRuntimeResponse{Assistant: castAssistant(assistant), DesiredRevision: castRuntimeRevision(desired), MaterializationRequired: required}, nil
}

func (server *Server) ReportWarmRuntime(ctx context.Context, request *controlplanev1.ReportWarmRuntimeRequest) (*controlplanev1.ReportWarmRuntimeResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ReportWarmRuntime_FullMethodName)
	if err != nil {
		return nil, err
	}
	payload := command.WarmRuntimeInput{WorkloadInstance: request.GetWorkloadInstance(), RuntimeRevision: request.GetRuntimeRevision(), State: assistantRuntimeState(request.GetState()), SafeErrorCode: request.GetSafeErrorCode()}
	assistant, err := server.service.ReportWarmRuntime(ctx, p, payload)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ReportWarmRuntimeResponse{Assistant: castAssistant(assistant)}, nil
}

func (server *Server) ClaimDueSchedules(ctx context.Context, request *controlplanev1.ClaimDueSchedulesRequest) (*controlplanev1.ClaimDueSchedulesResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ClaimDueSchedules_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimDueSchedules(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ClaimDueSchedulesResponse{}
	for _, item := range items {
		response.Claims = append(response.Claims, &controlplanev1.ScheduleClaim{
			Schedule:      &controlplanev1.Schedule{Ref: mapString(item, "scheduleRef"), Version: mapInt64(item, "scheduleVersion")},
			OccurrenceRef: mapString(item, "occurrenceRef"), Lease: castLease(item), ScheduledFor: mapTime(item, "scheduledFor"),
			InputDigest: mapString(item, "inputDigest"), ScheduleRevisionRef: mapString(item, "scheduleRevisionRef"),
			ScheduleRevision: mapInt64(item, "scheduleRevision"), ScheduleRevisionDigest: mapString(item, "scheduleRevisionDigest"),
			Attempt: int32(mapInt64(item, "attempt")), TargetRef: mapString(item, "targetRef"),
			TargetVersion: mapInt64(item, "targetVersion"), TargetDigest: mapString(item, "targetDigest"),
			AutomationTextDigest: mapString(item, "automationTextDigest"), PromptInputsDigest: mapString(item, "promptInputsDigest"),
		})
	}
	return response, nil
}

func (server *Server) MaterializeScheduleOccurrence(ctx context.Context, request *controlplanev1.MaterializeScheduleOccurrenceRequest) (*controlplanev1.MaterializeScheduleOccurrenceResponse, error) {
	payload := command.OccurrenceInput{OccurrenceRef: request.GetOccurrenceRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_MaterializeScheduleOccurrence_FullMethodName, command.MaterializeOccurrence, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.MaterializeScheduleOccurrenceResponse{Run: castRun(*result.Run), Schedule: castSchedule(*result.Schedule)}, nil
}

func (server *Server) RenewScheduleOccurrence(ctx context.Context, request *controlplanev1.RenewScheduleOccurrenceRequest) (*controlplanev1.RenewScheduleOccurrenceResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_RenewScheduleOccurrence_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.RenewScheduleOccurrence(ctx, p, command.OccurrenceInput{
		OccurrenceRef: request.GetOccurrenceRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RenewScheduleOccurrenceResponse{Lease: castLease(result)}, nil
}

func (server *Server) FailScheduleOccurrence(ctx context.Context, request *controlplanev1.FailScheduleOccurrenceRequest) (*controlplanev1.FailScheduleOccurrenceResponse, error) {
	payload := command.OccurrenceInput{
		OccurrenceRef: request.GetOccurrenceRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(),
		Generation: request.GetGeneration(), SafeErrorCode: request.GetSafeErrorCode(), Retryable: request.GetRetryable(),
	}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_FailScheduleOccurrence_FullMethodName,
		command.FailScheduleOccurrence, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.FailScheduleOccurrenceResponse{State: mapString(result.Runtime, "state"), Attempt: int32(mapInt64(result.Runtime, "attempt"))}, nil
}

func (server *Server) ClaimIntegrationConnectionTests(ctx context.Context, request *controlplanev1.ClaimIntegrationConnectionTestsRequest) (*controlplanev1.ClaimIntegrationConnectionTestsResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ClaimIntegrationConnectionTests_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimIntegrationConnectionTests(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ClaimIntegrationConnectionTestsResponse{}
	for _, item := range items {
		configuration, _ := item["configuration"].(map[string]any)
		claim := &controlplanev1.IntegrationConnectionTestClaim{
			TestRef: mapString(item, "testRef"), ConnectionRef: mapString(item, "connectionRef"),
			DefinitionKey: mapString(item, "definitionKey"), PublicConfiguration: structure(configuration), Lease: castLease(item),
			DefinitionVersion: mapString(item, "definitionVersion"), DefinitionDigest: mapString(item, "definitionDigest"),
		}
		if credential, ok := item["credential"].(entity.IntegrationCredentialRevision); ok {
			claim.CredentialRevision = castIntegrationCredential(credential)
		}
		response.Claims = append(response.Claims, claim)
	}
	return response, nil
}

func (server *Server) CompleteIntegrationConnectionTest(ctx context.Context, request *controlplanev1.CompleteIntegrationConnectionTestRequest) (*controlplanev1.CompleteIntegrationConnectionTestResponse, error) {
	payload := command.IntegrationConnectionTestInput{TestRef: request.GetTestRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Success: request.GetSuccess(), ResultSummary: request.GetResultSummary(), SafeErrorCode: request.GetSafeErrorCode()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_CompleteIntegrationConnectionTest_FullMethodName, command.CompleteConnectionTest, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteIntegrationConnectionTestResponse{Connection: castConnection(*result.Connection)}, nil
}

func (server *Server) ResolveIntegrationInvocation(ctx context.Context, request *controlplanev1.ResolveIntegrationInvocationRequest) (*controlplanev1.ResolveIntegrationInvocationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ResolveIntegrationInvocation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ResolveIntegrationInvocation(ctx, p, map[string]string{"run_ref": request.GetRunRef(), "node_ref": request.GetNodeRef(), "connection_ref": request.GetConnectionRef(), "capability_key": request.GetCapabilityKey(), "idempotency_key": request.GetIdempotencyKey()}, asMap(request.GetBoundedInput()))
	if err != nil {
		return nil, transportError(err)
	}
	resourceScope, _ := result["resourceScope"].(map[string]string)
	return &controlplanev1.ResolveIntegrationInvocationResponse{
		InvocationRef: mapString(result, "invocationRef"), GrantRef: mapString(result, "grantRef"),
		Operation: mapString(result, "operation"), State: mapString(result, "state"), GateRef: mapString(result, "gateRef"),
		Risk: integrationRisk(mapString(result, "risk")), ResourceScope: &controlplanev1.IntegrationResourceScope{
			Kind: integrationResourceKind(mapString(result, "resourceKind")), Values: resourceScope,
			Digest: mapString(result, "resourceScopeDigest"),
		},
	}, nil
}

func (server *Server) ClaimIntegrationInvocations(ctx context.Context, request *controlplanev1.ClaimIntegrationInvocationsRequest) (*controlplanev1.ClaimIntegrationInvocationsResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_ClaimIntegrationInvocations_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimIntegrationInvocations(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ClaimIntegrationInvocationsResponse{}
	for _, item := range items {
		configuration, _ := item["configuration"].(map[string]any)
		boundedInput, _ := item["boundedInput"].(map[string]any)
		resourceScope, _ := item["resourceScope"].(map[string]string)
		claim := &controlplanev1.IntegrationInvocationClaim{
			InvocationRef: mapString(item, "invocationRef"), DefinitionKey: mapString(item, "definitionKey"),
			ConnectionRef: mapString(item, "connectionRef"), CapabilityKey: mapString(item, "capabilityKey"),
			PublicConfiguration: structure(configuration), BoundedInput: structure(boundedInput), Lease: castLease(item),
			DefinitionVersion: mapString(item, "definitionVersion"), DefinitionDigest: mapString(item, "definitionDigest"),
			Operation: mapString(item, "operation"), Risk: integrationRisk(mapString(item, "risk")),
			ApprovalPolicy: integrationApprovalPolicy(mapString(item, "approvalPolicy")),
			ResourceScope: &controlplanev1.IntegrationResourceScope{
				Kind: integrationResourceKind(mapString(item, "resourceKind")), Values: resourceScope,
				Digest: mapString(item, "resourceScopeDigest"),
			},
			EffectKey: mapString(item, "effectKey"), InputDigest: mapString(item, "inputDigest"),
		}
		if credential, ok := item["credential"].(entity.IntegrationCredentialRevision); ok {
			claim.CredentialRevision = castIntegrationCredential(credential)
		}
		response.Claims = append(response.Claims, claim)
	}
	return response, nil
}

func (server *Server) GetIntegrationInvocation(ctx context.Context, request *controlplanev1.GetIntegrationInvocationRequest) (*controlplanev1.GetIntegrationInvocationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_GetIntegrationInvocation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetIntegrationInvocation(ctx, p, request.GetInvocationRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetIntegrationInvocationResponse{
		State: mapString(result, "state"), ResultSummary: mapString(result, "resultSummary"),
		SafeErrorCode: mapString(result, "safeErrorCode"), GateRef: mapString(result, "gateRef"),
		EffectReceiptRef: mapString(result, "effectReceiptRef"),
	}, nil
}

func (server *Server) CompleteIntegrationInvocation(ctx context.Context, request *controlplanev1.CompleteIntegrationInvocationRequest) (*controlplanev1.CompleteIntegrationInvocationResponse, error) {
	payload := command.IntegrationInvocationInput{InvocationRef: request.GetInvocationRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Success: request.GetSuccess(), UnknownOutcome: request.GetUnknownOutcome(), ResultSummary: request.GetResultSummary(), SafeErrorCode: request.GetSafeErrorCode()}
	if receipt := request.GetEffectReceipt(); receipt != nil {
		payload.ReceiptRef = receipt.GetRef()
		payload.EffectKey = receipt.GetEffectKey()
		payload.InputDigest = receipt.GetInputDigest()
		payload.ProviderEffectRef = receipt.GetProviderEffectRef()
		payload.ResponseDigest = receipt.GetResponseDigest()
	}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_CompleteIntegrationInvocation_FullMethodName, command.CompleteIntegrationInvocation, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteIntegrationInvocationResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}
