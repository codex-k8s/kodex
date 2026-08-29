package runtimecontract

import (
	"strings"
	"testing"
)

func TestRunnerNativeToolCallRequestAllowsOnlyClosedSafeProjection(t *testing.T) {
	valid := RunnerNativeToolCallRequest{RuntimeRevisionDigest: strings.Repeat("a", 64), NativeToolCall: NativeToolCall{
		CallID: "call-1", Kind: NativeToolKindShell, State: NativeToolStateSucceeded, DurationMS: 10,
		SafeResult: NativeToolResultCompleted, SafeParameters: map[string]any{
			"action_count": 1, "action_kinds": []string{"READ"}, "cwd_scope": "WORKSPACE", "exit_code": "ZERO", "source": "AGENT",
		},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid native tool call rejected: %v", err)
	}
	for name, mutate := range map[string]func(*RunnerNativeToolCallRequest){
		"kind":   func(value *RunnerNativeToolCallRequest) { value.Kind = "CODEX_UNKNOWN" },
		"state":  func(value *RunnerNativeToolCallRequest) { value.State = "RUNNING" },
		"result": func(value *RunnerNativeToolCallRequest) { value.SafeResult = "raw output" },
		"parameters": func(value *RunnerNativeToolCallRequest) {
			value.SafeParameters = map[string]any{"command": "print secret"}
		},
	} {
		candidate := valid
		mutate(&candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid %s accepted", name)
		}
	}
}
