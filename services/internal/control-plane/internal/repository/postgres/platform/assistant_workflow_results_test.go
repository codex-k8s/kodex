package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// assistantServerWorkflowFixture builds a fresh three-role editable graph through JSON, so the
// fixture uses the same map/array/number shapes as a real assistant proposal without live effects.
// assistantServerWorkflowFixture создаёт через JSON новый редактируемый граф трёх ролей с теми же
// формами map/array/number, что и настоящий план помощника, без действий в живой среде.
func assistantServerWorkflowFixture(t *testing.T) map[string]any {
	t.Helper()
	raw := `{"projectRef":"prj_fixture123","name":"Журнал обращений","purpose":"Подготовить небольшой сервис","coordinatorAgentRef":"agt_manager123","executionMode":"SERVER_DAG_V1","maxConcurrency":2,"timeoutSeconds":3600,"inputFields":[],"steps":[{"key":"spec","name":"Спецификация","purpose":"Описать требования","agentRef":"agt_analyst123","parallel":false,"parallelGroup":0,"timeoutSeconds":300,"expectedResult":"Спецификация","humanGate":false,"gateDecisions":[],"requiredCapabilityKeys":[],"dependsOn":[],"resultRequirements":[{"key":"spec","kind":"ARTIFACT","fileName":"spec.md","mediaType":"text/markdown"}]},{"key":"code","name":"Разработка","purpose":"Реализовать сервис","agentRef":"agt_developer1","parallel":false,"parallelGroup":0,"timeoutSeconds":600,"expectedResult":"PR","humanGate":false,"gateDecisions":[],"requiredCapabilityKeys":[],"dependsOn":["spec"],"resultRequirements":[{"key":"pr","kind":"PULL_REQUEST","repository":"codex-k8s/test-repo"}]},{"key":"review","name":"Ревью","purpose":"Проверить PR","agentRef":"agt_reviewer1","parallel":false,"parallelGroup":0,"timeoutSeconds":300,"expectedResult":"Заключение","humanGate":true,"approvalTimeoutSeconds":90,"gateDecisions":["APPROVE","REJECT"],"requiredCapabilityKeys":[],"dependsOn":["code"],"resultRequirements":[{"key":"review","kind":"REVIEW","subjectStepKey":"code"}]}]}`
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	return input
}

// TestAssistantServerWorkflowCreateReadUpdate preserves explicit dependency and subject keys,
// mode and approval TTL through the actual create parser, read projection, hydration and update.
// TestAssistantServerWorkflowCreateReadUpdate сохраняет ключи зависимостей и проверяемого шага,
// режим и TTL через настоящий create parser, read projection, подготовку и изменение плана.
func TestAssistantServerWorkflowCreateReadUpdate(t *testing.T) {
	input := assistantServerWorkflowFixture(t)
	created, err := assistantWorkflow(input)
	if err != nil || created.Draft == nil {
		t.Fatal("server creation rejected", err)
	}
	draft := *created.Draft
	if draft.ExecutionMode != "SERVER_DAG_V1" || draft.Steps[1].Key != "code" || draft.Steps[2].ResultRequirements[0].SubjectStepKey != "code" || draft.Steps[2].ApprovalTimeoutSeconds == nil || *draft.Steps[2].ApprovalTimeoutSeconds != 90 {
		t.Fatal("server create predicates changed")
	}
	fields, steps := assistantWorkflowGraphFields(draft)
	before := map[string]any{"workflowRef": "wfl_fixture123", "projectRef": created.ProjectRef, "executionMode": draft.ExecutionMode, "name": draft.Name, "purpose": draft.Purpose, "instructions": draft.Instructions, "completionCriteria": draft.CompletionCriteria, "coordinatorAgentRef": draft.CoordinatorAgentRef, "inputFields": fields, "steps": steps, "maxConcurrency": float64(draft.Concurrency), "timeoutSeconds": float64(draft.TimeoutSeconds), "draft": draft}
	for _, graphEdit := range []bool{false, true} {
		parameters := map[string]any{"workflowRef": "wfl_fixture123", "name": "Журнал обращений: ревью"}
		if graphEdit {
			clone := cloneAssistantFields(steps[2].(map[string]any))
			clone["key"], clone["agentRef"], clone["name"] = "security", "agt_security1", "Проверка безопасности"
			parameters["steps"] = append(append([]any{}, steps...), clone)
		}
		operation, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Key: "update", Parameters: parameters})
		if err != nil {
			t.Fatal("server update hydration rejected", err)
		}
		updated, version, err := assistantUpdateWorkflow(operation)
		if err != nil || version != 7 || updated.Draft == nil {
			t.Fatal("server update rejected", err)
		}
		if !reflect.DeepEqual(updated.Draft.Steps[:3], draft.Steps) || updated.Draft.ExecutionMode != draft.ExecutionMode {
			t.Fatal("existing server edges, predicates or TTL changed")
		}
		if graphEdit {
			step := updated.Draft.Steps[3]
			if step.Key != "security" || !reflect.DeepEqual(step.DependsOn, []string{"code"}) || step.ResultRequirements[0].SubjectStepKey != "code" {
				t.Fatal("new server step acquired inferred dependency or stale subject key")
			}
		}
		raw, err := json.Marshal(operation.After["draft"])
		var after entity.WorkflowVersion
		if err != nil || json.Unmarshal(raw, &after) != nil || !reflect.DeepEqual(after, *updated.Draft) || operation.After["executionMode"] != "SERVER_DAG_V1" {
			t.Fatal("owner preview disagrees with applied workflow")
		}
	}
}

