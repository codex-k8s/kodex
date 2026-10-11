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
		!strings.Contains(notice.Snapshot.SemanticValues["PURPOSE"], "continuation-task-projection-v1") {
		t.Fatal("notice lost exact task or exceeded unchanged bound")
	}
	messages := current["sessionContext"].([]map[string]string)
	after, _ := json.Marshal(messages[:len(messages)-1])
	if string(before) != string(after) {
		t.Fatal("history rewritten")
	}
}

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

func TestContinuationTaskProducerMatchesRunnerOracle(t *testing.T) {
	actual := continuationTaskOracleFixture(t)
	raw, err := os.ReadFile("testdata/continuation_task_projection_v1_instructions_v4.json")
	var fixture runtimecontract.RunnerInput
	if err != nil || json.Unmarshal(raw, &fixture) != nil || !reflect.DeepEqual(actual, fixture) {
		t.Fatal("exact producer output differs from shared synthetic runner fixture")
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

type continuationCustomTaskTx struct{ continuationTransitionTx }
type continuationCustomTaskRow struct{}

func (tx continuationCustomTaskTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == queryPromptContinuationTemplate {
		return continuationCustomTaskRow{}
	}
	return tx.continuationTransitionTx.QueryRow(ctx, query, args...)
}

func (continuationCustomTaskRow) Scan(dest ...any) error {
	content := "Owner requires the whole task: {{.task}}"
	digest := sha256.Sum256([]byte(content))
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = "ptpl_exact_owner", hex.EncodeToString(digest[:]), content
	return nil
}

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
