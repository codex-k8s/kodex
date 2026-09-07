package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"testing"

	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

func runtimeMaterializationFixture() runtimeMaterializationInput {
	return runtimeMaterializationInput{
		WorkloadInstance: "fixture-workload", LeaseRef: "lea_fixture", Fence: "fnc_fixture",
		Generation: 3, RuntimeRevisionRef: "rrev_fixture", RuntimeRevisionDigest: strings.Repeat("a", 64),
		SessionRef: "ses_fixture", TurnRef: "turn_fixture", Attempt: 2, InputDigest: strings.Repeat("b", 64),
	}
}

func TestRuntimeMaterializationDigestMatchesWireEnvelope(t *testing.T) {
	for _, assistant := range []bool{false, true} {
		input := runtimeMaterializationFixture()
		input.SystemAssistant = assistant
		request := &secretbrokerv1.MaterializeRuntimeCredentialsRequest{
			WorkloadInstance: input.WorkloadInstance, LeaseRef: input.LeaseRef, Fence: input.Fence,
			Generation: input.Generation, RuntimeRevisionRef: input.RuntimeRevisionRef,
			RuntimeRevisionDigest: input.RuntimeRevisionDigest, SessionRef: input.SessionRef,
			TurnRef: input.TurnRef, Attempt: int32(input.Attempt), InputDigest: input.InputDigest,
		}
		var envelope proto.Message = request
		expectedOperation := runtimeMaterializationOperation
		if assistant {
			envelope = &secretbrokerv1.MaterializeSystemAssistantCredentialsRequest{Execution: request}
			expectedOperation = assistantMaterializationOperation
		}
		raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		expected := sha256.Sum256(raw)
		operation, digest, err := runtimeMaterializationDigest(input)
		if err != nil || operation != expectedOperation || digest != hex.EncodeToString(expected[:]) {
			t.Fatalf("wire binding mismatch: assistant=%t error=%v", assistant, err)
		}
	}
}

func TestRuntimeMaterializationDigestBindsEveryExecutionField(t *testing.T) {
	_, original, err := runtimeMaterializationDigest(runtimeMaterializationFixture())
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*runtimeMaterializationInput){
		"workload":          func(input *runtimeMaterializationInput) { input.WorkloadInstance += "-other" },
		"lease":             func(input *runtimeMaterializationInput) { input.LeaseRef += "-other" },
		"fence":             func(input *runtimeMaterializationInput) { input.Fence += "-other" },
		"generation":        func(input *runtimeMaterializationInput) { input.Generation++ },
		"revision":          func(input *runtimeMaterializationInput) { input.RuntimeRevisionRef += "-other" },
		"revision-digest":   func(input *runtimeMaterializationInput) { input.RuntimeRevisionDigest = strings.Repeat("c", 64) },
		"session":           func(input *runtimeMaterializationInput) { input.SessionRef += "-other" },
		"turn":              func(input *runtimeMaterializationInput) { input.TurnRef += "-other" },
		"attempt":           func(input *runtimeMaterializationInput) { input.Attempt++ },
		"input":             func(input *runtimeMaterializationInput) { input.InputDigest = strings.Repeat("d", 64) },
		"assistant-wrapper": func(input *runtimeMaterializationInput) { input.SystemAssistant = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			input := runtimeMaterializationFixture()
			mutate(&input)
			_, digest, err := runtimeMaterializationDigest(input)
			if err != nil || digest == original {
				t.Fatalf("execution field is not bound: %v", err)
			}
		})
	}
}

func TestRuntimeMaterializationDigestRejectsInvalidAttempt(t *testing.T) {
	for _, attempt := range []int64{-1, 0, int64(math.MaxInt32) + 1} {
		input := runtimeMaterializationFixture()
		input.Attempt = attempt
		if _, _, err := runtimeMaterializationDigest(input); err == nil {
			t.Fatalf("invalid attempt accepted: %d", attempt)
		}
	}
}
