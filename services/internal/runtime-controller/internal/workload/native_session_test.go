package workload

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestNativeSessionWirePreservesExactCommitments rejects unknown fields and source substitution.
// TestNativeSessionWirePreservesExactCommitments отклоняет unknown fields и подмену source.
func TestNativeSessionWirePreservesExactCommitments(t *testing.T) {
	path, _ := runtimecontract.NativeSessionExportPath("ses_native", 2)
	value := &cp.NativeSessionCheckpoint{SessionId: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 2, RelativePath: path, Sha256: strings.Repeat("a", 64), SizeBytes: 800, WorkspaceRef: "wks_synthetic", WorkspaceGeneration: 3}
	got, err := nativeSessionFromProto(value)
	if err != nil || got.SessionID != value.SessionId || got.RelativePath != path || got.WorkspaceGeneration != 3 || got.WorkspaceRef != value.WorkspaceRef || got.SHA256 != value.Sha256 {
		t.Fatal("native commitments changed at wire boundary")
	}
	value.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	if _, err := nativeSessionFromProto(value); err == nil {
		t.Fatal("unknown native metadata accepted")
	}
	value.ProtoReflect().SetUnknown(nil)
	value.SessionId = "ses_foreign"
	if _, err := nativeSessionFromProto(value); err == nil {
		t.Fatal("substituted native identity accepted")
	}
}
