package kubernetestarget

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// resourceSchemaFixture builds an actual OpenAPI3 component/reference document with explicit
// resource registration and string-map/nullable fields; callers may mutate their private copy.
// resourceSchemaFixture создаёт actual OpenAPI3 component/reference document с explicit
// resource registration и string-map/nullable fields; caller меняет только свою private copy.
func resourceSchemaFixture(t *testing.T) map[string]any {
	t.Helper()
	var document map[string]any
	if json.Unmarshal([]byte(`{"openapi":"3.0.0","components":{"schemas":{"Meta":{"type":"object","properties":{"name":{"type":"string"},"namespace":{"type":"string"},"uid":{"type":"string"},"resourceVersion":{"type":"string"},"labels":{"type":"object","additionalProperties":{"type":"string"}}}},"ConfigMap":{"type":"object","required":["apiVersion","kind","metadata"],"x-kubernetes-group-version-kind":[{"group":"","version":"v1","kind":"ConfigMap"}],"properties":{"apiVersion":{"type":"string"},"kind":{"type":"string"},"metadata":{"$ref":"#/components/schemas/Meta"},"data":{"type":"object","additionalProperties":{"type":"string"}},"optional":{"type":"object","nullable":true,"properties":{"value":{"type":"string"}}}}}}}}`), &document) != nil {
		t.Fatal("invalid owned OpenAPI fixture")
	}
	return document
}

// compileResourceSchemaFixture pins the exact serialized document with Kubernetes' SHA512 hash.
// compileResourceSchemaFixture закрепляет exact serialized document SHA512 hash Kubernetes.
func compileResourceSchemaFixture(document map[string]any) (*registeredResourceSchema, error) {
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	hash := sha512.Sum512(raw)
	return compileRegisteredResourceSchema(raw, schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, strings.ToUpper(hex.EncodeToString(hash[:])))
}

// TestRegisteredResourceSchemaUsesExactLocalGVK validates through the real compiled component
// reference, permits declared map/nullable semantics and rejects unknown/case-aliased fields.
// TestRegisteredResourceSchemaUsesExactLocalGVK проверяет через real compiled component
// reference, допускает declared map/nullable semantics и отклоняет unknown/case-aliased fields.
func TestRegisteredResourceSchemaUsesExactLocalGVK(t *testing.T) {
	compiled, err := compileResourceSchemaFixture(resourceSchemaFixture(t))
	if err != nil {
		t.Fatal("owned registered OpenAPI document was rejected")
	}
	object := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]any{"name": "selected", "namespace": "example"},
		"data": map[string]any{"arbitrary": "business value"}, "optional": nil}
	if compiled.compiled.Validate(object) != nil {
		t.Fatal("registered nullable/map values were rejected")
	}
	if compiled.models["ConfigMap"].AdditionalProperties != nil {
		t.Fatal("validation-only closure changed native Kubernetes patch semantics")
	}
	object["unregistered"] = true
	if compiled.compiled.Validate(object) == nil {
		t.Fatal("unknown resource field escaped registered schema")
	}
	delete(object, "unregistered")
	object["metadata"].(map[string]any)["Name"] = "alias"
	if compiled.compiled.Validate(object) == nil {
		t.Fatal("case-aliased metadata escaped registered schema")
	}
}

// TestRegisteredResourceSchemaRejectsUnpinnedAndExternalDocuments prevents provider URL/ref
// execution and ambiguous registration. All failures occur before any effect transport exists.
// TestRegisteredResourceSchemaRejectsUnpinnedAndExternalDocuments запрещает provider URL/ref
// execution и ambiguous registration. Все отказы происходят до создания effect transport.
func TestRegisteredResourceSchemaRejectsUnpinnedAndExternalDocuments(t *testing.T) {
	for _, mutation := range []string{"external", "missing local", "schema id", "wrong kind", "ambiguous"} {
		t.Run(mutation, func(t *testing.T) {
			document := resourceSchemaFixture(t)
			models := document["components"].(map[string]any)["schemas"].(map[string]any)
			resource := models["ConfigMap"].(map[string]any)
			switch mutation {
			case "external":
				resource["properties"].(map[string]any)["metadata"] = map[string]any{"$ref": "https://foreign.invalid/schema"}
			case "missing local":
				resource["properties"].(map[string]any)["metadata"] = map[string]any{"$ref": "#/components/schemas/Missing"}
			case "schema id":
				resource["id"] = "https://foreign.invalid/schema"
			case "wrong kind":
				resource["x-kubernetes-group-version-kind"] = []any{map[string]any{"group": "", "version": "v1", "kind": "Secret"}}
			case "ambiguous":
				models["Duplicate"] = resource
			}
			if _, err := compileResourceSchemaFixture(document); err == nil {
				t.Fatal("unregistered/foreign schema was admitted")
			}
		})
	}
	raw, _ := json.Marshal(resourceSchemaFixture(t))
	if _, err := compileRegisteredResourceSchema(raw, schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, strings.Repeat("a", 128)); err == nil {
		t.Fatal("changed bytes reused an immutable schema pin")
	}
}

// TestResourceJSONClosesAmbiguousProviderRepresentations rejects syntax ambiguity before
// a body reaches schema validation or an object-policy predicate.
// TestResourceJSONClosesAmbiguousProviderRepresentations отклоняет syntax ambiguity до передачи
// body в schema validation либо object-policy predicate.
func TestResourceJSONClosesAmbiguousProviderRepresentations(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"metadata":{"name":"first","name":"second"}}`),
		[]byte(`{"metadata":{}} {}`), []byte(`null`), []byte(`[]`),
		[]byte("{\"value\":\"\xff\"}"),
		[]byte(`{"deep":` + strings.Repeat(`[`, 130) + `0` + strings.Repeat(`]`, 130) + `}`),
	} {
		if _, err := decodeResourceJSON(raw, resourceSchemaBytes, 128); err == nil {
			t.Fatal("ambiguous provider representation reached semantic use")
		}
	}
}
