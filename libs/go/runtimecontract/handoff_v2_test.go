package runtimecontract

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

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
		CodexSessionID: "00000000-0000-4000-8000-000000000001", ArchiveRelativePath: ".matter-codex/state/codex-home/sessions/2026/08/04/rollout-00000000-0000-4000-8000-000000000001.jsonl",
		ArchiveSHA256:     strings.Repeat("6", 64),
		ArchiveProvenance: "codex-app-server-rollout-v1:00000000-0000-4000-8000-000000000006:.matter-codex/state/codex-home/sessions/2026/08/04/rollout-00000000-0000-4000-8000-000000000001.jsonl:" + strings.Repeat("6", 64), ObservedAt: time.Now().UTC()}
	if err := handoff.Outputs[0].validate(); err != nil {
		t.Fatalf("output fixture is invalid: %v", err)
	}
	if err := handoff.Validate(); err != nil {
		t.Fatalf("handoff fixture is invalid: %v", err)
	}
	invalidProvenance := handoff
	invalidProvenance.ArchiveProvenance = strings.Replace(invalidProvenance.ArchiveProvenance,
		"00000000-0000-4000-8000-000000000006", "not-an-execution", 1)
	if err := invalidProvenance.Validate(); err == nil {
		t.Fatal("archive provenance without an exact source execution was accepted")
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
