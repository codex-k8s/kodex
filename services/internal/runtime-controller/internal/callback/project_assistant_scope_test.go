package callback

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

func TestProjectAssistantCanPrepareOnlyProjectSelfConfiguration(t *testing.T) {
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject,
		AssistantProfileRef: "asstprof_abcdefgh", AgentRef: "agt_project123", ProjectRef: "prj_project123", RuntimeEnvironmentRef: "renv_project123",
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "PROJECT", EntityRef: "prj_project123", AllowedOperations: []string{"CREATE_AGENT"}},
	}
	wanted := map[string]string{"UPDATE_AGENT": "agentRef", "CREATE_INSTRUCTION_DRAFT": "agentRef", "BIND_AGENT_RUNTIME_ENVIRONMENT": "agentRef", "PREPARE_RUNTIME_ENVIRONMENT_REVISION": "environmentRef"}
	seen := make(map[string]bool)
	for _, schema := range assistantPlanOperationSchemas(input) {
		properties := schema["properties"].(map[string]any)
		kind := properties["type"].(map[string]any)["const"].(string)
		if kind == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
			t.Fatal("project assistant obtained organization instruction operation")
		}
		field, wantedOperation := wanted[kind]
		if !wantedOperation {
			continue
		}
		if seen[kind] {
			t.Fatal("ambiguous duplicate project self operation")
		}
		seen[kind] = true
		parameters := assistantOrdinaryParametersSchema(properties["parameters"].(map[string]any))["properties"].(map[string]any)
		if _, present := parameters["systemAssistantRef"]; present {
			t.Fatal("project environment operation carried organization assistant authority")
		}
		ref := parameters[field].(map[string]any)["enum"].([]string)
		wantRef := input.AgentRef
		if field == "environmentRef" {
			wantRef = input.RuntimeEnvironmentRef
		}
		if len(ref) != 1 || ref[0] != wantRef {
			t.Fatal("project self configuration was detached from immutable profile")
		}
	}
	if len(seen) != len(wanted) {
		t.Fatal("project assistant self configuration operations are incomplete")
	}
}

type scopedAssistantPlanClient struct {
	controlplanev1.RuntimeWorkServiceClient
	requests []*controlplanev1.ProposeAssistantPlanRequest
}

func (client *scopedAssistantPlanClient) ProposeAssistantPlan(_ context.Context, request *controlplanev1.ProposeAssistantPlanRequest, _ ...grpc.CallOption) (*controlplanev1.ProposeAssistantPlanResponse, error) {
	client.requests = append(client.requests, request)
	return &controlplanev1.ProposeAssistantPlanResponse{Plan: &controlplanev1.AssistantPlan{Ref: "plan_abcdefgh", Version: 1}, Conversation: &controlplanev1.AssistantConversation{Ref: "acon_abcdefgh"}}, nil
}

func TestProjectSelfConfigurationTraversesOwnerDraftRPCWithExactLease(t *testing.T) {
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AssistantProfileRef: "asstprof_abcdefgh",
		AgentRef: "agt_project123", ProjectRef: "prj_project123", RuntimeEnvironmentRef: "renv_project123",
		LeaseRef: "lease_project123", LeaseFence: "fence", LeaseGeneration: 3,
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "PROJECT", EntityRef: "prj_project123", EntityName: "Project"},
	}
	for _, kind := range []string{"UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "PREPARE_RUNTIME_ENVIRONMENT_REVISION"} {
		t.Run(kind, func(t *testing.T) {
			client := &scopedAssistantPlanClient{}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			parameters := map[string]any{"agentRef": input.AgentRef}
			if kind == "CREATE_INSTRUCTION_DRAFT" {
				parameters["instructions"] = "Approved instructions"
			}
			if kind == "UPDATE_AGENT" {
				parameters["purpose"] = "Project assistant"
			}
			if kind == "BIND_AGENT_RUNTIME_ENVIRONMENT" {
				parameters["environmentRef"] = input.RuntimeEnvironmentRef
			}
			if kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
				parameters = map[string]any{"environmentRef": input.RuntimeEnvironmentRef}
			}
			arguments := map[string]any{"summary": "Prepare configuration", "operations": []any{map[string]any{"type": kind, "parameters": parameters}}}
			if _, err := server.proposeAssistantPlan(t.Context(), input, arguments, json.RawMessage(`1`)); err != nil {
				t.Fatal(err)
			}
			if len(client.requests) != 1 {
				t.Fatal("project self configuration did not reach exactly one owner draft command")
			}
			request := client.requests[0]
			if request.LeaseRef != input.LeaseRef || request.Fence != input.LeaseFence || request.Generation != input.LeaseGeneration || len(request.Operations) != 1 {
				t.Fatal("project draft lost exact lease authority")
			}
			if request.Operations[0].Parameters.AsMap()["systemAssistantRef"] != nil {
				t.Fatal("project self draft carried organization authority")
			}
		})
	}
}

