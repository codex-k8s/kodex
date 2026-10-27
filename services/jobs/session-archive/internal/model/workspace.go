package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// WorkspaceArchiveTask pins work-only archival separately from the legacy rollout receipt.
// Target.Source is absent: a restore source is a separately pinned Checkpoint, never writable shared data.
// WorkspaceArchiveTask закрепляет work-only archive отдельно от legacy rollout receipt.
// Target.Source отсутствует: restore source — отдельный Checkpoint, не shared writable data.
type WorkspaceArchiveTask struct {
	Target        runtimecontract.WorkspaceStorageBinding     `json:"target"`
	CheckpointRef string                                      `json:"checkpoint_ref"`
	Quiescence    string                                      `json:"quiescence,omitempty"`
	PVCUID        string                                      `json:"pvc_uid,omitempty"`
	Policy        runtimecontract.RuntimeWorkspacePolicy      `json:"policy"`
	ReconcileOnly bool                                        `json:"reconcile_only"`
	Checkpoint    *runtimecontract.WorkspaceCheckpointBinding `json:"checkpoint,omitempty"`
}

// errWorkspaceTaskInvalid is a fixed failure without task bytes, source paths or private scope metadata.
// errWorkspaceTaskInvalid — фиксированный отказ без task bytes, source paths и private scope metadata.
var errWorkspaceTaskInvalid = errors.New("session workspace archive task is invalid")

// workspaceArchiveTaskRefPattern matches the existing owner-assigned task namespace without accepting paths.
// workspaceArchiveTaskRefPattern соответствует existing owner-assigned task namespace, не принимая paths.
var workspaceArchiveTaskRefPattern = regexp.MustCompile(`^sat_[A-Za-z0-9_-]{8,89}$`)

// decodeWorkspaceAwareTask preserves legacy decoding while requiring canonical strict V2 task bytes.
// Null, alias-case, unknown or duplicate Workspace keys cannot downgrade a new-profile task to legacy.
// decodeWorkspaceAwareTask сохраняет legacy decoding и требует canonical strict V2 task bytes.
// Null, alias-case, unknown и duplicate Workspace keys не понижают новый профиль до legacy.
func decodeWorkspaceAwareTask(raw []byte) (Task, error) {
	var fields map[string]json.RawMessage
	var decoded wireTask
	if len(raw) < 1 || len(raw) > MaximumTaskBytes || json.Unmarshal(raw, &fields) != nil || json.Unmarshal(raw, &decoded) != nil {
		return Task{}, errWorkspaceTaskInvalid
	}
	newProfile := false
	for key := range fields {
		newProfile = newProfile || strings.EqualFold(key, "Workspace")
	}
	if newProfile {
		decoded = wireTask{}
		if runtimecontract.DecodeWorkspaceStorageTaskJSON(raw, &decoded) != nil || decoded.Workspace == nil {
			return Task{}, errWorkspaceTaskInvalid
		}
		canonical, err := json.Marshal(decoded)
		if err != nil || !bytes.Equal(raw, canonical) {
			return Task{}, errWorkspaceTaskInvalid
		}
	}
	return Task(decoded), nil
}

// SnapshotIdentity derives the only permitted root-manifest identity from an exact positive writer record.
// It is not owner authorization: the controller must separately bind the owned Pod/PVC and frozen task.
// SnapshotIdentity выводит root-manifest identity только из exact positive writer record.
// Это не owner authorization: controller отдельно связывает owned Pod/PVC и frozen task.
func (value WorkspaceArchiveTask) SnapshotIdentity() (runtimecontract.WorkspaceArchiveManifest, error) {
	joined, err := runtimecontract.DecodeWorkspaceQuiescence([]byte(value.Quiescence))
	if err != nil || joined.Scope != value.Target.Scope || joined.WorkspaceRef != value.Target.WorkspaceRef ||
		joined.Generation != value.Target.Generation || joined.Writer.PVCUID != value.PVCUID {
		return runtimecontract.WorkspaceArchiveManifest{}, errWorkspaceTaskInvalid
	}
	writer, err := joined.Commitment()
	identity := runtimecontract.WorkspaceArchiveManifest{Schema: runtimecontract.WorkspaceArchiveSchema,
		Scope: joined.Scope, WorkspaceRef: joined.WorkspaceRef, Generation: joined.Generation,
		CheckpointRef: value.CheckpointRef, Writer: writer}
	if err != nil || identity.ValidateIdentity() != nil {
		return runtimecontract.WorkspaceArchiveManifest{}, errWorkspaceTaskInvalid
	}
	return identity, nil
}

