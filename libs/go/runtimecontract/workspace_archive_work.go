package runtimecontract

import (
	"errors"
	"regexp"
)

// WorkspaceArchiveTask pins work-only archival separately from the legacy rollout receipt.
// Target.Source is absent: a restore source is a separately pinned Checkpoint, never writable shared data.
// WorkspaceArchiveTask закрепляет work-only archive отдельно от legacy rollout receipt.
// Target.Source отсутствует: restore source — отдельный Checkpoint, не shared writable data.
type WorkspaceArchiveTask struct {
	Target        WorkspaceStorageBinding     `json:"target"`
	CheckpointRef string                      `json:"checkpoint_ref"`
	Quiescence    string                      `json:"quiescence,omitempty"`
	PVCUID        string                      `json:"pvc_uid,omitempty"`
	Policy        RuntimeWorkspacePolicy      `json:"policy"`
	ReconcileOnly bool                        `json:"reconcile_only"`
	Checkpoint    *WorkspaceCheckpointBinding `json:"checkpoint,omitempty"`
}

// WorkspaceArchiveWork is the closed work-only task envelope shared by its owner and archive consumer.
// Its references carry no authority; the owner still proves lease, lineage, custody and current generation.
// WorkspaceArchiveWork — закрытый work-only task envelope владельца и archive consumer.
// Его ссылки не дают полномочий; owner отдельно доказывает lease, lineage, custody и current generation.
type WorkspaceArchiveWork struct {
	TaskRef, Kind, OrganizationRef, ProjectRef, SessionRef string
	RuntimeRevisionRef                                     string
	RuntimeRevisionVersion, ContentGeneration              int64
	RuntimeRevisionDigest, PVCName, InputDigest            string
	TargetObjectKey, TargetObjectVersion                   string
	Attempt                                                int32
	Workspace                                              *WorkspaceArchiveTask
}

// errWorkspaceArchiveWorkInvalid rejects malformed metadata without printing private commitments.
// errWorkspaceArchiveWorkInvalid отклоняет неверные metadata без вывода private commitments.
var errWorkspaceArchiveWorkInvalid = errors.New("session workspace archive task is invalid")

// workspaceArchiveTaskRefPattern matches the existing owner-assigned task namespace without accepting paths.
// workspaceArchiveTaskRefPattern соответствует existing owner-assigned task namespace, не принимая paths.
var workspaceArchiveTaskRefPattern = regexp.MustCompile(`^sat_[A-Za-z0-9_-]{8,89}$`)

// SnapshotIdentity derives the only permitted root-manifest identity from an exact positive writer record.
// It is not owner authorization: the controller must separately bind the owned Pod/PVC and frozen task.
// SnapshotIdentity выводит root-manifest identity только из exact positive writer record.
// Это не owner authorization: controller отдельно связывает owned Pod/PVC и frozen task.
func (value WorkspaceArchiveTask) SnapshotIdentity() (WorkspaceArchiveManifest, error) {
	joined, err := DecodeWorkspaceQuiescence([]byte(value.Quiescence))
	if err != nil || joined.Scope != value.Target.Scope || joined.WorkspaceRef != value.Target.WorkspaceRef ||
		joined.Generation != value.Target.Generation || joined.Writer.PVCUID != value.PVCUID {
		return WorkspaceArchiveManifest{}, errWorkspaceArchiveWorkInvalid
	}
	writer, err := joined.Commitment()
	identity := WorkspaceArchiveManifest{Schema: WorkspaceArchiveSchema,
		Scope: joined.Scope, WorkspaceRef: joined.WorkspaceRef, Generation: joined.Generation,
		CheckpointRef: value.CheckpointRef, Writer: writer}
	if err != nil || identity.ValidateIdentity() != nil {
		return WorkspaceArchiveManifest{}, errWorkspaceArchiveWorkInvalid
	}
	return identity, nil
}

