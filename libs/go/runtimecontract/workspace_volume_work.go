package runtimecontract

import (
	"bytes"
	"encoding/json"
	"regexp"
)

const (
	// WorkspaceVolumeCreate selects the single owner-authorized PVC creation attempt; it never grants adoption by name.
	// WorkspaceVolumeCreate выбирает единственную owner-authorized PVC create attempt, не разрешая adoption по имени.
	WorkspaceVolumeCreate = "CREATE_WORKSPACE_VOLUME"
	// WorkspaceVolumePrepare selects trusted empty-root preparation or its explicitly read-only reconciliation.
	// WorkspaceVolumePrepare выбирает trusted empty-root preparation либо её явно read-only reconciliation.
	WorkspaceVolumePrepare = "PREPARE_WORKSPACE_VOLUME"
	// WorkspaceVolumeLocalPathProfile closes the qualified storage class, root geometry and unchanged session volume size.
	// WorkspaceVolumeLocalPathProfile замыкает квалифицированный storage class, root geometry и прежний размер session volume.
	WorkspaceVolumeLocalPathProfile = "LOCAL_PATH_V1"
	// WorkspaceVolumeRequestedBytes preserves the existing twenty-GiB session PVC request without increasing writable-work quotas.
	// WorkspaceVolumeRequestedBytes сохраняет прежний запрос session PVC twenty-GiB без увеличения writable-work quotas.
	WorkspaceVolumeRequestedBytes int64 = 20 << 30
)

// workspaceVolumeTaskRefPattern separates the new closed queue from legacy archive, model and generic task namespaces.
// workspaceVolumeTaskRefPattern отделяет новую closed queue от legacy archive, model и generic task namespaces.
var workspaceVolumeTaskRefPattern = regexp.MustCompile("^svt_[A-Za-z0-9_-]{8,89}$")

// WorkspaceVolumeInput is the frozen owner-selected logical intent, independent of a model RuntimeRevision, provider session and attempt lease.
// CreateTaskRef pins the positive-create lineage; PreparationTaskRef survives read-only recovery after cancellation without reopening the original task.
// WorkspaceVolumeInput — frozen owner-selected logical intent, отдельный от model RuntimeRevision, provider session и attempt lease.
// CreateTaskRef закрепляет positive-create lineage; PreparationTaskRef сохраняется при read-only recovery после отмены без открытия исходной task.
type WorkspaceVolumeInput struct {
	Profile            string                `json:"profile"`
	Scope              WorkspaceStorageScope `json:"scope"`
	WorkspaceRef       string                `json:"workspace_ref"`
	VolumeGeneration   int64                 `json:"volume_generation"`
	CreateTaskRef      string                `json:"create_task_ref"`
	PreparationTaskRef string                `json:"preparation_task_ref,omitempty"`
	PVCUID             string                `json:"pvc_uid,omitempty"`
	ReadOnly           bool                  `json:"read_only"`
}

// WorkspaceVolumeWork freezes the exact effective attempt before materialization; it contains no credential, secret fence or invented model grant.
// ReconcileOnly is derived from immutable owner input and the bounded attempt, never selected freely by a caller.
// WorkspaceVolumeWork фиксирует exact effective attempt до materialization без credential, secret fence или выдуманного model grant.
// ReconcileOnly выводится из immutable owner input и bounded attempt, а не выбирается caller произвольно.
type WorkspaceVolumeWork struct {
	TaskRef       string               `json:"task_ref"`
	Kind          string               `json:"kind"`
	Attempt       int32                `json:"attempt"`
	InputDigest   string               `json:"input_digest"`
	ReconcileOnly bool                 `json:"reconcile_only"`
	Volume        WorkspaceVolumeInput `json:"volume"`
}

