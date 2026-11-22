package kubernetestarget

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

const (
	// resourceSchemaInvalid closes unsupported schemas without returning the provider's document or URLs.
	// resourceSchemaInvalid закрывает unsupported schemas без выдачи provider document либо URLs.
	resourceSchemaInvalid = "managed Kubernetes registered schema is invalid"
	// resourceSchemaURL is a local compiler resource; no network loader is registered for it.
	// resourceSchemaURL — local compiler resource; network loader для него не регистрируется.
	resourceSchemaURL = "urn:kodex:managed-kubernetes:registered-resource"
	// resourceSchemaReference accepts only components inside the already downloaded selected document.
	// resourceSchemaReference допускает только components ранее загруженного selected document.
	resourceSchemaReference = "#/components/schemas/"
)

// resourceComponentName follows OpenAPI's finite component-key syntax, not an arbitrary URI.
// resourceComponentName следует finite component-key syntax OpenAPI, не arbitrary URI.
var resourceComponentName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,512}$`)

// registeredResourceSchema keeps the actual immutable schema pin and native Kubernetes patch
// models beside the compiled validation schema. It contains no credential or reusable grant.
// registeredResourceSchema хранит actual immutable schema pin и native Kubernetes patch models
// рядом с compiled validation schema. Credential либо reusable grant здесь отсутствуют.
type registeredResourceSchema struct {
	digest          string
	immutableHash   string
	name            string
	kind            schema.GroupVersionKind
	compiled        *jsonschema.Schema
	models          map[string]*spec.Schema
	unversionedRead bool
}

// compileRegisteredResourceSchema verifies the immutable JSON hash and exact registered GVK.
// Only local component references are accepted. Native OpenAPI models remain unchanged for
// Kubernetes strategic/structured merge; a separate validation view adds strict unknown-field
// checks and preserves explicitly declared map/preserve-unknown/nullable semantics.
// compileRegisteredResourceSchema проверяет immutable JSON hash и exact registered GVK.
// Допустимы только local component references. Native OpenAPI models не меняются для Kubernetes
// strategic/structured merge; отдельный validation view добавляет strict unknown-field checks
// и сохраняет явно объявленные map/preserve-unknown/nullable semantics.
func compileRegisteredResourceSchema(raw []byte, expected schema.GroupVersionKind, immutableHash string) (*registeredResourceSchema, error) {
	if expected.Kind == "" || expected.Version == "" || !resourceSchemaHashMatches(raw, immutableHash) {
		return nil, errors.New(resourceSchemaInvalid)
	}
	document, err := decodeResourceJSON(raw, resourceSchemaBytes, 128)
	if err != nil || document["openapi"] != "3.0.0" {
		return nil, errors.New(resourceSchemaInvalid)
	}
	components, ok := document["components"].(map[string]any)
	if !ok {
		return nil, errors.New(resourceSchemaInvalid)
	}
	models, ok := components["schemas"].(map[string]any)
	if !ok || len(models) == 0 || len(models) > 4096 {
		return nil, errors.New(resourceSchemaInvalid)
	}
	native := make(map[string]*spec.Schema, len(models))
	validation := make(map[string]any, len(models))
	selected := ""
	for name, value := range models {
		model, valid := value.(map[string]any)
		if !valid || !resourceComponentName.MatchString(name) {
			return nil, errors.New(resourceSchemaInvalid)
		}
		if resourceSchemaHasKind(model, expected) {
			if selected != "" {
				return nil, errors.New(resourceSchemaInvalid)
			}
			selected = name
		}
		encoded, encodeErr := json.Marshal(model)
		var typed spec.Schema
		if encodeErr != nil || json.Unmarshal(encoded, &typed) != nil {
			return nil, errors.New(resourceSchemaInvalid)
		}
		native[name] = &typed
		converted, convertErr := normalizeResourceValidationSchema(model, 0)
		if convertErr != nil {
			return nil, convertErr
		}
		validation[name] = converted
	}
	if selected == "" {
		return nil, errors.New(resourceSchemaInvalid)
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	compiler.DefaultDraft(jsonschema.Draft4)
	view := map[string]any{"components": map[string]any{"schemas": validation}}
	if compiler.AddResource(resourceSchemaURL, view) != nil {
		return nil, errors.New(resourceSchemaInvalid)
	}
	compiled, err := compiler.Compile(resourceSchemaURL + resourceSchemaReference + selected)
	if err != nil {
		return nil, errors.New(resourceSchemaInvalid)
	}
	digest := sha256.Sum256(raw)
	return &registeredResourceSchema{digest: hex.EncodeToString(digest[:]), immutableHash: immutableHash,
		name: selected, kind: expected, compiled: compiled, models: native}, nil
}

// resourceSchemaHashMatches checks the exact downloaded bytes against the discovery hash.
// Kubernetes handler3 uses SHA-512; the explicitly supported SHA-256 alternative stays exact.
// resourceSchemaHashMatches сверяет exact downloaded bytes с discovery hash.
// Kubernetes handler3 использует SHA-512; явно поддержанный SHA-256 alternative тоже exact.
func resourceSchemaHashMatches(raw []byte, expected string) bool {
	switch len(expected) {
	case sha512.Size * 2:
		digest := sha512.Sum512(raw)
		return strings.EqualFold(expected, hex.EncodeToString(digest[:]))
	case sha256.Size * 2:
		digest := sha256.Sum256(raw)
		return strings.EqualFold(expected, hex.EncodeToString(digest[:]))
	}
	return false
}

// resourceSchemaHasKind binds schema selection to discovery's group/version/kind extension.
// Missing or ambiguous registrations do not fall back to a similarly named component.
// resourceSchemaHasKind связывает schema selection с group/version/kind extension discovery.
// Missing/ambiguous registrations не дают fallback на одноимённый component.
func resourceSchemaHasKind(model map[string]any, expected schema.GroupVersionKind) bool {
	registrations, ok := model["x-kubernetes-group-version-kind"].([]any)
	if !ok {
		return false
	}
	for _, raw := range registrations {
		registration, ok := raw.(map[string]any)
		if ok && registration["group"] == expected.Group && registration["version"] == expected.Version && registration["kind"] == expected.Kind {
			return true
		}
	}
	return false
}

// normalizeResourceValidationSchema translates OpenAPI 3.0 nullable into Draft4 JSON Schema
// and closes undeclared object fields without disabling declared maps or preserved subtrees.
// It traverses only schema keywords, never arbitrary default/example/business data. IDs and
// nonlocal refs fail before compiler use; no remote schema or meta-schema can be loaded.
// normalizeResourceValidationSchema переводит OpenAPI 3.0 nullable в Draft4 JSON Schema
// и закрывает undeclared object fields, сохраняя declared maps/preserved subtrees.
// Обходятся только schema keywords, не arbitrary default/example/business data. IDs/nonlocal refs
// отклоняются до compiler; remote schema либо meta-schema не загружается.
func normalizeResourceValidationSchema(input map[string]any, depth int) (map[string]any, error) {
	if depth > 128 {
		return nil, errors.New(resourceSchemaInvalid)
	}
	result := make(map[string]any, len(input)+1)
	for key, value := range input {
		switch key {
		case "$schema", "$id", "id":
			return nil, errors.New(resourceSchemaInvalid)
		case "$ref":
			ref, ok := value.(string)
			if !ok || !strings.HasPrefix(ref, resourceSchemaReference) || !resourceComponentName.MatchString(strings.TrimPrefix(ref, resourceSchemaReference)) {
				return nil, errors.New(resourceSchemaInvalid)
			}
			result[key] = value
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New(resourceSchemaInvalid)
			}
			converted := make(map[string]any, len(properties))
			for name, raw := range properties {
				child, ok := raw.(map[string]any)
				if !ok {
					return nil, errors.New(resourceSchemaInvalid)
				}
				next, err := normalizeResourceValidationSchema(child, depth+1)
				if err != nil {
					return nil, err
				}
				converted[name] = next
			}
			result[key] = converted
		case "items", "additionalProperties", "not":
			if child, ok := value.(map[string]any); ok {
				next, err := normalizeResourceValidationSchema(child, depth+1)
				if err != nil {
					return nil, err
				}
				result[key] = next
			} else if flag, ok := value.(bool); ok && key == "additionalProperties" {
				result[key] = flag
			} else {
				return nil, errors.New(resourceSchemaInvalid)
			}
		case "allOf", "anyOf", "oneOf":
			children, ok := value.([]any)
			if !ok {
				return nil, errors.New(resourceSchemaInvalid)
			}
			converted := make([]any, 0, len(children))
			for _, raw := range children {
				child, ok := raw.(map[string]any)
				if !ok {
					return nil, errors.New(resourceSchemaInvalid)
				}
				next, err := normalizeResourceValidationSchema(child, depth+1)
				if err != nil {
					return nil, err
				}
				converted = append(converted, next)
			}
			result[key] = converted
		case "nullable":
			if _, ok := value.(bool); !ok {
				return nil, errors.New(resourceSchemaInvalid)
			}
		default:
			result[key] = value
		}
	}
	if _, object := result["properties"]; object && input["x-kubernetes-preserve-unknown-fields"] != true {
		if _, declared := result["additionalProperties"]; !declared {
			result["additionalProperties"] = false
		}
	}
	if input["nullable"] == true {
		return map[string]any{"anyOf": []any{result, map[string]any{"type": "null"}}}, nil
	}
	return result, nil
}
