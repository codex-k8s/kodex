package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/jackc/pgx/v5"
)

// TestContinuationLargeRussianTaskRemainsWhole builds a continuation from a long Cyrillic task and
// requires exact task/history preservation, bounded notice materialization and the queued-task
// interrupt warning. It uses a synthetic prior-row transaction and validates the platform
// instruction binding without contacting a runner.
// TestContinuationLargeRussianTaskRemainsWhole строит continuation из длинного кириллического task
// и требует exact task/history preservation, bounded notice materialization и предупреждение о
// queued-task interrupt. Использует synthetic prior-row transaction и проверяет platform
// instruction binding без обращения к runner.
func TestContinuationLargeRussianTaskRemainsWhole(t *testing.T) {
	prompt := workflowPromptFixture()
	prompt.Locale, prompt.TargetKind, prompt.TargetRef = "ru", promptservice.TargetAgent, "agt_current"
	prompt.RunRef, prompt.SessionRef = "run_current", "ses_current"
	prompt.Variables["node.ref"], prompt.Variables["turn.ref"] = "nod_current", "trn_current"
	task := strings.Repeat("Точная инструкция роли без усечения.\n", 1200)
	prompt.Variables["task"] = task
	previous, _ := json.Marshal(map[string]any{"promptSnapshot": prompt, "capabilities": []string{"platform.run.delegate"}})
	if bindPlatformInstructions(&prompt) != nil {
		t.Fatal("fresh bind")
	}
	current := map[string]any{"promptSnapshot": prompt, "task": task, "runtimeRevisionRef": "rrev_current", "runRef": prompt.RunRef, "nodeRef": "nod_current", "sessionRef": prompt.SessionRef, "turnRef": "trn_current", "attempt": int32(8), "agentRef": "agt_current",
		"promptServiceTemplateRevision": promptservice.ServiceTemplateRevision, "capabilities": []string{"platform.run.delegate"}, "sessionContext": []map[string]string{{"role": "USER", "content": "Полный исходный ввод"}}}
	before, _ := json.Marshal(current["sessionContext"])
	var repository Repository
	notice, err := repository.prepareRuntimeContinuationNotice(t.Context(), continuationTransitionTx{previous: previous}, scope{}, current, prompt)
	if err != nil || notice == nil {
		t.Fatalf("large exact task rejected before provider: %s", runtimeEligibilityReason(err))
	}
	assertPlatformInstructions(t, notice.Snapshot)
	if len(notice.Materialization.Prompt) > runtimecontract.MaximumContinuationNoticeBytes || current["task"] != task || notice.Snapshot.Variables["task"] != task ||
		!strings.Contains(notice.Snapshot.SemanticValues["PURPOSE"], continuationTaskProjectionRevision) ||
		!strings.Contains(notice.Snapshot.SemanticValues["PURPOSE"], "A queued task can execute after a newer interrupt message") {
		t.Fatal("notice lost exact task or exceeded unchanged bound")
	}
	messages := current["sessionContext"].([]map[string]string)
	after, _ := json.Marshal(messages[:len(messages)-1])
	if string(before) != string(after) {
		t.Fatal("history rewritten")
	}
}

