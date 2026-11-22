package managedkubernetes

import (
	"bytes"
	"encoding/json"
	"sync"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// MaximumProtectedSecretPatchBytes bounds a broker-only protected patch document, not public input.
// MaximumProtectedSecretPatchBytes ограничивает broker-only protected patch document, не public input.
const MaximumProtectedSecretPatchBytes = 2 << 20

// protectedSecretPatchSchema compiles the same owned patch-body schema used by public operations.
// This cache stores syntax only; it cannot admit an upload, select a target or bypass owner scope.
// protectedSecretPatchSchema компилирует ту же owned patch-body schema, что public operations.
// Cache хранит только syntax; не допускает upload, не выбирает target и не обходит owner scope.
var protectedSecretPatchSchema = sync.OnceValue(func() compiledOperationInput {
	document := operationBodySchema("patch")
	raw, err := json.Marshal(document)
	var schemaDocument any
	if err == nil {
		err = json.Unmarshal(raw, &schemaDocument)
	}
	var compiled *jsonschema.Schema
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err == nil {
		err = compiler.AddResource("urn:kodex:kubernetes:protected-secret-patch", schemaDocument)
	}
	if err == nil {
		compiled, err = compiler.Compile("urn:kodex:kubernetes:protected-secret-patch")
	}
	return compiledOperationInput{schema: compiled, raw: raw, err: err}
})

// ValidateProtectedSecretPatchBody checks only broker-owned private patch syntax with the common
// schema/RFC6901 validator. Data paths are intentionally permitted here; public DecodeOperationInput
// still forbids them. The caller separately proves upload ownership, immutable ref/hash/target,
// readonly identity, schema and object policy before injecting UID/RV and executing one effect.
// This validation-only helper neither clears caller bytes nor returns their content in errors.
// ValidateProtectedSecretPatchBody проверяет только broker-owned private patch syntax общей
// schema/RFC6901 validation. Data paths здесь намеренно допустимы; public DecodeOperationInput
// по-прежнему запрещает их. Caller отдельно доказывает upload ownership, immutable ref/hash/target,
// readonly identity, schema/object policy до инъекции UID/RV и одного effect. Helper только
// проверяет syntax: не очищает caller bytes и не возвращает их содержимое в errors.
func ValidateProtectedSecretPatchBody(raw []byte, patchType string) error {
	if len(raw) == 0 || len(raw) > MaximumProtectedSecretPatchBytes || !utf8.Valid(raw) ||
		patchType != "JSON" && patchType != "MERGE" && patchType != "STRATEGIC" {
		return ErrInvalidOperationInput
	}
	compiled := protectedSecretPatchSchema()
	var document any
	if compiled.err != nil || internalrpcauth.DecodeStrictJSON(raw, &document) != nil || compiled.schema.Validate(document) != nil {
		return ErrInvalidOperationInput
	}
	_, array := document.([]any)
	if (patchType == "JSON") != array || array && !validJSONPatch(bytes.TrimSpace(raw)) {
		return ErrInvalidOperationInput
	}
	return nil
}
