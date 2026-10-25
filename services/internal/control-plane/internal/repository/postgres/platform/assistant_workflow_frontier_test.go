package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// assistantWorkflowFrontierFixture constructs a fresh33-step six-wave graph with custom
// dependency, preserved inputs/instructions/result schema and a final human gate, then proposes39
// steps by adding a fourth review peer per wave. It returns source and mutable proposal
// separately; fixture shape failure aborts the test, no runtime graph is created.
// assistantWorkflowFrontierFixture создаёт свежий33-step six-wave graph с custom dependency,
// сохранёнными inputs/instructions/result schema и final human gate, затем предлагает39 steps с
// четвёртым review peer каждой wave. Возвращает source и mutable proposal отдельно; неверная
// fixture shape завершает тест, runtime graph не создаётся.
func assistantWorkflowFrontierFixture(t *testing.T) (entity.WorkflowVersion, map[string]any, []any) {
	t.Helper()
	raw := []any{}
	add := func(name string, parallel bool, group int) {
		raw = append(raw, map[string]any{"name": name, "purpose": "Review exact immutable sources", "agentRef": "agt_fixture123",
			"parallel": parallel, "parallelGroup": float64(group), "timeoutSeconds": float64(1200),
			"expectedResult": "PASS or BLOCKED", "humanGate": false, "gateDecisions": []any{}, "requiredCapabilityKeys": []any{}})
	}
	add("Intake", false, 0)
	add("Architecture", false, 0)
	for wave := 1; wave <= 6; wave++ {
		add("Developer", false, 0)
		for range 3 {
			add("Review", true, wave)
		}
		add("Aggregation", false, 0)
	}
	add("Final readiness", false, 0)
	raw[32].(map[string]any)["humanGate"] = true
	raw[32].(map[string]any)["gateDecisions"] = []any{"APPROVE", "REJECT"}
	parsed, err := assistantWorkflow(map[string]any{"projectRef": "prj_fixture123", "name": "Software change", "purpose": "Bounded review waves", "coordinatorAgentRef": "agt_fixture123", "steps": raw, "maxConcurrency": float64(4), "timeoutSeconds": float64(86400)})
	if err != nil {
		t.Fatal(err)
	}
	draft := *parsed.Draft
	draft.Instructions, draft.ResultSchema = "Preserve original instructions", map[string]any{"type": "object"}
	for index := 1; index <= 4; index++ {
		draft.Inputs = append(draft.Inputs, entity.WorkflowInputField{Key: "field-" + leftPad(index, 3), Label: "Original field", Type: "TEXT", Required: true, DefaultValue: "Preserve default", Options: []string{}})
	}
	// Исходный DAG может иметь дополнительные edges, не выводимые из parallelGroups.
	draft.Steps[3].DependsOn = append(draft.Steps[3].DependsOn, draft.Steps[0].Key)
	fields, steps := assistantWorkflowGraphFields(draft)
	before := map[string]any{"workflowRef": "wfl_fixture123", "projectRef": "prj_fixture123", "name": draft.Name, "purpose": draft.Purpose,
		"coordinatorAgentRef": draft.CoordinatorAgentRef, "instructions": draft.Instructions, "completionCriteria": draft.CompletionCriteria,
		"maxConcurrency": float64(draft.Concurrency), "timeoutSeconds": float64(draft.TimeoutSeconds), "inputFields": fields, "steps": steps, "draft": draft}
	updated := []any{}
	for _, raw := range steps {
		step := cloneAssistantFields(raw.(map[string]any))
		updated = append(updated, step)
		if step["name"] == "Review" && len(updated) > 1 {
			// Третья review в каждой группе получает четвёртого peer перед aggregation.
			oldIndex := 0
			for _, existing := range steps {
				if existing.(map[string]any)["key"] == step["key"] {
					break
				}
				oldIndex++
			}
			if (oldIndex-5)%5 == 0 {
				peer := cloneAssistantFields(step)
				delete(peer, "key")
				peer["name"] = "Architecture review"
				updated = append(updated, peer)
			}
		}
	}
	if len(draft.Steps) != 33 || len(updated) != 39 {
		t.Fatal("invalid 33-to-39 fixture")
	}
	return draft, before, updated
}