// validateWorkspace enforces the closed V2 task union without inventing Codex identity or fresh execution authority.
// Repeated SNAPSHOT attempts have read-only reconciliation credit; missing objects remain unresolved.
// validateWorkspace проверяет closed V2 task union без выдумывания Codex identity/fresh execution authority.
// Повторные SNAPSHOT attempts имеют только read-only reconciliation; missing objects остаются unresolved.
func (task Task) validateWorkspace() error {
	value := task.Workspace
	if value == nil || !workspaceArchiveTaskRefPattern.MatchString(task.TaskRef) || task.Attempt < 1 || task.Attempt > 5 || task.ContentGeneration < 1 ||
		!workspaceHexDigest(task.InputDigest) || value.Target.Profile != runtimecontract.WorkspaceStorageProfile || value.Target.Source != nil ||
		value.Target.Scope.Validate() != nil || value.Target.Scope.OrganizationRef != task.OrganizationRef || value.Target.Scope.ProjectRef != task.ProjectRef ||
		value.Target.Scope.SessionRef != task.SessionRef || value.Policy.Validate() != nil ||
		value.Policy.MaximumWritableBytes > runtimecontract.RuntimeWorkspaceWritableBytes || value.Policy.MaximumFileCount > runtimecontract.RuntimeWorkspaceMaximumFiles ||
		task.CodexSessionID != "" || task.SourceRelativePath != "" || task.SourceSHA256 != "" || task.SourceSizeBytes != 0 || task.Archive != nil {
		return errWorkspaceTaskInvalid
	}
	if _, err := value.Target.GenerationSubPath(); err != nil {
		return errWorkspaceTaskInvalid
	}
	pvc, err := runtimecontract.SessionPVCName(task.SessionRef)
	if err != nil || task.PVCName != pvc {
		return errWorkspaceTaskInvalid
	}
	switch task.Kind {
	case "SNAPSHOT":
		identity, err := value.SnapshotIdentity()
		if err != nil || value.Checkpoint != nil || value.ReconcileOnly != (task.Attempt > 1) ||
			task.TargetObjectKey != identity.ObjectPrefix()+"manifest.json" || task.TargetObjectVersion != "" ||
			task.RuntimeRevisionRef != identity.Writer.RuntimeRevisionRef || task.RuntimeRevisionVersion != identity.Writer.RuntimeRevisionVersion ||
			task.RuntimeRevisionDigest != identity.Writer.RuntimeRevisionDigest {
			return errWorkspaceTaskInvalid
		}
	case "RESTORE", "DELETE_PVC", "DELETE_OBJECT":
		if value.Checkpoint == nil || value.Checkpoint.Validate() != nil || value.CheckpointRef != value.Checkpoint.CheckpointRef || value.Quiescence != "" ||
			value.Checkpoint.Scope.OrganizationRef != task.OrganizationRef || value.Checkpoint.Scope.ProjectRef != task.ProjectRef ||
			value.Checkpoint.Scope.AgentRef != value.Target.Scope.AgentRef {
			return errWorkspaceTaskInvalid
		}
		if task.Kind == "DELETE_OBJECT" && (task.TargetObjectKey != value.Checkpoint.Manifest.Key || task.TargetObjectVersion != value.Checkpoint.Manifest.Version) {
			return errWorkspaceTaskInvalid
		}
		if task.Kind == "DELETE_PVC" && (value.PVCUID == "" || value.Checkpoint.Scope != value.Target.Scope ||
			value.Checkpoint.WorkspaceRef != value.Target.WorkspaceRef || value.Checkpoint.Generation != value.Target.Generation) {
			return errWorkspaceTaskInvalid
		}
		if task.Kind == "DELETE_OBJECT" && (value.Checkpoint.Scope != value.Target.Scope || value.Checkpoint.WorkspaceRef != value.Target.WorkspaceRef ||
			value.Checkpoint.Generation != value.Target.Generation) {
			return errWorkspaceTaskInvalid
		}
		if task.Kind == "RESTORE" && (value.ReconcileOnly != (task.Attempt > 1) || value.Checkpoint.Scope.SessionRef == task.SessionRef &&
			value.Checkpoint.WorkspaceRef == value.Target.WorkspaceRef && value.Checkpoint.Generation >= value.Target.Generation) {
			return errWorkspaceTaskInvalid
		}
	default:
		return errWorkspaceTaskInvalid
	}
	return nil
}

