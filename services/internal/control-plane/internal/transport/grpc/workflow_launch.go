package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

func (server *Server) LaunchWorkflowExecution(ctx context.Context, request *controlplanev1.LaunchWorkflowExecutionRequest) (*controlplanev1.LaunchWorkflowExecutionResponse, error) {
	payload := command.LaunchWorkflowInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), WorkflowRef: request.GetWorkflowRef(), Task: request.GetTask(), Title: request.GetTitle(), Input: asMap(request.GetInput())}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_LaunchWorkflowExecution_FullMethodName, command.LaunchWorkflowExecution, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	launchRef, _ := result.Runtime["launchRef"].(string)
	callbackRef, _ := result.Runtime["callbackEdgeRef"].(string)
	return &controlplanev1.LaunchWorkflowExecutionResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph), LaunchRef: launchRef, CallbackEdgeRef: callbackRef}, nil
}
