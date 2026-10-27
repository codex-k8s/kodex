package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/jackc/pgx/v5"
)

// continuationTransitionTx embeds an unimplemented transaction and supplies only a prior-runtime
// snapshot query from stored test bytes. Other transaction operations are unsupported; it is not a
// concurrency or owner-boundary database fixture.
// continuationTransitionTx встраивает нереализованную transaction и поддерживает только
// prior-runtime snapshot query по test bytes. Другие transaction operations не поддерживаются; это
// не DB fixture конкуренции или owner boundary.
type continuationTransitionTx struct {
	pgx.Tx
	previous []byte
}

// TestRuntimeEligibilityReasonClosedAndRedacted checks known continuation reasons, UNKNOWN
// fallback, fixed public error text and wrapped-cause/class preservation. Infrastructure errors
// and cancellation must not become terminal candidate ineligibility; private cause strings are not
// public diagnostics.
// TestRuntimeEligibilityReasonClosedAndRedacted проверяет known continuation reasons, UNKNOWN
// fallback, fixed public error text и сохранение wrapped cause/class. Infrastructure errors и
// cancellation не должны становиться terminal candidate ineligibility; private cause strings не
// являются public diagnostics.
func TestRuntimeEligibilityReasonClosedAndRedacted(t *testing.T) {
	for _, reason := range []string{"RUNTIME_CONTEXT", "SESSION_RESUME", "CONTINUATION_PREVIOUS_READ", "CONTINUATION_PREVIOUS_JSON", "CONTINUATION_PREVIOUS_DEPENDENCIES", "CONTINUATION_CURRENT_DEPENDENCIES", "CONTINUATION_DIFF", "CONTINUATION_SNAPSHOT", "CONTINUATION_TEMPLATE_READ", "CONTINUATION_TEMPLATE_BYTES", "CONTINUATION_TEMPLATE_DIGEST", "CONTINUATION_DIFF_JSON", "CONTINUATION_BINDING", "CONTINUATION_WORKFLOW_CONTEXT", "CONTINUATION_TASK_PROJECTION", "CONTINUATION_MATERIALIZATION", "CONTINUATION_INCOMPLETE", "CONTINUATION_NOTICE_BYTES", "CONTINUATION_SESSION_CONTEXT", "private-data"} {
		cause := fmt.Errorf("private-source: %w", errs.ErrConflict)
		diagnostic := runtimeEligibilityDiagnostic(reason, cause)
		wrapped := fmt.Errorf("producer: %w", diagnostic)
		want := reason
		if reason == "private-data" {
			want = "UNKNOWN"
		}
		if runtimeEligibilityReason(wrapped) != want || diagnostic.Error() != "runtime eligibility validation failed" ||
			!errors.Is(diagnostic, cause) || runtimeEligibilityErrorClass(wrapped) != "CONFLICT" || !runtimeCandidateEligibilityFailure(wrapped) {
			t.Fatal("diagnostic leaked data or changed closed lifecycle")
		}
	}
	if runtimeEligibilityReason(errors.New("private-source")) != "UNKNOWN" ||
		runtimeCandidateEligibilityFailure(runtimeEligibilityDiagnostic("CONTINUATION_PREVIOUS_READ", errs.ErrUnavailable)) ||
		runtimeCandidateEligibilityFailure(runtimeEligibilityDiagnostic("SESSION_RESUME", context.Canceled)) {
		t.Fatal("unknown data or infrastructure became a terminal eligibility outcome")
	}
}

