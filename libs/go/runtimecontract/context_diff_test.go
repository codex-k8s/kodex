package runtimecontract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestRuntimeContextShapesPreserveWire checks exact JSON field order and tags, omitting zero
// descriptor fields while preserving null versus empty arrays, Unicode, and ordinary JSON int64
// width. It also round-trips the consumer's distinct named types; it does not validate semantic bounds.
// TestRuntimeContextShapesPreserveWire проверяет точные порядок и теги JSON, опускание нулевых полей
// дескриптора, различие null и пустых массивов, Unicode и ширину int64 обычного JSON. Также проверяется
// обратное преобразование отдельных именованных типов потребителя, но не семантические пределы.
func TestRuntimeContextShapesPreserveWire(t *testing.T) {
	for _, fixture := range []struct {
		name  string
		value any
		want  string
	}{
		{"empty descriptor", continuationDescriptor{}, `{}`},
		{"full descriptor", RuntimeContextDescriptor{Ref: "ref", Version: 9223372036854775807, Digest: "digest", Value: "текст"}, `{"ref":"ref","version":9223372036854775807,"digest":"digest","value":"текст"}`},
		{"null arrays", continuationChange{}, `{"component":"","previous":null,"current":null,"action":""}`},
		{"empty arrays", RuntimeContextChange[continuationDescriptor]{Component: "MODEL", Previous: []continuationDescriptor{}, Current: []continuationDescriptor{}, Action: "USE_CURRENT_CONTEXT"}, `{"component":"MODEL","previous":[],"current":[],"action":"USE_CURRENT_CONTEXT"}`},
		{"empty diff", continuationDiff{}, `{"previousRevisionRef":"","currentRevisionRef":"","sessionRef":"","turnRef":"","attempt":0,"changes":null,"digest":""}`},
		{"ordered diff", RuntimeContextDiff[continuationChange]{PreviousRevisionRef: "old", CurrentRevisionRef: "new", SessionRef: "session", TurnRef: "turn", Attempt: 2, Changes: []continuationChange{{Component: "MODEL", Current: []continuationDescriptor{{Value: "selected"}}, Action: "USE_CURRENT_CONTEXT"}}, Digest: "digest"}, `{"previousRevisionRef":"old","currentRevisionRef":"new","sessionRef":"session","turnRef":"turn","attempt":2,"changes":[{"component":"MODEL","previous":null,"current":[{"value":"selected"}],"action":"USE_CURRENT_CONTEXT"}],"digest":"digest"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := json.Marshal(fixture.value)
			if err != nil || string(raw) != fixture.want {
				t.Fatalf("wire mismatch: got %s, want %s, err %v", raw, fixture.want, err)
			}
			decoded := reflect.New(reflect.TypeOf(fixture.value)).Interface()
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(decoded); err != nil {
				t.Fatal(err)
			}
			roundtrip, err := json.Marshal(decoded)
			if err != nil || !bytes.Equal(raw, roundtrip) {
				t.Fatalf("roundtrip mismatch: %s, err %v", roundtrip, err)
			}
		})
	}
	if reflect.TypeOf(continuationDescriptor{}) == reflect.TypeOf(RuntimeContextDescriptor{}) ||
		reflect.TypeOf(continuationChange{}) == reflect.TypeOf(RuntimeContextChange[continuationDescriptor]{}) ||
		reflect.TypeOf(continuationDiff{}) == reflect.TypeOf(RuntimeContextDiff[continuationChange]{}) {
		t.Fatal("consumer types became aliases")
	}
}

// TestRuntimeContextShapesRetainStrictDecoding proves that sharing declarations does not add fields
// to the consumer's strict JSON schema or coerce invalid nested values. Semantic provenance, duplicate
// keys, and execution identity remain covered by the existing continuation validation tests.
// TestRuntimeContextShapesRetainStrictDecoding подтверждает, что общие объявления не добавляют полей
// в строгую JSON-схему потребителя и не преобразуют неверные вложенные значения. Семантическое
// происхождение, повторяющиеся ключи и личность исполнения покрыты прежними проверками продолжения.
func TestRuntimeContextShapesRetainStrictDecoding(t *testing.T) {
	for _, raw := range []string{
		`{"metadata":{}}`,
		`{"attempt":"1"}`,
		`{"changes":{}}`,
		`{"changes":[{"metadata":{}}]}`,
		`{"changes":[{"previous":[{"credential":"forbidden"}]}]}`,
		`{"changes":[{"current":[{"version":1.5}]}]}`,
	} {
		var decoded continuationDiff
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&decoded) == nil {
			t.Fatalf("invalid diff decoded: %s", raw)
		}
	}
}
