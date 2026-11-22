package kubernetestarget

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// projectResourceResponse validates actual ACK identity before returning bounded safe data.
// Mutation grants receive only an acknowledgement, never a read-by-write full object echo.
// READ candidates still require the upper owner's fresh result credit before publication.
// projectResourceResponse проверяет actual ACK identity до bounded safe data. Mutation grant
// получает только acknowledgement, без full object echo через write. Для выдачи READ candidate
// по-прежнему нужен fresh result credit верхнего owner.
func (prepared *PreparedResourceOperation) projectResourceResponse(raw []byte) ([]byte, string, string, error) {
	object, err := decodeResourceJSON(raw, resourceResponseBytes, 64)
	if err != nil {
		return nil, "", "", err
	}
	if resourceReadReferencesProtectedNamespace(object, prepared.protected) {
		return nil, "", "", errors.New(resourcePolicyForbidden)
	}
	if prepared.operation.Key == "kubernetes.delete" && object["kind"] == "Status" {
		details, valid := object["details"].(map[string]any)
		if object["apiVersion"] != "v1" || object["status"] != "Success" || !valid || details["uid"] != prepared.address.UID ||
			details["name"] != prepared.address.Name || details["group"] != nil && details["group"] != prepared.address.Group ||
			details["kind"] != prepared.address.Resource {
			return nil, "", "", errors.New(resourceOperationInvalid)
		}
		result, err := marshalResourceSafeResult(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Success",
			"details": map[string]any{"name": prepared.address.Name, "uid": prepared.address.UID, "group": prepared.address.Group, "kind": prepared.address.Resource}})
		return result, prepared.address.UID, "", err
	}
	if prepared.operation.Verb == "list" {
		return prepared.projectResourceList(object)
	}
	if prepared.schema.compiled.Validate(object) != nil {
		return nil, "", "", errors.New(resourceOperationInvalid)
	}
	expected := prepared.address
	if prepared.method == "POST" {
		metadata, ok := object["metadata"].(map[string]any)
		if !ok {
			return nil, "", "", errors.New(resourceOperationInvalid)
		}
		expected.Name, _ = metadata["name"].(string)
		// The nonreplayable request body has already been consumed and cleared; use only the validated private proposal.
		// Неповторяемый request body уже израсходован и очищен; используется только проверенный private proposal.
		intent, _ := prepared.proposed["metadata"].(map[string]any)
		name, _ := intent["name"].(string)
		prefix, _ := intent["generateName"].(string)
		if name != "" && name != expected.Name || name == "" && (prefix == "" || !strings.HasPrefix(expected.Name, prefix) || len(expected.Name) <= len(prefix)) {
			return nil, "", "", errors.New(resourceOperationInvalid)
		}
	}
	observed, err := resourceObjectIdentity(object, expected, prepared.schema)
	if err != nil {
		if prepared.operation.Risk == "READ" && prepared.observation.State == managedkubernetes.ObservationUnversionedRead &&
			prepared.observation.Validate() == nil && resourceUnversionedIdentityMatches(object, expected, prepared.schema) {
			result, err := marshalResourceSafeResult(sanitizeResourceRead(object))
			return result, "", "", err
		}
		return nil, "", "", errors.New(resourceOperationInvalid)
	}
	if prepared.method != "POST" && observed.UID != prepared.address.UID ||
		prepared.operation.Risk == "READ" && prepared.input.ResourceVersion != "" && observed.ResourceVersion != prepared.input.ResourceVersion {
		return nil, "", "", errors.New(resourceOperationInvalid)
	}
	var result []byte
	if prepared.operation.Risk == "READ" {
		result, err = projectResourceReadObject(object, observed)
	} else {
		metadata := map[string]any{"name": observed.Name, "uid": observed.UID, "resourceVersion": observed.ResourceVersion}
		if observed.Namespaced {
			metadata["namespace"] = observed.Namespace
		}
		result, err = marshalResourceSafeResult(map[string]any{"apiVersion": object["apiVersion"], "kind": object["kind"], "metadata": metadata})
	}
	return result, observed.UID, observed.ResourceVersion, err
}

