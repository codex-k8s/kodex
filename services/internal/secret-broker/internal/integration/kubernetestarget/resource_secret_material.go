package kubernetestarget

import (
	"encoding/base64"
	"errors"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"
)

// maximumSecretContentBytes follows the Kubernetes Secret decoded-content limit, independent of
// transport/base64 overhead and the smaller returned metadata-only projection bound.
// maximumSecretContentBytes следует Kubernetes Secret decoded-content limit, независимо от
// transport/base64 overhead и меньшего returned metadata-only projection bound.
const maximumSecretContentBytes = 1 << 20

// injectResourceSecretContent accepts only owner-resolved private data/stringData maps.
// Metadata, type, routing and CAS stay in the immutable public proposal; this function cannot
// retarget the request or silently combine two independently supplied content documents.
// injectResourceSecretContent принимает только owner-resolved private data/stringData maps.
// Metadata/type/routing/CAS остаются immutable public proposal; функция не меняет target и
// не объединяет молча два независимо присланных content documents.
func injectResourceSecretContent(object map[string]any, material []byte) (map[string]any, error) {
	if object["apiVersion"] != "v1" || object["kind"] != "Secret" || len(material) == 0 {
		return nil, errors.New(resourceMaterialRequired)
	}
	if _, exists := object["data"]; exists {
		return nil, errors.New(resourceOperationInvalid)
	}
	if _, exists := object["stringData"]; exists {
		return nil, errors.New(resourceOperationInvalid)
	}
	content, err := decodeResourceJSON(material, resourceResponseBytes, 16)
	if err != nil || len(content) > 2 {
		return nil, errors.New(resourceOperationInvalid)
	}
	if validateResourceSecretContent(content) != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	for field, values := range content {
		if field != "data" && field != "stringData" {
			return nil, errors.New(resourceOperationInvalid)
		}
		object[field] = values
	}
	return object, nil
}

// validateResourceSecretContent bounds keys/base64/decoded values after private patch reconstruction.
// validateResourceSecretContent ограничивает keys/base64/decoded values после private patch reconstruction.
func validateResourceSecretContent(content map[string]any) error {
	sizes := make(map[string]int)
	for _, field := range []string{"data", "stringData"} {
		raw, present := content[field]
		if !present {
			continue
		}
		values, ok := raw.(map[string]any)
		if !ok || len(values) > 4096 {
			return errors.New(resourceOperationInvalid)
		}
		for key, raw := range values {
			value, ok := raw.(string)
			if !ok || len(validation.IsConfigMapKey(key)) != 0 || !utf8.ValidString(value) {
				return errors.New(resourceOperationInvalid)
			}
			length := len(value)
			if field == "data" {
				if base64.StdEncoding.DecodedLen(len(value)) > maximumSecretContentBytes {
					return errors.New(resourceOperationInvalid)
				}
				decoded, decodeErr := base64.StdEncoding.DecodeString(value)
				length = len(decoded)
				clear(decoded)
				if decodeErr != nil {
					return errors.New(resourceOperationInvalid)
				}
			}
			sizes[key] = length
			if len(sizes) > 4096 {
				return errors.New(resourceOperationInvalid)
			}
		}
	}
	total := 0
	for _, length := range sizes {
		total += length
		if total > maximumSecretContentBytes {
			return errors.New(resourceOperationInvalid)
		}
	}
	return nil
}
