package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
)

const (
	// WorkspaceQuiescenceSchema versions a private positive join observation, never a run result.
	// WorkspaceQuiescenceSchema версионирует private positive join observation, не run result.
	WorkspaceQuiescenceSchema = "kodex.runtime-workspace-quiescence.v1"
	// MaximumWorkspaceQuiescenceBytes fits the unchanged Kubernetes termination message boundary.
	// MaximumWorkspaceQuiescenceBytes помещается в прежнюю Kubernetes termination message boundary.
	MaximumWorkspaceQuiescenceBytes = 2048
)

// WorkspaceQuiescence records only an observed joined writer and exact protected generation.
// The controller must additionally prove image, Pod/container ownership and terminal status.
// WorkspaceQuiescence записывает только observed joined writer и exact protected generation.
// Controller дополнительно доказывает image, Pod/container ownership и terminal status.
type WorkspaceQuiescence struct {
	Schema       string                    `json:"schema"`
	Scope        WorkspaceStorageScope     `json:"scope"`
	WorkspaceRef string                    `json:"workspace_ref"`
	Generation   int64                     `json:"generation"`
	Joined       bool                      `json:"joined"`
	Writer       WorkspaceWriterCommitment `json:"writer"`
}

// Validate rejects missing join and recursive hashes; it is not a filesystem or process witness.
// Validate отвергает отсутствие join и recursive hashes; это не filesystem/process witness.
func (value WorkspaceQuiescence) Validate() error {
	if value.Schema != WorkspaceQuiescenceSchema || value.Scope.Validate() != nil || !value.Joined ||
		!workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || value.Generation < 1 ||
		validateWorkspaceWriter(value.Writer, false) != nil {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// EncodeWorkspaceQuiescence limits the complete positive record before termination-log publication.
// EncodeWorkspaceQuiescence ограничивает полную positive record до termination-log publication.
func EncodeWorkspaceQuiescence(value WorkspaceQuiescence) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceQuiescenceBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceQuiescence requires one canonical bounded record and never upgrades malformed input.
// DecodeWorkspaceQuiescence требует одну canonical bounded record и не повышает malformed input.
func DecodeWorkspaceQuiescence(raw []byte) (WorkspaceQuiescence, error) {
	var value WorkspaceQuiescence
	if len(raw) < 1 || len(raw) > MaximumWorkspaceQuiescenceBytes ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 3) != nil {
		return value, errWorkspaceStorageInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Validate() != nil {
		return WorkspaceQuiescence{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceQuiescence(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return WorkspaceQuiescence{}, errWorkspaceStorageInvalid
	}
	return value, nil
}

// Commitment binds the exact record bytes without persisting a secret fence or self-referential digest.
// Commitment связывает exact record bytes без secret fence или self-referential digest.
func (value WorkspaceQuiescence) Commitment() (WorkspaceWriterCommitment, error) {
	raw, err := EncodeWorkspaceQuiescence(value)
	if err != nil {
		return WorkspaceWriterCommitment{}, err
	}
	writer := value.Writer
	writer.JoinSHA256 = sha256Sum(raw)
	return writer, nil
}

// Matches requires both the current immutable input tuple and owner-selected work generation.
// A caller-provided record cannot substitute for the controller's owned terminal-container readback.
// Matches требует current immutable input tuple и выбранную owner work generation.
// Caller record не заменяет owned terminal-container readback controller.
func (value WorkspaceQuiescence) Matches(input RunnerInput, binding WorkspaceStorageBinding, podUID, pvcUID string) bool {
	writer, err := value.Commitment()
	return err == nil && binding.ValidateForInput(input) == nil && value.Scope == binding.Scope &&
		value.WorkspaceRef == binding.WorkspaceRef && value.Generation == binding.Generation && writer.Matches(input) &&
		workspaceUID(podUID) && workspaceUID(pvcUID) && writer.PodUID == podUID && writer.PVCUID == pvcUID
}
