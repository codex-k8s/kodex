package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
)

func scheduledInput(t *testing.T) model.Input {
	t.Helper()
	return model.Input{ScheduleOccurrenceID: uuid.NewString(), OutboxRoot: t.TempDir(),
		ScheduledResultContract: &model.ScheduledResultContract{
			Schema: model.ScheduledResultSchemaV1, Path: model.ScheduledResultPathV1,
			Format: model.ScheduledResultFormatV1, SchemaSHA256: model.ScheduledResultSHA256V1,
			MaximumBytes: model.ScheduledResultMaxBytes,
		}}
}

func writeScheduledResult(t *testing.T, input model.Input, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(input.OutboxRoot, filepath.Base(input.ScheduledResultContract.Path)),
		[]byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledOutcomeRequiresVersionedClosedDocument(t *testing.T) {
	input := scheduledInput(t)
	if _, err := ScheduledOutcome(input, "SUCCEEDED"); err == nil {
		t.Fatal("missing scheduled outcome was accepted")
	}
	writeScheduledResult(t, input,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"no_action","summary":"Пусто.","artifact_refs":[]}`)
	if got, err := ScheduledOutcome(input, "SUCCEEDED"); err != nil || got != "no_action" {
		t.Fatalf("no_action outcome = %q, err=%v", got, err)
	}
	writeScheduledResult(t, input,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"requires_human","summary":"Нужно решение.","artifact_refs":["report.md"]}`)
	if got, err := ScheduledOutcome(input, "SUCCEEDED"); err != nil || got != "requires_human" {
		t.Fatalf("requires_human outcome = %q, err=%v", got, err)
	}
}

func TestScheduledOutcomeFailsClosedForMalformedDocuments(t *testing.T) {
	input := scheduledInput(t)
	invalid := []string{
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"action_taken","outcome":"no_action","summary":"x","artifact_refs":[]}`,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"arbitrary","summary":"x","artifact_refs":[]}`,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"action_taken","summary":"x","artifact_refs":[],"room_id":"forbidden"}`,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"action_taken","summary":"x"}`,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"action_taken","summary":"x","artifact_refs":["../secret"]}`,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"action_taken","summary":"` + strings.Repeat("x", 2001) + `","artifact_refs":[]}`,
	}
	for index, raw := range invalid {
		writeScheduledResult(t, input, raw)
		if _, err := ScheduledOutcome(input, "SUCCEEDED"); err == nil {
			t.Fatalf("invalid document %d was accepted", index)
		}
	}
	writeScheduledResult(t, input,
		`{"schema":"mattercodex.scheduled-result.v1","outcome":"no_action","summary":"x","artifact_refs":[]}`)
	if _, err := ScheduledOutcome(input, "FAILED"); err == nil {
		t.Fatal("non-failed scheduled outcome was accepted for failed runtime")
	}
	writeScheduledResult(t, input, strings.Repeat("x", model.ScheduledResultMaxBytes+1))
	if _, err := ScheduledOutcome(input, "SUCCEEDED"); err == nil {
		t.Fatal("oversized scheduled result was accepted")
	}
}
