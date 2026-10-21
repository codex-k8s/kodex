package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// UnmarshalJSON bounds the callback envelope and rejects duplicate, unknown or differently cased keys before decoding nested projections and assigning the request.
// UnmarshalJSON ограничивает callback-конверт и отклоняет повторные, неизвестные или иначе регистрозависимо записанные ключи до разбора вложенных проекций и присвоения запроса.
func (value *RunnerProgressRequest) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("runtime progress request is invalid")
	if len(raw) > MaximumRuntimeMessageBytes*6+2048 || !utf8.Valid(raw) || financialJSONKeys(raw) != nil {
		return invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil ||
		!financialKnownFields(fields, "financial", "runtime_revision_digest", "progress", "message", "provider_process") {
		return invalid
	}
	// plainProgress retains the request layout without recursively calling this envelope decoder.
	// plainProgress сохраняет структуру запроса без рекурсивного вызова этого декодера конверта.
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
