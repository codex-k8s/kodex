package callback

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/structpb"
)

func workflowLaunchAvailable(input runtimecontract.RunnerInput) bool {
	return runtimecontract.RuntimeWorkflowLaunchAvailable(input)
}

func workflowLaunchTool() map[string]any {
	return map[string]any{"name": "launch_workflow", "description": "Launch one existing published project Workflow using its exact reference from the task. The server creates a required child workflow and revalidates current authority. End this turn after acceptance; the final Workflow result arrives in a fresh callback turn. Do not repeat an accepted launch.", "inputSchema": objectSchema([]string{"workflow_ref", "task"}, map[string]any{"workflow_ref": opaqueRefSchema(), "task": map[string]any{"type": "string", "minLength": 1, "maxLength": runtimecontract.MaximumAssistantTurnCodepoints}, "title": map[string]any{"type": "string", "maxLength": 240}, "input": map[string]any{"type": "object"}})}
}

func (server *Server) launchWorkflow(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	if !workflowLaunchAvailable(input) || !onlyKeys(arguments, "workflow_ref", "task", "title", "input") {
		return nil, errors.New("workflow launch input is invalid")
	}
	workflow, ok := arguments["workflow_ref"].(string)
	if !ok || !runtimeFileRefPattern.MatchString(workflow) {
		return nil, errors.New("workflow launch input is invalid")
	}
	task, ok := arguments["task"].(string)
	if !ok || !runtimecontract.ValidAssistantTurnContent(task) {
		return nil, errors.New("workflow launch input is invalid")
	}
	title := ""
	if value, exists := arguments["title"]; exists {
		var ok bool
		title, ok = value.(string)
		if !ok || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 240 {
			return nil, errors.New("workflow launch input is invalid")
		}
	}
	values := map[string]any{}
	if value, exists := arguments["input"]; exists {
		var ok bool
		values, ok = value.(map[string]any)
		if !ok {
			return nil, errors.New("workflow launch input is invalid")
		}
	}
	structure, err := structpb.NewStruct(values)
	if err != nil {
		return nil, errors.New("workflow launch input is invalid")
	}
	requestCtx, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.LaunchWorkflowExecution(requestCtx, &controlplanev1.LaunchWorkflowExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, WorkflowRef: workflow, Task: task, Input: structure, Title: title})
	if err != nil {
		return nil, err
	}
	if response.GetRun().GetRef() == "" || response.GetLaunchRef() == "" || response.GetCallbackEdgeRef() == "" {
		return nil, errors.New("workflow launch response is invalid")
	}
	return map[string]any{"ok": true, "workflow_run_ref": response.GetRun().GetRef(), "launch_ref": response.GetLaunchRef(), "callback_edge_ref": response.GetCallbackEdgeRef()}, nil
}
