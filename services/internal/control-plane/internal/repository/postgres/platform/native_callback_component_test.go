package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// nativeCallbackAuditCount reads the original completed callback without feeding historical turns to the model.
// nativeCallbackAuditCount читает исходный завершённый callback без повторной передачи истории модели.
//
//go:embed testdata/sql/native_callback_audit_count.sql
var nativeCallbackAuditCount string

// assertNativeCallbackContinuation verifies exact parent resume, new owner-pinned child input and retained original audit.
// assertNativeCallbackContinuation проверяет точное продолжение родителя, новый закреплённый ввод дочерних результатов и сохранённый исходный аудит.
func assertNativeCallbackContinuation(t *testing.T, ctx context.Context, repository *Repository, previous, continuation map[string]any, summaries ...string) {
	t.Helper()
	messages := runtimeRevisionSessionContext(continuation["sessionContext"])
	noticeInput := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn,
		RuntimeRevisionRef: stringMap(continuation, "runtimeRevisionRef"), SessionRef: stringMap(continuation, "sessionRef"),
		TurnRef: stringMap(continuation, "turnRef"), Attempt: int32(runtimeRevisionMapInt64(continuation, "attempt")),
		Capabilities: runtimeRevisionStringSlice(continuation["capabilities"]), PromptServiceTemplateRevision: stringMap(continuation, "promptServiceTemplateRevision"), SessionContext: messages}
	notice, err := runtimecontract.ValidateRuntimeSessionContext(noticeInput)
	if err != nil || len(messages) > 1 || len(messages) == 1 && !notice {
		t.Fatal("native continuation replayed history instead of a fresh sealed notice")
	}
	raw, err := json.Marshal(continuation["nativeSession"])
	checkpoint, decodeErr := runtimecontract.DecodeNativeSessionCheckpoint(raw)
	if err != nil || decodeErr != nil || checkpoint != *nativeCheckpointForClaimFixture(previous) {
		t.Fatal("callback continuation lost exact parent checkpoint")
	}
	parts := strings.SplitN(stringMap(continuation, "task"), "\n\n", 2)
	var input struct {
		CompletedChildren []struct {
			ResultSummary string `json:"resultSummary"`
		} `json:"completedChildren"`
	}
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &input) != nil || len(input.CompletedChildren) != len(summaries) {
		t.Fatal("callback continuation lost owner-pinned child input")
	}
	for _, summary := range summaries {
		count := 0
		for _, child := range input.CompletedChildren {
			if child.ResultSummary == summary {
				count++
			}
		}
		if count != 1 {
			t.Fatal("callback summary was omitted or duplicated in the new turn")
		}
		var stored int
		if err := repository.pool.QueryRow(ctx, nativeCallbackAuditCount, stringMap(continuation, "sessionRef"), summary).Scan(&stored); err != nil || stored != 1 {
			t.Fatal("original callback audit was omitted or duplicated", err)
		}
	}
}
