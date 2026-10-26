package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// assistantWorkflowTextBudgetFixture produces individually valid Cyrillic fields at an exact aggregate UTF-8 byte count using the real editable assistant shape.
// assistantWorkflowTextBudgetFixture задаёт отдельно допустимые поля с кириллицей и точной суммой UTF-8 байт в настоящей редактируемой форме помощника.
func assistantWorkflowTextBudgetFixture(t *testing.T, size int) map[string]any {
	t.Helper()
	input := assistantServerWorkflowFixture(t)
	input["name"], input["purpose"], input["completionCriteria"] = "Budget", "", strings.Repeat("я", 512)
	used := len(input["name"].(string)) + len(input["completionCriteria"].(string))
	for _, raw := range input["steps"].([]any) {
		step := raw.(map[string]any)
		step["purpose"] = strings.Repeat("я", 1000)
		used += len(step["name"].(string)) + len(step["purpose"].(string)) + len(step["expectedResult"].(string))
	}
	remaining := size - used
	if remaining < 1 || remaining > 1999 {
		t.Fatal("text budget fixture exceeds an individual field bound")
	}
	input["purpose"] = strings.Repeat("я", remaining/2) + strings.Repeat("x", remaining%2)
	return input
}

// TestAssistantWorkflowTextBudgetBoundary exercises actual CREATE parsing and retains all unrelated per-field checks and the legacy profile at both aggregate boundaries.
// TestAssistantWorkflowTextBudgetBoundary проверяет настоящий CREATE parser и сохраняет прочие ограничения полей и прежний профиль на обеих суммарных границах.
func TestAssistantWorkflowTextBudgetBoundary(t *testing.T) {
	for _, size := range []int{8192, 8193} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			input := assistantWorkflowTextBudgetFixture(t, size)
			created, err := assistantWorkflow(input)
			if size == 8192 {
				if err != nil || created.Draft == nil {
					t.Fatal("valid server byte boundary rejected")
				}
			} else if !errors.Is(err, errs.ErrInvalid) || errs.AssistantPlanField(err) != "WORKFLOW_TEXT_BUDGET" {
				t.Fatal("server byte overflow lost its exact closed field", errs.AssistantPlanField(err))
			}
			input["executionMode"] = "MANAGER_DRIVEN"
			for _, raw := range input["steps"].([]any) {
				step := raw.(map[string]any)
				for _, key := range []string{"key", "dependsOn", "resultRequirements"} {
					delete(step, key)
				}
			}
			if _, err := assistantWorkflow(input); err != nil {
				t.Fatal("server aggregate budget changed legacy creation")
			}
		})
	}
}

// TestAssistantWorkflowTextBudgetUpdateCountsRetainedFields checks sparse UPDATE against the entire hydrated draft, including unchanged root instructions; it never mutates the before snapshot.
// TestAssistantWorkflowTextBudgetUpdateCountsRetainedFields проверяет sparse UPDATE по всему hydrated draft, включая прежние общие инструкции, не изменяя before snapshot.
func TestAssistantWorkflowTextBudgetUpdateCountsRetainedFields(t *testing.T) {
	created, err := assistantWorkflow(assistantWorkflowTextBudgetFixture(t, 8192))
	if err != nil || created.Draft == nil {
		t.Fatal("construct bounded server draft")
	}
	draft := *created.Draft
	draft.Instructions = strings.Repeat("я", 512)
	draft.Steps[0].Instructions = strings.Repeat("я", 488)
	fields, steps := assistantWorkflowGraphFields(draft)
	before := map[string]any{"workflowRef": "wfl_fixture123", "projectRef": created.ProjectRef, "name": draft.Name, "purpose": draft.Purpose, "instructions": draft.Instructions, "completionCriteria": draft.CompletionCriteria, "executionMode": draft.ExecutionMode, "coordinatorAgentRef": draft.CoordinatorAgentRef, "inputFields": fields, "steps": steps, "maxConcurrency": float64(draft.Concurrency), "timeoutSeconds": float64(draft.TimeoutSeconds), "draft": draft}
	original, _ := json.Marshal(before)
	positive, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: map[string]any{"workflowRef": "wfl_fixture123", "name": "budget"}})
	if err != nil || positive.After["instructions"] != draft.Instructions {
		t.Fatal("bounded sparse update lost retained instructions")
	}
	_, err = hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: map[string]any{"workflowRef": "wfl_fixture123", "purpose": draft.Purpose + "x"}})
	if !errors.Is(err, errs.ErrInvalid) || errs.AssistantPlanField(err) != "WORKFLOW_TEXT_BUDGET" {
		t.Fatal("hydrated overflow lost its closed budget field", errs.AssistantPlanField(err))
	}
	after, _ := json.Marshal(before)
	if string(original) != string(after) {
		t.Fatal("text budget validation mutated the owner snapshot")
	}
}
