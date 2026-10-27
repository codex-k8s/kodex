package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
)

const (
	// WorkspaceObserveBind requires a fresh current writer CAS before application filesystem writes.
	// WorkspaceObserveBind требует fresh current writer CAS до любых application filesystem writes.
	WorkspaceObserveBind = "BIND"
	// WorkspaceObserveQuiescent records a past joined writer and never reopens its business authority.
	// WorkspaceObserveQuiescent сохраняет past joined writer, не открывая business authority вновь.
	WorkspaceObserveQuiescent = "QUIESCENT"
	// WorkspaceWriterAcknowledgementSchema closes the owner ACK metadata stored outside model work.
	// WorkspaceWriterAcknowledgementSchema закрывает owner ACK metadata вне model work.
	WorkspaceWriterAcknowledgementSchema = "kodex.runtime-workspace-writer.v1"
	// WorkspaceWriterAcknowledgementPath names the exact role-only read-only file projection.
	// WorkspaceWriterAcknowledgementPath задаёт exact role-only read-only file projection.
	WorkspaceWriterAcknowledgementPath = "/var/run/config/kodex/workspace/writer.json"
	// MaximumWorkspaceObservationBytes bounds the complete callback independently of generic completion.
	// MaximumWorkspaceObservationBytes ограничивает callback отдельно от generic completion.
	MaximumWorkspaceObservationBytes = 4096
)

// RunnerWorkspaceObservation is a closed callback request, never caller-selected storage authority.
// BIND requires owned Pod/PVC and fresh owner CAS; QUIESCENT additionally requires positive join proof.
// RunnerWorkspaceObservation — закрытый callback request, не caller-selected storage authority.
// BIND требует owned Pod/PVC и fresh owner CAS; QUIESCENT также требует positive join proof.
type RunnerWorkspaceObservation struct {
	RuntimeRevisionDigest string               `json:"runtime_revision_digest"`
	Phase                 string               `json:"phase"`
	WorkspaceRef          string               `json:"workspace_ref"`
	Generation            int64                `json:"generation"`
	Quiescence            *WorkspaceQuiescence `json:"quiescence,omitempty"`
}

// ValidateForInput rejects stale or unknown observation phases without creating a writer.
// Controller-observed UID/container ownership is a separate mandatory verification boundary.
// ValidateForInput отвергает stale или unknown phase, не создавая writer.
// Controller-observed UID/container ownership проверяются на отдельной обязательной границе.
func (value RunnerWorkspaceObservation) ValidateForInput(input RunnerInput) error {
	if input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil ||
		value.RuntimeRevisionDigest != input.RuntimeRevisionDigest || !sha256Pattern.MatchString(value.RuntimeRevisionDigest) ||
		value.WorkspaceRef != input.WorkspaceStorage.WorkspaceRef || value.Generation != input.WorkspaceStorage.Generation {
		return errWorkspaceStorageInvalid
	}
	switch value.Phase {
	case WorkspaceObserveBind:
		if value.Quiescence != nil {
			return errWorkspaceStorageInvalid
		}
	case WorkspaceObserveQuiescent:
		if value.Quiescence == nil || value.Quiescence.Validate() != nil ||
			!value.Quiescence.Matches(input, *input.WorkspaceStorage, value.Quiescence.Writer.PodUID, value.Quiescence.Writer.PVCUID) {
			return errWorkspaceStorageInvalid
		}
	default:
		return errWorkspaceStorageInvalid
	}
	return nil
}

// WorkspaceWriterAcknowledgement contains safe immutable writer pins returned only after owner CAS.
// Local shape/matching is insufficient authority without the authenticated owner response and readback.
// WorkspaceWriterAcknowledgement содержит safe immutable writer pins после owner CAS.
// Local shape/matching не заменяют authenticated owner response и readback.
type WorkspaceWriterAcknowledgement struct {
	Schema       string                    `json:"schema"`
	Scope        WorkspaceStorageScope     `json:"scope"`
	WorkspaceRef string                    `json:"workspace_ref"`
	Generation   int64                     `json:"generation"`
	Writer       WorkspaceWriterCommitment `json:"writer"`
}

// Matches checks the complete expected materialization and refuses recursive/fabricated join hashes.
// Matches проверяет полную ожидаемую materialization и запрещает recursive/fabricated join hashes.
func (value WorkspaceWriterAcknowledgement) Matches(input RunnerInput) bool {
	return input.WorkspaceStorage != nil && input.WorkspaceStorage.ValidateForInput(input) == nil &&
		value.Schema == WorkspaceWriterAcknowledgementSchema && value.Scope == input.WorkspaceStorage.Scope &&
		value.WorkspaceRef == input.WorkspaceStorage.WorkspaceRef && value.Generation == input.WorkspaceStorage.Generation &&
		validateWorkspaceWriter(value.Writer, false) == nil && value.Writer.Matches(input)
}

// EncodeWorkspaceWriterAcknowledgement publishes bounded canonical metadata without secret fences.
// EncodeWorkspaceWriterAcknowledgement формирует bounded canonical metadata без secret fences.
func EncodeWorkspaceWriterAcknowledgement(value WorkspaceWriterAcknowledgement) ([]byte, error) {
	if value.Schema != WorkspaceWriterAcknowledgementSchema || value.Scope.Validate() != nil ||
		!workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || value.Generation < 1 || validateWorkspaceWriter(value.Writer, false) != nil {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceQuiescenceBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceWriterAcknowledgement accepts one closed canonical ACK, never owner-authenticates it.
// DecodeWorkspaceWriterAcknowledgement принимает один closed canonical ACK, не аутентифицируя owner.
func DecodeWorkspaceWriterAcknowledgement(raw []byte) (WorkspaceWriterAcknowledgement, error) {
	var value WorkspaceWriterAcknowledgement
	if len(raw) < 1 || len(raw) > MaximumWorkspaceQuiescenceBytes || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 3) != nil {
		return value, errWorkspaceStorageInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return WorkspaceWriterAcknowledgement{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceWriterAcknowledgement(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return WorkspaceWriterAcknowledgement{}, errWorkspaceStorageInvalid
	}
	return value, nil
}
