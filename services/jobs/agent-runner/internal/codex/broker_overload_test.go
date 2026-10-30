package codex

import (
	"bytes"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestBrokerOverloadProvenance transfers a structured failed turn through the real
// broker frame codec, then rejects another attempt or input without exposing a
// diagnostic. It proves a local producer boundary, not upstream availability.
// TestBrokerOverloadProvenance передаёт структурированный failed turn настоящим
// broker frame codec, затем отклоняет другую попытку либо вход без выдачи
// диагностики. Это проверка локальной границы producer, не доступности upstream.
func TestBrokerOverloadProvenance(t *testing.T) {
	input := diagnosticInput()
	value := bindProviderDiagnostic(input, runtimecontract.ProviderFailureDiagnostic{
		Kind: "TERMINAL_FAILURE", Stage: "TERMINAL_RESULT", Class: "PROVIDER", Detail: "NONE",
		Notification: "NONE", NotificationError: "NONE", AccountRead: "NONE", TerminalCode: "server_overloaded"})
	var wire bytes.Buffer
	writer := &brokerFrameWriter{writer: &wire, input: &input}
	result := Result{Outcome: "FAILED", FailureCode: "server_overloaded", Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}}
	if value == nil || writer.finish(brokerResponse{Result: result, OK: true, Diagnostic: value}) != nil {
		t.Fatal("write isolated terminal overload")
	}
	actual, err := readBoundProviderBrokerResponse(bytes.NewReader(wire.Bytes()), &input, providerWriterUID)
	diagnostic := actual.ProviderDiagnostic(input)
	if err != nil || diagnostic == nil || !diagnostic.IsOverload() || actual.Outcome != "FAILED" || actual.FailureCode != result.FailureCode || actual.Usage != result.Usage {
		t.Fatal("trusted broker lost terminal overload provenance")
	}
	for _, name := range []string{"attempt", "input", "revision", "execution", "session", "turn"} {
		other := input
		switch name {
		case "attempt":
			other.Attempt++
		case "input":
			other.InputDigest = strings.Repeat("d", 64)
		case "revision":
			other.RuntimeRevisionDigest = strings.Repeat("d", 64)
		case "execution":
			other.ExecutionBindingDigest = strings.Repeat("d", 64)
		case "session":
			other.SessionRef = "ses_foreign"
		case "turn":
			other.TurnRef = "trn_foreign"
		}
		if actual.ProviderDiagnostic(other) != nil {
			t.Fatalf("overload crossed exact %s binding", name)
		}
	}
}
