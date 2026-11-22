package kubernetestarget

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// secretProjectionFixture puts distinct synthetic beacons in every unsafe Secret output path.
// secretProjectionFixture размещает разные synthetic beacons во всех unsafe Secret output paths.
func secretProjectionFixture() (string, ResourceAddress) {
	return `{"apiVersion":"v1","kind":"Secret","type":"Opaque","metadata":{
	"name":"selected","namespace":"selected","uid":"9a99d4f2-836d-4efb-868e-5de8792ade07","resourceVersion":"37",
	"creationTimestamp":"2026-11-14T00:00:00Z",
	"labels":{"unsafe":"synthetic-label-beacon"},
	"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"synthetic-last-applied-beacon"},
	"managedFields":[{"manager":"synthetic-managed-fields-beacon","operation":"Update","apiVersion":"v1"}]},
	"data":{"token":"c3ludGhldGljLXByaXZhdGUtZGF0YS1iZWFjb24=","cert":"c3ludGhldGljLWNlcnRpZmljYXRl"},
	"stringData":{"token":"synthetic-private-stringdata-beacon","password":"synthetic-password-beacon"}}`,
		ResourceAddress{Version: "v1", Resource: "secrets", Namespaced: true, Namespace: "selected", Name: "selected",
			UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37"}
}

// TestProjectSecretCannotExposeValuesOrFreeFormMetadata proves formats receive only the safe shape.
// It covers stringData overlays and last-applied/managedFields, not only the ordinary data map.
// TestProjectSecretCannotExposeValuesOrFreeFormMetadata доказывает выдачу formats только safe shape.
// Проверяет stringData overlays и last-applied/managedFields, а не только обычную data map.
func TestProjectSecretCannotExposeValuesOrFreeFormMetadata(t *testing.T) {
	raw, address := secretProjectionFixture()
	result, err := ProjectSecret([]byte(raw), address)
	if err != nil || result.Metadata.UID != address.UID || result.Metadata.ResourceVersion != "37" ||
		!reflect.DeepEqual(result.KeyNames, []string{"cert", "password", "token"}) {
		t.Fatal("exact Secret metadata/key-name projection failed")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal("encode safe Secret projection")
	}
	for _, forbidden := range []string{"synthetic-", "c3ludGhldGlj", `"data"`, `"stringData"`, `"annotations"`, `"labels"`, `"managedFields"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("private values or free-form Secret metadata escaped projection")
		}
	}
	address.UID, address.ResourceVersion = "", ""
	if _, err = ProjectSecret([]byte(raw), address); err != nil {
		t.Fatal("owner-scoped initial read could not obtain safe UID metadata")
	}
}

// TestProjectSecretRejectsForeignAndAmbiguousObjects closes all error paths without raw fallback.
// TestProjectSecretRejectsForeignAndAmbiguousObjects закрывает error paths без raw fallback.
func TestProjectSecretRejectsForeignAndAmbiguousObjects(t *testing.T) {
	raw, address := secretProjectionFixture()
	for _, changed := range []string{
		strings.Replace(raw, `"kind":"Secret"`, `"kind":"ConfigMap"`, 1),
		strings.Replace(raw, `"namespace":"selected"`, `"namespace":"foreign"`, 1),
		strings.Replace(raw, `"name":"selected"`, `"name":"foreign"`, 1),
		strings.Replace(raw, address.UID, "7fb0912f-a690-453f-be9a-3520d2e99a85", 1),
		strings.Replace(raw, `"resourceVersion":"37"`, `"resourceVersion":"38"`, 1),
		strings.Replace(raw, `"name":"selected"`, `"name":"foreign","name":"selected"`, 1),
		strings.Replace(raw, `"data":{`, `"data":{"token":"foreign",`, 1),
		strings.Replace(raw, `"token":"c3ludGhldGljLXByaXZhdGUtZGF0YS1iZWFjb24="`, `"token":{}`, 1),
		strings.Replace(raw, `"token":"c3ludGhldGljLXByaXZhdGUtZGF0YS1iZWFjb24="`, `"token":null`, 1),
		strings.Replace(raw, "Opaque", "invalid-"+string([]byte{0xff}), 1),
		strings.Replace(raw, `"kind":"Secret"`, `"kind":"Secret","unsafeRawOutput":true`, 1),
		strings.Replace(raw, `"token":"c3ludGhldGlj`, `"../token":"c3ludGhldGlj`, 1),
		raw + `{}`,
		strings.Repeat(" ", maximumSecretProjectionInput+1),
	} {
		result, err := ProjectSecret([]byte(changed), address)
		if err == nil || err.Error() != invalidSecretProjection || !reflect.DeepEqual(result, SecretProjection{}) {
			t.Fatal("foreign or ambiguous Secret escaped projection or disclosed diagnostics")
		}
	}
	for _, mutate := range []func(*ResourceAddress){
		func(value *ResourceAddress) { value.Resource = "pods" },
		func(value *ResourceAddress) { value.Name = "" },
		func(value *ResourceAddress) { value.Subresource = "status" },
		func(value *ResourceAddress) { value.Namespaced, value.Namespace = false, "" },
	} {
		changed := address
		mutate(&changed)
		if _, err := ProjectSecret([]byte(raw), changed); err == nil {
			t.Fatal("Secret projection accepted another resource scope")
		}
	}
}

// TestProjectSecretBoundsExpandedKeyNames rejects a small valid input whose key-only output exceeds
// the safe-result budget; raw credential values are never substituted to salvage a response.
// TestProjectSecretBoundsExpandedKeyNames отклоняет valid input, чей key-only output превышает
// safe-result budget; raw credential values не выдаются ради сохранения ответа.
func TestProjectSecretBoundsExpandedKeyNames(t *testing.T) {
	raw, address := secretProjectionFixture()
	var document map[string]any
	if json.Unmarshal([]byte(raw), &document) != nil {
		t.Fatal("decode disposable projection fixture")
	}
	data := make(map[string]string)
	for index := range 1024 {
		data[fmt.Sprintf("key-%04d-%s", index, strings.Repeat("a", 120))] = ""
	}
	document["data"], document["stringData"] = data, nil
	encoded, err := json.Marshal(document)
	if err != nil || len(encoded) >= maximumSecretProjectionInput {
		t.Fatal("expanded key-name fixture exceeds input budget")
	}
	if _, err = ProjectSecret(encoded, address); err == nil {
		t.Fatal("key-name projection exceeded its output budget")
	}
}
