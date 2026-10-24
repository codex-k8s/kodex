package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestCanonicalContinuationConsumerPreservesBasePromptKind checks actual producer/consumer
// continuation retains agent/stage/automation base kinds and rejects missing, replayed, malformed,
// tampered or user-origin runtime notices.
// TestCanonicalContinuationConsumerPreservesBasePromptKind проверяет сохранение базовых видов
// агента, стадии и автоматизации настоящими producer и consumer продолжения с отказом
// отсутствующим, повторным, неверным, изменённым либо пользовательским runtime-уведомлениям.
func TestCanonicalContinuationConsumerPreservesBasePromptKind(t *testing.T) {
	for _, kind := range []string{TargetAgent, TargetWorkflowStage, TargetAutomation} {
		t.Run(kind, func(t *testing.T) {
			snapshot := semanticFixture()
			snapshot.TargetKind = kind
			base, err := Materialize(`Agent instructions {{slot "PURPOSE"}}`, snapshot)
			if err != nil || !base.Complete {
				t.Fatal("base producer failed")
			}
			diff, rawDiff := continuationModelDiffFixture(t)
			snapshot.TargetKind, snapshot.SessionContinuation = TargetSessionContinuation, string(rawDiff)
			notice, err := Materialize(`Custom continuation {{slot "RUNTIME_CHANGES"}}`, snapshot)
			if err != nil || !notice.Complete {
				t.Fatal("continuation producer failed")
			}
			input := runtimecontract.RunnerInput{Instructions: base.Prompt, Capabilities: base.EffectiveCapabilities,
				PromptServiceTemplateRevision: base.ServiceTemplateRevision, PromptServiceTemplateDigest: base.ServiceTemplateDigest, PromptTargetKind: kind,
				CodexSessionID: "previous-provider-thread", RuntimeRevisionRef: diff.CurrentRevisionRef, SessionRef: diff.SessionRef, TurnRef: diff.TurnRef, Attempt: diff.Attempt,
				SessionContext: []runtimecontract.RunnerSessionMessage{{Role: "USER", Content: "Prior task"}, {Role: "USER", Content: notice.Prompt}}}
			for _, cold := range []bool{false, true} {
				candidate := input
				if cold {
					candidate.CodexSessionID = ""
				}
				if _, err := runtimecontract.DecodePromptService(candidate); err != nil {
					t.Fatalf("owner producer cannot materialize provider continuation: %v", err)
				}
				if found, err := runtimecontract.CurrentContinuationNotice(candidate); err != nil || !found {
					t.Fatalf("owner notice lost exact identity: %v", err)
				}
			}
			for _, failure := range []string{"missing", "historical", "revision", "session", "turn", "attempt", "capability", "duplicate key", "unknown diff field", "tampered digest", "user slot"} {
				t.Run(failure, func(t *testing.T) {
					candidate := input
					candidate.SessionContext = append([]runtimecontract.RunnerSessionMessage(nil), input.SessionContext...)
					last := &candidate.SessionContext[len(candidate.SessionContext)-1]
					switch failure {
					case "missing":
						candidate.SessionContext = nil
					case "historical":
						candidate.SessionContext = candidate.SessionContext[:1]
					case "revision":
						candidate.RuntimeRevisionRef = "rrev_other"
					case "session":
						candidate.SessionRef = "ses_other"
					case "turn":
						candidate.TurnRef = "turn_other"
					case "attempt":
						candidate.Attempt++
					case "capability":
						candidate.Capabilities = []string{"admin"}
					case "duplicate key":
						last.Content = strings.Replace(last.Content, `"revision":`, `"revision":"other","revision":`, 1)
					case "unknown diff field", "tampered digest", "user slot":
						var envelope runtimecontract.PromptServiceEnvelope
						if json.Unmarshal([]byte(last.Content), &envelope) != nil {
							t.Fatal("fixture decode")
						}
						for index, section := range envelope.Sections {
							if section.Slot != "RUNTIME_CHANGES" {
								continue
							}
							switch failure {
							case "unknown diff field":
								envelope.Sections[index].Content = strings.Replace(section.Content, `"attempt":`, `"credential":"forbidden","attempt":`, 1)
							case "tampered digest":
								envelope.Sections[index].Content = strings.Replace(section.Content, "current-model", "other-model", 1)
							case "user slot":
								envelope.Sections[index].Source = "USER_TEMPLATE"
							}
						}
						raw, err := json.Marshal(envelope)
						if err != nil {
							t.Fatal(err)
						}
						last.Content = string(raw)
					}
					if _, err := runtimecontract.DecodePromptService(candidate); err == nil {
						t.Fatal("invalid continuation authorized provider resume")
					}
				})
			}
		})
	}
}