// TestAssistantWorkflowStructuralUpdateRetainsDependencies hydrates the33→39 structural edit, maps
// the typed draft and checks After equals the exact applied DAG with stable keys and no
// server-assigned key in mutable input. Shared assertions cover old edges and new peer
// prerequisites; no workflow is persisted or launched.
// TestAssistantWorkflowStructuralUpdateRetainsDependencies гидратирует33→39 structural edit,
// строит typed draft и проверяет After как exact applied DAG со stable keys без server-assigned
// key в mutable input. Общие assertions проверяют old edges и new peer prerequisites; workflow не
// сохраняется и не запускается.
func TestAssistantWorkflowStructuralUpdateRetainsDependencies(t *testing.T) {
	draft, before, updated := assistantWorkflowFrontierFixture(t)
	operation, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Key: "update", Title: "Add architectural peers", Summary: "Preserve DAG", Parameters: map[string]any{"workflowRef": before["workflowRef"], "steps": updated}})
	if err != nil {
		t.Fatal(err)
	}
	mapped, _, err := assistantUpdateWorkflow(operation)
	if err != nil {
		t.Fatal(err)
	}
	assertAssistantWorkflowFrontier(t, draft, *mapped.Draft)
	var after entity.WorkflowVersion
	raw, err := json.Marshal(operation.After["draft"])
	if err != nil || json.Unmarshal(raw, &after) != nil || !reflect.DeepEqual(after, *mapped.Draft) {
		t.Fatal("normalized After does not expose exact applied DAG")
	}
	if len(operation.Parameters["steps"].([]any)[6].(map[string]any)) != 10 || operation.Parameters["steps"].([]any)[6].(map[string]any)["key"] != nil {
		t.Fatal("server assigned key leaked into mutable proposal input")
	}
	for index, raw := range operation.After["steps"].([]any) {
		if raw.(map[string]any)["key"] != after.Steps[index].Key {
			t.Fatal("After projection disagrees with normalized DAG keys")
		}
	}
}

// assertAssistantWorkflowFrontier asserts39 unique steps, unchanged inputs/result
// schema/instructions, retention of every original edge, four-peer aggregation, new peers
// depending on Developer and a final human gate. It fails the caller test on mismatch and checks
// only the supplied in-memory graph.
// assertAssistantWorkflowFrontier проверяет39 unique steps, неизменные inputs/result
// schema/instructions, каждое исходное ребро, four-peer aggregation, зависимость новых peers от
// Developer и final human gate. Завершает caller test при несовпадении и проверяет лишь переданный
// граф в памяти.
func assertAssistantWorkflowFrontier(t *testing.T, original, updated entity.WorkflowVersion) {
	t.Helper()
	byKey := map[string]entity.WorkflowStep{}
	for _, step := range updated.Steps {
		byKey[step.Key] = step
	}
	if len(updated.Steps) != 39 || len(byKey) != 39 || !reflect.DeepEqual(original.Inputs, updated.Inputs) || !reflect.DeepEqual(original.ResultSchema, updated.ResultSchema) || original.Instructions != updated.Instructions {
		t.Fatal("normalized structural edit lost immutable graph data")
	}
	for _, step := range original.Steps {
		actual, exists := byKey[step.Key]
		if !exists {
			t.Fatal("original key lost")
		}
		for _, dependency := range step.DependsOn {
			if !contains(actual.DependsOn, dependency) {
				t.Fatalf("original edge %s -> %s lost", dependency, step.Key)
			}
		}
		if step.Name == "Aggregation" && len(actual.DependsOn) != 4 {
			t.Fatal("aggregation does not wait for all four peers")
		}
	}
	for _, step := range updated.Steps {
		if step.Name == "Architecture review" && (len(step.DependsOn) != 1 || byKey[step.DependsOn[0]].Name != "Developer") {
			t.Fatal("new peer did not inherit review-group prerequisite")
		}
	}
	if !updated.Steps[38].HumanGateAfter {
		t.Fatal("final human gate lost")
	}
}

