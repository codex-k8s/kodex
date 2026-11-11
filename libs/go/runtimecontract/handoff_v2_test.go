package runtimecontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

// TestSignedHandoffV2RejectsTampering checks scheduled/runtime outcome compatibility, exact archive
// source provenance, and signature verification before rejecting a modified signed envelope.
// TestSignedHandoffV2RejectsTampering проверяет совместимость исходов расписания и runtime, точное
// происхождение архива и проверку подписи с последующим отказом изменённому подписанному конверту.
func TestSignedHandoffV2RejectsTampering(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	handoff := HandoffV2{Schema: HandoffSchemaV2, ExecutionID: "execution", ExecutionVersion: 1,
		Fence: 1, GrantGeneration: 1, RuntimeRevisionSHA256: strings.Repeat("1", 64),
		EffectiveRuntimeSHA256: strings.Repeat("2", 64), ImmutableInputSHA256: strings.Repeat("3", 64),
		SessionID: "session", TurnID: "turn", Attempt: 1, ProviderBindingID: "provider",
		ProviderBindingVersion: 1, ProviderBindingSHA256: strings.Repeat("4", 64), Outcome: "SUCCEEDED",
		TerminalReference: "codex://session", TerminalSHA256: strings.Repeat("5", 64),
		Outputs: []OutputV2{{Kind: "FINAL_MARKDOWN", ID: "output", Version: 1,
			SHA256: "2689367b205c16ce32ed4200942b8b8b1e262dfc70d9bc9fbc77c49699a4f1df",
			Name:   "result.md", MediaType: "text/markdown", Payload: []byte("ok"), SizeBytes: 2, Sequence: 1, Total: 1}},
		NativeSession: &NativeSessionCheckpoint{SessionID: "ses_fixture", Version: OpenCodeVersion, Generation: 1, RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_fixture.json", SHA256: strings.Repeat("6", 64), SizeBytes: 800}, ObservedAt: time.Now().UTC()}

	if err := handoff.Outputs[0].validate(); err != nil {
		t.Fatalf("output fixture is invalid: %v", err)
	}
	if err := handoff.Validate(); err != nil {
		t.Fatalf("handoff fixture is invalid: %v", err)
	}
	scheduled := handoff
	scheduled.ScheduleOccurrenceID = "00000000-0000-4000-8000-000000000010"
	scheduled.ScheduledOutcome = "requires_human"
	if err := scheduled.Validate(); err != nil {
		t.Fatalf("scheduled handoff fixture is invalid: %v", err)
	}
	scheduled.Outcome = "FAILED"
	if err := scheduled.Validate(); err == nil {
		t.Fatal("scheduled human outcome with failed runtime was accepted")
	}
	scheduled.ScheduledOutcome = "failed"
	if err := scheduled.Validate(); err != nil {
		t.Fatalf("scheduled failed outcome was rejected: %v", err)
	}
	invalidProvenance := handoff
	foreign := *handoff.NativeSession
	foreign.SessionID = "ses_foreign"
	invalidProvenance.NativeSession = &foreign
	if invalidProvenance.Validate() == nil {
		t.Fatal("native checkpoint without exact identity accepted")
	}

	raw, err := SignHandoffV2(handoff, "key-v1", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSignedHandoffV2(raw, map[string]ed25519.PublicKey{"key-v1": publicKey}); err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 1
	if _, err := DecodeSignedHandoffV2(raw, map[string]ed25519.PublicKey{"key-v1": publicKey}); err == nil {
		t.Fatal("tampered handoff accepted")
	}
}