// continuationTaskOracleFixture materializes a fresh platform-bound agent prompt and appends one
// continuation notice to synthetic USER/ASSISTANT history, returning a fully pinned RunnerInput.
// The exact Cyrillic/template-looking task remains data; the fixture does not create a durable
// runtime revision or grant.
// continuationTaskOracleFixture материализует свежий platform-bound agent prompt и добавляет один
// continuation notice в synthetic USER/ASSISTANT history, возвращая полный pinned RunnerInput.
// Exact Cyrillic/template-looking task остаётся данными; fixture не создаёт durable runtime
// revision либо grant.
func continuationTaskOracleFixture(t *testing.T) runtimecontract.RunnerInput {
	t.Helper()
	prompt := workflowPromptFixture()
	prompt.Locale, prompt.TargetKind, prompt.TargetRef = "ru", promptservice.TargetAgent, "agt_current"
	prompt.RunRef, prompt.SessionRef = "run_current", "ses_current"
	prompt.Variables["node.ref"], prompt.Variables["turn.ref"] = "nod_current", "trn_current"
	prompt.Variables["task"] = strings.Repeat("Exact full task: кириллица {{not a template}}.\n", 30)
	prior, _ := json.Marshal(map[string]any{"promptSnapshot": prompt, "capabilities": []string{"platform.run.delegate"}})
	if bindPlatformInstructions(&prompt) != nil {
		t.Fatal("fresh binding")
	}
	base, err := promptservice.Materialize("Owner instructions", promptservice.FromSnapshot(prompt))
	if err != nil {
		t.Fatal(err)
	}
	current := map[string]any{"task": prompt.Variables["task"], "promptSnapshot": prompt, "runtimeRevisionRef": "rrev_current", "runRef": prompt.RunRef, "nodeRef": "nod_current", "sessionRef": prompt.SessionRef,
		"turnRef": "trn_current", "attempt": int32(8), "agentRef": "agt_current", "capabilities": base.EffectiveCapabilities, "promptServiceTemplateRevision": base.ServiceTemplateRevision,
		"sessionContext": []map[string]string{{"role": "USER", "content": "Exact original USER"}, {"role": "ASSISTANT", "content": "Exact previous result"}}}
	var repository Repository
	if _, err := repository.prepareRuntimeContinuationNotice(t.Context(), continuationTransitionTx{previous: prior}, scope{}, current, prompt); err != nil {
		t.Fatal(err)
	}
	return runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, RuntimeRevisionRef: "rrev_current", RunRef: prompt.RunRef, NodeRef: "nod_current", SessionRef: prompt.SessionRef, TurnRef: "trn_current", Attempt: 8,
		Task: prompt.Variables["task"], Instructions: base.Prompt, PromptServiceTemplateRevision: base.ServiceTemplateRevision, PromptServiceTemplateDigest: base.ServiceTemplateDigest,
		PromptTargetKind: promptservice.TargetAgent, Capabilities: base.EffectiveCapabilities, SessionContext: runtimeRevisionSessionContext(current["sessionContext"])}
}

// TestContinuationTaskProducerMatchesRunnerOracle round-trips the fresh v5 producer input without
// change and validates it plus historical v2/v3/v4 synthetic notices through the shared consumer.
// The v4 file has a fixed byte digest; this is serialized producer/consumer compatibility, not
// execution by a live runner.
// TestContinuationTaskProducerMatchesRunnerOracle проверяет неизменный round-trip fresh v5
// producer input и его совместимость с historical v2/v3/v4 synthetic notices через shared
// consumer. V4 file имеет fixed byte digest; это serialized producer/consumer compatibility, а не
// execution live runner.
func TestContinuationTaskProducerMatchesRunnerOracle(t *testing.T) {
	actual := continuationTaskOracleFixture(t)
	if !strings.Contains(actual.Instructions, "Revision: platform-source-conformity-v5") {
		t.Fatal("fresh continuation omitted v5 instructions")
	}
	freshRaw, err := json.Marshal(actual)
	var fresh runtimecontract.RunnerInput
	if err != nil || json.Unmarshal(freshRaw, &fresh) != nil || !reflect.DeepEqual(actual, fresh) {
		t.Fatal("fresh v5 producer changed exact input on serialized rejoin")
	}
	raw, err := os.ReadFile("testdata/continuation_task_projection_v1_instructions_v4.json")
	var fixture runtimecontract.RunnerInput
	digest := sha256.Sum256(raw)
	if err != nil || hex.EncodeToString(digest[:]) != "5bd8d0b6f3d41032f0ac3a6356448caa32195b1a43d91021adaac0521ac5954e" || json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("historical v4 shared synthetic fixture is unavailable")
	}
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(fixture); err != nil || !notice {
		t.Fatal("historical v4 notice rejected by unchanged shared runner consumer")
	}
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(actual); err != nil || !notice {
		t.Fatal("exact fresh producer notice rejected by shared runner consumer")
	}
	historicalRaw, err := os.ReadFile("testdata/continuation_task_projection_v1.json")
	var historical runtimecontract.RunnerInput
	if err != nil || json.Unmarshal(historicalRaw, &historical) != nil {
		t.Fatal("historical v2 fixture is unavailable")
	}
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(historical); err != nil || !notice {
		t.Fatal("historical v2 notice rejected by unchanged shared runner consumer")
	}
	v3Raw, err := os.ReadFile("testdata/continuation_task_projection_v1_instructions_v3.json")
	var v3 runtimecontract.RunnerInput
	if err != nil || json.Unmarshal(v3Raw, &v3) != nil {
		t.Fatal("historical v3 fixture is unavailable")
	}
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(v3); err != nil || !notice {
		t.Fatal("historical v3 notice rejected by unchanged shared runner consumer")
	}
}

