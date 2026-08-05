package output

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
)

func TestRecoveryJournalRejectsCorruptionBeforeUse(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".matter-codex/recovery"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("полный итог")
	journal := RecoveryJournal{TurnID: uuid.NewString(), SourceExecutionID: uuid.NewString(),
		ArchiveExecutionID: uuid.NewString(), SourceAttempt: 1,
		OriginalOutcome: "SUCCEEDED", TerminalMarkdown: "Итог", Failed: []RecoveryItem{{
			Kind: "FILE", Name: "full-result.md", MediaType: "text/markdown", SHA256: digestBytesForTest(payload),
			SizeBytes: uint64(len(payload)), Sequence: 1, Total: 1, InlinePayload: payload,
		}}}
	input := model.Input{WorkspaceRoot: root, ExecutionID: uuid.NewString()}
	if err := SaveRecovery(input, journal); err != nil {
		t.Fatalf("SaveRecovery() error = %v", err)
	}
	loaded, ok, err := LoadRecovery(input)
	if err != nil || !ok || loaded.TurnID != journal.TurnID || loaded.SourceAttempt != 1 {
		t.Fatalf("LoadRecovery() = %#v, %v, %v", loaded, ok, err)
	}
	path := filepath.Join(root, recoveryRelativePath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), `"source_attempt":1`, `"source_attempt":2`, 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadRecovery(input); err == nil {
		t.Fatal("corrupted recovery journal was accepted")
	}
}

func TestRecoveryRequiresExactServerOwnedExecutionMarker(t *testing.T) {
	input := model.Input{TurnID: uuid.NewString(), Attempt: 2}
	source := uuid.NewString()
	journal := RecoveryJournal{TurnID: input.TurnID, SourceExecutionID: source,
		ArchiveExecutionID: uuid.NewString(), SourceAttempt: 1}
	if err := AuthorizeRecovery(input, journal, true); err == nil {
		t.Fatal("локальный journal сам назначил delivery recovery")
	}
	input.CodexDeliveryRecoverySourceExecutionID = source
	if err := AuthorizeRecovery(input, RecoveryJournal{}, false); !errors.Is(err, ErrRecoveryJournalUnavailable) {
		t.Fatal("server marker без journal допустил повтор provider")
	}
	if err := AuthorizeRecovery(input, journal, true); err != nil {
		t.Fatalf("exact delivery recovery отклонён: %v", err)
	}
	input.Attempt = 3
	if err := AuthorizeRecovery(input, journal, true); err != nil {
		t.Fatalf("journal после owner-visible missing attempt отклонён: %v", err)
	}
	if journal.ArchiveExecutionID == journal.SourceExecutionID {
		t.Fatal("delivery execution подменил provenance исходного provider execution")
	}
	journal.SourceExecutionID = uuid.NewString()
	if err := AuthorizeRecovery(input, journal, true); err == nil {
		t.Fatal("journal другого execution принят")
	}
}

func TestTerminalOnlyDoesNotRequireOutbox(t *testing.T) {
	input := model.Input{ExecutionID: uuid.NewString(), MattermostPostMaximumRunes: 16383}
	result, err := TerminalOnly(input, "Доставка не восстановлена.")
	if err != nil || len(result.Outputs) != 1 || result.Outputs[0].Kind != "FINAL_MARKDOWN" || len(result.Failed) != 0 {
		t.Fatalf("TerminalOnly() = %#v, %v", result, err)
	}
}

func digestBytesForTest(raw []byte) string {
	return makeRecoveryItem(pendingOutput{payload: raw}, 1, 1).SHA256
}