// validateWorkspaceVolumeWorkShape closes the task union without requiring the digest's self-reference during construction.
// validateWorkspaceVolumeWorkShape замыкает task union без требования self-reference digest во время построения.
func validateWorkspaceVolumeWorkShape(value WorkspaceVolumeWork) error {
	input := value.Volume
	if !workspaceVolumeTaskRefPattern.MatchString(value.TaskRef) || value.Attempt < 1 || value.Attempt > 5 ||
		input.Profile != WorkspaceVolumeLocalPathProfile || input.Scope.Validate() != nil ||
		!workspaceStorageRefPattern.MatchString(input.WorkspaceRef) || input.VolumeGeneration < 1 ||
		!workspaceVolumeTaskRefPattern.MatchString(input.CreateTaskRef) ||
		value.ReconcileOnly != (input.ReadOnly || value.Attempt > 1) {
		return errWorkspaceStorageInvalid
	}
	switch value.Kind {
	case WorkspaceVolumeCreate:
		if value.TaskRef != input.CreateTaskRef || input.PreparationTaskRef != "" || input.PVCUID != "" || input.ReadOnly {
			return errWorkspaceStorageInvalid
		}
	case WorkspaceVolumePrepare:
		if !workspaceVolumeTaskRefPattern.MatchString(input.PreparationTaskRef) || !workspaceUID(input.PVCUID) ||
			input.PreparationTaskRef == input.CreateTaskRef || value.TaskRef == input.CreateTaskRef ||
			!input.ReadOnly && value.TaskRef != input.PreparationTaskRef {
			return errWorkspaceStorageInvalid
		}
	default:
		return errWorkspaceStorageInvalid
	}
	return nil
}

// Digest binds every effective attempt field while excluding only its own digest text; the owner separately binds lease, actor and workload generation.
// Digest связывает все effective attempt fields, исключая только собственный digest; owner отдельно связывает lease, actor и workload generation.
func (value WorkspaceVolumeWork) Digest() (string, error) {
	if validateWorkspaceVolumeWorkShape(value) != nil {
		return "", errWorkspaceStorageInvalid
	}
	value.InputDigest = ""
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceVolumeReadyBytes {
		return "", errWorkspaceStorageInvalid
	}
	return sha256Sum(raw), nil
}

// Validate rejects mutated attempt, scope, incarnation or operation before a consumer may inspect or modify the physical volume.
// Validate отклоняет изменённые attempt/scope/incarnation/operation до обращения consumer к physical volume.
func (value WorkspaceVolumeWork) Validate() error {
	digest, err := value.Digest()
	if err != nil || !ValidSHA256Text(value.InputDigest) || value.InputDigest != digest {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// EncodeWorkspaceVolumeWork serializes only a validated attempt; a digest alone never authorizes physical execution.
// EncodeWorkspaceVolumeWork сериализует только проверенную attempt; digest сам по себе не разрешает physical execution.
func EncodeWorkspaceVolumeWork(value WorkspaceVolumeWork) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceVolumeReadyBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceVolumeWork rejects unknown, duplicate and aliased fields and requires exact canonical bytes before consumer dispatch.
// DecodeWorkspaceVolumeWork отклоняет unknown/duplicate/aliased fields и требует exact canonical bytes до consumer dispatch.
func DecodeWorkspaceVolumeWork(raw []byte) (WorkspaceVolumeWork, error) {
	var value WorkspaceVolumeWork
	if len(raw) < 1 || len(raw) > MaximumWorkspaceVolumeReadyBytes || DecodeWorkspaceStorageTaskJSON(raw, &value) != nil {
		return WorkspaceVolumeWork{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceVolumeWork(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return WorkspaceVolumeWork{}, errWorkspaceStorageInvalid
	}
	return value, nil
}

// Preparation returns only the logical preparation identity assigned by the persisted owner intent, never a model binding or permission.
// Preparation возвращает только logical preparation identity из persisted owner intent, не model binding или permission.
func (value WorkspaceVolumeWork) Preparation() (WorkspaceVolumePreparation, error) {
	if value.Validate() != nil || value.Kind != WorkspaceVolumePrepare {
		return WorkspaceVolumePreparation{}, errWorkspaceStorageInvalid
	}
	return WorkspaceVolumePreparation{Scope: value.Volume.Scope, WorkspaceRef: value.Volume.WorkspaceRef,
		TaskRef: value.Volume.PreparationTaskRef, PVCUID: value.Volume.PVCUID, VolumeGeneration: value.Volume.VolumeGeneration}, nil
}
