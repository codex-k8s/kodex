package kubernetestarget

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// prepareResourcePatch preserves native JSON/MERGE/STRATEGIC request semantics while binding
// them to the exact current UID/RV. Every reconstructed object is later schema/policy checked;
// unsupported strategic models fail without a JSON Merge or whole-object replacement fallback.
// prepareResourcePatch сохраняет native JSON/MERGE/STRATEGIC request semantics с exact current
// UID/RV. Reconstructed object затем проходит schema/policy; unsupported strategic models
// отклоняются без JSON Merge либо whole-object replacement fallback.
func (prepared *PreparedResourceOperation) prepareResourcePatch(raw []byte, options map[string]any) ([]byte, string, map[string]any, error) {
	patchType, ok := options["patchType"].(string)
	if !ok || prepared.current == nil {
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	private := prepared.materialRef != ""
	if private && managedkubernetes.ValidateProtectedSecretPatchBody(raw, patchType) != nil {
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	current, err := json.Marshal(prepared.current)
	if err != nil {
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	defer clear(current)
	var wire, reconstructed []byte
	contentType := "application/merge-patch+json"
	switch patchType {
	case "JSON":
		var operations []map[string]json.RawMessage
		if json.Unmarshal(raw, &operations) != nil || len(operations) > 256 {
			return nil, "", nil, errors.New(resourceOperationInvalid)
		}
		prefix := []map[string]json.RawMessage{{"op": json.RawMessage(`"test"`), "path": json.RawMessage(`"/metadata/uid"`)},
			{"op": json.RawMessage(`"test"`), "path": json.RawMessage(`"/metadata/resourceVersion"`)}}
		prefix[0]["value"], _ = json.Marshal(prepared.address.UID)
		prefix[1]["value"], _ = json.Marshal(prepared.address.ResourceVersion)
		for _, operation := range operations {
			if !resourcePatchIdentitySafe(operation, private, prepared.address.Subresource == "status") {
				return nil, "", nil, errors.New(resourceOperationInvalid)
			}
		}
		wire, err = json.Marshal(append(prefix, operations...))
		if err == nil {
			reconstructed, err = applyBoundedResourceJSONPatch(current, append(prefix, operations...))
		}
		contentType = "application/json-patch+json"
	case "MERGE", "STRATEGIC":
		patch, decodeErr := decodeResourceJSON(raw, resourceResponseBytes, 64)
		if decodeErr != nil {
			return nil, "", nil, decodeErr
		}
		if prepared.address.Subresource == "status" && !resourceStatusBodyOnly(patch) {
			return nil, "", nil, errors.New(resourceOperationInvalid)
		}
		if private {
			if metadata, exists := patch["metadata"]; exists {
				fields, valid := metadata.(map[string]any)
				if !valid {
					return nil, "", nil, errors.New(resourceOperationInvalid)
				}
				for _, key := range []string{"name", "namespace", "uid", "resourceVersion", "generateName"} {
					if _, present := fields[key]; present {
						return nil, "", nil, errors.New(resourceOperationInvalid)
					}
				}
			}
		}
		if err = pinResourceBodyIdentity(patch, prepared.address); err != nil {
			return nil, "", nil, err
		}
		wire, err = json.Marshal(patch)
		if err == nil && patchType == "MERGE" {
			reconstructed, err = jsonpatch.MergePatch(current, wire)
		} else if err == nil {
			if !clientgoscheme.Scheme.Recognizes(prepared.schema.kind) {
				return nil, "", nil, errors.New(resourceOperationInvalid)
			}
			metadata := strategicpatch.PatchMetaFromOpenAPIV3{SchemaList: prepared.schema.models, Schema: prepared.schema.models[prepared.schema.name]}
			reconstructed, err = strategicpatch.StrategicMergePatchUsingLookupPatchMeta(current, wire, metadata)
			contentType = "application/strategic-merge-patch+json"
		}
	default:
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	if err != nil || len(wire) > resourceResponseBytes || len(reconstructed) > resourceResponseBytes {
		clear(wire)
		clear(reconstructed)
		return nil, "", nil, errors.New(resourceOperationInvalid)
	}
	defer clear(reconstructed)
	proposed, err := decodeResourceJSON(reconstructed, resourceResponseBytes, 64)
	if err != nil {
		clear(wire)
		return nil, "", nil, err
	}
	return wire, contentType, proposed, nil
}

// applyBoundedResourceJSONPatch applies the existing JSON Patch library one operation at a time
// solely to bound intermediate copy growth. The provider still receives one atomic native patch.
// No process-global library size limit is mutated and no ad-hoc patch interpreter is introduced.
// applyBoundedResourceJSONPatch применяет existing JSON Patch library по одной operation только
// для ограничения intermediate copy growth. Provider получает один atomic native patch.
// Process-global size limit не меняется; отдельный ad-hoc patch interpreter не создаётся.
func applyBoundedResourceJSONPatch(current []byte, operations []map[string]json.RawMessage) ([]byte, error) {
	owned := append([]byte(nil), current...)
	for _, operation := range operations {
		raw, err := json.Marshal([]map[string]json.RawMessage{operation})
		if err != nil {
			clear(owned)
			return nil, errors.New(resourceOperationInvalid)
		}
		patch, err := jsonpatch.DecodePatch(raw)
		if err != nil {
			clear(raw)
			clear(owned)
			return nil, errors.New(resourceOperationInvalid)
		}
		next, err := patch.Apply(owned)
		// The library may reuse backing storage for a no-op; retain independent owned bytes.
		// Library может reuse backing storage для no-op; сохраняются independent owned bytes.
		next = append([]byte(nil), next...)
		clear(raw)
		clear(owned)
		if err != nil || len(next) > resourceResponseBytes {
			clear(next)
			return nil, errors.New(resourceOperationInvalid)
		}
		owned = next
	}
	return owned, nil
}

// resourcePatchIdentitySafe prevents caller-directed name/namespace/UID/RV changes, including
// an ancestor metadata/root replacement or move source. Status mutations remain inside status.
// Private Secret content patches may not address readonly identity even as a test or copy source;
// their preconditions come only from the broker's observed object and server-resolved upload.
// resourcePatchIdentitySafe запрещает caller-directed name/namespace/UID/RV changes, включая
// ancestor metadata/root replacement либо move source. Status mutations ограничены status.
// Private Secret content patches не адресуют readonly identity даже как test/copy source:
// preconditions берутся только из broker observation и server-resolved upload.
func resourcePatchIdentitySafe(operation map[string]json.RawMessage, private, statusOnly bool) bool {
	var verb, path, from string
	if json.Unmarshal(operation["op"], &verb) != nil || json.Unmarshal(operation["path"], &path) != nil {
		return false
	}
	if raw, present := operation["from"]; present && json.Unmarshal(raw, &from) != nil {
		return false
	}
	if (verb != "test" || private) && resourceIdentityPointer(path) ||
		(verb == "move" || private && from != "") && resourceIdentityPointer(from) {
		return false
	}
	if private && (verb == "copy" || verb == "move") && resourceSecretDataPointer(from) && !resourceSecretDataPointer(path) {
		return false
	}
	return !statusOnly || verb == "test" || path == "/status" || strings.HasPrefix(path, "/status/")
}

// resourceSecretDataPointer recognizes only the two exact Secret value containers. This is a
// finite private/public transfer boundary, not a generic taint interpreter or a key-name deny.
// resourceSecretDataPointer распознаёт лишь два exact Secret value containers. Это finite
// private/public transfer boundary, не generic taint interpreter либо key-name deny.
func resourceSecretDataPointer(pointer string) bool {
	return pointer == "/data" || strings.HasPrefix(pointer, "/data/") || pointer == "/stringData" || strings.HasPrefix(pointer, "/stringData/")
}

// resourceIdentityPointer recognizes decoded RFC6901 path segments, not string prefixes that
// could confuse a mutable annotation key with an immutable identity field.
// resourceIdentityPointer распознаёт decoded RFC6901 segments, не string prefixes, которые
// могли бы смешать mutable annotation key с immutable identity field.
func resourceIdentityPointer(pointer string) bool {
	if pointer == "" || pointer == "/metadata" {
		return true
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for index := range parts {
		parts[index] = strings.ReplaceAll(strings.ReplaceAll(parts[index], "~1", "/"), "~0", "~")
	}
	if len(parts) >= 2 && parts[0] == "metadata" {
		switch parts[1] {
		case "name", "namespace", "uid", "resourceVersion", "generateName":
			return true
		}
	}
	return false
}
