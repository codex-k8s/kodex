package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// castWorkspaceArchiveWork maps a validated owner envelope to the existing field22 task union.
// The lease was issued by the owner separately; this projection creates no execution or storage authority.
// castWorkspaceArchiveWork переносит проверенный owner envelope в существующий field22 task union.
// Lease отдельно выдан owner; эта проекция не создаёт execution либо storage authority.
func castWorkspaceArchiveWork(work runtimecontract.WorkspaceArchiveWork, lease *cp.WorkLease) *cp.SessionArchiveTask {
	value := work.Workspace
	policy := &cp.RuntimeWorkspacePolicy{Revision: value.Policy.Revision, Root: value.Policy.Root, Digest: value.Policy.Digest,
		MaximumWritableBytes: value.Policy.MaximumWritableBytes, MaximumFileCount: value.Policy.MaximumFileCount}
	for _, rule := range value.Policy.Rules {
		policy.Rules = append(policy.Rules, &cp.RuntimeWorkspacePathRule{Path: rule.Path,
			Access: cp.RuntimeWorkspaceAccess(cp.RuntimeWorkspaceAccess_value["RUNTIME_WORKSPACE_ACCESS_"+rule.Access])})
	}
	for _, reason := range value.Policy.DenialReasons {
		policy.DenialReasons = append(policy.DenialReasons,
			cp.RuntimeWorkspaceDenialReason(cp.RuntimeWorkspaceDenialReason_value["RUNTIME_WORKSPACE_DENIAL_REASON_"+reason]))
	}
	return &cp.SessionArchiveTask{TaskRef: work.TaskRef, Kind: castSessionArchiveKind(work.Kind), OrganizationRef: work.OrganizationRef,
		ProjectRef: work.ProjectRef, SessionRef: work.SessionRef, RuntimeRevisionRef: work.RuntimeRevisionRef,
		RuntimeRevisionVersion: work.RuntimeRevisionVersion, RuntimeRevisionDigest: work.RuntimeRevisionDigest,
		ContentGeneration: work.ContentGeneration, PvcName: work.PVCName, InputDigest: work.InputDigest,
		TargetObjectKey: work.TargetObjectKey, TargetObjectVersion: work.TargetObjectVersion, Attempt: work.Attempt, Lease: lease,
		Workspace: &cp.SessionWorkspaceArchiveTask{Target: &cp.RuntimeWorkspaceStorage{Profile: value.Target.Profile,
			Scope: castWorkspaceScope(value.Target.Scope), WorkspaceRef: value.Target.WorkspaceRef, Generation: value.Target.Generation},
			CheckpointRef: value.CheckpointRef, Quiescence: value.Quiescence, PvcUid: value.PVCUID,
			ReconcileOnly: value.ReconcileOnly, Policy: policy, Checkpoint: castWorkspaceCheckpoint(value.Checkpoint)}}
}

// castSessionArchiveKind preserves the closed legacy and V2 discriminators without normalizing unknown kinds.
// castSessionArchiveKind сохраняет закрытые legacy/V2 discriminators без нормализации неизвестных видов.
func castSessionArchiveKind(kind string) cp.SessionArchiveTaskKind {
	switch kind {
	case "SNAPSHOT":
		return cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT
	case "RESTORE":
		return cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_RESTORE
	case "DELETE_PVC":
		return cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_PVC
	case "DELETE_OBJECT":
		return cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_OBJECT
	default:
		return cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_UNSPECIFIED
	}
}
