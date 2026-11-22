package kubernetestarget

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	strictjson "sigs.k8s.io/json"
)

const (
	// resourceResponseBytes bounds private object/list responses before projection, not returned tool output.
	// resourceResponseBytes ограничивает private object/list responses до projection, не выдаваемый tool output.
	resourceResponseBytes = 2 << 20
	// resourceSchemaBytes permits the selected group-version's OpenAPI document without truncation.
	// resourceSchemaBytes допускает OpenAPI document выбранной group-version без усечения.
	resourceSchemaBytes = 8 << 20
	// resourceJSONInvalid never includes upstream content, field names or private parser diagnostics.
	// resourceJSONInvalid никогда не содержит upstream content, field names либо private parser diagnostics.
	resourceJSONInvalid = "managed Kubernetes resource JSON is invalid"
)

// decodeResourceJSON bounds UTF-8, nesting and duplicate-free provider objects before semantic
// use. Kubernetes' strict decoder owns JSON parsing; the token pass only limits nesting so
// deeply nested managedFields/OpenAPI documents cannot exhaust the later schema validator.
// Unknown object properties are checked against the selected registered schema, not a Go map.
// decodeResourceJSON ограничивает UTF-8, nesting и duplicate-free provider objects до semantic
// use. JSON разбирает strict decoder Kubernetes; token pass только ограничивает nesting, чтобы
// глубокие managedFields/OpenAPI не исчерпали поздний schema validator. Unknown properties
// проверяются по выбранной registered schema, не по Go map.
func decodeResourceJSON(raw []byte, maximumBytes, maximumDepth int) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > maximumBytes || !utf8.Valid(raw) {
		return nil, errors.New(resourceJSONInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New(resourceJSONInvalid)
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
				if depth > maximumDepth {
					return nil, errors.New(resourceJSONInvalid)
				}
			case '}', ']':
				depth--
			}
		}
	}
	var object map[string]any
	strictErrors, err := strictjson.UnmarshalStrict(raw, &object)
	if err != nil || len(strictErrors) != 0 || object == nil || depth != 0 {
		return nil, errors.New(resourceJSONInvalid)
	}
	return object, nil
}
