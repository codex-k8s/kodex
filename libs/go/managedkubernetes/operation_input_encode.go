package managedkubernetes

import (
	"encoding/json"
	"slices"
	"unicode/utf8"
)

// EncodeOperationInput revalidates typed input through the single registered parser and returns
// its canonical bytes. Only schema-required empty group/namespace keys are restored after
// omitempty; no target, identity, object witness or other missing required field is synthesized.
// UTF-8 is checked before encoding/json can replace malformed strings with replacement runes.
// EncodeOperationInput повторно проверяет typed input единственным registered parser и выдаёт
// canonical bytes. После omitempty восстанавливаются лишь schema-required empty group/namespace;
// target, identity, object witness либо иные missing required fields не синтезируются.
// UTF-8 проверяется до подмены malformed strings replacement runes библиотекой encoding/json.
func EncodeOperationInput(operation string, input OperationInput) ([]byte, error) {
	spec, known := operationInputRegistry[operation]
	if !known || !utf8.Valid(input.Body) || !utf8.Valid(input.Options) {
		return nil, ErrInvalidOperationInput
	}
	for _, field := range []string{input.Group, input.Version, input.Resource, input.Namespace, input.Name,
		input.UID, input.ResourceVersion, input.Subresource, input.Selector} {
		if !utf8.ValidString(field) {
			return nil, ErrInvalidOperationInput
		}
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > MaximumOperationInputBytes {
		clear(raw)
		return nil, ErrInvalidOperationInput
	}
	defer clear(raw)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, ErrInvalidOperationInput
	}
	for _, key := range []string{"group", "namespace"} {
		if slices.Contains(spec.required, key) {
			if _, present := fields[key]; !present {
				fields[key] = json.RawMessage(`""`)
			}
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, ErrInvalidOperationInput
	}
	defer clear(encoded)
	_, canonical, err := DecodeOperationInput(operation, encoded)
	return canonical, err
}
