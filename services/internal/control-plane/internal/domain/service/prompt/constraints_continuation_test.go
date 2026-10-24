package prompt

import (
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestConstraintRevisionContinuationAndRejoin checks supported v2/v3 base kinds preserve cold and
// warm durable continuation while other service revisions, unknown envelopes and previous attempts
// cannot authorize resume.
// TestConstraintRevisionContinuationAndRejoin проверяет холодное и тёплое durable-продолжение
// базовых видов v2/v3 с запретом resume для других service revisions, неизвестных конвертов и
// предыдущих попыток.
func TestConstraintRevisionContinuationAndRejoin(t *testing.T) {
	for _, revision := range []string{pinnedServiceTemplateV2, ServiceTemplateRevision} {
		for _, kind := range []string{TargetAgent, TargetWorkflowStage, TargetAutomation} {
			t.Run(revision+"/"+kind, func(t *testing.T) {
				input, notice := constraintContinuationFixture(t, revision, kind)
				otherRevision := pinnedServiceTemplateV2
				if revision == pinnedServiceTemplateV2 {
					otherRevision = ServiceTemplateRevision
				}
				_, otherNotice := constraintContinuationFixture(t, otherRevision, kind)
				for _, cold := range []bool{false, true} {
					candidate := input
					if cold {
						candidate.CodexSessionID = ""
					}
					// История хранит прежнюю версию без переписывания; только последний
					// owner notice относится к текущей runtime revision.
					candidate.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: otherNotice.Prompt}, {Role: "USER", Content: notice.Prompt}}
					raw, err := json.Marshal(candidate)
					if err != nil || json.Unmarshal(raw, &candidate) != nil {
						t.Fatal("durable runner input persistence failed")
					}
					if _, err := runtimecontract.DecodePromptService(candidate); err != nil {
						t.Fatalf("pinned base prompt rejected: %v", err)
					}
					if found, err := runtimecontract.CurrentContinuationNotice(candidate); err != nil || !found {
						t.Fatalf("current notice lost revision or execution binding: %v", err)
					}
					// Даже совпадающие execution IDs не разрешают downgrade/upgrade notice.
					candidate.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: otherNotice.Prompt}}
					if found, err := runtimecontract.CurrentContinuationNotice(candidate); err == nil || found {
						t.Fatal("mixed service revision authorized continuation")
					}
					if !cold {
						if _, err := runtimecontract.DecodePromptService(candidate); err == nil {
							t.Fatal("mixed service revision authorized provider resume")
						}
					}
					var future runtimecontract.PromptServiceEnvelope
					if json.Unmarshal([]byte(notice.Prompt), &future) != nil {
						t.Fatal("fixture envelope decode failed")
					}
					future.Revision = "prompt-service-v4"
					futureRaw, err := json.Marshal(future)
					if err != nil {
						t.Fatal(err)
					}
					candidate.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: string(futureRaw)}}
					if found, err := runtimecontract.CurrentContinuationNotice(candidate); err == nil || found {
						t.Fatal("unknown typed notice became ordinary conversation history")
					}
					candidate.SessionContext = input.SessionContext
					candidate.Attempt++
					if found, err := runtimecontract.CurrentContinuationNotice(candidate); err == nil || found {
						t.Fatal("previous attempt notice authorized replay")
					}
				}
			})
		}
	}
}

// constraintContinuationFixture builds a pinned base prompt and separate model-change continuation
// notice with exact runtime, session, turn and attempt identity for consumer checks.
// constraintContinuationFixture создаёт закреплённый базовый prompt и отдельное уведомление
// продолжения об изменении модели с точными runtime, session, turn и attempt для проверок
// consumer.
func constraintContinuationFixture(t *testing.T, revision, kind string) (runtimecontract.RunnerInput, Materialization) {
	t.Helper()
	snapshot := semanticFixture()
	snapshot.TargetKind, snapshot.ServiceTemplateRevision = kind, revision
	base, err := Materialize("Agent", snapshot)
	if err != nil || !base.Complete {
		t.Fatalf("base producer failed: %v", err)
	}
	diff, raw := continuationModelDiffFixture(t)
	snapshot.TargetKind, snapshot.SessionContinuation = TargetSessionContinuation, string(raw)
	notice, err := Materialize(`Continuation {{slot "RUNTIME_CHANGES"}}`, snapshot)
	if err != nil || !notice.Complete {
		t.Fatalf("notice producer failed: %v", err)
	}
	input := constraintConsumerInput(base, kind)
	input.CodexSessionID, input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt = "previous-provider-thread", diff.CurrentRevisionRef, diff.SessionRef, diff.TurnRef, diff.Attempt
	input.SessionContext = []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: notice.Prompt}}
	return input, notice
}

// continuationModelDiffFixture builds the shared synthetic model-change diff and its JSON with
// exact revision, session, turn and attempt pins; callers create and verify their own notices.
// continuationModelDiffFixture создаёт общий синтетический diff смены модели и его JSON с точными
// pins ревизии, сессии, turn и попытки; вызывающие сами строят и проверяют свои уведомления.
func continuationModelDiffFixture(t *testing.T) (RuntimeDiff, []byte) {
	t.Helper()
	previous, current := map[string][]RuntimeDescriptor{}, map[string][]RuntimeDescriptor{}
	for _, component := range runtimeComponentOrder {
		previous[component], current[component] = []RuntimeDescriptor{}, []RuntimeDescriptor{}
	}
	previous["MODEL"] = []RuntimeDescriptor{{Value: "old-model"}}
	current["MODEL"] = []RuntimeDescriptor{{Value: "current-model"}}
	diff, err := CompareRuntimeContexts(previous, current, RuntimeDiff{PreviousRevisionRef: "rrev_previous", CurrentRevisionRef: "rrev_current", SessionRef: "ses_current", TurnRef: "turn_current", Attempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(diff)
	if err != nil {
		t.Fatal(err)
	}
	return diff, raw
}
