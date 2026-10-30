package grpc

import (
	"bytes"
	"encoding/json"
	"io"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castRuntimeWorkspace accepts absent legacy metadata but never suppresses an invalid assigned V2.
// Scope and quotas come from the owner snapshot, not from the storage descriptor's own references.
// castRuntimeWorkspace сохраняет absent legacy metadata, но не скрывает invalid assigned V2.
// Scope и quotas берутся из owner snapshot, не из ссылок самого storage descriptor.
func castRuntimeWorkspace(result *cp.RuntimeRevisionSnapshot, values map[string]any) bool {
	value, present := values["workspaceStorage"]
	if !present {
		return true
	}
	raw, err := json.Marshal(value)
	if err != nil || bytes.Equal(raw, []byte("null")) || len(raw) > runtimecontract.MaximumWorkspaceManifestBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var binding runtimecontract.WorkspaceStorageBinding
	if decoder.Decode(&binding) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	policy, valid := values["workspacePolicy"].(entity.RuntimeWorkspacePolicy)
	if !valid {
		return false
	}
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn,
		RoleRuntimeContractRevision: result.RoleRuntimeContractRevision,
		AssistantScope:              runtimecontract.AssistantScope(mapString(values, "assistantScope")),
		OrganizationRef:             result.OrganizationRef, ProjectRef: mapString(values, "projectRef"), SessionRef: result.SessionRef, AgentRef: result.AgentRef,
		WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicy{Revision: policy.Revision, Root: policy.Root, Digest: policy.Digest,
			MaximumWritableBytes: policy.MaximumWritableBytes, MaximumFileCount: policy.MaximumFileCount, DenialReasons: policy.DenialReasons}}
	for _, rule := range policy.Rules {
		input.WorkspacePolicy.Rules = append(input.WorkspacePolicy.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.Path, Access: rule.Access})
	}
	if input.WorkspacePolicy.Validate() != nil || binding.ValidateForInput(input) != nil {
		return false
	}
	result.WorkspaceStorage = &cp.RuntimeWorkspaceStorage{Profile: binding.Profile, Scope: castWorkspaceScope(binding.Scope),
		WorkspaceRef: binding.WorkspaceRef, Generation: binding.Generation}
	result.WorkspaceStorage.Source = castWorkspaceCheckpoint(binding.Source)
	result.WorkspaceStorage.VolumeReady = castWorkspaceVolumeReady(binding.VolumeReady)
	return true
}

// castWorkspaceVolumeReady preserves the owner's exact sealed preparation without adding execution authority.
// castWorkspaceVolumeReady сохраняет exact sealed preparation owner без добавления execution authority.
func castWorkspaceVolumeReady(value *runtimecontract.WorkspaceVolumeReady) *cp.RuntimeWorkspaceVolumeReady {
	if value == nil {
		return nil
	}
	preparation := value.Preparation
	return &cp.RuntimeWorkspaceVolumeReady{Schema: value.Schema, Device: value.Device, Inode: value.Inode,
		Preparation: &cp.RuntimeWorkspaceVolumePreparation{Scope: castWorkspaceScope(preparation.Scope),
			WorkspaceRef: preparation.WorkspaceRef, TaskRef: preparation.TaskRef, PvcUid: preparation.PVCUID,
			VolumeGeneration: preparation.VolumeGeneration}}
}

// castWorkspaceScope copies only already-validated data-owner references into internal transport.
// castWorkspaceScope копирует только проверенные data-owner refs во внутренний transport.
func castWorkspaceScope(value runtimecontract.WorkspaceStorageScope) *cp.RuntimeWorkspaceScope {
	return &cp.RuntimeWorkspaceScope{OrganizationRef: value.OrganizationRef, ProjectRef: value.ProjectRef,
		SessionRef: value.SessionRef, AgentRef: value.AgentRef}
}

// castWorkspaceCheckpoint projects a previously validated immutable checkpoint without changing its source scope.
// castWorkspaceCheckpoint переносит проверенный immutable checkpoint, не меняя его source scope.
func castWorkspaceCheckpoint(source *runtimecontract.WorkspaceCheckpointBinding) *cp.RuntimeWorkspaceCheckpoint {
	if source == nil {
		return nil
	}
	return &cp.RuntimeWorkspaceCheckpoint{Scope: castWorkspaceScope(source.Scope), WorkspaceRef: source.WorkspaceRef,
		Generation: source.Generation, CheckpointRef: source.CheckpointRef, TreeSha256: source.TreeSHA256, Bytes: source.Bytes, Entries: source.Entries,
		Manifest: &cp.RuntimeWorkspaceObject{Key: source.Manifest.Key, Version: source.Manifest.Version, Etag: source.Manifest.ETag,
			Digest: source.Manifest.Digest, SizeBytes: source.Manifest.SizeBytes}}
}
