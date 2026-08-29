package callback

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestNativeToolCallProjectionKeepsCorrelationAndSafeMetadata(t *testing.T) {
	input := validWarmExecutionInput()
	payload := runtimecontract.RunnerNativeToolCallRequest{
		RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		NativeToolCall: runtimecontract.NativeToolCall{
			CallID: "call-file-1", Kind: runtimecontract.NativeToolKindFileChange,
			State: runtimecontract.NativeToolStateSucceeded, DurationMS: 42,
			SafeResult: runtimecontract.NativeToolResultCompleted,
			SafeParameters: map[string]any{"change_count": 1, "change_kinds": []string{"UPDATE"},
				"paths": []string{"internal/app.go"}, "paths_truncated": false},
		},
	}
	if err := payload.Validate(); err != nil {
		t.Fatalf("payload validation: %v payload=%#v", err, payload)
	}
	first, err := nativeToolCallProjection(input, payload)
	if err != nil {
		t.Fatalf("nativeToolCallProjection() error = %v", err)
	}
	second, err := nativeToolCallProjection(input, payload)
	if err != nil {
		t.Fatalf("nativeToolCallProjection() repeat error = %v", err)
	}
	if first.GetCallRef() == "" || first.GetCallRef() != second.GetCallRef() ||
		first.GetMutation().GetIdempotencyKey() != second.GetMutation().GetIdempotencyKey() {
		t.Fatalf("correlation is not deterministic: first=%#v second=%#v", first, second)
	}
	if first.GetTool() != runtimecontract.NativeToolKindFileChange || first.GetCapabilityRef() != "" || first.GetGrantRef() != "" ||
		first.GetSafeParameters().AsMap()["change_count"] != float64(1) ||
		first.GetSafeParameters().AsMap()["codex_item_id"] != payload.CallID {
		t.Fatalf("unexpected native tool projection: %#v", first)
	}
}

func TestNativeToolCallProjectionRejectsUnknownKind(t *testing.T) {
	input := validWarmExecutionInput()
	payload := runtimecontract.RunnerNativeToolCallRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		NativeToolCall: runtimecontract.NativeToolCall{CallID: "call-1", Kind: "CODEX_FUTURE_TOOL",
			State: runtimecontract.NativeToolStateSucceeded, SafeResult: runtimecontract.NativeToolResultCompleted,
			SafeParameters: map[string]any{}}}
	if _, err := nativeToolCallProjection(input, payload); err == nil {
		t.Fatal("unknown native tool kind was accepted")
	}
}
