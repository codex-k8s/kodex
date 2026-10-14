package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func diagnosticInput() model.Input {
	return model.Input{SessionRef: "ses_fixture1", TurnRef: "trn_fixture1", Attempt: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64)}
}

func TestBrokerDiagnosticTransfersOnlyClosedSourceCauseWithMeasurements(t *testing.T) {
	input := diagnosticInput()
	for _, stage := range []providerExecutionStage{providerStageHomePrepare, providerStageTerminalWait, providerStageUnknown} {
		var wire bytes.Buffer
		writer := &brokerFrameWriter{writer: &wire, input: &input}
		usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
		err := atProviderStage(stage, &appServerCallFailure{detail: "STREAM_CLOSED"})
		if writeProviderBrokerResultFailure(writer, Result{Usage: usage}, err) != nil {
			t.Fatal("write failed")
		}
		result, failure := readBoundProviderBrokerResponse(bytes.NewReader(wire.Bytes()), &input, providerWriterUID)
		diagnostic := result.ProviderDiagnostic(input)
		if failure == nil || diagnostic == nil || diagnostic.Stage != string(stage) || diagnostic.Detail != "STREAM_CLOSED" || result.Usage != usage || result.Outcome != "" {
			t.Fatal("failure diagnostic changed execution outcome or lost binding")
		}
		diagnostic.Stage = "PRIVATE_SENTINEL"
		if result.ProviderDiagnostic(input) == nil {
			t.Fatal("accessor leaked mutable sealed state")
		}
		if strings.Contains(wire.String(), "PRIVATE_SENTINEL") {
			t.Fatal("private source leaked")
		}
		other := input
		other.Attempt++
		if result.ProviderDiagnostic(other) != nil {
			t.Fatal("sealed diagnostic crossed attempt")
		}
		var forged Result
		if json.Unmarshal([]byte(`{"providerDiagnostic":{"stage":"TERMINAL_WAIT"}}`), &forged) != nil || forged.ProviderDiagnostic(input) != nil {
			t.Fatal("JSON assigned private diagnostic")
		}
	}
}

func TestBrokerDiagnosticEarlyStageAndPrivacy(t *testing.T) {
	input := diagnosticInput()
	var wire bytes.Buffer
	writer := &brokerFrameWriter{writer: &wire, input: &input}
	if writeProviderBrokerFailureAtStage(writer, providerStageAuthRead, errors.New("PRIVATE_SENTINEL")) != nil {
		t.Fatal("early failure write failed")
	}
	result, err := readBoundProviderBrokerResponse(bytes.NewReader(wire.Bytes()), &input, providerWriterUID)
	if err == nil || result.ProviderDiagnostic(input) == nil || result.ProviderDiagnostic(input).Stage != "AUTH_READ" || strings.Contains(wire.String(), "PRIVATE_SENTINEL") {
		t.Fatal("early failure leaked or lost explicit stage")
	}
}

func TestBrokerDiagnosticPreservesBoundTerminalFailure(t *testing.T) {
	input := diagnosticInput()
	value := bindProviderDiagnostic(input, runtimecontract.ProviderFailureDiagnostic{
		Kind: "TERMINAL_FAILURE", Stage: "TERMINAL_RESULT", Class: "PROVIDER", Detail: "NONE",
		Notification: "NONE", NotificationError: "NONE", AccountRead: "NONE", TerminalCode: "provider_interrupted",
	})
	var wire bytes.Buffer
	writer := &brokerFrameWriter{writer: &wire, input: &input}
	result := Result{Outcome: "FAILED", FailureCode: "provider_interrupted", Usage: runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}}
	if value == nil || writer.finish(brokerResponse{Result: result, OK: true, Diagnostic: value}) != nil {
		t.Fatal("terminal failure diagnostic could not be written")
	}
	actual, err := readBoundProviderBrokerResponse(bytes.NewReader(wire.Bytes()), &input, providerWriterUID)
	diagnostic := actual.ProviderDiagnostic(input)
	if err != nil || diagnostic == nil || diagnostic.Kind != "TERMINAL_FAILURE" || diagnostic.TerminalCode != result.FailureCode || actual.Outcome != result.Outcome || actual.Usage != result.Usage {
		t.Fatal("terminal diagnostic changed outcome or measured usage")
	}
}

func TestBrokerDiagnosticRejectsForeignPeerPinsAndMalformedTerminal(t *testing.T) {
	input := diagnosticInput()
	value, _ := providerFailureDetails(providerStageTerminalWait, errors.New("PRIVATE_SENTINEL"))
	diagnostic := bindProviderDiagnostic(input, value)
	for _, scenario := range []string{"peer", "input", "revision", "execution", "session", "turn", "attempt", "success", "null", "unknown", "broken"} {
		t.Run(scenario, func(t *testing.T) {
			other := input
			uid := uint32(providerWriterUID)
			response := brokerResponse{Failure: providerBrokerFailureProvider, Diagnostic: diagnostic}
			switch scenario {
			case "peer":
				uid = 10001
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
			case "attempt":
				other.Attempt++
			case "success":
				response.OK, response.Failure, response.Result.Outcome = true, "", "SUCCEEDED"
			}
			raw, _ := json.Marshal(brokerFrame{Version: providerBrokerVersion, Sequence: 1, Kind: brokerFrameTerminal, Terminal: &response})
			if scenario == "null" {
				raw = bytes.Replace(raw, []byte(`"diagnostic":`), []byte(`"diagnostic":null,"ignored":`), 1)
			}
			if scenario == "unknown" {
				raw = bytes.Replace(raw, []byte(`"stage":`), []byte(`"PRIVATE_SENTINEL":"PRIVATE_SENTINEL","stage":`), 1)
			}
			if scenario == "broken" {
				raw = raw[:len(raw)/2]
			}
			result, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &other, uid)
			if err == nil || result.ProviderDiagnostic(input) != nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("foreign or malformed terminal produced diagnostic")
			}
		})
	}
}
