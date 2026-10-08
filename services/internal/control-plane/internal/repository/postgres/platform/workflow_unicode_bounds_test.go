package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestWorkflowUnicodeTextBounds(t *testing.T) {
	t.Parallel()
	fields := []struct {
		name  string
		limit int
		set   func(*entity.WorkflowVersion, string)
	}{
		{"name", 160, func(v *entity.WorkflowVersion, s string) { v.Name = s }},
		{"purpose", 2000, func(v *entity.WorkflowVersion, s string) { v.Purpose = s }},
		{"completion", 2000, func(v *entity.WorkflowVersion, s string) { v.CompletionCriteria = s }},
		{"step_name", 160, func(v *entity.WorkflowVersion, s string) { v.Steps[0].Name = s }},
		{"step_instructions", 1000, func(v *entity.WorkflowVersion, s string) { v.Steps[0].Instructions = s }},
		{"step_expected", 1000, func(v *entity.WorkflowVersion, s string) { v.Steps[0].ExpectedResult = s }},
		{"input_label", 160, func(v *entity.WorkflowVersion, s string) { v.Inputs[0].Label = s }},
		{"input_help", 500, func(v *entity.WorkflowVersion, s string) { v.Inputs[0].Help = s }},
		{"input_option", 160, func(v *entity.WorkflowVersion, s string) {
			v.Inputs[0].Type = "SELECT"
			v.Inputs[0].Options = []string{s}
		}},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, sample := range []struct {
				name, text string
				valid      bool
			}{
				{"ascii_boundary", strings.Repeat("a", field.limit), true},
				{"cyrillic_boundary", strings.Repeat("я", field.limit), true},
				{"emoji_boundary", strings.Repeat("😀", field.limit), true},
				{"ascii_overflow", strings.Repeat("a", field.limit+1), false},
				{"unicode_overflow", strings.Repeat("я", field.limit+1), false},
				{"invalid_utf8", "text\xff", false},
			} {
				t.Run(sample.name, func(t *testing.T) {
					v := validWorkflowFixture()
					v.Inputs = []entity.WorkflowInputField{{Key: "field-001", Label: "Поле", Type: "TEXT"}}
					field.set(&v, sample.text)
					if got := validWorkflowVersion(v); got != sample.valid {
						t.Fatalf("workflow validation = %v, want %v", got, sample.valid)
					}
				})
			}
		})
	}
}

func TestWorkflowUnicodeNativeShapedInstructions(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		name                   string
		ascii, cyrillic, bytes int
	}{
		{"review", 693, 244, 1181}, {"aggregation", 544, 265, 1074}, {"final", 600, 261, 1122},
	} {
		t.Run(sample.name, func(t *testing.T) {
			v := validWorkflowFixture()
			v.Steps[0].Instructions = strings.Repeat("a", sample.ascii) + strings.Repeat("я", sample.cyrillic)
			if len(v.Steps[0].Instructions) != sample.bytes {
				t.Fatal("fixture byte count mismatch")
			}
			if !validWorkflowVersion(v) {
				t.Fatal("bounded Unicode instructions rejected")
			}
		})
	}
}

func TestWorkflowUnicodeAggregateRemainsByteBounded(t *testing.T) {
	t.Parallel()
	v := validWorkflowFixture()
	v.Steps = v.Steps[:1]
	v.Steps[0].Name, v.Steps[0].Instructions, v.Steps[0].ExpectedResult = "a", "a", ""
	v.CompletionCriteria = ""
	v.Instructions = strings.Repeat("я", 32767)
	if !validWorkflowVersion(v) {
		t.Fatal("exact 64 KiB byte budget rejected")
	}
	v.Instructions += "a"
	if validWorkflowVersion(v) {
		t.Fatal("byte budget overflow accepted")
	}
	v.Instructions = "text\xff"
	if validWorkflowVersion(v) {
		t.Fatal("invalid UTF-8 root instructions accepted")
	}
}

