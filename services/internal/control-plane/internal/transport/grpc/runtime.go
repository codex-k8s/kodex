package grpc

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
	result := &controlplanev1.RuntimeRevisionSnapshot{Ref: mapString(values, "runtimeRevision"), Version: 1, RunRef: mapString(values, "runRef"), NodeRef: mapString(values, "nodeRef"), SessionRef: mapString(values, "sessionRef"), TurnRef: mapString(values, "turnRef"), Attempt: int32(mapInt64(values, "attempt")), AgentRef: agentRef, Instructions: instructions, InputDigest: mapString(values, "inputDigest"), RevisionDigest: mapString(values, "revisionDigest"), SystemAssistant: mapString(values, "stableKey") == "system-assistant"}
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
			result.IntegrationGrants = append(result.IntegrationGrants, &controlplanev1.IntegrationGrant{Ref: grant["ref"], ConnectionRef: grant["connectionRef"], DefinitionKey: grant["definitionKey"], ConnectionName: grant["connectionName"], CapabilityKey: grant["capabilityKey"], CapabilityName: grant["capabilityName"], CapabilityDescription: grant["capabilityDescription"], Risk: grant["risk"], Enabled: true})
		}
	}
	if targets, ok := values["delegationTargets"].([]map[string]string); ok {
		for _, target := range targets {
			result.DelegationTargets = append(result.DelegationTargets, &controlplanev1.DelegationTarget{Ref: target["ref"], Name: target["name"], Purpose: target["purpose"], RoleDescription: target["roleDescription"]})
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

func (server *Server) CompleteExecution(ctx context.Context, request *controlplanev1.CompleteExecutionRequest) (*controlplanev1.CompleteExecutionResponse, error) {
	payload := command.CompleteExecutionInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Success: request.GetSuccess(), ResultSummary: request.GetResultSummary(), SafeErrorCode: request.GetSafeErrorCode()}
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
	payload := command.DelegateInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), TargetAgentRef: request.GetTargetAgentRef(), Task: request.GetTask(), Input: asMap(request.GetInput())}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_DelegateExecution_FullMethodName, command.DelegateExecution, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DelegateExecutionResponse{ChildRun: castRun(*result.Run), RootGraph: castGraph(*result.Graph), CallbackEdgeRef: mapString(result.Runtime, "callbackEdgeRef")}, nil
}

func (server *Server) DeliverCallback(ctx context.Context, request *controlplanev1.DeliverCallbackRequest) (*controlplanev1.DeliverCallbackResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_DeliverCallback_FullMethodName, command.DeliverCallback, request.GetMutation(), command.CallbackInput{ChildRunRef: request.GetChildRunRef(), CallbackEdgeRef: request.GetCallbackEdgeRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeliverCallbackResponse{ParentRun: castRun(*result.Run), RootGraph: castGraph(*result.Graph), Duplicate: result.Duplicate}, nil
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
	payload := command.WarmRuntimeInput{WorkloadInstance: request.GetWorkloadInstance(), RuntimeRevision: request.GetRuntimeRevision(), State: assistantRuntimeState(request.GetState()), SafeErrorCode: request.GetSafeErrorCode()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_ReportWarmRuntime_FullMethodName, command.ReportWarmRuntime, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReportWarmRuntimeResponse{Assistant: castAssistant(*result.Assistant)}, nil
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
		response.Claims = append(response.Claims, &controlplanev1.ScheduleClaim{Schedule: &controlplanev1.Schedule{Ref: mapString(item, "scheduleRef"), Version: mapInt64(item, "scheduleVersion")}, OccurrenceRef: mapString(item, "occurrenceRef"), Lease: castLease(item), ScheduledFor: mapTime(item, "scheduledFor")})
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

func (server *Server) CompleteScheduleOccurrence(ctx context.Context, request *controlplanev1.CompleteScheduleOccurrenceRequest) (*controlplanev1.CompleteScheduleOccurrenceResponse, error) {
	payload := command.OccurrenceInput{OccurrenceRef: request.GetOccurrenceRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Outcome: request.GetOutcome()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_CompleteScheduleOccurrence_FullMethodName, command.CompleteOccurrence, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteScheduleOccurrenceResponse{Schedule: castSchedule(*result.Schedule)}, nil
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
		response.Claims = append(response.Claims, &controlplanev1.IntegrationConnectionTestClaim{TestRef: mapString(item, "testRef"), ConnectionRef: mapString(item, "connectionRef"), DefinitionKey: mapString(item, "definitionKey"), CredentialMaterializationRef: mapString(item, "credentialRef"), PublicConfiguration: structure(configuration), Lease: castLease(item)})
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
	return &controlplanev1.ResolveIntegrationInvocationResponse{InvocationRef: mapString(result, "invocationRef"), GrantRef: mapString(result, "grantRef"), Operation: mapString(result, "operation")}, nil
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
		response.Claims = append(response.Claims, &controlplanev1.IntegrationInvocationClaim{InvocationRef: mapString(item, "invocationRef"), DefinitionKey: mapString(item, "definitionKey"), ConnectionRef: mapString(item, "connectionRef"), CredentialMaterializationRef: mapString(item, "credentialRef"), CapabilityKey: mapString(item, "capabilityKey"), PublicConfiguration: structure(configuration), BoundedInput: structure(boundedInput), Lease: castLease(item)})
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
	return &controlplanev1.GetIntegrationInvocationResponse{State: mapString(result, "state"), ResultSummary: mapString(result, "resultSummary"), SafeErrorCode: mapString(result, "safeErrorCode")}, nil
}

func (server *Server) CompleteIntegrationInvocation(ctx context.Context, request *controlplanev1.CompleteIntegrationInvocationRequest) (*controlplanev1.CompleteIntegrationInvocationResponse, error) {
	payload := command.IntegrationInvocationInput{InvocationRef: request.GetInvocationRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Success: request.GetSuccess(), ResultSummary: request.GetResultSummary(), SafeErrorCode: request.GetSafeErrorCode()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_CompleteIntegrationInvocation_FullMethodName, command.CompleteIntegrationInvocation, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteIntegrationInvocationResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}
