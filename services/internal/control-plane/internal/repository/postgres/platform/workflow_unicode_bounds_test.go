package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestWorkflowSelfOnlyStagesCannotRequireUnavailableDelegation checks coordinator-only stages
// reject a required delegation capability without a distinct target while remaining structurally
// readable for repair. Adding a real distinct agent restores validity. This tests static
// executable-graph rules, not actual delegation permission or worker execution.
// TestWorkflowSelfOnlyStagesCannotRequireUnavailableDelegation проверяет отказ required delegation
// capability у coordinator-only stages без distinct target при сохранении structural readability
// для исправления. Добавление distinct agent восстанавливает validity. Проверяет static
// executable-graph rules, а не actual delegation permission и worker execution.
func TestWorkflowSelfOnlyStagesCannotRequireUnavailableDelegation(t *testing.T) {
	v := validWorkflowFixture()
	for i := range v.Steps {
		v.Steps[i].AgentRef = v.CoordinatorAgentRef
		v.Steps[i].RequiredCapabilityKeys = []string{"platform.artifact.manage"}
	}
	if !validWorkflowVersion(v) {
		t.Fatal("ordinary coordinator-owned stages rejected")
	}
	v.Steps[0].RequiredCapabilityKeys = []string{"platform.run.delegate"}
	if validWorkflowVersion(v) || workflowVersionInvalidField(v) != workflowInvalidInvariants {
		t.Fatal("self-only workflow claimed an unavailable delegation path")
	}
	if workflowVersionStructureInvalidField(v) != "" {
		t.Fatal("unexecutable workflow cannot be read for repair")
	}
	v.Steps[1].AgentRef = "agt_specialist_fixture"
	if !validWorkflowVersion(v) {
		t.Fatal("workflow with an actual distinct delegation target rejected")
	}
}

// TestWorkflowUnicodeTextBounds table-checks field-specific name/purpose/completion/step/input
// codepoint limits with ASCII, Cyrillic and emoji boundaries, overflow and invalid UTF-8. Each
// case uses a fresh valid graph; acceptance here does not bypass the separate aggregate byte
// budget or authorize publication.
// TestWorkflowUnicodeTextBounds таблично проверяет field-specific codepoint limits
// name/purpose/completion/step/input на ASCII, Cyrillic и emoji boundaries, overflow и invalid
// UTF-8. Каждый case использует fresh valid graph; acceptance не обходит separate aggregate byte
// budget и не разрешает публикацию.
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

// TestWorkflowUnicodeNativeShapedInstructions checks three mixed ASCII/Cyrillic instruction
// samples retain their expected byte lengths yet pass the codepoint-bounded validator. It guards
// prior byte-versus-codepoint confusion with synthetic native-shaped text, not delivered runtime
// prompts.
// TestWorkflowUnicodeNativeShapedInstructions проверяет expected byte lengths трёх mixed
// ASCII/Cyrillic instruction samples при прохождении codepoint-bounded validator. Защищает от
// byte-versus-codepoint confusion на synthetic native-shaped text, а не доставленных runtime
// prompts.
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

// TestWorkflowUnicodeAggregateRemainsByteBounded checks a one-step fixture reaches the exact
// combined64KiB text budget, rejects one extra byte and rejects malformed root UTF-8. It preserves
// aggregate byte bounds independently of per-field Unicode limits without storing or executing the
// workflow.
// TestWorkflowUnicodeAggregateRemainsByteBounded проверяет exact combined64KiB text budget
// one-step fixture, отказ дополнительного байта и malformed root UTF-8. Сохраняет aggregate byte
// bounds отдельно от per-field Unicode limits без сохранения и исполнения workflow.
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

// TestWorkflowUnicodeInvalidFieldIsClosedAndMatchesValidity checks malformed text, graph, inputs,
// capabilities, timeout and byte budget produce the expected closed diagnostic field and agree
// with validity. It uses fresh synthetic graphs and does not prove that arbitrary runtime failures
// are diagnosed or redacted by every transport.
// TestWorkflowUnicodeInvalidFieldIsClosedAndMatchesValidity проверяет expected closed diagnostic
// field и согласие с validity для malformed text, graph, inputs, capabilities, timeout и byte
// budget. Использует fresh synthetic graphs и не доказывает диагностику либо redaction
// произвольных runtime failures всеми transports.
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

// TestWorkflowUnicodeCountsCodepointsNotGraphemes checks500 base-plus-combining-mark pairs count
// as1000 codepoints and one extra combining mark exceeds the step limit. It distinguishes Unicode
// codepoints from grapheme clusters without normalizing or truncating user text.
// TestWorkflowUnicodeCountsCodepointsNotGraphemes проверяет учёт500 base-plus-combining-mark pairs
// как1000 codepoints и превышение step limit дополнительным combining mark. Различает Unicode
// codepoints и grapheme clusters без нормализации и усечения user text.
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

// TestWorkflowUnicodeUpdateHydrationAndCommand checks valid native-shaped/emoji text survives
// hydration, normalization and typed workflow mapping byte-for-byte, while overflow/invalid UTF-8
// returns Invalid. Original dependency and human-gate pins remain. This is an in-memory proposal
// path, not owner apply or runtime launch.
// TestWorkflowUnicodeUpdateHydrationAndCommand проверяет byte-for-byte сохранение valid
// native-shaped/emoji text через hydration, normalization и typed workflow mapping с Invalid для
// overflow/invalid UTF-8. Сохраняются original dependency и human-gate pins. Это proposal path в
// памяти, а не owner apply и runtime launch.
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