// projectResourceList binds every item to the selected GVR/scope and filters protected namespaces.
// It preserves continuation metadata so a bounded collection is never presented as exhaustive.
// projectResourceList связывает каждый item с selected GVR/scope и фильтрует protected namespaces.
// Сохраняет continuation metadata: bounded collection не выдаётся за exhaustive.
func (prepared *PreparedResourceOperation) projectResourceList(object map[string]any) ([]byte, string, string, error) {
	items, valid := object["items"].([]any)
	metadata, metaValid := object["metadata"].(map[string]any)
	if !valid || !metaValid || len(items) > 500 || object["apiVersion"] != prepared.schema.kind.GroupVersion().String() ||
		object["kind"] != prepared.schema.kind.Kind+"List" {
		return nil, "", "", errors.New(resourceOperationInvalid)
	}
	safeItems := make([]any, 0, len(items))
	for _, value := range items {
		item, ok := value.(map[string]any)
		fields, fieldsValid := item["metadata"].(map[string]any)
		if !ok || !fieldsValid || prepared.schema.compiled.Validate(item) != nil {
			return nil, "", "", errors.New(resourceOperationInvalid)
		}
		address := prepared.address
		address.Name, _ = fields["name"].(string)
		address.UID, address.ResourceVersion, address.AllNamespaces = "", "", false
		if prepared.address.AllNamespaces {
			address.Namespace, _ = fields["namespace"].(string)
		}
		observed, identityErr := resourceObjectIdentity(item, address, prepared.schema)
		unversioned := false
		if identityErr != nil && prepared.schema.unversionedRead && resourceUnversionedIdentityMatches(item, address, prepared.schema) {
			witness := prepared.observation
			witness.State, witness.Resource = managedkubernetes.ObservationUnversionedRead, address
			unversioned = witness.Validate() == nil
		}
		if identityErr != nil && !unversioned {
			return nil, "", "", identityErr
		}
		if !unversioned {
			address = observed
		}
		if resourceNamespaceProtected(address, prepared.protected) || resourceReadReferencesProtectedNamespace(item, prepared.protected) {
			continue
		}
		var raw []byte
		var err error
		if unversioned {
			raw, err = marshalResourceSafeResult(sanitizeResourceRead(item))
		} else {
			raw, err = projectResourceReadObject(item, address)
		}
		if err != nil {
			return nil, "", "", err
		}
		var safe any
		if json.Unmarshal(raw, &safe) != nil {
			return nil, "", "", errors.New(resourceOperationInvalid)
		}
		safeItems = append(safeItems, safe)
	}
	safeMetadata := map[string]any{}
	for _, key := range []string{"resourceVersion", "continue", "remainingItemCount"} {
		if value, exists := metadata[key]; exists {
			if key == "remainingItemCount" {
				count, valid := resourceInteger(value)
				if !valid || count < 0 {
					return nil, "", "", errors.New(resourceOperationInvalid)
				}
			} else if text, valid := value.(string); !valid || len(text) > 4096 {
				return nil, "", "", errors.New(resourceOperationInvalid)
			}
			safeMetadata[key] = value
		}
	}
	raw, err := marshalResourceSafeResult(map[string]any{"apiVersion": object["apiVersion"], "kind": object["kind"], "metadata": safeMetadata, "items": safeItems})
	return raw, "", "", err
}

// projectResourceReadObject removes all Secret values before any result formatting.
// projectResourceReadObject удаляет все Secret values до любого result formatting.
func projectResourceReadObject(object map[string]any, address ResourceAddress) ([]byte, error) {
	if address.Group == "" && address.Resource == "secrets" {
		raw, err := json.Marshal(object)
		if err != nil {
			return nil, errors.New(resourceOperationInvalid)
		}
		defer clear(raw)
		projection, err := ProjectSecret(raw, address)
		if err != nil {
			return nil, err
		}
		return marshalResourceSafeResult(projection)
	}
	return marshalResourceSafeResult(sanitizeResourceRead(object))
}

// sanitizeResourceRead drops known free-form last-applied snapshots from otherwise readable objects.
// It operates only on broker-owned maps, never on caller input or a cached grant.
// sanitizeResourceRead удаляет known free-form last-applied snapshots из readable objects.
// Работает только с broker-owned maps, не с caller input либо cached grant.
func sanitizeResourceRead(object map[string]any) map[string]any {
	if metadata, ok := object["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
		if annotations, ok := metadata["annotations"].(map[string]any); ok {
			delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		}
	}
	return object
}

// resourceReadReferencesProtectedNamespace prevents cluster PV/Namespace lists from bypassing
// the protected namespace boundary through a foreign namespace locator.
// resourceReadReferencesProtectedNamespace не позволяет cluster PV/Namespace list обойти
// protected namespace boundary через foreign namespace locator.
func resourceReadReferencesProtectedNamespace(object map[string]any, protected []string) bool {
	metadata, _ := object["metadata"].(map[string]any)
	if object["apiVersion"] == "v1" && object["kind"] == "Namespace" {
		for _, namespace := range protected {
			if metadata["name"] == namespace {
				return true
			}
		}
	}
	if object["apiVersion"] == "v1" && object["kind"] == "PersistentVolume" {
		spec, _ := object["spec"].(map[string]any)
		claim, _ := spec["claimRef"].(map[string]any)
		for _, namespace := range protected {
			if claim["namespace"] == namespace {
				return true
			}
		}
	}
	return false
}
