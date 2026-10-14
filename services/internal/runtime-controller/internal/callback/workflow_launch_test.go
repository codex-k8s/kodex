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

func TestWorkflowLaunchSchemaExplainsExactInputKeys(t *testing.T) {
	tool := workflowLaunchTool()
	schema := tool["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	version := properties["expected_workflow_version"].(map[string]any)
	if version["type"] != "integer" || version["minimum"] != 1 {
		t.Fatal("workflow version OCC schema is absent")
	}
	input := properties["input"].(map[string]any)
	if input["type"] != "object" {
		t.Fatal("workflow launch input must remain an object map")
	}
	for name, description := range map[string]any{"tool": tool["description"], "input": input["description"]} {
		t.Run(name, func(t *testing.T) {
			text, ok := description.(string)
			if !ok {
				t.Fatal("workflow launch input guidance is absent")
			}
			for _, requirement := range []string{"WorkflowInputField.Key", "every required field", "labels", "aliases", "obtain them before launching"} {
				if !strings.Contains(text, requirement) {
					t.Fatalf("workflow launch input guidance omits %q", requirement)
				}
			}
		})
	}
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
	for _, variant := range []string{"capability", "assistant", "system", "missing-scope", "unknown-scope", "project", "mode"} {
		t.Run(variant, func(t *testing.T) {
			copy := input
			switch variant {
			case "capability":
				copy.Capabilities = nil
			case "assistant":
				copy.AssistantScope = runtimecontract.AssistantScopeProject
			case "system":
				copy.AssistantScope = runtimecontract.AssistantScopeSystem
			case "missing-scope":
				copy.AssistantScope = ""
			case "unknown-scope":
				copy.AssistantScope = "UNKNOWN"
			case "project":
				copy.ProjectRef = ""
			case "mode":
				copy.Mode = ""
			}
			if hasTool(copy) {
				t.Fatal("ineligible launch tool was exposed")
			}
			client := &workflowLaunchClient{}
			server := &Server{control: &controlplaneclient.Client{Runtime: client}}
			if _, err := server.launchWorkflow(t.Context(), copy, map[string]any{"workflow_ref": "wf_workflow001", "task": "bounded"}, nil); err == nil || client.calls != 0 {
				t.Fatal("ineligible launch reached authoritative owner")
			}
		})
	}
	client := &workflowLaunchClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	args := map[string]any{"workflow_ref": "wf_workflow001", "expected_published_ref": "wfv_workflow001", "expected_spec_digest": strings.Repeat("a", 64), "expected_workflow_version": float64(7), "task": strings.Repeat("🙂", runtimecontract.MaximumAssistantTurnCodepoints), "input": map[string]any{"record": "bounded"}}
	response, err := server.launchWorkflow(t.Context(), input, args, json.RawMessage(`"call-one"`))
	if err != nil || response == nil || client.calls != 1 {
		t.Fatalf("launch: %v", err)
	}
	if client.request.LeaseRef != input.LeaseRef || client.request.Fence != input.LeaseFence || client.request.Generation != 7 || client.request.WorkflowRef != args["workflow_ref"] || client.request.Task != args["task"] {
		t.Fatal("owner-bound launch request changed")
	}
	if client.request.ExpectedPublishedRef != args["expected_published_ref"] || client.request.ExpectedSpecDigest != args["expected_spec_digest"] || client.request.ExpectedWorkflowVersion != 7 {
		t.Fatal("published launch pins lost")
	}
	for _, key := range []string{"expected_published_ref", "expected_spec_digest", "expected_workflow_version"} {
		copy := map[string]any{}
		for name, value := range args {
			copy[name] = value
		}
		delete(copy, key)
		if _, err := server.launchWorkflow(t.Context(), input, copy, nil); err == nil {
			t.Fatal("missing launch pin accepted")
		}
	}
	for _, key := range []string{"actor_ref", "project_ref", "organization_ref", "root_run_ref", "source", "runtime_revision_ref"} {
		copy := map[string]any{}
		for name, value := range args {
			copy[name] = value
		}
		copy[key] = "forged"
		if _, err := server.launchWorkflow(t.Context(), input, copy, nil); err == nil {
			t.Fatal("client authority accepted")
		}
	}
	for _, value := range []any{nil, "7", float64(0), float64(-1), 7.5, float64(9007199254740992)} {
		copy := map[string]any{}
		for key, original := range args {
			copy[key] = original
		}
		copy["expected_workflow_version"] = value
		if _, err := server.launchWorkflow(t.Context(), input, copy, nil); err == nil {
			t.Fatal("invalid workflow OCC accepted")
		}
	}
	for _, task := range []string{"", strings.Repeat("🙂", runtimecontract.MaximumAssistantTurnCodepoints+1), "bad\x00task", string([]byte{0xff})} {
		invalid := map[string]any{}
		for key, value := range args {
			invalid[key] = value
		}
		invalid["task"] = task
		if _, err := server.launchWorkflow(t.Context(), input, invalid, nil); err == nil {
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
