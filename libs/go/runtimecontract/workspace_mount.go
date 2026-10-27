package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
)

const (
	// WorkspaceMountProofSchema identifies local inode evidence produced only after owner BIND.
	// WorkspaceMountProofSchema обозначает local inode evidence, создаваемое лишь после owner BIND.
	WorkspaceMountProofSchema = "kodex.runtime-workspace-mount.v1"
	// WorkspaceMountProofPath is an exact read-only file subPath, never a private-parent mount.
	// WorkspaceMountProofPath — exact read-only file subPath, не mount private parent.
	WorkspaceMountProofPath = "/var/run/config/kodex/workspace/mount.json"
	// MaximumWorkspaceMountProofBytes bounds local metadata independently of the data tree.
	// MaximumWorkspaceMountProofBytes ограничивает local metadata отдельно от data tree.
	MaximumWorkspaceMountProofBytes = 1024
)

// WorkspaceMountProof associates one observed inode with the current immutable materialization.
// It is not a writer grant and contains no PVC contents, secret fence or restore object keys.
// WorkspaceMountProof связывает observed inode с current immutable materialization.
// Это не writer grant; proof не содержит PVC contents, secret fence или restore object keys.
type WorkspaceMountProof struct {
	Schema                 string `json:"schema"`
	BindingSHA256          string `json:"binding_sha256"`
	ExecutionBindingDigest string `json:"execution_binding_digest"`
	Device                 uint64 `json:"device"`
	Inode                  uint64 `json:"inode"`
}

// NewWorkspaceMountProof accepts trusted inode observations after current owner/input validation.
// The caller must prove the PVC, exact generation path and owner BIND before creating any files.
// NewWorkspaceMountProof принимает trusted inode observation после проверки owner/input.
// Caller доказывает PVC, exact generation path и owner BIND до создания любых файлов.
func NewWorkspaceMountProof(input RunnerInput, device, inode uint64) (WorkspaceMountProof, error) {
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil ||
		!sha256Pattern.MatchString(input.ExecutionBindingDigest) || device == 0 || inode == 0 {
		return WorkspaceMountProof{}, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(input.WorkspaceStorage)
	if err != nil {
		return WorkspaceMountProof{}, errWorkspaceStorageInvalid
	}
	return WorkspaceMountProof{Schema: WorkspaceMountProofSchema, BindingSHA256: sha256Sum(raw),
		ExecutionBindingDigest: input.ExecutionBindingDigest, Device: device, Inode: inode}, nil
}

// Matches checks current input and inode equality; read-only provenance is checked by the file opener.
// Matches проверяет current input и inode equality; read-only provenance проверяется file opener.
func (value WorkspaceMountProof) Matches(input RunnerInput, device, inode uint64) bool {
	expected, err := NewWorkspaceMountProof(input, device, inode)
	return err == nil && value == expected
}

// EncodeWorkspaceMountProof bounds the immutable local record and rejects missing commitments.
// EncodeWorkspaceMountProof ограничивает immutable local record и отвергает отсутствие commitments.
func EncodeWorkspaceMountProof(value WorkspaceMountProof) ([]byte, error) {
	if value.Schema != WorkspaceMountProofSchema || !sha256Pattern.MatchString(value.BindingSHA256) ||
		!sha256Pattern.MatchString(value.ExecutionBindingDigest) || value.Device == 0 || value.Inode == 0 {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceMountProofBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceMountProof rejects unknown/duplicate keys and noncanonical or oversized metadata.
// DecodeWorkspaceMountProof отвергает unknown/duplicate keys и noncanonical/oversized metadata.
func DecodeWorkspaceMountProof(raw []byte) (WorkspaceMountProof, error) {
	var value WorkspaceMountProof
	if len(raw) < 1 || len(raw) > MaximumWorkspaceMountProofBytes ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 2) != nil {
		return value, errWorkspaceStorageInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return WorkspaceMountProof{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceMountProof(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return WorkspaceMountProof{}, errWorkspaceStorageInvalid
	}
	return value, nil
}