// TestContinuationTaskProjectionRejectsMismatchWithoutMutation mutates task and exact
// run/node/session/turn/revision/attempt pins, encoding, target or existing owner overlay and
// requires Conflict with byte-equivalent snapshot before/after. No rejected projection may replace
// owner data or partially bind a task.
// TestContinuationTaskProjectionRejectsMismatchWithoutMutation меняет task и exact
// run/node/session/turn/revision/attempt pins, encoding, target либо существующий owner overlay и
// требует Conflict с byte-equivalent snapshot до/после. Отклонённая projection не должна заменять
// owner data или частично связывать task.
func TestContinuationTaskProjectionRejectsMismatchWithoutMutation(t *testing.T) {
	for _, mutation := range []string{"task", "run", "node", "session", "turn", "revision", "attempt", "utf8", "nul", "empty", "target", "overlay"} {
		t.Run(mutation, func(t *testing.T) {
			prompt := workflowPromptFixture()
			prompt.TargetKind, prompt.RunRef, prompt.SessionRef = promptservice.TargetSessionContinuation, "run_current", "ses_current"
			prompt.Variables["task"], prompt.Variables["node.ref"], prompt.Variables["turn.ref"] = "Exact task", "nod_current", "trn_current"
			current := map[string]any{"task": "Exact task", "runRef": "run_current", "nodeRef": "nod_current", "sessionRef": "ses_current", "turnRef": "trn_current", "runtimeRevisionRef": "rrev_current", "attempt": int32(8)}
			switch mutation {
			case "task":
				current["task"] = "Different task"
			case "run":
				current["runRef"] = "run_other"
			case "node":
				current["nodeRef"] = "nod_other"
			case "session":
				current["sessionRef"] = "ses_other"
			case "turn":
				current["turnRef"] = "trn_other"
			case "revision":
				current["runtimeRevisionRef"] = "invalid/ref"
			case "attempt":
				current["attempt"] = int32(0)
			case "utf8":
				prompt.Variables["task"], current["task"] = string([]byte{255}), string([]byte{255})
			case "nul":
				prompt.Variables["task"], current["task"] = "a\x00b", "a\x00b"
			case "empty":
				prompt.Variables["task"], current["task"] = "", ""
			case "target":
				prompt.TargetKind = promptservice.TargetAgent
			case "overlay":
				prompt.SemanticValues = map[string]string{"PURPOSE": "Owner must not be silently replaced"}
			}
			before, _ := json.Marshal(prompt)
			if !errors.Is(bindRuntimeContinuationTaskProjection(&prompt, current), errs.ErrConflict) {
				t.Fatal("invalid task projection accepted")
			}
			after, _ := json.Marshal(prompt)
			if string(before) != string(after) {
				t.Fatal("rejection mutated prompt")
			}
		})
	}
}

// continuationCustomTaskTx extends the synthetic previous-revision transaction with one custom
// continuation-template row. It supports only the fixture query paths and does not model database
// authorization, locks or commit.
// continuationCustomTaskTx расширяет synthetic previous-revision transaction одной custom
// continuation-template row. Поддерживает только fixture query paths и не моделирует DB
// authorization, locks либо commit.
type continuationCustomTaskTx struct{ continuationTransitionTx }

