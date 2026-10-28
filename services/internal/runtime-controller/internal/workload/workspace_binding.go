package workload

import (
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

var (
	// errWorkspaceStorageBinding closes malformed assigned storage without leaking private metadata.
	// errWorkspaceStorageBinding закрыто отклоняет malformed assigned storage без выдачи private metadata.
	errWorkspaceStorageBinding = errors.New("runtime workspace storage is invalid")
	// errWorkspaceSourceBinding rejects an invalid owner-selected checkpoint before materialization.
	// errWorkspaceSourceBinding отклоняет invalid owner-selected checkpoint до materialization.
	errWorkspaceSourceBinding = errors.New("runtime workspace source is invalid")
	// errWorkspaceVolumePreparation rejects an incomplete or unknown sealed physical observation.
	// errWorkspaceVolumePreparation отклоняет incomplete либо unknown sealed physical observation.
	errWorkspaceVolumePreparation = errors.New("runtime workspace volume preparation is invalid")
)

// workspaceStorageFromProto preserves nil legacy and maps every private checkpoint commitment.
// Missing/invalid assigned metadata fails closed; full execution scope is validated after base input.
// workspaceStorageFromProto сохраняет nil legacy и переносит каждую private checkpoint commitment.
// Missing/invalid assigned metadata закрыто отклоняется; execution scope проверяется после base input.
func workspaceStorageFromProto(value *cp.RuntimeWorkspaceStorage) (*runtimecontract.WorkspaceStorageBinding, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 || value.GetScope() == nil || len(value.GetScope().ProtoReflect().GetUnknown()) != 0 {
		return nil, errWorkspaceStorageBinding
	}
	binding := &runtimecontract.WorkspaceStorageBinding{Profile: value.GetProfile(), Scope: workspaceScopeFromProto(value.GetScope()),
		WorkspaceRef: value.GetWorkspaceRef(), Generation: value.GetGeneration()}
	ready, err := workspaceVolumeReadyFromProto(value.GetVolumeReady())
	if err != nil {
		return nil, err
	}
	binding.VolumeReady = ready
	if source := value.GetSource(); source != nil {
		manifest := source.GetManifest()
		if len(source.ProtoReflect().GetUnknown()) != 0 || source.GetScope() == nil || len(source.GetScope().ProtoReflect().GetUnknown()) != 0 ||
			manifest == nil || len(manifest.ProtoReflect().GetUnknown()) != 0 {
			return nil, errWorkspaceSourceBinding
		}
		binding.Source = &runtimecontract.WorkspaceCheckpointBinding{Scope: workspaceScopeFromProto(source.GetScope()),
			WorkspaceRef: source.GetWorkspaceRef(), Generation: source.GetGeneration(), CheckpointRef: source.GetCheckpointRef(),
			TreeSHA256: source.GetTreeSha256(), Bytes: source.GetBytes(), Entries: source.GetEntries(),
			Manifest: runtimecontract.WorkspaceObjectCommitment{Key: manifest.GetKey(), Version: manifest.GetVersion(), ETag: manifest.GetEtag(),
				Digest: manifest.GetDigest(), SizeBytes: manifest.GetSizeBytes()}}
		if binding.Source.Validate() != nil {
			return nil, errWorkspaceSourceBinding
		}
	}
	if _, err := binding.GenerationSubPath(); err != nil {
		return nil, errWorkspaceStorageBinding
	}
	return binding, nil
}

// workspaceVolumeReadyFromProto preserves sealed physical identity and refuses unknown nested wire data.
// Nil remains distinguishable from an accepted record; it never manufactures preparation or a lease.
// workspaceVolumeReadyFromProto сохраняет sealed physical identity и отклоняет unknown nested wire data.
// Nil отличается от accepted record; preparation либо lease не создаются.
func workspaceVolumeReadyFromProto(value *cp.RuntimeWorkspaceVolumeReady) (*runtimecontract.WorkspaceVolumeReady, error) {
	if value == nil {
		return nil, nil
	}
	preparation := value.GetPreparation()
	if len(value.ProtoReflect().GetUnknown()) != 0 || preparation == nil || len(preparation.ProtoReflect().GetUnknown()) != 0 ||
		preparation.GetScope() == nil || len(preparation.GetScope().ProtoReflect().GetUnknown()) != 0 {
		return nil, errWorkspaceVolumePreparation
	}
	ready := &runtimecontract.WorkspaceVolumeReady{Schema: value.GetSchema(), Device: value.GetDevice(), Inode: value.GetInode(),
		Preparation: runtimecontract.WorkspaceVolumePreparation{Scope: workspaceScopeFromProto(preparation.GetScope()),
			WorkspaceRef: preparation.GetWorkspaceRef(), TaskRef: preparation.GetTaskRef(), PVCUID: preparation.GetPvcUid(),
			VolumeGeneration: preparation.GetVolumeGeneration()}}
	if _, err := runtimecontract.EncodeWorkspaceVolumeReady(*ready); err != nil {
		return nil, errWorkspaceVolumePreparation
	}
	return ready, nil
}

// workspaceScopeFromProto copies internal data ownership without treating references as capability.
// workspaceScopeFromProto копирует internal data ownership, не превращая refs в capability.
func workspaceScopeFromProto(value *cp.RuntimeWorkspaceScope) runtimecontract.WorkspaceStorageScope {
	return runtimecontract.WorkspaceStorageScope{OrganizationRef: value.GetOrganizationRef(), ProjectRef: value.GetProjectRef(),
		SessionRef: value.GetSessionRef(), AgentRef: value.GetAgentRef()}
}
