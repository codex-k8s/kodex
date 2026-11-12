package callback

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// nativeCompletionProto copies already-validated export metadata into the existing owner completion RPC.
// It adds no authority: the input tuple, broker proof and owner writer CAS remain independently required.
// nativeCompletionProto копирует проверенные export metadata в прежний owner completion RPC.
// Не добавляет authority: input tuple, broker proof и owner writer CAS проверяются отдельно.
func nativeCompletionProto(value *runtimecontract.NativeSessionCheckpoint) *cp.NativeSessionCheckpoint {
	if value == nil {
		return nil
	}
	return &cp.NativeSessionCheckpoint{SessionId: value.SessionID, Version: value.Version, Generation: value.Generation, RelativePath: value.RelativePath, Sha256: value.SHA256, SizeBytes: value.SizeBytes, WorkspaceRef: value.WorkspaceRef, WorkspaceGeneration: value.WorkspaceGeneration}
}
