package output

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
)

func TestScheduledOutcomeClosedDocument(t *testing.T) {
	root := t.TempDir()
	input := model.Input{ScheduleOccurrenceID: uuid.NewString(), OutboxRoot: root}
	if got, err := ScheduledOutcome(input, "SUCCEEDED"); err != nil || got != "action_taken" {
		t.Fatalf("default scheduled outcome = %q, err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, scheduledResultFile),
		[]byte(`{"outcome":"requires_human"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ScheduledOutcome(input, "SUCCEEDED"); err != nil || got != "requires_human" {
		t.Fatalf("requires_human outcome = %q, err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, scheduledResultFile),
		[]byte(`{"outcome":"arbitrary"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScheduledOutcome(input, "SUCCEEDED"); err == nil {
		t.Fatal("arbitrary scheduled outcome was accepted")
	}
}
