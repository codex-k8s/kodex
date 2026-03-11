package runstatus

import (
	"testing"

	webhookdomain "github.com/codex-k8s/codex-k8s/libs/go/domain/webhook"
	nextstepdomain "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/nextstep"
)

func TestStageDescriptorFromTriggerKind_QAReviseUsesQAStage(t *testing.T) {
	t.Parallel()

	descriptor, ok := stageDescriptorFromTriggerKind(nextstepdomain.DefaultLabels(), string(webhookdomain.TriggerKindQARevise))
	if !ok {
		t.Fatalf("stageDescriptorFromTriggerKind() ok = false, want true")
	}
	if descriptor.Stage != "qa" {
		t.Fatalf("stageDescriptorFromTriggerKind().Stage = %q, want %q", descriptor.Stage, "qa")
	}
	if descriptor.ReviseLabel != webhookdomain.DefaultRunQAReviseLabel {
		t.Fatalf("stageDescriptorFromTriggerKind().ReviseLabel = %q, want %q", descriptor.ReviseLabel, webhookdomain.DefaultRunQAReviseLabel)
	}
}

func TestStageDescriptorFromTriggerKind_UsesConfiguredQALabels(t *testing.T) {
	t.Parallel()

	labels := nextstepdomain.NewLabels(nextstepdomain.Config{
		RunQA:       "run:quality-assurance",
		RunQARevise: "run:quality-assurance:revise",
	})

	descriptor, ok := stageDescriptorFromTriggerKind(labels, string(webhookdomain.TriggerKindQARevise))
	if !ok {
		t.Fatalf("stageDescriptorFromTriggerKind() ok = false, want true")
	}
	if descriptor.RunLabel != "run:quality-assurance" {
		t.Fatalf("stageDescriptorFromTriggerKind().RunLabel = %q, want %q", descriptor.RunLabel, "run:quality-assurance")
	}
	if descriptor.ReviseLabel != "run:quality-assurance:revise" {
		t.Fatalf("stageDescriptorFromTriggerKind().ReviseLabel = %q, want %q", descriptor.ReviseLabel, "run:quality-assurance:revise")
	}
}