// continuationCustomTaskRow represents the fixed owner-template response used to test the full
// task against notice-size limits. It carries no external template state or authority.
// continuationCustomTaskRow представляет fixed owner-template response для проверки full task
// относительно notice-size limits. Не переносит внешнее template state либо authority.
type continuationCustomTaskRow struct{}

// QueryRow intercepts only the continuation-template SQL and returns the fixed custom row; all
// other queries delegate to the embedded prior-revision stub. Context/arguments are forwarded only
// on delegation, without real SQL execution.
// QueryRow перехватывает только continuation-template SQL и возвращает fixed custom row; остальные
// queries передаёт embedded prior-revision stub. Context/arguments передаются только при
// delegation, реальный SQL не выполняется.
func (tx continuationCustomTaskTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == queryPromptContinuationTemplate {
		return continuationCustomTaskRow{}
	}
	return tx.continuationTransitionTx.QueryRow(ctx, query, args...)
}

// Scan fills the assumed ref/digest/content destinations with a fixed owner template and its exact
// SHA-256. It relies on the expected pointer types and performs no arity/type validation or
// database read.
// Scan заполняет ожидаемые ref/digest/content destinations fixed owner template и его exact
// SHA-256. Полагается на ожидаемые pointer types, не проверяет arity/types и не читает БД.
func (continuationCustomTaskRow) Scan(dest ...any) error {
	content := "Owner requires the whole task: {{.task}}"
	digest := sha256.Sum256([]byte(content))
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = "ptpl_exact_owner", hex.EncodeToString(digest[:]), content
	return nil
}

// TestContinuationCustomTaskTemplateIsNotTruncated renders a custom owner template with a 64 KiB
// task and requires notice-byte Conflict rather than truncation, with no notice or mutation of
// current context. The task and owner template are synthetic and no provider call occurs.
// TestContinuationCustomTaskTemplateIsNotTruncated рендерит custom owner template с task 64 KiB и
// требует notice-byte Conflict вместо усечения, без notice либо изменения current context. Task и
// owner template синтетические; provider call отсутствует.
func TestContinuationCustomTaskTemplateIsNotTruncated(t *testing.T) {
	prompt := workflowPromptFixture()
	prompt.TargetKind, prompt.TargetRef, prompt.RunRef, prompt.SessionRef = promptservice.TargetAgent, "agt_current", "run_current", "ses_current"
	prompt.Variables["task"], prompt.Variables["node.ref"], prompt.Variables["turn.ref"] = strings.Repeat("x", 64<<10), "nod_current", "trn_current"
	prior, _ := json.Marshal(map[string]any{"promptSnapshot": prompt})
	if bindPlatformInstructions(&prompt) != nil {
		t.Fatal("bind")
	}
	current := map[string]any{"task": prompt.Variables["task"], "promptSnapshot": prompt, "runtimeRevisionRef": "rrev_current", "runRef": prompt.RunRef, "nodeRef": "nod_current", "sessionRef": prompt.SessionRef,
		"turnRef": "trn_current", "attempt": int32(8), "agentRef": "agt_current", "capabilities": []string{"platform.run.delegate"}, "promptServiceTemplateRevision": promptservice.ServiceTemplateRevision,
		"sessionContext": []map[string]string{{"role": "USER", "content": "Original"}}}
	before, _ := json.Marshal(current)
	var repository Repository
	notice, err := repository.prepareRuntimeContinuationNotice(t.Context(), continuationCustomTaskTx{continuationTransitionTx{previous: prior}}, scope{}, current, prompt)
	after, _ := json.Marshal(current)
	if notice != nil || !errors.Is(err, errs.ErrConflict) || runtimeEligibilityReason(err) != "CONTINUATION_NOTICE_BYTES" || string(before) != string(after) {
		t.Fatal("owner template bypassed bound or full task/context changed")
	}
}
