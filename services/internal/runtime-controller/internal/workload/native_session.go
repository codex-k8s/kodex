package workload

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// nativeSessionFromProto preserves every owner export commitment and rejects unknown wire fields.
// A valid projection is not authority; the complete current RunnerInput is validated after materialization.
// nativeSessionFromProto сохраняет все owner export commitments и отклоняет unknown wire fields.
// Верная projection не является authority; полный current RunnerInput проверяется после materialization.
func nativeSessionFromProto(value *cp.NativeSessionCheckpoint) (*runtimecontract.NativeSessionCheckpoint, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 {
		return nil, runtimecontract.ErrNativeSession
	}
	result := &runtimecontract.NativeSessionCheckpoint{SessionID: value.GetSessionId(), Version: value.GetVersion(), Generation: value.GetGeneration(), RelativePath: value.GetRelativePath(), SHA256: value.GetSha256(), SizeBytes: value.GetSizeBytes(), WorkspaceRef: value.GetWorkspaceRef(), WorkspaceGeneration: value.GetWorkspaceGeneration()}
	if result.Validate() != nil {
		return nil, runtimecontract.ErrNativeSession
	}
	return result, nil
}