// TestAssistantWorkflowStructuralUpdateRejectsInvalidGraph rejects deleting/reordering original
// steps, duplicate/foreign keys, caller-supplied dependencies and duplicate source edges through
// independent fresh fixtures. It checks closed Invalid hydration failures without running a
// scheduler or evaluating delegation grants.
// TestAssistantWorkflowStructuralUpdateRejectsInvalidGraph отклоняет удаление и перестановку
// прежних steps, duplicate/foreign keys, caller-supplied dependencies и duplicate source edges на
// отдельных свежих fixtures. Проверяет closed Invalid hydration failures без scheduler и оценки
// delegation grants.
func TestAssistantWorkflowStructuralUpdateRejectsInvalidGraph(t *testing.T) {
	for _, name := range []string{"deleted", "forward", "duplicate-key", "foreign-key", "caller-dependencies", "duplicate-source-dependency"} {
		t.Run(name, func(t *testing.T) {
			draft, before, updated := assistantWorkflowFrontierFixture(t)
			switch name {
			case "deleted":
				updated = append(updated[:3], updated[4:]...)
			case "forward":
				updated[0], updated[1] = updated[1], updated[0]
			case "duplicate-key":
				updated[6].(map[string]any)["key"] = "step-001"
			case "foreign-key":
				updated[6].(map[string]any)["key"] = "step-034"
			case "caller-dependencies":
				updated[6].(map[string]any)["dependsOn"] = []any{"step-003"}
			case "duplicate-source-dependency":
				draft.Steps[3].DependsOn = append(draft.Steps[3].DependsOn, draft.Steps[3].DependsOn[0])
				before["draft"] = draft
			}
			_, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: map[string]any{"workflowRef": before["workflowRef"], "steps": updated}})
			if !errors.Is(err, errs.ErrInvalid) {
				t.Fatalf("invalid %s graph accepted: %v", name, err)
			}
		})
	}
}

// TestAssistantWorkflowUnchangedStructurePreservesExactCustomDAG changes one purpose string
// without structural edits and requires every original dependency slice to remain exactly equal
// after typed mapping. It guards custom DAG preservation in memory without claiming published
// version or runtime frontier equality.
// TestAssistantWorkflowUnchangedStructurePreservesExactCustomDAG меняет один purpose string без
// structural edits и требует точного равенства каждого исходного dependency slice после typed
// mapping. Защищает custom DAG в памяти без утверждения о published version и runtime frontier
// equality.
func TestAssistantWorkflowUnchangedStructurePreservesExactCustomDAG(t *testing.T) {
	draft, before, _ := assistantWorkflowFrontierFixture(t)
	steps := append([]any(nil), before["steps"].([]any)...)
	steps[3] = cloneAssistantFields(steps[3].(map[string]any))
	steps[3].(map[string]any)["purpose"] = "Updated bounded instruction"
	operation, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: map[string]any{"workflowRef": before["workflowRef"], "steps": steps}})
	if err != nil {
		t.Fatal(err)
	}
	payload, _, err := assistantUpdateWorkflow(operation)
	if err != nil {
		t.Fatal(err)
	}
	for index, step := range payload.Draft.Steps {
		if !reflect.DeepEqual(step.DependsOn, draft.Steps[index].DependsOn) {
			t.Fatal("nonstructural edit changed exact dependencies")
		}
	}
}

// TestAssistantWorkflowEditedPlanRegeneratesNormalizedAfter edits a hydrated plan with forged
// Before/After and a new name, requiring original source/target/OCC preservation and regenerated
// After matching the typed draft. It reuses the full frontier assertions; owner edit validation
// here is pure and does not authorize apply.
// TestAssistantWorkflowEditedPlanRegeneratesNormalizedAfter редактирует hydrated plan с forged
// Before/After и новым name, требуя сохранения original source/target/OCC и regenerated After,
// совпадающего с typed draft. Использует полные frontier assertions; чистая owner edit validation
// здесь не разрешает apply.
func TestAssistantWorkflowEditedPlanRegeneratesNormalizedAfter(t *testing.T) {
	draft, before, updated := assistantWorkflowFrontierFixture(t)
	original, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Key: "update", Title: "Add peers", Summary: "Preserve DAG", Parameters: map[string]any{"workflowRef": before["workflowRef"], "steps": updated}})
	if err != nil {
		t.Fatal(err)
	}
	edited := original
	edited.After = map[string]any{"draft": "untrusted"}
	edited.Before = map[string]any{"projectRef": "forged"}
	edited.Parameters = cloneAssistantFields(original.Parameters)
	edited.Parameters["name"] = "Edited bounded workflow"
	rehydrated, err := rehydrateEditedAssistantWorkflow(original, edited)
	if err != nil || !assistantJSONEqual(rehydrated.Before, original.Before) || rehydrated.Target.Ref != original.Target.Ref || *rehydrated.ExpectedVersion != 7 {
		t.Fatal("edited plan lost immutable source or OCC")
	}
	mapped, _, err := assistantUpdateWorkflow(rehydrated)
	if err != nil || mapped.Draft.Name != "Edited bounded workflow" || !assistantJSONEqual(rehydrated.After["draft"], *mapped.Draft) {
		t.Fatal("edited plan retained forged After")
	}
	assertAssistantWorkflowFrontier(t, draft, *mapped.Draft)
}
