package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// WorkspaceArchiveTask preserves the existing task JSON while sharing exact V2 metadata with its owner.
// WorkspaceArchiveTask сохраняет существующий task JSON и общий exact V2 metadata с владельцем.
type WorkspaceArchiveTask = runtimecontract.WorkspaceArchiveTask

// errWorkspaceTaskInvalid is a fixed failure without task bytes, source paths or private scope metadata.
// errWorkspaceTaskInvalid — фиксированный отказ без task bytes, source paths и private scope metadata.
var errWorkspaceTaskInvalid = errors.New("session workspace archive task is invalid")

// WorkspaceCheckpoint binds a successful worker result to its exact assigned V2 snapshot before owner submission.
// It checks bounded metadata only; actual object custody and owner acceptance remain separate proofs.
// WorkspaceCheckpoint связывает успешный worker result с exact assigned V2 snapshot до owner submission.
// Проверяется bounded metadata; actual object custody и owner acceptance остаются отдельными доказательствами.
func (result Result) WorkspaceCheckpoint(task Task) (runtimecontract.WorkspaceCheckpointBinding, error) {
	var empty runtimecontract.WorkspaceCheckpointBinding
	if task.Workspace == nil || task.Kind != "SNAPSHOT" || task.Validate() != nil || !result.Success || result.FormatVersion != 2 ||
		result.SafeErrorCode != "" || result.ObjectKey != task.TargetObjectKey || result.SourceSizeBytes > task.Workspace.Policy.MaximumWritableBytes ||
		result.SourceEntryCount > task.Workspace.Policy.MaximumFileCount {
		return empty, errWorkspaceTaskInvalid
	}
	value := runtimecontract.WorkspaceCheckpointBinding{Scope: task.Workspace.Target.Scope, WorkspaceRef: task.Workspace.Target.WorkspaceRef,
		Generation: task.Workspace.Target.Generation, CheckpointRef: task.Workspace.CheckpointRef, TreeSHA256: result.SourceSHA256,
		Bytes: result.SourceSizeBytes, Entries: result.SourceEntryCount, Manifest: runtimecontract.WorkspaceObjectCommitment{
			Key: result.ObjectKey, Version: result.ObjectVersion, ETag: result.ObjectETag, Digest: result.ObjectDigest, SizeBytes: result.ObjectSizeBytes}}
	if value.Validate() != nil {
		return empty, errWorkspaceTaskInvalid
	}
	return value, nil
}

// MatchesWorkspaceRestore accepts only the exact frozen source metadata, without granting owner acceptance or execution authority.
// MatchesWorkspaceRestore принимает только exact frozen source metadata, не выдавая owner acceptance либо execution authority.
func (result Result) MatchesWorkspaceRestore(task Task) bool {
	if task.Workspace == nil || task.Kind != "RESTORE" || task.Validate() != nil || task.Workspace.PVCUID == "" {
		return false
	}
	source := task.Workspace.Checkpoint
	return result == (Result{Success: true, FormatVersion: 2, ObjectKey: source.Manifest.Key, ObjectVersion: source.Manifest.Version,
		ObjectETag: source.Manifest.ETag, ObjectDigest: source.Manifest.Digest, ObjectSizeBytes: source.Manifest.SizeBytes,
		SourceSHA256: source.TreeSHA256, SourceSizeBytes: source.Bytes, SourceEntryCount: source.Entries})
}

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

// validateWorkspace maps the actual worker task into the same closed contract used by its owner.
// Legacy-only fields stay forbidden even when the shared work envelope is valid.
// validateWorkspace переводит actual worker task в тот же закрытый контракт, что использует owner.
// Legacy-only поля запрещены даже при валидном общем work envelope.
func (task Task) validateWorkspace() error {
	if task.CodexSessionID != "" || task.SourceRelativePath != "" || task.SourceSHA256 != "" ||
		task.SourceSizeBytes != 0 || task.Archive != nil {
		return errWorkspaceTaskInvalid
	}
	work := runtimecontract.WorkspaceArchiveWork{
		TaskRef: task.TaskRef, Kind: task.Kind, OrganizationRef: task.OrganizationRef, ProjectRef: task.ProjectRef, SessionRef: task.SessionRef,
		RuntimeRevisionRef: task.RuntimeRevisionRef, RuntimeRevisionVersion: task.RuntimeRevisionVersion,
		RuntimeRevisionDigest: task.RuntimeRevisionDigest, ContentGeneration: task.ContentGeneration,
		PVCName: task.PVCName, InputDigest: task.InputDigest, TargetObjectKey: task.TargetObjectKey,
		TargetObjectVersion: task.TargetObjectVersion, Attempt: task.Attempt, Workspace: task.Workspace,
	}
	if work.Validate() != nil {
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
