package integrationpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// maxOpenAPIInputSchemaBytes caps executable input schemas before compilation and runtime use.
// maxOpenAPIInputSchemaBytes ограничивает размер исполняемой схемы до компиляции и применения.
const maxOpenAPIInputSchemaBytes = 64 << 10

// validateOpenAPIInputSchema compiles a bounded closed-object schema without external references or dialect changes.
// validateOpenAPIInputSchema компилирует ограниченную схему закрытого объекта без внешних ссылок и смены диалекта.
func validateOpenAPIInputSchema(candidate map[string]any) ([]byte, *jsonschema.Schema, error) {
	if candidate["type"] != "object" || candidate["additionalProperties"] != false {
		return nil, nil, errors.New("OpenAPI input schema root must be a closed object")
	}
	encoded, err := json.Marshal(candidate)
	if err != nil || len(encoded) == 0 || len(encoded) > maxOpenAPIInputSchemaBytes {
		return nil, nil, errors.New("OpenAPI input schema size is invalid")
	}
	// item pairs one schema node with traversal depth so complexity limits apply before compilation.
	// item связывает узел схемы с глубиной обхода для проверки сложности до компиляции.
	type item struct {
		value any
		depth int
	}
	stack := []item{{value: candidate, depth: 0}}
	nodes := 0
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		nodes++
		if nodes > maxOpenAPINodes || current.depth > maxOpenAPIDepth {
			return nil, nil, errors.New("OpenAPI input schema complexity limit exceeded")
		}
		switch value := current.value.(type) {
		case map[string]any:
			// A nested JSON object must also be closed; otherwise one selected operation accepts arbitrary body fields.
			// Вложенный JSON-объект тоже закрыт, иначе выбранная операция примет произвольные поля тела.
			if value["type"] == "object" && value["additionalProperties"] != false {
				return nil, nil, errors.New("OpenAPI input object schema must be closed")
			}
			for key, child := range value {
				switch key {
				case "$ref":
					ref, ok := child.(string)
					if !ok || !strings.HasPrefix(ref, "#/$defs/") || strings.ContainsAny(ref[8:], "%?#\x00\r\n") {
						return nil, nil, errors.New("OpenAPI input schema reference is invalid")
					}
				case "$schema":
					if child != "https://json-schema.org/draft/2020-12/schema" {
						return nil, nil, errors.New("OpenAPI input schema dialect is invalid")
					}
				case "$defs":
				case "patternProperties", "unevaluatedProperties":
					return nil, nil, errors.New("OpenAPI input dynamic object fields are unsupported")
				default:
					if strings.HasPrefix(key, "$") {
						return nil, nil, errors.New("OpenAPI input schema keyword is unsupported")
					}
				}
				stack = append(stack, item{value: child, depth: current.depth + 1})
			}
		case []any:
			for _, child := range value {
				stack = append(stack, item{value: child, depth: current.depth + 1})
			}
		}
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("https://schema.kodex.invalid/openapi-input", candidate); err != nil {
		return nil, nil, errors.New("OpenAPI input schema registration failed")
	}
	compiled, err := compiler.Compile("https://schema.kodex.invalid/openapi-input")
	if err != nil {
		return nil, nil, errors.New("OpenAPI input schema compilation failed")
	}
	return encoded, compiled, nil
}

// validateOpenAPIInput accepts one bounded object against the compiled schema and returns canonical JSON bytes.
// validateOpenAPIInput принимает один ограниченный объект по скомпилированной схеме и возвращает канонические байты JSON.
func validateOpenAPIInput(raw []byte, compiled *jsonschema.Schema) ([]byte, error) {
	if len(raw) == 0 || len(raw) > maxObjectBytes || compiled == nil {
		return nil, errors.New("OpenAPI input is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(&struct{}{}) != io.EOF || compiled.Validate(value) != nil {
		return nil, errors.New("OpenAPI input is invalid")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("OpenAPI input is invalid")
	}
	return canonical, nil
}
