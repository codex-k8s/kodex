package runtimecontract

import (
	"encoding/json"
	"testing"
)

// TestProviderObservationRejectionPreservesReceiver proves the shared decoder
// cannot replace either receiver with a partially decoded invalid observation.
// TestProviderObservationRejectionPreservesReceiver доказывает, что общий decoder
// не заменяет ни одного получателя частично разобранным неверным наблюдением.
func TestProviderObservationRejectionPreservesReceiver(t *testing.T) {
	_, diagnostic := diagnosticFixture()
	process := BindProviderProcessObservation(processObservationInput(), "0.160.0")
	for _, raw := range []string{`null`, `{}`, `{"schema":"wrong"}`, `{"schema":null}`, `{} {}`} {
		actualDiagnostic, actualProcess := diagnostic, process
		if json.Unmarshal([]byte(raw), &actualDiagnostic) == nil || actualDiagnostic != diagnostic {
			t.Fatal("invalid diagnostic replaced the receiver")
		}
		if json.Unmarshal([]byte(raw), &actualProcess) == nil || actualProcess != process {
			t.Fatal("invalid process observation replaced the receiver")
		}
	}
}
