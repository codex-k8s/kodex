package model

import (
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// ErrWorkspaceVolumeClaimInvalid rejects a malformed physical claim without exposing private fields.
// ErrWorkspaceVolumeClaimInvalid отклоняет malformed physical claim без выдачи private fields.
var ErrWorkspaceVolumeClaimInvalid = errors.New("workspace volume claim is invalid")

// WorkspaceVolumeFromProto reconstructs only the immutable physical intent and checks its exact
// digest/closed union. Private lease authority stays outside this work and is validated by app.
// WorkspaceVolumeFromProto восстанавливает только immutable physical intent и проверяет exact
// digest/closed union. Private lease authority остаётся вне work и проверяется app.
func WorkspaceVolumeFromProto(input *cp.WorkspaceVolumeTask) (runtimecontract.WorkspaceVolumeWork, error) {
	invalid := ErrWorkspaceVolumeClaimInvalid
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.GetVolume() == nil ||
		len(input.GetVolume().ProtoReflect().GetUnknown()) != 0 || input.GetVolume().GetScope() == nil ||
		len(input.GetVolume().GetScope().ProtoReflect().GetUnknown()) != 0 {
		return runtimecontract.WorkspaceVolumeWork{}, invalid
	}
	kind := ""
	switch input.GetKind() {
	case cp.WorkspaceVolumeTaskKind_WORKSPACE_VOLUME_TASK_KIND_CREATE:
		kind = runtimecontract.WorkspaceVolumeCreate
	case cp.WorkspaceVolumeTaskKind_WORKSPACE_VOLUME_TASK_KIND_PREPARE:
		kind = runtimecontract.WorkspaceVolumePrepare
	default:
		return runtimecontract.WorkspaceVolumeWork{}, invalid
	}
	volume, scope := input.GetVolume(), input.GetVolume().GetScope()
	work := runtimecontract.WorkspaceVolumeWork{TaskRef: input.GetTaskRef(), Kind: kind, Attempt: input.GetAttempt(),
		InputDigest: input.GetInputDigest(), ReconcileOnly: input.GetReconcileOnly(), Volume: runtimecontract.WorkspaceVolumeInput{
			Profile: volume.GetProfile(), WorkspaceRef: volume.GetWorkspaceRef(), VolumeGeneration: volume.GetVolumeGeneration(),
			CreateTaskRef: volume.GetCreateTaskRef(), PreparationTaskRef: volume.GetPreparationTaskRef(), PVCUID: volume.GetPvcUid(), ReadOnly: volume.GetReadOnly(),
			Scope: runtimecontract.WorkspaceStorageScope{OrganizationRef: scope.GetOrganizationRef(), ProjectRef: scope.GetProjectRef(),
				SessionRef: scope.GetSessionRef(), AgentRef: scope.GetAgentRef()}}}
	if work.Validate() != nil {
		return runtimecontract.WorkspaceVolumeWork{}, invalid
	}
	return work, nil
}