// workspaceTaskFromProto maps only generated private metadata; it does not authorize the task or read a Secret.
// Invalid assigned source commitments are rejected before any worker Pod is materialized.
// workspaceTaskFromProto переносит только generated private metadata без task authorization или Secret read.
// Invalid source commitments отклоняются до материализации worker Pod.
func workspaceTaskFromProto(input *controlplanev1.SessionWorkspaceArchiveTask) (WorkspaceArchiveTask, error) {
	if input == nil || input.GetTarget() == nil || input.GetTarget().GetSource() != nil || input.GetPolicy() == nil {
		return WorkspaceArchiveTask{}, errWorkspaceTaskInvalid
	}
	target, policy := input.GetTarget(), input.GetPolicy()
	scope := target.GetScope()
	value := WorkspaceArchiveTask{Target: runtimecontract.WorkspaceStorageBinding{Profile: target.GetProfile(),
		Scope: runtimecontract.WorkspaceStorageScope{OrganizationRef: scope.GetOrganizationRef(), ProjectRef: scope.GetProjectRef(),
			SessionRef: scope.GetSessionRef(), AgentRef: scope.GetAgentRef()}, WorkspaceRef: target.GetWorkspaceRef(), Generation: target.GetGeneration()},
		CheckpointRef: input.GetCheckpointRef(), Quiescence: input.GetQuiescence(), PVCUID: input.GetPvcUid(), ReconcileOnly: input.GetReconcileOnly(),
		Policy: runtimecontract.RuntimeWorkspacePolicy{Revision: policy.GetRevision(), Root: policy.GetRoot(),
			MaximumWritableBytes: policy.GetMaximumWritableBytes(), MaximumFileCount: policy.GetMaximumFileCount(), Digest: policy.GetDigest()}}
	for _, rule := range policy.GetRules() {
		if rule == nil {
			return WorkspaceArchiveTask{}, errWorkspaceTaskInvalid
		}
		access := ""
		switch rule.GetAccess() {
		case controlplanev1.RuntimeWorkspaceAccess_RUNTIME_WORKSPACE_ACCESS_READ_ONLY:
			access = runtimecontract.RuntimeWorkspaceReadOnly
		case controlplanev1.RuntimeWorkspaceAccess_RUNTIME_WORKSPACE_ACCESS_WRITABLE:
			access = runtimecontract.RuntimeWorkspaceWritable
		default:
			return WorkspaceArchiveTask{}, errWorkspaceTaskInvalid
		}
		value.Policy.Rules = append(value.Policy.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.GetPath(), Access: access})
	}
	for _, reason := range policy.GetDenialReasons() {
		switch reason {
		case controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_READ_ONLY:
			value.Policy.DenialReasons = append(value.Policy.DenialReasons, runtimecontract.RuntimeWorkspaceReadOnly)
		case controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_QUOTA_EXCEEDED:
			value.Policy.DenialReasons = append(value.Policy.DenialReasons, runtimecontract.RuntimeWorkspaceQuotaExceeded)
		case controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_PATH_OUTSIDE_WORKSPACE:
			value.Policy.DenialReasons = append(value.Policy.DenialReasons, runtimecontract.RuntimeWorkspacePathOutsideWorkspace)
		case controlplanev1.RuntimeWorkspaceDenialReason_RUNTIME_WORKSPACE_DENIAL_REASON_RUNTIME_IO_ERROR:
			value.Policy.DenialReasons = append(value.Policy.DenialReasons, runtimecontract.RuntimeWorkspaceIOError)
		default:
			return WorkspaceArchiveTask{}, errWorkspaceTaskInvalid
		}
	}
	if checkpoint := input.GetCheckpoint(); checkpoint != nil {
		sourceScope, manifest := checkpoint.GetScope(), checkpoint.GetManifest()
		value.Checkpoint = &runtimecontract.WorkspaceCheckpointBinding{Scope: runtimecontract.WorkspaceStorageScope{
			OrganizationRef: sourceScope.GetOrganizationRef(), ProjectRef: sourceScope.GetProjectRef(), SessionRef: sourceScope.GetSessionRef(), AgentRef: sourceScope.GetAgentRef()},
			WorkspaceRef: checkpoint.GetWorkspaceRef(), Generation: checkpoint.GetGeneration(), CheckpointRef: checkpoint.GetCheckpointRef(),
			TreeSHA256: checkpoint.GetTreeSha256(), Bytes: checkpoint.GetBytes(), Entries: checkpoint.GetEntries(),
			Manifest: runtimecontract.WorkspaceObjectCommitment{Key: manifest.GetKey(), Version: manifest.GetVersion(), ETag: manifest.GetEtag(), Digest: manifest.GetDigest(), SizeBytes: manifest.GetSizeBytes()}}
	}
	return value, nil
}

// workspaceHexDigest checks exact lowercase SHA256 syntax instead of accepting length-only metadata.
// workspaceHexDigest проверяет exact lowercase SHA256 syntax, а не только длину metadata.
func workspaceHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, symbol := range value {
		if !(symbol >= '0' && symbol <= '9' || symbol >= 'a' && symbol <= 'f') {
			return false
		}
	}
	return true
}
