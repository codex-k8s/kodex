package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"k8s.io/client-go/kubernetes/fake"
)

// TestLargeContinuationPassesExactControllerBindingAndClosedDiagnostics proves
// a large owner runtime-change notice survives sealing and runner encoding,
// while an oversized ordinary USER message still fails the shared validator
// and receives the closed diagnostic instead of inheriting the notice allowance.
// TestLargeContinuationPassesExactControllerBindingAndClosedDiagnostics
// проверяет сохранение большого уведомления владельца об изменениях runtime
// через закрепление и кодирование runner. Обычное слишком длинное USER-сообщение
// по-прежнему отвергается общим валидатором с закрытым диагностическим кодом.
func TestLargeContinuationPassesExactControllerBindingAndClosedDiagnostics(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	execution := testExecution(false)
	setSemanticPrompt(execution.Revision)
	sealTestTurnExecution(execution)
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatal(err)
	}
	diff := struct {
		PreviousRevisionRef string `json:"previousRevisionRef"`
		CurrentRevisionRef  string `json:"currentRevisionRef"`
		SessionRef          string `json:"sessionRef"`
		TurnRef             string `json:"turnRef"`
		Attempt             int32  `json:"attempt"`
		Changes             []any  `json:"changes"`
		Digest              string `json:"digest"`
	}{"rrev_previous", input.RuntimeRevisionRef, input.SessionRef, input.TurnRef, input.Attempt, []any{}, ""}
	raw, _ := json.Marshal(diff)
	digest := sha256.Sum256(raw)
	diff.Digest = hex.EncodeToString(digest[:])
	raw, _ = json.Marshal(diff)
	var envelope runtimecontract.PromptServiceEnvelope
	if json.Unmarshal([]byte(input.Instructions), &envelope) != nil {
		t.Fatal("invalid semantic fixture")
	}
	envelope.Sections[1].Content = strings.Repeat("a", 50000)
	envelope.Sections = append(envelope.Sections, runtimecontract.PromptServiceSection{Source: "PLATFORM", Slot: "RUNTIME_CHANGES", Content: string(raw)})
	raw, _ = json.Marshal(envelope)
	notice := string(raw)
	execution.Revision.SessionContext = []*controlplanev1.SessionContextMessage{{Role: "USER", Content: "Exact original task"}, {Role: "USER", Content: notice}}
	sealTestTurnExecution(execution)
	input, _, err = manager.BuildTurnInput(execution)
	if err != nil || input.SessionContext[1].Content != notice || runtimeEnvelopeReason(input) != "UNKNOWN" {
		t.Fatalf("valid owner notice rejected or mislabeled: %v", err)
	}
	encoded, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := runtimecontract.DecodeRunnerInput(encoded)
	if err != nil || decoded.SessionContext[1].Content != notice {
		t.Fatal("RC runner wire lost full notice")
	}
	input.SessionContext[0].Content = strings.Repeat("a", runtimecontract.MaximumAssistantTurnCodepoints+1)
	if input.Validate() == nil || runtimeEnvelopeReason(input) != "ENVELOPE_SESSION_USER_CONTENT" {
		t.Fatal("ordinary USER guard or closed diagnostic weakened")
	}
}