// TestContinuationDiagnosticRejectsBeforePartialAppend injects seven prior/current dependency,
// diff, binding, size and history failures, requiring the exact closed reason and unchanged
// serialized current context. Unsupported locale is deliberately rejected by binding before
// materialization; no partial notice may be appended.
// TestContinuationDiagnosticRejectsBeforePartialAppend добавляет семь prior/current dependency,
// diff, binding, size и history failures, требуя exact closed reason и неизменного serialized
// current context. Unsupported locale намеренно отклоняется binding до materialization; partial
// notice не добавляется.
func TestContinuationDiagnosticRejectsBeforePartialAppend(t *testing.T) {
	for _, test := range []struct{ mutation, want string }{
		{"previous-json", "CONTINUATION_PREVIOUS_JSON"},
		{"previous-dependencies", "CONTINUATION_PREVIOUS_DEPENDENCIES"},
		{"current-dependencies", "CONTINUATION_CURRENT_DEPENDENCIES"},
		{"diff", "CONTINUATION_DIFF"},
		{"materialization", "CONTINUATION_MATERIALIZATION"},
		{"notice-bytes", "CONTINUATION_NOTICE_BYTES"},
		{"session-context", "CONTINUATION_SESSION_CONTEXT"},
	} {
		t.Run(test.mutation, func(t *testing.T) {
			prompt := workflowPromptFixture()
			prompt.TargetKind, prompt.TargetRef = promptservice.TargetAgent, "agt_current"
			prompt.RunRef, prompt.SessionRef = "run_current", "ses_current"
			prompt.Variables["node.ref"], prompt.Variables["turn.ref"], prompt.Variables["task"] = "nod_current", "trn_current", "Exact original task"
			previous, _ := json.Marshal(map[string]any{"promptSnapshot": prompt, "capabilities": []string{"platform.run.delegate"}})
			if bindPlatformInstructions(&prompt) != nil {
				t.Fatal("bind")
			}
			current := map[string]any{"promptSnapshot": prompt, "runRef": "run_current", "nodeRef": "nod_current", "task": prompt.Variables["task"], "runtimeRevisionRef": "rrev_current", "sessionRef": "ses_current", "turnRef": "trn_current", "attempt": int32(7), "agentRef": "agt_current",
				"promptServiceTemplateRevision": promptservice.ServiceTemplateRevision, "capabilities": []string{"platform.run.delegate"}, "sessionContext": []map[string]string{{"role": "USER", "content": "Original"}}}
			switch test.mutation {
			case "previous-json":
				previous = []byte("{")
			case "previous-dependencies":
				previous = []byte(`{"promptSnapshot":{"platformInstructions":{"Ref":"forged"}}}`)
			case "current-dependencies":
				bad := prompt
				bad.SemanticValues = map[string]string{"CONSTRAINTS": "MISSING_PROJECTION"}
				current["promptSnapshot"] = bad
			case "diff":
				current["runtimeRevisionRef"] = "rrev_previous"
			case "materialization":
				prompt.Locale = "unsupported"
				// Binding guard отклонит этот locale раньше materializer.
				test.want = "CONTINUATION_BINDING"
			case "notice-bytes":
				prompt.StructuredVariables["input"].(map[string]any)["values"] = map[string]any{"exact": strings.Repeat("x", runtimecontract.MaximumContinuationNoticeBytes)}
			case "session-context":
				current["sessionContext"] = []map[string]string{{"role": "USER", "content": strings.Repeat("x", runtimecontract.MaximumAssistantTurnCodepoints+1)}}
			}
			before, _ := json.Marshal(current)
			var repository Repository
			notice, err := repository.prepareRuntimeContinuationNotice(t.Context(), continuationTransitionTx{previous: previous}, scope{}, current, prompt)
			after, _ := json.Marshal(current)
			if notice != nil || !errors.Is(err, errs.ErrConflict) || runtimeEligibilityReason(err) != test.want || string(before) != string(after) {
				t.Fatalf("rejection reason/lifecycle changed: got=%s want=%s", runtimeEligibilityReason(err), test.want)
			}
		})
	}
}

// continuationTransitionRow holds optional synthetic previous snapshot bytes; nil distinguishes no
// row from a present snapshot. It has no independent version, tenant or authority validation.
// continuationTransitionRow хранит optional synthetic previous snapshot bytes; nil отличает
// отсутствие row от присутствующего snapshot. Самостоятельные version, tenant либо authority
// validation отсутствуют.
type continuationTransitionRow struct {
	previous []byte
}

// QueryRow returns prior snapshot bytes only for the exact previous-continuation SQL and an empty
// row otherwise. It intentionally ignores context/arguments and cannot prove the production query
// owner or lease predicates.
// QueryRow возвращает prior snapshot bytes только для exact previous-continuation SQL и empty row
// иначе. Намеренно игнорирует context/arguments и не доказывает production query owner либо lease
// predicates.
func (tx continuationTransitionTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if query == queryPromptContinuationPrevious {
		return continuationTransitionRow{previous: tx.previous}
	}
	return continuationTransitionRow{}
}

