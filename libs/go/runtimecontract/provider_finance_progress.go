package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// Callback envelope не допускает второй payload или альтернативный регистр
// ключей, который стандартный decoder мог бы неявно сопоставить полю Go.
func (value *RunnerProgressRequest) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("runtime progress request is invalid")
	if len(raw) > MaximumRuntimeMessageBytes*6+2048 || !utf8.Valid(raw) || financialJSONKeys(raw) != nil {
		return invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil ||
		!financialKnownFields(fields, "financial", "runtime_revision_digest", "progress", "message") {
		return invalid
	}
	type plainProgress RunnerProgressRequest
	var decoded plainProgress
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil {
		return invalid
	}
	*value = RunnerProgressRequest(decoded)
	return nil
}
