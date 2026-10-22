package codex

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestBrokerUsageCompletenessActualWireOracle crosses real writer/strict-reader code for every closed
// usage quality and success/failure/overflow mode, retaining measured values and verified capture without
// mutating the original result. Temporary captured files are synthetic, not a live provider transcript.
// TestBrokerUsageCompletenessActualWireOracle проходит настоящий writer и строгий reader для каждого
// значения полноты расхода и режима успеха, отказа или переполнения, сохраняя измерения и проверенный
// захват без изменения исходного результата. Файлы синтетические, а не запись живого провайдера.
func TestBrokerUsageCompletenessActualWireOracle(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		quality UsageCompleteness
	}{{"UNKNOWN", UsageUnknown}, {"PARTIAL", UsagePartial}, {"COMPLETE", UsageComplete}} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, mode := range []string{"success", "terminal failure", "provider failure", "overflow"} {
				t.Run(mode, func(t *testing.T) {
					input, captured := capturedRolloutFixture(t, model.Input{})
					captured.UsageCompleteness = fixture.quality
					response := brokerResponse{Result: captured, OK: true}
					switch mode {
					case "terminal failure":
						response.Result.Outcome, response.Result.FailureCode = "FAILED", "PROVIDER_FAILED"
					case "provider failure":
						response.OK, response.Failure = false, providerBrokerFailureProvider
					case "overflow":
						response.Result.FinalMessage = strings.Repeat("<", maximumFinalBytes)
					}
					var wire bytes.Buffer
					writer := &brokerFrameWriter{writer: &wire}
					if err := writer.finish(response); err != nil {
						t.Fatal("synthetic producer rejected its own closed quality")
					}
					result, err := readBoundProviderBrokerResponse(bytes.NewReader(wire.Bytes()), &input, uint32(os.Geteuid()))
					failed := mode == "provider failure" || mode == "overflow"
					if (!failed && err != nil) || (failed && !errors.Is(err, errProviderBrokerFailed)) {
						t.Fatal("actual writer output rejected by strict consumer")
					}
					if result.UsageCompleteness != fixture.quality || result.Usage != captured.Usage || !result.HasVerifiedRollout(input) {
						t.Fatal("actual roundtrip lost usage quality or verified capture")
					}
					if failed && (result.Outcome != "" || result.FinalMessage != "") || !failed && result.Outcome != response.Result.Outcome {
						t.Fatal("usage quality changed the execution outcome")
					}
					if captured.UsageCompleteness != fixture.quality || captured.Outcome != "SUCCEEDED" {
						t.Fatal("transport mutated the captured source")
					}
				})
			}
		})
	}
}

// TestBrokerUsageCompletenessExplicitUnknownWire verifies omitted zero quality and explicit UNKNOWN
// both retain the existing optional-wire contract, measured usage and input-bound archive proof.
// TestBrokerUsageCompletenessExplicitUnknownWire проверяет сохранение необязательного wire-контракта,
// измерений и привязанного доказательства архива как без нулевого поля, так и с явным UNKNOWN.
func TestBrokerUsageCompletenessExplicitUnknownWire(t *testing.T) {
	input, captured := capturedRolloutFixture(t, model.Input{})
	var wire bytes.Buffer
	if err := (&brokerFrameWriter{writer: &wire}).finish(brokerResponse{Result: captured, OK: true}); err != nil {
		t.Fatal("synthetic terminal failed")
	}
	if bytes.Contains(wire.Bytes(), []byte(`"UsageCompleteness"`)) {
		t.Fatal("zero quality no longer follows the existing optional wire")
	}
	encoded := bytes.Replace(wire.Bytes(), []byte(`"Usage":`), []byte(`"UsageCompleteness":"UNKNOWN","Usage":`), 1)
	result, err := readBoundProviderBrokerResponse(bytes.NewReader(encoded), &input, uint32(os.Geteuid()))
	if err != nil || result.UsageCompleteness != UsageUnknown || result.Usage != captured.Usage || !result.HasVerifiedRollout(input) {
		t.Fatal("explicit closed unknown quality was rejected")
	}
}

// TestBrokerUsageCompletenessRejectsMalformedWire mutates the actual terminal field into malformed,
// wrong-type, unknown, empty or duplicate values and requires a redacted error with an entirely empty result.
// TestBrokerUsageCompletenessRejectsMalformedWire подменяет настоящее поле итога повреждёнными,
// неверно типизированными, неизвестными, пустыми или повторными значениями и требует безопасную ошибку без результата.
func TestBrokerUsageCompletenessRejectsMalformedWire(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		field string
	}{
		{"malformed", `"UsageCompleteness":`},
		{"null", `"UsageCompleteness":null`},
		{"numeric", `"UsageCompleteness":1`},
		{"object", `"UsageCompleteness":{}`},
		{"array", `"UsageCompleteness":[]`},
		{"boolean", `"UsageCompleteness":true`},
		{"unknown enum", `"UsageCompleteness":"PRIVATE_BODY_SENTINEL"`},
		{"lowercase enum", `"UsageCompleteness":"partial"`},
		{"empty enum", `"UsageCompleteness":""`},
		{"unknown field", `"UsageCompleteness":"PARTIAL","UnknownQuality":"COMPLETE"`},
		{"duplicate same", `"UsageCompleteness":"PARTIAL","UsageCompleteness":"PARTIAL"`},
		{"duplicate different", `"UsageCompleteness":"PARTIAL","UsageCompleteness":"COMPLETE"`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			input, captured := capturedRolloutFixture(t, model.Input{})
			captured.UsageCompleteness = UsagePartial
			var wire bytes.Buffer
			if err := (&brokerFrameWriter{writer: &wire}).finish(brokerResponse{Result: captured, OK: true}); err != nil {
				t.Fatal("synthetic terminal failed")
			}
			encoded := bytes.Replace(wire.Bytes(), []byte(`"UsageCompleteness":"PARTIAL"`), []byte(fixture.field), 1)
			if bytes.Equal(encoded, wire.Bytes()) {
				t.Fatal("fixture did not mutate the exact field")
			}
			result, err := readBoundProviderBrokerResponse(bytes.NewReader(encoded), &input, uint32(os.Geteuid()))
			if !errors.Is(err, errProviderBrokerResponseInvalid) || !reflect.DeepEqual(result, Result{}) || strings.Contains(err.Error(), "PRIVATE_BODY_SENTINEL") {
				t.Fatal("malformed quality escaped the closed boundary")
			}
		})
	}
}
