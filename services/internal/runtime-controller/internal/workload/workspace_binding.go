package workload

import (
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// workspaceStorageFromProto preserves nil legacy and maps every private checkpoint commitment.
// Missing/invalid assigned metadata fails closed; full execution scope is validated after base input.
// workspaceStorageFromProto сохраняет nil legacy и переносит каждую private checkpoint commitment.
// Missing/invalid assigned metadata закрыто отклоняется; execution scope проверяется после base input.
func workspaceStorageFromProto(value *cp.RuntimeWorkspaceStorage) (*runtimecontract.WorkspaceStorageBinding, error) {
	if value == nil {
		return nil, nil
	}
	binding := &runtimecontract.WorkspaceStorageBinding{Profile: value.GetProfile(), Scope: workspaceScopeFromProto(value.GetScope()),
		WorkspaceRef: value.GetWorkspaceRef(), Generation: value.GetGeneration()}
	if source := value.GetSource(); source != nil {
		manifest := source.GetManifest()
		binding.Source = &runtimecontract.WorkspaceCheckpointBinding{Scope: workspaceScopeFromProto(source.GetScope()),
			WorkspaceRef: source.GetWorkspaceRef(), Generation: source.GetGeneration(), CheckpointRef: source.GetCheckpointRef(),
			TreeSHA256: source.GetTreeSha256(), Bytes: source.GetBytes(), Entries: source.GetEntries(),
			Manifest: runtimecontract.WorkspaceObjectCommitment{Key: manifest.GetKey(), Version: manifest.GetVersion(), ETag: manifest.GetEtag(),
				Digest: manifest.GetDigest(), SizeBytes: manifest.GetSizeBytes()}}
		if binding.Source.Validate() != nil {
			return nil, errors.New("runtime workspace source is invalid")
		}
	}
	if _, err := binding.GenerationSubPath(); err != nil {
		return nil, errors.New("runtime workspace storage is invalid")
	}
	return binding, nil
}

// workspaceScopeFromProto copies internal data ownership without treating references as capability.
// workspaceScopeFromProto копирует internal data ownership, не превращая refs в capability.
func workspaceScopeFromProto(value *cp.RuntimeWorkspaceScope) runtimecontract.WorkspaceStorageScope {
	return runtimecontract.WorkspaceStorageScope{OrganizationRef: value.GetOrganizationRef(), ProjectRef: value.GetProjectRef(),
		SessionRef: value.GetSessionRef(), AgentRef: value.GetAgentRef()}
}
