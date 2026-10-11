package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestProtocolEmptySafeArraysRoundTripThroughBoundBroker(t *testing.T) {
	for _, scenario := range []struct {
		name string
		item string
		keys []string
	}{
		{"shell", `{"command":"PRIVATE_SENTINEL","commandActions":[],"cwd":"/workspace/work","exitCode":0,"id":"array-tool","status":"STATUS","type":"commandExecution"}`, []string{"action_kinds"}},
		{"empty-file-change", `{"changes":[],"id":"array-tool","status":"STATUS","type":"fileChange"}`, []string{"change_kinds", "paths"}},
		{"outside-file-change", `{"changes":[{"diff":"PRIVATE_SENTINEL","kind":{"type":"update"},"path":"/outside/PRIVATE_SENTINEL"}],"id":"array-tool","status":"STATUS","type":"fileChange"}`, []string{"paths"}},
		{"unknown-workspace-file-change", `{"changes":[{"diff":"PRIVATE_SENTINEL","kind":{"type":"add"},"path":"/workspace/work/PRIVATE_SENTINEL"}],"id":"array-tool","status":"STATUS","type":"fileChange"}`, []string{"paths"}},
	} {
		for _, terminalStatus := range []string{"completed", "failed", "declined"} {
			t.Run(scenario.name+"/"+terminalStatus, func(t *testing.T) {
				input := diagnosticInput()
				var wire bytes.Buffer
				writer := &brokerFrameWriter{writer: &wire, input: &input}
				state := newProtocolState(testThreadID)
				state.threadID, state.turnID, state.workspaceRoot = testThreadID, testTurnID, "/workspace/work"
				if scenario.name == "unknown-workspace-file-change" {
					state.workspaceRoot = ""
				}
				state.onActivity = writer.activity
				for index, status := range []string{"inProgress", terminalStatus} {
					if err := state.consumeItem(raw(strings.ReplaceAll(scenario.item, "STATUS", status)), index == 1, int64(10+index*10)); err != nil {
						t.Fatal("producer rejected valid fixture")
					}
				}
				if writer.finish(brokerResponse{OK: true, Result: Result{Outcome: "SUCCEEDED"}}) != nil {
					t.Fatal("terminal write failed")
				}
				observed := 0
				result, err := readBoundProviderBrokerResponse(bytes.NewReader(wire.Bytes()), &input, providerWriterUID, func(activity runtimecontract.RuntimeActivity) error {
					observed++
					if activity.ToolCall == nil || activity.ToolCall.Revision != int64(observed) {
						t.Fatal("tool lifecycle changed in transit")
					}
					wantState, wantResult := runtimecontract.NativeToolStateRunning, ""
					if observed == 2 {
						wantState, wantResult, _, _ = terminalNativeState(terminalStatus, nil)
					}
					if activity.ToolCall.State != wantState || activity.ToolCall.SafeResult != wantResult {
						t.Fatal("tool state or result changed in transit")
					}
					for _, key := range scenario.keys {
						values, ok := activity.ToolCall.SafeParameters[key].([]any)
						if !ok || values == nil || len(values) != 0 {
							t.Fatal("empty safe list did not remain an array")
						}
					}
					return nil
				})
				if err != nil || observed != 2 || result.Outcome != "SUCCEEDED" {
					t.Fatalf("valid producer activity failed broker roundtrip: category=%s observed=%d", BrokerResponseFailureCategory(err), observed)
				}
				if bytes.Contains(wire.Bytes(), []byte("PRIVATE_SENTINEL")) {
					t.Fatal("private command, diff or unprojectable path escaped projection")
				}
				for _, key := range scenario.keys {
					encodedKey, _ := json.Marshal(key)
					validArray := append(append([]byte{}, encodedKey...), []byte(":[]")...)
					invalidNull := append(append([]byte{}, encodedKey...), []byte(":null")...)
					forged := bytes.Replace(wire.Bytes(), validArray, invalidNull, 1)
					if bytes.Equal(forged, wire.Bytes()) {
						t.Fatal("fixture lacks canonical array")
					}
					_, err := readBoundProviderBrokerResponse(bytes.NewReader(forged), &input, providerWriterUID)
					if BrokerResponseFailureCategory(err) != "ACTIVITY_SCHEMA" {
						t.Fatal("consumer accepted forged null array")
					}
				}
			})
		}
	}
}

func TestProtocolEmptyArrayNormalizationKeepsFileChangeGuards(t *testing.T) {
	for _, item := range []string{
		`{"changes":[{"diff":"PRIVATE_SENTINEL","kind":{"type":"unknown"},"path":"/outside"}],"id":"invalid-file","status":"completed","type":"fileChange"}`,
		`{"changes":[{"diff":"PRIVATE_SENTINEL","kind":{"type":"update"},"path":null}],"id":"invalid-file","status":"completed","type":"fileChange"}`,
		`{"changes":[],"id":"invalid-file","status":"completed","type":"fileChange","unknown":"PRIVATE_SENTINEL"}`,
	} {
		state := newProtocolState(testThreadID)
		state.workspaceRoot = "/workspace/work"
		observed := false
		state.onActivity = func(runtimecontract.RuntimeActivity) error { observed = true; return nil }
		if state.consumeItem(raw(item), true, 10) == nil || observed {
			t.Fatal("invalid file change reached activity publication")
		}
	}
}
