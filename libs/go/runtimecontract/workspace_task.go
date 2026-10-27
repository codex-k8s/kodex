package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
)

// DecodeWorkspaceStorageTaskJSON reuses the workspace duplicate/depth boundary for private archive-task data.
// It requires one bounded object and a closed typed destination, without importing authentication/JWT code.
// Callers separately validate their schema, canonical bytes, owner task and current lease authority.
// DecodeWorkspaceStorageTaskJSON повторно использует workspace duplicate/depth boundary для private archive task.
// Требуется bounded object и closed typed destination, без зависимости от authentication/JWT code.
// Caller отдельно проверяет schema, canonical bytes, owner task и current lease authority.
func DecodeWorkspaceStorageTaskJSON(raw []byte, destination any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(raw) < 1 || len(raw) > MaximumWorkspaceManifestBytes || len(trimmed) == 0 || trimmed[0] != '{' ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 8) != nil {
		return errWorkspaceStorageInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if decoder.Decode(destination) != nil || decoder.Decode(new(any)) != io.EOF {
		return errWorkspaceStorageInvalid
	}
	return nil
}
