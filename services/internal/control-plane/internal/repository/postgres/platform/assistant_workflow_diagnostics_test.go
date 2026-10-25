package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAssistantWorkflowRejectingGuardProducesOnlyClosedField mutates concurrency, timeout, step
// shape/key and bounded text in a fresh workflow fixture, requiring Invalid with the exact closed
// field and no private marker. Stage/category/index must survive diagnostic wrapping; this is an
// in-memory hydration test, not workflow execution.
// TestAssistantWorkflowRejectingGuardProducesOnlyClosedField изменяет concurrency, timeout, step
// shape/key и bounded text свежей workflow fixture, требуя Invalid с точным closed field без
// private marker. Stage/category/index сохраняются после diagnostic wrapping; это hydration test в
// памяти, а не исполнение workflow.
func TestAssistantWorkflowRejectingGuardProducesOnlyClosedField(t *testing.T) {
	for _, test := range []struct {
		name, field string
		change      func(map[string]any, []any)
	}{
		{"concurrency", "MAX_CONCURRENCY", func(p map[string]any, _ []any) { p["maxConcurrency"] = 0.0 }},
		{"timeout", "TIMEOUT_SECONDS", func(p map[string]any, _ []any) { p["timeoutSeconds"] = 0.0 }},
		{"step shape", "STEP_SHAPE", func(_ map[string]any, s []any) { s[0].(map[string]any)["PRIVATE_KEY"] = "PRIVATE_PAYLOAD" }},
		{"step key", "STEP_KEY", func(_ map[string]any, s []any) { s[0].(map[string]any)["key"] = "PRIVATE_KEY" }},
		{"instructions", "STEPS_INSTRUCTIONS", func(_ map[string]any, s []any) { s[0].(map[string]any)["purpose"] = strings.Repeat("я", 1001) }},
		{"expected result", "STEPS_EXPECTED_RESULT", func(_ map[string]any, s []any) { s[0].(map[string]any)["expectedResult"] = strings.Repeat("я", 1001) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, before, steps := assistantWorkflowFrontierFixture(t)
			parameters := map[string]any{"workflowRef": before["workflowRef"], "steps": steps}
			test.change(parameters, steps)
			_, failure := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: parameters})
			if !errors.Is(failure, errs.ErrInvalid) || errs.AssistantPlanField(failure) != test.field || strings.Contains(failure.Error(), "PRIVATE") {
				t.Fatal("rejecting guard lost exact closed field or disclosed input")
			}
			stage, category, index, ok := errs.AssistantPlanDiagnostic(errs.WithAssistantPlanStage(failure, errs.AssistantPlanHydrate, 1))
			if !ok || stage != errs.AssistantPlanHydrate || category != "INVALID" || index != 1 {
				t.Fatal("actual rejecting field did not survive the proposal stage")
			}
		})
	}
}

// TestAssistantWorkflowDiagnosticUsesTheSharedUnicodePredicate checks937 multibyte Cyrillic
// codepoints remain valid during hydration, guarding against a second byte-based instruction
// validator. It exercises the diagnostic path with a synthetic graph and does not claim runtime
// prompt delivery.
// TestAssistantWorkflowDiagnosticUsesTheSharedUnicodePredicate проверяет допустимость937
// многобайтных Cyrillic codepoints при hydration, защищая от второго byte-based instruction
// validator. Проверяет diagnostic path на synthetic graph без утверждения runtime prompt delivery.
func TestAssistantWorkflowDiagnosticUsesTheSharedUnicodePredicate(t *testing.T) {
	_, before, steps := assistantWorkflowFrontierFixture(t)
	steps[0].(map[string]any)["purpose"] = strings.Repeat("я", 937)
	_, failure := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Parameters: map[string]any{"workflowRef": before["workflowRef"], "steps": steps}})
	if failure != nil {
		t.Fatal("diagnostics introduced a second byte-based workflow validator")
	}
}
