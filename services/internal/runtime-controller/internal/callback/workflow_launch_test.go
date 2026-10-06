package callback

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

type workflowLaunchClient struct {
	controlplanev1.RuntimeWorkServiceClient
	request *controlplanev1.LaunchWorkflowExecutionRequest
	calls   int
}

func (client *workflowLaunchClient) LaunchWorkflowExecution(_ context.Context, input *controlplanev1.LaunchWorkflowExecutionRequest, _ ...grpc.CallOption) (*controlplanev1.LaunchWorkflowExecutionResponse, error) {
	client.request = input
	client.calls++
	return &controlplanev1.LaunchWorkflowExecutionResponse{Run: &controlplanev1.Run{Ref: "run_workflow001"}, LaunchRef: "wlaunch_workflow01", CallbackEdgeRef: "edg_callback001"}, nil
}

func TestWorkflowLaunchNativeCatalogAndClosedAuthority(t *testing.T) {
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_project001", Capabilities: []string{"platform.run.launch"}, LeaseRef: "lse_origin001", LeaseFence: "private-test-fence", LeaseGeneration: 7}
	hasTool := func(input runtimecontract.RunnerInput) bool {
		for _, tool := range tools(input) {
			if tool["name"] == "launch_workflow" {
				return true
			}
		}
		return false
	}
	if !hasTool(input) {
		t.Fatal("native launch consumer is absent")
	}
	for _, variant := range []string{"capability", "assistant", "project", "mode"} {
		t.Run(variant, func(t *testing.T) {
			copy := input
			switch variant {
			case "capability":
				copy.Capabilities = nil
			case "assistant":
				copy.AssistantScope = runtimecontract.AssistantScopeProject
			case "project":
				copy.ProjectRef = ""
			case "mode":
				copy.Mode = ""
			}
			if hasTool(copy) {
				t.Fatal("ineligible launch tool was exposed")
			}
		})
	}
	client := &workflowLaunchClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	args := map[string]any{"workflow_ref": "wf_workflow001", "task": strings.Repeat("🙂", runtimecontract.MaximumAssistantTurnCodepoints), "input": map[string]any{"record": "bounded"}}
	response, err := server.launchWorkflow(t.Context(), input, args, json.RawMessage(`"call-one"`))
	if err != nil || response == nil || client.calls != 1 {
		t.Fatalf("launch: %v", err)
	}
	if client.request.LeaseRef != input.LeaseRef || client.request.Fence != input.LeaseFence || client.request.Generation != 7 || client.request.WorkflowRef != args["workflow_ref"] || client.request.Task != args["task"] {
		t.Fatal("owner-bound launch request changed")
	}
	for _, key := range []string{"actor_ref", "project_ref", "organization_ref", "root_run_ref", "source", "runtime_revision_ref"} {
		copy := map[string]any{"workflow_ref": "wf_workflow001", "task": "bounded", key: "forged"}
		if _, err := server.launchWorkflow(t.Context(), input, copy, nil); err == nil {
			t.Fatal("client authority accepted")
		}
	}
	for _, task := range []string{"", strings.Repeat("🙂", runtimecontract.MaximumAssistantTurnCodepoints+1), "bad\x00task", string([]byte{0xff})} {
		if _, err := server.launchWorkflow(t.Context(), input, map[string]any{"workflow_ref": "wf_workflow001", "task": task}, nil); err == nil {
			t.Fatal("invalid task accepted")
		}
	}
	if client.calls != 1 {
		t.Fatal("rejected input reached owner")
	}
	projection, capability, grant, ok := safeToolCallParameters(input, "launch_workflow", args)
	if !ok || capability != "platform.run.launch" || grant != "" || len(projection) != 1 || projection["workflow_ref"] != args["workflow_ref"] {
		t.Fatal("launch safe activity projection changed")
	}
}
