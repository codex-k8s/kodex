package callback

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Первый вариант union сохраняет прежнюю точную экранную привязку.
func assistantOrdinaryParametersSchema(schema map[string]any) map[string]any {
	if branches, ok := schema["oneOf"].([]map[string]any); ok {
		return branches[0]
	}
	return schema
}

func TestProjectAssistantLocatorSchemasAreExclusiveAndPersistAcrossScreens(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, screen := range []string{"PROJECT", "AGENT", "ENVIRONMENT", "FILE"} {
			t.Run(string(scope)+"/"+screen, func(t *testing.T) {
				input := assistantConfigurationFixture(scope)
				input.RuntimeEnvironmentRef = "renv_current123"
				input.AssistantContext.EntityKind, input.AssistantContext.EntityRef = screen, "res_foreign123"
				input.AssistantContext.AllowedOperations = []string{}
				seen := map[string]bool{}
				for _, operation := range assistantPlanOperationSchemas(input) {
					kind := assistantSchemaType(operation)
					if !projectAssistantLocatorOperation(kind, map[string]any{"projectAssistantRef": input.AgentRef}) {
						continue
					}
					if seen[kind] {
						t.Fatal("duplicate operation kind breaks indexed discovery")
					}
					seen[kind] = true
					parameters := operation["properties"].(map[string]any)["parameters"].(map[string]any)
					if branches, union := parameters["oneOf"].([]map[string]any); union {
						if len(branches) != 2 || branches[0]["properties"].(map[string]any)["projectAssistantRef"] != nil {
							t.Fatal("ordinary and helper locator forms are not exclusive")
						}
						parameters = branches[1]
					}
					fields := parameters["properties"].(map[string]any)
					if parameters["additionalProperties"] != false || fields["projectAssistantRef"] == nil {
						t.Fatal("helper locator schema is not closed")
					}
					for _, forbidden := range []string{"agentRef", "systemAssistantRef", "projectRef", "organizationRef", "expectedVersion", "assistantProfileRef", "secretValue"} {
						if fields[forbidden] != nil {
							t.Fatal("helper locator schema exposed authority or plaintext secret")
						}
					}
					if scope == runtimecontract.AssistantScopeProject && !reflect.DeepEqual(fields["projectAssistantRef"].(map[string]any)["enum"], []string{input.AgentRef}) {
						t.Fatal("PROJECT locator is not exact immutable self")
					}
					if kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" && !reflect.DeepEqual(parameters["required"], []string{"projectAssistantRef"}) {
						t.Fatal("environment revision cannot use owner-resolved binding")
					}
				}
				if len(seen) != 3 {
					t.Fatal("helper configuration disappeared in unrelated screen context")
				}
			})
		}
	}
}

func TestProjectAssistantLocatorTraversesFencedOwnerDraftWithoutScreenRebind(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, kind := range []string{"CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "PREPARE_RUNTIME_ENVIRONMENT_REVISION"} {
			for _, boundLocator := range []bool{false, true} {
				if kind != "PREPARE_RUNTIME_ENVIRONMENT_REVISION" && boundLocator {
					continue
				}
				t.Run(string(scope)+"/"+kind+"/"+map[bool]string{false: "derived", true: "explicit"}[boundLocator], func(t *testing.T) {
					input := assistantConfigurationFixture(scope)
					input.AssistantContext.EntityKind, input.AssistantContext.EntityRef = "FILE", "file_foreign123"
					ref := input.AgentRef
					if scope == runtimecontract.AssistantScopeSystem {
						ref = "agt_project123"
					}
					parameters := map[string]any{"projectAssistantRef": ref}
					target := "AGENT"
					switch kind {
					case "CREATE_INSTRUCTION_DRAFT":
						parameters["instructions"] = "Approved project assistant instructions"
					case "BIND_AGENT_RUNTIME_ENVIRONMENT":
						parameters["environmentRef"] = "renv_project123"
					case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
						parameters["description"] = "Updated assistant environment"
						target = "ENVIRONMENT"
						if boundLocator {
							parameters["environmentRef"] = "renv_project123"
						}
					}
					client := &scopedAssistantPlanClient{}
					server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
					arguments := map[string]any{"summary": "Prepare helper configuration", "operations": []any{map[string]any{
						"type": kind, "parameters": parameters, "expectedVersion": float64(99),
						"target": map[string]any{"kind": "ORGANIZATION", "ref": "untrusted", "name": "untrusted"},
						"before": map[string]any{"owner": "untrusted"}, "after": map[string]any{"owner": "untrusted"},
					}}}
					if _, err := server.proposeAssistantPlan(t.Context(), input, arguments, json.RawMessage(`14`)); err != nil {
						t.Fatal(err)
					}
					if len(client.requests) != 1 {
						t.Fatal("helper locator did not reach exactly one owner draft RPC")
					}
					request, operation := client.requests[0], client.requests[0].Operations[0]
					if request.LeaseRef != input.LeaseRef || request.Fence != input.LeaseFence || request.Generation != input.LeaseGeneration || operation.TargetKind != target || operation.TargetRef != "" || operation.ExpectedVersion != nil || len(operation.Before.AsMap()) != 0 || !reflect.DeepEqual(operation.After.AsMap(), parameters) || !reflect.DeepEqual(operation.Parameters.AsMap(), parameters) {
						t.Fatal("helper draft retained caller authority or lost lease/scoped locator")
					}
					if input.AssistantContext.EntityKind != "FILE" || input.AssistantContext.EntityRef != "file_foreign123" {
						t.Fatal("helper draft rebound screen context")
					}
				})
			}
		}
	}
}

func TestProjectAssistantLocatorRejectsMixedAuthorityBeforeRPC(t *testing.T) {
	for _, kind := range []string{"CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "PREPARE_RUNTIME_ENVIRONMENT_REVISION"} {
		for _, forbidden := range []string{"agentRef", "systemAssistantRef", "projectRef", "organizationRef", "assistantProfileRef", "ownerRef", "expectedVersion", "secretValue"} {
			t.Run(kind+"/"+forbidden, func(t *testing.T) {
				parameters := map[string]any{"projectAssistantRef": "agt_project123", forbidden: "untrusted"}
				switch kind {
				case "CREATE_INSTRUCTION_DRAFT":
					parameters["instructions"] = "Approved project assistant instructions"
				case "BIND_AGENT_RUNTIME_ENVIRONMENT":
					parameters["environmentRef"] = "renv_project123"
				case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
					parameters["description"] = "Updated assistant environment"
				}
				assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, kind, parameters)
			})
		}
		assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeProject, kind, map[string]any{"projectAssistantRef": "agt_foreign123"})
		assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeNone, kind, map[string]any{"projectAssistantRef": "agt_project123"})
		assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, kind, map[string]any{"projectAssistantRef": "bad"})
		assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, kind, map[string]any{"projectAssistantRef": "agt_project123"})
	}
}