// TestAssistantServerWorkflowRejectsMalformedPredicates prevents lossy coercion, mode confusion
// and narrowing overflow from bypassing the bounded owner graph contract.
// TestAssistantServerWorkflowRejectsMalformedPredicates исключает исправление неверных типов,
// смешение режимов и обход лимитов через переполнение при сужении чисел.
func TestAssistantServerWorkflowRejectsMalformedPredicates(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any, map[string]any){
		"mode null":              func(v, _ map[string]any) { v["executionMode"] = nil },
		"mode empty":             func(v, _ map[string]any) { v["executionMode"] = "" },
		"mode whitespace":        func(v, _ map[string]any) { v["executionMode"] = " SERVER_DAG_V1 " },
		"legacy explicit graph":  func(v, _ map[string]any) { v["executionMode"] = "MANAGER_DRIVEN" },
		"concurrency wrap":       func(v, _ map[string]any) { v["maxConcurrency"] = float64(1<<32 + 1) },
		"concurrency malformed":  func(v, _ map[string]any) { v["maxConcurrency"] = "two" },
		"concurrency three":      func(v, _ map[string]any) { v["maxConcurrency"] = float64(3) },
		"timeout malformed":      func(v, _ map[string]any) { v["timeoutSeconds"] = nil },
		"step timeout wrap":      func(_, s map[string]any) { s["timeoutSeconds"] = float64(1<<32 + 300) },
		"group wrap":             func(_, s map[string]any) { s["parallelGroup"] = float64(1 << 32) },
		"key whitespace":         func(_, s map[string]any) { s["key"] = " spec " },
		"dependencies missing":   func(_, s map[string]any) { delete(s, "dependsOn") },
		"dependencies null":      func(_, s map[string]any) { s["dependsOn"] = nil },
		"dependencies malformed": func(_, s map[string]any) { s["dependsOn"] = []any{true} },
		"requirements missing":   func(_, s map[string]any) { delete(s, "resultRequirements") },
		"requirements empty":     func(_, s map[string]any) { s["resultRequirements"] = []any{} },
		"requirements overflow": func(_, s map[string]any) {
			r := s["resultRequirements"].([]any)[0]
			s["resultRequirements"] = []any{r, r, r, r, r}
		},
		"unknown field":          func(_, s map[string]any) { s["resultRequirements"].([]any)[0].(map[string]any)["owner"] = "actor" },
		"hidden null predicate":  func(_, s map[string]any) { s["resultRequirements"].([]any)[0].(map[string]any)["repository"] = nil },
		"hidden empty predicate": func(_, s map[string]any) { s["resultRequirements"].([]any)[0].(map[string]any)["repository"] = "" },
		"wrong optional type":    func(_, s map[string]any) { s["resultRequirements"].([]any)[0].(map[string]any)["operation"] = true },
		"approval without gate":  func(_, s map[string]any) { s["approvalTimeoutSeconds"] = float64(30) },
	} {
		t.Run(name, func(t *testing.T) {
			input := assistantServerWorkflowFixture(t)
			mutate(input, input["steps"].([]any)[0].(map[string]any))
			if _, err := assistantWorkflow(input); !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("invalid server configuration accepted", err)
			}
		})
	}
}
