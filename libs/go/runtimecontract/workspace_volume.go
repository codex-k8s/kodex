package runtimecontract

import (
	"bytes"
	"encoding/json"
)

const (
	// WorkspaceVolumeReadySchema identifies a local root-preparation observation, never execution authority or proof of empty business data.
	// WorkspaceVolumeReadySchema обозначает local root-preparation observation, не execution authority или доказательство пустоты business data.
	WorkspaceVolumeReadySchema = "kodex.workspace-volume-ready.v1"
	// MaximumWorkspaceVolumeReadyBytes bounds private metadata below the existing termination-message ceiling.
	// MaximumWorkspaceVolumeReadyBytes ограничивает private metadata ниже существующего termination-message ceiling.
	MaximumWorkspaceVolumeReadyBytes = 2048
	// WorkspaceVolumeMetadataDirectory separates trusted volume custody from provider history and all writable work generations.
	// WorkspaceVolumeMetadataDirectory отделяет trusted volume custody от provider history и writable work generations.
	WorkspaceVolumeMetadataDirectory = "volume-meta"
	// WorkspaceVolumeReadyFile is the immutable record read by trusted preparation and runtime initializers, never model containers.
	// WorkspaceVolumeReadyFile — immutable record для trusted preparation/runtime initializers, не model containers.
	WorkspaceVolumeReadyFile = "ready.json"
)

// WorkspaceVolumePreparation pins one owner-selected physical volume and logical preparation independently of model RuntimeRevision or work generation.
// Its references do not authorize creation, adoption or initialization; the consumer must prove the exact stored task grant and positive PVC UID first.
// WorkspaceVolumePreparation закрепляет owner-selected physical volume и logical preparation отдельно от model RuntimeRevision/work generation.
// Ссылки не разрешают create/adoption/init: consumer сначала доказывает exact stored task grant и positive PVC UID.
type WorkspaceVolumePreparation struct {
	Scope            WorkspaceStorageScope `json:"scope"`
	WorkspaceRef     string                `json:"workspace_ref"`
	TaskRef          string                `json:"task_ref"`
	PVCUID           string                `json:"pvc_uid"`
	VolumeGeneration int64                 `json:"volume_generation"`
}

// Validate rejects absent scope, borrowed path-like references and missing physical identity without inventing a model execution.
// VolumeGeneration is the owner's monotonically increasing volume incarnation, not a writer lease, attempt or writable tree generation.
// Validate отклоняет отсутствующий scope, path-like references и missing physical identity без выдумывания model execution.
// VolumeGeneration — монотонная owner incarnation тома, не writer lease, attempt или generation рабочего дерева.
func (value WorkspaceVolumePreparation) Validate() error {
	if value.Scope.Validate() != nil || !workspaceStorageRefPattern.MatchString(value.WorkspaceRef) ||
		!workspaceArchiveTaskRefPattern.MatchString(value.TaskRef) || !workspaceUID(value.PVCUID) || value.VolumeGeneration < 1 {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// EncodeWorkspaceVolumePreparation freezes bounded canonical preparation input without inventing a RuntimeRevision, provider identity or lease grant.
// EncodeWorkspaceVolumePreparation фиксирует bounded canonical preparation input без выдуманной RuntimeRevision, provider identity или lease grant.
func EncodeWorkspaceVolumePreparation(value WorkspaceVolumePreparation) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceVolumeReadyBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceVolumePreparation rejects noncanonical input before a trusted helper opens the assigned volume.
// DecodeWorkspaceVolumePreparation отклоняет noncanonical input до открытия назначенного тома trusted helper.
func DecodeWorkspaceVolumePreparation(raw []byte) (WorkspaceVolumePreparation, error) {
	var value WorkspaceVolumePreparation
	if len(raw) < 1 || len(raw) > MaximumWorkspaceVolumeReadyBytes || DecodeWorkspaceStorageTaskJSON(raw, &value) != nil {
		return WorkspaceVolumePreparation{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceVolumePreparation(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return WorkspaceVolumePreparation{}, errWorkspaceStorageInvalid
	}
	return value, nil
}

// WorkspaceVolumeReady binds the preparation intent to an observed root inode; later private data may exist below that root.
// Owner acceptance, positive process join and exact Job/PVC provenance remain separate and cannot be inferred from these bytes.
// WorkspaceVolumeReady связывает preparation intent с observed root inode; позднее под ним могут находиться private data.
// Owner acceptance, positive process join и exact Job/PVC provenance проверяются отдельно, а не выводятся из этих bytes.
type WorkspaceVolumeReady struct {
	Schema      string                     `json:"schema"`
	Preparation WorkspaceVolumePreparation `json:"preparation"`
	Device      uint64                     `json:"device"`
	Inode       uint64                     `json:"inode"`
}

// EncodeWorkspaceVolumeReady produces one canonical bounded record without lease fences, credentials or private contents.
// EncodeWorkspaceVolumeReady создаёт canonical bounded record без lease fences, credentials или private contents.
func EncodeWorkspaceVolumeReady(value WorkspaceVolumeReady) ([]byte, error) {
	if value.Schema != WorkspaceVolumeReadySchema || value.Preparation.Validate() != nil || value.Device == 0 || value.Inode == 0 {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceVolumeReadyBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceVolumeReady accepts only exact canonical bytes; malformed, duplicate, unknown, aliased or oversized metadata stays untrusted.
// DecodeWorkspaceVolumeReady принимает только exact canonical bytes; malformed/duplicate/unknown/alias/oversized metadata остаются недоверенными.
func DecodeWorkspaceVolumeReady(raw []byte) (WorkspaceVolumeReady, error) {
	var value WorkspaceVolumeReady
	if len(raw) < 1 || len(raw) > MaximumWorkspaceVolumeReadyBytes || DecodeWorkspaceStorageTaskJSON(raw, &value) != nil {
		return WorkspaceVolumeReady{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceVolumeReady(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return WorkspaceVolumeReady{}, errWorkspaceStorageInvalid
	}
	return value, nil
}