// Validate enforces the closed V2 task union without inventing Codex identity or fresh execution authority.
// Repeated SNAPSHOT attempts have read-only reconciliation credit; missing objects remain unresolved.
// Validate проверяет closed V2 task union без выдумывания Codex identity/fresh execution authority.
// Повторные SNAPSHOT attempts имеют только read-only reconciliation; missing objects остаются unresolved.
func (task WorkspaceArchiveWork) Validate() error {
	value := task.Workspace
	if value == nil || !workspaceArchiveTaskRefPattern.MatchString(task.TaskRef) || task.Attempt < 1 || task.Attempt > 5 || task.ContentGeneration < 1 ||
		!ValidSHA256Text(task.InputDigest) || value.Target.Profile != WorkspaceStorageProfile || value.Target.Source != nil ||
		value.Target.Scope.Validate() != nil || value.Target.Scope.OrganizationRef != task.OrganizationRef || value.Target.Scope.ProjectRef != task.ProjectRef ||
		value.Target.Scope.SessionRef != task.SessionRef || value.Policy.Validate() != nil ||
		value.Policy.MaximumWritableBytes > RuntimeWorkspaceWritableBytes || value.Policy.MaximumFileCount > RuntimeWorkspaceMaximumFiles {
		return errWorkspaceArchiveWorkInvalid
	}
	if _, err := value.Target.GenerationSubPath(); err != nil {
		return errWorkspaceArchiveWorkInvalid
	}
	pvc, err := SessionPVCName(task.SessionRef)
	if err != nil || task.PVCName != pvc {
		return errWorkspaceArchiveWorkInvalid
	}
	switch task.Kind {
	case "SNAPSHOT":
		identity, err := value.SnapshotIdentity()
		if err != nil || value.Checkpoint != nil || value.ReconcileOnly != (task.Attempt > 1) ||
			task.TargetObjectKey != identity.ObjectPrefix()+"manifest.json" || task.TargetObjectVersion != "" ||
			task.RuntimeRevisionRef != identity.Writer.RuntimeRevisionRef || task.RuntimeRevisionVersion != identity.Writer.RuntimeRevisionVersion ||
			task.RuntimeRevisionDigest != identity.Writer.RuntimeRevisionDigest {
			return errWorkspaceArchiveWorkInvalid
		}
	case "RESTORE", "DELETE_PVC", "DELETE_OBJECT":
		if value.Checkpoint == nil || value.Checkpoint.Validate() != nil || value.CheckpointRef != value.Checkpoint.CheckpointRef || value.Quiescence != "" ||
			value.Checkpoint.Scope.OrganizationRef != task.OrganizationRef || value.Checkpoint.Scope.ProjectRef != task.ProjectRef ||
			value.Checkpoint.Scope.AgentRef != value.Target.Scope.AgentRef {
			return errWorkspaceArchiveWorkInvalid
		}
		if task.Kind == "DELETE_OBJECT" && (task.TargetObjectKey != value.Checkpoint.Manifest.Key || task.TargetObjectVersion != value.Checkpoint.Manifest.Version) {
			return errWorkspaceArchiveWorkInvalid
		}
		if task.Kind == "DELETE_PVC" && (value.PVCUID == "" || value.Checkpoint.Scope != value.Target.Scope ||
			value.Checkpoint.WorkspaceRef != value.Target.WorkspaceRef || value.Checkpoint.Generation != value.Target.Generation) {
			return errWorkspaceArchiveWorkInvalid
		}
		if task.Kind == "DELETE_OBJECT" && (value.Checkpoint.Scope != value.Target.Scope || value.Checkpoint.WorkspaceRef != value.Target.WorkspaceRef ||
			value.Checkpoint.Generation != value.Target.Generation) {
			return errWorkspaceArchiveWorkInvalid
		}
		if task.Kind == "RESTORE" && (value.ReconcileOnly != (task.Attempt > 1) || value.Checkpoint.Scope.SessionRef == task.SessionRef &&
			value.Checkpoint.WorkspaceRef == value.Target.WorkspaceRef && value.Checkpoint.Generation >= value.Target.Generation) {
			return errWorkspaceArchiveWorkInvalid
		}
	default:
		return errWorkspaceArchiveWorkInvalid
	}
	return nil
}