// Scan returns ErrNoRows for nil prior bytes, otherwise writes fixed previous ID/ref and snapshot
// bytes into expected destinations. It assumes the three destination types and performs no backend
// I/O.
// Scan возвращает ErrNoRows для nil prior bytes, иначе пишет fixed previous ID/ref и snapshot
// bytes в expected destinations. Полагается на три destination types и не выполняет backend I/O.
func (row continuationTransitionRow) Scan(dest ...any) error {
	if row.previous == nil {
		return pgx.ErrNoRows
	}
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*[]byte) = "previous-id", "rrev_previous", row.previous
	return nil
}

// TestContinuationHistoricalToFreshPlatformBinding checks both locales when a historical snapshot
// acquires a fresh platform binding: only INSTRUCTIONS may differ, prior USER/ASSISTANT history
// remains exact, and the shared consumer accepts the appended notice. It tests local transition
// materialization, not persisted lease renewal or live rejoin.
// TestContinuationHistoricalToFreshPlatformBinding проверяет обе locale при добавлении fresh
// platform binding к historical snapshot: меняется только INSTRUCTIONS, прежняя USER/ASSISTANT
// history остаётся точной, shared consumer принимает добавленный notice. Проверяет local
// transition materialization, а не persisted lease renewal либо live rejoin.
func TestContinuationHistoricalToFreshPlatformBinding(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			prompt := workflowPromptFixture()
			prompt.Locale, prompt.TargetKind, prompt.TargetRef = locale, promptservice.TargetAgent, "agt_current"
			prompt.RunRef, prompt.SessionRef = "run_current", "ses_current"
			prompt.Variables["node.ref"], prompt.Variables["turn.ref"] = "nod_current", "trn_current"
			prompt.Variables["task"] = "Exact original task"
			previous := map[string]any{"promptSnapshot": prompt, "capabilities": []string{"platform.run.delegate"}}
			raw, _ := json.Marshal(previous)
			if bindPlatformInstructions(&prompt) != nil {
				t.Fatal("fresh bind")
			}
			current := map[string]any{"promptSnapshot": prompt, "runRef": "run_current", "nodeRef": "nod_current", "task": prompt.Variables["task"], "runtimeRevisionRef": "rrev_current", "sessionRef": "ses_current", "turnRef": "trn_current", "attempt": int32(7), "agentRef": "agt_current",
				"promptServiceTemplateRevision": promptservice.ServiceTemplateRevision, "capabilities": []string{"platform.run.delegate"},
				"sessionContext": []map[string]string{{"role": "USER", "content": "Exact original task"}, {"role": "ASSISTANT", "content": "Exact historical result"}}}
			history, _ := json.Marshal(current["sessionContext"])
			var repository Repository
			notice, err := repository.prepareRuntimeContinuationNotice(t.Context(), continuationTransitionTx{previous: raw}, scope{}, current, prompt)
			if err != nil || notice == nil {
				t.Fatal("historical to fresh continuation rejected", err)
			}
			assertPlatformInstructions(t, notice.Snapshot)
			if len(notice.Diff.Changes) != 1 || notice.Diff.Changes[0].Component != "INSTRUCTIONS" || !reflect.DeepEqual(notice.Snapshot.PlatformInstructions, prompt.PlatformInstructions) {
				t.Fatal("fresh binding or sole changed dependency lost")
			}
			messages := current["sessionContext"].([]map[string]string)
			retained, _ := json.Marshal(messages[:len(messages)-1])
			if string(history) != string(retained) {
				t.Fatal("history changed")
			}
			input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, RuntimeRevisionRef: "rrev_current", SessionRef: "ses_current", TurnRef: "trn_current", Attempt: 7,
				Capabilities: []string{"platform.run.delegate"}, PromptServiceTemplateRevision: promptservice.ServiceTemplateRevision, SessionContext: runtimeRevisionSessionContext(messages)}
			if found, err := runtimecontract.ValidateRuntimeSessionContext(input); err != nil || !found {
				t.Fatal("unchanged consumer rejected full fresh notice", err)
			}
		})
	}
}
