package managedkubernetes

import (
	"strings"
	"testing"
)

// TestSecretMaterialInputBoundary keeps resource credential values out of durable invocation
// input while preserving typed metadata and an opaque owner-provisioned material reference.
// TestSecretMaterialInputBoundary исключает resource credential values из durable invocation
// input, сохраняя typed metadata и opaque owner-provisioned material reference.
func TestSecretMaterialInputBoundary(t *testing.T) {
	create := `{"group":"","version":"v1","resource":"secrets","namespace":"example","body":{"apiVersion":"v1","kind":"Secret","metadata":{"name":"credentials","namespace":"example"},"type":"Opaque"},"options":{"secretMaterialRef":"krs_owner_material"}}`
	if _, raw, err := DecodeOperationInput("kubernetes.create", []byte(create)); err != nil || strings.Contains(string(raw), "private-material-canary") {
		t.Fatal("metadata-only Secret material reference rejected")
	}
	for _, raw := range []string{
		strings.Replace(create, `"type":"Opaque"`, `"type":"Opaque","data":{"password":"private-material-canary"}`, 1),
		strings.Replace(create, `"type":"Opaque"`, `"type":"Opaque","stringData":{"password":"private-material-canary"}`, 1),
		strings.Replace(create, `"type":"Opaque"`, `"type":"Opaque","data":{}`, 1),
		strings.Replace(create, `"type":"Opaque"`, `"type":"Opaque","Data":{"password":"private-material-canary"}`, 1),
		strings.Replace(create, `"type":"Opaque"`, `"type":"Opaque","STRINGDATA":{"password":"private-material-canary"}`, 1),
		strings.Replace(create, `"type":"Opaque"`, `"type":"kubernetes.io/service-account-token"`, 1),
		strings.Replace(create, `"resource":"secrets"`, `"resource":"configmaps"`, 1),
		strings.Replace(create, `"kind":"Secret"`, `"kind":"ConfigMap"`, 1),
		strings.Replace(create, `"krs_owner_material"`, `"generic_owner_material"`, 1),
	} {
		// Both route and body must independently select the core Secret representation.
		// Route и body должны независимо выбирать core Secret representation.
		if strings.Contains(raw, `"resource":"configmaps"`) || strings.Contains(raw, `"kind":"ConfigMap"`) {
			raw = strings.ReplaceAll(raw, `"type":"Opaque"`, `"type":"Opaque","data":{"password":"private-material-canary"}`)
		}
		if input, canonical, err := DecodeOperationInput("kubernetes.create", []byte(raw)); err != ErrInvalidOperationInput || canonical != nil || input.Resource != "" {
			t.Fatal("Secret material escaped strict input refusal")
		}
	}
	base := `"group":"","version":"v1","resource":"secrets","namespace":"example","name":"credentials","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"`
	for _, patch := range []string{`{"data":{"password":"private-material-canary"}}`, `{"stringData":{"password":null}}`, `[{"op":"test","path":"/data/password","value":"private-material-canary"}]`, `[{"op":"copy","from":"/data/password","path":"/metadata/annotations/leak"}]`, `[{"op":"add","path":"/stringData/password","value":"private-material-canary"}]`} {
		patchType := "MERGE"
		if strings.HasPrefix(patch, "[") {
			patchType = "JSON"
		}
		raw := `{` + base + `,"body":` + patch + `,"options":{"patchType":"` + patchType + `"}}`
		if _, canonical, err := DecodeOperationInput("kubernetes.patch", []byte(raw)); err != ErrInvalidOperationInput || canonical != nil {
			t.Fatal("Secret patch material/test/copy crossed the input boundary")
		}
	}
	configmap := strings.ReplaceAll(strings.ReplaceAll(create, `"resource":"secrets"`, `"resource":"configmaps"`), `"kind":"Secret"`, `"kind":"ConfigMap"`)
	if _, _, err := DecodeOperationInput("kubernetes.create", []byte(configmap)); err != ErrInvalidOperationInput {
		t.Fatal("ordinary resource accepted a Secret material selector")
	}
	metadataPatch := `{` + base + `,"body":{"metadata":{"labels":{"purpose":"example"}}},"options":{"patchType":"MERGE"}}`
	if _, _, err := DecodeOperationInput("kubernetes.patch", []byte(metadataPatch)); err != nil {
		t.Fatal("metadata-only Secret patch was silently removed")
	}
}

// TestSecretPrivatePatchPlaceholder preserves native patch ordering entirely inside the private
// document. Public metadata and private patch arrays are never silently combined or reordered.
// TestSecretPrivatePatchPlaceholder сохраняет native patch ordering целиком в private document.
// Public metadata и private patch arrays не объединяются и не переупорядочиваются скрыто.
func TestSecretPrivatePatchPlaceholder(t *testing.T) {
	base := `"group":"","version":"v1","resource":"secrets","namespace":"example","name":"credentials","uid":"dc7b5a7f-2421-41bf-bf3b-71f7b362fda8","resourceVersion":"17"`
	for _, mode := range []string{"JSON", "MERGE", "STRATEGIC"} {
		t.Run(mode, func(t *testing.T) {
			body := "{}"
			if mode == "JSON" {
				body = "[]"
			}
			raw := `{` + base + `,"body":` + body + `,"options":{"patchType":"` + mode + `","secretMaterialRef":"krs_owner_material"}}`
			if _, _, err := DecodeOperationInput("kubernetes.patch", []byte(raw)); err != nil {
				t.Fatal("private patch placeholder rejected")
			}
			withoutRef := strings.Replace(raw, `,"secretMaterialRef":"krs_owner_material"`, "", 1)
			if _, _, err := DecodeOperationInput("kubernetes.patch", []byte(withoutRef)); err != ErrInvalidOperationInput {
				t.Fatal("empty ordinary patch acquired private semantics")
			}
			mixed := "{\"metadata\":{\"labels\":{\"mode\":\"value\"}}}"
			if mode == "JSON" {
				mixed = `[{"op":"add","path":"/metadata/labels/mode","value":"value"}]`
			}
			raw = strings.Replace(raw, `"body":`+body, `"body":`+mixed, 1)
			if _, _, err := DecodeOperationInput("kubernetes.patch", []byte(raw)); err != ErrInvalidOperationInput {
				t.Fatal("public and private patches were silently combined")
			}
		})
	}
}
