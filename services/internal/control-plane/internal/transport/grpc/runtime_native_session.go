package grpc

import (
	"encoding/json"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// castNativeSession projects only validated metadata selected by the authoritative owner snapshot.
// Absence means a new native session; malformed assigned state is never silently suppressed.
// castNativeSession переносит только проверенные metadata из authoritative owner snapshot.
// Отсутствие означает новую native session; malformed assigned state не скрывается.
func castNativeSession(values map[string]any) (*cp.NativeSessionCheckpoint, bool) {
	value, present := values["nativeSession"]
	if !present {
		return nil, true
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	checkpoint, err := runtimecontract.DecodeNativeSessionCheckpoint(raw)
	if err != nil {
		return nil, false
	}
	return &cp.NativeSessionCheckpoint{SessionId: checkpoint.SessionID, Version: checkpoint.Version, Generation: checkpoint.Generation, RelativePath: checkpoint.RelativePath, Sha256: checkpoint.SHA256, SizeBytes: checkpoint.SizeBytes, WorkspaceRef: checkpoint.WorkspaceRef, WorkspaceGeneration: checkpoint.WorkspaceGeneration}, true
}

// nativeCompletionFromProto rejects unknown wire fields before passing worker evidence to the owner.
// It does not establish ownership, generation or a stopped writer; the owner transaction checks those.
// nativeCompletionFromProto отклоняет unknown wire fields до передачи worker evidence владельцу.
// Не доказывает ownership, generation или stopped writer: их проверяет owner transaction.
func nativeCompletionFromProto(value *cp.NativeSessionCheckpoint) (*runtimecontract.NativeSessionCheckpoint, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 {
		return nil, runtimecontract.ErrNativeSession
	}
	checkpoint := runtimecontract.NativeSessionCheckpoint{SessionID: value.GetSessionId(), Version: value.GetVersion(), Generation: value.GetGeneration(), RelativePath: value.GetRelativePath(), SHA256: value.GetSha256(), SizeBytes: value.GetSizeBytes(), WorkspaceRef: value.GetWorkspaceRef(), WorkspaceGeneration: value.GetWorkspaceGeneration()}
	if checkpoint.Validate() != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	return &checkpoint, nil
}
