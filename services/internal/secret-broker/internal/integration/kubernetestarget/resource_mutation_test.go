package kubernetestarget

import (
	"encoding/json"
	"testing"
)

// TestResourceStatusCannotChangeSpecificationOrMetadata checks both narrow status forms and
// normal full-object update semantics without allowing hidden label/spec mutation authority.
// TestResourceStatusCannotChangeSpecificationOrMetadata проверяет narrow status forms и
// normal full-object update semantics без скрытого label/spec mutation authority.
func TestResourceStatusCannotChangeSpecificationOrMetadata(t *testing.T) {
	current, _ := decodeResourceJSON([]byte(resourceWireObject("10")), resourceResponseBytes, 64)
	full, _ := cloneResourceObject(current)
	full["status"] = map[string]any{"ready": false}
	if !resourceStatusUpdateMatchesCurrent(full, current) || !resourceStatusBodyOnly(map[string]any{"status": map[string]any{"ready": false}}) {
		t.Fatal("unchanged full object or narrow status update was rejected")
	}
	full["data"].(map[string]any)["private-business"] = "changed"
	if resourceStatusUpdateMatchesCurrent(full, current) {
		t.Fatal("status capability acquired specification changes")
	}
	full, _ = cloneResourceObject(current)
	full["metadata"].(map[string]any)["labels"] = map[string]any{"changed": "label"}
	if resourceStatusUpdateMatchesCurrent(full, current) || resourceStatusBodyOnly(map[string]any{"metadata": map[string]any{"labels": map[string]any{"changed": "label"}}}) {
		t.Fatal("status capability acquired metadata changes")
	}
}

// TestResourcePrivatePatchCannotTransferSecretValuesToPublicFields preserves native data-to-data
// copy/move while refusing private-to-metadata/type/other fields before Bind can become ready.
// TestResourcePrivatePatchCannotTransferSecretValuesToPublicFields сохраняет native data-to-data
// copy/move, отклоняя private-to-metadata/type/other fields до ready Bind.
func TestResourcePrivatePatchCannotTransferSecretValuesToPublicFields(t *testing.T) {
	for _, verb := range []string{"copy", "move"} {
		for _, destination := range []string{"/metadata/annotations/leak", "/metadata/labels/leak", "/type", "/immutable", "/other"} {
			operation := map[string]json.RawMessage{}
			operation["op"], _ = json.Marshal(verb)
			operation["path"], _ = json.Marshal(destination)
			operation["from"] = json.RawMessage(`"/data/private"`)
			if resourcePatchIdentitySafe(operation, true, false) {
				t.Fatal("private Secret values could flow into public metadata or schema fields")
			}
			operation["path"] = json.RawMessage(`"/data/renamed"`)
			if !resourcePatchIdentitySafe(operation, true, false) {
				t.Fatal("native private data-to-data copy/move was silently disabled")
			}
		}
	}
}

// TestResourceJSONPatchNoOpAndIdentityPreconditions verifies bounded native reconstruction,
// including an empty caller patch and exact UID/RV tests before a proposed value is modified.
// TestResourceJSONPatchNoOpAndIdentityPreconditions проверяет bounded native reconstruction,
// включая empty caller patch и exact UID/RV tests до изменения proposed value.
func TestResourceJSONPatchNoOpAndIdentityPreconditions(t *testing.T) {
	raw := []byte(resourceWireObject("10"))
	operations := []map[string]json.RawMessage{{"op": json.RawMessage(`"test"`), "path": json.RawMessage(`"/metadata/resourceVersion"`), "value": json.RawMessage(`"10"`)}}
	result, err := applyBoundedResourceJSONPatch(raw, operations)
	if err != nil || !json.Valid(result) || string(raw) != resourceWireObject("10") {
		t.Fatal("native test/no-op patch corrupted caller bytes")
	}
	operations[0]["value"] = json.RawMessage(`"11"`)
	if result, err := applyBoundedResourceJSONPatch(raw, operations); err == nil || len(result) != 0 {
		t.Fatal("stale resourceVersion test became a runnable patch")
	}
}
