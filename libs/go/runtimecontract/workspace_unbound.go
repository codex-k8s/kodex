package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
)

// WorkspaceObserveUnboundStop retires an intent only after an exact never-bound workload stopped.
// WorkspaceObserveUnboundStop закрывает intent лишь после остановки exact never-bound workload.
const WorkspaceObserveUnboundStop = "STOPPED_BEFORE_BIND"

// WorkspaceUnboundStopSchema distinguishes never-admitted intent retirement from positive writer join.
// WorkspaceUnboundStopSchema отделяет закрытие недопущенного intent от positive writer join.
const WorkspaceUnboundStopSchema = "kodex.runtime-workspace-unbound-stop.v1"

// workspaceUnboundNodeName accepts bounded Kubernetes node names in immutable evidence.
// workspaceUnboundNodeName допускает bounded Kubernetes node names в immutable evidence.
var workspaceUnboundNodeName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// WorkspaceUnboundStop binds the controller's stopped initial-workload witness to immutable owner input.
// It is not a writer ACK, native checkpoint, effect receipt, retry grant or evidence of an absent Pod.
// WorkspaceUnboundStop связывает stopped initial-workload witness контроллера с immutable owner input.
// Это не writer ACK, native checkpoint, effect receipt, retry grant или доказательство отсутствия Pod.
type WorkspaceUnboundStop struct {
	Schema       string                    `json:"schema"`
	Scope        WorkspaceStorageScope     `json:"scope"`
	WorkspaceRef string                    `json:"workspace_ref"`
	Generation   int64                     `json:"generation"`
	NodeName     string                    `json:"node_name"`
	NodeUID      string                    `json:"node_uid"`
	Evidence     string                    `json:"evidence"`
	Execution    WorkspaceWriterCommitment `json:"execution"`
}

// Validate permits only fixed never-started boundaries without a fabricated join hash.
// Validate допускает лишь фиксированные never-started boundaries без выдуманного join hash.
func (value WorkspaceUnboundStop) Validate() error {
	if value.Schema != WorkspaceUnboundStopSchema || value.Scope.Validate() != nil || !workspaceStorageRefPattern.MatchString(value.WorkspaceRef) || value.Generation < 1 ||
		!workspaceUID(value.NodeUID) || len(value.NodeName) > 253 || !workspaceUnboundNodeName.MatchString(value.NodeName) ||
		(value.Evidence != "ALL_UNSTARTED" && value.Evidence != "FIRST_INIT_FAILED") || validateWorkspaceWriter(value.Execution, false) != nil {
		return errWorkspaceStorageInvalid
	}
	return nil
}

// Matches binds every execution coordinate and the prepared physical PVC, without granting authority.
// Matches связывает все execution coordinates и prepared physical PVC без выдачи authority.
func (value WorkspaceUnboundStop) Matches(input RunnerInput) bool {
	return value.Validate() == nil && input.WorkspaceStorage != nil && input.WorkspaceStorage.ValidateForInput(input) == nil &&
		value.Scope == input.WorkspaceStorage.Scope && value.WorkspaceRef == input.WorkspaceStorage.WorkspaceRef && value.Generation == input.WorkspaceStorage.Generation &&
		workspaceWriterTupleMatches(value.Execution, input) && (input.WorkspaceStorage.VolumeReady == nil || value.Execution.PVCUID == input.WorkspaceStorage.VolumeReady.Preparation.PVCUID)
}

// EncodeWorkspaceUnboundStop creates bounded canonical metadata without fences or filesystem content.
// EncodeWorkspaceUnboundStop создаёт bounded canonical metadata без fences и filesystem content.
func EncodeWorkspaceUnboundStop(value WorkspaceUnboundStop) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errWorkspaceStorageInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumWorkspaceQuiescenceBytes {
		return nil, errWorkspaceStorageInvalid
	}
	return raw, nil
}

// DecodeWorkspaceUnboundStop rejects duplicate, unknown, oversized or noncanonical witness bytes.
// DecodeWorkspaceUnboundStop отклоняет duplicate, unknown, oversized и noncanonical witness bytes.
func DecodeWorkspaceUnboundStop(raw []byte) (WorkspaceUnboundStop, error) {
	var value WorkspaceUnboundStop
	if len(raw) < 1 || len(raw) > MaximumWorkspaceQuiescenceBytes || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 3) != nil {
		return value, errWorkspaceStorageInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || value.Validate() != nil {
		return WorkspaceUnboundStop{}, errWorkspaceStorageInvalid
	}
	canonical, err := EncodeWorkspaceUnboundStop(value)
	if err != nil || !bytes.Equal(canonical, raw) {
		return WorkspaceUnboundStop{}, errWorkspaceStorageInvalid
	}
	return value, nil
}
