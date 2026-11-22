package managedkubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// streamEffectReceiptFixture supplies validated synthetic exit facts without granting owner authority.
// streamEffectReceiptFixture предоставляет valid synthetic exit facts без owner authority.
func streamEffectReceiptFixture() TerminalReceipt {
	zero := int32(0)
	return TerminalReceipt{Origin: receiptOriginFixture(), HandshakeDigest: strings.Repeat("d", 64), Protocol: string(WebSocketV5), TransportJoined: true, LocalState: "COMPLETED", RemoteOutcome: "SUCCEEDED", ExitCode: &zero, ReasonCode: "REMOTE_EXIT"}
}

// TestStreamEffectResultPreservesRemoteAndLocalFacts checks exact wire shape across distinct terminal outcomes.
// TestStreamEffectResultPreservesRemoteAndLocalFacts проверяет exact wire shape для разных terminal outcomes.
func TestStreamEffectResultPreservesRemoteAndLocalFacts(t *testing.T) {
	for _, scenario := range []string{"success", "remote failure", "lost output", "unknown", "watch EOF", "tunnel"} {
		t.Run(scenario, func(t *testing.T) {
			receipt := streamEffectReceiptFixture()
			switch scenario {
			case "remote failure":
				code := int32(7)
				receipt.ExitCode = &code
				receipt.RemoteOutcome = "FAILED"
			case "lost output":
				receipt.LocalState = "LOST"
				receipt.ReasonCode = "OUTPUT_LOST"
			case "unknown", "tunnel":
				receipt.LocalState = "LOST"
				receipt.RemoteOutcome = "UNKNOWN"
				receipt.ExitCode = nil
				receipt.ReasonCode = "TRANSPORT_LOST"
				if scenario == "tunnel" {
					receipt.Origin.Operation = "kubernetes.port-forward"
					receipt.Protocol = string(PortForwardWebSocketV4)
				}
			case "watch EOF":
				receipt.Origin.Operation = "kubernetes.watch"
				receipt.Protocol = HTTPWatchV1
				receipt.ExitCode = nil
				receipt.CleanEOF = true
				receipt.ReasonCode = "CLEAN_EOF"
			}
			result, err := BuildStreamEffectResult(receipt)
			if err != nil || result.Outcome != receipt.RemoteOutcome || ValidateStreamEffectResult(receipt, result.ResultJSON, result.ResultDigest, result.Outcome, result.ReasonCode) != nil {
				t.Fatal("terminal result lost observed facts")
			}
			if bytes.Contains(result.ResultJSON, []byte("origin")) || bytes.Contains(result.ResultJSON, []byte(receipt.Origin.EffectRef)) || bytes.Contains(result.ResultJSON, []byte("payload")) {
				t.Fatal("private metadata entered safe projection")
			}
			switch scenario {
			case "success":
				if string(result.ResultJSON) != `{"stream":{"localState":"COMPLETED","remoteOutcome":"SUCCEEDED","exitCode":0}}` || result.ReasonCode != "" {
					t.Fatal("canonical success shape changed")
				}
			case "remote failure":
				if result.ReasonCode != ReasonRemoteFailure {
					t.Fatal("confirmed remote failure became uncertain")
				}
			case "lost output":
				if result.Outcome != "SUCCEEDED" || !bytes.Contains(result.ResultJSON, []byte(`"localState":"LOST"`)) {
					t.Fatal("lost output erased remote success or claimed full delivery")
				}
			default:
				if bytes.Contains(result.ResultJSON, []byte("exitCode")) {
					t.Fatal("absent exit code invented")
				}
			}
			if receipt.ExitCode != nil {
				*receipt.ExitCode = 99
				if bytes.Contains(result.ResultJSON, []byte("99")) {
					t.Fatal("result aliases receipt exit pointer")
				}
			}
		})
	}
}

// TestStreamEffectResultRequiresExactStoredReceipt rejects added text, reordered JSON and semantic promotion.
// TestStreamEffectResultRequiresExactStoredReceipt отклоняет added text, reordered JSON и semantic promotion.
func TestStreamEffectResultRequiresExactStoredReceipt(t *testing.T) {
	receipt := streamEffectReceiptFixture()
	result, err := BuildStreamEffectResult(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{nil, []byte(`{}`), append([]byte(" "), result.ResultJSON...), []byte(`{"stream":{"remoteOutcome":"SUCCEEDED","localState":"COMPLETED","exitCode":0}}`), []byte(`{"stream":{"localState":"COMPLETED","remoteOutcome":"SUCCEEDED","exitCode":0,"output":"private"}}`)} {
		hash := sha256.Sum256(raw)
		if ValidateStreamEffectResult(receipt, raw, hex.EncodeToString(hash[:]), result.Outcome, result.ReasonCode) == nil {
			t.Fatal("noncanonical or extra result accepted")
		}
	}
	if ValidateStreamEffectResult(receipt, result.ResultJSON, strings.Repeat("e", 64), result.Outcome, "") == nil || ValidateStreamEffectResult(receipt, result.ResultJSON, result.ResultDigest, "UNKNOWN", ReasonTransportUnknown) == nil {
		t.Fatal("changed hash/outcome accepted")
	}
	bad := receipt
	bad.RemoteOutcome = "UNKNOWN"
	if _, err := BuildStreamEffectResult(bad); err == nil {
		t.Fatal("contradictory exit accepted")
	}
	bad = receipt
	bad.LocalState = "ACTIVE"
	if _, err := BuildStreamEffectResult(bad); err == nil {
		t.Fatal("active stream projected as terminal")
	}
}