func TestOnlySystemAssistantCanProposeProjectAssistantCreation(t *testing.T) {
	parameters := map[string]any{"projectRef": "prj_project123", "name": "Project Kodex", "purpose": "Coordinate project", "instructions": "Approved instruction"}
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input := runtimecontract.RunnerInput{AssistantScope: scope, ProjectRef: "prj_project123", LeaseRef: "lease_project123", LeaseFence: "fence", LeaseGeneration: 2}
		client := &scopedAssistantPlanClient{}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
		arguments := map[string]any{"summary": "Configure project assistant", "operations": []any{map[string]any{"type": "CREATE_PROJECT_ASSISTANT", "parameters": parameters}}}
		_, err := server.proposeAssistantPlan(t.Context(), input, arguments, json.RawMessage(`1`))
		if scope == runtimecontract.AssistantScopeProject {
			if err == nil || len(client.requests) != 0 {
				t.Fatal("project assistant reached privileged profile creation RPC")
			}
			for _, schema := range assistantPlanOperationSchemas(input) {
				if assistantSchemaType(schema) == "CREATE_PROJECT_ASSISTANT" {
					t.Fatal("project assistant catalog exposed privileged creation")
				}
			}
		} else if err != nil || len(client.requests) != 1 || client.requests[0].Operations[0].TargetKind != "PROJECT_ASSISTANT" {
			t.Fatalf("system assistant project setup draft unavailable: %v", err)
		}
	}
}

func TestProjectSelfCatalogPersistsAcrossForeignScreenContexts(t *testing.T) {
	for _, kind := range []string{"AGENT", "ENVIRONMENT", "PROJECT"} {
		t.Run(kind, func(t *testing.T) {
			input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AgentRef: "agt_own12345", RuntimeEnvironmentRef: "renv_own12345",
				AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: kind, EntityRef: "res_foreign123", AllowedOperations: []string{"CREATE_AGENT"}},
			}
			ownCount := 0
			for _, schema := range assistantPlanOperationSchemas(input) {
				op := assistantSchemaType(schema)
				if op != "UPDATE_AGENT" && op != "CREATE_INSTRUCTION_DRAFT" && op != "BIND_AGENT_RUNTIME_ENVIRONMENT" && op != "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
					continue
				}
				ownCount++
				parameters := assistantOrdinaryParametersSchema(schema["properties"].(map[string]any)["parameters"].(map[string]any))["properties"].(map[string]any)
				field, want := "agentRef", input.AgentRef
				if op == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
					field, want = "environmentRef", input.RuntimeEnvironmentRef
				}
				refs := parameters[field].(map[string]any)["enum"].([]string)
				if len(refs) != 1 || refs[0] != want {
					t.Fatal("foreign screen context removed or broadened project self configuration")
				}
			}
			if ownCount != 4 {
				t.Fatal("project self configuration lost across screen transition")
			}
		})
	}
}
