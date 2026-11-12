package grpc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestNativeSessionOwnerProjectionNeverHidesMalformedState closes missing or unrecognized assigned metadata.
// TestNativeSessionOwnerProjectionNeverHidesMalformedState закрыто отклоняет missing/unrecognized assigned metadata.
func TestNativeSessionOwnerProjectionNeverHidesMalformedState(t *testing.T) {
	path, _ := runtimecontract.NativeSessionExportPath("ses_native", 2)
	value := runtimecontract.NativeSessionCheckpoint{SessionID: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 2, RelativePath: path, SHA256: strings.Repeat("a", 64), SizeBytes: 800}
	if got, valid := castNativeSession(map[string]any{"nativeSession": value}); !valid || got.GetSessionId() != value.SessionID || got.GetGeneration() != 2 {
		t.Fatal("exact native metadata changed")
	}
	raw, _ := json.Marshal(value)
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("synthetic metadata encoding failed")
	}
	fields["secret"] = "synthetic-do-not-accept"
	for _, invalid := range []any{nil, map[string]any{}, fields, "ses_native"} {
		if _, valid := castNativeSession(map[string]any{"nativeSession": invalid}); valid {
			t.Fatal("invalid assigned state silently accepted")
		}
	}
	if got, valid := castNativeSession(map[string]any{}); !valid || got != nil {
		t.Fatal("new session was assigned fabricated native state")
	}
}
