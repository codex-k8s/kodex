package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHandoffRoundTripAndUnknownField(t *testing.T) {
	digest := strings.Repeat("a", 64)
	resultPayload := []byte("result")
	resultDigest := sha256.Sum256(resultPayload)
	input := HandoffV1{
		Schema: HandoffSchemaV1, ExecutionID: "execution", ExecutionVersion: 7,
		Fence: 8, GrantGeneration: 9, RuntimeRevisionSHA256: digest,
		EffectiveRuntimeSHA256: digest, ImmutableInputSHA256: digest,
		AgentSessionID: 10, AgentSessionTurnID: 11, AgentRunID: "run",
		AgentBindingSHA256: digest, Outcome: "SUCCEEDED", TerminalReference: "result://exact",
		TerminalSHA256: digest, ResultArtifactID: "artifact", ResultArtifactVersion: 1,
		ResultArtifactSHA256: hex.EncodeToString(resultDigest[:]), ResultArtifactName: "result.md", ResultArtifactMediaType: "text/markdown",
		ResultArtifactPayload: resultPayload, ObservedAt: time.Unix(1, 0).UTC(),
	}
	raw, err := EncodeHandoff(input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := DecodeHandoff(raw)
	if err != nil || !reflect.DeepEqual(output, input) {
		t.Fatalf("round trip mismatch: %#v, %v", output, err)
	}
	unknown := bytes.Replace(raw, []byte(`"schema"`), []byte(`"unknown":true,"schema"`), 1)
	if _, err := DecodeHandoff(unknown); err == nil {
		t.Fatal("unknown handoff field was accepted")
	}
	input.ResultArtifactSHA256 = digest
	if _, err := EncodeHandoff(input); err == nil {
		t.Fatal("mismatched result artifact digest was accepted")
	}
}
