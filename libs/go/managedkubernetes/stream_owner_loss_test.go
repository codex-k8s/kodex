package managedkubernetes

import (
	"strings"
	"testing"
)

// TestUnreportedOwnerLossCannotInventNativeFacts permits metadata-only recovery without claiming another replica's writer joined.
// TestUnreportedOwnerLossCannotInventNativeFacts допускает metadata-only recovery без выдуманного join writer другой replica.
func TestUnreportedOwnerLossCannotInventNativeFacts(t *testing.T) {
	base := TerminalReceipt{Origin: receiptOriginFixture(), Protocol: string(WebSocketV5), LocalState: "LOST", RemoteOutcome: "UNKNOWN", ReasonCode: "OWNER_STATE_LOST"}
	if _, err := base.Digest(); err != nil {
		t.Fatal("unreported owner state cannot be represented")
	}
	result, err := BuildStreamEffectResult(base)
	if err != nil || result.Outcome != "UNKNOWN" || result.ReasonCode != ReasonStreamLost {
		t.Fatal("owner state loss became a successful effect")
	}
	for _, scenario := range []string{"joined", "handshake", "completed", "canceled", "success", "failure", "exit", "eof", "status"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := base
			switch scenario {
			case "joined":
				candidate.TransportJoined = true
			case "handshake":
				candidate.HandshakeDigest = strings.Repeat("a", 64)
			case "completed":
				candidate.LocalState = "COMPLETED"
			case "canceled":
				candidate.LocalState = "CANCELED"
			case "success":
				candidate.RemoteOutcome = "SUCCEEDED"
			case "failure":
				candidate.RemoteOutcome = "FAILED"
			case "exit":
				code := int32(0)
				candidate.ExitCode = &code
			case "eof":
				candidate.CleanEOF = true
			case "status":
				candidate.RemoteStatusCode = 500
			}
			if _, err := candidate.Digest(); err == nil {
				t.Fatal("missing owner state invented native facts")
			}
		})
	}
}