func TestWorkflowUnicodeInvalidFieldIsClosedAndMatchesValidity(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		name, reason string
		mutate       func(*entity.WorkflowVersion)
	}{
		{"valid", "", func(*entity.WorkflowVersion) {}},
		{"instructions_overflow", "STEPS_INSTRUCTIONS", func(v *entity.WorkflowVersion) { v.Steps[0].Instructions = strings.Repeat("я", 1001) }},
		{"instructions_invalid_utf8", "STEPS_INSTRUCTIONS", func(v *entity.WorkflowVersion) { v.Steps[0].Instructions = "private\xff" }},
		{"instructions_blank", "STEPS_INSTRUCTIONS", func(v *entity.WorkflowVersion) { v.Steps[0].Instructions = " \n" }},
		{"expected_overflow", "STEPS_EXPECTED_RESULT", func(v *entity.WorkflowVersion) { v.Steps[0].ExpectedResult = strings.Repeat("я", 1001) }},
		{"expected_invalid_utf8", "STEPS_EXPECTED_RESULT", func(v *entity.WorkflowVersion) { v.Steps[0].ExpectedResult = "private\xff" }},
		{"name_overflow", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Name = strings.Repeat("я", 161) }},
		{"root_invalid_utf8", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Instructions = "private\xff" }},
		{"step_key_invalid_utf8", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Steps[0].Key = "step\xff" }},
		{"unknown_dependency", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Steps[0].DependsOn = []string{"step-missing"} }},
		{"duplicate_step", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Steps[1].Key = v.Steps[0].Key }},
		{"gate_missing_decision", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Steps[1].GateDecisions = nil }},
		{"unsafe_capability", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Steps[0].RequiredCapabilityKeys = []string{"crm.read;drop"} }},
		{"invalid_timeout", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.TimeoutSeconds = 0 }},
		{"byte_budget", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) { v.Instructions = strings.Repeat("я", 32768) }},
		{"input_label_invalid_utf8", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) {
			v.Inputs = []entity.WorkflowInputField{{Key: "field-001", Label: "private\xff", Type: "TEXT"}}
		}},
		{"input_duplicate", "WORKFLOW_INVARIANTS", func(v *entity.WorkflowVersion) {
			v.Inputs = []entity.WorkflowInputField{{Key: "field-001", Label: "A", Type: "TEXT"}, {Key: "field-001", Label: "B", Type: "TEXT"}}
		}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			v := validWorkflowFixture()
			sample.mutate(&v)
			if got := workflowVersionInvalidField(v); got != sample.reason {
				t.Fatalf("closed field = %q, want %q", got, sample.reason)
			}
			if validWorkflowVersion(v) != (sample.reason == "") {
				t.Fatal("validator and closed reason disagree")
			}
		})
	}
}

func TestWorkflowUnicodeCountsCodepointsNotGraphemes(t *testing.T) {
	t.Parallel()
	v := validWorkflowFixture()
	v.Steps[0].Instructions = strings.Repeat("я\u0301", 500)
	if !validWorkflowVersion(v) {
		t.Fatal("1000 codepoints rejected")
	}
	v.Steps[0].Instructions += "\u0301"
	if validWorkflowVersion(v) {
		t.Fatal("1001 codepoints accepted")
	}
}

func TestWorkflowUnicodeUpdateHydrationAndCommand(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		name, text string
		valid      bool
	}{
		{"native_shaped", strings.Repeat("a", 693) + strings.Repeat("я", 244), true},
		{"emoji_boundary", strings.Repeat("😀", 1000), true},
		{"unicode_overflow", strings.Repeat("я", 1001), false},
		{"invalid_utf8", "private\xff", false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			previous := validWorkflowFixture()
			fields, steps := assistantWorkflowGraphFields(previous)
			before := map[string]any{
				"workflowRef": "wfl_12345678", "projectRef": "prj_12345678",
				"name": previous.Name, "purpose": previous.Purpose, "instructions": previous.Instructions,
				"coordinatorAgentRef": previous.CoordinatorAgentRef, "completionCriteria": previous.CompletionCriteria,
				"inputFields": fields, "steps": steps, "maxConcurrency": float64(previous.Concurrency),
				"timeoutSeconds": float64(previous.TimeoutSeconds), "draft": previous,
			}
			_, proposed := assistantWorkflowGraphFields(previous)
			proposed[0].(map[string]any)["purpose"] = sample.text
			operation, err := hydrateAssistantWorkflowFields(before, 7, entity.AssistantPlanOperation{
				Type: "UPDATE_WORKFLOW", Key: "unicode-update", Title: "Обновить Workflow", Summary: "Обновить Unicode-инструкции", Parameters: map[string]any{"workflowRef": "wfl_12345678", "steps": proposed},
			})
			if !sample.valid {
				if !errors.Is(err, errs.ErrInvalid) {
					t.Fatalf("invalid Unicode update was not rejected: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("valid Unicode update rejected: %v", err)
			}
			operation, err = normalizeAssistantOperation(operation)
			if err != nil {
				t.Fatalf("valid Unicode normalization rejected: %v", err)
			}
			mapped, err := assistantOperationCommand(operation)
			if err != nil {
				t.Fatalf("valid Unicode command rejected: %v", err)
			}
			payload := mapped.Payload.(command.WorkflowInput)
			if payload.Draft == nil || payload.Draft.Steps[0].Instructions != sample.text {
				t.Fatal("Unicode instructions changed in command")
			}
			if payload.Draft.Steps[1].DependsOn[0] != previous.Steps[1].DependsOn[0] || !payload.Draft.Steps[1].HumanGateAfter {
				t.Fatal("graph or gate changed in Unicode update")
			}
		})
	}
}
