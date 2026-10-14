package callback

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestProjectAssistantSelfCapabilityDiscovery(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeProject)
	input.DelegationTargets = []runtimecontract.RunnerDelegationTarget{{Ref: "agt_employee123", Name: "Employee"}}
	for _, screen := range []string{"PROJECT", "AGENT"} {
		t.Run(screen, func(t *testing.T) {
			current := input
			if screen == "AGENT" {
				current.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: input.AgentRef, EntityName: "Project helper", AllowedOperations: []string{"CHANGE_CAPABILITY"}}
			}
			catalog, err := configurationCatalog(current, map[string]any{"operation_types": []any{"CHANGE_CAPABILITY"}})
			if err != nil {
				t.Fatal("exact project helper capability is absent from discovery")
			}
			schemas := catalog.(map[string]any)["operation_schemas"].([]map[string]any)
			if len(schemas) != 1 {
				t.Fatal("exact capability schema is unavailable")
			}
			parameters := schemas[0]["properties"].(map[string]any)["parameters"].(map[string]any)
			fields := parameters["properties"].(map[string]any)
			if !slices.Equal(fields["agentRef"].(map[string]any)["enum"].([]string), []string{input.AgentRef}) {
				t.Fatal("capability target is not the immutable source helper")
			}
			if !slices.Contains(fields["capabilityKey"].(map[string]any)["enum"].([]string), "platform.artifact.manage") || fields["expectedVersion"] != nil {
				t.Fatal("capability or server-owned OCC boundary changed")
			}
		})
	}
}

func TestProjectAssistantSelfCapabilityDraftBoundary(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeProject)
	input.AssistantProfileRef = "asstprof_exact123"
	parameters := map[string]any{"agentRef": input.AgentRef, "capabilityKey": "platform.artifact.manage", "enabled": true}
	for _, scenario := range []struct {
		name   string
		change func(*runtimecontract.RunnerInput, map[string]any)
		want   bool
	}{
		{"self", func(_ *runtimecontract.RunnerInput, _ map[string]any) {}, true},
		{"other helper", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["agentRef"] = "agt_other123" }, false},
		{"caller project", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["projectRef"] = "prj_foreign123" }, false},
		{"ordinary staff", func(i *runtimecontract.RunnerInput, _ map[string]any) {
			i.AssistantScope = runtimecontract.AssistantScopeNone
		}, false},
		{"unknown capability", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["capabilityKey"] = "platform.future.manage" }, false},
		{"caller actor", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["actorRef"] = "usr_foreign123" }, false},
		{"caller OCC", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["expectedVersion"] = float64(2) }, false},
		{"malformed enabled", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["enabled"] = "true" }, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			current := input
			p := make(map[string]any, len(parameters))
			for key, value := range parameters {
				p[key] = value
			}
			scenario.change(&current, p)
			client := &scopedAssistantPlanClient{}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			_, err := server.proposeAssistantPlan(t.Context(), current, map[string]any{"summary": "Предложить работу с файлами", "operations": []any{map[string]any{"type": "CHANGE_CAPABILITY", "title": "Разрешить работу с файлами", "parameters": p}}}, json.RawMessage(`1`))
			if (err == nil) != scenario.want || (len(client.requests) == 1) != scenario.want {
				t.Fatal("capability draft crossed the immutable helper boundary")
			}
			if scenario.want {
				request := client.requests[0]
				if request.GetLeaseRef() != current.LeaseRef || request.GetFence() != current.LeaseFence || request.GetGeneration() != current.LeaseGeneration || request.GetOperations()[0].GetTargetKind() != "AGENT" {
					t.Fatal("draft lost exact execution or owner-hydrated target")
				}
			}
		})
	}
	// Существующая экранная команда сотрудника не становится self-configuration.
	input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: "agt_employee123", EntityName: "Employee", AllowedOperations: []string{"CHANGE_CAPABILITY"}}
	parameters["agentRef"] = input.AssistantContext.EntityRef
	if !assistantConfigurationParametersAllowed(input, "CHANGE_CAPABILITY", parameters) || assistantOperationTargetContext(input, "CHANGE_CAPABILITY", parameters) != input.AssistantContext {
		t.Fatal("ordinary screen-bound capability path changed")
	}
}
