package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRunnerNativeToolCallRequestAllowsOnlyClosedSafeProjection accepts a pinned safe shell summary
// and rejects unknown kinds, inconsistent lifecycle, raw results, and arbitrary command parameters.
// TestRunnerNativeToolCallRequestAllowsOnlyClosedSafeProjection допускает закреплённую безопасную сводку shell,
// отклоняя неизвестные виды, неверный lifecycle, сырые результаты и произвольные параметры команд.
func TestRunnerNativeToolCallRequestAllowsOnlyClosedSafeProjection(t *testing.T) {
	valid := RunnerNativeToolCallRequest{RuntimeRevisionDigest: strings.Repeat("a", 64), NativeToolCall: NativeToolCall{
		CallID: "call-1", Revision: 2, Kind: NativeToolKindShell, State: NativeToolStateSucceeded, DurationMS: 10,
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

// TestNativeToolCallEmptySafeArraysValidatedJSONRoundTrip preserves explicit empty arrays across
// all lifecycle states and JSON persistence while rejecting null substitutions.
// TestNativeToolCallEmptySafeArraysValidatedJSONRoundTrip сохраняет явные пустые массивы во всех
// состояниях lifecycle и JSON-сохранении, отклоняя подмену на null.
func TestNativeToolCallEmptySafeArraysValidatedJSONRoundTrip(t *testing.T) {
	for _, call := range []NativeToolCall{
		{CallID: "shell-empty", Revision: 2, Kind: NativeToolKindShell, State: NativeToolStateSucceeded, SafeResult: NativeToolResultCompleted,
			SafeParameters: map[string]any{"action_count": 0, "action_kinds": []string{}, "cwd_scope": "WORKSPACE", "exit_code": "ZERO", "source": "AGENT"}},
		{CallID: "files-empty", Revision: 2, Kind: NativeToolKindFileChange, State: NativeToolStateSucceeded, SafeResult: NativeToolResultCompleted,
			SafeParameters: map[string]any{"change_count": 0, "change_kinds": []string{}, "paths": []string{}, "paths_truncated": false}},
		{CallID: "files-redacted", Revision: 2, Kind: NativeToolKindFileChange, State: NativeToolStateSucceeded, SafeResult: NativeToolResultCompleted,
			SafeParameters: map[string]any{"change_count": 1, "change_kinds": []string{"UPDATE"}, "paths": []string{}, "paths_truncated": false}},
	} {
		for _, lifecycle := range []struct{ state, result string }{
			{NativeToolStateRunning, ""},
			{NativeToolStateSucceeded, NativeToolResultCompleted},
			{NativeToolStateFailed, NativeToolResultFailed},
			{NativeToolStateCancelled, "CANCELLED"},
		} {
			t.Run(call.CallID+"/"+lifecycle.state, func(t *testing.T) {
				call.State, call.SafeResult = lifecycle.state, lifecycle.result
				call.Revision = 2
				if lifecycle.state == NativeToolStateRunning {
					call.Revision = 1
				}
				if call.Validate() != nil {
					t.Fatal("valid producer rejected")
				}
				encoded, err := json.Marshal(call)
				var decoded NativeToolCall
				if err != nil || json.Unmarshal(encoded, &decoded) != nil || decoded.Validate() != nil {
					t.Fatal("valid safe arrays failed JSON roundtrip")
				}
				for key, value := range decoded.SafeParameters {
					if _, isArray := value.([]any); !isArray {
						continue
					}
					decoded.SafeParameters[key] = nil
					if decoded.Validate() == nil {
						t.Fatal("null array accepted")
					}
					decoded.SafeParameters[key] = value
				}
			})
		}
	}
}
