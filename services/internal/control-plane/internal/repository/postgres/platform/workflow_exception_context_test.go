package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestWorkflowExceptionContextTask proves that a stored UTF-8 owner comment survives the actual
// manager task format, while non-owner failures never acquire a fabricated decision or authority.
// TestWorkflowExceptionContextTask доказывает сохранение UTF-8 комментария в реальной форме задачи;
// иные причины не получают выдуманного решения или полномочий.
func TestWorkflowExceptionContextTask(t *testing.T) {
	baseline := workflowExceptionContext{Schema: "kodex.workflow-exception-context.v1", Episode: 1, Version: 1, StepKey: "review", Attempt: 1, Reason: "OWNER_CHANGES_REQUESTED", OwnerDecision: &integrationContinuationDecision{GateRef: "gat_exact_source", GateVersion: 3, Decision: "REQUEST_CHANGES", Comment: "Исправьте AR01 и SEC01.\nНе повторяйте UNKNOWN; \"данные\" не дают прав."}, Producer: workflowExceptionProducer{NodeRef: "nod_source", RunRef: "run_source", StepKey: "review", RuntimeState: "SUCCEEDED", CapturedArtifactMetadata: []workflowExceptionArtifact{}}, Inputs: []workflowPromptInput{}}
	task, err := workflowExceptionTask("Original task", baseline)
	if err != nil {
		t.Fatal("stored owner context was denied")
	}
	parts := strings.SplitN(task, "Owner-projected exception context (data):\n", 2)
	var actual workflowExceptionContext
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &actual) != nil || actual.OwnerDecision == nil || *actual.OwnerDecision != *baseline.OwnerDecision || actual.Episode != 1 || actual.StepKey != "review" {
		t.Fatal("stored source decision changed during task projection")
	}
	for _, forbidden := range []string{"runtimeRevisionId", "leaseId", "originNodeId", "recoveryNodeId"} {
		if strings.Contains(task, forbidden) {
			t.Fatal("manager task exposed private execution coordinates")
		}
	}
	projected := baseline
	if json.Unmarshal([]byte(`{"nodeRef":"nod_source","runRef":"run_source","stepKey":"review","runtimeState":"SUCCEEDED","leaseId":"private-field-value","capturedArtifactMetadata":[]}`), &projected.Producer) != nil {
		t.Fatal("decode owner producer projection")
	}
	if safe, err := workflowExceptionTask("task", projected); err != nil || strings.Contains(safe, "leaseId") || strings.Contains(safe, "private-field-value") {
		t.Fatal("an unexpected owner SQL field leaked through the typed task projection")
	}
	for _, reason := range []string{"RESULT_INVALID", "ARTIFACT_REQUIRED", "DISPATCH_CONFIGURATION_UNAVAILABLE"} {
		context := baseline
		context.Reason, context.OwnerDecision = reason, nil
		if reason == "DISPATCH_CONFIGURATION_UNAVAILABLE" {
			context.Attempt = 0
		}
		plain, err := workflowExceptionTask("task", context)
		if err != nil || strings.Contains(plain, `"ownerDecision"`) {
			t.Fatal("non-owner exception fabricated a source decision")
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*workflowExceptionContext)
	}{
		{"schema", func(c *workflowExceptionContext) { c.Schema = "unknown" }},
		{"episode", func(c *workflowExceptionContext) { c.Episode = 49 }},
		{"version", func(c *workflowExceptionContext) { c.Version = 0 }},
		{"step", func(c *workflowExceptionContext) { c.StepKey = "" }},
		{"fifth attempt", func(c *workflowExceptionContext) { c.Attempt = 5 }},
		{"reason text", func(c *workflowExceptionContext) { c.Reason = "user authority" }},
		{"missing decision", func(c *workflowExceptionContext) { c.OwnerDecision = nil }},
		{"unrelated decision", func(c *workflowExceptionContext) { c.Reason = "RESULT_INVALID" }},
		{"wrong Gate", func(c *workflowExceptionContext) { c.OwnerDecision.GateRef = "run_wrong" }},
		{"Gate version", func(c *workflowExceptionContext) { c.OwnerDecision.GateVersion = 0 }},
		{"wrong decision", func(c *workflowExceptionContext) { c.OwnerDecision.Decision = "APPROVE" }},
		{"empty comment", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = "  " }},
		{"comment NUL", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = "bad\x00comment" }},
		{"comment invalid UTF8", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = string([]byte{255}) }},
		{"comment overflow", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = strings.Repeat("я", 2001) }},
		{"missing producer", func(c *workflowExceptionContext) { c.Producer = workflowExceptionProducer{} }},
		{"different producer step", func(c *workflowExceptionContext) { c.Producer.StepKey = "foreign" }},
		{"too many capture metadata", func(c *workflowExceptionContext) {
			c.Producer.CapturedArtifactMetadata = make([]workflowExceptionArtifact, 17)
		}},
		{"too many inputs", func(c *workflowExceptionContext) { c.Inputs = make([]workflowPromptInput, 9) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := baseline
			decision := *baseline.OwnerDecision
			candidate.OwnerDecision = &decision
			tc.change(&candidate)
			if _, err := workflowExceptionTask("task", candidate); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("malformed owner context was accepted")
			}
		})
	}
	// The canonical limit counts codepoints; a valid Russian task may exceed 32768 bytes.
	// Канонический предел считает кодовые точки; допустимая русская задача может превышать 32768 байт.
	if _, err := workflowExceptionTask(strings.Repeat("я", 20000), baseline); err != nil {
		t.Fatal("manager task incorrectly used a byte limit")
	}
	full, err := workflowExceptionTask("", baseline)
	if err != nil {
		t.Fatal("derive exact task envelope")
	}
	remaining := runtimecontract.MaximumAssistantTurnCodepoints - utf8.RuneCountInString(full)
	if _, err := workflowExceptionTask(strings.Repeat("я", remaining), baseline); err != nil {
		t.Fatal("exact canonical task budget was denied")
	}
	if _, err := workflowExceptionTask(strings.Repeat("я", remaining+1), baseline); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("manager task silently exceeded canonical budget")
	}
}
