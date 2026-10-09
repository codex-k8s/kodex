package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) LaunchWorkflowExecution(ctx context.Context, request *controlplanev1.LaunchWorkflowExecutionRequest) (*controlplanev1.LaunchWorkflowExecutionResponse, error) {
	payload := command.LaunchWorkflowInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), WorkflowRef: request.GetWorkflowRef(), Task: request.GetTask(), Title: request.GetTitle(), Input: asMap(request.GetInput()), ExpectedPublishedRef: request.GetExpectedPublishedRef(), ExpectedSpecDigest: request.GetExpectedSpecDigest(), ExpectedWorkflowVersion: request.GetExpectedWorkflowVersion()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_LaunchWorkflowExecution_FullMethodName, command.LaunchWorkflowExecution, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	launchRef, _ := result.Runtime["launchRef"].(string)
	callbackRef, _ := result.Runtime["callbackEdgeRef"].(string)
	return &controlplanev1.LaunchWorkflowExecutionResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph), LaunchRef: launchRef, CallbackEdgeRef: callbackRef}, nil
}

func (server *Server) GetExecutionWorkflowCatalog(ctx context.Context, request *controlplanev1.GetExecutionWorkflowCatalogRequest) (*controlplanev1.GetExecutionWorkflowCatalogResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_GetExecutionWorkflowCatalog_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetExecutionWorkflowCatalog(ctx, p, query.ExecutionWorkflowCatalog{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Query: request.GetQuery(), PageToken: request.GetPageToken()})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetExecutionWorkflowCatalogResponse{NextPageToken: result.NextPageToken}
	for _, item := range result.Items {
		entry := &controlplanev1.ExecutionWorkflowCatalogEntry{WorkflowRef: item.WorkflowRef, Name: item.Name, Purpose: item.Purpose, WorkflowVersion: item.WorkflowVersion, PublishedRef: item.PublishedRef, SpecDigest: item.SpecDigest, Readiness: &controlplanev1.WorkflowLaunchReadiness{AllowedToSubmit: item.Readiness.AllowedToSubmit, Reason: item.Readiness.Reason, WorkflowVersion: item.Readiness.WorkflowVersion, RevisionRef: item.Readiness.RevisionRef, ContextDigest: item.Readiness.ContextDigest, OperationalState: item.Readiness.OperationalState}}
		for _, field := range item.Inputs {
			entry.InputFields = append(entry.InputFields, &controlplanev1.WorkflowInputField{Key: field.Key, Label: field.Label, Description: field.Help, ValueType: field.Type, Required: field.Required, Options: field.Options})
		}
		response.Items = append(response.Items, entry)
	}
	return response, nil
}
