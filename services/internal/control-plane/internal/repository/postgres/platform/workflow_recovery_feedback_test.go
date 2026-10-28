package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestWorkflowRecoveryTask preserves exact public feedback for first materialization and attempts
// two through four, rejecting malformed decisions, private-looking fields and oversized task output.
// TestWorkflowRecoveryTask сохраняет точные публичные пояснения для первой материализации и попыток
// два–четыре, отклоняя неверные решения и переполнение задачи без усечения комментария.
func TestWorkflowRecoveryTask(t *testing.T) {
	baseline := workflowRecoveryFeedback{Schema: "kodex.workflow-recovery-feedback.v1", Episode: 1, EpisodeVersion: 3, StepKey: "work", PreviousAttempt: 1, NextAttempt: 2, Reason: workflowArtifactRequired, OwnerDecision: integrationContinuationDecision{GateRef: "gat_feedback", GateVersion: 2, Decision: "APPROVE", Comment: "Исправить \"файл\".\nНе повторять UNKNOWN."}, ManagerAnalysis: "The required material is absent."}
	for previous := int32(0); previous < 4; previous++ {
		feedback := baseline
		feedback.PreviousAttempt = previous
		feedback.NextAttempt = previous + 1
		if previous == 0 {
			feedback.ManagerAnalysis = ""
			feedback.Reason = "DISPATCH_CONFIGURATION_UNAVAILABLE"
		}
		task, err := workflowRecoveryTask("Original task", feedback)
		if err != nil {
			t.Fatal("valid owner feedback denied", previous)
		}
		raw := strings.SplitN(task, "Owner-approved recovery feedback (data):\n", 2)
		var decoded workflowRecoveryFeedback
		if len(raw) != 2 || json.NewDecoder(strings.NewReader(raw[1])).Decode(&decoded) != nil || !reflect.DeepEqual(feedback, decoded) {
			t.Fatal("public feedback changed during task serialization")
		}
		if strings.Contains(task, "leaseId") || strings.Contains(task, "runtimeRevisionId") || strings.Contains(task, "originNodeId") {
			t.Fatal("private provenance leaked into recovery task")
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*workflowRecoveryFeedback)
	}{
		{"schema", func(f *workflowRecoveryFeedback) { f.Schema = "unknown" }},
		{"episode", func(f *workflowRecoveryFeedback) { f.Episode = 49 }},
		{"version", func(f *workflowRecoveryFeedback) { f.EpisodeVersion = 0 }},
		{"step", func(f *workflowRecoveryFeedback) { f.StepKey = "" }},
		{"fifth attempt", func(f *workflowRecoveryFeedback) { f.PreviousAttempt = 4; f.NextAttempt = 5 }},
		{"attempt gap", func(f *workflowRecoveryFeedback) { f.NextAttempt = 3 }},
		{"reason text", func(f *workflowRecoveryFeedback) { f.Reason = "untrusted diagnosis" }},
		{"gate", func(f *workflowRecoveryFeedback) { f.OwnerDecision.GateRef = "run_not_gate" }},
		{"gate version", func(f *workflowRecoveryFeedback) { f.OwnerDecision.GateVersion = 0 }},
		{"rejected", func(f *workflowRecoveryFeedback) { f.OwnerDecision.Decision = "REJECT" }},
		{"comment nul", func(f *workflowRecoveryFeedback) { f.OwnerDecision.Comment = "bad\x00text" }},
		{"comment invalid utf8", func(f *workflowRecoveryFeedback) { f.OwnerDecision.Comment = string([]byte{255}) }},
		{"comment overflow", func(f *workflowRecoveryFeedback) { f.OwnerDecision.Comment = strings.Repeat("я", 2001) }},
		{"analysis overflow", func(f *workflowRecoveryFeedback) { f.ManagerAnalysis = strings.Repeat("я", 1501) }},
		{"analysis nul", func(f *workflowRecoveryFeedback) { f.ManagerAnalysis = "bad\x00text" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			feedback := baseline
			tc.change(&feedback)
			if _, err := workflowRecoveryTask("task", feedback); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("malformed public feedback was accepted")
			}
		})
	}
	if _, err := workflowRecoveryTask(strings.Repeat("x", 64<<10), baseline); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("feedback silently truncated oversized task")
	}
}
