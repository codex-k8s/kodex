package runstatus

import (
	"testing"

	webhookdomain "github.com/codex-k8s/codex-k8s/libs/go/domain/webhook"
)

func TestStageDescriptorFromTriggerKind_QAReviseUsesQAStage(t *testing.T) {
	t.Parallel()

	descriptor, ok := stageDescriptorFromTriggerKind(string(webhookdomain.TriggerKindQARevise))
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
