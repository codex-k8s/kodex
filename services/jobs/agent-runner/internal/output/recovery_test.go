package output

import (
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
	journal := RecoveryJournal{TurnID: uuid.NewString(), SourceExecutionID: uuid.NewString(), SourceAttempt: 1,
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

func digestBytesForTest(raw []byte) string {
	return makeRecoveryItem(pendingOutput{payload: raw}, 1, 1).SHA256
}
